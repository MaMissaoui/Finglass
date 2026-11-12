// Package service manages documents: storage, OCR, linking and transaction
// suggestions.
//
// Documents live in <profile dir>/documents/. A stored file is named
// <uuid>_<sanitized name> and is made owner-only on Unix. Deleting a document
// removes its stored file only when the canonical path is inside the documents
// folder. This ports FinDesk's DocumentService without the loading/error state
// flows.
package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/limits"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/receipt"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

// SupportedExtensions are the file extensions a document may have.
var SupportedExtensions = []string{"pdf", "jpg", "jpeg", "png", "tif", "tiff"}

// matchWindowDays is how many days around a receipt date matching transactions
// are considered.
const matchWindowDays = 14

// UploadResult is the result of an upload.
type UploadResult struct {
	// Document is the stored document.
	Document model.DocumentItem
	// OCR is the extracted text, when there is any.
	OCR *ocr.Result
}

// MatchSuggestion is a transaction that plausibly matches a receipt, with a
// 0..1 score.
type MatchSuggestion struct {
	// Transaction is the candidate transaction.
	Transaction model.TransactionItem
	// Score is the match score from 0 to 1.
	Score float64
}

// Service is document storage and lookup.
type Service struct {
	db           *db.Database
	ocr          *ocr.Service
	documentsDir string
}

// NewService builds a service for one database and documents folder.
func NewService(d *db.Database, ocrService *ocr.Service, documentsDir string) *Service {
	if ocrService == nil {
		ocrService = ocr.WithoutOCR()
	}
	return &Service{db: d, ocr: ocrService, documentsDir: documentsDir}
}

// DefaultDocumentsDirectory returns the folder for a profile's documents, as
// M8a backs it up.
func DefaultDocumentsDirectory(appPaths paths.AppPaths, slug string) (string, error) {
	directory, err := appPaths.ProfileDir(slug)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "documents"), nil
}

// DocumentsDir returns the folder this service stores documents in.
func (s *Service) DocumentsDir() string {
	return s.documentsDir
}

// UploadDocument copies source into the documents folder, records it, and
// extracts text.
func (s *Service) UploadDocument(
	ctx context.Context,
	source string,
	transactionID *model.TransactionID,
	performOCR bool,
) (*UploadResult, DocumentError) {
	fileName := fileNameOf(source)

	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return nil, &FileDoesNotExist{Name: fileName}
	}

	if info.Size() > int64(limits.MaxDocumentBytes) {
		return nil, &DocumentTooLarge{Name: fileName}
	}

	mimeType := DetectMIMEType(source)
	if mimeType == "application/octet-stream" {
		return nil, &UnsupportedFileType{Extension: extensionOf(source)}
	}

	if err := paths.EnsureDir(s.documentsDir); err != nil {
		return nil, wrapDocumentError(err)
	}

	identifier, err := newUUID()
	if err != nil {
		return nil, wrapDocumentError(fmt.Errorf("generate document id: %w", err))
	}
	storedName := identifier + "_" + SanitizeFileName(fileName)
	destination := filepath.Join(s.documentsDir, storedName)
	// Rust leaves a partial copy behind when the copy fails. Removing it here
	// is the one deliberate improvement over Rust.
	if err := copyFile(source, destination); err != nil {
		_ = os.Remove(destination)
		return nil, wrapDocumentError(err)
	}
	if err := secureFile(destination); err != nil {
		return nil, wrapDocumentError(err)
	}

	created, err := documents.Create(ctx, s.db, model.NewDocument{
		FileName:      fileName,
		StoredPath:    destination,
		MIMEType:      mimeType,
		TransactionID: transactionID,
	})
	if err != nil {
		return nil, wrapDocumentError(err)
	}

	var extracted *ocr.Result
	if performOCR {
		extracted = s.ocr.ProcessDocument(destination)
	}

	if extracted != nil && strings.TrimSpace(extracted.Text) != "" {
		confidence := extracted.Confidence
		if _, err := documents.SaveOCRText(
			ctx,
			s.db,
			created.ID,
			extracted.Text,
			extracted.Language,
			&confidence,
		); err != nil {
			return nil, wrapDocumentError(err)
		}
		if err := audit.Log(
			ctx,
			s.db,
			"OCR_PROCESSED",
			audit.ActorSystem,
			fmt.Sprintf("Text extracted (%s) for: %s", extracted.Source, fileName),
		); err != nil {
			return nil, wrapDocumentError(err)
		}
	}

	if err := audit.Log(
		ctx,
		s.db,
		"DOCUMENT_UPLOADED",
		audit.ActorUser,
		"Uploaded: "+fileName,
	); err != nil {
		return nil, wrapDocumentError(err)
	}

	document, ok, err := documents.GetItem(ctx, s.db, created.ID)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	if !ok {
		return nil, ErrNotFound
	}

	return &UploadResult{Document: document, OCR: extracted}, nil
}

// ReprocessOCR re-runs text extraction for an existing document.
func (s *Service) ReprocessOCR(ctx context.Context, documentID int64) (*ocr.Result, DocumentError) {
	document, ok, err := documents.Get(ctx, s.db, documentID)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	if !ok {
		return nil, nil
	}

	result := s.ocr.ProcessDocument(document.StoredPath)
	if result != nil && strings.TrimSpace(result.Text) != "" {
		confidence := result.Confidence
		if _, err := documents.SaveOCRText(
			ctx,
			s.db,
			documentID,
			result.Text,
			result.Language,
			&confidence,
		); err != nil {
			return nil, wrapDocumentError(err)
		}
		if err := audit.Log(
			ctx,
			s.db,
			"OCR_PROCESSED",
			audit.ActorUser,
			fmt.Sprintf("Text extracted again (%s) for: %s", result.Source, document.FileName),
		); err != nil {
			return nil, wrapDocumentError(err)
		}
	}

	return result, nil
}

// LinkToTransaction links a document to a transaction.
func (s *Service) LinkToTransaction(
	ctx context.Context,
	documentID int64,
	transactionID model.TransactionID,
) (bool, DocumentError) {
	linked, err := documents.LinkToTransaction(ctx, s.db, documentID, transactionID)
	if err != nil {
		return false, wrapDocumentError(err)
	}
	ok := linked != nil
	if ok {
		if err := audit.Log(
			ctx,
			s.db,
			"DOCUMENT_LINKED",
			audit.ActorUser,
			fmt.Sprintf("Linked document %d to transaction %d", documentID, int64(transactionID)),
		); err != nil {
			return false, wrapDocumentError(err)
		}
	}
	return ok, nil
}

// UnlinkFromTransaction removes a document's transaction link.
func (s *Service) UnlinkFromTransaction(ctx context.Context, documentID int64) (bool, DocumentError) {
	unlinked, err := documents.UnlinkFromTransaction(ctx, s.db, documentID)
	if err != nil {
		return false, wrapDocumentError(err)
	}
	ok := unlinked != nil
	if ok {
		if err := audit.Log(
			ctx,
			s.db,
			"DOCUMENT_UNLINKED",
			audit.ActorUser,
			fmt.Sprintf("Unlinked document %d", documentID),
		); err != nil {
			return false, wrapDocumentError(err)
		}
	}
	return ok, nil
}

// SearchDocuments searches by file name or OCR text.
func (s *Service) SearchDocuments(ctx context.Context, query string) ([]model.DocumentItem, DocumentError) {
	items, err := documents.FindItems(ctx, s.db, &query, false, nil)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	return items, nil
}

// ListDocuments lists documents, optionally filtered by text or by missing
// transaction links.
func (s *Service) ListDocuments(
	ctx context.Context,
	query *string,
	unlinkedOnly bool,
) ([]model.DocumentItem, DocumentError) {
	items, err := documents.FindItems(ctx, s.db, query, unlinkedOnly, nil)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	return items, nil
}

// GetUnlinkedDocuments lists documents without a linked transaction.
func (s *Service) GetUnlinkedDocuments(ctx context.Context) ([]model.DocumentItem, DocumentError) {
	items, err := documents.FindItems(ctx, s.db, nil, true, nil)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	return items, nil
}

// GetDocumentsForTransaction lists documents linked to one transaction.
func (s *Service) GetDocumentsForTransaction(
	ctx context.Context,
	transactionID model.TransactionID,
) ([]model.DocumentItem, DocumentError) {
	items, err := documents.FindItems(ctx, s.db, nil, false, &transactionID)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	return items, nil
}

// GetOCRText returns the stored OCR text of a document, or nil when there is
// none.
func (s *Service) GetOCRText(ctx context.Context, documentID int64) (*string, DocumentError) {
	content, ok, err := documents.GetOCRText(ctx, s.db, documentID)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	if !ok {
		return nil, nil
	}
	return &content, nil
}

// SuggestTransactions suggests transactions for a document based on the total
// and date found in its text.
//
// An expense with the same absolute amount scores highest; closer dates score
// higher.
func (s *Service) SuggestTransactions(
	ctx context.Context,
	documentID int64,
	limit int,
) ([]MatchSuggestion, DocumentError) {
	text, ok, err := documents.GetOCRText(ctx, s.db, documentID)
	if err != nil {
		return nil, wrapDocumentError(err)
	}
	if !ok {
		return []MatchSuggestion{}, nil
	}

	fields := receipt.Parse(text, money.EUR, civil.FromTime(time.Now().UTC()))
	if len(fields.Amounts) == 0 {
		return []MatchSuggestion{}, nil
	}

	anchor := fields.Date
	filter := model.TransactionFilter{}
	if anchor != nil {
		start := anchor.AddDays(-matchWindowDays)
		end := anchor.AddDays(matchWindowDays)
		filter.StartDate = &start
		filter.EndDate = &end
	}
	candidates, err := transactions.FindItems(ctx, s.db, &filter, nil)
	if err != nil {
		return nil, wrapDocumentError(err)
	}

	suggestions := make([]MatchSuggestion, 0, len(candidates))
	for _, transaction := range candidates {
		amount := absoluteMinor(transaction.Amount.Minor)

		amountScore := 0.0
		matched := false
		if fields.Total != nil {
			switch {
			case fields.Total.Minor == amount:
				amountScore = 1.0
				matched = true
			case containsAmount(fields.Amounts, amount):
				amountScore = 0.6
				matched = true
			}
		} else if containsAmount(fields.Amounts, amount) {
			amountScore = 0.6
			matched = true
		}
		if !matched {
			continue
		}

		dateScore := 0.5
		if anchor != nil {
			days := daysBetween(transaction.Date, *anchor)
			if days < 0 {
				days = -days
			}
			dateScore = 1.0 - float64(days)/float64(matchWindowDays+1)
		}

		suggestions = append(suggestions, MatchSuggestion{
			Transaction: transaction,
			Score:       amountScore*0.7 + dateScore*0.3,
		})
	}

	sort.SliceStable(suggestions, func(left, right int) bool {
		return suggestions[left].Score > suggestions[right].Score
	})
	if limit < 0 {
		limit = 0
	}
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions, nil
}

// DeleteDocument deletes a document record and its stored file.
//
// Only files whose canonical path is inside the documents folder are deleted.
func (s *Service) DeleteDocument(ctx context.Context, documentID int64) (bool, DocumentError) {
	document, ok, err := documents.Get(ctx, s.db, documentID)
	if err != nil {
		return false, wrapDocumentError(err)
	}
	if !ok {
		return false, nil
	}

	file := document.StoredPath
	if _, err := os.Stat(file); err == nil {
		canonicalFile, fileErr := filepath.EvalSymlinks(file)
		canonicalDir, dirErr := filepath.EvalSymlinks(s.documentsDir)
		if fileErr == nil && dirErr == nil && insideDirectory(canonicalDir, canonicalFile) {
			if err := os.Remove(canonicalFile); err != nil {
				return false, wrapDocumentError(err)
			}
		}
	}

	if _, err := documents.Delete(ctx, s.db, documentID); err != nil {
		return false, wrapDocumentError(err)
	}
	if err := audit.Log(
		ctx,
		s.db,
		"DOCUMENT_DELETED",
		audit.ActorUser,
		"Deleted document: "+document.FileName,
	); err != nil {
		return false, wrapDocumentError(err)
	}
	return true, nil
}

// DetectMIMEType returns the MIME type for a supported file extension.
//
// The extension is case-sensitive, as in Rust's service::extension_of: "X.PDF"
// is not a PDF.
func DetectMIMEType(file string) string {
	switch extensionOf(file) {
	case "pdf":
		return "application/pdf"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "tiff", "tif":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

// SanitizeFileName makes a user-supplied file name safe to use on disk.
//
// It keeps the last path segment, drops control characters and bounds the
// length. The display name is stored separately and unchanged.
func SanitizeFileName(name string) string {
	index := strings.LastIndexAny(name, `/\`)
	if index >= 0 {
		name = name[index+1:]
	}
	cleaned := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		cleaned = "document"
	}
	runes := []rune(cleaned)
	if len(runes) > 150 {
		runes = runes[:150]
	}
	return string(runes)
}

func extensionOf(file string) string {
	base := filepath.Base(file)
	index := strings.LastIndexByte(base, '.')
	if index <= 0 {
		return ""
	}
	return base[index+1:]
}

func fileNameOf(file string) string {
	base := filepath.Base(file)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return "document"
	}
	return base
}

func newUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0F) | 0x40
	bytes[8] = (bytes[8] & 0x3F) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	), nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source) // #nosec G304 -- the caller names the file to upload.
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()

	// Owner-only from the first byte, and never over an existing file.
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- the service builds this path.
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func absoluteMinor(minor int64) int64 {
	if minor == -1<<63 {
		return 1<<63 - 1
	}
	if minor < 0 {
		return -minor
	}
	return minor
}

func containsAmount(amounts []money.Money, minor int64) bool {
	for _, amount := range amounts {
		if amount.Minor == minor {
			return true
		}
	}
	return false
}

func daysBetween(left, right civil.Date) int {
	leftTime := time.Date(left.Year(), left.Month(), left.Day(), 0, 0, 0, 0, time.UTC)
	rightTime := time.Date(right.Year(), right.Month(), right.Day(), 0, 0, 0, 0, time.UTC)
	return int(leftTime.Sub(rightTime) / (24 * time.Hour))
}

// insideDirectory reports whether file is inside directory, comparing path
// components rather than string prefixes so /a/docs2/x is not inside /a/docs.
func insideDirectory(directory, file string) bool {
	relative, err := filepath.Rel(directory, file)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
