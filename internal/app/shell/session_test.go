package shell

import (
	"errors"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

func novemberEighth() (civil.Date, time.Time) {
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	return civil.FromTime(now), now
}

func TestNavigatingBackToTheDashboardResetsItToTodaysMonth(t *testing.T) {
	today, now := novemberEighth()
	session := newTestSessionAt(t, testPaths(t), today, now)
	session.mu.Lock()
	session.dashboard.SelectedMonth = dashboard.NewYearMonth(2026, 10)
	session.dashboard.CurrentMonth = dashboard.NewYearMonth(2026, 10)
	session.mu.Unlock()

	session.Navigate("dashboard")

	session.mu.Lock()
	selected := session.dashboard.SelectedMonth
	current := session.dashboard.CurrentMonth
	session.mu.Unlock()

	if selected != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("selected month = %+v", selected)
	}
	if current != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("current month = %+v", current)
	}
}

func TestLeavingTheDashboardByALinkRemembersAPastMonth(t *testing.T) {
	today, now := novemberEighth()
	session := newTestSessionAt(t, testPaths(t), today, now)
	session.mu.Lock()
	session.dashboard.SelectedMonth = dashboard.NewYearMonth(2026, 10)
	session.dashboard.CurrentMonth = dashboard.NewYearMonth(2026, 11)
	session.mu.Unlock()

	session.FollowLink("reports", PageArgs{})
	entry, ok := session.NavTarget()
	if !ok || entry.Screen != "dashboard" {
		t.Fatalf("back target = %+v, %v", entry, ok)
	}
	if entry.Args.DashboardMonth == nil || *entry.Args.DashboardMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("remembered month = %v", entry.Args.DashboardMonth)
	}

	session.Navigate("dashboard")
	session.FollowLink("reports", PageArgs{})
	entry, ok = session.NavTarget()
	if !ok || entry.Screen != "dashboard" {
		t.Fatalf("back target = %+v, %v", entry, ok)
	}
	if entry.Args.DashboardMonth != nil {
		t.Fatalf("the current month opens today again, as FinDesk's backArgs does: %v", entry.Args.DashboardMonth)
	}
}

func TestBackRestoresTheTransactionsArgumentsALinkLeftWith(t *testing.T) {
	period := transactionsview.PeriodThisYear
	category := model.CategoryID(4)
	leave := PageArgs{
		Transactions: transactionsview.StartArgs{
			Period:     &period,
			CategoryID: &category,
		},
	}

	var nav NavState
	nav.Follow("transactions", "reports", leave, PageArgs{})

	entry, ok := nav.Back()
	if !ok {
		t.Fatal("a link leaves a back entry")
	}
	if entry.Args.Transactions.Period == nil || *entry.Args.Transactions.Period != period {
		t.Fatalf("restored period = %v", entry.Args.Transactions.Period)
	}
	if entry.Args.Transactions.CategoryID == nil || *entry.Args.Transactions.CategoryID != category {
		t.Fatalf("restored category = %v", entry.Args.Transactions.CategoryID)
	}
}

func TestAReloadOnTheDashboardKeepsTheSelectedMonth(t *testing.T) {
	today, now := novemberEighth()
	session := newTestSessionAt(t, testPaths(t), today, now)

	october := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	session.mu.Lock()
	session.dashboard.BeginLoad(dashboard.NewYearMonth(2026, 9), civil.FromTime(october))
	// A reload prepares the state for today again, keeping the selected month.
	session.dashboard.BeginLoad(session.dashboard.SelectedMonth, today)
	selected := session.dashboard.SelectedMonth
	current := session.dashboard.CurrentMonth
	session.mu.Unlock()

	if selected != dashboard.NewYearMonth(2026, 9) {
		t.Fatalf("selected month = %+v, want September", selected)
	}
	if current != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("current month = %+v, want November", current)
	}
}

func TestTheProfileBusyIsSharedWhenTheAccountsStateIsRebuilt(t *testing.T) {
	session := newTestSession(t, testPaths(t))
	busy := session.profileBusy

	rebuilt := newAccountsState(i18n.En, busy)
	session.mu.Lock()
	session.accounts = rebuilt
	session.mu.Unlock()

	busy.Begin()
	session.mu.Lock()
	accountsBusy := session.accounts.Busy.Active()
	profileBusy := session.profileBusy.Active()
	session.mu.Unlock()
	if !accountsBusy {
		t.Fatal("an active profile busy is not seen through the rebuilt Accounts state")
	}
	if !profileBusy {
		t.Fatal("the profile busy is not active")
	}
	busy.End()
	session.mu.Lock()
	accountsBusy = session.accounts.Busy.Active()
	session.mu.Unlock()
	if accountsBusy {
		t.Fatal("the busy flag is still active after End")
	}
}

func TestAFinishedBankingActionKeepsASyncClaimedMeanwhile(t *testing.T) {
	session := newTestSession(t, testPaths(t))
	result := banking.New(i18n.En)

	bank := model.BankConnectionID(7)
	step := "Connecting"
	session.mu.Lock()
	session.banking.SyncingBank = &bank
	session.banking.SyncStep = &step
	session.mu.Unlock()

	session.KeepLiveSyncFields(result)

	if result.SyncingBank == nil || *result.SyncingBank != bank {
		t.Fatalf("sync bank = %v", result.SyncingBank)
	}
	if result.SyncStep == nil || *result.SyncStep != step {
		t.Fatalf("sync step = %v", result.SyncStep)
	}
	if !result.IsSyncing() {
		t.Fatal("the result does not show the live sync")
	}
}

func TestALateResultFromBeforeTheTeardownIsDropped(t *testing.T) {
	session := newTestSession(t, testPaths(t))
	send := make(chan string)
	received := make(chan struct {
		result   string
		accepted bool
	}, 1)

	generation, ok := session.beginJob()
	if !ok {
		t.Fatal("the job did not begin")
	}
	go func() {
		result := <-send
		accepted := session.jobs.IsCurrent(generation)
		session.jobs.EndGeneration(generation)
		received <- struct {
			result   string
			accepted bool
		}{result: result, accepted: accepted}
	}()

	session.Teardown(startup.Startup{})
	send <- "late result"
	got := <-received

	if got.result != "late result" {
		t.Fatalf("result = %q", got.result)
	}
	if got.accepted {
		t.Fatal("the generation check did not drop the old profile's result")
	}
	if session.jobs.Active() {
		t.Fatal("the job did not release its slot")
	}
}

func TestAfterASwitchTheControllersAndUnlockUseTheTargetFolder(t *testing.T) {
	appPaths := testPaths(t)
	old := createTestDatabase(t, appPaths, "default")
	target, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create target profile: %v", err)
	}
	slug := target.Slug
	targetDB := createTestDatabase(t, appPaths, slug)
	if err := targetDB.Close(); err != nil {
		t.Fatalf("close target database: %v", err)
	}

	list, err := profiles.List(appPaths)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	settings := newSettingsState(i18n.En, accountsview.NewBusy())
	settings.Profiles = list

	session := newTestSession(t, appPaths)
	session.mu.Lock()
	session.db = old
	session.slug = nil
	session.settings = settings
	session.mu.Unlock()

	if err := session.SwitchProfile(1); err != nil {
		t.Fatalf("switch profile: %v", err)
	}

	if got := session.Slug(); got == nil || *got != slug {
		t.Fatalf("session slug = %v, want %q", got, slug)
	}
	session.mu.Lock()
	documentsProfile := session.documents.Profile
	session.mu.Unlock()
	if documentsProfile != slug {
		t.Fatalf("documents profile = %q, want %q", documentsProfile, slug)
	}

	screen := session.UnlockScreen()
	if screen.Mode != "unlock" {
		t.Fatalf("unlock mode = %q, want unlock", screen.Mode)
	}
	targetFile, err := appPaths.DatabaseFile(startup.SlugKey(session.Slug()))
	if err != nil {
		t.Fatalf("target database file: %v", err)
	}
	if targetFile != target.Dir+"/finglass.db" {
		t.Fatalf("target file = %q", targetFile)
	}
	if unlock.ModeFor(targetFile) != unlock.ModeUnlock {
		t.Fatal("the target database does not open in unlock mode")
	}
	requireClosed(t, old, "the old database")
}

// A review addition: Go's calls can interleave where Rust's UI thread cannot, so
// no job and no unlock may start between the teardown and the end of a switch.
func TestNothingStartsBetweenTheTeardownAndTheEndOfASwitch(t *testing.T) {
	appPaths := testPaths(t)
	old := createTestDatabase(t, appPaths, "default")
	target, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create target profile: %v", err)
	}
	targetDB := createTestDatabase(t, appPaths, target.Slug)
	if err := targetDB.Close(); err != nil {
		t.Fatalf("close target database: %v", err)
	}
	list, err := profiles.List(appPaths)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	settings := newSettingsState(i18n.En, accountsview.NewBusy())
	settings.Profiles = list

	session := newTestSession(t, appPaths)
	var jobBegan, unlockErr error
	session.mu.Lock()
	session.db = old
	session.slug = nil
	session.settings = settings
	session.switchGate = func() {
		if _, ok := session.beginJob(); ok {
			jobBegan = errors.New("a job began after the teardown, before the switch ended")
		}
		unlockErr = session.Unlock([]byte(testPassphrase))
	}
	session.mu.Unlock()

	if err := session.SwitchProfile(1); err != nil {
		t.Fatalf("switch profile: %v", err)
	}
	if jobBegan != nil {
		t.Fatal(jobBegan)
	}
	if !errors.Is(unlockErr, ErrSwitchInProgress) {
		t.Fatalf("unlock during the switch = %v, want ErrSwitchInProgress", unlockErr)
	}

	session.mu.Lock()
	switching := session.switching
	session.mu.Unlock()
	if switching {
		t.Fatal("switching is still set after the switch ended")
	}
	generation, ok := session.beginJob()
	if !ok {
		t.Fatal("a job did not begin after the switch ended")
	}
	session.jobs.EndGeneration(generation)
}
