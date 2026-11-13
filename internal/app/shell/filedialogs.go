package shell

// FileDialogs is the shell's native file-dialog seam.
//
// Production implements it with Wails' dialogs in cmd/finglass; tests use a
// fake that returns fixture or temporary paths. The session holds one and calls
// it inside a job, without the session mutex, exactly as Rust calls rfd inside
// spawn. A nil seam means every dialog is cancelled.
//
// No method takes a path, a file name, a directory or file contents: the dialog
// is the only source of a path, and nothing returns one to JavaScript.
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
}

// SetFileDialogs installs the native file-dialog seam. Production sets it once,
// after the main window exists and before any page can open a dialog.
func (s *Session) SetFileDialogs(dialogs FileDialogs) {
	s.mu.Lock()
	s.fileDialogs = dialogs
	s.mu.Unlock()
}
