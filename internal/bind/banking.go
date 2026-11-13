package bind

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"

	"github.com/wailsapp/wails/v3/pkg/application"

	bankingcore "github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// BankingEvent is the name of the event emitted after every bank-login change.
const BankingEvent = "banking:changed"

// BankingPromptEvent is the name of the event emitted when a prompt opens or
// closes.
const BankingPromptEvent = "banking:prompt"

func init() {
	// Registering the events here gives the generator the names and payload
	// types, so the frontend binds both at compile time.
	application.RegisterEvent[BankingDTO](BankingEvent)
	application.RegisterEvent[PromptDTO](BankingPromptEvent)
}

var errBankingNotStarted = errors.New("banking is not started")

// BankMenuItemDTO is one entry of a bank card's menu.
type BankMenuItemDTO struct {
	Action  string `json:"action"`
	Label   string `json:"label"`
	Checked bool   `json:"checked"`
	Danger  bool   `json:"danger"`
	Enabled bool   `json:"enabled"`
	Icon    string `json:"icon"`
}

// BankingPromptChoiceDTO is one choice in a TAN-procedure or TAN-medium prompt.
type BankingPromptChoiceDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PromptDTO is the UI model of one open banking prompt, without any answer.
//
// It carries only what Rust's prompt_view shows (plus the bridge's sequence id,
// a deliberate deviation from Rust): kind, titles, the PIN prompt's bank name
// and user id, the choice list, the TAN challenge and flicker note, and the
// decoded challenge image as a data: URL. No field ever holds a PIN or a TAN.
type PromptDTO struct {
	ID              uint64                   `json:"id"`
	Kind            string                   `json:"kind"`
	Title           string                   `json:"title"`
	Text            string                   `json:"text"`
	Label           string                   `json:"label"`
	BankName        string                   `json:"bankName"`
	UserID          string                   `json:"userId"`
	RememberVisible bool                     `json:"rememberVisible"`
	Choices         []BankingPromptChoiceDTO `json:"choices"`
	Challenge       string                   `json:"challenge"`
	FlickerNote     string                   `json:"flickerNote"`
	ImageURL        string                   `json:"imageUrl"`
}

// BankingBankDTO is one bank's online-banking fields, as bank_cards and
// bank_menu build them for the page.
type BankingBankDTO struct {
	BankID      int               `json:"bankId"`
	Syncing     bool              `json:"syncing"`
	SyncStep    string            `json:"syncStep"`
	SyncEnabled bool              `json:"syncEnabled"`
	SyncTooltip string            `json:"syncTooltip"`
	PinText     string            `json:"pinText"`
	Menu        []BankMenuItemDTO `json:"menu"`
}

// BankingDTO is everything the banking part of the Accounts page shows, as
// apply_props, publish, sync_all_* and online_sync_note set it.
type BankingDTO struct {
	ErrorText   string `json:"errorText"`
	MessageText string `json:"messageText"`

	Busy        bool   `json:"busy"`
	Syncing     bool   `json:"syncing"`
	SyncBankID  int    `json:"syncBankId"`
	SyncStep    string `json:"syncStep"`
	SyncAllText string `json:"syncAllText"`

	SyncAllVisible      bool `json:"syncAllVisible"`
	SyncAllEnabled      bool `json:"syncAllEnabled"`
	SyncControlsVisible bool `json:"syncControlsVisible"`
	OnlineSyncAvailable bool `json:"onlineSyncAvailable"`
	CanRememberPin      bool `json:"canRememberPin"`

	OnlineSyncNote string           `json:"onlineSyncNote"`
	Banks          []BankingBankDTO `json:"banks"`
	InfoText       string           `json:"infoText"`
}

// BankingOptions is the process wiring the caller supplies.
type BankingOptions struct {
	// Emit publishes banking:changed; nil uses the running Wails application.
	Emit func(BankingDTO)
	// EmitPrompt publishes banking:prompt; nil uses the running Wails
	// application.
	EmitPrompt func(PromptDTO)
}

// Banking is the bank-login Wails service.
type Banking struct {
	session    *shell.Session
	emit       func(BankingDTO)
	emitPrompt func(PromptDTO)
	// answerSeam is nil in production. Tests set it to retain each PIN or TAN
	// byte slice the service creates and assert it is wiped after the call.
	answerSeam func([]byte)
}

// NewBanking builds the banking service. session may be nil only for the
// self-check, which inspects the registry without calling the service.
func NewBanking(session *shell.Session, options BankingOptions) *Banking {
	return &Banking{session: session, emit: options.Emit, emitPrompt: options.EmitPrompt}
}

// ServiceName implements application.ServiceName.
func (b *Banking) ServiceName() string {
	return "Banking"
}

// ServiceShutdown is a no-op: the router owns the one session listener.
func (b *Banking) ServiceShutdown() error {
	return nil
}

// publishPage emits the current banking state.
func (b *Banking) publishPage() {
	b.emitDTO(b.snapshot())
}

// publishPrompt emits the current prompt, or the empty prompt when none is
// open.
func (b *Banking) publishPrompt() {
	b.emitPromptDTO(b.promptSnapshot())
}

// State returns the current banking state without changing anything.
func (b *Banking) State() (BankingDTO, error) {
	if b.session == nil {
		return BankingDTO{}, nil
	}
	return b.snapshot(), nil
}

// Prompt returns the open prompt, or the empty prompt when none is open.
func (b *Banking) Prompt() (PromptDTO, error) {
	if b.session == nil {
		return PromptDTO{}, nil
	}
	return b.promptSnapshot(), nil
}

// Load loads the bank list.
func (b *Banking) Load() (BankingDTO, error) {
	return b.action(func() { b.session.BankingLoad() })
}

// Refresh reloads the bank list.
func (b *Banking) Refresh() (BankingDTO, error) {
	return b.action(func() { b.session.BankingRefresh() })
}

// OpenAddBank opens the add-online-bank dialog.
func (b *Banking) OpenAddBank() (BankingDTO, error) {
	return b.action(func() { b.session.BankingOpenAddBank() })
}

// SubmitAddOnlineBank validates and stores an online bank login.
func (b *Banking) SubmitAddOnlineBank(name string, blz string, bic string, userID string, serverURL string) (BankingDTO, error) {
	return b.action(func() {
		b.session.BankingSubmitAddOnlineBank(name, blz, bic, userID, serverURL)
	})
}

// SubmitConnection saves the connection dialog.
func (b *Banking) SubmitConnection(bankID int, name string, blz string, userID string, serverURL string) (BankingDTO, error) {
	return b.action(func() {
		b.session.BankingSubmitConnection(bankID, name, blz, userID, serverURL)
	})
}

// ForgetPin forgets a bank's remembered PIN.
func (b *Banking) ForgetPin(bankID int) (BankingDTO, error) {
	return b.action(func() { b.session.BankingForgetPin(bankID) })
}

// SetIncludeInSyncAll toggles whether a bank takes part in "Sync all".
func (b *Banking) SetIncludeInSyncAll(bankID int) (BankingDTO, error) {
	return b.action(func() { b.session.BankingSetIncludeInSyncAll(bankID) })
}

// ClearHistory drops a bank's stored history start.
func (b *Banking) ClearHistory(bankID int) (BankingDTO, error) {
	return b.action(func() { b.session.BankingClearHistory(bankID) })
}

// SubmitHistory validates the date and starts a history sync.
func (b *Banking) SubmitHistory(bankID int, date string) (BankingDTO, error) {
	return b.action(func() { b.session.BankingSubmitHistory(bankID, date) })
}

// StartSync starts one bank's sync.
func (b *Banking) StartSync(bankID int) (BankingDTO, error) {
	return b.action(func() { b.session.BankingStartSync(bankID) })
}

// StartSyncAll starts "Sync all".
func (b *Banking) StartSyncAll() (BankingDTO, error) {
	return b.action(func() { b.session.BankingStartSyncAll() })
}

// DeleteBank deletes an online bank with its registration and remembered PIN.
func (b *Banking) DeleteBank(bankID int) (BankingDTO, error) {
	return b.action(func() { b.session.BankingRemoveBank(bankID) })
}

// OpenConnectionDialog opens the connection-settings dialog.
func (b *Banking) OpenConnectionDialog(bankID int) (BankingDTO, error) {
	return b.action(func() { b.session.BankingOpenConnectionDialog(bankID) })
}

// OpenHistoryDialog opens the history-from dialog.
func (b *Banking) OpenHistoryDialog(bankID int) (BankingDTO, error) {
	return b.action(func() { b.session.BankingOpenHistoryDialog(bankID) })
}

// HandleBankMenu handles one entry of a bank card's menu.
func (b *Banking) HandleBankMenu(bankID int, action string) (BankingDTO, error) {
	return b.action(func() { b.session.BankingHandleMenu(bankID, action) })
}

// ClearMessages clears the bank-login error and success banner.
func (b *Banking) ClearMessages() (BankingDTO, error) {
	return b.action(func() { b.session.BankingClearMessages() })
}

// ClearInfo clears the bank's informational message.
func (b *Banking) ClearInfo() error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	bridge.ClearInfo()
	return nil
}

// AnswerPin answers the PIN prompt with id. The PIN is converted once to a byte
// slice, handed to the bridge, and wiped on every path. Remember is ignored
// when the system offers no credential store, exactly as can_remember_pin says.
func (b *Banking) AnswerPin(id uint64, pin string, remember bool) error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	if remember && (b.session == nil || !b.session.Credentials().IsAvailable()) {
		remember = false
	}
	raw := []byte(pin)
	defer clear(raw)
	b.captureAnswer(raw)
	bridge.AnswerPin(id, raw, remember)
	clear(raw)
	return nil
}

// CancelPin cancels the open PIN prompt.
func (b *Banking) CancelPin(id uint64) error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	bridge.AnswerPin(id, nil, false)
	return nil
}

// AnswerTan answers the TAN prompt with id. The TAN is converted once to a byte
// slice, handed to the bridge, and wiped on every path; it is never stored.
func (b *Banking) AnswerTan(id uint64, tan string) error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	raw := []byte(tan)
	defer clear(raw)
	b.captureAnswer(raw)
	bridge.AnswerTan(id, raw)
	clear(raw)
	return nil
}

// CancelTan cancels the open TAN prompt.
func (b *Banking) CancelTan(id uint64) error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	bridge.AnswerTan(id, nil)
	return nil
}

// AnswerChoice answers a TAN-procedure or TAN-medium prompt with id.
func (b *Banking) AnswerChoice(id uint64, choice string) error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	value := choice
	bridge.AnswerChoice(id, &value)
	return nil
}

// CancelChoice cancels the open choice prompt.
func (b *Banking) CancelChoice(id uint64) error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	bridge.AnswerChoice(id, nil)
	return nil
}

// AnswerDecoupled answers a decoupled-confirmation prompt with id.
func (b *Banking) AnswerDecoupled(id uint64, confirmed bool) error {
	bridge := b.bridge()
	if bridge == nil {
		return errBankingNotStarted
	}
	bridge.AnswerDecoupled(id, confirmed)
	return nil
}

func (b *Banking) bridge() *bankingcore.PromptBridge {
	if b.session == nil {
		return nil
	}
	return b.session.Bridge()
}

func (b *Banking) captureAnswer(raw []byte) {
	if b.answerSeam != nil {
		b.answerSeam(raw)
	}
}

func (b *Banking) action(run func()) (BankingDTO, error) {
	if b.session == nil {
		return BankingDTO{}, errBankingNotStarted
	}
	run()
	return b.snapshot(), nil
}

func (b *Banking) emitDTO(dto BankingDTO) {
	if b.emit != nil {
		b.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(BankingEvent, dto)
	}
}

func (b *Banking) emitPromptDTO(dto PromptDTO) {
	if b.emitPrompt != nil {
		b.emitPrompt(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(BankingPromptEvent, dto)
	}
}

func (b *Banking) snapshot() BankingDTO {
	if b.session == nil {
		return BankingDTO{}
	}
	state := b.session.BankingState()
	dto := bankingDTO(state, b.session.Language())
	if bridge := b.session.Bridge(); bridge != nil {
		if info := bridge.CurrentInfo(); info != nil {
			dto.InfoText = *info
		}
	}
	return dto
}

func (b *Banking) promptSnapshot() PromptDTO {
	if b.session == nil {
		return emptyPromptDTO()
	}
	bridge := b.session.Bridge()
	if bridge == nil {
		return emptyPromptDTO()
	}
	canRemember := b.session.BankingState().CanRememberPin
	return promptDTO(bridge.Prompt(), b.session.Language(), canRemember)
}

func bankingDTO(state bankingcore.BankingState, language i18n.Language) BankingDTO {
	dto := BankingDTO{
		Busy:                state.IsSyncing(),
		Syncing:             state.SyncingBank != nil,
		SyncAllText:         syncAllText(&state),
		SyncAllVisible:      syncAllVisible(&state),
		SyncAllEnabled:      syncAllEnabled(&state),
		SyncControlsVisible: syncControlsVisible(shell.OnlineSyncAvailable, &state),
		OnlineSyncAvailable: shell.OnlineSyncAvailable,
		CanRememberPin:      state.CanRememberPin,
		OnlineSyncNote:      onlineSyncNote(&state),
		Banks:               bankingBanks(&state, language),
	}
	if state.Error != nil {
		dto.ErrorText = *state.Error
	}
	if state.Message != nil {
		dto.MessageText = *state.Message
	}
	if state.SyncingBank != nil {
		dto.SyncBankID = int(*state.SyncingBank)
	}
	if state.SyncStep != nil {
		dto.SyncStep = *state.SyncStep
	}
	return dto
}

func bankingBanks(state *bankingcore.BankingState, language i18n.Language) []BankingBankDTO {
	banks := make([]BankingBankDTO, 0, len(state.Banks))
	for _, bank := range state.Banks {
		banks = append(banks, BankingBankDTO{
			BankID:      int(bank.ID),
			Syncing:     state.SyncingBank != nil && *state.SyncingBank == bank.ID,
			SyncStep:    bankSyncStep(state, language),
			SyncEnabled: shell.OnlineSyncAvailable && !state.IsSyncing(),
			SyncTooltip: bankSyncTooltip(language),
			PinText:     bankPinText(&bank, state, language),
			Menu:        bankMenu(&bank, state, false, language),
		})
	}
	return banks
}

func emptyPromptDTO() PromptDTO {
	return PromptDTO{Choices: []BankingPromptChoiceDTO{}}
}

// promptDTO maps a core prompt into the dialog model, as Rust's prompt_view
// does. The remember field is hidden when no credential store exists.
func promptDTO(prompt *bankingcore.BankingPrompt, language i18n.Language, canRememberPin bool) PromptDTO {
	dto := emptyPromptDTO()
	if prompt == nil {
		return dto
	}
	dto.ID = prompt.ID
	switch prompt.Kind {
	case bankingcore.Pin:
		dto.Kind = "pin"
		dto.Title = i18n.Format(language, i18n.PinFor, i18n.Str(prompt.BankName))
		dto.Text = i18n.Format(language, i18n.PinExplanation, i18n.Str(prompt.UserID))
		dto.Label = i18n.Text(language, i18n.Pin)
		dto.BankName = prompt.BankName
		dto.UserID = prompt.UserID
		dto.RememberVisible = prompt.OfferRemember && canRememberPin
	case bankingcore.ChooseTanMethod:
		dto.Kind = "choice"
		dto.Title = i18n.Text(language, i18n.ChooseTanProcedure)
		dto.Choices = make([]BankingPromptChoiceDTO, 0, len(prompt.TanMethods))
		for _, method := range prompt.TanMethods {
			dto.Choices = append(dto.Choices, BankingPromptChoiceDTO{ID: method.ID, Label: method.Name})
		}
	case bankingcore.ChooseTanMedium:
		dto.Kind = "choice"
		dto.Title = i18n.Text(language, i18n.ChooseTanMedium)
		dto.Choices = make([]BankingPromptChoiceDTO, 0, len(prompt.TanMediums))
		for _, medium := range prompt.TanMediums {
			dto.Choices = append(dto.Choices, BankingPromptChoiceDTO{ID: medium, Label: medium})
		}
	case bankingcore.Tan:
		dto.Kind = "tan"
		dto.Title = i18n.Text(language, i18n.TanRequired)
		dto.Label = i18n.Text(language, i18n.TanLabel)
		dto.Challenge = prompt.Challenge.Message
		if dto.Challenge == "" || isBlank(dto.Challenge) {
			dto.Challenge = i18n.Text(language, i18n.TanDefaultMessage)
		}
		if prompt.Challenge.FlickerCode != nil {
			dto.FlickerNote = i18n.Text(language, i18n.TanFlickerUnsupported)
		}
		dto.ImageURL = tanImageDataURL(prompt.Challenge.Image)
	case bankingcore.Decoupled:
		dto.Kind = "decoupled"
		dto.Title = i18n.Text(language, i18n.ConfirmInBankingApp)
		dto.Text = prompt.Message
	}
	return dto
}

func isBlank(text string) bool {
	for _, character := range text {
		if character != ' ' && character != '\t' && character != '\n' && character != '\r' {
			return false
		}
	}
	return true
}

// tanImageDataURL decodes a bank's photoTAN/QR image. The bytes are untrusted:
// more than 1 MB of input or more than 2048x2048 pixels is refused, and a decode
// failure just leaves the text-only dialog.
func tanImageDataURL(bytesIn []byte) string {
	const maxInput = 1_000_000
	const maxSide = 2048
	if len(bytesIn) == 0 || len(bytesIn) > maxInput {
		return ""
	}
	config, err := png.DecodeConfig(bytes.NewReader(bytesIn))
	if err != nil || config.Width == 0 || config.Height == 0 || config.Width > maxSide || config.Height > maxSide {
		return ""
	}
	image, err := png.Decode(bytes.NewReader(bytesIn))
	if err != nil || image.Bounds().Dx() != config.Width || image.Bounds().Dy() != config.Height {
		return ""
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
}

// syncAllText is Rust's sync_all_text: empty before the first bank starts.
func syncAllText(state *bankingcore.BankingState) string {
	if state.SyncAll == nil || state.SyncAll.Position == 0 {
		return ""
	}
	return i18n.Format(
		state.Language,
		i18n.SyncAllProgress,
		i18n.Int(int64(state.SyncAll.Position)),
		i18n.Int(int64(state.SyncAll.Total)),
		i18n.Str(state.SyncAll.BankName),
	)
}

// syncAllVisible reports whether the page shows the "Sync all" control.
func syncAllVisible(state *bankingcore.BankingState) bool {
	for _, bank := range state.Banks {
		if !bank.Offline {
			return true
		}
	}
	return false
}

// syncControlsVisible reports whether the page shows any Sync control.
func syncControlsVisible(available bool, state *bankingcore.BankingState) bool {
	if !available {
		return false
	}
	for _, bank := range state.Banks {
		if !bank.Offline {
			return true
		}
	}
	return false
}

// syncAllEnabled reports whether "Sync all" may start now.
func syncAllEnabled(state *bankingcore.BankingState) bool {
	return shell.OnlineSyncAvailable && syncAllReady(state)
}

func syncAllReady(state *bankingcore.BankingState) bool {
	if state.IsSyncing() {
		return false
	}
	for _, bank := range state.Banks {
		if bank.IncludeInSyncAll && !bank.Offline {
			return true
		}
	}
	return false
}

// onlineSyncNote is the note shown while online sync is unavailable, or empty
// when every bank is offline.
func onlineSyncNote(state *bankingcore.BankingState) string {
	if shell.OnlineSyncAvailable || !syncAllVisible(state) {
		return ""
	}
	return i18n.Text(state.Language, i18n.OnlineSyncNotYetAvailable)
}

func bankSyncStep(state *bankingcore.BankingState, language i18n.Language) string {
	if state.SyncStep != nil {
		return *state.SyncStep
	}
	return i18n.Text(language, i18n.Syncing)
}

func bankSyncTooltip(language i18n.Language) string {
	if shell.OnlineSyncAvailable {
		return ""
	}
	return i18n.Text(language, i18n.OnlineSyncNotYetAvailable)
}

func bankPinText(bank *bankingcore.BankItem, state *bankingcore.BankingState, language i18n.Language) string {
	if bank.Offline {
		return ""
	}
	if bank.HasSavedPin {
		return i18n.Text(language, i18n.PinSavedForSyncs)
	}
	if state.CanRememberPin {
		return i18n.Text(language, i18n.PinAskedEverySync)
	}
	return i18n.Text(language, i18n.PinNeverStored)
}

// bankMenu is Rust's bank_menu, in FinDesk's order. Offline banks only rename
// or delete; online banks get the sync actions. History and "include in Sync
// all" stay disabled until the FinTS gateway replaces UnavailableGateway.
func bankMenu(
	bank *bankingcore.BankItem,
	state *bankingcore.BankingState,
	accountsBusy bool,
	language i18n.Language,
) []BankMenuItemDTO {
	if bank == nil {
		return []BankMenuItemDTO{}
	}
	enabled := !state.IsSyncing()
	syncEnabled := shell.OnlineSyncAvailable && enabled
	items := make([]BankMenuItemDTO, 0, 7)
	if !bank.Offline {
		items = append(items,
			BankMenuItemDTO{
				Action:  "history",
				Label:   i18n.Text(language, i18n.FetchHistory),
				Enabled: syncEnabled,
				Icon:    "date_range",
			},
			BankMenuItemDTO{
				Action:  "include",
				Label:   i18n.Text(language, i18n.IncludeInSyncAll),
				Checked: bank.IncludeInSyncAll,
				Enabled: syncEnabled,
				Icon:    "check",
			},
			BankMenuItemDTO{
				Action:  "rename",
				Label:   i18n.Text(language, i18n.RenameConnection),
				Enabled: enabled,
				Icon:    "edit",
			},
			BankMenuItemDTO{
				Action:  "connection",
				Label:   i18n.Text(language, i18n.ConnectionSettings),
				Enabled: enabled,
				Icon:    "settings",
			},
		)
		if bank.SyncFrom != nil {
			items = append(items, BankMenuItemDTO{
				Action:  "clear-history",
				Label:   i18n.Text(language, i18n.ClearHistoryStart),
				Enabled: enabled,
				Icon:    "history",
			})
		}
		if bank.HasSavedPin {
			items = append(items, BankMenuItemDTO{
				Action:  "forget-pin",
				Label:   i18n.Text(language, i18n.ForgetPin),
				Enabled: enabled,
				Icon:    "lock",
			})
		}
	} else {
		items = append(items, BankMenuItemDTO{
			Action:  "connection",
			Label:   i18n.Text(language, i18n.RenameConnection),
			Enabled: enabled,
			Icon:    "edit",
		})
	}
	items = append(items, BankMenuItemDTO{
		Action:  "delete",
		Label:   i18n.Text(language, i18n.RemoveBank),
		Danger:  true,
		Enabled: enabled && !accountsBusy,
		Icon:    "delete",
	})
	return items
}
