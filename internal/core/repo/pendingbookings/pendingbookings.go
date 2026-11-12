// Package pendingbookings is the pending booking repository.
//
// The bank's pending lines are fetched apart from the real bookings. Nothing of
// the booking pipeline reads them. It ports
// `crates/finglass-core/src/repo/pending_bookings.rs`.
package pendingbookings

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
)

const columns = "id, account_id, booking_date, value_date, amount_minor, currency, partner_name, purpose, " +
	"fetched_at"

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func fromRow(row scanner) (model.PendingBookingItem, error) {
	var (
		id          int64
		accountID   int64
		bookingDate string
		valueDate   *string
		amountMinor int64
		currency    string
		partnerName *string
		purpose     *string
		fetchedAt   string
	)
	if err := row.Scan(
		&id,
		&accountID,
		&bookingDate,
		&valueDate,
		&amountMinor,
		&currency,
		&partnerName,
		&purpose,
		&fetchedAt,
	); err != nil {
		return model.PendingBookingItem{}, err
	}
	_ = id

	parsedCurrency, err := repo.ParseCurrency(currency)
	if err != nil {
		return model.PendingBookingItem{}, err
	}
	parsedDate, err := repo.ParseDate(bookingDate)
	if err != nil {
		return model.PendingBookingItem{}, err
	}
	var parsedValueDate *civil.Date
	if valueDate != nil {
		parsed, err := repo.ParseDate(*valueDate)
		if err != nil {
			return model.PendingBookingItem{}, err
		}
		parsedValueDate = &parsed
	}
	parsedFetchedAt, err := repo.ParseTimestamp(fetchedAt)
	if err != nil {
		return model.PendingBookingItem{}, err
	}

	return model.PendingBookingItem{
		AccountID:   model.AccountID(accountID),
		Date:        parsedDate,
		ValueDate:   parsedValueDate,
		Amount:      money.Money{Minor: amountMinor, Currency: parsedCurrency},
		PartnerName: partnerName,
		Purpose:     purpose,
		FetchedAt:   parsedFetchedAt,
	}, nil
}

// ReplaceForAccount replaces the account's stored pending rows with items,
// stamped with fetchedAt, in one transaction.
//
// Call it with an empty list when the bank reported none pending; a failed
// snapshot never calls this, so what is stored stays.
func ReplaceForAccount(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	items []model.NewPendingBooking,
	fetchedAt time.Time,
) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM pending_bookings WHERE account_id = ?1",
			int64(accountID),
		); err != nil {
			return err
		}

		fetchedAtText, err := repo.FormatTimestamp(fetchedAt)
		if err != nil {
			return err
		}
		for _, item := range items {
			date, err := repo.FormatDate(item.Date)
			if err != nil {
				return err
			}
			valueDate, err := optionalDate(item.ValueDate)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(
				ctx,
				"INSERT INTO pending_bookings (\n"+
					"                    account_id, booking_date, value_date, amount_minor, currency, partner_name, purpose, fetched_at\n"+
					"                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
				int64(accountID),
				date,
				valueDate,
				item.Amount.Minor,
				item.Amount.Currency.String(),
				optionalString(item.PartnerName),
				optionalString(item.Purpose),
				fetchedAtText,
			); err != nil {
				return err
			}
		}

		return audit.LogConn(
			ctx, tx, "pending_booking.replace", audit.ActorUser,
			fmt.Sprintf("id=%d count=%d", int64(accountID), len(items)),
		)
	})
}

// FindItems returns the account's stored pending lines, newest first.
func FindItems(ctx context.Context, d *db.Database, accountID model.AccountID) ([]model.PendingBookingItem, error) {
	var items []model.PendingBookingItem

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT "+columns+" FROM pending_bookings\n"+
				"             WHERE account_id = ?1\n"+
				"             ORDER BY booking_date DESC, id DESC",
			int64(accountID),
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			item, err := fromRow(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// DeleteForAccount removes the account's stored pending lines.
func DeleteForAccount(ctx context.Context, d *db.Database, accountID model.AccountID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM pending_bookings WHERE account_id = ?1",
			int64(accountID),
		); err != nil {
			return err
		}
		return audit.LogConn(
			ctx, tx, "pending_booking.delete", audit.ActorUser, fmt.Sprintf("id=%d", int64(accountID)),
		)
	})
}

func optionalDate(date *civil.Date) (any, error) {
	if date == nil {
		return nil, nil
	}
	return repo.FormatDate(*date)
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
