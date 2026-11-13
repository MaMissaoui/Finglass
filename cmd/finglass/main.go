// Command finglass is the Finglass desktop application.
package main

import (
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/frontend"
	"github.com/MaMissaoui/Finglass/internal/app/findesk"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/bind"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// shellService is set from main before the Wails application is built. The
// self-check process only inspects its type, so its zero value is enough.
var shellService = bind.NewShell(nil, nil, bind.ShellOptions{})

// dashboardService is set from main once the shell service exists.
var dashboardService = bind.NewDashboard(nil, bind.DashboardOptions{})

// accountsService is set from main once the shell service exists.
var accountsService = bind.NewAccounts(nil, bind.AccountsOptions{})

// bankingService is set from main once the shell service exists.
var bankingService = bind.NewBanking(nil, bind.BankingOptions{})

func main() {
	// Finder may add its own arguments, so look for the flag instead of
	// handing all of os.Args to flag.Parse.
	for _, arg := range os.Args[1:] {
		if arg == "--self-check" || arg == "-self-check" {
			os.Exit(runSelfCheck())
		}
	}

	appPaths, err := paths.FromEnv()
	if err != nil {
		log.Fatalf("resolve data paths: %v", err)
	}

	session, offer, _ := shell.Start(
		appPaths,
		banksync.SystemCredentialStore(),
		findesk.DefaultFindeskStartDir(),
		time.Now,
		func() civil.Date { return civil.FromTime(time.Now()) },
		os.LookupEnv,
	)

	var app *application.App
	var window *application.WebviewWindow
	var menu *menuController
	var shellStateMu sync.Mutex
	shellWasOpen := false

	openURL := func(url string) {
		if app != nil {
			_ = app.Browser.OpenURL(url)
		}
	}

	shellService = bind.NewShell(session, offer, bind.ShellOptions{
		Paths:   appPaths,
		Clock:   time.Now,
		OpenURL: openURL,
		AfterChange: func(dto bind.ShellDTO) {
			shellStateMu.Lock()
			maximise := dto.ShellOpen && !shellWasOpen
			shellWasOpen = dto.ShellOpen
			shellStateMu.Unlock()

			if maximise && window != nil {
				window.Maximise()
			}
			if menu != nil {
				menu.apply(dto)
			}
		},
	})
	dashboardService = bind.NewDashboard(session, bind.DashboardOptions{})
	accountsService = bind.NewAccounts(session, bind.AccountsOptions{})
	bankingService = bind.NewBanking(session, bind.BankingOptions{})

	router := bind.NewRouter(session)
	router.Register("dashboard", dashboardService)
	router.Register("accounts", accountsService)
	router.Register("banking", bankingService)
	router.RegisterPrompt("banking-prompt", bankingService)
	router.RegisterShell(shellService)

	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		log.Fatalf("open embedded frontend: %v", err)
	}

	app = application.New(applicationOptions(dist))

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Finglass",
		Width:            1100,
		Height:           720,
		MinWidth:         800,
		MinHeight:        560,
		URL:              "/",
		BackgroundColour: application.NewRGB(255, 255, 255),
		DevToolsEnabled:  false,
	})

	if runtime.GOOS == "darwin" {
		menu = &menuController{
			app:     app,
			window:  window,
			service: shellService,
			openURL: openURL,
		}
		if dto, stateErr := shellService.State(); stateErr == nil {
			menu.apply(dto)
		}
		menu.running.Store(true)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// boundService is one Wails service with its registration name.
type boundService struct {
	name    string
	service application.Service
}

func boundServices() []boundService {
	return []boundService{
		{name: "AppInfo", service: application.NewService(&bind.AppInfo{})},
		{name: "Shell", service: application.NewService(shellService)},
		{name: "Dashboard", service: application.NewService(dashboardService)},
		{name: "Accounts", service: application.NewService(accountsService)},
		{name: "Banking", service: application.NewService(bankingService)},
	}
}

func services() []application.Service {
	specs := boundServices()
	result := make([]application.Service, 0, len(specs))
	for _, spec := range specs {
		result = append(result, spec.service)
	}
	return result
}

// systemLogger is Wails' own logger. Wails logs every bound call's raw
// arguments at Debug ("Binding call complete", "Runtime call"), and those
// arguments include passphrases, so the level is pinned at Info whatever the
// build tags.
func systemLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func applicationOptions(dist fs.FS) application.Options {
	return application.Options{
		Name:        "Finglass",
		Description: "Read-only, local-first personal finance",
		Logger:      systemLogger(),
		LogLevel:    slog.LevelInfo,
		Services:    services(),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(dist),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.mamissaoui.finglass",
			ExitCode: 1,
		},
	}
}

func runSelfCheck() int {
	if err := checkAssets(); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		return 1
	}
	if err := checkServices(); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		return 1
	}
	fmt.Printf("OK: %s\n", bind.Version)
	return 0
}

func checkAssets() error {
	if err := fs.WalkDir(frontend.Dist, ".", func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." || path == "dist" {
			return nil
		}
		if !strings.HasPrefix(path, "dist/") {
			return fmt.Errorf("embedded file %q is outside dist", path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk embedded assets: %w", err)
	}

	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return fmt.Errorf("open dist: %w", err)
	}

	if info, err := fs.Stat(dist, "index.html"); err != nil || info.IsDir() {
		return fmt.Errorf("embedded assets have no index.html")
	}

	foundJS := false
	if err := fs.WalkDir(dist, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".js") {
			foundJS = true
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk dist: %w", err)
	}
	if !foundJS {
		return fmt.Errorf("embedded assets have no JS bundle")
	}

	return nil
}

type expectedService struct {
	name    string
	matches func(any) bool
}

// expectedServices is the self-check's registry of services that must be
// bound. Add an entry here whenever a service is added to boundServices.
var expectedServices = []expectedService{
	{
		name: "AppInfo",
		matches: func(instance any) bool {
			_, ok := instance.(*bind.AppInfo)
			return ok
		},
	},
	{
		name: "Shell",
		matches: func(instance any) bool {
			_, ok := instance.(*bind.Shell)
			return ok
		},
	},
	{
		name: "Dashboard",
		matches: func(instance any) bool {
			_, ok := instance.(*bind.Dashboard)
			return ok
		},
	},
	{
		name: "Accounts",
		matches: func(instance any) bool {
			_, ok := instance.(*bind.Accounts)
			return ok
		},
	},
	{
		name: "Banking",
		matches: func(instance any) bool {
			_, ok := instance.(*bind.Banking)
			return ok
		},
	},
}

func checkServices() error {
	registered := services()
	seenNames := make(map[string]bool, len(registered))

	for _, service := range registered {
		instance := service.Instance()
		if instance == nil {
			return fmt.Errorf("bound service has no instance")
		}

		matched := false
		for _, expected := range expectedServices {
			if expected.matches(instance) {
				matched = true
				if seenNames[expected.name] {
					return fmt.Errorf("bound service %q is registered twice", expected.name)
				}
				seenNames[expected.name] = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("bound service %T is not in the self-check registry", instance)
		}
	}

	for _, expected := range expectedServices {
		if !seenNames[expected.name] {
			return fmt.Errorf("bound service %q is not registered", expected.name)
		}
	}
	return nil
}
