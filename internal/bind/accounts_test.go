package bind

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

type bindAccountsSeed struct {
	bank     model.BankConnectionID
	checking model.AccountID
	savings  model.AccountID
	loan     model.AccountID
	group    model.AccountGroupID
}

type accountsFixture struct {
	paths    paths.AppPaths
	session  *shell.Session
	shell    *Shell
	accounts *Accounts
	router   *Router
	seed     bindAccountsSeed

	mu          sync.Mutex
	shellEvents []ShellDTO
	events      chan AccountsDTO
}

func seedBindAccounts(t *testing.T, appPaths paths.AppPaths) bindAccountsSeed {
	t.Helper()
	ctx := context.Background()
	database, err := unlock.Create(appPaths, "default", []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}

	bank, err := bankconnections.CreateOffline(ctx, database, "Bank Norwegian")
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	group, err := accountgroups.Create(ctx, database, model.NewAccountGroup{Name: "Reisen", SortOrder: 1})
	if err != nil {
		t.Fatalf("create account group: %v", err)
	}

	name := "Girokonto"
	checking, err := accounts.Create(ctx, database, model.NewAccount{
		BankConnectionID: &bank.ID,
		Name:             &name,
		IBAN:             stringValue("DE89370400440532013000"),
		Currency:         money.EUR,
		AccountType:      model.AccountTypeChecking,
		Balance:          money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create checking: %v", err)
	}
	savings, err := accounts.Create(ctx, database, model.NewAccount{
		BankConnectionID: &bank.ID,
		IBAN:             stringValue("DE44500105175407324931"),
		Currency:         money.EUR,
		AccountType:      model.AccountTypeSavings,
		Balance:          money.Money{Minor: 25_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create savings: %v", err)
	}
	loan, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringValue("DE02120300000000202051"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeLoan,
		Balance:     money.Money{Minor: -9_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create loan: %v", err)
	}

	insert := func(day civil.Date, minor int64, partner string, isNew bool) {
		partnerName := partner
		if _, err := transactions.InsertPlanned(ctx, database, checking, []model.NewTransaction{{
			BookingDate: day,
			Amount:      money.Money{Minor: minor, Currency: money.EUR},
			PartnerName: &partnerName,
			ImportHash:  fmt.Sprintf("bind-%s-%d-%s", day.String(), minor, partner),
			CreatedAt:   testNow.UTC(),
			IsNew:       isNew,
		}}); err != nil {
			t.Fatalf("insert %s: %v", partner, err)
		}
	}
	insert(civil.FromTime(testNow).AddDays(-2), 2_000, "Employer", false)
	insert(civil.FromTime(testNow).AddDays(-1), -500, "Bakery", true)

	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
	return bindAccountsSeed{
		bank:     bank.ID,
		checking: checking,
		savings:  savings,
		loan:     loan,
		group:    group,
	}
}

func newAccountsFixture(t *testing.T) *accountsFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	seed := seedBindAccounts(t, appPaths)

	session, offer, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	if offer != nil {
		t.Fatal("a nil FinDesk root offered a migration")
	}

	fixture := &accountsFixture{
		paths:   appPaths,
		session: session,
		seed:    seed,
		events:  make(chan AccountsDTO, 256),
	}
	fixture.shell = NewShell(session, offer, ShellOptions{
		Paths: appPaths,
		Clock: func() time.Time { return testNow },
		Emit: func(dto ShellDTO) {
			fixture.mu.Lock()
			fixture.shellEvents = append(fixture.shellEvents, dto)
			fixture.mu.Unlock()
		},
	})
	fixture.accounts = NewAccounts(session, AccountsOptions{
		Emit: func(dto AccountsDTO) {
			fixture.events <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("accounts", fixture.accounts)
	fixture.router.RegisterShell(fixture.shell)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.accounts.ServiceShutdown()
		_ = fixture.shell.ServiceShutdown()
	})
	return fixture
}

func (f *accountsFixture) unlock(t *testing.T) {
	t.Helper()
	if _, err := f.shell.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func (f *accountsFixture) load(t *testing.T) AccountsDTO {
	t.Helper()
	f.unlock(t)
	dto, err := f.accounts.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f.wait(t, "the loaded accounts", func(dto AccountsDTO) bool {
		return !dto.Loading && dto.HasContent
	}, dto)
}

func (f *accountsFixture) wait(
	t *testing.T,
	description string,
	predicate func(AccountsDTO) bool,
	fallback ...AccountsDTO,
) AccountsDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case dto := <-f.events:
			if predicate(dto) {
				return dto
			}
		case <-deadline:
			if len(fallback) > 0 {
				return fallback[0]
			}
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func (f *accountsFixture) drain() {
	for len(f.events) > 0 {
		<-f.events
	}
}

func TestAccountsServiceLoadsEveryAppliedField(t *testing.T) {
	fixture := newAccountsFixture(t)
	dto := fixture.load(t)

	if dto.Loading || !dto.ShowContent || !dto.HasContent {
		t.Fatalf("content flags = loading %v show %v has %v", dto.Loading, dto.ShowContent, dto.HasContent)
	}
	if dto.ErrorText != "" || dto.MessageText != "" {
		t.Fatalf("messages = %q / %q", dto.ErrorText, dto.MessageText)
	}

	if len(dto.Figures) != 2 {
		t.Fatalf("figures = %+v", dto.Figures)
	}
	if want := i18n.Format(i18n.En, i18n.AccountsNetLabel, i18n.Int(3)); dto.Figures[0].Label != want {
		t.Fatalf("net label = %q, want %q", dto.Figures[0].Label, want)
	}
	if len(dto.Figures[0].Values) != 1 || dto.Figures[0].Values[0].Text != "€1,160.00" {
		t.Fatalf("net values = %+v", dto.Figures[0].Values)
	}
	if dto.Figures[1].Label != i18n.Text(i18n.En, i18n.AccountsLoansLabel) ||
		!dto.Figures[1].Secondary || dto.Figures[1].Values[0].Tone != "plain" {
		t.Fatalf("loan figure = %+v", dto.Figures[1])
	}

	if len(dto.BankCards) != 2 {
		t.Fatalf("bank cards = %+v", dto.BankCards)
	}
	bankCard := dto.BankCards[0]
	if bankCard.BankID != int(fixture.seed.bank) || bankCard.Name != "Bank Norwegian" ||
		!bankCard.Offline || bankCard.Subtitle != i18n.Text(i18n.En, i18n.BankOfflineHint) ||
		bankCard.AccountCountText != "2 accounts" {
		t.Fatalf("bank card = %+v", bankCard)
	}
	if len(bankCard.Accounts) != 2 || len(bankCard.Totals) != 1 || bankCard.Totals[0].Text != "€1,250.00" {
		t.Fatalf("bank card rows/totals = %+v", bankCard)
	}
	if bankCard.Logo != "" || bankCard.LogoLetters != "BN" {
		t.Fatalf("bank badge = %q / %q", bankCard.Logo, bankCard.LogoLetters)
	}
	withoutBank := dto.BankCards[1]
	if withoutBank.BankID != -1 || withoutBank.Name != i18n.Text(i18n.En, i18n.AccountsWithoutBank) ||
		len(withoutBank.Accounts) != 1 {
		t.Fatalf("without-bank card = %+v", withoutBank)
	}

	if len(dto.PanelRows) == 0 || dto.PanelRows[0].Kind != "all" ||
		dto.PanelRows[0].Label != i18n.Text(i18n.En, i18n.AllAccounts) ||
		dto.PanelRows[0].NewCount != 1 {
		t.Fatalf("all row = %+v", dto.PanelRows[0])
	}
	var named, unnamed *AccountPanelRowDTO
	for index := range dto.PanelRows {
		row := &dto.PanelRows[index]
		if row.Kind != "account" {
			continue
		}
		switch row.ID {
		case int(fixture.seed.checking):
			named = row
		case int(fixture.seed.savings):
			unnamed = row
		}
	}
	if named == nil || named.Label != "Girokonto" || named.Secondary != "…3000" || !named.Indent {
		t.Fatalf("named row = %+v", named)
	}
	if unnamed == nil || unnamed.Label != "Savings …4931" || unnamed.Secondary != "" {
		t.Fatalf("unnamed row = %+v", unnamed)
	}

	if len(dto.GroupChoices) != 1 || dto.GroupChoices[0].ID != int(fixture.seed.group) ||
		dto.GroupChoices[0].Name != "Reisen" {
		t.Fatalf("group choices = %+v", dto.GroupChoices)
	}
	if len(dto.TypeChoices) != 5 || dto.TypeChoices[2].Code != "credit_card" ||
		dto.TypeChoices[2].Label != "Credit card" {
		t.Fatalf("type choices = %+v", dto.TypeChoices)
	}
	if !reflect.DeepEqual(dto.CurrencyCodes, []string{"EUR", "USD", "GBP", "CHF", "JPY", "CAD"}) {
		t.Fatalf("currency codes = %+v", dto.CurrencyCodes)
	}
	if len(dto.DaysChoices) != 3 || dto.DaysChoices[2].Days != 365 ||
		dto.DaysChoices[0].Label != "30 days" || dto.DaysChoices[2].Label != "1 year" {
		t.Fatalf("days choices = %+v", dto.DaysChoices)
	}
	if !reflect.DeepEqual(dto.DayLabels, []string{"30 days", "90 days", "1 year"}) {
		t.Fatalf("day labels = %+v", dto.DayLabels)
	}
	if dto.DetailOpen || dto.DetailDays != 90 || len(dto.DetailRows) != 0 {
		t.Fatalf("detail defaults = open %v days %d rows %d", dto.DetailOpen, dto.DetailDays, len(dto.DetailRows))
	}

	state, err := fixture.accounts.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !reflect.DeepEqual(state, dto) {
		t.Fatalf("State = %+v, want the loaded DTO %+v", state, dto)
	}
}

func TestAccountsDetailMapsRowsTitleSummaryAndPath(t *testing.T) {
	fixture := newAccountsFixture(t)
	fixture.load(t)
	fixture.drain()

	dto, err := fixture.accounts.OpenDetail(int(fixture.seed.checking))
	if err != nil {
		t.Fatalf("OpenDetail: %v", err)
	}
	dto = fixture.wait(t, "the open detail", func(dto AccountsDTO) bool {
		return dto.DetailOpen
	}, dto)
	if dto.DetailTitle != "Bank Norwegian · Girokonto" {
		t.Fatalf("detail title = %q", dto.DetailTitle)
	}
	if dto.DetailSubtitle != "DE89 3704 0044 0532 0130 00" {
		t.Fatalf("detail subtitle = %q", dto.DetailSubtitle)
	}
	if dto.DetailBalance != "€1,000.00" || dto.DetailTone != "positive" {
		t.Fatalf("detail balance = %q tone %q", dto.DetailBalance, dto.DetailTone)
	}
	if dto.DetailAccountID != int(fixture.seed.checking) || dto.DetailDays != 90 {
		t.Fatalf("detail id/days = %d / %d", dto.DetailAccountID, dto.DetailDays)
	}
	wantSummary := i18n.Format(
		i18n.En,
		i18n.LastDaysSummary,
		i18n.Int(90),
		i18n.Str("€20.00"),
		i18n.Str("€5.00"),
	)
	if dto.DetailSummary != wantSummary {
		t.Fatalf("detail summary = %q, want %q", dto.DetailSummary, wantSummary)
	}
	if !strings.HasPrefix(dto.DetailPath, "M ") {
		t.Fatalf("detail path = %q", dto.DetailPath)
	}
	if len(dto.DetailRows) != 2 {
		t.Fatalf("detail rows = %+v", dto.DetailRows)
	}
	if dto.DetailRows[0].Title != "Bakery" || dto.DetailRows[0].AmountText != "-€5.00" ||
		!dto.DetailRows[0].Expense {
		t.Fatalf("detail row = %+v", dto.DetailRows[0])
	}

	if _, err := fixture.accounts.SelectDays(int(fixture.seed.checking), 30); err != nil {
		t.Fatalf("SelectDays: %v", err)
	}
	dto = fixture.wait(t, "the 30-day detail", func(dto AccountsDTO) bool {
		return dto.DetailOpen && dto.DetailDays == 30
	})
	if _, err := fixture.accounts.CloseDetail(); err != nil {
		t.Fatalf("CloseDetail: %v", err)
	}
	dto = fixture.wait(t, "the closed detail", func(dto AccountsDTO) bool {
		return !dto.DetailOpen && dto.DetailDays == 90
	})
	if len(dto.DetailRows) != 0 {
		t.Fatalf("closed detail rows = %+v", dto.DetailRows)
	}
}

func TestAccountsBalancePathMatchesTheRustGeometry(t *testing.T) {
	eur := money.EUR
	history := []balance.BalancePoint{
		{Date: civil.FromTime(testNow), Balance: money.Money{Minor: 10_000, Currency: eur}},
		{Date: civil.FromTime(testNow), Balance: money.Money{Minor: 9_000, Currency: eur}},
		{Date: civil.FromTime(testNow), Balance: money.Money{Minor: 11_500, Currency: eur}},
	}
	chart := accountsBalancePath(history, i18n.En)
	if !strings.HasPrefix(chart.Path, "M ") || strings.Count(chart.Path, "L") != 2 ||
		strings.Count(chart.Path, "M") != 1 {
		t.Fatalf("path = %q", chart.Path)
	}
	if chart.AxisTop != "€115" || chart.AxisBottom != "€90" {
		t.Fatalf("axes = %q / %q", chart.AxisTop, chart.AxisBottom)
	}
	if chart.EndX != accountsChartWidth || chart.EndLabel != "€115.00" {
		t.Fatalf("end = %v / %q", chart.EndX, chart.EndLabel)
	}

	flat := []balance.BalancePoint{
		{Date: civil.FromTime(testNow), Balance: money.Money{Minor: 100, Currency: eur}},
		{Date: civil.FromTime(testNow), Balance: money.Money{Minor: 100, Currency: eur}},
		{Date: civil.FromTime(testNow), Balance: money.Money{Minor: 100, Currency: eur}},
	}
	chart = accountsBalancePath(flat, i18n.En)
	if chart.AxisTop == "" || chart.AxisBottom != "" {
		t.Fatalf("flat axes = %q / %q", chart.AxisTop, chart.AxisBottom)
	}
	middle := accountsChartPadding + 0.5*(accountsChartHeight-2*accountsChartPadding)
	if chart.EndX != accountsChartWidth || chart.EndY != middle {
		t.Fatalf("flat end = %v / %v, want %v / %v", chart.EndX, chart.EndY, accountsChartWidth, middle)
	}
	if chart.EndLabel != "€1.00" {
		t.Fatalf("flat end label = %q", chart.EndLabel)
	}
}

func TestAccountsActionsReturnAndPublishTheChangedDTO(t *testing.T) {
	fixture := newAccountsFixture(t)
	fixture.load(t)
	fixture.drain()

	opened, err := fixture.accounts.OpenAddBank()
	if err != nil {
		t.Fatalf("OpenAddBank: %v", err)
	}
	if opened.Dialog.Kind != "add-bank" {
		t.Fatalf("dialog = %+v", opened.Dialog)
	}

	invalid, err := fixture.accounts.SubmitAddBank("  ")
	if err != nil {
		t.Fatalf("SubmitAddBank blank: %v", err)
	}
	if invalid.Dialog.ErrorText != i18n.Text(i18n.En, i18n.BankNameRequired) {
		t.Fatalf("dialog error = %q", invalid.Dialog.ErrorText)
	}

	if _, err := fixture.accounts.SubmitAddBank("New Bank"); err != nil {
		t.Fatalf("SubmitAddBank: %v", err)
	}
	dto := fixture.wait(t, "the new bank card", func(dto AccountsDTO) bool {
		return len(dto.BankCards) == 3
	})
	if dto.Dialog.Kind != "" {
		t.Fatalf("dialog stayed open: %+v", dto.Dialog)
	}

	if _, err := fixture.accounts.NewGroup("Urlaub"); err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	dto = fixture.wait(t, "the new group", func(dto AccountsDTO) bool {
		return len(dto.GroupChoices) == 2
	})
	var urlaub int
	for _, choice := range dto.GroupChoices {
		if choice.Name == "Urlaub" {
			urlaub = choice.ID
		}
	}
	if urlaub == 0 {
		t.Fatalf("group choices = %+v", dto.GroupChoices)
	}
	if _, err := fixture.accounts.RenameGroup(urlaub, "Reisen 2"); err != nil {
		t.Fatalf("RenameGroup: %v", err)
	}
	fixture.wait(t, "the renamed group", func(dto AccountsDTO) bool {
		for _, choice := range dto.GroupChoices {
			if choice.ID == urlaub && choice.Name == "Reisen 2" {
				return true
			}
		}
		return false
	})

	if _, err := fixture.accounts.OpenAddAccount(int(fixture.seed.bank)); err != nil {
		t.Fatalf("OpenAddAccount: %v", err)
	}
	dto, err = fixture.accounts.SubmitAddAccount(int(fixture.seed.bank), "Card", "4321", "credit_card", "EUR", "10")
	if err != nil {
		t.Fatalf("SubmitAddAccount: %v", err)
	}
	dto = fixture.wait(t, "the added account", func(dto AccountsDTO) bool {
		for _, row := range dto.PanelRows {
			if row.Kind == "account" && row.Label == "Card" {
				return true
			}
		}
		return false
	})
	if dto.Dialog.Kind != "" {
		t.Fatalf("add-account dialog stayed open: %+v", dto.Dialog)
	}

	if _, err := fixture.accounts.OpenBalance(int(fixture.seed.checking)); err != nil {
		t.Fatalf("OpenBalance: %v", err)
	}
	dto, err = fixture.accounts.SubmitBalance(int(fixture.seed.checking), "2.000,00", "savings")
	if err != nil {
		t.Fatalf("SubmitBalance: %v", err)
	}
	fixture.wait(t, "the changed balance", func(dto AccountsDTO) bool {
		for _, row := range dto.PanelRows {
			if row.Kind == "account" && row.ID == int(fixture.seed.checking) &&
				len(row.Totals) == 1 && row.Totals[0].Text == "€2,000.00" {
				return true
			}
		}
		return false
	})

	if _, err := fixture.accounts.OpenRename(int(fixture.seed.checking)); err != nil {
		t.Fatalf("OpenRename: %v", err)
	}
	if _, err := fixture.accounts.SubmitRename(int(fixture.seed.checking), "Haushalt"); err != nil {
		t.Fatalf("SubmitRename: %v", err)
	}
	fixture.wait(t, "the renamed account", func(dto AccountsDTO) bool {
		for _, row := range dto.PanelRows {
			if row.Kind == "account" && row.ID == int(fixture.seed.checking) && row.Label == "Haushalt" {
				return true
			}
		}
		return false
	})

	if _, err := fixture.accounts.OpenDeleteAccount(int(fixture.seed.savings)); err != nil {
		t.Fatalf("OpenDeleteAccount: %v", err)
	}
	if _, err := fixture.accounts.ConfirmDeleteAccount(int(fixture.seed.savings)); err != nil {
		t.Fatalf("ConfirmDeleteAccount: %v", err)
	}
	fixture.wait(t, "the deleted account", func(dto AccountsDTO) bool {
		for _, row := range dto.PanelRows {
			if row.Kind == "account" && row.ID == int(fixture.seed.savings) {
				return false
			}
		}
		return true
	})

	if _, err := fixture.accounts.OpenDeleteBank(int(fixture.seed.bank)); err != nil {
		t.Fatalf("OpenDeleteBank: %v", err)
	}
	if _, err := fixture.accounts.ConfirmDeleteBank(int(fixture.seed.bank)); err != nil {
		t.Fatalf("ConfirmDeleteBank: %v", err)
	}
	fixture.wait(t, "the deleted bank", func(dto AccountsDTO) bool {
		for _, card := range dto.BankCards {
			if card.BankID == int(fixture.seed.bank) {
				return false
			}
		}
		return true
	})
}

func TestAccountsLoadEmitsTheReturnedLoadingDTO(t *testing.T) {
	fixture := newAccountsFixture(t)
	fixture.unlock(t)
	fixture.drain()

	returned, err := fixture.accounts.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	event := fixture.wait(t, "the loading event", func(AccountsDTO) bool { return true })
	if !reflect.DeepEqual(event, returned) {
		t.Fatalf("event = %+v, want the returned DTO %+v", event, returned)
	}
	if !returned.Loading {
		t.Fatal("Load did not return the loading state")
	}
	fixture.wait(t, "the loaded accounts", func(dto AccountsDTO) bool {
		return !dto.Loading && dto.HasContent
	})
}

func TestAccountsSynchronousActionsEmitTheReturnedDTO(t *testing.T) {
	fixture := newAccountsFixture(t)
	fixture.load(t)
	fixture.drain()

	returned, err := fixture.accounts.FilterChanged("giro")
	if err != nil {
		t.Fatalf("FilterChanged: %v", err)
	}
	event := fixture.wait(t, "the filter event", func(AccountsDTO) bool { return true })
	if !reflect.DeepEqual(event, returned) {
		t.Fatalf("event = %+v, want the returned DTO %+v", event, returned)
	}
	if returned.FilterText != "giro" || returned.ShownText != i18n.Format(i18n.En, i18n.AccountsShown, i18n.Int(1), i18n.Int(3)) {
		t.Fatalf("filter text = %q shown %q", returned.FilterText, returned.ShownText)
	}

	returned, err = fixture.accounts.DismissMessage()
	if err != nil {
		t.Fatalf("DismissMessage: %v", err)
	}
	event = fixture.wait(t, "the dismiss event", func(AccountsDTO) bool { return true })
	if !reflect.DeepEqual(event, returned) {
		t.Fatalf("dismiss event = %+v, want %+v", event, returned)
	}
}

func TestAccountsPanelRowsNeverShowAFullIBANWhereRustShowsTheDigits(t *testing.T) {
	fixture := newAccountsFixture(t)
	dto := fixture.load(t)

	full := "DE89370400440532013000"
	formatted := "DE89 3704 0044 0532 0130 00"
	for _, row := range dto.PanelRows {
		if strings.Contains(row.Label, full) || strings.Contains(row.Label, formatted) {
			t.Fatalf("panel label %q shows the full IBAN", row.Label)
		}
		if strings.Contains(row.Secondary, full) || strings.Contains(row.Secondary, formatted) {
			t.Fatalf("panel secondary %q shows the full IBAN", row.Secondary)
		}
	}

	fixture.drain()
	invalid, err := fixture.accounts.SubmitAddAccount(
		int(fixture.seed.bank),
		"",
		full,
		"checking",
		"EUR",
		"1",
	)
	if err != nil {
		t.Fatalf("SubmitAddAccount: %v", err)
	}
	if strings.Contains(invalid.Dialog.ErrorText, full) || strings.Contains(invalid.Dialog.ErrorText, formatted) {
		t.Fatalf("error %q shows the full IBAN", invalid.Dialog.ErrorText)
	}
	if invalid.Dialog.ErrorText != i18n.Text(i18n.En, i18n.AccountIbanExists) {
		t.Fatalf("error = %q", invalid.Dialog.ErrorText)
	}
}
