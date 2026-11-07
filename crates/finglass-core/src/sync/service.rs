//! Bank-connection lifecycle and sync, ported from FinDesk's `BankingService.kt` minus every order method.

use std::fmt::Write as _;

use time::{Date, Duration, OffsetDateTime};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::i18n::{self, Arg, Key, Language};
use crate::import::amount_to_money;
use crate::import::dedup::{self, Candidate};
use crate::import::hash::import_hash;
use crate::model::{
    AccountId, AccountType, BankAccessItem, BankConnection, BankConnectionId, BankConnectionUpdate, NewAccount,
    NewBankConnection, NewPendingBooking, NewTransaction, TransactionFilter,
};
use crate::money::{Currency, Money};
use crate::repo::{self, RepoError};
use crate::rules;

use super::client::FinTsClient;
use super::config::FinTsConfig;
use super::failures::{BankResult, BankingError};
use super::gateway::{AccountSnapshot, FinTsAccount, FinTsTransaction};
use super::interaction::BankingInteraction;
use super::redactor;

/// First sync goes back this far (many banks require a TAN for more than 90 days).
pub const INITIAL_SYNC_DAYS: i64 = 90;

/// Later syncs overlap the previous one to catch late bookings; duplicates are skipped.
pub const SYNC_OVERLAP_DAYS: i64 = 10;

/// What one account's bank bookings did.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ImportResult {
    /// Rows inserted.
    pub imported: usize,
    /// Exact duplicates, probable duplicates and remembered deletions.
    pub duplicates: usize,
    /// Inserted rows a rule categorized.
    pub categorized: usize,
    /// Bookings skipped because their amount has too many decimal places for the account's currency.
    pub invalid_amount: usize,
}

/// What a sync did.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SyncReport {
    /// Accounts the bank listed with an IBAN.
    pub accounts_synced: usize,
    /// Accounts created locally.
    pub accounts_added: usize,
    /// Bookings inserted.
    pub imported: usize,
    /// Bookings already known (including remembered deletions).
    pub duplicates: usize,
    /// Bookings a rule categorized.
    pub categorized: usize,
    /// Things that should make the sync look incomplete, such as a failed statement job.
    pub warnings: Vec<String>,
    /// Things the user should know that must not make the sync look failed.
    pub notes: Vec<String>,
    /// The chosen history start the sync used, if any.
    pub history_from: Option<Date>,
}

impl SyncReport {
    /// The summary in English, matching FinDesk's wording.
    pub fn summary(&self) -> String {
        self.summary_in(Language::En)
    }

    /// The summary in `language`.
    pub fn summary_in(&self, language: Language) -> String {
        let mut text = i18n::format(language, Key::SyncSummary, &[Arg::from(self.accounts_synced)]);
        if self.accounts_added > 0 {
            let _ = write!(
                text,
                "{}",
                i18n::format(language, Key::SyncSummaryNew, &[Arg::from(self.accounts_added)])
            );
        }
        let _ = write!(
            text,
            "{}",
            i18n::format(language, Key::SyncSummaryTransactions, &[Arg::from(self.imported)])
        );
        if self.categorized > 0 {
            let _ = write!(
                text,
                "{}",
                i18n::format(language, Key::SyncSummaryCategorized, &[Arg::from(self.categorized)])
            );
        }
        if self.duplicates > 0 {
            let _ = write!(
                text,
                "{}",
                i18n::format(language, Key::SyncSummaryDuplicates, &[Arg::from(self.duplicates)])
            );
        }
        if let Some(from) = self.history_from {
            let _ = write!(
                text,
                "{}",
                i18n::format(
                    language,
                    Key::SyncSummaryHistoryFrom,
                    &[Arg::Str(format_date_dmy(from))]
                )
            );
        }
        for note in &self.notes {
            text.push('\n');
            text.push_str(note);
        }
        text
    }
}

/// Coordinates FinTS work with the local database.
pub struct BankingService {
    client: FinTsClient,
    language: Language,
}

impl BankingService {
    /// Builds a service around a client.
    pub fn new(client: FinTsClient) -> Self {
        let language = client.language();
        Self { client, language }
    }

    /// The client, for callers that need `can_remember_pin` or `pin_not_saved`.
    pub fn client(&self) -> &FinTsClient {
        &self.client
    }

    /// Whether the system offers a credential store, so the UI may offer to remember the PIN.
    pub fn can_remember_pin(&self) -> bool {
        self.client.can_remember_pin()
    }

    /// Set when the last sync's opted-in PIN could not be saved.
    pub fn pin_not_saved(&self) -> bool {
        self.client.pin_not_saved()
    }

    /// A PIN is remembered for `bank_id` (settings flag only; the credential store is never read here).
    pub fn has_saved_pin(&self, db: &Database, bank_id: BankConnectionId) -> bool {
        self.client.has_saved_pin(db, bank_id)
    }

    /// Deletes a remembered PIN for `bank_id`.
    pub fn forget_pin(&self, db: &Database, bank_id: BankConnectionId) {
        self.client.forget_pin(db, bank_id);
    }

    /// Registers a bank login locally (no network). The first sync asks for the PIN and fetches the accounts.
    pub fn add_bank_connection(&self, db: &Database, config: &FinTsConfig) -> BankResult<BankConnection> {
        let connection = repo::bank_connections::create(
            db,
            NewBankConnection {
                bank_name: config.bank_name.clone(),
                blz: config.blz.clone(),
                bic: Some(config.bic.clone()),
                user_id: config.user_id.clone(),
            },
        )
        .map_err(internal_error)?;

        if config.server_url.is_some() || config.tan_method.is_some() {
            repo::bank_connections::update_connection(
                db,
                connection.id,
                BankConnectionUpdate {
                    server_url: config.server_url.clone(),
                    tan_method: config.tan_method.clone(),
                    ..BankConnectionUpdate::default()
                },
            )
            .map_err(internal_error)?;
        }

        let _ = audit::log(
            db,
            "ADD_BANK",
            Actor::User,
            &format!("Added bank access {}", connection.id.0),
        );
        repo::bank_connections::get(db, connection.id)
            .map_err(internal_error)?
            .ok_or_else(|| BankingError::new("NOT_FOUND", i18n::text(self.language, Key::ErrorBankConnectionNotFound)))
    }

    /// Saves a bank login's connection settings.
    ///
    /// The name is only a label. When the BLZ, user id or server URL changed, the stored login is reset (registration
    /// and PIN) so the next sync registers again instead of reusing the old values. A PIN is bound to the bank code and
    /// user id; changing the server URL also forgets it, so a kept PIN is never sent to a different host.
    pub fn update_connection(
        &self,
        db: &Database,
        bank_id: BankConnectionId,
        name: &str,
        blz: &str,
        user_id: &str,
        server_url: &str,
    ) -> BankResult<BankAccessItem> {
        let previous = repo::bank_connections::get_item(db, bank_id).map_err(internal_error)?;
        let Some(previous) = previous else {
            return Err(BankingError::new(
                "NOT_FOUND",
                i18n::text(self.language, Key::ErrorBankConnectionNotFound),
            ));
        };

        // An offline bank has no FinTS login: only its name may change, and never its registration or PIN.
        if previous.offline {
            return repo::bank_connections::update_connection(
                db,
                bank_id,
                BankConnectionUpdate {
                    bank_name: Some(name.to_owned()),
                    ..BankConnectionUpdate::default()
                },
            )
            .map_err(internal_error)?
            .ok_or_else(|| {
                BankingError::new("NOT_FOUND", i18n::text(self.language, Key::ErrorBankConnectionNotFound))
            });
        }

        let new_blz = blz.trim();
        let new_user_id = user_id.trim();
        let new_url = server_url.trim();
        let new_url = (!new_url.is_empty()).then(|| new_url.to_owned());
        let blz_changed = previous.blz != new_blz;
        let user_changed = previous.user_id != new_user_id;
        let url_changed = previous.server_url != new_url;
        let login_changed = blz_changed || user_changed || url_changed;

        // Reset before saving: a failed save then only costs a fresh registration, while a failed reset after the
        // save would leave the old registration in use with the new settings shown. Any login change forgets the PIN.
        if login_changed {
            self.client.reset_login(db, bank_id, true)?;
        }

        let updated = repo::bank_connections::update_connection(
            db,
            bank_id,
            BankConnectionUpdate {
                server_url: Some(server_url.to_owned()),
                user_id: Some(user_id.to_owned()),
                bank_name: Some(name.to_owned()),
                blz: Some(blz.to_owned()),
                // The BIC follows the BLZ only when the bank directory knows the new one. The directory is not
                // ported, so the old BIC stays.
                bic: None,
                ..BankConnectionUpdate::default()
            },
        )
        .map_err(internal_error)?
        .ok_or_else(|| BankingError::new("NOT_FOUND", i18n::text(self.language, Key::ErrorBankConnectionNotFound)))?;

        if login_changed {
            let mut fields = Vec::new();
            if blz_changed {
                fields.push("bank code");
            }
            if user_changed {
                fields.push("user ID");
            }
            if url_changed {
                fields.push("server URL");
            }
            let _ = audit::log(
                db,
                "RESET_FINTS_LOGIN",
                Actor::User,
                &format!(
                    "Bank access {}: {} changed, registration reset",
                    bank_id.0,
                    fields.join(", ")
                ),
            );
        }
        if blz_changed {
            let _ = audit::log(
                db,
                "CHANGE_BANK_CODE",
                Actor::User,
                &format!("Bank access {}: BLZ {} -> {}", bank_id.0, previous.blz, new_blz),
            );
        }

        Ok(updated)
    }

    /// Removes a bank login with its accounts, transactions and stored FinTS registration/PIN.
    pub fn remove_bank(&self, db: &Database, bank_id: BankConnectionId) -> BankResult<bool> {
        // PIN and flag first, so a later failure can never leave a secret behind for a bank that no longer exists.
        self.client.delete_stored_credentials(db, bank_id);

        let removed = repo::bank_connections::delete_with_accounts(db, bank_id).map_err(internal_error)?;
        if removed {
            let _ = audit::log(
                db,
                "DELETE_BANK_ACCESS",
                Actor::User,
                &format!("Deleted bank access {}", bank_id.0),
            );
        }
        Ok(removed)
    }

    /// Syncs one bank login: accounts (matched by IBAN, new ones are created), balances and bookings.
    pub fn sync_bank_connection(
        &self,
        db: &Database,
        bank_id: BankConnectionId,
        interaction: &dyn BankingInteraction,
        today: Date,
    ) -> BankResult<SyncReport> {
        let bank = repo::bank_connections::get_item(db, bank_id)
            .map_err(internal_error)?
            .ok_or_else(|| {
                BankingError::new("NOT_FOUND", i18n::text(self.language, Key::ErrorBankConnectionNotFound))
            })?;
        if bank.offline {
            return Err(BankingError::new(
                "OFFLINE",
                i18n::text(self.language, Key::ErrorBankOffline),
            ));
        }

        let known = repo::accounts::list(db)
            .map_err(internal_error)?
            .into_iter()
            .filter(|account| account.bank_connection_id == Some(bank_id))
            .collect::<Vec<_>>();
        let default_start = today - Duration::days(INITIAL_SYNC_DAYS);
        let history_from = bank.sync_from;

        let since = |iban: &str| -> Option<Date> {
            let normal = if known.iter().any(|account| {
                account
                    .iban
                    .as_deref()
                    .is_some_and(|known| known.eq_ignore_ascii_case(iban))
            }) {
                bank.last_sync
                    .map(|last| (last.date() - Duration::days(SYNC_OVERLAP_DAYS)).max(default_start))
                    .unwrap_or(default_start)
            } else {
                default_start
            };
            match history_from {
                Some(from) => Some(from.min(normal)),
                None => Some(normal),
            }
        };

        let snapshots = self.client.fetch_all(db, &bank, interaction, &since, today)?;

        self.store_snapshots(db, &bank, &known, &snapshots, history_from, today)
    }

    /// Imports bank bookings, skipping ones already imported (stored or remembered as deleted) and applying rules.
    pub fn import_transactions(
        &self,
        db: &Database,
        account_id: AccountId,
        transactions: &[FinTsTransaction],
    ) -> BankResult<ImportResult> {
        let account = repo::accounts::get(db, account_id)
            .map_err(internal_error)?
            .ok_or_else(|| BankingError::new("NOT_FOUND", i18n::text(self.language, Key::ErrorAccountNotFound)))?;

        let candidates = transactions
            .iter()
            .map(|transaction| Candidate {
                date: transaction.date,
                amount: transaction.amount,
                hash: import_hash(
                    transaction.date,
                    transaction.amount,
                    transaction.partner_name.as_deref(),
                    transaction.purpose.as_deref(),
                    &transaction.account.iban,
                    transaction.bank_id.as_deref(),
                    transaction.balance_after,
                    transaction.prima_nota.as_deref(),
                ),
                item: transaction.clone(),
            })
            .collect::<Vec<_>>();

        let plan = dedup::plan(db, account_id, account.currency, candidates).map_err(internal_error)?;
        let is_first_import = !repo::transactions::has_bookings(db, account_id).map_err(internal_error)?;
        let matcher = rules::matcher(db).map_err(internal_error)?;

        let mut invalid_amount = 0;
        let mut rows = Vec::with_capacity(plan.to_import.len());
        for candidate in plan.to_import {
            let transaction = candidate.item;
            let Ok(amount) = amount_to_money(transaction.amount, account.currency) else {
                invalid_amount += 1;
                continue;
            };
            let balance_after = match transaction.balance_after {
                Some(value) => match amount_to_money(value, account.currency) {
                    Ok(money) => Some(money),
                    Err(_) => {
                        invalid_amount += 1;
                        continue;
                    }
                },
                None => None,
            };
            // Booking texts are the user's own data and are stored as the bank sent them, as FinDesk and the CSV
            // import do (rules and search may match an IBAN in the purpose). Only error, warning and note texts are
            // masked.
            let partner_name = transaction
                .partner_name
                .as_deref()
                .map(|name| name.chars().take(255).collect::<String>());
            let purpose = transaction.purpose.clone();
            let category_id =
                matcher.category_for(purpose.as_deref(), partner_name.as_deref(), Some(transaction.amount));
            rows.push(NewTransaction {
                booking_date: transaction.date,
                amount,
                balance_after,
                partner_name,
                purpose,
                category_id,
                import_hash: candidate.hash,
                created_at: OffsetDateTime::now_utc(),
                is_new: !is_first_import,
                tax_override: None,
            });
        }

        let categorized = rows.iter().filter(|row| row.category_id.is_some()).count();
        let imported = repo::transactions::insert_planned(db, account_id, rows).map_err(internal_error)?;

        // A remembered deleted booking comes back through no new field: it is not new, so it counts as duplicate.
        let duplicates = plan.exact_duplicates + plan.probable_duplicates + plan.previously_deleted;
        Ok(ImportResult {
            imported,
            duplicates,
            categorized,
            invalid_amount,
        })
    }
}

impl BankingService {
    fn store_snapshots(
        &self,
        db: &Database,
        bank: &BankAccessItem,
        known: &[crate::model::Account],
        snapshots: &[AccountSnapshot],
        history_from: Option<Date>,
        today: Date,
    ) -> BankResult<SyncReport> {
        let mut accounts_added = 0;
        let mut imported = 0;
        let mut duplicates = 0;
        let mut categorized = 0;
        let mut warnings = Vec::new();
        let mut notes = Vec::new();
        let stored = snapshots
            .iter()
            .filter(|snapshot| !snapshot.account.iban.trim().is_empty())
            .collect::<Vec<_>>();
        let now = OffsetDateTime::now_utc();
        let language = self.language;

        let default_start = today - Duration::days(INITIAL_SYNC_DAYS);
        let gap_start = bank
            .last_sync
            .map(|last| last.date() - Duration::days(SYNC_OVERLAP_DAYS));
        if let Some(gap_start) = gap_start
            && gap_start < default_start
            && history_from.is_none_or(|from| from > gap_start)
            && stored.iter().any(|snapshot| {
                let iban = crate::iban::normalize(&snapshot.account.iban);
                known.iter().any(|account| {
                    account
                        .iban
                        .as_deref()
                        .is_some_and(|known| known.eq_ignore_ascii_case(&iban))
                })
            })
        {
            let gap_end = history_from.unwrap_or(default_start).min(default_start) - Duration::days(1);
            notes.push(i18n::format(
                language,
                Key::SyncGapNotFetched,
                &[
                    Arg::Str(format_date_dmy(gap_start)),
                    Arg::Str(format_date_dmy(gap_end)),
                    Arg::from(INITIAL_SYNC_DAYS),
                ],
            ));
        }

        for snapshot in snapshots
            .iter()
            .filter(|snapshot| snapshot.account.iban.trim().is_empty())
        {
            notes.push(i18n::format(
                language,
                Key::SyncAccountWithoutIban,
                &[Arg::Str(note_suffix(&snapshot.account))],
            ));
        }

        for snapshot in &stored {
            let iban = crate::iban::normalize(&snapshot.account.iban);
            let existing = known.iter().find(|account| {
                account
                    .iban
                    .as_deref()
                    .is_some_and(|known| known.eq_ignore_ascii_case(&iban))
            });
            let mut account_currency = existing
                .map(|account| account.currency)
                .unwrap_or_else(|| parse_currency(&snapshot.account.currency));
            let balance = snapshot.balance.as_ref();

            // Older versions created accounts as EUR when the bank's account data had no currency. If the bank now
            // states the balance in another currency and the account has no EUR booking to lose, correct it instead
            // of noting the balance as belonging elsewhere on every sync.
            if let Some(account) = existing
                && !snapshot.account.currency_stated
                && let Some(balance) = balance
            {
                let reported = balance.currency.trim().to_uppercase();
                if is_currency_code(&reported)
                    && reported != account.currency.as_str()
                    && account.currency == Currency::EUR
                    && repo::transactions::find_items(
                        db,
                        &TransactionFilter {
                            account_id: Some(account.id),
                            currency: Some(account.currency),
                            ..TransactionFilter::default()
                        },
                        Some(1),
                    )
                    .map_err(internal_error)?
                    .is_empty()
                    && let Ok(new_currency) = Currency::new(&reported)
                    && let Ok(money) = amount_to_money(balance.balance, new_currency)
                {
                    repo::accounts::set_currency(db, account.id, new_currency, money, now)?;
                    notes.push(i18n::format(
                        language,
                        Key::SyncCurrencyCorrected,
                        &[
                            Arg::Str(tail_four(&iban)),
                            Arg::Str(account.currency.as_str().to_owned()),
                            Arg::Str(reported.clone()),
                        ],
                    ));
                    account_currency = new_currency;
                }
            }

            if let Some(account) = existing
                && account.account_type == AccountType::Depot
                && snapshot.account.account_type == AccountType::Loan.as_str()
            {
                repo::accounts::set_account_type(db, account.id, AccountType::Loan)?;
            }

            let balance_matches_currency =
                balance.is_none_or(|balance| balance.currency.eq_ignore_ascii_case(account_currency.as_str()));

            let account_id = match existing {
                Some(account) => account.id,
                None => {
                    let starting_balance = match balance {
                        Some(balance) if balance.currency.eq_ignore_ascii_case(&snapshot.account.currency) => {
                            amount_to_money(balance.balance, account_currency).unwrap_or(Money {
                                minor: 0,
                                currency: account_currency,
                            })
                        }
                        _ => Money {
                            minor: 0,
                            currency: account_currency,
                        },
                    };
                    let id = repo::accounts::create(
                        db,
                        NewAccount {
                            bank_connection_id: Some(bank.id),
                            institution: Some(bank.bank_name.clone()),
                            name: None,
                            iban: Some(iban.clone()),
                            account_number: (!snapshot.account.account_number.is_empty())
                                .then(|| snapshot.account.account_number.clone()),
                            currency: account_currency,
                            account_type: AccountType::from_stored(&snapshot.account.account_type)
                                .unwrap_or(AccountType::Checking),
                            balance: starting_balance,
                            balance_updated_at: None,
                            group_id: None,
                        },
                    )
                    .map_err(internal_error)?;
                    accounts_added += 1;
                    id
                }
            };

            if let Some(balance) = balance {
                if balance_matches_currency {
                    if let Ok(money) = amount_to_money(balance.balance, account_currency) {
                        repo::accounts::set_balance_for_import(db, account_id, money, now).map_err(internal_error)?;
                    }
                } else {
                    notes.push(i18n::format(
                        language,
                        Key::SyncBalanceOtherCurrency,
                        &[
                            Arg::Str(tail_four(&iban)),
                            Arg::Str(balance.currency.clone()),
                            Arg::Str(account_currency.as_str().to_owned()),
                        ],
                    ));
                }
            }

            if let Some(error) = &snapshot.statement_error {
                warnings.push(format!(
                    "{}: {}",
                    tail_four(&iban),
                    redactor::mask_account_numbers(error)
                ));
            }

            if balance.is_none() && existing.is_none_or(|account| account.balance.minor == 0) {
                let key = if snapshot.balance_code.is_some() {
                    Key::BalanceNotProvided
                } else {
                    Key::BalanceNotProvidedNoCode
                };
                let mut args = vec![Arg::Str(tail_four(&iban))];
                if let Some(code) = &snapshot.balance_code {
                    args.push(Arg::Str(code.clone()));
                }
                notes.push(i18n::format(language, key, &args));
            }

            let result = self.import_transactions(db, account_id, &snapshot.transactions)?;
            imported += result.imported;
            duplicates += result.duplicates;
            categorized += result.categorized;

            // Pending (vorgemerkte) lines stay apart from the bookings: fetched means replace all of the account's
            // rows (an empty list clears them), `None` keeps what is stored. A failure here never fails the sync.
            if let Some(lines) = &snapshot.pending {
                let items = lines
                    .iter()
                    .filter_map(|line| pending_item(line, account_currency))
                    .collect::<Vec<_>>();
                let _ = repo::pending_bookings::replace_for_account(db, account_id, items, now);
            }
        }

        // If statements failed for any account, keep the old sync date so the next sync re-fetches that window.
        if warnings.is_empty() {
            repo::bank_connections::update_last_sync(db, bank.id).map_err(internal_error)?;
            // The chosen history start is one-shot: a fully successful sync clears it, a failure keeps it.
            if history_from.is_some() {
                repo::bank_connections::set_sync_from(db, bank.id, None).map_err(internal_error)?;
            }
        }

        let report = SyncReport {
            accounts_synced: stored.len(),
            accounts_added,
            imported,
            duplicates,
            categorized,
            warnings,
            notes,
            history_from,
        };
        let _ = audit::log(
            db,
            "SYNC_BANK",
            Actor::User,
            &format!(
                "Bank access {}: {} accounts synced, {} imported, {} duplicates, {} categorized",
                bank.id.0, report.accounts_synced, report.imported, report.duplicates, report.categorized
            ),
        );
        Ok(report)
    }
}

fn pending_item(line: &FinTsTransaction, currency: Currency) -> Option<NewPendingBooking> {
    let amount = amount_to_money(line.amount, currency).ok()?;
    Some(NewPendingBooking {
        date: line.date,
        value_date: Some(line.value_date),
        amount,
        partner_name: line
            .partner_name
            .as_deref()
            .map(|name| name.chars().take(255).collect()),
        purpose: line.purpose.clone(),
    })
}

fn parse_currency(text: &str) -> Currency {
    Currency::new(text).unwrap_or(Currency::EUR)
}

fn is_currency_code(text: &str) -> bool {
    text.len() == 3 && text.bytes().all(|byte| byte.is_ascii_alphabetic())
}

fn tail_four(text: &str) -> String {
    let chars = text.chars().collect::<Vec<_>>();
    chars[chars.len().saturating_sub(4)..].iter().collect()
}

fn format_date_dmy(date: Date) -> String {
    format!("{:02}.{:02}.{:04}", date.day(), date.month() as u8, date.year())
}

/// At most the last four characters of an account number, else the product name or type as the identifier.
fn note_suffix(account: &FinTsAccount) -> String {
    if !account.account_number.is_empty() {
        format!("…{}", tail_four(&account.account_number))
    } else if let Some(name) = account
        .product_name
        .as_deref()
        .map(str::trim)
        .filter(|name| !name.is_empty())
        .map(redactor::mask_account_numbers)
    {
        name
    } else if !account.account_type.is_empty() {
        account.account_type.clone()
    } else {
        "?".to_owned()
    }
}

fn internal_error(error: impl std::fmt::Display) -> BankingError {
    BankingError::new("SYNC_FAILED", error.to_string())
}

impl From<RepoError> for BankingError {
    fn from(error: RepoError) -> Self {
        BankingError::new("SYNC_FAILED", error.to_string())
    }
}
