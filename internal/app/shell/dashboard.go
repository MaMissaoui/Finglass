package shell

import (
	"context"

	"github.com/MaMissaoui/Finglass/internal/app/dashboardview"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

// DashboardState returns a deep copy of the Dashboard page state.
func (s *Session) DashboardState() dashboardview.DashboardState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dashboard.Clone()
}

// beginDashboardLoadLocked prepares a load of month. The caller holds the
// session mutex and starts the returned job after releasing it.
//
// Without an open database the header still follows navigation, as the page
// did before its load existed; there is no job to start.
func (s *Session) beginDashboardLoadLocked(month dashboard.YearMonth) *pageJob {
	today := s.today()
	if s.db == nil {
		s.dashboard.BeginLoad(month, today)
		return nil
	}
	return s.beginDashboardJobLocked(today, &month)
}

// beginDashboardOpenLocked prepares the Dashboard's open on today's month.
// The caller holds the session mutex.
func (s *Session) beginDashboardOpenLocked() *pageJob {
	today := s.today()
	if s.db == nil {
		s.dashboard.Open(today)
		return nil
	}
	return s.beginDashboardJobLocked(today, nil)
}

func (s *Session) beginDashboardJobLocked(today civil.Date, month *dashboard.YearMonth) *pageJob {
	generation, ok := s.beginJobLocked()
	if !ok {
		return nil
	}

	request := s.nextPageRequestLocked("dashboard")
	pending := s.dashboard.Clone()
	pending.Language = s.language
	if month != nil {
		pending.BeginLoad(*month, today)
	} else {
		pending.Open(today)
	}

	published := pending.Clone()
	s.dashboard = &published
	database := s.db

	return &pageJob{
		page:       "dashboard",
		generation: generation,
		request:    request,
		database:   database,
		load: func(ctx context.Context, open *db.Database) {
			_ = pending.Load(ctx, open, today)
		},
		publish: func() {
			s.dashboard = &pending
		},
	}
}

// DashboardPreviousMonth opens the month before the selected one.
func (s *Session) DashboardPreviousMonth() {
	s.mu.Lock()
	month := s.dashboard.SelectedMonth.MinusMonths(1)
	job := s.beginDashboardLoadLocked(month)
	s.mu.Unlock()
	s.startPageJob(job)
}

// DashboardNextMonth opens the month after the selected one.
func (s *Session) DashboardNextMonth() {
	s.mu.Lock()
	month := s.dashboard.SelectedMonth.PlusMonths(1)
	job := s.beginDashboardLoadLocked(month)
	s.mu.Unlock()
	s.startPageJob(job)
}

// DashboardCurrentMonth returns to today's month.
func (s *Session) DashboardCurrentMonth() {
	s.mu.Lock()
	job := s.beginDashboardOpenLocked()
	s.mu.Unlock()
	s.startPageJob(job)
}

// DashboardSelectMonth opens the month at index in the month list. An index
// outside the list does nothing and returns false.
func (s *Session) DashboardSelectMonth(index int) bool {
	s.mu.Lock()
	if index < 0 || index >= len(s.dashboard.Months) {
		s.mu.Unlock()
		return false
	}
	month := s.dashboard.Months[index]
	job := s.beginDashboardLoadLocked(month)
	s.mu.Unlock()
	s.startPageJob(job)
	return true
}

// DashboardDismissError clears the page's error text.
func (s *Session) DashboardDismissError() {
	s.mu.Lock()
	s.dashboard.Error = nil
	s.mu.Unlock()
	s.notify(Change{Page: "dashboard"})
}

// DashboardOpenCategory opens Transactions filtered to one category, or to the
// uncategorized bookings when categoryID is negative.
func (s *Session) DashboardOpenCategory(categoryID int) {
	selected, current := s.dashboardMonths()
	var navigation dashboardview.NavigationArgs
	if categoryID < 0 {
		navigation = dashboardview.UncategorizedNavigation(selected, current)
	} else {
		navigation = dashboardview.CategoryNavigation(
			selected,
			current,
			model.CategoryID(categoryID),
		)
	}
	s.FollowLink("transactions", PageArgs{Transactions: navigation.StartArgs()})
}

// DashboardOpenUncategorized opens the uncategorized bookings.
func (s *Session) DashboardOpenUncategorized() {
	selected, current := s.dashboardMonths()
	navigation := dashboardview.UncategorizedNavigation(selected, current)
	s.FollowLink("transactions", PageArgs{Transactions: navigation.StartArgs()})
}

// DashboardOpenAll opens Transactions with no filter.
func (s *Session) DashboardOpenAll() {
	s.FollowLink("transactions", PageArgs{})
}

// DashboardOpenPartner opens Transactions searching for partner.
func (s *Session) DashboardOpenPartner(partner string) {
	navigation := dashboardview.PartnerNavigation(partner)
	s.FollowLink("transactions", PageArgs{Transactions: navigation.StartArgs()})
}

// DashboardOpenAccounts opens Accounts.
func (s *Session) DashboardOpenAccounts() {
	s.FollowLink("accounts", PageArgs{})
}

// DashboardOpenSettings opens Settings on the categories section.
func (s *Session) DashboardOpenSettings() {
	section := SettingsCategories
	s.FollowLink("settings", PageArgs{SettingsSection: &section})
}

// DashboardOpenTax opens Reports on the loaded (or selected) month's tax year.
func (s *Session) DashboardOpenTax() {
	s.mu.Lock()
	month := s.dashboard.SelectedMonth
	if s.dashboard.LoadedMonth != nil {
		month = *s.dashboard.LoadedMonth
	}
	s.mu.Unlock()

	year := month.Year
	s.FollowLink("reports", PageArgs{ReportsYear: &year})
}

// dashboardMonths returns the selected and current months under the mutex.
func (s *Session) dashboardMonths() (dashboard.YearMonth, dashboard.YearMonth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dashboard.SelectedMonth, s.dashboard.CurrentMonth
}
