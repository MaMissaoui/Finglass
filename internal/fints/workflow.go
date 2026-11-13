// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// TanPrompter is the caller's TAN interaction. The workflow never owns UI: it
// asks for a TAN or announces that the bank's app must confirm the order.
type TanPrompter interface {
	// Challenge returns the TAN for a challenge. decoupled reports that the
	// bank confirmed the TAN in its app; in that case the workflow calls
	// [TanPrompter.Decoupled] instead and never calls Challenge.
	Challenge(ctx context.Context, challenge TanChallenge, decoupled bool) (*secret.Text, error)
	// Decoupled announces that the bank's app must confirm the challenge.
	Decoupled(ctx context.Context, challenge TanChallenge)
}

// StatementKind is the closed format of fetched statements.
type StatementKind uint8

// The statement formats.
const (
	// StatementMT940 is raw MT940 from HIKAZ.
	StatementMT940 StatementKind = iota
	// StatementCamt is raw camt XML from HICAZ.
	StatementCamt
)

// String returns the kind's name. It never contains statement content.
func (k StatementKind) String() string {
	if k == StatementCamt {
		return "camt"
	}
	return "mt940"
}

// Statements holds the raw statement documents of one fetch, unchanged.
//
// Only the field matching Kind is set. MT940 concatenates HIKAZ's binary values;
// Camt keeps one byte slice per HICAZ document.
type Statements struct {
	// Kind says which field holds the documents.
	Kind StatementKind
	// MT940 is the raw MT940 data when Kind is StatementMT940.
	MT940 MT940Statement
	// Camt is the raw camt data when Kind is StatementCamt.
	Camt CamtStatement
}

// String returns only the kind and document counts, never statement content.
func (s Statements) String() string {
	return fmt.Sprintf(
		"fints.Statements{kind:%s mt940:%s camt:%s}",
		s.Kind, s.MT940, s.Camt,
	)
}

// GoString returns only the kind and document counts.
func (s Statements) GoString() string {
	return s.String()
}

// Format writes only the kind and document counts, for every fmt verb.
func (s Statements) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(s.String()))
}

// maxTouchdownPages is the hard cap for one statement fetch. Reaching it with
// another touchdown point is an [ErrorKindTouchdownLimit] error.
const maxTouchdownPages = 100

// Workflow runs the balance and statement operations of one open dialog.
type Workflow struct {
	dialog   *Dialog
	prompter TanPrompter
}

// NewWorkflow builds a workflow around a dialog. prompter may be nil for
// operations that never hit a TAN; a TAN step then fails with InvalidState.
func NewWorkflow(dialog *Dialog, prompter TanPrompter) *Workflow {
	return &Workflow{dialog: dialog, prompter: prompter}
}

// FetchBalance fetches one account's balance with HKSAL, completing any TAN
// step through the prompter. ok is false when the bank answered successfully
// without a HISAL (fints-rs's BalanceResult::Empty); the dialog stays open.
func (w *Workflow) FetchBalance(ctx context.Context, account Account) (balance Balance, ok bool, err error) {
	if w == nil || w.dialog == nil {
		return Balance{}, false, newInvalidStateError()
	}
	if err := w.ensureOpen(ctx); err != nil {
		return Balance{}, false, err
	}

	version := maxUint16(w.dialog.params.SupportedVersion(BusinessHKSAL, 7), 5)
	segment := NewHKSAL(version, account.IBAN, account.BIC, nil)
	response, _, err := w.send(ctx, segment, true)
	if err != nil {
		return Balance{}, false, err
	}

	balance, ok = response.Balance()
	return balance, ok, nil
}

// FetchStatements fetches one account's statements from HKCAZ when the bank
// offers an accepted camt format, otherwise from HKKAZ. It follows touchdown
// pages up to [maxTouchdownPages] and returns the raw documents unchanged.
func (w *Workflow) FetchStatements(
	ctx context.Context,
	account Account,
	from, to Date,
) (Statements, error) {
	if w == nil || w.dialog == nil {
		return Statements{}, newInvalidStateError()
	}
	if err := w.ensureOpen(ctx); err != nil {
		return Statements{}, err
	}

	camtFormat, useCamt := acceptedCamtFormat(w.dialog.params.CamtFormats)
	statements := Statements{Kind: StatementMT940}
	if useCamt {
		statements.Kind = StatementCamt
	}

	touchdown := ""
	for page := 1; ; page++ {
		segment := w.statementSegment(account, from, to, touchdown, camtFormat, useCamt)
		// HKTAN process 4 goes with the first page only, as in fints-rs.
		response, next, err := w.send(ctx, segment, touchdown == "")
		if err != nil {
			return Statements{}, err
		}

		if useCamt {
			part := ExtractCamt(response.Segments)
			statements.Camt.Booked = append(statements.Camt.Booked, part.Booked...)
			statements.Camt.Pending = append(statements.Camt.Pending, part.Pending...)
		} else {
			part := ExtractMT940(response.Segments)
			statements.MT940.Booked = append(statements.MT940.Booked, part.Booked...)
			statements.MT940.Pending = append(statements.MT940.Pending, part.Pending...)
		}

		if next == "" {
			return statements, nil
		}
		if page >= maxTouchdownPages {
			_ = w.dialog.End(ctx)
			return Statements{}, &DialogError{Kind: ErrorKindTouchdownLimit}
		}
		touchdown = next
	}
}

func (w *Workflow) statementSegment(
	account Account,
	from, to Date,
	touchdown, camtFormat string,
	useCamt bool,
) Segment {
	var point *string
	if touchdown != "" {
		value := touchdown
		point = &value
	}
	if useCamt {
		version := w.dialog.params.SupportedVersion(BusinessHKCAZ, 7)
		return NewHKCAZ(version, account.IBAN, account.BIC, []string{camtFormat}, false, from, to, nil, point)
	}
	version := maxUint16(w.dialog.params.SupportedVersion(BusinessHKKAZ, 7), 5)
	return NewHKKAZ(version, account.IBAN, account.BIC, from, to, point)
}

// ensureOpen completes a pending initial TAN and requires an open dialog.
func (w *Workflow) ensureOpen(ctx context.Context) error {
	switch w.dialog.state {
	case DialogOpen:
		return nil
	case DialogTANPending:
		_, err := w.completeTAN(ctx, w.dialog.challenge, w.dialog.decoupled)
		return err
	default:
		return newInvalidStateError()
	}
}

// send sends one business segment and, when the bank asks for a TAN, completes
// the TAN step. It returns the response that holds the data and its touchdown
// point.
func (w *Workflow) send(ctx context.Context, segment Segment, withTAN bool) (*Response, string, error) {
	result, err := w.dialog.send(ctx, segment, withTAN)
	if err != nil {
		return nil, "", err
	}
	if result.Kind != SendNeedTAN {
		return result.Response, result.Touchdown, nil
	}

	response, err := w.completeTAN(ctx, result.Challenge, result.Decoupled)
	if err != nil {
		return nil, "", err
	}
	if point, ok := response.Touchdown(); ok {
		return response, point, nil
	}
	return response, "", nil
}

func (w *Workflow) completeTAN(
	ctx context.Context,
	challenge TanChallenge,
	decoupled bool,
) (*Response, error) {
	if w.prompter == nil {
		_ = w.dialog.End(ctx)
		return nil, newInvalidStateError()
	}

	if decoupled {
		w.prompter.Decoupled(ctx, challenge)
		result, err := w.dialog.Poll(ctx)
		if err != nil {
			return nil, err
		}
		return result.Response, nil
	}

	tan, err := w.prompter.Challenge(ctx, challenge, false)
	if err != nil {
		if tan != nil {
			tan.Wipe()
		}
		_ = w.dialog.End(ctx)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, newCancelledError(ctxErr)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, newCancelledError(err)
		}
		// A prompter error may carry UI text; keep only the kind.
		return nil, newCancelledError(context.Canceled)
	}

	response, err := w.dialog.SubmitTAN(ctx, tan)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// acceptedCamtFormat returns the first accepted camt URN the bank offers.
func acceptedCamtFormat(formats []string) (string, bool) {
	for _, format := range formats {
		if isAcceptedCamtFormat(format) {
			return format, true
		}
	}
	return "", false
}

// isAcceptedCamtFormat accepts camt.052.001.02, camt.052.001.08 and every
// camt.053 variant.
func isAcceptedCamtFormat(format string) bool {
	lower := strings.ToLower(format)
	switch {
	case strings.HasSuffix(lower, "camt.052.001.02"),
		strings.HasSuffix(lower, "camt.052.001.08"),
		strings.Contains(lower, "camt.053"):
		return true
	default:
		return false
	}
}

func maxUint16(left, right uint16) uint16 {
	if left > right {
		return left
	}
	return right
}
