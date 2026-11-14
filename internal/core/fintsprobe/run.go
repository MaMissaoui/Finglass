package fintsprobe

import (
	"context"
	"errors"
	"fmt"
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

// Notice is a fixed notice the probe reports through [Interaction].
type Notice uint8

// The probe notices.
const (
	// NoticeTanMethodUnsupported tells the person to choose a pushTAN method.
	NoticeTanMethodUnsupported Notice = iota
)

// Interaction is how the probe talks to the person running it.
type Interaction interface {
	// TAN shows the bank's challenge (may be empty) and returns the typed TAN.
	// A nil TAN or an error cancels the probe. The probe owns and wipes the TAN.
	TAN(ctx context.Context, challenge string) (*secret.Text, error)
	// ConfirmInApp says the bank's app must confirm (pushTAN / decoupled). The
	// probe polls meanwhile; the person cancels by cancelling ctx.
	ConfirmInApp(ctx context.Context, challenge string)
	// Notice reports a fixed notice.
	Notice(notice Notice)
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

// ProbeEnv is everything [Probe] needs. The caller passes ownership of PIN to
// Probe, which wipes it before returning.
type ProbeEnv struct {
	// Inputs are the validated probe inputs.
	Inputs Inputs
	// PIN is the caller-owned PIN; Probe wipes it.
	PIN *secret.Text
	// Transport carries FinTS messages. A fake transport is used in tests.
	Transport fints.Transport
	// Sleeper waits between decoupled polls. Nil uses the real sleeper.
	Sleeper fints.Sleeper
	// Interaction talks to the person running the probe.
	Interaction Interaction
	// Today is the statement period's end date.
	Today civil.Date
	// ProductVersion is the HKVVB product version.
	ProductVersion string
}

// String prints the masked inputs and the presence of the PIN. It never prints
// the PIN, the product id, the user id or the URL path.
func (e ProbeEnv) String() string {
	pin := `""`
	if e.PIN != nil {
		pin = "***"
	}
	return fmt.Sprintf(
		"fintsprobe.ProbeEnv{inputs:%s pin:%s today:%s productVersion:%s}",
		e.Inputs, pin, e.Today, maskedPresence(e.ProductVersion),
	)
}

// GoString prints the same masked form as String.
func (e ProbeEnv) GoString() string {
	return e.String()
}

// Format writes the masked form for every fmt verb.
func (e ProbeEnv) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(e.String()))
}

// Result is the probe's outcome without any terminal formatting.
type Result struct {
	// Decision is the GO/NO-GO decision.
	Decision Decision
	// Summaries are the probed accounts, in wire order.
	Summaries []AccountSummary
	// Lines are the account lines, including skipped ones, exactly as the
	// terminal prints them.
	Lines []string
	// Probed is the number of accounts the probe attempted.
	Probed int
	// Skipped is the number of accounts the probe skipped.
	Skipped int
	// Invalid reports invalid input (nil PIN or nil interaction); Run exits 2.
	Invalid bool
}

// String prints only the decision and counts, never an account or bank text.
func (r Result) String() string {
	decision := "NO-GO (" + r.Decision.Reason.String() + ")"
	if r.Decision.Go {
		decision = "GO"
	}
	return fmt.Sprintf(
		"fintsprobe.Result{decision:%s probed:%d skipped:%d invalid:%t lines:%d}",
		decision, r.Probed, r.Skipped, r.Invalid, len(r.Lines),
	)
}

// GoString prints the same masked form as String.
func (r Result) GoString() string {
	return r.String()
}

// Format writes the masked form for every fmt verb.
func (r Result) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(r.String()))
}

// Run performs the read-only probe and returns its exit code.
//
// It adapts the terminal to [Interaction], prints [Result.Lines] and the RESULT
// line, and returns 2 for an invalid environment.
//
// It never sends a payment or an order segment, never logs a secret and never
// prints an amount or bank text. The caller passes ownership of env.PIN to Run,
// which wipes it on every path.
func Run(ctx context.Context, env Env) int {
	var interaction Interaction
	if env.Terminal != nil {
		interaction = terminalInteraction{terminal: env.Terminal}
	}
	result := Probe(ctx, ProbeEnv{
		Inputs:         env.Inputs,
		PIN:            env.PIN,
		Transport:      env.Transport,
		Sleeper:        env.Sleeper,
		Interaction:    interaction,
		Today:          env.Today,
		ProductVersion: env.ProductVersion,
	})
	if result.Invalid {
		return 2
	}

	for _, line := range result.Lines {
		env.Terminal.Println(line)
	}
	if result.Decision.Go {
		env.Terminal.Println("RESULT: GO")
	} else {
		env.Terminal.Println("RESULT: NO-GO (" + result.Decision.Reason.String() + ")")
	}
	return result.Decision.ExitCode()
}

// Probe performs the read-only probe and returns its outcome without printing.
//
// It sends no payment or order segment, prints nothing and never returns bank
// text. The caller passes ownership of env.PIN to Probe, which wipes it on every
// path. A nil PIN or a nil Interaction makes the result invalid.
func Probe(ctx context.Context, env ProbeEnv) Result {
	if env.PIN == nil {
		return invalidResult()
	}
	defer env.PIN.Wipe()
	if env.Interaction == nil {
		return invalidResult()
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
		return resultFrom(nil, nil, 0, &reason)
	}

	if err := dialog.Sync(ctx); err != nil {
		reason := classifyError(err)
		return resultFrom(nil, nil, 0, &reason)
	}

	initResult, err := dialog.Init(ctx)
	if err != nil {
		reason := classifyError(err)
		return resultFrom(nil, nil, 0, &reason)
	}
	if !initResult.Opened {
		if reason, done := completeInitialTAN(ctx, env, dialog, initResult); done {
			return resultFrom(nil, nil, 0, reason)
		}
	}

	workflow := fints.NewWorkflow(dialog, interactionPrompter{interaction: env.Interaction})
	if _, err := workflow.FetchSEPAAccounts(ctx); err != nil {
		reason := classifyError(err)
		_ = dialog.End(ctx)
		return resultFrom(nil, nil, 0, &reason)
	}

	summaries, lines, skipped, failure := probeAccounts(ctx, env, dialog, workflow)
	_ = dialog.End(ctx)
	return resultFrom(summaries, lines, skipped, failure)
}

func invalidResult() Result {
	return Result{
		Decision: Decision{Reason: NoGoReason{Kind: NoGoOther, Other: "InvalidState"}},
		Invalid:  true,
	}
}

func resultFrom(
	summaries []AccountSummary,
	lines []string,
	skipped int,
	failure *NoGoReason,
) Result {
	return Result{
		Decision:  Decide(summaries, failure),
		Summaries: summaries,
		Lines:     lines,
		Probed:    len(summaries),
		Skipped:   skipped,
	}
}

// completeInitialTAN handles a TAN-pending Init. done reports that the probe
// must stop with reason.
func completeInitialTAN(
	ctx context.Context,
	env ProbeEnv,
	dialog *fints.Dialog,
	result fints.InitResult,
) (*NoGoReason, bool) {
	if result.Decoupled {
		env.Interaction.ConfirmInApp(ctx, result.Challenge.Challenge())
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
		env.Interaction.Notice(NoticeTanMethodUnsupported)
		_ = dialog.Cancel(ctx)
		reason := NoGoReason{Kind: NoGoTanMethodUnsupported}
		return &reason, true
	}

	tan, err := env.Interaction.TAN(ctx, result.Challenge.Challenge())
	if err != nil || tan == nil {
		if tan != nil {
			tan.Wipe()
		}
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
// adding a skipped line for accounts without balance/statement access and
// stopping after the first failure.
func probeAccounts(
	ctx context.Context,
	env ProbeEnv,
	dialog *fints.Dialog,
	workflow *fints.Workflow,
) ([]AccountSummary, []string, int, *NoGoReason) {
	summaries := make([]AccountSummary, 0)
	lines := make([]string, 0)
	skipped := 0
	from := env.Today.AddDays(-env.Inputs.Days)
	to := env.Today

	for _, account := range dialog.Params().Accounts {
		if skipAccount(workflow, account) {
			lines = append(lines, SkippedAccountLine(account.IBAN))
			skipped++
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
			return summaries, lines, skipped, &reason
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
			return summaries, lines, skipped, reason
		}
	}
	return summaries, lines, skipped, nil
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

// interactionPrompter completes TAN steps inside the balance and statement
// workflow through the probe's Interaction.
type interactionPrompter struct {
	interaction Interaction
}

// Challenge asks the interaction for the TAN and never a method name.
func (p interactionPrompter) Challenge(
	ctx context.Context,
	challenge fints.TanChallenge,
	decoupled bool,
) (*secret.Text, error) {
	if decoupled {
		return nil, errors.New("fintsprobe: unexpected decoupled challenge")
	}
	tan, err := p.interaction.TAN(ctx, challenge.Challenge())
	if err == nil && tan == nil {
		// A dismissed TAN prompt cancels, like a prompt error.
		return nil, errTANDismissed
	}
	return tan, err
}

// errTANDismissed reports a TAN prompt that returned neither a TAN nor an
// error, for example a closed dialog in the app.
var errTANDismissed = errors.New("fintsprobe: TAN prompt dismissed")

// Decoupled announces that the bank's app must confirm the challenge.
func (p interactionPrompter) Decoupled(ctx context.Context, challenge fints.TanChallenge) {
	p.interaction.ConfirmInApp(ctx, challenge.Challenge())
}

// terminalInteraction adapts Terminal to Interaction for Run.
type terminalInteraction struct {
	terminal Terminal
}

// TAN prints the challenge when non-empty and reads the TAN without echo.
func (t terminalInteraction) TAN(_ context.Context, challenge string) (*secret.Text, error) {
	if challenge != "" {
		t.terminal.Println(challenge)
	}
	return readSecret(t.terminal, tanPrompt)
}

// ConfirmInApp prints the fixed confirmation line, ignoring the challenge.
func (t terminalInteraction) ConfirmInApp(context.Context, string) {
	t.terminal.Println(confirmLogin)
}

// Notice prints the fixed notice line.
func (t terminalInteraction) Notice(notice Notice) {
	switch notice {
	case NoticeTanMethodUnsupported:
		t.terminal.Println(unsupportedTanMessage)
	}
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
