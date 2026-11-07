//! Bridges blocking [`BankingInteraction`] calls from a worker thread to prompts the UI answers.
//!
//! This is the Rust port of FinDesk's `UiBankingInteraction`, minus `ConfirmPayee` (Finglass has no payment code).
//! The worker blocks inside [`PromptBridge::ask`] while the prompt is open; the UI reads a clone through
//! [`PromptBridge::prompt`] and answers. No PIN or TAN ever lives in a published prompt: secrets travel only through
//! the answer calls, and the condition variable's mutex is released while the worker waits.

use std::sync::{Arc, Condvar, Mutex};
use std::time::{Duration, Instant};

use secrecy::{ExposeSecret, SecretString};

use crate::sync::{BankingInteraction, PinEntry, TanChallenge, TanMethod};

/// A question from the bank waiting for the user. It never carries a secret.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BankingPrompt {
    /// The online-banking PIN.
    Pin {
        /// The bank's display name.
        bank_name: String,
        /// The online-banking login.
        user_id: String,
        /// Whether the UI may offer to remember the PIN.
        offer_remember: bool,
    },
    /// One of the bank's TAN procedures.
    ChooseTanMethod(Vec<TanMethod>),
    /// One of the bank's TAN media.
    ChooseTanMedium(Vec<String>),
    /// A TAN challenge.
    Tan(TanChallenge),
    /// A decoupled procedure (for example pushTAN) that must be confirmed in the bank's app.
    Decoupled(String),
}

/// The answer the worker receives. This is the only place a secret ever lives, and it is consumed immediately.
enum Answer {
    Pin(Option<PinEntry>),
    Choice(Option<String>),
    Tan(Option<SecretString>),
    Decoupled(bool),
}

struct Inner {
    prompt: Option<BankingPrompt>,
    info: Option<String>,
    answer: Option<Answer>,
    closed: bool,
}

/// Turns blocking banking callbacks into a prompt the UI can read and answer.
///
/// `on_change` is called whenever the prompt opens or closes or `info` changes, always without holding the bridge's
/// lock, so a callback may call [`Self::prompt`] or [`Self::info`] again.
pub struct PromptBridge {
    inner: Mutex<Inner>,
    changed: Condvar,
    timeout: Duration,
    on_change: Arc<dyn Fn() + Send + Sync + 'static>,
}

impl PromptBridge {
    /// Builds a bridge. A waiting prompt times out after `timeout` and counts as cancelled.
    pub fn new(timeout: Duration, on_change: impl Fn() + Send + Sync + 'static) -> Self {
        Self {
            inner: Mutex::new(Inner {
                prompt: None,
                info: None,
                answer: None,
                closed: false,
            }),
            changed: Condvar::new(),
            timeout,
            on_change: Arc::new(on_change),
        }
    }

    /// The open prompt, if any.
    pub fn prompt(&self) -> Option<BankingPrompt> {
        self.inner.lock().expect("prompt bridge lock").prompt.clone()
    }

    /// The bank's current informational message, if any.
    pub fn info(&self) -> Option<String> {
        self.inner.lock().expect("prompt bridge lock").info.clone()
    }

    /// Clears the bank's informational message.
    pub fn clear_info(&self) {
        let changed = {
            let mut inner = self.inner.lock().expect("prompt bridge lock");
            inner.info.take().is_some()
        };
        if changed {
            (self.on_change)();
        }
    }

    /// Answers the open PIN prompt. `None` or an empty PIN cancels.
    pub fn answer_pin(&self, pin: Option<SecretString>, remember: bool) {
        let answer = match pin {
            Some(pin) if !pin.expose_secret().is_empty() => Answer::Pin(Some(PinEntry::new(pin, remember))),
            _ => Answer::Pin(None),
        };
        self.answer_if(|prompt| matches!(prompt, BankingPrompt::Pin { .. }), answer);
    }

    /// Answers a TAN-procedure or TAN-medium prompt. `None` cancels.
    pub fn answer_choice(&self, choice: Option<String>) {
        self.answer_if(
            |prompt| {
                matches!(
                    prompt,
                    BankingPrompt::ChooseTanMethod(_) | BankingPrompt::ChooseTanMedium(_)
                )
            },
            Answer::Choice(choice),
        );
    }

    /// Answers a TAN prompt. `None` cancels.
    pub fn answer_tan(&self, tan: Option<SecretString>) {
        self.answer_if(|prompt| matches!(prompt, BankingPrompt::Tan(_)), Answer::Tan(tan));
    }

    /// Answers a decoupled-confirmation prompt.
    pub fn answer_decoupled(&self, confirmed: bool) {
        self.answer_if(
            |prompt| matches!(prompt, BankingPrompt::Decoupled(_)),
            Answer::Decoupled(confirmed),
        );
    }

    /// Cancels the open prompt and every later one. The worker is unblocked at once and every new `ask` returns
    /// `None`; the bridge is closed for good. The shell calls this on a profile switch or when the window closes.
    pub fn cancel_pending(&self) {
        let cleared = {
            let mut inner = self.inner.lock().expect("prompt bridge lock");
            inner.closed = true;
            inner.answer = None;
            let cleared = inner.prompt.take().is_some();
            self.changed.notify_all();
            cleared
        };
        if cleared {
            (self.on_change)();
        }
    }

    /// Sets an answer only when the open prompt is of the expected kind; otherwise it is ignored.
    fn answer_if(&self, matches_prompt: impl Fn(&BankingPrompt) -> bool, answer: Answer) {
        let mut inner = self.inner.lock().expect("prompt bridge lock");
        if inner.closed {
            return;
        }
        let Some(prompt) = inner.prompt.as_ref() else {
            return;
        };
        if !matches_prompt(prompt) {
            return;
        }
        inner.answer = Some(answer);
        self.changed.notify_all();
    }

    /// Publishes `prompt`, waits for an answer or a timeout, then closes the prompt. Only one prompt is open at a
    /// time; a concurrent `ask` is refused instead of replacing the open one.
    fn ask(&self, prompt: BankingPrompt) -> Option<Answer> {
        {
            let mut inner = self.inner.lock().expect("prompt bridge lock");
            if inner.closed || inner.prompt.is_some() {
                return None;
            }
            inner.prompt = Some(prompt);
            inner.answer = None;
        }
        (self.on_change)();

        let deadline = Instant::now() + self.timeout;
        let mut inner = self.inner.lock().expect("prompt bridge lock");
        loop {
            if inner.closed {
                let was_open = inner.prompt.take().is_some();
                inner.answer = None;
                drop(inner);
                if was_open {
                    (self.on_change)();
                }
                return None;
            }
            if let Some(answer) = inner.answer.take() {
                inner.prompt = None;
                drop(inner);
                (self.on_change)();
                return Some(answer);
            }

            let now = Instant::now();
            if now >= deadline {
                inner.prompt = None;
                drop(inner);
                (self.on_change)();
                return None;
            }
            let remaining = deadline.saturating_duration_since(now);
            let (guard, _) = self.changed.wait_timeout(inner, remaining).expect("prompt bridge lock");
            inner = guard;
        }
    }
}

impl BankingInteraction for PromptBridge {
    fn pin(&self, bank_name: &str, user_id: &str, offer_remember: bool) -> Option<PinEntry> {
        match self.ask(BankingPrompt::Pin {
            bank_name: bank_name.to_owned(),
            user_id: user_id.to_owned(),
            offer_remember,
        }) {
            Some(Answer::Pin(entry)) => entry,
            _ => None,
        }
    }

    fn select_tan_method(&self, options: &[TanMethod]) -> Option<String> {
        match self.ask(BankingPrompt::ChooseTanMethod(options.to_vec())) {
            Some(Answer::Choice(choice)) => choice,
            _ => None,
        }
    }

    fn select_tan_medium(&self, options: &[String]) -> Option<String> {
        match self.ask(BankingPrompt::ChooseTanMedium(options.to_vec())) {
            Some(Answer::Choice(choice)) => choice,
            _ => None,
        }
    }

    fn tan(&self, challenge: &TanChallenge) -> Option<SecretString> {
        match self.ask(BankingPrompt::Tan(challenge.clone())) {
            Some(Answer::Tan(tan)) => tan,
            _ => None,
        }
    }

    fn confirm_decoupled(&self, message: &str) -> bool {
        match self.ask(BankingPrompt::Decoupled(message.to_owned())) {
            Some(Answer::Decoupled(confirmed)) => confirmed,
            _ => false,
        }
    }

    fn info(&self, message: &str) {
        if message.trim().is_empty() {
            return;
        }
        {
            let mut inner = self.inner.lock().expect("prompt bridge lock");
            inner.info = Some(message.to_owned());
        }
        (self.on_change)();
    }
}
