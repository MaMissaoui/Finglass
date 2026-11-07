//! Wiring for the bank-login part of the Accounts page.
//!
//! The state, the actions and the blocking prompt bridge live in `finglass_core::app::banking`. This module maps that
//! state into the page and dialog models, claims sync work on the UI thread, and runs every blocking call on a
//! worker. PINs and TANs only travel from a dialog callback into [`finglass_core::app::banking::PromptBridge`]; they
//! are never kept here.

use std::path::PathBuf;
use std::sync::{Arc, Weak as SyncWeak};
use std::thread;
use std::time::Duration;

use finglass_core::app::accounts::{AccountsState, Busy};
use finglass_core::app::banking::{BankItem, BankingPrompt, BankingState, PromptBridge};
use finglass_core::app::startup::slug_key;
use finglass_core::db::Database;
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::model::BankConnectionId;
use finglass_core::paths::AppPaths;
use finglass_core::sync::{BankingService, CredentialStore, FinTsClient, UnavailableGateway};
use secrecy::SecretString;
use slint::{ComponentHandle, ModelRc, VecModel, Weak};

use crate::accounts;
use crate::shell::ShellState;
use crate::{AccountsBankCard, BankMenuItem, BankingPromptChoice, MainWindow};

/// FinDesk's `UiBankingInteraction` default: a dialog may wait ten minutes for the user.
pub(crate) const PROMPT_TIMEOUT: Duration = Duration::from_secs(10 * 60);

/// The FinTS product id. The real adapter (M5b) owns registration; the placeholder gateway ignores it.
const PRODUCT_ID: &str = "Finglass";

/// A prompt bridge together with the token that keeps its callbacks from reaching a later profile's dialogs.
pub(crate) struct BridgeHandle {
    bridge: Arc<PromptBridge>,
    _token: Arc<()>,
}

impl BridgeHandle {
    /// Builds a bridge whose `on_change` posts only while this handle is the live one.
    pub(crate) fn new(main: Weak<MainWindow>, state: SyncWeak<ShellState>) -> Self {
        let token = Arc::new(());
        let token_weak = Arc::downgrade(&token);
        let bridge = Arc::new_cyclic(|bridge_weak: &SyncWeak<PromptBridge>| {
            let bridge_weak = bridge_weak.clone();
            let token_weak = token_weak.clone();
            let main_weak = main.clone();
            let state_weak = state.clone();
            PromptBridge::new(PROMPT_TIMEOUT, move || {
                let Some(bridge) = bridge_weak.upgrade() else {
                    return;
                };
                let Some(main) = main_weak.upgrade() else {
                    return;
                };
                let main_weak = main.as_weak();
                let token = token_weak.clone();
                let state_weak = state_weak.clone();
                let _ = slint::invoke_from_event_loop(move || {
                    if token.upgrade().is_none() {
                        return;
                    }
                    let Some(ui) = main_weak.upgrade() else {
                        return;
                    };
                    let can_remember = state_weak
                        .upgrade()
                        .map(|state| state.banking.lock().expect("banking state poisoned").can_remember_pin)
                        .unwrap_or(false);
                    apply_prompt(&ui, &bridge, can_remember);
                });
            })
        });
        Self { bridge, _token: token }
    }

    /// The live bridge.
    pub(crate) fn bridge(&self) -> Arc<PromptBridge> {
        self.bridge.clone()
    }
}

/// The inputs a worker needs to build a [`BankingService`].
#[derive(Clone)]
pub(crate) struct ServiceInputs {
    credentials: Arc<dyn CredentialStore>,
    profile_dir: PathBuf,
    language: Language,
}

impl ServiceInputs {
    pub(crate) fn service(&self) -> BankingService {
        BankingService::new(
            FinTsClient::new(
                Arc::new(UnavailableGateway::new(self.language)),
                self.profile_dir.clone(),
                PRODUCT_ID,
                self.credentials.clone(),
            )
            .with_language(self.language),
        )
    }
}

/// Reads the open profile's service inputs. Never creates a credential store: the shell's one is reused.
pub(crate) fn service_inputs(state: &ShellState, language: Language) -> Option<ServiceInputs> {
    let slug = state.slug();
    let profile_dir = state.paths.profile_dir(slug_key(slug.as_deref())).ok()?;
    Some(ServiceInputs {
        credentials: state.credentials.clone(),
        profile_dir,
        language,
    })
}

/// Keeps the banking bank list in step with an Accounts action that added or removed an offline bank.
pub(crate) fn merge_banks(banking: &mut BankingState, accounts: &AccountsState) {
    banking
        .banks
        .retain(|bank| accounts.bank_accesses.iter().any(|access| access.id == bank.id));
    for access in &accounts.bank_accesses {
        if banking.banks.iter().any(|bank| bank.id == access.id) {
            continue;
        }
        banking.banks.push(BankItem {
            id: access.id,
            bank_name: access.bank_name.clone(),
            blz: access.blz.clone(),
            user_id: access.user_id.clone(),
            server_url: access.server_url.clone(),
            offline: access.offline,
            include_in_sync_all: access.include_in_sync_all,
            sync_from: access.sync_from,
            last_sync: access.last_sync,
            has_saved_pin: false,
        });
    }
}

/// The state read by a worker.
pub(crate) fn state_snapshot(state: &ShellState) -> BankingState {
    state.banking.lock().expect("banking state poisoned").clone()
}

/// The open profile's bridge.
pub(crate) fn current_bridge(state: &ShellState) -> Option<Arc<PromptBridge>> {
    state
        .banking_bridge
        .lock()
        .expect("banking bridge poisoned")
        .as_ref()
        .map(BridgeHandle::bridge)
}

/// Replaces the open profile's bridge, cancelling the old one first so its worker unblocks.
pub(crate) fn replace_bridge(state: &ShellState, main: Weak<MainWindow>, state_weak: SyncWeak<ShellState>) {
    let mut slot = state.banking_bridge.lock().expect("banking bridge poisoned");
    if let Some(old) = slot.replace(BridgeHandle::new(main, state_weak)) {
        old.bridge.cancel_pending();
    }
}

// ---- Model mapping -------------------------------------------------------

/// The bank-card model for the page, with the sync fields filled in from the banking state.
pub(crate) fn bank_cards(
    accounts: &AccountsState,
    banking: &BankingState,
    language: Language,
) -> Vec<AccountsBankCard> {
    let mut cards = accounts::bank_cards(accounts, language);
    for card in &mut cards {
        let Some(bank) = banking
            .banks
            .iter()
            .find(|bank| i32::try_from(bank.id.0).ok() == Some(card.bank_id))
        else {
            card.menu = ModelRc::new(VecModel::from(Vec::<BankMenuItem>::new()));
            continue;
        };
        card.syncing = banking.syncing_bank == Some(bank.id);
        card.sync_step = banking
            .sync_step
            .clone()
            .unwrap_or_else(|| i18n::text(language, Key::Syncing).to_owned())
            .into();
        card.sync_enabled = !banking.is_syncing();
        card.pin_text = if bank.offline {
            String::new()
        } else if bank.has_saved_pin {
            i18n::text(language, Key::PinSavedForSyncs).to_owned()
        } else if banking.can_remember_pin {
            i18n::text(language, Key::PinAskedEverySync).to_owned()
        } else {
            i18n::text(language, Key::PinNeverStored).to_owned()
        }
        .into();
        card.menu = ModelRc::new(VecModel::from(bank_menu(bank, banking, accounts, language)));
    }
    cards
}

/// The menu entries of one bank card. Offline banks only rename or delete; online banks get the sync actions.
pub(crate) fn bank_menu(
    bank: &BankItem,
    banking: &BankingState,
    accounts: &AccountsState,
    language: Language,
) -> Vec<BankMenuItem> {
    let enabled = !banking.is_syncing();
    let mut items = Vec::new();
    if !bank.offline {
        items.push(BankMenuItem {
            action: "sync".into(),
            label: i18n::text(language, Key::SyncNow).into(),
            checked: false,
            danger: false,
            enabled: enabled && !banking.is_syncing(),
        });
        items.push(BankMenuItem {
            action: "history".into(),
            label: i18n::text(language, Key::FetchHistory).into(),
            checked: false,
            danger: false,
            enabled,
        });
        if bank.sync_from.is_some() {
            items.push(BankMenuItem {
                action: "clear-history".into(),
                label: i18n::text(language, Key::ClearHistoryStart).into(),
                checked: false,
                danger: false,
                enabled,
            });
        }
        items.push(BankMenuItem {
            action: "include".into(),
            label: i18n::text(language, Key::IncludeInSyncAll).into(),
            checked: bank.include_in_sync_all,
            danger: false,
            enabled,
        });
        items.push(BankMenuItem {
            action: "connection".into(),
            label: i18n::text(language, Key::ConnectionSettings).into(),
            checked: false,
            danger: false,
            enabled,
        });
        if bank.has_saved_pin {
            items.push(BankMenuItem {
                action: "forget-pin".into(),
                label: i18n::text(language, Key::ForgetPin).into(),
                checked: false,
                danger: false,
                enabled,
            });
        }
    } else {
        items.push(BankMenuItem {
            action: "connection".into(),
            label: i18n::text(language, Key::RenameConnection).into(),
            checked: false,
            danger: false,
            enabled,
        });
    }
    items.push(BankMenuItem {
        action: "delete".into(),
        label: i18n::text(language, Key::RemoveBank).into(),
        checked: false,
        danger: true,
        enabled: enabled && !accounts.busy.active(),
    });
    items
}

/// The "Sync all" progress text, empty before the first bank starts.
pub(crate) fn sync_all_text(banking: &BankingState) -> String {
    banking.sync_all.as_ref().map_or_else(String::new, |progress| {
        if progress.position == 0 {
            String::new()
        } else {
            i18n::format(
                banking.language,
                Key::SyncAllProgress,
                &[
                    progress.position.into(),
                    progress.total.into(),
                    Arg::Str(progress.bank_name.clone()),
                ],
            )
        }
    })
}

/// Whether the page shows the "Sync all" control at all.
pub(crate) fn sync_all_visible(banking: &BankingState) -> bool {
    banking.banks.iter().any(|bank| !bank.offline)
}

/// Whether "Sync all" may start now.
pub(crate) fn sync_all_enabled(banking: &BankingState) -> bool {
    !banking.is_syncing()
        && banking
            .banks
            .iter()
            .any(|bank| bank.include_in_sync_all && !bank.offline)
}

/// Applies the banking-only page fields. The account fields are applied by [`crate::accounts::apply`].
pub(crate) fn apply_props(main: &MainWindow, accounts: &AccountsState, banking: &BankingState) {
    main.set_accounts_banking_busy(banking.is_syncing());
    main.set_accounts_sync_all_progress(sync_all_text(banking).into());
    main.set_accounts_sync_all_visible(sync_all_visible(banking));
    main.set_accounts_sync_all_enabled(sync_all_enabled(banking));
    main.set_accounts_error_text(
        banking
            .error
            .clone()
            .or_else(|| accounts.error.clone())
            .unwrap_or_default()
            .into(),
    );
    main.set_accounts_message_text(
        banking
            .message
            .clone()
            .or_else(|| accounts.message.clone())
            .unwrap_or_default()
            .into(),
    );
    // Banking errors and messages take the page's one banner. Keep the accounts texts when the banking state is quiet.
}

/// Publishes a banking snapshot on the UI thread, using the page's current account state.
pub(crate) fn publish(main: &MainWindow, state: &ShellState, banking: &BankingState) {
    let accounts = state.accounts.lock().expect("accounts state poisoned").clone();
    accounts::apply(main, &accounts, banking);
}

/// Publishes both snapshots and stores them in the shell state.
pub(crate) fn publish_both(main: &MainWindow, state: &ShellState, banking: &BankingState, accounts: &AccountsState) {
    *state.banking.lock().expect("banking state poisoned") = banking.clone();
    *state.accounts.lock().expect("accounts state poisoned") = accounts.clone();
    accounts::apply(main, accounts, banking);
}

// ---- Prompt mapping ------------------------------------------------------

/// One option in a choice prompt.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct ChoiceView {
    /// The value the bridge receives.
    pub id: String,
    /// The label shown to the user.
    pub label: String,
}

/// The UI model of one open [`BankingPrompt`], without the image bytes.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct PromptView {
    /// "pin", "choice", "tan", "decoupled" or "".
    pub kind: String,
    /// The dialog title.
    pub title: String,
    /// The explanatory text.
    pub text: String,
    /// The field label.
    pub label: String,
    /// Whether the remember-PIN checkbox is shown.
    pub remember_visible: bool,
    /// The choice list.
    pub choices: Vec<ChoiceView>,
    /// The TAN challenge text.
    pub challenge: String,
    /// The chipTAN note shown instead of a flicker code.
    pub flicker_note: String,
}

impl PromptView {
    fn empty() -> Self {
        Self {
            kind: String::new(),
            title: String::new(),
            text: String::new(),
            label: String::new(),
            remember_visible: false,
            choices: Vec::new(),
            challenge: String::new(),
            flicker_note: String::new(),
        }
    }
}

/// Maps a core prompt into the dialog model. The remember field is hidden when no credential store exists.
pub(crate) fn prompt_view(prompt: &BankingPrompt, language: Language, can_remember_pin: bool) -> PromptView {
    match prompt {
        BankingPrompt::Pin {
            bank_name,
            user_id,
            offer_remember,
        } => PromptView {
            kind: "pin".to_owned(),
            title: i18n::format(language, Key::PinFor, &[Arg::Str(bank_name.clone())]),
            text: i18n::format(language, Key::PinExplanation, &[Arg::Str(user_id.clone())]),
            label: i18n::text(language, Key::Pin).to_owned(),
            remember_visible: *offer_remember && can_remember_pin,
            ..PromptView::empty()
        },
        BankingPrompt::ChooseTanMethod(methods) => PromptView {
            kind: "choice".to_owned(),
            title: i18n::text(language, Key::ChooseTanProcedure).to_owned(),
            choices: methods
                .iter()
                .map(|method| ChoiceView {
                    id: method.id.clone(),
                    label: method.name.clone(),
                })
                .collect(),
            ..PromptView::empty()
        },
        BankingPrompt::ChooseTanMedium(media) => PromptView {
            kind: "choice".to_owned(),
            title: i18n::text(language, Key::ChooseTanMedium).to_owned(),
            choices: media
                .iter()
                .map(|medium| ChoiceView {
                    id: medium.clone(),
                    label: medium.clone(),
                })
                .collect(),
            ..PromptView::empty()
        },
        BankingPrompt::Tan(challenge) => PromptView {
            kind: "tan".to_owned(),
            title: i18n::text(language, Key::TanRequired).to_owned(),
            label: i18n::text(language, Key::TanLabel).to_owned(),
            challenge: if challenge.message.trim().is_empty() {
                i18n::text(language, Key::TanDefaultMessage).to_owned()
            } else {
                challenge.message.clone()
            },
            flicker_note: if challenge.flicker_code.is_some() {
                i18n::text(language, Key::TanFlickerUnsupported).to_owned()
            } else {
                String::new()
            },
            ..PromptView::empty()
        },
        BankingPrompt::Decoupled(message) => PromptView {
            kind: "decoupled".to_owned(),
            title: i18n::text(language, Key::ConfirmInBankingApp).to_owned(),
            text: message.clone(),
            ..PromptView::empty()
        },
    }
}

/// Shows the current prompt and info banner. Called on the event loop from the bridge's `on_change`.
pub(crate) fn apply_prompt(main: &MainWindow, bridge: &PromptBridge, can_remember_pin: bool) {
    main.set_banking_info_text(bridge.info().unwrap_or_default().into());
    let Some(prompt) = bridge.prompt() else {
        clear_prompt(main);
        return;
    };
    let language = Language::from_code(main.get_language().as_str()).unwrap_or(Language::En);
    let view = prompt_view(&prompt, language, can_remember_pin);
    main.set_banking_prompt(view.kind.into());
    main.set_banking_prompt_title(view.title.into());
    main.set_banking_prompt_text(view.text.into());
    main.set_banking_prompt_label(view.label.into());
    main.set_banking_prompt_remember_visible(view.remember_visible);
    main.set_banking_prompt_choices(ModelRc::new(VecModel::from(
        view.choices
            .into_iter()
            .map(|choice| BankingPromptChoice {
                id: choice.id.into(),
                label: choice.label.into(),
            })
            .collect::<Vec<_>>(),
    )));
    main.set_banking_prompt_challenge(view.challenge.into());
    main.set_banking_prompt_flicker_note(view.flicker_note.into());

    let image = match &prompt {
        BankingPrompt::Tan(challenge) => challenge.image.as_deref().and_then(decode_tan_image),
        _ => None,
    };
    main.set_banking_prompt_show_image(image.is_some());
    main.set_banking_prompt_image(image.unwrap_or_default());
}

/// Decodes a bank's photoTAN/QR image. The bytes are untrusted: more than 1 MB of input or more than
/// 2048×2048 pixels is refused, and a decode failure just leaves the text-only dialog.
pub(crate) fn decode_tan_image(bytes: &[u8]) -> Option<slint::Image> {
    const MAX_INPUT: usize = 1_000_000;
    const MAX_SIDE: u32 = 2048;
    if bytes.is_empty() || bytes.len() > MAX_INPUT {
        return None;
    }

    let limits = png::Limits { bytes: MAX_INPUT };
    let mut decoder = png::Decoder::new_with_limits(std::io::Cursor::new(bytes), limits);
    decoder.set_transformations(png::Transformations::normalize_to_color8());
    let mut reader = decoder.read_info().ok()?;
    let (width, height) = {
        let info = reader.info();
        (info.width, info.height)
    };
    if width == 0 || height == 0 || width > MAX_SIDE || height > MAX_SIDE {
        return None;
    }

    let mut buffer = vec![0; reader.output_buffer_size()?];
    let info = reader.next_frame(&mut buffer).ok()?;
    let data = &buffer[..info.buffer_size()];
    let rgba = match info.color_type {
        png::ColorType::Rgba => data.to_vec(),
        png::ColorType::Rgb => data
            .as_chunks::<3>()
            .0
            .iter()
            .flat_map(|pixel| [pixel[0], pixel[1], pixel[2], 255])
            .collect(),
        png::ColorType::Grayscale => data.iter().flat_map(|value| [*value, *value, *value, 255]).collect(),
        png::ColorType::GrayscaleAlpha => data
            .as_chunks::<2>()
            .0
            .iter()
            .flat_map(|pixel| [pixel[0], pixel[0], pixel[0], pixel[1]])
            .collect(),
        _ => return None,
    };
    let expected = usize::try_from(width).ok()? * usize::try_from(height).ok()?;
    if rgba.len() < expected * 4 {
        return None;
    }

    let mut pixels = slint::SharedPixelBuffer::<slint::Rgba8Pixel>::new(width, height);
    for (pixel, chunk) in pixels.make_mut_slice().iter_mut().zip(rgba.as_chunks::<4>().0) {
        pixel.r = chunk[0];
        pixel.g = chunk[1];
        pixel.b = chunk[2];
        pixel.a = chunk[3];
    }
    Some(slint::Image::from_rgba8(pixels))
}

fn clear_prompt(main: &MainWindow) {
    main.set_banking_prompt("".into());
    main.set_banking_prompt_title("".into());
    main.set_banking_prompt_text("".into());
    main.set_banking_prompt_label("".into());
    main.set_banking_prompt_remember_visible(false);
    main.set_banking_prompt_choices(ModelRc::new(VecModel::from(Vec::<BankingPromptChoice>::new())));
    main.set_banking_prompt_challenge("".into());
    main.set_banking_prompt_flicker_note("".into());
    main.set_banking_prompt_show_image(false);
    main.set_banking_prompt_image(slint::Image::default());
}

// ---- Secret answers ------------------------------------------------------

/// The PIN dialog's OK and Cancel both pass through here. Cancel uses `None`.
pub(crate) fn answer_pin(bridge: &PromptBridge, pin: Option<&str>, remember: bool) {
    let pin = pin.map(SecretString::from);
    bridge.answer_pin(pin, remember);
}

/// The TAN dialog's OK and Cancel both pass through here. Cancel uses `None`.
pub(crate) fn answer_tan(bridge: &PromptBridge, tan: Option<&str>) {
    bridge.answer_tan(tan.map(SecretString::from));
}

/// A TAN-procedure or TAN-medium row. Cancel uses `None`.
pub(crate) fn answer_choice(bridge: &PromptBridge, choice: Option<&str>) {
    bridge.answer_choice(choice.map(str::to_owned));
}

/// The decoupled "I have confirmed" / Cancel pair.
pub(crate) fn answer_decoupled(bridge: &PromptBridge, confirmed: bool) {
    bridge.answer_decoupled(confirmed);
}

/// The dialog's Cancel: answers the open prompt's own `None` without closing the bridge.
pub(crate) fn cancel_prompt(bridge: &PromptBridge, kind: &str) {
    match kind {
        "pin" => answer_pin(bridge, None, false),
        "choice" => answer_choice(bridge, None),
        "tan" => answer_tan(bridge, None),
        "decoupled" => answer_decoupled(bridge, false),
        _ => {}
    }
}

// ---- Page actions --------------------------------------------------------

fn refuse_if_syncing(main: &MainWindow, state: &ShellState) -> bool {
    let mut banking = state.banking.lock().expect("banking state poisoned");
    if !banking.is_syncing() {
        return false;
    }
    banking.error = Some(i18n::text(banking.language, Key::ErrorConnectionBusy).to_owned());
    let snapshot = banking.clone();
    drop(banking);
    publish(main, state, &snapshot);
    true
}

/// Opens the add-bank dialog unless a sync holds the profile.
pub(crate) fn open_add_bank(main: &MainWindow, state: &Arc<ShellState>) {
    if refuse_if_syncing(main, state) {
        return;
    }
    {
        let mut banking = state.banking.lock().expect("banking state poisoned");
        banking.clear_messages();
    }
    main.set_accounts_dialog_online(false);
    accounts::open_add_bank(main, state);
    let banking = state_snapshot(state);
    publish(main, state, &banking);
}

/// Validates and stores an online bank login. Validation errors stay in the open dialog.
pub(crate) fn submit_add_online_bank(
    main: &MainWindow,
    state: &Arc<ShellState>,
    name: &str,
    blz: &str,
    bic: &str,
    user_id: &str,
    server_url: &str,
) {
    if refuse_if_syncing(main, state) {
        return;
    }
    {
        let banking = state.banking.lock().expect("banking state poisoned");
        if let Some(message) = banking
            .validate_bank_access(name, blz)
            .or_else(|| banking.validate_server_url(server_url))
        {
            main.set_accounts_dialog_error(message.into());
            return;
        }
    }
    accounts::close_dialog(main);
    let (name, blz, bic, user_id, server_url) = (
        name.to_owned(),
        blz.to_owned(),
        bic.to_owned(),
        user_id.to_owned(),
        server_url.to_owned(),
    );
    spawn_action(main, state, move |banking, db, service, _busy| {
        let _ = banking.add_bank_access(db, service, &name, &blz, &bic, &user_id, &server_url);
    });
}

/// Saves the connection dialog. Offline banks only get a new name.
pub(crate) fn submit_connection(
    main: &MainWindow,
    state: &Arc<ShellState>,
    bank_id: i32,
    name: &str,
    blz: &str,
    user_id: &str,
    server_url: &str,
) {
    if refuse_if_syncing(main, state) {
        return;
    }
    let id = BankConnectionId(i64::from(bank_id));
    let offline = state
        .accounts
        .lock()
        .expect("accounts state poisoned")
        .bank_accesses
        .iter()
        .find(|bank| bank.id == id)
        .is_some_and(|bank| bank.offline);
    {
        let banking = state.banking.lock().expect("banking state poisoned");
        let error = if offline {
            (name.trim().is_empty()).then(|| i18n::text(banking.language, Key::BankNameRequired).to_owned())
        } else {
            banking
                .validate_bank_access(name, blz)
                .or_else(|| banking.validate_server_url(server_url))
        };
        if let Some(message) = error {
            main.set_accounts_dialog_error(message.into());
            return;
        }
    }
    accounts::close_dialog(main);
    let (name, blz, user_id, server_url) = (
        name.to_owned(),
        blz.to_owned(),
        user_id.to_owned(),
        server_url.to_owned(),
    );
    spawn_action(main, state, move |banking, db, service, _busy| {
        let _ = banking.update_connection(db, service, id, &name, &blz, &user_id, &server_url);
    });
}

/// Forgets the remembered PIN of an online bank.
pub(crate) fn forget_pin(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>, bank_id: i32) {
    if refuse_if_syncing(main, state) {
        return;
    }
    let id = BankConnectionId(i64::from(bank_id));
    spawn_action(main, state, move |banking, db, service, _busy| {
        let _ = banking.forget_pin(db, service, id);
    });
}

/// Sets whether a bank takes part in "Sync all".
pub(crate) fn set_include_in_sync_all(
    main: &MainWindow,
    state: &Arc<ShellState>,
    _paths: &Arc<AppPaths>,
    bank_id: i32,
) {
    if refuse_if_syncing(main, state) {
        return;
    }
    let id = BankConnectionId(i64::from(bank_id));
    let include = {
        let banking = state.banking.lock().expect("banking state poisoned");
        banking
            .banks
            .iter()
            .find(|bank| bank.id == id)
            .is_some_and(|bank| !bank.include_in_sync_all)
    };
    spawn_action(main, state, move |banking, db, service, _busy| {
        let _ = banking.set_include_in_sync_all(db, service, id, include);
    });
}

/// Drops the stored history start of a bank.
pub(crate) fn clear_history(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>, bank_id: i32) {
    if refuse_if_syncing(main, state) {
        return;
    }
    accounts::close_dialog(main);
    let id = BankConnectionId(i64::from(bank_id));
    spawn_action(main, state, move |banking, db, service, _busy| {
        let _ = banking.clear_sync_from(db, service, id);
    });
}

/// Validates the date, closes the dialog and starts a history sync.
pub(crate) fn submit_history(
    main: &MainWindow,
    state: &Arc<ShellState>,
    _paths: &Arc<AppPaths>,
    bank_id: i32,
    input: &str,
) {
    if refuse_if_syncing(main, state) {
        return;
    }
    let id = BankConnectionId(i64::from(bank_id));
    let today = time::OffsetDateTime::now_utc().date();
    let Some(date) = format::parse_date(input) else {
        let language = *state.language.lock().expect("language state poisoned");
        main.set_accounts_dialog_error(i18n::text(language, Key::ErrorSyncFromDateInvalid).into());
        return;
    };
    {
        let banking = state.banking.lock().expect("banking state poisoned");
        if let Some(message) = banking.validate_sync_from(input, today) {
            main.set_accounts_dialog_error(message.into());
            return;
        }
    }
    let Some(work) = worker_inputs(state) else {
        return;
    };
    accounts::close_dialog(main);
    let mut banking = state.banking.lock().expect("banking state poisoned");
    if !banking.begin_history_from(id, date, today) {
        let snapshot = banking.clone();
        drop(banking);
        publish(main, state, &snapshot);
        return;
    }
    let snapshot = banking.clone();
    drop(banking);
    publish(main, state, &snapshot);

    let WorkerInputs {
        db,
        inputs,
        busy,
        bridge,
        accounts: account_snapshot,
    } = work;
    let pending = snapshot;
    let generation = state.jobs.begin();
    let main_weak = main.as_weak();
    let state = state.clone();
    thread::spawn(move || {
        let service = inputs.service();
        let mut banking = pending;
        banking.run_history_from(&db, &service, &bridge, &busy, id, date, today);
        let (banking, accounts) = reload_after_sync(banking, &db, account_snapshot);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                publish_both(&ui, &state, &banking, &accounts);
            }
        });
    });
}

/// Starts one bank's sync on a worker. The claim happens here, on the UI thread.
pub(crate) fn start_sync(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>, bank_id: i32) {
    let id = BankConnectionId(i64::from(bank_id));
    let Some(work) = worker_inputs(state) else {
        return;
    };
    let mut banking = state.banking.lock().expect("banking state poisoned");
    if !banking.begin_sync(id) {
        let snapshot = banking.clone();
        drop(banking);
        publish(main, state, &snapshot);
        return;
    }
    let pending = banking.clone();
    drop(banking);
    publish(main, state, &pending);

    let WorkerInputs {
        db,
        inputs,
        busy,
        bridge,
        accounts: account_snapshot,
    } = work;
    let generation = state.jobs.begin();
    let today = time::OffsetDateTime::now_utc().date();
    let main_weak = main.as_weak();
    let state = state.clone();
    thread::spawn(move || {
        let service = inputs.service();
        let mut banking = pending;
        banking.run_sync(&db, &service, &bridge, &busy, id, today);
        let (banking, accounts) = reload_after_sync(banking, &db, account_snapshot);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                publish_both(&ui, &state, &banking, &accounts);
            }
        });
    });
}

/// Starts "Sync all" on a worker.
pub(crate) fn start_sync_all(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>) {
    let Some(work) = worker_inputs(state) else {
        return;
    };
    let mut banking = state.banking.lock().expect("banking state poisoned");
    if !banking.begin_sync_all() {
        let snapshot = banking.clone();
        drop(banking);
        publish(main, state, &snapshot);
        return;
    }
    let pending = banking.clone();
    drop(banking);
    publish(main, state, &pending);

    let WorkerInputs {
        db,
        inputs,
        busy,
        bridge,
        accounts: account_snapshot,
    } = work;
    let generation = state.jobs.begin();
    let today = time::OffsetDateTime::now_utc().date();
    let main_weak = main.as_weak();
    let state = state.clone();

    thread::spawn(move || {
        let service = inputs.service();
        let mut banking = pending;
        let progress_state = state.clone();
        let progress_main = main_weak.clone();
        banking.run_sync_all(&db, &service, &bridge, &busy, today, &|current| {
            let progress = current.sync_all.clone();
            let syncing = current.syncing_bank;
            let step = current.sync_step.clone();
            let progress_state = progress_state.clone();
            let progress_main = progress_main.clone();
            let _ = slint::invoke_from_event_loop(move || {
                if !progress_state.jobs.is_current(generation) {
                    return;
                }
                let snapshot = {
                    let mut live = progress_state.banking.lock().expect("banking state poisoned");
                    live.sync_all = progress;
                    live.syncing_bank = syncing;
                    live.sync_step = step;
                    live.clone()
                };
                if let Some(ui) = progress_main.upgrade() {
                    publish(&ui, &progress_state, &snapshot);
                }
            });
        });
        let (banking, accounts) = reload_after_sync(banking, &db, account_snapshot);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                publish_both(&ui, &state, &banking, &accounts);
            }
        });
    });
}

/// Handles one entry of a bank card's menu.
pub(crate) fn handle_bank_menu(
    main: &MainWindow,
    state: &Arc<ShellState>,
    paths: &Arc<AppPaths>,
    bank_id: i32,
    action: &str,
) {
    match action {
        "sync" => start_sync(main, state, paths, bank_id),
        "history" => open_history_dialog(main, state, bank_id),
        "clear-history" => clear_history(main, state, paths, bank_id),
        "include" => set_include_in_sync_all(main, state, paths, bank_id),
        "connection" => open_connection_dialog(main, state, bank_id),
        "forget-pin" => forget_pin(main, state, paths, bank_id),
        "delete" => accounts::open_delete_bank(main, state, bank_id),
        _ => {}
    }
}

/// Opens the connection-settings dialog. An offline bank only gets a name field.
pub(crate) fn open_connection_dialog(main: &MainWindow, state: &ShellState, bank_id: i32) {
    if refuse_if_syncing(main, state) {
        return;
    }
    let id = BankConnectionId(i64::from(bank_id));
    let language = *state.language.lock().expect("language state poisoned");
    let bank = state
        .banking
        .lock()
        .expect("banking state poisoned")
        .banks
        .iter()
        .find(|bank| bank.id == id)
        .cloned();
    let Some(bank) = bank else {
        return;
    };
    let pin_text = if bank.offline {
        String::new()
    } else if bank.has_saved_pin {
        i18n::text(language, Key::PinSavedForSyncs).to_owned()
    } else if state.banking.lock().expect("banking state poisoned").can_remember_pin {
        i18n::text(language, Key::PinAskedEverySync).to_owned()
    } else {
        i18n::text(language, Key::PinNeverStored).to_owned()
    };
    main.set_accounts_dialog_title(if bank.offline {
        i18n::text(language, Key::RenameConnection).into()
    } else {
        i18n::format(language, Key::OnlineBankingFor, &[Arg::Str(bank.bank_name.clone())]).into()
    });
    main.set_accounts_dialog_account_name(bank.bank_name.clone().into());
    main.set_accounts_connection_name(bank.bank_name.clone().into());
    main.set_accounts_dialog_bank_id(bank_id);
    main.set_accounts_dialog_offline(bank.offline);
    main.set_accounts_connection_blz(bank.blz.into());
    main.set_accounts_connection_user_id(bank.user_id.into());
    main.set_accounts_connection_server_url(bank.server_url.unwrap_or_default().into());
    main.set_accounts_connection_has_pin(bank.has_saved_pin);
    main.set_accounts_connection_pin_text(pin_text.into());
    main.set_accounts_connection_syncing(
        state.banking.lock().expect("banking state poisoned").syncing_bank == Some(id),
    );
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("connection".into());
}

/// Opens the history-from dialog with the stored date or FinDesk's 90-day default.
pub(crate) fn open_history_dialog(main: &MainWindow, state: &ShellState, bank_id: i32) {
    if refuse_if_syncing(main, state) {
        return;
    }
    let id = BankConnectionId(i64::from(bank_id));
    let language = *state.language.lock().expect("language state poisoned");
    let Some(bank) = state
        .banking
        .lock()
        .expect("banking state poisoned")
        .banks
        .iter()
        .find(|bank| bank.id == id)
        .cloned()
    else {
        return;
    };
    let start = bank
        .sync_from
        .unwrap_or_else(|| time::OffsetDateTime::now_utc().date() - time::Duration::days(90));
    main.set_accounts_dialog_title(i18n::format(language, Key::FetchHistoryFor, &[Arg::Str(bank.bank_name)]).into());
    main.set_accounts_dialog_bank_id(bank_id);
    main.set_accounts_history_date(format::date(start).into());
    main.set_accounts_history_has_start(bank.sync_from.is_some());
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("history".into());
}

/// Deletes an online bank with its remembered PIN and registration.
pub(crate) fn delete_bank(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>, bank_id: i32) {
    if refuse_if_syncing(main, state) {
        return;
    }
    let id = BankConnectionId(i64::from(bank_id));
    spawn_action(main, state, move |banking, db, service, busy| {
        let _ = banking.remove_bank(db, service, busy, id);
    });
}

fn spawn_action(
    main: &MainWindow,
    state: &Arc<ShellState>,
    work: impl FnOnce(&mut BankingState, &Database, &BankingService, &Busy) + Send + 'static,
) {
    let Some(db) = current_db(state) else {
        return;
    };
    let Some(inputs) = service_inputs(state, *state.language.lock().expect("language state poisoned")) else {
        return;
    };
    let busy = state.profile_busy.lock().expect("profile busy poisoned").clone();
    let pending = state_snapshot(state);
    let account_snapshot = account_snapshot(state);
    let generation = state.jobs.begin();
    let main_weak = main.as_weak();
    let state = state.clone();
    thread::spawn(move || {
        let service = inputs.service();
        let mut banking = pending;
        work(&mut banking, &db, &service, &busy);
        let (mut banking, accounts) = reload_after_sync(banking, &db, account_snapshot);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            keep_live_sync_fields(&mut banking, &state);
            if let Some(ui) = main_weak.upgrade() {
                publish_both(&ui, &state, &banking, &accounts);
            }
        });
    });
}

/// An action's result was computed from a snapshot taken before it ran. A sync may have been claimed since (actions
/// are only refused while a sync already runs), so the live claim and progress win over the snapshot's.
pub(crate) fn keep_live_sync_fields(result: &mut BankingState, state: &ShellState) {
    let live = state.banking.lock().expect("banking state poisoned");
    result.syncing_bank = live.syncing_bank;
    result.sync_all.clone_from(&live.sync_all);
    result.sync_step.clone_from(&live.sync_step);
}

/// Everything a sync worker needs, gathered before the claim so a missing piece never leaves a sync claimed.
struct WorkerInputs {
    db: Arc<Database>,
    inputs: ServiceInputs,
    busy: Busy,
    bridge: Arc<PromptBridge>,
    accounts: AccountsState,
}

fn worker_inputs(state: &ShellState) -> Option<WorkerInputs> {
    let language = *state.language.lock().expect("language state poisoned");
    Some(WorkerInputs {
        db: current_db(state)?,
        inputs: service_inputs(state, language)?,
        busy: state.profile_busy.lock().expect("profile busy poisoned").clone(),
        bridge: current_bridge(state)?,
        accounts: account_snapshot(state),
    })
}

fn account_snapshot(state: &ShellState) -> AccountsState {
    state.accounts.lock().expect("accounts state poisoned").clone()
}

fn current_db(state: &ShellState) -> Option<Arc<Database>> {
    state.db.lock().expect("database state poisoned").clone()
}

/// Loads the banking state for the Accounts page on a worker.
pub(crate) fn load(
    db: &Database,
    service: &BankingService,
    mut banking: BankingState,
    mut accounts: AccountsState,
) -> (BankingState, AccountsState) {
    let _ = banking.refresh(db, service);
    accounts.language = banking.language;
    let _ = accounts.load_accounts(db);
    (banking, accounts)
}

/// Reloads the account rows after a sync or banking action, keeping the banking state as it ended.
pub(crate) fn reload_after_sync(
    banking: BankingState,
    db: &Database,
    mut accounts: AccountsState,
) -> (BankingState, AccountsState) {
    accounts.language = banking.language;
    let _ = accounts.load_accounts(db);
    (banking, accounts)
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicBool, Ordering};
    use std::sync::mpsc;
    use std::time::{Duration as StdDuration, Instant};

    use finglass_core::app::banking::{BankItem, SyncAllProgress};
    use finglass_core::db::Database;
    use finglass_core::model::{BankAccessItem, BankConnectionId, BankConnectionUpdate, NewBankConnection};
    use finglass_core::repo;
    use finglass_core::secret::{Passphrase, PassphrasePurpose};
    use finglass_core::settings;
    use finglass_core::sync::client::{login_signature, pin_flag_key};
    use finglass_core::sync::credentials::{MemoryCredentialStore, pin_key};
    use finglass_core::sync::{
        AccountSnapshot, BankingInteraction, BankingService, FetchOutcome, FinTsClient, FinTsConnection, FinTsGateway,
        GatewayError, TanChallenge, TanMethod,
    };
    use slint::Model;
    use time::macros::date;

    use super::*;

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn data_dir(temp: &tempfile::TempDir) -> PathBuf {
        temp.path().to_path_buf()
    }

    fn online_bank(db: &Database, name: &str) -> BankConnectionId {
        let bank = repo::bank_connections::create(
            db,
            NewBankConnection {
                bank_name: name.to_owned(),
                blz: "37040044".to_owned(),
                bic: Some("COLSDE33XXX".to_owned()),
                user_id: "user".to_owned(),
            },
        )
        .unwrap()
        .id;
        repo::bank_connections::update_connection(
            db,
            bank,
            BankConnectionUpdate {
                server_url: Some("https://fints.example/pintan".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap();
        bank
    }

    fn store_service(
        gateway: Arc<dyn FinTsGateway>,
        dir: &std::path::Path,
        store: Arc<MemoryCredentialStore>,
    ) -> BankingService {
        BankingService::new(FinTsClient::new(gateway, dir.to_path_buf(), "TEST", store).with_language(Language::En))
    }

    fn item(bank_id: BankConnectionId, offline: bool, include_in_sync_all: bool) -> BankItem {
        BankItem {
            id: bank_id,
            bank_name: "Sparkasse".to_owned(),
            blz: if offline { String::new() } else { "37040044".to_owned() },
            user_id: if offline { String::new() } else { "user".to_owned() },
            server_url: (!offline).then(|| "https://fints.example".to_owned()),
            offline,
            include_in_sync_all,
            sync_from: None,
            last_sync: None,
            has_saved_pin: false,
        }
    }

    fn banking_state(banks: Vec<BankItem>) -> BankingState {
        let mut banking = BankingState::new(Language::En);
        banking.can_remember_pin = true;
        banking.banks = banks;
        banking
    }

    fn accounts_state(banks: Vec<BankAccessItem>) -> AccountsState {
        let mut accounts = AccountsState::new(Language::En);
        accounts.bank_accesses = banks;
        accounts
    }

    fn bank_access(id: BankConnectionId, offline: bool) -> BankAccessItem {
        BankAccessItem {
            id,
            bank_name: "Sparkasse".to_owned(),
            blz: if offline { String::new() } else { "37040044".to_owned() },
            bic: None,
            user_id: if offline { String::new() } else { "user".to_owned() },
            tan_method: None,
            server_url: (!offline).then(|| "https://fints.example".to_owned()),
            last_sync: None,
            capabilities: None,
            sync_from: None,
            include_in_sync_all: true,
            offline,
        }
    }

    fn wait_until(mut condition: impl FnMut() -> bool) {
        let deadline = Instant::now() + StdDuration::from_secs(2);
        while !condition() {
            assert!(Instant::now() < deadline, "condition did not become true in time");
            std::thread::sleep(StdDuration::from_millis(2));
        }
    }

    #[test]
    fn bank_cards_show_sync_state_and_the_menu_of_each_bank_kind() {
        let online = BankConnectionId(1);
        let offline = BankConnectionId(2);
        let accounts = accounts_state(vec![bank_access(online, false), bank_access(offline, true)]);
        let mut banking = banking_state(vec![item(online, false, true), item(offline, true, false)]);
        banking.syncing_bank = Some(online);
        banking.sync_step = Some("Waiting for the bank".to_owned());

        let cards = bank_cards(&accounts, &banking, Language::En);
        assert!(cards[0].syncing);
        assert_eq!(cards[0].sync_step, "Waiting for the bank");
        assert!(!cards[0].sync_enabled);
        assert_eq!(cards[0].pin_text, i18n::text(Language::En, Key::PinAskedEverySync));
        assert!(!cards[1].syncing);
        assert_eq!(cards[1].pin_text, "");

        let online_actions: Vec<String> = (0..cards[0].menu.row_count())
            .filter_map(|index| cards[0].menu.row_data(index))
            .map(|item| item.action.to_string())
            .collect();
        assert_eq!(
            online_actions,
            vec!["sync", "history", "include", "connection", "delete"]
        );
        let offline_actions: Vec<String> = (0..cards[1].menu.row_count())
            .filter_map(|index| cards[1].menu.row_data(index))
            .map(|item| item.action.to_string())
            .collect();
        assert_eq!(offline_actions, vec!["connection", "delete"]);
    }

    #[test]
    fn the_menu_adds_history_clear_and_forget_when_they_apply() {
        let id = BankConnectionId(7);
        let mut bank = item(id, false, false);
        bank.sync_from = Some(date!(2026 - 01 - 01));
        bank.has_saved_pin = true;
        let banking = banking_state(vec![bank.clone()]);
        let accounts = accounts_state(vec![bank_access(id, false)]);
        let menu = bank_menu(&bank, &banking, &accounts, Language::En);
        let actions: Vec<&str> = menu.iter().map(|item| item.action.as_str()).collect();
        assert_eq!(
            actions,
            vec![
                "sync",
                "history",
                "clear-history",
                "include",
                "connection",
                "forget-pin",
                "delete"
            ]
        );
        let include = menu.iter().find(|item| item.action == "include").unwrap();
        assert!(!include.checked);
    }

    #[test]
    fn sync_all_progress_uses_the_bank_name_and_position() {
        let mut banking = banking_state(vec![item(BankConnectionId(1), false, true)]);
        banking.sync_all = Some(SyncAllProgress {
            position: 2,
            total: 3,
            bank_name: "Sparkasse".to_owned(),
        });
        assert_eq!(
            sync_all_text(&banking),
            i18n::format(
                Language::En,
                Key::SyncAllProgress,
                &[2.into(), 3.into(), Arg::Str("Sparkasse".to_owned())]
            )
        );
        assert!(sync_all_visible(&banking));
        assert!(!sync_all_enabled(&banking), "the running run is claimed");
        banking.sync_all = None;
        assert!(sync_all_enabled(&banking));
        banking.banks[0].include_in_sync_all = false;
        assert!(!sync_all_enabled(&banking));
    }

    #[test]
    fn a_pin_prompt_hides_the_remember_box_without_a_credential_store() {
        let prompt = BankingPrompt::Pin {
            bank_name: "Sparkasse".to_owned(),
            user_id: "user".to_owned(),
            offer_remember: true,
        };
        let shown = prompt_view(&prompt, Language::En, true);
        assert_eq!(shown.kind, "pin");
        assert!(shown.remember_visible);
        assert_eq!(shown.title, "PIN for Sparkasse");
        assert!(!prompt_view(&prompt, Language::En, false).remember_visible);

        let method = TanMethod {
            id: "942".to_owned(),
            name: "chipTAN optisch".to_owned(),
            medium_name: None,
            tan_format: None,
        };
        let choice = prompt_view(&BankingPrompt::ChooseTanMethod(vec![method]), Language::En, true);
        assert_eq!(choice.kind, "choice");
        assert_eq!(choice.choices[0].id, "942");
        assert_eq!(choice.choices[0].label, "chipTAN optisch");
    }

    #[test]
    fn a_tan_prompt_shows_the_challenge_and_the_chip_tan_note() {
        let prompt = BankingPrompt::Tan(TanChallenge {
            message: "Bitte TAN eingeben".to_owned(),
            image: None,
            image_mime_type: None,
            flicker_code: Some("123".to_owned()),
        });
        let view = prompt_view(&prompt, Language::En, true);
        assert_eq!(view.kind, "tan");
        assert_eq!(view.challenge, "Bitte TAN eingeben");
        assert_eq!(view.flicker_note, i18n::text(Language::En, Key::TanFlickerUnsupported));
        assert_eq!(view.label, i18n::text(Language::En, Key::TanLabel));
    }

    #[test]
    fn cancel_answers_none_and_leaves_the_bridge_usable() {
        let bridge = Arc::new(PromptBridge::new(StdDuration::from_secs(5), || {}));
        let (sender, receiver) = mpsc::channel();
        let worker = {
            let bridge = bridge.clone();
            std::thread::spawn(move || {
                let _ = sender.send(bridge.pin("Sparkasse", "user", true));
            })
        };
        wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
        cancel_prompt(&bridge, "pin");
        assert!(receiver.recv_timeout(StdDuration::from_secs(1)).unwrap().is_none());
        worker.join().unwrap();

        let (sender, receiver) = mpsc::channel();
        let worker = {
            let bridge = bridge.clone();
            std::thread::spawn(move || {
                let _ = sender.send(bridge.pin("Sparkasse", "user", true));
            })
        };
        wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
        answer_pin(&bridge, Some("12345"), false);
        let entry = receiver
            .recv_timeout(StdDuration::from_secs(1))
            .unwrap()
            .expect("the bridge still answers after a cancel");
        assert_eq!(secrecy::ExposeSecret::expose_secret(entry.pin()), "12345");
        worker.join().unwrap();
    }

    fn save_pin(store: &MemoryCredentialStore, db: &Database, dir: &std::path::Path, id: BankConnectionId) {
        let bank = repo::bank_connections::get_item(db, id).unwrap().unwrap();
        store
            .write(&pin_key(dir, id.0), &SecretString::from("12345".to_owned()))
            .unwrap();
        settings::set_setting(db, &pin_flag_key(id), &login_signature(&bank)).unwrap();
    }

    fn has_pin(store: &MemoryCredentialStore, db: &Database, dir: &std::path::Path, id: BankConnectionId) -> bool {
        let bank = repo::bank_connections::get_item(db, id);
        bank.is_ok()
            && store.contains(&pin_key(dir, id.0))
            && settings::get_setting(db, &pin_flag_key(id)).unwrap().is_some()
    }

    #[test]
    fn changing_a_login_field_forgets_the_pin_but_renaming_does_not() {
        for (blz, user, url) in [
            ("37040044", "user", "https://fints.example/pintan"),
            ("37040044", "other", "https://fints.example/pintan"),
            ("37040044", "user", "https://other.example/pintan"),
        ] {
            let (temp, db) = database();
            let dir = data_dir(&temp);
            let store = Arc::new(MemoryCredentialStore::new());
            let service = store_service(Arc::new(UnavailableGateway::new(Language::En)), &dir, store.clone());
            let id = online_bank(&db, "Sparkasse");
            save_pin(&store, &db, &dir, id);
            let mut banking = BankingState::new(Language::En);
            banking.load(&db, &service).unwrap();

            banking
                .update_connection(&db, &service, id, "Renamed", blz, user, url)
                .unwrap();
            let changed = blz != "37040044" || user != "user" || url != "https://fints.example/pintan";
            assert_eq!(
                !has_pin(&store, &db, &dir, id),
                changed,
                "blz={blz} user={user} url={url}"
            );
        }

        let (temp, db) = database();
        let dir = data_dir(&temp);
        let store = Arc::new(MemoryCredentialStore::new());
        let service = store_service(Arc::new(UnavailableGateway::new(Language::En)), &dir, store.clone());
        let id = online_bank(&db, "Sparkasse");
        save_pin(&store, &db, &dir, id);
        let mut banking = BankingState::new(Language::En);
        banking.load(&db, &service).unwrap();
        banking
            .update_connection(
                &db,
                &service,
                id,
                "Only renamed",
                "37040044",
                "user",
                "https://fints.example/pintan",
            )
            .unwrap();
        assert!(has_pin(&store, &db, &dir, id));
    }

    #[test]
    fn deleting_an_online_bank_forgets_its_pin() {
        let (temp, db) = database();
        let dir = data_dir(&temp);
        let store = Arc::new(MemoryCredentialStore::new());
        let service = store_service(Arc::new(UnavailableGateway::new(Language::En)), &dir, store.clone());
        let id = online_bank(&db, "Sparkasse");
        save_pin(&store, &db, &dir, id);
        let mut banking = BankingState::new(Language::En);
        banking.load(&db, &service).unwrap();

        banking.remove_bank(&db, &service, &Busy::default(), id).unwrap();
        assert!(!store.contains(&pin_key(&dir, id.0)));
        assert!(repo::bank_connections::get_item(&db, id).unwrap().is_none());
    }

    fn png_bytes(width: u32, height: u32) -> Vec<u8> {
        let mut bytes = Vec::new();
        {
            let mut encoder = png::Encoder::new(&mut bytes, width, height);
            encoder.set_color(png::ColorType::Rgb);
            encoder.set_depth(png::BitDepth::Eight);
            let mut writer = encoder.write_header().unwrap();
            let data = vec![0_u8; width as usize * height as usize * 3];
            writer.write_image_data(&data).unwrap();
            writer.finish().unwrap();
        }
        bytes
    }

    #[test]
    fn a_small_png_decodes_and_oversized_or_garbage_images_fall_back_to_text() {
        let image = decode_tan_image(&png_bytes(32, 32)).expect("a small PNG decodes");
        assert_eq!(image.size().width, 32);
        assert!(decode_tan_image(&png_bytes(2049, 2049)).is_none());
        assert!(decode_tan_image(b"not a png").is_none());
        assert!(decode_tan_image(&vec![0; 1_000_001]).is_none());
    }

    #[test]
    fn realistic_and_maximum_size_photo_tan_images_decode() {
        // The decoder's allocation limit must not reject what the pixel check allows.
        let image = decode_tan_image(&png_bytes(800, 800)).expect("an 800x800 PNG decodes");
        assert_eq!(image.size().width, 800);
        let image = decode_tan_image(&png_bytes(2048, 2048)).expect("a 2048x2048 PNG decodes");
        assert_eq!(image.size().height, 2048);
    }

    struct PinGateway {
        busy: Busy,
        busy_seen: AtomicBool,
    }

    impl FinTsGateway for PinGateway {
        fn fetch_all(
            &self,
            connection: &FinTsConnection,
            interaction: &dyn BankingInteraction,
            _since: &dyn Fn(&str) -> Option<time::Date>,
            _to: time::Date,
        ) -> Result<FetchOutcome, GatewayError> {
            if self.busy.active() {
                self.busy_seen.store(true, Ordering::SeqCst);
            }
            if interaction
                .pin(&connection.bank_name, &connection.user_id, true)
                .is_none()
            {
                return Err(GatewayError::Cancelled);
            }
            Ok(FetchOutcome {
                snapshots: Vec::<AccountSnapshot>::new(),
                tan_method: None,
                capabilities: None,
            })
        }
    }

    #[test]
    fn a_sync_answered_through_the_pin_dialog_function_succeeds_under_the_profile_busy() {
        let (temp, db) = database();
        let dir = data_dir(&temp);
        let store = Arc::new(MemoryCredentialStore::new());
        let busy = Busy::default();
        let gateway = Arc::new(PinGateway {
            busy: busy.clone(),
            busy_seen: AtomicBool::new(false),
        });
        let service = store_service(gateway.clone(), &dir, store);
        let id = online_bank(&db, "Sparkasse");
        let bridge = Arc::new(PromptBridge::new(StdDuration::from_secs(5), || {}));
        let mut banking = BankingState::new(Language::En);
        banking.load(&db, &service).unwrap();

        assert!(banking.begin_sync(id));
        let (sender, receiver) = mpsc::channel();
        let worker = {
            let bridge = bridge.clone();
            let thread_busy = busy.clone();
            std::thread::spawn(move || {
                banking.run_sync(&db, &service, &bridge, &thread_busy, id, date!(2026 - 11 - 07));
                let _ = sender.send(banking);
            })
        };
        wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
        assert!(busy.active(), "the profile busy is active during the gateway call");
        answer_pin(&bridge, Some("12345"), false);
        let banking = receiver.recv_timeout(StdDuration::from_secs(2)).unwrap();
        worker.join().unwrap();

        assert!(banking.error.is_none(), "{:?}", banking.error);
        let message = banking.message.expect("a summary");
        assert!(message.contains("Synced"), "{message}");
        assert!(gateway.busy_seen.load(Ordering::SeqCst));
        assert!(!busy.active());
    }
}
