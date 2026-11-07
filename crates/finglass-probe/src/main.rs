use std::time::{Duration, Instant};

use finglass_probe::{
    AccountSummary, Decision, Inputs, NoGoReason, StatementState, decide, is_unsupported_tan_method, parse_inputs,
    summary_line,
};
use fints::banks::{BankConfig, bank_by_blz};
use fints::error::FinTSError;
use fints::protocol::{Account, BalanceResult, Dialog, PollResult};
use fints::types::{Pin, ProductId, TransactionStatus, UserId};
use fints::workflow::{AnyBank, InitiateOutcome, bank_ops_with_config};
use fints::{FinTSUrl, Open, TanChallenge, TanPending};
use rpassword::prompt_password;
use secrecy::{ExposeSecret, SecretString};

const TAN_TIMEOUT: Duration = Duration::from_secs(5 * 60);
const TAN_PROMPT: &str = "TAN: ";

enum TanOutcome {
    Open(Box<Dialog<Open>>),
    Failure(NoGoReason),
}

#[tokio::main]
async fn main() {
    let inputs = match parse_inputs(|name| std::env::var(name).ok()) {
        Ok(inputs) => inputs,
        Err(errors) => {
            for error in errors {
                println!("{error}");
            }
            std::process::exit(2);
        }
    };

    let pin = match prompt_password("PIN: ") {
        Ok(pin) => SecretString::from(pin),
        Err(_) => {
            println!("PIN: cannot be read from the terminal");
            std::process::exit(2);
        }
    };

    let exit_code = run(inputs, pin).await;
    std::process::exit(exit_code);
}

fn make_bank(inputs: &Inputs) -> AnyBank {
    let mut config = bank_by_blz(&inputs.blz)
        .unwrap_or_else(|| BankConfig::new("custom", inputs.blz.as_str(), "", inputs.url.as_str()));
    config.url = FinTSUrl::new(inputs.url.as_str());
    bank_ops_with_config(config)
}

async fn run(inputs: Inputs, pin: SecretString) -> i32 {
    let bank = make_bank(&inputs);
    let user = UserId::new(inputs.user.as_str());
    let product = ProductId::new(inputs.product_id.as_str());
    // The PIN is exposed exactly once, at the call that hands it to the crate.
    let pin = Pin::new(pin.expose_secret());

    let mut summaries = Vec::new();
    let mut failure = None;

    let outcome = bank.initiate(&user, &pin, &product, None, None, None).await;

    let (accounts, open_dialog) = match outcome {
        Ok(InitiateOutcome::Authenticated(result)) => (result.params.accounts_from_upd, Some(result.dialog)),
        Ok(InitiateOutcome::NeedTan(result)) => {
            let accounts = result.params.accounts_from_upd;
            let method_name = result
                .tan_methods
                .iter()
                .find(|method| method.security_function == result.params.selected_security_function)
                .map(|method| method.name.clone());

            let tan_outcome = if result.challenge.decoupled {
                poll_decoupled(result.dialog, &result.challenge).await
            } else if method_name.as_deref().is_some_and(is_unsupported_tan_method) {
                let name = method_name.as_deref().unwrap_or("unknown");
                println!("TAN method {name} is not supported by the probe; choose pushTAN in your bank settings");
                let _ = result.dialog.cancel().await;
                TanOutcome::Failure(NoGoReason::TanMethodUnsupported)
            } else {
                submit_typed_tan(result.dialog, &result.challenge).await
            };

            match tan_outcome {
                TanOutcome::Open(dialog) => (accounts, Some(*dialog)),
                TanOutcome::Failure(reason) => {
                    failure = Some(reason);
                    (accounts, None)
                }
            }
        }
        Err(error) => {
            failure = Some(classify_error(&error));
            (Vec::new(), None)
        }
    };

    if let Some(mut dialog) = open_dialog {
        for account_info in accounts {
            let bic = if account_info.bic.as_str().is_empty() {
                bank.config().bic.as_str()
            } else {
                account_info.bic.as_str()
            };
            let account_currency = account_info
                .currency
                .as_ref()
                .map(ToString::to_string)
                .unwrap_or_else(|| "-".to_string());

            let account = match Account::new(account_info.iban.as_str(), bic) {
                Ok(account) => account,
                Err(_) => {
                    if failure.is_none() {
                        failure = Some(NoGoReason::Other("MissingBic"));
                    }
                    summaries.push(AccountSummary::new(
                        account_info.iban.as_str(),
                        account_currency,
                        false,
                        StatementState::Unavailable,
                    ));
                    continue;
                }
            };

            let mut balance_present = false;
            let mut balance_currency = None;

            match dialog.balance(&account).await {
                Ok(BalanceResult::Success(balance)) => {
                    balance_present = true;
                    balance_currency = Some(balance.currency.to_string());
                }
                Ok(_) => {}
                Err(error) => {
                    if failure.is_none() {
                        failure = Some(classify_error(&error));
                    }
                }
            }

            let currency = balance_currency.unwrap_or(account_currency);

            // Stop after a fatal login/TAN problem instead of hammering the bank.
            if matches!(failure, Some(NoGoReason::LoginRejected | NoGoReason::TanTimeout)) {
                summaries.push(AccountSummary::new(
                    account_info.iban.as_str(),
                    currency,
                    balance_present,
                    StatementState::Unavailable,
                ));
                break;
            }

            // Statements only: the balance was fetched above, holdings are not needed.
            let opts = fints::workflow::FetchOpts {
                balance: false,
                transactions: true,
                holdings: false,
                days: inputs.days,
            };

            let statement = match bank.fetch_with_opts(&mut dialog, &account, &opts).await {
                Ok(fetch) => {
                    let dates: Vec<String> = fetch
                        .transactions
                        .iter()
                        .filter(|transaction| transaction.status == TransactionStatus::Booked)
                        .map(|transaction| transaction.date.to_string())
                        .collect();
                    let booking_count = dates.len();
                    let earliest = dates.iter().min().cloned();
                    let latest = dates.iter().max().cloned();
                    StatementState::Available {
                        booking_count,
                        earliest,
                        latest,
                    }
                }
                Err(error) => {
                    let reason = classify_statement_error(&error);
                    if failure.is_none() {
                        failure = Some(reason);
                    }
                    StatementState::Unavailable
                }
            };

            summaries.push(AccountSummary::new(
                account_info.iban.as_str(),
                currency,
                balance_present,
                statement,
            ));
        }

        let _ = dialog.end().await;
    }

    for summary in &summaries {
        println!("{}", summary_line(summary));
    }

    let decision = decide(&summaries, failure);
    match decision {
        Decision::Go => println!("RESULT: GO"),
        Decision::NoGo(reason) => println!("RESULT: NO-GO ({})", reason.label()),
    }

    decision.exit_code()
}

async fn poll_decoupled(dialog: Dialog<TanPending>, challenge: &TanChallenge) -> TanOutcome {
    println!("Confirm in your banking app …");

    let (first_wait, next_wait, _max_polls) = dialog.bank_params().decoupled_params();
    let start = Instant::now();
    let mut dialog = dialog;
    let mut polls = 0u32;

    loop {
        let wait = if polls == 0 { first_wait } else { next_wait };
        let remaining = TAN_TIMEOUT.saturating_sub(start.elapsed());
        if remaining.is_zero() {
            let _ = dialog.cancel().await;
            return TanOutcome::Failure(NoGoReason::TanTimeout);
        }

        tokio::time::sleep(Duration::from_secs(wait).min(remaining)).await;

        if start.elapsed() >= TAN_TIMEOUT {
            let _ = dialog.cancel().await;
            return TanOutcome::Failure(NoGoReason::TanTimeout);
        }

        polls += 1;
        match dialog.poll(&challenge.task_reference).await {
            Ok(PollResult::Confirmed(open, _response)) => return TanOutcome::Open(Box::new(open)),
            Ok(PollResult::Pending(pending)) => dialog = pending,
            Err(error) => return TanOutcome::Failure(classify_error(&error)),
        }
    }
}

async fn submit_typed_tan(dialog: Dialog<TanPending>, challenge: &TanChallenge) -> TanOutcome {
    println!("{}", challenge.challenge);

    let tan = match prompt_password(TAN_PROMPT) {
        Ok(tan) => SecretString::from(tan),
        Err(_) => {
            let _ = dialog.cancel().await;
            return TanOutcome::Failure(NoGoReason::Other("PromptFailed"));
        }
    };

    match dialog.submit_tan(&challenge.task_reference, tan.expose_secret()).await {
        Ok((open, _response)) => TanOutcome::Open(Box::new(open)),
        Err(error) => TanOutcome::Failure(classify_error(&error)),
    }
}

fn classify_error(error: &FinTSError) -> NoGoReason {
    if is_login_rejection(error) {
        NoGoReason::LoginRejected
    } else if matches!(error, FinTSError::TanTimeout) {
        NoGoReason::TanTimeout
    } else {
        NoGoReason::Other(error_kind_name(error))
    }
}

fn classify_statement_error(error: &FinTSError) -> NoGoReason {
    let reason = classify_error(error);
    match reason {
        NoGoReason::LoginRejected | NoGoReason::TanTimeout => reason,
        _ => NoGoReason::StatementsUnavailable,
    }
}

fn is_login_rejection(error: &FinTSError) -> bool {
    match error {
        FinTSError::PinWrong | FinTSError::AccountLocked | FinTSError::Auth(_) => true,
        FinTSError::BankError { kind, .. } => matches!(
            kind,
            fints::ResponseCodeKind::PinWrong
                | fints::ResponseCodeKind::AccountLocked
                | fints::ResponseCodeKind::AuthenticationMissing
        ),
        _ => false,
    }
}

/// The enum variant name only: bank messages can contain account numbers.
fn error_kind_name(error: &FinTSError) -> &'static str {
    match error {
        FinTSError::Parse(_) => "Parse",
        FinTSError::Serialize(_) => "Serialize",
        FinTSError::Transport(_) => "Transport",
        FinTSError::Http { .. } => "Http",
        FinTSError::Dialog(_) => "Dialog",
        FinTSError::Auth(_) => "Auth",
        FinTSError::TanRequired => "TanRequired",
        FinTSError::TanRejected(_) => "TanRejected",
        FinTSError::TanTimeout => "TanTimeout",
        FinTSError::BankError { .. } => "BankError",
        FinTSError::SegmentNotSupported(_) => "SegmentNotSupported",
        FinTSError::InvalidResponse(_) => "InvalidResponse",
        FinTSError::Mt940(_) => "Mt940",
        FinTSError::PinWrong => "PinWrong",
        FinTSError::AccountLocked => "AccountLocked",
        FinTSError::Io(_) => "Io",
        FinTSError::Reqwest(_) => "Reqwest",
    }
}
