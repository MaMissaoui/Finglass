package shell

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/dashboardview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

func dashboardSeedDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

func seedDashboardDatabase(t *testing.T, appPaths paths.AppPaths) model.CategoryID {
	t.Helper()
	ctx := context.Background()
	database := createTestDatabase(t, appPaths, "default")

	account, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringPointer("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	if _, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringPointer("DE44500105175407324931"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeSavings,
		Balance:     money.Money{Minor: 50_000, Currency: money.EUR},
	}); err != nil {
		t.Fatalf("accounts.Create savings: %v", err)
	}

	budget := int64(10_000)
	food, err := categories.Create(ctx, database, model.NewCategory{
		Name:        "Food",
		Color:       "#FF5722",
		BudgetLimit: &money.Money{Minor: budget, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}

	insert := func(day civil.Date, minor int64, partner string, category *model.CategoryID) {
		name := partner
		hash := fmt.Sprintf("%d-%s-%d-%s", account, day, minor, partner)
		if _, err := transactions.InsertPlanned(ctx, database, account, []model.NewTransaction{{
			BookingDate: day,
			Amount:      money.Money{Minor: minor, Currency: money.EUR},
			PartnerName: &name,
			CategoryID:  category,
			ImportHash:  hash,
			CreatedAt:   testNow.UTC(),
		}}); err != nil {
			t.Fatalf("transactions.InsertPlanned(%s): %v", partner, err)
		}
	}

	insert(dashboardSeedDate(t, 2026, time.March, 5), -10_000, "March donation", nil)
	insert(dashboardSeedDate(t, 2026, time.September, 4), -6_000, "September food", &food)
	insert(dashboardSeedDate(t, 2026, time.September, 6), 150_000, "Employer", nil)
	insert(dashboardSeedDate(t, 2026, time.October, 2), -4_000, "October food", &food)
	insert(dashboardSeedDate(t, 2026, time.October, 3), 200_000, "Employer", nil)
	insert(dashboardSeedDate(t, 2026, time.October, 5), -1_500, "October uncategorized", nil)

	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
	return food
}

func stringPointer(value string) *string {
	return &value
}

func listenForChanges(session *Session) (<-chan Change, func()) {
	events := make(chan Change, 256)
	remove := session.AddListener(func(change Change) {
		events <- change
	})
	return events, remove
}

func waitForDashboard(
	t *testing.T,
	session *Session,
	events <-chan Change,
	description string,
	predicate func(dashboardview.DashboardState) bool,
) dashboardview.DashboardState {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case change := <-events:
			if change.Page != "dashboard" {
				continue
			}
			state := session.DashboardState()
			if predicate(state) {
				return state
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func newLoadedDashboardSession(t *testing.T) *Session {
	t.Helper()
	appPaths := testPaths(t)
	seedDashboardDatabase(t, appPaths)
	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)
	return session
}

func TestUnlockPublishesLoadingThenTheDashboardFigures(t *testing.T) {
	appPaths := testPaths(t)
	seedDashboardDatabase(t, appPaths)
	session := newTestSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.pageLoadGate = func(page string) {
		if page != "dashboard" {
			return
		}
		entered <- struct{}{}
		<-release
	}

	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the dashboard load did not start")
	}

	loading := waitForDashboard(t, session, events, "the loading state", func(state dashboardview.DashboardState) bool {
		return state.IsLoading
	})
	if loading.LoadedMonth != nil || loading.View != nil {
		t.Fatalf("the loading state already has figures: %+v", loading)
	}

	close(release)
	loaded := waitForDashboard(t, session, events, "the loaded figures", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.LoadedMonth != nil
	})
	if loaded.Error != nil {
		t.Fatalf("dashboard error = %q", *loaded.Error)
	}

	database := sessionDatabase(t, session)
	ctx := context.Background()
	figures, err := dashboard.LoadDashboard(ctx, database, testToday(), loaded.SelectedMonth)
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}
	items, err := documents.FindItems(ctx, database, nil, true, nil)
	if err != nil {
		t.Fatalf("documents.FindItems: %v", err)
	}
	want := dashboardview.FromFigures(figures, len(items), i18n.En)
	if loaded.View == nil || !reflect.DeepEqual(*loaded.View, want) {
		t.Fatalf("dashboard view does not match FromFigures:\n got = %+v\nwant = %+v", loaded.View, want)
	}
	if loaded.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("selected month = %+v", loaded.SelectedMonth)
	}
}

func TestDashboardMonthNavigation(t *testing.T) {
	session := newLoadedDashboardSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	state := session.DashboardState()
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}

	session.DashboardPreviousMonth()
	state = waitForDashboard(t, session, events, "September", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.SelectedMonth == dashboard.NewYearMonth(2026, 9)
	})
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 9) {
		t.Fatalf("loaded month = %+v", state.LoadedMonth)
	}

	session.DashboardNextMonth()
	state = waitForDashboard(t, session, events, "October again", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.SelectedMonth == dashboard.NewYearMonth(2026, 10)
	})
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("loaded month = %+v", state.LoadedMonth)
	}

	session.DashboardPreviousMonth()
	state = waitForDashboard(t, session, events, "September before selecting", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.SelectedMonth == dashboard.NewYearMonth(2026, 9)
	})
	if !session.DashboardSelectMonth(2) {
		t.Fatal("selecting the third month was refused")
	}
	state = waitForDashboard(t, session, events, "August", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.SelectedMonth == dashboard.NewYearMonth(2026, 8)
	})

	before := session.DashboardState()
	if session.DashboardSelectMonth(len(before.Months)) {
		t.Fatal("an out-of-range month index was accepted")
	}
	if session.DashboardSelectMonth(-1) {
		t.Fatal("a negative month index was accepted")
	}
	after := session.DashboardState()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused selection changed the state: %+v, want %+v", after, before)
	}
	select {
	case change := <-events:
		t.Fatalf("a refused selection published %+v", change)
	default:
	}

	session.DashboardCurrentMonth()
	state = waitForDashboard(t, session, events, "the current month", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.SelectedMonth == dashboard.NewYearMonth(2026, 10)
	})
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("loaded month = %+v", state.LoadedMonth)
	}
}

func TestASupersededDashboardLoadIsDropped(t *testing.T) {
	session := newLoadedDashboardSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.DashboardPreviousMonth()
	first := <-entered
	session.DashboardPreviousMonth()
	second := <-entered

	close(second)
	state := waitForDashboard(t, session, events, "the second load", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.SelectedMonth == dashboard.NewYearMonth(2026, 8)
	})
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 8) {
		t.Fatalf("loaded month = %+v", state.LoadedMonth)
	}

	close(first)
	waitForJobs(t, session)

	state = session.DashboardState()
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 8) {
		t.Fatalf("the superseded load published %+v", state.SelectedMonth)
	}
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 8) {
		t.Fatalf("the superseded load replaced the loaded month: %+v", state.LoadedMonth)
	}
}

func TestTeardownDuringALoadDropsItAndClosesWhenTheJobEnds(t *testing.T) {
	session := newLoadedDashboardSession(t)

	entered := make(chan chan struct{}, 1)
	session.pageLoadGate = func(page string) {
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.DashboardPreviousMonth()
	gate := <-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the load")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(gate)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the dashboard job did not end")
	}

	requireClosed(t, old, "the old database after the job ends")
	state := session.DashboardState()
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("the teardown did not drop the load: selected %+v", state.SelectedMonth)
	}
	if state.LoadedMonth != nil {
		t.Fatalf("the teardown kept the old figures: %+v", state.LoadedMonth)
	}
}

func TestADashboardErrorIsPublishedAndDismissed(t *testing.T) {
	session := newLoadedDashboardSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	database := sessionDatabase(t, session)
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	session.DashboardNextMonth()
	state := waitForDashboard(t, session, events, "the error", func(state dashboardview.DashboardState) bool {
		return !state.IsLoading && state.Error != nil
	})
	want := i18n.Text(i18n.En, i18n.ErrorUnexpected)
	if *state.Error != want {
		t.Fatalf("error text = %q, want %q", *state.Error, want)
	}

	session.DashboardDismissError()
	state = waitForDashboard(t, session, events, "the dismissed error", func(state dashboardview.DashboardState) bool {
		return state.Error == nil
	})
	if state.Error != nil {
		t.Fatalf("error text = %q after DismissError", *state.Error)
	}
}

func TestDashboardStateIsSafeWhileLoadsRun(t *testing.T) {
	session := newLoadedDashboardSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	var wait sync.WaitGroup
	stop := make(chan struct{})
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = session.DashboardState()
			}
		}
	}()

	for index := 0; index < 4; index++ {
		session.DashboardPreviousMonth()
		waitForDashboard(t, session, events, "a load while reading state", func(state dashboardview.DashboardState) bool {
			return !state.IsLoading
		})
		session.DashboardNextMonth()
		waitForDashboard(t, session, events, "a load while reading state", func(state dashboardview.DashboardState) bool {
			return !state.IsLoading
		})
	}

	close(stop)
	wait.Wait()
}

func TestAListenerMayReadTheSessionWhileItRuns(t *testing.T) {
	appPaths := testPaths(t)
	seedDashboardDatabase(t, appPaths)
	session := newTestSession(t, appPaths)

	called := make(chan struct{}, 8)
	session.AddListener(func(Change) {
		_ = session.DashboardState()
		called <- struct{}{}
	})

	done := make(chan error, 1)
	go func() {
		done <- session.Unlock([]byte(testPassphrase))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unlock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Unlock deadlocked in a listener")
	}
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener was never called")
	}
	waitForJobs(t, session)
}

func setDashboardMonths(session *Session, selected, current dashboard.YearMonth) {
	session.mu.Lock()
	session.dashboard.SelectedMonth = selected
	session.dashboard.CurrentMonth = current
	session.dashboard.EarliestMonth = dashboard.NewYearMonth(2026, 3)
	session.mu.Unlock()
}

func TestDashboardLinksOpenTheRightPageAndBackReturnsToTheMonth(t *testing.T) {
	october := dashboard.NewYearMonth(2026, 10)
	november := dashboard.NewYearMonth(2026, 11)

	tests := []struct {
		name   string
		open   func(*Session)
		screen string
		check  func(*testing.T, PageArgs)
	}{
		{
			name:   "category",
			open:   func(session *Session) { session.DashboardOpenCategory(7) },
			screen: "transactions",
			check: func(t *testing.T, args PageArgs) {
				t.Helper()
				if args.Transactions.CategoryID == nil || *args.Transactions.CategoryID != model.CategoryID(7) {
					t.Fatalf("category = %+v", args.Transactions.CategoryID)
				}
				if args.Transactions.Month == nil || *args.Transactions.Month != october {
					t.Fatalf("month = %+v", args.Transactions.Month)
				}
				if args.Transactions.Period != nil || args.Transactions.UncategorizedOnly {
					t.Fatalf("args = %+v", args.Transactions)
				}
			},
		},
		{
			name:   "uncategorized",
			open:   func(session *Session) { session.DashboardOpenUncategorized() },
			screen: "transactions",
			check: func(t *testing.T, args PageArgs) {
				t.Helper()
				if !args.Transactions.UncategorizedOnly {
					t.Fatal("uncategorized only is false")
				}
				if args.Transactions.CategoryID != nil {
					t.Fatalf("category = %+v", args.Transactions.CategoryID)
				}
				if args.Transactions.Month == nil || *args.Transactions.Month != october {
					t.Fatalf("month = %+v", args.Transactions.Month)
				}
			},
		},
		{
			name:   "all",
			open:   func(session *Session) { session.DashboardOpenAll() },
			screen: "transactions",
			check: func(t *testing.T, args PageArgs) {
				t.Helper()
				if !reflect.DeepEqual(args.Transactions, transactionsview.StartArgs{}) {
					t.Fatalf("args = %+v", args.Transactions)
				}
			},
		},
		{
			name:   "partner",
			open:   func(session *Session) { session.DashboardOpenPartner("Bakery") },
			screen: "transactions",
			check: func(t *testing.T, args PageArgs) {
				t.Helper()
				if args.Transactions.Partner == nil || *args.Transactions.Partner != "Bakery" {
					t.Fatalf("partner = %+v", args.Transactions.Partner)
				}
			},
		},
		{
			name:   "accounts",
			open:   func(session *Session) { session.DashboardOpenAccounts() },
			screen: "accounts",
			check: func(t *testing.T, args PageArgs) {
				t.Helper()
				if !reflect.DeepEqual(args, PageArgs{}) {
					t.Fatalf("args = %+v", args)
				}
			},
		},
		{
			name:   "settings",
			open:   func(session *Session) { session.DashboardOpenSettings() },
			screen: "settings",
			check: func(t *testing.T, args PageArgs) {
				t.Helper()
				if args.SettingsSection == nil || *args.SettingsSection != SettingsCategories {
					t.Fatalf("settings section = %+v", args.SettingsSection)
				}
			},
		},
		{
			name:   "tax",
			open:   func(session *Session) { session.DashboardOpenTax() },
			screen: "reports",
			check: func(t *testing.T, args PageArgs) {
				t.Helper()
				if args.ReportsYear == nil || *args.ReportsYear != 2026 {
					t.Fatalf("reports year = %+v", args.ReportsYear)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := newTestSession(t, testPaths(t))
			setDashboardMonths(session, october, november)

			test.open(session)
			if screen := session.CurrentScreen(); screen != test.screen {
				t.Fatalf("screen = %q, want %q", screen, test.screen)
			}
			test.check(t, session.NavCurrent())

			target, ok := session.NavTarget()
			if !ok || target.Screen != "dashboard" {
				t.Fatalf("back target = %+v, %v", target, ok)
			}
			if target.Args.DashboardMonth == nil || *target.Args.DashboardMonth != october {
				t.Fatalf("remembered month = %+v", target.Args.DashboardMonth)
			}

			session.Back()
			if screen := session.CurrentScreen(); screen != "dashboard" {
				t.Fatalf("Back opened %q, want dashboard", screen)
			}
			state := session.DashboardState()
			if state.SelectedMonth != october {
				t.Fatalf("Back selected %+v, want October", state.SelectedMonth)
			}
		})
	}
}
