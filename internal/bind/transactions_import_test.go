package bind

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func importIntPointer(value int) *int { return &value }

func importedBooking(t *testing.T, year int, month time.Month, day int, minor int64, partner string) imports.ImportedBooking {
	t.Helper()
	date, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d)", year, month, day)
	}
	name := partner
	return imports.ImportedBooking{
		Date:        date,
		Amount:      decimal.New(minor, 2),
		PartnerName: &name,
	}
}

func importPreviewFor(entries []transactionsview.ImportEntry, failures []transactionsview.ImportFailure) *transactionsview.ImportPreview {
	accountID := model.AccountID(1)
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, entry.File)
	}
	return &transactionsview.ImportPreview{
		Files:     files,
		Entries:   entries,
		Failures:  failures,
		AccountID: &accountID,
	}
}

func TestImportFileRowsShowEachFilePeriodWarningAndFailure(t *testing.T) {
	csv := &imports.CsvLayout{
		Delimiter:   ';',
		CharsetName: "UTF-8",
		HeaderLine:  1,
		Headers:     []string{"Buchung", "Betrag"},
		Mapping: imports.CsvMapping{
			Date:   importIntPointer(0),
			Amount: importIntPointer(1),
		},
	}
	ibanText := "DE89370400440532013000"
	statement := imports.ParsedStatement{
		Format: imports.FormatCSV,
		Bookings: []imports.ImportedBooking{
			importedBooking(t, 2026, time.October, 2, -2_000, "Stadtwerke"),
			importedBooking(t, 2026, time.October, 15, -3_000, "Obststand"),
		},
		AccountIBAN:    &ibanText,
		Accounts:       []string{ibanText},
		Skipped:        []string{"line 3"},
		PendingSkipped: 2,
		Csv:            csv,
	}
	preview := importPreviewFor(
		[]transactionsview.ImportEntry{{File: "/tmp/oktober.csv", Statement: statement}},
		[]transactionsview.ImportFailure{{FileName: "kaputt.csv", Message: "Unreadable"}},
	)

	rows := transactionsImportFileRows(preview, i18n.En, nil)

	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Name != "oktober.csv" {
		t.Fatalf("name = %q", rows[0].Name)
	}
	if rows[0].CountText != "2 bookings" {
		t.Fatalf("count = %q", rows[0].CountText)
	}
	if rows[0].PeriodText != "02.10.2026–15.10.2026" {
		t.Fatalf("period = %q", rows[0].PeriodText)
	}
	if !strings.Contains(rows[0].WarningText, "2 pending left out") {
		t.Fatalf("warning = %q", rows[0].WarningText)
	}
	if !strings.Contains(rows[0].WarningText, "1 unreadable") {
		t.Fatalf("warning = %q", rows[0].WarningText)
	}
	if rows[0].Failure {
		t.Fatal("a readable file was marked as a failure")
	}
	if !rows[1].Failure || rows[1].WarningText != "Unreadable" {
		t.Fatalf("failure row = %+v", rows[1])
	}
	if transactionsImportHasBalances(preview) {
		t.Fatal("a statement without balances reported balances")
	}
}

func TestImportCSVFieldsUseTheHeadersAndOneBasedIndices(t *testing.T) {
	csv := &imports.CsvLayout{
		Delimiter:   ';',
		CharsetName: "UTF-8",
		HeaderLine:  1,
		Headers:     []string{"Buchung", "", "Betrag"},
		Mapping: imports.CsvMapping{
			Date:   importIntPointer(0),
			Amount: importIntPointer(2),
		},
	}
	statement := imports.ParsedStatement{
		Format:   imports.FormatCSV,
		Bookings: []imports.ImportedBooking{importedBooking(t, 2026, time.October, 2, -2_000, "Stadtwerke")},
		Csv:      csv,
	}
	preview := importPreviewFor(
		[]transactionsview.ImportEntry{{File: "/tmp/a.csv", Statement: statement}},
		nil,
	)

	fields := transactionsImportCSVFields(preview, i18n.En)

	if len(fields) != 7 {
		t.Fatalf("fields = %+v", fields)
	}
	if fields[0].Field != "date" || fields[0].Index != 1 {
		t.Fatalf("date field = %+v", fields[0])
	}
	if len(fields[0].Options) != 4 {
		t.Fatalf("options = %+v", fields[0].Options)
	}
	if fields[0].Options[0] != i18n.Text(i18n.En, i18n.NoneDash) ||
		fields[0].Options[1] != "Buchung" ||
		fields[0].Options[2] != "Column 2" ||
		fields[0].Options[3] != "Betrag" {
		t.Fatalf("options = %+v", fields[0].Options)
	}
	if fields[1].Field != "amount" || fields[1].Index != 3 {
		t.Fatalf("amount field = %+v", fields[1])
	}
}

func TestApplyImportSetsEveryRustField(t *testing.T) {
	csv := &imports.CsvLayout{
		Headers: []string{"Date", "Amount"},
		Mapping: imports.CsvMapping{
			Date:   importIntPointer(0),
			Amount: importIntPointer(1),
		},
	}
	balance := decimal.New(10_000, 2)
	bookings := []imports.ImportedBooking{importedBooking(t, 2026, time.October, 2, -2_000, "Stadtwerke")}
	bookings[0].BalanceAfter = &balance
	statement := imports.ParsedStatement{
		Format:   imports.FormatCSV,
		Bookings: bookings,
		Csv:      csv,
	}
	preview := importPreviewFor(
		[]transactionsview.ImportEntry{{File: "/tmp/a.csv", Statement: statement}},
		nil,
	)
	preview.BalanceUpdateAllowed = false

	state := transactionsview.NewState(i18n.En)
	state.Accounts = []accountsview.AccountItem{{
		ID:       model.AccountID(1),
		IBAN:     "DE89370400440532013000",
		Currency: money.EUR,
	}}
	state.ImportPreview = preview

	dto := TransactionsDTO{}
	transactionsApplyImport(&dto, *state, i18n.En)

	if !dto.ImportOpen {
		t.Fatal("the import dialog is closed")
	}
	if dto.ImportTitle != i18n.Format(i18n.En, i18n.ImportFile, i18n.Str("a.csv")) {
		t.Fatalf("title = %q", dto.ImportTitle)
	}
	if dto.ImportSummary != "" {
		t.Fatalf("summary = %q", dto.ImportSummary)
	}
	if len(dto.ImportFiles) != 1 || dto.ImportFiles[0].Name != "a.csv" {
		t.Fatalf("files = %+v", dto.ImportFiles)
	}
	if len(dto.ImportCSVFields) != 7 {
		t.Fatalf("csv fields = %+v", dto.ImportCSVFields)
	}
	if dto.ImportCSVHint != "" {
		t.Fatalf("csv hint = %q", dto.ImportCSVHint)
	}
	if len(dto.ImportAccountOptions) != 1 || dto.ImportAccountIndex != 0 {
		t.Fatalf("accounts = %+v / %d", dto.ImportAccountOptions, dto.ImportAccountIndex)
	}
	if dto.ImportBalanceAllowed || dto.ImportUpdateBalance {
		t.Fatalf("balance flags = %v / %v", dto.ImportBalanceAllowed, dto.ImportUpdateBalance)
	}
	if dto.ImportBalanceText != i18n.Text(i18n.En, i18n.SetAccountBalance) {
		t.Fatalf("balance text = %q", dto.ImportBalanceText)
	}
	if dto.ImportNewerText != i18n.Text(i18n.En, i18n.NewerBookingsUnchanged) {
		t.Fatalf("newer text = %q", dto.ImportNewerText)
	}
	if dto.Importing {
		t.Fatal("a fresh preview is importing")
	}
	if dto.ImportConfirmText != i18n.Format(i18n.En, i18n.ImportCount, i18n.Int(1)) {
		t.Fatalf("confirm text = %q", dto.ImportConfirmText)
	}
	if !dto.ImportConfirmEnabled {
		t.Fatal("the confirm button is disabled")
	}

	state.IsImporting = true
	dto = TransactionsDTO{}
	transactionsApplyImport(&dto, *state, i18n.En)
	if !dto.Importing || dto.ImportConfirmText != i18n.Text(i18n.En, i18n.Importing) {
		t.Fatalf("importing = %v / %q", dto.Importing, dto.ImportConfirmText)
	}
	if dto.ImportConfirmEnabled {
		t.Fatal("a running import can be confirmed again")
	}

	state.ImportPreview = nil
	dto = TransactionsDTO{}
	transactionsApplyImport(&dto, *state, i18n.En)
	if dto.ImportOpen {
		t.Fatal("no preview opened the import dialog")
	}
}

// allowedTransactionParameters lists every exported method that takes an
// argument, with the exact parameter names Rust's callbacks pass. Any method
// outside this list must take no arguments, so no path, file name, directory or
// file contents can reach a dialog from JavaScript.
var allowedTransactionParameters = map[string][]string{
	"ImportAccountChanged": {"index"},
	"ImportCSVChanged":     {"field", "value"},
	"ConfirmImport":        {"updateBalance"},
	"Search":               {"query"},
	"PeriodChanged":        {"index"},
	"ApplyRange":           {"from", "to"},
	"CategoryChanged":      {"index"},
	"DirectionChanged":     {"index"},
	"AccountChanged":       {"index"},
	"ApplyAmount":          {"minimum", "maximum"},
	"ToggleReapplyGroup":   {"from", "to"},
	"SaveRule":             {"pattern", "categoryIndex"},
	"OpenDetail":           {"id"},
	"ToggleSelect":         {"id"},
	"SelectRange":          {"id"},
	"ShowPartner":          {"partner"},
	"OpenDelete":           {"id"},
	"ConfirmDelete":        {"id"},
	"BatchCategory":        {"index"},
	"BatchTax":             {"index"},
	"AddSubmit":            {"accountIndex", "expense", "date", "amount", "partner", "purpose", "categoryIndex"},
	"DetailCategory":       {"index"},
	"DetailTax":            {"index"},
	"DetailShowPartner":    {"partner"},
	"PanelSelect":          {"kind", "groupKind", "id", "accountType"},
	"PanelToggle":          {"kind", "groupKind", "id", "accountType"},
	"PanelMarkRead":        {"kind", "groupKind", "id", "accountType"},
	"PanelNewGroup":        {"name"},
	"PanelRenameGroup":     {"id", "name"},
	"PanelDeleteGroup":     {"id"},
	"PanelMoveGroup":       {"id", "up"},
	"PanelMoveAccount":     {"accountID", "groupID"},
	"PanelFilterChanged":   {"text"},
}

func TestTransactionsBindingsNeverTakeAPath(t *testing.T) {
	actualNames := transactionParameterNames(t)
	typ := reflect.TypeOf(&Transactions{})
	methods := make(map[string]bool, typ.NumMethod())
	for index := 0; index < typ.NumMethod(); index++ {
		method := typ.Method(index)
		methods[method.Name] = true
		allowed, listed := allowedTransactionParameters[method.Name]
		names := actualNames[method.Name]
		if !listed && method.Type.NumIn() != 1 {
			// The receiver is the only input of a no-argument method.
			t.Fatalf("%s takes arguments but is not in the allowed list", method.Name)
		}
		if len(names) != method.Type.NumIn()-1 {
			t.Fatalf("%s parameter names = %v, but reflection sees %d", method.Name, names, method.Type.NumIn()-1)
		}
		for _, name := range names {
			lower := strings.ToLower(name)
			for _, forbidden := range []string{"path", "file", "dir", "folder", "document", "content"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s parameter %q could carry a path", method.Name, name)
				}
			}
		}
		if !listed {
			continue
		}
		if len(allowed) != len(names) {
			t.Fatalf("%s parameters = %v, want %v", method.Name, names, allowed)
		}
		for position, name := range allowed {
			if names[position] != name {
				t.Fatalf("%s parameter %d = %q, want %q", method.Name, position, names[position], name)
			}
			parameter := method.Type.In(position + 1)
			switch parameter.Kind() {
			case reflect.Int, reflect.Bool, reflect.String:
			default:
				t.Fatalf("%s parameter %q has type %s", method.Name, name, parameter)
			}
		}
	}

	for _, name := range []string{"OpenImport", "ExportCSV", "AttachReceipt"} {
		if !methods[name] {
			t.Fatalf("%s is not exported", name)
		}
		if allowed, listed := allowedTransactionParameters[name]; listed && len(allowed) > 0 {
			t.Fatalf("%s must take no arguments", name)
		}
		if len(actualNames[name]) != 0 {
			t.Fatalf("%s takes %v", name, actualNames[name])
		}
	}
}

// transactionParameterNames reads the declared names from the service source.
// Reflection exposes a method's parameter types but not their names.
func transactionParameterNames(t *testing.T) map[string][]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "transactions.go", nil, 0)
	if err != nil {
		t.Fatalf("parse transactions.go: %v", err)
	}
	names := make(map[string][]string)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) == 0 {
			continue
		}
		if receiverType(function.Recv.List[0].Type) != "Transactions" {
			continue
		}
		names[function.Name.Name] = parameterNamesOf(function.Type.Params)
	}
	return names
}

func receiverType(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.StarExpr:
		return receiverType(typed.X)
	case *ast.Ident:
		return typed.Name
	default:
		return ""
	}
}

func parameterNamesOf(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	names := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}
