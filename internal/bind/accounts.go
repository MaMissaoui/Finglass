package bind

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	bankingcore "github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/accountids"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/bankdirectory"
	"github.com/MaMissaoui/Finglass/internal/core/banklogo"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// AccountsEvent is the name of the event emitted after every Accounts change.
const AccountsEvent = "accounts:changed"

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[AccountsDTO](AccountsEvent)
}

var errAccountsNotStarted = errors.New("accounts is not started")

// The currency codes the add-account dialog offers, as accounts.rs lists them.
var accountsCurrencyCodes = []string{"EUR", "USD", "GBP", "CHF", "JPY", "CAD"}

// The balance-history windows FinDesk offers.
var accountsDetailDays = []int64{30, 90, 365}

const (
	accountsChartWidth   float32 = 700
	accountsChartHeight  float32 = 200
	accountsChartPadding float32 = 6
)

// AccountAmountDTO is one amount line in the header or the panel.
type AccountAmountDTO struct {
	Text string `json:"text"`
	Tone string `json:"tone"`
}

// AccountsFigureDTO is a labelled group of figures.
type AccountsFigureDTO struct {
	Label     string             `json:"label"`
	Values    []AccountAmountDTO `json:"values"`
	Secondary bool               `json:"secondary"`
}

// BankAccountRowDTO is one account row inside a bank card.
type BankAccountRowDTO struct {
	AccountID   int    `json:"accountId"`
	Name        string `json:"name"`
	Subtitle    string `json:"subtitle"`
	BalanceText string `json:"balanceText"`
	Tone        string `json:"tone"`
	NewCount    int    `json:"newCount"`
	Logo        string `json:"logo"`
	LogoLetters string `json:"logoLetters"`
	LogoColour  int    `json:"logoColour"`
}

// AccountsBankCardDTO is one bank card, including the online-banking fields
// Rust's banking::bank_cards sets (sync state, menu, PIN).
type AccountsBankCardDTO struct {
	BankID            int                 `json:"bankId"`
	Name              string              `json:"name"`
	Subtitle          string              `json:"subtitle"`
	CentralBLZHint    string              `json:"centralBlzHint"`
	Offline           bool                `json:"offline"`
	Collapsed         bool                `json:"collapsed"`
	AccountCountText  string              `json:"accountCountText"`
	Totals            []AccountAmountDTO  `json:"totals"`
	Accounts          []BankAccountRowDTO `json:"accounts"`
	Syncing           bool                `json:"syncing"`
	SyncStep          string              `json:"syncStep"`
	SyncEnabled       bool                `json:"syncEnabled"`
	SyncTooltip       string              `json:"syncTooltip"`
	PinText           string              `json:"pinText"`
	CanTestConnection bool                `json:"canTestConnection"`
	Menu              []BankMenuItemDTO   `json:"menu"`
	Logo              string              `json:"logo"`
	LogoLetters       string              `json:"logoLetters"`
	LogoColour        int                 `json:"logoColour"`
}

// AccountPanelRowDTO is one row of the shared account panel.
type AccountPanelRowDTO struct {
	Kind        string             `json:"kind"`
	GroupKind   string             `json:"groupKind"`
	AccountType string             `json:"accountType"`
	ID          int                `json:"id"`
	Label       string             `json:"label"`
	Secondary   string             `json:"secondary"`
	Detail      string             `json:"detail"`
	Name        string             `json:"name"`
	DeleteText  string             `json:"deleteText"`
	Selected    bool               `json:"selected"`
	Collapsed   bool               `json:"collapsed"`
	NewCount    int                `json:"newCount"`
	Totals      []AccountAmountDTO `json:"totals"`
	Indent      bool               `json:"indent"`
	Logo        string             `json:"logo"`
	LogoLetters string             `json:"logoLetters"`
	LogoColour  int                `json:"logoColour"`
}

// AccountGroupChoiceDTO is one custom group a booking can move into.
type AccountGroupChoiceDTO struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AccountTypeChoiceDTO is one account type in the account forms.
type AccountTypeChoiceDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// AccountDaysChoiceDTO is one balance-history window.
type AccountDaysChoiceDTO struct {
	Days  int    `json:"days"`
	Label string `json:"label"`
}

// AccountDetailRowDTO is one recent booking in the detail view.
type AccountDetailRowDTO struct {
	DateText   string `json:"dateText"`
	Title      string `json:"title"`
	AmountText string `json:"amountText"`
	Expense    bool   `json:"expense"`
}

// AccountsDialogDTO is the Accounts page's dialog state.
type AccountsDialogDTO struct {
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Text         string `json:"text"`
	ErrorText    string `json:"errorText"`
	AccountLabel string `json:"accountLabel"`
	AccountName  string `json:"accountName"`
	Balance      string `json:"balance"`
	AccountType  string `json:"accountType"`
	Offline      bool   `json:"offline"`
	BankID       int    `json:"bankId"`
	AccountID    int    `json:"accountId"`

	ConnectionName      string `json:"connectionName"`
	ConnectionBLZ       string `json:"connectionBlz"`
	ConnectionUserID    string `json:"connectionUserId"`
	ConnectionServerURL string `json:"connectionServerUrl"`
	ConnectionHasPin    bool   `json:"connectionHasPin"`
	ConnectionPinText   string `json:"connectionPinText"`
	ConnectionSyncing   bool   `json:"connectionSyncing"`
	HistoryDate         string `json:"historyDate"`
	HistoryHasStart     bool   `json:"historyHasStart"`
}

// AccountsDTO is everything the Accounts page shows.
type AccountsDTO struct {
	Loading     bool   `json:"loading"`
	ShowContent bool   `json:"showContent"`
	HasContent  bool   `json:"hasContent"`
	ErrorText   string `json:"errorText"`
	MessageText string `json:"messageText"`

	Figures       []AccountsFigureDTO     `json:"figures"`
	BankCards     []AccountsBankCardDTO   `json:"bankCards"`
	PanelRows     []AccountPanelRowDTO    `json:"panelRows"`
	GroupChoices  []AccountGroupChoiceDTO `json:"groupChoices"`
	FilterText    string                  `json:"filterText"`
	ShownText     string                  `json:"shownText"`
	TypeChoices   []AccountTypeChoiceDTO  `json:"typeChoices"`
	CurrencyCodes []string                `json:"currencyCodes"`
	DaysChoices   []AccountDaysChoiceDTO  `json:"daysChoices"`
	DayLabels     []string                `json:"dayLabels"`

	Dialog AccountsDialogDTO `json:"dialog"`

	DetailOpen       bool                  `json:"detailOpen"`
	DetailTitle      string                `json:"detailTitle"`
	DetailLogo       string                `json:"detailLogo"`
	DetailLetters    string                `json:"detailLetters"`
	DetailColour     int                   `json:"detailColour"`
	DetailSubtitle   string                `json:"detailSubtitle"`
	DetailBalance    string                `json:"detailBalance"`
	DetailTone       string                `json:"detailTone"`
	DetailAccountID  int                   `json:"detailAccountId"`
	DetailDays       int                   `json:"detailDays"`
	DetailSummary    string                `json:"detailSummary"`
	DetailPath       string                `json:"detailPath"`
	DetailAxisTop    string                `json:"detailAxisTop"`
	DetailAxisBottom string                `json:"detailAxisBottom"`
	DetailEndX       float32               `json:"detailEndX"`
	DetailEndY       float32               `json:"detailEndY"`
	DetailEndLabel   string                `json:"detailEndLabel"`
	DetailRows       []AccountDetailRowDTO `json:"detailRows"`
}

// AccountsOptions is the process wiring the caller supplies.
type AccountsOptions struct {
	// Emit publishes accounts:changed; nil uses the running Wails application.
	Emit func(AccountsDTO)
}

// Accounts is the Accounts page's Wails service.
type Accounts struct {
	session *shell.Session
	emit    func(AccountsDTO)
}

// NewAccounts builds the Accounts service. session may be nil only for the
// self-check, which inspects the registry without calling the service.
func NewAccounts(session *shell.Session, options AccountsOptions) *Accounts {
	return &Accounts{session: session, emit: options.Emit}
}

// ServiceName implements application.ServiceName.
func (a *Accounts) ServiceName() string {
	return "Accounts"
}

// ServiceShutdown is a no-op: the router owns the one session listener.
func (a *Accounts) ServiceShutdown() error {
	return nil
}

// publishPage emits the current Accounts state.
func (a *Accounts) publishPage() {
	a.emitDTO(a.snapshot())
}

// State returns the current Accounts state without changing anything.
func (a *Accounts) State() (AccountsDTO, error) {
	if a.session == nil {
		return AccountsDTO{}, nil
	}
	return a.snapshot(), nil
}

// Load loads the Accounts page.
func (a *Accounts) Load() (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsLoad() })
}

// OpenAddBank opens the add-offline-bank dialog.
func (a *Accounts) OpenAddBank() (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenAddBank() })
}

// CloseDialog closes the current dialog.
func (a *Accounts) CloseDialog() (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsCloseDialog() })
}

// SubmitAddBank creates the offline bank.
func (a *Accounts) SubmitAddBank(name string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsSubmitAddBank(name) })
}

// OpenAddAccount opens the add-account dialog for one bank.
func (a *Accounts) OpenAddAccount(bankID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenAddAccount(bankID) })
}

// SubmitAddAccount creates the account.
func (a *Accounts) SubmitAddAccount(
	bankID int,
	name string,
	identifier string,
	accountType string,
	currency string,
	balance string,
) (AccountsDTO, error) {
	return a.action(func() {
		a.session.AccountsSubmitAddAccount(bankID, name, identifier, accountType, currency, balance)
	})
}

// OpenBalance opens the balance-and-type dialog for one account.
func (a *Accounts) OpenBalance(accountID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenBalance(accountID) })
}

// SubmitBalance stores the balance and/or type.
func (a *Accounts) SubmitBalance(accountID int, balance string, accountType string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsSubmitBalance(accountID, balance, accountType) })
}

// OpenRename opens the rename dialog for one account.
func (a *Accounts) OpenRename(accountID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenRename(accountID) })
}

// SubmitRename renames the account.
func (a *Accounts) SubmitRename(accountID int, name string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsSubmitRename(accountID, name) })
}

// OpenDeleteAccount opens the delete-account confirmation.
func (a *Accounts) OpenDeleteAccount(accountID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenDeleteAccount(accountID) })
}

// ConfirmDeleteAccount deletes the account.
func (a *Accounts) ConfirmDeleteAccount(accountID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsConfirmDeleteAccount(accountID) })
}

// OpenDeleteBank opens the delete-bank confirmation.
func (a *Accounts) OpenDeleteBank(bankID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenDeleteBank(bankID) })
}

// ConfirmDeleteBank deletes an offline bank.
func (a *Accounts) ConfirmDeleteBank(bankID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsConfirmDeleteBank(bankID) })
}

// OpenDetail opens the detail view with the default window.
func (a *Accounts) OpenDetail(accountID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenDetail(accountID) })
}

// SelectDays reopens the detail view for another number of days.
func (a *Accounts) SelectDays(accountID int, days int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsSelectDays(accountID, days) })
}

// CloseDetail closes the detail view.
func (a *Accounts) CloseDetail() (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsCloseDetail() })
}

// FilterChanged stores the panel's free-text filter.
func (a *Accounts) FilterChanged(text string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsFilterChanged(text) })
}

// DismissMessage clears the error and success banner.
func (a *Accounts) DismissMessage() (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsDismissMessage() })
}

// Select selects an account, group or all accounts.
func (a *Accounts) Select(kind string, groupKind string, id int, accountType string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsSelect(kind, groupKind, id, accountType) })
}

// ToggleGroup collapses or expands one panel group.
func (a *Accounts) ToggleGroup(kind string, groupKind string, id int, accountType string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsToggleGroup(kind, groupKind, id, accountType) })
}

// MarkRead marks the bookings behind one panel row as read.
func (a *Accounts) MarkRead(kind string, groupKind string, id int, accountType string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsMarkRead(kind, groupKind, id, accountType) })
}

// ToggleBank collapses or expands a bank's account rows.
func (a *Accounts) ToggleBank(bankID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsToggleBank(bankID) })
}

// ForgetDeleted forgets the deleted bookings of one account.
func (a *Accounts) ForgetDeleted(accountID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsForgetDeleted(accountID) })
}

// NewGroup creates a custom group.
func (a *Accounts) NewGroup(name string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsNewGroup(name) })
}

// RenameGroup renames a custom group.
func (a *Accounts) RenameGroup(id int, name string) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsRenameGroup(id, name) })
}

// DeleteGroup deletes a custom group.
func (a *Accounts) DeleteGroup(id int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsDeleteGroup(id) })
}

// MoveGroup moves a custom group up or down.
func (a *Accounts) MoveGroup(id int, up bool) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsMoveGroup(id, up) })
}

// MoveAccount moves an account into a custom group, or to its type group when
// groupID is negative.
func (a *Accounts) MoveAccount(accountID int, groupID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsMoveAccount(accountID, groupID) })
}

// OpenTransactions opens Transactions filtered to one account.
func (a *Accounts) OpenTransactions(accountID int) (AccountsDTO, error) {
	return a.action(func() { a.session.AccountsOpenTransactions(accountID) })
}

func (a *Accounts) action(run func()) (AccountsDTO, error) {
	if a.session == nil {
		return AccountsDTO{}, errAccountsNotStarted
	}
	run()
	return a.snapshot(), nil
}

func (a *Accounts) emitDTO(dto AccountsDTO) {
	if a.emit != nil {
		a.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(AccountsEvent, dto)
	}
}

func (a *Accounts) snapshot() AccountsDTO {
	if a.session == nil {
		return AccountsDTO{}
	}
	return accountsDTO(
		a.session.AccountsState(),
		a.session.AccountsDialog(),
		a.session.BankingState(),
	)
}

func accountsDTO(
	state accountsview.AccountsState,
	dialog shell.AccountsDialogState,
	bankingState bankingcore.BankingState,
) AccountsDTO {
	language := state.Language
	hasContent := len(state.BankAccesses) > 0 || len(state.Accounts) > 0
	dto := AccountsDTO{
		Loading:       state.IsLoading,
		ShowContent:   state.IsLoading || hasContent,
		HasContent:    hasContent,
		Figures:       accountsFigures(state, language),
		BankCards:     accountsBankCards(state, bankingState, language),
		PanelRows:     accountsPanelRows(state, language),
		GroupChoices:  accountsGroupChoices(state),
		FilterText:    state.AccountFilterText,
		ShownText:     accountsShownText(state, language),
		TypeChoices:   accountsTypeChoices(language),
		CurrencyCodes: append([]string(nil), accountsCurrencyCodes...),
		DaysChoices:   accountsDaysChoices(language),
		DayLabels:     accountsDayLabels(language),
		Dialog: AccountsDialogDTO{
			Kind:                dialog.Kind,
			Title:               dialog.Title,
			Text:                dialog.Text,
			ErrorText:           dialog.ErrorText,
			AccountLabel:        dialog.AccountLabel,
			AccountName:         dialog.AccountName,
			Balance:             dialog.Balance,
			AccountType:         dialog.AccountType,
			Offline:             dialog.Offline,
			BankID:              dialog.BankID,
			AccountID:           dialog.AccountID,
			ConnectionName:      dialog.ConnectionName,
			ConnectionBLZ:       dialog.ConnectionBLZ,
			ConnectionUserID:    dialog.ConnectionUserID,
			ConnectionServerURL: dialog.ConnectionServerURL,
			ConnectionHasPin:    dialog.ConnectionHasPin,
			ConnectionPinText:   dialog.ConnectionPinText,
			ConnectionSyncing:   dialog.ConnectionSyncing,
			HistoryDate:         dialog.HistoryDate,
			HistoryHasStart:     dialog.HistoryHasStart,
		},
	}
	if state.Error != nil {
		dto.ErrorText = *state.Error
	}
	if state.Message != nil {
		dto.MessageText = *state.Message
	}
	// Banking errors and messages take the page's one banner; keep the accounts
	// texts when the banking state is quiet (Rust's apply_props).
	if bankingState.Error != nil {
		dto.ErrorText = *bankingState.Error
	}
	if bankingState.Message != nil {
		dto.MessageText = *bankingState.Message
	}
	accountsApplyDetail(&dto, state, language)
	return dto
}

func accountsApplyDetail(dto *AccountsDTO, state accountsview.AccountsState, language i18n.Language) {
	detail := state.Detail
	if detail == nil {
		dto.DetailOpen = false
		dto.DetailRows = []AccountDetailRowDTO{}
		dto.DetailDays = 90
		return
	}

	badge := accountsAccountBadge(detail.Account, accountsBankFor(&state, &detail.Account))
	dto.DetailOpen = true
	dto.DetailTitle = accountsDetailTitle(detail, language)
	dto.DetailLogo = badge.Logo
	dto.DetailLetters = badge.Letters
	dto.DetailColour = badge.Colour
	dto.DetailSubtitle = accountsAccountIdentifier(detail.Account)
	dto.DetailBalance = format.Money(detail.Account.Balance, language, false)
	dto.DetailTone = accountsToneCode(accountgrouping.BalanceToneValue(
		detail.Account.Balance,
		detail.Account.AccountType == accountgrouping.LoanType,
	))
	dto.DetailAccountID = accountsID(int64(detail.Account.ID))
	dto.DetailDays = accountsDaysValue(detail.Days)
	dto.DetailSummary = i18n.Format(
		language,
		i18n.LastDaysSummary,
		i18n.Int(detail.Days),
		i18n.Str(format.Money(detail.IncomeInPeriod, language, false)),
		i18n.Str(format.Money(detail.ExpensesInPeriod, language, false)),
	)

	chart := accountsBalancePath(detail.History, language)
	dto.DetailPath = chart.Path
	dto.DetailAxisTop = chart.AxisTop
	dto.DetailAxisBottom = chart.AxisBottom
	dto.DetailEndX = chart.EndX
	dto.DetailEndY = chart.EndY
	dto.DetailEndLabel = chart.EndLabel
	dto.DetailRows = accountsDetailRows(detail, language)
}

func accountsFigures(state accountsview.AccountsState, language i18n.Language) []AccountsFigureDTO {
	allAccounts := accountsGrouping(state.Accounts)
	loans := make([]accountgrouping.AccountItem, 0, len(allAccounts))
	for index, account := range state.Accounts {
		if account.AccountType == accountgrouping.LoanType {
			loans = append(loans, allAccounts[index])
		}
	}

	figures := make([]AccountsFigureDTO, 0, 2)
	if len(state.Accounts) > 0 {
		key := i18n.AccountsTotalLabel
		if len(loans) > 0 {
			key = i18n.AccountsNetLabel
		}
		figures = append(figures, AccountsFigureDTO{
			Label: i18n.Format(language, key, i18n.Int(int64(len(state.Accounts)))),
			Values: accountsAmounts(
				state.GrandTotal,
				allAccounts,
				language,
				len(state.GrandTotal) > 1,
			),
			Secondary: false,
		})
	}
	if len(loans) > 0 {
		totals := accountgrouping.GrandTotal(loans, state.PrimaryCurrency)
		figures = append(figures, AccountsFigureDTO{
			Label:     i18n.Text(language, i18n.AccountsLoansLabel),
			Values:    accountsAmounts(totals, loans, language, true),
			Secondary: true,
		})
	}
	return figures
}

func accountsAmounts(
	totals []money.CurrencyTotal,
	accounts []accountgrouping.AccountItem,
	language i18n.Language,
	plain bool,
) []AccountAmountDTO {
	rows := make([]AccountAmountDTO, 0, len(totals))
	for _, total := range totals {
		tone := "plain"
		if !plain {
			tone = accountsToneCode(accountgrouping.BalanceToneFor(accounts, total.Currency))
		}
		rows = append(rows, AccountAmountDTO{
			Text: format.Money(money.Money{Minor: total.Total, Currency: total.Currency}, language, false),
			Tone: tone,
		})
	}
	return rows
}

func accountsBankCards(
	state accountsview.AccountsState,
	bankingState bankingcore.BankingState,
	language i18n.Language,
) []AccountsBankCardDTO {
	cards := make([]AccountsBankCardDTO, 0, len(state.BankAccesses)+1)
	for _, bank := range state.BankAccesses {
		accounts := make([]accountsview.AccountItem, 0)
		for _, account := range state.Accounts {
			if account.BankAccessID != nil && *account.BankAccessID == bank.ID {
				accounts = append(accounts, account)
			}
		}
		card := accountsBankCard(state, bank.ID, bank.BankName, bank.BLZ, bank.BIC,
			accountsBankSubtitle(bank, language), accountsCentralBLZHint(bank, language),
			bank.Offline, accountsCollapsedBank(state, bank.ID), accounts, language)
		applyBankingCard(&card, &bankingState, state.Busy.Active(), language)
		cards = append(cards, card)
	}

	withoutBank := make([]accountsview.AccountItem, 0)
	for _, account := range state.Accounts {
		if account.BankAccessID == nil {
			withoutBank = append(withoutBank, account)
		}
	}
	if len(withoutBank) > 0 {
		card := accountsBankCard(
			state,
			-1,
			i18n.Text(language, i18n.AccountsWithoutBank),
			"",
			nil,
			"",
			"",
			false,
			false,
			withoutBank,
			language,
		)
		applyBankingCard(&card, &bankingState, state.Busy.Active(), language)
		cards = append(cards, card)
	}
	return cards
}

// applyBankingCard fills the online fields Rust's banking::bank_cards sets. A
// card without a banking bank keeps the accounts defaults and an empty menu.
func applyBankingCard(
	card *AccountsBankCardDTO,
	bankingState *bankingcore.BankingState,
	accountsBusy bool,
	language i18n.Language,
) {
	bank := bankingBankForCard(bankingState, card.BankID)
	if bank == nil {
		card.Menu = []BankMenuItemDTO{}
		return
	}
	card.Syncing = bankingState.SyncingBank != nil && *bankingState.SyncingBank == bank.ID
	card.SyncStep = bankSyncStep(bankingState, language)
	card.SyncEnabled = shell.OnlineSyncAvailable && !bankingState.IsSyncing()
	card.SyncTooltip = bankSyncTooltip(language)
	card.PinText = bankPinText(bank, bankingState, language)
	card.CanTestConnection = bank.CanTestConnection
	card.Menu = bankMenu(bank, bankingState, accountsBusy, language)
}

func bankingBankForCard(state *bankingcore.BankingState, bankID int) *bankingcore.BankItem {
	for index := range state.Banks {
		if int64(state.Banks[index].ID) == int64(bankID) {
			return &state.Banks[index]
		}
	}
	return nil
}

func accountsBankCard(
	state accountsview.AccountsState,
	bankID model.BankConnectionID,
	name string,
	blz string,
	bic *string,
	subtitle string,
	centralBLZHint string,
	offline bool,
	collapsed bool,
	accounts []accountsview.AccountItem,
	language i18n.Language,
) AccountsBankCardDTO {
	groupingAccounts := make([]accountgrouping.AccountItem, 0, len(accounts))
	for _, account := range accounts {
		groupingAccounts = append(groupingAccounts, accountsGroupingItem(account))
	}
	totals := accountgrouping.GrandTotal(groupingAccounts, state.PrimaryCurrency)

	rows := make([]BankAccountRowDTO, 0, len(accounts))
	for index := range accounts {
		rows = append(rows, accountsBankAccountRow(&state, &accounts[index], language))
	}

	var badge accountsBadgeFields
	if bankID >= 0 {
		if offline {
			badge = accountsBadgeForName(name, nil, nil)
		} else {
			badge = accountsBadgeForName(name, accountsOptionalTrimmed(blz), accountsOptionalTrimmedPointer(bic))
		}
	}

	return AccountsBankCardDTO{
		BankID:           accountsID(int64(bankID)),
		Name:             name,
		Subtitle:         subtitle,
		CentralBLZHint:   centralBLZHint,
		Offline:          offline,
		Collapsed:        collapsed,
		AccountCountText: i18n.Format(language, i18n.AccountCount, i18n.Int(int64(len(accounts)))),
		Totals:           accountsAmounts(totals, groupingAccounts, language, false),
		Accounts:         rows,
		SyncEnabled:      true,
		Menu:             []BankMenuItemDTO{},
		Logo:             badge.Logo,
		LogoLetters:      badge.Letters,
		LogoColour:       badge.Colour,
	}
}

func accountsBankSubtitle(bank model.BankAccessItem, language i18n.Language) string {
	if bank.Offline {
		return i18n.Text(language, i18n.BankOfflineHint)
	}

	parts := []string{i18n.Format(language, i18n.BlzPrefix, i18n.Str(bank.BLZ))}
	if bic := accountsTrimmedPointer(bank.BIC); bic != "" {
		parts = append(parts, i18n.Format(language, i18n.BicPrefix, i18n.Str(bic)))
	}
	if bank.LastSync != nil {
		parts = append(parts, i18n.Format(language, i18n.SyncedPrefix, i18n.Str(format.DateTime(*bank.LastSync))))
	}
	if bank.SyncFrom != nil {
		parts = append(parts, i18n.Format(language, i18n.NextSyncFrom, i18n.Str(format.Date(*bank.SyncFrom))))
	}
	if !bank.IncludeInSyncAll {
		parts = append(parts, i18n.Text(language, i18n.NotInSyncAll))
	}
	return strings.Join(parts, " · ")
}

func accountsCentralBLZHint(bank model.BankAccessItem, language i18n.Language) string {
	if bank.Offline || strings.TrimSpace(bank.BLZ) == "" {
		return ""
	}
	mapped := bankdirectory.FintsBLZ(bankdirectory.Bundled(), bank.BLZ)
	if mapped == strings.TrimSpace(bank.BLZ) {
		return ""
	}
	return i18n.Format(language, i18n.CentralBlzHint, i18n.Str(mapped))
}

func accountsBankAccountRow(
	state *accountsview.AccountsState,
	account *accountsview.AccountItem,
	language i18n.Language,
) BankAccountRowDTO {
	badge := accountsAccountBadge(*account, accountsBankFor(state, account))
	return BankAccountRowDTO{
		AccountID:   accountsID(int64(account.ID)),
		Name:        accountsAccountLabel(*account, language),
		Subtitle:    accountsAccountSubtitle(*account, language),
		BalanceText: format.Money(account.Balance, language, false),
		Tone: accountsToneCode(accountgrouping.BalanceToneValue(
			account.Balance,
			account.AccountType == accountgrouping.LoanType,
		)),
		NewCount:    accountsNewCount(account.NewCount),
		Logo:        badge.Logo,
		LogoLetters: badge.Letters,
		LogoColour:  badge.Colour,
	}
}

func accountsPanelRows(state accountsview.AccountsState, language i18n.Language) []AccountPanelRowDTO {
	visible := make(map[model.AccountID]struct{})
	for _, account := range state.FilteredAccounts() {
		visible[account.ID] = struct{}{}
	}
	filtering := strings.TrimSpace(state.AccountFilterText) != ""
	allAccounts := accountsGrouping(state.Accounts)
	allNew := int64(0)
	for _, account := range state.Accounts {
		allNew += account.NewCount
	}

	rows := make([]AccountPanelRowDTO, 0, len(state.Groups)+len(state.Accounts)+1)
	rows = append(rows, AccountPanelRowDTO{
		Kind:        "all",
		GroupKind:   "",
		AccountType: "",
		ID:          -1,
		Label:       i18n.Text(language, i18n.AllAccounts),
		Secondary:   "",
		Detail:      "",
		Name:        "",
		DeleteText:  "",
		Selected:    state.Selection.Kind == accountpanel.SelectionAll,
		Collapsed:   false,
		NewCount:    accountsNewCount(allNew),
		Totals: accountsAmounts(
			state.GrandTotal,
			allAccounts,
			language,
			len(state.GrandTotal) > 1,
		),
		Indent:      false,
		Logo:        "",
		LogoLetters: "",
		LogoColour:  0,
	})

	for groupIndex := range state.Groups {
		group := &state.Groups[groupIndex]
		matching := make([]accountgrouping.AccountItem, 0, len(group.Accounts))
		for _, account := range group.Accounts {
			if _, ok := visible[account.ID]; ok {
				matching = append(matching, account)
			}
		}
		if filtering && len(matching) == 0 {
			continue
		}

		collapsed := false
		if _, ok := state.Collapsed[group.Key.ID()]; ok {
			collapsed = true
		}
		selected := state.Selection.Kind == accountpanel.SelectionGroup &&
			state.Selection.Group.ID() == group.Key.ID()

		groupKind := "type"
		accountType := ""
		id := -1
		name := ""
		if group.Key.Kind == accountgrouping.GroupKeyCustom {
			groupKind = "custom"
			accountType = ""
			id = accountsID(int64(group.Key.GroupID))
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
		badge := accountsGroupBadge(&state, group)

		rows = append(rows, AccountPanelRowDTO{
			Kind:        "group",
			GroupKind:   groupKind,
			AccountType: accountType,
			ID:          id,
			Label:       label,
			Secondary:   "",
			Detail:      "",
			Name:        name,
			DeleteText:  deleteText,
			Selected:    selected,
			Collapsed:   collapsed,
			NewCount:    accountsNewCount(group.NewCount),
			Totals:      accountsAmounts(group.Totals, group.Accounts, language, false),
			Indent:      false,
			Logo:        badge.Logo,
			LogoLetters: badge.Letters,
			LogoColour:  badge.Colour,
		})

		if collapsed {
			continue
		}
		for _, account := range matching {
			full := accountsFindAccount(state.Accounts, account.ID)
			if full == nil {
				continue
			}
			rows = append(rows, accountsPanelAccountRow(&state, group, account, full, language))
		}
	}
	return rows
}

func accountsPanelAccountRow(
	state *accountsview.AccountsState,
	group *accountgrouping.AccountGroupView,
	account accountgrouping.AccountItem,
	full *accountsview.AccountItem,
	language i18n.Language,
) AccountPanelRowDTO {
	secondary := ""
	if full.Name != nil {
		if digits := accountsAccountDigits(*full); digits != "" {
			secondary = "…" + digits
		}
	}
	detail := ""
	if full.LastUpdate != nil {
		detail = i18n.Format(language, i18n.LastFetched, i18n.Str(format.Date(civilDate(*full.LastUpdate))))
	}
	badge := accountsAccountBadge(*full, accountsBankFor(state, full))

	return AccountPanelRowDTO{
		Kind:        "account",
		GroupKind:   "",
		AccountType: account.AccountType,
		ID:          accountsID(int64(account.ID)),
		Label:       accountsAccountLabel(*full, language),
		Secondary:   secondary,
		Detail:      detail,
		Name:        "",
		DeleteText:  "",
		Selected: state.Selection.Kind == accountpanel.SelectionAccount &&
			state.Selection.Account == account.ID,
		Collapsed: false,
		NewCount:  accountsNewCount(account.NewCount),
		Totals: []AccountAmountDTO{{
			Text: format.Money(account.Balance, language, false),
			Tone: accountsToneCode(accountgrouping.BalanceToneFor(group.Accounts, account.Balance.Currency)),
		}},
		Indent:      true,
		Logo:        badge.Logo,
		LogoLetters: badge.Letters,
		LogoColour:  badge.Colour,
	}
}

func accountsGroupChoices(state accountsview.AccountsState) []AccountGroupChoiceDTO {
	choices := make([]AccountGroupChoiceDTO, 0, len(state.CustomGroups))
	for _, group := range state.CustomGroups {
		choices = append(choices, AccountGroupChoiceDTO{
			ID:   accountsID(int64(group.ID)),
			Name: group.Name,
		})
	}
	return choices
}

func accountsShownText(state accountsview.AccountsState, language i18n.Language) string {
	if strings.TrimSpace(state.AccountFilterText) == "" {
		return ""
	}
	return i18n.Format(
		language,
		i18n.AccountsShown,
		i18n.Int(int64(len(state.FilteredAccounts()))),
		i18n.Int(int64(len(state.Accounts))),
	)
}

func accountsTypeChoices(language i18n.Language) []AccountTypeChoiceDTO {
	choices := make([]AccountTypeChoiceDTO, 0, len(accountgrouping.AccountTypes))
	for _, code := range accountgrouping.AccountTypes {
		choices = append(choices, AccountTypeChoiceDTO{
			Code:  code,
			Label: accountsAccountTypeLabel(code, language),
		})
	}
	return choices
}

func accountsDaysChoices(language i18n.Language) []AccountDaysChoiceDTO {
	choices := make([]AccountDaysChoiceDTO, 0, len(accountsDetailDays))
	for _, days := range accountsDetailDays {
		choices = append(choices, AccountDaysChoiceDTO{
			Days:  int(days),
			Label: accountsDaysLabel(days, language),
		})
	}
	return choices
}

func accountsDayLabels(language i18n.Language) []string {
	labels := make([]string, 0, len(accountsDetailDays))
	for _, days := range accountsDetailDays {
		labels = append(labels, accountsDaysLabel(days, language))
	}
	return labels
}

func accountsDaysLabel(days int64, language i18n.Language) string {
	if days == 365 {
		return i18n.Text(language, i18n.OneYear)
	}
	return i18n.Format(language, i18n.DaysCount, i18n.Int(days))
}

func accountsDetailRows(detail *accountsview.AccountDetail, language i18n.Language) []AccountDetailRowDTO {
	rows := make([]AccountDetailRowDTO, 0, len(detail.Recent))
	for _, transaction := range detail.Recent {
		rows = append(rows, AccountDetailRowDTO{
			DateText:   format.Date(transaction.Date),
			Title:      transaction.RawDescription(),
			AmountText: format.Money(transaction.Amount, language, true),
			Expense:    transaction.Amount.Minor < 0,
		})
	}
	return rows
}

func accountsDetailTitle(detail *accountsview.AccountDetail, language i18n.Language) string {
	label := accountsAccountLabel(detail.Account, language)
	if detail.Account.BankName == "" {
		return label
	}
	return detail.Account.BankName + " · " + label
}

type accountsPath struct {
	Path       string
	AxisTop    string
	AxisBottom string
	EndX       float32
	EndY       float32
	EndLabel   string
}

func accountsBalancePath(history []balance.BalancePoint, language i18n.Language) accountsPath {
	if len(history) == 0 {
		return accountsPath{}
	}

	low := history[0].Balance.Minor
	high := history[0].Balance.Minor
	for _, point := range history[1:] {
		if point.Balance.Minor < low {
			low = point.Balance.Minor
		}
		if point.Balance.Minor > high {
			high = point.Balance.Minor
		}
	}
	currency := history[0].Balance.Currency
	span := high - low

	var path strings.Builder
	var endX, endY float32
	for index, point := range history {
		var x float32
		if len(history) > 1 {
			x = float32(index) / float32(len(history)-1) * accountsChartWidth
		} else {
			x = accountsChartWidth / 2
		}
		ratio := float32(0.5)
		if span != 0 {
			ratio = float32(point.Balance.Minor-low) / float32(span)
		}
		y := accountsChartPadding + (1-ratio)*(accountsChartHeight-2*accountsChartPadding)
		if index == len(history)-1 {
			endX = x
			endY = y
		}
		if index == 0 {
			fmt.Fprintf(&path, "M %.2f %.2f", x, y)
		} else {
			fmt.Fprintf(&path, " L %.2f %.2f", x, y)
		}
	}

	axisTop := format.WholeMoney(money.Money{Minor: high, Currency: currency}, language)
	axisBottom := ""
	if high != low {
		axisBottom = format.WholeMoney(money.Money{Minor: low, Currency: currency}, language)
	}
	return accountsPath{
		Path:       path.String(),
		AxisTop:    axisTop,
		AxisBottom: axisBottom,
		EndX:       endX,
		EndY:       endY,
		EndLabel:   format.Money(history[len(history)-1].Balance, language, false),
	}
}

func accountsGrouping(accounts []accountsview.AccountItem) []accountgrouping.AccountItem {
	grouped := make([]accountgrouping.AccountItem, 0, len(accounts))
	for _, account := range accounts {
		grouped = append(grouped, accountsGroupingItem(account))
	}
	return grouped
}

func accountsGroupingItem(account accountsview.AccountItem) accountgrouping.AccountItem {
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

func accountsFindAccount(accounts []accountsview.AccountItem, id model.AccountID) *accountsview.AccountItem {
	for index := range accounts {
		if accounts[index].ID == id {
			return &accounts[index]
		}
	}
	return nil
}

func accountsBankFor(
	state *accountsview.AccountsState,
	account *accountsview.AccountItem,
) *model.BankAccessItem {
	if account.BankAccessID == nil {
		return nil
	}
	for index := range state.BankAccesses {
		if state.BankAccesses[index].ID == *account.BankAccessID {
			return &state.BankAccesses[index]
		}
	}
	return nil
}

type accountsBadgeFields struct {
	Logo    string
	Letters string
	Colour  int
}

func accountsBadgeForName(name string, blz *string, bic *string) accountsBadgeFields {
	logo := ""
	if found, ok := banklogo.For(name, blz, bic); ok {
		logo = found.ID()
	}
	monogram := banklogo.MonogramFor(name)
	return accountsBadgeFields{
		Logo:    logo,
		Letters: monogram.Letters,
		Colour:  int(monogram.Colour),
	}
}

func accountsAccountBadge(
	account accountsview.AccountItem,
	bank *model.BankAccessItem,
) accountsBadgeFields {
	if bank != nil {
		if bank.Offline {
			return accountsBadgeForName(bank.BankName, nil, nil)
		}
		return accountsBadgeForName(
			bank.BankName,
			accountsOptionalTrimmed(bank.BLZ),
			accountsOptionalTrimmedPointer(bank.BIC),
		)
	}
	if strings.TrimSpace(account.BankName) != "" {
		return accountsBadgeForName(account.BankName, nil, nil)
	}
	blz := accountsIBANBLZ(account.IBAN)
	if blz == "" {
		return accountsBadgeFields{}
	}
	entry, ok := bankdirectory.Bundled().Lookup(blz)
	if !ok {
		return accountsBadgeFields{}
	}
	return accountsBadgeForName(entry.Name, &blz, &entry.BIC)
}

func accountsGroupBadge(
	state *accountsview.AccountsState,
	group *accountgrouping.AccountGroupView,
) accountsBadgeFields {
	if group.Key.Kind == accountgrouping.GroupKeyCustom {
		return accountsBadgeFields{}
	}
	var first accountsBadgeFields
	found := false
	for _, grouped := range group.Accounts {
		account := accountsFindAccount(state.Accounts, grouped.ID)
		if account == nil {
			continue
		}
		badge := accountsAccountBadge(*account, accountsBankFor(state, account))
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

func accountsIBANBLZ(ibanText string) string {
	characters := make([]rune, 0, len(ibanText))
	for _, character := range ibanText {
		if unicode.IsSpace(character) {
			continue
		}
		characters = append(characters, character)
	}
	if len(characters) < 12 || characters[0] != 'D' || characters[1] != 'E' {
		return ""
	}
	blz := string(characters[4:12])
	for _, character := range blz {
		if character < '0' || character > '9' {
			return ""
		}
	}
	return blz
}

func accountsAccountLabel(account accountsview.AccountItem, language i18n.Language) string {
	if account.Name != nil {
		if trimmed := strings.TrimSpace(*account.Name); trimmed != "" {
			return trimmed
		}
	}
	accountType := accountsAccountTypeLabel(account.AccountType, language)
	if digits := accountsAccountDigits(account); digits != "" {
		return accountType + " …" + digits
	}
	return accountType
}

func accountsAccountDigits(account accountsview.AccountItem) string {
	source := ""
	if trimmed := strings.TrimSpace(account.IBAN); trimmed != "" {
		source = trimmed
	} else if account.AccountNumber != nil {
		source = strings.TrimSpace(*account.AccountNumber)
	}
	if source == "" {
		return ""
	}
	characters := []rune(source)
	start := len(characters) - 4
	if start < 0 {
		start = 0
	}
	return string(characters[start:])
}

func accountsAccountSubtitle(account accountsview.AccountItem, language i18n.Language) string {
	display := accountsAccountIdentifier(account)
	subtitle := ""
	if account.Name != nil {
		if display != "" {
			subtitle = accountsAccountTypeLabel(account.AccountType, language) + " · " + display
		} else {
			subtitle = accountsAccountTypeLabel(account.AccountType, language)
		}
	} else {
		subtitle = display
	}
	if account.LastUpdate != nil {
		fetched := i18n.Format(
			language,
			i18n.LastFetched,
			i18n.Str(format.Date(civilDate(*account.LastUpdate))),
		)
		if subtitle == "" {
			subtitle = fetched
		} else {
			subtitle += " · " + fetched
		}
	}
	return subtitle
}

func accountsAccountIdentifier(account accountsview.AccountItem) string {
	if trimmed := strings.TrimSpace(account.IBAN); trimmed != "" {
		return accountids.Display(trimmed)
	}
	if account.AccountNumber != nil {
		if trimmed := strings.TrimSpace(*account.AccountNumber); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func accountsAccountTypeLabel(accountType string, language i18n.Language) string {
	switch accountType {
	case "checking":
		return i18n.Text(language, i18n.TypeChecking)
	case "savings":
		return i18n.Text(language, i18n.TypeSavings)
	case "credit_card":
		return i18n.Text(language, i18n.TypeCreditCard)
	case accountgrouping.LoanType:
		return i18n.Text(language, i18n.TypeLoan)
	case "depot":
		return i18n.Text(language, i18n.TypeDepot)
	default:
		return accountType
	}
}

func accountsToneCode(tone accountgrouping.BalanceTone) string {
	switch tone {
	case accountgrouping.BalanceTonePositive:
		return "positive"
	case accountgrouping.BalanceToneNegative:
		return "negative"
	case accountgrouping.BalanceToneLoan:
		return "loan"
	case accountgrouping.BalanceToneZero:
		return "zero"
	default:
		return "positive"
	}
}

func accountsCollapsedBank(state accountsview.AccountsState, id model.BankConnectionID) bool {
	_, collapsed := state.CollapsedBanks[id]
	return collapsed
}

func accountsOptionalTrimmed(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func accountsOptionalTrimmedPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return accountsOptionalTrimmed(*value)
}

func accountsTrimmedPointer(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func accountsID(id int64) int {
	if id < math.MinInt32 || id > math.MaxInt32 {
		return -1
	}
	return int(id)
}

func accountsNewCount(value int64) int {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(value)
}

func accountsDaysValue(days int64) int {
	if days < math.MinInt32 || days > math.MaxInt32 {
		return 90
	}
	return int(days)
}

func civilDate(value time.Time) civil.Date {
	return civil.FromTime(value)
}
