// Package programs resolves external helper programs to absolute paths.
//
// OCR runs the tesseract and pdftoppm command-line programs. An app started
// from Finder or the Dock gets launchd's minimal PATH
// (/usr/bin:/bin:/usr/sbin:/sbin), which does not contain Homebrew's or
// MacPorts' install folders. Resolving to the absolute path here keeps the
// helpers reachable from a GUI launch.
//
// The result is cached once per process and name: a helper that the user
// installs while Finglass is running is found only after a restart.
package programs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// fallbackDirs are the additional folders searched after PATH on macOS, in
// order.
//
// Homebrew uses /opt/homebrew/bin on Apple silicon and /usr/local/bin on Intel;
// /opt/local/bin is MacPorts. Other systems search only PATH.
var fallbackDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}

type cacheEntry struct {
	path string
	ok   bool
}

var cache = struct {
	sync.Mutex
	entries map[string]cacheEntry
}{entries: make(map[string]cacheEntry)}

// Resolve resolves name to an absolute path, searching once per process.
//
// The result is cached for the life of the process, so installing a helper
// while Finglass runs takes effect only after a restart.
func Resolve(name string) (string, bool) {
	cache.Lock()
	defer cache.Unlock()

	if entry, ok := cache.entries[name]; ok {
		return entry.path, entry.ok
	}

	path, ok := resolveIn(name, searchDirs())
	cache.entries[name] = cacheEntry{path: path, ok: ok}
	return path, ok
}

func searchDirs() []string {
	path, set := os.LookupEnv("PATH")
	return searchDirsWith(path, set)
}

func searchDirsWith(path string, set bool) []string {
	var dirs []string
	if set {
		// Only absolute folders: an empty or relative entry such as "." would
		// run a program from the current folder.
		for _, dir := range filepath.SplitList(path) {
			if filepath.IsAbs(dir) {
				dirs = append(dirs, dir)
			}
		}
	}

	if runtime.GOOS == "darwin" {
		dirs = append(dirs, fallbackDirs...)
	}

	return dirs
}

// resolveIn resolves name against dirs only, without touching the process
// environment.
//
// name must be a bare file name. The first candidate that is a regular file
// with an execute bit wins; symlinks are followed.
func resolveIn(name string, dirs []string) (string, bool) {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", false
	}

	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		if isExecutableFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func isExecutableFile(path string) bool {
	metadata, err := os.Stat(path)
	if err != nil {
		return false
	}
	return metadata.Mode().IsRegular() && isExecutable(metadata)
}
