package shell

import "github.com/MaMissaoui/Finglass/internal/core/i18n"

// FileDialogs is the shell's native file-dialog seam.
//
// Production implements it with Wails' dialogs in cmd/finglass; tests use a
// fake that returns fixture or temporary paths. The session holds one and calls
// it inside a job, without the session mutex, exactly as Rust calls rfd inside
// spawn. A nil seam means every dialog is cancelled.
//
// No method takes a destination, a file's contents or a folder to read: the
// dialog is the only source of a path, and nothing returns one to JavaScript.
// SaveFile's default name and PickFindeskFolder's start folder are only the
// dialog's suggestions and are never used as a destination.
type FileDialogs interface {
	// PickStatements asks for one or more statement files. The boolean is false
	// when the user cancelled.
	PickStatements(title string) ([]string, bool)
	// PickReceipts asks for one or more receipt files. The boolean is false
	// when the user cancelled.
	PickReceipts(title string) ([]string, bool)
	// SaveCSV asks where to write the export. The boolean is false when the
	// user cancelled.
	SaveCSV(title string, defaultName string) (string, bool)
	// SaveFile asks where to write one of the Reports exports. title is the
	// i18n key of the dialog title; defaultName is the suggested file name. The
	// boolean is false when the user cancelled.
	SaveFile(title i18n.Key, defaultName string) (string, bool)
	// SaveTrace asks where to write the redacted connection trace. The boolean
	// is false when the user cancelled.
	SaveTrace(title string, defaultName string) (string, bool)
	// SaveRules asks where to write the rule export. The boolean is false when
	// the user cancelled.
	SaveRules(title string, defaultName string) (string, bool)
	// PickRuleFile asks for one rule file. The boolean is false when the user
	// cancelled.
	PickRuleFile(title string) (string, bool)
	// SaveBackup asks where to write a backup. defaultName is the suggested
	// name; the boolean is false when the user cancelled.
	SaveBackup(title string, defaultName string) (string, bool)
	// PickBackup asks for one backup file, with the zip filter. The boolean is
	// false when the user cancelled.
	PickBackup(title string) (string, bool)
	// PickFindeskFolder asks for a FinDesk folder. startDir is the folder the
	// picker starts in; it may be empty and is never resolved by the shell.
	PickFindeskFolder(title string, startDir string) (string, bool)
}

// SetFileDialogs installs the native file-dialog seam. Production sets it once,
// after the main window exists and before any page can open a dialog.
func (s *Session) SetFileDialogs(dialogs FileDialogs) {
	s.mu.Lock()
	s.fileDialogs = dialogs
	s.mu.Unlock()
}

// FileOpener opens a written file with the system's default viewer.
//
// print_tax uses it for the temporary Tax bookings PDF, as Rust calls
// open::that_detached. Tests record the path and never open anything.
type FileOpener interface {
	// OpenFile opens path with the platform's default application.
	OpenFile(path string) error
}

// SetFileOpener installs the system-viewer seam. Production sets it once, after
// the main window exists.
func (s *Session) SetFileOpener(opener FileOpener) {
	s.mu.Lock()
	s.fileOpener = opener
	s.mu.Unlock()
}
