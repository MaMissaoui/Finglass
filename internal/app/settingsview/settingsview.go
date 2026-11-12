// Package settingsview ports crates/finglass-core/src/app/settings/: the
// Settings page's state, without any UI.
//
// Every write goes through the existing repositories and services, which audit;
// this package adds audit entries only for the settings and profile changes
// that have no repository of their own. It ports FinDesk's SettingsViewModel
// minus the protocol trace and the dark-theme flag, which app/prefs owns.
//
// The state is single-caller, as Rust's `&mut self` implies. The Wails layer
// serializes calls with one sync.Mutex per profile.
package settingsview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/auditview"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulerepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/report"
	"github.com/MaMissaoui/Finglass/internal/core/ruleimport"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
	"golang.org/x/text/cases"
	xlanguage "golang.org/x/text/language"
)

// BackupVersion is written into the manifest of backups this view creates.
//
// Rust's backup service reads crate::VERSION itself. Go's core cannot import
// internal/bind and this view cannot import it either, because the bind layer
// calls the view. The bind layer sets this to bind.Version when it starts.
var BackupVersion string

// TaxKeywordsKey is the settings key holding the user's tax suggestion
// keywords, re-exported from the report package.
const TaxKeywordsKey = report.KeyKeywords

// DefaultTaxKeywords are the built-in tax suggestion keywords, re-exported
// from the report package.
var DefaultTaxKeywords = report.DefaultKeywords

// CommonCurrencies are the currencies Settings offers for the primary
// currency, before the accounts' own currencies are added.
//
// money.Currencies in this crate had no common list when this module was
// ported; the list is kept here so the settings state can offer the same
// choices without changing another module.
var CommonCurrencies = [11]string{
	"EUR", "USD", "GBP", "CHF", "PLN", "CZK", "SEK", "DKK", "NOK", "HUF", "JPY",
}

// SettingsErrorKind says which failure a SettingsError carries.
type SettingsErrorKind uint8

const (
	// SettingsErrorMessage is a user-visible message rendered from the i18n
	// table.
	SettingsErrorMessage SettingsErrorKind = iota
	// SettingsErrorUnknownProfile means no profile is registered under that
	// slug.
	SettingsErrorUnknownProfile
	// SettingsErrorRepo is a repository failure.
	SettingsErrorRepo
	// SettingsErrorDB is a settings or database failure.
	SettingsErrorDB
	// SettingsErrorProfile is a profile folder failure.
	SettingsErrorProfile
	// SettingsErrorBackup is a backup or restore failure.
	SettingsErrorBackup
	// SettingsErrorRuleImport is a rule import or export failure.
	SettingsErrorRuleImport
)

// SettingsError is everything that can go wrong in a Settings action.
type SettingsError struct {
	// Kind says which failure this is.
	Kind SettingsErrorKind
	// Key is set when Kind is SettingsErrorMessage.
	Key i18n.Key
	// Err is the underlying failure otherwise.
	Err error
}

// Error implements error with Rust's SettingsError display.
func (e *SettingsError) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch e.Kind {
	case SettingsErrorMessage:
		return "user-visible message"
	case SettingsErrorUnknownProfile:
		return "unknown profile"
	default:
		if e.Err == nil {
			return ""
		}
		return e.Err.Error()
	}
}

// Unwrap returns the underlying repository, database, profile, backup or rule
// import failure.
func (e *SettingsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// MessageKey returns the user-visible key when this is a message failure.
func (e *SettingsError) MessageKey() (i18n.Key, bool) {
	if e == nil || e.Kind != SettingsErrorMessage {
		return 0, false
	}
	return e.Key, true
}

func messageError(key i18n.Key) *SettingsError {
	return &SettingsError{Kind: SettingsErrorMessage, Key: key}
}

func unknownProfileError() *SettingsError {
	return &SettingsError{Kind: SettingsErrorUnknownProfile}
}

func repoError(err error) *SettingsError {
	return &SettingsError{Kind: SettingsErrorRepo, Err: err}
}

func dbError(err error) *SettingsError {
	return &SettingsError{Kind: SettingsErrorDB, Err: err}
}

func profileErrorValue(err error) *SettingsError {
	return &SettingsError{Kind: SettingsErrorProfile, Err: err}
}

func backupError(err error) *SettingsError {
	return &SettingsError{Kind: SettingsErrorBackup, Err: err}
}

func ruleImportError(err error) *SettingsError {
	return &SettingsError{Kind: SettingsErrorRuleImport, Err: err}
}

// RestoreCandidate is a backup chosen for restore, waiting for confirmation.
type RestoreCandidate struct {
	// Backup is the backup file.
	Backup string
	// Info is what its manifest says.
	Info backup.Info
}

// SettingsState is the Settings page state.
//
// It mirrors FinDesk's SettingsState minus darkTheme, protocolTraceEnabled and
// hasProtocolTrace. Fields that only the shell can know (the data directory,
// the documents folder, OCR availability, encryption state) are left to the
// shell; this state only holds what its actions load and change.
//
// The state is single-caller, as Rust's `&mut self` implies. The Wails layer
// serializes calls with one sync.Mutex per profile.
type SettingsState struct {
	// Categories holds every category, in repository order (by name).
	Categories []model.Category
	// CategoryUsage is bookings per category id; a category with no bookings
	// is absent.
	CategoryUsage map[model.CategoryID]int
	// Rules holds every rule, highest priority first.
	Rules []model.Rule
	// AuditLog holds the newest audit entries, ready for the settings screen.
	AuditLog []auditview.AuditRow
	// PrimaryCurrency is the primary currency.
	PrimaryCurrency money.Currency
	// CurrencyOptions are the primary-currency choices: the common ones plus
	// any an account uses.
	CurrencyOptions []money.Currency
	// TaxKeywords are the tax suggestion keywords as one editable multi-line
	// text.
	TaxKeywords string
	// Profiles holds every profile, the default one first.
	Profiles []profiles.Profile
	// OpenProfile is the profile running now (nil is the default profile).
	OpenProfile *string
	// NextProfile is the profile that opens at the next start.
	NextProfile *string
	// RuleImport is a rule file preview waiting for confirmation.
	RuleImport *ruleimport.RuleImportPreview
	// IsImportingRules says whether a confirmed rule import is applying its
	// preview.
	IsImportingRules bool
	// HasPendingRestore says whether a restore is staged and waits for the
	// next start.
	HasPendingRestore bool
	// RestoreCandidate is a backup chosen for restore, waiting for
	// confirmation.
	RestoreCandidate *RestoreCandidate
	// RestorePassphraseFor is a backup made under another passphrase, waiting
	// for that passphrase.
	RestorePassphraseFor *string
	// RestorePassphraseWrong says whether the last passphrase for
	// RestorePassphraseFor was wrong.
	RestorePassphraseWrong bool
	// IsBusy says whether a backup or restore is running.
	IsBusy bool
	// IsLoading says whether a load or refresh is running.
	IsLoading bool
	// Error is the last error, already localized.
	Error *string
	// Message is the last success message, already localized.
	Message *string
	// Language is the language for user-visible texts.
	Language i18n.Language
	// Busy is the profile's busy flag, shared with the other views; the app
	// replaces it with its own (see accountsview.Busy).
	Busy accountsview.Busy
}

// New builds an empty state for language.
func New(language i18n.Language) *SettingsState {
	return &SettingsState{
		Categories:             make([]model.Category, 0),
		CategoryUsage:          make(map[model.CategoryID]int),
		Rules:                  make([]model.Rule, 0),
		AuditLog:               make([]auditview.AuditRow, 0),
		PrimaryCurrency:        money.EUR,
		CurrencyOptions:        commonCurrencies(),
		TaxKeywords:            "",
		Profiles:               make([]profiles.Profile, 0),
		OpenProfile:            nil,
		NextProfile:            nil,
		RuleImport:             nil,
		IsImportingRules:       false,
		HasPendingRestore:      false,
		RestoreCandidate:       nil,
		RestorePassphraseFor:   nil,
		RestorePassphraseWrong: false,
		IsBusy:                 false,
		IsLoading:              false,
		Error:                  nil,
		Message:                nil,
		Language:               language,
		Busy:                   accountsview.NewBusy(),
	}
}

// Default builds an empty state in English, like Rust's SettingsState::default.
func Default() *SettingsState {
	return New(i18n.En)
}

// Load loads every part of the Settings page.
func (s *SettingsState) Load(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	openProfile *string,
) error {
	s.IsLoading = true
	s.OpenProfile = copyString(openProfile)
	profile := copyString(s.OpenProfile)
	if err := s.Refresh(ctx, d, appPaths, profile); err != nil {
		s.IsLoading = false
		return s.actionError(err)
	}
	return nil
}

// Refresh reloads the state from the database and the profile registry.
func (s *SettingsState) Refresh(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	profile *string,
) error {
	categories, err := listCategories(ctx, d)
	if err != nil {
		return err
	}
	categoryUsage, err := categoryUsage(ctx, d)
	if err != nil {
		return err
	}
	rules, err := listRules(ctx, d)
	if err != nil {
		return err
	}
	auditLog, err := auditview.Recent(ctx, d, 100)
	if err != nil {
		return repoError(err)
	}
	primaryCurrency, err := money.Primary(ctx, d)
	if err != nil {
		return dbError(err)
	}
	currencyOptions, err := currencyOptions(ctx, d)
	if err != nil {
		return err
	}
	taxKeywords, err := taxKeywordsText(ctx, d)
	if err != nil {
		return err
	}
	profileList, err := profiles.List(appPaths)
	if err != nil {
		return profileErrorValue(err)
	}
	active, err := profiles.Active(appPaths)
	if err != nil {
		return profileErrorValue(err)
	}
	service, err := backupService(appPaths, profile)
	if err != nil {
		return err
	}

	s.Categories = categories
	s.CategoryUsage = categoryUsage
	s.Rules = rules
	s.AuditLog = auditLog
	s.PrimaryCurrency = primaryCurrency
	s.CurrencyOptions = currencyOptions
	s.TaxKeywords = taxKeywords
	s.Profiles = profileList
	s.NextProfile = profileSlug(active)
	s.HasPendingRestore = service.HasPendingRestore()
	s.IsLoading = false
	return nil
}

// SetPrimaryCurrency stores the primary currency and records the change.
func (s *SettingsState) SetPrimaryCurrency(ctx context.Context, d *db.Database, code string) error {
	currency, err := money.NewCurrency(code)
	if err != nil {
		s.fail(i18n.ErrorUnexpected)
		return nil
	}

	if err := settings.Set(ctx, d, money.PrimaryCurrencyKey, currency.String()); err != nil {
		return dbError(err)
	}
	if err := audit.Log(
		ctx,
		d,
		"SET_PRIMARY_CURRENCY",
		audit.ActorUser,
		currency.String(),
	); err != nil {
		return dbError(err)
	}
	s.PrimaryCurrency = currency
	return nil
}

// ValidateCategory validates a category name and colour; excludeID is the
// category being edited, if any.
func (s *SettingsState) ValidateCategory(name, color string, excludeID *model.CategoryID) *string {
	if strings.TrimSpace(name) == "" {
		message := i18n.Text(s.Language, i18n.ErrorNameRequired)
		return &message
	}
	if !isHexColor(color) {
		message := i18n.Text(s.Language, i18n.ErrorColorFormat)
		return &message
	}

	candidate := lower(strings.TrimSpace(name))
	duplicate := false
	for _, category := range s.Categories {
		if excludeID != nil && category.ID == *excludeID {
			continue
		}
		if lower(category.Name) == candidate {
			duplicate = true
			break
		}
	}
	if duplicate {
		message := i18n.Text(s.Language, i18n.ErrorCategoryExists)
		return &message
	}
	return nil
}

// SaveCategory creates or updates a category and reloads the state.
func (s *SettingsState) SaveCategory(
	ctx context.Context,
	d *db.Database,
	id *model.CategoryID,
	name string,
	color string,
	budgetLimit *money.Money,
	isTaxRelevant bool,
	isTransfer bool,
) error {
	if message := s.ValidateCategory(name, color, id); message != nil {
		s.Error = message
		return nil
	}

	newCategory := model.NewCategory{
		Name:          strings.TrimSpace(name),
		Color:         color,
		BudgetLimit:   budgetLimit,
		IsTaxRelevant: isTaxRelevant,
		IsTransfer:    isTransfer,
	}
	var err error
	if id == nil {
		_, err = categories.Create(ctx, d, newCategory)
	} else {
		err = categories.Update(ctx, d, *id, newCategory)
	}
	if err != nil {
		return repoError(err)
	}
	return s.reload(ctx, d)
}

// DeleteCategory deletes a category; its bookings stay uncategorized and its
// rules are removed.
func (s *SettingsState) DeleteCategory(
	ctx context.Context,
	d *db.Database,
	id model.CategoryID,
) error {
	if err := categories.Delete(ctx, d, id); err != nil {
		return repoError(err)
	}
	return s.reload(ctx, d)
}

// SaveTaxKeywords stores the tax suggestion keywords: one per line, trimmed,
// non-blank and de-duplicated case-insensitively.
//
// The audit entry carries the saved count only, never keyword text.
func (s *SettingsState) SaveTaxKeywords(ctx context.Context, d *db.Database, text string) error {
	seen := make(map[string]struct{})
	saved := make([]string, 0)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key := lower(trimmed)
		if _, present := seen[key]; present {
			continue
		}
		seen[key] = struct{}{}
		saved = append(saved, trimmed)
	}

	if err := settings.Set(ctx, d, TaxKeywordsKey, strings.Join(saved, "\n")); err != nil {
		return dbError(err)
	}
	if err := audit.Log(
		ctx,
		d,
		"SAVE_TAX_KEYWORDS",
		audit.ActorUser,
		fmt.Sprintf("Saved %d tax suggestion keywords", len(saved)),
	); err != nil {
		return dbError(err)
	}
	return s.reload(ctx, d)
}

// ResetTaxKeywords removes the custom keyword list so the built-in defaults
// apply again; the audit entry carries the removed count only.
func (s *SettingsState) ResetTaxKeywords(ctx context.Context, d *db.Database) error {
	removed := 0
	stored, ok, err := settings.Get(ctx, d, TaxKeywordsKey)
	if err != nil {
		return dbError(err)
	}
	if ok {
		for _, line := range strings.Split(stored, "\n") {
			if strings.TrimSpace(line) != "" {
				removed++
			}
		}
	}

	if err := deleteSetting(ctx, d, TaxKeywordsKey); err != nil {
		return dbError(err)
	}
	if err := audit.Log(
		ctx,
		d,
		"RESET_TAX_KEYWORDS",
		audit.ActorUser,
		fmt.Sprintf("Reset %d tax suggestion keywords", removed),
	); err != nil {
		return dbError(err)
	}
	return s.reload(ctx, d)
}

// FixDuplicateColours recolours categories that share a colour: in each group
// the lowest id keeps its colour and the others get the first palette colour
// that no category uses at that moment.
func (s *SettingsState) FixDuplicateColours(ctx context.Context, d *db.Database) error {
	loaded, err := listCategories(ctx, d)
	if err != nil {
		return err
	}

	used := make(map[string]struct{}, len(loaded))
	for _, category := range loaded {
		used[upper(category.Color)] = struct{}{}
	}

	type colourGroup struct {
		key     string
		entries []model.Category
	}
	groups := make([]colourGroup, 0)
	for _, category := range loaded {
		key := upper(category.Color)
		found := -1
		for index := range groups {
			if groups[index].key == key {
				found = index
				break
			}
		}
		if found >= 0 {
			groups[found].entries = append(groups[found].entries, category)
		} else {
			groups = append(groups, colourGroup{key: key, entries: []model.Category{category}})
		}
	}

	recoloured := 0
	for _, group := range groups {
		if len(group.entries) < 2 {
			continue
		}
		sort.SliceStable(group.entries, func(left, right int) bool {
			return group.entries[left].ID < group.entries[right].ID
		})
		for _, category := range group.entries[1:] {
			free := ""
			hasFree := false
			for _, color := range categories.Colors {
				if _, taken := used[upper(color)]; taken {
					continue
				}
				free = color
				hasFree = true
				break
			}
			if !hasFree {
				break
			}

			if err := categories.Update(ctx, d, category.ID, model.NewCategory{
				Name:          category.Name,
				Color:         free,
				BudgetLimit:   category.BudgetLimit,
				IsTaxRelevant: category.IsTaxRelevant,
				IsTransfer:    category.IsTransfer,
			}); err != nil {
				return repoError(err)
			}
			used[upper(free)] = struct{}{}
			recoloured++
		}
	}

	if err := s.reload(ctx, d); err != nil {
		return err
	}
	if recoloured > 0 {
		if err := audit.Log(
			ctx,
			d,
			"FIX_CATEGORY_COLOURS",
			audit.ActorUser,
			fmt.Sprintf("Recoloured %d categories", recoloured),
		); err != nil {
			return dbError(err)
		}
		message := i18n.Format(
			s.Language,
			i18n.MessageColoursFixed,
			i18n.Int(int64(recoloured)),
		)
		s.Message = &message
	} else {
		message := i18n.Text(s.Language, i18n.MessageColoursAlreadyDistinct)
		s.Message = &message
	}
	return nil
}

// AddDefaultCategories adds the default categories that do not exist yet, in
// the current UI language.
func (s *SettingsState) AddDefaultCategories(ctx context.Context, d *db.Database) error {
	added, err := categories.SeedDefaults(ctx, d, s.Language)
	if err != nil {
		return repoError(err)
	}
	if err := s.reload(ctx, d); err != nil {
		return err
	}
	if added == 0 {
		message := i18n.Text(s.Language, i18n.MessageDefaultCategoriesNone)
		s.Message = &message
	} else {
		message := i18n.Format(
			s.Language,
			i18n.MessageDefaultCategoriesAdded,
			i18n.Int(int64(added)),
		)
		s.Message = &message
	}
	return nil
}

// ValidateRule validates a rule pattern, as a localized message or nil.
func (s *SettingsState) ValidateRule(pattern string) *string {
	return rules.ValidateRulePattern(pattern, s.Language)
}

// SaveRule creates or updates a rule, including its tax flag (nil means "as
// the category").
func (s *SettingsState) SaveRule(
	ctx context.Context,
	d *db.Database,
	id *model.RuleID,
	pattern string,
	categoryID model.CategoryID,
	priority int64,
	autoApply bool,
	matchField model.RuleField,
	direction model.RuleDirection,
	taxRelevant *bool,
) error {
	if message := s.ValidateRule(pattern); message != nil {
		s.Error = message
		return nil
	}

	newRule := model.NewRule{
		Pattern:     strings.TrimSpace(pattern),
		CategoryID:  categoryID,
		Priority:    priority,
		AutoApply:   autoApply,
		MatchField:  matchField,
		Direction:   direction,
		TaxRelevant: taxRelevant,
	}
	var err error
	if id == nil {
		_, err = rulerepo.Create(ctx, d, newRule)
	} else {
		err = rulerepo.Update(ctx, d, *id, newRule)
	}
	if err != nil {
		return repoError(err)
	}
	return s.reload(ctx, d)
}

// DeleteRule deletes a rule and reloads the state.
func (s *SettingsState) DeleteRule(ctx context.Context, d *db.Database, id model.RuleID) error {
	if err := rulerepo.Delete(ctx, d, id); err != nil {
		return repoError(err)
	}
	return s.reload(ctx, d)
}

// ExportRules writes the current rules to a shareable rule file and reports
// how many were written and skipped.
func (s *SettingsState) ExportRules(ctx context.Context, d *db.Database, path string) error {
	total, err := listRules(ctx, d)
	if err != nil {
		return err
	}
	written, err := ruleimport.Export(ctx, d, path)
	if err != nil {
		return ruleImportError(err)
	}
	skipped := saturatingSub(len(total), written)

	if skipped == 0 {
		message := i18n.Format(
			s.Language,
			i18n.MessageRulesExported,
			i18n.Int(int64(written)),
		)
		s.Message = &message
	} else {
		message := i18n.Format(
			s.Language,
			i18n.MessageRulesExportSkipped,
			i18n.Int(int64(written)),
			i18n.Int(int64(skipped)),
		)
		s.Message = &message
	}
	s.Error = nil
	return nil
}

// ChooseRuleImport parses and classifies a rule file; nothing is written yet.
func (s *SettingsState) ChooseRuleImport(
	ctx context.Context,
	d *db.Database,
	path string,
) error {
	metadata, err := os.Stat(path)
	if err != nil {
		s.fail(i18n.ErrorUnexpected)
		return nil
	}
	if metadata.Size() > int64(rules.RuleFileMaxBytes) {
		s.fail(i18n.ErrorRuleFileTooLarge)
		return nil
	}

	// #nosec G304 -- the user chooses the rule file to import.
	data, err := os.ReadFile(path)
	if err != nil {
		s.fail(i18n.ErrorUnexpected)
		return nil
	}
	parsed, err := rules.ParseRuleFile(data, s.Language)
	if err != nil {
		message := err.Error()
		s.Error = &message
		return nil
	}
	preview, err := ruleimport.Preview(ctx, d, parsed)
	if err != nil {
		return s.actionError(ruleImportError(err))
	}

	s.RuleImport = preview
	s.Error = nil
	s.Message = nil
	return nil
}

// ConfirmRuleImport applies the confirmed preview to the database and clears
// the preview.
func (s *SettingsState) ConfirmRuleImport(
	ctx context.Context,
	d *db.Database,
	categorize bool,
) error {
	preview := s.RuleImport
	if preview == nil {
		return nil
	}
	s.IsImportingRules = true

	result, err := ruleimport.Apply(ctx, d, preview, categorize, s.Language)
	if err != nil {
		s.IsImportingRules = false
		return s.actionError(ruleImportError(err))
	}

	if err := s.reload(ctx, d); err != nil {
		s.IsImportingRules = false
		return s.actionError(err)
	}
	s.RuleImport = nil
	s.IsImportingRules = false
	message := i18n.Format(
		s.Language,
		i18n.MessageRulesImported,
		i18n.Int(int64(result.Rules)),
		i18n.Int(int64(result.Categories)),
		i18n.Int(int64(result.Categorized)),
	)
	s.Message = &message
	s.Error = nil
	return nil
}

// CancelRuleImport drops the preview without applying anything.
func (s *SettingsState) CancelRuleImport() {
	s.RuleImport = nil
}

// CreateBackup writes a backup of the profile's data and reports its counts.
func (s *SettingsState) CreateBackup(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	profile *string,
	target string,
) error {
	s.IsBusy = true
	s.Error = nil

	service, err := backupService(appPaths, profile)
	if err != nil {
		s.IsBusy = false
		return s.actionError(err)
	}
	var info backup.Info
	var createErr error
	s.Busy.Run(func() {
		info, createErr = service.CreateBackup(d, target)
	})
	if createErr != nil {
		s.IsBusy = false
		return s.actionError(backupError(createErr))
	}

	name := fileName(target)
	if err := audit.Log(
		ctx,
		d,
		"BACKUP_CREATED",
		audit.ActorUser,
		fmt.Sprintf(
			"Backup %s: %d transactions, %d documents",
			name,
			info.Transactions,
			info.Documents,
		),
	); err != nil {
		s.IsBusy = false
		return s.actionError(dbError(err))
	}
	if err := s.Refresh(ctx, d, appPaths, profile); err != nil {
		s.IsBusy = false
		return s.actionError(err)
	}
	s.IsBusy = false
	message := i18n.Format(
		s.Language,
		i18n.MessageBackupSaved,
		i18n.Str(name),
		i18n.Int(info.Transactions),
		i18n.Int(int64(info.Documents)),
	)
	s.Message = &message
	return nil
}

// ChooseRestore reads a backup's manifest and asks for confirmation; nothing
// changes yet.
func (s *SettingsState) ChooseRestore(
	appPaths paths.AppPaths,
	profile *string,
	backupPath string,
) error {
	service, err := backupService(appPaths, profile)
	if err != nil {
		return s.actionError(err)
	}
	info, err := service.Inspect(backupPath)
	if err != nil {
		return s.actionError(backupError(err))
	}
	s.RestoreCandidate = &RestoreCandidate{Backup: backupPath, Info: info}
	return nil
}

// ConfirmRestore confirms the chosen backup with the live database's
// passphrase; it replaces the data on the next start.
func (s *SettingsState) ConfirmRestore(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	profile *string,
) error {
	if s.RestoreCandidate == nil {
		return nil
	}
	backupPath := s.RestoreCandidate.Backup
	return s.stageRestore(ctx, d, appPaths, profile, backupPath, nil)
}

// ConfirmRestoreWithPassphrase confirms a backup made under another
// passphrase. The passphrase is checked and never stored.
//
// The caller owns and wipes passphrase. This method copies it into a
// secret.Passphrase, which is wiped as soon as the staged restore returns, on
// every path.
func (s *SettingsState) ConfirmRestoreWithPassphrase(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	profile *string,
	passphrase []byte,
) error {
	if s.RestorePassphraseFor == nil {
		return nil
	}
	backupPath := *s.RestorePassphraseFor
	unlock, err := secret.NewPassphrase(passphrase, secret.Unlock)
	if err != nil {
		s.RestorePassphraseWrong = true
		return nil
	}
	defer unlock.Wipe()
	return s.stageRestore(ctx, d, appPaths, profile, backupPath, unlock)
}

// CancelRestoreChoice drops the backup chosen for restore.
func (s *SettingsState) CancelRestoreChoice() {
	s.RestoreCandidate = nil
}

// CancelRestorePassphrase drops the passphrase request for a backup made under
// another passphrase.
func (s *SettingsState) CancelRestorePassphrase() {
	s.RestorePassphraseFor = nil
	s.RestorePassphraseWrong = false
}

// CancelPendingRestore removes a staged restore that waits for the next start.
func (s *SettingsState) CancelPendingRestore(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	profile *string,
) error {
	service, err := backupService(appPaths, profile)
	if err != nil {
		return s.actionError(err)
	}
	service.CancelPendingRestore()
	if err := s.Refresh(ctx, d, appPaths, profile); err != nil {
		return s.actionError(err)
	}
	message := i18n.Text(s.Language, i18n.MessageRestoreCancelled)
	s.Message = &message
	return nil
}

// CreateProfile creates a profile and records it.
func (s *SettingsState) CreateProfile(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	name string,
) error {
	created, err := profiles.Create(appPaths, name)
	if err != nil {
		return s.actionError(profileError(err))
	}
	slug := "default"
	if created.Slug != "" {
		slug = created.Slug
	}
	if err := audit.Log(
		ctx,
		d,
		"PROFILE_CREATED",
		audit.ActorUser,
		fmt.Sprintf("Profile %s (%s)", created.Name, slug),
	); err != nil {
		return dbError(err)
	}
	return s.Refresh(ctx, d, appPaths, s.OpenProfile)
}

// RenameProfile renames the default profile (nil) or a stored one and records
// it.
func (s *SettingsState) RenameProfile(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	slug *string,
	name string,
) error {
	if err := profiles.Rename(appPaths, profileSlugArgument(slug), name); err != nil {
		return s.actionError(profileError(err))
	}
	if err := audit.Log(
		ctx,
		d,
		"PROFILE_RENAMED",
		audit.ActorUser,
		fmt.Sprintf("Profile %s renamed to %s", slugText(slug), strings.TrimSpace(name)),
	); err != nil {
		return dbError(err)
	}
	return s.Refresh(ctx, d, appPaths, s.OpenProfile)
}

// SwitchProfile validates a profile and returns the slug to switch to; the
// live switch (closing the database and showing the unlock window) is the
// app's job.
func (s *SettingsState) SwitchProfile(appPaths paths.AppPaths, slug *string) (*string, error) {
	if slug != nil {
		listed, err := profiles.List(appPaths)
		if err != nil {
			return nil, profileErrorValue(err)
		}
		known := false
		for _, profile := range listed {
			if profile.Slug != "" && profile.Slug == *slug {
				known = true
				break
			}
		}
		if !known {
			return nil, unknownProfileError()
		}
	}
	if slug == nil {
		return nil, nil
	}
	value := *slug
	return &value, nil
}

// DeleteProfile moves a profile's folder aside and forgets its remembered PINs
// first.
//
// forgetPins runs before the folder moves, because the credentials live in the
// operating system's store keyed by the old folder. The app passes the real
// credential store later; tests pass a recording fake.
func (s *SettingsState) DeleteProfile(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	openProfile *string,
	slug string,
	forgetPins func(slug string),
) error {
	// Validate before forgetting anything: a refused delete must not touch
	// the credential store.
	if err := validateDelete(appPaths, openProfile, slug); err != nil {
		return s.actionError(profileError(err))
	}
	forgetPins(slug)
	moved, err := profiles.Delete(appPaths, slug, profileSlugArgument(openProfile))
	if err != nil {
		return s.actionError(profileError(err))
	}
	if err := audit.Log(
		ctx,
		d,
		"PROFILE_DELETED",
		audit.ActorUser,
		fmt.Sprintf("Profile %s moved to %s", slug, moved),
	); err != nil {
		return dbError(err)
	}
	if err := s.Refresh(ctx, d, appPaths, s.OpenProfile); err != nil {
		return s.actionError(err)
	}
	message := i18n.Format(s.Language, i18n.MessageProfileMoved, i18n.Str(moved))
	s.Message = &message
	return nil
}

// ClearMessages clears the error and success message.
func (s *SettingsState) ClearMessages() {
	s.Error = nil
	s.Message = nil
}

func (s *SettingsState) stageRestore(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	profile *string,
	backupPath string,
	passphrase *secret.Passphrase,
) error {
	s.IsBusy = true
	s.RestoreCandidate = nil
	s.RestorePassphraseFor = nil

	service, err := backupService(appPaths, profile)
	if err != nil {
		s.IsBusy = false
		return s.actionError(err)
	}
	var stageErr error
	s.Busy.Run(func() {
		_, stageErr = service.StageRestore(d, backupPath, passphrase)
	})
	if stageErr != nil {
		var required *backup.PassphraseRequiredError
		if errors.As(stageErr, &required) {
			s.IsBusy = false
			forBackup := backupPath
			s.RestorePassphraseFor = &forBackup
			s.RestorePassphraseWrong = required.WrongPassphraseGiven
			return nil
		}
		s.IsBusy = false
		return s.actionError(backupError(stageErr))
	}

	s.RestorePassphraseWrong = false
	if err := audit.Log(
		ctx,
		d,
		"RESTORE_STAGED",
		audit.ActorUser,
		fmt.Sprintf("Restore from %s staged", fileName(backupPath)),
	); err != nil {
		s.IsBusy = false
		return s.actionError(dbError(err))
	}
	if err := s.Refresh(ctx, d, appPaths, profile); err != nil {
		s.IsBusy = false
		return s.actionError(err)
	}
	s.IsBusy = false
	message := i18n.Text(s.Language, i18n.MessageRestorePrepared)
	s.Message = &message
	return nil
}

// reload reloads the database-backed fields after an action that does not
// change the profile registry.
func (s *SettingsState) reload(ctx context.Context, d *db.Database) error {
	loadedCategories, err := listCategories(ctx, d)
	if err != nil {
		return err
	}
	usage, err := categoryUsage(ctx, d)
	if err != nil {
		return err
	}
	loadedRules, err := listRules(ctx, d)
	if err != nil {
		return err
	}
	auditLog, err := auditview.Recent(ctx, d, 100)
	if err != nil {
		return repoError(err)
	}
	primaryCurrency, err := money.Primary(ctx, d)
	if err != nil {
		return dbError(err)
	}
	options, err := currencyOptions(ctx, d)
	if err != nil {
		return err
	}
	taxKeywordText, err := taxKeywordsText(ctx, d)
	if err != nil {
		return err
	}

	s.Categories = loadedCategories
	s.CategoryUsage = usage
	s.Rules = loadedRules
	s.AuditLog = auditLog
	s.PrimaryCurrency = primaryCurrency
	s.CurrencyOptions = options
	s.TaxKeywords = taxKeywordText
	s.IsLoading = false
	return nil
}

func (s *SettingsState) actionError(err error) error {
	var settingsErr *SettingsError
	if errors.As(err, &settingsErr) {
		if key, ok := settingsErr.MessageKey(); ok {
			s.fail(key)
			return nil
		}
	}
	s.fail(i18n.ErrorUnexpected)
	return err
}

func (s *SettingsState) fail(key i18n.Key) {
	message := i18n.Text(s.Language, key)
	s.Error = &message
}

func listCategories(ctx context.Context, d *db.Database) ([]model.Category, error) {
	loaded, err := categories.List(ctx, d)
	if err != nil {
		return nil, repoError(err)
	}
	return loaded, nil
}

func listRules(ctx context.Context, d *db.Database) ([]model.Rule, error) {
	loaded, err := rulerepo.List(ctx, d)
	if err != nil {
		return nil, repoError(err)
	}
	return loaded, nil
}

func commonCurrencies() []money.Currency {
	options := make([]money.Currency, 0, len(CommonCurrencies))
	for _, code := range CommonCurrencies {
		currency, err := money.NewCurrency(code)
		if err != nil {
			panic("the common currency codes are valid")
		}
		options = append(options, currency)
	}
	return options
}

// currencyOptions returns the common choices plus every currency an account
// uses, keeping the first occurrence.
func currencyOptions(ctx context.Context, d *db.Database) ([]money.Currency, error) {
	options := commonCurrencies()
	listed, err := accounts.List(ctx, d)
	if err != nil {
		return nil, repoError(err)
	}
	for _, account := range listed {
		if !containsCurrency(options, account.Currency) {
			options = append(options, account.Currency)
		}
	}
	return options, nil
}

// TaxKeywords returns the keywords the tax suggestions use: the stored lines,
// or the built-in defaults (see the report package).
func TaxKeywords(ctx context.Context, d *db.Database) ([]string, error) {
	return report.Keywords(ctx, d)
}

func taxKeywordsText(ctx context.Context, d *db.Database) (string, error) {
	keywords, err := TaxKeywords(ctx, d)
	if err != nil {
		return "", repoError(err)
	}
	return strings.Join(keywords, "\n"), nil
}

// deleteSetting deletes a setting. FinDesk's SettingsRepository.delete has no
// counterpart in the settings package yet.
func deleteSetting(ctx context.Context, d *db.Database, key string) error {
	return d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "DELETE FROM settings WHERE key = ?1", key)
		return err
	})
}

func categoryUsage(
	ctx context.Context,
	d *db.Database,
) (map[model.CategoryID]int, error) {
	usage := make(map[model.CategoryID]int)

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT category_id, count(*) FROM transactions\n"+
				"             WHERE category_id IS NOT NULL GROUP BY category_id",
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				id    int64
				count int64
			)
			if err := rows.Scan(&id, &count); err != nil {
				return err
			}
			usage[model.CategoryID(id)] = usizeFromInt64(count)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, dbError(err)
	}
	return usage, nil
}

func backupService(appPaths paths.AppPaths, profile *string) (*backup.Service, error) {
	dir, err := dataDir(appPaths, profile)
	if err != nil {
		return nil, profileErrorValue(err)
	}
	return backup.NewService(dir, BackupVersion), nil
}

// dataDir mirrors profiles::data_dir: nil is the default profile's root,
// while Some is a stored profile's folder (and "default" is rejected there,
// as profile_dir does).
func dataDir(appPaths paths.AppPaths, profile *string) (string, error) {
	if profile == nil {
		return profiles.DataDir(appPaths, "")
	}
	return profiles.ProfileDir(appPaths, *profile)
}

// validateDelete checks a delete target the same way profiles.Delete will,
// before any side effect runs.
func validateDelete(appPaths paths.AppPaths, openProfile *string, slug string) error {
	if openProfile != nil && *openProfile == slug {
		return profiles.ErrOpenProfile
	}
	if slug == "default" {
		return profiles.ErrDefaultProfile
	}
	listed, err := profiles.List(appPaths)
	if err != nil {
		return err
	}
	for _, profile := range listed {
		if profile.Slug != "" && profile.Slug == slug {
			return nil
		}
	}
	return &profiles.UnknownError{Slug: slug}
}

func profileError(err error) error {
	if errors.Is(err, profiles.ErrEmptyName) {
		return messageError(i18n.ErrorNameRequired)
	}
	return profileErrorValue(err)
}

func fileName(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return path
	}
	return base
}

func isHexColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for index := 1; index < len(color); index++ {
		character := color[index]
		isDigit := character >= '0' && character <= '9'
		isLower := character >= 'a' && character <= 'f'
		isUpper := character >= 'A' && character <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

// lower is full Unicode lower-casing, like Rust's str::to_lowercase: "İ"
// becomes "i" + U+0307 and a word-final "Σ" becomes "ς".
func lower(text string) string {
	return cases.Lower(xlanguage.Und).String(text)
}

// upper is full Unicode upper-casing, like Rust's str::to_uppercase: "ß"
// becomes "SS".
func upper(text string) string {
	return cases.Upper(xlanguage.Und).String(text)
}

func containsCurrency(options []money.Currency, currency money.Currency) bool {
	for _, option := range options {
		if option == currency {
			return true
		}
	}
	return false
}

func profileSlug(profile profiles.Profile) *string {
	if profile.Slug == "" {
		return nil
	}
	value := profile.Slug
	return &value
}

// profileSlugArgument converts an optional slug for the profiles package,
// whose default profile is the empty string, as Rust's None is.
func profileSlugArgument(slug *string) string {
	if slug == nil {
		return ""
	}
	return *slug
}

// slugText is the text used in audit details and messages, where Rust writes
// "default" for the default profile.
func slugText(slug *string) string {
	if slug == nil {
		return "default"
	}
	return *slug
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func saturatingSub(left, right int) int {
	if right > left {
		return 0
	}
	return left - right
}

func usizeFromInt64(value int64) int {
	maxInt := int64(^uint(0) >> 1)
	if value < 0 || value > maxInt {
		return 0
	}
	return int(value)
}
