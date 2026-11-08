//! The native menu bar's shared actions.
//!
//! Slint turns the `MenuBar` in `main_window.slint` into the macOS screen menu bar. The window's
//! callbacks only forward here, so the pages and the menu share the same navigation targets and
//! help links.

/// The project's repository, opened by Help > Finglass on GitHub.
pub(crate) const GITHUB_URL: &str = "https://github.com/MaMissaoui/Finglass";

/// The repository's issue form, opened by Help > Report a problem.
pub(crate) const REPORT_PROBLEM_URL: &str = "https://github.com/MaMissaoui/Finglass/issues/new";

/// The View menu's pages, in sidebar order.
const PAGE_TARGETS: [&str; 6] = [
    "dashboard",
    "accounts",
    "transactions",
    "documents",
    "reports",
    "settings",
];

/// The screen a View menu item opens, by its position in the menu.
#[must_use]
pub(crate) fn page_target(index: i32) -> Option<&'static str> {
    usize::try_from(index)
        .ok()
        .and_then(|index| PAGE_TARGETS.get(index))
        .copied()
}

/// Opens `url` with the system's default browser.
pub(crate) fn open_help(url: &str) {
    let _ = open::that_detached(url);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn page_items_follow_the_sidebar_order() {
        assert_eq!(page_target(0), Some("dashboard"));
        assert_eq!(page_target(1), Some("accounts"));
        assert_eq!(page_target(2), Some("transactions"));
        assert_eq!(page_target(3), Some("documents"));
        assert_eq!(page_target(4), Some("reports"));
        assert_eq!(page_target(5), Some("settings"));
    }

    #[test]
    fn page_items_outside_the_menu_have_no_target() {
        assert_eq!(page_target(-1), None);
        assert_eq!(page_target(6), None);
        assert_eq!(page_target(i32::MAX), None);
    }

    #[test]
    fn help_links_point_at_the_project_repository() {
        assert_eq!(GITHUB_URL, "https://github.com/MaMissaoui/Finglass");
        assert_eq!(REPORT_PROBLEM_URL, "https://github.com/MaMissaoui/Finglass/issues/new");
    }
}
