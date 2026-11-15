// Command finglass-shots exports page data from a Finglass backup for the
// read-only real-data screenshot runner.
//
// It never opens the user's data folder. The backup is restored inside a fresh
// temporary home, the pages are rendered headlessly through internal/app/shell,
// and the snapshot JSON is written to a folder outside the repository.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/bind"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const snapshotName = "snapshot.json"

// runOptions carries the seams the tests need. Production leaves all of them
// empty.
type runOptions struct {
	// tempParent is where the throwaway home is created; "" uses os.TempDir.
	tempParent string
	// now supplies the restore timestamp and the page's current day.
	now func() time.Time
	// afterRestore, when set by a test helper, runs once the restore finished.
	// Production leaves it nil.
	afterRestore func()
}

// interruptCleanup holds the state an interrupt handler needs to undo an
// interrupted run. The handler runs on its own goroutine, so the fields are
// guarded by mu.
type interruptCleanup struct {
	mu            sync.Mutex
	tempHome      string
	session       *shell.Session
	passphrase    []byte
	terminalFD    int
	terminalState *term.State
}

func (c *interruptCleanup) setSession(session *shell.Session) {
	c.mu.Lock()
	c.session = session
	c.mu.Unlock()
}

func (c *interruptCleanup) setPassphrase(passphrase []byte) {
	c.mu.Lock()
	c.passphrase = passphrase
	c.mu.Unlock()
}

func (c *interruptCleanup) setTerminal(fd int, state *term.State) {
	c.mu.Lock()
	c.terminalFD = fd
	c.terminalState = state
	c.mu.Unlock()
}

func (c *interruptCleanup) clearTerminal() {
	c.mu.Lock()
	c.terminalFD = -1
	c.terminalState = nil
	c.mu.Unlock()
}

// cleanUp removes everything an interrupted run may have restored. It is best
// effort: the process exits right after it.
func (c *interruptCleanup) cleanUp() {
	c.mu.Lock()
	tempHome := c.tempHome
	session := c.session
	passphrase := c.passphrase
	terminalFD := c.terminalFD
	terminalState := c.terminalState
	c.session = nil
	c.passphrase = nil
	c.terminalState = nil
	c.mu.Unlock()

	if terminalState != nil && terminalFD >= 0 {
		_ = term.Restore(terminalFD, terminalState)
	}
	if session != nil {
		done := make(chan struct{})
		go func() {
			_ = session.Shutdown()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	if tempHome != "" {
		_ = os.RemoveAll(tempHome)
	}
	wipe(passphrase)
}

// installInterruptHandler removes the temporary copy when the user interrupts
// the exporter. It returns a function that stops listening on a normal return.
func installInterruptHandler(cleanup *interruptCleanup) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-signals:
			cleanup.cleanUp()
			_, _ = fmt.Fprintln(
				os.Stderr,
				"Interrupted; the temporary copy was removed.",
			)
			os.Exit(130)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

// snapshotFile is one JSON file holding every page state in EN and DE.
type snapshotFile struct {
	Version   int                         `json:"version"`
	Languages map[string]snapshotLanguage `json:"languages"`
}

// snapshotLanguage is the bind layer's DTOs for one language.
type snapshotLanguage struct {
	Shell        bind.ShellDTO        `json:"shell"`
	Dashboard    bind.DashboardDTO    `json:"dashboard"`
	Accounts     bind.AccountsDTO     `json:"accounts"`
	Transactions bind.TransactionsDTO `json:"transactions"`
	Documents    bind.DocumentsDTO    `json:"documents"`
	Reports      bind.ReportsDTO      `json:"reports"`
	Settings     bind.SettingsDTO     `json:"settings"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, runOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "finglass-shots: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, options runOptions) error {
	flags := flag.NewFlagSet("finglass-shots", flag.ContinueOnError)
	flags.SetOutput(stderr)
	backupPath := flags.String("backup", "", "Finglass backup ZIP")
	outPath := flags.String("out", "", "output folder outside the repository")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *backupPath == "" {
		return errors.New("--backup is required")
	}
	if *outPath == "" {
		return errors.New("--out is required")
	}

	info, err := os.Stat(*backupPath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("backup %q is not a readable file", *backupPath)
	}
	outDir, err := prepareOutput(*outPath)
	if err != nil {
		return err
	}

	now := time.Now
	if options.now != nil {
		now = options.now
	}

	tempHome, err := os.MkdirTemp(options.tempParent, "finglass-shots-")
	if err != nil {
		return fmt.Errorf("create temporary home: %w", err)
	}
	cleanup := &interruptCleanup{tempHome: tempHome, terminalFD: -1}
	stopInterrupts := installInterruptHandler(cleanup)
	defer func() { _ = os.RemoveAll(tempHome) }()
	defer stopInterrupts()
	if err := os.Chmod(tempHome, 0o700); err != nil { // #nosec G302 -- a directory must be owner-only.
		return fmt.Errorf("restrict temporary home: %w", err)
	}

	previousHome, hadHome := os.LookupEnv("FINGLASS_HOME")
	if err := os.Setenv("FINGLASS_HOME", tempHome); err != nil {
		return fmt.Errorf("set temporary FINGLASS_HOME: %w", err)
	}
	defer func() {
		if hadHome {
			_ = os.Setenv("FINGLASS_HOME", previousHome)
			return
		}
		_ = os.Unsetenv("FINGLASS_HOME")
	}()

	appPaths, err := paths.FromEnv()
	if err != nil {
		return fmt.Errorf("resolve temporary data folder: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home folder: %w", err)
	}
	if err := validateDataFolder(appPaths.Root(), tempHome, homeDir); err != nil {
		return err
	}

	passphrase, err := readPassphrase(stdin, stderr, cleanup.setTerminal)
	cleanup.clearTerminal()
	if err != nil {
		return err
	}
	cleanup.setPassphrase(passphrase)
	defer wipe(passphrase)

	if err := restoreBackup(appPaths, *backupPath, passphrase, now()); err != nil {
		return err
	}
	if options.afterRestore != nil {
		options.afterRestore()
	}

	session, _, refusal := shell.StartOffline(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		now,
		func() civil.Date { return civil.FromTime(now()) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		return fmt.Errorf("start session: %s", refusal.Message(i18n.En))
	}
	cleanup.setSession(session)
	defer func() {
		_ = session.Shutdown()
		cleanup.setSession(nil)
	}()

	if err := session.Unlock(passphrase); err != nil {
		return fmt.Errorf("unlock restored database: %w", err)
	}

	snapshot, err := captureSnapshot(session)
	if err != nil {
		return err
	}
	if err := writeSnapshot(outDir, snapshot); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		stdout,
		"Snapshot written to %s\n",
		filepath.Join(outDir, snapshotName),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(
		stdout,
		"It contains real account data; keep it outside the repository.",
	); err != nil {
		return err
	}
	return nil
}

// prepareOutput validates and creates the output folder.
func prepareOutput(out string) (string, error) {
	absolute, err := filepath.Abs(out)
	if err != nil {
		return "", fmt.Errorf("resolve output folder: %w", err)
	}

	info, statErr := os.Stat(absolute)
	switch {
	case statErr == nil:
		if !info.IsDir() {
			return "", fmt.Errorf("output %q is not a folder", out)
		}
		entries, err := os.ReadDir(absolute)
		if err != nil {
			return "", fmt.Errorf("read output folder: %w", err)
		}
		if len(entries) != 0 {
			return "", fmt.Errorf("output folder %q is not empty", out)
		}
	case !errors.Is(statErr, fs.ErrNotExist):
		return "", fmt.Errorf("read output folder: %w", statErr)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working folder: %w", err)
	}
	if repository, ok := findRepositoryRoot(cwd); ok {
		inside, err := pathIsWithin(repository, absolute)
		if err != nil {
			return "", err
		}
		if inside {
			return "", fmt.Errorf("output folder %q is inside the repository", out)
		}
	}

	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create output folder: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil { // #nosec G302 -- a directory must be owner-only.
		return "", fmt.Errorf("restrict output folder: %w", err)
	}
	return absolute, nil
}

// findRepositoryRoot walks up from start looking for a checkout.
func findRepositoryRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if exists(filepath.Join(dir, ".git")) || exists(filepath.Join(dir, "go.mod")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// validateDataFolder refuses any data folder that could be the user's real one.
func validateDataFolder(dataDir, tempHome, homeDir string) error {
	if homeDir == "" {
		return errors.New("cannot resolve the home folder")
	}
	data, err := canonicalForCompare(dataDir)
	if err != nil {
		return fmt.Errorf("resolve data folder: %w", err)
	}
	temp, err := canonicalForCompare(tempHome)
	if err != nil {
		return fmt.Errorf("resolve temporary home: %w", err)
	}
	defaultDir, err := canonicalForCompare(filepath.Join(homeDir, ".finglass"))
	if err != nil {
		return fmt.Errorf("resolve the default data folder: %w", err)
	}
	findeskDir, err := canonicalForCompare(filepath.Join(homeDir, ".findesk"))
	if err != nil {
		return fmt.Errorf("resolve the FinDesk folder: %w", err)
	}

	if data == defaultDir {
		return errors.New("refusing to open the default data folder ~/.finglass")
	}
	if inside, err := pathIsWithin(findeskDir, data); err != nil {
		return err
	} else if inside {
		return errors.New("refusing to open a data folder inside ~/.findesk")
	}
	if inside, err := pathIsWithin(temp, data); err != nil {
		return err
	} else if !inside {
		return errors.New("refusing to open a data folder outside the temporary home")
	}
	return nil
}

// restoreBackup stages the backup next to an empty throwaway profile and
// applies it, all inside the temporary home.
func restoreBackup(appPaths paths.AppPaths, backupPath string, passphrase []byte, now time.Time) error {
	profileDir, err := appPaths.ProfileDir("default")
	if err != nil {
		return fmt.Errorf("resolve throwaway profile: %w", err)
	}
	if err := paths.EnsureDir(profileDir); err != nil {
		return fmt.Errorf("create throwaway profile: %w", err)
	}

	throwaway, err := throwawayPassphrase()
	if err != nil {
		return err
	}
	defer throwaway.Wipe()

	database, err := db.Create(filepath.Join(profileDir, backup.DBName), throwaway)
	if err != nil {
		return fmt.Errorf("create throwaway database: %w", err)
	}

	backupPassphrase, err := secret.NewPassphrase(passphrase, secret.Unlock)
	if err != nil {
		return errors.New("backup passphrase is empty")
	}
	defer backupPassphrase.Wipe()

	service := backup.NewService(profileDir, bind.Version)
	_, stageErr := service.StageRestore(database, backupPath, backupPassphrase)
	closeErr := database.Close()
	if stageErr != nil {
		return fmt.Errorf("stage restore: %w", stageErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close throwaway database: %w", closeErr)
	}
	if _, _, err := service.ApplyPendingRestore(now.UTC()); err != nil {
		return fmt.Errorf("apply restore: %w", err)
	}
	return nil
}

// throwawayPassphrase builds a random key for the empty database that exists
// only until the restore replaces it.
func throwawayPassphrase() (*secret.Passphrase, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("build throwaway key: %w", err)
	}
	defer wipe(raw)
	encoded := []byte(base64.RawURLEncoding.EncodeToString(raw))
	defer wipe(encoded)
	value, err := secret.NewPassphrase(encoded, secret.New)
	if err != nil {
		return nil, fmt.Errorf("build throwaway key: %w", err)
	}
	return value, nil
}

func readPassphrase(
	stdin io.Reader,
	stderr io.Writer,
	onTerminal func(fd int, state *term.State),
) ([]byte, error) {
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fd := int(file.Fd())
		if onTerminal != nil {
			if state, err := term.GetState(fd); err == nil {
				onTerminal(fd, state)
			}
		}
		_, _ = fmt.Fprint(stderr, "Backup passphrase: ")
		value, err := term.ReadPassword(fd)
		_, _ = fmt.Fprintln(stderr)
		if err != nil {
			return nil, fmt.Errorf("read passphrase: %w", err)
		}
		if len(value) == 0 {
			return nil, errors.New("passphrase is empty")
		}
		return value, nil
	}

	text, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read passphrase: %w", err)
	}
	text = strings.TrimSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\r")
	if text == "" {
		return nil, errors.New("passphrase is empty")
	}
	return []byte(text), nil
}

// captureSnapshot opens every page in EN and DE and reads the bind layer's
// DTOs.
func captureSnapshot(session *shell.Session) (snapshotFile, error) {
	shellService := bind.NewShell(session, nil, bind.ShellOptions{})
	dashboardService := bind.NewDashboard(session, bind.DashboardOptions{})
	accountsService := bind.NewAccounts(session, bind.AccountsOptions{})
	transactionsService := bind.NewTransactions(session, bind.TransactionsOptions{})
	documentsService := bind.NewDocuments(session, bind.DocumentsOptions{})
	reportsService := bind.NewReports(session, bind.ReportsOptions{})
	settingsService := bind.NewSettings(session, bind.SettingsOptions{})

	result := snapshotFile{
		Version:   1,
		Languages: make(map[string]snapshotLanguage, 2),
	}

	for _, language := range []i18n.Language{i18n.En, i18n.De} {
		if err := session.SetLanguage(language); err != nil {
			return snapshotFile{}, fmt.Errorf("set language %s: %w", language.Code(), err)
		}

		var item snapshotLanguage

		session.Navigate("dashboard")
		if err := waitForPage(session, "dashboard", func() bool {
			return !session.DashboardState().IsLoading
		}); err != nil {
			return snapshotFile{}, err
		}
		var err error
		if item.Shell, err = shellService.State(); err != nil {
			return snapshotFile{}, fmt.Errorf("read shell: %w", err)
		}
		if item.Dashboard, err = dashboardService.State(); err != nil {
			return snapshotFile{}, fmt.Errorf("read dashboard: %w", err)
		}

		session.Navigate("accounts")
		if err := waitForPage(session, "accounts", func() bool {
			return !session.AccountsState().IsLoading
		}); err != nil {
			return snapshotFile{}, err
		}
		if item.Accounts, err = accountsService.State(); err != nil {
			return snapshotFile{}, fmt.Errorf("read accounts: %w", err)
		}

		session.Navigate("transactions")
		if err := waitForPage(session, "transactions", func() bool {
			return !session.TransactionsState().IsLoading
		}); err != nil {
			return snapshotFile{}, err
		}
		if item.Transactions, err = transactionsService.State(); err != nil {
			return snapshotFile{}, fmt.Errorf("read transactions: %w", err)
		}

		session.Navigate("documents")
		if err := waitForPage(session, "documents", func() bool {
			return !session.DocumentsState().State.IsLoading
		}); err != nil {
			return snapshotFile{}, err
		}
		if item.Documents, err = documentsService.State(); err != nil {
			return snapshotFile{}, fmt.Errorf("read documents: %w", err)
		}

		session.Navigate("reports")
		if err := waitForPage(session, "reports", func() bool {
			return !session.ReportsState().State.IsLoading
		}); err != nil {
			return snapshotFile{}, err
		}
		if item.Reports, err = reportsService.State(); err != nil {
			return snapshotFile{}, fmt.Errorf("read reports: %w", err)
		}

		session.Navigate("settings")
		if err := waitForPage(session, "settings", func() bool {
			return !session.SettingsState().IsLoading
		}); err != nil {
			return snapshotFile{}, err
		}
		if item.Settings, err = settingsService.State(); err != nil {
			return snapshotFile{}, fmt.Errorf("read settings: %w", err)
		}

		result.Languages[language.Code()] = item
	}
	return result, nil
}

// waitForPage waits until page is current and its load has finished.
func waitForPage(session *shell.Session, page string, ready func() bool) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		if session.CurrentScreen() == page && ready() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the %s page", page)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeSnapshot(outDir string, snapshot snapshotFile) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	data = append(data, '\n')

	target := filepath.Join(outDir, snapshotName)
	temp := target + ".tmp"
	defer func() { _ = os.Remove(temp) }()

	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- temp is inside the validated output folder.
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		return fmt.Errorf("move snapshot into place: %w", err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		return fmt.Errorf("restrict snapshot: %w", err)
	}
	return nil
}

// canonicalForCompare resolves symlinks on the path that exists and keeps the
// missing tail, so a folder that does not exist yet can be compared safely.
func canonicalForCompare(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := absolute
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			parts := append([]string{resolved}, tail...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absolute), nil
		}
		tail = append([]string{filepath.Base(current)}, tail...)
		current = parent
	}
}

// pathIsWithin reports whether child is parent or below it.
func pathIsWithin(parent, child string) (bool, error) {
	canonicalParent, err := canonicalForCompare(parent)
	if err != nil {
		return false, fmt.Errorf("resolve %q: %w", parent, err)
	}
	canonicalChild, err := canonicalForCompare(child)
	if err != nil {
		return false, fmt.Errorf("resolve %q: %w", child, err)
	}
	// Paths on different volumes (C: and D: on Windows) are never inside one
	// another; filepath.Rel reports that as an error. For the temp-home check
	// "not inside" means refuse, so this stays fail-safe.
	if !strings.EqualFold(filepath.VolumeName(canonicalParent), filepath.VolumeName(canonicalChild)) {
		return false, nil
	}
	relative, err := filepath.Rel(canonicalParent, canonicalChild)
	if err != nil {
		return false, fmt.Errorf("compare %q: %w", child, err)
	}
	if relative == "." {
		return true, nil
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
