package bind

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/banktext"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/iban"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/report"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// TransactionsEvent is the name of the event emitted after every Transactions
// change.
const TransactionsEvent = "transactions:changed"

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[TransactionsDTO](TransactionsEvent)
}

var errTransactionsNotStarted = errors.New("transactions is not started")

const (
	transactionsChartWidth   float64 = 700
	transactionsChartHeight  float64 = 200
	transactionsChartPadding float64 = 8
)

// TransactionRowDTO is one row of the booking list, including the pending bank
// lines shown above it.
type TransactionRowDTO struct {
	Pending         bool   `json:"pending"`
	ID              int    `json:"id"`
	DateText        string `json:"dateText"`
	Title           string `json:"title"`
	Subtitle        string `json:"subtitle"`
	CategoryText    string `json:"categoryText"`
	CategoryColour  string `json:"categoryColour"`
	Partner         string `json:"partner"`
	PartnerLinkText string `json:"partnerLinkText"`
	AmountText      string `json:"amountText"`
	Expense         bool   `json:"expense"`
	IsNew           bool   `json:"isNew"`
	Selected        bool   `json:"selected"`
	HasDocuments    bool   `json:"hasDocuments"`
	DocumentsText   string `json:"documentsText"`
	BalanceText     string `json:"balanceText"`
	BalanceTone     string `json:"balanceTone"`
	BalanceWarning  bool   `json:"balanceWarning"`
	BalanceMismatch string `json:"balanceMismatch"`
}

// ReapplyGroupRowDTO is one checkbox row of the re-apply preview dialog.
type ReapplyGroupRowDTO struct {
	FromID int    `json:"fromId"`
	ToID   int    `json:"toId"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Ticked bool   `json:"ticked"`
}

// TransactionsDTO is everything the Transactions page shows.
//
// Money is already formatted by the view; chart coordinates stay numbers.
// Every value Rust's apply_with_excluded, apply_reapply, apply_chart,
// apply_selection, apply_detail and apply_duplicate set has a field here. The
// statement-import dialog is G8c-2 and has no fields yet.
type TransactionsDTO struct {
	Loading     bool   `json:"loading"`
	HasAccounts bool   `json:"hasAccounts"`
	ErrorText   string `json:"errorText"`
	MessageText string `json:"messageText"`

	CountText         string                  `json:"countText"`
	IncomeAmounts     []AccountAmountDTO      `json:"incomeAmounts"`
	ExpenseAmounts    []AccountAmountDTO      `json:"expenseAmounts"`
	HasBalance        bool                    `json:"hasBalance"`
	BalanceText       string                  `json:"balanceText"`
	BalanceTone       string                  `json:"balanceTone"`
	ShowBack          bool                    `json:"showBack"`
	PeriodOptions     []string                `json:"periodOptions"`
	PeriodIndex       int                     `json:"periodIndex"`
	PeriodExtra       string                  `json:"periodExtra"`
	CategoryOptions   []string                `json:"categoryOptions"`
	CategoryIndex     int                     `json:"categoryIndex"`
	DirectionOptions  []string                `json:"directionOptions"`
	DirectionIndex    int                     `json:"directionIndex"`
	AmountLabel       string                  `json:"amountLabel"`
	AccountOptions    []string                `json:"accountOptions"`
	AccountIndex      int                     `json:"accountIndex"`
	PanelRows         []AccountPanelRowDTO    `json:"panelRows"`
	GroupChoices      []AccountGroupChoiceDTO `json:"groupChoices"`
	PanelFilterText   string                  `json:"panelFilterText"`
	PanelShownText    string                  `json:"panelShownText"`
	PendingHeader     string                  `json:"pendingHeader"`
	Rows              []TransactionRowDTO     `json:"rows"`
	HasTransactions   bool                    `json:"hasTransactions"`
	HasBalanceColumn  bool                    `json:"hasBalanceColumn"`
	AddCategoryNames  []string                `json:"addCategoryNames"`
	AddDate           string                  `json:"addDate"`
	AddAmountLabel    string                  `json:"addAmountLabel"`
	AddAccountOptions []string                `json:"addAccountOptions"`
	AddAccountIndex   int                     `json:"addAccountIndex"`

	HasChart         bool    `json:"hasChart"`
	ChartCollapsed   bool    `json:"chartCollapsed"`
	ChartLine        string  `json:"chartLine"`
	ChartArea        string  `json:"chartArea"`
	ChartAxisTop     string  `json:"chartAxisTop"`
	ChartAxisBottom  string  `json:"chartAxisBottom"`
	ChartAxisStart   string  `json:"chartAxisStart"`
	ChartAxisMid     string  `json:"chartAxisMid"`
	ChartAxisEnd     string  `json:"chartAxisEnd"`
	ChartValue       string  `json:"chartValue"`
	ChartNotFromZero bool    `json:"chartNotFromZero"`
	ChartEndX        float64 `json:"chartEndX"`
	ChartEndY        float64 `json:"chartEndY"`
	ChartEndLabel    string  `json:"chartEndLabel"`

	SelectionVisible    bool     `json:"selectionVisible"`
	AllSelected         bool     `json:"allSelected"`
	SelectionCountText  string   `json:"selectionCountText"`
	SelectionTotalsText string   `json:"selectionTotalsText"`
	CategoryNames       []string `json:"categoryNames"`
	TaxOptions          []string `json:"taxOptions"`

	DetailOpen          bool   `json:"detailOpen"`
	DetailCanCreateRule bool   `json:"detailCanCreateRule"`
	DetailTitle         string `json:"detailTitle"`
	DetailAmount        string `json:"detailAmount"`
	DetailExpense       bool   `json:"detailExpense"`
	DetailDate          string `json:"detailDate"`
	DetailAccount       string `json:"detailAccount"`
	DetailPartner       string `json:"detailPartner"`
	DetailShop          string `json:"detailShop"`
	DetailPurpose       string `json:"detailPurpose"`
	DetailCategoryIndex int    `json:"detailCategoryIndex"`
	DetailTaxRelevant   bool   `json:"detailTaxRelevant"`
	DetailTaxState      string `json:"detailTaxState"`
	DetailTaxIndex      int    `json:"detailTaxIndex"`
	DetailHasMismatch   bool   `json:"detailHasMismatch"`
	DetailMismatch      string `json:"detailMismatch"`
	DetailPartnerLink   string `json:"detailPartnerLink"`

	DuplicateOpen  bool   `json:"duplicateOpen"`
	DuplicateTitle string `json:"duplicateTitle"`
	DuplicateText  string `json:"duplicateText"`

	CanUndoReapply        bool                 `json:"canUndoReapply"`
	ReapplyOpen           bool                 `json:"reapplyOpen"`
	ReapplySummary        string               `json:"reapplySummary"`
	ReapplyWarning        string               `json:"reapplyWarning"`
	ReapplyGroups         []ReapplyGroupRowDTO `json:"reapplyGroups"`
	ReapplyConfirmText    string               `json:"reapplyConfirmText"`
	ReapplyConfirmEnabled bool                 `json:"reapplyConfirmEnabled"`

	RangeOpen  bool   `json:"rangeOpen"`
	RangeFrom  string `json:"rangeFrom"`
	RangeTo    string `json:"rangeTo"`
	RangeError string `json:"rangeError"`

	AmountOpen     bool   `json:"amountOpen"`
	AmountMin      string `json:"amountMin"`
	AmountMax      string `json:"amountMax"`
	AmountMinLabel string `json:"amountMinLabel"`
	AmountMaxLabel string `json:"amountMaxLabel"`
	AmountError    string `json:"amountError"`

	AddOpen  bool   `json:"addOpen"`
	AddError string `json:"addError"`

	RuleOpen          bool     `json:"ruleOpen"`
	RulePattern       string   `json:"rulePattern"`
	RuleCategories    []string `json:"ruleCategories"`
	RuleCategoryIndex int      `json:"ruleCategoryIndex"`
	RuleError         string   `json:"ruleError"`

	DeleteOpen  bool   `json:"deleteOpen"`
	DeleteID    int    `json:"deleteId"`
	DeleteTitle string `json:"deleteTitle"`
	DeleteText  string `json:"deleteText"`

	MultiDeleteOpen   bool     `json:"multiDeleteOpen"`
	MultiDeleteTitle  string   `json:"multiDeleteTitle"`
	MultiDeleteRange  string   `json:"multiDeleteRange"`
	MultiDeleteTotals string   `json:"multiDeleteTotals"`
	MultiDeleteLines  []string `json:"multiDeleteLines"`
	MultiDeleteMore   string   `json:"multiDeleteMore"`

	ImportOpen           bool                `json:"importOpen"`
	ImportTitle          string              `json:"importTitle"`
	ImportSummary        string              `json:"importSummary"`
	ImportFiles          []ImportFileRowDTO  `json:"importFiles"`
	ImportCSVFields      []ImportCSVFieldDTO `json:"importCsvFields"`
	ImportCSVHint        string              `json:"importCsvHint"`
	ImportAccountOptions []string            `json:"importAccountOptions"`
	ImportAccountIndex   int                 `json:"importAccountIndex"`
	ImportBalanceAllowed bool                `json:"importBalanceAllowed"`
	ImportUpdateBalance  bool                `json:"importUpdateBalance"`
	ImportBalanceText    string              `json:"importBalanceText"`
	ImportNewerText      string              `json:"importNewerText"`
	Importing            bool                `json:"importing"`
	ImportConfirmText    string              `json:"importConfirmText"`
	ImportConfirmEnabled bool                `json:"importConfirmEnabled"`
}

// TransactionsOptions is the process wiring the caller supplies.
type TransactionsOptions struct {
	// Emit publishes transactions:changed; nil uses the running Wails
	// application.
	Emit func(TransactionsDTO)
}

// Transactions is the Transactions page's Wails service.
type Transactions struct {
	session *shell.Session
	emit    func(TransactionsDTO)
}

// NewTransactions builds the Transactions service. session may be nil only for
// the self-check, which inspects the registry without calling the service.
func NewTransactions(session *shell.Session, options TransactionsOptions) *Transactions {
	return &Transactions{session: session, emit: options.Emit}
}

// ServiceName implements application.ServiceName.
func (t *Transactions) ServiceName() string {
	return "Transactions"
}

// ServiceShutdown is a no-op: the router owns the one session listener.
func (t *Transactions) ServiceShutdown() error {
	return nil
}

// publishPage emits the current Transactions state.
func (t *Transactions) publishPage() {
	t.emitDTO(t.snapshot())
}

// State returns the current Transactions state without changing anything.
func (t *Transactions) State() (TransactionsDTO, error) {
	if t.session == nil {
		return TransactionsDTO{}, nil
	}
	return t.snapshot(), nil
}

// Load loads the page with no navigation filter.
func (t *Transactions) Load() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpen(transactionsview.StartArgs{}) })
}

// OpenWithAdd opens the page and then the add-transaction dialog, as the File
// menu does.
func (t *Transactions) OpenWithAdd() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenWithAdd() })
}

// OpenWithImport opens the page and then the statement-import dialog, as the
// File menu does.
func (t *Transactions) OpenWithImport() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenWithImport() })
}

// OpenImport asks for statement files and opens the import preview.
func (t *Transactions) OpenImport() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenImport() })
}

// ImportAccountChanged picks the account for the open preview.
func (t *Transactions) ImportAccountChanged(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsImportAccountChanged(index) })
}

// ImportCSVChanged applies one CSV mapping change from the dialog.
func (t *Transactions) ImportCSVChanged(field string, value string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsImportCSVChanged(field, value) })
}

// ConfirmImport imports the previewed entries.
func (t *Transactions) ConfirmImport(updateBalance bool) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsConfirmImport(updateBalance) })
}

// CancelImport closes the preview without importing.
func (t *Transactions) CancelImport() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCancelImport() })
}

// ExportCSV asks where to write the export and writes exactly what is shown.
func (t *Transactions) ExportCSV() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsExportCSV() })
}

// AttachReceipt asks for receipt files and attaches them to the selected
// booking.
func (t *Transactions) AttachReceipt() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsAttachReceipt() })
}

// Search applies the latest search text, deferring while another job runs.
func (t *Transactions) Search(query string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsSearch(query) })
}

// PeriodChanged applies one period row.
func (t *Transactions) PeriodChanged(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPeriodChanged(index) })
}

// OpenRange opens the custom-range dialog.
func (t *Transactions) OpenRange() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenRange() })
}

// ApplyRange applies the custom range.
func (t *Transactions) ApplyRange(from string, to string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsApplyRange(from, to) })
}

// CloseRange closes the custom-range dialog.
func (t *Transactions) CloseRange() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCloseRange() })
}

// CategoryChanged applies one category-filter row.
func (t *Transactions) CategoryChanged(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCategoryChanged(index) })
}

// DirectionChanged applies one direction row.
func (t *Transactions) DirectionChanged(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDirectionChanged(index) })
}

// AccountChanged selects the account choice at index.
func (t *Transactions) AccountChanged(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsAccountChanged(index) })
}

// OpenAmount opens the amount-filter dialog.
func (t *Transactions) OpenAmount() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenAmount() })
}

// ApplyAmount applies the amount bounds.
func (t *Transactions) ApplyAmount(minimum string, maximum string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsApplyAmount(minimum, maximum) })
}

// ResetAmount clears the amount filter.
func (t *Transactions) ResetAmount() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsResetAmount() })
}

// CloseAmount closes the amount-filter dialog.
func (t *Transactions) CloseAmount() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCloseAmount() })
}

// ToggleChart folds or unfolds the balance chart.
func (t *Transactions) ToggleChart() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsToggleChart() })
}

// AutoCategorize applies the auto-apply rules.
func (t *Transactions) AutoCategorize() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsAutoCategorize() })
}

// ReapplyRules opens the re-apply preview.
func (t *Transactions) ReapplyRules() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsReapplyRules() })
}

// ToggleReapplyGroup ticks or unticks one preview group.
func (t *Transactions) ToggleReapplyGroup(from int, to int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsToggleReapplyGroup(from, to) })
}

// ConfirmReapplyRules applies the preview's ticked groups.
func (t *Transactions) ConfirmReapplyRules() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsConfirmReapplyRules() })
}

// CancelReapplyRules closes the preview.
func (t *Transactions) CancelReapplyRules() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCancelReapplyRules() })
}

// UndoReapply restores the last re-apply.
func (t *Transactions) UndoReapply() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsUndoReapply() })
}

// OpenRule opens the create-rule dialog.
func (t *Transactions) OpenRule() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenRule() })
}

// CloseRule closes the create-rule dialog.
func (t *Transactions) CloseRule() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCloseRule() })
}

// SaveRule creates a rule from the dialog.
func (t *Transactions) SaveRule(pattern string, categoryIndex int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsSaveRule(pattern, categoryIndex) })
}

// Back clears the page's search.
func (t *Transactions) Back() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsBack() })
}

// OpenDetail opens the detail panel for one booking.
func (t *Transactions) OpenDetail(id int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenDetail(model.TransactionID(id)) })
}

// ToggleSelect toggles one booking in the multi-selection.
func (t *Transactions) ToggleSelect(id int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsToggleSelect(model.TransactionID(id)) })
}

// SelectRange selects the range from the anchor to id.
func (t *Transactions) SelectRange(id int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsSelectRange(model.TransactionID(id)) })
}

// SelectAll selects every shown booking.
func (t *Transactions) SelectAll() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsSelectAll() })
}

// ClearSelection empties the multi-selection.
func (t *Transactions) ClearSelection() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsClearSelection() })
}

// ShowPartner opens the list for one partner.
func (t *Transactions) ShowPartner(partner string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsShowPartner(partner) })
}

// OpenDelete opens the delete confirmation for one booking.
func (t *Transactions) OpenDelete(id int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenDelete(model.TransactionID(id)) })
}

// ConfirmDelete deletes one booking.
func (t *Transactions) ConfirmDelete(id int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsConfirmDelete(model.TransactionID(id)) })
}

// CancelDelete closes the delete confirmation.
func (t *Transactions) CancelDelete() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCancelDelete() })
}

// OpenMultiDelete opens the multi-delete confirmation.
func (t *Transactions) OpenMultiDelete() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenMultiDelete() })
}

// ConfirmMultiDelete deletes the selected bookings.
func (t *Transactions) ConfirmMultiDelete() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsConfirmMultiDelete() })
}

// CancelMultiDelete closes the multi-delete confirmation.
func (t *Transactions) CancelMultiDelete() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCancelMultiDelete() })
}

// BatchCategory sets the selected bookings' category.
func (t *Transactions) BatchCategory(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsBatchCategory(index) })
}

// BatchTax sets the selected bookings' tax override.
func (t *Transactions) BatchTax(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsBatchTax(index) })
}

// OpenAdd opens the add-transaction dialog.
func (t *Transactions) OpenAdd() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsOpenAdd() })
}

// AddSubmit adds a manual booking.
func (t *Transactions) AddSubmit(
	accountIndex int,
	expense bool,
	date string,
	amount string,
	partner string,
	purpose string,
	categoryIndex int,
) (TransactionsDTO, error) {
	return t.action(func() {
		t.session.TransactionsAddSubmit(accountIndex, expense, date, amount, partner, purpose, categoryIndex)
	})
}

// CloseAdd closes the add-transaction dialog.
func (t *Transactions) CloseAdd() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCloseAdd() })
}

// ConfirmDuplicate adds the pending identical booking.
func (t *Transactions) ConfirmDuplicate() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsConfirmDuplicate() })
}

// DismissDuplicate closes the duplicate confirmation.
func (t *Transactions) DismissDuplicate() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDismissDuplicate() })
}

// CloseDetail closes the detail panel.
func (t *Transactions) CloseDetail() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsCloseDetail() })
}

// DetailClose closes the detail panel from Rust's detail-close callback.
func (t *Transactions) DetailClose() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDetailClose() })
}

// DetailCreateRule opens the rule dialog from the detail panel.
func (t *Transactions) DetailCreateRule() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDetailCreateRule() })
}

// DetailCategory changes the selected booking's category.
func (t *Transactions) DetailCategory(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDetailCategory(index) })
}

// DetailTax changes the selected booking's tax override.
func (t *Transactions) DetailTax(index int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDetailTax(index) })
}

// DetailShowPartner opens the list for the selected partner.
func (t *Transactions) DetailShowPartner(partner string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDetailShowPartner(partner) })
}

// DismissMessage clears the error and success banner.
func (t *Transactions) DismissMessage() (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsDismissMessage() })
}

// PanelSelect selects an account, group or all accounts.
func (t *Transactions) PanelSelect(kind string, groupKind string, id int, accountType string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelSelect(kind, groupKind, id, accountType) })
}

// PanelToggle collapses or expands one panel group.
func (t *Transactions) PanelToggle(kind string, groupKind string, id int, accountType string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelToggle(kind, groupKind, id, accountType) })
}

// PanelMarkRead marks the bookings behind one panel row as read.
func (t *Transactions) PanelMarkRead(kind string, groupKind string, id int, accountType string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelMarkRead(kind, groupKind, id, accountType) })
}

// PanelNewGroup creates a custom group.
func (t *Transactions) PanelNewGroup(name string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelNewGroup(name) })
}

// PanelRenameGroup renames a custom group.
func (t *Transactions) PanelRenameGroup(id int, name string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelRenameGroup(id, name) })
}

// PanelDeleteGroup deletes a custom group.
func (t *Transactions) PanelDeleteGroup(id int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelDeleteGroup(id) })
}

// PanelMoveGroup moves a custom group up or down.
func (t *Transactions) PanelMoveGroup(id int, up bool) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelMoveGroup(id, up) })
}

// PanelMoveAccount moves an account into a group, or to its type group when
// groupID is negative.
func (t *Transactions) PanelMoveAccount(accountID int, groupID int) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelMoveAccount(accountID, groupID) })
}

// PanelFilterChanged stores the panel's free-text filter.
func (t *Transactions) PanelFilterChanged(text string) (TransactionsDTO, error) {
	return t.action(func() { t.session.TransactionsPanelFilterChanged(text) })
}

func (t *Transactions) action(run func()) (TransactionsDTO, error) {
	if t.session == nil {
		return TransactionsDTO{}, errTransactionsNotStarted
	}
	run()
	return t.snapshot(), nil
}

func (t *Transactions) emitDTO(dto TransactionsDTO) {
	if t.emit != nil {
		t.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(TransactionsEvent, dto)
	}
}

func (t *Transactions) snapshot() TransactionsDTO {
	if t.session == nil {
		return TransactionsDTO{}
	}
	return transactionsDTO(
		t.session.TransactionsState(),
		t.session.TransactionsDialog(),
		t.session.TransactionsReapplyExcludedKeys(),
		t.session.TransactionsToday(),
	)
}

func transactionsDTO(
	state transactionsview.TransactionsState,
	dialog shell.TransactionsDialogState,
	excludedKeys []shell.TransactionsReapplyKey,
	today civil.Date,
) TransactionsDTO {
	language := state.Language
	dto := TransactionsDTO{
		Loading:             state.IsLoading,
		HasAccounts:         len(state.Accounts) > 0,
		CountText:           fmt.Sprintf("%d", len(state.Transactions)),
		IncomeAmounts:       transactionsAmountRows(state.TotalIncome, "positive", language),
		ExpenseAmounts:      transactionsAmountRows(state.TotalExpenses, "negative", language),
		HasBalance:          state.CurrentBalance != nil,
		ShowBack:            transactionsShowBack(state),
		PeriodOptions:       transactionsPeriodOptions(language),
		PeriodIndex:         transactionsPeriodIndex(state.Period),
		PeriodExtra:         transactionsPeriodExtra(state, language),
		CategoryOptions:     transactionsCategoryFilterOptions(state, language),
		CategoryIndex:       transactionsCategoryFilterIndex(state),
		DirectionOptions:    transactionsDirectionOptions(language),
		DirectionIndex:      transactionsDirectionIndex(state.Direction),
		AmountLabel:         transactionsAmountLabel(state, language),
		PanelFilterText:     state.AccountFilterText,
		PanelShownText:      transactionsPanelShownText(state, language),
		PendingHeader:       transactionsPendingHeader(state, language),
		Rows:                transactionsTransactionRows(state, language),
		HasTransactions:     len(state.Transactions) > 0,
		HasBalanceColumn:    state.Balances != nil,
		AddCategoryNames:    transactionsAddCategoryNames(state, language),
		AddDate:             format.Date(today),
		AddAmountLabel:      i18n.Format(language, i18n.AmountIn, i18n.Str(format.CurrencySymbol(state.PrimaryCurrency))),
		ChartCollapsed:      state.ChartCollapsed,
		TaxOptions:          transactionsTaxOptions(language),
		CategoryNames:       transactionsCategoryNames(state, language),
		DetailCanCreateRule: len(state.Categories) > 0,
		RangeOpen:           dialog.RangeOpen,
		RangeFrom:           dialog.RangeFrom,
		RangeTo:             dialog.RangeTo,
		RangeError:          dialog.RangeError,
		AmountOpen:          dialog.AmountOpen,
		AmountMin:           dialog.AmountMin,
		AmountMax:           dialog.AmountMax,
		AmountMinLabel:      dialog.AmountMinLabel,
		AmountMaxLabel:      dialog.AmountMaxLabel,
		AmountError:         dialog.AmountError,
		AddOpen:             dialog.AddOpen,
		AddError:            dialog.AddError,
		RuleOpen:            dialog.RuleOpen,
		RulePattern:         dialog.RulePattern,
		RuleCategories:      transactionsCategoryList(state),
		RuleCategoryIndex:   dialog.RuleCategoryIndex,
		RuleError:           dialog.RuleError,
		DeleteOpen:          dialog.DeleteOpen,
		DeleteID:            dialog.DeleteID,
		DeleteTitle:         dialog.DeleteTitle,
		DeleteText:          dialog.DeleteText,
		MultiDeleteOpen:     dialog.MultiDeleteOpen,
		MultiDeleteTitle:    dialog.MultiDeleteTitle,
		MultiDeleteRange:    dialog.MultiDeleteRange,
		MultiDeleteTotals:   dialog.MultiDeleteTotals,
		MultiDeleteLines:    append([]string(nil), dialog.MultiDeleteLines...),
		MultiDeleteMore:     dialog.MultiDeleteMore,
	}
	if state.Error != nil {
		dto.ErrorText = *state.Error
	}
	if state.Message != nil {
		dto.MessageText = *state.Message
	}
	if state.CurrentBalance != nil {
		dto.BalanceText = format.Money(*state.CurrentBalance, language, false)
		dto.BalanceTone = accountsToneCode(accountgrouping.BalanceToneValue(
			*state.CurrentBalance,
			state.CurrentBalanceIsLoan,
		))
	}

	allAccounts := transactionsGroupingAccounts(state.Accounts)
	dto.PanelRows = transactionsPanelRows(state, allAccounts, language)
	dto.GroupChoices = transactionsGroupChoices(state)
	dto.AccountOptions, dto.AccountIndex = transactionsAccountChoices(state, language)
	dto.AddAccountOptions, dto.AddAccountIndex = transactionsAddAccountChoices(state, language)

	transactionsApplyChart(&dto, state, language)
	transactionsApplySelection(&dto, state, language)
	transactionsApplyDetail(&dto, state, language)
	transactionsApplyImport(&dto, state, language)
	transactionsApplyReapply(&dto, state, language, excludedKeys)
	return dto
}

func transactionsApplyChart(dto *TransactionsDTO, state transactionsview.TransactionsState, language i18n.Language) {
	chart, ok := transactionsChartData(state.BalanceChart, state.CurrentBalance, language)
	if !ok {
		dto.HasChart = false
		return
	}
	dto.HasChart = true
	dto.ChartCollapsed = state.ChartCollapsed
	dto.ChartLine = chart.Line
	dto.ChartArea = chart.Area
	dto.ChartAxisTop = chart.AxisTop
	dto.ChartAxisBottom = chart.AxisBottom
	dto.ChartAxisStart = chart.AxisStart
	dto.ChartAxisMid = chart.AxisMid
	dto.ChartAxisEnd = chart.AxisEnd
	dto.ChartValue = chart.Value
	dto.ChartNotFromZero = chart.NotFromZero
	dto.ChartEndX = chart.EndX
	dto.ChartEndY = chart.EndY
	dto.ChartEndLabel = chart.EndLabel
}

func transactionsApplySelection(dto *TransactionsDTO, state transactionsview.TransactionsState, language i18n.Language) {
	selected := state.SelectedShown()
	dto.SelectionVisible = len(state.SelectedIDs) > 0
	dto.AllSelected = len(state.Transactions) > 0 && len(selected) == len(state.Transactions)
	dto.SelectionCountText = i18n.Format(language, i18n.SelectedCount, i18n.Int(int64(len(state.SelectedIDs))))
	totals := make([]string, 0)
	for _, total := range state.SelectedTotals() {
		totals = append(totals, format.Money(money.Money{Minor: total.Total, Currency: total.Currency}, language, true))
	}
	dto.SelectionTotalsText = strings.Join(totals, " · ")
}

func transactionsApplyDetail(dto *TransactionsDTO, state transactionsview.TransactionsState, language i18n.Language) {
	dto.DetailCategoryIndex = 0
	dto.DetailTaxIndex = 0
	dto.DetailCanCreateRule = len(state.Categories) > 0
	transaction := state.Selected
	if transaction == nil {
		dto.DetailOpen = false
		dto.DuplicateOpen = state.PendingDuplicate != nil
		transactionsApplyDuplicate(dto, state, language)
		return
	}

	account := transactionsFindAccount(state.Accounts, transaction.AccountID)
	categoryIndex := 0
	if transaction.CategoryID != nil {
		for index := range state.Categories {
			if state.Categories[index].ID == *transaction.CategoryID {
				categoryIndex = index + 1
				break
			}
		}
	}
	taxIndex := 0
	if transaction.TaxOverride != nil {
		if *transaction.TaxOverride {
			taxIndex = 1
		} else {
			taxIndex = 2
		}
	}
	partnerLink := ""
	if transaction.PartnerName != nil {
		partnerLink = i18n.Format(language, i18n.ShowAllFromPartner, i18n.Str(*transaction.PartnerName))
	}
	mismatch := ""
	if state.Balances != nil {
		if entry, ok := state.Balances[transaction.ID]; ok && entry.BankDifference != nil {
			mismatch = transactionsBalanceMismatchText(language, entry.Balance, *entry.BankDifference)
		}
	}

	dto.DetailOpen = true
	dto.DetailTitle = banktext.DisplayDescription(transaction)
	dto.DetailAmount = format.Money(transaction.Amount, language, true)
	dto.DetailExpense = transaction.IsExpense()
	dto.DetailDate = format.Date(transaction.Date)
	if account != nil {
		dto.DetailAccount = accountsAccountLabel(*account, language)
	} else if transaction.AccountIBAN != nil {
		dto.DetailAccount = format.IBAN(*transaction.AccountIBAN)
	}
	if transaction.PartnerName != nil {
		dto.DetailPartner = *transaction.PartnerName
	}
	if merchant := rules.MerchantOf(transaction.Purpose, transaction.PartnerName); merchant != nil {
		dto.DetailShop = *merchant
	}
	if transaction.Purpose != nil {
		dto.DetailPurpose = *transaction.Purpose
	}
	dto.DetailCategoryIndex = categoryIndex
	dto.DetailTaxRelevant = transaction.IsTaxRelevant
	dto.DetailTaxState = transactionsTaxStateLine(transaction, language)
	dto.DetailTaxIndex = taxIndex
	dto.DetailHasMismatch = mismatch != ""
	dto.DetailMismatch = mismatch
	dto.DetailPartnerLink = partnerLink
	dto.DuplicateOpen = state.PendingDuplicate != nil
	transactionsApplyDuplicate(dto, state, language)
}

func transactionsApplyDuplicate(dto *TransactionsDTO, state transactionsview.TransactionsState, language i18n.Language) {
	entry := state.PendingDuplicate
	if entry == nil {
		return
	}
	dto.DuplicateTitle = i18n.Text(language, i18n.DuplicateTransactionTitle)
	dto.DuplicateText = i18n.Format(
		language,
		i18n.DuplicateTransactionText,
		i18n.Str(format.Date(entry.Date)),
		i18n.Str(format.Money(entry.Amount, language, false)),
	)
}

func transactionsApplyReapply(
	dto *TransactionsDTO,
	state transactionsview.TransactionsState,
	language i18n.Language,
	excludedKeys []shell.TransactionsReapplyKey,
) {
	dto.CanUndoReapply = state.CanUndoReapply
	preview := state.ReapplyPreview
	if preview == nil {
		dto.ReapplyOpen = false
		return
	}
	excluded := transactionsExcludedSet(excludedKeys)
	dto.ReapplyOpen = true
	dto.ReapplyGroups = transactionsReapplyGroupRows(preview, excluded, language)
	ticked := 0
	for _, group := range preview.Groups {
		if _, skip := excluded[reapplyKeyValueOf(group.FromCategoryID, group.ToCategoryID)]; !skip {
			ticked += group.Count
		}
	}
	dto.ReapplySummary = i18n.Format(
		language,
		i18n.ReapplyRulesSummary,
		i18n.Int(int64(len(preview.Changes))),
		i18n.Int(int64(preview.ShownCount)),
	)
	dto.ReapplyWarning = i18n.Text(language, i18n.ReapplyRulesWarning)
	dto.ReapplyConfirmText = i18n.Format(language, i18n.ReapplyRulesConfirm, i18n.Int(int64(ticked)))
	dto.ReapplyConfirmEnabled = ticked > 0
}

func transactionsExcludedSet(keys []shell.TransactionsReapplyKey) map[transactionsReapplyKeyValue]struct{} {
	excluded := make(map[transactionsReapplyKeyValue]struct{}, len(keys))
	for _, key := range keys {
		excluded[reapplyKeyValueOf(key.FromCategoryID, key.ToCategoryID)] = struct{}{}
	}
	return excluded
}

type transactionsReapplyKeyValue struct {
	hasFrom bool
	from    model.CategoryID
	to      model.CategoryID
}

func reapplyKeyValueOf(from *model.CategoryID, to model.CategoryID) transactionsReapplyKeyValue {
	key := transactionsReapplyKeyValue{to: to}
	if from != nil {
		key.from = *from
		key.hasFrom = true
	}
	return key
}

func transactionsReapplyGroupRows(
	preview *transactionsview.ReapplyPreview,
	excluded map[transactionsReapplyKeyValue]struct{},
	language i18n.Language,
) []ReapplyGroupRowDTO {
	rows := make([]ReapplyGroupRowDTO, 0, len(preview.Groups))
	for _, group := range preview.Groups {
		fromID := -1
		if group.FromCategoryID != nil {
			fromID = transactionsID(int64(*group.FromCategoryID))
		}
		detail := fmt.Sprintf("%d", group.Count)
		if group.Sum != nil {
			detail += " · " + format.Money(*group.Sum, language, true)
		}
		_, skip := excluded[reapplyKeyValueOf(group.FromCategoryID, group.ToCategoryID)]
		rows = append(rows, ReapplyGroupRowDTO{
			FromID: fromID,
			ToID:   transactionsID(int64(group.ToCategoryID)),
			Title:  group.FromName + " → " + group.ToName,
			Detail: detail,
			Ticked: !skip,
		})
	}
	return rows
}

func transactionsTransactionRows(state transactionsview.TransactionsState, language i18n.Language) []TransactionRowDTO {
	rows := make([]TransactionRowDTO, 0, len(state.PendingBookings)+len(state.Transactions))
	for _, line := range state.PendingBookings {
		rows = append(rows, transactionsPendingRow(line, language))
	}
	for _, transaction := range state.Transactions {
		rows = append(rows, transactionsBookingRow(state, transaction, language))
	}
	return rows
}

func transactionsBookingRow(
	state transactionsview.TransactionsState,
	transaction model.TransactionItem,
	language i18n.Language,
) TransactionRowDTO {
	entry, hasBalance := balance.Entry{}, false
	if state.Balances != nil {
		entry, hasBalance = state.Balances[transaction.ID]
	}
	balanceText := ""
	balanceTone := "zero"
	mismatch := ""
	if hasBalance {
		balanceText = format.Money(entry.Balance, language, false)
		balanceTone = accountsToneCode(accountgrouping.BalanceToneValue(entry.Balance, state.CurrentBalanceIsLoan))
		if entry.BankDifference != nil {
			mismatch = transactionsBalanceMismatchText(language, entry.Balance, *entry.BankDifference)
		}
	}
	partner := ""
	if transaction.PartnerName != nil {
		partner = strings.TrimSpace(*transaction.PartnerName)
	}
	partnerLink := ""
	if partner != "" {
		partnerLink = i18n.Format(language, i18n.ShowAllFromPartner, i18n.Str(partner))
	}
	categoryText := i18n.Text(language, i18n.Uncategorized)
	if transaction.CategoryName != nil {
		categoryText = *transaction.CategoryName
	}
	_, bold := state.BoldIDs[transaction.ID]
	_, selected := state.SelectedIDs[transaction.ID]
	return TransactionRowDTO{
		Pending:         false,
		ID:              transactionsID(int64(transaction.ID)),
		DateText:        format.Date(transaction.Date),
		Title:           banktext.DisplayDescription(&transaction),
		Subtitle:        transactionsOptionalText(banktext.SecondaryLine(&transaction)),
		CategoryText:    categoryText,
		CategoryColour:  transactionsColour(transaction.CategoryColor),
		Partner:         partner,
		PartnerLinkText: partnerLink,
		AmountText:      format.Money(transaction.Amount, language, true),
		Expense:         transaction.IsExpense(),
		IsNew:           bold,
		Selected:        selected,
		HasDocuments:    transaction.DocumentCount > 0,
		DocumentsText:   i18n.Format(language, i18n.DocumentsCount, i18n.Int(int64(transaction.DocumentCount))),
		BalanceText:     balanceText,
		BalanceTone:     balanceTone,
		BalanceWarning:  mismatch != "",
		BalanceMismatch: mismatch,
	}
}

func transactionsPendingRow(line model.PendingBookingItem, language i18n.Language) TransactionRowDTO {
	title := "—"
	if partner := banktext.DisplayPartner(line.PartnerName); partner != nil {
		title = *partner
	} else if purpose := banktext.DisplayPurpose(line.Purpose); purpose != nil {
		title = *purpose
	}
	subtitle := ""
	if line.PartnerName != nil {
		subtitle = transactionsOptionalText(banktext.DisplayPurpose(line.Purpose))
	}
	return TransactionRowDTO{
		Pending:     true,
		ID:          -1,
		DateText:    format.Date(line.Date),
		Title:       title,
		Subtitle:    subtitle,
		AmountText:  format.Money(line.Amount, language, true),
		Expense:     line.Amount.Minor < 0,
		BalanceTone: "zero",
	}
}

func transactionsPendingHeader(state transactionsview.TransactionsState, language i18n.Language) string {
	if len(state.PendingBookings) == 0 {
		return ""
	}
	return i18n.Format(
		language,
		i18n.PendingHeader,
		i18n.Str(format.DateTime(state.PendingBookings[0].FetchedAt)),
	)
}

func transactionsPanelRows(
	state transactionsview.TransactionsState,
	allAccounts []accountgrouping.AccountItem,
	language i18n.Language,
) []AccountPanelRowDTO {
	filtering := strings.TrimSpace(state.AccountFilterText) != ""
	allNew := int64(0)
	for _, account := range state.Accounts {
		allNew += account.NewCount
	}
	rows := make([]AccountPanelRowDTO, 0, len(state.Groups)+len(state.Accounts)+1)
	rows = append(rows, AccountPanelRowDTO{
		Kind:     "all",
		ID:       -1,
		Label:    i18n.Text(language, i18n.AllAccounts),
		Selected: state.Selection.Kind == accountpanel.SelectionAll,
		NewCount: accountsNewCount(allNew),
		Totals:   accountsAmounts(state.GrandTotal, allAccounts, language, len(state.GrandTotal) > 1),
	})

	for groupIndex := range state.Groups {
		group := &state.Groups[groupIndex]
		matching := make([]accountgrouping.AccountItem, 0, len(group.Accounts))
		for _, grouped := range group.Accounts {
			account := transactionsFindAccount(state.Accounts, grouped.ID)
			if account == nil {
				continue
			}
			if !filtering || accountpanel.MatchesFilter(
				account.Name,
				accountsAccountIdentifier(*account),
				account.BankName,
				state.AccountFilterText,
			) {
				matching = append(matching, grouped)
			}
		}
		if filtering && len(matching) == 0 {
			continue
		}

		_, collapsed := state.Collapsed[group.Key.ID()]
		selected := state.Selection.Kind == accountpanel.SelectionGroup &&
			state.Selection.Group.ID() == group.Key.ID()

		groupKind := "type"
		accountType := ""
		id := -1
		name := ""
		if group.Key.Kind == accountgrouping.GroupKeyCustom {
			groupKind = "custom"
			id = transactionsID(int64(group.Key.GroupID))
			if group.Name != nil {
				name = *group.Name
			}
		} else {
			accountType = group.Key.AccountType
		}
		label := name
		if group.Name == nil {
			label = accountsAccountTypeLabel(accountType, language)
		}
		deleteText := ""
		if groupKind == "custom" {
			deleteText = i18n.Format(language, i18n.DeleteGroupText, i18n.Str(name))
		}
		badge := transactionsGroupBadge(state.Accounts, group)
		rows = append(rows, AccountPanelRowDTO{
			Kind:        "group",
			GroupKind:   groupKind,
			AccountType: accountType,
			ID:          id,
			Label:       label,
			Name:        name,
			DeleteText:  deleteText,
			Selected:    selected,
			Collapsed:   collapsed,
			NewCount:    accountsNewCount(group.NewCount),
			Totals:      accountsAmounts(group.Totals, group.Accounts, language, false),
			Logo:        badge.Logo,
			LogoLetters: badge.Letters,
			LogoColour:  badge.Colour,
		})
		if collapsed {
			continue
		}
		for _, grouped := range matching {
			account := transactionsFindAccount(state.Accounts, grouped.ID)
			if account == nil {
				continue
			}
			secondary := ""
			if account.Name != nil {
				if digits := accountsAccountDigits(*account); digits != "" {
					secondary = "…" + digits
				}
			}
			detail := ""
			if account.LastUpdate != nil {
				detail = i18n.Format(language, i18n.LastFetched, i18n.Str(format.Date(civil.FromTime(*account.LastUpdate))))
			}
			badge := accountsAccountBadge(*account, nil)
			rows = append(rows, AccountPanelRowDTO{
				Kind:        "account",
				AccountType: grouped.AccountType,
				ID:          transactionsID(int64(grouped.ID)),
				Label:       accountsAccountLabel(*account, language),
				Secondary:   secondary,
				Detail:      detail,
				Selected: state.Selection.Kind == accountpanel.SelectionAccount &&
					state.Selection.Account == grouped.ID,
				NewCount: accountsNewCount(grouped.NewCount),
				Totals: []AccountAmountDTO{{
					Text: format.Money(grouped.Balance, language, false),
					Tone: accountsToneCode(accountgrouping.BalanceToneFor(group.Accounts, grouped.Balance.Currency)),
				}},
				Indent:      true,
				Logo:        badge.Logo,
				LogoLetters: badge.Letters,
				LogoColour:  badge.Colour,
			})
		}
	}
	return rows
}

func transactionsGroupBadge(
	accounts []accountsview.AccountItem,
	group *accountgrouping.AccountGroupView,
) accountsBadgeFields {
	if group.Key.Kind == accountgrouping.GroupKeyCustom {
		return accountsBadgeFields{}
	}
	var first accountsBadgeFields
	found := false
	for _, grouped := range group.Accounts {
		account := transactionsFindAccount(accounts, grouped.ID)
		if account == nil {
			continue
		}
		badge := accountsAccountBadge(*account, nil)
		if found && badge != first {
			return accountsBadgeFields{}
		}
		if !found {
			first = badge
			found = true
		}
	}
	return first
}

func transactionsGroupChoices(state transactionsview.TransactionsState) []AccountGroupChoiceDTO {
	choices := make([]AccountGroupChoiceDTO, 0, len(state.CustomGroups))
	for _, group := range state.CustomGroups {
		choices = append(choices, AccountGroupChoiceDTO{
			ID:   accountsID(int64(group.ID)),
			Name: group.Name,
		})
	}
	return choices
}

func transactionsPanelShownText(state transactionsview.TransactionsState, language i18n.Language) string {
	if strings.TrimSpace(state.AccountFilterText) == "" {
		return ""
	}
	shown := 0
	for _, account := range state.Accounts {
		if accountpanel.MatchesFilter(
			account.Name,
			accountsAccountIdentifier(account),
			account.BankName,
			state.AccountFilterText,
		) {
			shown++
		}
	}
	return i18n.Format(language, i18n.AccountsShown, i18n.Int(int64(shown)), i18n.Int(int64(len(state.Accounts))))
}

func transactionsAccountChoices(
	state transactionsview.TransactionsState,
	language i18n.Language,
) ([]string, int) {
	choices := make([]accountpanel.AccountSelection, 0, 1+len(state.Groups)+len(state.Accounts))
	labels := make([]string, 0, cap(choices))
	choices = append(choices, accountpanel.AllSelection())
	labels = append(labels, i18n.Text(language, i18n.AllAccounts))
	for _, group := range state.Groups {
		label := ""
		if group.Name != nil {
			label = *group.Name
		} else if group.Key.Kind == accountgrouping.GroupKeyCustom {
			label = fmt.Sprintf("%d", group.Key.GroupID)
		} else {
			label = accountsAccountTypeLabel(group.Key.AccountType, language)
		}
		choices = append(choices, accountpanel.GroupSelection(group.Key))
		labels = append(labels, label)
	}
	for _, account := range state.Accounts {
		choices = append(choices, accountpanel.AccountSelectionFor(account.ID))
		labels = append(labels, accountsAccountLabel(account, language))
	}
	index := 0
	for choiceIndex, choice := range choices {
		if choice == state.Selection {
			index = choiceIndex
			break
		}
	}
	return labels, index
}

func transactionsAddAccountChoices(
	state transactionsview.TransactionsState,
	language i18n.Language,
) ([]string, int) {
	labels := make([]string, 0, len(state.Accounts))
	index := 0
	for position, account := range state.Accounts {
		labels = append(labels, accountsAccountLabel(account, language))
		if state.AccountID != nil && account.ID == *state.AccountID {
			index = position
		}
	}
	return labels, index
}

func transactionsAmountRows(
	totals []money.CurrencyTotal,
	tone string,
	language i18n.Language,
) []AccountAmountDTO {
	rows := make([]AccountAmountDTO, 0, len(totals))
	for _, total := range totals {
		rows = append(rows, AccountAmountDTO{
			Text: format.Money(money.Money{Minor: total.Total, Currency: total.Currency}, language, false),
			Tone: tone,
		})
	}
	return rows
}

func transactionsAmountLabel(state transactionsview.TransactionsState, language i18n.Language) string {
	switch {
	case state.MinAmount != nil && state.MaxAmount != nil:
		return format.Money(*state.MinAmount, language, false) + "–" + format.Money(*state.MaxAmount, language, false)
	case state.MinAmount != nil:
		return "≥ " + format.Money(*state.MinAmount, language, false)
	case state.MaxAmount != nil:
		return "≤ " + format.Money(*state.MaxAmount, language, false)
	default:
		return i18n.Text(language, i18n.AmountFilterAny)
	}
}

func transactionsPeriodOptions(language i18n.Language) []string {
	keys := []i18n.Key{
		i18n.PeriodThisMonth,
		i18n.PeriodLastMonth,
		i18n.PeriodLast3Months,
		i18n.PeriodThisYear,
		i18n.PeriodLastYear,
		i18n.PeriodAllTime,
	}
	options := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		options = append(options, i18n.Text(language, key))
	}
	options = append(options, i18n.Text(language, i18n.PeriodCustomRange))
	return options
}

func transactionsPeriodIndex(period transactionsview.Period) int {
	switch period {
	case transactionsview.PeriodThisMonth:
		return 0
	case transactionsview.PeriodLastMonth:
		return 1
	case transactionsview.PeriodLast3Months:
		return 2
	case transactionsview.PeriodThisYear:
		return 3
	case transactionsview.PeriodLastYear:
		return 4
	default:
		return 5
	}
}

func transactionsPeriodExtra(state transactionsview.TransactionsState, language i18n.Language) string {
	if state.Month != nil {
		name := transactionsMonthName(language, state.Month.Month)
		runes := []rune(name)
		if len(runes) > 0 {
			upper := strings.ToUpper(string(runes[0]))
			name = upper + string(runes[1:])
		}
		return fmt.Sprintf("%s %d", name, state.Month.Year)
	}
	if state.CustomRange != nil {
		return format.Date(state.CustomRange.From) + " – " + format.Date(state.CustomRange.To)
	}
	return ""
}

// transactionsMonthName returns the full month name without converting an
// unchecked int to uint8 (YearMonth only ever holds 1..12).
func transactionsMonthName(language i18n.Language, month int) string {
	switch month {
	case 1:
		return report.MonthName(language, 1)
	case 2:
		return report.MonthName(language, 2)
	case 3:
		return report.MonthName(language, 3)
	case 4:
		return report.MonthName(language, 4)
	case 5:
		return report.MonthName(language, 5)
	case 6:
		return report.MonthName(language, 6)
	case 7:
		return report.MonthName(language, 7)
	case 8:
		return report.MonthName(language, 8)
	case 9:
		return report.MonthName(language, 9)
	case 10:
		return report.MonthName(language, 10)
	case 11:
		return report.MonthName(language, 11)
	case 12:
		return report.MonthName(language, 12)
	default:
		return report.MonthName(language, 1)
	}
}

func transactionsDirectionOptions(language i18n.Language) []string {
	return []string{
		i18n.Text(language, i18n.DirectionAnyLabel),
		i18n.Text(language, i18n.DirectionIncome),
		i18n.Text(language, i18n.DirectionExpenses),
	}
}

func transactionsDirectionIndex(direction model.Direction) int {
	switch direction {
	case model.DirectionIncome:
		return 1
	case model.DirectionExpenses:
		return 2
	default:
		return 0
	}
}

func transactionsCategoryFilterOptions(state transactionsview.TransactionsState, language i18n.Language) []string {
	options := []string{
		i18n.Text(language, i18n.AllCategories),
		i18n.Text(language, i18n.Uncategorized),
	}
	for _, category := range state.Categories {
		options = append(options, category.Name)
	}
	return options
}

func transactionsCategoryFilterIndex(state transactionsview.TransactionsState) int {
	switch state.CategoryFilter.Kind {
	case transactionsview.CategoryFilterKindNone:
		return 1
	case transactionsview.CategoryFilterKindOnly:
		for index, category := range state.Categories {
			if category.ID == state.CategoryFilter.CategoryID {
				return index + 2
			}
		}
	}
	return 0
}

func transactionsCategoryNames(state transactionsview.TransactionsState, language i18n.Language) []string {
	names := []string{i18n.Text(language, i18n.Uncategorized)}
	for _, category := range state.Categories {
		if category.IsTaxRelevant {
			names = append(names, category.Name+" ("+i18n.Text(language, i18n.Tax)+")")
		} else {
			names = append(names, category.Name)
		}
	}
	return names
}

func transactionsAddCategoryNames(state transactionsview.TransactionsState, language i18n.Language) []string {
	names := []string{i18n.Text(language, i18n.AutoRules)}
	for _, category := range state.Categories {
		names = append(names, category.Name)
	}
	return names
}

func transactionsCategoryList(state transactionsview.TransactionsState) []string {
	names := make([]string, 0, len(state.Categories))
	for _, category := range state.Categories {
		names = append(names, category.Name)
	}
	return names
}

func transactionsTaxOptions(language i18n.Language) []string {
	return []string{
		i18n.Text(language, i18n.TaxAutomatic),
		i18n.Text(language, i18n.TaxRelevantChoice),
		i18n.Text(language, i18n.TaxNotRelevantChoice),
	}
}

func transactionsTaxStateLine(transaction *model.TransactionItem, language i18n.Language) string {
	base := i18n.Text(language, i18n.TaxNotRelevantChoice)
	if transaction.IsTaxRelevant {
		base = i18n.Text(language, i18n.TaxRelevantChoice)
	}
	switch transaction.TaxSource {
	case model.TaxSourceManual:
		return i18n.Format(language, i18n.TaxSetByYou, i18n.Str(base))
	case model.TaxSourceRule:
		pattern := ""
		if transaction.TaxRulePattern != nil {
			pattern = *transaction.TaxRulePattern
		}
		return i18n.Format(language, i18n.TaxFromRule, i18n.Str(base), i18n.Str(pattern))
	case model.TaxSourceCategory:
		category := i18n.Text(language, i18n.Uncategorized)
		if transaction.CategoryName != nil {
			category = *transaction.CategoryName
		}
		return i18n.Format(language, i18n.TaxFromCategory, i18n.Str(base), i18n.Str(category))
	default:
		return base
	}
}

func transactionsBalanceMismatchText(language i18n.Language, bank money.Money, difference money.Money) string {
	return i18n.Format(
		language,
		i18n.BalanceBankMismatch,
		i18n.Str(format.Money(bank, language, false)),
		i18n.Str(format.Money(money.Money{Minor: -difference.Minor, Currency: difference.Currency}, language, true)),
	)
}

func transactionsShowBack(state transactionsview.TransactionsState) bool {
	return strings.TrimSpace(state.SearchQuery) != "" &&
		state.AccountID == nil &&
		state.Period == transactionsview.PeriodAll &&
		state.Direction == model.DirectionAll &&
		state.CategoryFilter.Kind == transactionsview.CategoryFilterKindAny &&
		state.CustomRange == nil &&
		state.MinAmount == nil &&
		state.MaxAmount == nil
}

type transactionsChart struct {
	Line        string
	Area        string
	AxisTop     string
	AxisBottom  string
	AxisStart   string
	AxisMid     string
	AxisEnd     string
	Value       string
	NotFromZero bool
	EndX        float64
	EndY        float64
	EndLabel    string
}

func transactionsChartData(points []balance.BalancePoint, current *money.Money, language i18n.Language) (transactionsChart, bool) {
	if len(points) < 2 {
		return transactionsChart{}, false
	}
	low := points[0].Balance.Minor
	high := points[0].Balance.Minor
	for _, point := range points[1:] {
		if point.Balance.Minor < low {
			low = point.Balance.Minor
		}
		if point.Balance.Minor > high {
			high = point.Balance.Minor
		}
	}
	currency := points[0].Balance.Currency
	span := high - low
	var line strings.Builder
	endY := 0.0
	for index, point := range points {
		x := float64(index) / float64(len(points)-1) * transactionsChartWidth
		ratio := 0.5
		if span != 0 {
			ratio = float64(point.Balance.Minor-low) / float64(span)
		}
		y := transactionsChartPadding + (1-ratio)*(transactionsChartHeight-2*transactionsChartPadding)
		endY = y
		if index == 0 {
			fmt.Fprintf(&line, "M %.2f %.2f", x, y)
		} else {
			fmt.Fprintf(&line, " L %.2f %.2f", x, y)
		}
	}
	lineText := line.String()
	area := fmt.Sprintf("%s L %.2f %.2f L 0 %.2f Z", lineText, transactionsChartWidth, transactionsChartHeight, transactionsChartHeight)
	middle := len(points) / 2
	last := points[len(points)-1].Balance
	value := ""
	if current != nil {
		value = format.Money(*current, language, false)
	}
	return transactionsChart{
		Line:        lineText,
		Area:        area,
		AxisTop:     format.WholeMoney(money.Money{Minor: high, Currency: currency}, language),
		AxisBottom:  format.WholeMoney(money.Money{Minor: low, Currency: currency}, language),
		AxisStart:   format.Date(points[0].Date),
		AxisMid:     format.Date(points[middle].Date),
		AxisEnd:     format.Date(points[len(points)-1].Date),
		Value:       value,
		NotFromZero: low > 0 || high < 0,
		EndX:        transactionsChartWidth,
		EndY:        endY,
		EndLabel:    format.Money(last, language, false),
	}, true
}

func transactionsFindAccount(accounts []accountsview.AccountItem, id model.AccountID) *accountsview.AccountItem {
	for index := range accounts {
		if accounts[index].ID == id {
			return &accounts[index]
		}
	}
	return nil
}

func transactionsGroupingAccounts(accounts []accountsview.AccountItem) []accountgrouping.AccountItem {
	grouped := make([]accountgrouping.AccountItem, 0, len(accounts))
	for _, account := range accounts {
		grouped = append(grouped, accountsGroupingItem(account))
	}
	return grouped
}

func transactionsOptionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func transactionsColour(value *string) string {
	const fallback = "#9e9e9e"
	if value == nil {
		return fallback
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(*value), "#"))
	if len(text) != 6 {
		return fallback
	}
	if _, err := strconvParseHex(text); err != nil {
		return fallback
	}
	return "#" + strings.ToLower(text)
}

func strconvParseHex(text string) (uint64, error) {
	var value uint64
	for _, character := range text {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint64(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint64(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint64(character-'A') + 10
		default:
			return 0, errors.New("not hexadecimal")
		}
	}
	return value, nil
}

func transactionsID(id int64) int {
	if id < math.MinInt32 || id > math.MaxInt32 {
		return -1
	}
	return int(id)
}

// ImportFileRowDTO is one row of the import dialog's file list, including the
// selections that could not be read.
type ImportFileRowDTO struct {
	Name        string `json:"name"`
	CountText   string `json:"countText"`
	PeriodText  string `json:"periodText"`
	WarningText string `json:"warningText"`
	Failure     bool   `json:"failure"`
}

// ImportCSVFieldDTO is one row of the CSV mapping editor.
type ImportCSVFieldDTO struct {
	Field   string   `json:"field"`
	Label   string   `json:"label"`
	Options []string `json:"options"`
	Index   int      `json:"index"`
}

// transactionsApplyImport sets every value Rust's apply_import sets.
func transactionsApplyImport(
	dto *TransactionsDTO,
	state transactionsview.TransactionsState,
	language i18n.Language,
) {
	preview := state.ImportPreview
	if preview == nil {
		dto.ImportOpen = false
		return
	}

	title := ""
	if len(preview.Files) <= 1 {
		name := ""
		if len(preview.Files) > 0 {
			name = transactionsImportDisplayName(preview.Files[0])
		}
		title = i18n.Format(language, i18n.ImportFile, i18n.Str(name))
	} else {
		title = i18n.Format(language, i18n.ImportFiles, i18n.Int(int64(len(preview.Files))))
	}
	summary := ""
	if len(preview.Files) > 1 {
		summary = i18n.Format(language, i18n.BookingCount, i18n.Int(int64(preview.BookingCount())))
	}

	var selectedAccount *accountsview.AccountItem
	accountOptions := make([]string, 0, len(state.Accounts))
	for index := range state.Accounts {
		accountOptions = append(accountOptions, accountsAccountLabel(state.Accounts[index], language))
		if preview.AccountID != nil && state.Accounts[index].ID == *preview.AccountID {
			selectedAccount = &state.Accounts[index]
		}
	}
	accountIndex := 0
	if preview.AccountID != nil {
		for index := range state.Accounts {
			if state.Accounts[index].ID == *preview.AccountID {
				accountIndex = index
				break
			}
		}
	}

	newerText := ""
	if transactionsImportHasBalances(preview) && !preview.BalanceUpdateAllowed {
		newerText = i18n.Text(language, i18n.NewerBookingsUnchanged)
	}

	confirmText := i18n.Format(
		language,
		i18n.ImportCount,
		i18n.Int(int64(preview.BookingCount())),
	)
	if state.IsImporting {
		confirmText = i18n.Text(language, i18n.Importing)
	}

	dto.ImportOpen = true
	dto.ImportTitle = title
	dto.ImportSummary = summary
	dto.ImportFiles = transactionsImportFileRows(preview, language, selectedAccount)
	dto.ImportCSVFields = transactionsImportCSVFields(preview, language)
	dto.ImportCSVHint = transactionsImportCSVHint(preview, language)
	dto.ImportAccountOptions = accountOptions
	dto.ImportAccountIndex = accountIndex
	dto.ImportBalanceAllowed = preview.BalanceUpdateAllowed
	dto.ImportUpdateBalance = preview.BalanceUpdateAllowed
	dto.ImportBalanceText = i18n.Text(language, i18n.SetAccountBalance)
	dto.ImportNewerText = newerText
	dto.Importing = state.IsImporting
	dto.ImportConfirmText = confirmText
	dto.ImportConfirmEnabled = !state.IsImporting &&
		preview.AccountID != nil &&
		preview.BookingCount() > 0
}

// transactionsImportFileRows is import_file_rows: each readable file's period
// and warnings, then the unreadable selections.
func transactionsImportFileRows(
	preview *transactionsview.ImportPreview,
	language i18n.Language,
	selectedAccount *accountsview.AccountItem,
) []ImportFileRowDTO {
	rows := make([]ImportFileRowDTO, 0, len(preview.Entries)+len(preview.Failures))
	multi := len(preview.Files) > 1
	for _, entry := range preview.Entries {
		bookings := entry.Statement.Bookings
		var from, to *imports.ImportedBooking
		for index := range bookings {
			booking := &bookings[index]
			if from == nil || booking.Date.Before(from.Date) {
				from = booking
			}
			if to == nil || booking.Date.After(to.Date) {
				to = booking
			}
		}
		periodText := ""
		if from != nil && to != nil {
			if from.Date == to.Date {
				periodText = format.Date(from.Date)
			} else {
				periodText = format.Date(from.Date) + "–" + format.Date(to.Date)
			}
		}

		warnings := make([]string, 0, 3)
		if entry.Statement.PendingSkipped > 0 {
			warnings = append(warnings, i18n.Format(
				language,
				i18n.PendingLeftOut,
				i18n.Int(int64(entry.Statement.PendingSkipped)),
			))
		}
		if len(entry.Statement.Skipped) > 0 {
			warnings = append(warnings, i18n.Format(
				language,
				i18n.UnreadableCount,
				i18n.Int(int64(len(entry.Statement.Skipped))),
			))
		}
		if selectedAccount != nil && entry.Statement.AccountIBAN != nil &&
			iban.Normalize(selectedAccount.IBAN) != iban.Normalize(*entry.Statement.AccountIBAN) {
			if multi {
				warnings = append(warnings, i18n.Format(
					language,
					i18n.FileBelongsToNamed,
					i18n.Str(transactionsImportDisplayName(entry.File)),
					i18n.Str(format.IBAN(*entry.Statement.AccountIBAN)),
				))
			} else {
				warnings = append(warnings, i18n.Format(
					language,
					i18n.FileBelongsTo,
					i18n.Str(format.IBAN(*entry.Statement.AccountIBAN)),
				))
			}
		}

		rows = append(rows, ImportFileRowDTO{
			Name:        transactionsImportDisplayName(entry.File),
			CountText:   i18n.Format(language, i18n.BookingCount, i18n.Int(int64(len(bookings)))),
			PeriodText:  periodText,
			WarningText: strings.Join(warnings, " · "),
			Failure:     false,
		})
	}
	for _, failure := range preview.Failures {
		rows = append(rows, ImportFileRowDTO{
			Name:        failure.FileName,
			WarningText: failure.Message,
			Failure:     true,
		})
	}
	return rows
}

// transactionsImportCSVFields is import_csv_fields: the mapping editor's rows,
// empty when the preview has no CSV file.
func transactionsImportCSVFields(
	preview *transactionsview.ImportPreview,
	language i18n.Language,
) []ImportCSVFieldDTO {
	var csv *imports.CsvLayout
	for index := range preview.Entries {
		if preview.Entries[index].Statement.Csv != nil {
			csv = preview.Entries[index].Statement.Csv
			break
		}
	}
	if csv == nil {
		return []ImportCSVFieldDTO{}
	}
	options := shell.ImportCSVOptions(csv, language)
	fields := []struct {
		field string
		label i18n.Key
		index *int
	}{
		{"date", i18n.ColDate, csv.Mapping.Date},
		{"amount", i18n.ColAmount, csv.Mapping.Amount},
		{"debit", i18n.ColDebit, csv.Mapping.Debit},
		{"credit", i18n.ColCredit, csv.Mapping.Credit},
		{"partner", i18n.ColPartner, csv.Mapping.Partner},
		{"purpose", i18n.ColPurpose, csv.Mapping.Purpose},
		{"balance", i18n.ColBalance, csv.Mapping.Balance},
	}
	rows := make([]ImportCSVFieldDTO, 0, len(fields))
	for _, field := range fields {
		index := 0
		if field.index != nil {
			index = *field.index + 1
		}
		rows = append(rows, ImportCSVFieldDTO{
			Field:   field.field,
			Label:   i18n.Text(language, field.label),
			Options: append([]string(nil), options...),
			Index:   index,
		})
	}
	return rows
}

// transactionsImportCSVHint is import_csv_hint: how many selected CSV files the
// shown columns apply to.
func transactionsImportCSVHint(
	preview *transactionsview.ImportPreview,
	language i18n.Language,
) string {
	var csv *imports.CsvLayout
	for index := range preview.Entries {
		if preview.Entries[index].Statement.Csv != nil {
			csv = preview.Entries[index].Statement.Csv
			break
		}
	}
	if csv == nil {
		return ""
	}
	same := 0
	for index := range preview.Entries {
		other := preview.Entries[index].Statement.Csv
		if other != nil && equalStrings(other.Headers, csv.Headers) {
			same++
		}
	}
	if len(preview.Entries) > 1 {
		return i18n.Format(language, i18n.ColumnsApplyToSame, i18n.Int(int64(same)))
	}
	return ""
}

// transactionsImportHasBalances is import_has_balances: whether the newest file
// carries balances that could update the account.
func transactionsImportHasBalances(preview *transactionsview.ImportPreview) bool {
	if len(preview.Entries) == 0 {
		return false
	}
	newest := preview.Newest().Statement
	for _, booking := range newest.Bookings {
		if booking.BalanceAfter != nil {
			return true
		}
	}
	return newest.ClosingBalance != nil
}

func transactionsImportDisplayName(file string) string {
	base := filepath.Base(file)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return file
	}
	return base
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
