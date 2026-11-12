package shell

import (
	"reflect"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

func pastMonth() dashboard.YearMonth {
	return dashboard.NewYearMonth(2026, 9)
}

func currentMonth() dashboard.YearMonth {
	return dashboard.NewYearMonth(2026, 11)
}

func TestADashboardLinkRemembersAPastMonthOnly(t *testing.T) {
	pastArgs := DashboardPageArgs(pastMonth(), currentMonth())
	if pastArgs.DashboardMonth == nil || *pastArgs.DashboardMonth != pastMonth() {
		t.Fatalf("past dashboard month = %v", pastArgs.DashboardMonth)
	}
	if DashboardPageArgs(currentMonth(), currentMonth()).DashboardMonth != nil {
		t.Fatal("the current month must not be remembered")
	}
}

func TestALinkPushesWhereTheUserLeavesFrom(t *testing.T) {
	var nav NavState
	nav.Enter(DashboardPageArgs(currentMonth(), currentMonth()))
	leave := DashboardPageArgs(pastMonth(), currentMonth())
	category := model.CategoryID(7)

	nav.Follow(
		"dashboard",
		"transactions",
		leave,
		PageArgs{Transactions: transactionsview.StartArgs{CategoryID: &category}},
	)

	target, ok := nav.Target()
	if !ok {
		t.Fatal("a link leaves a back entry")
	}
	if target.Screen != "dashboard" {
		t.Fatalf("target screen = %q", target.Screen)
	}
	if !reflect.DeepEqual(target.Args, leave) {
		t.Fatalf("target args = %+v, want %+v (Back reopens September)", target.Args, leave)
	}
	current := nav.Current()
	if current.Transactions.CategoryID == nil || *current.Transactions.CategoryID != category {
		t.Fatalf("current transactions args = %+v", current.Transactions)
	}
}

func TestBackRestoresTheScreenAndItsArguments(t *testing.T) {
	var nav NavState
	nav.Enter(PageArgs{})
	reportsArgs := PageArgs{ReportsYear: intPointer(2026)}
	nav.Follow("dashboard", "reports", PageArgs{DashboardMonth: monthPointer(pastMonth())}, reportsArgs)

	entry, ok := nav.Back()
	if !ok {
		t.Fatal("the entry pops")
	}
	if entry.Screen != "dashboard" {
		t.Fatalf("entry screen = %q", entry.Screen)
	}
	if entry.Args.DashboardMonth == nil || *entry.Args.DashboardMonth != pastMonth() {
		t.Fatalf("entry dashboard month = %v", entry.Args.DashboardMonth)
	}
	if _, ok := nav.Target(); ok {
		t.Fatal("the stack is empty again")
	}
	if current := nav.Current(); current.DashboardMonth == nil || *current.DashboardMonth != pastMonth() {
		t.Fatalf("restored page args = %+v", current)
	}
	if _, ok := nav.Back(); ok {
		t.Fatal("Back on an empty stack is a no-op")
	}
}

func TestChoosingAPageDirectlyClearsTheStack(t *testing.T) {
	var nav NavState
	nav.Enter(PageArgs{})
	nav.Follow("dashboard", "transactions", PageArgs{}, PageArgs{})
	if nav.Len() != 1 {
		t.Fatalf("stack length = %d", nav.Len())
	}

	nav.Enter(PageArgs{})

	if nav.Len() != 0 {
		t.Fatalf("stack length = %d", nav.Len())
	}
	if _, ok := nav.Target(); ok {
		t.Fatal("the target is still set")
	}
}

func TestALinkToTheSamePagePushesNothing(t *testing.T) {
	var nav NavState
	nav.Enter(PageArgs{})
	partner := "Bakery"

	nav.Follow(
		"transactions",
		"transactions",
		PageArgs{},
		PageArgs{Transactions: transactionsview.StartArgs{Partner: &partner}},
	)

	if nav.Len() != 0 {
		t.Fatalf("stack length = %d", nav.Len())
	}
	current := nav.Current()
	if current.Transactions.Partner == nil || *current.Transactions.Partner != partner {
		t.Fatalf("the same page still gets its new arguments: %+v", current.Transactions)
	}
}

func TestTheStackKeepsAtMostTenEntriesAndDropsTheOldest(t *testing.T) {
	var nav NavState
	nav.Enter(PageArgs{})
	for range MaxBack + 3 {
		nav.Follow("dashboard", "transactions", PageArgs{}, PageArgs{})
		nav.Follow("transactions", "dashboard", PageArgs{}, PageArgs{})
	}

	if nav.Len() != MaxBack {
		t.Fatalf("stack length = %d, want %d", nav.Len(), MaxBack)
	}
}

func TestEveryScreenHasANavigationTitle(t *testing.T) {
	for _, screen := range []string{"dashboard", "accounts", "transactions", "documents", "reports", "settings"} {
		if got, ok := ScreenName(screen); !ok || got != screen {
			t.Fatalf("ScreenName(%q) = %q, %v", screen, got, ok)
		}
		for _, language := range i18n.Languages {
			if Title(screen, language) == "" {
				t.Fatalf("empty title for %s in %s", screen, language.Code())
			}
		}
	}
	if _, ok := ScreenName("payments"); ok {
		t.Fatal("payments is not a page")
	}
}

func intPointer(value int) *int {
	return &value
}

func monthPointer(value dashboard.YearMonth) *dashboard.YearMonth {
	return &value
}
