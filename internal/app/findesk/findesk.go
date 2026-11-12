// Package findesk ports crates/finglass-core/src/app/findesk.rs: UI-free
// helpers and state for the one-time FinDesk import.
//
// The Settings page only picks a folder and asks for FinDesk's passphrase;
// everything else lives here. The state keeps what the dialogs need (the chosen
// folder, a wrong-passphrase flag, the report or a localized error) and runs
// the import into the open profile. The passphrase itself is never stored.
package findesk

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// FindeskStartDir returns the folder the FinDesk picker starts in.
//
// findeskHome is FinDesk's own FINDESK_HOME, read by the caller. Without it the
// default is `home/.findesk`; starting inside the (normally hidden) dot-folder
// is what makes the picker show the database on macOS.
func FindeskStartDir(home string, findeskHome *string) string {
	if findeskHome != nil {
		return *findeskHome
	}
	return filepath.Join(home, ".findesk")
}

// DefaultFindeskStartDir is [FindeskStartDir] for this machine: the user's home
// folder and FinDesk's FINDESK_HOME, if set and not empty. Nil when the home
// folder is unknown. Only the app calls this; tests use [FindeskStartDir].
func DefaultFindeskStartDir() *string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var findeskHome *string
	if value := os.Getenv("FINDESK_HOME"); value != "" {
		copy := value
		findeskHome = &copy
	}

	resolved := FindeskStartDir(home, findeskHome)
	return &resolved
}

// ResolveFindeskDir resolves what the user picked in the folder dialog to the
// folder that holds findesk.db.
//
// Accepted are a folder that holds findesk.db, a FinDesk root whose
// `profiles/<slug>/` folders hold one, and the findesk.db file itself. A
// FinDesk root's own database is its default profile and wins over the named
// profiles; when it has none, the only named profile with a database is used;
// with several, the user must pick one.
func ResolveFindeskDir(chosen string) *string {
	info, err := os.Stat(chosen)
	if err == nil && info.Mode().IsRegular() {
		if filepath.Base(chosen) != "findesk.db" {
			return nil
		}

		parent := filepath.Dir(chosen)
		if parent == "" {
			parent = "."
		}
		return &parent
	}

	info, err = os.Stat(chosen)
	if err != nil || !info.IsDir() {
		return nil
	}

	if isRegularFile(filepath.Join(chosen, "findesk.db")) {
		return &chosen
	}

	profilesDir := filepath.Join(chosen, "profiles")
	if !isDirectory(profilesDir) {
		return nil
	}

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil
	}

	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(profilesDir, entry.Name())
		if isDirectory(path) && isRegularFile(filepath.Join(path, "findesk.db")) {
			candidates = append(candidates, path)
		}
	}

	// Never guess between several profiles: the user then picks the profile's
	// own folder.
	if len(candidates) == 1 {
		return &candidates[0]
	}
	return nil
}

// FindeskImportState is the state behind the FinDesk import dialogs and
// action.
type FindeskImportState struct {
	// Language is the language for user-visible texts.
	Language i18n.Language
	// SourceDir is the resolved FinDesk data folder, once the user picked a
	// valid one.
	SourceDir *string
	// PassphraseWrong says whether the last passphrase was rejected.
	PassphraseWrong bool
	// IsImporting says whether an import is running.
	IsImporting bool
	// Report is the report of a successful import.
	Report *findeskimport.FinDeskImportReport
	// Error is the localized error of a failed import.
	Error *string
}

// NewFindeskImportState builds an empty state for language.
func NewFindeskImportState(language i18n.Language) *FindeskImportState {
	return &FindeskImportState{
		Language:        language,
		SourceDir:       nil,
		PassphraseWrong: false,
		IsImporting:     false,
		Report:          nil,
		Error:           nil,
	}
}

// DefaultFindeskImportState builds the English default state.
func DefaultFindeskImportState() *FindeskImportState {
	return NewFindeskImportState(i18n.En)
}

// Choose validates and remembers chosen, returning the resolved data folder.
func (s *FindeskImportState) Choose(chosen string) *string {
	resolved := ResolveFindeskDir(chosen)
	if resolved == nil {
		return nil
	}
	s.SourceDir = resolved
	s.PassphraseWrong = false
	s.Report = nil
	s.Error = nil
	copy := *resolved
	return &copy
}

// Cancel forgets the chosen folder and closes any pending result.
func (s *FindeskImportState) Cancel() {
	s.SourceDir = nil
	s.PassphraseWrong = false
	s.IsImporting = false
	s.Report = nil
	s.Error = nil
}

// ClearResult drops the success report or the error without forgetting the
// folder.
func (s *FindeskImportState) ClearResult() {
	s.Report = nil
	s.Error = nil
}

// Import runs the import from the chosen folder into profileDir (the documents
// go into its `documents/` subfolder), using offset for FinDesk's zone-less
// dates.
func (s *FindeskImportState) Import(
	database *db.Database,
	profileDir string,
	passphrase *secret.Passphrase,
	offset *time.Location,
) (*findeskimport.FinDeskImportReport, error) {
	if s.SourceDir == nil {
		return s.fail(findeskimport.ErrNotFound)
	}
	sourceDir := *s.SourceDir

	s.IsImporting = true
	documentsDir := filepath.Join(profileDir, "documents")
	report, err := findeskimport.Import(database, documentsDir, sourceDir, passphrase, offset)
	s.IsImporting = false

	if err != nil {
		if errors.Is(err, findeskimport.ErrWrongPassphrase) {
			s.Report = nil
			s.Error = nil
			s.PassphraseWrong = true
			return nil, err
		}
		return s.fail(err)
	}

	s.Report = report
	s.Error = nil
	s.PassphraseWrong = false
	return report, nil
}

// ImportInProfile runs the import into the open Finglass profile, with the
// machine's local UTC offset.
func (s *FindeskImportState) ImportInProfile(
	database *db.Database,
	appPaths paths.AppPaths,
	slug *string,
	passphrase *secret.Passphrase,
) (*findeskimport.FinDeskImportReport, error) {
	value := ""
	if slug != nil {
		value = *slug
	}

	profileDir, err := profiles.DataDir(appPaths, value)
	if err != nil {
		return s.fail(&findeskimport.FinDeskImportError{
			Kind: findeskimport.KindIO,
			Err:  errors.New("profile folder is unavailable"),
		})
	}

	_, seconds := time.Now().Zone()
	offset := time.FixedZone("", seconds)
	return s.Import(database, profileDir, passphrase, offset)
}

// ReportText returns the success report as display text, or nil.
func (s *FindeskImportState) ReportText() *string {
	if s.Report == nil {
		return nil
	}
	text := ReportText(s.Report, s.Language)
	return &text
}

func (s *FindeskImportState) fail(err error) (*findeskimport.FinDeskImportReport, error) {
	s.Report = nil
	s.PassphraseWrong = false
	message := importErrorMessage(err, s.Language)
	s.Error = &message
	return nil, err
}

// ReportText returns the full success report: the one-line summary (which
// already names documents, settings and payment orders) plus why rows were
// skipped.
func ReportText(report *findeskimport.FinDeskImportReport, language i18n.Language) string {
	orphan := report.AccountGroups.SkippedOrphan +
		report.BankConnections.SkippedOrphan +
		report.Accounts.SkippedOrphan +
		report.Categories.SkippedOrphan +
		report.Rules.SkippedOrphan +
		report.Transactions.SkippedOrphan +
		report.DeletedBookings.SkippedOrphan +
		report.PendingBookings.SkippedOrphan +
		report.OCRTexts.SkippedOrphan
	invalid := report.AccountGroups.SkippedInvalid +
		report.BankConnections.SkippedInvalid +
		report.Accounts.SkippedInvalid +
		report.Categories.SkippedInvalid +
		report.Rules.SkippedInvalid +
		report.Transactions.SkippedInvalid +
		report.DeletedBookings.SkippedInvalid +
		report.PendingBookings.SkippedInvalid +
		report.Documents.SkippedInvalid +
		report.OCRTexts.SkippedInvalid
	fractional := report.AccountGroups.SkippedFractionalAmount +
		report.BankConnections.SkippedFractionalAmount +
		report.Accounts.SkippedFractionalAmount +
		report.Categories.SkippedFractionalAmount +
		report.Rules.SkippedFractionalAmount +
		report.Transactions.SkippedFractionalAmount +
		report.DeletedBookings.SkippedFractionalAmount +
		report.PendingBookings.SkippedFractionalAmount +
		report.OCRTexts.SkippedFractionalAmount

	text := report.Summary(language)
	if report.TotalSkipped() > 0 {
		text += "\n"
		text += i18n.Format(
			language,
			i18n.FinDeskImportSkipped,
			i18n.Int(int64(orphan)),
			i18n.Int(int64(invalid)),
			i18n.Int(int64(fractional)),
			i18n.Int(int64(report.Settings.Skipped)),
		)
	}
	return text
}

func importErrorMessage(err error, language i18n.Language) string {
	var importError *findeskimport.FinDeskImportError
	if errors.As(err, &importError) {
		return importError.Message(language)
	}
	return err.Error()
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
