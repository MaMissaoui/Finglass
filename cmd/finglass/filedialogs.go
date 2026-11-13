package main

import (
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// wailsFileDialogs is the production implementation of the shell's dialog
// seam.
var _ shell.FileDialogs = (*wailsFileDialogs)(nil)

// statementExtensions is Rust's STATEMENT_EXTENSIONS as one Wails filter
// pattern.
const statementExtensions = "*.csv;*.sta;*.mt940;*.xml;*.zip;*.ofx;*.qfx;*.qif;*.xlsx"

// wailsFileDialogs is the production file-dialog seam. The session calls it
// inside a job, without its mutex, and every dialog is attached to the main
// window.
type wailsFileDialogs struct {
	app      *application.App
	window   *application.WebviewWindow
	language func() i18n.Language
}

// newWailsFileDialogs builds the native dialog seam for the main window.
func newWailsFileDialogs(
	app *application.App,
	window *application.WebviewWindow,
	language func() i18n.Language,
) *wailsFileDialogs {
	return &wailsFileDialogs{app: app, window: window, language: language}
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

// SaveFile asks where to write one of the Reports exports. The title key is
// localized with the profile's current language.
func (d *wailsFileDialogs) SaveFile(title i18n.Key, defaultName string) (string, bool) {
	if d == nil || d.app == nil {
		return "", false
	}
	language := i18n.En
	if d.language != nil {
		language = d.language()
	}
	titleText := i18n.Text(language, title)
	pattern := exportFilterPattern(defaultName)
	options := &application.SaveFileDialogOptions{
		Title:                titleText,
		Filename:             defaultName,
		CanCreateDirectories: true,
		Window:               d.window,
	}
	if pattern != "" {
		options.Filters = []application.FileFilter{{DisplayName: titleText, Pattern: pattern}}
	}
	path, err := d.app.Dialog.SaveFileWithOptions(options).PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

// SaveRules asks where to write the rule export.
func (d *wailsFileDialogs) SaveRules(title string, defaultName string) (string, bool) {
	if d == nil || d.app == nil {
		return "", false
	}
	path, err := d.app.Dialog.SaveFile().
		SetFilename(defaultName).
		AddFilter(title, "*.tsv").
		AttachToWindow(d.window).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

// exportFilterPattern turns a suggested file name's extension into Wails'
// filter pattern.
func exportFilterPattern(defaultName string) string {
	extension := strings.ToLower(filepath.Ext(defaultName))
	if extension == "" || extension == "." {
		return ""
	}
	return "*" + extension
}

// SaveBackup asks where to write a backup.
func (d *wailsFileDialogs) SaveBackup(title string, defaultName string) (string, bool) {
	if d == nil || d.app == nil {
		return "", false
	}
	path, err := d.app.Dialog.SaveFile().
		SetFilename(defaultName).
		AddFilter(title, "*.zip").
		AttachToWindow(d.window).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

// PickBackup asks for one backup file.
func (d *wailsFileDialogs) PickBackup(title string) (string, bool) {
	if d == nil || d.app == nil {
		return "", false
	}
	path, err := d.app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter(title, "*.zip").
		AttachToWindow(d.window).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

// PickFindeskFolder asks for the FinDesk folder, starting in startDir when it
// is not empty. The start folder comes from cmd/finglass, not the shell.
func (d *wailsFileDialogs) PickFindeskFolder(title string, startDir string) (string, bool) {
	if d == nil || d.app == nil {
		return "", false
	}
	options := &application.OpenFileDialogOptions{
		Title:                title,
		CanChooseDirectories: true,
		CanChooseFiles:       false,
		CanCreateDirectories: false,
		ShowHiddenFiles:      true,
		Window:               d.window,
	}
	if strings.TrimSpace(startDir) != "" {
		options.Directory = startDir
	}
	path, err := d.app.Dialog.OpenFileWithOptions(options).PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

// PickRuleFile asks for one rule file.
func (d *wailsFileDialogs) PickRuleFile(title string) (string, bool) {
	if d == nil || d.app == nil {
		return "", false
	}
	path, err := d.app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter(title, "*.tsv;*.txt").
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
