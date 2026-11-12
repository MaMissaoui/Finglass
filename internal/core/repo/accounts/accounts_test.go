package accounts

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func database(t *testing.T) *db.Database {
	t.Helper()

	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	opened, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func newAccount(name string) model.NewAccount {
	displayName := name
	return model.NewAccount{
		BankConnectionID: nil,
		Institution:      nil,
		Name:             &displayName,
		IBAN:             nil,
		AccountNumber:    nil,
		Currency:         money.EUR,
		AccountType:      model.AccountTypeChecking,
		Balance:          eur(0),
		BalanceUpdatedAt: nil,
		GroupID:          nil,
	}
}

func account(t *testing.T, d *db.Database, accountType model.AccountType) model.AccountID {
	t.Helper()

	new := newAccount("Account")
	new.AccountType = accountType
	id, err := Create(context.Background(), d, new)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return id
}

func count(t *testing.T, d *db.Database, statement string) int64 {
	t.Helper()

	var value int64
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, statement).Scan(&value)
	})
	if err != nil {
		t.Fatalf("count %q: %v", statement, err)
	}
	return value
}

func entries(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	recent, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return recent
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestAccountTypeAndCurrencyChecksRejectBadValues(t *testing.T) {
	d := database(t)

	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "INSERT INTO accounts (account_type) VALUES ('bogus')"); err == nil {
			t.Error("an unknown account type was accepted")
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO accounts (currency) VALUES ('EU')"); err == nil {
			t.Error("a two-letter currency was accepted")
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO accounts (currency) VALUES ('EURO')"); err == nil {
			t.Error("a four-letter currency was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithConn: %v", err)
	}

	if got := count(t, d, "SELECT count(*) FROM accounts"); got != 0 {
		t.Errorf("accounts = %d", got)
	}
}

func TestAccountsCRUDOrderingAndBalanceCurrency(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	late, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: "Late", SortOrder: 10})
	if err != nil {
		t.Fatalf("Create group Late: %v", err)
	}
	early, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: "Early", SortOrder: 1})
	if err != nil {
		t.Fatalf("Create group Early: %v", err)
	}

	bravoNew := newAccount("Bravo")
	bravoNew.GroupID = &late
	bravo, err := Create(ctx, d, bravoNew)
	if err != nil {
		t.Fatalf("Create Bravo: %v", err)
	}
	beta, err := Create(ctx, d, newAccount("Beta"))
	if err != nil {
		t.Fatalf("Create Beta: %v", err)
	}
	zuluNew := newAccount("Zulu")
	zuluNew.GroupID = &early
	zulu, err := Create(ctx, d, zuluNew)
	if err != nil {
		t.Fatalf("Create Zulu: %v", err)
	}
	alphaNew := newAccount("Alpha")
	alphaNew.GroupID = &early
	alpha, err := Create(ctx, d, alphaNew)
	if err != nil {
		t.Fatalf("Create Alpha: %v", err)
	}
	alphaWithoutGroup, err := Create(ctx, d, newAccount("Alpha"))
	if err != nil {
		t.Fatalf("Create Alpha without group: %v", err)
	}

	listed, err := List(ctx, d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ordered := make([]model.AccountID, 0, len(listed))
	for _, account := range listed {
		ordered = append(ordered, account.ID)
	}
	wantOrder := []model.AccountID{alpha, zulu, bravo, alphaWithoutGroup, beta}
	if !slices.Equal(ordered, wantOrder) {
		t.Errorf("account order = %v, want %v (groups by sort order, then names, with ungrouped accounts last)",
			ordered, wantOrder)
	}

	loaded, ok, err := Get(ctx, d, alpha)
	if err != nil || !ok {
		t.Fatalf("Get(alpha) = %v, %v", ok, err)
	}
	if loaded.Name == nil || *loaded.Name != "Alpha" {
		t.Errorf("name = %v", loaded.Name)
	}
	if loaded.Currency != money.EUR || loaded.AccountType != model.AccountTypeChecking || loaded.Balance != eur(0) {
		t.Errorf("loaded = %+v", loaded)
	}

	renamed := "Renamed"
	if err := Rename(ctx, d, alpha, &renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	loaded, _, err = Get(ctx, d, alpha)
	if err != nil || loaded.Name == nil || *loaded.Name != "Renamed" {
		t.Errorf("after Rename name = %v, %v", loaded.Name, err)
	}
	if err := Rename(ctx, d, alpha, nil); err != nil {
		t.Fatalf("Rename(nil): %v", err)
	}
	loaded, _, err = Get(ctx, d, alpha)
	if err != nil || loaded.Name != nil {
		t.Errorf("after Rename(nil) name = %v, %v", loaded.Name, err)
	}

	if err := SetGroup(ctx, d, bravo, &early); err != nil {
		t.Fatalf("SetGroup: %v", err)
	}
	loaded, _, err = Get(ctx, d, bravo)
	if err != nil || loaded.GroupID == nil || *loaded.GroupID != early {
		t.Errorf("after SetGroup = %v, %v", loaded.GroupID, err)
	}
	if err := SetGroup(ctx, d, bravo, nil); err != nil {
		t.Fatalf("SetGroup(nil): %v", err)
	}
	loaded, _, err = Get(ctx, d, bravo)
	if err != nil || loaded.GroupID != nil {
		t.Errorf("after SetGroup(nil) = %v, %v", loaded.GroupID, err)
	}

	at := time.Now().UTC()
	if err := SetBalance(ctx, d, alpha, eur(1234), at); err != nil {
		t.Fatalf("SetBalance: %v", err)
	}
	loaded, _, err = Get(ctx, d, alpha)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.Balance != eur(1234) {
		t.Errorf("balance = %v", loaded.Balance)
	}
	if loaded.BalanceUpdatedAt == nil || !loaded.BalanceUpdatedAt.Equal(at) {
		t.Errorf("balance updated at = %v, want %v", loaded.BalanceUpdatedAt, at)
	}

	usd := money.Money{Minor: 1, Currency: mustCurrency(t, "USD")}
	if err := SetBalance(ctx, d, alpha, usd, at); !errors.Is(err, repo.ErrCurrencyMismatch) {
		t.Errorf("SetBalance(USD) = %v, want ErrCurrencyMismatch", err)
	}
	loaded, _, err = Get(ctx, d, alpha)
	if err != nil || loaded.Balance != eur(1234) {
		t.Errorf("balance after a mismatch = %v, %v", loaded.Balance, err)
	}

	if err := Delete(ctx, d, beta); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := Get(ctx, d, beta); err != nil || ok {
		t.Errorf("Get after Delete = %v, %v", ok, err)
	}
	if err := Delete(ctx, d, beta); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestDeletingAGroupKeepsItsAccountsWithoutAGroup(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	group, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: "Group", SortOrder: 0})
	if err != nil {
		t.Fatalf("Create group: %v", err)
	}
	new := newAccount("Account")
	new.GroupID = &group
	id, err := Create(ctx, d, new)
	if err != nil {
		t.Fatalf("Create account: %v", err)
	}

	if err := accountgroups.Delete(ctx, d, group); err != nil {
		t.Fatalf("Delete group: %v", err)
	}

	loaded, ok, err := Get(ctx, d, id)
	if err != nil || !ok {
		t.Fatalf("Get: %v, %v", ok, err)
	}
	if loaded.GroupID != nil {
		t.Errorf("group id = %v, want nil", loaded.GroupID)
	}
}

func TestDeletingAnAccountCascadesButKeepsDocuments(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	id, err := Create(ctx, d, newAccount("Account"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var transactionID int64
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		result, err := conn.ExecContext(
			ctx,
			"INSERT INTO transactions "+
				"(account_id, booking_date, amount_minor, currency, partner_name, purpose, import_hash, created_at, is_new) "+
				"VALUES (?1, '2024-01-01', -500, 'EUR', 'Partner', 'Purpose', 'hash', '2024-01-01T00:00:00Z', 0)",
			int64(id),
		)
		if err != nil {
			return err
		}
		transactionID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(
			ctx,
			"INSERT INTO pending_bookings\n"+
				"                    (account_id, booking_date, amount_minor, currency, fetched_at)\n"+
				"                 VALUES (?1, '2024-01-02', 100, 'EUR', '2024-01-02T00:00:00Z')",
			int64(id),
		); err != nil {
			return err
		}
		if _, err := conn.ExecContext(
			ctx,
			"INSERT INTO deleted_bookings (account_id, import_hash, deleted_at)\n"+
				"                 VALUES (?1, 'gone', '2024-01-02T00:00:00Z')",
			int64(id),
		); err != nil {
			return err
		}
		_, err = conn.ExecContext(
			ctx,
			"INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)\n"+
				"                 VALUES ('a.pdf', 'a.pdf', 'application/pdf', '2024-01-02T00:00:00Z', ?1)",
			transactionID,
		)
		return err
	})
	if err != nil {
		t.Fatalf("seed bookings: %v", err)
	}

	if err := Delete(ctx, d, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if got := count(t, d, "SELECT count(*) FROM transactions"); got != 0 {
		t.Errorf("transactions = %d", got)
	}
	if got := count(t, d, "SELECT count(*) FROM pending_bookings"); got != 0 {
		t.Errorf("pending bookings = %d", got)
	}
	if got := count(t, d, "SELECT count(*) FROM deleted_bookings"); got != 0 {
		t.Errorf("deleted bookings = %d", got)
	}
	if got := count(t, d, "SELECT count(*) FROM documents"); got != 1 {
		t.Errorf("documents = %d", got)
	}

	var linked *int64
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT transaction_id FROM documents").Scan(&linked)
	})
	if err != nil {
		t.Fatalf("read linked transaction: %v", err)
	}
	if linked != nil {
		t.Errorf("linked transaction = %v, want nil", *linked)
	}
}

func TestSetAccountTypeChangesTheStoredTypeAndAuditsOnlyAChange(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	id := account(t, d, model.AccountTypeChecking)
	before := len(entries(t, d))

	changed, err := SetAccountType(ctx, d, id, model.AccountTypeLoan)
	if err != nil || !changed {
		t.Fatalf("SetAccountType(Loan) = %v, %v", changed, err)
	}
	loaded, _, err := Get(ctx, d, id)
	if err != nil || loaded.AccountType != model.AccountTypeLoan {
		t.Errorf("account type = %v, %v", loaded.AccountType, err)
	}
	if got := len(entries(t, d)); got != before+1 {
		t.Errorf("audit entries = %d, want %d", got, before+1)
	}

	changed, err = SetAccountType(ctx, d, id, model.AccountTypeLoan)
	if err != nil {
		t.Fatalf("SetAccountType(Loan) again: %v", err)
	}
	if changed {
		t.Error("the same type changes nothing")
	}
	if got := len(entries(t, d)); got != before+1 {
		t.Errorf("audit entries = %d, want %d; the same type writes no second entry", got, before+1)
	}

	entry := entries(t, d)[0]
	if entry.Action != "CHANGE_ACCOUNT_TYPE" || entry.Actor != audit.ActorUser {
		t.Errorf("audit entry = %+v", entry)
	}
	if want := "Account " + itoa(int64(id)) + ": checking -> loan"; entry.Details != want {
		t.Errorf("details = %q, want %q", entry.Details, want)
	}
}

func TestSetAccountTypeRejectsAnUnknownAccount(t *testing.T) {
	d := database(t)
	if _, err := SetAccountType(context.Background(), d, model.AccountID(999999), model.AccountTypeLoan); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("SetAccountType on an unknown account = %v, want ErrNotFound", err)
	}
}

func TestSetCurrencyChangesCurrencyAndBalanceAndAuditsItOnce(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	id := account(t, d, model.AccountTypeChecking)
	before := len(entries(t, d))
	chf := mustCurrency(t, "CHF")

	changed, err := SetCurrency(ctx, d, id, chf, money.Money{Minor: 8_850, Currency: chf}, time.Now().UTC())
	if err != nil || !changed {
		t.Fatalf("SetCurrency = %v, %v", changed, err)
	}

	loaded, _, err := Get(ctx, d, id)
	if err != nil || loaded.Currency != chf || loaded.Balance.Minor != 8_850 {
		t.Errorf("stored = %v, %v", loaded, err)
	}
	if got := len(entries(t, d)); got != before+1 {
		t.Errorf("audit entries = %d, want %d", got, before+1)
	}
	entry := entries(t, d)[0]
	if entry.Action != "CORRECT_ACCOUNT_CURRENCY" || entry.Actor != audit.ActorUser {
		t.Errorf("audit entry = %+v", entry)
	}
	if want := "Account " + itoa(int64(id)) + ": EUR -> CHF"; entry.Details != want {
		t.Errorf("details = %q, want %q", entry.Details, want)
	}

	changed, err = SetCurrency(ctx, d, id, chf, money.Money{Minor: 8_850, Currency: chf}, time.Now().UTC())
	if err != nil {
		t.Fatalf("SetCurrency again: %v", err)
	}
	if changed {
		t.Error("the same currency changes nothing")
	}
	if got := len(entries(t, d)); got != before+1 {
		t.Errorf("audit entries = %d, want %d; the same currency writes no second entry", got, before+1)
	}
}

func TestSetCurrencyRejectsUnknownAccountsAndMismatchedBalances(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	chf := mustCurrency(t, "CHF")
	usd := mustCurrency(t, "USD")

	if _, err := SetCurrency(ctx, d, model.AccountID(999999), chf, money.Money{Minor: 1, Currency: chf}, time.Now().UTC()); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("SetCurrency on an unknown account = %v, want ErrNotFound", err)
	}
	id := account(t, d, model.AccountTypeChecking)
	if _, err := SetCurrency(ctx, d, id, chf, money.Money{Minor: 1, Currency: usd}, time.Now().UTC()); !errors.Is(err, repo.ErrCurrencyMismatch) {
		t.Errorf("SetCurrency with a mismatched balance = %v, want ErrCurrencyMismatch", err)
	}
}

func TestEveryAccountMutationWritesOneAuditEntryWithoutTheIBAN(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	iban := "DE89370400440532013000"

	new := newAccount("Audit account")
	new.IBAN = &iban
	id, err := Create(ctx, d, new)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wantActions := []string{"account.create"}

	renamed := "Audit account 2"
	if err := Rename(ctx, d, id, &renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	wantActions = append(wantActions, "account.rename")

	group, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: "Group", SortOrder: 0})
	if err != nil {
		t.Fatalf("Create group: %v", err)
	}
	wantActions = append(wantActions, "account_group.create")
	if err := SetGroup(ctx, d, id, &group); err != nil {
		t.Fatalf("SetGroup: %v", err)
	}
	wantActions = append(wantActions, "account.set_group")

	if err := SetBalance(ctx, d, id, eur(1234), time.Now().UTC()); err != nil {
		t.Fatalf("SetBalance: %v", err)
	}
	wantActions = append(wantActions, "account.set_balance")

	if err := SetBalanceForImport(ctx, d, id, eur(4321), time.Now().UTC()); err != nil {
		t.Fatalf("SetBalanceForImport: %v", err)
	}

	if _, err := SetAccountType(ctx, d, id, model.AccountTypeSavings); err != nil {
		t.Fatalf("SetAccountType: %v", err)
	}
	wantActions = append(wantActions, "CHANGE_ACCOUNT_TYPE")

	chf := mustCurrency(t, "CHF")
	if _, err := SetCurrency(ctx, d, id, chf, money.Money{Minor: 1, Currency: chf}, time.Now().UTC()); err != nil {
		t.Fatalf("SetCurrency: %v", err)
	}
	wantActions = append(wantActions, "CORRECT_ACCOUNT_CURRENCY")

	if err := Delete(ctx, d, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	wantActions = append(wantActions, "account.delete")

	recent := entries(t, d)
	gotActions := make([]string, 0, len(recent))
	for _, entry := range recent {
		gotActions = append(gotActions, entry.Action)
	}
	slices.Reverse(gotActions)
	if !slices.Equal(gotActions, wantActions) {
		t.Errorf("audit actions = %v, want %v", gotActions, wantActions)
	}
	details := make(map[string]string)
	for _, entry := range recent {
		if entry.Actor != audit.ActorUser {
			t.Errorf("audit actor = %q", entry.Actor)
		}
		if strings.Contains(entry.Details, iban) {
			t.Errorf("an audit entry leaked an IBAN: %q", entry.Details)
		}
		details[entry.Action] = entry.Details
	}
	for action, want := range map[string]string{
		"account.create":           "id=" + itoa(int64(id)) + " name=Audit account",
		"account.rename":           "id=" + itoa(int64(id)) + " name=Audit account 2",
		"account.set_group":        "id=" + itoa(int64(id)),
		"account.set_balance":      "id=" + itoa(int64(id)),
		"CHANGE_ACCOUNT_TYPE":      "Account " + itoa(int64(id)) + ": checking -> savings",
		"CORRECT_ACCOUNT_CURRENCY": "Account " + itoa(int64(id)) + ": EUR -> CHF",
		"account.delete":           "id=" + itoa(int64(id)),
	} {
		if got := details[action]; got != want {
			t.Errorf("%s details = %q, want %q", action, got, want)
		}
	}
}

func TestNotFoundCasesForAccountMutations(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	missing := model.AccountID(999999)
	name := "Missing"
	group := model.AccountGroupID(1)

	if _, _, err := Get(ctx, d, missing); err != nil {
		t.Errorf("Get on an unknown account = %v", err)
	}
	if err := Rename(ctx, d, missing, &name); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("Rename on an unknown account = %v", err)
	}
	if err := SetGroup(ctx, d, missing, &group); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("SetGroup on an unknown account = %v", err)
	}
	if err := SetBalance(ctx, d, missing, eur(1), time.Now().UTC()); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("SetBalance on an unknown account = %v", err)
	}
	if err := SetBalanceForImport(ctx, d, missing, eur(1), time.Now().UTC()); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("SetBalanceForImport on an unknown account = %v", err)
	}
	if err := Delete(ctx, d, missing); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("Delete on an unknown account = %v", err)
	}
}

func mustCurrency(t *testing.T, code string) money.Currency {
	t.Helper()

	currency, err := money.NewCurrency(code)
	if err != nil {
		t.Fatalf("NewCurrency(%q): %v", code, err)
	}
	return currency
}
