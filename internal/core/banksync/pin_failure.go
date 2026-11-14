package banksync

import (
	"context"
	"fmt"
	"strconv"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

// PinFailureInput is one bank answer after a stored PIN was used. It carries
// bank return codes only, never bank text.
type PinFailureInput struct {
	// Codes are the bank's four-digit return codes.
	Codes []string
	// ExplicitRejection is set when the source already established that the
	// bank answered with an explicit wrong-PIN code, for example the gateway's
	// PIN-rejected kind, which carries no codes.
	ExplicitRejection bool
	// PINRejected is set when the source classified the answer as a PIN
	// rejection, even when no explicit wrong-PIN code is present.
	PINRejected bool
	// AccountLocked is set for the bank's locked-account answer (3938).
	AccountLocked bool
}

// PinFailureClass is what one bank answer means for a stored PIN.
type PinFailureClass int

const (
	// PinFailureKeep leaves the stored PIN and the rejection count as they are.
	PinFailureKeep PinFailureClass = iota
	// PinFailureRemoveNow deletes the stored PIN at once.
	PinFailureRemoveNow
	// PinFailureCount adds one "other" rejection and removes the PIN only when
	// the bank's option says so.
	PinFailureCount
)

// PinFailureResult is what applying one bank answer did.
type PinFailureResult int

const (
	// PinFailureIgnored changed nothing.
	PinFailureIgnored PinFailureResult = iota
	// PinFailureCounted kept the PIN and advanced the rejection count.
	PinFailureCounted
	// PinFailureRemoved deleted the stored PIN.
	PinFailureRemoved
)

// PinFailureFromGateway adapts the sync gateway's failure to the shared
// classifier.
func PinFailureFromGateway(failure GatewayError) PinFailureInput {
	codes := failure.Codes()
	return PinFailureInput{
		Codes:             codes,
		ExplicitRejection: failure.Kind() == GatewayErrorPinRejected,
		PINRejected:       failure.PINRejectedKind(),
		AccountLocked:     containsCode(codes, "3938"),
	}
}

// PinFailureFromCodes adapts a connection test's NO-GO answer: loginRejected
// covers the probe's NoGoLoginRejected (a PIN rejection or a locked account).
func PinFailureFromCodes(codes []string, loginRejected bool) PinFailureInput {
	return PinFailureInput{
		Codes:         append([]string(nil), codes...),
		PINRejected:   loginRejected,
		AccountLocked: containsCode(codes, "3938"),
	}
}

// ClassifyPinFailure decides what a bank answer means for a stored PIN.
func ClassifyPinFailure(failure PinFailureInput) PinFailureClass {
	if failure.ExplicitRejection ||
		failure.AccountLocked ||
		fints.IsExplicitPINRejection(failure.Codes) {
		return PinFailureRemoveNow
	}
	if failure.PINRejected || fints.BlocksPINRepeat(failure.Codes) {
		return PinFailureCount
	}
	return PinFailureKeep
}

// PinForgetAfterKey is the settings key of a bank access's stored-PIN removal
// option: "1" removes it after the first rejection, "2" after the second.
func PinForgetAfterKey(bankID model.BankConnectionID) string {
	return "fints.pin_forget_after." + strconv.FormatInt(int64(bankID), 10)
}

// PinRejectionsKey is the settings key of a bank access's count of "other"
// PIN rejections in a row.
func PinRejectionsKey(bankID model.BankConnectionID) string {
	return "fints.pin_rejections." + strconv.FormatInt(int64(bankID), 10)
}

// PinForgetAfter returns the bank's removal option, defaulting to "1".
func (c *FinTsClient) pinForgetAfter(ctx context.Context, database *db.Database, bankID model.BankConnectionID) string {
	value, ok, err := settings.Get(ctx, database, PinForgetAfterKey(bankID))
	if err != nil || !ok {
		return "1"
	}
	if value == "2" {
		return "2"
	}
	return "1"
}

// ResetPinRejections clears the "other" rejection count after a successful
// dialog, a forgotten PIN, a login change or a newly saved PIN.
func (c *FinTsClient) resetPinRejections(ctx context.Context, database *db.Database, bankID model.BankConnectionID) {
	_ = deleteSetting(ctx, database, PinRejectionsKey(bankID))
}

// applyPinFailure applies one bank answer to the stored PIN and its rejection
// count. It never stores a PIN.
func (c *FinTsClient) applyPinFailure(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	failure PinFailureInput,
) PinFailureResult {
	class := ClassifyPinFailure(failure)
	if class == PinFailureKeep {
		return PinFailureIgnored
	}
	// A re-prompt may already have removed the PIN (the remembering
	// interaction forgets it before asking again). Then there is nothing left
	// to count.
	if !c.HasSavedPin(ctx, database, bankID) {
		return PinFailureIgnored
	}
	if class == PinFailureRemoveNow {
		c.ForgetPin(ctx, database, bankID)
		return PinFailureRemoved
	}

	count := 0
	if value, ok, err := settings.Get(ctx, database, PinRejectionsKey(bankID)); err == nil && ok {
		if parsed, parseErr := strconv.Atoi(value); parseErr == nil && parsed > 0 {
			count = parsed
		}
	}
	count++
	option := 1
	if c.pinForgetAfter(ctx, database, bankID) == "2" {
		option = 2
	}
	if count >= option {
		c.ForgetPin(ctx, database, bankID)
		return PinFailureRemoved
	}
	_ = settings.Set(ctx, database, PinRejectionsKey(bankID), strconv.Itoa(count))
	return PinFailureCounted
}

// savePinForgetAfter stores the removal option and audits a real change. The
// caller passes the option's value before the save so a login reset in between
// cannot turn an unchanged choice into an audit entry.
func (c *FinTsClient) savePinForgetAfter(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	value,
	previous string,
) error {
	if value != "1" && value != "2" {
		return NewBankingError(CodeSyncFailed, i18n.Text(c.language, i18n.ErrorUnexpected))
	}
	if err := settings.Set(ctx, database, PinForgetAfterKey(bankID), value); err != nil {
		return internalError(err)
	}
	if previous == "" {
		previous = "1"
	}
	if value != previous {
		option, _ := strconv.Atoi(value)
		_ = audit.Log(
			ctx,
			database,
			CodePinForgetAfter,
			audit.ActorUser,
			fmt.Sprintf("Bank access %d: remove the saved PIN after rejection %d", int64(bankID), option),
		)
	}
	return nil
}

func containsCode(codes []string, wanted string) bool {
	for _, code := range codes {
		if code == wanted {
			return true
		}
	}
	return false
}

// ApplyPinFailure applies one bank answer to a bank's stored PIN and its
// rejection count. It never stores a PIN.
func (s *BankingService) ApplyPinFailure(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	failure PinFailureInput,
) PinFailureResult {
	return s.client.applyPinFailure(ctx, database, bankID, failure)
}

// ResetPinRejections clears a bank's "other" rejection count.
func (s *BankingService) ResetPinRejections(ctx context.Context, database *db.Database, bankID model.BankConnectionID) {
	s.client.resetPinRejections(ctx, database, bankID)
}

// PinForgetAfter returns a bank's stored-PIN removal option ("1" or "2").
func (s *BankingService) PinForgetAfter(ctx context.Context, database *db.Database, bankID model.BankConnectionID) string {
	return s.client.pinForgetAfter(ctx, database, bankID)
}

// SavePinForgetAfter stores a bank's stored-PIN removal option. previous is the
// value from before the dialog saved, so only a real change is audited.
func (s *BankingService) SavePinForgetAfter(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	value,
	previous string,
) error {
	return s.client.savePinForgetAfter(ctx, database, bankID, value, previous)
}
