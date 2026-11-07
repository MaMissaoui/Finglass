//! EN texts.

use super::Key;

pub(super) fn text(key: Key) -> &'static str {
    match key {
        Key::Cancel => "Cancel",
        Key::Save => "Save",
        Key::Add => "Add",
        Key::Delete => "Delete",
        Key::Close => "Close",
        Key::Ok => "OK",
        Key::Open | Key::OpenProfile => "Open",
        Key::Back => "Back",
        Key::Dismiss | Key::DismissSuggestion => "Dismiss",
        Key::More => "More",
        Key::ClearSearch => "Clear search",
        Key::Uncategorized => "Uncategorized",
        Key::TaxRelevant => "tax-relevant",
        Key::Tax => "tax",
        Key::ToggleTheme => "Toggle theme",
        Key::Link => "Link",
        Key::Unlink => "Unlink",
        Key::OpenDetails => "Open details",
        Key::ChangeCategory => "Change category: %s",
        Key::OpenAccount => "Open account",
        Key::OpenDocument => "Open document",
        Key::ViewPendingTasks => "View pending tasks",
        Key::EnterValidDate | Key::ErrorSyncFromDateInvalid => "Enter a valid date (dd.mm.yyyy)",
        Key::EnterValidAmount => "Enter a valid amount",
        Key::EnterValidBalance => "Enter a valid balance",
        Key::InvalidDateRange => "The end date must be after the start date",
        Key::PatternInvalid => "Enter a valid regular expression",
        Key::OpticalTanCode => "Optical TAN code for a TAN generator. Hold the TAN generator against the bars.",
        Key::WindowTitle => "Finglass - Your Financial Workspace",
        Key::NavDashboard => "Dashboard",
        Key::NavAccounts => "Accounts",
        Key::NavTransactions
        | Key::Transactions
        | Key::SummaryCount
        | Key::StatTransactions
        | Key::PdfTransactionCount => "Transactions",
        Key::NavDocuments | Key::DocumentsFolder => "Documents",
        Key::NavReports => "Reports",
        Key::NavSettings => "Settings",
        Key::TypeChecking => "Checking",
        Key::TypeSavings => "Savings",
        Key::TypeCreditCard => "Credit card",
        Key::TypeLoan => "Loan",
        Key::TypeDepot => "Depot",
        Key::PeriodThisMonth => "This month",
        Key::PeriodLastMonth => "Last month",
        Key::PeriodLast3Months => "Last 3 months",
        Key::PeriodThisYear => "This year",
        Key::PeriodLastYear => "Last year",
        Key::PeriodAllTime => "All time",
        Key::PeriodCustomRange => "Custom range…",
        Key::DirectionAll => "All",
        Key::DirectionIncome => "Income",
        Key::DirectionExpenses => "Expenses",
        Key::DirectionAnyLabel => "In and out",
        Key::CadenceWeekly => "Weekly",
        Key::CadenceMonthly => "Monthly",
        Key::CadenceQuarterly => "Quarterly",
        Key::CadenceYearly => "Yearly",
        Key::WelcomeTitle => "Welcome to Finglass",
        Key::AddAccount => "Add account",
        Key::AvailableBalance => "Available",
        Key::LoansSubtitle => "Loans %1$s · net %2$s",
        Key::IncomeThisMonth => "Income this month",
        Key::ExpensesThisMonth => "Expenses this month",
        Key::PendingTasks => "Pending tasks",
        Key::PendingSubtitle => "%d uncategorized · %d unlinked {2:doc|docs}",
        Key::ExpensesByCategory => "Expenses by category (this month)",
        Key::NoExpensesThisMonth => "No expenses this month",
        Key::IncomeVsExpenses => "Income vs expenses (6 months)",
        Key::Upcoming30Days => "Upcoming in the next 30 days",
        Key::AmountVaries => "amount varies",
        Key::BudgetsThisMonth => "Budgets this month",
        Key::SpentOfLimit => "%s of %s",
        Key::OverBudget => "%s over budget",
        Key::LeftBudget => "%s left",
        Key::TaxDeductibleYtd => "Tax-deductible expenses (year to date)",
        Key::TaxDeductibleHint => "Mark categories as tax-relevant in Settings to track deductible expenses.",
        Key::RecentTransactions => "Recent transactions",
        Key::ViewAll => "View all",
        Key::NoTransactionsYet => "No transactions yet",
        Key::GettingStartedIntro => "Three steps to your first overview.",
        Key::StepAddBank => "Add your bank",
        Key::StepAddBankHint => "Enter the bank code and your online-banking login.",
        Key::StepSync => "Fetch balances and bookings",
        Key::StepSyncHint => "Sync asks your bank once for your PIN; balances can also be entered by hand.",
        Key::StepCategorize => "Sort your bookings",
        Key::StepCategorizeHint => "Categories and rules group spending and mark what counts for taxes.",
        Key::OpenSettings => "Open settings",
        Key::NothingPending => "Nothing to do",
        Key::NoBookingsSixMonths => "No bookings in the last six months",
        Key::PreviousMonth => "Previous month",
        Key::NextMonth => "Next month",
        Key::CurrentMonth => "Current month",
        Key::IncomeInMonth => "Income in %s",
        Key::ExpensesInMonth => "Expenses in %s",
        Key::BudgetsInMonth => "Budgets in %s",
        Key::NoExpensesInMonth => "No expenses in %s",
        Key::ExpensesByCategoryInMonth => "Expenses by category in %s",
        Key::NoBookingsSixMonthsTo => "No bookings in the six months to %s",
        Key::AvailableAsOf => "Available on %s",
        Key::CarryForwardFrom => "Carried over from %1$s: %2$s",
        Key::OtherMovements => "Transfers and loans: %s",
        Key::AccountsTotal => "Total %s across %d {2:account|accounts}",
        Key::AccountsTotalLabel => "Total of %d {1:account|accounts}",
        Key::AccountsNetLabel => "Net of %d {1:account|accounts}",
        Key::AccountsLoansLabel => "Of which loans",
        Key::AccountCount => "%d {1:account|accounts}",
        Key::AddBank => "Add bank",
        Key::NoBanksYet => "No banks yet",
        Key::NoBanksDescription => {
            "Add your bank, then add its accounts. Balances and transactions can be entered manually until online banking sync is set up."
        }
        Key::BlzPrefix => "BLZ %s",
        Key::BicPrefix => "BIC %s",
        Key::SyncedPrefix => "synced %s",
        Key::Syncing => "Syncing…",
        Key::Sync => "Sync",
        Key::SyncAll => "Sync all",
        Key::SyncAllProgress => "Syncing %d of %d: %s",
        Key::IncludeInSyncAll => "Include in Sync all",
        Key::NotInSyncAll => "not in Sync all",
        Key::SyncAllNothingIncluded => "No connection is included in Sync all",
        Key::SyncAllStopped => "Sync all stopped: %s was cancelled.",
        Key::FetchHistory => "Fetch history from…",
        Key::FetchHistoryFor => "Fetch history for %s",
        Key::FetchHistoryDate => "Start date",
        Key::FetchHistoryHint => {
            "Bookings already imported are skipped. Banks limit how far back they go and many ask for a TAN beyond 90 days."
        }
        Key::Fetch => "Fetch",
        Key::Reset => "Reset",
        Key::NextSyncFrom => "next sync from %s",
        Key::ConnectionSettings => "Connection settings",
        Key::RenameConnection => "Rename",
        Key::ConnectionName | Key::ColName | Key::Name => "Name",
        Key::ConnectionNameHint => "Only shown in Finglass, e.g. “DKB business”. The bank login stays the same.",
        Key::AddAccountShort | Key::DetailAccount | Key::ColAccount => "Account",
        Key::RemoveBank => "Remove bank",
        Key::NoAccountsYet => "No accounts yet",
        Key::EditBalance => "Edit balance",
        Key::DeleteAccount => "Delete account",
        Key::AllowDeletedAgain => "Allow deleted bookings again",
        Key::BlzDigits => "BLZ (8 digits)",
        Key::BankName => "Bank name",
        Key::BicOptional => "BIC (optional)",
        Key::OnlineUserIdOptional => "Online banking user ID (optional)",
        Key::NoFintsServer => "no FinTS server listed",
        Key::OnlineSyncAvailable => "online sync available",
        Key::BankSearchHint => "Search bank: name, BLZ, BIC or IBAN",
        Key::BankSearchNoMatch => "No bank found — enter the details below",
        Key::BankManualEntry => "or enter the details manually",
        Key::BankUnknownBlz => "BLZ %s is not in the bank directory",
        Key::BankFintsProposed => "FinTS server: %s",
        Key::BankUseSuggestion => "Use this bank",
        Key::AddAccountAt => "Add account at %s",
        Key::Iban => "IBAN",
        Key::BalanceNotANumber => "Balance is not a number",
        Key::UpdateBalance => "Update balance",
        Key::OnlineBankingFor => "Online banking for %s",
        Key::UserIdLogin => "User ID (Anmeldename / Legitimations-ID)",
        Key::FintsServerUrl => "FinTS server URL",
        Key::LeaveEmptyToUse => "Leave empty to use %s",
        Key::AskBankForFints => "Ask your bank for its FinTS (PIN/TAN) address",
        Key::RememberedTan => "Remembered TAN procedure: %s",
        Key::PinNeverStored => "Your PIN is asked for on every sync and can't be stored on this system.",
        Key::PinSavedForSyncs => "PIN saved for syncs",
        Key::ForgetPin => "Forget PIN",
        Key::PinAskedEverySync => {
            "Your PIN is asked for on every sync. Tick “Remember PIN for syncs” in the PIN prompt to keep it in your system's secure credential store."
        }
        Key::DaysCount => "%d days",
        Key::OneYear => "1 year",
        Key::LastDaysSummary => "Last %d days: in %s · out %s",
        Key::RecentBookings => "Recent bookings",
        Key::NoneInPeriod => "None in this period",
        Key::HistoryReconstructed => {
            "History is reconstructed from the current balance and the bookings Finglass knows about."
        }
        Key::AllTransactions => "All transactions",
        Key::DeleteAccountTitle => "Delete account?",
        Key::DeleteAccountText => {
            "This deletes %s and all of its transactions. Linked documents are kept but unlinked."
        }
        Key::RenameAccount => "Rename account",
        Key::ChangeAccountType => "Change account type…",
        Key::AccountTypeLabel => "Account type",
        Key::LoanBalanceHint => "Enter what you still owe as a negative amount, e.g. -12,000.00.",
        Key::AccountName => "Account name",
        Key::AccountNameHint => "Only shown in Finglass. Leave empty to show “%s”.",
        Key::RemoveBankTitle => "Remove %s?",
        Key::RemoveBankText => "This deletes the bank, all of its accounts and their transactions.",
        Key::CentralBlzNote => {
            "This bank's online banking uses bank code %1$s for all branches, so Finglass uses it instead of %2$s."
        }
        Key::CentralBlzHint => "Online banking for this bank uses bank code %s.",
        Key::UseBlz => "Use %s",
        Key::BlzNotInDirectory => "Not in the bank list. Check the bank code.",
        Key::BlzOtherBank => "This bank code belongs to %s, not to this connection's bank.",
        Key::LoginResetNote => {
            "Changing the bank code, user ID or server URL resets Finglass's registration with the bank. The next sync registers again and may ask you to confirm with a TAN."
        }
        Key::Import => "Import",
        Key::AutoCategorize => "Auto-categorize",
        Key::ExportCsv => "Export CSV",
        Key::SearchPartnerPurpose => "Search partner or purpose…",
        Key::AllAccounts => "All accounts",
        Key::TransactionsSummary => "%d {1:transaction|transactions} · in %s · out %s",
        Key::SummaryIn => "In",
        Key::SummaryOut => "Out",
        Key::SummaryBalance | Key::ColBalance => "Balance",
        Key::TransactionsBalance => " · Balance %s",
        Key::BalanceBankMismatch => {
            "The bank reports %1$s after this booking. Bookings of %2$s are missing after it, e.g. ones the bank hasn't put in its statement yet."
        }
        Key::ColDate | Key::DetailDate => "Date",
        Key::ColDescription => "Description",
        Key::ColCategory | Key::DetailCategory | Key::CategoryMenu => "Category",
        Key::ColAmount => "Amount",
        Key::NoTransactions => "No transactions",
        Key::NoTransactionsAddBank => "Add your bank to import or enter transactions.",
        Key::NothingMatches => "Nothing matches the current filters.",
        Key::ImportBankStatement => "Import bank statement (CSV, CAMT XML/ZIP)",
        Key::AttachReceipt => "Attach receipt",
        Key::ExportTransactions => "Export transactions",
        Key::AllCategories => "All categories",
        Key::MinAmountIn | Key::MinAmount => "min %s",
        Key::MaxAmountIn => "max %s",
        Key::AmountFilterAny => "Any amount",
        Key::AddTransaction => "Add transaction",
        Key::Expense => "Expense",
        Key::Income | Key::ColIncome | Key::ChartIncome | Key::TaxSectionIncome => "Income",
        Key::DateFormat => "Date (dd.mm.yyyy)",
        Key::AmountIn => "Amount (%s)",
        Key::DelimiterLabel => "delimiter %s",
        Key::PartnerPayee => "Partner / payee",
        Key::Purpose | Key::ColPurpose | Key::DetailPurpose => "Purpose",
        Key::CategoryAuto => "Category: auto (rules)",
        Key::AutoRules => "Auto (rules)",
        Key::ImportFile => "Import %s",
        Key::BookingCount | Key::CategoryUsage => "%d {1:booking|bookings}",
        Key::PendingLeftOut => "%d pending left out",
        Key::UnreadableCount => "%d unreadable",
        Key::IntoAccount => "Into account: ",
        Key::ChooseAccount => "Choose account",
        Key::FileBelongsTo => "The file belongs to %s, not the selected account.",
        Key::ImportFiles => "Import %d files",
        Key::FileBelongsToNamed => "%s belongs to %s, not the selected account.",
        Key::ColumnsApplyToSame => "Column changes apply to the %d {1:file|files} with these columns.",
        Key::ImportFilesSkipped => "%d {1:file is|files are} skipped because {1:it|they} could not be read.",
        Key::UnlockWindowTitle => "Unlock Finglass",
        Key::UnlockProfile => "Profile: %s",
        Key::UnlockHeading => "Enter your passphrase",
        Key::UnlockText => "Your data is encrypted. Enter the passphrase to open it.",
        Key::Passphrase => "Passphrase",
        Key::CurrentPassphrase => "Current passphrase",
        Key::NewPassphrase => "New passphrase",
        Key::RepeatPassphrase => "Repeat the passphrase",
        Key::ShowPassphrase => "Show passphrase",
        Key::Unlock => "Unlock",
        Key::ChangePassphrase => "Change passphrase",
        Key::ChangePassphraseText => {
            "Enter the current passphrase and choose a new one. Finglass re-encrypts your data and then opens it."
        }
        Key::ChangeAndOpen => "Change and open",
        Key::CreatePassphraseHeading => "Protect your data with a passphrase",
        Key::CreatePassphraseText => {
            "Finglass encrypts its database: accounts, transactions, settings and bank connection keys. You enter this passphrase every time Finglass starts."
        }
        Key::EncryptPassphraseHeading => "Encrypt your data",
        Key::EncryptPassphraseText => {
            "Your data isn't encrypted yet. Choose a passphrase: Finglass encrypts the database now and asks for the passphrase every time it starts."
        }
        Key::PassphraseHint => "At least %d characters. A few unrelated words are easy to remember and hard to guess.",
        Key::DocumentsNotEncrypted => {
            "Document files (receipts, invoices) are stored as files and are not encrypted. Turn on FileVault (macOS) or BitLocker (Windows) to protect them."
        }
        Key::PassphraseNoRecovery => "I understand that nobody can recover my data if I forget this passphrase.",
        Key::EncryptAndOpen => "Encrypt and open",
        Key::Encrypting => "Encrypting your data…",
        Key::Unlocking => "Opening…",
        Key::WrongPassphrase => "Wrong passphrase.",
        Key::PassphraseTooShort => "Use at least %d characters.",
        Key::PassphraseSpaces => "The passphrase can't start or end with a space.",
        Key::PassphraseMismatch => "The passphrases don't match.",
        Key::PassphraseAcknowledge => "Confirm that you understand there's no recovery.",
        Key::UnlockFailed => "Finglass couldn't open the database: %s",
        Key::RestoredEncryptedHint => {
            "A backup was restored. Enter the passphrase that was in effect when the backup was made."
        }
        Key::RestoredPlainHint => "A backup without encryption was restored.",
        Key::PlaintextLeftoversTitle => "Unencrypted copies of the database are still on disk",
        Key::PlaintextLeftoversText => {
            "Finglass encrypts the database it opens, but these copies in the data folder were left behind by earlier restores, deleted profiles or profiles that were never opened. Anyone who can read this disk can read them without your passphrase. Delete the ones you no longer need."
        }
        Key::PlaintextLeftoverPreRestore => "Replaced by a restore",
        Key::PlaintextLeftoverDeletedProfile => "Deleted profile",
        Key::PlaintextLeftoverPendingRestore => "Restore waiting for the next start",
        Key::PlaintextLeftoverOtherProfile => "Another profile",
        Key::EncryptionStatus => {
            "Database encrypted (SQLCipher, AES-256). Finglass asks for the passphrase at every start; you can change it on the unlock screen."
        }
        Key::EncryptionOff => "Database not encrypted (development or test mode).",
        Key::BackupPassphraseTitle => "Passphrase of this backup",
        Key::BackupPassphraseText => {
            "This backup was made with a different passphrase. Enter that passphrase to check the backup. Finglass asks for it again at the next start."
        }
        Key::BackupNote => "Backups stay encrypted with the passphrase in effect when they are made.",
        Key::Edit => "Edit",
        Key::Profiles => "Profiles",
        Key::ProfilesHint => "Each profile has its own accounts, transactions, documents and bank connections.",
        Key::NewProfile => "New profile",
        Key::ProfileName => "Profile name",
        Key::RenameProfile => "Rename %s",
        Key::DeleteProfile => "Delete %s",
        Key::DeleteProfileText => {
            "The data of %s is moved to the profiles-deleted folder. Nothing is erased: the folder, unencrypted documents included, stays until you delete it yourself."
        }
        Key::SwitchProfile => "Open at next start",
        Key::ProfileOpen => "open now",
        Key::ProfileNextStart => "opens at next start",
        Key::RestartToOpenProfile => "Restart Finglass to open %s.",
        Key::ProfileSwitchBusy => "Finish the running task before switching profiles.",
        Key::WrongCurrencySkipped => {
            "%1$d {1:booking is|bookings are} in another currency than this %2$s account and will be skipped."
        }
        Key::PlusOtherCurrencies => "+ %s in other currencies",
        Key::OtherCurrenciesExcluded => {
            "All amounts in %1$s. %2$d {2:booking|bookings} in other currencies {2:is|are} not included."
        }
        Key::PrimaryCurrency => "Primary currency",
        Key::PrimaryCurrencyHint => {
            "Dashboard, budgets and reports add up bookings in this currency only. Amounts are never converted."
        }
        Key::CurrencyIs => "Currency: %s",
        Key::CurrentBalanceIn => "Current balance (%s)",
        Key::Columns => "Columns",
        Key::Preview => "Preview",
        Key::ColDebit => "Debit",
        Key::ColCredit => "Credit",
        Key::ColPartner | Key::DetailPartner => "Partner",
        Key::ColShop | Key::DetailShop => "Shop",
        Key::NoneDash => "— none —",
        Key::ColumnN => "Column %d",
        Key::AndMore => "… and %d more",
        Key::SetAccountBalance => "Set the account balance from the newest booking in the file",
        Key::SetAccountBalanceClosing => "Set the account balance to %s (the file's closing balance)",
        Key::SetAccountBalanceClosingPending => {
            "Set the account balance to %1$s (the file's closing balance without its %2$d pending entries)"
        }
        Key::NewerBookingsUnchanged => {
            "The account has newer bookings than this file, so its balance is left unchanged."
        }
        Key::BookingsSkippedHint => {
            "Bookings imported before are skipped, as are ones that match an existing booking from another source (sync or manual entry) by date and amount."
        }
        Key::Importing => "Importing…",
        Key::ImportCount => "Import %d",
        Key::ErrorImportTooManyFiles => "Select at most %d files at once.",
        Key::ErrorImportFilesTooLarge => "The selected files are larger than %d MB together.",
        Key::ErrorImportNoReadableFile => "None of the selected files could be read: %s",
        Key::ErrorImportDifferentAccounts => {
            "The files belong to different accounts (%s). Import one account at a time."
        }
        Key::ErrorImportStopped => "Import stopped at %s: %s. %d {3:file was|files were} imported before it.",
        Key::DeleteTransactionTitle => "Delete transaction?",
        Key::DocumentsCount => "%d {1:document|documents}",
        Key::AttachReceiptEllipsis => "Attach receipt…",
        Key::NoDocumentsLinked => "No documents linked",
        Key::CategorizeSimilar => "Categorize similar bookings automatically",
        Key::PatternRegex => "Pattern (regex)",
        Key::CreateRule => "Create rule",
        Key::TabDelimiter => "tab",
        Key::NewGroup => "New group",
        Key::GroupName => "Group name",
        Key::RenameGroup => "Rename group",
        Key::DeleteGroup => "Delete group",
        Key::DeleteGroupText => "Delete group “%s”? Its accounts move back to their default group.",
        Key::MoveUp => "Move up",
        Key::MoveDown => "Move down",
        Key::MoveToGroup => "Move to group…",
        Key::DefaultGroup => "Default (by type)",
        Key::FilterAccounts => "Filter accounts",
        Key::AccountsShown | Key::TaxReceiptsOf => "%1$d of %2$d",
        Key::LastFetched => "Last fetched %s",
        Key::Collapse => "Collapse",
        Key::Expand => "Expand",
        Key::NewBookingsCount => "%d {1:new booking|new bookings}",
        Key::NewBooking => "New booking",
        Key::MarkAsRead => "Mark as read",
        Key::DragToGroupHint => "Drop on a group to move the account",
        Key::CollapseSidebar => "Collapse sidebar",
        Key::ExpandSidebar => "Expand sidebar",
        Key::ChangeLanguage => "Change language",
        Key::LanguageBusy => "Finish the running task first",
        Key::SwitchProfileMenu => "Switch profile",
        Key::ManageProfiles => "Manage profiles…",
        Key::BackToProfile | Key::BackTo => "Back to %s",
        Key::From | Key::FromDate => "From",
        Key::OcrNotInstalled => {
            "Text in images and scanned PDFs can't be read until Tesseract is installed (see %s). PDFs with text work without it."
        }
        Key::Upload => "Upload",
        Key::UploadDocuments => "Upload documents",
        Key::SearchDocuments => "Search documents by name or text content…",
        Key::TabAllDocuments => "All documents",
        Key::TabUnlinked => "Unlinked",
        Key::TabLinked => "Linked to transaction",
        Key::TabWithCount => "%1$s (%2$d)",
        Key::NoDocuments => "No documents",
        Key::NoMatches => "No matches",
        Key::UploadHint => "Upload receipts, invoices and statements (PDF, JPG, PNG, TIFF).",
        Key::TryAnotherSearch => "Try another search term.",
        Key::DeleteDocumentTitle => "Delete document?",
        Key::DeleteDocumentText => "%s and its stored copy will be deleted.",
        Key::NotLinked => "not linked",
        Key::TextExtracted => "Text extracted",
        Key::NoText => "No text",
        Key::ReExtractText => "Re-extract text",
        Key::LinkedTransaction => "Linked transaction",
        Key::SuggestedMatches => "Suggested matches (amount and date from the document)",
        Key::FindTransactionToLink => "Find a transaction to link…",
        Key::ExtractedText => "Extracted text",
        Key::ExtractedTextConfidence => "Extracted text (confidence %d%%)",
        Key::NoTextExtracted => "No text extracted.",
        Key::MatchPercent => "match %s",
        Key::PreviewOf => "Preview of %s",
        Key::Export => "Export",
        Key::ExportPdf => "Export PDF",
        Key::ExportReport => "Export report",
        Key::NothingToExport => "No bookings to export in %s",
        Key::BookingsCountTotal => "%1$d {1:booking|bookings} · %2$s",
        Key::TaxYear => "Tax year",
        Key::CustomRange => "Custom range",
        Key::PreviousYear => "Previous year",
        Key::NextYear => "Next year",
        Key::TabTaxSummary => "Tax summary",
        Key::TabMonthlyBreakdown | Key::PdfMonthlyBreakdown => "Monthly breakdown",
        Key::TabCategoryAnalysis => "Category analysis",
        Key::TabRecurring => "Recurring",
        Key::NoTransactionsIn => "No transactions in %s",
        Key::ReportsBuiltFrom => "Reports are built from the transactions of the selected period.",
        Key::TotalIncome | Key::PdfTotalIncome => "Total income",
        Key::TotalExpenses | Key::PdfTotalExpenses => "Total expenses",
        Key::NetIncome | Key::PdfNetIncome => "Net income",
        Key::TaxRelevantAmount => "Tax deductions",
        Key::TaxRelevantByCategory | Key::PdfTaxByCategory => "Tax-relevant expenses by category",
        Key::NoTaxRelevantExpenses => {
            "No tax-relevant expenses. Mark categories as tax-relevant in Settings (e.g. Arbeitsmittel, Spenden, Versicherungen)."
        }
        Key::CategoriesUsed => "Categories used",
        Key::AvgMonthlyExpenses => "Avg monthly expenses",
        Key::HighestMonth => "Highest month",
        Key::MonthlyIncomeExpenses => "Monthly income & expenses - %s",
        Key::ColMonth | Key::PdfMonth => "Month",
        Key::ColExpenses | Key::ChartExpenses => "Expenses",
        Key::ColNet => "Net",
        Key::ReportTotal => "Total",
        Key::SpendingByCategory => "Spending by category",
        Key::TopVendors => "Top recipients",
        Key::ToDate => "To",
        Key::Apply => "Apply",
        Key::NoRecurringFound => "No recurring bookings found",
        Key::RecurringHint => {
            "Detected from the last 13 months once something repeats at least three times (yearly: twice)."
        }
        Key::RecurringPayments => "Recurring payments",
        Key::CostPerYear => "Cost per year",
        Key::PerMonth => "Per month",
        Key::SubscriptionsTitle => "Subscriptions, contracts and regular income",
        Key::ColEvery => "Every",
        Key::ColNext => "Next",
        Key::ColPerYear => "Per year",
        Key::TimesSeen => "%dx seen",
        Key::Appearance => "Appearance",
        Key::DarkTheme => "Dark theme",
        Key::MenuFile => "File",
        Key::MenuView => "View",
        Key::MenuWindow => "Window",
        Key::MenuHelp => "Help",
        Key::MenuSettings => "Settings…",
        Key::MenuExit => "Exit",
        Key::MenuEdit => "Edit…",
        Key::MenuDetails => "Details",
        Key::SyncNow => "Sync now",
        Key::ShowTransactions => "Show transactions",
        Key::ShowAllFromPartner => "Show all from %s",
        Key::ShowInFinder => "Show in Finder",
        Key::ShowInExplorer => "Show in Explorer",
        Key::ShowInFolder => "Show in folder",
        Key::MenuNewTransaction => "New Transaction…",
        Key::MenuImportStatement => "Import Bank Statement…",
        Key::MenuMinimize => "Minimize",
        Key::MenuZoom => "Zoom",
        Key::MenuGitHub => "Finglass on GitHub",
        Key::MenuReportProblem => "Report a Problem…",
        Key::AboutTitle => "About Finglass",
        Key::AboutVersion => "Version %s",
        Key::AboutText => {
            "Your financial workspace: accounts, FinTS banking, documents and tax reports. Alpha software — not for use with real bank accounts."
        }
        Key::AboutTypeface => "Typeface: IBM Plex Sans (SIL Open Font License 1.1)",
        Key::Language => "Language",
        Key::Categories => "Categories",
        Key::AddCategory => "Add category",
        Key::ColourSwatch => "Colour %1$d of %2$d",
        Key::FixDuplicateColours => "Give each category its own colour",
        Key::NoCategoriesYet => "No categories yet.",
        Key::AddDefaultCategories => "Add default categories",
        Key::CategoryIsTransfer => "Transfer between own accounts",
        Key::CategoryIsTransferHint => {
            "Not counted as income or spending. Use it for credit-card payments only when the card's own bookings are imported."
        }
        Key::CategoryTransferLabel => "transfer",
        Key::CategorySplitMore => "%1$d more {1:category|categories}",
        Key::CategorySplitTransfers => "Not counted: transfers %s",
        Key::DefaultCategoryGroceries => "Groceries",
        Key::DefaultCategoryDrugstore => "Drugstore & household",
        Key::DefaultCategoryHousing => "Housing",
        Key::DefaultCategoryEnergy => "Energy",
        Key::DefaultCategoryPhoneInternetMedia => "Phone, internet & media",
        Key::DefaultCategoryInsurance => "Insurance",
        Key::DefaultCategoryLoans => "Loans & financing",
        Key::DefaultCategorySavings => "Savings & investments",
        Key::DefaultCategoryTransport => "Transport",
        Key::DefaultCategoryHealth => "Health",
        Key::DefaultCategoryClothing => "Clothing & shoes",
        Key::DefaultCategoryOnlineShopping => "Online shopping",
        Key::DefaultCategoryLeisure => "Leisure & clubs",
        Key::DefaultCategoryRestaurants => "Restaurants & cafés",
        Key::DefaultCategoryTaxes => "Taxes & fees",
        Key::DefaultCategoryCash => "Cash",
        Key::DefaultCategoryCreditCard => "Credit card",
        Key::DefaultCategoryOwnTransfers => "Own transfers",
        Key::DefaultCategorySalary => "Salary",
        Key::DefaultCategoryChildBenefit => "Child benefit",
        Key::DefaultCategoryInterestFees => "Interest & bank fees",
        Key::DefaultCategoryFamily => "Family",
        Key::DefaultCategoryChildrenSchool => "Children & school",
        Key::DefaultCategoryOtherIncome => "Other income",
        Key::BudgetAmount => "Budget %s",
        Key::AutoCategorizationRules => "Auto-categorization rules",
        Key::AddRule => "Add rule",
        Key::RulesHint => {
            "A rule's pattern is a case-insensitive regular expression matched against partner name and purpose, or only one of them, for expenses, income or both. Higher priority wins; on equal priority the older rule wins."
        }
        Key::NoRulesYet => "No rules yet. Add one, or import a rules file.",
        Key::Prio => "prio %d",
        Key::Manual => "manual",
        Key::RuleSearchIn => "Search in",
        Key::RuleFieldAny => "Partner and purpose",
        Key::RuleFieldPartner => "Partner name only",
        Key::RuleFieldPurpose => "Purpose only",
        Key::RuleAppliesTo => "Applies to",
        Key::RuleDirectionAny => "Expenses and income",
        Key::RuleDirectionExpense => "Expenses only",
        Key::RuleDirectionIncome => "Income only",
        Key::RuleCategoryLabel => "Category: %s",
        Key::ImportRules => "Import rules…",
        Key::ExportRules => "Export rules…",
        Key::RuleImportTitle => "Import rules",
        Key::RuleImportCategorize => "Categorize %d uncategorized {1:booking|bookings} now",
        Key::RuleImportNewRules => "%d new {1:rule|rules}",
        Key::RuleImportCategories => "%d {1:category|categories} to create",
        Key::RuleImportDuplicates => "%d {1:duplicate|duplicates} skipped",
        Key::RuleImportConflicts => "%d {1:conflict|conflicts} skipped",
        Key::RuleImportErrors => "%d {1:invalid row|invalid rows}",
        Key::RuleImportConflictLine => "Line %d: %s → %s (existing: %s)",
        Key::RuleImportErrorLine => "Line %d: %s",
        Key::MessageRulesExported => "%d {1:rule|rules} exported",
        Key::MessageRulesExportSkipped => {
            "%1$d {1:rule|rules} exported, %2$d skipped (tab or line break in pattern or category)"
        }
        Key::MessageRulesImported => {
            "%1$d {1:rule|rules} and %2$d {2:category|categories} added, %3$d {3:booking|bookings} categorized"
        }
        Key::RuleFileErrorColumns => "Expected 6 columns, found %d",
        Key::RuleFileErrorCategory => "Category is empty or longer than 100 characters",
        Key::RuleFileErrorPattern => "Invalid pattern: %s",
        Key::RuleFileErrorPatternLength => "Pattern longer than 1000 characters",
        Key::RuleFileErrorSearchIn => "Unknown search_in value: %s (any, partner, purpose)",
        Key::RuleFileErrorAppliesTo => "Unknown applies_to value: %s (any, expense, income)",
        Key::RuleFileErrorPriority => "Priority is not a whole number: %s",
        Key::RuleFileErrorAutoApply => "auto_apply must be true or false: %s",
        Key::ErrorRuleFileHeader => "Not a Finglass rule file: the header line is missing or wrong",
        Key::ErrorRuleFileTooLarge => "Rule file too large (at most 1 MB and 5000 rules)",
        Key::ErrorRuleFileEncoding => "The rule file is not valid UTF-8 text",
        Key::DataAndOcr => "Data & OCR",
        Key::Database => "Database",
        Key::TesseractAvailable => "Tesseract OCR available (German + English)",
        Key::TesseractMissing => "Tesseract not found. Install with: brew install tesseract tesseract-lang",
        Key::BackupAndRestore => "Backup & restore",
        Key::BackupHint => {
            "A backup contains the database, all stored documents and the online-banking connection files."
        }
        Key::CreateBackup => "Create backup",
        Key::RestoreFromBackup => "Restore from backup…",
        Key::RestoreWaiting => "A restore is waiting. Restart Finglass to apply it.",
        Key::CancelRestore => "Cancel restore",
        Key::BackupRestoredAt => "Backup restored at this start. Your previous data is in %s",
        Key::AuditLogLatest => "Audit log (latest 100)",
        Key::AuditLogShort => "Audit log",
        Key::AuditLogEmpty => "Nothing recorded yet.",
        Key::RestoreFileTitle => "Restore %s?",
        Key::RestoreCreated => "Created %s · %d {2:transaction|transactions} · %d {3:document|documents}",
        Key::RestoreExplanation => {
            "On the next start, all current data is replaced by this backup. The current data is moved to a pre-restore folder, not deleted; it stays there, unencrypted documents included, until you delete it yourself."
        }
        Key::PrepareRestore => "Prepare restore",
        Key::DeleteCategoryTitle => "Delete category %s?",
        Key::DeleteCategoryText => "Its transactions become uncategorized and its rules are deleted.",
        Key::DeleteCategoryUsage => "Used by %d {1:booking|bookings}.",
        Key::DeleteRuleTitle => "Delete rule %s?",
        Key::DeleteRuleText => "Bookings it already categorized keep their category.",
        Key::EditCategory => "Edit category",
        Key::MonthlyBudgetOptional => "Monthly budget (€, optional)",
        Key::TaxRelevantHint => "Tax-relevant (Werbungskosten, Sonderausgaben, …)",
        Key::BudgetNotANumber => "Budget is not a number",
        Key::EditRule => "Edit rule",
        Key::PatternExample => "Pattern (e.g. rewe|edeka|aldi)",
        Key::Priority => "Priority",
        Key::ApplyAutomatically => "Apply automatically",
        Key::PriorityMustBeNumber => "Priority must be a whole number",
        Key::SaveBackup => "Save backup",
        Key::RestoreBackup => "Restore backup",
        Key::PinFor => "PIN for %s",
        Key::PinExplanation => "Online banking PIN for user %s.",
        Key::RememberPinForSyncs => "Remember PIN for syncs",
        Key::RememberPinExplanation => "Kept in your system's secure credential store. Transfers still ask for it.",
        Key::Pin => "PIN",
        Key::ChooseTanProcedure => "Choose TAN procedure",
        Key::ChooseTanMedium => "Choose TAN medium",
        Key::TanRequired => "TAN required",
        Key::TanDefaultMessage => "Enter the TAN shown by your TAN procedure.",
        Key::TanChallenge => "TAN challenge",
        Key::ConfirmInBankingApp => "Confirm in your banking app",
        Key::IConfirmedIt => "I confirmed it",
        Key::MessageFromBank => "Message from your bank",
        Key::NotEnoughData => "Not enough data",
        Key::Max => "max",
        Key::BalanceHistory => "Balance history",
        Key::AxisNotFromZero => "Scale doesn't start at zero",
        Key::MessageAdded => "Added %s",
        Key::MessageAccountAdded => "Account added",
        Key::MessageAccountDeleted => "Account deleted",
        Key::MessageDeletedForgotten => "%d deleted {1:booking|bookings} can be imported again",
        Key::MessageNoDeletedRemembered => "No deleted bookings are remembered for this account",
        Key::MessageBankRemoved => "Bank removed",
        Key::MessageConnectionSaved => "Connection settings saved",
        Key::SyncConnecting => "Connecting…",
        Key::MessageTransactionAdded => "Transaction added",
        Key::MessageRuleCreated => "Rule created and applied to %d uncategorized {1:transaction|transactions}",
        Key::MessageDefaultCategoriesAdded => "%d default categories added",
        Key::MessageDefaultCategoriesNone => "All default categories already exist",
        Key::MessageColoursFixed => "%d {1:category|categories} recoloured",
        Key::MessageColoursAlreadyDistinct => "Every category already has its own colour",
        Key::MessageAutoCategorized => "Categorized %d of %d uncategorized {2:transaction|transactions}",
        Key::MessageExportedCount => "Exported %d {1:transaction|transactions} to %s",
        Key::MessageBackupSaved => "Backup saved to %s (%d {2:transaction|transactions}, %d {3:document|documents})",
        Key::MessageRestorePrepared => {
            "Restore prepared. Restart Finglass to finish; your current data will be kept in a pre-restore folder."
        }
        Key::MessageRestoreCancelled => "Pending restore cancelled",
        Key::MessageProfileMoved => "Profile moved to %s",
        Key::MessageSavedTo => "Saved %s",
        Key::PinNotSaved => "The PIN could not be saved in the system's secure credential store.",
        Key::MessageUploaded => "Uploaded %d {1:file|files}, text extracted from %d",
        Key::UploadProgress => "Uploading %1$d of %2$d…",
        Key::MessageDocumentLinked => "Document linked",
        Key::MessageDocumentDeleted => "Document deleted",
        Key::MessageNoTextExtracted => "No text could be extracted",
        Key::BankNameRequired => "Bank name is required",
        Key::BlzMustBe8Digits => "BLZ must be 8 digits",
        Key::ErrorConnectionBusy => "Wait until the sync of this bank has finished.",
        Key::ErrorDeleteWhileBusy => "Wait until the running sync or import has finished.",
        Key::IbanRequired => "IBAN is required",
        Key::IbanChecksumFailed => "IBAN is not valid (checksum failed)",
        Key::AccountIbanExists => "An account with this IBAN already exists",
        Key::ErrorAmountNotZero => "Amount must not be zero",
        Key::DuplicateTransactionTitle => "Add the same booking again?",
        Key::DuplicateTransactionText => {
            "This account already has a booking on %s for %s with the same partner and purpose. Add a second one, for example for a repeated purchase?"
        }
        Key::AddAnyway => "Add anyway",
        Key::ErrorExportFailed => "Export failed",
        Key::ErrorNameRequired => "Name is required",
        Key::ErrorColorFormat => "Color must look like #3BC7B0",
        Key::ErrorCategoryExists => "Category already exists",
        Key::ErrorPatternRequired => "Pattern is required",
        Key::ErrorInvalidRegex => "Invalid regular expression: %s",
        Key::ErrorPdfExportFailed => "PDF export failed",
        Key::ErrorCsvExportFailed => "CSV export failed",
        Key::ErrorSyncFromDateFuture => "The start date must not be in the future",
        Key::ErrorAccountTypeInvalid => "Unknown account type",
        Key::ErrorBankConnectionNotFound => "Bank connection not found",
        Key::ErrorAccountNotFound => "Account not found",
        Key::ErrorSyncFailed => "Sync failed",
        Key::ErrorBankRejected => "The bank rejected the request",
        Key::ErrorBankRejectedStatements => "The bank rejected the statement request",
        Key::StatementsNotSupported => "Statements not supported for this account",
        Key::ErrorNoFintsServer => "No FinTS server known for BLZ %s. Enter the bank's FinTS URL.",
        Key::ErrorBadServerUrl => "Invalid FinTS server URL",
        Key::ErrorEnterUserId => "Enter your online banking user ID (Anmeldename/Legitimations-ID) for %s.",
        Key::ErrorConnectionNotFound => "Bank connection %d not found",
        Key::ErrorCancelled => "Cancelled",
        Key::ErrorCancelledByUser => "Cancelled by user",
        Key::ErrorDialogFailed => "FinTS dialog failed",
        Key::HintNeedsAccount => "Add an account first",
        Key::HintNeedsCategory => "Add a category first",
        Key::HintNothingToExport => "No transactions in this view to export",
        Key::ErrorBankUnreachable => {
            "Could not reach the bank's FinTS server. Check the server URL and your internet connection."
        }
        Key::ErrorServerHttpStatus => {
            "The bank's server answered with HTTP %d. Check that FinTS/HBCI access is activated in your online banking and that the bank code (BLZ) and server URL of this connection are right, or try again later."
        }
        Key::ErrorServerNotFints => {
            "The bank's server sent an answer that isn't FinTS (%s). Check the FinTS server URL in the connection settings. If it is right, the bank may be blocking the request: try again later or capture a protocol trace in Settings."
        }
        Key::ErrorBankRejectedPin => "The bank rejected the PIN",
        Key::ErrorPassportReset => {
            "Finglass could not reset its stored bank registration. Remove and re-add this bank."
        }
        Key::ProtocolTraceTitle => "FinTS protocol trace",
        Key::ProtocolTraceHint => {
            "Collect a log of the next online-banking dialogs to help diagnose connection problems. PIN, TAN, user id and account numbers are masked, but the log contains your bank's full answers: names, purposes, amounts and balances. Read it before you share it. Traces stay in memory until you save them and are never sent automatically."
        }
        Key::ProtocolTraceEnable => "Collect FinTS protocol trace",
        Key::ProtocolTraceSave => "Save protocol trace",
        Key::ProtocolTraceAvailable => "A trace is ready to save.",
        Key::ProtocolTraceNone => {
            "No trace captured yet. Enable collection, reproduce the problem (e.g. run a sync), then save it here."
        }
        Key::MessageProtocolTraceSaved => "Protocol trace saved to %s",
        Key::ErrorNoProtocolTrace => {
            "No protocol trace captured yet. Enable collection and reproduce the problem first."
        }
        Key::ProgressConnecting => "Connecting to bank…",
        Key::ProgressSavingAccount => "Saving %s…",
        Key::SyncSummary => "Synced %d {1:account|accounts}",
        Key::SyncSummaryNew | Key::SyncSummaryTransactions => ": %d new {1:transaction|transactions}",
        Key::SyncSummaryCategorized | Key::ImportSummaryAuto => ", %d auto-categorized",
        Key::SyncSummaryDuplicates | Key::ImportSummaryKnown => ", %d already known",
        Key::SyncSummaryHistoryFrom => " History requested from %s.",
        Key::BalanceNotProvided => {
            "…%s: the bank sends no balance for this account (%s). Enter it with “Edit balance”; a loan as a negative amount."
        }
        Key::BalanceNotProvidedNoCode => {
            "…%s: the bank sends no balance for this account. Enter it with “Edit balance”; a loan as a negative amount."
        }
        Key::SyncGapNotFetched => {
            "Bookings from %s to %s were not fetched: Finglass fetches at most the last %d days. Use “Fetch history from…” to get them (the bank may ask for a TAN)."
        }
        Key::SyncAccountWithoutIban => "%s: the bank lists this account without an IBAN; Finglass can't store it yet.",
        Key::SyncBalanceOtherCurrency => {
            "…%s: the bank reported the balance in %s, but the account is kept in %s; the balance was not changed."
        }
        Key::SyncCurrencyCorrected => {
            "…%s: the account's currency was corrected from %s to %s, as the bank reports its balance in that currency and states no account currency."
        }
        Key::PendingHeader => "Pending (not yet booked) · as of %s",
        Key::ImportSummary => "Imported %d {1:transaction|transactions}",
        Key::ImportSummaryFiles => "Imported %d {1:transaction|transactions} from %d files",
        Key::ImportSummaryProbable => ", %d probable {1:duplicate|duplicates} of existing bookings skipped",
        Key::ImportSummaryWrongCurrency => ", %d in another currency than the account skipped",
        Key::ImportSummaryBalance => ". Balance set to %s",
        Key::ImportSummaryDeleted => ", %d deleted earlier, skipped",
        Key::ErrorFileNotFound => "File not found: %s",
        Key::ErrorFileTooLarge => "File is too large",
        Key::ErrorImportSeveralAccounts => {
            "This file holds bookings for %d accounts (%s). Export one file per account and import them one at a time."
        }
        Key::ErrorXlsxTooLarge => "The Excel file is too large or damaged.",
        Key::ErrorXlsxUnreadable => "The Excel file could not be read.",
        Key::ErrorFileDoesNotExist => "File does not exist: %s",
        Key::ErrorUnsupportedFileType => "Unsupported file type: %s",
        Key::ErrorUnreadableImage => "Unreadable image: %s",
        Key::ErrorDocumentTooLarge => "%s is too large (the limit is %d MB)",
        Key::ErrorImageTooLarge => "%s has too many pixels to read",
        Key::ReportUnknownCategory => "Unknown",
        Key::ReportTitleTax => "Finglass Tax Report %d",
        Key::ReportTitleRange => "Finglass Report %s",
        Key::PdfGenerated => "Generated %s",
        Key::PdfSummary => "Summary",
        Key::PdfTaxRelevantExpenses => "Tax-relevant expenses",
        Key::PdfAllAmountsIn => {
            "All amounts in %s. %d {2:booking|bookings} in other currencies {2:is|are} not included."
        }
        Key::PdfNoTaxRelevant => "No categories are marked as tax-relevant.",
        Key::PdfIncomeExpensesNet => "Income / Expenses / Net",
        Key::PdfExpensesByCategory => "Expenses by category",
        Key::PdfTaxTransactions => "Tax-relevant transactions",
        Key::ColCurrency => "Currency",
        Key::ChartOther => "Other",
        Key::ChartSoFar => "so far",
        Key::ChartPartialMonthNote => "Pale: month so far",
        Key::FlickerSpeed => "Speed",
        Key::FlickerSize => "Size",
        Key::FlickerHint => "Hold the TAN generator against the bars so its arrows line up with the triangles.",
        Key::TanLabel => "TAN",
        Key::AppLogo => "Finglass logo",
        Key::EditCategoryNamed => "Edit category: %s",
        Key::EditRuleNamed => "Edit rule: %s",
        Key::OpenPreviewNamed => "Open preview: %s",
        Key::BankOfflineHint => "Offline: import statements in Transactions.",
        Key::IbanOrCardLast4 => "IBAN or last 4 digits of the card",
        Key::CardLast4Only => "Enter only the last 4 digits of the card.",
        Key::ErrorBankOffline => "This bank is offline: nothing is sent to the bank.",
        Key::BankOnlineMode => "Online banking (FinTS)",
        Key::BankOfflineMode => "Offline (import statements only)",
        Key::AccountsWithoutBank => "Accounts without a bank",
        Key::ReapplyRules => "Re-apply rules to shown bookings",
        Key::ReapplyRulesTitle => "Re-apply rules?",
        Key::ReapplyRulesSummary => "New category for %1$d of %2$d shown {2:booking|bookings}.",
        Key::ReapplyRulesWarning => {
            "Categories you set by hand are overwritten too. Narrow the list with the filters first if needed. You can undo this afterwards from the menu."
        }
        Key::ReapplyRulesConfirm => "Change %d",
        Key::UndoReapplyRules => "Undo re-applied rules",
        Key::MessageReapplyNothing => "No shown booking would get a different category",
        Key::MessageReapplied => "%d {1:booking|bookings} recategorized",
        Key::MessageReapplyUndone => "%d {1:booking|bookings} restored",
        Key::HintNothingShown => "No bookings shown",
        Key::SelectedCount => "%d selected",
        Key::DeleteSelected => "Delete…",
        Key::ClearSelection => "Clear selection",
        Key::DeleteSelectedCount => "Delete %d selected",
        Key::SelectAll => "Select all",
        Key::DeleteTransactionsTitle => "Delete %d {1:transaction|transactions}?",
        Key::DeleteSyncedHint => {
            "Deleted bookings are remembered, so a later import or bank sync won't add them again. To allow that, use “Allow deleted bookings again” in the account's menu on the Accounts screen. Linked receipts are kept and only unlinked."
        }
        Key::MessageTransactionsDeleted => "Deleted %d {1:transaction|transactions}",
        Key::TaxAutomatic => "Automatic",
        Key::TaxRelevantChoice => "Tax relevant",
        Key::TaxNotRelevantChoice => "Not tax relevant",
        Key::TaxSetByYou => "%s · set by you",
        Key::TaxFromRule => "%1$s · from rule “%2$s”",
        Key::TaxFromCategory => "%1$s · from category “%2$s”",
        Key::TaxMenu | Key::RuleTaxMarkerYes => "Tax",
        Key::MarkTaxRelevant => "Mark as tax relevant",
        Key::MarkNotTaxRelevant => "Mark as not tax relevant",
        Key::ResetTaxAutomatic => "Reset to automatic",
        Key::RuleTaxAsCategory => "As the category",
        Key::RuleTaxMarkerNo => "No tax",
        Key::MessageTaxOverrideSet => "Tax status changed for %d {1:booking|bookings}",
        Key::MessageCategorySet => "Category changed for %d {1:booking|bookings}",
        Key::TabTaxBookings => "Tax bookings",
        Key::TaxIncomeTotal => "Tax-relevant income",
        Key::TaxDeductionTotal | Key::TaxSectionDeductions => "Deductions",
        Key::TaxReceipts => "Receipts",
        Key::ReceiptsMissing => "%d missing",
        Key::ReceiptsComplete => "All there",
        Key::TaxSuggestionsTitle => "Possibly tax relevant: %1$d {1:booking|bookings}",
        Key::TaxSuggestionsHint => {
            "Matched by keyword. Accept marks the booking tax relevant; dismiss hides the suggestion for good. The keywords are editable in Settings."
        }
        Key::MatchesKeyword => "Matches “%s”",
        Key::NoReceipt => "No receipt",
        Key::MissingReceiptsOnly => "Missing receipts only",
        Key::ShowAllCount => "Show all (%d)",
        Key::NoTaxBookingsIn => "No tax-relevant bookings in %s",
        Key::TaxBookingsHowTo => {
            "Mark a category as tax relevant in Settings → Categories, give a rule a Tax option, or mark bookings in the transaction list."
        }
        Key::TaxSuggestionKeywords => "Tax suggestions",
        Key::TaxKeywordsCount => "%1$d {1:keyword|keywords}",
        Key::ResetToDefaults => "Reset to defaults",
        Key::Accept => "Accept",
        Key::ExportExcel => "Export Excel",
        Key::Print => "Print…",
        Key::OpenTaxBookings => "Open tax bookings",
        Key::XlsxSheetOverview => "Overview",
        Key::XlsxSheetBookings | Key::TaxColBookings => "Bookings",
        Key::TaxExportTitle => "Tax-relevant bookings %s",
        Key::TaxColSection => "Section",
        Key::TaxColWithReceipt => "With receipt",
        Key::TaxColReceipt => "Receipt",
        Key::TaxColSource => "Source",
        Key::Yes => "Yes",
        Key::No => "No",
        Key::PdfPageOf => "Page %1$d of %2$d",
        Key::MessagePrintSent => "Sent to the printer",
        Key::ErrorTaxExportFailed => "The export of the tax report failed",
        Key::ErrorPrintFailed => "Printing failed",
        Key::ComingSoon => "This screen is coming in a later milestone.",
        Key::Security => "Security",
        Key::ThemeSystem => "System",
        Key::ThemeLight => "Light",
        Key::ThemeDark => "Dark",
        Key::Theme => "Theme",
        Key::ErrorFinDeskNotFound => "No FinDesk database was found in the selected folder.",
        Key::ErrorFinDeskWrongPassphrase => "The FinDesk passphrase is wrong.",
        Key::ErrorFinDeskNotFinDesk => "This database is not a FinDesk database.",
        Key::ErrorFinDeskTargetNotEmpty => "The Finglass profile already contains data. Import into an empty profile.",
        Key::FinDeskImportSummary => {
            "Imported %d account groups, %d bank connections, %d accounts, %d categories, %d rules, \
             %d transactions, %d deleted bookings, %d pending bookings, %d documents, %d OCR texts and \
             %d settings. Skipped %d rows, %d documents with a missing file and %d refused documents. \
             Left %d payment orders behind."
        }
        Key::ErrorNotEncrypted => "This data folder contains an unencrypted database, which Finglass cannot open.",
        Key::ErrorDatabaseTooNew => "This database was created by a newer version of Finglass.",
        Key::ErrorAlreadyRunning => "Finglass is already running with this data folder.",
        Key::ErrorStartupFailed => {
            "Finglass could not prepare this profile's data at startup. Quit Finglass and try again."
        }
        Key::ErrorOnlineSyncUnavailable => "Online sync is not available in this build yet.",
        Key::ErrorServerUrlRequired => "The FinTS server URL is required",
        Key::ErrorServerUrlMustBeHttps => "The FinTS server URL must start with https://",
        Key::ErrorUnexpected => "an unexpected error",
    }
}
