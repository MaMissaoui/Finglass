// Package backup writes and restores Finglass backups.
//
// A backup is a ZIP file with a consistent copy of the encrypted database, the
// files in `documents/` and a small manifest. Writing starts at a private
// temporary file that is owner-only from the first byte and is moved into place
// only once the backup is complete, so a failed run leaves no partial or
// world-readable ZIP behind.
//
// Restoring replaces the live database, so it happens in two phases:
// [Service.StageRestore] validates the backup and unpacks it next to the data,
// and [Service.ApplyPendingRestore] moves the current data aside into
// `pre-restore-<timestamp>/` and puts the staged data in place. The second phase
// must run before the database is opened.
//
// It ports `crates/finglass-core/src/backup.rs`.
package backup

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// DBName is the database file inside a profile and inside a backup.
const DBName = "finglass.db"

// Manifest is the backup manifest.
const Manifest = "finglass-backup.properties"

// PendingDir holds a staged restore until the next start.
const PendingDir = "restore-pending"

// PreRestorePrefix starts the folders that hold the data a restore replaced.
const PreRestorePrefix = "pre-restore-"

// MaxEntryBytes is the upper bound for one entry, whatever the ZIP declares.
const MaxEntryBytes uint64 = 2 * 1024 * 1024 * 1024

// MaxRestoreBytes is the upper bound for everything a restore unpacks, whatever
// the ZIP declares.
const MaxRestoreBytes uint64 = 8 * 1024 * 1024 * 1024

// contentDirs are the content folders packed into a backup. Finglass has no
// `passports/` folder (no HBCI4Java); a later folder can be added here without
// touching the rest of the module.
var contentDirs = []string{"documents"}

// bufferSize is the size of the copy buffer. It counts bytes while copying
// instead of trusting the ZIP's declared sizes.
const bufferSize = 8 * 1024

// stampLayout is the timestamp format used for `pre-restore-<timestamp>`
// folders. It is Rust's `[year][month][day]-[hour][minute][second]`.
const stampLayout = "20060102-150405"

// Info is what a backup says about itself.
type Info struct {
	// Created is when the backup was written, as text.
	Created string
	// AppVersion is the Finglass version that wrote it.
	AppVersion string
	// Transactions is the number of transactions in the database copy.
	Transactions int64
	// Documents is the number of files in the content folders.
	Documents int
}

// Service writes and validates backups for one profile.
type Service struct {
	profileDir      string
	appVersion      string
	maxRestoreBytes uint64
}

// NewService creates a service for the profile at profileDir.
//
// Rust writes `crate::VERSION` into the manifest. Go's core may not import
// `internal/bind`, so the caller passes `bind.Version` as appVersion.
func NewService(profileDir, appVersion string) *Service {
	return &Service{
		profileDir:      profileDir,
		appVersion:      appVersion,
		maxRestoreBytes: MaxRestoreBytes,
	}
}

// WithMaxRestoreBytes overrides the total restore limit. Only useful for tests.
func (s *Service) WithMaxRestoreBytes(n uint64) *Service {
	s.maxRestoreBytes = n
	return s
}

// HasPendingRestore reports whether a staged restore is waiting for the next
// start.
func (s *Service) HasPendingRestore() bool {
	info, err := os.Stat(s.pendingDir())
	return err == nil && info.IsDir()
}

// CreateBackup writes a backup of the current data to target.
func (s *Service) CreateBackup(d *db.Database, target string) (Info, error) {
	targetDir := filepath.Dir(target)
	if targetDir == "" {
		targetDir = "."
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return Info{}, err
	}

	tmpDB := uniqueTempPath(s.profileDir, "finglass-backup", ".db.tmp")
	_ = os.Remove(tmpDB)
	tmpZip, err := createPrivateTempFile(targetDir, "finglass-backup", ".zip.tmp")
	if err != nil {
		return Info{}, err
	}

	info, result := func() (Info, error) {
		// VACUUM INTO gives a consistent, compacted copy even while the
		// database is open. The target path is bound as a parameter, never
		// formatted into SQL.
		if err := d.VacuumInto(tmpDB); err != nil {
			return Info{}, err
		}

		tmpZipCanonical, err := canonicalOrAbsolute(tmpZip)
		if err != nil {
			return Info{}, err
		}
		targetCanonical, err := canonicalOrAbsolute(target)
		if err != nil {
			return Info{}, err
		}
		content, err := s.contentFiles([]string{tmpZipCanonical, targetCanonical})
		if err != nil {
			return Info{}, err
		}

		// Refused up front rather than written: StageRestore would reject it,
		// leaving a backup that cannot be restored.
		dbInfo, err := os.Stat(tmpDB)
		if err != nil {
			return Info{}, err
		}
		dbLen := dbInfo.Size()
		if dbLen < 0 || dbLen > int64(MaxEntryBytes) {
			return Info{}, ErrTooLarge
		}
		total := dbLen
		for _, file := range content {
			fileInfo, err := os.Stat(file.path)
			if err != nil {
				return Info{}, err
			}
			length := fileInfo.Size()
			if length < 0 || length > int64(MaxEntryBytes) {
				return Info{}, ErrTooLarge
			}
			if total > math.MaxInt64-length {
				total = math.MaxInt64
			} else {
				total += length
			}
		}
		if total < 0 || uint64(total) > s.maxRestoreBytes {
			return Info{}, ErrTooLarge
		}

		transactions, err := attachedTransactionCount(d, tmpDB)
		if err != nil {
			return Info{}, err
		}
		info := Info{
			Created:      time.Now().UTC().Format(time.RFC3339Nano),
			AppVersion:   s.appVersion,
			Transactions: transactions,
			Documents:    len(content),
		}

		if err := writeZip(tmpZip, info, tmpDB, content); err != nil {
			return Info{}, err
		}
		if err := moveIntoPlace(tmpZip, target); err != nil {
			return Info{}, err
		}
		secureFile(target)
		return info, nil
	}()

	_ = os.Remove(tmpDB)
	_ = os.Remove(tmpZip)
	return info, result
}

// Inspect reads the manifest of a backup without changing anything.
//
// Missing `created` and `appVersion` values become "?", missing or unparsable
// counts become 0.
func (s *Service) Inspect(backup string) (Info, error) {
	reader, err := zipOpenReader(backup)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = reader.Close() }()

	manifestEntry := lastEntryNamed(reader, Manifest)
	if manifestEntry == nil {
		return Info{}, ErrNotABackup
	}

	manifest, err := readEntryText(manifestEntry)
	if err != nil {
		return Info{}, err
	}

	if lastEntryNamed(reader, DBName) == nil {
		return Info{}, ErrNoDatabase
	}

	return infoFromProperties(manifest), nil
}

// StageRestore validates backup and unpacks it into the pending-restore folder.
//
// Nothing live is touched. When the staged database cannot be opened with the
// live key, backupPassphrase is required: nil means Rust's `None` and returns
// [PassphraseRequiredError] with WrongPassphraseGiven false, a wrong one turns
// it true. A failed stage removes the staging folder again.
//
// Opening the staged copy with its own passphrase runs migrations on the staged
// copy; it never changes the live database.
func (s *Service) StageRestore(d *db.Database, backup string, backupPassphrase *secret.Passphrase) (Info, error) {
	info, err := s.Inspect(backup)
	if err != nil {
		return Info{}, err
	}

	pending := s.pendingDir()
	if err := removeDirAllIfExists(pending); err != nil {
		return Info{}, err
	}
	if err := secureDirectory(pending); err != nil {
		return Info{}, err
	}

	result := func() error {
		reader, err := zipOpenReader(backup)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()

		budget := s.maxRestoreBytes

		for _, entry := range reader.File {
			if isDirEntry(entry.Name) {
				continue
			}

			name := entry.Name
			if name == Manifest {
				continue
			}

			target, err := safeTarget(pending, name)
			if err != nil {
				return err
			}

			// Reject an entry whose declared size is too large; the real limit
			// is enforced while copying below, because the archive's own size
			// is just a claim.
			if entry.UncompressedSize64 > MaxEntryBytes {
				return ErrTooLarge
			}

			if parent := filepath.Dir(target); parent != "" {
				if err := secureDirectory(parent); err != nil {
					return err
				}
			}

			limit := MaxEntryBytes
			if budget < limit {
				limit = budget
			}

			source, err := entry.Open()
			if err != nil {
				return err
			}
			written, err := copyAtMost(source, target, limit)
			_ = source.Close()
			if err != nil {
				return err
			}
			if written > limit {
				return ErrTooLarge
			}
			budget -= written
			secureFile(target)
		}

		stagedDB := filepath.Join(pending, DBName)
		stagedInfo, err := os.Stat(stagedDB)
		if err != nil || !stagedInfo.Mode().IsRegular() {
			return ErrNoDatabase
		}

		readable, err := d.CanReadWithCurrentKey(stagedDB)
		if err != nil {
			var tooNew *db.TooNewError
			if errors.As(err, &tooNew) {
				return ErrTooNew
			}
			return err
		}

		if !readable {
			if backupPassphrase == nil {
				return &PassphraseRequiredError{WrongPassphraseGiven: false}
			}

			staged, err := db.Open(stagedDB, backupPassphrase)
			if err != nil {
				if errors.Is(err, db.ErrWrongPassphraseOrDamaged) {
					return &PassphraseRequiredError{WrongPassphraseGiven: true}
				}
				var tooNew *db.TooNewError
				if errors.As(err, &tooNew) {
					return ErrTooNew
				}
				return err
			}
			// The staged copy is migrated here. That is accepted and cannot
			// touch the live database.
			_ = staged.Close()
		}

		return nil
	}()

	if result != nil {
		_ = os.RemoveAll(pending)
		return Info{}, result
	}

	return info, nil
}

// CancelPendingRestore removes a staged restore.
func (s *Service) CancelPendingRestore() {
	_ = os.RemoveAll(s.pendingDir())
}

// ApplyPendingRestore moves the current data into `pre-restore-<timestamp>/`
// and puts the staged data in place, when a restore is staged. It must run
// before the database is opened.
//
// It returns the folder holding the previous data and whether a restore was
// applied.
func (s *Service) ApplyPendingRestore(now time.Time) (previous string, applied bool, err error) {
	pending := s.pendingDir()

	stagedDB := filepath.Join(pending, DBName)
	if !isRegularFile(stagedDB) {
		if err := removeDirAllIfExists(pending); err != nil {
			return "", false, err
		}
		return "", false, nil
	}

	previous = filepath.Join(s.profileDir, PreRestorePrefix+now.Format(stampLayout))
	if err := secureDirectory(previous); err != nil {
		return "", false, err
	}

	currentNames := []string{DBName}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		currentNames = append(currentNames, DBName+suffix)
	}
	currentNames = append(currentNames, contentDirs...)

	for _, name := range currentNames {
		current := filepath.Join(s.profileDir, name)
		if pathExists(current) {
			if err := os.Rename(current, filepath.Join(previous, name)); err != nil {
				return "", false, err
			}
		}
	}

	stagedNames := []string{DBName}
	stagedNames = append(stagedNames, contentDirs...)

	for _, name := range stagedNames {
		staged := filepath.Join(pending, name)
		if pathExists(staged) {
			if err := os.Rename(staged, filepath.Join(s.profileDir, name)); err != nil {
				return "", false, err
			}
		}
	}

	if err := removeDirAllIfExists(pending); err != nil {
		return "", false, err
	}
	return previous, true, nil
}

func (s *Service) pendingDir() string {
	return filepath.Join(s.profileDir, PendingDir)
}

// contentFiles lists regular files in the content folders, sorted by name and
// excluding the backup and its temporary ZIP.
//
// Symlinks are skipped: following them could pack a file from outside the
// profile.
func (s *Service) contentFiles(excluded []string) ([]contentFile, error) {
	var files []contentFile

	for _, dirName := range contentDirs {
		dir := filepath.Join(s.profileDir, dirName)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		// os.ReadDir sorts by file name, the same byte order as Rust's
		// OsString sort.

		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())

			metadata, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			if !metadata.Mode().IsRegular() {
				continue
			}

			canonical, err := canonicalPath(path)
			if err != nil {
				return nil, err
			}
			if containsPath(excluded, canonical) {
				continue
			}

			files = append(files, contentFile{
				name: dirName + "/" + entry.Name(),
				path: path,
			})
		}
	}

	return files, nil
}

// contentFile is one regular file packed into a backup.
type contentFile struct {
	// name is the entry name inside the ZIP.
	name string
	// path is the file's location in the profile.
	path string
}

// infoFromProperties reads a manifest the way Rust's `inspect` does.
func infoFromProperties(manifest string) Info {
	properties := parseProperties(manifest)

	get := func(key string) string {
		value, ok := properties[key]
		if !ok {
			return "?"
		}
		return value
	}

	transactions := int64(0)
	if text, ok := properties["transactions"]; ok {
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
			transactions = parsed
		}
	}

	documents := 0
	if text, ok := properties["documents"]; ok {
		if parsed, err := strconv.Atoi(text); err == nil && parsed >= 0 {
			documents = parsed
		}
	}

	return Info{
		Created:      get("created"),
		AppVersion:   get("appVersion"),
		Transactions: transactions,
		Documents:    documents,
	}
}

// zipOpenReader opens a ZIP archive. Go's `zip.ErrInsecurePath` is not a failure
// by itself: the reader it returns is still used, and safeTarget decides, as
// Rust does.
func zipOpenReader(path string) (*zip.ReadCloser, error) {
	reader, err := zip.OpenReader(path)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, err
	}
	if reader == nil {
		return nil, err
	}
	return reader, nil
}

// lastEntryNamed returns the last entry with name, matching the zip crate's
// name index, or nil when there is none.
func lastEntryNamed(reader *zip.ReadCloser, name string) *zip.File {
	var found *zip.File
	for _, entry := range reader.File {
		if entry.Name == name {
			found = entry
		}
	}
	return found
}

// readEntryText reads an entry and requires valid UTF-8, as Rust's
// `read_to_string` does.
func readEntryText(entry *zip.File) (string, error) {
	source, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = source.Close() }()

	text, err := io.ReadAll(source)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(text) {
		return "", errors.New("stream did not contain valid UTF-8")
	}
	return string(text), nil
}

func containsPath(paths []string, path string) bool {
	for _, candidate := range paths {
		if candidate == path {
			return true
		}
	}
	return false
}

// uniqueTempPath builds a unique temporary path without creating it.
func uniqueTempPath(dir, prefix, suffix string) string {
	nanos := time.Now().UnixNano()
	if nanos < 0 {
		nanos = 0
	}
	counter := tempPathCounter.Add(1) - 1

	return filepath.Join(
		dir,
		fmt.Sprintf("%s-%d-%d-%d%s", prefix, os.Getpid(), nanos, counter, suffix),
	)
}

// tempPathCounter makes temporary names unique within the process.
var tempPathCounter atomic.Uint64

// secureDirectory creates path and restricts it to the owner. Permission
// failures are best effort.
func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}

	_ = chmod(path, 0o700)

	return nil
}

// secureFile restricts an existing file to the owner. Permission failures are
// best effort.
func secureFile(path string) {
	_ = chmod(path, 0o600)
}

// pathExists reports whether path exists, following symlinks like Rust's
// `Path::exists`.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isRegularFile reports whether path is a regular file, following symlinks like
// Rust's `Path::is_file`.
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isDirEntry reports whether a ZIP entry name denotes a directory, as Rust's
// `is_dir` does.
func isDirEntry(name string) bool {
	return strings.HasSuffix(name, "/") || strings.HasSuffix(name, "\\")
}

// unicodeWhitespaceIndex returns the byte index of the first Unicode whitespace
// rune in line, or -1.
func unicodeWhitespaceIndex(line string) int {
	for index, character := range line {
		if unicode.IsSpace(character) {
			return index
		}
	}
	return -1
}
