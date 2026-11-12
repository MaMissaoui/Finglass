// Package accountpanel ports crates/finglass-core/src/app/account_panel.rs: the
// account panel's stored state and group actions, shared by the Accounts and
// Transactions views.
//
// The state types are single-caller, as Rust's `&mut self` implies. The Wails
// layer serializes calls with one sync.Mutex per profile.
package accountpanel

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// SelectionKey is the settings key holding the panel's selection, as FinDesk
// stores it.
const SelectionKey = "transactions.selection"

// CollapsedKey is the settings key holding the panel's collapsed groups, as
// FinDesk stores it.
const CollapsedKey = "accountGroups.collapsed"

// MaxGroupNameLength is the length of FinDesk's account-group name column.
const MaxGroupNameLength = 100

// SelectionKind says what an AccountSelection stands for.
type SelectionKind uint8

const (
	// SelectionAll means every account.
	SelectionAll SelectionKind = iota
	// SelectionGroup means one built-in or custom group.
	SelectionGroup
	// SelectionAccount means one account.
	SelectionAccount
)

// AccountSelection is what the account panel filters by.
type AccountSelection struct {
	// Kind says which field identifies the selection.
	Kind SelectionKind
	// Group is the selected group when Kind is SelectionGroup.
	Group accountgrouping.GroupKey
	// Account is the selected account when Kind is SelectionAccount.
	Account model.AccountID
}

// AllSelection returns the selection of every account.
func AllSelection() AccountSelection {
	return AccountSelection{Kind: SelectionAll}
}

// GroupSelection returns the selection of one built-in or custom group.
func GroupSelection(key accountgrouping.GroupKey) AccountSelection {
	return AccountSelection{Kind: SelectionGroup, Group: key}
}

// AccountSelectionFor returns the selection of one account.
func AccountSelectionFor(id model.AccountID) AccountSelection {
	return AccountSelection{Kind: SelectionAccount, Account: id}
}

// AppErrorKind says which failure an AppError carries.
type AppErrorKind uint8

const (
	// AppErrorMessage is a user-visible message rendered from the i18n table.
	AppErrorMessage AppErrorKind = iota
	// AppErrorRepo is a repository failure.
	AppErrorRepo
	// AppErrorDB is a settings/database failure.
	AppErrorDB
)

// AppError is why a panel action failed.
//
// User-visible validation failures carry their i18n key; callers render them
// in their own language. Repository and database failures stay errors and are
// not shown verbatim.
type AppError struct {
	// Kind says which failure this is.
	Kind AppErrorKind
	// Key is set when Kind is AppErrorMessage.
	Key i18n.Key
	// Err is the underlying repository or database error otherwise.
	Err error
}

// MessageError returns a user-visible validation failure.
func MessageError(key i18n.Key) *AppError {
	return &AppError{Kind: AppErrorMessage, Key: key}
}

// RepoError wraps a repository failure.
func RepoError(err error) *AppError {
	return &AppError{Kind: AppErrorRepo, Err: err}
}

// DBError wraps a settings/database failure.
func DBError(err error) *AppError {
	return &AppError{Kind: AppErrorDB, Err: err}
}

// Error implements error with Rust's AppError display.
func (e *AppError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Kind == AppErrorMessage {
		return "user-visible message"
	}
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the underlying repository or database error.
func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Message returns the user-visible key when this is a message failure.
func (e *AppError) Message() (i18n.Key, bool) {
	if e == nil || e.Kind != AppErrorMessage {
		return 0, false
	}
	return e.Key, true
}

// LoadSelection reads the stored selection, falling back to All when it is
// absent or names a group or account that no longer exists.
func LoadSelection(
	ctx context.Context,
	d *db.Database,
	groups []accountgrouping.AccountGroupView,
	accounts []accountgrouping.AccountItem,
) (AccountSelection, error) {
	raw, ok, err := settings.Get(ctx, d, SelectionKey)
	if err != nil {
		return AccountSelection{}, DBError(err)
	}
	if !ok {
		return AllSelection(), nil
	}

	if keyID, ok := strings.CutPrefix(raw, "group:"); ok {
		var key *accountgrouping.GroupKey
		if accountType, ok := strings.CutPrefix(keyID, "type:"); ok {
			created := accountgrouping.TypeKey(accountType)
			key = &created
		} else if groupID, ok := strings.CutPrefix(keyID, "custom:"); ok {
			if parsed, err := strconv.ParseInt(groupID, 10, 64); err == nil {
				created := accountgrouping.CustomKey(model.AccountGroupID(parsed))
				key = &created
			}
		}
		if key != nil {
			for _, group := range groups {
				if group.Key.ID() == key.ID() {
					return GroupSelection(*key), nil
				}
			}
		}
		return AllSelection(), nil
	}

	if id, ok := strings.CutPrefix(raw, "account:"); ok {
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err == nil {
			accountID := model.AccountID(parsed)
			for _, account := range accounts {
				if account.ID == accountID {
					return AccountSelectionFor(accountID), nil
				}
			}
		}
		return AllSelection(), nil
	}

	return AllSelection(), nil
}

// SaveSelection stores the selection for both screens.
func SaveSelection(ctx context.Context, d *db.Database, selection AccountSelection) error {
	if err := settings.Set(ctx, d, SelectionKey, SelectionValue(selection)); err != nil {
		return DBError(err)
	}
	return nil
}

// StoreSelection stores the selection for both screens, under the name the
// task uses.
func StoreSelection(ctx context.Context, d *db.Database, selection AccountSelection) error {
	return SaveSelection(ctx, d, selection)
}

// Resolve falls back to All when the selected account or group no longer
// exists.
func Resolve(
	selection AccountSelection,
	groups []accountgrouping.AccountGroupView,
	accounts []accountgrouping.AccountItem,
) AccountSelection {
	switch selection.Kind {
	case SelectionAll:
		return AllSelection()
	case SelectionAccount:
		for _, account := range accounts {
			if account.ID == selection.Account {
				return selection
			}
		}
		return AllSelection()
	case SelectionGroup:
		for _, group := range groups {
			if group.Key.ID() == selection.Group.ID() {
				return selection
			}
		}
		return AllSelection()
	default:
		return AllSelection()
	}
}

// SelectionValue returns the stored text of a selection.
func SelectionValue(selection AccountSelection) string {
	switch selection.Kind {
	case SelectionGroup:
		return "group:" + selection.Group.ID()
	case SelectionAccount:
		return "account:" + strconv.FormatInt(int64(selection.Account), 10)
	default:
		return "all"
	}
}

// LoadCollapsed returns the collapsed panel groups stored for this profile.
func LoadCollapsed(ctx context.Context, d *db.Database) (map[string]struct{}, error) {
	raw, ok, err := settings.Get(ctx, d, CollapsedKey)
	if err != nil {
		return nil, DBError(err)
	}
	collapsed := make(map[string]struct{})
	if !ok {
		return collapsed, nil
	}
	for _, value := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			collapsed[trimmed] = struct{}{}
		}
	}
	return collapsed, nil
}

// SaveCollapsed stores the collapsed panel groups, sorted and comma-separated
// like FinDesk.
func SaveCollapsed(ctx context.Context, d *db.Database, collapsed map[string]struct{}) error {
	values := make([]string, 0, len(collapsed))
	for value := range collapsed {
		values = append(values, value)
	}
	sort.Strings(values)
	if err := settings.Set(ctx, d, CollapsedKey, strings.Join(values, ",")); err != nil {
		return DBError(err)
	}
	return nil
}

// VisibleAccounts returns the account ids selection shows, or nil for every
// account.
//
// An empty non-nil set is an empty custom group, or a selection whose group no
// longer has accounts: it shows nothing rather than everything.
func VisibleAccounts(
	selection AccountSelection,
	groups []accountgrouping.AccountGroupView,
) *model.AccountIDSet {
	switch selection.Kind {
	case SelectionGroup:
		visible := make(model.AccountIDSet)
		for _, group := range groups {
			if group.Key.ID() != selection.Group.ID() {
				continue
			}
			for _, account := range group.Accounts {
				visible[account.ID] = struct{}{}
			}
			break
		}
		return &visible
	case SelectionAccount:
		visible := model.AccountIDSet{selection.Account: struct{}{}}
		return &visible
	default:
		return nil
	}
}

// MatchesFilter reports whether an account row matches the panel's free-text
// filter.
//
// FinDesk searches the display name, the identifier and the bank name,
// case-insensitively; an empty query matches.
func MatchesFilter(name *string, identifier, bankName, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	query = strings.ToLower(query)
	displayName := ""
	if name != nil {
		displayName = *name
	}
	return strings.Contains(strings.ToLower(displayName), query) ||
		strings.Contains(strings.ToLower(identifier), query) ||
		strings.Contains(strings.ToLower(bankName), query)
}

// AccountGroupActions is the account panel's group actions.
//
// Each call is synchronous and takes the database; the caller refreshes its
// state afterwards. Every mutation goes through the repository layer, which
// writes exactly one audit entry in the same transaction.
type AccountGroupActions struct{}

// NewAccountGroupActions builds the action set.
func NewAccountGroupActions() AccountGroupActions {
	return AccountGroupActions{}
}

// Create creates a group after the existing ones. A blank name is a
// user-visible error.
func (AccountGroupActions) Create(
	ctx context.Context,
	d *db.Database,
	name string,
) (model.AccountGroupID, error) {
	clean, err := NormalizeGroupName(name)
	if err != nil {
		return 0, err
	}

	existing, err := accountgroups.List(ctx, d)
	if err != nil {
		return 0, RepoError(err)
	}
	sortOrder := int64(0)
	found := false
	for _, group := range existing {
		if !found || group.SortOrder > sortOrder {
			sortOrder = group.SortOrder
			found = true
		}
	}
	sortOrder++

	created, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: clean, SortOrder: sortOrder})
	if err != nil {
		return 0, RepoError(err)
	}
	return created, nil
}

// Rename renames a group. A blank name is a user-visible error; an unknown id
// changes nothing.
func (AccountGroupActions) Rename(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	name string,
) error {
	clean, err := NormalizeGroupName(name)
	if err != nil {
		return err
	}

	existing, err := accountgroups.List(ctx, d)
	if err != nil {
		return RepoError(err)
	}
	found := false
	for _, group := range existing {
		if group.ID == id {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	if err := accountgroups.Rename(ctx, d, id, clean); err != nil {
		return RepoError(err)
	}
	return nil
}

// Delete deletes a group; its accounts return to their type groups.
//
// It returns true when current was that group. The stored selection is then
// reset to All, and the caller shows All too, exactly like FinDesk.
func (AccountGroupActions) Delete(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	current AccountSelection,
) (bool, error) {
	if err := accountgroups.Delete(ctx, d, id); err != nil {
		return false, RepoError(err)
	}

	wasSelected := current.Kind == SelectionGroup &&
		current.Group.Kind == accountgrouping.GroupKeyCustom &&
		current.Group.GroupID == id
	if wasSelected {
		if err := SaveSelection(ctx, d, AllSelection()); err != nil {
			return true, err
		}
	}
	return wasSelected, nil
}

// MoveUpOrDown moves a group up or down. At either end this is a no-op, but it
// still writes FinDesk's audit entry.
func (AccountGroupActions) MoveUpOrDown(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	up bool,
) (bool, error) {
	moved, err := accountgroups.MoveGroup(ctx, d, id, up)
	if err != nil {
		return false, RepoError(err)
	}
	return moved, nil
}

// MoveAccount moves an account into a custom group, or back to its type group
// with nil.
func (AccountGroupActions) MoveAccount(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	groupID *model.AccountGroupID,
) error {
	if err := accounts.SetGroup(ctx, d, accountID, groupID); err != nil {
		return RepoError(err)
	}
	return nil
}

// MarkRead marks the bookings behind selection as read: All clears everything,
// a group its accounts, an account its own.
func (AccountGroupActions) MarkRead(
	ctx context.Context,
	d *db.Database,
	selection AccountSelection,
	groups []accountgrouping.AccountGroupView,
) error {
	if err := transactions.MarkRead(ctx, d, VisibleAccounts(selection, groups)); err != nil {
		return RepoError(err)
	}
	return nil
}

// NormalizeGroupName trims and caps a group name, rejecting a blank one.
func NormalizeGroupName(name string) (string, error) {
	clean := []rune(strings.TrimSpace(name))
	if len(clean) > MaxGroupNameLength {
		clean = clean[:MaxGroupNameLength]
	}
	if len(clean) == 0 {
		return "", MessageError(i18n.ErrorNameRequired)
	}
	return string(clean), nil
}
