// Package startup ports crates/finglass-core/src/app/startup.rs: which profile
// opens, and the restore that must happen before the database does.
//
// Finglass applies a staged restore before the unlock gate looks at the
// database. The profile's folder is created before the restore is applied,
// because that folder is where the restore puts its `pre-restore-<timestamp>`
// data.
package startup

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

// backupVersion is passed to the backup service. Applying a staged restore
// never reads it; Rust keeps the crate version inside BackupService.
const backupVersion = ""

// SlugKey returns the string form of a profile slug, with "default" for the
// default profile.
//
// This is the one conversion between a nil slug and "default".
func SlugKey(slug *string) string {
	if slug == nil {
		return "default"
	}
	return *slug
}

// Startup is the profile opened at this start and the data a restore moved
// aside, if any.
type Startup struct {
	// Slug is the active profile's slug; nil is the default profile.
	Slug *string
	// RestoredFrom is the pre-restore-<timestamp> folder holding the data the
	// restore replaced.
	RestoredFrom *string
}

// LeftoverKind says why a folder with unencrypted documents is still on disk.
type LeftoverKind uint8

const (
	// LeftoverPreRestore is the data a restore replaced.
	LeftoverPreRestore LeftoverKind = iota
	// LeftoverDeletedProfile is the data folder of a deleted profile.
	LeftoverDeletedProfile
	// LeftoverPendingRestore is a staged restore waiting for the next start.
	LeftoverPendingRestore
)

// Leftover is a folder that still holds unencrypted documents.
type Leftover struct {
	// Kind says where the folder came from.
	Kind LeftoverKind
	// Path is the folder relative to the data root, with "/" separators, for
	// display.
	Path string
}

// Leftovers returns the folders under appPaths that still hold unencrypted
// documents, sorted by path.
//
// Finglass never writes a plaintext database, but the folders it moves aside
// keep a `documents/` folder of unencrypted files. Only a folder with at least
// one regular file under `documents/` is listed. Symlinks are never followed
// and no file contents are read.
func Leftovers(appPaths paths.AppPaths) []Leftover {
	root := appPaths.Root()
	found := make([]Leftover, 0)

	// The default profile is the data root itself.
	scanProfileDir(root, root, &found)

	profilesRoot := filepath.Join(root, profiles.ProfilesDir)
	if isRealDir(profilesRoot) {
		for _, dir := range subdirectories(profilesRoot) {
			scanProfileDir(dir, root, &found)
		}
	}

	deletedRoot := filepath.Join(root, profiles.DeletedDir)
	if isRealDir(deletedRoot) {
		for _, dir := range subdirectories(deletedRoot) {
			if hasDocuments(dir) {
				found = append(found, Leftover{
					Kind: LeftoverDeletedProfile,
					Path: relativePath(root, dir),
				})
			}
		}
	}

	sort.Slice(found, func(left, right int) bool {
		return found[left].Path < found[right].Path
	})
	return found
}

func scanProfileDir(dir, root string, found *[]Leftover) {
	pending := filepath.Join(dir, backup.PendingDir)
	if hasDocuments(pending) {
		*found = append(*found, Leftover{
			Kind: LeftoverPendingRestore,
			Path: relativePath(root, pending),
		})
	}

	for _, folder := range subdirectories(dir) {
		startsWithPrefix := strings.HasPrefix(filepath.Base(folder), backup.PreRestorePrefix)
		if startsWithPrefix && hasDocuments(folder) {
			*found = append(*found, Leftover{
				Kind: LeftoverPreRestore,
				Path: relativePath(root, folder),
			})
		}
	}
}

// subdirectories returns the real subdirectories of dir, without following
// symlinks.
func subdirectories(dir string) []string {
	dirs := make([]string, 0)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dirs
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.IsDir() {
			dirs = append(dirs, filepath.Join(dir, entry.Name()))
		}
	}
	return dirs
}

// isRealDir reports whether path is a directory itself, not a symlink to one.
func isRealDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// hasDocuments reports whether dir/documents holds at least one regular file,
// directly or in a real subfolder.
func hasDocuments(dir string) bool {
	return hasRegularFile(filepath.Join(dir, "documents"))
}

func hasRegularFile(dir string) bool {
	if !isRealDir(dir) {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		switch {
		case info.Mode().IsRegular():
			return true
		case info.IsDir() && hasRegularFile(filepath.Join(dir, entry.Name())):
			return true
		}
	}
	return false
}

// relativePath returns path relative to root with "/" separators, for display.
func relativePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	if relative == "." {
		return ""
	}
	return filepath.ToSlash(relative)
}

// StartupErrorKind says which part of startup preparation failed.
type StartupErrorKind uint8

const (
	// StartupProfile means the profile could not be resolved.
	StartupProfile StartupErrorKind = iota
	// StartupBackup means the staged restore could not be applied.
	StartupBackup
	// StartupIO means the profile folder could not be created.
	StartupIO
)

// StartupError is everything that can go wrong while preparing the startup
// profile.
//
// No value carries a passphrase.
type StartupError struct {
	// Kind says which failure this is.
	Kind StartupErrorKind
	// Err is the underlying error.
	Err error
}

// Error implements error. It never contains a passphrase.
func (e *StartupError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return i18n.Text(i18n.En, i18n.ErrorStartupFailed)
}

// Unwrap returns the underlying error.
func (e *StartupError) Unwrap() error {
	return e.Err
}

// Message returns the localized text shown by the unlock window when startup
// preparation fails.
func (e *StartupError) Message(language i18n.Language) string {
	return i18n.Text(language, i18n.ErrorStartupFailed)
}

// Prepare resolves the active profile and applies a staged restore before the
// database is opened.
//
// A leftover migration staging folder is removed first: after a crash it can
// hold unencrypted document copies.
func Prepare(appPaths paths.AppPaths, now time.Time) (Startup, error) {
	removeMigrationStaging(appPaths)

	profile, err := profiles.Active(appPaths)
	if err != nil {
		return Startup{}, &StartupError{Kind: StartupProfile, Err: err}
	}

	var slug *string
	if profile.Slug != "" {
		copy := profile.Slug
		slug = &copy
	}
	return PrepareProfile(appPaths, slug, now)
}

func removeMigrationStaging(appPaths paths.AppPaths) {
	staging := filepath.Join(appPaths.Root(), migration.StagingDir)
	if isRealDir(staging) {
		_ = os.RemoveAll(staging)
	}
}

// PrepareProfile prepares slug (or the default profile) and applies its staged
// restore.
func PrepareProfile(appPaths paths.AppPaths, slug *string, now time.Time) (Startup, error) {
	value := ""
	if slug != nil {
		value = *slug
	}

	dir, err := profiles.DataDir(appPaths, value)
	if err != nil {
		return Startup{}, &StartupError{Kind: StartupProfile, Err: err}
	}
	if err := paths.EnsureDir(dir); err != nil {
		return Startup{}, &StartupError{Kind: StartupIO, Err: err}
	}
	previous, applied, err := backup.NewService(dir, backupVersion).ApplyPendingRestore(now)
	if err != nil {
		return Startup{}, &StartupError{Kind: StartupBackup, Err: err}
	}

	var restoredFrom *string
	if applied {
		copy := previous
		restoredFrom = &copy
	}

	var startupSlug *string
	if slug != nil {
		copy := *slug
		startupSlug = &copy
	}

	return Startup{Slug: startupSlug, RestoredFrom: restoredFrom}, nil
}
