package bind

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/findesk"
	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/ruleimport"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// SettingsEvent is the name of the event emitted after every Settings change.
const SettingsEvent = "settings:changed"

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[SettingsDTO](SettingsEvent)
}

var errSettingsNotStarted = errors.New("settings is not started")

// SettingsCategoryRowDTO is one row of the category list.
type SettingsCategoryRowDTO struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Colour      string `json:"colour"`
	BudgetText  string `json:"budgetText"`
	UsageText   string `json:"usageText"`
	TaxRelevant bool   `json:"taxRelevant"`
	Transfer    bool   `json:"transfer"`
}

// CategoryRowDTO is the row type's short name, matching Rust's CategoryRow.
type CategoryRowDTO = SettingsCategoryRowDTO

// SettingsColourSwatchDTO is one palette choice of the category dialog.
type SettingsColourSwatchDTO struct {
	Hex    string `json:"hex"`
	Colour string `json:"colour"`
}

// ColourSwatchDTO is the swatch type's short name, matching Rust's
// ColourSwatch.
type ColourSwatchDTO = SettingsColourSwatchDTO

// SettingsRuleRowDTO is one row of the rule list.
type SettingsRuleRowDTO struct {
	ID           int    `json:"id"`
	Pattern      string `json:"pattern"`
	CategoryText string `json:"categoryText"`
	Colour       string `json:"colour"`
	Detail       string `json:"detail"`
	PrioText     string `json:"prioText"`
}

// RuleRowDTO is the row type's short name, matching Rust's RuleRow.
type RuleRowDTO = SettingsRuleRowDTO

// SettingsProfileRowDTO is one row of the profile list, as Rust's
// profile_model builds it.
type SettingsProfileRowDTO struct {
	// Index is the row SwitchProfile takes.
	Index int `json:"index"`
	// Name is the profile's display name.
	Name string `json:"name"`
	// Open says whether this profile is the one running now.
	Open bool `json:"open"`
	// NextStart says whether this profile opens at the next start.
	NextStart bool `json:"nextStart"`
	// CanSwitch says whether the row may be switched to.
	CanSwitch bool `json:"canSwitch"`
	// CanDelete says whether the row may be deleted.
	CanDelete bool `json:"canDelete"`
}

// ProfileRowDTO is the profile row's short name, matching Rust's ProfileRow.
type ProfileRowDTO = SettingsProfileRowDTO

// SettingsDTO is everything the Settings page shows.
//
// It carries appearance, the primary currency, categories, rules, tax keywords
// and the rule import/export dialog (G8f-1), plus the profile list and dialogs,
// backup/restore and the FinDesk import (G8f-2). A backup appears only by its
// file name.
type SettingsDTO struct {
	Loading     bool   `json:"loading"`
	Section     int    `json:"section"`
	ErrorText   string `json:"errorText"`
	MessageText string `json:"messageText"`

	CategoriesEmpty     bool `json:"categoriesEmpty"`
	HasDuplicateColours bool `json:"hasDuplicateColours"`

	CurrencyOptions      []string                 `json:"currencyOptions"`
	PrimaryCurrency      string                   `json:"primaryCurrency"`
	PrimaryCurrencyIndex int                      `json:"primaryCurrencyIndex"`
	CategoryRows         []SettingsCategoryRowDTO `json:"categoryRows"`
	RuleRows             []SettingsRuleRowDTO     `json:"ruleRows"`
	TaxKeywords          string                   `json:"taxKeywords"`
	TaxKeywordsCount     string                   `json:"taxKeywordsCount"`

	CategoryDialogSwatches     []SettingsColourSwatchDTO `json:"categoryDialogSwatches"`
	RuleDialogCategories       []string                  `json:"ruleDialogCategories"`
	RuleDialogFieldOptions     []string                  `json:"ruleDialogFieldOptions"`
	RuleDialogDirectionOptions []string                  `json:"ruleDialogDirectionOptions"`
	RuleDialogTaxOptions       []string                  `json:"ruleDialogTaxOptions"`

	CategoryDialogOpen     bool   `json:"categoryDialogOpen"`
	CategoryDialogID       int    `json:"categoryDialogId"`
	CategoryDialogName     string `json:"categoryDialogName"`
	CategoryDialogColour   string `json:"categoryDialogColour"`
	CategoryDialogBudget   string `json:"categoryDialogBudget"`
	CategoryDialogTax      bool   `json:"categoryDialogTax"`
	CategoryDialogTransfer bool   `json:"categoryDialogTransfer"`
	CategoryDialogError    string `json:"categoryDialogError"`

	CategoryDeleteOpen  bool   `json:"categoryDeleteOpen"`
	CategoryDeleteID    int    `json:"categoryDeleteId"`
	CategoryDeleteTitle string `json:"categoryDeleteTitle"`
	CategoryDeleteText  string `json:"categoryDeleteText"`

	RuleDialogOpen           bool   `json:"ruleDialogOpen"`
	RuleDialogID             int    `json:"ruleDialogId"`
	RuleDialogPattern        string `json:"ruleDialogPattern"`
	RuleDialogCategoryIndex  int    `json:"ruleDialogCategoryIndex"`
	RuleDialogPriority       string `json:"ruleDialogPriority"`
	RuleDialogAutoApply      bool   `json:"ruleDialogAutoApply"`
	RuleDialogFieldIndex     int    `json:"ruleDialogFieldIndex"`
	RuleDialogDirectionIndex int    `json:"ruleDialogDirectionIndex"`
	RuleDialogTaxIndex       int    `json:"ruleDialogTaxIndex"`
	RuleDialogError          string `json:"ruleDialogError"`

	RuleDeleteOpen  bool   `json:"ruleDeleteOpen"`
	RuleDeleteID    int    `json:"ruleDeleteId"`
	RuleDeleteTitle string `json:"ruleDeleteTitle"`
	RuleDeleteText  string `json:"ruleDeleteText"`

	RuleImportOpen           bool   `json:"ruleImportOpen"`
	RuleImportBusy           bool   `json:"ruleImportBusy"`
	RuleImportCategorize     bool   `json:"ruleImportCategorize"`
	RuleImportNewRulesText   string `json:"ruleImportNewRulesText"`
	RuleImportCategoriesText string `json:"ruleImportCategoriesText"`
	RuleImportCategoriesList string `json:"ruleImportCategoriesList"`
	RuleImportDuplicatesText string `json:"ruleImportDuplicatesText"`
	RuleImportConflictsText  string `json:"ruleImportConflictsText"`
	RuleImportConflictsList  string `json:"ruleImportConflictsList"`
	RuleImportErrorsText     string `json:"ruleImportErrorsText"`
	RuleImportErrorsList     string `json:"ruleImportErrorsList"`
	RuleImportShowCategorize bool   `json:"ruleImportShowCategorize"`
	RuleImportCategorizeText string `json:"ruleImportCategorizeText"`
	RuleImportConfirmEnabled bool   `json:"ruleImportConfirmEnabled"`

	ProfileRows []SettingsProfileRowDTO `json:"profileRows"`

	ProfileDialogOpen  bool   `json:"profileDialogOpen"`
	ProfileDialogIndex int    `json:"profileDialogIndex"`
	ProfileDialogTitle string `json:"profileDialogTitle"`
	ProfileDialogName  string `json:"profileDialogName"`
	ProfileDialogError string `json:"profileDialogError"`

	ProfileDeleteOpen  bool   `json:"profileDeleteOpen"`
	ProfileDeleteIndex int    `json:"profileDeleteIndex"`
	ProfileDeleteTitle string `json:"profileDeleteTitle"`
	ProfileDeleteText  string `json:"profileDeleteText"`

	// Busy is the backup/restore busy flag.
	Busy               bool   `json:"busy"`
	HasPendingRestore  bool   `json:"hasPendingRestore"`
	BackupRestoredText string `json:"backupRestoredText"`

	RestoreOpen            bool   `json:"restoreOpen"`
	RestoreTitle           string `json:"restoreTitle"`
	RestoreText            string `json:"restoreText"`
	RestorePassphraseOpen  bool   `json:"restorePassphraseOpen"`
	RestorePassphraseFile  string `json:"restorePassphraseFile"`
	RestorePassphraseWrong bool   `json:"restorePassphraseWrong"`

	FindeskBusy            bool   `json:"findeskBusy"`
	FindeskFolder          string `json:"findeskFolder"`
	FindeskPassphraseOpen  bool   `json:"findeskPassphraseOpen"`
	FindeskPassphraseWrong bool   `json:"findeskPassphraseWrong"`
	FindeskReportOpen      bool   `json:"findeskReportOpen"`
	FindeskReportTitle     string `json:"findeskReportTitle"`
	FindeskReportText      string `json:"findeskReportText"`
	FindeskReportError     bool   `json:"findeskReportError"`
}

// SettingsOptions is the process wiring the caller supplies.
type SettingsOptions struct {
	// Emit publishes settings:changed; nil uses the running Wails application.
	Emit func(SettingsDTO)
}

// Settings is the Settings page's Wails service.
type Settings struct {
	session *shell.Session
	emit    func(SettingsDTO)
	// observeSecret is nil in production. Tests set it to retain each secret
	// the service creates (a []byte for a restore passphrase, a
	// *secret.Passphrase for FinDesk's) and assert it is wiped after the job.
	observeSecret func(any)
}

// NewSettings builds the Settings service. session may be nil only for the
// self-check, which inspects the registry without calling the service.
func NewSettings(session *shell.Session, options SettingsOptions) *Settings {
	return &Settings{session: session, emit: options.Emit}
}

// ServiceName implements application.ServiceName.
func (s *Settings) ServiceName() string {
	return "Settings"
}

// ServiceShutdown is a no-op: the router owns the one session listener.
func (s *Settings) ServiceShutdown() error {
	return nil
}

// publishPage emits the current Settings state.
func (s *Settings) publishPage() {
	s.emitDTO(s.snapshot())
}

// State returns the current Settings state without changing anything.
func (s *Settings) State() (SettingsDTO, error) {
	if s.session == nil {
		return SettingsDTO{}, nil
	}
	return s.snapshot(), nil
}

// Load loads the page, as opening Settings does.
func (s *Settings) Load() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsLoad() })
}

// DismissMessage clears the error and success banner.
func (s *Settings) DismissMessage() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsDismissMessages() })
}

// SetPrimaryCurrency stores the primary currency.
func (s *Settings) SetPrimaryCurrency(code string) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsSetPrimaryCurrency(code) })
}

// AddDefaultCategories adds the default categories that do not exist yet.
func (s *Settings) AddDefaultCategories() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsAddDefaultCategories() })
}

// FixDuplicateColours recolours categories that share a colour.
func (s *Settings) FixDuplicateColours() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsFixDuplicateColours() })
}

// OpenAddCategory opens the category dialog for a new category.
func (s *Settings) OpenAddCategory() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsOpenAddCategory() })
}

// OpenEditCategory opens the category dialog for an existing category.
func (s *Settings) OpenEditCategory(id int) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsOpenEditCategory(model.CategoryID(id)) })
}

// OpenDeleteCategory opens the delete confirmation for a category.
func (s *Settings) OpenDeleteCategory(id int) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsRequestDeleteCategory(model.CategoryID(id)) })
}

// CategoryChanged re-runs the category validation while the dialog is edited.
func (s *Settings) CategoryChanged(name string, colour string) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCategoryChanged(name, colour) })
}

// SaveCategory saves the category dialog's fields.
func (s *Settings) SaveCategory(
	name string,
	colour string,
	budget string,
	taxRelevant bool,
	transfer bool,
) (SettingsDTO, error) {
	return s.action(func() {
		s.session.SettingsSaveCategory(name, colour, budget, taxRelevant, transfer)
	})
}

// CloseCategoryDialog closes the category dialog without changing anything.
func (s *Settings) CloseCategoryDialog() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCloseCategoryDialog() })
}

// ConfirmDeleteCategory deletes the category whose confirmation is open.
func (s *Settings) ConfirmDeleteCategory() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsConfirmDeleteCategory() })
}

// CancelDeleteCategory closes the category delete confirmation.
func (s *Settings) CancelDeleteCategory() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelDeleteCategory() })
}

// OpenAddRule opens the rule dialog for a new rule.
func (s *Settings) OpenAddRule() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsOpenAddRule() })
}

// OpenEditRule opens the rule dialog for an existing rule.
func (s *Settings) OpenEditRule(id int) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsOpenEditRule(model.RuleID(id)) })
}

// OpenDeleteRule opens the delete confirmation for a rule.
func (s *Settings) OpenDeleteRule(id int) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsRequestDeleteRule(model.RuleID(id)) })
}

// RuleChanged re-runs the rule validation while the dialog is edited.
func (s *Settings) RuleChanged(pattern string, priority string) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsRuleChanged(pattern, priority) })
}

// SaveRule saves the rule dialog's fields. The dropdown indices are resolved
// exactly as Rust's field_at, direction_at and tax_at do.
//
// flow is the direction index; the name avoids "dir", which the bindings'
// path-reflection check would mistake for a directory parameter.
func (s *Settings) SaveRule(
	pattern string,
	categoryIndex int,
	priority string,
	autoApply bool,
	fieldIndex int,
	flow int,
	taxIndex int,
) (SettingsDTO, error) {
	return s.action(func() {
		s.session.SettingsSaveRule(
			pattern,
			categoryIndex,
			priority,
			autoApply,
			fieldIndex,
			flow,
			taxIndex,
		)
	})
}

// CloseRuleDialog closes the rule dialog without changing anything.
func (s *Settings) CloseRuleDialog() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCloseRuleDialog() })
}

// ConfirmDeleteRule deletes the rule whose confirmation is open.
func (s *Settings) ConfirmDeleteRule() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsConfirmDeleteRule() })
}

// CancelDeleteRule closes the rule delete confirmation.
func (s *Settings) CancelDeleteRule() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelDeleteRule() })
}

// SaveTaxKeywords stores the tax suggestion keywords.
func (s *Settings) SaveTaxKeywords(text string) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsSaveTaxKeywords(text) })
}

// ResetTaxKeywords removes the custom keywords so the defaults apply again.
func (s *Settings) ResetTaxKeywords() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsResetTaxKeywords() })
}

// ExportRules asks for a file and exports the rules into it.
func (s *Settings) ExportRules() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsExportRules() })
}

// ImportRules asks for a rule file and prepares its preview.
func (s *Settings) ImportRules() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsImportRules() })
}

// ConfirmRuleImport applies the confirmed rule-import preview.
func (s *Settings) ConfirmRuleImport(categorize bool) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsConfirmRuleImport(categorize) })
}

// CancelRuleImport drops the preview without applying anything.
func (s *Settings) CancelRuleImport() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelRuleImport() })
}

// CreateBackup asks for a target and writes a backup of the open profile.
func (s *Settings) CreateBackup() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCreateBackup() })
}

// RestoreBackup asks for a backup file and prepares its confirmation dialog.
func (s *Settings) RestoreBackup() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsRestoreBackup() })
}

// ConfirmRestore confirms the restore candidate in the confirmation dialog.
func (s *Settings) ConfirmRestore() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsConfirmRestore() })
}

// CancelRestoreChoice drops the restore confirmation without changing
// anything.
func (s *Settings) CancelRestoreChoice() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelRestoreChoice() })
}

// ConfirmRestorePassphrase confirms a backup made with another passphrase.
//
// The input is converted once, handed to the session, which owns and wipes it
// on every path. No path, error or DTO keeps it.
func (s *Settings) ConfirmRestorePassphrase(passphrase string) (SettingsDTO, error) {
	if s.session == nil {
		return SettingsDTO{}, errSettingsNotStarted
	}
	input := []byte(passphrase)
	if s.observeSecret != nil {
		s.observeSecret(input)
	}
	s.session.SettingsConfirmRestorePassphrase(input)
	return s.snapshot(), nil
}

// CancelRestorePassphrase drops the passphrase request for a backup made under
// another passphrase.
func (s *Settings) CancelRestorePassphrase() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelRestorePassphrase() })
}

// CancelPendingRestore removes a staged restore that waits for the next start.
func (s *Settings) CancelPendingRestore() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelPendingRestore() })
}

// ImportFindesk asks for a FinDesk folder and opens the passphrase dialog for a
// valid one.
func (s *Settings) ImportFindesk() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsImportFindesk() })
}

// SubmitFindeskPassphrase runs the chosen FinDesk import.
//
// The input is converted once into a secret.Passphrase and every intermediate
// slice is wiped before this method returns. The session owns the passphrase and
// wipes it on every path; no DTO, event, error or log keeps it.
func (s *Settings) SubmitFindeskPassphrase(passphrase string) (SettingsDTO, error) {
	if s.session == nil {
		return SettingsDTO{}, errSettingsNotStarted
	}
	input := []byte(passphrase)
	if s.observeSecret != nil {
		s.observeSecret(input)
	}
	built, err := secret.NewPassphrase(input, secret.Unlock)
	wipe(input)
	if err != nil {
		built = nil
	}
	if built != nil && s.observeSecret != nil {
		s.observeSecret(built)
	}
	s.session.SettingsSubmitFindeskPassphrase(built)
	return s.snapshot(), nil
}

// CancelFindeskPassphrase closes the passphrase dialog and forgets the chosen
// folder.
func (s *Settings) CancelFindeskPassphrase() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelFindeskPassphrase() })
}

// CloseFindeskReport closes the success report or failure dialog.
func (s *Settings) CloseFindeskReport() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCloseFindeskReport() })
}

// NewProfile opens the name dialog for a new profile.
func (s *Settings) NewProfile() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsOpenNewProfile() })
}

// RenameProfile opens the name dialog for the profile at index.
func (s *Settings) RenameProfile(index int) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsOpenRenameProfile(index) })
}

// CloseProfileDialog closes the name dialog without changing anything.
func (s *Settings) CloseProfileDialog() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCloseProfileDialog() })
}

// SaveProfile creates or renames the profile the name dialog shows.
func (s *Settings) SaveProfile(name string) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsSaveProfile(name) })
}

// DeleteProfile opens the confirmation for deleting the profile at index.
func (s *Settings) DeleteProfile(index int) (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsRequestDeleteProfile(index) })
}

// CancelDeleteProfile closes the delete confirmation without moving anything.
func (s *Settings) CancelDeleteProfile() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsCancelDeleteProfile() })
}

// ConfirmDeleteProfile moves the confirmed profile's folder aside.
func (s *Settings) ConfirmDeleteProfile() (SettingsDTO, error) {
	return s.action(func() { s.session.SettingsConfirmDeleteProfile() })
}

func (s *Settings) action(run func()) (SettingsDTO, error) {
	if s.session == nil {
		return SettingsDTO{}, errSettingsNotStarted
	}
	run()
	return s.snapshot(), nil
}

func (s *Settings) emitDTO(dto SettingsDTO) {
	if s.emit != nil {
		s.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(SettingsEvent, dto)
	}
}

func (s *Settings) snapshot() SettingsDTO {
	if s.session == nil {
		return SettingsDTO{}
	}
	return settingsDTO(
		s.session.SettingsState(),
		s.session.SettingsDialog(),
		s.session.SettingsSection(),
		s.session.RestoredFrom(),
		s.session.FindeskState(),
	)
}

func settingsDTO(
	state settingsview.SettingsState,
	dialog shell.SettingsDialogState,
	section int,
	restoredFrom *string,
	findeskState findesk.FindeskImportState,
) SettingsDTO {
	dto := SettingsDTO{
		Loading:                    state.IsLoading,
		Section:                    section,
		CategoriesEmpty:            len(state.Categories) == 0,
		HasDuplicateColours:        hasDuplicateColours(state.Categories),
		CurrencyOptions:            currencyOptions(state),
		PrimaryCurrency:            state.PrimaryCurrency.String(),
		PrimaryCurrencyIndex:       primaryCurrencyIndex(state),
		CategoryRows:               categoryRows(state),
		RuleRows:                   ruleRows(state),
		TaxKeywords:                state.TaxKeywords,
		TaxKeywordsCount:           taxKeywordsCount(state),
		CategoryDialogSwatches:     colourSwatches(),
		RuleDialogCategories:       ruleDialogCategories(state),
		RuleDialogFieldOptions:     fieldOptions(state.Language),
		RuleDialogDirectionOptions: directionOptions(state.Language),
		RuleDialogTaxOptions:       taxOptions(state.Language),
		CategoryDialogOpen:         dialog.CategoryOpen,
		CategoryDialogID:           dialog.CategoryID,
		CategoryDialogName:         dialog.CategoryName,
		CategoryDialogColour:       dialog.CategoryColour,
		CategoryDialogBudget:       dialog.CategoryBudget,
		CategoryDialogTax:          dialog.CategoryTaxRelevant,
		CategoryDialogTransfer:     dialog.CategoryTransfer,
		CategoryDialogError:        dialog.CategoryError,
		CategoryDeleteOpen:         dialog.CategoryDeleteOpen,
		CategoryDeleteID:           dialog.CategoryDeleteID,
		CategoryDeleteTitle:        dialog.CategoryDeleteTitle,
		CategoryDeleteText:         dialog.CategoryDeleteText,
		RuleDialogOpen:             dialog.RuleOpen,
		RuleDialogID:               dialog.RuleID,
		RuleDialogPattern:          dialog.RulePattern,
		RuleDialogCategoryIndex:    dialog.RuleCategoryIndex,
		RuleDialogPriority:         dialog.RulePriority,
		RuleDialogAutoApply:        dialog.RuleAutoApply,
		RuleDialogFieldIndex:       dialog.RuleFieldIndex,
		RuleDialogDirectionIndex:   dialog.RuleDirectionIndex,
		RuleDialogTaxIndex:         dialog.RuleTaxIndex,
		RuleDialogError:            dialog.RuleError,
		RuleDeleteOpen:             dialog.RuleDeleteOpen,
		RuleDeleteID:               dialog.RuleDeleteID,
		RuleDeleteTitle:            dialog.RuleDeleteTitle,
		RuleDeleteText:             dialog.RuleDeleteText,
		ProfileDialogOpen:          dialog.ProfileOpen,
		ProfileDialogIndex:         dialog.ProfileIndex,
		ProfileDialogTitle:         dialog.ProfileTitle,
		ProfileDialogName:          dialog.ProfileName,
		ProfileDialogError:         dialog.ProfileError,
		ProfileDeleteOpen:          dialog.ProfileDeleteOpen,
		ProfileDeleteIndex:         dialog.ProfileDeleteIndex,
		ProfileDeleteTitle:         dialog.ProfileDeleteTitle,
		ProfileDeleteText:          dialog.ProfileDeleteText,
		Busy:                       state.IsBusy,
		HasPendingRestore:          state.HasPendingRestore,
		FindeskPassphraseOpen:      dialog.FindeskPassphraseOpen,
	}
	if state.Error != nil {
		dto.ErrorText = *state.Error
	}
	if state.Message != nil {
		dto.MessageText = *state.Message
	}
	applyRuleImport(&dto, state, dialog)
	applyProfiles(&dto, state)
	applyBackup(&dto, state, restoredFrom)
	applyFindesk(&dto, findeskState, dialog)
	return dto
}

// applyProfiles maps the profile list, as Rust's apply_profiles and
// profile_model do.
func applyProfiles(dto *SettingsDTO, state settingsview.SettingsState) {
	open := ""
	if state.OpenProfile != nil {
		open = *state.OpenProfile
	}
	next := ""
	if state.NextProfile != nil {
		next = *state.NextProfile
	}
	rows := make([]SettingsProfileRowDTO, 0, len(state.Profiles))
	for index, profile := range state.Profiles {
		isOpen := profile.Slug == open
		rows = append(rows, SettingsProfileRowDTO{
			Index:     index,
			Name:      profile.Name,
			Open:      isOpen,
			NextStart: !isOpen && profile.Slug == next,
			CanSwitch: !isOpen,
			CanDelete: !profile.IsDefault() && !isOpen,
		})
	}
	dto.ProfileRows = rows
}

// applyBackup maps the backup and restore state, including the two restore
// dialogs, as Rust's apply_backup does.
func applyBackup(
	dto *SettingsDTO,
	state settingsview.SettingsState,
	restoredFrom *string,
) {
	if restoredFrom != nil {
		dto.BackupRestoredText = i18n.Format(
			state.Language,
			i18n.BackupRestoredAt,
			i18n.Str(*restoredFrom),
		)
	}

	if candidate := state.RestoreCandidate; candidate != nil {
		name := fileName(candidate.Backup)
		dto.RestoreTitle = i18n.Format(
			state.Language,
			i18n.RestoreFileTitle,
			i18n.Str(name),
		)
		created := i18n.Format(
			state.Language,
			i18n.RestoreCreated,
			i18n.Str(candidate.Info.Created),
			i18n.Int(candidate.Info.Transactions),
			i18n.Int(int64(candidate.Info.Documents)),
		)
		explanation := i18n.Text(state.Language, i18n.RestoreExplanation)
		dto.RestoreText = created + "\n\n" + explanation
		dto.RestoreOpen = true
	}

	dto.RestorePassphraseWrong = state.RestorePassphraseWrong
	if forBackup := state.RestorePassphraseFor; forBackup != nil {
		dto.RestorePassphraseFile = fileName(*forBackup)
		dto.RestorePassphraseOpen = true
	}
}

// applyFindesk maps the FinDesk import state to its folder, passphrase and
// result dialogs, as Rust's apply_findesk does.
func applyFindesk(
	dto *SettingsDTO,
	state findesk.FindeskImportState,
	dialog shell.SettingsDialogState,
) {
	dto.FindeskBusy = state.IsImporting
	if state.SourceDir != nil {
		dto.FindeskFolder = *state.SourceDir
	}
	dto.FindeskPassphraseWrong = state.PassphraseWrong
	dto.FindeskPassphraseOpen = dialog.FindeskPassphraseOpen

	switch {
	case state.Report != nil:
		dto.FindeskReportTitle = i18n.Text(state.Language, i18n.FinDeskImportReportTitle)
		dto.FindeskReportText = findesk.ReportText(state.Report, state.Language)
		dto.FindeskReportError = false
		dto.FindeskReportOpen = true
	case state.Error != nil:
		dto.FindeskReportTitle = i18n.Text(state.Language, i18n.FinDeskImportFailedTitle)
		dto.FindeskReportText = *state.Error
		dto.FindeskReportError = true
		dto.FindeskReportOpen = true
	}
}

// fileName is Rust's file_name: the last path element, or the whole text when
// there is none. The DTO never shows a backup's full path.
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

func categoryRows(state settingsview.SettingsState) []SettingsCategoryRowDTO {
	rows := make([]SettingsCategoryRowDTO, 0, len(state.Categories))
	for _, category := range state.Categories {
		usage := state.CategoryUsage[category.ID]
		budgetText := ""
		if category.BudgetLimit != nil {
			budgetText = i18n.Format(
				state.Language,
				i18n.BudgetAmount,
				i18n.Str(format.Money(*category.BudgetLimit, state.Language, false)),
			)
		}
		rows = append(rows, SettingsCategoryRowDTO{
			ID:          int(category.ID),
			Name:        category.Name,
			Colour:      colourHex(category.Color),
			BudgetText:  budgetText,
			UsageText:   i18n.Format(state.Language, i18n.CategoryUsage, i18n.Int(int64(usage))),
			TaxRelevant: category.IsTaxRelevant,
			Transfer:    category.IsTransfer,
		})
	}
	return rows
}

func ruleRows(state settingsview.SettingsState) []SettingsRuleRowDTO {
	names := make(map[model.CategoryID]string, len(state.Categories))
	colours := make(map[model.CategoryID]string, len(state.Categories))
	for _, category := range state.Categories {
		names[category.ID] = category.Name
		colours[category.ID] = category.Color
	}

	rows := make([]SettingsRuleRowDTO, 0, len(state.Rules))
	for _, rule := range state.Rules {
		categoryText := names[rule.CategoryID]
		switch {
		case rule.TaxRelevant != nil && *rule.TaxRelevant:
			categoryText += " · " + i18n.Text(state.Language, i18n.RuleTaxMarkerYes)
		case rule.TaxRelevant != nil && !*rule.TaxRelevant:
			categoryText += " · " + i18n.Text(state.Language, i18n.RuleTaxMarkerNo)
		}

		restrictions := make([]string, 0, 2)
		switch rule.MatchField {
		case model.RuleFieldPartner:
			restrictions = append(restrictions, i18n.Text(state.Language, i18n.RuleFieldPartner))
		case model.RuleFieldPurpose:
			restrictions = append(restrictions, i18n.Text(state.Language, i18n.RuleFieldPurpose))
		}
		switch rule.Direction {
		case model.RuleDirectionExpense:
			restrictions = append(restrictions, i18n.Text(state.Language, i18n.RuleDirectionExpense))
		case model.RuleDirectionIncome:
			restrictions = append(restrictions, i18n.Text(state.Language, i18n.RuleDirectionIncome))
		}

		prioText := i18n.Format(state.Language, i18n.Prio, i18n.Int(rule.Priority))
		if !rule.AutoApply {
			prioText += " · " + i18n.Text(state.Language, i18n.Manual)
		}

		colour := "#757575"
		if categoryColour, ok := colours[rule.CategoryID]; ok {
			colour = colourHex(categoryColour)
		}
		rows = append(rows, SettingsRuleRowDTO{
			ID:           int(rule.ID),
			Pattern:      rule.Pattern,
			CategoryText: categoryText,
			Colour:       colour,
			Detail:       strings.Join(restrictions, " · "),
			PrioText:     prioText,
		})
	}
	return rows
}

func currencyOptions(state settingsview.SettingsState) []string {
	options := make([]string, 0, len(state.CurrencyOptions))
	for _, currency := range state.CurrencyOptions {
		options = append(options, currency.String())
	}
	return options
}

func primaryCurrencyIndex(state settingsview.SettingsState) int {
	for index, currency := range state.CurrencyOptions {
		if currency == state.PrimaryCurrency {
			return index
		}
	}
	return 0
}

func hasDuplicateColours(categories []model.Category) bool {
	seen := make(map[string]int, len(categories))
	for _, category := range categories {
		seen[strings.ToUpper(category.Color)]++
	}
	for _, count := range seen {
		if count > 1 {
			return true
		}
	}
	return false
}

func taxKeywordsCount(state settingsview.SettingsState) string {
	count := 0
	for _, line := range strings.Split(state.TaxKeywords, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return i18n.Format(state.Language, i18n.TaxKeywordsCount, i18n.Int(int64(count)))
}

func ruleDialogCategories(state settingsview.SettingsState) []string {
	names := make([]string, 0, len(state.Categories))
	for _, category := range state.Categories {
		names = append(names, category.Name)
	}
	return names
}

func colourSwatches() []SettingsColourSwatchDTO {
	swatches := make([]SettingsColourSwatchDTO, 0, len(categories.Colors))
	for _, hex := range categories.Colors {
		swatches = append(swatches, SettingsColourSwatchDTO{
			Hex:    hex,
			Colour: colourHex(hex),
		})
	}
	return swatches
}

func fieldOptions(language i18n.Language) []string {
	return []string{
		i18n.Text(language, i18n.RuleFieldAny),
		i18n.Text(language, i18n.RuleFieldPartner),
		i18n.Text(language, i18n.RuleFieldPurpose),
	}
}

func directionOptions(language i18n.Language) []string {
	return []string{
		i18n.Text(language, i18n.RuleDirectionAny),
		i18n.Text(language, i18n.RuleDirectionExpense),
		i18n.Text(language, i18n.RuleDirectionIncome),
	}
}

func taxOptions(language i18n.Language) []string {
	return []string{
		i18n.Text(language, i18n.RuleTaxAsCategory),
		i18n.Text(language, i18n.TaxRelevantChoice),
		i18n.Text(language, i18n.TaxNotRelevantChoice),
	}
}

func applyRuleImport(
	dto *SettingsDTO,
	state settingsview.SettingsState,
	dialog shell.SettingsDialogState,
) {
	preview := state.RuleImport
	if preview == nil {
		dto.RuleImportOpen = false
		dto.RuleImportCategorize = dialog.RuleImportCategorize
		return
	}

	language := state.Language
	dto.RuleImportOpen = true
	dto.RuleImportBusy = state.IsImportingRules
	if dialog.RuleImportOpen {
		dto.RuleImportCategorize = dialog.RuleImportCategorize
	} else {
		dto.RuleImportCategorize = true
	}
	dto.RuleImportNewRulesText = i18n.Format(
		language,
		i18n.RuleImportNewRules,
		i18n.Int(int64(len(preview.NewRules))),
	)
	if len(preview.CategoriesToCreate) == 0 {
		dto.RuleImportCategoriesText = ""
	} else {
		dto.RuleImportCategoriesText = i18n.Format(
			language,
			i18n.RuleImportCategories,
			i18n.Int(int64(len(preview.CategoriesToCreate))),
		)
	}
	dto.RuleImportCategoriesList = listed(language, preview.CategoriesToCreate)
	if preview.Duplicates == 0 {
		dto.RuleImportDuplicatesText = ""
	} else {
		dto.RuleImportDuplicatesText = i18n.Format(
			language,
			i18n.RuleImportDuplicates,
			i18n.Int(int64(preview.Duplicates)),
		)
	}
	if len(preview.Conflicts) == 0 {
		dto.RuleImportConflictsText = ""
	} else {
		dto.RuleImportConflictsText = i18n.Format(
			language,
			i18n.RuleImportConflicts,
			i18n.Int(int64(len(preview.Conflicts))),
		)
	}
	dto.RuleImportConflictsList = conflictList(language, preview.Conflicts)
	if len(preview.Errors) == 0 {
		dto.RuleImportErrorsText = ""
	} else {
		dto.RuleImportErrorsText = i18n.Format(
			language,
			i18n.RuleImportErrors,
			i18n.Int(int64(len(preview.Errors))),
		)
	}
	dto.RuleImportErrorsList = errorList(language, preview.Errors)
	dto.RuleImportShowCategorize = preview.MatchCount > 0
	dto.RuleImportCategorizeText = i18n.Format(
		language,
		i18n.RuleImportCategorize,
		i18n.Int(int64(preview.MatchCount)),
	)
	dto.RuleImportConfirmEnabled = !state.IsImportingRules &&
		(len(preview.NewRules) > 0 || len(preview.CategoriesToCreate) > 0)
}

func listed(language i18n.Language, names []string) string {
	shown := names
	if len(shown) > 20 {
		shown = shown[:20]
	}
	text := strings.Join(shown, ", ")
	more := len(names) - 20
	if more > 0 {
		if text != "" {
			text += ", "
		}
		text += i18n.Format(language, i18n.AndMore, i18n.Int(int64(more)))
	}
	return text
}

func conflictList(language i18n.Language, conflicts []ruleimport.RuleImportConflict) string {
	lines := make([]string, 0, len(conflicts))
	for index, conflict := range conflicts {
		if index == 20 {
			break
		}
		lines = append(lines, i18n.Format(
			language,
			i18n.RuleImportConflictLine,
			i18n.Int(int64(conflict.Line)),
			i18n.Str(conflict.Pattern),
			i18n.Str(conflict.FileCategory),
			i18n.Str(conflict.ExistingCategory),
		))
	}
	text := strings.Join(lines, "\n")
	if more := len(conflicts) - 20; more > 0 {
		if text != "" {
			text += "\n"
		}
		text += i18n.Format(language, i18n.AndMore, i18n.Int(int64(more)))
	}
	return text
}

func errorList(language i18n.Language, errors []rules.RuleFileError) string {
	lines := make([]string, 0, len(errors))
	for index, entry := range errors {
		if index == 20 {
			break
		}
		lines = append(lines, i18n.Format(
			language,
			i18n.RuleImportErrorLine,
			i18n.Int(int64(entry.Line)),
			i18n.Str(entry.Reason),
		))
	}
	text := strings.Join(lines, "\n")
	if more := len(errors) - 20; more > 0 {
		if text != "" {
			text += "\n"
		}
		text += i18n.Format(language, i18n.AndMore, i18n.Int(int64(more)))
	}
	return text
}

func colourHex(hex string) string {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(hex), "#"))
	if len(text) != 6 {
		return "#757575"
	}
	for index := 0; index < len(text); index++ {
		character := text[index]
		isDigit := character >= '0' && character <= '9'
		isLower := character >= 'a' && character <= 'f'
		isUpper := character >= 'A' && character <= 'F'
		if !isDigit && !isLower && !isUpper {
			return "#757575"
		}
	}
	return "#" + strings.ToUpper(text)
}
