package connectiontest

import "github.com/MaMissaoui/Finglass/internal/core/fintsprobe"

// SyncUnlockAction is what a completed connection test does to a bank's sync
// unlock.
type SyncUnlockAction int

const (
	// SyncUnlockUnchanged leaves the stored unlock as it was.
	SyncUnlockUnchanged SyncUnlockAction = iota
	// SyncUnlockSet unlocks the bank for sync.
	SyncUnlockSet
	// SyncUnlockClear locks the bank for sync.
	SyncUnlockClear
)

// SyncUnlockActionFor maps a completed test outcome to the unlock change. A GO
// unlocks unless it needed the strong-sync fallback. A NO-GO in which the bank answered locks. A cancel, TAN timeout,
// prompt failure, transport/network failure, invalid input or missing product
// id leaves the unlock unchanged.
func SyncUnlockActionFor(outcome Outcome) SyncUnlockAction {
	if outcome.Cancelled || outcome.Other == OtherCancelled {
		return SyncUnlockUnchanged
	}
	if outcome.Go && outcome.StrongSync {
		// The GO needed a fresh registration with a TAN, which the sync
		// gateway does not do yet: unlocking would let every Sync now repeat
		// the bank's rejection. The result line says why it stays locked.
		return SyncUnlockUnchanged
	}
	if outcome.Go {
		return SyncUnlockSet
	}
	return syncUnlockActionForReason(fintsprobe.NoGoReason{
		Kind:  outcome.Kind,
		Other: outcome.Other,
	})
}

// otherReasonActions decides every NoGoOther reason the probe and this package
// produce. A bank answer locks; anything that never reached a bank verdict
// (cancel, timeout, limits, local or transport failures) leaves the unlock as
// it was. A test fails when a producer adds a reason missing here.
var otherReasonActions = map[string]SyncUnlockAction{
	"PinRejected":         SyncUnlockClear,
	"AccountLocked":       SyncUnlockClear,
	"TanRejected":         SyncUnlockClear,
	"BankError":           SyncUnlockClear,
	"Protocol":            SyncUnlockClear,
	"NoAccounts":          SyncUnlockClear,
	"BalanceAbsent":       SyncUnlockClear,
	"Transport":           SyncUnlockUnchanged,
	"Cancelled":           SyncUnlockUnchanged,
	"PollLimit":           SyncUnlockUnchanged,
	"TouchdownLimit":      SyncUnlockUnchanged,
	"InvalidState":        SyncUnlockUnchanged,
	"PromptFailed":        SyncUnlockUnchanged,
	"Unknown":             SyncUnlockUnchanged,
	OtherInvalidLogin:     SyncUnlockUnchanged,
	OtherProductIDMissing: SyncUnlockUnchanged,
	OtherProductIDInvalid: SyncUnlockUnchanged,
}

func syncUnlockActionForReason(reason fintsprobe.NoGoReason) SyncUnlockAction {
	switch reason.Kind {
	case fintsprobe.NoGoLoginRejected,
		fintsprobe.NoGoTanMethodUnsupported,
		fintsprobe.NoGoStatementsUnavailable:
		return SyncUnlockClear
	case fintsprobe.NoGoTanTimeout:
		return SyncUnlockUnchanged
	case fintsprobe.NoGoOther:
		if action, ok := otherReasonActions[reason.Other]; ok {
			return action
		}
		return SyncUnlockUnchanged
	default:
		return SyncUnlockUnchanged
	}
}
