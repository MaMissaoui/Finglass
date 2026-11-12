// Package accountgrouping groups accounts for the account panel and decides
// their balance tones.
//
// It ports the non-UI part of crates/finglass-core/src/account_grouping.rs. The
// Compose geometry (drag targets with offsets and rects) has no counterpart
// here; only grouping, ordering, totals and balance tones are part of the core.
package accountgrouping

import (
	"math"
	"sort"
	"strconv"

	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// AccountItem is the account fields the grouping needs, with the unread-booking
// counter the account panel shows.
type AccountItem struct {
	// ID is the row id.
	ID model.AccountID
	// Name is the user's own label, when set.
	Name *string
	// Currency is the account currency.
	Currency money.Currency
	// Balance is the latest known balance.
	Balance money.Money
	// AccountType is the account type text ("checking", "savings",
	// "credit_card", "loan", "depot", or another type).
	AccountType string
	// GroupID is the custom group the account belongs to; nil keeps it in its
	// account-type group.
	GroupID *model.AccountGroupID
	// NewCount is how many bookings a bank sync brought in that the user has
	// not read yet.
	NewCount int64
}

// GroupKeyKind says whether a GroupKey stands for a built-in account-type
// group or one of the user's custom groups.
type GroupKeyKind uint8

const (
	// GroupKeyType is a built-in account-type group.
	GroupKeyType GroupKeyKind = iota
	// GroupKeyCustom is a custom group.
	GroupKeyCustom
)

// GroupKey is what an AccountGroupView stands for: a built-in account-type
// group or one of the user's custom groups.
type GroupKey struct {
	// Kind says which field identifies the group.
	Kind GroupKeyKind
	// AccountType is the account type of a GroupKeyType.
	AccountType string
	// GroupID is the row id of a GroupKeyCustom.
	GroupID model.AccountGroupID
}

// TypeKey returns the key of the built-in group for an account type.
func TypeKey(accountType string) GroupKey {
	return GroupKey{Kind: GroupKeyType, AccountType: accountType}
}

// CustomKey returns the key of a custom group.
func CustomKey(groupID model.AccountGroupID) GroupKey {
	return GroupKey{Kind: GroupKeyCustom, GroupID: groupID}
}

// ID returns the stable id used for the persisted selection and the collapsed
// state.
func (key GroupKey) ID() string {
	if key.Kind == GroupKeyCustom {
		return "custom:" + strconv.FormatInt(int64(key.GroupID), 10)
	}
	return "type:" + key.AccountType
}

// AccountGroupView is one entry in the account panel: its key, an optional
// custom name and the accounts with their per-currency totals.
type AccountGroupView struct {
	// Key is the group key.
	Key GroupKey
	// Name is the custom group's name, or nil for a type group (the UI
	// localizes those).
	Name *string
	// Accounts are the accounts in the group, in input order.
	Accounts []AccountItem
	// Totals are the balances per currency, primary first. Different
	// currencies are never added up.
	Totals []money.CurrencyTotal
	// NewCount is the sum of the accounts' unread bookings, for the panel's
	// counter badge.
	NewCount int64
}

// LoanType is the account type of a loan, whose balance is negative by nature.
const LoanType = "loan"

// AccountTypes are the account types FinDesk offers, in the order their groups
// are shown.
var AccountTypes = []string{"checking", "savings", "credit_card", LoanType, "depot"}

// Group puts every account into its custom group when AccountItem.GroupID names
// an existing group, and into its account-type group otherwise (an unknown
// group id falls back to the type).
//
// Type groups with no accounts are left out; custom groups are always shown so
// an empty one can receive accounts. Accounts inside a group keep the input
// order, which is the repository order.
func Group(accounts []AccountItem, groups []model.AccountGroup, primary money.Currency) []AccountGroupView {
	known := make(map[model.AccountGroupID]struct{}, len(groups))
	customAccounts := make(map[model.AccountGroupID][]AccountItem, len(groups))
	for _, group := range groups {
		known[group.ID] = struct{}{}
		customAccounts[group.ID] = nil
	}
	typeAccounts := make(map[string][]AccountItem)

	for _, account := range accounts {
		if account.GroupID != nil {
			if _, ok := known[*account.GroupID]; ok {
				customAccounts[*account.GroupID] = append(customAccounts[*account.GroupID], account)
				continue
			}
		}
		typeAccounts[account.AccountType] = append(typeAccounts[account.AccountType], account)
	}

	typeKeys := make([]string, 0, len(AccountTypes)+len(typeAccounts))
	typeKeys = append(typeKeys, AccountTypes...)
	unknown := make([]string, 0, len(typeAccounts))
	for accountType := range typeAccounts {
		if !isKnownType(accountType) {
			unknown = append(unknown, accountType)
		}
	}
	sort.Strings(unknown)
	typeKeys = append(typeKeys, unknown...)

	views := make([]AccountGroupView, 0, len(typeKeys)+len(groups))
	for _, accountType := range typeKeys {
		accountsInGroup := typeAccounts[accountType]
		if len(accountsInGroup) == 0 {
			continue
		}
		views = append(views, AccountGroupView{
			Key:      TypeKey(accountType),
			Name:     nil,
			Accounts: accountsInGroup,
			Totals:   totals(accountsInGroup, primary),
			NewCount: sumNewCount(accountsInGroup),
		})
	}

	for _, custom := range groups {
		accountsInGroup := customAccounts[custom.ID]
		delete(customAccounts, custom.ID)
		name := custom.Name
		views = append(views, AccountGroupView{
			Key:      CustomKey(custom.ID),
			Name:     &name,
			Accounts: accountsInGroup,
			Totals:   totals(accountsInGroup, primary),
			NewCount: sumNewCount(accountsInGroup),
		})
	}

	return views
}

func isKnownType(accountType string) bool {
	for _, known := range AccountTypes {
		if accountType == known {
			return true
		}
	}
	return false
}

// GrandTotal returns the balances per currency, primary first; different
// currencies are never added up.
func GrandTotal(accounts []AccountItem, primary money.Currency) []money.CurrencyTotal {
	return totals(accounts, primary)
}

// totals returns the balances per currency, primary first.
func totals(accounts []AccountItem, primary money.Currency) []money.CurrencyTotal {
	sums := make(map[money.Currency]int64)
	for _, account := range accounts {
		sums[account.Currency] = addSaturating(sums[account.Currency], account.Balance.Minor)
	}

	entries := make([]money.CurrencyTotal, 0, len(sums))
	for currency, total := range sums {
		entries = append(entries, money.CurrencyTotal{Currency: currency, Total: total})
	}
	sort.SliceStable(entries, func(left, right int) bool {
		leftPrimary := entries[left].Currency != primary
		rightPrimary := entries[right].Currency != primary
		if leftPrimary != rightPrimary {
			return !leftPrimary
		}
		return entries[left].Currency.String() < entries[right].Currency.String()
	})
	return entries
}

func sumNewCount(accounts []AccountItem) int64 {
	total := int64(0)
	for _, account := range accounts {
		total = addSaturating(total, account.NewCount)
	}
	return total
}

// IsWarningBalance reports whether a total of accounts' balances in currency
// warns red.
//
// The total is below zero and the same total without the loan accounts is also
// below zero. A loan is negative by nature, so red there is noise and a total
// negative only because of loans stays in the plain text colour. Red is kept
// for overdrawn checking accounts and credit cards that are due. One account
// reduces to the one-account rule: red unless it is a loan's.
func IsWarningBalance(accounts []AccountItem, currency money.Currency) bool {
	total := int64(0)
	withoutLoans := int64(0)
	inCurrency := 0
	for _, account := range accounts {
		if account.Currency != currency {
			continue
		}
		inCurrency++
		total = addSaturating(total, account.Balance.Minor)
		if account.AccountType != LoanType {
			withoutLoans = addSaturating(withoutLoans, account.Balance.Minor)
		}
	}
	if inCurrency == 0 || total >= 0 {
		return false
	}
	return withoutLoans < 0
}

// BalanceTone is how an account balance or a total of balances is coloured.
type BalanceTone uint8

const (
	// BalanceTonePositive is above zero.
	BalanceTonePositive BalanceTone = iota
	// BalanceToneNegative is below zero and a warning.
	BalanceToneNegative
	// BalanceToneLoan is below zero only because of loans.
	BalanceToneLoan
	// BalanceToneZero is exactly zero.
	BalanceToneZero
)

// BalanceToneFor returns the tone of accounts' total in currency; the red/grey
// split follows IsWarningBalance. The name mirrors GroupIDFor because Go cannot
// have a function and a type both named BalanceTone.
func BalanceToneFor(accounts []AccountItem, currency money.Currency) BalanceTone {
	total := int64(0)
	for _, account := range accounts {
		if account.Currency != currency {
			continue
		}
		total = addSaturating(total, account.Balance.Minor)
	}
	switch {
	case total > 0:
		return BalanceTonePositive
	case total == 0:
		return BalanceToneZero
	case IsWarningBalance(accounts, currency):
		return BalanceToneNegative
	default:
		return BalanceToneLoan
	}
}

// BalanceToneValue returns the tone of one balance that is not a list of
// accounts (a single account's running balance, a dashboard figure).
func BalanceToneValue(value money.Money, isLoan bool) BalanceTone {
	switch {
	case value.Minor > 0:
		return BalanceTonePositive
	case value.Minor == 0:
		return BalanceToneZero
	case isLoan:
		return BalanceToneLoan
	default:
		return BalanceToneNegative
	}
}

// GroupIDFor returns the custom group key stands for, or nil for a type group.
func GroupIDFor(key GroupKey) *model.AccountGroupID {
	if key.Kind != GroupKeyCustom {
		return nil
	}
	groupID := key.GroupID
	return &groupID
}

// addSaturating adds two minor-unit amounts without silently wrapping. Rust
// panics on overflow in a debug build and has no release behaviour to match, so
// the value is clamped instead of wrapping.
func addSaturating(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}
