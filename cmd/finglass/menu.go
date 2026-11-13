// Command finglass is the Finglass desktop application.
package main

import (
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/prefs"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/bind"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// MenuActionEvent is the event the pages handle for menu-only actions.
const MenuActionEvent = "menu:action"

func init() {
	application.RegisterEvent[string](MenuActionEvent)
}

// menuState is the shell state the menu bar reflects.
type menuState struct {
	shellOpen   bool
	findEnabled bool
	backEnabled bool
	dark        bool
}

// menuActions are the effects a menu item has. Tests replace them with
// recorders.
type menuActions struct {
	NewTransaction  func()
	ImportStatement func()
	Navigate        func(screen string)
	NavigatePage    func(index int)
	Find            func()
	Back            func()
	ToggleTheme     func()
	Minimize        func()
	Zoom            func()
	OpenURL         func(url string)
}

// menuItemModel is one menu item, independent of Wails.
type menuItemModel struct {
	Title     string
	Shortcut  string
	Enabled   bool
	Checkable bool
	Checked   bool
	Separator bool
	Action    func()
}

// menuSectionModel is one top-level menu.
type menuSectionModel struct {
	Title string
	Items []menuItemModel
}

// buildMenu builds File, View, Window and Help in language. Every item that
// needs an open profile is disabled while the unlock view shows.
func buildMenu(language i18n.Language, state menuState, actions menuActions) []menuSectionModel {
	text := func(key i18n.Key) string { return i18n.Text(language, key) }
	action := func(fn func()) func() {
		if fn == nil {
			return nil
		}
		return fn
	}

	file := menuSectionModel{
		Title: text(i18n.MenuFile),
		Items: []menuItemModel{
			{
				Title:    text(i18n.MenuNewTransaction),
				Shortcut: "CmdOrCtrl+N",
				Enabled:  state.shellOpen,
				Action:   action(actions.NewTransaction),
			},
			{
				Title:    text(i18n.MenuImportStatement),
				Shortcut: "CmdOrCtrl+Shift+I",
				Enabled:  state.shellOpen,
				Action:   action(actions.ImportStatement),
			},
			{Separator: true},
			{
				Title:    text(i18n.MenuSettings),
				Shortcut: "CmdOrCtrl+,",
				Enabled:  state.shellOpen,
				Action:   action(func() { navigate(actions.Navigate, "settings") }),
			},
		},
	}

	viewItems := []menuItemModel{
		{
			Title:    text(i18n.MenuFind),
			Shortcut: "CmdOrCtrl+F",
			Enabled:  state.shellOpen && state.findEnabled,
			Action:   action(actions.Find),
		},
		{
			Title:    text(i18n.Back),
			Shortcut: "CmdOrCtrl+[",
			Enabled:  state.shellOpen && state.backEnabled,
			Action:   action(actions.Back),
		},
		{Separator: true},
	}
	for index := 0; index < 6; index++ {
		screen, ok := shell.PageTarget(index)
		if !ok {
			continue
		}
		itemIndex := index
		viewItems = append(viewItems, menuItemModel{
			Title:    shell.Title(screen, language),
			Shortcut: "CmdOrCtrl+" + string(rune('1'+index)),
			Enabled:  state.shellOpen,
			Action: action(func() {
				if actions.NavigatePage != nil {
					actions.NavigatePage(itemIndex)
				}
			}),
		})
	}
	viewItems = append(viewItems,
		menuItemModel{Separator: true},
		menuItemModel{
			Title:     text(i18n.DarkTheme),
			Shortcut:  "CmdOrCtrl+Shift+D",
			Enabled:   state.shellOpen,
			Checkable: true,
			Checked:   state.dark,
			Action:    action(actions.ToggleTheme),
		},
	)
	view := menuSectionModel{Title: text(i18n.MenuView), Items: viewItems}

	window := menuSectionModel{
		Title: text(i18n.MenuWindow),
		Items: []menuItemModel{
			{
				Title:    text(i18n.MenuMinimize),
				Shortcut: "CmdOrCtrl+M",
				Enabled:  true,
				Action:   action(actions.Minimize),
			},
			{
				Title:   text(i18n.MenuZoom),
				Enabled: true,
				Action:  action(actions.Zoom),
			},
		},
	}

	helpItem := func(key i18n.Key, url string) menuItemModel {
		return menuItemModel{
			Title:   text(key),
			Enabled: true,
			Action: action(func() {
				if actions.OpenURL != nil {
					actions.OpenURL(url)
				}
			}),
		}
	}
	help := menuSectionModel{
		Title: text(i18n.MenuHelp),
		Items: []menuItemModel{
			helpItem(i18n.MenuGitHub, shell.GithubURL),
			helpItem(i18n.MenuReportProblem, shell.ReportProblemURL),
		},
	}

	return []menuSectionModel{file, view, window, help}
}

func navigate(handler func(string), screen string) {
	if handler != nil {
		handler(screen)
	}
}

// nativeMenu turns the pure menu model into the Wails menu, keeping the
// application and Edit menus the app already had.
func nativeMenu(
	app *application.App,
	language i18n.Language,
	state menuState,
	actions menuActions,
) *application.Menu {
	menu := app.NewMenu()

	appMenu := menu.AddSubmenu("Finglass")
	appMenu.AddRole(application.About)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Hide)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Quit)

	editMenu := menu.AddSubmenu("Edit")
	editMenu.AddRole(application.Undo)
	editMenu.AddRole(application.Redo)
	editMenu.AddSeparator()
	editMenu.AddRole(application.Cut)
	editMenu.AddRole(application.Copy)
	editMenu.AddRole(application.Paste)
	editMenu.AddSeparator()
	editMenu.AddRole(application.SelectAll)

	for _, section := range buildMenu(language, state, actions) {
		submenu := menu.AddSubmenu(section.Title)
		for _, item := range section.Items {
			if item.Separator {
				submenu.AddSeparator()
				continue
			}

			var native *application.MenuItem
			if item.Checkable {
				native = submenu.AddCheckbox(item.Title, item.Checked)
			} else {
				native = submenu.Add(item.Title)
			}
			if item.Shortcut != "" {
				native.SetAccelerator(item.Shortcut)
			}
			native.SetEnabled(item.Enabled)
			if item.Action != nil {
				native.OnClick(func(*application.Context) { item.Action() })
			}
		}
	}

	return menu
}

// transactionsMenuService opens the Transactions page with the dialog the menu
// asked for.
type transactionsMenuService interface {
	OpenWithAdd() (bind.TransactionsDTO, error)
	OpenWithImport() (bind.TransactionsDTO, error)
}

// menuController owns the menu's copy of the shell state and rebuilds the
// native menu whenever the shell changes.
type menuController struct {
	mu sync.Mutex

	app          *application.App
	window       *application.WebviewWindow
	service      *bind.Shell
	transactions transactionsMenuService
	openURL      func(string)

	state    menuState
	language i18n.Language

	// running is set once the first menu is in place before app.Run; later
	// rebuilds come from Wails' worker goroutines.
	running atomic.Bool
}

// apply follows a shell DTO and rebuilds the menu.
func (c *menuController) apply(dto bind.ShellDTO) {
	c.mu.Lock()
	c.state = menuState{
		shellOpen:   dto.ShellOpen,
		findEnabled: dto.FindEnabled,
		backEnabled: dto.BackTitle != "",
		dark:        c.effectiveDark(dto.Theme),
	}
	if language, ok := i18n.LanguageFromCode(dto.Language); ok {
		c.language = language
	}
	c.mu.Unlock()

	c.rebuild()
}

// effectiveDark reports whether the current theme shows the dark palette, as
// Rust's Theme.dark does; "system" follows the operating system.
func (c *menuController) effectiveDark(theme string) bool {
	preference, ok := prefs.ThemeFromCode(theme)
	if !ok {
		preference = prefs.ThemeSystem
	}
	switch preference {
	case prefs.ThemeDark:
		return true
	case prefs.ThemeLight:
		return false
	default:
		return c.app.Env.IsDarkMode()
	}
}

// rebuild replaces the native menu. Wails marshals only setMainMenu to the main
// thread, while AppKit wants the NSMenu items built there too, so once the app
// runs every rebuild goes through InvokeAsync. The closure reads the newest
// state when it runs, so out-of-order rebuilds still end on the latest one.
func (c *menuController) rebuild() {
	set := func() {
		c.mu.Lock()
		language := c.language
		state := c.state
		c.mu.Unlock()

		c.app.Menu.Set(nativeMenu(c.app, language, state, c.actions()))
	}
	if c.running.Load() {
		application.InvokeAsync(set)
		return
	}
	set()
}

func (c *menuController) actions() menuActions {
	emit := func(action string) {
		c.app.Event.Emit(MenuActionEvent, action)
	}
	toggleTheme := func() {
		dto, err := c.service.State()
		if err != nil {
			return
		}
		c.mu.Lock()
		next := prefs.ThemeDark
		if c.effectiveDark(dto.Theme) {
			next = prefs.ThemeLight
		}
		c.mu.Unlock()
		_, _ = c.service.SetTheme(next.Code())
	}

	return menuActions{
		NewTransaction: func() {
			if c.transactions != nil {
				_, _ = c.transactions.OpenWithAdd()
			}
		},
		ImportStatement: func() {
			if c.transactions != nil {
				_, _ = c.transactions.OpenWithImport()
			}
		},
		Navigate: func(screen string) {
			_, _ = c.service.Navigate(screen)
		},
		NavigatePage: func(index int) {
			_, _ = c.service.NavigateMenuItem(index)
		},
		Find: func() { emit("find") },
		Back: func() {
			_, _ = c.service.Back()
		},
		ToggleTheme: toggleTheme,
		Minimize: func() {
			c.window.Minimise()
		},
		Zoom: func() {
			c.window.ToggleMaximise()
		},
		OpenURL: func(url string) {
			if c.openURL != nil {
				c.openURL(url)
			}
		},
	}
}
