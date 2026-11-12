package bind

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	coremigration "github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/app/prefs"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

// ShellEvent is the name of the event emitted after every shell state change.
const ShellEvent = "shell:changed"

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[ShellDTO](ShellEvent)
}

// ErrNoMigration means the first-run migration offer was not made.
var ErrNoMigration = errors.New("no migration is offered")

// ShellDTO is everything the unlock view and the main shell show.
//
// It never carries a passphrase or any other secret.
type ShellDTO struct {
	Mode           string   `json:"mode"`
	AllowChange    bool     `json:"allowChange"`
	Busy           bool     `json:"busy"`
	Restored       bool     `json:"restored"`
	StartupError   bool     `json:"startupError"`
	ErrorText      string   `json:"errorText"`
	ProfileLabel   string   `json:"profileLabel"`
	ProfileName    string   `json:"profileName"`
	PassphraseHint string   `json:"passphraseHint"`
	LeftoverLines  []string `json:"leftoverLines"`

	ShellOpen          bool   `json:"shellOpen"`
	Language           string `json:"language"`
	Theme              string `json:"theme"`
	SidebarCollapsed   bool   `json:"sidebarCollapsed"`
	Screen             string `json:"screen"`
	ScreenTitle        string `json:"screenTitle"`
	BackTitle          string `json:"backTitle"`
	CurrentProfileName string `json:"currentProfileName"`
	Version            string `json:"version"`
	FindEnabled        bool   `json:"findEnabled"`

	MigrationStep                  string   `json:"migrationStep"`
	MigrationProfileNames          []string `json:"migrationProfileNames"`
	MigrationProfileHeading        string   `json:"migrationProfileHeading"`
	MigrationProfileName           string   `json:"migrationProfileName"`
	MigrationProfileCount          int      `json:"migrationProfileCount"`
	MigrationSamePassphrase        bool     `json:"migrationSamePassphrase"`
	MigrationNewPassphraseRequired bool     `json:"migrationNewPassphraseRequired"`
	MigrationSummaryLines          []string `json:"migrationSummaryLines"`
}

// ShellOptions is the process wiring the caller supplies. The service itself
// never resolves a path, reads the environment, reads the clock or opens a
// browser.
type ShellOptions struct {
	// Paths is the data root the session was started with.
	Paths paths.AppPaths
	// Clock supplies the time for migration markers and profile preparation.
	Clock func() time.Time
	// OpenURL opens Help links; tests pass a recorder.
	OpenURL func(string)
	// Emit publishes shell:changed; nil uses the running Wails application.
	Emit func(ShellDTO)
	// AfterChange runs after Emit, e.g. so the native menu follows the state.
	AfterChange func(ShellDTO)
}

// Shell is the thin Wails service around a shell session.
//
// Every state-changing method returns the new DTO and publishes it as
// shell:changed. The session owns all locking and all shell state.
type Shell struct {
	session *shell.Session
	paths   paths.AppPaths
	clock   func() time.Time

	// migrationMu guards the wizard's step, passphrase flags and results. It
	// is never held around the session's unlock key derivation.
	migrationMu sync.Mutex
	wizard      *shell.MigrationSession
	active      atomic.Bool

	openURL     func(string)
	emit        func(ShellDTO)
	afterChange func(ShellDTO)

	// observePassphrase is a test seam. Production leaves it nil; tests use it
	// to capture the exact slice handed to the session and check it is wiped.
	observePassphrase func([]byte)
}

// NewShell builds the shell service. session may be nil only for the
// self-check, which inspects the registry without calling the service.
func NewShell(
	session *shell.Session,
	offer *coremigration.FinDeskInstall,
	options ShellOptions,
) *Shell {
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	service := &Shell{
		session:     session,
		paths:       options.Paths,
		clock:       clock,
		openURL:     options.OpenURL,
		emit:        options.Emit,
		afterChange: options.AfterChange,
	}
	if offer != nil && session != nil {
		service.wizard = shell.NewMigrationSession(*offer, startup.Startup{
			Slug:         session.Slug(),
			RestoredFrom: session.RestoredFrom(),
		})
		service.active.Store(true)
	}
	return service
}

// ServiceName implements application.ServiceName.
func (s *Shell) ServiceName() string {
	return "Shell"
}

// State returns the current shell state without changing anything.
func (s *Shell) State() (ShellDTO, error) {
	return s.snapshot(), nil
}

// Unlock opens the current profile with passphrase.
func (s *Shell) Unlock(passphrase string) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	input := []byte(passphrase)
	defer wipe(input)
	if s.observePassphrase != nil {
		s.observePassphrase(input)
	}

	// A refusal is shown through the DTO's error text, not returned.
	_ = s.session.Unlock(input)
	return s.publish(), nil
}

// Create creates the current profile's database.
func (s *Shell) Create(passphrase, repeat string, acknowledged bool) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	first := []byte(passphrase)
	second := []byte(repeat)
	defer wipe(first)
	defer wipe(second)
	if s.observePassphrase != nil {
		s.observePassphrase(first)
		s.observePassphrase(second)
	}

	_ = s.session.Create(first, second, acknowledged)
	return s.publish(), nil
}

// Change replaces the current profile's passphrase.
func (s *Shell) Change(current, passphrase, repeat string, acknowledged bool) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	old := []byte(current)
	first := []byte(passphrase)
	second := []byte(repeat)
	defer wipe(old)
	defer wipe(first)
	defer wipe(second)
	if s.observePassphrase != nil {
		s.observePassphrase(old)
		s.observePassphrase(first)
		s.observePassphrase(second)
	}

	_ = s.session.Change(old, first, second, acknowledged)
	return s.publish(), nil
}

// ShowChange switches the unlock view between unlock and change mode.
func (s *Shell) ShowChange(change bool) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	s.session.ShowChange(change)
	return s.publish(), nil
}

// Navigate opens a screen directly.
func (s *Shell) Navigate(screen string) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	if _, ok := shell.ScreenName(screen); !ok {
		return s.snapshot(), fmt.Errorf("unknown screen %q", screen)
	}
	s.session.Navigate(screen)
	return s.publish(), nil
}

// NavigateMenuItem opens the View menu's page at index.
func (s *Shell) NavigateMenuItem(index int) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	screen, ok := shell.PageTarget(index)
	if !ok {
		return s.snapshot(), fmt.Errorf("unknown menu index %d", index)
	}
	s.session.Navigate(screen)
	return s.publish(), nil
}

// Back goes back one step.
func (s *Shell) Back() (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	s.session.Back()
	return s.publish(), nil
}

// SetLanguage changes and saves the UI language.
func (s *Shell) SetLanguage(code string) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	language, ok := i18n.LanguageFromCode(code)
	if !ok {
		return s.snapshot(), fmt.Errorf("unknown language %q", code)
	}
	err := s.session.SetLanguage(language)
	return s.publish(), err
}

// SetTheme changes and saves the theme.
func (s *Shell) SetTheme(code string) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	theme, ok := prefs.ThemeFromCode(code)
	if !ok {
		return s.snapshot(), fmt.Errorf("unknown theme %q", code)
	}
	err := s.session.SetTheme(theme)
	return s.publish(), err
}

// SetSidebarCollapsed changes and saves the sidebar's collapsed state.
func (s *Shell) SetSidebarCollapsed(collapsed bool) (ShellDTO, error) {
	if s.session == nil {
		return ShellDTO{}, errors.New("shell is not started")
	}
	err := s.session.SetSidebarCollapsed(collapsed)
	return s.publish(), err
}

// MigrationMigrate writes the migrate marker and moves to the first profile.
// Without the acknowledgement nothing happens, as Rust's callback does.
func (s *Shell) MigrationMigrate(acknowledged bool) (ShellDTO, error) {
	s.migrationMu.Lock()
	if s.wizard == nil {
		s.migrationMu.Unlock()
		return s.snapshot(), ErrNoMigration
	}
	err := s.wizard.BeginMigration(s.language(), s.paths, s.clock().UTC(), acknowledged)
	s.migrationMu.Unlock()
	return s.publish(), err
}

// MigrationStartEmpty writes the skip marker and leaves the wizard.
func (s *Shell) MigrationStartEmpty() (ShellDTO, error) {
	s.migrationMu.Lock()
	if s.wizard == nil {
		s.migrationMu.Unlock()
		return s.snapshot(), ErrNoMigration
	}
	err := s.wizard.BeginEmpty(s.language(), s.paths, s.clock().UTC())
	if err == nil {
		s.active.Store(false)
	}
	s.migrationMu.Unlock()
	return s.publish(), err
}

// MigrationSubmit validates the current profile's passphrases and runs its
// migration. samePassphrase is the wizard's "use the same passphrase" box.
func (s *Shell) MigrationSubmit(passphrase, newPassphrase, repeat string, samePassphrase bool) (ShellDTO, error) {
	s.migrationMu.Lock()
	if s.wizard == nil {
		s.migrationMu.Unlock()
		return s.snapshot(), ErrNoMigration
	}

	language := s.language()
	wizard := s.wizard
	findeskInput := []byte(passphrase)
	newInput := []byte(newPassphrase)
	repeatInput := []byte(repeat)
	defer wipe(findeskInput)
	defer wipe(newInput)
	defer wipe(repeatInput)
	if s.observePassphrase != nil {
		s.observePassphrase(findeskInput)
		s.observePassphrase(newInput)
		s.observePassphrase(repeatInput)
	}

	findeskPassphrase, finglassPassphrase, err := wizard.Submit(
		language,
		findeskInput,
		newInput,
		repeatInput,
		samePassphrase,
	)

	if err != nil {
		s.migrationMu.Unlock()
		return s.publish(), nil
	}
	defer findeskPassphrase.Wipe()
	defer finglassPassphrase.Wipe()

	install := wizard.Install()
	index := wizard.Index()
	if index < 0 || index >= len(install.Profiles) {
		wizard.FailCurrent(i18n.Text(language, i18n.ErrorUnexpected))
		s.migrationMu.Unlock()
		return s.publish(), nil
	}

	profile := &install.Profiles[index]
	// The migration may derive keys and copy files; the wizard is busy, so no
	// other call mutates it meanwhile. Release the mutex so State stays
	// responsive.
	s.migrationMu.Unlock()
	migrated, migrateErr := coremigration.MigrateProfile(
		s.paths,
		profile,
		findeskPassphrase,
		finglassPassphrase,
		s.clock().Location(),
	)

	s.migrationMu.Lock()
	if migrateErr != nil {
		s.publishMigrationFailure(language, migrateErr)
	} else {
		wizard.Complete(migrated)
	}
	s.migrationMu.Unlock()
	return s.publish(), nil
}

// MigrationSkip skips the current FinDesk profile.
func (s *Shell) MigrationSkip() (ShellDTO, error) {
	s.migrationMu.Lock()
	if s.wizard == nil {
		s.migrationMu.Unlock()
		return s.snapshot(), ErrNoMigration
	}
	s.wizard.SkipCurrent()
	s.migrationMu.Unlock()
	return s.publish(), nil
}

// MigrationContinue makes the migrated profile active and prepares it.
func (s *Shell) MigrationContinue() (ShellDTO, error) {
	s.migrationMu.Lock()
	if s.wizard == nil {
		s.migrationMu.Unlock()
		return s.snapshot(), ErrNoMigration
	}
	language := s.language()

	_, active, err := s.wizard.Continue()
	if err != nil {
		s.migrationMu.Unlock()
		return s.publish(), nil
	}

	slug := ""
	if active != nil {
		slug = *active
	}
	if err := profiles.SetActive(s.paths, slug); err != nil {
		s.wizard.ContinueFailed(language)
		s.migrationMu.Unlock()
		return s.publish(), nil
	}

	prepared, err := startup.PrepareProfile(s.paths, active, s.clock().UTC())
	if err != nil {
		s.wizard.ContinueFailed(language)
		s.migrationMu.Unlock()
		return s.publish(), nil
	}

	s.session.TeardownToUnlock(prepared)
	s.wizard.FinishContinue()
	s.active.Store(false)
	s.migrationMu.Unlock()
	return s.publish(), nil
}

// MigrationPassphraseChanged updates whether the FinDesk passphrase needs a
// different Finglass passphrase.
func (s *Shell) MigrationPassphraseChanged(text string) (ShellDTO, error) {
	s.migrationMu.Lock()
	if s.wizard == nil {
		s.migrationMu.Unlock()
		return s.snapshot(), ErrNoMigration
	}
	value := []byte(text)
	defer wipe(value)
	if s.observePassphrase != nil {
		s.observePassphrase(value)
	}
	s.wizard.PassphraseChanged(value)
	s.migrationMu.Unlock()
	return s.publish(), nil
}

// OpenGitHub opens the project repository.
func (s *Shell) OpenGitHub() (ShellDTO, error) {
	if s.openURL != nil {
		s.openURL(shell.GithubURL)
	}
	return s.snapshot(), nil
}

// ReportProblem opens the project's issue form.
func (s *Shell) ReportProblem() (ShellDTO, error) {
	if s.openURL != nil {
		s.openURL(shell.ReportProblemURL)
	}
	return s.snapshot(), nil
}

// ServiceShutdown ends the session for good.
func (s *Shell) ServiceShutdown() error {
	if s.session == nil {
		return nil
	}
	return s.session.Shutdown()
}

// publish builds the new DTO and sends it to every listener.
func (s *Shell) publish() ShellDTO {
	dto := s.snapshot()
	s.emitDTO(dto)
	return dto
}

func (s *Shell) emitDTO(dto ShellDTO) {
	if s.emit != nil {
		s.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(ShellEvent, dto)
	}
	if s.afterChange != nil {
		s.afterChange(dto)
	}
}

func (s *Shell) snapshot() ShellDTO {
	if s.session == nil {
		return ShellDTO{}
	}

	language := s.session.Language()
	unlock := s.session.UnlockScreen()
	screen := s.session.CurrentScreen()

	dto := ShellDTO{
		Mode:               unlock.Mode,
		AllowChange:        unlock.AllowChange,
		Busy:               unlock.Busy,
		Restored:           unlock.Restored,
		StartupError:       unlock.StartupError,
		ErrorText:          unlock.ErrorText,
		ProfileLabel:       unlock.ProfileLabel,
		ProfileName:        unlock.ProfileName,
		PassphraseHint:     unlock.PassphraseHint,
		LeftoverLines:      append([]string(nil), unlock.LeftoverLines...),
		ShellOpen:          s.session.IsOpen(),
		Language:           language.Code(),
		Theme:              s.session.Theme().Code(),
		SidebarCollapsed:   s.session.SidebarCollapsed(),
		Screen:             screen,
		ScreenTitle:        shell.Title(screen, language),
		CurrentProfileName: unlock.ProfileName,
		Version:            shell.AboutVersionText(language, Version),
		FindEnabled:        shell.FindEnabled(screen, false),
	}
	if target, ok := s.session.NavTarget(); ok {
		dto.BackTitle = shell.Title(target.Screen, language)
	}

	s.migrationMu.Lock()
	if wizard := s.wizard; wizard != nil && s.active.Load() {
		dto.Mode = "migrate"
		dto.Busy = wizard.Busy()
		dto.ErrorText = ""
		if text := wizard.ErrorText(); text != nil {
			dto.ErrorText = *text
		}
		dto.MigrationStep = shell.StepName(wizard.Step())
		dto.MigrationProfileNames = wizard.ProfileNames()
		dto.MigrationProfileHeading = wizard.ProfileHeading(language)
		dto.MigrationProfileName = wizard.ProfileName()
		dto.MigrationProfileCount = wizard.ProfileCount()
		dto.MigrationSamePassphrase = wizard.SamePassphrase()
		dto.MigrationNewPassphraseRequired = !wizard.SamePassphrase() || wizard.NeedsNewPassphrase()
		dto.MigrationSummaryLines = wizard.SummaryLines(language)
	}
	s.migrationMu.Unlock()
	return dto
}

func (s *Shell) language() i18n.Language {
	if s.session == nil {
		return i18n.En
	}
	return s.session.Language()
}

func (s *Shell) publishMigrationFailure(language i18n.Language, err error) {
	if errors.Is(err, findeskimport.ErrWrongPassphrase) {
		s.wizard.Retryable(i18n.Text(language, i18n.WrongPassphrase))
		return
	}

	var importError *findeskimport.FinDeskImportError
	if errors.As(err, &importError) {
		switch importError.Kind {
		case findeskimport.KindSourceChanged, findeskimport.KindNotFound, findeskimport.KindIO:
			s.wizard.Retryable(importError.Message(language))
			return
		}
	}

	var migrationError *coremigration.MigrationError
	if errors.As(err, &migrationError) && migrationError.Kind == coremigration.MigrationIO {
		s.wizard.Retryable(migrationError.Message(language))
		return
	}

	text := i18n.Text(language, i18n.ErrorUnexpected)
	if errors.As(err, &migrationError) {
		text = migrationError.Message(language)
	}
	s.wizard.FailCurrent(text)
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
