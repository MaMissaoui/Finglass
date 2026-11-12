// Package documents is the document repository.
//
// Documents and their OCR texts live in separate tables. Deleting a document
// row removes its OCR texts through the foreign key, but **never its file on
// disk**: removing the file is the document service's job. It ports
// `crates/finglass-core/src/repo/documents.rs`.
package documents

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
)

// nowUTC is overridable in tests.
var nowUTC = func() time.Time { return time.Now().UTC() }

const columns = "id, file_name, stored_path, mime_type, uploaded_at, transaction_id"

const itemSelect = "SELECT d.id, d.file_name, d.stored_path, d.mime_type, d.uploaded_at, d.transaction_id,\n" +
	"            t.partner_name, t.purpose, t.booking_date,\n" +
	"            EXISTS (SELECT 1 FROM ocr_texts o WHERE o.document_id = d.id),\n" +
	"            (SELECT o.confidence FROM ocr_texts o WHERE o.document_id = d.id ORDER BY o.id LIMIT 1)\n" +
	"     FROM documents d\n" +
	"     LEFT JOIN transactions t ON t.id = d.transaction_id"

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// queryer is satisfied by *sql.Conn and *sql.Tx.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func fromRow(row scanner) (model.Document, error) {
	var (
		id            int64
		fileName      string
		storedPath    string
		mimeType      string
		uploadedAt    string
		transactionID *int64
	)
	if err := row.Scan(&id, &fileName, &storedPath, &mimeType, &uploadedAt, &transactionID); err != nil {
		return model.Document{}, err
	}
	parsedUploadedAt, err := repo.ParseTimestamp(uploadedAt)
	if err != nil {
		return model.Document{}, err
	}

	document := model.Document{
		ID:         id,
		FileName:   fileName,
		StoredPath: storedPath,
		MIMEType:   mimeType,
		UploadedAt: parsedUploadedAt,
	}
	if transactionID != nil {
		converted := model.TransactionID(*transactionID)
		document.TransactionID = &converted
	}
	return document, nil
}

func readDocument(ctx context.Context, conn queryer, id int64) (model.Document, bool, error) {
	row := conn.QueryRowContext(ctx, "SELECT "+columns+" FROM documents WHERE id = ?1", id)
	document, err := fromRow(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return model.Document{}, false, nil
	case err != nil:
		return model.Document{}, false, err
	default:
		return document, true, nil
	}
}

// escapeLike escapes SQL `LIKE` wildcards so user text is matched literally.
func escapeLike(text string) string {
	var escaped strings.Builder
	escaped.Grow(len(text))
	for _, character := range text {
		switch character {
		case '\\', '%', '_':
			escaped.WriteByte('\\')
			escaped.WriteRune(character)
		default:
			escaped.WriteRune(character)
		}
	}
	return escaped.String()
}

func itemFromRow(row scanner) (model.DocumentItem, error) {
	var (
		id            int64
		fileName      string
		storedPath    string
		mimeType      string
		uploadedAt    string
		transactionID *int64
		partnerName   *string
		purpose       *string
		bookingDate   *string
		hasOCRValue   int64
		ocrConfidence *float64
	)
	if err := row.Scan(
		&id,
		&fileName,
		&storedPath,
		&mimeType,
		&uploadedAt,
		&transactionID,
		&partnerName,
		&purpose,
		&bookingDate,
		&hasOCRValue,
		&ocrConfidence,
	); err != nil {
		return model.DocumentItem{}, err
	}

	parsedUploadedAt, err := repo.ParseTimestamp(uploadedAt)
	if err != nil {
		return model.DocumentItem{}, err
	}

	var transactionIDValue *model.TransactionID
	if transactionID != nil {
		converted := model.TransactionID(*transactionID)
		transactionIDValue = &converted
	}
	var transactionLabel *string
	if transactionIDValue != nil && bookingDate != nil {
		title := "Transaction"
		if partnerName != nil {
			title = *partnerName
		} else if purpose != nil {
			title = *purpose
		}
		label := title + " (" + *bookingDate + ")"
		transactionLabel = &label
	}

	return model.DocumentItem{
		ID:               id,
		FileName:         fileName,
		StoredPath:       storedPath,
		MIMEType:         mimeType,
		UploadedAt:       parsedUploadedAt,
		TransactionID:    transactionIDValue,
		TransactionLabel: transactionLabel,
		HasOCR:           hasOCRValue != 0,
		OCRConfidence:    ocrConfidence,
	}, nil
}

func queryItems(
	ctx context.Context,
	conn *sql.Conn,
	query *string,
	unlinkedOnly bool,
	transactionID *model.TransactionID,
	onlyID *int64,
) ([]model.DocumentItem, error) {
	sqlText := itemSelect
	sqlText += " WHERE 1 = 1"
	var values []any

	if query != nil {
		text := strings.TrimSpace(*query)
		if text != "" {
			pattern := "%" + escapeLike(text) + "%"
			sqlText += " AND (d.file_name LIKE ? ESCAPE '\\'\n" +
				"                   OR EXISTS (SELECT 1 FROM ocr_texts o WHERE o.document_id = d.id\n" +
				"                              AND o.content LIKE ? ESCAPE '\\'))"
			values = append(values, pattern, pattern)
		}
	}
	if unlinkedOnly {
		sqlText += " AND d.transaction_id IS NULL"
	}
	if transactionID != nil {
		sqlText += " AND d.transaction_id = ?"
		values = append(values, int64(*transactionID))
	}
	if onlyID != nil {
		sqlText += " AND d.id = ?"
		values = append(values, *onlyID)
	}

	sqlText += " ORDER BY d.uploaded_at DESC, d.id DESC"

	rows, err := conn.QueryContext(ctx, sqlText, values...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []model.DocumentItem
	for rows.Next() {
		item, err := itemFromRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// List lists every document.
func List(ctx context.Context, d *db.Database) ([]model.Document, error) {
	var documents []model.Document

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT "+columns+" FROM documents ORDER BY id")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			document, err := fromRow(rows)
			if err != nil {
				return err
			}
			documents = append(documents, document)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return documents, nil
}

// Get reads one document. ok is false when the document does not exist.
func Get(ctx context.Context, d *db.Database, id int64) (document model.Document, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		loaded, found, readErr := readDocument(ctx, conn, id)
		if readErr != nil {
			return readErr
		}
		document = loaded
		ok = found
		return nil
	})
	if err != nil {
		return model.Document{}, false, err
	}
	return document, ok, nil
}

// Create creates a document row and stamps it with the current time.
//
// The file itself is copied by the document service; this only stores the row.
func Create(ctx context.Context, d *db.Database, new model.NewDocument) (model.Document, error) {
	var created model.Document

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		uploadedAt := nowUTC()
		uploadedAtText, err := repo.FormatTimestamp(uploadedAt)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(
			ctx,
			"INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)\n"+
				"             VALUES (?1, ?2, ?3, ?4, ?5)",
			new.FileName,
			new.StoredPath,
			new.MIMEType,
			uploadedAtText,
			optionalTransactionID(new.TransactionID),
		)
		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "document.create", audit.ActorUser, repo.IDAndName(id, new.FileName),
		); err != nil {
			return err
		}

		created = model.Document{
			ID:            id,
			FileName:      new.FileName,
			StoredPath:    new.StoredPath,
			MIMEType:      new.MIMEType,
			UploadedAt:    uploadedAt,
			TransactionID: new.TransactionID,
		}
		return nil
	})
	if err != nil {
		return model.Document{}, err
	}
	return created, nil
}

// LinkToTransaction links a document to a transaction.
func LinkToTransaction(
	ctx context.Context,
	d *db.Database,
	id int64,
	transactionID model.TransactionID,
) (*model.Document, error) {
	var document *model.Document

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE documents SET transaction_id = ?1 WHERE id = ?2",
			int64(transactionID), id,
		); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "document.link_to_transaction", audit.ActorUser, repo.IDOnly(id),
		); err != nil {
			return err
		}

		loaded, found, err := readDocument(ctx, tx, id)
		if err != nil {
			return err
		}
		if found {
			document = &loaded
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return document, nil
}

// UnlinkFromTransaction removes a document's transaction link.
func UnlinkFromTransaction(ctx context.Context, d *db.Database, id int64) (*model.Document, error) {
	var document *model.Document

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE documents SET transaction_id = NULL WHERE id = ?1",
			id,
		); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "document.unlink_from_transaction", audit.ActorUser, repo.IDOnly(id),
		); err != nil {
			return err
		}

		loaded, found, err := readDocument(ctx, tx, id)
		if err != nil {
			return err
		}
		if found {
			document = &loaded
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return document, nil
}

// Delete deletes a document row and its OCR texts.
//
// The file on disk is left alone; deleting it is the document service's job.
func Delete(ctx context.Context, d *db.Database, id int64) (bool, error) {
	deleted := false

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM documents WHERE id = ?1", id)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "document.delete", audit.ActorUser, repo.IDOnly(id),
		); err != nil {
			return err
		}
		deleted = changed > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// FindItems returns newest-first snapshots. query matches the file name or the
// OCR text.
func FindItems(
	ctx context.Context,
	d *db.Database,
	query *string,
	unlinkedOnly bool,
	transactionID *model.TransactionID,
) ([]model.DocumentItem, error) {
	var items []model.DocumentItem

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		loaded, queryErr := queryItems(ctx, conn, query, unlinkedOnly, transactionID, nil)
		if queryErr != nil {
			return queryErr
		}
		items = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// SaveOCRText stores (or replaces) the OCR text for a document and returns the
// new OCR row id.
func SaveOCRText(
	ctx context.Context,
	d *db.Database,
	documentID int64,
	content string,
	language string,
	confidence *float64,
) (int64, error) {
	var id int64

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		var exists int64
		err := tx.QueryRowContext(
			ctx, "SELECT 1 FROM documents WHERE id = ?1", documentID,
		).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return repo.ErrNotFound
		}
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(
			ctx, "DELETE FROM ocr_texts WHERE document_id = ?1", documentID,
		); err != nil {
			return err
		}
		result, err := tx.ExecContext(
			ctx,
			"INSERT INTO ocr_texts (document_id, content, language, confidence) VALUES (?1, ?2, ?3, ?4)",
			documentID, content, language, optionalFloat(confidence),
		)
		if err != nil {
			return err
		}
		inserted, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id = inserted

		return audit.LogConn(
			ctx, tx, "document.save_ocr_text", audit.ActorUser, repo.IDOnly(documentID),
		)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// GetOCRText returns the document's OCR text, when one is stored.
func GetOCRText(ctx context.Context, d *db.Database, documentID int64) (content string, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		scanErr := conn.QueryRowContext(
			ctx,
			"SELECT content FROM ocr_texts WHERE document_id = ?1 ORDER BY id LIMIT 1",
			documentID,
		).Scan(&content)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			return nil
		case scanErr != nil:
			return scanErr
		default:
			ok = true
			return nil
		}
	})
	if err != nil {
		return "", false, err
	}
	return content, ok, nil
}

// GetItem reads one document snapshot. ok is false when the document does not
// exist.
func GetItem(ctx context.Context, d *db.Database, id int64) (item model.DocumentItem, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		items, err := queryItems(ctx, conn, nil, false, nil, &id)
		if err != nil {
			return err
		}
		if len(items) > 0 {
			item = items[0]
			ok = true
		}
		return nil
	})
	if err != nil {
		return model.DocumentItem{}, false, err
	}
	return item, ok, nil
}

func optionalTransactionID(id *model.TransactionID) any {
	if id == nil {
		return nil
	}
	return int64(*id)
}

func optionalFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
