// Duplicate detection shared by FinTS sync and file import, ported from
// FinDesk's BookingDedup and crates/finglass-core/src/import/dedup.rs.
//
// Exact: a booking whose hash is already stored. Probable: the same booking
// from a different source (FinTS vs CSV vs CAMT vs manual), whose
// partner/purpose text and therefore hash differ. For those, bookings with the
// same account, booking date and amount are compared by count: if 2 such
// bookings exist and 3 arrive, 1 is new. Genuine repeats (two coffees) are
// kept, and a different amount on the same day is always new.
//
// A booking the user deleted is remembered by hash:
// such a candidate has no stored row but is counted in Plan.PreviouslyDeleted
// and skipped, unless a row with the same hash is stored again — a stored row
// always wins over a remembered deletion.
package imports

import (
	"context"
	"sort"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

// Candidate is one booking to plan, with the fields duplicate detection
// compares.
type Candidate[T any] struct {
	// Item is the booking itself.
	Item T
	// Date is its booking date.
	Date civil.Date
	// Amount is its signed amount.
	Amount decimal.Decimal
	// Hash is its dedup hash (see Hashes).
	Hash string
}

// Plan is what PlanDuplicates decided.
type Plan[T any] struct {
	// ToImport are the candidates to insert, in their original order.
	ToImport []Candidate[T]
	// ExactDuplicates counts candidates whose hash is already stored.
	ExactDuplicates int
	// ProbableDuplicates counts candidates skipped as probable duplicates of
	// an existing same-day, same-amount booking.
	ProbableDuplicates int
	// PreviouslyDeleted counts candidates skipped because the user deleted a
	// booking with that hash earlier.
	PreviouslyDeleted int
}

type dayAmountKey struct {
	date   civil.Date
	amount string
}

func keyOf[T any](candidate Candidate[T]) dayAmountKey {
	return dayAmountKey{
		date:   candidate.Date,
		amount: candidate.Amount.Normalize().String(),
	}
}

type indexedCandidate[T any] struct {
	index     int
	candidate Candidate[T]
}

// PlanDuplicates plans which candidates to import, preserving their order.
func PlanDuplicates[T any](
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	currency money.Currency,
	candidates []Candidate[T],
) (Plan[T], error) {
	// Indexed so identical bookings (equal candidates) keep their own identity
	// and the file order.
	known := make([]indexedCandidate[T], 0)
	rest := make([]indexedCandidate[T], 0)
	for index, candidate := range candidates {
		found, err := transactions.FindByHash(ctx, d, accountID, candidate.Hash)
		if err != nil {
			return Plan[T]{}, err
		}
		item := indexedCandidate[T]{index: index, candidate: candidate}
		if found {
			known = append(known, item)
		} else {
			rest = append(rest, item)
		}
	}

	// Remembered deletions are read once per plan; they are skipped, not
	// counted as known or probable.
	deleted, err := transactions.DeletedHashes(ctx, d, accountID)
	if err != nil {
		return Plan[T]{}, err
	}
	previouslyDeleted := make([]indexedCandidate[T], 0)
	unknown := make([]indexedCandidate[T], 0)
	for _, item := range rest {
		if _, isDeleted := deleted[item.candidate.Hash]; isDeleted {
			previouslyDeleted = append(previouslyDeleted, item)
		} else {
			unknown = append(unknown, item)
		}
	}

	knownPerDay := make(map[dayAmountKey]int)
	for _, item := range known {
		knownPerDay[keyOf(item.candidate)]++
	}

	groups := make(map[dayAmountKey][]indexedCandidate[T])
	order := make([]dayAmountKey, 0)
	for _, item := range unknown {
		groupKey := keyOf(item.candidate)
		if _, present := groups[groupKey]; !present {
			order = append(order, groupKey)
		}
		groups[groupKey] = append(groups[groupKey], item)
	}

	toImport := make([]indexedCandidate[T], 0)
	probableDuplicates := 0
	for _, groupKey := range order {
		group := groups[groupKey]
		// Existing bookings that this batch's exact matches don't already
		// account for.
		amountMinor, err := AmountToMoney(group[0].candidate.Amount, currency)
		if err != nil {
			return Plan[T]{}, err
		}
		existing, err := transactions.CountSameBooking(
			ctx, d, accountID, groupKey.date, amountMinor.Minor,
		)
		if err != nil {
			return Plan[T]{}, err
		}
		alreadyKnown := int64(knownPerDay[groupKey])
		unmatchedExisting := existing - alreadyKnown
		if unmatchedExisting < 0 {
			unmatchedExisting = 0
		}
		skip := int(unmatchedExisting)
		if skip > len(group) {
			skip = len(group)
		}
		probableDuplicates += skip
		toImport = append(toImport, group[skip:]...)
	}

	sort.SliceStable(toImport, func(left, right int) bool {
		return toImport[left].index < toImport[right].index
	})
	imports := make([]Candidate[T], len(toImport))
	for index, item := range toImport {
		imports[index] = item.candidate
	}

	return Plan[T]{
		ToImport:           imports,
		ExactDuplicates:    len(known),
		ProbableDuplicates: probableDuplicates,
		PreviouslyDeleted:  len(previouslyDeleted),
	}, nil
}
