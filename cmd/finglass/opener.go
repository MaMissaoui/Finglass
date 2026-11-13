package main

import "github.com/wailsapp/wails/v3/pkg/application"

// wailsFileOpener opens written files with the platform's default viewer.
//
// Reports uses it for print_tax, as Rust calls open::that_detached on the
// temporary Tax bookings PDF.
type wailsFileOpener struct {
	app *application.App
}

// newWailsFileOpener builds the system-viewer seam for the main application.
func newWailsFileOpener(app *application.App) *wailsFileOpener {
	return &wailsFileOpener{app: app}
}

// OpenFile opens path in the system's default application.
func (o *wailsFileOpener) OpenFile(path string) error {
	if o == nil || o.app == nil {
		return nil
	}
	return o.app.Browser.OpenFile(path)
}
