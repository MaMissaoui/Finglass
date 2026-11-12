package service

import (
	"errors"

	"github.com/MaMissaoui/Finglass/internal/core/documents/limits"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// DocumentError is an error from document handling with a user-facing message.
type DocumentError interface {
	error
	// Message returns the user text in lang. Technical failures keep their
	// English description.
	Message(lang i18n.Language) string
}

// FileDoesNotExist means the source file is not a regular file. It holds the
// display file name.
type FileDoesNotExist struct {
	// Name is the source's file name.
	Name string
}

// Error is the English text.
func (e *FileDoesNotExist) Error() string {
	return e.Message(i18n.En)
}

// Message returns the user text in lang.
func (e *FileDoesNotExist) Message(lang i18n.Language) string {
	return i18n.Format(lang, i18n.ErrorFileDoesNotExist, i18n.Str(e.Name))
}

// DocumentTooLarge means the file exceeds limits.MaxDocumentBytes. It holds
// the display file name.
type DocumentTooLarge struct {
	// Name is the source's file name.
	Name string
}

// Error is the English text.
func (e *DocumentTooLarge) Error() string {
	return e.Message(i18n.En)
}

// Message returns the user text in lang.
func (e *DocumentTooLarge) Message(lang i18n.Language) string {
	return i18n.Format(
		lang,
		i18n.ErrorDocumentTooLarge,
		i18n.Str(e.Name),
		i18n.Int(int64(limits.MaxDocumentBytes/(1024*1024))),
	)
}

// UnsupportedFileType means the extension is not a supported document type. It
// holds the extension exactly as the file wrote it.
type UnsupportedFileType struct {
	// Extension is the file's extension.
	Extension string
}

// Error is the English text.
func (e *UnsupportedFileType) Error() string {
	return e.Message(i18n.En)
}

// Message returns the user text in lang.
func (e *UnsupportedFileType) Message(lang i18n.Language) string {
	return i18n.Format(lang, i18n.ErrorUnsupportedFileType, i18n.Str(e.Extension))
}

type wrappedDocumentError struct {
	cause error
}

func (e *wrappedDocumentError) Error() string {
	return e.cause.Error()
}

func (e *wrappedDocumentError) Unwrap() error {
	return e.cause
}

func (e *wrappedDocumentError) Message(i18n.Language) string {
	return e.cause.Error()
}

type notFoundError struct{}

func (notFoundError) Error() string {
	return "document not found"
}

func (notFoundError) Message(i18n.Language) string {
	return "document not found"
}

// ErrNotFound means the stored document row disappeared.
var ErrNotFound DocumentError = notFoundError{}

func wrapDocumentError(err error) DocumentError {
	if err == nil {
		return nil
	}
	return &wrappedDocumentError{cause: err}
}

// Message returns the user-facing text for err in lang. Technical failures
// (database, file system) keep their English description.
func Message(err error, lang i18n.Language) string {
	if err == nil {
		return ""
	}
	var documentError DocumentError
	if errors.As(err, &documentError) {
		return documentError.Message(lang)
	}
	return err.Error()
}
