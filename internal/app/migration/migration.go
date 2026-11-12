// Package migration ports crates/finglass-core/src/app/migration.rs: the
// first-run migration from a FinDesk install.
//
// FinDesk's folder is only ever read. Each selected profile is verified and
// imported by [findeskimport], which copies the database first, into a staging
// folder below the Finglass root; the finished profile is then moved into
// place. The staging folder is removed on every path.
package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// MarkerFile records that the first-run offer was handled.
const MarkerFile = "findesk-migration"

// StagingDir is the folder inside the Finglass root where a profile is
// prepared before it is moved into place.
const StagingDir = ".findesk-migration-staging"

var (
	stagingCounter atomic.Uint64
	markerCounter  atomic.Uint64
)

// failMove is the test-only switch that makes the next [moveIntoPlace] fail
// after the documents and side files were moved.
var failMove atomic.Bool

// nameLower is Rust's `str::to_lowercase`: Unicode full lowercasing, including
// the special cases such as "İ" and a word-final "Σ".
var nameLower = cases.Lower(language.Und)

// FinDeskProfile is one profile of a FinDesk install.
type FinDeskProfile struct {
	// Slug is FinDesk's slug, or nil for its default profile.
	Slug *string
	// Name is the display name.
	Name string
	// DataDir holds findesk.db and documents/.
	DataDir string
}

// FinDeskInstall is a FinDesk install found on this machine.
type FinDeskInstall struct {
	// Root is FinDesk's data root.
	Root string
	// Profiles holds the profiles with a database, the default profile first.
	Profiles []FinDeskProfile
	// ActiveSlug is FinDesk's active profile slug, when one is recorded.
	ActiveSlug *string
}

// MigratedProfile is a profile that was migrated.
type MigratedProfile struct {
	// FinglassSlug is the Finglass slug, or nil for Finglass's default profile.
	FinglassSlug *string
	// Report is what the import did.
	Report findeskimport.FinDeskImportReport
}

// SlugPair maps one FinDesk slug to the Finglass slug it was migrated to. Nil
// means the respective default profile.
type SlugPair struct {
	// FinDesk is FinDesk's slug.
	FinDesk *string
	// Finglass is the Finglass slug.
	Finglass *string
}

// MigrationErrorKind says which part of a migration failed.
type MigrationErrorKind uint8

const (
	// MigrationFinDesk means reading FinDesk's database failed.
	MigrationFinDesk MigrationErrorKind = iota
	// MigrationProfile means creating or renaming the Finglass profile failed.
	MigrationProfile
	// MigrationDatabase means creating the Finglass database failed.
	MigrationDatabase
	// MigrationIO means a file operation failed.
	MigrationIO
	// MigrationTargetNotEmpty means the Finglass target already holds data.
	MigrationTargetNotEmpty
)

// MigrationError is everything that can go wrong while migrating a profile.
//
// No value carries a passphrase.
type MigrationError struct {
	// Kind says which failure this is.
	Kind MigrationErrorKind
	// Err is the underlying error, when there is one.
	Err error
}

// Error implements error. It never contains a passphrase.
func (e *MigrationError) Error() string {
	switch e.Kind {
	case MigrationFinDesk:
		if e.Err != nil {
			return e.Err.Error()
		}
	case MigrationTargetNotEmpty:
		return i18n.Text(i18n.En, i18n.ErrorMigrationTargetNotEmpty)
	default:
		if e.Err != nil {
			return e.Err.Error()
		}
	}
	return i18n.Text(i18n.En, i18n.ErrorUnexpected)
}

// Unwrap returns the underlying error.
func (e *MigrationError) Unwrap() error {
	return e.Err
}

// Message returns the user text in language. Technical failures keep their
// English description.
func (e *MigrationError) Message(language i18n.Language) string {
	switch e.Kind {
	case MigrationFinDesk:
		var importError *findeskimport.FinDeskImportError
		if errors.As(e.Err, &importError) {
			return importError.Message(language)
		}
		if e.Err != nil {
			return e.Err.Error()
		}
	case MigrationTargetNotEmpty:
		return i18n.Text(language, i18n.ErrorMigrationTargetNotEmpty)
	case MigrationProfile, MigrationDatabase, MigrationIO:
		return i18n.Text(language, i18n.ErrorUnexpected)
	}
	return i18n.Text(language, i18n.ErrorUnexpected)
}

// Discover finds the FinDesk profiles under findeskRoot that have a database.
//
// This is read-only: it never follows symlinks and never writes or creates
// anything in the FinDesk folder. A missing profiles.properties means the
// default profile only, named "Personal".
func Discover(findeskRoot string) *FinDeskInstall {
	properties := readProperties(findeskRoot)

	defaultName, ok := properties[profiles.NamePrefix+profiles.DefaultKey]
	if !ok {
		defaultName = profiles.DefaultName
	}

	found := make([]FinDeskProfile, 0, len(properties)+1)

	if isRegularFile(filepath.Join(findeskRoot, "findesk.db")) {
		found = append(found, FinDeskProfile{
			Slug:    nil,
			Name:    defaultName,
			DataDir: findeskRoot,
		})
	}

	named := make([]FinDeskProfile, 0, len(properties))
	for key, name := range properties {
		slug, ok := strings.CutPrefix(key, profiles.NamePrefix)
		if !ok || slug == "" || slug == profiles.DefaultKey || !paths.IsValidSlug(slug) {
			continue
		}

		dataDir := filepath.Join(findeskRoot, profiles.ProfilesDir, slug)
		if !isRegularFile(filepath.Join(dataDir, "findesk.db")) {
			continue
		}

		slugCopy := slug
		named = append(named, FinDeskProfile{
			Slug:    &slugCopy,
			Name:    name,
			DataDir: dataDir,
		})
	}

	sort.SliceStable(named, func(left, right int) bool {
		leftName := nameLower.String(named[left].Name)
		rightName := nameLower.String(named[right].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return *named[left].Slug < *named[right].Slug
	})
	found = append(found, named...)

	if len(found) == 0 {
		return nil
	}

	var activeSlug *string
	if value, ok := properties["active"]; ok && value != "" {
		copy := value
		activeSlug = &copy
	}

	return &FinDeskInstall{
		Root:       findeskRoot,
		Profiles:   found,
		ActiveSlug: activeSlug,
	}
}

// ShouldOffer reports whether the unlock window should offer the migration.
//
// All four conditions must hold: no Finglass default database, no named
// Finglass profile, no marker, and a FinDesk install with at least one profile
// that has a database.
func ShouldOffer(appPaths paths.AppPaths, install *FinDeskInstall) bool {
	if install == nil || len(install.Profiles) == 0 {
		return false
	}
	if file, err := appPaths.DatabaseFile(profiles.DefaultKey); err == nil && pathExists(file) {
		return false
	}
	if !listsNoNamedProfiles(appPaths) {
		return false
	}
	return !pathExists(filepath.Join(appPaths.Root(), MarkerFile))
}

// WriteMarker writes the marker atomically, recording whether the user
// migrated or chose an empty start.
func WriteMarker(appPaths paths.AppPaths, migrate bool, now time.Time) error {
	if err := paths.EnsureDir(appPaths.Root()); err != nil {
		return err
	}

	action := "skip"
	if migrate {
		action = "migrate"
	}
	timestamp, err := formatRFC3339(now)
	if err != nil {
		return err
	}

	target := filepath.Join(appPaths.Root(), MarkerFile)
	temp := markerTempPath(appPaths.Root())

	write := func() error {
		file, err := os.Create(temp) // #nosec G304 -- temp is inside the Finglass root.
		if err != nil {
			return err
		}
		if _, err := io.WriteString(file, action+" "+timestamp+"\n"); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}

		if runtime.GOOS == "windows" && pathExists(target) {
			if err := os.Remove(target); err != nil {
				return err
			}
		}

		return os.Rename(temp, target)
	}

	if err := write(); err != nil {
		_ = os.Remove(temp)
		return err
	}

	return nil
}

// markerTempPath builds the temporary marker name with the process id and a
// per-process counter.
func markerTempPath(root string) string {
	counter := markerCounter.Add(1) - 1
	return filepath.Join(root, fmt.Sprintf(".findesk-migration-%d-%d.tmp", os.Getpid(), counter))
}

// formatRFC3339 formats now exactly as time 0.3.55's Rfc3339 formatter does:
// four-digit year, a "Z" for UTC or ±HH:MM otherwise, and a non-zero fraction
// with trailing zeroes removed.
func formatRFC3339(now time.Time) (string, error) {
	year := now.Year()
	if year < 0 || year > 9999 {
		return "", errors.New("could not format the migration time")
	}

	_, offset := now.Zone()
	absolute := offset
	if absolute < 0 {
		absolute = -absolute
	}
	if absolute/3600 > 23 || absolute%60 != 0 {
		return "", errors.New("could not format the migration time")
	}

	text := fmt.Sprintf(
		"%04d-%02d-%02dT%02d:%02d:%02d",
		year,
		int(now.Month()),
		now.Day(),
		now.Hour(),
		now.Minute(),
		now.Second(),
	)

	if nanos := now.Nanosecond(); nanos != 0 {
		fraction := strings.TrimRight(fmt.Sprintf("%09d", nanos), "0")
		text += "." + fraction
	}

	if offset == 0 {
		return text + "Z", nil
	}

	sign := "+"
	if offset < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%s%02d:%02d", text, sign, absolute/3600, (absolute/60)%60), nil
}

// ActiveAfter returns the Finglass slug that should be active after the
// migration.
//
// migrated holds FinDesk slug → Finglass slug pairs. When FinDesk's active
// profile was not migrated, the default Finglass profile (nil) is chosen.
func ActiveAfter(install *FinDeskInstall, migrated []SlugPair) *string {
	if install == nil {
		return nil
	}
	for _, pair := range migrated {
		if sameSlug(pair.FinDesk, install.ActiveSlug) {
			return cloneSlug(pair.Finglass)
		}
	}
	return nil
}

// MigrateProfile verifies the FinDesk passphrase, imports the profile into a
// fresh Finglass profile and moves it into place.
//
// The staging folder is always removed, also when an error occurs. Nothing is
// written to FinDesk's folder.
func MigrateProfile(
	appPaths paths.AppPaths,
	profile *FinDeskProfile,
	findesk *secret.Passphrase,
	finglass *secret.Passphrase,
	offset *time.Location,
) (*MigratedProfile, error) {
	if err := findeskimport.VerifyPassphrase(profile.DataDir, findesk); err != nil {
		return nil, &MigrationError{Kind: MigrationFinDesk, Err: err}
	}

	staging, err := createStaging(appPaths)
	if err != nil {
		return nil, &MigrationError{Kind: MigrationIO, Err: err}
	}

	result, resultErr := migrateInStaging(appPaths, profile, findesk, finglass, offset, staging)
	_ = os.RemoveAll(staging)
	// Also drop the now empty parent; a crash before this leaves it for startup
	// to remove.
	_ = removeDirectory(filepath.Dir(staging))
	return result, resultErr
}

func migrateInStaging(
	appPaths paths.AppPaths,
	profile *FinDeskProfile,
	findesk *secret.Passphrase,
	finglass *secret.Passphrase,
	offset *time.Location,
	staging string,
) (*MigratedProfile, error) {
	stagingDB := filepath.Join(staging, "finglass.db")
	stagingDocuments := filepath.Join(staging, "documents")

	// The database handle is closed before anything is moved.
	database, err := db.Create(stagingDB, finglass)
	if err != nil {
		return nil, &MigrationError{Kind: MigrationDatabase, Err: err}
	}
	if err := audit.Log(
		context.Background(),
		database,
		"DATABASE_CREATED",
		audit.ActorUser,
		"New encrypted database",
	); err != nil {
		_ = database.Close()
		return nil, &MigrationError{Kind: MigrationDatabase, Err: err}
	}

	report, importErr := findeskimport.Import(database, stagingDocuments, profile.DataDir, findesk, offset)
	_ = database.Close()
	if importErr != nil {
		return nil, &MigrationError{Kind: MigrationFinDesk, Err: importErr}
	}

	if profile.Slug == nil {
		target := appPaths.Root()
		databaseExists := false
		if file, err := appPaths.DatabaseFile(profiles.DefaultKey); err == nil {
			databaseExists = pathExists(file)
		}
		if databaseExists ||
			pathExists(filepath.Join(target, "findesk.db")) ||
			!documentsAbsentOrEmpty(filepath.Join(target, "documents")) {
			return nil, &MigrationError{Kind: MigrationTargetNotEmpty}
		}

		if err := moveIntoPlace(staging, target); err != nil {
			return nil, &MigrationError{Kind: MigrationIO, Err: err}
		}

		currentName := ""
		if listed, err := profiles.List(appPaths); err == nil && len(listed) > 0 {
			currentName = listed[0].Name
		}
		if currentName != profile.Name {
			if err := profiles.Rename(appPaths, "", profile.Name); err != nil {
				return nil, &MigrationError{Kind: MigrationProfile, Err: err}
			}
		}

		return &MigratedProfile{FinglassSlug: nil, Report: *report}, nil
	}

	created, err := profiles.Create(appPaths, profile.Name)
	if err != nil {
		return nil, &MigrationError{Kind: MigrationProfile, Err: err}
	}
	if err := moveIntoPlace(staging, created.Dir); err != nil {
		rollBackCreatedProfile(appPaths, staging, created)
		return nil, &MigrationError{Kind: MigrationIO, Err: err}
	}

	slug := created.Slug
	return &MigratedProfile{FinglassSlug: &slug, Report: *report}, nil
}

// rollBackCreatedProfile undoes profiles.Create after a failed move, best
// effort.
//
// Files that already reached the profile folder go back to staging, and the
// profile is removed and unregistered only when its folder is empty again. A
// folder that still holds unexpected files stays registered so nothing is
// lost.
func rollBackCreatedProfile(appPaths paths.AppPaths, staging string, created profiles.Profile) {
	moveBackToStaging(created.Dir, staging)

	entries, err := os.ReadDir(created.Dir)
	empty := err == nil && len(entries) == 0
	if !empty {
		return
	}
	if removeDirectory(created.Dir) == nil && created.Slug != "" {
		_ = profiles.ForgetCreated(appPaths, created.Slug)
	}
}

// moveBackToStaging moves whatever moveIntoPlace already placed back into
// staging, leaving conflicts alone.
func moveBackToStaging(target, staging string) {
	for _, name := range []string{"documents", "finglass.db-wal", "finglass.db-shm", "finglass.db"} {
		placed := filepath.Join(target, name)
		back := filepath.Join(staging, name)
		if pathExists(placed) && !pathExists(back) {
			_ = os.Rename(placed, back)
		}
	}
}

func createStaging(appPaths paths.AppPaths) (string, error) {
	root := filepath.Join(appPaths.Root(), StagingDir)
	if err := paths.EnsureDir(root); err != nil {
		return "", err
	}

	for {
		number := stagingCounter.Add(1) - 1
		candidate := filepath.Join(root, strconv.FormatUint(number, 10))
		err := os.Mkdir(candidate, 0o700)
		if err == nil {
			return candidate, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return "", err
	}
}

// moveIntoPlace moves the documents first, then the database's side files and
// the database itself last, so a profile folder never holds a database whose
// documents or WAL are still in staging.
func moveIntoPlace(staging, target string) error {
	// The database must be there: a profile without one would be reported as
	// migrated but empty.
	database := filepath.Join(staging, "finglass.db")
	if !isRegularFile(database) {
		return fmt.Errorf("the staged database is missing: %w", fs.ErrNotExist)
	}

	stagingDocuments := filepath.Join(staging, "documents")
	if pathExists(stagingDocuments) {
		targetDocuments := filepath.Join(target, "documents")
		if pathExists(targetDocuments) {
			// Only an empty folder can be replaced here; a non-empty one was
			// refused before.
			if err := removeDirectory(targetDocuments); err != nil {
				return err
			}
		}
		if err := os.Rename(stagingDocuments, targetDocuments); err != nil {
			return err
		}
	}

	for _, name := range []string{"finglass.db-wal", "finglass.db-shm"} {
		source := filepath.Join(staging, name)
		if pathExists(source) {
			if err := os.Rename(source, filepath.Join(target, name)); err != nil {
				return err
			}
		}
	}

	if failMoveBeforeDatabase() {
		return errors.New("injected move failure")
	}

	return os.Rename(database, filepath.Join(target, "finglass.db"))
}

// failNextMoveBeforeDatabase arms the next failure between the documents and
// the database.
func failNextMoveBeforeDatabase() {
	failMove.Store(true)
}

func failMoveBeforeDatabase() bool {
	return failMove.Swap(false)
}

func documentsAbsentOrEmpty(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return errors.Is(err, fs.ErrNotExist)
	}
	if !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

func listsNoNamedProfiles(appPaths paths.AppPaths) bool {
	data, err := os.ReadFile(filepath.Join(appPaths.Root(), "profiles.properties")) // #nosec G304 -- inside the Finglass root.
	if err != nil || !utf8.Valid(data) {
		data = nil
	}
	properties := profiles.ParseProperties(string(data))

	for key := range properties {
		if slug, ok := strings.CutPrefix(key, profiles.NamePrefix); ok &&
			slug != "" &&
			slug != profiles.DefaultKey {
			return false
		}
	}
	return true
}

func readProperties(root string) map[string]string {
	data, err := os.ReadFile(filepath.Join(root, "profiles.properties")) // #nosec G304 -- inside the selected FinDesk root.
	if err != nil || !utf8.Valid(data) {
		return map[string]string{}
	}
	return profiles.ParseProperties(string(data))
}

func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// removeDirectory mirrors Rust's fs::remove_dir: it never removes a file or a
// symlink, and it only removes an empty folder.
func removeDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("remove %s: not a directory", path)
	}
	return os.Remove(path)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sameSlug(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneSlug(slug *string) *string {
	if slug == nil {
		return nil
	}
	copy := *slug
	return &copy
}
