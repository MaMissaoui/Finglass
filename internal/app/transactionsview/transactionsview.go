// Package transactionsview ports crates/finglass-core/src/app/transactions/: the
// Transactions view's state and actions, without any UI.
//
// This is the UI-free port of FinDesk's TransactionsViewModel. The page renders
// TransactionsState and calls the synchronous methods on TransactionsView with
// the database. Every method takes today (and sometimes now) explicitly; nothing
// here reads the clock or an environment variable.
//
// The account panel is shared with the Accounts view through
// internal/app/accountpanel; this package only wires it into the transaction
// list.
//
// The state types are single-caller, as Rust's `&mut self` implies. The Wails
// layer serializes calls with one sync.Mutex per profile.
//
// This page lists, edits, categorizes, imports and exports stored bookings.
// Nothing is sent to a bank, and there is no transfer or order of any kind.
package transactionsview

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/iban"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/pendingbookings"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/report"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// SearchDebounceMS is FinDesk's search debounce. A synchronous caller may use it
// before calling UpdateSearchQuery; the method itself applies the query at once.
const SearchDebounceMS uint64 = 250

// maxImportTotalBytes is MaxImportTotalBytes as a signed value, so a file size
// can be compared without an unsigned conversion.
const maxImportTotalBytes = int64(MaxImportTotalBytes)

// maxFileBytes is imports.MaxFileBytes as a signed value, so a file size can be
// compared without an unsigned conversion.
const maxFileBytes = int64(imports.MaxFileBytes)

// MaxImportFiles is how many statement files may be selected at once.
const MaxImportFiles = 240

// MaxImportTotalBytes is the total size of the files selected at once.
const MaxImportTotalBytes uint64 = 200 * 1024 * 1024

// MaxImportTotalMB is the MB the total-size limit names in the user message.
const MaxImportTotalMB uint64 = MaxImportTotalBytes / (1024 * 1024)

// PendingMaxAgeDays is the age after which pending rows are hidden.
const PendingMaxAgeDays int64 = 7

// ChartCollapsedKey is whether the balance chart is folded away, per profile.
const ChartCollapsedKey = "transactions.chartCollapsed"

// SelectionKey is the settings key holding the panel's selection.
const SelectionKey = accountpanel.SelectionKey

// CollapsedKey is the settings key holding the panel's collapsed groups.
const CollapsedKey = accountpanel.CollapsedKey

// Period is the transaction list's period menu.
type Period int

const (
	// PeriodThisMonth runs from the first day of this month through today's
	// month end.
	PeriodThisMonth Period = iota
	// PeriodLastMonth is the whole previous calendar month.
	PeriodLastMonth
	// PeriodLast3Months is the previous two months plus this month.
	PeriodLast3Months
	// PeriodThisYear is January 1 through December 31 of this year.
	PeriodThisYear
	// PeriodLastYear is the whole previous calendar year.
	PeriodLastYear
	// PeriodAll has no date restriction.
	PeriodAll
)

// Range returns the inclusive date range, or (nil, nil) for all time.
func (p Period) Range(today civil.Date) (*civil.Date, *civil.Date) {
	month := dashboard.YearMonthFromDate(today)
	switch p {
	case PeriodThisMonth:
		from, to := month.Start(), month.End()
		return &from, &to
	case PeriodLastMonth:
		last := month.MinusMonths(1)
		from, to := last.Start(), last.End()
		return &from, &to
	case PeriodLast3Months:
		from, to := month.MinusMonths(2).Start(), month.End()
		return &from, &to
	case PeriodThisYear:
		from := mustDate(today.Year(), time.January, 1)
		to := mustDate(today.Year(), time.December, 31)
		return &from, &to
	case PeriodLastYear:
		year := today.Year() - 1
		from := mustDate(year, time.January, 1)
		to := mustDate(year, time.December, 31)
		return &from, &to
	default:
		return nil, nil
	}
}

// CategoryFilterKind says what a CategoryFilter selects.
type CategoryFilterKind uint8

const (
	// CategoryFilterKindAny selects every category, including uncategorized
	// bookings.
	CategoryFilterKindAny CategoryFilterKind = iota
	// CategoryFilterKindNone selects only bookings without a category.
	CategoryFilterKindNone
	// CategoryFilterKindOnly selects exactly one category.
	CategoryFilterKindOnly
)

// CategoryFilter is the category filter: everything, only uncategorized, or one
// category.
type CategoryFilter struct {
	// Kind says which case this is.
	Kind CategoryFilterKind
	// CategoryID is the selected category when Kind is CategoryFilterKindOnly.
	CategoryID model.CategoryID
}

// AnyCategoryFilter selects every category, including uncategorized bookings.
func AnyCategoryFilter() CategoryFilter {
	return CategoryFilter{Kind: CategoryFilterKindAny}
}

// NoneCategoryFilter selects only bookings without a category.
func NoneCategoryFilter() CategoryFilter {
	return CategoryFilter{Kind: CategoryFilterKindNone}
}

// OnlyCategoryFilter selects exactly one category.
func OnlyCategoryFilter(id model.CategoryID) CategoryFilter {
	return CategoryFilter{Kind: CategoryFilterKindOnly, CategoryID: id}
}

// StartArgs is the navigation context a screen passes to Start.
type StartArgs struct {
	// AccountID opens one account.
	AccountID *model.AccountID
	// UncategorizedOnly shows only uncategorized bookings.
	UncategorizedOnly bool
	// Partner searches for one partner.
	Partner *string
	// CategoryID shows one category.
	CategoryID *model.CategoryID
	// Period overrides the period.
	Period *Period
	// Month shows exactly one month (wins over the period).
	Month *dashboard.YearMonth
}

// ImportEntry is one selected statement file that was read successfully.
type ImportEntry struct {
	// File is the source file.
	File string
	// Statement is the parsed statement.
	Statement imports.ParsedStatement
}

// ImportFailure is a selected file that could not be read, with the reason shown
// to the user.
type ImportFailure struct {
	// FileName is the file's display name.
	FileName string
	// Message is the user-visible reason.
	Message string
}

// ImportPreview is the selected statement files, read and waiting for the user
// to pick the account and confirm.
type ImportPreview struct {
	// Files holds every selected file, in selection order: a column remap
	// re-reads all of them.
	Files []string
	// Entries holds the readable files in import order. Never empty.
	Entries []ImportEntry
	// Failures holds the files that could not be read.
	Failures []ImportFailure
	// AccountID is the account the user picked or the file matched.
	AccountID *model.AccountID
	// BalanceUpdateAllowed says the newest entry's balance may replace the
	// account balance.
	BalanceUpdateAllowed bool
}

// Newest returns the entry imported last: the file with the greatest
// latest-booking date.
func (p *ImportPreview) Newest() *ImportEntry {
	return &p.Entries[len(p.Entries)-1]
}

// BookingCount returns the number of bookings across all readable files.
func (p *ImportPreview) BookingCount() int {
	count := 0
	for _, entry := range p.Entries {
		count += len(entry.Statement.Bookings)
	}
	return count
}

// ReapplyChange is one booking the rules would recategorize.
type ReapplyChange struct {
	// TransactionID is the booking.
	TransactionID model.TransactionID
	// FromCategoryID is its category at preview time, nil when it was
	// uncategorized.
	FromCategoryID *model.CategoryID
	// ToCategoryID is the category the highest-priority matching rule assigns.
	ToCategoryID model.CategoryID
}

// ReapplyGroup is one checkbox row of the re-apply preview dialog.
type ReapplyGroup struct {
	// FromCategoryID is the category the bookings leave.
	FromCategoryID *model.CategoryID
	// FromName is its display name, or the localized "Uncategorized".
	FromName string
	// ToCategoryID is the category they move to.
	ToCategoryID model.CategoryID
	// ToName is its display name.
	ToName string
	// Count is how many bookings this row covers.
	Count int
	// Sum is the sum of their amounts, or nil when they do not all use the
	// primary currency.
	Sum *money.Money
	// Currency is the primary currency.
	Currency money.Currency
}

// ReapplyPreview is the exact change list a re-apply preview computed.
type ReapplyPreview struct {
	// Changes are the changes, in list order.
	Changes []ReapplyChange
	// Groups are the dialog's checkbox rows.
	Groups []ReapplyGroup
	// ShownCount is the bookings the preview checked (everything the list
	// showed).
	ShownCount int
}

// ManualEntry is a manual booking waiting for the user to confirm it as a
// genuine repeat.
type ManualEntry struct {
	// AccountID is the account.
	AccountID model.AccountID
	// Date is the booking date.
	Date civil.Date
	// Amount is the amount in the account's currency.
	Amount money.Money
	// Currency is the account currency (kept for the dialog).
	Currency money.Currency
	// PartnerName is the trimmed partner.
	PartnerName *string
	// Purpose is the trimmed purpose.
	Purpose *string
	// CategoryID is the chosen category.
	CategoryID *model.CategoryID
}

// ManualTransaction is the argument of AddManualTransaction.
type ManualTransaction struct {
	// AccountID is the account.
	AccountID model.AccountID
	// Date is the booking date.
	Date civil.Date
	// Amount is the amount in the account's currency.
	Amount money.Money
	// PartnerName is the partner name as typed.
	PartnerName *string
	// Purpose is the purpose as typed.
	Purpose *string
	// CategoryID is the chosen category, or nil to match a rule.
	CategoryID *model.CategoryID
	// ConfirmedDuplicate says whether the user already confirmed an identical
	// repeat.
	ConfirmedDuplicate bool
}

// DateRange is one inclusive custom range.
type DateRange struct {
	// From is the first day.
	From civil.Date
	// To is the last day.
	To civil.Date
}

// TransactionsState is the Transactions view state.
type TransactionsState struct {
	// Transactions holds the filtered bookings, newest first.
	Transactions []model.TransactionItem
	// Categories holds every category.
	Categories []model.Category
	// Accounts holds every account, with the panel's unread counters.
	Accounts []accountsview.AccountItem
	// SearchQuery is the search text.
	SearchQuery string
	// Period is the selected period.
	Period Period
	// Month is an exact month: wins over the period.
	Month *dashboard.YearMonth
	// CustomRange is an arbitrary inclusive range: after an exact month,
	// before the period.
	CustomRange *DateRange
	// Direction is the money flow.
	Direction model.Direction
	// CategoryFilter is the category filter.
	CategoryFilter CategoryFilter
	// AccountID is the selected single account, derived from Selection.
	AccountID *model.AccountID
	// Selection is what the account panel filters by.
	Selection accountpanel.AccountSelection
	// Groups holds the panel's groups in display order.
	Groups []accountgrouping.AccountGroupView
	// GrandTotal is the grand total, per currency, primary first.
	GrandTotal []money.CurrencyTotal
	// Collapsed holds the ids of the collapsed groups.
	Collapsed map[string]struct{}
	// BoldIDs holds the ids rendered bold: bookings a sync brought in.
	BoldIDs map[model.TransactionID]struct{}
	// AccountFilterText is the free-text filter over the panel's account rows.
	AccountFilterText string
	// CustomGroups holds the custom groups as loaded.
	CustomGroups []model.AccountGroup
	// Balances is the running balance after each shown booking, when one
	// non-depot account is selected. nil means no running balance is shown.
	Balances map[model.TransactionID]balance.Entry
	// BalanceChart is the selected account's balance per day over the shown
	// period. nil means no chart is shown.
	BalanceChart []balance.BalancePoint
	// ChartCollapsed says whether the chart card is folded away.
	ChartCollapsed bool
	// CurrentBalance is the selected account's current balance.
	CurrentBalance *money.Money
	// BalanceCurrency is the selected account's currency.
	BalanceCurrency *money.Currency
	// CurrentBalanceIsLoan says whether the balance belongs to a loan.
	CurrentBalanceIsLoan bool
	// TotalIncome is income per currency.
	TotalIncome []money.CurrencyTotal
	// TotalExpenses is expenses per currency, as positive amounts.
	TotalExpenses []money.CurrencyTotal
	// PrimaryCurrency is the profile's primary currency.
	PrimaryCurrency money.Currency
	// ImportPreview is the open import preview.
	ImportPreview *ImportPreview
	// MinAmount is the smallest absolute amount.
	MinAmount *money.Money
	// MaxAmount is the largest absolute amount.
	MaxAmount *money.Money
	// Selected is the open detail panel.
	Selected *model.TransactionItem
	// SelectedDocuments are the detail panel's documents.
	SelectedDocuments []model.DocumentItem
	// PendingDuplicate is a manual entry waiting for confirmation.
	PendingDuplicate *ManualEntry
	// IsImporting says whether an import is running.
	IsImporting bool
	// PendingBookings are the selected account's pending bank lines.
	PendingBookings []model.PendingBookingItem
	// ReapplyPreview is the rules' preview of the shown bookings.
	ReapplyPreview *ReapplyPreview
	// CanUndoReapply says whether the last re-apply can still be undone.
	CanUndoReapply bool
	// SelectedIDs are the selected bookings.
	SelectedIDs map[model.TransactionID]struct{}
	// SelectionAnchorID is the anchor of the next Shift range.
	SelectionAnchorID *model.TransactionID
	// IsLoading says whether a load is running.
	IsLoading bool
	// Error is the current error text.
	Error *string
	// Message is the current success text.
	Message *string
	// Language is the language of the state's user-visible texts.
	Language i18n.Language
}

// NewState builds empty state for language.
func NewState(language i18n.Language) *TransactionsState {
	return &TransactionsState{
		Transactions:         make([]model.TransactionItem, 0),
		Categories:           make([]model.Category, 0),
		Accounts:             make([]accountsview.AccountItem, 0),
		SearchQuery:          "",
		Period:               PeriodThisMonth,
		Month:                nil,
		CustomRange:          nil,
		Direction:            model.DirectionAll,
		CategoryFilter:       AnyCategoryFilter(),
		AccountID:            nil,
		Selection:            accountpanel.AllSelection(),
		Groups:               make([]accountgrouping.AccountGroupView, 0),
		GrandTotal:           make([]money.CurrencyTotal, 0),
		Collapsed:            make(map[string]struct{}),
		BoldIDs:              make(map[model.TransactionID]struct{}),
		AccountFilterText:    "",
		CustomGroups:         make([]model.AccountGroup, 0),
		Balances:             nil,
		BalanceChart:         nil,
		ChartCollapsed:       false,
		CurrentBalance:       nil,
		BalanceCurrency:      nil,
		CurrentBalanceIsLoan: false,
		TotalIncome:          make([]money.CurrencyTotal, 0),
		TotalExpenses:        make([]money.CurrencyTotal, 0),
		PrimaryCurrency:      money.DefaultPrimaryCurrency,
		ImportPreview:        nil,
		MinAmount:            nil,
		MaxAmount:            nil,
		Selected:             nil,
		SelectedDocuments:    make([]model.DocumentItem, 0),
		PendingDuplicate:     nil,
		IsImporting:          false,
		PendingBookings:      make([]model.PendingBookingItem, 0),
		ReapplyPreview:       nil,
		CanUndoReapply:       false,
		SelectedIDs:          make(map[model.TransactionID]struct{}),
		SelectionAnchorID:    nil,
		IsLoading:            false,
		Error:                nil,
		Message:              nil,
		Language:             language,
	}
}

// DefaultState builds empty state in English, like Rust's
// TransactionsState::default.
func DefaultState() *TransactionsState {
	return NewState(i18n.En)
}

// SelectedShown returns the selected bookings among the currently shown list,
// in list order.
func (s *TransactionsState) SelectedShown() []*model.TransactionItem {
	shown := make([]*model.TransactionItem, 0)
	for index := range s.Transactions {
		if _, selected := s.SelectedIDs[s.Transactions[index].ID]; selected {
			shown = append(shown, &s.Transactions[index])
		}
	}
	return shown
}

// SelectedTotals returns totals of the selected bookings per currency;
// currencies are never added together.
func (s *TransactionsState) SelectedTotals() []money.CurrencyTotal {
	items := s.SelectedShown()
	return money.SumByCurrency(
		items,
		money.DefaultPrimaryCurrency,
		func(transaction *model.TransactionItem) money.Currency { return transaction.Currency },
		func(transaction *model.TransactionItem) int64 { return transaction.Amount.Minor },
	)
}

// ReapplyKey is the exact key of one re-apply checkbox row: the category a
// booking leaves (nil for uncategorized) and the category it moves to.
type ReapplyKey struct {
	// FromCategoryID is the category the bookings leave, nil when
	// uncategorized.
	FromCategoryID *model.CategoryID
	// ToCategoryID is the category they move to.
	ToCategoryID model.CategoryID
}

// reapplyKeyValue is ReapplyKey by value, so pointer identity never decides
// membership.
type reapplyKeyValue struct {
	from    model.CategoryID
	hasFrom bool
	to      model.CategoryID
}

func (k ReapplyKey) value() reapplyKeyValue {
	value := reapplyKeyValue{to: k.ToCategoryID}
	if k.FromCategoryID != nil {
		value.from = *k.FromCategoryID
		value.hasFrom = true
	}
	return value
}

func changeKey(change ReapplyChange) reapplyKeyValue {
	return ReapplyKey{
		FromCategoryID: change.FromCategoryID,
		ToCategoryID:   change.ToCategoryID,
	}.value()
}

// TransactionsView is the Transactions view: state plus the synchronous actions
// that change it.
//
// The view is single-caller: the Wails layer serializes calls with one
// sync.Mutex per profile.
type TransactionsView struct {
	state                  TransactionsState
	restoreSelection       bool
	collapsedLoaded        bool
	chartCollapsedLoaded   bool
	lastRefreshedSelection *accountpanel.AccountSelection
	reapplyUndo            []ReapplyChange
	busy                   accountsview.Busy
}

// New builds a view with empty state for language.
func New(language i18n.Language) *TransactionsView {
	return &TransactionsView{
		state:                  *NewState(language),
		restoreSelection:       false,
		collapsedLoaded:        false,
		chartCollapsedLoaded:   false,
		lastRefreshedSelection: nil,
		reapplyUndo:            make([]ReapplyChange, 0),
		busy:                   accountsview.NewBusy(),
	}
}

// Default builds a view with empty state in English, like Rust's
// TransactionsView::default.
func Default() *TransactionsView {
	return New(i18n.En)
}

// State returns the current state.
func (v *TransactionsView) State() *TransactionsState {
	return &v.state
}

// StateMut returns the current state, mutably. The page only reads it; actions
// live on this view.
func (v *TransactionsView) StateMut() *TransactionsState {
	return &v.state
}

// Busy returns the shared busy flag.
func (v *TransactionsView) Busy() *accountsview.Busy {
	return &v.busy
}

// SetBusy replaces the busy flag with an externally shared one.
func (v *TransactionsView) SetBusy(busy accountsview.Busy) {
	v.busy = busy
}

// CurrentFilter returns the filter the current state describes.
func (v *TransactionsView) CurrentFilter(today civil.Date) model.TransactionFilter {
	start, end := v.DateRange(today)
	var (
		accountID  *model.AccountID
		accountIDs *model.AccountIDSet
	)
	switch v.state.Selection.Kind {
	case accountpanel.SelectionAccount:
		id := v.state.Selection.Account
		accountID = &id
	case accountpanel.SelectionGroup:
		visible := make(model.AccountIDSet)
		for _, group := range v.state.Groups {
			if group.Key.ID() != v.state.Selection.Group.ID() {
				continue
			}
			for _, account := range group.Accounts {
				visible[account.ID] = struct{}{}
			}
			break
		}
		accountIDs = &visible
	}

	categoryID := (*model.CategoryID)(nil)
	if v.state.CategoryFilter.Kind == CategoryFilterKindOnly {
		id := v.state.CategoryFilter.CategoryID
		categoryID = &id
	}

	text := (*string)(nil)
	if strings.TrimSpace(v.state.SearchQuery) != "" {
		query := v.state.SearchQuery
		text = &query
	}

	return model.TransactionFilter{
		StartDate:         start,
		EndDate:           end,
		AccountID:         accountID,
		AccountIDs:        accountIDs,
		CategoryID:        categoryID,
		UncategorizedOnly: v.state.CategoryFilter.Kind == CategoryFilterKindNone,
		Direction:         v.state.Direction,
		Text:              text,
		MinAmount:         v.state.MinAmount,
		MaxAmount:         v.state.MaxAmount,
	}
}

// DateRange returns the inclusive date range behind the month, the custom range
// or the period.
func (v *TransactionsView) DateRange(today civil.Date) (*civil.Date, *civil.Date) {
	if v.state.Month != nil {
		start, end := v.state.Month.Start(), v.state.Month.End()
		return &start, &end
	}
	if v.state.CustomRange != nil {
		start, end := v.state.CustomRange.From, v.state.CustomRange.To
		return &start, &end
	}
	return v.state.Period.Range(today)
}

// Start applies navigation context (e.g. "transactions of this account") and
// loads.
func (v *TransactionsView) Start(
	ctx context.Context,
	d *db.Database,
	args StartArgs,
	today civil.Date,
) error {
	if args.Partner != nil {
		return v.ShowPartner(ctx, d, *args.Partner, today)
	}
	if args.CategoryID != nil {
		return v.showCategory(ctx, d, *args.CategoryID, args.Period, args.Month, today)
	}
	if args.UncategorizedOnly {
		return v.showUncategorizedLink(ctx, d, args.AccountID, args.Period, args.Month, today)
	}

	v.restoreSelection = args.AccountID == nil
	navigated := args.Period != nil || args.Month != nil || args.AccountID != nil
	effectivePeriod := v.state.Period
	switch {
	case args.Period != nil:
		effectivePeriod = *args.Period
	case args.AccountID != nil:
		effectivePeriod = PeriodAll
	}
	v.state.AccountID = args.AccountID
	if args.AccountID != nil {
		v.state.Selection = accountpanel.AccountSelectionFor(*args.AccountID)
	}
	v.state.CategoryFilter = AnyCategoryFilter()
	v.state.Period = effectivePeriod
	v.state.Month = args.Month
	if navigated {
		v.state.CustomRange = nil
	}
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// showCategory lists every booking of one category over period (default all
// time) or exactly month.
func (v *TransactionsView) showCategory(
	ctx context.Context,
	d *db.Database,
	categoryID model.CategoryID,
	period *Period,
	month *dashboard.YearMonth,
	today civil.Date,
) error {
	v.restoreSelection = false
	v.resetForCategoryLink(OnlyCategoryFilter(categoryID))
	v.state.Period = PeriodAll
	if period != nil {
		v.state.Period = *period
	}
	v.state.Month = month
	return v.Load(ctx, d, today)
}

// showUncategorizedLink is the dashboard's uncategorized link.
func (v *TransactionsView) showUncategorizedLink(
	ctx context.Context,
	d *db.Database,
	accountID *model.AccountID,
	period *Period,
	month *dashboard.YearMonth,
	today civil.Date,
) error {
	v.restoreSelection = false
	v.resetForCategoryLink(NoneCategoryFilter())
	v.state.Period = PeriodAll
	if period != nil {
		v.state.Period = *period
	}
	v.state.Month = month
	if accountID != nil {
		v.state.Selection = accountpanel.AccountSelectionFor(*accountID)
		v.state.AccountID = accountID
	}
	return v.Load(ctx, d, today)
}

func (v *TransactionsView) resetForCategoryLink(categoryFilter CategoryFilter) {
	v.state.SearchQuery = ""
	v.state.Direction = model.DirectionAll
	v.state.CategoryFilter = categoryFilter
	v.state.Selection = accountpanel.AllSelection()
	v.state.AccountID = nil
	v.state.MinAmount = nil
	v.state.MaxAmount = nil
	v.state.CustomRange = nil
	v.ClearSelection()
}

// ShowPartner lists every booking from partner, any time, account, category,
// direction or amount.
func (v *TransactionsView) ShowPartner(
	ctx context.Context,
	d *db.Database,
	partner string,
	today civil.Date,
) error {
	v.restoreSelection = false
	v.state.SearchQuery = partner
	v.state.Period = PeriodAll
	v.state.Month = nil
	v.state.Direction = model.DirectionAll
	v.state.CategoryFilter = AnyCategoryFilter()
	v.state.Selection = accountpanel.AllSelection()
	v.state.AccountID = nil
	v.state.MinAmount = nil
	v.state.MaxAmount = nil
	v.state.CustomRange = nil
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// Load reloads the list, its figures and the panel data.
func (v *TransactionsView) Load(ctx context.Context, d *db.Database, today civil.Date) error {
	v.state.IsLoading = true
	v.state.Error = nil
	if err := v.refresh(ctx, d, today); err != nil {
		return v.RecordError(err)
	}
	return nil
}

func (v *TransactionsView) refresh(ctx context.Context, d *db.Database, today civil.Date) error {
	var previousSelection *accountpanel.AccountSelection
	if v.lastRefreshedSelection != nil {
		value := *v.lastRefreshedSelection
		previousSelection = &value
	}

	items, err := loadAccountItems(ctx, d)
	if err != nil {
		return err
	}
	customGroups, err := accountgroups.List(ctx, d)
	if err != nil {
		return err
	}
	primary, err := money.Primary(ctx, d)
	if err != nil {
		return accountpanel.DBError(err)
	}
	groups := groupAccounts(items, customGroups, primary)

	var candidate accountpanel.AccountSelection
	if v.restoreSelection {
		candidate, err = accountpanel.LoadSelection(ctx, d, groups, groupingAccounts(items))
		if err != nil {
			return err
		}
	} else {
		candidate = v.state.Selection
	}
	selection := accountpanel.Resolve(candidate, groups, groupingAccounts(items))
	v.restoreSelection = false
	v.lastRefreshedSelection = &selection

	v.state.Accounts = make([]accountsview.AccountItem, len(items))
	copy(v.state.Accounts, items)
	v.state.CustomGroups = make([]model.AccountGroup, len(customGroups))
	copy(v.state.CustomGroups, customGroups)
	v.state.Groups = make([]accountgrouping.AccountGroupView, len(groups))
	copy(v.state.Groups, groups)
	v.state.GrandTotal = accountgrouping.GrandTotal(groupingAccounts(items), primary)
	v.state.PrimaryCurrency = primary
	if !v.collapsedLoaded {
		collapsed, err := accountpanel.LoadCollapsed(ctx, d)
		if err != nil {
			return err
		}
		v.state.Collapsed = collapsed
		v.collapsedLoaded = true
	}
	if !v.chartCollapsedLoaded {
		collapsed, err := loadChartCollapsed(ctx, d)
		if err != nil {
			return err
		}
		v.state.ChartCollapsed = collapsed
		v.chartCollapsedLoaded = true
	}
	v.state.Selection = selection
	v.state.AccountID = nil
	if selection.Kind == accountpanel.SelectionAccount {
		id := selection.Account
		v.state.AccountID = &id
	}

	filter := v.CurrentFilter(today)
	loadedTransactions, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	shownNew := make(map[model.TransactionID]struct{})
	for _, transaction := range loadedTransactions {
		if transaction.IsNew {
			shownNew[transaction.ID] = struct{}{}
		}
	}
	// Bold rows survive a refresh so clearing a counter cannot unbold them,
	// but a selection change replaces them.
	boldIDs := make(map[model.TransactionID]struct{})
	if previousSelection != nil && *previousSelection == selection {
		for id := range v.state.BoldIDs {
			boldIDs[id] = struct{}{}
		}
		for id := range shownNew {
			boldIDs[id] = struct{}{}
		}
	} else {
		for id := range shownNew {
			boldIDs[id] = struct{}{}
		}
	}

	// Opening a single account marks its bookings read, however it was
	// selected. View state only.
	if selection.Kind == accountpanel.SelectionAccount {
		id := selection.Account
		needsMark := false
		for _, account := range v.state.Accounts {
			if account.ID == id && account.NewCount > 0 {
				needsMark = true
				break
			}
		}
		if needsMark {
			accountFilter := model.TransactionFilter{
				AccountID: &id,
				Direction: model.DirectionAll,
			}
			allForAccount, err := transactions.FindItems(ctx, d, &accountFilter, nil)
			if err != nil {
				return accountpanel.RepoError(err)
			}
			for _, transaction := range allForAccount {
				if transaction.IsNew {
					boldIDs[transaction.ID] = struct{}{}
				}
			}
			marked := model.AccountIDSet{id: struct{}{}}
			if err := transactions.MarkRead(ctx, d, &marked); err != nil {
				return accountpanel.RepoError(err)
			}
			items, err = loadAccountItems(ctx, d)
			if err != nil {
				return err
			}
			groups = groupAccounts(items, customGroups, primary)
		}
	}

	// Keep the open detail panel in sync with edits.
	if v.state.Selected != nil {
		selected := *v.state.Selected
		stillShown := false
		for index := range loadedTransactions {
			if loadedTransactions[index].ID == selected.ID {
				shown := loadedTransactions[index]
				v.state.Selected = &shown
				stillShown = true
				break
			}
		}
		if !stillShown {
			exists, err := transactionExists(ctx, d, selected.ID)
			if err != nil {
				return err
			}
			if !exists {
				v.state.Selected = nil
				v.state.SelectedDocuments = make([]model.DocumentItem, 0)
			}
		}
	}

	loadedCategories, err := categories.List(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}

	pending := make([]model.PendingBookingItem, 0)
	if selection.Kind == accountpanel.SelectionAccount && v.showsPendingRows(today) {
		rows, err := pendingbookings.FindItems(ctx, d, selection.Account)
		if err != nil {
			return accountpanel.RepoError(err)
		}
		if pendingRowsAreFresh(rows, today) {
			pending = rows
		}
	}

	var balanceAccount *accountsview.AccountItem
	if v.state.AccountID != nil {
		for index := range v.state.Accounts {
			account := &v.state.Accounts[index]
			if account.ID == *v.state.AccountID && account.AccountType != string(model.AccountTypeDepot) {
				balanceAccount = account
				break
			}
		}
	}
	var accountBookings []model.TransactionItem
	if balanceAccount != nil {
		accountFilter := model.TransactionFilter{
			AccountID: &balanceAccount.ID,
			Direction: model.DirectionAll,
		}
		accountBookings, err = transactions.FindItems(ctx, d, &accountFilter, nil)
		if err != nil {
			return accountpanel.RepoError(err)
		}
	}
	var running map[model.TransactionID]balance.Entry
	var chart []balance.BalancePoint
	if balanceAccount != nil {
		running = balance.ComputeRunning(balanceAccount.Balance, accountBookings)
		chart = v.balanceChartPoints(accountBookings, running, today)
		if chart == nil {
			chart = make([]balance.BalancePoint, 0)
		}
	}

	income := make([]*model.TransactionItem, 0)
	expenses := make([]*model.TransactionItem, 0)
	for index := range loadedTransactions {
		transaction := &loadedTransactions[index]
		if transaction.Amount.Minor > 0 {
			income = append(income, transaction)
		}
		if transaction.IsExpense() {
			expenses = append(expenses, transaction)
		}
	}
	totalIncome := money.SumByCurrency(
		income,
		money.DefaultPrimaryCurrency,
		func(transaction *model.TransactionItem) money.Currency { return transaction.Currency },
		func(transaction *model.TransactionItem) int64 { return transaction.Amount.Minor },
	)
	totalExpenses := money.SumByCurrency(
		expenses,
		money.DefaultPrimaryCurrency,
		func(transaction *model.TransactionItem) money.Currency { return transaction.Currency },
		func(transaction *model.TransactionItem) int64 { return absMinor(transaction.Amount.Minor) },
	)
	shownIDs := make(map[model.TransactionID]struct{}, len(loadedTransactions))
	for _, transaction := range loadedTransactions {
		shownIDs[transaction.ID] = struct{}{}
	}

	v.state.Transactions = loadedTransactions
	v.state.Categories = loadedCategories
	v.state.PendingBookings = pending
	v.state.Accounts = items
	v.state.Groups = groups
	v.state.GrandTotal = accountgrouping.GrandTotal(groupingAccounts(v.state.Accounts), primary)
	v.state.PrimaryCurrency = primary
	v.state.BoldIDs = boldIDs
	v.state.Balances = running
	v.state.BalanceChart = chart
	v.state.CurrentBalance = nil
	v.state.BalanceCurrency = nil
	v.state.CurrentBalanceIsLoan = false
	if balanceAccount != nil {
		current := balanceAccount.Balance
		currency := balanceAccount.Currency
		v.state.CurrentBalance = &current
		v.state.BalanceCurrency = &currency
		v.state.CurrentBalanceIsLoan = balanceAccount.AccountType == accountgrouping.LoanType
	}
	v.state.TotalIncome = totalIncome
	v.state.TotalExpenses = totalExpenses
	for id := range v.state.SelectedIDs {
		if _, shown := shownIDs[id]; !shown {
			delete(v.state.SelectedIDs, id)
		}
	}
	v.state.IsLoading = false
	return nil
}

// showsPendingRows reports whether pending rows may show: one account, a range
// including today, and no narrowing filter.
func (v *TransactionsView) showsPendingRows(today civil.Date) bool {
	if v.state.Selection.Kind != accountpanel.SelectionAccount {
		return false
	}
	start, end := v.DateRange(today)
	includesToday := (start == nil || !start.After(today)) && (end == nil || !end.Before(today))
	return includesToday &&
		strings.TrimSpace(v.state.SearchQuery) == "" &&
		v.state.Direction == model.DirectionAll &&
		v.state.MinAmount == nil &&
		v.state.MaxAmount == nil &&
		v.state.CategoryFilter.Kind == CategoryFilterKindAny
}

func pendingRowsAreFresh(rows []model.PendingBookingItem, today civil.Date) bool {
	cutoff := today.AddDays(int(-PendingMaxAgeDays))
	for _, row := range rows {
		if civil.FromTime(row.FetchedAt).Before(cutoff) {
			return false
		}
	}
	return true
}

func (v *TransactionsView) balanceChartPoints(
	bookings []model.TransactionItem,
	running map[model.TransactionID]balance.Entry,
	today civil.Date,
) []balance.BalancePoint {
	start, end := v.DateRange(today)
	var from civil.Date
	if start != nil {
		from = *start
	} else {
		oldest := civil.Date{}
		for index, booking := range bookings {
			if index == 0 || booking.Date.Before(oldest) {
				oldest = booking.Date
			}
		}
		if len(bookings) == 0 {
			return make([]balance.BalancePoint, 0)
		}
		from = oldest
	}
	to := today
	if end != nil && !end.After(today) {
		to = *end
	}
	return balance.HistoryFromRunning(bookings, running, from, to)
}

// UpdateSearchQuery applies the search text. Debouncing is meaningless in a
// synchronous API: the query is applied at once.
func (v *TransactionsView) UpdateSearchQuery(
	ctx context.Context,
	d *db.Database,
	query string,
	today civil.Date,
) error {
	v.state.SearchQuery = query
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// UpdatePeriod applies one period; it ends the exact-month navigation and the
// custom range.
func (v *TransactionsView) UpdatePeriod(
	ctx context.Context,
	d *db.Database,
	period Period,
	today civil.Date,
) error {
	v.state.Period = period
	v.state.Month = nil
	v.state.CustomRange = nil
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// SetCustomRange applies an arbitrary inclusive From/To range. A reversed range
// changes nothing.
func (v *TransactionsView) SetCustomRange(
	ctx context.Context,
	d *db.Database,
	from civil.Date,
	to civil.Date,
	today civil.Date,
) error {
	if from.After(to) {
		return nil
	}
	v.state.CustomRange = &DateRange{From: from, To: to}
	v.state.Month = nil
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// UpdateDirection applies the money-flow filter.
func (v *TransactionsView) UpdateDirection(
	ctx context.Context,
	d *db.Database,
	direction model.Direction,
	today civil.Date,
) error {
	v.state.Direction = direction
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// UpdateCategoryFilter applies the category filter.
func (v *TransactionsView) UpdateCategoryFilter(
	ctx context.Context,
	d *db.Database,
	filter CategoryFilter,
	today civil.Date,
) error {
	v.state.CategoryFilter = filter
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// SelectAccounts selects an account, group or all accounts, persists the choice,
// and reloads.
func (v *TransactionsView) SelectAccounts(
	ctx context.Context,
	d *db.Database,
	selection accountpanel.AccountSelection,
	today civil.Date,
) error {
	v.state.Selection = selection
	v.state.AccountID = nil
	if selection.Kind == accountpanel.SelectionAccount {
		id := selection.Account
		v.state.AccountID = &id
	}
	v.ClearSelection()
	v.state.IsLoading = true
	v.state.Error = nil
	if err := accountpanel.SaveSelection(ctx, d, selection); err != nil {
		return v.RecordError(err)
	}
	if err := v.refresh(ctx, d, today); err != nil {
		return v.RecordError(err)
	}
	return nil
}

// UpdateAccountFilter selects one account, or all accounts for nil.
func (v *TransactionsView) UpdateAccountFilter(
	ctx context.Context,
	d *db.Database,
	accountID *model.AccountID,
	today civil.Date,
) error {
	selection := accountpanel.AllSelection()
	if accountID != nil {
		selection = accountpanel.AccountSelectionFor(*accountID)
	}
	return v.SelectAccounts(ctx, d, selection, today)
}

// MarkRead marks the bookings behind selection as read on the user's request.
func (v *TransactionsView) MarkRead(
	ctx context.Context,
	d *db.Database,
	selection accountpanel.AccountSelection,
	today civil.Date,
) error {
	v.state.IsLoading = true
	v.state.Error = nil
	actions := accountpanel.NewAccountGroupActions()
	if err := actions.MarkRead(ctx, d, selection, v.state.Groups); err != nil {
		return v.RecordError(err)
	}
	if err := v.refresh(ctx, d, today); err != nil {
		return v.RecordError(err)
	}
	return nil
}

// ToggleCollapsed collapses or expands a group; view state only, persisted but
// not audited.
func (v *TransactionsView) ToggleCollapsed(
	ctx context.Context,
	d *db.Database,
	key accountgrouping.GroupKey,
) error {
	id := key.ID()
	if _, collapsed := v.state.Collapsed[id]; collapsed {
		delete(v.state.Collapsed, id)
	} else {
		v.state.Collapsed[id] = struct{}{}
	}
	if err := accountpanel.SaveCollapsed(ctx, d, v.state.Collapsed); err != nil {
		return v.RecordError(err)
	}
	return nil
}

// ToggleChart collapses or expands the balance chart; view state only,
// persisted but not audited.
func (v *TransactionsView) ToggleChart(ctx context.Context, d *db.Database) error {
	v.state.ChartCollapsed = !v.state.ChartCollapsed
	if err := settings.Set(ctx, d, ChartCollapsedKey, strconv.FormatBool(v.state.ChartCollapsed)); err != nil {
		return v.RecordError(accountpanel.DBError(err))
	}
	return nil
}

// UpdateAccountFilterText filters the panel's account rows by name, IBAN or
// bank; the booking list is unaffected.
func (v *TransactionsView) UpdateAccountFilterText(text string) {
	v.state.AccountFilterText = text
}

// CreateGroup creates a group and reloads.
func (v *TransactionsView) CreateGroup(
	ctx context.Context,
	d *db.Database,
	name string,
	today civil.Date,
) error {
	actions := accountpanel.NewAccountGroupActions()
	if _, err := actions.Create(ctx, d, name); err != nil {
		return v.actionError(err)
	}
	return v.refresh(ctx, d, today)
}

// RenameGroup renames a group and reloads.
func (v *TransactionsView) RenameGroup(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	name string,
	today civil.Date,
) error {
	actions := accountpanel.NewAccountGroupActions()
	if err := actions.Rename(ctx, d, id, name); err != nil {
		return v.actionError(err)
	}
	return v.refresh(ctx, d, today)
}

// DeleteGroup deletes a group, resetting the selection to All when it was
// selected, then reloads.
func (v *TransactionsView) DeleteGroup(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	today civil.Date,
) error {
	actions := accountpanel.NewAccountGroupActions()
	wasSelected, err := actions.Delete(ctx, d, id, v.state.Selection)
	if err != nil {
		return v.actionError(err)
	}
	if wasSelected {
		v.state.Selection = accountpanel.AllSelection()
		v.state.AccountID = nil
	}
	return v.refresh(ctx, d, today)
}

// MoveGroup moves a group up or down and reloads.
func (v *TransactionsView) MoveGroup(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	up bool,
	today civil.Date,
) error {
	actions := accountpanel.NewAccountGroupActions()
	if _, err := actions.MoveUpOrDown(ctx, d, id, up); err != nil {
		return v.actionError(err)
	}
	return v.refresh(ctx, d, today)
}

// MoveAccountToGroup moves an account into a custom group, or back to its type
// group, then reloads.
func (v *TransactionsView) MoveAccountToGroup(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	groupID *model.AccountGroupID,
	today civil.Date,
) error {
	actions := accountpanel.NewAccountGroupActions()
	if err := actions.MoveAccount(ctx, d, accountID, groupID); err != nil {
		return v.actionError(err)
	}
	return v.refresh(ctx, d, today)
}

// UpdateAmountRange applies the absolute amount bounds.
func (v *TransactionsView) UpdateAmountRange(
	ctx context.Context,
	d *db.Database,
	min *money.Money,
	max *money.Money,
	today civil.Date,
) error {
	v.state.MinAmount = min
	v.state.MaxAmount = max
	v.ClearSelection()
	return v.Load(ctx, d, today)
}

// SelectTransaction opens the detail panel with the transaction's documents.
func (v *TransactionsView) SelectTransaction(
	ctx context.Context,
	_ *db.Database,
	documents *documentsservice.Service,
	transaction *model.TransactionItem,
) error {
	v.state.Selected = nil
	if transaction != nil {
		selected := *transaction
		v.state.Selected = &selected
	}
	v.state.SelectedDocuments = make([]model.DocumentItem, 0)
	if transaction == nil {
		return nil
	}
	loaded, documentError := documents.GetDocumentsForTransaction(ctx, transaction.ID)
	if documentError != nil {
		message := documentError.Message(v.state.Language)
		v.state.Error = &message
		return nil
	}
	if v.state.Selected != nil && v.state.Selected.ID == transaction.ID {
		v.state.SelectedDocuments = loaded
	}
	return nil
}

// AttachDocuments uploads the files to the transaction and refreshes its
// document list.
func (v *TransactionsView) AttachDocuments(
	ctx context.Context,
	d *db.Database,
	documents *documentsservice.Service,
	transactionID model.TransactionID,
	files []string,
	today civil.Date,
) error {
	language := v.state.Language
	var failures []string
	v.busy.Run(func() {
		for _, file := range files {
			if _, err := documents.UploadDocument(ctx, file, &transactionID, true); err != nil {
				failures = append(failures, displayName(file)+": "+documentsservice.Message(err, language))
			}
		}
	})
	uploaded, uploadError := documents.GetDocumentsForTransaction(ctx, transactionID)
	if uploadError != nil {
		message := uploadError.Message(language)
		v.state.Error = &message
		return nil
	}
	var message *string
	if len(failures) > 0 {
		joined := strings.Join(failures, "\n")
		message = &joined
	}
	if v.state.Selected != nil && v.state.Selected.ID == transactionID {
		v.state.SelectedDocuments = uploaded
	}
	v.state.Error = message
	return v.refresh(ctx, d, today)
}

// RulePatternFor returns the suggested rule pattern for a transaction.
func (v *TransactionsView) RulePatternFor(transaction *model.TransactionItem) string {
	raw := ""
	switch merchant := rules.MerchantOf(transaction.Purpose, transaction.PartnerName); {
	case merchant != nil:
		raw = *merchant
	case transaction.PartnerName != nil:
		raw = *transaction.PartnerName
	case transaction.Purpose != nil:
		raw = *transaction.Purpose
	}

	words := make([]string, 0, 3)
	for _, word := range strings.Fields(raw) {
		if word == "" {
			continue
		}
		digits := 0
		for _, character := range word {
			if character >= '0' && character <= '9' {
				digits++
			}
		}
		if digits >= 3 {
			continue
		}
		words = append(words, word)
		if len(words) == 3 {
			break
		}
	}
	joined := strings.Join(words, " ")

	var escaped strings.Builder
	escaped.Grow(len(joined))
	for _, character := range joined {
		switch character {
		case '\\', '.', '^', '$', '|', '?', '*', '+', '(', ')', '[', ']', '{', '}':
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(character)
	}
	return escaped.String()
}

// CreateRule creates an auto-categorization rule and applies it to all
// uncategorized transactions.
func (v *TransactionsView) CreateRule(
	ctx context.Context,
	d *db.Database,
	pattern string,
	categoryID model.CategoryID,
	today civil.Date,
) error {
	if _, err := rulesrepo.Create(ctx, d, model.NewRule{
		Pattern:     pattern,
		CategoryID:  categoryID,
		Priority:    5,
		AutoApply:   true,
		MatchField:  model.RuleFieldAny,
		Direction:   model.RuleDirectionAny,
		TaxRelevant: nil,
	}); err != nil {
		return accountpanel.RepoError(err)
	}

	uncategorizedFilter := model.TransactionFilter{
		UncategorizedOnly: true,
		Direction:         model.DirectionAll,
	}
	uncategorized, err := transactions.FindItems(ctx, d, &uncategorizedFilter, nil)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	matcher := rules.Of([]rules.RulePair{{Pattern: pattern, CategoryID: categoryID}})
	changes := make([]transactions.CategoryChange, 0)
	for _, transaction := range uncategorized {
		amount := decimalAmount(transaction.Amount)
		if matcher.CategoryFor(transaction.Purpose, transaction.PartnerName, &amount) == nil {
			continue
		}
		target := categoryID
		changes = append(changes, transactions.CategoryChange{
			TransactionID: transaction.ID,
			CategoryID:    &target,
		})
	}
	applied := 0
	if len(changes) > 0 {
		applied, err = transactions.UpdateCategories(ctx, d, changes)
		if err != nil {
			return accountpanel.RepoError(err)
		}
	}
	if err := v.refresh(ctx, d, today); err != nil {
		return err
	}
	text := i18n.Format(v.state.Language, i18n.MessageRuleCreated, i18n.Int(int64(applied)))
	v.state.Message = &text
	return nil
}

// UpdateTransactionCategory changes one booking's category.
func (v *TransactionsView) UpdateTransactionCategory(
	ctx context.Context,
	d *db.Database,
	transactionID model.TransactionID,
	categoryID *model.CategoryID,
	today civil.Date,
) error {
	if err := transactions.SetCategory(ctx, d, transactionID, categoryID); err != nil {
		return accountpanel.RepoError(err)
	}
	return v.refresh(ctx, d, today)
}

// AddManualTransaction adds a manual booking, asking first when an identical one
// already exists that day.
func (v *TransactionsView) AddManualTransaction(
	ctx context.Context,
	d *db.Database,
	entry ManualTransaction,
	today civil.Date,
	now time.Time,
) error {
	if entry.Amount.Minor == 0 {
		return v.userError(i18n.ErrorAmountNotZero)
	}
	account, ok, err := accounts.Get(ctx, d, entry.AccountID)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	if !ok {
		return v.userError(i18n.ErrorAccountNotFound)
	}

	var partner *string
	if entry.PartnerName != nil {
		trimmed := strings.TrimSpace(*entry.PartnerName)
		if trimmed != "" {
			partner = &trimmed
		}
	}
	var text *string
	if entry.Purpose != nil {
		trimmed := strings.TrimSpace(*entry.Purpose)
		if trimmed != "" {
			text = &trimmed
		}
	}

	date := entry.Date
	accountID := entry.AccountID
	identicalFilter := model.TransactionFilter{
		StartDate: &date,
		EndDate:   &date,
		AccountID: &accountID,
		Direction: model.DirectionAll,
	}
	existing, err := transactions.FindItems(ctx, d, &identicalFilter, nil)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	identical := 0
	for _, transaction := range existing {
		if transaction.Amount.Minor == entry.Amount.Minor &&
			equalOptionalString(transaction.PartnerName, partner) &&
			equalOptionalString(transaction.Purpose, text) {
			identical++
		}
	}

	if identical > 0 && !entry.ConfirmedDuplicate {
		currency := account.Currency
		v.state.PendingDuplicate = &ManualEntry{
			AccountID:   entry.AccountID,
			Date:        entry.Date,
			Amount:      entry.Amount,
			Currency:    currency,
			PartnerName: partner,
			Purpose:     text,
			CategoryID:  entry.CategoryID,
		}
		return nil
	}

	repeat := ""
	if identical > 0 {
		repeat = "#" + strconv.Itoa(identical)
	}
	ibanText := ""
	if account.IBAN != nil {
		ibanText = *account.IBAN
	}
	purposeText := ""
	if text != nil {
		purposeText = *text
	}
	combined := purposeText + "#" + ibanText + "#manual" + repeat
	hash := imports.TransactionHash(entry.Date, decimalAmount(entry.Amount), partner, &combined)

	var resolvedCategory *model.CategoryID
	if entry.CategoryID != nil {
		id := *entry.CategoryID
		resolvedCategory = &id
	} else {
		matcher, err := rules.Matcher(ctx, d)
		if err != nil {
			return accountpanel.RepoError(err)
		}
		amount := decimalAmount(entry.Amount)
		resolvedCategory = matcher.CategoryFor(entry.Purpose, entry.PartnerName, &amount)
	}

	if _, err := transactions.InsertImported(ctx, d, entry.AccountID, []model.NewTransaction{{
		BookingDate:  entry.Date,
		Amount:       entry.Amount,
		BalanceAfter: nil,
		PartnerName:  partner,
		Purpose:      text,
		CategoryID:   resolvedCategory,
		ImportHash:   hash,
		CreatedAt:    now,
		IsNew:        false,
		TaxOverride:  nil,
	}}); err != nil {
		return accountpanel.RepoError(err)
	}
	if err := v.refresh(ctx, d, today); err != nil {
		return err
	}
	message := i18n.Text(v.state.Language, i18n.MessageTransactionAdded)
	v.state.Message = &message
	return nil
}

// ConfirmDuplicate adds the pending identical entry after the user confirmed it
// is a second, real booking.
//
// It returns false when nothing was waiting for confirmation.
func (v *TransactionsView) ConfirmDuplicate(
	ctx context.Context,
	d *db.Database,
	today civil.Date,
	now time.Time,
) (bool, error) {
	entry := v.state.PendingDuplicate
	if entry == nil {
		return false, nil
	}
	v.state.PendingDuplicate = nil
	accountID := entry.AccountID
	date := entry.Date
	amount := entry.Amount
	partner := entry.PartnerName
	purpose := entry.Purpose
	category := entry.CategoryID
	if err := v.AddManualTransaction(ctx, d, ManualTransaction{
		AccountID:          accountID,
		Date:               date,
		Amount:             amount,
		PartnerName:        partner,
		Purpose:            purpose,
		CategoryID:         category,
		ConfirmedDuplicate: true,
	}, today, now); err != nil {
		return false, err
	}
	return true, nil
}

// DismissDuplicate closes the duplicate confirmation without adding anything.
func (v *TransactionsView) DismissDuplicate() {
	v.state.PendingDuplicate = nil
}

// DeleteTransaction deletes one booking.
func (v *TransactionsView) DeleteTransaction(
	ctx context.Context,
	d *db.Database,
	transactionID model.TransactionID,
	today civil.Date,
) error {
	if err := transactions.Delete(ctx, d, transactionID); err != nil {
		return accountpanel.RepoError(err)
	}
	return v.refresh(ctx, d, today)
}

// ToggleSelection toggles one booking in the multi-selection and makes it the
// range anchor.
func (v *TransactionsView) ToggleSelection(id model.TransactionID) {
	if _, selected := v.state.SelectedIDs[id]; selected {
		delete(v.state.SelectedIDs, id)
	} else {
		v.state.SelectedIDs[id] = struct{}{}
	}
	anchor := id
	v.state.SelectionAnchorID = &anchor
}

// SelectRange selects every shown booking between the anchor and id, inclusive
// (Shift-click).
func (v *TransactionsView) SelectRange(id model.TransactionID) {
	if v.state.SelectionAnchorID == nil {
		v.ToggleSelection(id)
		return
	}
	anchor := *v.state.SelectionAnchorID
	from := -1
	to := -1
	for index, transaction := range v.state.Transactions {
		if transaction.ID == anchor {
			from = index
		}
		if transaction.ID == id {
			to = index
		}
	}
	if from < 0 || to < 0 {
		v.ToggleSelection(id)
		return
	}
	low, high := from, to
	if low > high {
		low, high = high, low
	}
	for index := low; index <= high; index++ {
		v.state.SelectedIDs[v.state.Transactions[index].ID] = struct{}{}
	}
	target := id
	v.state.SelectionAnchorID = &target
}

// SelectAllShown selects every booking the current filters show.
func (v *TransactionsView) SelectAllShown() {
	selected := make(map[model.TransactionID]struct{}, len(v.state.Transactions))
	for _, transaction := range v.state.Transactions {
		selected[transaction.ID] = struct{}{}
	}
	v.state.SelectedIDs = selected
}

// ClearSelection empties the multi-selection.
func (v *TransactionsView) ClearSelection() {
	v.state.SelectedIDs = make(map[model.TransactionID]struct{})
	v.state.SelectionAnchorID = nil
}

// DeleteSelected deletes exactly the passed ids that are selected and still
// shown.
func (v *TransactionsView) DeleteSelected(
	ctx context.Context,
	d *db.Database,
	ids map[model.TransactionID]struct{},
	today civil.Date,
) error {
	if len(ids) == 0 {
		return nil
	}
	shown := make(map[model.TransactionID]struct{}, len(v.state.Transactions))
	for _, transaction := range v.state.Transactions {
		shown[transaction.ID] = struct{}{}
	}
	targets := make([]model.TransactionID, 0)
	for id := range ids {
		if _, selected := v.state.SelectedIDs[id]; !selected {
			continue
		}
		if _, visible := shown[id]; !visible {
			continue
		}
		targets = append(targets, id)
	}
	sortTransactionIDs(targets)
	if len(targets) > 0 {
		deleted, err := transactions.DeleteAll(ctx, d, targets)
		if err != nil {
			return accountpanel.RepoError(err)
		}
		if deleted > 0 {
			text := i18n.Format(v.state.Language, i18n.MessageTransactionsDeleted, i18n.Int(int64(deleted)))
			v.state.Message = &text
		}
	}
	v.ClearSelection()
	return v.refresh(ctx, d, today)
}

// SetTaxOverrideForSelection sets the manual tax override of the selected
// bookings that are still shown.
func (v *TransactionsView) SetTaxOverrideForSelection(
	ctx context.Context,
	d *db.Database,
	value *bool,
	today civil.Date,
) error {
	shown := make(map[model.TransactionID]struct{}, len(v.state.Transactions))
	for _, transaction := range v.state.Transactions {
		shown[transaction.ID] = struct{}{}
	}
	targets := make([]model.TransactionID, 0)
	for id := range v.state.SelectedIDs {
		if _, visible := shown[id]; visible {
			targets = append(targets, id)
		}
	}
	sortTransactionIDs(targets)
	changed, err := transactions.SetTaxOverride(ctx, d, targets, value)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	if changed > 0 {
		text := i18n.Format(v.state.Language, i18n.MessageTaxOverrideSet, i18n.Int(int64(changed)))
		v.state.Message = &text
	}
	return v.refresh(ctx, d, today)
}

// SetCategoryForSelection sets the category of the selected bookings that are
// still shown.
func (v *TransactionsView) SetCategoryForSelection(
	ctx context.Context,
	d *db.Database,
	categoryID *model.CategoryID,
	today civil.Date,
) error {
	// Only bookings that actually move are written, so an all-already-there
	// selection writes no audit entry.
	targets := make([]transactions.CategoryChange, 0)
	for index := range v.state.Transactions {
		transaction := &v.state.Transactions[index]
		if _, selected := v.state.SelectedIDs[transaction.ID]; !selected {
			continue
		}
		if equalOptionalCategoryID(transaction.CategoryID, categoryID) {
			continue
		}
		targets = append(targets, transactions.CategoryChange{
			TransactionID: transaction.ID,
			CategoryID:    categoryID,
		})
	}
	changed := 0
	var err error
	if len(targets) > 0 {
		changed, err = transactions.UpdateCategories(ctx, d, targets)
		if err != nil {
			return accountpanel.RepoError(err)
		}
	}
	if changed > 0 {
		text := i18n.Format(v.state.Language, i18n.MessageCategorySet, i18n.Int(int64(changed)))
		v.state.Message = &text
	}
	return v.refresh(ctx, d, today)
}

// SetTaxOverride sets the manual tax override of one booking from the details
// dialog.
func (v *TransactionsView) SetTaxOverride(
	ctx context.Context,
	d *db.Database,
	transactionID model.TransactionID,
	value *bool,
	today civil.Date,
) error {
	changed, err := transactions.SetTaxOverride(ctx, d, []model.TransactionID{transactionID}, value)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	if changed > 0 {
		text := i18n.Format(v.state.Language, i18n.MessageTaxOverrideSet, i18n.Int(int64(changed)))
		v.state.Message = &text
	}
	return v.refresh(ctx, d, today)
}

// AutoCategorize applies the auto-apply rules to every uncategorized booking.
func (v *TransactionsView) AutoCategorize(
	ctx context.Context,
	d *db.Database,
	today civil.Date,
) error {
	uncategorizedFilter := model.TransactionFilter{
		UncategorizedOnly: true,
		Direction:         model.DirectionAll,
	}
	uncategorized, err := transactions.FindItems(ctx, d, &uncategorizedFilter, nil)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	matcher, err := rules.Matcher(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	changes := make([]transactions.CategoryChange, 0)
	for _, transaction := range uncategorized {
		amount := decimalAmount(transaction.Amount)
		matched := matcher.CategoryFor(transaction.Purpose, transaction.PartnerName, &amount)
		if matched == nil {
			continue
		}
		changes = append(changes, transactions.CategoryChange{
			TransactionID: transaction.ID,
			CategoryID:    matched,
		})
	}
	updated := 0
	if len(changes) > 0 {
		updated, err = transactions.UpdateCategories(ctx, d, changes)
		if err != nil {
			return accountpanel.RepoError(err)
		}
	}
	if err := v.refresh(ctx, d, today); err != nil {
		return err
	}
	text := i18n.Format(
		v.state.Language,
		i18n.MessageAutoCategorized,
		i18n.Int(int64(updated)),
		i18n.Int(int64(len(uncategorized))),
	)
	v.state.Message = &text
	return nil
}

// PreviewReapplyRules computes the re-apply preview for exactly the bookings the
// current filters show.
func (v *TransactionsView) PreviewReapplyRules(
	ctx context.Context,
	d *db.Database,
	today civil.Date,
) error {
	filter := v.CurrentFilter(today)
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	matcher, err := rules.Matcher(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	changes := make([]ReapplyChange, 0)
	for _, transaction := range items {
		amount := decimalAmount(transaction.Amount)
		matched := matcher.CategoryFor(transaction.Purpose, transaction.PartnerName, &amount)
		if matched == nil {
			continue
		}
		if equalOptionalCategoryID(transaction.CategoryID, matched) {
			continue
		}
		changes = append(changes, ReapplyChange{
			TransactionID:  transaction.ID,
			FromCategoryID: transaction.CategoryID,
			ToCategoryID:   *matched,
		})
	}
	if len(changes) == 0 {
		v.state.ReapplyPreview = nil
		text := i18n.Text(v.state.Language, i18n.MessageReapplyNothing)
		v.state.Message = &text
		return nil
	}

	primary, err := money.Primary(ctx, d)
	if err != nil {
		return accountpanel.DBError(err)
	}
	listedCategories, err := categories.List(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	names := make(map[model.CategoryID]string, len(listedCategories))
	for _, category := range listedCategories {
		names[category.ID] = category.Name
	}
	uncategorizedName := i18n.Text(v.state.Language, i18n.Uncategorized)
	byID := make(map[model.TransactionID]*model.TransactionItem, len(items))
	for index := range items {
		byID[items[index].ID] = &items[index]
	}

	type groupEntry struct {
		key     ReapplyKey
		changes []ReapplyChange
	}
	grouped := make(map[reapplyKeyValue]*groupEntry)
	ordered := make([]*groupEntry, 0)
	for _, change := range changes {
		key := changeKey(change)
		entry, ok := grouped[key]
		if !ok {
			entry = &groupEntry{key: ReapplyKey{
				FromCategoryID: change.FromCategoryID,
				ToCategoryID:   change.ToCategoryID,
			}}
			grouped[key] = entry
			ordered = append(ordered, entry)
		}
		entry.changes = append(entry.changes, change)
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		return compareReapplyKeys(ordered[left].key, ordered[right].key) < 0
	})

	groups := make([]ReapplyGroup, 0, len(ordered))
	for _, entry := range ordered {
		bookings := make([]*model.TransactionItem, 0, len(entry.changes))
		for _, change := range entry.changes {
			if booking, ok := byID[change.TransactionID]; ok {
				bookings = append(bookings, booking)
			}
		}
		allPrimary := true
		total := int64(0)
		for _, booking := range bookings {
			if booking.Currency != primary {
				allPrimary = false
				break
			}
			total += booking.Amount.Minor
		}
		var sum *money.Money
		if allPrimary {
			value := money.Money{Minor: total, Currency: primary}
			sum = &value
		}
		fromCategoryID := entry.key.FromCategoryID
		fromName := uncategorizedName
		if fromCategoryID != nil {
			if name, ok := names[*fromCategoryID]; ok {
				fromName = name
			} else {
				fromName = strconv.FormatInt(int64(*fromCategoryID), 10)
			}
		}
		toName, ok := names[entry.key.ToCategoryID]
		if !ok {
			toName = strconv.FormatInt(int64(entry.key.ToCategoryID), 10)
		}
		groups = append(groups, ReapplyGroup{
			FromCategoryID: fromCategoryID,
			FromName:       fromName,
			ToCategoryID:   entry.key.ToCategoryID,
			ToName:         toName,
			Count:          len(entry.changes),
			Sum:            sum,
			Currency:       primary,
		})
	}
	sort.SliceStable(groups, func(left, right int) bool {
		if groups[left].Count != groups[right].Count {
			return groups[left].Count > groups[right].Count
		}
		if groups[left].FromName != groups[right].FromName {
			return groups[left].FromName < groups[right].FromName
		}
		return groups[left].ToName < groups[right].ToName
	})

	v.state.ReapplyPreview = &ReapplyPreview{
		Changes:    changes,
		Groups:     groups,
		ShownCount: len(items),
	}
	return nil
}

// CancelReapplyRules closes the preview dialog without changing anything.
func (v *TransactionsView) CancelReapplyRules() {
	v.state.ReapplyPreview = nil
}

// ConfirmReapplyRules applies exactly the stored preview's changes minus the
// excluded groups.
func (v *TransactionsView) ConfirmReapplyRules(
	ctx context.Context,
	d *db.Database,
	excluded map[ReapplyKey]struct{},
	today civil.Date,
) error {
	preview := v.state.ReapplyPreview
	if preview == nil {
		return nil
	}
	v.state.ReapplyPreview = nil

	current, err := categoryByTransaction(ctx, d)
	if err != nil {
		return err
	}
	excludedValues := make(map[reapplyKeyValue]struct{}, len(excluded))
	for key := range excluded {
		excludedValues[key.value()] = struct{}{}
	}
	changes := make([]ReapplyChange, 0, len(preview.Changes))
	for _, change := range preview.Changes {
		if _, skip := excludedValues[changeKey(change)]; skip {
			continue
		}
		value, ok := current[change.TransactionID]
		if !ok || !equalOptionalCategoryID(value, change.FromCategoryID) {
			continue
		}
		changes = append(changes, change)
	}
	count := 0
	if len(changes) > 0 {
		updates := make([]transactions.CategoryChange, 0, len(changes))
		for _, change := range changes {
			target := change.ToCategoryID
			updates = append(updates, transactions.CategoryChange{
				TransactionID: change.TransactionID,
				CategoryID:    &target,
			})
		}
		count, err = transactions.UpdateCategories(ctx, d, updates)
		if err != nil {
			return accountpanel.RepoError(err)
		}
	}
	if count > 0 {
		v.reapplyUndo = changes
	}
	if err := v.refresh(ctx, d, today); err != nil {
		return err
	}
	text := i18n.Format(v.state.Language, i18n.MessageReapplied, i18n.Int(int64(count)))
	v.state.Message = &text
	v.state.CanUndoReapply = len(v.reapplyUndo) > 0
	return nil
}

// UndoReapplyRules restores the previous categories of the last re-apply.
func (v *TransactionsView) UndoReapplyRules(
	ctx context.Context,
	d *db.Database,
	today civil.Date,
) error {
	if len(v.reapplyUndo) == 0 {
		return nil
	}
	current, err := categoryByTransaction(ctx, d)
	if err != nil {
		return err
	}
	previous := make([]transactions.CategoryChange, 0)
	for _, change := range v.reapplyUndo {
		value, ok := current[change.TransactionID]
		if !ok {
			continue
		}
		target := change.ToCategoryID
		if !equalOptionalCategoryID(value, &target) {
			continue
		}
		previous = append(previous, transactions.CategoryChange{
			TransactionID: change.TransactionID,
			CategoryID:    change.FromCategoryID,
		})
	}
	count := 0
	if len(previous) > 0 {
		count, err = transactions.UpdateCategories(ctx, d, previous)
		if err != nil {
			return accountpanel.RepoError(err)
		}
	}
	v.reapplyUndo = make([]ReapplyChange, 0)
	if err := v.refresh(ctx, d, today); err != nil {
		return err
	}
	text := i18n.Format(v.state.Language, i18n.MessageReapplyUndone, i18n.Int(int64(count)))
	v.state.Message = &text
	v.state.CanUndoReapply = false
	return nil
}

// ExportCSV exports exactly what is currently shown.
func (v *TransactionsView) ExportCSV(_ context.Context, _ *db.Database, file string) error {
	var balances map[model.TransactionID]money.Money
	if v.state.Balances != nil {
		balances = make(map[model.TransactionID]money.Money, len(v.state.Balances))
		for id, entry := range v.state.Balances {
			balances[id] = entry.Balance
		}
	}
	name := displayName(file)
	if err := report.ExportToCSV(v.state.Transactions, file, balances, v.state.Language); err != nil {
		text := i18n.Text(v.state.Language, i18n.ErrorExportFailed)
		v.state.Error = &text
		return nil
	}
	text := i18n.Format(
		v.state.Language,
		i18n.MessageExportedCount,
		i18n.Int(int64(len(v.state.Transactions))),
		i18n.Str(name),
	)
	v.state.Message = &text
	return nil
}

// OpenImport reads the selected statement files and opens the import preview
// (nothing is stored yet).
func (v *TransactionsView) OpenImport(
	ctx context.Context,
	d *db.Database,
	files []string,
	mapping *imports.CsvMapping,
	today civil.Date,
) error {
	return v.readImportFiles(ctx, d, files, func(string) *imports.CsvMapping { return mapping }, today)
}

func (v *TransactionsView) readImportFiles(
	ctx context.Context,
	d *db.Database,
	files []string,
	mappingFor func(string) *imports.CsvMapping,
	today civil.Date,
) error {
	if len(files) == 0 {
		return nil
	}
	if len(files) > MaxImportFiles {
		text := i18n.Format(v.state.Language, i18n.ErrorImportTooManyFiles, i18n.Int(MaxImportFiles))
		v.state.Error = &text
		return nil
	}
	totalBytes := int64(0)
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if size := info.Size(); size > 0 {
			totalBytes += size
		}
	}
	if totalBytes > maxImportTotalBytes {
		text := i18n.Format(
			v.state.Language,
			i18n.ErrorImportFilesTooLarge,
			i18n.Int(int64(MaxImportTotalMB)),
		)
		v.state.Error = &text
		return nil
	}

	entries := make([]ImportEntry, 0)
	failures := make([]ImportFailure, 0)
	for _, file := range files {
		statement, message := readStatement(file, mappingFor(file), today, v.state.Language)
		if message != "" {
			failures = append(failures, ImportFailure{FileName: displayName(file), Message: message})
			continue
		}
		entries = append(entries, ImportEntry{File: file, Statement: statement})
	}
	if len(entries) == 0 {
		v.state.ImportPreview = nil
		var text string
		if len(failures) == 1 {
			text = failures[0].Message
		} else {
			joined := make([]string, 0, len(failures))
			for _, failure := range failures {
				joined = append(joined, failure.FileName+": "+failure.Message)
			}
			text = i18n.Format(
				v.state.Language,
				i18n.ErrorImportNoReadableFile,
				i18n.Str(strings.Join(joined, ", ")),
			)
		}
		v.state.Error = &text
		return nil
	}

	ordered := sortImportEntries(entries)

	ibans := make([]string, 0)
	for _, entry := range ordered {
		if entry.Statement.AccountIBAN == nil {
			continue
		}
		normalised := iban.Normalize(*entry.Statement.AccountIBAN)
		found := false
		for _, seen := range ibans {
			if seen == normalised {
				found = true
				break
			}
		}
		if !found {
			ibans = append(ibans, normalised)
		}
	}
	if len(ibans) > 1 {
		masked := make([]string, 0, len(ibans))
		for _, value := range ibans {
			masked = append(masked, iban.Mask(value))
		}
		text := i18n.Format(
			v.state.Language,
			i18n.ErrorImportDifferentAccounts,
			i18n.Str(strings.Join(masked, ", ")),
		)
		v.state.Error = &text
		return nil
	}

	statement := &ordered[len(ordered)-1].Statement
	for index := range ordered {
		if ordered[index].Statement.AccountIBAN != nil {
			statement = &ordered[index].Statement
			break
		}
	}
	var accountID *model.AccountID
	accountID = imports.MatchAccount(ctx, d, *statement)
	if accountID == nil && v.state.ImportPreview != nil {
		accountID = v.state.ImportPreview.AccountID
	}
	if accountID == nil {
		accountID = v.state.AccountID
	}
	if accountID == nil && len(v.state.Accounts) == 1 {
		id := v.state.Accounts[0].ID
		accountID = &id
	}
	newest := &ordered[len(ordered)-1].Statement
	allowed := false
	if accountID != nil {
		allowedValue, err := imports.BalanceUpdateAllowed(ctx, d, *accountID, *newest, today)
		if err == nil {
			allowed = allowedValue
		}
	}

	v.state.ImportPreview = &ImportPreview{
		Files:                append([]string{}, files...),
		Entries:              ordered,
		Failures:             failures,
		AccountID:            accountID,
		BalanceUpdateAllowed: allowed,
	}
	v.state.Error = nil
	return nil
}

// RemapImport re-reads the CSV files with a user-adjusted column mapping.
func (v *TransactionsView) RemapImport(
	ctx context.Context,
	d *db.Database,
	mapping imports.CsvMapping,
	today civil.Date,
) error {
	if v.state.ImportPreview == nil {
		return nil
	}
	preview := *v.state.ImportPreview
	var firstCsv *imports.CsvLayout
	for index := range preview.Entries {
		if preview.Entries[index].Statement.Csv != nil {
			firstCsv = preview.Entries[index].Statement.Csv
			break
		}
	}
	return v.readImportFiles(ctx, d, preview.Files, func(file string) *imports.CsvMapping {
		var entry *ImportEntry
		for index := range preview.Entries {
			if preview.Entries[index].File == file {
				entry = &preview.Entries[index]
				break
			}
		}
		if firstCsv != nil && entry != nil && entry.Statement.Csv != nil &&
			equalStrings(entry.Statement.Csv.Headers, firstCsv.Headers) {
			value := mapping
			return &value
		}
		return nil
	}, today)
}

// SelectImportAccount picks the account for the open preview.
func (v *TransactionsView) SelectImportAccount(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	today civil.Date,
) error {
	if v.state.ImportPreview != nil {
		v.state.ImportPreview.AccountID = &accountID
		v.state.ImportPreview.BalanceUpdateAllowed = false
	}
	if v.state.ImportPreview == nil {
		return nil
	}
	preview := *v.state.ImportPreview
	allowed, err := imports.BalanceUpdateAllowed(ctx, d, accountID, preview.Newest().Statement, today)
	allowedValue := allowed
	if err != nil {
		allowedValue = false
	}
	if v.state.ImportPreview != nil && v.state.ImportPreview.AccountID != nil &&
		*v.state.ImportPreview.AccountID == accountID {
		v.state.ImportPreview.BalanceUpdateAllowed = allowedValue
	}
	return nil
}

// CancelImport closes the import preview.
func (v *TransactionsView) CancelImport() {
	v.state.ImportPreview = nil
}

// ConfirmImport imports the previewed entries in order, each with its own dedup
// hashing and audit entry.
func (v *TransactionsView) ConfirmImport(
	ctx context.Context,
	d *db.Database,
	updateBalance bool,
	today civil.Date,
) error {
	if v.state.ImportPreview == nil {
		return nil
	}
	preview := *v.state.ImportPreview
	if preview.AccountID == nil {
		return nil
	}
	accountID := *preview.AccountID
	v.state.IsImporting = true
	newestIndex := len(preview.Entries) - 1
	var aggregate *imports.ImportReport
	importedFiles := 0
	for index, entry := range preview.Entries {
		shouldUpdateBalance := updateBalance && index == newestIndex
		var (
			report    imports.ImportReport
			importErr error
		)
		v.busy.Run(func() {
			report, importErr = imports.Import(ctx, d, accountID, entry.Statement, shouldUpdateBalance, today)
		})
		if importErr != nil {
			failedName := displayName(entry.File)
			failedMessage := importErrorMessage(importErr, v.state.Language)
			text := i18n.Format(
				v.state.Language,
				i18n.ErrorImportStopped,
				i18n.Str(failedName),
				i18n.Str(failedMessage),
				i18n.Int(int64(importedFiles)),
			)
			v.state.ImportPreview = nil
			v.state.IsImporting = false
			v.state.Error = &text
			if err := v.refresh(ctx, d, today); err != nil {
				return err
			}
			return nil
		}
		if aggregate == nil {
			value := report
			aggregate = &value
		} else {
			combined := combineReports(*aggregate, report)
			aggregate = &combined
		}
		importedFiles++
	}
	if aggregate == nil {
		v.state.IsImporting = false
		return nil
	}
	v.state.ImportPreview = nil
	v.state.IsImporting = false
	v.state.Period = PeriodAll
	v.state.Month = nil
	v.state.CustomRange = nil
	v.state.Selection = accountpanel.AccountSelectionFor(accountID)
	v.state.AccountID = &accountID
	text := importSummary(v.state.Language, *aggregate, importedFiles)
	v.state.Message = &text
	v.ClearSelection()
	return v.refresh(ctx, d, today)
}

// ClearMessages clears the error and success message.
func (v *TransactionsView) ClearMessages() {
	v.state.Error = nil
	v.state.Message = nil
}

func (v *TransactionsView) userError(key i18n.Key) error {
	text := i18n.Text(v.state.Language, key)
	v.state.Error = &text
	return nil
}

// RecordError records the user-visible text of error and returns error.
func (v *TransactionsView) RecordError(err error) error {
	v.state.IsLoading = false
	key := i18n.ErrorUnexpected
	var appError *accountpanel.AppError
	if errors.As(err, &appError) {
		if messageKey, ok := appError.Message(); ok {
			key = messageKey
		}
	}
	text := i18n.Text(v.state.Language, key)
	v.state.Error = &text
	return err
}

// actionError handles a group action's failure like Rust's match on AppError:
// a user-visible message is shown and swallowed, anything else is recorded.
func (v *TransactionsView) actionError(err error) error {
	var appError *accountpanel.AppError
	if errors.As(err, &appError) {
		if key, ok := appError.Message(); ok {
			text := i18n.Text(v.state.Language, key)
			v.state.Error = &text
			return nil
		}
	}
	return v.RecordError(err)
}

// decimalAmount converts a money amount to the decimal the rule matcher and
// hashes expect.
func decimalAmount(value money.Money) decimal.Decimal {
	return decimal.New(value.Minor, decimalScale(value.Currency))
}

// decimalScale is money.Currency's minor exponent as the scale decimal.New
// takes. The currency exponent is 0, 2 or 3; anything else never occurs.
func decimalScale(currency money.Currency) int32 {
	switch currency.MinorExponent() {
	case 0:
		return 0
	case 2:
		return 2
	case 3:
		return 3
	default:
		panic("unexpected currency minor exponent")
	}
}

// transactionExists reports whether id still exists, without a dedicated
// repository lookup.
func transactionExists(ctx context.Context, d *db.Database, id model.TransactionID) (bool, error) {
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return false, accountpanel.RepoError(err)
	}
	for _, transaction := range items {
		if transaction.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// groupingAccount returns the grouping form of an account row.
func groupingAccount(account accountsview.AccountItem) accountgrouping.AccountItem {
	return accountgrouping.AccountItem{
		ID:          account.ID,
		Name:        account.Name,
		Currency:    account.Currency,
		Balance:     account.Balance,
		AccountType: account.AccountType,
		GroupID:     account.GroupID,
		NewCount:    account.NewCount,
	}
}

func groupingAccounts(items []accountsview.AccountItem) []accountgrouping.AccountItem {
	grouped := make([]accountgrouping.AccountItem, 0, len(items))
	for _, account := range items {
		grouped = append(grouped, groupingAccount(account))
	}
	return grouped
}

func groupAccounts(
	items []accountsview.AccountItem,
	customGroups []model.AccountGroup,
	primary money.Currency,
) []accountgrouping.AccountGroupView {
	return accountgrouping.Group(groupingAccounts(items), customGroups, primary)
}

// loadAccountItems loads every account row with its bank name and unread
// counter, like the Accounts view.
func loadAccountItems(ctx context.Context, d *db.Database) ([]accountsview.AccountItem, error) {
	listed, err := accounts.List(ctx, d)
	if err != nil {
		return nil, accountpanel.RepoError(err)
	}
	bankAccesses, err := bankconnections.ListItems(ctx, d)
	if err != nil {
		return nil, accountpanel.RepoError(err)
	}
	newCounts, err := transactions.CountNew(ctx, d)
	if err != nil {
		return nil, accountpanel.RepoError(err)
	}
	bankNames := make(map[model.BankConnectionID]string, len(bankAccesses))
	for _, bank := range bankAccesses {
		bankNames[bank.ID] = bank.BankName
	}
	items := make([]accountsview.AccountItem, 0, len(listed))
	for _, account := range listed {
		bankName := ""
		if account.BankConnectionID != nil {
			bankName = bankNames[*account.BankConnectionID]
		}
		ibanText := ""
		if account.IBAN != nil {
			ibanText = *account.IBAN
		}
		items = append(items, accountsview.AccountItem{
			ID:            account.ID,
			BankAccessID:  account.BankConnectionID,
			BankName:      bankName,
			IBAN:          ibanText,
			AccountNumber: account.AccountNumber,
			Balance:       account.Balance,
			Currency:      account.Currency,
			AccountType:   account.AccountType.AsStr(),
			LastUpdate:    account.BalanceUpdatedAt,
			Name:          account.Name,
			GroupID:       account.GroupID,
			NewCount:      newCounts[account.ID],
		})
	}
	return items, nil
}

func loadChartCollapsed(ctx context.Context, d *db.Database) (bool, error) {
	stored, ok, err := settings.Get(ctx, d, ChartCollapsedKey)
	if err != nil {
		return false, accountpanel.DBError(err)
	}
	if !ok {
		return false, nil
	}
	switch stored {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, nil
	}
}

func displayName(file string) string {
	base := filepath.Base(file)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return file
	}
	return base
}

// readStatement reads one statement, returning FinDesk's user-facing reason on
// failure.
func readStatement(
	file string,
	mapping *imports.CsvMapping,
	today civil.Date,
	language i18n.Language,
) (imports.ParsedStatement, string) {
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return imports.ParsedStatement{}, i18n.Format(
			language,
			i18n.ErrorFileNotFound,
			i18n.Str(displayName(file)),
		)
	}
	if info.Size() > maxFileBytes {
		return imports.ParsedStatement{}, i18n.Text(language, i18n.ErrorFileTooLarge)
	}
	data, err := os.ReadFile(file) // #nosec G304 -- the import path is chosen by the caller.
	if err != nil {
		return imports.ParsedStatement{}, i18n.Text(language, i18n.ErrorUnexpected)
	}
	statement, err := imports.Read(displayName(file), data, mapping, today)
	if err != nil {
		return imports.ParsedStatement{}, importErrorText(err, language)
	}
	return statement, ""
}

func importErrorText(err error, language i18n.Language) string {
	switch {
	case errors.Is(err, imports.ErrFileTooLarge):
		return i18n.Text(language, i18n.ErrorFileTooLarge)
	case errors.Is(err, imports.ErrXlsxTooLarge):
		return i18n.Text(language, i18n.ErrorXlsxTooLarge)
	case errors.Is(err, imports.ErrXlsxUnreadable):
		return i18n.Text(language, i18n.ErrorXlsxUnreadable)
	default:
		// Parser diagnostics carry the specific reason; the surrounding UI
		// labels it as unreadable.
		return err.Error()
	}
}

func importErrorMessage(err error, language i18n.Language) string {
	switch {
	case errors.Is(err, imports.ErrAccountNotFound):
		return i18n.Text(language, i18n.ErrorAccountNotFound)
	default:
		var several *imports.SeveralAccountsError
		if errors.As(err, &several) {
			return several.Message
		}
		return i18n.Text(language, i18n.ErrorUnexpected)
	}
}

func combineReports(previous imports.ImportReport, next imports.ImportReport) imports.ImportReport {
	previous.Imported += next.Imported
	previous.Duplicates += next.Duplicates
	previous.Categorized += next.Categorized
	previous.BalanceUpdated = next.BalanceUpdated
	previous.ProbableDuplicates += next.ProbableDuplicates
	previous.WrongCurrency += next.WrongCurrency
	previous.Currency = next.Currency
	previous.PreviouslyDeleted += next.PreviouslyDeleted
	previous.InvalidAmount += next.InvalidAmount
	return previous
}

func importSummary(language i18n.Language, report imports.ImportReport, files int) string {
	var text string
	if files <= 1 {
		text = i18n.Format(language, i18n.ImportSummary, i18n.Int(int64(report.Imported)))
	} else {
		text = i18n.Format(
			language,
			i18n.ImportSummaryFiles,
			i18n.Int(int64(report.Imported)),
			i18n.Int(int64(files)),
		)
	}
	if report.Categorized > 0 {
		text += i18n.Format(language, i18n.ImportSummaryAuto, i18n.Int(int64(report.Categorized)))
	}
	if report.Duplicates > 0 {
		text += i18n.Format(language, i18n.ImportSummaryKnown, i18n.Int(int64(report.Duplicates)))
	}
	if report.ProbableDuplicates > 0 {
		text += i18n.Format(language, i18n.ImportSummaryProbable, i18n.Int(int64(report.ProbableDuplicates)))
	}
	if report.PreviouslyDeleted > 0 {
		text += i18n.Format(language, i18n.ImportSummaryDeleted, i18n.Int(int64(report.PreviouslyDeleted)))
	}
	if report.WrongCurrency > 0 {
		text += i18n.Format(language, i18n.ImportSummaryWrongCurrency, i18n.Int(int64(report.WrongCurrency)))
	}
	if report.BalanceUpdated != nil {
		if value, ok := decimalToMoney(*report.BalanceUpdated, report.Currency); ok {
			text += i18n.Format(
				language,
				i18n.ImportSummaryBalance,
				i18n.Str(format.Money(value, language, false)),
			)
		}
	}
	return text
}

func decimalToMoney(value decimal.Decimal, currency money.Currency) (money.Money, bool) {
	normalised := value.Normalize()
	scale := int64(normalised.Scale())
	exponent := int64(currency.MinorExponent())
	if scale > exponent {
		return money.Money{}, false
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(exponent-scale), nil)
	minor := new(big.Int).Mul(normalised.Mantissa(), factor)
	if !minor.IsInt64() {
		return money.Money{}, false
	}
	return money.Money{Minor: minor.Int64(), Currency: currency}, true
}

// sortImportEntries orders entries oldest first by their latest booking, empty
// files first, keeping selection order as the tie-break.
func sortImportEntries(entries []ImportEntry) []ImportEntry {
	type indexedEntry struct {
		index int
		entry ImportEntry
	}
	indexed := make([]indexedEntry, 0, len(entries))
	for index, entry := range entries {
		indexed = append(indexed, indexedEntry{index: index, entry: entry})
	}
	sort.SliceStable(indexed, func(left, right int) bool {
		a := indexed[left]
		b := indexed[right]
		aEmpty := len(a.entry.Statement.Bookings) == 0
		bEmpty := len(b.entry.Statement.Bookings) == 0
		if aEmpty != bEmpty {
			return aEmpty
		}
		aMax := newestBookingDate(a.entry.Statement.Bookings)
		bMax := newestBookingDate(b.entry.Statement.Bookings)
		if aMax != bMax {
			return aMax.Before(bMax)
		}
		aMin := oldestBookingDate(a.entry.Statement.Bookings)
		bMin := oldestBookingDate(b.entry.Statement.Bookings)
		if aMin != bMin {
			return aMin.Before(bMin)
		}
		return a.index < b.index
	})
	ordered := make([]ImportEntry, 0, len(indexed))
	for _, entry := range indexed {
		ordered = append(ordered, entry.entry)
	}
	return ordered
}

func newestBookingDate(bookings []imports.ImportedBooking) civil.Date {
	newest := civil.Date{}
	for index, booking := range bookings {
		if index == 0 || booking.Date.After(newest) {
			newest = booking.Date
		}
	}
	return newest
}

func oldestBookingDate(bookings []imports.ImportedBooking) civil.Date {
	oldest := civil.Date{}
	for index, booking := range bookings {
		if index == 0 || booking.Date.Before(oldest) {
			oldest = booking.Date
		}
	}
	return oldest
}

func compareReapplyKeys(left, right ReapplyKey) int {
	if left.FromCategoryID == nil {
		if right.FromCategoryID != nil {
			return -1
		}
	} else {
		if right.FromCategoryID == nil {
			return 1
		}
		if *left.FromCategoryID != *right.FromCategoryID {
			if *left.FromCategoryID < *right.FromCategoryID {
				return -1
			}
			return 1
		}
	}
	if left.ToCategoryID < right.ToCategoryID {
		return -1
	}
	if left.ToCategoryID > right.ToCategoryID {
		return 1
	}
	return 0
}

func categoryByTransaction(
	ctx context.Context,
	d *db.Database,
) (map[model.TransactionID]*model.CategoryID, error) {
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return nil, accountpanel.RepoError(err)
	}
	current := make(map[model.TransactionID]*model.CategoryID, len(items))
	for _, transaction := range items {
		current[transaction.ID] = transaction.CategoryID
	}
	return current, nil
}

func equalOptionalCategoryID(left, right *model.CategoryID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func absMinor(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func sortTransactionIDs(ids []model.TransactionID) {
	sort.Slice(ids, func(left, right int) bool {
		return ids[left] < ids[right]
	})
}

func mustDate(year int, month time.Month, day int) civil.Date {
	date, ok := civil.New(year, month, day)
	if !ok {
		panic("valid date")
	}
	return date
}
