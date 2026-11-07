//! DE texts.

use super::Key;

pub(super) fn text(key: Key) -> &'static str {
    match key {
        Key::Cancel => "Abbrechen",
        Key::Save => "Speichern",
        Key::Add => "Hinzufügen",
        Key::Delete => "Löschen",
        Key::Close | Key::Dismiss => "Schließen",
        Key::Ok => "OK",
        Key::Open | Key::OpenProfile => "Öffnen",
        Key::Back => "Zurück",
        Key::More => "Mehr",
        Key::ClearSearch => "Suche löschen",
        Key::Uncategorized => "Ohne Kategorie",
        Key::TaxRelevant => "steuerrelevant",
        Key::Tax | Key::TaxMenu | Key::RuleTaxMarkerYes => "Steuer",
        Key::ToggleTheme => "Design umschalten",
        Key::Link => "Verknüpfen",
        Key::Unlink => "Trennen",
        Key::OpenDetails => "Details öffnen",
        Key::ChangeCategory => "Kategorie ändern: %s",
        Key::OpenAccount => "Konto öffnen",
        Key::OpenDocument => "Dokument öffnen",
        Key::ViewPendingTasks => "Offene Aufgaben anzeigen",
        Key::EnterValidDate | Key::ErrorSyncFromDateInvalid => "Gültiges Datum eingeben (tt.mm.jjjj)",
        Key::EnterValidAmount => "Gültigen Betrag eingeben",
        Key::EnterValidBalance => "Gültigen Saldo eingeben",
        Key::InvalidDateRange => "Das Enddatum muss nach dem Startdatum liegen",
        Key::PatternInvalid => "Gültigen regulären Ausdruck eingeben",
        Key::OpticalTanCode => "Optischer TAN-Code für einen TAN-Generator. Halten Sie den Generator an die Balken.",
        Key::WindowTitle => "Finglass – Ihr Finanz-Arbeitsplatz",
        Key::NavDashboard | Key::XlsxSheetOverview => "Übersicht",
        Key::NavAccounts => "Konten",
        Key::NavTransactions | Key::Transactions | Key::SummaryCount | Key::StatTransactions => "Umsätze",
        Key::NavDocuments | Key::DocumentsFolder => "Dokumente",
        Key::NavReports => "Berichte",
        Key::NavSettings => "Einstellungen",
        Key::TypeChecking => "Girokonto",
        Key::TypeSavings => "Sparkonto",
        Key::TypeCreditCard => "Kreditkarte",
        Key::TypeLoan => "Kredit",
        Key::TypeDepot => "Depot",
        Key::PeriodThisMonth => "Dieser Monat",
        Key::PeriodLastMonth => "Letzter Monat",
        Key::PeriodLast3Months => "Letzte 3 Monate",
        Key::PeriodThisYear => "Dieses Jahr",
        Key::PeriodLastYear => "Letztes Jahr",
        Key::PeriodAllTime => "Gesamter Zeitraum",
        Key::PeriodCustomRange => "Eigener Zeitraum…",
        Key::DirectionAll => "Alle",
        Key::DirectionIncome => "Einnahmen",
        Key::DirectionExpenses => "Ausgaben",
        Key::DirectionAnyLabel => "Ein- und Ausgänge",
        Key::CadenceWeekly => "Wöchentlich",
        Key::CadenceMonthly => "Monatlich",
        Key::CadenceQuarterly => "Vierteljährlich",
        Key::CadenceYearly => "Jährlich",
        Key::WelcomeTitle => "Willkommen bei Finglass",
        Key::AddAccount => "Konto hinzufügen",
        Key::AvailableBalance => "Verfügbar",
        Key::LoansSubtitle => "Kredite %1$s · netto %2$s",
        Key::IncomeThisMonth => "Einnahmen diesen Monat",
        Key::ExpensesThisMonth => "Ausgaben diesen Monat",
        Key::PendingTasks => "Offene Aufgaben",
        Key::PendingSubtitle => "%d ohne Kategorie · %d {2:unverknüpftes Dokument|unverknüpfte Dokumente}",
        Key::ExpensesByCategory => "Ausgaben nach Kategorie (dieser Monat)",
        Key::NoExpensesThisMonth => "Keine Ausgaben in diesem Monat",
        Key::IncomeVsExpenses => "Einnahmen vs. Ausgaben (6 Monate)",
        Key::Upcoming30Days => "Anstehend in den nächsten 30 Tagen",
        Key::AmountVaries => "Betrag variabel",
        Key::BudgetsThisMonth => "Budgets diesen Monat",
        Key::SpentOfLimit => "%s von %s",
        Key::OverBudget => "%s über Budget",
        Key::LeftBudget => "%s übrig",
        Key::TaxDeductibleYtd => "Steuerlich absetzbare Ausgaben (Jahr bis dato)",
        Key::TaxDeductibleHint => {
            "Markieren Sie Kategorien in den Einstellungen als steuerrelevant, um absetzbare Ausgaben zu verfolgen."
        }
        Key::RecentTransactions => "Letzte Umsätze",
        Key::ViewAll => "Alle anzeigen",
        Key::NoTransactionsYet => "Noch keine Umsätze",
        Key::GettingStartedIntro => "In drei Schritten zur ersten Übersicht.",
        Key::StepAddBank | Key::AddBank => "Bank hinzufügen",
        Key::StepAddBankHint => "Bankleitzahl und Online-Banking-Zugang eingeben.",
        Key::StepSync => "Salden und Umsätze abrufen",
        Key::StepSyncHint => {
            "Die Synchronisierung fragt Ihre Bank einmal nach der PIN; Salden lassen sich auch von Hand eintragen."
        }
        Key::StepCategorize => "Umsätze zuordnen",
        Key::StepCategorizeHint => "Kategorien und Regeln gruppieren Ausgaben und markieren, was steuerlich zählt.",
        Key::OpenSettings => "Einstellungen öffnen",
        Key::NothingPending => "Nichts zu tun",
        Key::NoBookingsSixMonths => "Keine Umsätze in den letzten sechs Monaten",
        Key::PreviousMonth => "Voriger Monat",
        Key::NextMonth => "Nächster Monat",
        Key::CurrentMonth => "Aktueller Monat",
        Key::IncomeInMonth => "Einnahmen im %s",
        Key::ExpensesInMonth => "Ausgaben im %s",
        Key::BudgetsInMonth => "Budgets im %s",
        Key::NoExpensesInMonth => "Keine Ausgaben im %s",
        Key::ExpensesByCategoryInMonth => "Ausgaben nach Kategorie im %s",
        Key::NoBookingsSixMonthsTo => "Keine Umsätze in den sechs Monaten bis %s",
        Key::AvailableAsOf => "Verfügbar am %s",
        Key::CarryForwardFrom => "Übertrag aus %1$s: %2$s",
        Key::OtherMovements => "Umbuchungen und Kredite: %s",
        Key::AccountsTotal => "Gesamt %s auf %d {2:Konto|Konten}",
        Key::AccountsTotalLabel => "Gesamt über %d {1:Konto|Konten}",
        Key::AccountsNetLabel => "Netto über %d {1:Konto|Konten}",
        Key::AccountsLoansLabel => "Davon Kredite",
        Key::AccountCount => "%d {1:Konto|Konten}",
        Key::NoBanksYet => "Noch keine Banken",
        Key::NoBanksDescription => {
            "Fügen Sie Ihre Bank hinzu und danach deren Konten. Salden und Umsätze können manuell erfasst werden, bis die Online-Synchronisation eingerichtet ist."
        }
        Key::BlzPrefix => "BLZ %s",
        Key::BicPrefix => "BIC %s",
        Key::SyncedPrefix => "synchronisiert %s",
        Key::Syncing => "Synchronisiere…",
        Key::Sync => "Sync",
        Key::SyncAll => "Alle synchronisieren",
        Key::SyncAllProgress => "Synchronisiere %d von %d: %s",
        Key::IncludeInSyncAll => "In „Alle synchronisieren“ einbeziehen",
        Key::NotInSyncAll => "nicht in „Alle synchronisieren“",
        Key::SyncAllNothingIncluded => "Keine Verbindung ist in „Alle synchronisieren“ einbezogen",
        Key::SyncAllStopped => "Alle synchronisieren gestoppt: %s wurde abgebrochen.",
        Key::FetchHistory => "Verlauf ab Datum abrufen…",
        Key::FetchHistoryFor => "Verlauf abrufen für %s",
        Key::FetchHistoryDate => "Startdatum",
        Key::FetchHistoryHint => {
            "Bereits importierte Buchungen werden übersprungen. Banken begrenzen, wie weit sie zurückreichen, und verlangen für mehr als 90 Tage oft eine TAN."
        }
        Key::Fetch => "Abrufen",
        Key::Reset => "Zurücksetzen",
        Key::NextSyncFrom => "nächster Abruf ab %s",
        Key::ConnectionSettings => "Verbindungseinstellungen",
        Key::RenameConnection => "Umbenennen",
        Key::ConnectionName | Key::ColName | Key::Name => "Name",
        Key::ConnectionNameHint => "Nur in Finglass sichtbar, z. B. „DKB Geschäftlich“. Der Bank-Login bleibt gleich.",
        Key::AddAccountShort | Key::DetailAccount | Key::ColAccount => "Konto",
        Key::RemoveBank => "Bank entfernen",
        Key::NoAccountsYet => "Noch keine Konten",
        Key::EditBalance => "Saldo bearbeiten",
        Key::DeleteAccount => "Konto löschen",
        Key::AllowDeletedAgain => "Gelöschte Buchungen wieder zulassen",
        Key::BlzDigits => "BLZ (8 Stellen)",
        Key::BankName => "Bankname",
        Key::BicOptional => "BIC (optional)",
        Key::OnlineUserIdOptional => "Online-Banking-Benutzerkennung (optional)",
        Key::NoFintsServer => "kein FinTS-Server hinterlegt",
        Key::OnlineSyncAvailable => "Online-Sync verfügbar",
        Key::BankSearchHint => "Bank suchen: Name, BLZ, BIC oder IBAN",
        Key::BankSearchNoMatch => "Keine Bank gefunden – Daten unten eingeben",
        Key::BankManualEntry => "oder Daten manuell eingeben",
        Key::BankUnknownBlz => "BLZ %s ist nicht im Bankverzeichnis",
        Key::BankFintsProposed => "FinTS-Server: %s",
        Key::BankUseSuggestion => "Diese Bank verwenden",
        Key::AddAccountAt => "Konto bei %s hinzufügen",
        Key::Iban => "IBAN",
        Key::BalanceNotANumber => "Saldo ist keine Zahl",
        Key::UpdateBalance => "Saldo aktualisieren",
        Key::OnlineBankingFor => "Online-Banking für %s",
        Key::UserIdLogin => "Benutzerkennung (Anmeldename / Legitimations-ID)",
        Key::FintsServerUrl => "FinTS-Server-URL",
        Key::LeaveEmptyToUse => "Leer lassen, um %s zu verwenden",
        Key::AskBankForFints => "Fragen Sie Ihre Bank nach der FinTS-Adresse (PIN/TAN)",
        Key::RememberedTan => "Gemerkte TAN-Verfahren: %s",
        Key::PinNeverStored => {
            "Ihre PIN wird bei jedem Sync abgefragt und kann auf diesem System nicht gespeichert werden."
        }
        Key::PinSavedForSyncs => "PIN für Syncs gespeichert",
        Key::ForgetPin => "PIN vergessen",
        Key::PinAskedEverySync => {
            "Ihre PIN wird bei jedem Sync abgefragt. Aktivieren Sie im PIN-Dialog „PIN für Syncs merken“, um sie im sicheren Anmeldedatenspeicher Ihres Systems zu speichern."
        }
        Key::DaysCount => "%d Tage",
        Key::OneYear => "1 Jahr",
        Key::LastDaysSummary => "Letzte %d Tage: Eingang %s · Ausgang %s",
        Key::RecentBookings => "Letzte Buchungen",
        Key::NoneInPeriod => "Keine in diesem Zeitraum",
        Key::HistoryReconstructed => {
            "Der Verlauf wird aus dem aktuellen Saldo und den Finglass bekannten Buchungen rekonstruiert."
        }
        Key::AllTransactions => "Alle Umsätze",
        Key::DeleteAccountTitle => "Konto löschen?",
        Key::DeleteAccountText => {
            "Dadurch werden %s und alle zugehörigen Umsätze gelöscht. Verknüpfte Dokumente bleiben erhalten, werden aber getrennt."
        }
        Key::RenameAccount => "Konto umbenennen",
        Key::ChangeAccountType => "Kontoart ändern…",
        Key::AccountTypeLabel => "Kontoart",
        Key::LoanBalanceHint => "Tragen Sie die Restschuld als negativen Betrag ein, z. B. -12.000,00.",
        Key::AccountName => "Kontoname",
        Key::AccountNameHint => "Nur in Finglass sichtbar. Leer lassen, um „%s“ anzuzeigen.",
        Key::RemoveBankTitle => "%s entfernen?",
        Key::RemoveBankText => "Dadurch werden die Bank, alle Konten und deren Umsätze gelöscht.",
        Key::CentralBlzNote => {
            "Das Online-Banking dieser Bank nutzt für alle Filialen die Bankleitzahl %1$s, daher verwendet Finglass sie statt %2$s."
        }
        Key::CentralBlzHint => "Das Online-Banking dieser Bank nutzt die Bankleitzahl %s.",
        Key::UseBlz => "%s verwenden",
        Key::BlzNotInDirectory => "Nicht im Bankverzeichnis. Prüfen Sie die Bankleitzahl.",
        Key::BlzOtherBank => "Diese Bankleitzahl gehört zu %s, nicht zur Bank dieser Verbindung.",
        Key::LoginResetNote => {
            "Wenn Sie Bankleitzahl, Benutzerkennung oder Server-URL ändern, setzt Finglass die Anmeldung bei der Bank zurück. Die nächste Synchronisierung meldet Finglass neu an und fragt eventuell nach einer TAN."
        }
        Key::Import => "Importieren",
        Key::AutoCategorize => "Auto-kategorisieren",
        Key::ExportCsv => "CSV exportieren",
        Key::SearchPartnerPurpose => "Partner oder Verwendungszweck suchen…",
        Key::AllAccounts => "Alle Konten",
        Key::TransactionsSummary => "%d {1:Umsatz|Umsätze} · Eingang %s · Ausgang %s",
        Key::SummaryIn => "Eingang",
        Key::SummaryOut => "Ausgang",
        Key::SummaryBalance | Key::ColBalance => "Saldo",
        Key::TransactionsBalance => " · Saldo %s",
        Key::BalanceBankMismatch => {
            "Die Bank meldet nach dieser Buchung %1$s. Danach fehlen Buchungen über %2$s, z. B. solche, die noch nicht im Kontoauszug der Bank stehen."
        }
        Key::ColDate | Key::DetailDate => "Datum",
        Key::ColDescription => "Beschreibung",
        Key::ColCategory | Key::DetailCategory | Key::CategoryMenu => "Kategorie",
        Key::ColAmount => "Betrag",
        Key::NoTransactions => "Keine Umsätze",
        Key::NoTransactionsAddBank => "Fügen Sie Ihre Bank hinzu, um Umsätze zu importieren oder einzutragen.",
        Key::NothingMatches => "Nichts entspricht den aktuellen Filtern.",
        Key::ImportBankStatement => "Kontoauszug importieren (CSV, CAMT XML/ZIP)",
        Key::AttachReceipt => "Beleg anhängen",
        Key::ExportTransactions => "Umsätze exportieren",
        Key::AllCategories => "Alle Kategorien",
        Key::MinAmountIn | Key::MinAmount => "min %s",
        Key::MaxAmountIn => "max %s",
        Key::AmountFilterAny => "Jeder Betrag",
        Key::AddTransaction => "Umsatz hinzufügen",
        Key::Expense => "Ausgabe",
        Key::Income => "Einnahme",
        Key::DateFormat => "Datum (tt.mm.jjjj)",
        Key::AmountIn => "Betrag (%s)",
        Key::DelimiterLabel => "Trennzeichen %s",
        Key::PartnerPayee => "Partner / Empfänger",
        Key::Purpose | Key::ColPurpose => "Verwendungszweck",
        Key::CategoryAuto => "Kategorie: automatisch (Regeln)",
        Key::AutoRules => "Automatisch (Regeln)",
        Key::ImportFile => "%s importieren",
        Key::BookingCount | Key::CategoryUsage => "%d {1:Buchung|Buchungen}",
        Key::PendingLeftOut => "%d vorgemerkt ausgelassen",
        Key::UnreadableCount => "%d unlesbar",
        Key::IntoAccount => "Auf Konto: ",
        Key::ChooseAccount => "Konto wählen",
        Key::FileBelongsTo => "Die Datei gehört zu %s, nicht zum gewählten Konto.",
        Key::ImportFiles => "%d Dateien importieren",
        Key::FileBelongsToNamed => "%s gehört zu %s, nicht zum gewählten Konto.",
        Key::ColumnsApplyToSame => "Spaltenänderungen gelten für die %d {1:Datei|Dateien} mit diesen Spalten.",
        Key::ImportFilesSkipped => {
            "%d {1:Datei wird|Dateien werden} übersprungen, weil {1:sie|sie} nicht gelesen werden {1:konnte|konnten}."
        }
        Key::UnlockWindowTitle => "Finglass entsperren",
        Key::UnlockProfile => "Profil: %s",
        Key::UnlockHeading => "Passphrase eingeben",
        Key::UnlockText => "Ihre Daten sind verschlüsselt. Geben Sie die Passphrase ein, um sie zu öffnen.",
        Key::Passphrase => "Passphrase",
        Key::CurrentPassphrase => "Aktuelle Passphrase",
        Key::NewPassphrase => "Neue Passphrase",
        Key::RepeatPassphrase => "Passphrase wiederholen",
        Key::ShowPassphrase => "Passphrase anzeigen",
        Key::Unlock => "Entsperren",
        Key::ChangePassphrase => "Passphrase ändern",
        Key::ChangePassphraseText => {
            "Geben Sie die aktuelle Passphrase ein und wählen Sie eine neue. Finglass verschlüsselt Ihre Daten neu und öffnet sie dann."
        }
        Key::ChangeAndOpen => "Ändern und öffnen",
        Key::CreatePassphraseHeading => "Daten mit einer Passphrase schützen",
        Key::CreatePassphraseText => {
            "Finglass verschlüsselt seine Datenbank: Konten, Umsätze, Einstellungen und Schlüssel der Bankverbindungen. Sie geben diese Passphrase bei jedem Start ein."
        }
        Key::EncryptPassphraseHeading => "Daten verschlüsseln",
        Key::EncryptPassphraseText => {
            "Ihre Daten sind noch nicht verschlüsselt. Wählen Sie eine Passphrase: Finglass verschlüsselt die Datenbank jetzt und fragt bei jedem Start nach der Passphrase."
        }
        Key::PassphraseHint => {
            "Mindestens %d Zeichen. Ein paar zusammenhanglose Wörter sind leicht zu merken und schwer zu erraten."
        }
        Key::DocumentsNotEncrypted => {
            "Belege und Rechnungen werden als Dateien gespeichert und nicht verschlüsselt. Schalten Sie FileVault (macOS) oder BitLocker (Windows) ein, um sie zu schützen."
        }
        Key::PassphraseNoRecovery => {
            "Mir ist klar, dass niemand meine Daten wiederherstellen kann, wenn ich diese Passphrase vergesse."
        }
        Key::EncryptAndOpen => "Verschlüsseln und öffnen",
        Key::Encrypting => "Daten werden verschlüsselt …",
        Key::Unlocking => "Wird geöffnet …",
        Key::WrongPassphrase => "Falsche Passphrase.",
        Key::PassphraseTooShort => "Verwenden Sie mindestens %d Zeichen.",
        Key::PassphraseSpaces => "Die Passphrase darf nicht mit einem Leerzeichen beginnen oder enden.",
        Key::PassphraseMismatch => "Die Passphrasen stimmen nicht überein.",
        Key::PassphraseAcknowledge => "Bestätigen Sie, dass es keine Wiederherstellung gibt.",
        Key::UnlockFailed => "Finglass konnte die Datenbank nicht öffnen: %s",
        Key::RestoredEncryptedHint => {
            "Eine Sicherung wurde wiederhergestellt. Geben Sie die Passphrase ein, die beim Erstellen der Sicherung galt."
        }
        Key::RestoredPlainHint => "Eine unverschlüsselte Sicherung wurde wiederhergestellt.",
        Key::PlaintextLeftoversTitle => "Unverschlüsselte Datenbankkopien liegen noch auf der Festplatte",
        Key::PlaintextLeftoversText => {
            "Finglass verschlüsselt die Datenbank, die es öffnet. Diese Kopien im Datenordner stammen jedoch von früheren Wiederherstellungen, gelöschten Profilen oder Profilen, die noch nie geöffnet wurden. Wer diesen Datenträger lesen kann, kann sie ohne Ihre Passphrase lesen. Löschen Sie die Kopien, die Sie nicht mehr brauchen."
        }
        Key::PlaintextLeftoverPreRestore => "Durch eine Wiederherstellung ersetzt",
        Key::PlaintextLeftoverDeletedProfile => "Gelöschtes Profil",
        Key::PlaintextLeftoverPendingRestore => "Wiederherstellung wartet auf den nächsten Start",
        Key::PlaintextLeftoverOtherProfile => "Anderes Profil",
        Key::EncryptionStatus => {
            "Datenbank verschlüsselt (SQLCipher, AES-256). Finglass fragt bei jedem Start nach der Passphrase; ändern können Sie sie auf dem Entsperren-Bildschirm."
        }
        Key::EncryptionOff => "Datenbank nicht verschlüsselt (Entwicklungs- oder Testmodus).",
        Key::BackupPassphraseTitle => "Passphrase dieser Sicherung",
        Key::BackupPassphraseText => {
            "Diese Sicherung wurde mit einer anderen Passphrase erstellt. Geben Sie diese Passphrase ein, um die Sicherung zu prüfen. Finglass fragt beim nächsten Start erneut danach."
        }
        Key::BackupNote => "Sicherungen bleiben mit der Passphrase verschlüsselt, die beim Erstellen galt.",
        Key::Edit => "Bearbeiten",
        Key::Profiles => "Profile",
        Key::ProfilesHint => "Jedes Profil hat eigene Konten, Umsätze, Dokumente und Bankzugänge.",
        Key::NewProfile => "Neues Profil",
        Key::ProfileName => "Profilname",
        Key::RenameProfile => "%s umbenennen",
        Key::DeleteProfile => "%s löschen",
        Key::DeleteProfileText => {
            "Die Daten von %s werden in den Ordner profiles-deleted verschoben. Nichts wird gelöscht: Der Ordner bleibt samt unverschlüsselter Dokumente erhalten, bis Sie ihn selbst löschen."
        }
        Key::SwitchProfile => "Beim nächsten Start öffnen",
        Key::ProfileOpen => "jetzt geöffnet",
        Key::ProfileNextStart => "öffnet beim nächsten Start",
        Key::RestartToOpenProfile => "Starten Sie Finglass neu, um %s zu öffnen.",
        Key::WrongCurrencySkipped => {
            "%1$d {1:Buchung ist|Buchungen sind} in einer anderen Währung als dieses %2$s-Konto und {1:wird|werden} übersprungen."
        }
        Key::PlusOtherCurrencies => "+ %s in anderen Währungen",
        Key::OtherCurrenciesExcluded => {
            "Alle Beträge in %1$s. %2$d {2:Buchung|Buchungen} in anderen Währungen {2:ist|sind} nicht enthalten."
        }
        Key::PrimaryCurrency => "Hauptwährung",
        Key::PrimaryCurrencyHint => {
            "Übersicht, Budgets und Berichte summieren nur Buchungen in dieser Währung. Beträge werden nie umgerechnet."
        }
        Key::CurrencyIs => "Währung: %s",
        Key::CurrentBalanceIn => "Aktueller Saldo (%s)",
        Key::Columns => "Spalten",
        Key::Preview => "Vorschau",
        Key::ColDebit => "Soll",
        Key::ColCredit => "Haben",
        Key::ColPartner | Key::DetailPartner => "Partner",
        Key::ColShop | Key::DetailShop => "Händler",
        Key::NoneDash => "— keine —",
        Key::ColumnN => "Spalte %d",
        Key::AndMore => "… und %d weitere",
        Key::SetAccountBalance => "Kontosaldo aus der neuesten Buchung der Datei setzen",
        Key::SetAccountBalanceClosing => "Kontosaldo auf %s setzen (Schlusssaldo der Datei)",
        Key::SetAccountBalanceClosingPending => {
            "Kontosaldo auf %1$s setzen (Schlusssaldo der Datei ohne die %2$d vorgemerkten Umsätze)"
        }
        Key::NewerBookingsUnchanged => "Das Konto hat neuere Buchungen als diese Datei, der Saldo bleibt unverändert.",
        Key::BookingsSkippedHint => {
            "Bereits importierte Buchungen werden übersprungen, ebenso solche, die nach Datum und Betrag zu einer bestehenden Buchung aus anderer Quelle (Sync oder manuell) passen."
        }
        Key::Importing => "Importiere…",
        Key::ImportCount => "%d importieren",
        Key::ErrorImportTooManyFiles => "Wählen Sie höchstens %d Dateien auf einmal aus.",
        Key::ErrorImportFilesTooLarge => "Die gewählten Dateien sind zusammen größer als %d MB.",
        Key::ErrorImportNoReadableFile => "Keine der gewählten Dateien konnte gelesen werden: %s",
        Key::ErrorImportDifferentAccounts => {
            "Die Dateien gehören zu verschiedenen Konten (%s). Importieren Sie ein Konto nach dem anderen."
        }
        Key::ErrorImportStopped => "Import gestoppt bei %s: %s. Vorher wurden %d {3:Datei|Dateien} importiert.",
        Key::DeleteTransactionTitle => "Umsatz löschen?",
        Key::DocumentsCount => "%d {1:Dokument|Dokumente}",
        Key::DetailPurpose => "Zweck",
        Key::AttachReceiptEllipsis => "Beleg anhängen…",
        Key::NoDocumentsLinked => "Keine Dokumente verknüpft",
        Key::CategorizeSimilar => "Ähnliche Buchungen automatisch kategorisieren",
        Key::PatternRegex => "Muster (Regex)",
        Key::CreateRule => "Regel anlegen",
        Key::TabDelimiter => "Tab",
        Key::NewGroup => "Neue Gruppe",
        Key::GroupName => "Gruppenname",
        Key::RenameGroup => "Gruppe umbenennen",
        Key::DeleteGroup => "Gruppe löschen",
        Key::DeleteGroupText => "Gruppe „%s“ löschen? Ihre Konten kehren in ihre Standardgruppe zurück.",
        Key::MoveUp => "Nach oben",
        Key::MoveDown => "Nach unten",
        Key::MoveToGroup => "In Gruppe verschieben…",
        Key::DefaultGroup => "Standard (nach Kontoart)",
        Key::FilterAccounts => "Konten filtern",
        Key::AccountsShown | Key::TaxReceiptsOf => "%1$d von %2$d",
        Key::LastFetched => "Zuletzt abgerufen %s",
        Key::Collapse => "Einklappen",
        Key::Expand => "Ausklappen",
        Key::NewBookingsCount => "%d {1:neue Buchung|neue Buchungen}",
        Key::NewBooking => "Neue Buchung",
        Key::MarkAsRead => "Als gelesen markieren",
        Key::DragToGroupHint => "Auf eine Gruppe ziehen, um das Konto zu verschieben",
        Key::CollapseSidebar => "Seitenleiste einklappen",
        Key::ExpandSidebar => "Seitenleiste ausklappen",
        Key::ChangeLanguage => "Sprache ändern",
        Key::LanguageBusy => "Schließen Sie zuerst die laufende Aufgabe ab",
        Key::SwitchProfileMenu => "Profil wechseln",
        Key::ManageProfiles => "Profile verwalten…",
        Key::BackToProfile | Key::BackTo => "Zurück zu %s",
        Key::From | Key::FromDate => "Von",
        Key::OcrNotInstalled => {
            "Text in Bildern und gescannten PDFs kann erst gelesen werden, wenn Tesseract installiert ist (siehe %s). PDFs mit Text funktionieren ohne."
        }
        Key::Upload => "Hochladen",
        Key::UploadDocuments => "Dokumente hochladen",
        Key::SearchDocuments => "Dokumente nach Name oder Textinhalt suchen…",
        Key::TabAllDocuments => "Alle Dokumente",
        Key::TabUnlinked => "Unverknüpft",
        Key::TabLinked => "Mit Umsatz verknüpft",
        Key::TabWithCount => "%1$s (%2$d)",
        Key::NoDocuments => "Keine Dokumente",
        Key::NoMatches => "Keine Treffer",
        Key::UploadHint => "Belege, Rechnungen und Auszüge hochladen (PDF, JPG, PNG, TIFF).",
        Key::TryAnotherSearch => "Anderen Suchbegriff versuchen.",
        Key::DeleteDocumentTitle => "Dokument löschen?",
        Key::DeleteDocumentText => "%s und die gespeicherte Kopie werden gelöscht.",
        Key::NotLinked => "nicht verknüpft",
        Key::TextExtracted => "Text erkannt",
        Key::NoText => "Kein Text",
        Key::ReExtractText => "Text erneut erkennen",
        Key::LinkedTransaction => "Verknüpfter Umsatz",
        Key::SuggestedMatches => "Vorgeschlagene Treffer (Betrag und Datum aus dem Dokument)",
        Key::FindTransactionToLink => "Umsatz zum Verknüpfen suchen…",
        Key::ExtractedText => "Erkannter Text",
        Key::ExtractedTextConfidence => "Erkannter Text (Konfidenz %d%%)",
        Key::NoTextExtracted => "Kein Text erkannt.",
        Key::MatchPercent => "Treffer %s",
        Key::PreviewOf => "Vorschau von %s",
        Key::Export => "Exportieren",
        Key::ExportPdf => "PDF exportieren",
        Key::ExportReport => "Bericht exportieren",
        Key::NothingToExport => "Keine Buchungen zum Exportieren in %s",
        Key::BookingsCountTotal => "%1$d {1:Buchung|Buchungen} · %2$s",
        Key::TaxYear => "Steuerjahr",
        Key::CustomRange => "Zeitraum",
        Key::PreviousYear => "Vorjahr",
        Key::NextYear => "Folgejahr",
        Key::TabTaxSummary => "Steuerübersicht",
        Key::TabMonthlyBreakdown => "Monatsübersicht",
        Key::TabCategoryAnalysis => "Kategorieanalyse",
        Key::TabRecurring => "Wiederkehrend",
        Key::NoTransactionsIn => "Keine Umsätze in %s",
        Key::ReportsBuiltFrom => "Berichte entstehen aus den Umsätzen des gewählten Zeitraums.",
        Key::TotalIncome | Key::PdfTotalIncome => "Gesamteinnahmen",
        Key::TotalExpenses | Key::PdfTotalExpenses => "Gesamtausgaben",
        Key::NetIncome | Key::PdfNetIncome => "Nettoeinkommen",
        Key::TaxRelevantAmount => "Steuerliche Abzüge",
        Key::TaxRelevantByCategory | Key::PdfTaxByCategory => "Steuerrelevante Ausgaben nach Kategorie",
        Key::NoTaxRelevantExpenses => {
            "Keine steuerrelevanten Ausgaben. Markieren Sie Kategorien in den Einstellungen als steuerrelevant (z. B. Arbeitsmittel, Spenden, Versicherungen)."
        }
        Key::CategoriesUsed => "Genutzte Kategorien",
        Key::AvgMonthlyExpenses => "Ø monatliche Ausgaben",
        Key::HighestMonth => "Höchster Monat",
        Key::MonthlyIncomeExpenses => "Monatliche Einnahmen & Ausgaben – %s",
        Key::ColMonth | Key::PdfMonth => "Monat",
        Key::ColIncome | Key::ChartIncome | Key::TaxSectionIncome => "Einnahmen",
        Key::ColExpenses | Key::ChartExpenses => "Ausgaben",
        Key::ColNet => "Netto",
        Key::ReportTotal => "Gesamt",
        Key::SpendingByCategory | Key::PdfExpensesByCategory => "Ausgaben nach Kategorie",
        Key::TopVendors => "Top-Empfänger",
        Key::ToDate => "Bis",
        Key::Apply => "Anwenden",
        Key::NoRecurringFound => "Keine wiederkehrenden Buchungen gefunden",
        Key::RecurringHint => {
            "Erkannt aus den letzten 13 Monaten, sobald etwas mindestens dreimal vorkommt (jährlich: zweimal)."
        }
        Key::RecurringPayments => "Wiederkehrende Zahlungen",
        Key::CostPerYear => "Kosten pro Jahr",
        Key::PerMonth => "Pro Monat",
        Key::SubscriptionsTitle => "Abos, Verträge und regelmäßige Einnahmen",
        Key::ColEvery => "Alle",
        Key::ColNext => "Nächste",
        Key::ColPerYear => "Pro Jahr",
        Key::TimesSeen => "%dx gesehen",
        Key::Appearance | Key::MenuView => "Darstellung",
        Key::DarkTheme => "Dunkles Design",
        Key::MenuFile => "Ablage",
        Key::MenuWindow => "Fenster",
        Key::MenuHelp => "Hilfe",
        Key::MenuSettings => "Einstellungen …",
        Key::MenuExit => "Beenden",
        Key::MenuEdit => "Bearbeiten …",
        Key::MenuDetails => "Details",
        Key::SyncNow => "Jetzt synchronisieren",
        Key::ShowTransactions => "Umsätze anzeigen",
        Key::ShowAllFromPartner => "Alle von %s anzeigen",
        Key::ShowInFinder => "Im Finder zeigen",
        Key::ShowInExplorer => "Im Explorer anzeigen",
        Key::ShowInFolder => "Im Ordner anzeigen",
        Key::MenuNewTransaction => "Neuer Umsatz …",
        Key::MenuImportStatement => "Kontoauszug importieren …",
        Key::MenuMinimize => "Im Dock ablegen",
        Key::MenuZoom => "Zoomen",
        Key::MenuGitHub => "Finglass auf GitHub",
        Key::MenuReportProblem => "Problem melden …",
        Key::AboutTitle => "Über Finglass",
        Key::AboutVersion => "Version %s",
        Key::AboutText => {
            "Ihr Finanz-Arbeitsplatz: Konten, FinTS-Banking, Belege und Steuerberichte. Alpha-Software – nicht für echte Bankkonten."
        }
        Key::AboutTypeface => "Schrift: IBM Plex Sans (SIL Open Font License 1.1)",
        Key::Language => "Sprache",
        Key::Categories => "Kategorien",
        Key::AddCategory => "Kategorie hinzufügen",
        Key::ColourSwatch => "Farbe %1$d von %2$d",
        Key::FixDuplicateColours => "Jeder Kategorie ihre eigene Farbe geben",
        Key::NoCategoriesYet => "Noch keine Kategorien.",
        Key::AddDefaultCategories => "Standardkategorien hinzufügen",
        Key::CategoryIsTransfer => "Umbuchung zwischen eigenen Konten",
        Key::CategoryIsTransferHint => {
            "Zählt weder als Einnahme noch als Ausgabe. Nur für Kreditkarten-Zahlungen verwenden, wenn die eigenen Buchungen der Karte importiert werden."
        }
        Key::CategoryTransferLabel => "Umbuchung",
        Key::CategorySplitMore => "%1$d weitere {1:Kategorie|Kategorien}",
        Key::CategorySplitTransfers => "Nicht gezählt: Umbuchungen %s",
        Key::DefaultCategoryGroceries => "Lebensmittel",
        Key::DefaultCategoryDrugstore => "Drogerie & Haushalt",
        Key::DefaultCategoryHousing => "Wohnen",
        Key::DefaultCategoryEnergy => "Energie",
        Key::DefaultCategoryPhoneInternetMedia => "Telefon, Internet & Medien",
        Key::DefaultCategoryInsurance => "Versicherungen",
        Key::DefaultCategoryLoans => "Kredite & Finanzierung",
        Key::DefaultCategorySavings => "Sparen & Anlage",
        Key::DefaultCategoryTransport => "Mobilität",
        Key::DefaultCategoryHealth => "Gesundheit",
        Key::DefaultCategoryClothing => "Kleidung & Schuhe",
        Key::DefaultCategoryOnlineShopping => "Online-Shopping",
        Key::DefaultCategoryLeisure => "Freizeit & Vereine",
        Key::DefaultCategoryRestaurants => "Restaurant & Café",
        Key::DefaultCategoryTaxes => "Steuern & Abgaben",
        Key::DefaultCategoryCash => "Bargeld",
        Key::DefaultCategoryCreditCard => "Kreditkarte",
        Key::DefaultCategoryOwnTransfers => "Umbuchung",
        Key::DefaultCategorySalary => "Gehalt",
        Key::DefaultCategoryChildBenefit => "Kindergeld",
        Key::DefaultCategoryInterestFees => "Zinsen & Bankgebühren",
        Key::DefaultCategoryFamily => "Familie",
        Key::DefaultCategoryChildrenSchool => "Kinder & Schule",
        Key::DefaultCategoryOtherIncome => "Sonstige Einnahmen",
        Key::BudgetAmount => "Budget %s",
        Key::AutoCategorizationRules => "Auto-Kategorisierungsregeln",
        Key::AddRule => "Regel hinzufügen",
        Key::RulesHint => {
            "Das Muster einer Regel ist ein groß-/kleinschreibungsunabhängiger regulärer Ausdruck für Partnername und Verwendungszweck oder nur eins davon, für Ausgaben, Einnahmen oder beides. Höhere Priorität gewinnt, bei gleicher Priorität die ältere Regel."
        }
        Key::NoRulesYet => "Noch keine Regeln. Legen Sie eine an oder importieren Sie eine Regeldatei.",
        Key::Prio => "Prio %d",
        Key::Manual => "manuell",
        Key::RuleSearchIn => "Suche in",
        Key::RuleFieldAny => "Partner und Zweck",
        Key::RuleFieldPartner => "Nur Partnername",
        Key::RuleFieldPurpose => "Nur Verwendungszweck",
        Key::RuleAppliesTo => "Gilt für",
        Key::RuleDirectionAny => "Ausgaben und Einnahmen",
        Key::RuleDirectionExpense => "Nur Ausgaben",
        Key::RuleDirectionIncome => "Nur Einnahmen",
        Key::RuleCategoryLabel => "Kategorie: %s",
        Key::ImportRules => "Regeln importieren…",
        Key::ExportRules => "Regeln exportieren…",
        Key::RuleImportTitle => "Regeln importieren",
        Key::RuleImportCategorize => "%d nicht kategorisierte {1:Buchung|Buchungen} jetzt kategorisieren",
        Key::RuleImportNewRules => "%d neue {1:Regel|Regeln}",
        Key::RuleImportCategories => "%d {1:Kategorie|Kategorien} neu anlegen",
        Key::RuleImportDuplicates => "%d {1:Duplikat|Duplikate} übersprungen",
        Key::RuleImportConflicts => "%d {1:Konflikt|Konflikte} übersprungen",
        Key::RuleImportErrors => "%d {1:ungültige Zeile|ungültige Zeilen}",
        Key::RuleImportConflictLine => "Zeile %d: %s → %s (vorhanden: %s)",
        Key::RuleImportErrorLine => "Zeile %d: %s",
        Key::MessageRulesExported => "%d {1:Regel|Regeln} exportiert",
        Key::MessageRulesExportSkipped => {
            "%1$d {1:Regel|Regeln} exportiert, %2$d übersprungen (Tabulator oder Zeilenumbruch in Muster oder Kategorie)"
        }
        Key::MessageRulesImported => {
            "%1$d {1:Regel|Regeln} und %2$d {2:Kategorie|Kategorien} hinzugefügt, %3$d {3:Buchung|Buchungen} kategorisiert"
        }
        Key::RuleFileErrorColumns => "6 Spalten erwartet, %d gefunden",
        Key::RuleFileErrorCategory => "Kategorie ist leer oder länger als 100 Zeichen",
        Key::RuleFileErrorPattern => "Ungültiges Muster: %s",
        Key::RuleFileErrorPatternLength => "Muster länger als 1000 Zeichen",
        Key::RuleFileErrorSearchIn => "Unbekannter search_in-Wert: %s (any, partner, purpose)",
        Key::RuleFileErrorAppliesTo => "Unbekannter applies_to-Wert: %s (any, expense, income)",
        Key::RuleFileErrorPriority => "Priorität ist keine ganze Zahl: %s",
        Key::RuleFileErrorAutoApply => "auto_apply muss true oder false sein: %s",
        Key::ErrorRuleFileHeader => "Keine Finglass-Regeldatei: Kopfzeile fehlt oder ist falsch",
        Key::ErrorRuleFileTooLarge => "Regeldatei zu groß (höchstens 1 MB und 5000 Regeln)",
        Key::ErrorRuleFileEncoding => "Die Regeldatei ist kein gültiger UTF-8-Text",
        Key::DataAndOcr => "Daten & OCR",
        Key::Database => "Datenbank",
        Key::TesseractAvailable => "Tesseract-OCR verfügbar (Deutsch + Englisch)",
        Key::TesseractMissing => "Tesseract nicht gefunden. Installation: brew install tesseract tesseract-lang",
        Key::BackupAndRestore => "Sicherung & Wiederherstellung",
        Key::BackupHint => {
            "Eine Sicherung enthält die Datenbank, alle gespeicherten Dokumente und die Online-Banking-Verbindungsdateien."
        }
        Key::CreateBackup => "Sicherung erstellen",
        Key::RestoreFromBackup => "Aus Sicherung wiederherstellen…",
        Key::RestoreWaiting => "Eine Wiederherstellung wartet. Starten Sie Finglass neu, um sie anzuwenden.",
        Key::CancelRestore => "Wiederherstellung abbrechen",
        Key::BackupRestoredAt => "Sicherung beim Start wiederhergestellt. Ihre bisherigen Daten liegen in %s",
        Key::AuditLogLatest => "Prüfprotokoll (letzte 100)",
        Key::AuditLogShort => "Protokoll",
        Key::AuditLogEmpty => "Noch keine Einträge.",
        Key::RestoreFileTitle => "%s wiederherstellen?",
        Key::RestoreCreated => "Erstellt %s · %d {2:Umsatz|Umsätze} · %d {3:Dokument|Dokumente}",
        Key::RestoreExplanation => {
            "Beim nächsten Start werden alle aktuellen Daten durch diese Sicherung ersetzt. Die aktuellen Daten werden in einen Ordner pre-restore verschoben, nicht gelöscht; dort bleiben sie samt unverschlüsselter Dokumente, bis Sie ihn selbst löschen."
        }
        Key::PrepareRestore => "Wiederherstellung vorbereiten",
        Key::DeleteCategoryTitle => "Kategorie %s löschen?",
        Key::DeleteCategoryText => "Zugehörige Umsätze werden unkategorisiert, die Regeln werden gelöscht.",
        Key::DeleteCategoryUsage => "Wird von %d {1:Buchung|Buchungen} verwendet.",
        Key::DeleteRuleTitle => "Regel %s löschen?",
        Key::DeleteRuleText => "Bereits damit kategorisierte Buchungen behalten ihre Kategorie.",
        Key::EditCategory => "Kategorie bearbeiten",
        Key::MonthlyBudgetOptional => "Monatliches Budget (€, optional)",
        Key::TaxRelevantHint => "Steuerrelevant (Werbungskosten, Sonderausgaben, …)",
        Key::BudgetNotANumber => "Budget ist keine Zahl",
        Key::EditRule => "Regel bearbeiten",
        Key::PatternExample => "Muster (z. B. rewe|edeka|aldi)",
        Key::Priority => "Priorität",
        Key::ApplyAutomatically => "Automatisch anwenden",
        Key::PriorityMustBeNumber => "Priorität muss eine ganze Zahl sein",
        Key::SaveBackup => "Sicherung speichern",
        Key::RestoreBackup => "Sicherung wiederherstellen",
        Key::PinFor => "PIN für %s",
        Key::PinExplanation => "Online-Banking-PIN für Benutzer %s.",
        Key::RememberPinForSyncs => "PIN für Syncs merken",
        Key::RememberPinExplanation => {
            "Wird im sicheren Anmeldedatenspeicher Ihres Systems gespeichert. Überweisungen fragen sie weiterhin ab."
        }
        Key::Pin => "PIN",
        Key::ChooseTanProcedure => "TAN-Verfahren wählen",
        Key::ChooseTanMedium => "TAN-Medium wählen",
        Key::TanRequired => "TAN erforderlich",
        Key::TanDefaultMessage => "Geben Sie die TAN aus Ihrem TAN-Verfahren ein.",
        Key::TanChallenge => "TAN-Herausforderung",
        Key::ConfirmInBankingApp => "In Ihrer Banking-App bestätigen",
        Key::IConfirmedIt => "Ich habe bestätigt",
        Key::MessageFromBank => "Nachricht Ihrer Bank",
        Key::NotEnoughData => "Nicht genug Daten",
        Key::Max => "max",
        Key::BalanceHistory => "Saldo-Verlauf",
        Key::AxisNotFromZero => "Skala beginnt nicht bei null",
        Key::MessageAdded => "%s hinzugefügt",
        Key::MessageAccountAdded => "Konto hinzugefügt",
        Key::MessageAccountDeleted => "Konto gelöscht",
        Key::MessageDeletedForgotten => "%d gelöschte {1:Buchung kann|Buchungen können} wieder importiert werden",
        Key::MessageNoDeletedRemembered => "Für dieses Konto sind keine gelöschten Buchungen vermerkt",
        Key::MessageBankRemoved => "Bank entfernt",
        Key::MessageConnectionSaved => "Verbindungseinstellungen gespeichert",
        Key::SyncConnecting => "Verbinden…",
        Key::MessageTransactionAdded => "Buchung hinzugefügt",
        Key::MessageRuleCreated => "Regel erstellt und auf %d nicht kategorisierte {1:Buchung|Buchungen} angewendet",
        Key::MessageDefaultCategoriesAdded => "%d Standardkategorien hinzugefügt",
        Key::MessageDefaultCategoriesNone => "Alle Standardkategorien sind bereits vorhanden",
        Key::MessageColoursFixed => "%d {1:Kategorie umgefärbt|Kategorien umgefärbt}",
        Key::MessageColoursAlreadyDistinct => "Jede Kategorie hat bereits ihre eigene Farbe",
        Key::MessageAutoCategorized => "%d von %d nicht kategorisierten {2:Buchung|Buchungen} kategorisiert",
        Key::MessageExportedCount => "%d {1:Buchung|Buchungen} nach %s exportiert",
        Key::MessageBackupSaved => "Backup gespeichert unter %s (%d {2:Buchung|Buchungen}, %d {3:Dokument|Dokumente})",
        Key::MessageRestorePrepared => {
            "Wiederherstellung vorbereitet. Starten Sie Finglass neu, um sie abzuschließen; Ihre aktuellen Daten bleiben in einem Ordner pre-restore erhalten."
        }
        Key::MessageRestoreCancelled => "Ausstehende Wiederherstellung abgebrochen",
        Key::MessageProfileMoved => "Profil verschoben nach %s",
        Key::MessageSavedTo => "%s gespeichert",
        Key::PinNotSaved => "Die PIN konnte nicht im sicheren Anmeldedatenspeicher des Systems gespeichert werden.",
        Key::MessageUploaded => "%d {1:Datei|Dateien} hochgeladen, Text aus %d extrahiert",
        Key::UploadProgress => "Lade %1$d von %2$d hoch…",
        Key::MessageDocumentLinked => "Dokument verknüpft",
        Key::MessageDocumentDeleted => "Dokument gelöscht",
        Key::MessageNoTextExtracted => "Es konnte kein Text extrahiert werden",
        Key::BankNameRequired => "Bankname ist erforderlich",
        Key::BlzMustBe8Digits => "BLZ muss 8 Ziffern haben",
        Key::ErrorConnectionBusy => "Warten Sie, bis die Synchronisierung dieser Bank abgeschlossen ist.",
        Key::ErrorDeleteWhileBusy => "Warten Sie, bis die laufende Synchronisierung oder der Import abgeschlossen ist.",
        Key::IbanRequired => "IBAN ist erforderlich",
        Key::IbanChecksumFailed => "IBAN ist ungültig (Prüfsumme fehlerhaft)",
        Key::AccountIbanExists => "Ein Konto mit dieser IBAN existiert bereits",
        Key::ErrorAmountNotZero => "Betrag darf nicht null sein",
        Key::DuplicateTransactionTitle => "Dieselbe Buchung noch einmal hinzufügen?",
        Key::DuplicateTransactionText => {
            "Dieses Konto hat am %s bereits eine Buchung über %s mit demselben Partner und Verwendungszweck. Eine zweite hinzufügen, etwa für einen wiederholten Kauf?"
        }
        Key::AddAnyway => "Trotzdem hinzufügen",
        Key::ErrorExportFailed => "Export fehlgeschlagen",
        Key::ErrorNameRequired => "Name ist erforderlich",
        Key::ErrorColorFormat => "Farbe muss wie #3BC7B0 aussehen",
        Key::ErrorCategoryExists => "Kategorie existiert bereits",
        Key::ErrorPatternRequired => "Muster ist erforderlich",
        Key::ErrorInvalidRegex => "Ungültiger regulärer Ausdruck: %s",
        Key::ErrorPdfExportFailed => "PDF-Export fehlgeschlagen",
        Key::ErrorCsvExportFailed => "CSV-Export fehlgeschlagen",
        Key::ErrorSyncFromDateFuture => "Das Startdatum darf nicht in der Zukunft liegen",
        Key::ErrorAccountTypeInvalid => "Unbekannte Kontoart",
        Key::ErrorBankConnectionNotFound => "Bankverbindung nicht gefunden",
        Key::ErrorAccountNotFound => "Konto nicht gefunden",
        Key::ErrorSyncFailed => "Synchronisierung fehlgeschlagen",
        Key::ErrorBankRejected => "Die Bank hat den Auftrag abgelehnt",
        Key::ErrorBankRejectedStatements => "Die Bank hat die Umsatzabfrage abgelehnt",
        Key::StatementsNotSupported => "Umsätze werden für dieses Konto nicht unterstützt",
        Key::ErrorNoFintsServer => "Kein FinTS-Server für BLZ %s bekannt. Geben Sie die FinTS-URL der Bank ein.",
        Key::ErrorBadServerUrl => "Ungültige FinTS-Server-URL",
        Key::ErrorEnterUserId => {
            "Geben Sie Ihre Online-Banking-Benutzerkennung (Anmeldename/Legitimations-ID) für %s ein."
        }
        Key::ErrorConnectionNotFound => "Bankverbindung %d nicht gefunden",
        Key::ErrorCancelled => "Abgebrochen",
        Key::ErrorCancelledByUser => "Vom Benutzer abgebrochen",
        Key::ErrorDialogFailed => "FinTS-Dialog fehlgeschlagen",
        Key::HintNeedsAccount => "Legen Sie zuerst ein Konto an",
        Key::HintNeedsCategory => "Legen Sie zuerst eine Kategorie an",
        Key::HintNothingToExport => "In dieser Ansicht gibt es keine Umsätze zum Exportieren",
        Key::ErrorBankUnreachable => {
            "Der FinTS-Server der Bank war nicht erreichbar. Prüfen Sie die Server-URL und Ihre Internetverbindung."
        }
        Key::ErrorServerHttpStatus => {
            "Der Server der Bank hat mit HTTP %d geantwortet. Prüfen Sie, ob der FinTS/HBCI-Zugang im Online-Banking freigeschaltet ist und ob Bankleitzahl (BLZ) und Server-URL dieser Verbindung stimmen, oder versuchen Sie es später erneut."
        }
        Key::ErrorServerNotFints => {
            "Der Server der Bank hat eine Antwort gesendet, die kein FinTS ist (%s). Prüfen Sie die FinTS-Server-URL in den Verbindungseinstellungen. Wenn sie richtig ist, blockiert die Bank die Anfrage möglicherweise: Versuchen Sie es später erneut oder zeichnen Sie ein Protokoll in den Einstellungen auf."
        }
        Key::ErrorBankRejectedPin => "Die Bank hat die PIN abgelehnt",
        Key::ErrorPassportReset => {
            "Finglass konnte die gespeicherte Bank-Anmeldung nicht zurücksetzen. Entfernen Sie diese Bank und fügen Sie sie erneut hinzu."
        }
        Key::ProtocolTraceTitle => "FinTS-Protokoll",
        Key::ProtocolTraceHint => {
            "Zeichnet ein Protokoll der nächsten Online-Banking-Dialoge auf, um Verbindungsprobleme zu analysieren. PIN, TAN, Benutzerkennung und Kontonummern werden maskiert, das Protokoll enthält aber die vollständigen Antworten Ihrer Bank: Namen, Verwendungszwecke, Beträge und Salden. Lesen Sie es, bevor Sie es weitergeben. Protokolle bleiben im Speicher, bis Sie sie speichern, und werden nie automatisch gesendet."
        }
        Key::ProtocolTraceEnable => "FinTS-Protokoll aufzeichnen",
        Key::ProtocolTraceSave => "Protokoll speichern",
        Key::ProtocolTraceAvailable => "Ein Protokoll kann gespeichert werden.",
        Key::ProtocolTraceNone => {
            "Noch kein Protokoll aufgezeichnet. Aufzeichnung aktivieren, das Problem reproduzieren (z. B. Sync ausführen) und dann hier speichern."
        }
        Key::MessageProtocolTraceSaved => "Protokoll gespeichert unter %s",
        Key::ErrorNoProtocolTrace => {
            "Noch kein Protokoll aufgezeichnet. Aufzeichnung aktivieren und das Problem zuerst reproduzieren."
        }
        Key::ProgressConnecting => "Verbindung zur Bank…",
        Key::ProgressSavingAccount => "%s wird gespeichert…",
        Key::SyncSummary => "%d {1:Konto|Konten} synchronisiert",
        Key::SyncSummaryNew | Key::SyncSummaryTransactions => ": %d {1:neue Buchung|neue Buchungen}",
        Key::SyncSummaryCategorized | Key::ImportSummaryAuto => ", %d automatisch kategorisiert",
        Key::SyncSummaryDuplicates | Key::ImportSummaryKnown => ", %d bereits bekannt",
        Key::SyncSummaryHistoryFrom => " Verlauf angefordert ab %s.",
        Key::BalanceNotProvided => {
            "…%s: Die Bank liefert für dieses Konto keinen Saldo (%s). Tragen Sie ihn mit „Saldo bearbeiten“ ein, einen Kredit als negativen Betrag."
        }
        Key::BalanceNotProvidedNoCode => {
            "…%s: Die Bank liefert für dieses Konto keinen Saldo. Tragen Sie ihn mit „Saldo bearbeiten“ ein, einen Kredit als negativen Betrag."
        }
        Key::SyncGapNotFetched => {
            "Buchungen vom %s bis %s wurden nicht abgerufen: Finglass ruft höchstens die letzten %d Tage ab. Mit „Verlauf ab Datum abrufen…“ holen Sie sie nach (die Bank fragt eventuell nach einer TAN)."
        }
        Key::SyncAccountWithoutIban => {
            "%s: Die Bank meldet dieses Konto ohne IBAN; Finglass kann es noch nicht speichern."
        }
        Key::SyncBalanceOtherCurrency => {
            "…%s: Die Bank meldet den Saldo in %s, das Konto wird aber in %s geführt; der Saldo wurde nicht geändert."
        }
        Key::SyncCurrencyCorrected => {
            "…%s: Die Kontowährung wurde von %s auf %s korrigiert, da die Bank den Saldo in dieser Währung meldet und keine Kontowährung angibt."
        }
        Key::PendingHeader => "Vorgemerkt (noch nicht gebucht) · Stand %s",
        Key::ImportSummary => "%d {1:Buchung|Buchungen} importiert",
        Key::ImportSummaryFiles => "%d {1:Buchung|Buchungen} aus %d Dateien importiert",
        Key::ImportSummaryProbable => {
            ", %d {1:wahrscheinliche Dublette|wahrscheinliche Dubletten} vorhandener Buchungen übersprungen"
        }
        Key::ImportSummaryWrongCurrency => ", %d in anderer Währung als das Konto übersprungen",
        Key::ImportSummaryBalance => ". Saldo auf %s gesetzt",
        Key::ImportSummaryDeleted => ", %d früher gelöscht, übersprungen",
        Key::ErrorFileNotFound => "Datei nicht gefunden: %s",
        Key::ErrorFileTooLarge => "Datei ist zu groß",
        Key::ErrorImportSeveralAccounts => {
            "Diese Datei enthält Buchungen von %d Konten (%s). Exportieren Sie eine Datei pro Konto und importieren Sie sie einzeln."
        }
        Key::ErrorXlsxTooLarge => "Die Excel-Datei ist zu groß oder beschädigt.",
        Key::ErrorXlsxUnreadable => "Die Excel-Datei konnte nicht gelesen werden.",
        Key::ErrorFileDoesNotExist => "Datei existiert nicht: %s",
        Key::ErrorUnsupportedFileType => "Nicht unterstützter Dateityp: %s",
        Key::ErrorUnreadableImage => "Bild nicht lesbar: %s",
        Key::ErrorDocumentTooLarge => "%s ist zu groß (höchstens %d MB)",
        Key::ErrorImageTooLarge => "%s hat zu viele Pixel zum Lesen",
        Key::ReportUnknownCategory => "Unbekannt",
        Key::ReportTitleTax => "Finglass-Steuerbericht %d",
        Key::ReportTitleRange => "Finglass-Bericht %s",
        Key::PdfGenerated => "Erstellt am %s",
        Key::PdfSummary => "Zusammenfassung",
        Key::PdfTaxRelevantExpenses => "Steuerrelevante Ausgaben",
        Key::PdfTransactionCount | Key::XlsxSheetBookings | Key::TaxColBookings => "Buchungen",
        Key::PdfAllAmountsIn => {
            "Alle Beträge in %s. %d {2:Buchung|Buchungen} in anderen Währungen {2:ist|sind} nicht enthalten."
        }
        Key::PdfNoTaxRelevant => "Keine Kategorien sind als steuerrelevant markiert.",
        Key::PdfMonthlyBreakdown => "Monatliche Aufschlüsselung",
        Key::PdfIncomeExpensesNet => "Einnahmen / Ausgaben / Netto",
        Key::PdfTaxTransactions => "Steuerrelevante Buchungen",
        Key::ColCurrency => "Währung",
        Key::ChartOther => "Sonstige",
        Key::ChartSoFar => "bisher",
        Key::ChartPartialMonthNote => "Blass: Monat bisher",
        Key::FlickerSpeed => "Geschwindigkeit",
        Key::FlickerSize => "Größe",
        Key::FlickerHint => {
            "Halten Sie den TAN-Generator an die Balken, sodass seine Pfeile mit den Dreiecken übereinstimmen."
        }
        Key::TanLabel => "TAN",
        Key::AppLogo => "Finglass-Logo",
        Key::EditCategoryNamed => "Kategorie bearbeiten: %s",
        Key::EditRuleNamed => "Regel bearbeiten: %s",
        Key::OpenPreviewNamed => "Vorschau öffnen: %s",
        Key::BankOfflineHint => "Offline: Umsätze im Bereich „Umsätze“ importieren.",
        Key::IbanOrCardLast4 => "IBAN oder die letzten 4 Ziffern der Karte",
        Key::CardLast4Only => "Geben Sie nur die letzten 4 Ziffern der Karte ein.",
        Key::ErrorBankOffline => "Diese Bank ist offline: es wird nichts an die Bank gesendet.",
        Key::BankOnlineMode => "Online-Banking (FinTS)",
        Key::BankOfflineMode => "Offline (nur Umsätze importieren)",
        Key::AccountsWithoutBank => "Konten ohne Bank",
        Key::ReapplyRules => "Regeln auf angezeigte Buchungen erneut anwenden",
        Key::ReapplyRulesTitle => "Regeln erneut anwenden?",
        Key::ReapplyRulesSummary => "Neue Kategorie für %1$d von %2$d angezeigten {2:Buchung|Buchungen}.",
        Key::ReapplyRulesWarning => {
            "Auch von Hand gesetzte Kategorien werden überschrieben. Grenzen Sie die Liste bei Bedarf vorher mit den Filtern ein. Sie können dies anschließend über das Menü rückgängig machen."
        }
        Key::ReapplyRulesConfirm => "%d ändern",
        Key::UndoReapplyRules => "Erneut angewendete Regeln rückgängig machen",
        Key::MessageReapplyNothing => "Keine angezeigte Buchung würde eine andere Kategorie erhalten",
        Key::MessageReapplied => "%d {1:Buchung|Buchungen} neu kategorisiert",
        Key::MessageReapplyUndone => "%d {1:Buchung|Buchungen} wiederhergestellt",
        Key::HintNothingShown => "Keine Umsätze angezeigt",
        Key::SelectedCount => "%d ausgewählt",
        Key::DeleteSelected => "Löschen…",
        Key::ClearSelection => "Auswahl aufheben",
        Key::DeleteSelectedCount => "%d ausgewählte löschen",
        Key::DeleteTransactionsTitle => "%d {1:Buchung|Buchungen} löschen?",
        Key::DeleteSyncedHint => {
            "Gelöschte Buchungen werden gemerkt, sodass ein späterer Import oder Bankabgleich sie nicht wieder hinzufügt. Über „Gelöschte Buchungen wieder zulassen“ im Menü des Kontos auf der Konten-Ansicht lassen Sie das wieder zu. Verknüpfte Belege bleiben erhalten und werden nur entknüpft."
        }
        Key::MessageTransactionsDeleted => "%d {1:Buchung|Buchungen} gelöscht",
        Key::TaxAutomatic => "Automatisch",
        Key::TaxRelevantChoice => "Steuerrelevant",
        Key::TaxNotRelevantChoice => "Nicht steuerrelevant",
        Key::TaxSetByYou => "%s · von Ihnen gesetzt",
        Key::TaxFromRule => "%1$s · aus Regel „%2$s“",
        Key::TaxFromCategory => "%1$s · aus Kategorie „%2$s“",
        Key::MarkTaxRelevant => "Als steuerrelevant markieren",
        Key::MarkNotTaxRelevant => "Als nicht steuerrelevant markieren",
        Key::ResetTaxAutomatic => "Auf automatisch zurücksetzen",
        Key::RuleTaxAsCategory => "Wie die Kategorie",
        Key::RuleTaxMarkerNo => "keine Steuer",
        Key::MessageTaxOverrideSet => "Steuerstatus geändert für %d {1:Buchung|Buchungen}",
        Key::MessageCategorySet => "Kategorie geändert für %d {1:Buchung|Buchungen}",
        Key::TabTaxBookings => "Steuerbuchungen",
        Key::TaxIncomeTotal => "Steuerrelevante Einnahmen",
        Key::TaxDeductionTotal | Key::TaxSectionDeductions => "Absetzbare Ausgaben",
        Key::TaxReceipts => "Belege",
        Key::ReceiptsMissing => "%d fehlen",
        Key::ReceiptsComplete => "Vollständig",
        Key::TaxSuggestionsTitle => "Möglicherweise steuerrelevant: %1$d {1:Buchung|Buchungen}",
        Key::TaxSuggestionsHint => {
            "Übereinstimmung per Stichwort. Übernehmen markiert die Buchung als steuerrelevant; Ablehnen verwirft den Vorschlag endgültig. Die Stichwörter können in den Einstellungen geändert werden."
        }
        Key::MatchesKeyword => "Passt zu „%s“",
        Key::NoReceipt => "Kein Beleg",
        Key::MissingReceiptsOnly => "Nur fehlende Belege",
        Key::ShowAllCount => "Alle anzeigen (%d)",
        Key::NoTaxBookingsIn => "Keine steuerrelevanten Buchungen in %s",
        Key::TaxBookingsHowTo => {
            "Markieren Sie eine Kategorie in Einstellungen → Kategorien als steuerrelevant, geben Sie einer Regel eine Steuer-Option oder markieren Sie Buchungen in der Buchungsliste."
        }
        Key::TaxSuggestionKeywords => "Steuervorschläge",
        Key::TaxKeywordsCount => "%1$d {1:Stichwort|Stichwörter}",
        Key::ResetToDefaults => "Auf Standard zurücksetzen",
        Key::Accept => "Übernehmen",
        Key::DismissSuggestion => "Ablehnen",
        Key::ExportExcel => "Excel exportieren",
        Key::Print => "Drucken…",
        Key::OpenTaxBookings => "Steuerbuchungen öffnen",
        Key::TaxExportTitle => "Steuerrelevante Buchungen %s",
        Key::TaxColSection => "Bereich",
        Key::TaxColWithReceipt => "Mit Beleg",
        Key::TaxColReceipt => "Beleg",
        Key::TaxColSource => "Quelle",
        Key::Yes => "Ja",
        Key::No => "Nein",
        Key::PdfPageOf => "Seite %1$d von %2$d",
        Key::MessagePrintSent => "An den Drucker gesendet",
        Key::ErrorTaxExportFailed => "Der Export des Steuerberichts ist fehlgeschlagen",
        Key::ErrorPrintFailed => "Drucken fehlgeschlagen",
        Key::ComingSoon => "Dieser Bereich folgt in einem späteren Meilenstein.",
        Key::Security => "Sicherheit",
        Key::ThemeSystem => "System",
        Key::ThemeLight => "Hell",
        Key::ThemeDark => "Dunkel",
        Key::Theme => "Design",
        Key::ErrorFinDeskNotFound => "Im gewählten Ordner wurde keine FinDesk-Datenbank gefunden.",
        Key::ErrorFinDeskWrongPassphrase => "Die FinDesk-Passphrase ist falsch.",
        Key::ErrorFinDeskNotFinDesk => "Diese Datenbank ist keine FinDesk-Datenbank.",
        Key::ErrorFinDeskTargetNotEmpty => {
            "Das Finglass-Profil enthält bereits Daten. Importiere in ein leeres Profil."
        }
        Key::FinDeskImportSummary => {
            "Importiert: %d Kontogruppen, %d Bankverbindungen, %d Konten, %d Kategorien, %d Regeln, \
             %d Umsätze, %d gelöschte Buchungen, %d vorgemerkte Buchungen, %d Dokumente, %d OCR-Texte und \
             %d Einstellungen. Übersprungen: %d Zeilen, %d Dokumente mit fehlender Datei und %d abgelehnte \
             Dokumente. %d Zahlungsaufträge blieben zurück."
        }
        Key::ErrorNotEncrypted => {
            "Dieser Datenordner enthält eine unverschlüsselte Datenbank, die Finglass nicht öffnen kann."
        }
        Key::ErrorDatabaseTooNew => "Diese Datenbank wurde von einer neueren Version von Finglass erstellt.",
        Key::ErrorStartupFailed => {
            "Finglass konnte die Daten dieses Profils beim Start nicht vorbereiten. Beenden Sie Finglass und versuchen Sie es erneut."
        }
        Key::ErrorUnexpected => "ein unerwarteter Fehler",
    }
}
