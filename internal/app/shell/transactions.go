package shell

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/banktext"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// TransactionsDialogState is the Transactions page's dialog state without Slint.
//
// Every dialog the page opens itself lives here: the custom range, the amount
// filter, the add form, the rule form, the single delete confirmation and the
// multi-delete confirmation. The statement import dialog is G8c-2 and has no
// fields yet.
type TransactionsDialogState struct {
	// RangeOpen says whether the custom-range dialog is open.
	RangeOpen bool
	// RangeFrom and RangeTo are its date fields.
	RangeFrom string
	RangeTo   string
	// RangeError is its validation message.
	RangeError string

	// AmountOpen says whether the amount-filter dialog is open.
	AmountOpen bool
	// AmountMin and AmountMax are its amount fields.
	AmountMin string
	AmountMax string
	// AmountMinLabel and AmountMaxLabel are the localized field labels.
	AmountMinLabel string
	AmountMaxLabel string
	// AmountError is its validation message.
	AmountError string

	// AddOpen says whether the add-transaction dialog is open.
	AddOpen bool
	// AddError is its validation message.
	AddError string

	// RuleOpen says whether the create-rule dialog is open.
	RuleOpen bool
	// RulePattern is its pattern field.
	RulePattern string
	// RuleCategoryIndex is the selected category row.
	RuleCategoryIndex int
	// RuleError is its validation message.
	RuleError string

	// DeleteOpen says whether the single-delete confirmation is open.
	DeleteOpen bool
	// DeleteID is the transaction it deletes.
	DeleteID int
	// DeleteTitle and DeleteText are its heading and body.
	DeleteTitle string
	DeleteText  string

	// MultiDeleteOpen says whether the multi-delete confirmation is open.
	MultiDeleteOpen bool
	// MultiDeleteTitle, MultiDeleteRange, MultiDeleteTotals and
	// MultiDeleteLines are its heading and summary.
	MultiDeleteTitle  string
	MultiDeleteRange  string
	MultiDeleteTotals string
	MultiDeleteLines  []string
	// MultiDeleteMore is the "... and N more" line.
	MultiDeleteMore string
}

// transactionsPendingOpenKind is Rust's PendingOpen: the dialog the menu asked
// for, opened once its page load has finished.
type transactionsPendingOpenKind uint8

const (
	transactionsPendingNone transactionsPendingOpenKind = iota
	transactionsPendingAdd
	transactionsPendingImport
)

// pendingTransactionsSearch is Rust's PendingSearch: the newest query typed
// while a job was running, kept until that job has finished.
type pendingTransactionsSearch struct {
	query *string
}

func (p *pendingTransactionsSearch) deferQuery(query string) {
	copied := query
	p.query = &copied
}

func (p *pendingTransactionsSearch) take() (string, bool) {
	if p.query == nil {
		return "", false
	}
	query := *p.query
	p.query = nil
	return query, true
}

func (p *pendingTransactionsSearch) restore(query string) {
	if p.query == nil {
		copied := query
		p.query = &copied
	}
}

// transactionsReapplyKey is one re-apply checkbox row's value key; pointer
// identity never decides membership.
type transactionsReapplyKey struct {
	hasFrom bool
	from    model.CategoryID
	to      model.CategoryID
}

func reapplyKeyOf(from *model.CategoryID, to model.CategoryID) transactionsReapplyKey {
	key := transactionsReapplyKey{to: to}
	if from != nil {
		key.from = *from
		key.hasFrom = true
	}
	return key
}

func (k transactionsReapplyKey) viewKey() transactionsview.ReapplyKey {
	key := transactionsview.ReapplyKey{ToCategoryID: k.to}
	if k.hasFrom {
		from := k.from
		key.FromCategoryID = &from
	}
	return key
}

// TransactionsState returns a deep copy of the Transactions page state.
func (s *Session) TransactionsState() transactionsview.TransactionsState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transactions == nil {
		return *transactionsview.NewState(s.language)
	}
	return s.transactions.State().Clone()
}

// TransactionsDialog returns a deep copy of the Transactions page's dialogs.
func (s *Session) TransactionsDialog() TransactionsDialogState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transactionsDialog.clone()
}

// TransactionsReapplyKey is one unticked re-apply group, as the DTO needs it.
type TransactionsReapplyKey struct {
	// FromCategoryID is the category the bookings leave, nil when uncategorized.
	FromCategoryID *model.CategoryID
	// ToCategoryID is the category they move to.
	ToCategoryID model.CategoryID
}

// TransactionsReapplyExcludedKeys returns the preview groups the user unticked.
func (s *Session) TransactionsReapplyExcludedKeys() []TransactionsReapplyKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]TransactionsReapplyKey, 0, len(s.transactionsReapplyExcluded))
	for key := range s.transactionsReapplyExcluded {
		entry := TransactionsReapplyKey{ToCategoryID: key.to}
		if key.hasFrom {
			from := key.from
			entry.FromCategoryID = &from
		}
		keys = append(keys, entry)
	}
	return keys
}

// TransactionsToday returns the session's today, for the add form's date field.
func (s *Session) TransactionsToday() civil.Date {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.today()
}

func (d TransactionsDialogState) clone() TransactionsDialogState {
	cloned := d
	cloned.MultiDeleteLines = append([]string(nil), d.MultiDeleteLines...)
	return cloned
}

// TransactionsOpen opens the page with args, as a navigation or a link does.
func (s *Session) TransactionsOpen(args transactionsview.StartArgs) {
	s.mu.Lock()
	job := s.beginTransactionsOpenLocked(args)
	s.mu.Unlock()
	s.startPageJob(job)
}

// TransactionsOpenWithAdd opens the page and then the add dialog, as the File
// menu does.
func (s *Session) TransactionsOpenWithAdd() {
	s.transactionsOpenWithPending(transactionsPendingAdd)
}

// TransactionsOpenWithImport opens the page and then the statement-import
// dialog, as the File menu does.
func (s *Session) TransactionsOpenWithImport() {
	s.transactionsOpenWithPending(transactionsPendingImport)
}

func (s *Session) transactionsOpenWithPending(pending transactionsPendingOpenKind) {
	args := PageArgs{Transactions: transactionsview.StartArgs{}}
	s.mu.Lock()
	s.nav.Enter(args)
	s.transactionsPendingOpen = pending
	job := s.openPageLocked("transactions", args)
	if job == nil {
		s.transactionsPendingOpen = transactionsPendingNone
	}
	s.mu.Unlock()

	s.notify(Change{Page: "shell"})
	s.startPageJob(job)
}

// TransactionsOpenImport asks for statement files and opens the import preview,
// as the page's Import button does.
func (s *Session) TransactionsOpenImport() {
	s.mu.Lock()
	dialogs := s.fileDialogs
	language := s.language
	job, _ := s.beginTransactionsJobLocked(false, func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
	) {
		if dialogs == nil {
			return
		}
		files, ok := dialogs.PickStatements(i18n.Text(language, i18n.ImportBankStatement))
		if !ok || len(files) == 0 {
			return
		}
		_ = view.OpenImport(ctx, database, files, nil, s.today())
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// TransactionsImportAccountChanged picks the account for the open preview.
func (s *Session) TransactionsImportAccountChanged(index int) {
	s.mu.Lock()
	var accountID *model.AccountID
	if s.transactions != nil {
		accounts := s.transactions.State().Accounts
		if index >= 0 && index < len(accounts) {
			id := accounts[index].ID
			accountID = &id
		}
	}
	s.mu.Unlock()
	if accountID == nil {
		return
	}
	s.startTransactionsAction(func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
		today civil.Date,
	) {
		_ = view.SelectImportAccount(ctx, database, *accountID, today)
	})
}

// TransactionsImportCSVChanged applies one CSV mapping change from the dialog.
func (s *Session) TransactionsImportCSVChanged(field string, value string) {
	s.mu.Lock()
	language := s.language
	var mapping *imports.CsvMapping
	if s.transactions != nil {
		preview := s.transactions.State().ImportPreview
		if preview != nil {
			for index := range preview.Entries {
				csv := preview.Entries[index].Statement.Csv
				if csv == nil {
					continue
				}
				options := ImportCSVOptions(csv, language)
				optionIndex := 0
				for position, option := range options {
					if option == value {
						optionIndex = position
						break
					}
				}
				remapped := RemapCSVMapping(csv.Mapping, field, optionIndex)
				mapping = &remapped
				break
			}
		}
	}
	s.mu.Unlock()
	if mapping == nil {
		return
	}
	s.startTransactionsAction(func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
		today civil.Date,
	) {
		_ = view.RemapImport(ctx, database, *mapping, today)
	})
}

// TransactionsConfirmImport imports the previewed entries, holding the profile
// busy on top of the page's own as spawn_import does.
func (s *Session) TransactionsConfirmImport(updateBalance bool) {
	s.mu.Lock()
	today := s.today()
	job, _ := s.beginTransactionsImportJobLocked(func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
	) {
		_ = view.ConfirmImport(ctx, database, updateBalance, today)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// TransactionsCancelImport closes the preview without importing.
func (s *Session) TransactionsCancelImport() {
	s.mu.Lock()
	if s.transactionsWork.Active() || s.transactions == nil {
		// Rust's update_view finds no view while a worker owns it.
		s.mu.Unlock()
		return
	}
	s.transactions.CancelImport()
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsExportCSV asks where to write the export and writes exactly what
// is shown.
func (s *Session) TransactionsExportCSV() {
	s.mu.Lock()
	dialogs := s.fileDialogs
	language := s.language
	today := s.today()
	job, _ := s.beginTransactionsJobLocked(false, func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
	) {
		if dialogs == nil {
			return
		}
		defaultName := fmt.Sprintf(
			"transactions-%04d-%02d-%02d.csv",
			today.Year(),
			int(today.Month()),
			today.Day(),
		)
		path, ok := dialogs.SaveCSV(i18n.Text(language, i18n.ExportTransactions), defaultName)
		if !ok || strings.TrimSpace(path) == "" {
			return
		}
		_ = view.ExportCSV(ctx, database, path)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// TransactionsAttachReceipt asks for receipt files and attaches them to the
// selected booking.
func (s *Session) TransactionsAttachReceipt() {
	s.mu.Lock()
	if s.transactions == nil {
		s.mu.Unlock()
		return
	}
	state := s.transactions.State()
	selected := state.Selected
	if selected == nil {
		s.mu.Unlock()
		return
	}
	id := selected.ID
	dialogs := s.fileDialogs
	language := s.language
	documentsDir := s.transactionsDocumentsDirLocked()
	job, _ := s.beginTransactionsJobLocked(false, func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
	) {
		if dialogs == nil {
			return
		}
		files, ok := dialogs.PickReceipts(i18n.Text(language, i18n.AttachReceipt))
		if !ok || len(files) == 0 {
			return
		}
		service := documentsservice.NewService(database, ocr.WithoutOCR(), documentsDir)
		_ = view.AttachDocuments(ctx, database, service, id, files, s.today())
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// beginTransactionsOpenLocked prepares the page's navigation load. The caller
// holds the session mutex.
func (s *Session) beginTransactionsOpenLocked(args transactionsview.StartArgs) *pageJob {
	if s.transactions != nil {
		s.transactions.StateMut().Language = s.language
	}
	if s.db == nil {
		return nil
	}
	today := s.today()
	job, _ := s.beginTransactionsJobLocked(true, func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
	) {
		_ = view.Start(ctx, database, args, today)
	})
	return job
}

// beginTransactionsJobLocked prepares one action on a clone of the view. The
// caller holds the session mutex and starts the returned job after releasing
// it.
func (s *Session) beginTransactionsJobLocked(
	loading bool,
	work func(context.Context, *transactionsview.TransactionsView, *db.Database),
) (*pageJob, bool) {
	return s.beginTransactionsJobWithProfileBusyLocked(loading, false, work)
}

// beginTransactionsImportJobLocked is spawn_import: it holds the profile Busy on
// top of the page's own for the whole import job.
func (s *Session) beginTransactionsImportJobLocked(
	work func(context.Context, *transactionsview.TransactionsView, *db.Database),
) (*pageJob, bool) {
	return s.beginTransactionsJobWithProfileBusyLocked(false, true, work)
}

func (s *Session) beginTransactionsJobWithProfileBusyLocked(
	loading bool,
	holdProfileBusy bool,
	work func(context.Context, *transactionsview.TransactionsView, *db.Database),
) (*pageJob, bool) {
	if s.db == nil || s.transactionsWork.Active() || s.transactions == nil {
		return nil, false
	}

	busy := s.transactionsWork
	busy.Begin()
	var profileBusy accountsview.Busy
	if holdProfileBusy {
		profileBusy = s.profileBusy
		profileBusy.Begin()
	}
	generation, ok := s.beginJobLocked()
	if !ok {
		if holdProfileBusy {
			profileBusy.End()
		}
		busy.End()
		return nil, false
	}

	request := s.nextPageRequestLocked("transactions")
	pending := s.transactions.Clone()
	pending.StateMut().Language = s.language
	if loading {
		pending.StateMut().IsLoading = true
	}
	// The published state is a copy: the worker mutates pending without the
	// session mutex, exactly as the Dashboard's job does.
	published := pending.Clone()
	s.transactions = published
	database := s.db

	return &pageJob{
		page:       "transactions",
		generation: generation,
		request:    request,
		database:   database,
		load: func(ctx context.Context, open *db.Database) {
			work(ctx, pending, open)
		},
		publish: func() {
			s.transactions = pending
			s.runTransactionsPendingOpenLocked()
		},
		finish: func(published bool) {
			// Rust's BusyGuard ends after the result is returned; the pending
			// import dialog starts once the page's own busy section is over.
			if holdProfileBusy {
				profileBusy.End()
			}
			busy.End()
			if published {
				s.runPendingTransactionsImport()
			}
		},
		afterPublish: func(bool) {
			s.runPendingTransactionsSearch()
		},
	}, true
}

// resetTransactionsAuxLocked clears the controller-only page state that Rust's
// attach/reset clears.
func (s *Session) resetTransactionsAuxLocked() {
	s.transactionsDialog = TransactionsDialogState{}
	s.transactionsPendingOpen = transactionsPendingNone
	s.transactionsMultiDeleteIDs = nil
	s.transactionsReapplyExcluded = nil
}

// runTransactionsPendingOpenLocked opens the add dialog the menu asked for once
// the page load has finished. The import dialog needs a job of its own, so it
// stays pending until runPendingTransactionsImport. The caller holds the
// session mutex.
func (s *Session) runTransactionsPendingOpenLocked() {
	switch s.transactionsPendingOpen {
	case transactionsPendingAdd:
		s.transactionsPendingOpen = transactionsPendingNone
		s.openTransactionsAddLocked()
	case transactionsPendingImport:
		// Started without the mutex, once this job has finished.
	}
}

// runPendingTransactionsImport starts the import dialog the menu asked for. It
// runs after the page's job released the mutex and its busy section.
func (s *Session) runPendingTransactionsImport() {
	s.mu.Lock()
	pending := s.transactionsPendingOpen == transactionsPendingImport
	if pending {
		s.transactionsPendingOpen = transactionsPendingNone
	}
	s.mu.Unlock()
	if pending {
		s.TransactionsOpenImport()
	}
}

// runPendingTransactionsSearch starts the latest deferred query when no job is
// running, keeping it otherwise.
func (s *Session) runPendingTransactionsSearch() {
	s.mu.Lock()
	if s.transactionsWork.Active() {
		s.mu.Unlock()
		return
	}
	query, ok := s.transactionsPendingSearch.take()
	if !ok {
		s.mu.Unlock()
		return
	}
	today := s.today()
	job, started := s.beginTransactionsJobLocked(false, func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
	) {
		_ = view.UpdateSearchQuery(ctx, database, query, today)
	})
	s.mu.Unlock()
	if !started {
		s.mu.Lock()
		s.transactionsPendingSearch.restore(query)
		s.mu.Unlock()
		return
	}
	s.startPageJob(job)
}

// TransactionsSearch applies the latest search text; while a job runs the query
// is kept and run afterwards, so the last typed query always wins.
func (s *Session) TransactionsSearch(query string) {
	s.mu.Lock()
	s.transactionsPendingSearch.deferQuery(query)
	s.mu.Unlock()
	s.runPendingTransactionsSearch()
}

// TransactionsPeriodChanged applies one period row.
func (s *Session) TransactionsPeriodChanged(index int) {
	period, ok := transactionsPeriodAt(index)
	if !ok {
		return
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UpdatePeriod(ctx, database, period, today)
	})
}

// TransactionsCategoryChanged applies one category-filter row.
func (s *Session) TransactionsCategoryChanged(index int) {
	s.mu.Lock()
	filter, ok := transactionsCategoryFilterAt(s.transactions.State(), index)
	s.mu.Unlock()
	if !ok {
		return
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UpdateCategoryFilter(ctx, database, filter, today)
	})
}

// TransactionsDirectionChanged applies one direction row.
func (s *Session) TransactionsDirectionChanged(index int) {
	direction := model.DirectionAll
	switch index {
	case 1:
		direction = model.DirectionIncome
	case 2:
		direction = model.DirectionExpenses
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UpdateDirection(ctx, database, direction, today)
	})
}

// TransactionsAccountChanged selects the account choice at index.
func (s *Session) TransactionsAccountChanged(index int) {
	s.mu.Lock()
	selection, ok := transactionsSelectionAt(s.transactions.State(), index)
	s.mu.Unlock()
	if !ok {
		return
	}
	s.selectTransactionAccounts(selection)
}

func (s *Session) selectTransactionAccounts(selection accountpanel.AccountSelection) {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.SelectAccounts(ctx, database, selection, today)
	})
}

// TransactionsOpenRange opens the custom-range dialog with the range shown now.
func (s *Session) TransactionsOpenRange() {
	today := s.today()
	s.mu.Lock()
	state := s.transactions.State()
	from, to := transactionsRangeInitial(state, today)
	s.transactionsDialog.RangeOpen = true
	s.transactionsDialog.RangeFrom = format.Date(from)
	s.transactionsDialog.RangeTo = format.Date(to)
	s.transactionsDialog.RangeError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsApplyRange validates and applies the custom range.
func (s *Session) TransactionsApplyRange(fromText string, toText string) {
	from, ok := format.ParseDate(fromText)
	if !ok {
		s.setTransactionsRangeError(i18n.EnterValidDate)
		return
	}
	to, ok := format.ParseDate(toText)
	if !ok {
		s.setTransactionsRangeError(i18n.EnterValidDate)
		return
	}
	if from.After(to) {
		s.setTransactionsRangeError(i18n.InvalidDateRange)
		return
	}
	s.mu.Lock()
	s.transactionsDialog.RangeOpen = false
	s.transactionsDialog.RangeError = ""
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.SetCustomRange(ctx, database, from, to, today)
	})
}

// TransactionsCloseRange closes the custom-range dialog.
func (s *Session) TransactionsCloseRange() {
	s.mu.Lock()
	s.transactionsDialog.RangeOpen = false
	s.transactionsDialog.RangeError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

func (s *Session) setTransactionsRangeError(key i18n.Key) {
	s.mu.Lock()
	s.transactionsDialog.RangeError = i18n.Text(s.language, key)
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsOpenAmount opens the amount-filter dialog with the current bounds.
func (s *Session) TransactionsOpenAmount() {
	s.mu.Lock()
	state := s.transactions.State()
	language := s.language
	currency := state.PrimaryCurrency
	minimum := ""
	if state.MinAmount != nil {
		minimum = money.FormatPlain(*state.MinAmount)
	}
	maximum := ""
	if state.MaxAmount != nil {
		maximum = money.FormatPlain(*state.MaxAmount)
	}
	symbol := format.CurrencySymbol(currency)
	s.transactionsDialog.AmountOpen = true
	s.transactionsDialog.AmountMin = minimum
	s.transactionsDialog.AmountMax = maximum
	s.transactionsDialog.AmountMinLabel = i18n.Format(language, i18n.MinAmountIn, i18n.Str(symbol))
	s.transactionsDialog.AmountMaxLabel = i18n.Format(language, i18n.MaxAmountIn, i18n.Str(symbol))
	s.transactionsDialog.AmountError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsApplyAmount validates and applies the amount bounds.
func (s *Session) TransactionsApplyAmount(minimumText string, maximumText string) {
	s.mu.Lock()
	currency := s.transactions.State().PrimaryCurrency
	s.mu.Unlock()

	minimum := transactionsParseBound(minimumText, currency)
	maximum := transactionsParseBound(maximumText, currency)
	if strings.TrimSpace(minimumText) != "" && minimum == nil {
		s.setTransactionsAmountError(i18n.EnterValidAmount)
		return
	}
	if strings.TrimSpace(maximumText) != "" && maximum == nil {
		s.setTransactionsAmountError(i18n.EnterValidAmount)
		return
	}
	s.mu.Lock()
	s.transactionsDialog.AmountOpen = false
	s.transactionsDialog.AmountError = ""
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UpdateAmountRange(ctx, database, minimum, maximum, today)
	})
}

// TransactionsResetAmount clears the amount filter and closes the dialog.
func (s *Session) TransactionsResetAmount() {
	s.mu.Lock()
	s.transactionsDialog.AmountOpen = false
	s.transactionsDialog.AmountError = ""
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UpdateAmountRange(ctx, database, nil, nil, today)
	})
}

// TransactionsCloseAmount closes the amount-filter dialog.
func (s *Session) TransactionsCloseAmount() {
	s.mu.Lock()
	s.transactionsDialog.AmountOpen = false
	s.transactionsDialog.AmountError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

func (s *Session) setTransactionsAmountError(key i18n.Key) {
	s.mu.Lock()
	s.transactionsDialog.AmountError = i18n.Text(s.language, key)
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsToggleChart folds or unfolds the balance chart.
func (s *Session) TransactionsToggleChart() {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, _ civil.Date) {
		_ = view.ToggleChart(ctx, database)
	})
}

// TransactionsAutoCategorize applies the auto-apply rules.
func (s *Session) TransactionsAutoCategorize() {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.AutoCategorize(ctx, database, today)
	})
}

// TransactionsReapplyRules opens the re-apply preview for the shown bookings.
func (s *Session) TransactionsReapplyRules() {
	s.mu.Lock()
	s.transactionsReapplyExcluded = nil
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.PreviewReapplyRules(ctx, database, today)
	})
}

// TransactionsToggleReapplyGroup ticks or unticks one preview group.
func (s *Session) TransactionsToggleReapplyGroup(from int, to int) {
	var fromID *model.CategoryID
	if from >= 0 {
		id := model.CategoryID(from)
		fromID = &id
	}
	key := reapplyKeyOf(fromID, model.CategoryID(to))
	s.mu.Lock()
	if s.transactionsReapplyExcluded == nil {
		s.transactionsReapplyExcluded = make(map[transactionsReapplyKey]struct{})
	}
	if _, excluded := s.transactionsReapplyExcluded[key]; excluded {
		delete(s.transactionsReapplyExcluded, key)
	} else {
		s.transactionsReapplyExcluded[key] = struct{}{}
	}
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsConfirmReapplyRules applies the preview minus the unticked groups.
func (s *Session) TransactionsConfirmReapplyRules() {
	s.mu.Lock()
	excludedValues := s.transactionsReapplyExcluded
	s.transactionsReapplyExcluded = nil
	excluded := make(map[transactionsview.ReapplyKey]struct{}, len(excludedValues))
	for key := range excludedValues {
		excluded[key.viewKey()] = struct{}{}
	}
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.ConfirmReapplyRules(ctx, database, excluded, today)
	})
}

// TransactionsCancelReapplyRules closes the preview without changing anything.
func (s *Session) TransactionsCancelReapplyRules() {
	s.mu.Lock()
	s.transactionsReapplyExcluded = nil
	s.transactions.CancelReapplyRules()
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsUndoReapply restores the categories of the last re-apply.
func (s *Session) TransactionsUndoReapply() {
	s.mu.Lock()
	s.transactionsReapplyExcluded = nil
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UndoReapplyRules(ctx, database, today)
	})
}

// TransactionsOpenRule opens the create-rule dialog for the selected booking.
func (s *Session) TransactionsOpenRule() {
	s.mu.Lock()
	state := s.transactions.State()
	transaction := state.Selected
	if transaction == nil {
		s.mu.Unlock()
		return
	}
	pattern := s.transactions.RulePatternFor(transaction)
	categoryIndex := 0
	if transaction.CategoryID != nil {
		for index := range state.Categories {
			if state.Categories[index].ID == *transaction.CategoryID {
				categoryIndex = index
				break
			}
		}
	}
	s.transactionsDialog.RuleOpen = true
	s.transactionsDialog.RulePattern = pattern
	s.transactionsDialog.RuleCategoryIndex = categoryIndex
	s.transactionsDialog.RuleError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsCloseRule closes the create-rule dialog.
func (s *Session) TransactionsCloseRule() {
	s.mu.Lock()
	s.transactionsDialog.RuleOpen = false
	s.transactionsDialog.RuleError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsSaveRule validates and creates a rule.
func (s *Session) TransactionsSaveRule(pattern string, categoryIndex int) {
	s.mu.Lock()
	language := s.language
	if strings.TrimSpace(pattern) == "" {
		s.transactionsDialog.RuleError = i18n.Text(language, i18n.ErrorPatternRequired)
		s.mu.Unlock()
		s.notify(Change{Page: "transactions"})
		return
	}
	categoryID, ok := transactionsRuleCategoryAt(s.transactions.State(), categoryIndex)
	if !ok {
		s.mu.Unlock()
		return
	}
	s.transactionsDialog.RuleOpen = false
	s.transactionsDialog.RuleError = ""
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.CreateRule(ctx, database, pattern, categoryID, today)
	})
}

// TransactionsOpenDelete opens the delete confirmation for one booking.
func (s *Session) TransactionsOpenDelete(id model.TransactionID) {
	s.mu.Lock()
	state := s.transactions.State()
	language := s.language
	var transaction *model.TransactionItem
	for index := range state.Transactions {
		if state.Transactions[index].ID == id {
			transaction = &state.Transactions[index]
			break
		}
	}
	if transaction == nil {
		s.mu.Unlock()
		return
	}
	text := format.Date(transaction.Date) + " " + banktext.DisplayDescription(transaction) + " " + format.Money(transaction.Amount, language, false)
	s.transactionsDialog.DeleteOpen = true
	s.transactionsDialog.DeleteID = transactionsID(int64(id))
	s.transactionsDialog.DeleteTitle = i18n.Text(language, i18n.DeleteTransactionTitle)
	s.transactionsDialog.DeleteText = text
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsConfirmDelete deletes one booking.
func (s *Session) TransactionsConfirmDelete(id model.TransactionID) {
	s.mu.Lock()
	s.transactionsDialog.DeleteOpen = false
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.DeleteTransaction(ctx, database, id, today)
	})
}

// TransactionsCancelDelete closes the delete confirmation.
func (s *Session) TransactionsCancelDelete() {
	s.mu.Lock()
	s.transactionsDialog.DeleteOpen = false
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsOpenMultiDelete opens the confirmation for the selected bookings.
func (s *Session) TransactionsOpenMultiDelete() {
	s.mu.Lock()
	state := s.transactions.State()
	language := s.language
	bookings := state.SelectedShown()
	if len(bookings) == 0 {
		s.mu.Unlock()
		return
	}
	ids := make(map[model.TransactionID]struct{}, len(bookings))
	from := bookings[0].Date
	to := bookings[0].Date
	for _, booking := range bookings {
		ids[booking.ID] = struct{}{}
		if booking.Date.Before(from) {
			from = booking.Date
		}
		if booking.Date.After(to) {
			to = booking.Date
		}
	}
	rangeText := format.Date(from)
	if from != to {
		rangeText = format.Date(from) + "–" + format.Date(to)
	}
	totals := make([]string, 0)
	for _, total := range state.SelectedTotals() {
		totals = append(totals, format.Money(money.Money{Minor: total.Total, Currency: total.Currency}, language, true))
	}
	lines := make([]string, 0, 5)
	for index, booking := range bookings {
		if index == 5 {
			break
		}
		lines = append(lines, format.Date(booking.Date)+" "+banktext.DisplayDescription(booking)+" "+format.Money(booking.Amount, language, false))
	}
	more := ""
	if extra := len(bookings) - 5; extra > 0 {
		more = i18n.Format(language, i18n.AndMore, i18n.Int(int64(extra)))
	}
	s.transactionsMultiDeleteIDs = ids
	s.transactionsDialog.MultiDeleteOpen = true
	s.transactionsDialog.MultiDeleteTitle = i18n.Format(language, i18n.DeleteTransactionsTitle, i18n.Int(int64(len(bookings))))
	s.transactionsDialog.MultiDeleteRange = rangeText
	s.transactionsDialog.MultiDeleteTotals = strings.Join(totals, " · ")
	s.transactionsDialog.MultiDeleteLines = lines
	s.transactionsDialog.MultiDeleteMore = more
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsConfirmMultiDelete deletes the bookings the dialog described.
func (s *Session) TransactionsConfirmMultiDelete() {
	s.mu.Lock()
	ids := s.transactionsMultiDeleteIDs
	s.transactionsMultiDeleteIDs = nil
	s.transactionsDialog.MultiDeleteOpen = false
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.DeleteSelected(ctx, database, ids, today)
	})
}

// TransactionsCancelMultiDelete closes the multi-delete confirmation.
func (s *Session) TransactionsCancelMultiDelete() {
	s.mu.Lock()
	s.transactionsDialog.MultiDeleteOpen = false
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsBatchCategory sets the selected bookings' category.
func (s *Session) TransactionsBatchCategory(index int) {
	s.mu.Lock()
	categoryID, _ := transactionsCategoryIDAt(s.transactions.State(), index)
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.SetCategoryForSelection(ctx, database, categoryID, today)
	})
}

// TransactionsBatchTax sets the selected bookings' tax override.
func (s *Session) TransactionsBatchTax(index int) {
	value := transactionsTaxValue(index)
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.SetTaxOverrideForSelection(ctx, database, value, today)
	})
}

// TransactionsOpenAdd opens the add-transaction dialog.
func (s *Session) TransactionsOpenAdd() {
	s.mu.Lock()
	s.openTransactionsAddLocked()
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

func (s *Session) openTransactionsAddLocked() {
	s.transactionsDialog.AddOpen = true
	s.transactionsDialog.AddError = ""
}

// TransactionsCloseAdd closes the add-transaction dialog.
func (s *Session) TransactionsCloseAdd() {
	s.mu.Lock()
	s.transactionsDialog.AddOpen = false
	s.transactionsDialog.AddError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsAddSubmit validates and adds a manual booking.
func (s *Session) TransactionsAddSubmit(
	accountIndex int,
	expense bool,
	dateText string,
	amountText string,
	partner string,
	purpose string,
	categoryIndex int,
) {
	s.mu.Lock()
	state := s.transactions.State()
	language := s.language
	var account *accountsviewAccount
	for index := range state.Accounts {
		if index == accountIndex || (accountIndex < 0 || accountIndex >= len(state.Accounts)) && index == 0 {
			item := state.Accounts[index]
			account = &accountsviewAccount{id: item.ID, currency: item.Currency}
			break
		}
	}
	if account == nil && len(state.Accounts) > 0 {
		item := state.Accounts[0]
		account = &accountsviewAccount{id: item.ID, currency: item.Currency}
	}
	if account == nil {
		s.mu.Unlock()
		return
	}
	date, ok := format.ParseDate(dateText)
	if !ok {
		s.transactionsDialog.AddError = i18n.Text(language, i18n.EnterValidDate)
		s.mu.Unlock()
		s.notify(Change{Page: "transactions"})
		return
	}
	parsed, err := money.ParseDecimal(amountText, account.currency)
	if err != nil {
		s.transactionsDialog.AddError = i18n.Text(language, i18n.EnterValidAmount)
		s.mu.Unlock()
		s.notify(Change{Page: "transactions"})
		return
	}
	amount := money.Money{Minor: absInt64(parsed.Minor), Currency: account.currency}
	if expense {
		amount.Minor = -amount.Minor
	}
	categoryID, _ := transactionsCategoryIDAt(state, categoryIndex)
	partnerName := transactionsOptionalTrimmed(partner)
	purposeText := transactionsOptionalTrimmed(purpose)
	s.transactionsDialog.AddOpen = false
	s.transactionsDialog.AddError = ""
	s.mu.Unlock()

	entry := transactionsview.ManualTransaction{
		AccountID:          account.id,
		Date:               date,
		Amount:             amount,
		PartnerName:        partnerName,
		Purpose:            purposeText,
		CategoryID:         categoryID,
		ConfirmedDuplicate: false,
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.AddManualTransaction(ctx, database, entry, today, s.clock().UTC())
	})
}

// accountsviewAccount is the two fields the add form needs from an account row.
type accountsviewAccount struct {
	id       model.AccountID
	currency money.Currency
}

// TransactionsConfirmDuplicate adds the pending identical booking.
func (s *Session) TransactionsConfirmDuplicate() {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_, _ = view.ConfirmDuplicate(ctx, database, today, s.clock().UTC())
	})
}

// TransactionsDismissDuplicate closes the duplicate confirmation.
func (s *Session) TransactionsDismissDuplicate() {
	s.mu.Lock()
	s.transactions.DismissDuplicate()
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsOpenDetail opens the detail panel with the booking's documents.
func (s *Session) TransactionsOpenDetail(id model.TransactionID) {
	s.mu.Lock()
	state := s.transactions.State()
	var transaction *model.TransactionItem
	for index := range state.Transactions {
		if state.Transactions[index].ID == id {
			cloned := state.Transactions[index]
			transaction = &cloned
			break
		}
	}
	documentsDir := s.transactionsDocumentsDirLocked()
	s.mu.Unlock()
	if transaction == nil {
		return
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, _ civil.Date) {
		service := documentsservice.NewService(database, ocr.WithoutOCR(), documentsDir)
		_ = view.SelectTransaction(ctx, database, service, transaction)
	})
}

// TransactionsCloseDetail closes the detail panel.
func (s *Session) TransactionsCloseDetail() {
	s.mu.Lock()
	documentsDir := s.transactionsDocumentsDirLocked()
	s.mu.Unlock()
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, _ civil.Date) {
		service := documentsservice.NewService(database, ocr.WithoutOCR(), documentsDir)
		_ = view.SelectTransaction(ctx, database, service, nil)
	})
}

// TransactionsDetailClose is the Rust callback name for closing the detail
// panel.
func (s *Session) TransactionsDetailClose() {
	s.TransactionsCloseDetail()
}

// TransactionsDetailCreateRule is the Rust callback name for opening the rule
// dialog from the detail panel.
func (s *Session) TransactionsDetailCreateRule() {
	s.TransactionsOpenRule()
}

func (s *Session) transactionsDocumentsDirLocked() string {
	profile := s.documents.Profile
	directory, err := documentsservice.DefaultDocumentsDirectory(s.paths, profile)
	if err != nil {
		return ""
	}
	return directory
}

// TransactionsDetailCategory changes the selected booking's category.
func (s *Session) TransactionsDetailCategory(index int) {
	s.mu.Lock()
	state := s.transactions.State()
	categoryID, ok := transactionsCategoryIDAt(state, index)
	selected := state.Selected
	var id model.TransactionID
	if selected != nil {
		id = selected.ID
	}
	s.mu.Unlock()
	if selected == nil || !ok {
		return
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UpdateTransactionCategory(ctx, database, id, categoryID, today)
	})
}

// TransactionsDetailTax changes the selected booking's tax override.
func (s *Session) TransactionsDetailTax(index int) {
	s.mu.Lock()
	selected := s.transactions.State().Selected
	var id model.TransactionID
	if selected != nil {
		id = selected.ID
	}
	s.mu.Unlock()
	if selected == nil {
		return
	}
	value := transactionsTaxValue(index)
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.SetTaxOverride(ctx, database, id, value, today)
	})
}

// TransactionsDetailShowPartner opens the list for the selected partner.
func (s *Session) TransactionsDetailShowPartner(partner string) {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.ShowPartner(ctx, database, partner, today)
	})
}

// TransactionsShowPartner opens the list for one partner.
func (s *Session) TransactionsShowPartner(partner string) {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.ShowPartner(ctx, database, partner, today)
	})
}

// TransactionsToggleSelect toggles one booking in the multi-selection.
func (s *Session) TransactionsToggleSelect(id model.TransactionID) {
	s.mu.Lock()
	s.transactions.ToggleSelection(id)
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsSelectRange selects the range from the anchor to id.
func (s *Session) TransactionsSelectRange(id model.TransactionID) {
	s.mu.Lock()
	s.transactions.SelectRange(id)
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsSelectAll selects every shown booking.
func (s *Session) TransactionsSelectAll() {
	s.mu.Lock()
	s.transactions.SelectAllShown()
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsClearSelection empties the multi-selection.
func (s *Session) TransactionsClearSelection() {
	s.mu.Lock()
	s.transactions.ClearSelection()
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsDismissMessage clears the error and success banner.
func (s *Session) TransactionsDismissMessage() {
	s.mu.Lock()
	s.transactions.ClearMessages()
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsPanelSelect selects an account, group or all accounts.
func (s *Session) TransactionsPanelSelect(kind string, groupKind string, id int, accountType string) {
	selection, ok := accountsSelectionFrom(kind, groupKind, id, accountType)
	if !ok {
		return
	}
	s.selectTransactionAccounts(selection)
}

// TransactionsPanelToggle collapses or expands one panel group.
func (s *Session) TransactionsPanelToggle(kind string, groupKind string, id int, accountType string) {
	selection, ok := accountsSelectionFrom(kind, groupKind, id, accountType)
	if !ok || selection.Kind != accountpanel.SelectionGroup {
		return
	}
	key := selection.Group
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, _ civil.Date) {
		_ = view.ToggleCollapsed(ctx, database, key)
	})
}

// TransactionsPanelMarkRead marks the bookings behind one panel row as read.
func (s *Session) TransactionsPanelMarkRead(kind string, groupKind string, id int, accountType string) {
	selection, ok := accountsSelectionFrom(kind, groupKind, id, accountType)
	if !ok {
		return
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.MarkRead(ctx, database, selection, today)
	})
}

// TransactionsPanelNewGroup creates a custom group.
func (s *Session) TransactionsPanelNewGroup(name string) {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.CreateGroup(ctx, database, name, today)
	})
}

// TransactionsPanelRenameGroup renames a custom group.
func (s *Session) TransactionsPanelRenameGroup(id int, name string) {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.RenameGroup(ctx, database, model.AccountGroupID(id), name, today)
	})
}

// TransactionsPanelDeleteGroup deletes a custom group.
func (s *Session) TransactionsPanelDeleteGroup(id int) {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.DeleteGroup(ctx, database, model.AccountGroupID(id), today)
	})
}

// TransactionsPanelMoveGroup moves a custom group up or down.
func (s *Session) TransactionsPanelMoveGroup(id int, up bool) {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.MoveGroup(ctx, database, model.AccountGroupID(id), up, today)
	})
}

// TransactionsPanelMoveAccount moves an account into a group, or back to its
// type group when groupID is negative.
func (s *Session) TransactionsPanelMoveAccount(accountID int, groupID int) {
	var group *model.AccountGroupID
	if groupID >= 0 {
		id := model.AccountGroupID(groupID)
		group = &id
	}
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.MoveAccountToGroup(ctx, database, model.AccountID(accountID), group, today)
	})
}

// TransactionsPanelFilterChanged stores the panel's free-text filter.
func (s *Session) TransactionsPanelFilterChanged(text string) {
	s.mu.Lock()
	s.transactions.UpdateAccountFilterText(text)
	s.mu.Unlock()
	s.notify(Change{Page: "transactions"})
}

// TransactionsBack clears the page's search, as the page's Back button does.
func (s *Session) TransactionsBack() {
	s.startTransactionsAction(func(ctx context.Context, view *transactionsview.TransactionsView, database *db.Database, today civil.Date) {
		_ = view.UpdateSearchQuery(ctx, database, "", today)
	})
}

// startTransactionsAction prepares one action on a clone of the page state and
// starts it. It is Rust's spawn.
func (s *Session) startTransactionsAction(
	work func(context.Context, *transactionsview.TransactionsView, *db.Database, civil.Date),
) {
	s.mu.Lock()
	today := s.today()
	job, _ := s.beginTransactionsJobLocked(false, func(
		ctx context.Context,
		view *transactionsview.TransactionsView,
		database *db.Database,
	) {
		work(ctx, view, database, today)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

func transactionsPeriodAt(index int) (transactionsview.Period, bool) {
	switch index {
	case 0:
		return transactionsview.PeriodThisMonth, true
	case 1:
		return transactionsview.PeriodLastMonth, true
	case 2:
		return transactionsview.PeriodLast3Months, true
	case 3:
		return transactionsview.PeriodThisYear, true
	case 4:
		return transactionsview.PeriodLastYear, true
	case 5:
		return transactionsview.PeriodAll, true
	default:
		return 0, false
	}
}

func transactionsCategoryFilterAt(
	state *transactionsview.TransactionsState,
	index int,
) (transactionsview.CategoryFilter, bool) {
	switch {
	case index == 0:
		return transactionsview.AnyCategoryFilter(), true
	case index == 1:
		return transactionsview.NoneCategoryFilter(), true
	case index >= 2:
		if categoryIndex := index - 2; categoryIndex < len(state.Categories) {
			return transactionsview.OnlyCategoryFilter(state.Categories[categoryIndex].ID), true
		}
	}
	return transactionsview.CategoryFilter{}, false
}

func transactionsCategoryIDAt(
	state *transactionsview.TransactionsState,
	index int,
) (*model.CategoryID, bool) {
	if index <= 0 || index > len(state.Categories) {
		return nil, true
	}
	id := state.Categories[index-1].ID
	return &id, true
}

func transactionsRuleCategoryAt(
	state *transactionsview.TransactionsState,
	index int,
) (model.CategoryID, bool) {
	if index < 0 || index >= len(state.Categories) {
		return 0, false
	}
	return state.Categories[index].ID, true
}

func transactionsSelectionAt(
	state *transactionsview.TransactionsState,
	index int,
) (accountpanel.AccountSelection, bool) {
	choices := make([]accountpanel.AccountSelection, 0, 1+len(state.Groups)+len(state.Accounts))
	choices = append(choices, accountpanel.AllSelection())
	for _, group := range state.Groups {
		choices = append(choices, accountpanel.GroupSelection(group.Key))
	}
	for _, account := range state.Accounts {
		choices = append(choices, accountpanel.AccountSelectionFor(account.ID))
	}
	if index < 0 || index >= len(choices) {
		return accountpanel.AccountSelection{}, false
	}
	return choices[index], true
}

func transactionsTaxValue(index int) *bool {
	switch index {
	case 1:
		value := true
		return &value
	case 2:
		value := false
		return &value
	default:
		return nil
	}
}

func transactionsRangeInitial(state *transactionsview.TransactionsState, today civil.Date) (civil.Date, civil.Date) {
	if state.CustomRange != nil {
		return state.CustomRange.From, state.CustomRange.To
	}
	if state.Month != nil {
		return state.Month.Start(), state.Month.End()
	}
	if from, to := state.Period.Range(today); from != nil && to != nil {
		return *from, *to
	}
	from, ok := civil.New(today.Year(), time.January, 1)
	if !ok {
		panic("valid date")
	}
	return from, today
}

func transactionsParseBound(text string, currency money.Currency) *money.Money {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	parsed, err := money.ParseDecimal(trimmed, currency)
	if err != nil {
		return nil
	}
	value := money.Money{Minor: absInt64(parsed.Minor), Currency: currency}
	return &value
}

func transactionsOptionalTrimmed(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func transactionsID(id int64) int {
	if id < math.MinInt32 || id > math.MaxInt32 {
		return -1
	}
	return int(id)
}

// ImportCSVOptions is one CSV column picker's choices: the localized "none" and
// the file's headers, with blank headers shown as "Column N".
func ImportCSVOptions(csv *imports.CsvLayout, language i18n.Language) []string {
	if csv == nil {
		return nil
	}
	options := make([]string, 0, len(csv.Headers)+1)
	options = append(options, i18n.Text(language, i18n.NoneDash))
	for index, header := range csv.Headers {
		if strings.TrimSpace(header) == "" {
			options = append(options, i18n.Format(language, i18n.ColumnN, i18n.Int(int64(index+1))))
		} else {
			options = append(options, header)
		}
	}
	return options
}

// RemapCSVMapping applies one CSV mapping field change from the dialog. A zero
// or negative index means "none"; Rust's index is one-based.
func RemapCSVMapping(mapping imports.CsvMapping, field string, index int) imports.CsvMapping {
	column := csvColumn(index)
	switch field {
	case "date":
		mapping.Date = column
	case "amount":
		mapping.Amount = column
		if column != nil {
			mapping.Debit = nil
			mapping.Credit = nil
		}
	case "debit":
		mapping.Debit = column
		if column != nil {
			mapping.Amount = nil
		}
	case "credit":
		mapping.Credit = column
		if column != nil {
			mapping.Amount = nil
		}
	case "partner":
		mapping.Partner = column
	case "purpose":
		mapping.Purpose = column
	case "balance":
		mapping.Balance = column
	}
	return mapping
}

// csvColumn converts a one-based dialog index into a zero-based header index.
func csvColumn(index int) *int {
	if index <= 0 {
		return nil
	}
	column := index - 1
	return &column
}
