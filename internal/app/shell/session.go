package shell

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/auditview"
	"github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/dashboardview"
	"github.com/MaMissaoui/Finglass/internal/app/documentsview"
	"github.com/MaMissaoui/Finglass/internal/app/findesk"
	"github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/app/prefs"
	"github.com/MaMissaoui/Finglass/internal/app/profileswitch"
	"github.com/MaMissaoui/Finglass/internal/app/reportsview"
	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/instance"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// PromptTimeout is how long a banking prompt waits for the user, as FinDesk's
// UiBankingInteraction does.
const PromptTimeout = 10 * time.Minute

var (
	// ErrUnlockBusy means another unlock, create or change call is running.
	ErrUnlockBusy = errors.New("an unlock is already running")
	// ErrSwitchInProgress means a profile switch holds the shell.
	ErrSwitchInProgress = errors.New("a profile switch is in progress")
	// ErrNoOpenProfile means no database is open in this session.
	ErrNoOpenProfile = errors.New("no profile is open")
	// ErrNotStarted means Start refused and the session has no usable state.
	ErrNotStarted = errors.New("the session did not start")
	// ErrStaleResult means a job from a torn-down profile tried to publish.
	ErrStaleResult = errors.New("the result belongs to a closed profile")
)

// StartupRefusalKind says why the startup path did not reach the normal unlock
// gate.
type StartupRefusalKind uint8

const (
	// StartupAlreadyRunning means another Finglass process holds this data
	// root's lock.
	StartupAlreadyRunning StartupRefusalKind = iota
	// StartupFailed means the lock could not be taken, or the active profile's
	// data could not be prepared.
	StartupFailed
)

// StartupRefusal is Rust's StartupRefusal.
type StartupRefusal struct {
	// Kind says which refusal this is.
	Kind StartupRefusalKind
}

// Error implements error.
func (r *StartupRefusal) Error() string {
	return r.Message(i18n.En)
}

// Message returns the localized text the unlock window shows.
func (r *StartupRefusal) Message(language i18n.Language) string {
	if r != nil && r.Kind == StartupAlreadyRunning {
		return i18n.Text(language, i18n.ErrorAlreadyRunning)
	}
	return i18n.Text(language, i18n.ErrorStartupFailed)
}

// UnlockScreen is everything configure_unlock_window and
// configure_startup_error set on the unlock window, without Slint.
type UnlockScreen struct {
	// Mode is "create", "unlock" or "change".
	Mode string
	// AllowChange says whether the profile may ask to change its passphrase.
	AllowChange bool
	// Busy says whether an unlock is running.
	Busy bool
	// Restored says whether a restore moved the previous data aside.
	Restored bool
	// StartupError says whether the screen shows a startup refusal.
	StartupError bool
	// ErrorText is the current error text.
	ErrorText string
	// ProfileLabel is the "Profile: ..." label.
	ProfileLabel string
	// ProfileName is the profile's display name.
	ProfileName string
	// PassphraseHint is the hint under the passphrase field.
	PassphraseHint string
	// Leftovers are the folders that still hold unencrypted documents.
	Leftovers []startup.Leftover
	// LeftoverLines are the leftovers as the window shows them.
	LeftoverLines []string
}

// Session is the shell's non-UI state for one running app.
//
// One sync.Mutex guards the session fields. The concurrency pattern is Rust's
// ShellState's: begin under the mutex (check the guards, take the job
// generation), run the slow work without the mutex, and publish under the
// mutex only if Jobs.IsCurrent(generation) still holds. The four guards stay
// separate, as in Rust: Jobs, switching, the profile Busy (shared with the
// Accounts state, sync and import) and the accounts-work Busy. PromptBridge is
// never called while the session mutex is held.
//
// A passphrase is never stored on the session. Unlock, Create and Change take
// []byte and the caller owns and wipes its slice; nothing here keeps a copy or
// a secret value after the call returns, and no error or formatted session
// contains one.
type Session struct {
	mu sync.Mutex

	paths       paths.AppPaths
	credentials credentials.Store
	clock       func() time.Time
	today       func() civil.Date
	getenv      func(string) (string, bool)

	language i18n.Language
	theme    prefs.ThemePref

	db           *db.Database
	dashboard    *dashboardview.DashboardState
	reports      *reportsview.ReportsState
	settings     *settingsview.SettingsState
	findesk      *findesk.FindeskImportState
	accounts     *accountsview.AccountsState
	banking      *banking.BankingState
	bridge       *banking.PromptBridge
	profileBusy  accountsview.Busy
	accountsWork accountsview.Busy
	documents    *documentsview.DocumentsState
	transactions *transactionsview.TransactionsView

	nav      NavState
	slug     *string
	restored *string

	jobs      Jobs
	switching bool

	unlockBusy  bool
	unlockMode  unlock.UnlockMode
	allowChange bool
	unlockError *string
	startupErr  *string

	screen           string
	sidebarCollapsed bool
	auditRows        []audit.Entry

	lock            *instance.Lock
	closedDatabases atomic.Int64

	// unlockGate is nil in production. Tests set it to hold an Unlock after it
	// claimed the busy flag, so a second concurrent call can race it
	// deterministically.
	unlockGate func()
	// switchGate is nil in production. Tests set it to hold a SwitchProfile
	// after the teardown and before the switch ends.
	switchGate func()
}

// Start takes the instance lock first, then prepares the active profile, as
// Rust's prepare_startup does.
//
// credentials is never the Keychain in tests. findeskRoot is the FinDesk folder
// the caller resolved; nil offers no migration. clock and today are injected so
// the package never reads the wall clock. getenv backs the locale detection.
//
// The session is returned also when a refusal occurs, so the caller can show
// UnlockScreen; only a successful start prepares the profile, holds the lock and
// returns a migration offer.
func Start(
	appPaths paths.AppPaths,
	credentials credentials.Store,
	findeskRoot *string,
	clock func() time.Time,
	today func() civil.Date,
	getenv func(string) (string, bool),
) (*Session, *migration.FinDeskInstall, *StartupRefusal) {
	language := SystemLanguage(getenv)
	now := clock().UTC()

	lock, err := instance.Acquire(appPaths)
	if err != nil {
		refusal := &StartupRefusal{Kind: StartupFailed}
		if errors.Is(err, instance.ErrAlreadyRunning) {
			refusal.Kind = StartupAlreadyRunning
		}
		session := newSession(appPaths, credentials, clock, today, getenv, language)
		session.configureStartupErrorLocked(refusal.Message(language))
		return session, nil, refusal
	}

	prepared, err := startup.Prepare(appPaths, now)
	if err != nil {
		_ = lock.Release()
		refusal := &StartupRefusal{Kind: StartupFailed}
		session := newSession(appPaths, credentials, clock, today, getenv, language)
		session.configureStartupErrorLocked(refusal.Message(language))
		return session, nil, refusal
	}

	session := newSession(appPaths, credentials, clock, today, getenv, language)
	session.lock = lock
	session.slug = copyString(prepared.Slug)
	session.restored = copyString(prepared.RestoredFrom)
	session.configureUnlockLocked(prepared)

	var offer *migration.FinDeskInstall
	if findeskRoot != nil {
		install := migration.Discover(*findeskRoot)
		if install != nil && migration.ShouldOffer(appPaths, install) {
			offer = install
		}
	}

	return session, offer, nil
}

// newSession builds the page states in language without touching the data
// folder.
func newSession(
	appPaths paths.AppPaths,
	credentials credentials.Store,
	clock func() time.Time,
	today func() civil.Date,
	getenv func(string) (string, bool),
	language i18n.Language,
) *Session {
	if credentials == nil {
		credentials = banksync.NoCredentialStore{}
	}
	if getenv == nil {
		getenv = func(string) (string, bool) { return "", false }
	}
	if clock == nil {
		panic("shell: Start needs a clock")
	}
	if today == nil {
		panic("shell: Start needs today")
	}

	date := today()
	profileBusy := accountsview.NewBusy()
	accountsWork := accountsview.NewBusy()
	transactions := transactionsview.New(language)
	transactions.SetBusy(profileBusy)

	return &Session{
		paths:        appPaths,
		credentials:  credentials,
		clock:        clock,
		today:        today,
		getenv:       getenv,
		language:     language,
		theme:        prefs.ThemeSystem,
		db:           nil,
		dashboard:    dashboardview.New(language, date),
		reports:      reportsview.New(language, date),
		settings:     newSettingsState(language, profileBusy),
		findesk:      findesk.NewFindeskImportState(language),
		accounts:     newAccountsState(language, profileBusy),
		banking:      banking.New(language),
		bridge:       newPromptBridge(),
		profileBusy:  profileBusy,
		accountsWork: accountsWork,
		documents:    documentsview.New(language, false),
		transactions: transactions,
		nav:          NavState{},
		screen:       "dashboard",
	}
}

// newAccountsState builds an Accounts state that shares the profile's one busy
// flag.
func newAccountsState(language i18n.Language, busy accountsview.Busy) *accountsview.AccountsState {
	accounts := accountsview.New(language)
	accounts.Busy = busy
	return accounts
}

// newSettingsState builds a Settings state that shares the profile's one busy
// flag.
func newSettingsState(language i18n.Language, busy accountsview.Busy) *settingsview.SettingsState {
	settings := settingsview.New(language)
	settings.Busy = busy
	return settings
}

func newPromptBridge() *banking.PromptBridge {
	return banking.NewPromptBridge(PromptTimeout, func() {})
}

// Credentials returns the one credential store the shell was given.
func (s *Session) Credentials() credentials.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.credentials
}

// Bridge returns the open profile's banking prompt bridge.
func (s *Session) Bridge() *banking.PromptBridge {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bridge
}

// Slug returns the open profile's slug; nil is the default profile.
func (s *Session) Slug() *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyString(s.slug)
}

// RestoredFrom returns the folder a startup restore moved aside, if any.
func (s *Session) RestoredFrom() *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyString(s.restored)
}

// Language returns the current language.
func (s *Session) Language() i18n.Language {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.language
}

// Theme returns the current theme preference.
func (s *Session) Theme() prefs.ThemePref {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.theme
}

// SidebarCollapsed reports whether the sidebar shows icons only.
func (s *Session) SidebarCollapsed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sidebarCollapsed
}

// CurrentScreen returns the screen shown now.
func (s *Session) CurrentScreen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen
}

// AuditRows returns the audit rows the last unlock or preference change read.
func (s *Session) AuditRows() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]audit.Entry, len(s.auditRows))
	copy(rows, s.auditRows)
	return rows
}

// UnlockScreen returns what configure_unlock_window and
// configure_startup_error set.
func (s *Session) UnlockScreen() UnlockScreen {
	s.mu.Lock()
	defer s.mu.Unlock()

	language := s.language
	screen := UnlockScreen{
		Mode:           ModeName(s.unlockMode),
		AllowChange:    s.allowChange,
		Busy:           s.unlockBusy,
		StartupError:   s.startupErr != nil,
		PassphraseHint: PassphraseHint(language),
	}
	if s.startupErr != nil {
		screen.ErrorText = *s.startupErr
		return screen
	}

	screen.Restored = s.restored != nil
	if s.unlockError != nil {
		screen.ErrorText = *s.unlockError
	}
	screen.ProfileLabel = ProfileLabel(language, s.paths, s.slug)
	screen.ProfileName = ProfileName(s.paths, s.slug)
	screen.Leftovers = startup.Leftovers(s.paths)
	screen.LeftoverLines = make([]string, 0, len(screen.Leftovers))
	for _, leftover := range screen.Leftovers {
		screen.LeftoverLines = append(screen.LeftoverLines, LeftoverText(language, leftover))
	}
	return screen
}

// configureUnlockLocked is configure_unlock_window without Slint.
func (s *Session) configureUnlockLocked(prepared startup.Startup) {
	slugKey := startup.SlugKey(prepared.Slug)
	mode := unlock.ModeCreate
	if file, err := s.paths.DatabaseFile(slugKey); err == nil {
		mode = unlock.ModeFor(file)
	}

	s.unlockMode = mode
	s.allowChange = mode == unlock.ModeUnlock
	s.unlockBusy = false
	s.startupErr = nil
	s.unlockError = nil
	s.restored = copyString(prepared.RestoredFrom)
}

// configureStartupErrorLocked is configure_startup_error without Slint.
func (s *Session) configureStartupErrorLocked(message string) {
	text := message
	s.unlockMode = unlock.ModeUnlock
	s.allowChange = false
	s.unlockBusy = false
	s.restored = nil
	s.startupErr = &text
	s.unlockError = nil
}

// beginJobLocked takes a job generation, refusing while a switch is starting.
// The caller holds the session mutex, so a switch cannot slip in between the
// check and the count.
func (s *Session) beginJobLocked() (uint64, bool) {
	if s.switching {
		return 0, false
	}
	return s.jobs.Begin(), true
}

// beginJob is the exported form for the bind layer's future background work.
func (s *Session) beginJob() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginJobLocked()
}

// startOpen claims the unlock guard and takes a job generation.
func (s *Session) startOpen() (uint64, *string, i18n.Language, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startupErr != nil {
		return 0, nil, i18n.En, ErrNotStarted
	}
	if s.unlockBusy {
		return 0, nil, i18n.En, ErrUnlockBusy
	}
	if s.switching {
		return 0, nil, i18n.En, ErrSwitchInProgress
	}

	s.unlockBusy = true
	s.unlockError = nil
	s.resetReportsLocked()
	s.resetDocumentsLocked()
	s.resetTransactionsLocked()

	generation, ok := s.beginJobLocked()
	if !ok {
		s.unlockBusy = false
		return 0, nil, i18n.En, ErrSwitchInProgress
	}
	return generation, copyString(s.slug), s.language, nil
}

// Unlock opens the open profile's database.
//
// The input is read once and never retained; the caller owns and must wipe its
// slice after the call. The key derivation and the loads run without the
// session mutex. A second concurrent call is refused with [ErrUnlockBusy].
func (s *Session) Unlock(input []byte) error {
	generation, slug, language, err := s.startOpen()
	if err != nil {
		return err
	}
	s.waitUnlockGate()

	opened, err := unlock.Unlock(s.paths, startup.SlugKey(slug), input)
	if err != nil {
		return s.failOpen(generation, language, err)
	}
	return s.finishOpen(generation, opened, language, slug)
}

// Create creates the open profile's encrypted database.
//
// The caller owns and must wipe passphrase and repeat after the call; nothing
// here keeps a copy. A second concurrent call is refused with [ErrUnlockBusy].
func (s *Session) Create(passphrase, repeat []byte, acknowledged bool) error {
	generation, slug, language, err := s.startOpen()
	if err != nil {
		return err
	}
	s.waitUnlockGate()

	opened, err := unlock.Create(s.paths, startup.SlugKey(slug), passphrase, repeat, acknowledged)
	if err != nil {
		return s.failOpen(generation, language, err)
	}
	return s.finishOpen(generation, opened, language, slug)
}

// Change replaces the open profile's passphrase.
//
// The current, new and repeat passphrases are read once and never retained; the
// caller owns and must wipe them after the call. A second concurrent call is
// refused with [ErrUnlockBusy].
func (s *Session) Change(current, passphrase, repeat []byte, acknowledged bool) error {
	generation, slug, language, err := s.startOpen()
	if err != nil {
		return err
	}
	s.waitUnlockGate()

	opened, err := unlock.Unlock(s.paths, startup.SlugKey(slug), current)
	if err != nil {
		return s.failOpen(generation, language, err)
	}
	if err := unlock.Change(opened, passphrase, repeat, acknowledged); err != nil {
		s.closeDatabase(opened)
		return s.failOpen(generation, language, err)
	}
	return s.finishOpen(generation, opened, language, slug)
}

// waitUnlockGate is a test seam: production leaves the gate nil.
func (s *Session) waitUnlockGate() {
	if gate := s.unlockGate; gate != nil {
		gate()
	}
}

// failOpen releases the job slot and publishes the localized error, if this
// result still belongs to the open profile.
func (s *Session) failOpen(generation uint64, language i18n.Language, cause error) error {
	s.jobs.EndGeneration(generation)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs.IsCurrent(generation) {
		s.unlockBusy = false
		text := UnlockErrorText(language, cause)
		s.unlockError = &text
	}
	return cause
}

// finishOpen loads what a fresh unlock publishes, without the session mutex,
// then installs it only if the job generation is still current.
func (s *Session) finishOpen(
	generation uint64,
	opened *db.Database,
	language i18n.Language,
	slug *string,
) error {
	ctx := context.Background()

	loadedPrefs, err := prefs.Load(ctx, opened, s.systemLanguage())
	if err != nil {
		s.closeDatabase(opened)
		return s.failOpen(generation, language, unlock.ErrFailed)
	}

	rows, err := audit.Recent(ctx, opened, AuditLimit)
	if err != nil {
		s.closeDatabase(opened)
		return s.failOpen(generation, language, unlock.ErrFailed)
	}

	settings := settingsview.New(loadedPrefs.Language)
	if err := settings.Load(ctx, opened, s.paths, slug); err != nil || settings.Error != nil {
		s.closeDatabase(opened)
		return s.failOpen(generation, language, unlock.ErrFailed)
	}

	s.mu.Lock()
	if !s.jobs.IsCurrent(generation) {
		s.mu.Unlock()
		s.jobs.EndGeneration(generation)
		s.closeDatabase(opened)
		return ErrStaleResult
	}
	s.jobs.EndGeneration(generation)

	busy := s.profileBusy
	old := s.db
	s.db = opened
	if old != nil && old != opened {
		// Go has no reference counting: close the previous handle once the jobs
		// that may still hold it finish.
		s.retireDatabaseLocked(old, s.jobs.Generation())
	}
	s.language = loadedPrefs.Language
	s.theme = loadedPrefs.Theme
	settings.Busy = busy
	s.settings = settings
	s.findesk = findesk.NewFindeskImportState(loadedPrefs.Language)
	s.accounts = newAccountsState(loadedPrefs.Language, busy)
	s.banking = banking.New(loadedPrefs.Language)
	s.dashboard = dashboardview.New(loadedPrefs.Language, s.today())
	s.reports = reportsview.New(loadedPrefs.Language, s.today())
	s.documents = documentsview.New(loadedPrefs.Language, s.documents.IsOCRAvailable)
	s.documents.SetProfile(startup.SlugKey(slug))
	s.transactions = transactionsview.New(loadedPrefs.Language)
	s.transactions.SetBusy(busy)
	s.nav.Reset()
	s.screen = "dashboard"
	s.unlockBusy = false
	s.unlockError = nil
	s.startupErr = nil
	s.auditRows = rows
	s.mu.Unlock()

	return nil
}

// ShowChange switches the unlock screen between the unlock and change modes, as
// on_show_change does.
func (s *Session) ShowChange(change bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if change {
		s.unlockMode = unlock.ModeChange
	} else {
		s.unlockMode = unlock.ModeUnlock
	}
	s.unlockError = nil
}

// SwitchProfile starts a live switch to the profile at index in the loaded
// Settings list.
//
// The switching check, the Jobs.Active check and setting switching happen
// atomically under the mutex. BeginSwitch then runs without the mutex, and
// Teardown closes the old profile after it succeeds.
func (s *Session) SwitchProfile(index int) error {
	s.mu.Lock()
	language := s.language
	if s.switching || s.jobs.Active() {
		s.mu.Unlock()
		err := &profileswitch.SwitchError{Kind: profileswitch.SwitchBusy}
		s.publishSettingsError(err.Message(language))
		return err
	}

	var (
		target *string
		found  bool
	)
	if s.settings != nil && index >= 0 && index < len(s.settings.Profiles) {
		found = true
		profile := s.settings.Profiles[index]
		if profile.Slug != "" {
			slug := profile.Slug
			target = &slug
		}
	}
	if !found {
		s.mu.Unlock()
		s.publishSettingsError(i18n.Text(language, i18n.ErrorUnexpected))
		return &profileswitch.SwitchError{Kind: profileswitch.SwitchUnknownProfile}
	}
	if s.db == nil {
		s.mu.Unlock()
		s.publishSettingsError(i18n.Text(language, i18n.ErrorUnexpected))
		return ErrNoOpenProfile
	}

	database := s.db
	from := copyString(s.slug)
	s.switching = true
	generation := s.jobs.Begin()
	s.mu.Unlock()

	prepared, switchErr := profileswitch.BeginSwitch(database, s.paths, from, target, s.clock().UTC())

	s.mu.Lock()
	s.jobs.EndGeneration(generation)
	current := s.jobs.IsCurrent(generation)
	if !current || switchErr != nil {
		s.switching = false
	}
	s.mu.Unlock()
	if !current {
		return ErrStaleResult
	}
	if switchErr != nil {
		text := i18n.Text(language, i18n.ErrorUnexpected)
		var switchError *profileswitch.SwitchError
		if errors.As(switchErr, &switchError) {
			text = switchError.Message(language)
		}
		s.publishSettingsError(text)
		return switchErr
	}

	// Rust tears down on the UI thread right after the switch, so nothing runs in
	// between. Here concurrent calls can, so switching stays set until the
	// teardown and the target's unlock screen are in place: no job and no unlock
	// can start against the half-closed profile.
	s.Teardown(prepared)
	if gate := s.switchGate; gate != nil {
		gate()
	}

	s.mu.Lock()
	s.configureUnlockLocked(prepared)
	s.switching = false
	s.mu.Unlock()
	return nil
}

// Teardown closes the open profile and prepares the next one's unlock state.
//
// The generation is bumped first, so a late result can never reach the next
// profile. The database is closed after the last job of the old generation
// ends, or at once when none is running.
func (s *Session) Teardown(prepared startup.Startup) {
	s.mu.Lock()
	oldBridge := s.bridge
	s.bridge = nil
	oldGeneration := s.jobs.Generation()
	s.jobs.Bump()
	s.teardownLocked(prepared, oldGeneration)
	s.mu.Unlock()

	if oldBridge != nil {
		oldBridge.CancelPending()
	}
	next := newPromptBridge()
	s.mu.Lock()
	s.bridge = next
	s.mu.Unlock()
}

func (s *Session) teardownLocked(prepared startup.Startup, oldGeneration uint64) {
	old := s.db
	s.db = nil
	s.retireDatabaseLocked(old, oldGeneration)

	s.slug = copyString(prepared.Slug)
	s.restored = copyString(prepared.RestoredFrom)

	busy := accountsview.NewBusy()
	s.profileBusy = busy
	s.accountsWork = accountsview.NewBusy()
	s.dashboard = dashboardview.New(s.language, s.today())
	s.settings = newSettingsState(s.language, busy)
	s.findesk = findesk.NewFindeskImportState(s.language)
	s.accounts = newAccountsState(s.language, busy)
	s.banking = banking.New(s.language)
	s.nav.Reset()
	s.reports = reportsview.New(s.language, s.today())
	ocrAvailable := s.documents != nil && s.documents.IsOCRAvailable
	s.documents = documentsview.New(s.language, ocrAvailable)
	s.documents.SetProfile(startup.SlugKey(prepared.Slug))
	s.transactions = transactionsview.New(s.language)
	s.transactions.SetBusy(busy)

	s.unlockBusy = false
	s.unlockError = nil
}

// retireDatabaseLocked closes old once, now or when the last job of its
// generation ends.
func (s *Session) retireDatabaseLocked(old *db.Database, generation uint64) {
	if old == nil {
		return
	}
	s.jobs.WhenIdle(generation, func() {
		_ = old.Close()
		s.closedDatabases.Add(1)
	})
}

// closeDatabase closes a database this session opened but could not publish.
func (s *Session) closeDatabase(database *db.Database) {
	if database == nil {
		return
	}
	_ = database.Close()
	s.closedDatabases.Add(1)
}

// database returns the open database without holding the session mutex after
// the call. The worker owns the temporary reference.

// ReloadAfterFindeskImport reloads everything the imported database changed, as
// a fresh unlock would: theme, language, settings and every page's state. It
// returns the new language and false when no profile is open.
func (s *Session) ReloadAfterFindeskImport() (i18n.Language, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return s.language, false
	}
	fallback := s.language
	loadedPrefs, err := prefs.Load(context.Background(), s.db, fallback)
	if err != nil {
		return s.language, false
	}
	rows, err := auditview.Recent(context.Background(), s.db, AuditLimit)
	if err != nil {
		rows = nil
	}
	slug := copyString(s.slug)
	settings := settingsview.New(loadedPrefs.Language)
	_ = settings.Load(context.Background(), s.db, s.paths, slug)

	s.language = loadedPrefs.Language
	s.theme = loadedPrefs.Theme
	settings.Busy = s.profileBusy
	s.settings = settings
	s.settings.AuditLog = rows
	s.dashboard = dashboardview.New(loadedPrefs.Language, s.today())
	s.accounts = newAccountsState(loadedPrefs.Language, s.profileBusy)
	s.banking = banking.New(loadedPrefs.Language)
	s.nav.Reset()
	s.reports = reportsview.New(loadedPrefs.Language, s.today())
	ocrAvailable := s.documents != nil && s.documents.IsOCRAvailable
	s.documents = documentsview.New(loadedPrefs.Language, ocrAvailable)
	s.documents.SetProfile(startup.SlugKey(slug))
	s.transactions = transactionsview.New(loadedPrefs.Language)
	s.transactions.SetBusy(s.profileBusy)
	s.nav.Reset()
	s.screen = "settings"

	return loadedPrefs.Language, true
}

// Navigate switches to screen directly, starting a new back trail.
func (s *Session) Navigate(screen string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.navigateToLocked(screen, nil)
}

func (s *Session) navigateToLocked(screen string, section *int) {
	known, ok := ScreenName(screen)
	if !ok {
		return
	}
	args := PageArgs{SettingsSection: SettingsSectionFor(known, section)}
	s.nav.Enter(args)
	s.openPageLocked(known, args)
}

// FollowLink follows a link inside a screen: it leaves a back entry for where
// the user leaves from, then opens the target. A link to the page already shown
// only changes its arguments.
func (s *Session) FollowLink(target string, args PageArgs) {
	s.mu.Lock()
	defer s.mu.Unlock()

	knownTarget, ok := ScreenName(target)
	if !ok {
		return
	}
	from, ok := ScreenName(s.screen)
	if !ok {
		return
	}
	leave := s.leavingArgsLocked(from)
	s.nav.Follow(from, knownTarget, leave, args)
	s.openPageLocked(knownTarget, args)
}

// Back goes back one step, or does nothing when the stack is empty.
func (s *Session) Back() {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.nav.Back()
	if ok {
		s.openPageLocked(entry.Screen, entry.Args)
	}
}

// NavTarget returns the screen Back would reopen.
func (s *Session) NavTarget() (BackEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nav.Target()
}

// NavCurrent returns the arguments of the page shown now.
func (s *Session) NavCurrent() PageArgs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nav.Current()
}

// NavLen returns the number of steps Back can take.
func (s *Session) NavLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nav.Len()
}

// leavingArgsLocked returns the arguments Back must reopen the page the user
// leaves from with.
func (s *Session) leavingArgsLocked(from string) PageArgs {
	if from == "dashboard" {
		// FinDesk reopens the Dashboard on the month it left, unless that was
		// the current month.
		return DashboardPageArgs(s.dashboard.SelectedMonth, s.dashboard.CurrentMonth)
	}
	return s.nav.Current()
}

// openPageLocked opens screen with args, without touching the back stack. It
// applies only the state the shell owns; the page loads are G8's.
func (s *Session) openPageLocked(screen string, args PageArgs) {
	s.screen = screen
	if screen != "dashboard" {
		return
	}
	if args.DashboardMonth != nil {
		s.dashboard.BeginLoad(*args.DashboardMonth, s.today())
		return
	}
	s.dashboard.Open(s.today())
}

// resetReportsLocked replaces the Reports state with a fresh one.
func (s *Session) resetReportsLocked() {
	s.reports = reportsview.New(s.language, s.today())
}

// resetDocumentsLocked replaces the Documents state with a fresh one, keeping
// the OCR availability and the profile.
func (s *Session) resetDocumentsLocked() {
	profile := "default"
	ocrAvailable := false
	if s.documents != nil {
		profile = s.documents.Profile
		ocrAvailable = s.documents.IsOCRAvailable
	}
	s.documents = documentsview.New(s.language, ocrAvailable)
	s.documents.SetProfile(profile)
}

// resetTransactionsLocked replaces the Transactions state with a fresh one,
// keeping the shared busy flag.
func (s *Session) resetTransactionsLocked() {
	busy := s.profileBusy
	s.transactions = transactionsview.New(s.language)
	s.transactions.SetBusy(busy)
}

// SetLanguage changes and saves the UI language, as on_set_language does.
func (s *Session) SetLanguage(language i18n.Language) error {
	s.mu.Lock()
	if s.switching {
		s.mu.Unlock()
		return ErrSwitchInProgress
	}
	s.applyLanguageLocked(language)
	generation, database := s.beginSaveLocked()
	s.mu.Unlock()
	if database == nil {
		return nil
	}
	return s.saveAndRefreshAudit(generation, database, func(ctx context.Context, d *db.Database) error {
		return prefs.SaveLanguage(ctx, d, language)
	})
}

func (s *Session) applyLanguageLocked(language i18n.Language) {
	s.language = language
	s.dashboard.Language = language
	s.reports.Language = language
	s.settings.Language = language
	s.findesk.Language = language
	s.accounts.Language = language
	s.banking.Language = language
	s.documents.Language = language
	s.transactions.StateMut().Language = language
}

// SetTheme changes and saves the theme, as on_theme_changed does.
func (s *Session) SetTheme(theme prefs.ThemePref) error {
	s.mu.Lock()
	if s.switching {
		s.mu.Unlock()
		return ErrSwitchInProgress
	}
	s.theme = theme
	generation, database := s.beginSaveLocked()
	s.mu.Unlock()
	if database == nil {
		return nil
	}
	return s.saveAndRefreshAudit(generation, database, func(ctx context.Context, d *db.Database) error {
		return prefs.SaveTheme(ctx, d, theme)
	})
}

// SetSidebarCollapsed changes and saves the sidebar's collapsed state.
func (s *Session) SetSidebarCollapsed(collapsed bool) error {
	s.mu.Lock()
	if s.switching {
		s.mu.Unlock()
		return ErrSwitchInProgress
	}
	s.sidebarCollapsed = collapsed
	generation, database := s.beginSaveLocked()
	s.mu.Unlock()
	if database == nil {
		return nil
	}
	return s.saveAndRefreshAudit(generation, database, func(ctx context.Context, d *db.Database) error {
		return prefs.SaveSidebarCollapsed(ctx, d, collapsed)
	})
}

// beginSaveLocked takes a job generation for a preference save. The caller
// holds the session mutex.
func (s *Session) beginSaveLocked() (uint64, *db.Database) {
	if s.db == nil {
		return 0, nil
	}
	generation, _ := s.beginJobLocked()
	return generation, s.db
}

func (s *Session) saveAndRefreshAudit(
	generation uint64,
	database *db.Database,
	work func(context.Context, *db.Database) error,
) error {
	ctx := context.Background()
	err := work(ctx, database)
	var rows []auditview.AuditRow
	if err == nil {
		rows, err = auditview.Recent(ctx, database, AuditLimit)
	}

	s.mu.Lock()
	s.jobs.EndGeneration(generation)
	if s.jobs.IsCurrent(generation) && err == nil {
		s.settings.AuditLog = rows
	}
	s.mu.Unlock()
	return err
}

// publishSettingsError sets the Settings page's error text.
func (s *Session) publishSettingsError(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		return
	}
	pending := *s.settings
	pending.Error = &text
	s.settings = &pending
}

// KeepLiveSyncFields lets a banking action's result keep the live sync claim
// when a sync was started after the action took its snapshot.
func (s *Session) KeepLiveSyncFields(result *banking.BankingState) {
	if result == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.banking == nil {
		return
	}
	result.SyncingBank = copyBankConnectionID(s.banking.SyncingBank)
	result.SyncStep = copyString(s.banking.SyncStep)
	result.SyncAll = copySyncAll(s.banking.SyncAll)
}

// Shutdown cancels the open prompt, closes the database and releases the
// instance lock, in that order.
func (s *Session) Shutdown() error {
	s.mu.Lock()
	bridge := s.bridge
	s.bridge = nil
	database := s.db
	s.db = nil
	lock := s.lock
	s.lock = nil
	s.unlockBusy = false
	s.mu.Unlock()

	if bridge != nil {
		bridge.CancelPending()
	}
	s.closeDatabase(database)
	if lock != nil {
		return lock.Release()
	}
	return nil
}

// systemLanguage returns the system language through the injected lookup.
func (s *Session) systemLanguage() i18n.Language {
	return SystemLanguage(s.getenv)
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyBankConnectionID(value *model.BankConnectionID) *model.BankConnectionID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copySyncAll(value *banking.SyncAllProgress) *banking.SyncAllProgress {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
