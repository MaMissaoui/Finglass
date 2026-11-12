package instance

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// helperTimeout bounds every wait on the helper process, so a failure cannot
// hang CI.
const helperTimeout = 30 * time.Second

func testPaths(t *testing.T) paths.AppPaths {
	t.Helper()

	p, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return p
}

func fileNames(t *testing.T, root string) []string {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func TestAcquireCreatesTheLockFileInAFreshRoot(t *testing.T) {
	p := testPaths(t)

	lock, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	if info, err := os.Stat(filepath.Join(p.Root(), LockFile)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("lock file is not a regular file: %v", err)
	}

	if got, want := fileNames(t, p.Root()), []string{LockFile}; !equalStrings(got, want) {
		t.Errorf("files = %v, want %v (the lock file is the only file created)", got, want)
	}
}

func TestASecondAcquireFailsWhileTheFirstIsAlive(t *testing.T) {
	p := testPaths(t)

	first, err := Acquire(p)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer func() { _ = first.Release() }()

	_, err = Acquire(p)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Acquire error = %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquiringWorksAgainAfterTheFirstIsDropped(t *testing.T) {
	p := testPaths(t)

	first, err := Acquire(p)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	second, err := Acquire(p)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

func TestDifferentRootsCanBeLockedAtTheSameTime(t *testing.T) {
	first, err := Acquire(testPaths(t))
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer func() { _ = first.Release() }()

	second, err := Acquire(testPaths(t))
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	defer func() { _ = second.Release() }()
}

func TestReleaseTwiceIsSafe(t *testing.T) {
	p := testPaths(t)

	lock, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}

	if _, err := os.Stat(filepath.Join(p.Root(), LockFile)); err != nil {
		t.Errorf("the lock file should stay on disk: %v", err)
	}
}

// TestHelperHoldLock is the child half of TestAnotherProcessIsExcluded. It
// returns immediately when the environment variable is unset, so a normal test
// run simply passes it.
func TestHelperHoldLock(t *testing.T) {
	root := os.Getenv("FINGLASS_LOCK_HELPER_ROOT")
	if root == "" {
		return
	}

	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	lock, err := Acquire(p)
	if err != nil {
		t.Fatalf("helper Acquire: %v", err)
	}
	defer func() { _ = lock.Release() }()

	if _, err := fmt.Fprintln(os.Stdout, "locked"); err != nil {
		t.Fatalf("print locked: %v", err)
	}

	// Block until the parent closes stdin.
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestAnotherProcessIsExcluded(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// #nosec G204,G702 -- the command is this test binary, os.Args[0].
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperHoldLock")
	cmd.Env = append(os.Environ(), "FINGLASS_LOCK_HELPER_ROOT="+root)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	locked := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() && scanner.Text() == "locked" {
			locked <- nil
			return
		}
		if err := scanner.Err(); err != nil {
			locked <- fmt.Errorf("read helper output: %w", err)
			return
		}
		locked <- errors.New("helper exited without printing locked")
	}()

	select {
	case err := <-locked:
		if err != nil {
			_ = cmd.Process.Kill()
			t.Fatalf("%v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(helperTimeout):
		_ = cmd.Process.Kill()
		t.Fatalf("helper did not take the lock within %s\nstderr: %s", helperTimeout, stderr.String())
	}

	if _, err := Acquire(p); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Acquire while helper holds the lock = %v, want ErrAlreadyRunning", err)
	}

	if err := stdin.Close(); err != nil {
		t.Fatalf("close helper stdin: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("helper exit: %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(helperTimeout):
		_ = cmd.Process.Kill()
		t.Fatalf("helper did not exit within %s\nstderr: %s", helperTimeout, stderr.String())
	}

	lock, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire after the helper exited: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func equalStrings(left, right []string) bool {
	return slices.Equal(left, right)
}
