// Package shell ports the non-UI part of crates/finglass/src: the session state
// behind the windows, its navigation model, the shell-wide job counter, the
// menu helpers and the first-run migration wizard.
//
// Everything that reaches outside the process is injected: the data paths, the
// credential store, the FinDesk root, the clock and today's date, and the
// environment lookup used for the locale. The package never reads the
// home folder, never touches the Keychain directly, never contacts a bank and
// never runs the app.
package shell

import (
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// MaxBack is the depth of the back stack; when full, the oldest entry is
// dropped. It is Rust's nav::MAX_BACK.
const MaxBack = 10

// The Settings sections, in the order settings_page.slint lists them. Links
// into Settings pass the section they are about; plain navigation opens
// Appearance, as FinDesk does.
const (
	// SettingsAppearance is the first Settings section.
	SettingsAppearance = 0
	// SettingsProfiles is the profiles section.
	SettingsProfiles = 1
	// SettingsCategories is the categories section.
	SettingsCategories = 2
)

// PageArgs is one screen's arguments, mirroring the fields FinDesk's
// NavigationArgs carries for the pages Finglass has.
type PageArgs struct {
	// DashboardMonth is the Dashboard's selected month when it is not the
	// current one.
	DashboardMonth *dashboard.YearMonth
	// Transactions is the filter Transactions was opened with.
	Transactions transactionsview.StartArgs
	// SettingsSection is the Settings section a link opens; nil opens
	// Appearance.
	SettingsSection *int
	// ReportsYear is the tax year a link into Reports opens on its Tax
	// bookings tab; nil opens the summary.
	ReportsYear *int
}

// DashboardPageArgs returns the arguments to reopen the Dashboard with: its
// selected month unless that is the current one. It is Rust's
// PageArgs::dashboard.
func DashboardPageArgs(selected, current dashboard.YearMonth) PageArgs {
	args := PageArgs{}
	if selected != current {
		month := selected
		args.DashboardMonth = &month
	}
	return args
}

// BackEntry is one step on the back stack: the screen a link left and the
// arguments to reopen it with.
type BackEntry struct {
	// Screen is the screen the link left.
	Screen string
	// Args are the arguments to reopen it with.
	Args PageArgs
}

// NavState is the navigation model: the back stack and the arguments of the
// page shown now.
type NavState struct {
	back    []BackEntry
	current PageArgs
}

// Current returns the arguments of the page shown now; a link from it leaves
// these behind. The result is a copy.
func (n *NavState) Current() PageArgs {
	return n.current
}

// Enter chooses a page directly (sidebar, shortcut, menu): it starts a new
// trail.
func (n *NavState) Enter(args PageArgs) {
	n.current = args
	n.back = n.back[:0]
}

// Follow records a link inside a screen: it pushes where the user leaves from,
// then opens the target with toArgs.
//
// A link to the screen already shown only changes its arguments, so going back
// would reopen the same screen and nothing is pushed.
func (n *NavState) Follow(from, to string, leaveArgs, toArgs PageArgs) {
	if from != to {
		if len(n.back) == MaxBack {
			n.back = n.back[1:]
		}
		n.back = append(n.back, BackEntry{Screen: from, Args: leaveArgs})
	}
	n.current = toArgs
}

// Back pops the last entry; false leaves everything unchanged when the stack
// is empty.
func (n *NavState) Back() (BackEntry, bool) {
	if len(n.back) == 0 {
		return BackEntry{}, false
	}
	last := len(n.back) - 1
	entry := n.back[last]
	n.back = n.back[:last]
	n.current = entry.Args
	return entry, true
}

// Target returns the screen Back would reopen, and false when the stack is
// empty.
func (n *NavState) Target() (BackEntry, bool) {
	if len(n.back) == 0 {
		return BackEntry{}, false
	}
	return n.back[len(n.back)-1], true
}

// Len returns the number of steps Back can take.
func (n *NavState) Len() int {
	return len(n.back)
}

// Reset forgets the trail, e.g. when the profile changes.
func (n *NavState) Reset() {
	n.back = n.back[:0]
	n.current = PageArgs{}
}

// ScreenName returns the known screen name, or false for a value no page owns.
func ScreenName(screen string) (string, bool) {
	switch screen {
	case "dashboard", "accounts", "transactions", "documents", "reports", "settings":
		return screen, true
	default:
		return "", false
	}
}

// TitleKey returns the navigation title key for screen, as the top bar shows
// it.
func TitleKey(screen string) i18n.Key {
	switch screen {
	case "accounts":
		return i18n.NavAccounts
	case "transactions":
		return i18n.NavTransactions
	case "documents":
		return i18n.NavDocuments
	case "reports":
		return i18n.NavReports
	case "settings":
		return i18n.NavSettings
	default:
		return i18n.NavDashboard
	}
}

// Title returns the screen's translated title.
func Title(screen string, language i18n.Language) string {
	return i18n.Text(language, TitleKey(screen))
}

// SettingsSectionFor returns the Settings section a navigation opens, or nil
// when it opens another screen.
func SettingsSectionFor(screen string, requested *int) *int {
	if screen != "settings" {
		return nil
	}
	if requested != nil {
		section := *requested
		return &section
	}
	section := SettingsAppearance
	return &section
}

// FindEnabled reports whether View > Find applies on screen: only the pages
// with a page-level search field, and never while a dialog is open.
func FindEnabled(screen string, dialogOpen bool) bool {
	if dialogOpen {
		return false
	}
	return screen == "transactions" || screen == "documents"
}
