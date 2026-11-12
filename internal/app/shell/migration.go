package shell

import (
	"errors"
	"time"

	coremigration "github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

var (
	// ErrMigrationBusy means the wizard is running a migration.
	ErrMigrationBusy = errors.New("the migration wizard is busy")
	// ErrNoMigrationProfile means every FinDesk profile was handled.
	ErrNoMigrationProfile = errors.New("no FinDesk profile is current")
)

// MigrationStep is one page of the first-run migration wizard.
type MigrationStep uint8

const (
	// MigrationWelcome asks whether to migrate.
	MigrationWelcome MigrationStep = iota
	// MigrationProfile asks for one FinDesk profile's passphrases.
	MigrationProfile
	// MigrationSummary lists what each profile did.
	MigrationSummary
)

// StepName returns the wizard page's value, as Rust's step_name does.
func StepName(step MigrationStep) string {
	switch step {
	case MigrationWelcome:
		return "welcome"
	case MigrationProfile:
		return "profile"
	case MigrationSummary:
		return "summary"
	default:
		return ""
	}
}

// MigrationOutcomeKind says how one FinDesk profile ended.
type MigrationOutcomeKind uint8

const (
	// MigrationMigrated means the profile was imported.
	MigrationMigrated MigrationOutcomeKind = iota
	// MigrationSkipped means the user skipped the profile.
	MigrationSkipped
	// MigrationFailed means the import failed for good.
	MigrationFailed
)

// MigrationOutcome is one profile's result.
type MigrationOutcome struct {
	// Kind says which outcome this is.
	Kind MigrationOutcomeKind
	// Report is set when the profile was migrated.
	Report findeskimport.FinDeskImportReport
	// Error is the localized failure text when the profile failed.
	Error string
}

// MigrationProfileResult is one line of the wizard's summary.
type MigrationProfileResult struct {
	// FinDeskSlug is FinDesk's slug; nil is FinDesk's default profile.
	FinDeskSlug *string
	// FinglassSlug is the Finglass slug the profile was migrated to.
	FinglassSlug *string
	// Name is the profile's display name.
	Name string
	// Outcome is what happened to the profile.
	Outcome MigrationOutcome
}

// MigrationSession is Rust's migration::Session without Slint: the wizard's
// step, index, passphrase choices, busy flag, error and results. The UI drives
// it with method calls.
//
// No passphrase is stored. Submit returns the two validated secrets; the caller
// runs the migration and must wipe them when it is done.
type MigrationSession struct {
	install coremigration.FinDeskInstall
	start   startup.Startup

	step               MigrationStep
	index              int
	samePassphrase     bool
	needsNewPassphrase bool
	busy               bool
	errorText          *string
	results            []MigrationProfileResult
}

// NewMigrationSession starts the wizard at the welcome step.
func NewMigrationSession(install coremigration.FinDeskInstall, start startup.Startup) *MigrationSession {
	return &MigrationSession{
		install:            install,
		start:              start,
		step:               MigrationWelcome,
		index:              0,
		samePassphrase:     true,
		needsNewPassphrase: false,
		results:            make([]MigrationProfileResult, 0),
	}
}

// Install returns the FinDesk install being migrated.
func (s *MigrationSession) Install() coremigration.FinDeskInstall {
	return s.install
}

// Startup returns the profile startup the wizard began with.
func (s *MigrationSession) Startup() startup.Startup {
	return s.start
}

// Step returns the current wizard page.
func (s *MigrationSession) Step() MigrationStep {
	return s.step
}

// Index returns the current FinDesk profile's position, 0-based.
func (s *MigrationSession) Index() int {
	return s.index
}

// ProfileCount returns how many FinDesk profiles the install has.
func (s *MigrationSession) ProfileCount() int {
	return len(s.install.Profiles)
}

// ProfileNames returns every FinDesk profile's display name.
func (s *MigrationSession) ProfileNames() []string {
	names := make([]string, 0, len(s.install.Profiles))
	for _, profile := range s.install.Profiles {
		names = append(names, profile.Name)
	}
	return names
}

// ProfileName returns the current profile's display name, or "".
func (s *MigrationSession) ProfileName() string {
	profile := s.currentProfile()
	if profile == nil {
		return ""
	}
	return profile.Name
}

// ProfileHeading returns the "Profile %d of %d: %s" heading.
func (s *MigrationSession) ProfileHeading(language i18n.Language) string {
	profile := s.currentProfile()
	if profile == nil {
		return ""
	}
	return i18n.Format(
		language,
		i18n.MigrationProfileHeading,
		i18n.Int(int64(s.index+1)),
		i18n.Int(int64(len(s.install.Profiles))),
		i18n.Str(profile.Name),
	)
}

// SamePassphrase reports whether the Finglass passphrase is the FinDesk one.
func (s *MigrationSession) SamePassphrase() bool {
	return s.samePassphrase
}

// NeedsNewPassphrase reports whether the FinDesk passphrase cannot be reused
// for the Finglass database.
func (s *MigrationSession) NeedsNewPassphrase() bool {
	return s.needsNewPassphrase
}

// Busy reports whether a migration is running.
func (s *MigrationSession) Busy() bool {
	return s.busy
}

// ErrorText returns the current error text, if any.
func (s *MigrationSession) ErrorText() *string {
	return copyString(s.errorText)
}

// Results returns the results so far.
func (s *MigrationSession) Results() []MigrationProfileResult {
	results := make([]MigrationProfileResult, len(s.results))
	copy(results, s.results)
	return results
}

// SummaryLines returns every result as the summary page's line.
func (s *MigrationSession) SummaryLines(language i18n.Language) []string {
	lines := make([]string, 0, len(s.results))
	for _, result := range s.results {
		lines = append(lines, SummaryLine(language, result))
	}
	return lines
}

// WelcomeAction writes the marker for a welcome action. The two welcome
// buttons are the only callers.
func WelcomeAction(appPaths paths.AppPaths, migrate bool, now time.Time) error {
	return coremigration.WriteMarker(appPaths, migrate, now)
}

// BeginMigration writes the "migrate" marker and moves to the first profile.
// An unacknowledged click does nothing, as Rust's callback does.
func (s *MigrationSession) BeginMigration(
	language i18n.Language,
	appPaths paths.AppPaths,
	now time.Time,
	acknowledged bool,
) error {
	if s.busy || !acknowledged {
		return nil
	}
	if err := WelcomeAction(appPaths, true, now); err != nil {
		text := i18n.Text(language, i18n.ErrorUnexpected)
		s.errorText = &text
		return err
	}
	s.step = MigrationProfile
	s.index = 0
	s.samePassphrase = true
	s.needsNewPassphrase = false
	s.errorText = nil
	return nil
}

// BeginEmpty writes the "skip" marker and leaves the wizard.
func (s *MigrationSession) BeginEmpty(language i18n.Language, appPaths paths.AppPaths, now time.Time) error {
	if s.busy {
		return nil
	}
	if err := WelcomeAction(appPaths, false, now); err != nil {
		text := i18n.Text(language, i18n.ErrorUnexpected)
		s.errorText = &text
		return err
	}
	return nil
}

// Submit validates the passphrases of the current profile, as
// on_migration_submit does.
//
// The migration itself always treats the recovery warning as acknowledged, as
// Rust's callback does. The returned passphrases are owned by the caller, which
// must wipe them after running the migration.
func (s *MigrationSession) Submit(
	language i18n.Language,
	findeskInput, newInput, repeatInput []byte,
	samePassphrase bool,
) (*secret.Passphrase, *secret.Passphrase, error) {
	if s.busy {
		return nil, nil, ErrMigrationBusy
	}
	profile := s.currentProfile()
	if profile == nil {
		return nil, nil, ErrNoMigrationProfile
	}

	s.samePassphrase = samePassphrase

	findeskPassphrase, err := secret.NewPassphrase(findeskInput, secret.Unlock)
	if err != nil {
		text := i18n.Text(language, i18n.WrongPassphrase)
		s.errorText = &text
		return nil, nil, unlock.ErrWrongPassphrase
	}

	useSame := s.samePassphrase && !s.needsNewPassphrase
	var finglassPassphrase *secret.Passphrase
	if useSame {
		finglassPassphrase, err = secret.NewPassphrase(findeskInput, secret.New)
		if err != nil {
			findeskPassphrase.Wipe()
			s.needsNewPassphrase = true
			text := i18n.Text(language, i18n.MigrationNewPassphraseNeeded)
			s.errorText = &text
			return nil, nil, err
		}
	} else {
		finglassPassphrase, err = unlock.ValidateNew(newInput, repeatInput, true)
		if err != nil {
			findeskPassphrase.Wipe()
			text := UnlockErrorText(language, err)
			s.errorText = &text
			return nil, nil, err
		}
	}

	s.busy = true
	s.errorText = nil
	return findeskPassphrase, finglassPassphrase, nil
}

// PassphraseChanged updates whether the current FinDesk passphrase needs a new
// Finglass passphrase, as on_migration_passphrase_changed does.
func (s *MigrationSession) PassphraseChanged(value []byte) {
	if s.busy || !s.samePassphrase {
		return
	}
	needsNew := false
	if len(value) > 0 {
		passphrase, err := secret.NewPassphrase(value, secret.New)
		if err != nil {
			needsNew = true
		} else {
			passphrase.Wipe()
		}
	}
	s.needsNewPassphrase = needsNew
}

// SkipCurrent skips the current FinDesk profile.
func (s *MigrationSession) SkipCurrent() {
	if s.busy {
		return
	}
	profile := s.currentProfile()
	if profile == nil {
		return
	}
	s.results = append(s.results, MigrationProfileResult{
		FinDeskSlug:  copyString(profile.Slug),
		FinglassSlug: nil,
		Name:         profile.Name,
		Outcome:      MigrationOutcome{Kind: MigrationSkipped},
	})
	s.advance()
}

// Complete records a successful migration and advances.
func (s *MigrationSession) Complete(migrated *coremigration.MigratedProfile) {
	profile := s.currentProfile()
	if profile == nil {
		s.busy = false
		return
	}
	s.results = append(s.results, MigratedResult(profile, migrated))
	s.busy = false
	s.advance()
}

// FailCurrent records a permanent failure and advances.
func (s *MigrationSession) FailCurrent(errorText string) {
	profile := s.currentProfile()
	if profile == nil {
		s.busy = false
		return
	}
	s.results = append(s.results, MigrationProfileResult{
		FinDeskSlug:  copyString(profile.Slug),
		FinglassSlug: nil,
		Name:         profile.Name,
		Outcome:      MigrationOutcome{Kind: MigrationFailed, Error: errorText},
	})
	s.busy = false
	s.advance()
}

// Retryable sets an error without advancing: the user can retry the same
// profile, for example after quitting FinDesk.
func (s *MigrationSession) Retryable(errorText string) {
	s.busy = false
	text := errorText
	s.errorText = &text
}

// Continue computes the active Finglass profile from the migrated pairs, as
// on_migration_continue does. The caller then makes it active and prepares it.
func (s *MigrationSession) Continue() ([]coremigration.SlugPair, *string, error) {
	if s.busy {
		return nil, nil, ErrMigrationBusy
	}
	pairs := make([]coremigration.SlugPair, 0, len(s.results))
	for _, result := range s.results {
		if result.Outcome.Kind != MigrationMigrated {
			continue
		}
		pairs = append(pairs, coremigration.SlugPair{
			FinDesk:  result.FinDeskSlug,
			Finglass: result.FinglassSlug,
		})
	}
	active := coremigration.ActiveAfter(&s.install, pairs)
	s.busy = true
	s.errorText = nil
	return pairs, active, nil
}

// FinishContinue clears the busy flag after the active profile was prepared.
func (s *MigrationSession) FinishContinue() {
	s.busy = false
}

// ContinueFailed publishes the unexpected-error text after Continue failed.
func (s *MigrationSession) ContinueFailed(language i18n.Language) {
	s.busy = false
	text := i18n.Text(language, i18n.ErrorUnexpected)
	s.errorText = &text
}

// advance moves to the next profile or to the summary.
func (s *MigrationSession) advance() {
	s.index++
	s.samePassphrase = true
	s.needsNewPassphrase = false
	s.errorText = nil
	if s.index >= len(s.install.Profiles) {
		s.step = MigrationSummary
	} else {
		s.step = MigrationProfile
	}
}

func (s *MigrationSession) currentProfile() *coremigration.FinDeskProfile {
	if s.index < 0 || s.index >= len(s.install.Profiles) {
		return nil
	}
	return &s.install.Profiles[s.index]
}

// MigratedResult builds a migrated profile's summary entry.
func MigratedResult(profile *coremigration.FinDeskProfile, migrated *coremigration.MigratedProfile) MigrationProfileResult {
	result := MigrationProfileResult{
		FinDeskSlug:  nil,
		FinglassSlug: nil,
		Name:         "",
		Outcome:      MigrationOutcome{Kind: MigrationMigrated},
	}
	if profile == nil {
		return result
	}
	result.FinDeskSlug = copyString(profile.Slug)
	result.Name = profile.Name
	if migrated != nil {
		result.FinglassSlug = copyString(migrated.FinglassSlug)
		result.Outcome.Report = migrated.Report
	}
	return result
}

// SummaryLine renders one result for the summary page.
func SummaryLine(language i18n.Language, result MigrationProfileResult) string {
	switch result.Outcome.Kind {
	case MigrationMigrated:
		return i18n.Format(
			language,
			i18n.MigrationProfileMigrated,
			i18n.Str(result.Name),
			i18n.Str(result.Outcome.Report.Summary(language)),
		)
	case MigrationSkipped:
		return i18n.Format(language, i18n.MigrationProfileSkipped, i18n.Str(result.Name))
	default:
		return i18n.Format(
			language,
			i18n.MigrationProfileFailed,
			i18n.Str(result.Name),
			i18n.Str(result.Outcome.Error),
		)
	}
}
