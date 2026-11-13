package shell

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

// fakeFileDialogs is the test seam. It records what the dialog was asked and
// may run a callback before answering.
type fakeFileDialogs struct {
	mu sync.Mutex

	statements       []string
	statementsResult bool
	statementsTitle  string
	statementsCalls  int
	onStatements     func()

	receipts       []string
	receiptsResult bool
	receiptsTitle  string
	receiptsCalls  int
	onReceipts     func()

	savePath   string
	saveResult bool
	saveTitle  string
	saveName   string
	saveCalls  int

	filePath        string
	fileResult      bool
	fileTitle       i18n.Key
	fileDefaultName string
	fileCalls       int
	onFile          func()

	rulesPath   string
	rulesResult bool
	rulesTitle  string
	rulesName   string
	rulesCalls  int

	ruleFile       string
	ruleFileResult bool
	ruleFileTitle  string
	ruleFileCalls  int
}

func (f *fakeFileDialogs) PickStatements(title string) ([]string, bool) {
	f.mu.Lock()
	f.statementsTitle = title
	f.statementsCalls++
	callback := f.onStatements
	f.mu.Unlock()
	if callback != nil {
		callback()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.statements...), f.statementsResult
}

func (f *fakeFileDialogs) PickReceipts(title string) ([]string, bool) {
	f.mu.Lock()
	f.receiptsTitle = title
	f.receiptsCalls++
	callback := f.onReceipts
	f.mu.Unlock()
	if callback != nil {
		callback()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.receipts...), f.receiptsResult
}

func (f *fakeFileDialogs) SaveCSV(title string, defaultName string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveTitle = title
	f.saveName = defaultName
	f.saveCalls++
	return f.savePath, f.saveResult
}

func (f *fakeFileDialogs) SaveFile(title i18n.Key, defaultName string) (string, bool) {
	f.mu.Lock()
	f.fileTitle = title
	f.fileDefaultName = defaultName
	f.fileCalls++
	callback := f.onFile
	f.mu.Unlock()
	if callback != nil {
		callback()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.filePath, f.fileResult
}

func (f *fakeFileDialogs) counts() (statements int, receipts int, saves int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statementsCalls, f.receiptsCalls, f.saveCalls
}

func (f *fakeFileDialogs) SaveRules(title string, defaultName string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rulesTitle = title
	f.rulesName = defaultName
	f.rulesCalls++
	return f.rulesPath, f.rulesResult
}

func (f *fakeFileDialogs) PickRuleFile(title string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ruleFileTitle = title
	f.ruleFileCalls++
	return f.ruleFile, f.ruleFileResult
}

func importFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "inputs", "import", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return path
}

func waitForDialogJob(t *testing.T, session *Session) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if !session.transactionsWork.Active() && !session.jobs.Active() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the dialog job did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func accountIndexIn(state transactionsview.TransactionsState, id model.AccountID) int {
	for index, account := range state.Accounts {
		if account.ID == id {
			return index
		}
	}
	return -1
}

func TestTransactionsOpenWithImportOpensTheDialogAfterTheLoad(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{
		statements:       []string{importFixture(t, "n26_preset_2026_10.csv")},
		statementsResult: true,
	}
	session.SetFileDialogs(fake)
	events, remove := listenForChanges(session)
	defer remove()

	session.TransactionsOpenWithImport()
	waitForTransactions(t, session, events, "the import dialog after the load", func(state transactionsview.TransactionsState) bool {
		return state.ImportPreview != nil
	})
	if screen := session.CurrentScreen(); screen != "transactions" {
		t.Fatalf("screen = %q", screen)
	}
	if args := session.NavCurrent().Transactions; !reflect.DeepEqual(args, transactionsview.StartArgs{}) {
		t.Fatalf("navigation args = %+v", args)
	}
	if session.NavLen() != 0 {
		t.Fatalf("the menu action left %d back entries", session.NavLen())
	}
}

func TestImportCSVThroughTheDialogSeamRemapsChoosesAnAccountAndConfirms(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{
		statements:       []string{importFixture(t, "n26_preset_2026_10.csv")},
		statementsResult: true,
	}
	session.SetFileDialogs(fake)

	beforeRows := len(session.TransactionsState().Transactions)
	beforeAudit := countTransactionsAudit(t, session, "IMPORT_STATEMENT")

	session.TransactionsOpenImport()
	waitForDialogJob(t, session)

	state := session.TransactionsState()
	if state.ImportPreview == nil {
		t.Fatalf("no preview was opened: %+v", state.Error)
	}
	if got := state.ImportPreview.BookingCount(); got != 2 {
		t.Fatalf("booking count = %d, want 2", got)
	}
	if state.ImportPreview.AccountID != nil {
		t.Fatalf("a CSV without an IBAN picked an account: %+v", state.ImportPreview.AccountID)
	}
	if len(state.ImportPreview.Entries) != 1 || state.ImportPreview.Entries[0].Statement.Csv == nil {
		t.Fatalf("CSV layout is missing: %+v", state.ImportPreview)
	}
	csv := state.ImportPreview.Entries[0].Statement.Csv

	options := ImportCSVOptions(csv, i18n.En)
	dateHeader := ""
	if csv.Mapping.Date != nil {
		dateHeader = options[*csv.Mapping.Date+1]
	}
	if dateHeader == "" {
		t.Fatal("the CSV has no date column")
	}
	session.TransactionsImportCSVChanged("date", dateHeader)
	waitForDialogJob(t, session)
	if state := session.TransactionsState(); state.ImportPreview == nil {
		t.Fatal("the remap closed the preview")
	}

	index := accountIndexIn(session.TransactionsState(), seed.checking)
	if index < 0 {
		t.Fatal("the checking account is not in the import choices")
	}
	session.TransactionsImportAccountChanged(index)
	waitForDialogJob(t, session)
	state = session.TransactionsState()
	if state.ImportPreview == nil || state.ImportPreview.AccountID == nil {
		t.Fatalf("the account was not picked: %+v", state.ImportPreview)
	}
	if *state.ImportPreview.AccountID != seed.checking {
		t.Fatalf("account = %d, want %d", *state.ImportPreview.AccountID, seed.checking)
	}

	session.TransactionsConfirmImport(true)
	waitForDialogJob(t, session)
	state = session.TransactionsState()
	if state.ImportPreview != nil {
		t.Fatal("the preview stayed open after confirming")
	}
	if state.Message == nil {
		t.Fatal("a confirmed import reported no summary")
	}
	if len(state.Transactions) <= beforeRows {
		t.Fatalf("bookings = %d, want more than %d", len(state.Transactions), beforeRows)
	}
	if got := countTransactionsAudit(t, session, "IMPORT_STATEMENT"); got != beforeAudit+1 {
		t.Fatalf("import audit entries = %d, want %d", got, beforeAudit+1)
	}
	if state.Period != transactionsview.PeriodAll {
		t.Fatalf("period = %v, want all", state.Period)
	}
}

func TestImportMT940AndCAMTFixturesThroughTheDialogSeam(t *testing.T) {
	for _, fixture := range []string{"mt940_two_statements.sta", "camt052_report.xml"} {
		t.Run(fixture, func(t *testing.T) {
			session, _ := newLoadedTransactionsSession(t)
			fake := &fakeFileDialogs{
				statements:       []string{importFixture(t, fixture)},
				statementsResult: true,
			}
			session.SetFileDialogs(fake)

			beforeAudit := countTransactionsAudit(t, session, "IMPORT_STATEMENT")
			session.TransactionsOpenImport()
			waitForDialogJob(t, session)
			state := session.TransactionsState()
			if state.ImportPreview == nil {
				t.Fatalf("no preview for %s: %+v", fixture, state.Error)
			}
			if state.ImportPreview.AccountID == nil {
				t.Fatalf("%s did not match the checking account", fixture)
			}
			wantBookings := state.ImportPreview.BookingCount()
			if wantBookings == 0 {
				t.Fatalf("%s has no bookings", fixture)
			}

			session.TransactionsConfirmImport(true)
			waitForDialogJob(t, session)
			state = session.TransactionsState()
			if state.Message == nil {
				t.Fatal("a confirmed import reported no summary")
			}
			if got := countTransactionsAudit(t, session, "IMPORT_STATEMENT"); got != beforeAudit+1 {
				t.Fatalf("import audit entries = %d, want %d", got, beforeAudit+1)
			}
		})
	}
}

func TestCancellingAtTheDialogOrAnEmptySelectionChangesNothing(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{}
	session.SetFileDialogs(fake)

	beforeRows := len(session.TransactionsState().Transactions)
	beforeAudit := countTransactionsAudit(t, session, "IMPORT_STATEMENT")

	session.TransactionsOpenImport()
	waitForDialogJob(t, session)

	fake.mu.Lock()
	fake.statementsResult = true
	fake.statements = nil
	fake.mu.Unlock()
	session.TransactionsOpenImport()
	waitForDialogJob(t, session)

	state := session.TransactionsState()
	if state.ImportPreview != nil {
		t.Fatal("a cancelled dialog opened a preview")
	}
	if len(state.Transactions) != beforeRows {
		t.Fatalf("rows = %d, want %d", len(state.Transactions), beforeRows)
	}
	if got := countTransactionsAudit(t, session, "IMPORT_STATEMENT"); got != beforeAudit {
		t.Fatalf("audit entries = %d, want %d", got, beforeAudit)
	}
	if statements, _, _ := fake.counts(); statements != 2 {
		t.Fatalf("dialog calls = %d, want 2", statements)
	}
}

func TestCancellingThePreviewChangesNothing(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{
		statements:       []string{importFixture(t, "n26_preset_2026_10.csv")},
		statementsResult: true,
	}
	session.SetFileDialogs(fake)

	session.TransactionsOpenImport()
	waitForDialogJob(t, session)
	if session.TransactionsState().ImportPreview == nil {
		t.Fatal("no preview")
	}
	beforeRows := len(session.TransactionsState().Transactions)
	beforeAudit := countTransactionsAudit(t, session, "IMPORT_STATEMENT")

	session.TransactionsCancelImport()
	state := session.TransactionsState()
	if state.ImportPreview != nil {
		t.Fatal("the preview stayed open")
	}
	if len(state.Transactions) != beforeRows {
		t.Fatalf("rows = %d, want %d", len(state.Transactions), beforeRows)
	}
	if got := countTransactionsAudit(t, session, "IMPORT_STATEMENT"); got != beforeAudit {
		t.Fatalf("audit entries = %d, want %d", got, beforeAudit)
	}
}

func TestExportCSVWritesToTheChosenTemporaryPath(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	output := filepath.Join(t.TempDir(), "export.csv")
	fake := &fakeFileDialogs{savePath: output, saveResult: true}
	session.SetFileDialogs(fake)

	rows := len(session.TransactionsState().Transactions)
	session.TransactionsExportCSV()
	waitForDialogJob(t, session)

	data, err := os.ReadFile(output) // #nosec G304 -- the test chose this temporary path.
	if err != nil {
		t.Fatalf("read the exported CSV: %v", err)
	}
	if !strings.HasPrefix(string(data), "\ufeff") {
		t.Fatal("the export has no UTF-8 BOM")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\r\n"), "\r\n")
	if len(lines) != rows+1 {
		t.Fatalf("export lines = %d, want %d", len(lines), rows+1)
	}
	if !strings.Contains(lines[0], i18n.Text(i18n.En, i18n.ColDate)) {
		t.Fatalf("header = %q", lines[0])
	}

	fake.mu.Lock()
	title := fake.saveTitle
	name := fake.saveName
	fake.mu.Unlock()
	if title != i18n.Text(i18n.En, i18n.ExportTransactions) {
		t.Fatalf("dialog title = %q", title)
	}
	if name != "transactions-2026-10-15.csv" {
		t.Fatalf("default name = %q", name)
	}
	if state := session.TransactionsState(); state.Message == nil {
		t.Fatal("the export reported no message")
	}
}

func TestAttachReceiptCopiesAndLinksTheChosenFile(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	transaction := session.TransactionsState().Transactions[0]
	session.TransactionsOpenDetail(transaction.ID)
	waitForDialogJob(t, session)
	if session.TransactionsState().Selected == nil {
		t.Fatal("the detail panel did not open")
	}

	receipt := filepath.Join("..", "..", "..", "testdata", "pdf", "text-receipt.pdf")
	fake := &fakeFileDialogs{receipts: []string{receipt}, receiptsResult: true}
	session.SetFileDialogs(fake)
	session.TransactionsAttachReceipt()
	waitForDialogJob(t, session)

	state := session.TransactionsState()
	if len(state.SelectedDocuments) != 1 {
		t.Fatalf("documents = %+v", state.SelectedDocuments)
	}
	document := state.SelectedDocuments[0]
	if document.FileName != "text-receipt.pdf" {
		t.Fatalf("document name = %q", document.FileName)
	}
	if document.TransactionID == nil || *document.TransactionID != transaction.ID {
		t.Fatalf("document link = %+v", document.TransactionID)
	}
	dir := session.transactionsDocumentsDirLocked()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read documents folder: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("stored files = %d, want 1", len(entries))
	}
	fake.mu.Lock()
	title := fake.receiptsTitle
	fake.mu.Unlock()
	if title != i18n.Text(i18n.En, i18n.AttachReceipt) {
		t.Fatalf("dialog title = %q", title)
	}
}

func TestAttachReceiptWithoutASelectionDoesNothing(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{
		receipts:       []string{filepath.Join("..", "..", "..", "testdata", "pdf", "text-receipt.pdf")},
		receiptsResult: true,
	}
	session.SetFileDialogs(fake)

	session.TransactionsAttachReceipt()
	if receipts, _, _ := fake.counts(); receipts != 0 {
		t.Fatalf("the dialog opened %d times without a selection", receipts)
	}
	if state := session.TransactionsState(); state.Selected != nil {
		t.Fatal("a detail panel opened without a selection")
	}
}

func TestTheImportDialogRunsWithoutTheSessionMutexAndInsideTheBusySection(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeFileDialogs{
		statements:       []string{importFixture(t, "n26_preset_2026_10.csv")},
		statementsResult: true,
	}
	fake.onStatements = func() {
		// This would deadlock if the dialog ran under the session mutex.
		_ = session.TransactionsState()
		entered <- struct{}{}
		<-release
	}
	session.SetFileDialogs(fake)

	session.TransactionsOpenImport()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the dialog did not run")
	}

	session.TransactionsPeriodChanged(5)
	close(release)
	waitForDialogJob(t, session)

	state := session.TransactionsState()
	if state.ImportPreview == nil {
		t.Fatal("the preview did not open")
	}
	if state.Period != transactionsview.PeriodThisMonth {
		t.Fatalf("an action started while the dialog was open: period = %v", state.Period)
	}
}

func TestASupersededImportResultIsDropped(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{
		statements:       []string{importFixture(t, "n26_preset_2026_10.csv")},
		statementsResult: true,
	}
	session.SetFileDialogs(fake)

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.TransactionsOpenImport()
	first := <-entered
	session.transactionsWork.End()
	session.TransactionsPeriodChanged(5)
	second := <-entered

	close(second)
	deadline := time.Now().Add(10 * time.Second)
	for session.TransactionsState().Period != transactionsview.PeriodAll {
		if time.Now().After(deadline) {
			t.Fatal("the second load did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	close(first)
	waitForJobs(t, session)

	state := session.TransactionsState()
	if state.ImportPreview != nil {
		t.Fatal("the superseded import published its preview")
	}
	if state.Period != transactionsview.PeriodAll {
		t.Fatalf("the superseded import changed the period to %v", state.Period)
	}
}

func TestTeardownDuringAnImportDropsItAndClosesWhenTheJobEnds(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{
		statements:       []string{importFixture(t, "n26_preset_2026_10.csv")},
		statementsResult: true,
	}
	session.SetFileDialogs(fake)

	entered := make(chan chan struct{}, 1)
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.TransactionsOpenImport()
	gate := <-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the import")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(gate)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the import job did not end")
	}

	requireClosed(t, old, "the old database after the import job ends")
	state := session.TransactionsState()
	if state.ImportPreview != nil {
		t.Fatalf("the teardown kept an import preview: %+v", state.ImportPreview)
	}
}

func TestAnImportJobHoldsTheProfileBusyAndReleasesIt(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		entered <- struct{}{}
		<-release
	}

	session.TransactionsConfirmImport(false)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the import job did not start")
	}
	if !session.profileBusy.Active() {
		t.Fatal("the import job did not hold the profile busy")
	}
	if !session.transactionsWork.Active() {
		t.Fatal("the import job did not hold the page busy")
	}

	close(release)
	waitForTransactions(t, session, events, "the import job to finish", func(transactionsview.TransactionsState) bool {
		return !session.transactionsWork.Active() && !session.profileBusy.Active()
	})
	if session.profileBusy.Active() {
		t.Fatal("the profile busy stayed set")
	}
}

func TestTheNextImportUsesTheReplacedProfileBusy(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	stale := accountsview.NewBusy()
	session.mu.Lock()
	session.profileBusy = stale
	session.transactions.SetBusy(stale)
	session.mu.Unlock()

	current := accountsview.NewBusy()
	session.mu.Lock()
	session.profileBusy = current
	session.transactions.SetBusy(current)
	session.mu.Unlock()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		entered <- struct{}{}
		<-release
	}

	session.TransactionsConfirmImport(false)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the import job did not start")
	}
	if !current.Active() {
		t.Fatal("the import job did not use the current profile busy")
	}
	if stale.Active() {
		t.Fatal("the import job used the previous profile busy")
	}

	close(release)
	waitForTransactions(t, session, events, "the import job to finish", func(transactionsview.TransactionsState) bool {
		return !current.Active()
	})
}

func TestImportCSVOptionsAndRemapFollowRust(t *testing.T) {
	csv := &imports.CsvLayout{Headers: []string{"Buchung", "", "Betrag"}}
	options := ImportCSVOptions(csv, i18n.En)
	if len(options) != 4 {
		t.Fatalf("options = %+v", options)
	}
	if options[0] != i18n.Text(i18n.En, i18n.NoneDash) ||
		options[1] != "Buchung" ||
		options[2] != i18n.Format(i18n.En, i18n.ColumnN, i18n.Int(2)) ||
		options[3] != "Betrag" {
		t.Fatalf("options = %+v", options)
	}

	amount := 2
	mapping := imports.CsvMapping{Amount: &amount}
	debit := RemapCSVMapping(mapping, "debit", 4)
	if debit.Debit == nil || *debit.Debit != 3 {
		t.Fatalf("debit = %+v", debit.Debit)
	}
	if debit.Amount != nil {
		t.Fatalf("amount = %+v, want none", debit.Amount)
	}

	remapped := RemapCSVMapping(debit, "amount", 2)
	if remapped.Amount == nil || *remapped.Amount != 1 {
		t.Fatalf("amount = %+v", remapped.Amount)
	}
	if remapped.Debit != nil || remapped.Credit != nil {
		t.Fatalf("debit/credit = %+v / %+v, want none", remapped.Debit, remapped.Credit)
	}

	none := RemapCSVMapping(mapping, "amount", 0)
	if none.Amount != nil {
		t.Fatalf("amount = %+v, want none", none.Amount)
	}
}

func TestImportDialogUsesTheRustFilterTitles(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	fake := &fakeFileDialogs{
		statements:       []string{importFixture(t, "n26_preset_2026_10.csv")},
		statementsResult: true,
		receipts:         []string{filepath.Join("..", "..", "..", "testdata", "pdf", "text-receipt.pdf")},
		receiptsResult:   true,
		savePath:         filepath.Join(t.TempDir(), "export.csv"),
		saveResult:       true,
	}
	session.SetFileDialogs(fake)

	session.TransactionsOpenImport()
	waitForDialogJob(t, session)
	fake.mu.Lock()
	statementsTitle := fake.statementsTitle
	fake.mu.Unlock()
	if statementsTitle != i18n.Text(i18n.En, i18n.ImportBankStatement) {
		t.Fatalf("statement title = %q", statementsTitle)
	}

	transaction := session.TransactionsState().Transactions[0]
	session.TransactionsOpenDetail(transaction.ID)
	waitForDialogJob(t, session)
	session.TransactionsAttachReceipt()
	waitForDialogJob(t, session)
	fake.mu.Lock()
	receiptsTitle := fake.receiptsTitle
	fake.mu.Unlock()
	if receiptsTitle != i18n.Text(i18n.En, i18n.AttachReceipt) {
		t.Fatalf("receipt title = %q", receiptsTitle)
	}
}
