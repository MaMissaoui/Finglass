// Command finglass is the Finglass desktop application.
package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/frontend"
	"github.com/MaMissaoui/Finglass/internal/bind"
)

func main() {
	// Finder may add its own arguments, so look for the flag instead of
	// handing all of os.Args to flag.Parse.
	for _, arg := range os.Args[1:] {
		if arg == "--self-check" || arg == "-self-check" {
			os.Exit(runSelfCheck())
		}
	}

	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		log.Fatalf("open embedded frontend: %v", err)
	}

	app := application.New(applicationOptions(dist))

	if runtime.GOOS == "darwin" {
		app.Menu.Set(applicationMenu(app))
	}

	app.Window.NewWithOptions(application.WebviewWindowOptions{
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

func applicationOptions(dist fs.FS) application.Options {
	return application.Options{
		Name:        "Finglass",
		Description: "Read-only, local-first personal finance",
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

// applicationMenu builds the native macOS menu. The Edit roles are what give
// text fields the standard shortcuts.
func applicationMenu(app *application.App) *application.Menu {
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

	return menu
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
