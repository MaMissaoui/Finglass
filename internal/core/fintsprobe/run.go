package fintsprobe

import (
	"context"
	"errors"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

const (
	// confirmLogin is printed when the bank's app must confirm a challenge.
	confirmLogin = "Confirm the login in your banking app."
	// unsupportedTanMessage is printed instead of a TAN method name.
	unsupportedTanMessage = "TAN method not supported by the probe; choose pushTAN in your bank settings"
	// tanPrompt is the prompt for a typed TAN.
	tanPrompt = "TAN: "
)

// tanTimeout bounds a decoupled login poll. It is a variable only so a test
// can shorten it.
var tanTimeout = 5 * time.Minute

// Terminal is the probe's terminal interaction.
type Terminal interface {
	// ReadSecret reads one line without echo. The caller owns and wipes the
	// returned bytes.
	ReadSecret(prompt string) ([]byte, error)
	// Println writes one line to the terminal.
	Println(line string)
}

// Env is everything Run needs. The caller passes ownership of PIN to Run, which
// wipes it before returning.
type Env struct {
	// Inputs are the validated probe inputs.
	Inputs Inputs
	// PIN is the caller-owned PIN; Run wipes it.
	PIN *secret.Text
	// Transport carries FinTS messages. A fake transport is used in tests.
	Transport fints.Transport
	// Sleeper waits between decoupled polls. Nil uses the real sleeper.
	Sleeper fints.Sleeper
	// Terminal reads secrets without echo and prints the probe's lines.
	Terminal Terminal
	// Today is the statement period's end date.
	Today civil.Date
	// ProductVersion is the HKVVB product version.
	ProductVersion string
}

// Run performs the read-only probe and returns its exit code.
//
// It never sends a payment or an order segment, never logs a secret and never
// prints an amount or bank text. The caller passes ownership of env.PIN to Run,
// which wipes it on every path.
func Run(ctx context.Context, env Env) int {
	if env.PIN == nil {
		return 2
	}
	defer env.PIN.Wipe()
	if env.Terminal == nil {
		return 2
	}

	dialog, err := fints.NewDialog(fints.DialogConfig{
		BLZ:            env.Inputs.BLZ,
		UserID:         env.Inputs.User,
		CustomerID:     env.Inputs.User,
		ProductID:      env.Inputs.ProductID,
		ProductVersion: env.ProductVersion,
		PIN:            env.PIN,
		Transport:      env.Transport,
		Sleeper:        env.Sleeper,
	})
	if err != nil {
		reason := NoGoReason{Kind: NoGoOther, Other: "InvalidState"}
		return finish(env.Terminal, nil, nil, &reason)
	}

	if err := dialog.Sync(ctx); err != nil {
		reason := classifyError(err)
		return finish(env.Terminal, nil, nil, &reason)
	}

	initResult, err := dialog.Init(ctx)
	if err != nil {
		reason := classifyError(err)
		return finish(env.Terminal, nil, nil, &reason)
	}
	if !initResult.Opened {
		if reason, done := completeInitialTAN(ctx, env, dialog, initResult); done {
			return finish(env.Terminal, nil, nil, reason)
		}
	}

	workflow := fints.NewWorkflow(dialog, terminalPrompter{terminal: env.Terminal})
	if _, err := workflow.FetchSEPAAccounts(ctx); err != nil {
		reason := classifyError(err)
		_ = dialog.End(ctx)
		return finish(env.Terminal, nil, nil, &reason)
	}

	summaries, lines, failure := probeAccounts(ctx, env, dialog, workflow)
	_ = dialog.End(ctx)
	return finish(env.Terminal, summaries, lines, failure)
}

// completeInitialTAN handles a TAN-pending Init. done reports that the probe
// must stop with reason.
func completeInitialTAN(
	ctx context.Context,
	env Env,
	dialog *fints.Dialog,
	result fints.InitResult,
) (*NoGoReason, bool) {
	if result.Decoupled {
		env.Terminal.Println(confirmLogin)
		pollContext, cancel := context.WithTimeout(ctx, tanTimeout)
		_, err := dialog.Poll(pollContext)
		deadlineReached := pollContext.Err() == context.DeadlineExceeded && ctx.Err() == nil
		cancel()
		if err != nil {
			var reason NoGoReason
			if deadlineReached {
				reason = NoGoReason{Kind: NoGoTanTimeout}
			} else {
				reason = classifyError(err)
			}
			return &reason, true
		}
		return nil, false
	}

	methodName := ""
	if method, ok := dialog.Params().SelectedMethod(); ok {
		methodName = method.Name()
	}
	if isUnsupportedTanMethod(methodName) {
		env.Terminal.Println(unsupportedTanMessage)
		_ = dialog.Cancel(ctx)
		reason := NoGoReason{Kind: NoGoTanMethodUnsupported}
		return &reason, true
	}

	if challenge := result.Challenge.Challenge(); challenge != "" {
		env.Terminal.Println(challenge)
	}
	tan, err := readSecret(env.Terminal, tanPrompt)
	if err != nil {
		_ = dialog.Cancel(ctx)
		reason := NoGoReason{Kind: NoGoOther, Other: "PromptFailed"}
		return &reason, true
	}
	if _, err := dialog.SubmitTAN(ctx, tan); err != nil {
		reason := classifyError(err)
		return &reason, true
	}
	return nil, false
}

// probeAccounts fetches the balance and statements of every usable account,
// printing a skipped line for accounts without balance/statement access and
// stopping after the first failure.
func probeAccounts(
	ctx context.Context,
	env Env,
	dialog *fints.Dialog,
	workflow *fints.Workflow,
) ([]AccountSummary, []string, *NoGoReason) {
	summaries := make([]AccountSummary, 0)
	lines := make([]string, 0)
	from := env.Today.AddDays(-env.Inputs.Days)
	to := env.Today

	for _, account := range dialog.Params().Accounts {
		if skipAccount(workflow, account) {
			lines = append(lines, SkippedAccountLine(account.IBAN))
			continue
		}

		summary := NewAccountSummary(
			account.IBAN,
			account.Currency,
			false,
			UnavailableStatements(),
		)

		balance, ok, err := workflow.FetchBalance(ctx, account)
		if err != nil {
			reason := classifyError(err)
			summaries = append(summaries, summary)
			lines = append(lines, SummaryLine(summary))
			return summaries, lines, &reason
		}
		if ok {
			summary.BalancePresent = true
			if balance.Currency != "" {
				summary.Currency = balance.Currency
			}
		}

		statement, reason := fetchStatementState(ctx, workflow, account, from, to)
		summary.Statement = statement
		summaries = append(summaries, summary)
		lines = append(lines, SummaryLine(summary))
		if reason != nil {
			return summaries, lines, reason
		}
	}
	return summaries, lines, nil
}

// skipAccount reports whether an account has no usable balance or statement
// access. An account without an IBAN is skipped because the summary line and
// the app key accounts by IBAN. An empty allowed-transactions list means the
// bank did not restrict the account. The workflow decides whether the account
// carries the identifiers the bank's request versions need.
func skipAccount(workflow *fints.Workflow, account fints.Account) bool {
	if account.IBAN == "" {
		return true
	}
	if len(account.AllowedBusinessTransactions) > 0 {
		hasBalance := false
		hasStatement := false
		for _, transaction := range account.AllowedBusinessTransactions {
			switch transaction {
			case "HKSAL":
				hasBalance = true
			case "HKKAZ", "HKCAZ":
				hasStatement = true
			}
		}
		if !hasBalance || !hasStatement {
			return true
		}
	}
	return !workflow.CanRequest(account)
}

func fetchStatementState(
	ctx context.Context,
	workflow *fints.Workflow,
	account fints.Account,
	from, to civil.Date,
) (StatementState, *NoGoReason) {
	statements, err := workflow.FetchStatements(ctx, account, toFinTSDate(from), toFinTSDate(to))
	if err != nil {
		reason := classifyStatementsError(err)
		return UnavailableStatements(), &reason
	}

	state, err := countBooked(statements)
	if err != nil {
		reason := NoGoReason{Kind: NoGoStatementsUnavailable}
		return UnavailableStatements(), &reason
	}
	return state, nil
}

// countBooked parses only booked statement bytes, never pending ones, and
// returns the booking count and date range.
func countBooked(statements fints.Statements) (StatementState, error) {
	bookings := make([]civil.Date, 0)
	format := StatementMT940
	switch statements.Kind {
	case fints.StatementCamt:
		format = StatementCamt
		for _, document := range statements.Camt.Booked {
			parsed, err := imports.ParseCAMT(document)
			if err != nil {
				return StatementState{}, err
			}
			for _, booking := range parsed.Bookings {
				bookings = append(bookings, booking.Date)
			}
		}
	default:
		if len(statements.MT940.Booked) > 0 {
			parsed, err := imports.ParseMT940(statements.MT940.Booked)
			if err != nil {
				return StatementState{}, err
			}
			for _, booking := range parsed.Bookings {
				bookings = append(bookings, booking.Date)
			}
		}
	}

	if len(bookings) == 0 {
		return BookedStatements(0, "", "", format), nil
	}
	earliest := bookings[0]
	latest := bookings[0]
	for _, booking := range bookings[1:] {
		if booking.Before(earliest) {
			earliest = booking
		}
		if booking.After(latest) {
			latest = booking
		}
	}
	return BookedStatements(len(bookings), earliest.String(), latest.String(), format), nil
}

func toFinTSDate(date civil.Date) fints.Date {
	return fints.Date{Year: date.Year(), Month: int(date.Month()), Day: date.Day()}
}

// terminalPrompter completes TAN steps inside the balance and statement
// workflow. It prints the challenge only for a typed TAN and never a method
// name.
type terminalPrompter struct {
	terminal Terminal
}

// Challenge prints the bank's challenge and reads the TAN without echo.
func (p terminalPrompter) Challenge(
	_ context.Context,
	challenge fints.TanChallenge,
	decoupled bool,
) (*secret.Text, error) {
	if decoupled {
		return nil, errors.New("fintsprobe: unexpected decoupled challenge")
	}
	if text := challenge.Challenge(); text != "" {
		p.terminal.Println(text)
	}
	return readSecret(p.terminal, tanPrompt)
}

// Decoupled announces that the bank's app must confirm the challenge.
func (p terminalPrompter) Decoupled(context.Context, fints.TanChallenge) {
	p.terminal.Println(confirmLogin)
}

// readSecret reads bytes without echo and moves them into a secret.Text. The
// raw bytes are wiped after the copy.
func readSecret(terminal Terminal, prompt string) (*secret.Text, error) {
	raw, err := terminal.ReadSecret(prompt)
	if err != nil {
		wipeBytes(raw)
		return nil, err
	}
	text := secret.NewText(raw)
	wipeBytes(raw)
	return text, nil
}

func wipeBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func finish(
	terminal Terminal,
	summaries []AccountSummary,
	lines []string,
	failure *NoGoReason,
) int {
	for _, line := range lines {
		terminal.Println(line)
	}

	decision := Decide(summaries, failure)
	if decision.Go {
		terminal.Println("RESULT: GO")
	} else {
		terminal.Println("RESULT: NO-GO (" + decision.Reason.String() + ")")
	}
	return decision.ExitCode()
}
