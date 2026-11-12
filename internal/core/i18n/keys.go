package i18n

// Key identifies a user-visible string.
//
// The constant names match FinDesk's StringKey.
type Key int

// The user-visible strings, in the same order as the Rust Key::ALL.
const (
	Cancel Key = iota
	Save
	Add
	Delete
	Close
	Ok
	Open
	Back
	Dismiss
	More
	ClearSearch
	Uncategorized
	TaxRelevant
	Tax
	ToggleTheme
	Link
	Unlink
	OpenDetails
	ChangeCategory
	OpenAccount
	OpenDocument
	ViewPendingTasks
	EnterValidDate
	EnterValidAmount
	EnterValidBalance
	InvalidDateRange
	PatternInvalid
	OpticalTanCode
	WindowTitle
	NavDashboard
	NavAccounts
	NavTransactions
	NavDocuments
	NavReports
	NavSettings
	TypeChecking
	TypeSavings
	TypeCreditCard
	TypeLoan
	TypeDepot
	PeriodThisMonth
	PeriodLastMonth
	PeriodLast3Months
	PeriodThisYear
	PeriodLastYear
	PeriodAllTime
	PeriodCustomRange
	DirectionAll
	DirectionIncome
	DirectionExpenses
	DirectionAnyLabel
	CadenceWeekly
	CadenceMonthly
	CadenceQuarterly
	CadenceYearly
	WelcomeTitle
	AddAccount
	AvailableBalance
	LoansSubtitle
	IncomeThisMonth
	ExpensesThisMonth
	PendingTasks
	PendingSubtitle
	ExpensesByCategory
	NoExpensesThisMonth
	IncomeVsExpenses
	Upcoming30Days
	AmountVaries
	BudgetsThisMonth
	SpentOfLimit
	OverBudget
	LeftBudget
	TaxDeductibleYtd
	TaxDeductibleHint
	RecentTransactions
	ViewAll
	NoTransactionsYet
	GettingStartedIntro
	StepAddBank
	StepAddBankHint
	StepSync
	StepSyncHint
	StepCategorize
	StepCategorizeHint
	OpenSettings
	NothingPending
	NoBookingsSixMonths
	PreviousMonth
	NextMonth
	CurrentMonth
	SelectMonth
	IncomeInMonth
	ExpensesInMonth
	BudgetsInMonth
	NoExpensesInMonth
	ExpensesByCategoryInMonth
	NoBookingsSixMonthsTo
	AvailableAsOf
	CarryForwardFrom
	CarryForwardLine
	OtherMovements
	OtherMovementsLabel
	AccountsTotal
	AccountsTotalLabel
	AccountsNetLabel
	AccountsLoansLabel
	AccountCount
	AddBank
	NoBanksYet
	NoBanksDescription
	BlzPrefix
	BicPrefix
	SyncedPrefix
	Syncing
	Sync
	SyncAll
	SyncAllProgress
	IncludeInSyncAll
	NotInSyncAll
	SyncAllNothingIncluded
	SyncAllStopped
	FetchHistory
	FetchHistoryFor
	FetchHistoryDate
	FetchHistoryHint
	Fetch
	Reset
	ClearHistoryStart
	NextSyncFrom
	ConnectionSettings
	RenameConnection
	ConnectionName
	ConnectionNameHint
	AddAccountShort
	RemoveBank
	NoAccountsYet
	Transactions
	EditBalance
	DeleteAccount
	AllowDeletedAgain
	BlzDigits
	BankName
	BicOptional
	OnlineUserIdOptional
	NoFintsServer
	OnlineSyncAvailable
	OnlineSyncNotYetAvailable
	BankSearchHint
	BankSearchNoMatch
	BankManualEntry
	BankUnknownBlz
	BankFintsProposed
	BankUseSuggestion
	AddAccountAt
	Iban
	BalanceNotANumber
	UpdateBalance
	OnlineBankingFor
	UserIdLogin
	FintsServerUrl
	LeaveEmptyToUse
	AskBankForFints
	RememberedTan
	PinNeverStored
	PinSavedForSyncs
	ForgetPin
	PinAskedEverySync
	DaysCount
	OneYear
	LastDaysSummary
	RecentBookings
	NoneInPeriod
	HistoryReconstructed
	AllTransactions
	DeleteAccountTitle
	DeleteAccountText
	RenameAccount
	ChangeAccountType
	AccountTypeLabel
	LoanBalanceHint
	AccountName
	AccountNameHint
	RemoveBankTitle
	RemoveBankText
	CentralBlzNote
	CentralBlzHint
	UseBlz
	BlzNotInDirectory
	BlzOtherBank
	LoginResetNote
	Import
	AutoCategorize
	ExportCsv
	SearchPartnerPurpose
	AllAccounts
	TransactionsSummary
	SummaryCount
	SummaryIn
	SummaryOut
	SummaryBalance
	TransactionsBalance
	BalanceBankMismatch
	ColDate
	ColDescription
	ColCategory
	ColAmount
	NoTransactions
	NoTransactionsAddBank
	NothingMatches
	ImportBankStatement
	AttachReceipt
	ExportTransactions
	AllCategories
	MinAmountIn
	MaxAmountIn
	AmountFilterAny
	AddTransaction
	Expense
	Income
	DateFormat
	AmountIn
	DelimiterLabel
	PartnerPayee
	Purpose
	CategoryAuto
	AutoRules
	ImportFile
	BookingCount
	PendingLeftOut
	UnreadableCount
	IntoAccount
	ChooseAccount
	FileBelongsTo
	ImportFiles
	FileBelongsToNamed
	ColumnsApplyToSame
	ImportFilesSkipped
	UnlockWindowTitle
	UnlockProfile
	UnlockHeading
	UnlockText
	Passphrase
	CurrentPassphrase
	NewPassphrase
	RepeatPassphrase
	ShowPassphrase
	Unlock
	ChangePassphrase
	ChangePassphraseText
	ChangeAndOpen
	CreatePassphraseHeading
	CreatePassphraseText
	EncryptPassphraseHeading
	EncryptPassphraseText
	PassphraseHint
	DocumentsNotEncrypted
	PassphraseNoRecovery
	EncryptAndOpen
	Encrypting
	Unlocking
	WrongPassphrase
	PassphraseTooShort
	PassphraseSpaces
	PassphraseMismatch
	PassphraseAcknowledge
	UnlockFailed
	RestoredEncryptedHint
	RestoredPlainHint
	PlaintextLeftoversTitle
	PlaintextLeftoversText
	PlaintextLeftoverPreRestore
	PlaintextLeftoverDeletedProfile
	PlaintextLeftoverPendingRestore
	PlaintextLeftoverOtherProfile
	UnencryptedLeftoversTitle
	UnencryptedLeftoversText
	EncryptionStatus
	EncryptionOff
	BackupPassphraseTitle
	BackupPassphraseText
	BackupNote
	Edit
	Profiles
	ProfilesHint
	NewProfile
	ProfileName
	RenameProfile
	DeleteProfile
	DeleteProfileText
	SwitchProfile
	ProfileOpen
	ProfileNextStart
	RestartToOpenProfile
	ProfileSwitchBusy
	WrongCurrencySkipped
	PlusOtherCurrencies
	OtherCurrenciesExcluded
	PrimaryCurrency
	PrimaryCurrencyHint
	CurrencyIs
	CurrentBalanceIn
	Columns
	Preview
	ColDebit
	ColCredit
	ColPartner
	ColShop
	ColPurpose
	ColBalance
	NoneDash
	ColumnN
	AndMore
	SetAccountBalance
	SetAccountBalanceClosing
	SetAccountBalanceClosingPending
	NewerBookingsUnchanged
	BookingsSkippedHint
	Importing
	ImportCount
	ErrorImportTooManyFiles
	ErrorImportFilesTooLarge
	ErrorImportNoReadableFile
	ErrorImportDifferentAccounts
	ErrorImportStopped
	DeleteTransactionTitle
	DocumentsCount
	DetailDate
	DetailAccount
	DetailPartner
	DetailShop
	DetailPurpose
	DetailCategory
	AttachReceiptEllipsis
	NoDocumentsLinked
	CategorizeSimilar
	PatternRegex
	CreateRule
	TabDelimiter
	NewGroup
	GroupName
	RenameGroup
	DeleteGroup
	DeleteGroupText
	MoveUp
	MoveDown
	MoveToGroup
	DefaultGroup
	FilterAccounts
	AccountsShown
	LastFetched
	Collapse
	Expand
	NewBookingsCount
	NewBooking
	MarkAsRead
	DragToGroupHint
	CollapseSidebar
	ExpandSidebar
	ChangeLanguage
	LanguageBusy
	SwitchProfileMenu
	ManageProfiles
	OpenProfile
	BackToProfile
	From
	OcrNotInstalled
	Upload
	UploadDocuments
	SearchDocuments
	TabAllDocuments
	TabUnlinked
	TabLinked
	TabWithCount
	NoDocuments
	NoMatches
	UploadHint
	TryAnotherSearch
	DeleteDocumentTitle
	DeleteDocumentText
	NotLinked
	TextExtracted
	NoText
	ReExtractText
	LinkedTransaction
	SuggestedMatches
	FindTransactionToLink
	ExtractedText
	ExtractedTextConfidence
	NoTextExtracted
	MatchPercent
	PreviewOf
	Export
	ExportPdf
	ExportReport
	NothingToExport
	BookingsCountTotal
	TaxYear
	CustomRange
	PreviousYear
	NextYear
	TabTaxSummary
	TabMonthlyBreakdown
	TabCategoryAnalysis
	TabRecurring
	NoTransactionsIn
	ReportsBuiltFrom
	TotalIncome
	TotalExpenses
	NetIncome
	TaxRelevantAmount
	TaxRelevantByCategory
	NoTaxRelevantExpenses
	StatTransactions
	CategoriesUsed
	AvgMonthlyExpenses
	HighestMonth
	MonthlyIncomeExpenses
	ColMonth
	ColIncome
	ColExpenses
	ColNet
	ReportTotal
	SpendingByCategory
	TopVendors
	FromDate
	ToDate
	Apply
	NoRecurringFound
	RecurringHint
	RecurringPayments
	CostPerYear
	PerMonth
	SubscriptionsTitle
	ColName
	ColEvery
	ColNext
	ColPerYear
	TimesSeen
	Appearance
	DarkTheme
	MenuFile
	MenuView
	MenuFind
	MenuWindow
	MenuHelp
	MenuSettings
	MenuExit
	MenuEdit
	MenuDetails
	SyncNow
	ShowTransactions
	ShowAllFromPartner
	ShowInFinder
	ShowInExplorer
	ShowInFolder
	MenuNewTransaction
	MenuImportStatement
	MenuMinimize
	MenuZoom
	MenuGitHub
	MenuReportProblem
	AboutTitle
	AboutVersion
	AboutText
	AboutTypeface
	AboutBankCodesSource
	LanguageKey // Rust's Language variant; the Go type already uses that name.
	Categories
	AddCategory
	CategoryUsage
	ColourSwatch
	FixDuplicateColours
	NoCategoriesYet
	AddDefaultCategories
	CategoryIsTransfer
	CategoryIsTransferHint
	CategoryTransferLabel
	CategorySplitMore
	CategorySplitTransfers
	DefaultCategoryGroceries
	DefaultCategoryDrugstore
	DefaultCategoryHousing
	DefaultCategoryEnergy
	DefaultCategoryPhoneInternetMedia
	DefaultCategoryInsurance
	DefaultCategoryLoans
	DefaultCategorySavings
	DefaultCategoryTransport
	DefaultCategoryHealth
	DefaultCategoryClothing
	DefaultCategoryOnlineShopping
	DefaultCategoryLeisure
	DefaultCategoryRestaurants
	DefaultCategoryTaxes
	DefaultCategoryCash
	DefaultCategoryCreditCard
	DefaultCategoryOwnTransfers
	DefaultCategorySalary
	DefaultCategoryChildBenefit
	DefaultCategoryInterestFees
	DefaultCategoryFamily
	DefaultCategoryChildrenSchool
	DefaultCategoryOtherIncome
	BudgetAmount
	AutoCategorizationRules
	AddRule
	RulesHint
	NoRulesYet
	Prio
	Manual
	RuleSearchIn
	RuleFieldAny
	RuleFieldPartner
	RuleFieldPurpose
	RuleAppliesTo
	RuleDirectionAny
	RuleDirectionExpense
	RuleDirectionIncome
	RuleCategoryLabel
	ImportRules
	ExportRules
	RuleImportTitle
	RuleImportCategorize
	RuleImportNewRules
	RuleImportCategories
	RuleImportDuplicates
	RuleImportConflicts
	RuleImportErrors
	RuleImportConflictLine
	RuleImportErrorLine
	MessageRulesExported
	MessageRulesExportSkipped
	MessageRulesImported
	RuleFileErrorColumns
	RuleFileErrorCategory
	RuleFileErrorPattern
	RuleFileErrorPatternLength
	RuleFileErrorSearchIn
	RuleFileErrorAppliesTo
	RuleFileErrorPriority
	RuleFileErrorAutoApply
	ErrorRuleFileHeader
	ErrorRuleFileTooLarge
	ErrorRuleFileEncoding
	DataAndOcr
	Database
	DocumentsFolder
	TesseractAvailable
	TesseractMissing
	BackupAndRestore
	BackupHint
	CreateBackup
	RestoreFromBackup
	RestoreWaiting
	CancelRestore
	BackupRestoredAt
	AuditLogLatest
	AuditLogShort
	AuditLogEmpty
	RestoreFileTitle
	RestoreCreated
	RestoreExplanation
	PrepareRestore
	DeleteCategoryTitle
	DeleteCategoryText
	DeleteCategoryUsage
	DeleteRuleTitle
	DeleteRuleText
	EditCategory
	Name
	MonthlyBudgetOptional
	TaxRelevantHint
	BudgetNotANumber
	EditRule
	PatternExample
	Priority
	ApplyAutomatically
	PriorityMustBeNumber
	SaveBackup
	RestoreBackup
	PinFor
	PinExplanation
	RememberPinForSyncs
	RememberPinExplanation
	Pin
	ChooseTanProcedure
	ChooseTanMedium
	TanRequired
	TanDefaultMessage
	TanChallenge
	TanFlickerUnsupported
	ConfirmInBankingApp
	IConfirmedIt
	MessageFromBank
	NotEnoughData
	Max
	MinAmount
	BalanceHistory
	AxisNotFromZero
	MessageAdded
	MessageAccountAdded
	MessageAccountDeleted
	MessageDeletedForgotten
	MessageNoDeletedRemembered
	MessageBankRemoved
	MessageConnectionSaved
	SyncConnecting
	MessageTransactionAdded
	MessageRuleCreated
	MessageDefaultCategoriesAdded
	MessageDefaultCategoriesNone
	MessageColoursFixed
	MessageColoursAlreadyDistinct
	MessageAutoCategorized
	MessageExportedCount
	MessageBackupSaved
	MessageRestorePrepared
	MessageRestoreCancelled
	MessageProfileMoved
	MessageSavedTo
	PinNotSaved
	MessageUploaded
	UploadProgress
	MessageDocumentLinked
	MessageDocumentDeleted
	MessageNoTextExtracted
	BankNameRequired
	BlzMustBe8Digits
	ErrorConnectionBusy
	ErrorDeleteWhileBusy
	IbanRequired
	IbanChecksumFailed
	AccountIbanExists
	ErrorAmountNotZero
	DuplicateTransactionTitle
	DuplicateTransactionText
	AddAnyway
	ErrorExportFailed
	ErrorNameRequired
	ErrorColorFormat
	ErrorCategoryExists
	ErrorPatternRequired
	ErrorInvalidRegex
	ErrorPdfExportFailed
	ErrorCsvExportFailed
	ErrorSyncFromDateInvalid
	ErrorSyncFromDateFuture
	ErrorAccountTypeInvalid
	ErrorBankConnectionNotFound
	ErrorAccountNotFound
	ErrorSyncFailed
	ErrorBankRejected
	ErrorBankRejectedStatements
	StatementsNotSupported
	ErrorNoFintsServer
	ErrorBadServerUrl
	ErrorEnterUserId
	ErrorConnectionNotFound
	ErrorCancelled
	ErrorCancelledByUser
	ErrorDialogFailed
	HintNeedsAccount
	HintNeedsCategory
	HintNothingToExport
	ErrorBankUnreachable
	ErrorServerHttpStatus
	ErrorServerNotFints
	ErrorBankRejectedPin
	ErrorPassportReset
	ProtocolTraceTitle
	ProtocolTraceHint
	ProtocolTraceEnable
	ProtocolTraceSave
	ProtocolTraceAvailable
	ProtocolTraceNone
	MessageProtocolTraceSaved
	ErrorNoProtocolTrace
	ProgressConnecting
	ProgressSavingAccount
	SyncSummary
	SyncSummaryNew
	SyncSummaryTransactions
	SyncSummaryCategorized
	SyncSummaryDuplicates
	SyncSummaryHistoryFrom
	BalanceNotProvided
	BalanceNotProvidedNoCode
	SyncGapNotFetched
	SyncAccountWithoutIban
	SyncBalanceOtherCurrency
	SyncCurrencyCorrected
	PendingHeader
	ImportSummary
	ImportSummaryFiles
	ImportSummaryAuto
	ImportSummaryKnown
	ImportSummaryProbable
	ImportSummaryWrongCurrency
	ImportSummaryBalance
	ImportSummaryDeleted
	ErrorFileNotFound
	ErrorFileTooLarge
	ErrorImportSeveralAccounts
	ErrorXlsxTooLarge
	ErrorXlsxUnreadable
	ErrorFileDoesNotExist
	ErrorUnsupportedFileType
	ErrorUnreadableImage
	ErrorDocumentTooLarge
	ErrorImageTooLarge
	ReportUnknownCategory
	ReportTitleTax
	ReportTitleRange
	PdfGenerated
	PdfSummary
	PdfTotalIncome
	PdfTotalExpenses
	PdfNetIncome
	PdfTaxRelevantExpenses
	PdfTransactionCount
	PdfAllAmountsIn
	PdfTaxByCategory
	PdfNoTaxRelevant
	PdfMonthlyBreakdown
	PdfMonth
	PdfIncomeExpensesNet
	PdfExpensesByCategory
	PdfTaxTransactions
	ColCurrency
	ColAccount
	ChartOther
	ChartIncome
	ChartExpenses
	ChartSoFar
	ChartPartialMonthNote
	FlickerSpeed
	FlickerSize
	FlickerHint
	TanLabel
	AppLogo
	EditCategoryNamed
	EditRuleNamed
	OpenPreviewNamed
	BankOfflineHint
	IbanOrCardLast4
	CardLast4Only
	ErrorBankOffline
	BankOnlineMode
	BankOfflineMode
	AccountsWithoutBank
	ReapplyRules
	ReapplyRulesTitle
	ReapplyRulesSummary
	ReapplyRulesWarning
	ReapplyRulesConfirm
	UndoReapplyRules
	MessageReapplyNothing
	MessageReapplied
	MessageReapplyUndone
	HintNothingShown
	SelectedCount
	DeleteSelected
	ClearSelection
	DeleteSelectedCount
	SelectAll
	DeleteTransactionsTitle
	DeleteSyncedHint
	MessageTransactionsDeleted
	TaxAutomatic
	TaxRelevantChoice
	TaxNotRelevantChoice
	TaxSetByYou
	TaxFromRule
	TaxFromCategory
	CategoryMenu
	TaxMenu
	MarkTaxRelevant
	MarkNotTaxRelevant
	ResetTaxAutomatic
	RuleTaxAsCategory
	RuleTaxMarkerYes
	RuleTaxMarkerNo
	MessageTaxOverrideSet
	MessageCategorySet
	TabTaxBookings
	TaxIncomeTotal
	TaxDeductionTotal
	TaxReceipts
	TaxReceiptsOf
	ReceiptsMissing
	ReceiptsComplete
	TaxSuggestionsTitle
	TaxSuggestionsHint
	MatchesKeyword
	TaxSectionIncome
	TaxSectionDeductions
	NoReceipt
	MissingReceiptsOnly
	ShowAllCount
	NoTaxBookingsIn
	TaxBookingsHowTo
	TaxSuggestionKeywords
	TaxKeywordsCount
	ResetToDefaults
	Accept
	DismissSuggestion
	ExportExcel
	Print
	OpenTaxBookings
	XlsxSheetOverview
	XlsxSheetBookings
	TaxExportTitle
	TaxColSection
	TaxColBookings
	TaxColWithReceipt
	TaxColReceipt
	TaxColSource
	Yes
	No
	PdfPageOf
	MessagePrintSent
	ErrorTaxExportFailed
	ErrorPrintFailed
	BackTo
	ComingSoon
	Security
	ThemeSystem
	ThemeLight
	ThemeDark
	Theme
	ErrorFinDeskNotFound
	ErrorFinDeskWrongPassphrase
	ErrorFinDeskNotFinDesk
	ErrorFinDeskTargetNotEmpty
	FinDeskImportSummary
	FinDeskImportTitle
	FinDeskImportHint
	FinDeskImportChooseFolder
	FinDeskPassphraseTitle
	FinDeskPassphraseText
	FinDeskImportReportTitle
	FinDeskImportFailedTitle
	FinDeskImportSkipped
	ErrorFinDeskNotAFolder
	ErrorNotEncrypted
	ErrorDatabaseTooNew
	ErrorAlreadyRunning
	ErrorStartupFailed
	ErrorOnlineSyncUnavailable
	ErrorServerUrlRequired
	ErrorServerUrlMustBeHttps
	ErrorUnexpected
	MigrationWelcomeTitle
	MigrationWelcomeText
	MigrationProfilesLabel
	MigrationCopiedText
	MigrationQuitHint
	MigrationStart
	MigrationStartEmpty
	MigrationProfileHeading
	MigrationFindeskPassphrase
	MigrationSamePassphrase
	MigrationNewPassphraseNeeded
	MigrationMigrateProfile
	MigrationSkip
	MigrationSummaryTitle
	MigrationSummaryText
	MigrationContinue
	MigrationProfileMigrated
	MigrationProfileSkipped
	MigrationProfileFailed
	MigrationBusy
	ErrorFinDeskSourceChanged
	ErrorMigrationTargetNotEmpty
)

// NumKeys is the number of keys.
const NumKeys = 874

// keyNames holds the variant name of every key, indexed by Key.
var keyNames = [NumKeys]string{ // #nosec G101 -- key names such as "Pin", not credentials.
	Cancel:                            "Cancel",
	Save:                              "Save",
	Add:                               "Add",
	Delete:                            "Delete",
	Close:                             "Close",
	Ok:                                "Ok",
	Open:                              "Open",
	Back:                              "Back",
	Dismiss:                           "Dismiss",
	More:                              "More",
	ClearSearch:                       "ClearSearch",
	Uncategorized:                     "Uncategorized",
	TaxRelevant:                       "TaxRelevant",
	Tax:                               "Tax",
	ToggleTheme:                       "ToggleTheme",
	Link:                              "Link",
	Unlink:                            "Unlink",
	OpenDetails:                       "OpenDetails",
	ChangeCategory:                    "ChangeCategory",
	OpenAccount:                       "OpenAccount",
	OpenDocument:                      "OpenDocument",
	ViewPendingTasks:                  "ViewPendingTasks",
	EnterValidDate:                    "EnterValidDate",
	EnterValidAmount:                  "EnterValidAmount",
	EnterValidBalance:                 "EnterValidBalance",
	InvalidDateRange:                  "InvalidDateRange",
	PatternInvalid:                    "PatternInvalid",
	OpticalTanCode:                    "OpticalTanCode",
	WindowTitle:                       "WindowTitle",
	NavDashboard:                      "NavDashboard",
	NavAccounts:                       "NavAccounts",
	NavTransactions:                   "NavTransactions",
	NavDocuments:                      "NavDocuments",
	NavReports:                        "NavReports",
	NavSettings:                       "NavSettings",
	TypeChecking:                      "TypeChecking",
	TypeSavings:                       "TypeSavings",
	TypeCreditCard:                    "TypeCreditCard",
	TypeLoan:                          "TypeLoan",
	TypeDepot:                         "TypeDepot",
	PeriodThisMonth:                   "PeriodThisMonth",
	PeriodLastMonth:                   "PeriodLastMonth",
	PeriodLast3Months:                 "PeriodLast3Months",
	PeriodThisYear:                    "PeriodThisYear",
	PeriodLastYear:                    "PeriodLastYear",
	PeriodAllTime:                     "PeriodAllTime",
	PeriodCustomRange:                 "PeriodCustomRange",
	DirectionAll:                      "DirectionAll",
	DirectionIncome:                   "DirectionIncome",
	DirectionExpenses:                 "DirectionExpenses",
	DirectionAnyLabel:                 "DirectionAnyLabel",
	CadenceWeekly:                     "CadenceWeekly",
	CadenceMonthly:                    "CadenceMonthly",
	CadenceQuarterly:                  "CadenceQuarterly",
	CadenceYearly:                     "CadenceYearly",
	WelcomeTitle:                      "WelcomeTitle",
	AddAccount:                        "AddAccount",
	AvailableBalance:                  "AvailableBalance",
	LoansSubtitle:                     "LoansSubtitle",
	IncomeThisMonth:                   "IncomeThisMonth",
	ExpensesThisMonth:                 "ExpensesThisMonth",
	PendingTasks:                      "PendingTasks",
	PendingSubtitle:                   "PendingSubtitle",
	ExpensesByCategory:                "ExpensesByCategory",
	NoExpensesThisMonth:               "NoExpensesThisMonth",
	IncomeVsExpenses:                  "IncomeVsExpenses",
	Upcoming30Days:                    "Upcoming30Days",
	AmountVaries:                      "AmountVaries",
	BudgetsThisMonth:                  "BudgetsThisMonth",
	SpentOfLimit:                      "SpentOfLimit",
	OverBudget:                        "OverBudget",
	LeftBudget:                        "LeftBudget",
	TaxDeductibleYtd:                  "TaxDeductibleYtd",
	TaxDeductibleHint:                 "TaxDeductibleHint",
	RecentTransactions:                "RecentTransactions",
	ViewAll:                           "ViewAll",
	NoTransactionsYet:                 "NoTransactionsYet",
	GettingStartedIntro:               "GettingStartedIntro",
	StepAddBank:                       "StepAddBank",
	StepAddBankHint:                   "StepAddBankHint",
	StepSync:                          "StepSync",
	StepSyncHint:                      "StepSyncHint",
	StepCategorize:                    "StepCategorize",
	StepCategorizeHint:                "StepCategorizeHint",
	OpenSettings:                      "OpenSettings",
	NothingPending:                    "NothingPending",
	NoBookingsSixMonths:               "NoBookingsSixMonths",
	PreviousMonth:                     "PreviousMonth",
	NextMonth:                         "NextMonth",
	CurrentMonth:                      "CurrentMonth",
	SelectMonth:                       "SelectMonth",
	IncomeInMonth:                     "IncomeInMonth",
	ExpensesInMonth:                   "ExpensesInMonth",
	BudgetsInMonth:                    "BudgetsInMonth",
	NoExpensesInMonth:                 "NoExpensesInMonth",
	ExpensesByCategoryInMonth:         "ExpensesByCategoryInMonth",
	NoBookingsSixMonthsTo:             "NoBookingsSixMonthsTo",
	AvailableAsOf:                     "AvailableAsOf",
	CarryForwardFrom:                  "CarryForwardFrom",
	CarryForwardLine:                  "CarryForwardLine",
	OtherMovements:                    "OtherMovements",
	OtherMovementsLabel:               "OtherMovementsLabel",
	AccountsTotal:                     "AccountsTotal",
	AccountsTotalLabel:                "AccountsTotalLabel",
	AccountsNetLabel:                  "AccountsNetLabel",
	AccountsLoansLabel:                "AccountsLoansLabel",
	AccountCount:                      "AccountCount",
	AddBank:                           "AddBank",
	NoBanksYet:                        "NoBanksYet",
	NoBanksDescription:                "NoBanksDescription",
	BlzPrefix:                         "BlzPrefix",
	BicPrefix:                         "BicPrefix",
	SyncedPrefix:                      "SyncedPrefix",
	Syncing:                           "Syncing",
	Sync:                              "Sync",
	SyncAll:                           "SyncAll",
	SyncAllProgress:                   "SyncAllProgress",
	IncludeInSyncAll:                  "IncludeInSyncAll",
	NotInSyncAll:                      "NotInSyncAll",
	SyncAllNothingIncluded:            "SyncAllNothingIncluded",
	SyncAllStopped:                    "SyncAllStopped",
	FetchHistory:                      "FetchHistory",
	FetchHistoryFor:                   "FetchHistoryFor",
	FetchHistoryDate:                  "FetchHistoryDate",
	FetchHistoryHint:                  "FetchHistoryHint",
	Fetch:                             "Fetch",
	Reset:                             "Reset",
	ClearHistoryStart:                 "ClearHistoryStart",
	NextSyncFrom:                      "NextSyncFrom",
	ConnectionSettings:                "ConnectionSettings",
	RenameConnection:                  "RenameConnection",
	ConnectionName:                    "ConnectionName",
	ConnectionNameHint:                "ConnectionNameHint",
	AddAccountShort:                   "AddAccountShort",
	RemoveBank:                        "RemoveBank",
	NoAccountsYet:                     "NoAccountsYet",
	Transactions:                      "Transactions",
	EditBalance:                       "EditBalance",
	DeleteAccount:                     "DeleteAccount",
	AllowDeletedAgain:                 "AllowDeletedAgain",
	BlzDigits:                         "BlzDigits",
	BankName:                          "BankName",
	BicOptional:                       "BicOptional",
	OnlineUserIdOptional:              "OnlineUserIdOptional",
	NoFintsServer:                     "NoFintsServer",
	OnlineSyncAvailable:               "OnlineSyncAvailable",
	OnlineSyncNotYetAvailable:         "OnlineSyncNotYetAvailable",
	BankSearchHint:                    "BankSearchHint",
	BankSearchNoMatch:                 "BankSearchNoMatch",
	BankManualEntry:                   "BankManualEntry",
	BankUnknownBlz:                    "BankUnknownBlz",
	BankFintsProposed:                 "BankFintsProposed",
	BankUseSuggestion:                 "BankUseSuggestion",
	AddAccountAt:                      "AddAccountAt",
	Iban:                              "Iban",
	BalanceNotANumber:                 "BalanceNotANumber",
	UpdateBalance:                     "UpdateBalance",
	OnlineBankingFor:                  "OnlineBankingFor",
	UserIdLogin:                       "UserIdLogin",
	FintsServerUrl:                    "FintsServerUrl",
	LeaveEmptyToUse:                   "LeaveEmptyToUse",
	AskBankForFints:                   "AskBankForFints",
	RememberedTan:                     "RememberedTan",
	PinNeverStored:                    "PinNeverStored",
	PinSavedForSyncs:                  "PinSavedForSyncs",
	ForgetPin:                         "ForgetPin",
	PinAskedEverySync:                 "PinAskedEverySync",
	DaysCount:                         "DaysCount",
	OneYear:                           "OneYear",
	LastDaysSummary:                   "LastDaysSummary",
	RecentBookings:                    "RecentBookings",
	NoneInPeriod:                      "NoneInPeriod",
	HistoryReconstructed:              "HistoryReconstructed",
	AllTransactions:                   "AllTransactions",
	DeleteAccountTitle:                "DeleteAccountTitle",
	DeleteAccountText:                 "DeleteAccountText",
	RenameAccount:                     "RenameAccount",
	ChangeAccountType:                 "ChangeAccountType",
	AccountTypeLabel:                  "AccountTypeLabel",
	LoanBalanceHint:                   "LoanBalanceHint",
	AccountName:                       "AccountName",
	AccountNameHint:                   "AccountNameHint",
	RemoveBankTitle:                   "RemoveBankTitle",
	RemoveBankText:                    "RemoveBankText",
	CentralBlzNote:                    "CentralBlzNote",
	CentralBlzHint:                    "CentralBlzHint",
	UseBlz:                            "UseBlz",
	BlzNotInDirectory:                 "BlzNotInDirectory",
	BlzOtherBank:                      "BlzOtherBank",
	LoginResetNote:                    "LoginResetNote",
	Import:                            "Import",
	AutoCategorize:                    "AutoCategorize",
	ExportCsv:                         "ExportCsv",
	SearchPartnerPurpose:              "SearchPartnerPurpose",
	AllAccounts:                       "AllAccounts",
	TransactionsSummary:               "TransactionsSummary",
	SummaryCount:                      "SummaryCount",
	SummaryIn:                         "SummaryIn",
	SummaryOut:                        "SummaryOut",
	SummaryBalance:                    "SummaryBalance",
	TransactionsBalance:               "TransactionsBalance",
	BalanceBankMismatch:               "BalanceBankMismatch",
	ColDate:                           "ColDate",
	ColDescription:                    "ColDescription",
	ColCategory:                       "ColCategory",
	ColAmount:                         "ColAmount",
	NoTransactions:                    "NoTransactions",
	NoTransactionsAddBank:             "NoTransactionsAddBank",
	NothingMatches:                    "NothingMatches",
	ImportBankStatement:               "ImportBankStatement",
	AttachReceipt:                     "AttachReceipt",
	ExportTransactions:                "ExportTransactions",
	AllCategories:                     "AllCategories",
	MinAmountIn:                       "MinAmountIn",
	MaxAmountIn:                       "MaxAmountIn",
	AmountFilterAny:                   "AmountFilterAny",
	AddTransaction:                    "AddTransaction",
	Expense:                           "Expense",
	Income:                            "Income",
	DateFormat:                        "DateFormat",
	AmountIn:                          "AmountIn",
	DelimiterLabel:                    "DelimiterLabel",
	PartnerPayee:                      "PartnerPayee",
	Purpose:                           "Purpose",
	CategoryAuto:                      "CategoryAuto",
	AutoRules:                         "AutoRules",
	ImportFile:                        "ImportFile",
	BookingCount:                      "BookingCount",
	PendingLeftOut:                    "PendingLeftOut",
	UnreadableCount:                   "UnreadableCount",
	IntoAccount:                       "IntoAccount",
	ChooseAccount:                     "ChooseAccount",
	FileBelongsTo:                     "FileBelongsTo",
	ImportFiles:                       "ImportFiles",
	FileBelongsToNamed:                "FileBelongsToNamed",
	ColumnsApplyToSame:                "ColumnsApplyToSame",
	ImportFilesSkipped:                "ImportFilesSkipped",
	UnlockWindowTitle:                 "UnlockWindowTitle",
	UnlockProfile:                     "UnlockProfile",
	UnlockHeading:                     "UnlockHeading",
	UnlockText:                        "UnlockText",
	Passphrase:                        "Passphrase",
	CurrentPassphrase:                 "CurrentPassphrase",
	NewPassphrase:                     "NewPassphrase",
	RepeatPassphrase:                  "RepeatPassphrase",
	ShowPassphrase:                    "ShowPassphrase",
	Unlock:                            "Unlock",
	ChangePassphrase:                  "ChangePassphrase",
	ChangePassphraseText:              "ChangePassphraseText",
	ChangeAndOpen:                     "ChangeAndOpen",
	CreatePassphraseHeading:           "CreatePassphraseHeading",
	CreatePassphraseText:              "CreatePassphraseText",
	EncryptPassphraseHeading:          "EncryptPassphraseHeading",
	EncryptPassphraseText:             "EncryptPassphraseText",
	PassphraseHint:                    "PassphraseHint",
	DocumentsNotEncrypted:             "DocumentsNotEncrypted",
	PassphraseNoRecovery:              "PassphraseNoRecovery",
	EncryptAndOpen:                    "EncryptAndOpen",
	Encrypting:                        "Encrypting",
	Unlocking:                         "Unlocking",
	WrongPassphrase:                   "WrongPassphrase",
	PassphraseTooShort:                "PassphraseTooShort",
	PassphraseSpaces:                  "PassphraseSpaces",
	PassphraseMismatch:                "PassphraseMismatch",
	PassphraseAcknowledge:             "PassphraseAcknowledge",
	UnlockFailed:                      "UnlockFailed",
	RestoredEncryptedHint:             "RestoredEncryptedHint",
	RestoredPlainHint:                 "RestoredPlainHint",
	PlaintextLeftoversTitle:           "PlaintextLeftoversTitle",
	PlaintextLeftoversText:            "PlaintextLeftoversText",
	PlaintextLeftoverPreRestore:       "PlaintextLeftoverPreRestore",
	PlaintextLeftoverDeletedProfile:   "PlaintextLeftoverDeletedProfile",
	PlaintextLeftoverPendingRestore:   "PlaintextLeftoverPendingRestore",
	PlaintextLeftoverOtherProfile:     "PlaintextLeftoverOtherProfile",
	UnencryptedLeftoversTitle:         "UnencryptedLeftoversTitle",
	UnencryptedLeftoversText:          "UnencryptedLeftoversText",
	EncryptionStatus:                  "EncryptionStatus",
	EncryptionOff:                     "EncryptionOff",
	BackupPassphraseTitle:             "BackupPassphraseTitle",
	BackupPassphraseText:              "BackupPassphraseText",
	BackupNote:                        "BackupNote",
	Edit:                              "Edit",
	Profiles:                          "Profiles",
	ProfilesHint:                      "ProfilesHint",
	NewProfile:                        "NewProfile",
	ProfileName:                       "ProfileName",
	RenameProfile:                     "RenameProfile",
	DeleteProfile:                     "DeleteProfile",
	DeleteProfileText:                 "DeleteProfileText",
	SwitchProfile:                     "SwitchProfile",
	ProfileOpen:                       "ProfileOpen",
	ProfileNextStart:                  "ProfileNextStart",
	RestartToOpenProfile:              "RestartToOpenProfile",
	ProfileSwitchBusy:                 "ProfileSwitchBusy",
	WrongCurrencySkipped:              "WrongCurrencySkipped",
	PlusOtherCurrencies:               "PlusOtherCurrencies",
	OtherCurrenciesExcluded:           "OtherCurrenciesExcluded",
	PrimaryCurrency:                   "PrimaryCurrency",
	PrimaryCurrencyHint:               "PrimaryCurrencyHint",
	CurrencyIs:                        "CurrencyIs",
	CurrentBalanceIn:                  "CurrentBalanceIn",
	Columns:                           "Columns",
	Preview:                           "Preview",
	ColDebit:                          "ColDebit",
	ColCredit:                         "ColCredit",
	ColPartner:                        "ColPartner",
	ColShop:                           "ColShop",
	ColPurpose:                        "ColPurpose",
	ColBalance:                        "ColBalance",
	NoneDash:                          "NoneDash",
	ColumnN:                           "ColumnN",
	AndMore:                           "AndMore",
	SetAccountBalance:                 "SetAccountBalance",
	SetAccountBalanceClosing:          "SetAccountBalanceClosing",
	SetAccountBalanceClosingPending:   "SetAccountBalanceClosingPending",
	NewerBookingsUnchanged:            "NewerBookingsUnchanged",
	BookingsSkippedHint:               "BookingsSkippedHint",
	Importing:                         "Importing",
	ImportCount:                       "ImportCount",
	ErrorImportTooManyFiles:           "ErrorImportTooManyFiles",
	ErrorImportFilesTooLarge:          "ErrorImportFilesTooLarge",
	ErrorImportNoReadableFile:         "ErrorImportNoReadableFile",
	ErrorImportDifferentAccounts:      "ErrorImportDifferentAccounts",
	ErrorImportStopped:                "ErrorImportStopped",
	DeleteTransactionTitle:            "DeleteTransactionTitle",
	DocumentsCount:                    "DocumentsCount",
	DetailDate:                        "DetailDate",
	DetailAccount:                     "DetailAccount",
	DetailPartner:                     "DetailPartner",
	DetailShop:                        "DetailShop",
	DetailPurpose:                     "DetailPurpose",
	DetailCategory:                    "DetailCategory",
	AttachReceiptEllipsis:             "AttachReceiptEllipsis",
	NoDocumentsLinked:                 "NoDocumentsLinked",
	CategorizeSimilar:                 "CategorizeSimilar",
	PatternRegex:                      "PatternRegex",
	CreateRule:                        "CreateRule",
	TabDelimiter:                      "TabDelimiter",
	NewGroup:                          "NewGroup",
	GroupName:                         "GroupName",
	RenameGroup:                       "RenameGroup",
	DeleteGroup:                       "DeleteGroup",
	DeleteGroupText:                   "DeleteGroupText",
	MoveUp:                            "MoveUp",
	MoveDown:                          "MoveDown",
	MoveToGroup:                       "MoveToGroup",
	DefaultGroup:                      "DefaultGroup",
	FilterAccounts:                    "FilterAccounts",
	AccountsShown:                     "AccountsShown",
	LastFetched:                       "LastFetched",
	Collapse:                          "Collapse",
	Expand:                            "Expand",
	NewBookingsCount:                  "NewBookingsCount",
	NewBooking:                        "NewBooking",
	MarkAsRead:                        "MarkAsRead",
	DragToGroupHint:                   "DragToGroupHint",
	CollapseSidebar:                   "CollapseSidebar",
	ExpandSidebar:                     "ExpandSidebar",
	ChangeLanguage:                    "ChangeLanguage",
	LanguageBusy:                      "LanguageBusy",
	SwitchProfileMenu:                 "SwitchProfileMenu",
	ManageProfiles:                    "ManageProfiles",
	OpenProfile:                       "OpenProfile",
	BackToProfile:                     "BackToProfile",
	From:                              "From",
	OcrNotInstalled:                   "OcrNotInstalled",
	Upload:                            "Upload",
	UploadDocuments:                   "UploadDocuments",
	SearchDocuments:                   "SearchDocuments",
	TabAllDocuments:                   "TabAllDocuments",
	TabUnlinked:                       "TabUnlinked",
	TabLinked:                         "TabLinked",
	TabWithCount:                      "TabWithCount",
	NoDocuments:                       "NoDocuments",
	NoMatches:                         "NoMatches",
	UploadHint:                        "UploadHint",
	TryAnotherSearch:                  "TryAnotherSearch",
	DeleteDocumentTitle:               "DeleteDocumentTitle",
	DeleteDocumentText:                "DeleteDocumentText",
	NotLinked:                         "NotLinked",
	TextExtracted:                     "TextExtracted",
	NoText:                            "NoText",
	ReExtractText:                     "ReExtractText",
	LinkedTransaction:                 "LinkedTransaction",
	SuggestedMatches:                  "SuggestedMatches",
	FindTransactionToLink:             "FindTransactionToLink",
	ExtractedText:                     "ExtractedText",
	ExtractedTextConfidence:           "ExtractedTextConfidence",
	NoTextExtracted:                   "NoTextExtracted",
	MatchPercent:                      "MatchPercent",
	PreviewOf:                         "PreviewOf",
	Export:                            "Export",
	ExportPdf:                         "ExportPdf",
	ExportReport:                      "ExportReport",
	NothingToExport:                   "NothingToExport",
	BookingsCountTotal:                "BookingsCountTotal",
	TaxYear:                           "TaxYear",
	CustomRange:                       "CustomRange",
	PreviousYear:                      "PreviousYear",
	NextYear:                          "NextYear",
	TabTaxSummary:                     "TabTaxSummary",
	TabMonthlyBreakdown:               "TabMonthlyBreakdown",
	TabCategoryAnalysis:               "TabCategoryAnalysis",
	TabRecurring:                      "TabRecurring",
	NoTransactionsIn:                  "NoTransactionsIn",
	ReportsBuiltFrom:                  "ReportsBuiltFrom",
	TotalIncome:                       "TotalIncome",
	TotalExpenses:                     "TotalExpenses",
	NetIncome:                         "NetIncome",
	TaxRelevantAmount:                 "TaxRelevantAmount",
	TaxRelevantByCategory:             "TaxRelevantByCategory",
	NoTaxRelevantExpenses:             "NoTaxRelevantExpenses",
	StatTransactions:                  "StatTransactions",
	CategoriesUsed:                    "CategoriesUsed",
	AvgMonthlyExpenses:                "AvgMonthlyExpenses",
	HighestMonth:                      "HighestMonth",
	MonthlyIncomeExpenses:             "MonthlyIncomeExpenses",
	ColMonth:                          "ColMonth",
	ColIncome:                         "ColIncome",
	ColExpenses:                       "ColExpenses",
	ColNet:                            "ColNet",
	ReportTotal:                       "ReportTotal",
	SpendingByCategory:                "SpendingByCategory",
	TopVendors:                        "TopVendors",
	FromDate:                          "FromDate",
	ToDate:                            "ToDate",
	Apply:                             "Apply",
	NoRecurringFound:                  "NoRecurringFound",
	RecurringHint:                     "RecurringHint",
	RecurringPayments:                 "RecurringPayments",
	CostPerYear:                       "CostPerYear",
	PerMonth:                          "PerMonth",
	SubscriptionsTitle:                "SubscriptionsTitle",
	ColName:                           "ColName",
	ColEvery:                          "ColEvery",
	ColNext:                           "ColNext",
	ColPerYear:                        "ColPerYear",
	TimesSeen:                         "TimesSeen",
	Appearance:                        "Appearance",
	DarkTheme:                         "DarkTheme",
	MenuFile:                          "MenuFile",
	MenuView:                          "MenuView",
	MenuFind:                          "MenuFind",
	MenuWindow:                        "MenuWindow",
	MenuHelp:                          "MenuHelp",
	MenuSettings:                      "MenuSettings",
	MenuExit:                          "MenuExit",
	MenuEdit:                          "MenuEdit",
	MenuDetails:                       "MenuDetails",
	SyncNow:                           "SyncNow",
	ShowTransactions:                  "ShowTransactions",
	ShowAllFromPartner:                "ShowAllFromPartner",
	ShowInFinder:                      "ShowInFinder",
	ShowInExplorer:                    "ShowInExplorer",
	ShowInFolder:                      "ShowInFolder",
	MenuNewTransaction:                "MenuNewTransaction",
	MenuImportStatement:               "MenuImportStatement",
	MenuMinimize:                      "MenuMinimize",
	MenuZoom:                          "MenuZoom",
	MenuGitHub:                        "MenuGitHub",
	MenuReportProblem:                 "MenuReportProblem",
	AboutTitle:                        "AboutTitle",
	AboutVersion:                      "AboutVersion",
	AboutText:                         "AboutText",
	AboutTypeface:                     "AboutTypeface",
	AboutBankCodesSource:              "AboutBankCodesSource",
	LanguageKey:                       "Language",
	Categories:                        "Categories",
	AddCategory:                       "AddCategory",
	CategoryUsage:                     "CategoryUsage",
	ColourSwatch:                      "ColourSwatch",
	FixDuplicateColours:               "FixDuplicateColours",
	NoCategoriesYet:                   "NoCategoriesYet",
	AddDefaultCategories:              "AddDefaultCategories",
	CategoryIsTransfer:                "CategoryIsTransfer",
	CategoryIsTransferHint:            "CategoryIsTransferHint",
	CategoryTransferLabel:             "CategoryTransferLabel",
	CategorySplitMore:                 "CategorySplitMore",
	CategorySplitTransfers:            "CategorySplitTransfers",
	DefaultCategoryGroceries:          "DefaultCategoryGroceries",
	DefaultCategoryDrugstore:          "DefaultCategoryDrugstore",
	DefaultCategoryHousing:            "DefaultCategoryHousing",
	DefaultCategoryEnergy:             "DefaultCategoryEnergy",
	DefaultCategoryPhoneInternetMedia: "DefaultCategoryPhoneInternetMedia",
	DefaultCategoryInsurance:          "DefaultCategoryInsurance",
	DefaultCategoryLoans:              "DefaultCategoryLoans",
	DefaultCategorySavings:            "DefaultCategorySavings",
	DefaultCategoryTransport:          "DefaultCategoryTransport",
	DefaultCategoryHealth:             "DefaultCategoryHealth",
	DefaultCategoryClothing:           "DefaultCategoryClothing",
	DefaultCategoryOnlineShopping:     "DefaultCategoryOnlineShopping",
	DefaultCategoryLeisure:            "DefaultCategoryLeisure",
	DefaultCategoryRestaurants:        "DefaultCategoryRestaurants",
	DefaultCategoryTaxes:              "DefaultCategoryTaxes",
	DefaultCategoryCash:               "DefaultCategoryCash",
	DefaultCategoryCreditCard:         "DefaultCategoryCreditCard",
	DefaultCategoryOwnTransfers:       "DefaultCategoryOwnTransfers",
	DefaultCategorySalary:             "DefaultCategorySalary",
	DefaultCategoryChildBenefit:       "DefaultCategoryChildBenefit",
	DefaultCategoryInterestFees:       "DefaultCategoryInterestFees",
	DefaultCategoryFamily:             "DefaultCategoryFamily",
	DefaultCategoryChildrenSchool:     "DefaultCategoryChildrenSchool",
	DefaultCategoryOtherIncome:        "DefaultCategoryOtherIncome",
	BudgetAmount:                      "BudgetAmount",
	AutoCategorizationRules:           "AutoCategorizationRules",
	AddRule:                           "AddRule",
	RulesHint:                         "RulesHint",
	NoRulesYet:                        "NoRulesYet",
	Prio:                              "Prio",
	Manual:                            "Manual",
	RuleSearchIn:                      "RuleSearchIn",
	RuleFieldAny:                      "RuleFieldAny",
	RuleFieldPartner:                  "RuleFieldPartner",
	RuleFieldPurpose:                  "RuleFieldPurpose",
	RuleAppliesTo:                     "RuleAppliesTo",
	RuleDirectionAny:                  "RuleDirectionAny",
	RuleDirectionExpense:              "RuleDirectionExpense",
	RuleDirectionIncome:               "RuleDirectionIncome",
	RuleCategoryLabel:                 "RuleCategoryLabel",
	ImportRules:                       "ImportRules",
	ExportRules:                       "ExportRules",
	RuleImportTitle:                   "RuleImportTitle",
	RuleImportCategorize:              "RuleImportCategorize",
	RuleImportNewRules:                "RuleImportNewRules",
	RuleImportCategories:              "RuleImportCategories",
	RuleImportDuplicates:              "RuleImportDuplicates",
	RuleImportConflicts:               "RuleImportConflicts",
	RuleImportErrors:                  "RuleImportErrors",
	RuleImportConflictLine:            "RuleImportConflictLine",
	RuleImportErrorLine:               "RuleImportErrorLine",
	MessageRulesExported:              "MessageRulesExported",
	MessageRulesExportSkipped:         "MessageRulesExportSkipped",
	MessageRulesImported:              "MessageRulesImported",
	RuleFileErrorColumns:              "RuleFileErrorColumns",
	RuleFileErrorCategory:             "RuleFileErrorCategory",
	RuleFileErrorPattern:              "RuleFileErrorPattern",
	RuleFileErrorPatternLength:        "RuleFileErrorPatternLength",
	RuleFileErrorSearchIn:             "RuleFileErrorSearchIn",
	RuleFileErrorAppliesTo:            "RuleFileErrorAppliesTo",
	RuleFileErrorPriority:             "RuleFileErrorPriority",
	RuleFileErrorAutoApply:            "RuleFileErrorAutoApply",
	ErrorRuleFileHeader:               "ErrorRuleFileHeader",
	ErrorRuleFileTooLarge:             "ErrorRuleFileTooLarge",
	ErrorRuleFileEncoding:             "ErrorRuleFileEncoding",
	DataAndOcr:                        "DataAndOcr",
	Database:                          "Database",
	DocumentsFolder:                   "DocumentsFolder",
	TesseractAvailable:                "TesseractAvailable",
	TesseractMissing:                  "TesseractMissing",
	BackupAndRestore:                  "BackupAndRestore",
	BackupHint:                        "BackupHint",
	CreateBackup:                      "CreateBackup",
	RestoreFromBackup:                 "RestoreFromBackup",
	RestoreWaiting:                    "RestoreWaiting",
	CancelRestore:                     "CancelRestore",
	BackupRestoredAt:                  "BackupRestoredAt",
	AuditLogLatest:                    "AuditLogLatest",
	AuditLogShort:                     "AuditLogShort",
	AuditLogEmpty:                     "AuditLogEmpty",
	RestoreFileTitle:                  "RestoreFileTitle",
	RestoreCreated:                    "RestoreCreated",
	RestoreExplanation:                "RestoreExplanation",
	PrepareRestore:                    "PrepareRestore",
	DeleteCategoryTitle:               "DeleteCategoryTitle",
	DeleteCategoryText:                "DeleteCategoryText",
	DeleteCategoryUsage:               "DeleteCategoryUsage",
	DeleteRuleTitle:                   "DeleteRuleTitle",
	DeleteRuleText:                    "DeleteRuleText",
	EditCategory:                      "EditCategory",
	Name:                              "Name",
	MonthlyBudgetOptional:             "MonthlyBudgetOptional",
	TaxRelevantHint:                   "TaxRelevantHint",
	BudgetNotANumber:                  "BudgetNotANumber",
	EditRule:                          "EditRule",
	PatternExample:                    "PatternExample",
	Priority:                          "Priority",
	ApplyAutomatically:                "ApplyAutomatically",
	PriorityMustBeNumber:              "PriorityMustBeNumber",
	SaveBackup:                        "SaveBackup",
	RestoreBackup:                     "RestoreBackup",
	PinFor:                            "PinFor",
	PinExplanation:                    "PinExplanation",
	RememberPinForSyncs:               "RememberPinForSyncs",
	RememberPinExplanation:            "RememberPinExplanation",
	Pin:                               "Pin",
	ChooseTanProcedure:                "ChooseTanProcedure",
	ChooseTanMedium:                   "ChooseTanMedium",
	TanRequired:                       "TanRequired",
	TanDefaultMessage:                 "TanDefaultMessage",
	TanChallenge:                      "TanChallenge",
	TanFlickerUnsupported:             "TanFlickerUnsupported",
	ConfirmInBankingApp:               "ConfirmInBankingApp",
	IConfirmedIt:                      "IConfirmedIt",
	MessageFromBank:                   "MessageFromBank",
	NotEnoughData:                     "NotEnoughData",
	Max:                               "Max",
	MinAmount:                         "MinAmount",
	BalanceHistory:                    "BalanceHistory",
	AxisNotFromZero:                   "AxisNotFromZero",
	MessageAdded:                      "MessageAdded",
	MessageAccountAdded:               "MessageAccountAdded",
	MessageAccountDeleted:             "MessageAccountDeleted",
	MessageDeletedForgotten:           "MessageDeletedForgotten",
	MessageNoDeletedRemembered:        "MessageNoDeletedRemembered",
	MessageBankRemoved:                "MessageBankRemoved",
	MessageConnectionSaved:            "MessageConnectionSaved",
	SyncConnecting:                    "SyncConnecting",
	MessageTransactionAdded:           "MessageTransactionAdded",
	MessageRuleCreated:                "MessageRuleCreated",
	MessageDefaultCategoriesAdded:     "MessageDefaultCategoriesAdded",
	MessageDefaultCategoriesNone:      "MessageDefaultCategoriesNone",
	MessageColoursFixed:               "MessageColoursFixed",
	MessageColoursAlreadyDistinct:     "MessageColoursAlreadyDistinct",
	MessageAutoCategorized:            "MessageAutoCategorized",
	MessageExportedCount:              "MessageExportedCount",
	MessageBackupSaved:                "MessageBackupSaved",
	MessageRestorePrepared:            "MessageRestorePrepared",
	MessageRestoreCancelled:           "MessageRestoreCancelled",
	MessageProfileMoved:               "MessageProfileMoved",
	MessageSavedTo:                    "MessageSavedTo",
	PinNotSaved:                       "PinNotSaved",
	MessageUploaded:                   "MessageUploaded",
	UploadProgress:                    "UploadProgress",
	MessageDocumentLinked:             "MessageDocumentLinked",
	MessageDocumentDeleted:            "MessageDocumentDeleted",
	MessageNoTextExtracted:            "MessageNoTextExtracted",
	BankNameRequired:                  "BankNameRequired",
	BlzMustBe8Digits:                  "BlzMustBe8Digits",
	ErrorConnectionBusy:               "ErrorConnectionBusy",
	ErrorDeleteWhileBusy:              "ErrorDeleteWhileBusy",
	IbanRequired:                      "IbanRequired",
	IbanChecksumFailed:                "IbanChecksumFailed",
	AccountIbanExists:                 "AccountIbanExists",
	ErrorAmountNotZero:                "ErrorAmountNotZero",
	DuplicateTransactionTitle:         "DuplicateTransactionTitle",
	DuplicateTransactionText:          "DuplicateTransactionText",
	AddAnyway:                         "AddAnyway",
	ErrorExportFailed:                 "ErrorExportFailed",
	ErrorNameRequired:                 "ErrorNameRequired",
	ErrorColorFormat:                  "ErrorColorFormat",
	ErrorCategoryExists:               "ErrorCategoryExists",
	ErrorPatternRequired:              "ErrorPatternRequired",
	ErrorInvalidRegex:                 "ErrorInvalidRegex",
	ErrorPdfExportFailed:              "ErrorPdfExportFailed",
	ErrorCsvExportFailed:              "ErrorCsvExportFailed",
	ErrorSyncFromDateInvalid:          "ErrorSyncFromDateInvalid",
	ErrorSyncFromDateFuture:           "ErrorSyncFromDateFuture",
	ErrorAccountTypeInvalid:           "ErrorAccountTypeInvalid",
	ErrorBankConnectionNotFound:       "ErrorBankConnectionNotFound",
	ErrorAccountNotFound:              "ErrorAccountNotFound",
	ErrorSyncFailed:                   "ErrorSyncFailed",
	ErrorBankRejected:                 "ErrorBankRejected",
	ErrorBankRejectedStatements:       "ErrorBankRejectedStatements",
	StatementsNotSupported:            "StatementsNotSupported",
	ErrorNoFintsServer:                "ErrorNoFintsServer",
	ErrorBadServerUrl:                 "ErrorBadServerUrl",
	ErrorEnterUserId:                  "ErrorEnterUserId",
	ErrorConnectionNotFound:           "ErrorConnectionNotFound",
	ErrorCancelled:                    "ErrorCancelled",
	ErrorCancelledByUser:              "ErrorCancelledByUser",
	ErrorDialogFailed:                 "ErrorDialogFailed",
	HintNeedsAccount:                  "HintNeedsAccount",
	HintNeedsCategory:                 "HintNeedsCategory",
	HintNothingToExport:               "HintNothingToExport",
	ErrorBankUnreachable:              "ErrorBankUnreachable",
	ErrorServerHttpStatus:             "ErrorServerHttpStatus",
	ErrorServerNotFints:               "ErrorServerNotFints",
	ErrorBankRejectedPin:              "ErrorBankRejectedPin",
	ErrorPassportReset:                "ErrorPassportReset",
	ProtocolTraceTitle:                "ProtocolTraceTitle",
	ProtocolTraceHint:                 "ProtocolTraceHint",
	ProtocolTraceEnable:               "ProtocolTraceEnable",
	ProtocolTraceSave:                 "ProtocolTraceSave",
	ProtocolTraceAvailable:            "ProtocolTraceAvailable",
	ProtocolTraceNone:                 "ProtocolTraceNone",
	MessageProtocolTraceSaved:         "MessageProtocolTraceSaved",
	ErrorNoProtocolTrace:              "ErrorNoProtocolTrace",
	ProgressConnecting:                "ProgressConnecting",
	ProgressSavingAccount:             "ProgressSavingAccount",
	SyncSummary:                       "SyncSummary",
	SyncSummaryNew:                    "SyncSummaryNew",
	SyncSummaryTransactions:           "SyncSummaryTransactions",
	SyncSummaryCategorized:            "SyncSummaryCategorized",
	SyncSummaryDuplicates:             "SyncSummaryDuplicates",
	SyncSummaryHistoryFrom:            "SyncSummaryHistoryFrom",
	BalanceNotProvided:                "BalanceNotProvided",
	BalanceNotProvidedNoCode:          "BalanceNotProvidedNoCode",
	SyncGapNotFetched:                 "SyncGapNotFetched",
	SyncAccountWithoutIban:            "SyncAccountWithoutIban",
	SyncBalanceOtherCurrency:          "SyncBalanceOtherCurrency",
	SyncCurrencyCorrected:             "SyncCurrencyCorrected",
	PendingHeader:                     "PendingHeader",
	ImportSummary:                     "ImportSummary",
	ImportSummaryFiles:                "ImportSummaryFiles",
	ImportSummaryAuto:                 "ImportSummaryAuto",
	ImportSummaryKnown:                "ImportSummaryKnown",
	ImportSummaryProbable:             "ImportSummaryProbable",
	ImportSummaryWrongCurrency:        "ImportSummaryWrongCurrency",
	ImportSummaryBalance:              "ImportSummaryBalance",
	ImportSummaryDeleted:              "ImportSummaryDeleted",
	ErrorFileNotFound:                 "ErrorFileNotFound",
	ErrorFileTooLarge:                 "ErrorFileTooLarge",
	ErrorImportSeveralAccounts:        "ErrorImportSeveralAccounts",
	ErrorXlsxTooLarge:                 "ErrorXlsxTooLarge",
	ErrorXlsxUnreadable:               "ErrorXlsxUnreadable",
	ErrorFileDoesNotExist:             "ErrorFileDoesNotExist",
	ErrorUnsupportedFileType:          "ErrorUnsupportedFileType",
	ErrorUnreadableImage:              "ErrorUnreadableImage",
	ErrorDocumentTooLarge:             "ErrorDocumentTooLarge",
	ErrorImageTooLarge:                "ErrorImageTooLarge",
	ReportUnknownCategory:             "ReportUnknownCategory",
	ReportTitleTax:                    "ReportTitleTax",
	ReportTitleRange:                  "ReportTitleRange",
	PdfGenerated:                      "PdfGenerated",
	PdfSummary:                        "PdfSummary",
	PdfTotalIncome:                    "PdfTotalIncome",
	PdfTotalExpenses:                  "PdfTotalExpenses",
	PdfNetIncome:                      "PdfNetIncome",
	PdfTaxRelevantExpenses:            "PdfTaxRelevantExpenses",
	PdfTransactionCount:               "PdfTransactionCount",
	PdfAllAmountsIn:                   "PdfAllAmountsIn",
	PdfTaxByCategory:                  "PdfTaxByCategory",
	PdfNoTaxRelevant:                  "PdfNoTaxRelevant",
	PdfMonthlyBreakdown:               "PdfMonthlyBreakdown",
	PdfMonth:                          "PdfMonth",
	PdfIncomeExpensesNet:              "PdfIncomeExpensesNet",
	PdfExpensesByCategory:             "PdfExpensesByCategory",
	PdfTaxTransactions:                "PdfTaxTransactions",
	ColCurrency:                       "ColCurrency",
	ColAccount:                        "ColAccount",
	ChartOther:                        "ChartOther",
	ChartIncome:                       "ChartIncome",
	ChartExpenses:                     "ChartExpenses",
	ChartSoFar:                        "ChartSoFar",
	ChartPartialMonthNote:             "ChartPartialMonthNote",
	FlickerSpeed:                      "FlickerSpeed",
	FlickerSize:                       "FlickerSize",
	FlickerHint:                       "FlickerHint",
	TanLabel:                          "TanLabel",
	AppLogo:                           "AppLogo",
	EditCategoryNamed:                 "EditCategoryNamed",
	EditRuleNamed:                     "EditRuleNamed",
	OpenPreviewNamed:                  "OpenPreviewNamed",
	BankOfflineHint:                   "BankOfflineHint",
	IbanOrCardLast4:                   "IbanOrCardLast4",
	CardLast4Only:                     "CardLast4Only",
	ErrorBankOffline:                  "ErrorBankOffline",
	BankOnlineMode:                    "BankOnlineMode",
	BankOfflineMode:                   "BankOfflineMode",
	AccountsWithoutBank:               "AccountsWithoutBank",
	ReapplyRules:                      "ReapplyRules",
	ReapplyRulesTitle:                 "ReapplyRulesTitle",
	ReapplyRulesSummary:               "ReapplyRulesSummary",
	ReapplyRulesWarning:               "ReapplyRulesWarning",
	ReapplyRulesConfirm:               "ReapplyRulesConfirm",
	UndoReapplyRules:                  "UndoReapplyRules",
	MessageReapplyNothing:             "MessageReapplyNothing",
	MessageReapplied:                  "MessageReapplied",
	MessageReapplyUndone:              "MessageReapplyUndone",
	HintNothingShown:                  "HintNothingShown",
	SelectedCount:                     "SelectedCount",
	DeleteSelected:                    "DeleteSelected",
	ClearSelection:                    "ClearSelection",
	DeleteSelectedCount:               "DeleteSelectedCount",
	SelectAll:                         "SelectAll",
	DeleteTransactionsTitle:           "DeleteTransactionsTitle",
	DeleteSyncedHint:                  "DeleteSyncedHint",
	MessageTransactionsDeleted:        "MessageTransactionsDeleted",
	TaxAutomatic:                      "TaxAutomatic",
	TaxRelevantChoice:                 "TaxRelevantChoice",
	TaxNotRelevantChoice:              "TaxNotRelevantChoice",
	TaxSetByYou:                       "TaxSetByYou",
	TaxFromRule:                       "TaxFromRule",
	TaxFromCategory:                   "TaxFromCategory",
	CategoryMenu:                      "CategoryMenu",
	TaxMenu:                           "TaxMenu",
	MarkTaxRelevant:                   "MarkTaxRelevant",
	MarkNotTaxRelevant:                "MarkNotTaxRelevant",
	ResetTaxAutomatic:                 "ResetTaxAutomatic",
	RuleTaxAsCategory:                 "RuleTaxAsCategory",
	RuleTaxMarkerYes:                  "RuleTaxMarkerYes",
	RuleTaxMarkerNo:                   "RuleTaxMarkerNo",
	MessageTaxOverrideSet:             "MessageTaxOverrideSet",
	MessageCategorySet:                "MessageCategorySet",
	TabTaxBookings:                    "TabTaxBookings",
	TaxIncomeTotal:                    "TaxIncomeTotal",
	TaxDeductionTotal:                 "TaxDeductionTotal",
	TaxReceipts:                       "TaxReceipts",
	TaxReceiptsOf:                     "TaxReceiptsOf",
	ReceiptsMissing:                   "ReceiptsMissing",
	ReceiptsComplete:                  "ReceiptsComplete",
	TaxSuggestionsTitle:               "TaxSuggestionsTitle",
	TaxSuggestionsHint:                "TaxSuggestionsHint",
	MatchesKeyword:                    "MatchesKeyword",
	TaxSectionIncome:                  "TaxSectionIncome",
	TaxSectionDeductions:              "TaxSectionDeductions",
	NoReceipt:                         "NoReceipt",
	MissingReceiptsOnly:               "MissingReceiptsOnly",
	ShowAllCount:                      "ShowAllCount",
	NoTaxBookingsIn:                   "NoTaxBookingsIn",
	TaxBookingsHowTo:                  "TaxBookingsHowTo",
	TaxSuggestionKeywords:             "TaxSuggestionKeywords",
	TaxKeywordsCount:                  "TaxKeywordsCount",
	ResetToDefaults:                   "ResetToDefaults",
	Accept:                            "Accept",
	DismissSuggestion:                 "DismissSuggestion",
	ExportExcel:                       "ExportExcel",
	Print:                             "Print",
	OpenTaxBookings:                   "OpenTaxBookings",
	XlsxSheetOverview:                 "XlsxSheetOverview",
	XlsxSheetBookings:                 "XlsxSheetBookings",
	TaxExportTitle:                    "TaxExportTitle",
	TaxColSection:                     "TaxColSection",
	TaxColBookings:                    "TaxColBookings",
	TaxColWithReceipt:                 "TaxColWithReceipt",
	TaxColReceipt:                     "TaxColReceipt",
	TaxColSource:                      "TaxColSource",
	Yes:                               "Yes",
	No:                                "No",
	PdfPageOf:                         "PdfPageOf",
	MessagePrintSent:                  "MessagePrintSent",
	ErrorTaxExportFailed:              "ErrorTaxExportFailed",
	ErrorPrintFailed:                  "ErrorPrintFailed",
	BackTo:                            "BackTo",
	ComingSoon:                        "ComingSoon",
	Security:                          "Security",
	ThemeSystem:                       "ThemeSystem",
	ThemeLight:                        "ThemeLight",
	ThemeDark:                         "ThemeDark",
	Theme:                             "Theme",
	ErrorFinDeskNotFound:              "ErrorFinDeskNotFound",
	ErrorFinDeskWrongPassphrase:       "ErrorFinDeskWrongPassphrase",
	ErrorFinDeskNotFinDesk:            "ErrorFinDeskNotFinDesk",
	ErrorFinDeskTargetNotEmpty:        "ErrorFinDeskTargetNotEmpty",
	FinDeskImportSummary:              "FinDeskImportSummary",
	FinDeskImportTitle:                "FinDeskImportTitle",
	FinDeskImportHint:                 "FinDeskImportHint",
	FinDeskImportChooseFolder:         "FinDeskImportChooseFolder",
	FinDeskPassphraseTitle:            "FinDeskPassphraseTitle",
	FinDeskPassphraseText:             "FinDeskPassphraseText",
	FinDeskImportReportTitle:          "FinDeskImportReportTitle",
	FinDeskImportFailedTitle:          "FinDeskImportFailedTitle",
	FinDeskImportSkipped:              "FinDeskImportSkipped",
	ErrorFinDeskNotAFolder:            "ErrorFinDeskNotAFolder",
	ErrorNotEncrypted:                 "ErrorNotEncrypted",
	ErrorDatabaseTooNew:               "ErrorDatabaseTooNew",
	ErrorAlreadyRunning:               "ErrorAlreadyRunning",
	ErrorStartupFailed:                "ErrorStartupFailed",
	ErrorOnlineSyncUnavailable:        "ErrorOnlineSyncUnavailable",
	ErrorServerUrlRequired:            "ErrorServerUrlRequired",
	ErrorServerUrlMustBeHttps:         "ErrorServerUrlMustBeHttps",
	ErrorUnexpected:                   "ErrorUnexpected",
	MigrationWelcomeTitle:             "MigrationWelcomeTitle",
	MigrationWelcomeText:              "MigrationWelcomeText",
	MigrationProfilesLabel:            "MigrationProfilesLabel",
	MigrationCopiedText:               "MigrationCopiedText",
	MigrationQuitHint:                 "MigrationQuitHint",
	MigrationStart:                    "MigrationStart",
	MigrationStartEmpty:               "MigrationStartEmpty",
	MigrationProfileHeading:           "MigrationProfileHeading",
	MigrationFindeskPassphrase:        "MigrationFindeskPassphrase",
	MigrationSamePassphrase:           "MigrationSamePassphrase",
	MigrationNewPassphraseNeeded:      "MigrationNewPassphraseNeeded",
	MigrationMigrateProfile:           "MigrationMigrateProfile",
	MigrationSkip:                     "MigrationSkip",
	MigrationSummaryTitle:             "MigrationSummaryTitle",
	MigrationSummaryText:              "MigrationSummaryText",
	MigrationContinue:                 "MigrationContinue",
	MigrationProfileMigrated:          "MigrationProfileMigrated",
	MigrationProfileSkipped:           "MigrationProfileSkipped",
	MigrationProfileFailed:            "MigrationProfileFailed",
	MigrationBusy:                     "MigrationBusy",
	ErrorFinDeskSourceChanged:         "ErrorFinDeskSourceChanged",
	ErrorMigrationTargetNotEmpty:      "ErrorMigrationTargetNotEmpty",
}

// keysByName is built from keyNames, so the two cannot drift apart.
var keysByName = func() map[string]Key {
	m := make(map[string]Key, NumKeys)
	for i, name := range keyNames {
		m[name] = Key(i)
	}
	return m
}()

// All returns every key in declaration order.
func All() []Key {
	all := make([]Key, NumKeys)
	for i := range all {
		all[i] = Key(i)
	}
	return all
}

// String returns the key's variant name.
func (k Key) String() string {
	if k < 0 || int(k) >= NumKeys {
		return ""
	}
	return keyNames[k]
}

// KeyFromName looks a key up by its variant name.
func KeyFromName(name string) (Key, bool) {
	key, ok := keysByName[name]
	return key, ok
}
