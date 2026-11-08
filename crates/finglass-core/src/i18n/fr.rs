//! FR texts.

use super::Key;

pub(super) fn text(key: Key) -> &'static str {
    match key {
        Key::Cancel => "Annuler",
        Key::Save => "Enregistrer",
        Key::Add => "Ajouter",
        Key::Delete => "Supprimer",
        Key::Close | Key::Dismiss => "Fermer",
        Key::Ok => "OK",
        Key::Open | Key::OpenProfile => "Ouvrir",
        Key::Back => "Retour",
        Key::More => "Plus",
        Key::ClearSearch => "Effacer la recherche",
        Key::Uncategorized => "Sans catégorie",
        Key::TaxRelevant => "fiscalement pertinent",
        Key::Tax | Key::RuleTaxMarkerYes => "impôt",
        Key::ToggleTheme => "Changer le thème",
        Key::Link => "Lier",
        Key::Unlink => "Délier",
        Key::OpenDetails => "Ouvrir les détails",
        Key::ChangeCategory => "Changer la catégorie : %s",
        Key::OpenAccount => "Ouvrir le compte",
        Key::OpenDocument => "Ouvrir le document",
        Key::ViewPendingTasks => "Voir les tâches en attente",
        Key::EnterValidDate | Key::ErrorSyncFromDateInvalid => "Saisissez une date valide (jj.mm.aaaa)",
        Key::EnterValidAmount => "Saisissez un montant valide",
        Key::EnterValidBalance => "Saisissez un solde valide",
        Key::InvalidDateRange => "La date de fin doit être postérieure à la date de début",
        Key::PatternInvalid => "Saisissez une expression régulière valide",
        Key::OpticalTanCode => "Code TAN optique pour un générateur TAN. Tenez le générateur contre les barres.",
        Key::WindowTitle => "Finglass – Votre espace financier",
        Key::NavDashboard => "Tableau de bord",
        Key::NavAccounts => "Comptes",
        Key::NavTransactions
        | Key::Transactions
        | Key::SummaryCount
        | Key::StatTransactions
        | Key::PdfTransactionCount
        | Key::TaxColBookings => "Opérations",
        Key::NavDocuments | Key::DocumentsFolder => "Documents",
        Key::NavReports => "Rapports",
        Key::NavSettings => "Paramètres",
        Key::TypeChecking => "Compte courant",
        Key::TypeSavings => "Compte d'épargne",
        Key::TypeCreditCard => "Carte de crédit",
        Key::TypeLoan => "Crédit",
        Key::TypeDepot => "Portefeuille",
        Key::PeriodThisMonth => "Ce mois",
        Key::PeriodLastMonth => "Mois dernier",
        Key::PeriodLast3Months => "3 derniers mois",
        Key::PeriodThisYear => "Cette année",
        Key::PeriodLastYear => "Année dernière",
        Key::PeriodAllTime => "Tout",
        Key::PeriodCustomRange => "Période personnalisée…",
        Key::DirectionAll => "Tout",
        Key::DirectionIncome => "Revenus",
        Key::DirectionExpenses => "Dépenses",
        Key::DirectionAnyLabel => "Entrées et sorties",
        Key::CadenceWeekly => "Hebdomadaire",
        Key::CadenceMonthly => "Mensuel",
        Key::CadenceQuarterly => "Trimestriel",
        Key::CadenceYearly => "Annuel",
        Key::WelcomeTitle => "Bienvenue dans Finglass",
        Key::AddAccount => "Ajouter un compte",
        Key::AvailableBalance => "Disponible",
        Key::LoansSubtitle => "Crédits %1$s · net %2$s",
        Key::IncomeThisMonth => "Revenus ce mois",
        Key::ExpensesThisMonth => "Dépenses ce mois",
        Key::PendingTasks => "Tâches en attente",
        Key::PendingSubtitle => "%d sans catégorie · %d {2:document non lié|documents non liés}",
        Key::ExpensesByCategory => "Dépenses par catégorie (ce mois)",
        Key::NoExpensesThisMonth => "Aucune dépense ce mois-ci",
        Key::IncomeVsExpenses => "Revenus vs dépenses (6 mois)",
        Key::Upcoming30Days => "À venir dans les 30 prochains jours",
        Key::AmountVaries => "montant variable",
        Key::BudgetsThisMonth => "Budgets ce mois",
        Key::SpentOfLimit => "%s sur %s",
        Key::OverBudget => "%s au-dessus du budget",
        Key::LeftBudget => "%s restants",
        Key::TaxDeductibleYtd => "Dépenses déductibles (année en cours)",
        Key::TaxDeductibleHint => {
            "Marquez des catégories comme fiscalement pertinentes dans les Paramètres pour suivre les dépenses déductibles."
        }
        Key::RecentTransactions => "Opérations récentes",
        Key::ViewAll => "Tout voir",
        Key::NoTransactionsYet => "Aucune opération pour l'instant",
        Key::GettingStartedIntro => "Trois étapes vers votre premier aperçu.",
        Key::StepAddBank => "Ajoutez votre banque",
        Key::StepAddBankHint => "Saisissez le code bancaire et vos identifiants de banque en ligne.",
        Key::StepSync => "Récupérez soldes et opérations",
        Key::StepSyncHint => {
            "La synchronisation demande une fois votre code PIN ; les soldes peuvent aussi être saisis à la main."
        }
        Key::StepCategorize => "Classez vos opérations",
        Key::StepCategorizeHint => {
            "Catégories et règles regroupent les dépenses et signalent ce qui compte pour les impôts."
        }
        Key::OpenSettings => "Ouvrir les paramètres",
        Key::NothingPending => "Rien à faire",
        Key::NoBookingsSixMonths => "Aucune opération ces six derniers mois",
        Key::PreviousMonth => "Mois précédent",
        Key::NextMonth => "Mois suivant",
        Key::CurrentMonth => "Mois en cours",
        Key::SelectMonth => "Choisir le mois",
        Key::IncomeInMonth => "Revenus en %s",
        Key::ExpensesInMonth => "Dépenses en %s",
        Key::BudgetsInMonth => "Budgets en %s",
        Key::NoExpensesInMonth => "Aucune dépense en %s",
        Key::ExpensesByCategoryInMonth => "Dépenses par catégorie en %s",
        Key::NoBookingsSixMonthsTo => "Aucune opération dans les six mois jusqu'à %s",
        Key::AvailableAsOf => "Disponible au %s",
        Key::CarryForwardFrom => "Report de %1$s : %2$s",
        Key::CarryForwardLine => "Report de %s",
        Key::OtherMovements => "Virements et crédits : %s",
        Key::OtherMovementsLabel => "Virements et prêts",
        Key::AccountsTotal => "Total %s sur %d {2:compte|comptes}",
        Key::AccountsTotalLabel => "Total de %d {1:compte|comptes}",
        Key::AccountsNetLabel => "Net de %d {1:compte|comptes}",
        Key::AccountsLoansLabel => "Dont prêts",
        Key::AccountCount => "%d {1:compte|comptes}",
        Key::AddBank => "Ajouter une banque",
        Key::NoBanksYet => "Aucune banque pour l'instant",
        Key::NoBanksDescription => {
            "Ajoutez votre banque, puis ses comptes. Soldes et opérations peuvent être saisis manuellement jusqu'à la synchronisation en ligne."
        }
        Key::BlzPrefix => "BLZ %s",
        Key::BicPrefix => "BIC %s",
        Key::SyncedPrefix => "synchronisé %s",
        Key::Syncing => "Synchronisation…",
        Key::Sync => "Sync",
        Key::SyncAll => "Tout synchroniser",
        Key::SyncAllProgress => "%d/%d · %s",
        Key::IncludeInSyncAll => "Inclure dans « Tout synchroniser »",
        Key::NotInSyncAll => "hors « Tout synchroniser »",
        Key::SyncAllNothingIncluded => "Aucune connexion n'est incluse dans « Tout synchroniser »",
        Key::SyncAllStopped => "Tout synchroniser interrompu : la synchronisation de %s a été annulée.",
        Key::FetchHistory => "Récupérer l'historique depuis…",
        Key::FetchHistoryFor => "Récupérer l'historique pour %s",
        Key::FetchHistoryDate => "Date de début",
        Key::FetchHistoryHint => {
            "Les opérations déjà importées sont ignorées. Les banques limitent la profondeur de l'historique et demandent souvent un TAN au-delà de 90 jours."
        }
        Key::Fetch => "Récupérer",
        Key::Reset => "Réinitialiser",
        Key::ClearHistoryStart => "Effacer le début de l'historique",
        Key::NextSyncFrom => "prochaine récupération depuis %s",
        Key::ConnectionSettings => "Paramètres de connexion",
        Key::RenameConnection => "Renommer",
        Key::ConnectionName | Key::ColName | Key::Name => "Nom",
        Key::ConnectionNameHint => {
            "Visible uniquement dans Finglass, p. ex. « DKB Pro ». L'accès bancaire reste le même."
        }
        Key::AddAccountShort | Key::DetailAccount | Key::ColAccount => "Compte",
        Key::RemoveBank => "Retirer la banque",
        Key::NoAccountsYet => "Aucun compte pour l'instant",
        Key::EditBalance => "Modifier le solde",
        Key::DeleteAccount => "Supprimer le compte",
        Key::AllowDeletedAgain => "Réautoriser les opérations supprimées",
        Key::BlzDigits => "BLZ (8 chiffres)",
        Key::BankName => "Nom de la banque",
        Key::BicOptional => "BIC (facultatif)",
        Key::OnlineUserIdOptional => "Identifiant banque en ligne (facultatif)",
        Key::NoFintsServer => "aucun serveur FinTS listé",
        Key::OnlineSyncAvailable => "sync en ligne disponible",
        Key::OnlineSyncNotYetAvailable => {
            "La synchronisation en ligne n'est pas encore disponible dans Finglass. Importez les relevés de votre banque dans « Opérations »."
        }
        Key::BankSearchHint => "Rechercher une banque : nom, BLZ, BIC ou IBAN",
        Key::BankSearchNoMatch => "Aucune banque trouvée — saisissez les données ci-dessous",
        Key::BankManualEntry => "ou saisissez les données manuellement",
        Key::BankUnknownBlz => "Le code %s n'est pas dans l'annuaire des banques",
        Key::BankFintsProposed => "Serveur FinTS : %s",
        Key::BankUseSuggestion => "Utiliser cette banque",
        Key::AddAccountAt => "Ajouter un compte chez %s",
        Key::Iban => "IBAN",
        Key::BalanceNotANumber => "Le solde n'est pas un nombre",
        Key::UpdateBalance => "Mettre à jour le solde",
        Key::OnlineBankingFor => "Banque en ligne pour %s",
        Key::UserIdLogin => "Identifiant (nom de connexion / ID de légitimation)",
        Key::FintsServerUrl => "URL du serveur FinTS",
        Key::LeaveEmptyToUse => "Laisser vide pour utiliser %s",
        Key::AskBankForFints => "Demandez à votre banque son adresse FinTS (PIN/TAN)",
        Key::RememberedTan => "Procédure TAN mémorisée : %s",
        Key::PinNeverStored => "Votre PIN est demandé à chaque sync et ne peut pas être stocké sur ce système.",
        Key::PinSavedForSyncs => "PIN enregistré pour les syncs",
        Key::ForgetPin => "Oublier le PIN",
        Key::PinAskedEverySync => {
            "Votre PIN est demandé à chaque sync. Cochez « Mémoriser le PIN pour les syncs » dans la boîte de dialogue pour le conserver dans le magasin d'identifiants sécurisé de votre système."
        }
        Key::DaysCount => "%d jours",
        Key::OneYear => "1 an",
        Key::LastDaysSummary => "%d derniers jours : entrées %s · sorties %s",
        Key::RecentBookings => "Écritures récentes",
        Key::NoneInPeriod => "Aucune sur cette période",
        Key::HistoryReconstructed => {
            "L'historique est reconstruit à partir du solde actuel et des écritures connues de Finglass."
        }
        Key::AllTransactions => "Toutes les opérations",
        Key::DeleteAccountTitle => "Supprimer le compte ?",
        Key::DeleteAccountText => {
            "Cela supprime %s et toutes ses opérations. Les documents liés sont conservés mais déliés."
        }
        Key::RenameAccount => "Renommer le compte",
        Key::ChangeAccountType => "Changer le type de compte…",
        Key::AccountTypeLabel => "Type de compte",
        Key::LoanBalanceHint => "Saisissez le capital restant dû en montant négatif, p. ex. -12 000,00.",
        Key::AccountName => "Nom du compte",
        Key::AccountNameHint => "Visible uniquement dans Finglass. Laisser vide pour afficher « %s ».",
        Key::RemoveBankTitle => "Retirer %s ?",
        Key::RemoveBankText => "Cela supprime la banque, tous ses comptes et leurs opérations.",
        Key::CentralBlzNote => {
            "La banque en ligne de cet établissement utilise le code banque %1$s pour toutes les agences ; Finglass l'utilise donc à la place de %2$s."
        }
        Key::CentralBlzHint => "La banque en ligne de cet établissement utilise le code banque %s.",
        Key::UseBlz => "Utiliser %s",
        Key::BlzNotInDirectory => "Absent de la liste des banques. Vérifiez le code banque.",
        Key::BlzOtherBank => "Ce code banque appartient à %s, pas à la banque de cette connexion.",
        Key::LoginResetNote => {
            "Modifier le code banque, l'identifiant ou l'URL du serveur réinitialise l'enregistrement de Finglass auprès de la banque. La prochaine synchronisation l'enregistre à nouveau et peut demander une confirmation par TAN."
        }
        Key::Import => "Importer",
        Key::AutoCategorize => "Catégoriser auto.",
        Key::ExportCsv => "Exporter CSV",
        Key::SearchPartnerPurpose => "Rechercher un partenaire ou un libellé…",
        Key::AllAccounts => "Tous les comptes",
        Key::TransactionsSummary => "%d {1:opération|opérations} · entrées %s · sorties %s",
        Key::SummaryIn => "Entrées",
        Key::SummaryOut => "Sorties",
        Key::SummaryBalance | Key::ColBalance => "Solde",
        Key::TransactionsBalance => " · Solde %s",
        Key::BalanceBankMismatch => {
            "La banque indique %1$s après cette opération. Des opérations de %2$s manquent ensuite, par ex. celles que la banque n'a pas encore mises dans son relevé."
        }
        Key::ColDate | Key::DetailDate => "Date",
        Key::ColDescription => "Description",
        Key::ColCategory | Key::DetailCategory | Key::CategoryMenu => "Catégorie",
        Key::ColAmount => "Montant",
        Key::NoTransactions => "Aucune opération",
        Key::NoTransactionsAddBank => "Ajoutez votre banque pour importer ou saisir des opérations.",
        Key::NothingMatches => "Rien ne correspond aux filtres actuels.",
        Key::ImportBankStatement => "Importer un relevé (CSV, CAMT XML/ZIP)",
        Key::AttachReceipt => "Joindre un justificatif",
        Key::ExportTransactions => "Exporter les opérations",
        Key::AllCategories => "Toutes les catégories",
        Key::MinAmountIn | Key::MinAmount => "min %s",
        Key::MaxAmountIn => "max %s",
        Key::AmountFilterAny => "Tout montant",
        Key::AddTransaction => "Ajouter une opération",
        Key::Expense => "Dépense",
        Key::Income => "Revenu",
        Key::DateFormat => "Date (jj.mm.aaaa)",
        Key::AmountIn => "Montant (%s)",
        Key::DelimiterLabel => "séparateur %s",
        Key::PartnerPayee => "Partenaire / bénéficiaire",
        Key::Purpose | Key::ColPurpose | Key::DetailPurpose => "Libellé",
        Key::CategoryAuto => "Catégorie : auto (règles)",
        Key::AutoRules => "Auto (règles)",
        Key::ImportFile => "Importer %s",
        Key::BookingCount | Key::CategoryUsage => "%d {1:écriture|écritures}",
        Key::PendingLeftOut => "%d en attente {1:omise|omises}",
        Key::UnreadableCount => "%d {1:illisible|illisibles}",
        Key::IntoAccount => "Vers le compte : ",
        Key::ChooseAccount => "Choisir un compte",
        Key::FileBelongsTo => "Le fichier appartient à %s, pas au compte sélectionné.",
        Key::ImportFiles => "Importer %d fichiers",
        Key::FileBelongsToNamed => "%s appartient à %s, pas au compte sélectionné.",
        Key::ColumnsApplyToSame => {
            "Les changements de colonnes s'appliquent aux %d {1:fichier|fichiers} avec ces colonnes."
        }
        Key::ImportFilesSkipped => {
            "%d {1:fichier est ignoré, car il n'a pas pu être lu|fichiers sont ignorés, car ils n'ont pas pu être lus}."
        }
        Key::UnlockWindowTitle => "Déverrouiller Finglass",
        Key::UnlockProfile => "Profil : %s",
        Key::UnlockHeading => "Saisissez votre phrase secrète",
        Key::UnlockText => "Vos données sont chiffrées. Saisissez la phrase secrète pour les ouvrir.",
        Key::Passphrase => "Phrase secrète",
        Key::CurrentPassphrase => "Phrase secrète actuelle",
        Key::NewPassphrase => "Nouvelle phrase secrète",
        Key::RepeatPassphrase => "Répétez la phrase secrète",
        Key::ShowPassphrase => "Afficher la phrase secrète",
        Key::Unlock => "Déverrouiller",
        Key::ChangePassphrase => "Changer la phrase secrète",
        Key::ChangePassphraseText => {
            "Saisissez la phrase secrète actuelle et choisissez-en une nouvelle. Finglass rechiffre vos données puis les ouvre."
        }
        Key::ChangeAndOpen => "Changer et ouvrir",
        Key::CreatePassphraseHeading => "Protégez vos données par une phrase secrète",
        Key::CreatePassphraseText => {
            "Finglass chiffre sa base de données : comptes, opérations, réglages et clés des connexions bancaires. Vous saisissez cette phrase secrète à chaque démarrage."
        }
        Key::EncryptPassphraseHeading => "Chiffrer vos données",
        Key::EncryptPassphraseText => {
            "Vos données ne sont pas encore chiffrées. Choisissez une phrase secrète : Finglass chiffre la base maintenant et la demande à chaque démarrage."
        }
        Key::PassphraseHint => {
            "Au moins %d caractères. Quelques mots sans rapport sont faciles à retenir et difficiles à deviner."
        }
        Key::DocumentsNotEncrypted => {
            "Les justificatifs et factures sont enregistrés comme fichiers et ne sont pas chiffrés. Activez FileVault (macOS) ou BitLocker (Windows) pour les protéger."
        }
        Key::PassphraseNoRecovery => {
            "Je comprends que personne ne pourra récupérer mes données si j'oublie cette phrase secrète."
        }
        Key::EncryptAndOpen => "Chiffrer et ouvrir",
        Key::Encrypting => "Chiffrement des données…",
        Key::Unlocking => "Ouverture…",
        Key::WrongPassphrase => "Phrase secrète incorrecte.",
        Key::PassphraseTooShort => "Utilisez au moins %d caractères.",
        Key::PassphraseSpaces => "La phrase secrète ne peut pas commencer ni finir par une espace.",
        Key::PassphraseMismatch => "Les phrases secrètes ne correspondent pas.",
        Key::PassphraseAcknowledge => "Confirmez que vous avez compris qu'aucune récupération n'est possible.",
        Key::UnlockFailed => "Finglass n'a pas pu ouvrir la base de données : %s",
        Key::RestoredEncryptedHint => {
            "Une sauvegarde a été restaurée. Saisissez la phrase secrète en vigueur lors de sa création."
        }
        Key::RestoredPlainHint => "Une sauvegarde non chiffrée a été restaurée.",
        Key::PlaintextLeftoversTitle => "Des copies non chiffrées de la base sont encore sur le disque",
        Key::PlaintextLeftoversText => {
            "Finglass chiffre la base qu'il ouvre, mais ces copies dans le dossier de données proviennent de restaurations antérieures, de profils supprimés ou de profils jamais ouverts. Quiconque peut lire ce disque peut les lire sans votre phrase secrète. Supprimez celles dont vous n'avez plus besoin."
        }
        Key::PlaintextLeftoverPreRestore => "Remplacé par une restauration",
        Key::PlaintextLeftoverDeletedProfile => "Profil supprimé",
        Key::PlaintextLeftoverPendingRestore => "Restauration en attente du prochain démarrage",
        Key::PlaintextLeftoverOtherProfile => "Autre profil",
        Key::UnencryptedLeftoversTitle => "Des documents non chiffrés sont encore sur le disque",
        Key::UnencryptedLeftoversText => {
            "Finglass chiffre sa base de données, mais les documents de ces dossiers sont enregistrés en clair. Ils proviennent de restaurations ou de profils supprimés. Quiconque peut lire ce disque peut les lire sans votre phrase secrète. Supprimez un dossier vous-même lorsque vous n'en avez plus besoin."
        }
        Key::EncryptionStatus => {
            "Base de données chiffrée (SQLCipher, AES-256). Finglass demande la phrase secrète à chaque démarrage ; vous pouvez la changer sur l'écran de déverrouillage."
        }
        Key::EncryptionOff => "Base de données non chiffrée (mode développement ou test).",
        Key::BackupPassphraseTitle => "Phrase secrète de cette sauvegarde",
        Key::BackupPassphraseText => {
            "Cette sauvegarde a été créée avec une autre phrase secrète. Saisissez-la pour vérifier la sauvegarde. Finglass la redemandera au prochain démarrage."
        }
        Key::BackupNote => "Les sauvegardes restent chiffrées avec la phrase secrète en vigueur lors de leur création.",
        Key::Edit => "Modifier",
        Key::Profiles => "Profils",
        Key::ProfilesHint => "Chaque profil a ses propres comptes, opérations, documents et connexions bancaires.",
        Key::NewProfile => "Nouveau profil",
        Key::ProfileName => "Nom du profil",
        Key::RenameProfile => "Renommer %s",
        Key::DeleteProfile => "Supprimer %s",
        Key::DeleteProfileText => {
            "Les données de %s sont déplacées dans le dossier profiles-deleted. Rien n'est effacé : le dossier, documents non chiffrés compris, reste jusqu'à ce que vous le supprimiez vous-même."
        }
        Key::SwitchProfile => "Ouvrir au prochain démarrage",
        Key::ProfileOpen => "ouvert",
        Key::ProfileNextStart => "s'ouvre au prochain démarrage",
        Key::RestartToOpenProfile => "Redémarrez Finglass pour ouvrir %s.",
        Key::ProfileSwitchBusy => "Terminez d'abord la tâche en cours avant de changer de profil.",
        Key::WrongCurrencySkipped => {
            "%1$d {1:opération est|opérations sont} dans une autre devise que ce compte en %2$s et {1:sera ignorée|seront ignorées}."
        }
        Key::PlusOtherCurrencies => "+ %s dans d'autres devises",
        Key::OtherCurrenciesExcluded => {
            "Tous les montants en %1$s. %2$d {2:opération|opérations} dans d'autres devises {2:n'est pas incluse|ne sont pas incluses}."
        }
        Key::PrimaryCurrency => "Devise principale",
        Key::PrimaryCurrencyHint => {
            "Le tableau de bord, les budgets et les rapports ne totalisent que les opérations dans cette devise. Les montants ne sont jamais convertis."
        }
        Key::CurrencyIs => "Devise : %s",
        Key::CurrentBalanceIn => "Solde actuel (%s)",
        Key::Columns => "Colonnes",
        Key::Preview | Key::XlsxSheetOverview => "Aperçu",
        Key::ColDebit => "Débit",
        Key::ColCredit => "Crédit",
        Key::ColPartner | Key::DetailPartner => "Partenaire",
        Key::ColShop | Key::DetailShop => "Marchand",
        Key::NoneDash => "— aucun —",
        Key::ColumnN => "Colonne %d",
        Key::AndMore => "… et %d de plus",
        Key::SetAccountBalance => "Définir le solde du compte d'après l'écriture la plus récente du fichier",
        Key::SetAccountBalanceClosing => "Définir le solde du compte à %s (solde de clôture du fichier)",
        Key::SetAccountBalanceClosingPending => {
            "Définir le solde du compte à %1$s (solde de clôture du fichier sans ses %2$d écritures en attente)"
        }
        Key::NewerBookingsUnchanged => {
            "Le compte a des écritures plus récentes que ce fichier, son solde reste inchangé."
        }
        Key::BookingsSkippedHint => {
            "Les écritures déjà importées sont ignorées, ainsi que celles qui correspondent à une écriture existante d'une autre source (sync ou saisie manuelle) par date et montant."
        }
        Key::Importing => "Importation…",
        Key::ImportCount => "Importer %d",
        Key::ErrorImportTooManyFiles => "Sélectionnez au maximum %d fichiers à la fois.",
        Key::ErrorImportFilesTooLarge => "Les fichiers sélectionnés dépassent ensemble %d Mo.",
        Key::ErrorImportNoReadableFile => "Aucun des fichiers sélectionnés n'a pu être lu : %s",
        Key::ErrorImportDifferentAccounts => {
            "Les fichiers appartiennent à des comptes différents (%s). Importez un compte à la fois."
        }
        Key::ErrorImportStopped => {
            "Import interrompu à %s : %s. %d {3:fichier a été importé|fichiers ont été importés} avant."
        }
        Key::DeleteTransactionTitle => "Supprimer l'opération ?",
        Key::DocumentsCount => "%d {1:document|documents}",
        Key::AttachReceiptEllipsis => "Joindre un justificatif…",
        Key::NoDocumentsLinked => "Aucun document lié",
        Key::CategorizeSimilar => "Catégoriser automatiquement des écritures similaires",
        Key::PatternRegex => "Motif (regex)",
        Key::CreateRule => "Créer une règle",
        Key::TabDelimiter => "tabulation",
        Key::NewGroup => "Nouveau groupe",
        Key::GroupName => "Nom du groupe",
        Key::RenameGroup => "Renommer le groupe",
        Key::DeleteGroup => "Supprimer le groupe",
        Key::DeleteGroupText => "Supprimer le groupe « %s » ? Ses comptes retournent dans leur groupe par défaut.",
        Key::MoveUp => "Monter",
        Key::MoveDown => "Descendre",
        Key::MoveToGroup => "Déplacer vers un groupe…",
        Key::DefaultGroup => "Par défaut (selon le type)",
        Key::FilterAccounts => "Filtrer les comptes",
        Key::AccountsShown | Key::TaxReceiptsOf => "%1$d sur %2$d",
        Key::LastFetched => "Dernière récupération %s",
        Key::Collapse => "Réduire",
        Key::Expand => "Développer",
        Key::NewBookingsCount => "%d {1:nouvelle écriture|nouvelles écritures}",
        Key::NewBooking => "Nouvelle écriture",
        Key::MarkAsRead => "Marquer comme lu",
        Key::DragToGroupHint => "Déposez sur un groupe pour déplacer le compte",
        Key::CollapseSidebar => "Réduire la barre latérale",
        Key::ExpandSidebar => "Développer la barre latérale",
        Key::ChangeLanguage => "Changer de langue",
        Key::LanguageBusy => "Terminez d'abord la tâche en cours",
        Key::SwitchProfileMenu => "Changer de profil",
        Key::ManageProfiles => "Gérer les profils…",
        Key::BackToProfile | Key::BackTo => "Retour à %s",
        Key::From => "De",
        Key::OcrNotInstalled => {
            "Le texte des images et des PDF scannés ne peut être lu qu'une fois Tesseract installé (voir %s). Les PDF avec texte fonctionnent sans."
        }
        Key::Upload => "Téléverser",
        Key::UploadDocuments => "Téléverser des documents",
        Key::SearchDocuments => "Rechercher des documents par nom ou contenu…",
        Key::TabAllDocuments => "Tous les documents",
        Key::TabUnlinked => "Non liés",
        Key::TabLinked => "Liés à une opération",
        Key::TabWithCount => "%1$s (%2$d)",
        Key::NoDocuments => "Aucun document",
        Key::NoMatches => "Aucun résultat",
        Key::UploadHint => "Téléversez reçus, factures et relevés (PDF, JPG, PNG, TIFF).",
        Key::TryAnotherSearch => "Essayez un autre terme de recherche.",
        Key::DeleteDocumentTitle => "Supprimer le document ?",
        Key::DeleteDocumentText => "%s et sa copie stockée seront supprimés.",
        Key::NotLinked => "non lié",
        Key::TextExtracted | Key::ExtractedText => "Texte extrait",
        Key::NoText => "Pas de texte",
        Key::ReExtractText => "Réextraire le texte",
        Key::LinkedTransaction => "Opération liée",
        Key::SuggestedMatches => "Correspondances suggérées (montant et date du document)",
        Key::FindTransactionToLink => "Trouver une opération à lier…",
        Key::ExtractedTextConfidence => "Texte extrait (confiance %d%%)",
        Key::NoTextExtracted => "Aucun texte extrait.",
        Key::MatchPercent => "correspondance %s",
        Key::PreviewOf => "Aperçu de %s",
        Key::Export => "Exporter",
        Key::ExportPdf => "Exporter PDF",
        Key::ExportReport => "Exporter le rapport",
        Key::NothingToExport => "Aucune opération à exporter en %s",
        Key::BookingsCountTotal => "%1$d {1:opération|opérations} · %2$s",
        Key::TaxYear => "Année fiscale",
        Key::CustomRange => "Période personnalisée",
        Key::PreviousYear => "Année précédente",
        Key::NextYear => "Année suivante",
        Key::TabTaxSummary => "Résumé fiscal",
        Key::TabMonthlyBreakdown | Key::PdfMonthlyBreakdown => "Répartition mensuelle",
        Key::TabCategoryAnalysis => "Analyse par catégorie",
        Key::TabRecurring => "Récurrent",
        Key::NoTransactionsIn => "Aucune opération en %s",
        Key::ReportsBuiltFrom => "Les rapports sont construits à partir des opérations de la période sélectionnée.",
        Key::TotalIncome => "Revenus totaux",
        Key::TotalExpenses => "Dépenses totales",
        Key::NetIncome | Key::PdfNetIncome => "Revenu net",
        Key::TaxRelevantAmount => "Déductions fiscales",
        Key::TaxRelevantByCategory => "Dépenses fiscales par catégorie",
        Key::NoTaxRelevantExpenses => {
            "Aucune dépense fiscale. Marquez des catégories comme pertinentes dans les Paramètres (p. ex. matériel, dons, assurances)."
        }
        Key::CategoriesUsed => "Catégories utilisées",
        Key::AvgMonthlyExpenses => "Dépenses mensuelles moy.",
        Key::HighestMonth => "Mois le plus élevé",
        Key::MonthlyIncomeExpenses => "Revenus et dépenses mensuels – %s",
        Key::ColMonth | Key::PdfMonth => "Mois",
        Key::ColIncome | Key::ChartIncome | Key::TaxSectionIncome => "Revenus",
        Key::ColExpenses | Key::ChartExpenses => "Dépenses",
        Key::ColNet => "Net",
        Key::ReportTotal => "Total",
        Key::SpendingByCategory | Key::PdfExpensesByCategory => "Dépenses par catégorie",
        Key::TopVendors => "Principaux bénéficiaires",
        Key::FromDate => "Du",
        Key::ToDate => "Au",
        Key::Apply => "Appliquer",
        Key::NoRecurringFound => "Aucune écriture récurrente trouvée",
        Key::RecurringHint => {
            "Détecté sur les 13 derniers mois dès qu'un motif se répète au moins trois fois (annuel : deux fois)."
        }
        Key::RecurringPayments => "Paiements récurrents",
        Key::CostPerYear => "Coût par an",
        Key::PerMonth => "Par mois",
        Key::SubscriptionsTitle => "Abonnements, contrats et revenus réguliers",
        Key::ColEvery => "Tous les",
        Key::ColNext => "Prochain",
        Key::ColPerYear => "Par an",
        Key::TimesSeen => "vu %d fois",
        Key::Appearance => "Apparence",
        Key::DarkTheme => "Thème sombre",
        Key::MenuFile => "Fichier",
        Key::MenuView => "Présentation",
        Key::MenuFind => "Rechercher",
        Key::MenuWindow => "Fenêtre",
        Key::MenuHelp => "Aide",
        Key::MenuSettings => "Paramètres…",
        Key::MenuExit => "Quitter",
        Key::MenuEdit => "Modifier…",
        Key::MenuDetails => "Détails",
        Key::SyncNow => "Synchroniser maintenant",
        Key::ShowTransactions => "Afficher les opérations",
        Key::ShowAllFromPartner => "Tout afficher pour %s",
        Key::ShowInFinder => "Afficher dans le Finder",
        Key::ShowInExplorer => "Afficher dans l'Explorateur",
        Key::ShowInFolder => "Afficher dans le dossier",
        Key::MenuNewTransaction => "Nouvelle opération…",
        Key::MenuImportStatement => "Importer un relevé…",
        Key::MenuMinimize => "Placer dans le Dock",
        Key::MenuZoom => "Réduire/agrandir",
        Key::MenuGitHub => "Finglass sur GitHub",
        Key::MenuReportProblem => "Signaler un problème…",
        Key::AboutTitle => "À propos de Finglass",
        Key::AboutVersion => "Version %s",
        Key::AboutText => {
            "Votre espace financier : comptes, banque FinTS, documents et rapports fiscaux. Logiciel alpha – pas pour de vrais comptes bancaires."
        }
        Key::AboutTypeface => "Police : IBM Plex Sans (SIL Open Font License 1.1)",
        Key::AboutBankCodesSource => "Codes bancaires : Quelle: Deutsche Bundesbank",
        Key::Language => "Langue",
        Key::Categories => "Catégories",
        Key::AddCategory => "Ajouter une catégorie",
        Key::ColourSwatch => "Couleur %1$d sur %2$d",
        Key::FixDuplicateColours => "Donner à chaque catégorie sa propre couleur",
        Key::NoCategoriesYet => "Aucune catégorie pour l'instant.",
        Key::AddDefaultCategories => "Ajouter les catégories standard",
        Key::CategoryIsTransfer => "Virement entre vos propres comptes",
        Key::CategoryIsTransferHint => {
            "N'est pas compté comme revenu ou dépense. À utiliser pour les paiements de carte de crédit uniquement lorsque les propres opérations de la carte sont importées."
        }
        Key::CategoryTransferLabel => "virement",
        Key::CategorySplitMore => "%1$d autres {1:catégorie|catégories}",
        Key::CategorySplitTransfers => "Non compté : virements %s",
        Key::DefaultCategoryGroceries => "Alimentation",
        Key::DefaultCategoryDrugstore => "Droguerie & maison",
        Key::DefaultCategoryHousing => "Logement",
        Key::DefaultCategoryEnergy => "Énergie",
        Key::DefaultCategoryPhoneInternetMedia => "Téléphone, internet & médias",
        Key::DefaultCategoryInsurance => "Assurances",
        Key::DefaultCategoryLoans => "Crédits & financement",
        Key::DefaultCategorySavings => "Épargne & placements",
        Key::DefaultCategoryTransport => "Mobilité",
        Key::DefaultCategoryHealth => "Santé",
        Key::DefaultCategoryClothing => "Vêtements & chaussures",
        Key::DefaultCategoryOnlineShopping => "Achats en ligne",
        Key::DefaultCategoryLeisure => "Loisirs & associations",
        Key::DefaultCategoryRestaurants => "Restaurants & cafés",
        Key::DefaultCategoryTaxes => "Impôts & taxes",
        Key::DefaultCategoryCash => "Espèces",
        Key::DefaultCategoryCreditCard => "Carte de crédit",
        Key::DefaultCategoryOwnTransfers => "Virements internes",
        Key::DefaultCategorySalary => "Salaire",
        Key::DefaultCategoryChildBenefit => "Allocations familiales",
        Key::DefaultCategoryInterestFees => "Intérêts & frais bancaires",
        Key::DefaultCategoryFamily => "Famille",
        Key::DefaultCategoryChildrenSchool => "Enfants & école",
        Key::DefaultCategoryOtherIncome => "Autres revenus",
        Key::BudgetAmount => "Budget %s",
        Key::AutoCategorizationRules => "Règles de catégorisation auto.",
        Key::AddRule => "Ajouter une règle",
        Key::RulesHint => {
            "Le motif d'une règle est une expression régulière insensible à la casse, appliquée au nom du partenaire et au libellé, ou à l'un des deux seulement, pour les dépenses, les revenus ou les deux. La priorité la plus élevée l'emporte ; à priorité égale, la règle la plus ancienne."
        }
        Key::NoRulesYet => "Aucune règle pour l'instant. Ajoutez-en une ou importez un fichier de règles.",
        Key::Prio => "prio %d",
        Key::Manual => "manuel",
        Key::RuleSearchIn => "Rechercher dans",
        Key::RuleFieldAny => "Partenaire et libellé",
        Key::RuleFieldPartner => "Nom du partenaire uniquement",
        Key::RuleFieldPurpose => "Libellé uniquement",
        Key::RuleAppliesTo => "S'applique à",
        Key::RuleDirectionAny => "Dépenses et revenus",
        Key::RuleDirectionExpense => "Dépenses uniquement",
        Key::RuleDirectionIncome => "Revenus uniquement",
        Key::RuleCategoryLabel => "Catégorie : %s",
        Key::ImportRules => "Importer des règles…",
        Key::ExportRules => "Exporter les règles…",
        Key::RuleImportTitle => "Importer des règles",
        Key::RuleImportCategorize => "Catégoriser %d {1:opération sans catégorie|opérations sans catégorie} maintenant",
        Key::RuleImportNewRules => "%d nouvelle{1:s||} {1:règle|règles}",
        Key::RuleImportCategories => "%d {1:catégorie à créer|catégories à créer}",
        Key::RuleImportDuplicates => "%d doublon{1:s||} ignoré{1:s||}",
        Key::RuleImportConflicts => "%d {1:conflit ignoré|conflits ignorés}",
        Key::RuleImportErrors => "%d {1:erreur de ligne|erreurs de ligne}",
        Key::RuleImportConflictLine => "Ligne %d : %s → %s (existante : %s)",
        Key::RuleImportErrorLine => "Ligne %d : %s",
        Key::MessageRulesExported => "%d {1:règle exportée|règles exportées}",
        Key::MessageRulesExportSkipped => {
            "%1$d {1:règle exportée|règles exportées}, %2$d ignorée{2:s||} (tabulation ou saut de ligne dans le motif ou la catégorie)"
        }
        Key::MessageRulesImported => {
            "%1$d {1:règle ajoutée|règles ajoutées} et %2$d {2:catégorie ajoutée|catégories ajoutées}, %3$d {3:opération catégorisée|opérations catégorisées}"
        }
        Key::RuleFileErrorColumns => "6 colonnes attendues, %d trouvées",
        Key::RuleFileErrorCategory => "La catégorie est vide ou dépasse 100 caractères",
        Key::RuleFileErrorPattern => "Motif invalide : %s",
        Key::RuleFileErrorPatternLength => "Motif de plus de 1000 caractères",
        Key::RuleFileErrorSearchIn => "Valeur search_in inconnue : %s (any, partner, purpose)",
        Key::RuleFileErrorAppliesTo => "Valeur applies_to inconnue : %s (any, expense, income)",
        Key::RuleFileErrorPriority => "La priorité n'est pas un nombre entier : %s",
        Key::RuleFileErrorAutoApply => "auto_apply doit être true ou false : %s",
        Key::ErrorRuleFileHeader => "Pas un fichier de règles Finglass : ligne d'en-tête absente ou incorrecte",
        Key::ErrorRuleFileTooLarge => "Fichier de règles trop volumineux (1 Mo et 5000 règles au plus)",
        Key::ErrorRuleFileEncoding => "Le fichier de règles n'est pas un texte UTF-8 valide",
        Key::DataAndOcr => "Données & OCR",
        Key::Database => "Base de données",
        Key::TesseractAvailable => "OCR Tesseract disponible (allemand + anglais)",
        Key::TesseractMissing => "Tesseract introuvable. Installation : brew install tesseract tesseract-lang",
        Key::BackupAndRestore => "Sauvegarde et restauration",
        Key::BackupHint => {
            "Une sauvegarde contient la base de données, tous les documents stockés et les fichiers de connexion bancaire."
        }
        Key::CreateBackup => "Créer une sauvegarde",
        Key::RestoreFromBackup => "Restaurer depuis une sauvegarde…",
        Key::RestoreWaiting => "Une restauration est en attente. Redémarrez Finglass pour l'appliquer.",
        Key::CancelRestore => "Annuler la restauration",
        Key::BackupRestoredAt => "Sauvegarde restaurée au démarrage. Vos données précédentes sont dans %s",
        Key::AuditLogLatest => "Journal d'audit (100 derniers)",
        Key::AuditLogShort => "Journal",
        Key::AuditLogEmpty => "Aucune entrée pour l'instant.",
        Key::RestoreFileTitle => "Restaurer %s ?",
        Key::RestoreCreated => "Créé %s · %d {2:opération|opérations} · %d {3:document|documents}",
        Key::RestoreExplanation => {
            "Au prochain démarrage, toutes les données actuelles seront remplacées par cette sauvegarde. Les données actuelles sont déplacées dans un dossier pre-restore, pas supprimées ; elles y restent, documents non chiffrés compris, jusqu'à ce que vous le supprimiez vous-même."
        }
        Key::PrepareRestore => "Préparer la restauration",
        Key::DeleteCategoryTitle => "Supprimer la catégorie %s ?",
        Key::DeleteCategoryText => "Ses opérations deviennent sans catégorie et ses règles sont supprimées.",
        Key::DeleteCategoryUsage => "Utilisée par %d {1:opération|opérations}.",
        Key::DeleteRuleTitle => "Supprimer la règle %s ?",
        Key::DeleteRuleText => "Les opérations déjà catégorisées par elle gardent leur catégorie.",
        Key::EditCategory => "Modifier la catégorie",
        Key::MonthlyBudgetOptional => "Budget mensuel (€, facultatif)",
        Key::TaxRelevantHint => "Fiscalement pertinent (frais professionnels, dons, …)",
        Key::BudgetNotANumber => "Le budget n'est pas un nombre",
        Key::EditRule => "Modifier la règle",
        Key::PatternExample => "Motif (p. ex. rewe|edeka|aldi)",
        Key::Priority => "Priorité",
        Key::ApplyAutomatically => "Appliquer automatiquement",
        Key::PriorityMustBeNumber => "La priorité doit être un nombre entier",
        Key::SaveBackup => "Enregistrer la sauvegarde",
        Key::RestoreBackup => "Restaurer la sauvegarde",
        Key::PinFor => "PIN pour %s",
        Key::PinExplanation => "PIN de banque en ligne pour l'utilisateur %s.",
        Key::RememberPinForSyncs => "Mémoriser le PIN pour les syncs",
        Key::RememberPinExplanation => {
            "Conservé dans le magasin d'identifiants sécurisé de votre système, jamais dans les fichiers de Finglass."
        }
        Key::Pin => "PIN",
        Key::ChooseTanProcedure => "Choisir la procédure TAN",
        Key::ChooseTanMedium => "Choisir le support TAN",
        Key::TanRequired => "TAN requis",
        Key::TanDefaultMessage => "Saisissez le TAN affiché par votre procédure TAN.",
        Key::TanChallenge => "Défi TAN",
        Key::TanFlickerUnsupported => {
            "Les codes clignotants chipTAN ne sont pas encore pris en charge dans cette version. Saisissez le TAN affiché sur le générateur."
        }
        Key::ConfirmInBankingApp => "Confirmer dans votre application bancaire",
        Key::IConfirmedIt => "J'ai confirmé",
        Key::MessageFromBank => "Message de votre banque",
        Key::NotEnoughData => "Pas assez de données",
        Key::Max => "max",
        Key::BalanceHistory => "Historique du solde",
        Key::AxisNotFromZero => "L'échelle ne commence pas à zéro",
        Key::MessageAdded => "%s ajouté",
        Key::MessageAccountAdded => "Compte ajouté",
        Key::MessageAccountDeleted => "Compte supprimé",
        Key::MessageDeletedForgotten => {
            "%d {1:opération supprimée peut être réimportée|opérations supprimées peuvent être réimportées}"
        }
        Key::MessageNoDeletedRemembered => "Aucune opération supprimée n'est mémorisée pour ce compte",
        Key::MessageBankRemoved => "Banque supprimée",
        Key::MessageConnectionSaved => "Paramètres de connexion enregistrés",
        Key::SyncConnecting => "Connexion…",
        Key::MessageTransactionAdded => "Opération ajoutée",
        Key::MessageRuleCreated => {
            "Règle créée et appliquée à %d {1:opération non catégorisée|opérations non catégorisées}"
        }
        Key::MessageDefaultCategoriesAdded => "%d catégories standard ajoutées",
        Key::MessageDefaultCategoriesNone => "Toutes les catégories standard existent déjà",
        Key::MessageColoursFixed => "%d {1:catégorie recoloriée|catégories recoloriées}",
        Key::MessageColoursAlreadyDistinct => "Chaque catégorie a déjà sa propre couleur",
        Key::MessageAutoCategorized => {
            "%d sur %d {2:opération non catégorisée|opérations non catégorisées} {1:a été catégorisée|ont été catégorisées}"
        }
        Key::MessageExportedCount => "%d {1:opération exportée|opérations exportées} vers %s",
        Key::MessageBackupSaved => {
            "Sauvegarde enregistrée dans %s (%d {2:opération|opérations}, %d {3:document|documents})"
        }
        Key::MessageRestorePrepared => {
            "Restauration préparée. Redémarrez Finglass pour la terminer ; vos données actuelles seront conservées dans un dossier pre-restore."
        }
        Key::MessageRestoreCancelled => "Restauration en attente annulée",
        Key::MessageProfileMoved => "Profil déplacé vers %s",
        Key::MessageSavedTo => "%s enregistré",
        Key::PinNotSaved => "Le PIN n'a pas pu être enregistré dans le magasin d'identifiants sécurisé du système.",
        Key::MessageUploaded => "%d {1:fichier importé|fichiers importés}, texte extrait de %d",
        Key::UploadProgress => "Téléversement de %1$d sur %2$d…",
        Key::MessageDocumentLinked => "Document lié",
        Key::MessageDocumentDeleted => "Document supprimé",
        Key::MessageNoTextExtracted => "Aucun texte n'a pu être extrait",
        Key::BankNameRequired => "Le nom de la banque est requis",
        Key::BlzMustBe8Digits => "Le code bancaire doit comporter 8 chiffres",
        Key::ErrorConnectionBusy => "Attendez la fin de la synchronisation de cette banque.",
        Key::ErrorDeleteWhileBusy => "Attendez la fin de la synchronisation ou de l'importation en cours.",
        Key::IbanRequired => "L'IBAN est requis",
        Key::IbanChecksumFailed => "L'IBAN n'est pas valide (somme de contrôle erronée)",
        Key::AccountIbanExists => "Un compte avec cet IBAN existe déjà",
        Key::ErrorAmountNotZero => "Le montant ne doit pas être nul",
        Key::DuplicateTransactionTitle => "Ajouter à nouveau la même opération ?",
        Key::DuplicateTransactionText => {
            "Ce compte a déjà une opération le %s de %s avec le même partenaire et le même libellé. En ajouter une deuxième, par exemple pour un achat répété ?"
        }
        Key::AddAnyway => "Ajouter quand même",
        Key::ErrorExportFailed => "Échec de l'export",
        Key::ErrorNameRequired => "Le nom est requis",
        Key::ErrorColorFormat => "La couleur doit ressembler à #3BC7B0",
        Key::ErrorCategoryExists => "La catégorie existe déjà",
        Key::ErrorPatternRequired => "Le motif est requis",
        Key::ErrorInvalidRegex => "Expression régulière non valide : %s",
        Key::ErrorPdfExportFailed => "Échec de l'export PDF",
        Key::ErrorCsvExportFailed => "Échec de l'export CSV",
        Key::ErrorSyncFromDateFuture => "La date de début ne doit pas être dans le futur",
        Key::ErrorAccountTypeInvalid => "Type de compte inconnu",
        Key::ErrorBankConnectionNotFound => "Connexion bancaire introuvable",
        Key::ErrorAccountNotFound => "Compte introuvable",
        Key::ErrorSyncFailed => "Échec de la synchronisation",
        Key::ErrorBankRejected => "La banque a rejeté la demande",
        Key::ErrorBankRejectedStatements => "La banque a rejeté la demande de relevés",
        Key::StatementsNotSupported => "Les relevés ne sont pas pris en charge pour ce compte",
        Key::ErrorNoFintsServer => "Aucun serveur FinTS connu pour le code %s. Saisissez l'URL FinTS de la banque.",
        Key::ErrorBadServerUrl => "URL du serveur FinTS non valide",
        Key::ErrorEnterUserId => {
            "Saisissez votre identifiant bancaire en ligne (Anmeldename/Legitimations-ID) pour %s."
        }
        Key::ErrorConnectionNotFound => "Connexion bancaire %d introuvable",
        Key::ErrorCancelled => "Annulé",
        Key::ErrorCancelledByUser => "Annulé par l'utilisateur",
        Key::ErrorDialogFailed => "Échec du dialogue FinTS",
        Key::HintNeedsAccount => "Ajoutez d'abord un compte",
        Key::HintNeedsCategory => "Ajoutez d'abord une catégorie",
        Key::HintNothingToExport => "Aucune opération à exporter dans cette vue",
        Key::ErrorBankUnreachable => {
            "Le serveur FinTS de la banque est injoignable. Vérifiez l'URL du serveur et votre connexion Internet."
        }
        Key::ErrorServerHttpStatus => {
            "Le serveur de la banque a répondu avec le code HTTP %d. Vérifiez que l'accès FinTS/HBCI est activé dans votre banque en ligne et que le code banque (BLZ) et l'URL du serveur de cette connexion sont corrects, ou réessayez plus tard."
        }
        Key::ErrorServerNotFints => {
            "Le serveur de la banque a renvoyé une réponse qui n'est pas du FinTS (%s). Vérifiez l'URL du serveur FinTS dans les paramètres de connexion. Si elle est correcte, la banque bloque peut-être la requête : réessayez plus tard ou capturez une trace du protocole dans les paramètres."
        }
        Key::ErrorBankRejectedPin => "La banque a rejeté le code PIN",
        Key::ErrorPassportReset => {
            "Finglass n'a pas pu réinitialiser l'enregistrement bancaire enregistré. Supprimez cette banque puis ajoutez-la à nouveau."
        }
        Key::ProtocolTraceTitle => "Trace du protocole FinTS",
        Key::ProtocolTraceHint => {
            "Enregistre un journal des prochains dialogues bancaires pour diagnostiquer les problèmes de connexion. Le PIN, le TAN, l'identifiant et les numéros de compte sont masqués, mais le journal contient les réponses complètes de votre banque : noms, libellés, montants et soldes. Relisez-le avant de le partager. Les traces restent en mémoire jusqu'à leur enregistrement et ne sont jamais envoyées automatiquement."
        }
        Key::ProtocolTraceEnable => "Collecter la trace du protocole FinTS",
        Key::ProtocolTraceSave => "Enregistrer la trace",
        Key::ProtocolTraceAvailable => "Une trace est prête à être enregistrée.",
        Key::ProtocolTraceNone => {
            "Aucune trace capturée pour l'instant. Activez la collecte, reproduisez le problème (par ex. lancez une synchronisation), puis enregistrez-la ici."
        }
        Key::MessageProtocolTraceSaved => "Trace enregistrée dans %s",
        Key::ErrorNoProtocolTrace => {
            "Aucune trace capturée pour l'instant. Activez la collecte et reproduisez d'abord le problème."
        }
        Key::ProgressConnecting => "Connexion à la banque…",
        Key::ProgressSavingAccount => "Enregistrement de %s…",
        Key::SyncSummary => "%d {1:compte synchronisé|comptes synchronisés}",
        Key::SyncSummaryNew | Key::SyncSummaryTransactions => " : %d {1:nouvelle opération|nouvelles opérations}",
        Key::SyncSummaryCategorized | Key::ImportSummaryAuto => ", %d {1:catégorisée|catégorisées} automatiquement",
        Key::SyncSummaryDuplicates | Key::ImportSummaryKnown => ", %d déjà {1:connue|connues}",
        Key::SyncSummaryHistoryFrom => " Historique demandé depuis le %s.",
        Key::BalanceNotProvided => {
            "…%s : la banque ne fournit pas de solde pour ce compte (%s). Saisissez-le avec « Modifier le solde », un crédit en montant négatif."
        }
        Key::BalanceNotProvidedNoCode => {
            "…%s : la banque ne fournit pas de solde pour ce compte. Saisissez-le avec « Modifier le solde », un crédit en montant négatif."
        }
        Key::SyncGapNotFetched => {
            "Les opérations du %s au %s n’ont pas été récupérées : Finglass récupère au plus les %d derniers jours. Utilisez « Récupérer l'historique depuis… » pour les obtenir (la banque peut demander un TAN)."
        }
        Key::SyncAccountWithoutIban => {
            "%s : la banque indique ce compte sans IBAN ; Finglass ne peut pas encore l’enregistrer."
        }
        Key::SyncBalanceOtherCurrency => {
            "…%s : la banque indique le solde en %s, mais le compte est tenu en %s ; le solde n’a pas été modifié."
        }
        Key::SyncCurrencyCorrected => {
            "…%s : la devise du compte a été corrigée de %s en %s, car la banque indique le solde dans cette devise et ne précise pas de devise de compte."
        }
        Key::PendingHeader => "En attente (pas encore comptabilisé) · au %s",
        Key::ImportSummary => "%d {1:opération importée|opérations importées}",
        Key::ImportSummaryFiles => "%d {1:opération importée|opérations importées} depuis %d fichiers",
        Key::ImportSummaryProbable => {
            ", %d {1:doublon probable|doublons probables} d'opérations existantes {1:ignoré|ignorés}"
        }
        Key::ImportSummaryWrongCurrency => ", %d dans une autre devise que le compte {1:ignorée|ignorées}",
        Key::ImportSummaryBalance => ". Solde défini à %s",
        Key::ImportSummaryDeleted => ", %d {1:supprimée|supprimées} auparavant, {1:ignorée|ignorées}",
        Key::ErrorFileNotFound => "Fichier introuvable : %s",
        Key::ErrorFileTooLarge => "Le fichier est trop volumineux",
        Key::ErrorImportSeveralAccounts => {
            "Ce fichier contient des opérations de %d comptes (%s). Exportez un fichier par compte et importez-les un par un."
        }
        Key::ErrorXlsxTooLarge => "Le fichier Excel est trop volumineux ou endommagé.",
        Key::ErrorXlsxUnreadable => "Le fichier Excel n'a pas pu être lu.",
        Key::ErrorFileDoesNotExist => "Le fichier n'existe pas : %s",
        Key::ErrorUnsupportedFileType => "Type de fichier non pris en charge : %s",
        Key::ErrorUnreadableImage => "Image illisible : %s",
        Key::ErrorDocumentTooLarge => "%s est trop volumineux (%d Mo au maximum)",
        Key::ErrorImageTooLarge => "%s contient trop de pixels pour être lu",
        Key::ReportUnknownCategory => "Inconnu",
        Key::ReportTitleTax => "Rapport fiscal Finglass %d",
        Key::ReportTitleRange => "Rapport Finglass %s",
        Key::PdfGenerated => "Généré le %s",
        Key::PdfSummary => "Résumé",
        Key::PdfTotalIncome => "Total des revenus",
        Key::PdfTotalExpenses => "Total des dépenses",
        Key::PdfTaxRelevantExpenses => "Dépenses fiscalement pertinentes",
        Key::PdfAllAmountsIn => {
            "Tous les montants en %s. %d {2:opération|opérations} dans d'autres devises {2:n'est pas incluse|ne sont pas incluses}."
        }
        Key::PdfTaxByCategory => "Dépenses fiscalement pertinentes par catégorie",
        Key::PdfNoTaxRelevant => "Aucune catégorie n'est marquée comme fiscalement pertinente.",
        Key::PdfIncomeExpensesNet => "Revenus / Dépenses / Net",
        Key::PdfTaxTransactions => "Opérations fiscalement pertinentes",
        Key::ColCurrency => "Devise",
        Key::ChartOther => "Autres",
        Key::ChartSoFar => "en cours",
        Key::ChartPartialMonthNote => "Pâle : mois en cours",
        Key::FlickerSpeed => "Vitesse",
        Key::FlickerSize => "Taille",
        Key::FlickerHint => {
            "Placez le générateur de TAN contre les barres, de sorte que ses flèches s'alignent sur les triangles."
        }
        Key::TanLabel => "TAN",
        Key::AppLogo => "Logo Finglass",
        Key::EditCategoryNamed => "Modifier la catégorie : %s",
        Key::EditRuleNamed => "Modifier la règle : %s",
        Key::OpenPreviewNamed => "Ouvrir l'aperçu : %s",
        Key::BankOfflineHint => "Hors ligne : importez les relevés dans « Opérations ».",
        Key::IbanOrCardLast4 => "IBAN ou 4 derniers chiffres de la carte",
        Key::CardLast4Only => "Saisissez uniquement les 4 derniers chiffres de la carte.",
        Key::ErrorBankOffline => "Cette banque est hors ligne : rien n'est envoyé à la banque.",
        Key::BankOnlineMode => "Banque en ligne (FinTS)",
        Key::BankOfflineMode => "Hors ligne (import de relevés uniquement)",
        Key::AccountsWithoutBank => "Comptes sans banque",
        Key::ReapplyRules => "Réappliquer les règles aux opérations affichées",
        Key::ReapplyRulesTitle => "Réappliquer les règles ?",
        Key::ReapplyRulesSummary => {
            "Nouvelle catégorie pour %1$d des %2$d {2:opération affichée|opérations affichées}."
        }
        Key::ReapplyRulesWarning => {
            "Les catégories définies à la main sont aussi remplacées. Filtrez d'abord la liste si nécessaire. Vous pourrez ensuite annuler depuis le menu."
        }
        Key::ReapplyRulesConfirm => "Modifier %d",
        Key::UndoReapplyRules => "Annuler la réapplication des règles",
        Key::MessageReapplyNothing => "Aucune opération affichée ne changerait de catégorie",
        Key::MessageReapplied => "%d {1:opération recatégorisée|opérations recatégorisées}",
        Key::MessageReapplyUndone => "%d {1:opération restaurée|opérations restaurées}",
        Key::HintNothingShown => "Aucune opération affichée",
        Key::SelectedCount => "%d {1:sélectionné|sélectionnés}",
        Key::DeleteSelected => "Supprimer…",
        Key::ClearSelection => "Effacer la sélection",
        Key::DeleteSelectedCount => "Supprimer %d {1:sélectionnée|sélectionnées}",
        Key::SelectAll => "Tout sélectionner",
        Key::DeleteTransactionsTitle => "Supprimer %d {1:opération|opérations} ?",
        Key::DeleteSyncedHint => {
            "Les opérations supprimées sont mémorisées : un import ou une synchronisation bancaire ultérieur ne les rajoutera pas. Pour les réautoriser, utilisez « Réautoriser les opérations supprimées » dans le menu du compte sur l'écran des comptes. Les justificatifs liés sont conservés et seulement déliés."
        }
        Key::MessageTransactionsDeleted => "%d {1:opération supprimée|opérations supprimées}",
        Key::TaxAutomatic => "Automatique",
        Key::TaxRelevantChoice => "Fiscalement pertinent",
        Key::TaxNotRelevantChoice => "Non fiscalement pertinent",
        Key::TaxSetByYou => "%s · défini par vous",
        Key::TaxFromRule => "%1$s · via la règle « %2$s »",
        Key::TaxFromCategory => "%1$s · via la catégorie « %2$s »",
        Key::TaxMenu => "Impôt",
        Key::MarkTaxRelevant => "Marquer comme fiscalement pertinent",
        Key::MarkNotTaxRelevant => "Marquer comme non fiscalement pertinent",
        Key::ResetTaxAutomatic => "Rétablir l'automatique",
        Key::RuleTaxAsCategory => "Comme la catégorie",
        Key::RuleTaxMarkerNo => "sans impôt",
        Key::MessageTaxOverrideSet => "Statut fiscal modifié pour %d {1:opération|opérations}",
        Key::MessageCategorySet => "Catégorie modifiée pour %d {1:opération|opérations}",
        Key::TabTaxBookings => "Écritures fiscales",
        Key::TaxIncomeTotal => "Revenus fiscalement pertinents",
        Key::TaxDeductionTotal | Key::TaxSectionDeductions => "Déductions",
        Key::TaxReceipts => "Justificatifs",
        Key::ReceiptsMissing => "%d manquants",
        Key::ReceiptsComplete => "Complets",
        Key::TaxSuggestionsTitle => "Possiblement fiscalement pertinent : %1$d {1:écriture|écritures}",
        Key::TaxSuggestionsHint => {
            "Trouvé par mot-clé. Accepter marque l'écriture comme fiscalement pertinente ; rejeter masque la suggestion définitivement. Les mots-clés sont modifiables dans les réglages."
        }
        Key::MatchesKeyword => "Correspond à « %s »",
        Key::NoReceipt => "Aucun justificatif",
        Key::MissingReceiptsOnly => "Seulement les justificatifs manquants",
        Key::ShowAllCount => "Tout afficher (%d)",
        Key::NoTaxBookingsIn => "Aucune écriture fiscalement pertinente dans %s",
        Key::TaxBookingsHowTo => {
            "Marquez une catégorie comme fiscalement pertinente dans Réglages → Catégories, donnez à une règle une option impôt, ou marquez des écritures dans la liste des opérations."
        }
        Key::TaxSuggestionKeywords => "Suggestions fiscales",
        Key::TaxKeywordsCount => "%1$d {1:mot-clé|mots-clés}",
        Key::ResetToDefaults => "Rétablir les valeurs par défaut",
        Key::Accept => "Accepter",
        Key::DismissSuggestion => "Ignorer",
        Key::ExportExcel => "Exporter Excel",
        Key::Print => "Imprimer…",
        Key::OpenTaxBookings => "Ouvrir les écritures fiscales",
        Key::XlsxSheetBookings => "Écritures",
        Key::TaxExportTitle => "Écritures fiscalement pertinentes %s",
        Key::TaxColSection => "Section",
        Key::TaxColWithReceipt => "Avec justificatif",
        Key::TaxColReceipt => "Justificatif",
        Key::TaxColSource => "Source",
        Key::Yes => "Oui",
        Key::No => "Non",
        Key::PdfPageOf => "Page %1$d sur %2$d",
        Key::MessagePrintSent => "Envoyé à l'imprimante",
        Key::ErrorTaxExportFailed => "L'export du rapport fiscal a échoué",
        Key::ErrorPrintFailed => "L'impression a échoué",
        Key::ComingSoon => "Cet écran arrivera dans une étape ultérieure.",
        Key::Security => "Sécurité",
        Key::ThemeSystem => "Système",
        Key::ThemeLight => "Clair",
        Key::ThemeDark => "Sombre",
        Key::Theme => "Thème",
        Key::ErrorFinDeskNotFound => "Aucune base de données FinDesk n'a été trouvée dans le dossier choisi.",
        Key::ErrorFinDeskWrongPassphrase => "La phrase secrète FinDesk est incorrecte.",
        Key::ErrorFinDeskNotFinDesk => "Cette base de données n'est pas une base de données FinDesk.",
        Key::ErrorFinDeskTargetNotEmpty => {
            "Le profil Finglass contient déjà des données. Créez un nouveau profil dans la section Profils et importez-y."
        }
        Key::FinDeskImportSummary => {
            "Importé : %d groupes de comptes, %d connexions bancaires, %d comptes, %d catégories, %d règles, \
             %d opérations, %d écritures supprimées, %d écritures en attente, %d documents, %d textes OCR et \
             %d paramètres. Ignoré : %d lignes, %d documents avec un fichier manquant et %d documents refusés. \
             %d ordres de paiement laissés de côté."
        }
        Key::FinDeskImportTitle => "Importer depuis FinDesk",
        Key::FinDeskImportHint => {
            "Copie les comptes, connexions bancaires, catégories, règles, écritures et documents de FinDesk dans le \
             profil ouvert. Les ordres de paiement et les codes PIN mémorisés ne sont pas importés. Quittez FinDesk d'abord."
        }
        Key::FinDeskImportChooseFolder => "Choisir le dossier FinDesk…",
        Key::FinDeskPassphraseTitle => "Phrase secrète FinDesk",
        Key::FinDeskPassphraseText => {
            "Saisissez la phrase secrète de la base FinDesk sélectionnée. Elle n'est utilisée que pour cet import et \
             n'est pas enregistrée."
        }
        Key::FinDeskImportReportTitle => "Import FinDesk terminé",
        Key::FinDeskImportFailedTitle => "Échec de l'import FinDesk",
        Key::FinDeskImportSkipped => {
            "Ignorés : %d lignes sans leur compte, catégorie ou document, %d lignes invalides, %d avec une fraction de centime et %d paramètres que Finglass n'utilise pas."
        }
        Key::ErrorFinDeskNotAFolder => "Le dossier choisi n'est pas un dossier FinDesk.",
        Key::ErrorNotEncrypted => {
            "Ce dossier de données contient une base non chiffrée que Finglass ne peut pas ouvrir."
        }
        Key::ErrorDatabaseTooNew => "Cette base de données a été créée par une version plus récente de Finglass.",
        Key::ErrorAlreadyRunning => "Finglass est déjà en cours d'exécution avec ce dossier de données.",
        Key::ErrorStartupFailed => {
            "Finglass n'a pas pu préparer les données de ce profil au démarrage. Quittez Finglass et réessayez."
        }
        Key::ErrorOnlineSyncUnavailable => {
            "La synchronisation en ligne n'est pas encore disponible dans cette version."
        }
        Key::ErrorServerUrlRequired => "L'URL du serveur FinTS est requise",
        Key::ErrorServerUrlMustBeHttps => "L'URL du serveur FinTS doit commencer par https://",
        Key::ErrorUnexpected => "une erreur inattendue",
        Key::MigrationWelcomeTitle => "FinDesk trouvé sur ce Mac",
        Key::MigrationWelcomeText => {
            "Finglass peut reprendre vos profils FinDesk maintenant. Chaque profil FinDesk devient un profil \
             Finglass du même nom."
        }
        Key::MigrationProfilesLabel => "Profils FinDesk",
        Key::MigrationCopiedText => {
            "Les comptes, connexions bancaires, catégories, règles, écritures et documents sont copiés. Les ordres \
             de paiement et les codes PIN mémorisés ne le sont pas. Les connexions bancaires devront se reconnecter."
        }
        Key::MigrationQuitHint => "Quittez FinDesk d'abord.",
        Key::MigrationStart => "Reprendre depuis FinDesk",
        Key::MigrationStartEmpty => "Commencer vide",
        Key::MigrationProfileHeading => "Profil %1$d sur %2$d : %3$s",
        Key::MigrationFindeskPassphrase => "Phrase secrète FinDesk",
        Key::MigrationSamePassphrase => "Utiliser la même phrase secrète pour Finglass",
        Key::MigrationNewPassphraseNeeded => {
            "La phrase secrète FinDesk ne respecte pas les règles d'une nouvelle phrase secrète Finglass. \
             Choisissez-en une nouvelle."
        }
        Key::MigrationMigrateProfile => "Reprendre le profil",
        Key::MigrationSkip => "Ignorer",
        Key::MigrationSummaryTitle => "Migration terminée",
        Key::MigrationSummaryText => "Ces profils FinDesk ont été traités :",
        Key::MigrationContinue => "Continuer",
        Key::MigrationProfileMigrated => "%1$s : %2$s",
        Key::MigrationProfileSkipped => "%1$s : ignoré",
        Key::MigrationProfileFailed => "%1$s : %2$s",
        Key::MigrationBusy => "Migration…",
        Key::ErrorFinDeskSourceChanged => "FinDesk semble être en cours d'exécution. Quittez FinDesk et réessayez.",
        Key::ErrorMigrationTargetNotEmpty => "Le profil Finglass contient déjà des données. Rien n'a été migré.",
    }
}
