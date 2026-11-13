package main

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
)

// statementExtensions is Rust's STATEMENT_EXTENSIONS as one Wails filter
// pattern.
const statementExtensions = "*.csv;*.sta;*.mt940;*.xml;*.zip;*.ofx;*.qfx;*.qif;*.xlsx"

// wailsFileDialogs is the production file-dialog seam. The session calls it
// inside a job, without its mutex, and every dialog is attached to the main
// window.
type wailsFileDialogs struct {
	app    *application.App
	window *application.WebviewWindow
}

// newWailsFileDialogs builds the native dialog seam for the main window.
func newWailsFileDialogs(app *application.App, window *application.WebviewWindow) *wailsFileDialogs {
	return &wailsFileDialogs{app: app, window: window}
}

// PickStatements asks for one or more bank statement files.
func (d *wailsFileDialogs) PickStatements(title string) ([]string, bool) {
	if d == nil || d.app == nil {
		return nil, false
	}
	files, err := d.app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter(title, statementExtensions).
		AttachToWindow(d.window).
		PromptForMultipleSelection()
	return selectedPaths(files, err)
}

// PickReceipts asks for one or more receipt files.
func (d *wailsFileDialogs) PickReceipts(title string) ([]string, bool) {
	if d == nil || d.app == nil {
		return nil, false
	}
	files, err := d.app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter(title, filterPattern(documentsservice.SupportedExtensions)).
		AttachToWindow(d.window).
		PromptForMultipleSelection()
	return selectedPaths(files, err)
}

// SaveCSV asks where to write the export.
func (d *wailsFileDialogs) SaveCSV(title string, defaultName string) (string, bool) {
	if d == nil || d.app == nil {
		return "", false
	}
	path, err := d.app.Dialog.SaveFile().
		SetFilename(defaultName).
		AddFilter(title, "*.csv").
		AttachToWindow(d.window).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

func selectedPaths(paths []string, err error) ([]string, bool) {
	if err != nil || len(paths) == 0 {
		return nil, false
	}
	return paths, true
}

// filterPattern turns bare extensions into Wails' semicolon-separated pattern.
func filterPattern(extensions []string) string {
	patterns := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		extension = strings.TrimSpace(strings.TrimPrefix(extension, "."))
		if extension == "" {
			continue
		}
		patterns = append(patterns, "*."+extension)
	}
	return strings.Join(patterns, ";")
}
