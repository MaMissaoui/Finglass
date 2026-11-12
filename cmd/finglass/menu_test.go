package main

import (
	"reflect"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func menuSection(t *testing.T, sections []menuSectionModel, title string) menuSectionModel {
	t.Helper()
	for _, section := range sections {
		if section.Title == title {
			return section
		}
	}
	t.Fatalf("no menu section %q in %+v", title, sections)
	return menuSectionModel{}
}

func menuItem(t *testing.T, section menuSectionModel, title string) menuItemModel {
	t.Helper()
	for _, item := range section.Items {
		if item.Title == title {
			return item
		}
	}
	t.Fatalf("no item %q in %q", title, section.Title)
	return menuItemModel{}
}

func TestMenuTitlesComeFromI18nInEveryLanguage(t *testing.T) {
	for _, language := range i18n.Languages {
		sections := buildMenu(language, menuState{shellOpen: true}, menuActions{})

		file := menuSection(t, sections, i18n.Text(language, i18n.MenuFile))
		menuItem(t, file, i18n.Text(language, i18n.MenuNewTransaction))
		menuItem(t, file, i18n.Text(language, i18n.MenuImportStatement))
		menuItem(t, file, i18n.Text(language, i18n.MenuSettings))

		view := menuSection(t, sections, i18n.Text(language, i18n.MenuView))
		menuItem(t, view, i18n.Text(language, i18n.MenuFind))
		menuItem(t, view, i18n.Text(language, i18n.Back))
		menuItem(t, view, i18n.Text(language, i18n.DarkTheme))

		window := menuSection(t, sections, i18n.Text(language, i18n.MenuWindow))
		menuItem(t, window, i18n.Text(language, i18n.MenuMinimize))
		menuItem(t, window, i18n.Text(language, i18n.MenuZoom))

		help := menuSection(t, sections, i18n.Text(language, i18n.MenuHelp))
		menuItem(t, help, i18n.Text(language, i18n.MenuGitHub))
		menuItem(t, help, i18n.Text(language, i18n.MenuReportProblem))
	}
}

func TestViewPagesFollowPageTarget(t *testing.T) {
	var visited []int
	sections := buildMenu(i18n.En, menuState{shellOpen: true}, menuActions{
		NavigatePage: func(index int) {
			visited = append(visited, index)
		},
	})
	view := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuView))

	for index := 0; index < 6; index++ {
		screen, ok := shell.PageTarget(index)
		if !ok {
			t.Fatalf("PageTarget(%d) has no target", index)
		}
		item := menuItem(t, view, shell.Title(screen, i18n.En))
		if item.Action == nil {
			t.Fatalf("the %s item has no action", screen)
		}
		item.Action()
	}

	want := []int{0, 1, 2, 3, 4, 5}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("page actions = %v, want %v", visited, want)
	}
}

func TestHelpItemsCallTheOpenerWithTheTwoURLs(t *testing.T) {
	var opened []string
	sections := buildMenu(i18n.En, menuState{shellOpen: true}, menuActions{
		OpenURL: func(url string) {
			opened = append(opened, url)
		},
	})
	help := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuHelp))

	github := menuItem(t, help, i18n.Text(i18n.En, i18n.MenuGitHub))
	report := menuItem(t, help, i18n.Text(i18n.En, i18n.MenuReportProblem))
	github.Action()
	report.Action()

	want := []string{shell.GithubURL, shell.ReportProblemURL}
	if !reflect.DeepEqual(opened, want) {
		t.Fatalf("opened URLs = %v, want %v", opened, want)
	}
}

func TestProfileOnlyItemsAreDisabledBeforeUnlock(t *testing.T) {
	sections := buildMenu(i18n.En, menuState{shellOpen: false, findEnabled: true, backEnabled: true}, menuActions{})

	file := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuFile))
	view := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuView))
	for _, section := range []menuSectionModel{file, view} {
		for _, item := range section.Items {
			if item.Separator {
				continue
			}
			if item.Enabled {
				t.Fatalf("%q is enabled before unlock", item.Title)
			}
		}
	}

	window := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuWindow))
	help := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuHelp))
	for _, section := range []menuSectionModel{window, help} {
		for _, item := range section.Items {
			if !item.Separator && !item.Enabled {
				t.Fatalf("%q is disabled with no profile open", item.Title)
			}
		}
	}
}

func TestFindBackAndThemeFollowTheShellState(t *testing.T) {
	off := buildMenu(i18n.En, menuState{shellOpen: true, findEnabled: false, backEnabled: false, dark: false}, menuActions{})
	view := menuSection(t, off, i18n.Text(i18n.En, i18n.MenuView))
	if menuItem(t, view, i18n.Text(i18n.En, i18n.MenuFind)).Enabled {
		t.Fatal("Find is enabled on a page without a search field")
	}
	if menuItem(t, view, i18n.Text(i18n.En, i18n.Back)).Enabled {
		t.Fatal("Back is enabled with an empty back stack")
	}
	dark := menuItem(t, view, i18n.Text(i18n.En, i18n.DarkTheme))
	if !dark.Checkable || dark.Checked {
		t.Fatalf("dark theme item = %+v, want checkable and unchecked", dark)
	}

	on := buildMenu(i18n.En, menuState{shellOpen: true, findEnabled: true, backEnabled: true, dark: true}, menuActions{})
	view = menuSection(t, on, i18n.Text(i18n.En, i18n.MenuView))
	if !menuItem(t, view, i18n.Text(i18n.En, i18n.MenuFind)).Enabled {
		t.Fatal("Find is disabled on a page with a search field")
	}
	if !menuItem(t, view, i18n.Text(i18n.En, i18n.Back)).Enabled {
		t.Fatal("Back is disabled with a back target")
	}
	if !menuItem(t, view, i18n.Text(i18n.En, i18n.DarkTheme)).Checked {
		t.Fatal("the dark theme item is not checked")
	}
}

func TestMenuActionsAreWired(t *testing.T) {
	var called []string
	actions := menuActions{
		NewTransaction:  func() { called = append(called, "newTransaction") },
		ImportStatement: func() { called = append(called, "importStatement") },
		Find:            func() { called = append(called, "find") },
		Navigate:        func(screen string) { called = append(called, "navigate:"+screen) },
		Back:            func() { called = append(called, "back") },
		ToggleTheme:     func() { called = append(called, "theme") },
		Minimize:        func() { called = append(called, "minimize") },
		Zoom:            func() { called = append(called, "zoom") },
	}
	sections := buildMenu(i18n.En, menuState{shellOpen: true, findEnabled: true, backEnabled: true}, actions)

	file := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuFile))
	menuItem(t, file, i18n.Text(i18n.En, i18n.MenuNewTransaction)).Action()
	menuItem(t, file, i18n.Text(i18n.En, i18n.MenuImportStatement)).Action()
	menuItem(t, file, i18n.Text(i18n.En, i18n.MenuSettings)).Action()

	view := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuView))
	menuItem(t, view, i18n.Text(i18n.En, i18n.MenuFind)).Action()
	menuItem(t, view, i18n.Text(i18n.En, i18n.Back)).Action()
	menuItem(t, view, i18n.Text(i18n.En, i18n.DarkTheme)).Action()

	window := menuSection(t, sections, i18n.Text(i18n.En, i18n.MenuWindow))
	menuItem(t, window, i18n.Text(i18n.En, i18n.MenuMinimize)).Action()
	menuItem(t, window, i18n.Text(i18n.En, i18n.MenuZoom)).Action()

	want := []string{
		"newTransaction", "importStatement", "navigate:settings",
		"find", "back", "theme", "minimize", "zoom",
	}
	if !reflect.DeepEqual(called, want) {
		t.Fatalf("menu actions = %v, want %v", called, want)
	}
}
