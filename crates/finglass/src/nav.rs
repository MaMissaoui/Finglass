//! The back trail, ported from FinDesk's `NavigationState`.
//!
//! A link inside a screen pushes where the user leaves from onto a stack of at most [`MAX_BACK`]
//! entries; choosing a page directly (sidebar, shortcuts, menus) clears it. Each entry keeps the
//! screen and the arguments to reopen it with, so Back restores the Dashboard's selected month and
//! the filters Transactions or Reports were opened with.

use finglass_core::app::transactions::StartArgs;
use finglass_core::dashboard::YearMonth;
use finglass_core::i18n::{self, Key, Language};

/// Depth of the back stack; when full, the oldest entry is dropped.
pub(crate) const MAX_BACK: usize = 10;

// The Settings sections, in the order `settings_page.slint` lists them. Links into Settings pass
// the section they are about; plain navigation opens Appearance, as FinDesk does.
pub(crate) const SETTINGS_APPEARANCE: i32 = 0;
pub(crate) const SETTINGS_PROFILES: i32 = 1;
pub(crate) const SETTINGS_CATEGORIES: i32 = 2;

/// One screen's arguments, mirroring the fields FinDesk's `NavigationArgs` carries for the pages
/// Finglass has.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub(crate) struct PageArgs {
    /// The Dashboard's selected month when it is not the current one.
    pub(crate) dashboard_month: Option<YearMonth>,
    /// The filters Transactions was opened with.
    pub(crate) transactions: StartArgs,
    /// The Settings section a link opens; `None` opens Appearance.
    pub(crate) settings_section: Option<i32>,
    /// The tax year a link into Reports opens on its Tax bookings tab; `None` opens the summary.
    pub(crate) reports_year: Option<i32>,
}

impl PageArgs {
    /// The arguments to reopen the Dashboard with: its selected month unless that is the current one.
    #[must_use]
    pub(crate) fn dashboard(selected: YearMonth, current: YearMonth) -> Self {
        Self {
            dashboard_month: (selected != current).then_some(selected),
            ..Self::default()
        }
    }
}

/// One step on the back stack: the screen a link left and the arguments to reopen it with.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct BackEntry {
    pub(crate) screen: &'static str,
    pub(crate) args: PageArgs,
}

/// The navigation model: the back stack and the arguments of the page shown now.
#[derive(Debug, Default)]
pub(crate) struct NavState {
    back: Vec<BackEntry>,
    current: PageArgs,
}

impl NavState {
    /// The arguments of the page shown now; a link from it leaves these behind.
    #[must_use]
    pub(crate) fn current(&self) -> &PageArgs {
        &self.current
    }

    /// Choosing a page directly (sidebar, shortcut, menu) starts a new trail.
    pub(crate) fn enter(&mut self, args: PageArgs) {
        self.current = args;
        self.back.clear();
    }

    /// A link inside a screen: push where the user leaves from, then open the target with `to_args`.
    ///
    /// A link to the screen already shown only changes its arguments, so going back would reopen
    /// the same screen and nothing is pushed.
    pub(crate) fn follow(&mut self, from: &'static str, to: &'static str, leave_args: PageArgs, to_args: PageArgs) {
        if from != to {
            if self.back.len() == MAX_BACK {
                self.back.remove(0);
            }
            self.back.push(BackEntry {
                screen: from,
                args: leave_args,
            });
        }
        self.current = to_args;
    }

    /// Pops the last entry; `None` leaves everything unchanged when the stack is empty.
    pub(crate) fn back(&mut self) -> Option<BackEntry> {
        let entry = self.back.pop()?;
        self.current = entry.args.clone();
        Some(entry)
    }

    /// The screen Back would reopen, or `None` when the stack is empty.
    #[must_use]
    pub(crate) fn target(&self) -> Option<&BackEntry> {
        self.back.last()
    }

    /// The number of steps Back can take; exposed for the tests.
    #[cfg(test)]
    pub(crate) fn len(&self) -> usize {
        self.back.len()
    }

    /// Forgets the trail, e.g. when the profile changes.
    pub(crate) fn reset(&mut self) {
        self.back.clear();
        self.current = PageArgs::default();
    }
}

/// The known screen name from Slint, or `None` for a value no page owns.
#[must_use]
pub(crate) fn screen_name(screen: &str) -> Option<&'static str> {
    match screen {
        "dashboard" => Some("dashboard"),
        "accounts" => Some("accounts"),
        "transactions" => Some("transactions"),
        "documents" => Some("documents"),
        "reports" => Some("reports"),
        "settings" => Some("settings"),
        _ => None,
    }
}

/// The navigation title key for `screen`, as the top bar shows it.
#[must_use]
pub(crate) fn title_key(screen: &str) -> Key {
    match screen {
        "accounts" => Key::NavAccounts,
        "transactions" => Key::NavTransactions,
        "documents" => Key::NavDocuments,
        "reports" => Key::NavReports,
        "settings" => Key::NavSettings,
        _ => Key::NavDashboard,
    }
}

/// The screen's translated title.
#[must_use]
pub(crate) fn title(screen: &str, language: Language) -> &'static str {
    i18n::text(language, title_key(screen))
}

#[cfg(test)]
mod tests {
    use finglass_core::app::dashboard::NavigationArgs;
    use finglass_core::model::CategoryId;

    use super::*;

    fn past() -> YearMonth {
        YearMonth::new(2026, 9)
    }

    fn current() -> YearMonth {
        YearMonth::new(2026, 11)
    }

    #[test]
    fn a_dashboard_link_remembers_a_past_month_only() {
        let past_args = PageArgs::dashboard(past(), current());
        assert_eq!(past_args.dashboard_month, Some(past()));
        assert_eq!(PageArgs::dashboard(current(), current()).dashboard_month, None);
    }

    #[test]
    fn a_link_pushes_where_the_user_leaves_from() {
        let mut nav = NavState::default();
        nav.enter(PageArgs::dashboard(current(), current()));
        let leave = PageArgs::dashboard(past(), current());

        nav.follow(
            "dashboard",
            "transactions",
            leave.clone(),
            PageArgs {
                transactions: NavigationArgs::category(past(), current(), CategoryId(7)).start_args(),
                ..PageArgs::default()
            },
        );

        let target = nav.target().expect("a link leaves a back entry");
        assert_eq!(target.screen, "dashboard");
        assert_eq!(target.args, leave, "Back reopens the Dashboard on September");
        assert_eq!(
            nav.current().transactions,
            NavigationArgs::category(past(), current(), CategoryId(7)).start_args()
        );
    }

    #[test]
    fn back_restores_the_screen_and_its_arguments() {
        let mut nav = NavState::default();
        nav.enter(PageArgs::default());
        let reports_args = PageArgs {
            reports_year: Some(2026),
            ..PageArgs::default()
        };
        nav.follow(
            "dashboard",
            "reports",
            PageArgs::dashboard(past(), current()),
            reports_args.clone(),
        );

        let entry = nav.back().expect("the entry pops");
        assert_eq!(entry.screen, "dashboard");
        assert_eq!(entry.args.dashboard_month, Some(past()));
        assert!(nav.target().is_none(), "the stack is empty again");
        assert_eq!(
            nav.current().dashboard_month,
            Some(past()),
            "the restored page is now current"
        );
        assert!(nav.back().is_none(), "Back on an empty stack is a no-op");
    }

    #[test]
    fn choosing_a_page_directly_clears_the_stack() {
        let mut nav = NavState::default();
        nav.enter(PageArgs::default());
        nav.follow("dashboard", "transactions", PageArgs::default(), PageArgs::default());
        assert_eq!(nav.len(), 1);

        nav.enter(PageArgs::default());

        assert_eq!(nav.len(), 0);
        assert!(nav.target().is_none());
    }

    #[test]
    fn a_link_to_the_same_page_pushes_nothing() {
        let mut nav = NavState::default();
        nav.enter(PageArgs::default());

        nav.follow(
            "transactions",
            "transactions",
            PageArgs::default(),
            PageArgs {
                transactions: StartArgs {
                    partner: Some("Bakery".to_owned()),
                    ..StartArgs::default()
                },
                ..PageArgs::default()
            },
        );

        assert_eq!(nav.len(), 0);
        assert_eq!(
            nav.current().transactions.partner.as_deref(),
            Some("Bakery"),
            "the same page still gets its new arguments"
        );
    }

    #[test]
    fn the_stack_keeps_at_most_ten_entries_and_drops_the_oldest() {
        let mut nav = NavState::default();
        nav.enter(PageArgs::default());
        for _ in 0..MAX_BACK + 3 {
            nav.follow("dashboard", "transactions", PageArgs::default(), PageArgs::default());
            nav.follow("transactions", "dashboard", PageArgs::default(), PageArgs::default());
        }

        assert_eq!(nav.len(), MAX_BACK);
    }

    #[test]
    fn every_screen_has_a_navigation_title() {
        for screen in [
            "dashboard",
            "accounts",
            "transactions",
            "documents",
            "reports",
            "settings",
        ] {
            assert_eq!(screen_name(screen), Some(screen));
            assert!(!title(screen, Language::En).is_empty(), "{screen}");
            assert!(!title(screen, Language::De).is_empty(), "{screen}");
            assert!(!title(screen, Language::Fr).is_empty(), "{screen}");
        }
        assert_eq!(screen_name("payments"), None);
    }
}
