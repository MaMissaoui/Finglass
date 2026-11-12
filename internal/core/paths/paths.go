// Package paths chooses where Finglass keeps its data on disk.
//
// The root folder is chosen by [Resolve]. Only [FromEnv] reads the environment,
// which keeps the rest of the package pure and easy to test. The layout matches
// `crates/finglass-core/src/paths.rs` exactly, so the Go build and the Rust app
// can share one data folder.
package paths

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNoHome is returned when neither FINGLASS_HOME nor a home folder is
// available.
var ErrNoHome = errors.New("no home folder is available to place the Finglass data in")

// ErrInvalidSlug is returned when a profile slug does not match the allowed
// pattern.
var ErrInvalidSlug = errors.New("invalid profile slug")

// AppPaths is the root of the Finglass data folder, plus helpers for folders
// inside it.
type AppPaths struct {
	root string
}

// Resolve resolves the data root from an optional FINGLASS_HOME value and an
// optional home folder. An empty string means the value is absent.
//
// A non-empty finglassHome wins and is used as given. Otherwise the root is
// <homeDir>/.finglass. When both are empty, [ErrNoHome] is returned.
func Resolve(finglassHome, homeDir string) (AppPaths, error) {
	if finglassHome != "" {
		return AppPaths{root: finglassHome}, nil
	}

	if homeDir != "" {
		return AppPaths{root: filepath.Join(homeDir, ".finglass")}, nil
	}

	return AppPaths{}, ErrNoHome
}

// FromEnv resolves the data root from FINGLASS_HOME and the user's home folder.
//
// This is the only function in this package that reads the environment.
func FromEnv() (AppPaths, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}

	return Resolve(os.Getenv("FINGLASS_HOME"), homeDir)
}

// Root returns the data root folder.
func (p AppPaths) Root() string {
	return p.root
}

// ProfileDir returns the folder for a profile. "default" maps to the root
// itself; other valid slugs map to root/profiles/<slug>.
//
// The slug is validated before it is joined, because filepath.Join cleans ".."
// while Rust's join does not. Validation is the only guard.
func (p AppPaths) ProfileDir(slug string) (string, error) {
	if slug == "default" {
		return p.root, nil
	}

	if !IsValidSlug(slug) {
		return "", ErrInvalidSlug
	}

	return filepath.Join(p.root, "profiles", slug), nil
}

// DatabaseFile returns the database file for a profile.
func (p AppPaths) DatabaseFile(slug string) (string, error) {
	dir, err := p.ProfileDir(slug)
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "finglass.db"), nil
}

// EnsureDir creates a folder and its missing parents with mode 0700.
//
// The mode of an existing folder is left unchanged.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

// IsValidSlug reports whether slug is 1 to 40 ASCII bytes: a lowercase letter
// or digit first, then lowercase letters, digits or hyphens.
func IsValidSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > 40 {
		return false
	}

	first := slug[0]
	validFirst := isLowerAlpha(first) || isDigit(first)
	if !validFirst {
		return false
	}

	for index := 1; index < len(slug); index++ {
		character := slug[index]
		if isLowerAlpha(character) || isDigit(character) || character == '-' {
			continue
		}
		return false
	}

	return true
}

func isLowerAlpha(character byte) bool {
	return character >= 'a' && character <= 'z'
}

func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}
