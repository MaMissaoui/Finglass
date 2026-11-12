// Reading and parsing a statement file, and converting imported decimals to
// money. Ports the non-service parts of crates/finglass-core/src/import/mod.rs.
package imports

import (
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// MaxFileBytes is the largest statement file Finglass reads.
const MaxFileBytes uint64 = 100 * 1024 * 1024

// MAX_FILE_BYTES is the Rust spelling of MaxFileBytes, kept for callers that
// mirror the crate's constant.
const MAX_FILE_BYTES = MaxFileBytes

var lowerText = cases.Lower(language.Und)

// Read reads and parses a file. csvMapping overrides the detected CSV column
// mapping.
func Read(fileName string, data []byte, csvMapping *CsvMapping, today civil.Date) (ParsedStatement, error) {
	if uint64(len(data)) > MaxFileBytes {
		return ParsedStatement{}, ErrFileTooLarge
	}
	extension := lowerText.String(fileExtension(fileName))
	var (
		statement ParsedStatement
		err       error
	)
	switch extension {
	case "xml", "zip":
		statement, err = ParseCAMT(data)
	case "ofx", "qfx":
		statement, err = ParseOFX(data)
	case "qif":
		statement, err = ParseQIFWithToday(data, today)
	case "sta", "mt940":
		statement, err = ParseMT940WithToday(data, today)
	case "xlsx":
		rows, rowsErr := FirstSheetRows(data)
		if rowsErr != nil {
			return ParsedStatement{}, rowsErr
		}
		statement = ParseCSVRows(rows, csvMapping, FormatXLSX, "XLSX", ';', today)
	case "csv":
		statement, err = ParseCSVWithToday(data, csvMapping, today)
	default:
		switch {
		case LooksLikeMT940(data):
			statement, err = ParseMT940WithToday(data, today)
		case LooksLikeCAMT(data):
			statement, err = ParseCAMT(data)
		case LooksLikeOFX(data):
			statement, err = ParseOFX(data)
		case LooksLikeQIF(data):
			statement, err = ParseQIFWithToday(data, today)
		default:
			statement, err = ParseCSVWithToday(data, csvMapping, today)
		}
	}
	if err != nil {
		return ParsedStatement{}, err
	}
	if err := requireSingleAccount(statement); err != nil {
		return ParsedStatement{}, err
	}
	return statement, nil
}

// fileExtension mirrors Rust's Path::extension for Unix paths.
func fileExtension(fileName string) string {
	base := fileName
	if index := strings.LastIndexByte(base, '/'); index >= 0 {
		base = base[index+1:]
	}
	index := strings.LastIndexByte(base, '.')
	if index < 0 {
		return ""
	}
	if index == 0 && strings.Count(base, ".") == 1 {
		return ""
	}
	return base[index+1:]
}

// AmountToMoney converts an imported decimal amount to the currency's minor
// units. Amounts with more decimal places than the currency allows are
// rejected, never rounded.
//
// Exported for the golden and later import-service specs.
func AmountToMoney(amount decimal.Decimal, currency money.Currency) (money.Money, error) {
	normalized := amount.Normalize()
	scale := normalized.Scale()
	exponent := currency.MinorExponent()

	if scale > 0 && int64(scale) > int64(exponent) {
		return money.Money{}, money.InvalidError{Msg: fmt.Sprintf("too many decimal places for %s", currency.String())}
	}

	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)-int64(scale)), nil)
	minor := new(big.Int).Mul(normalized.Mantissa(), factor)
	if !minor.IsInt64() {
		return money.Money{}, money.ErrOverflow
	}
	return money.Money{Minor: minor.Int64(), Currency: currency}, nil
}

func requireSingleAccount(statement ParsedStatement) error {
	if len(statement.Accounts) <= 1 {
		return nil
	}
	accounts := make([]string, 0, len(statement.Accounts))
	for _, account := range statement.Accounts {
		characters := []rune(account)
		tail := characters
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		accounts = append(accounts, "…"+string(tail))
	}
	return Invalid(fmt.Sprintf(
		"This file holds bookings for %d accounts (%s). Export one file per account and import them one at a time.",
		len(statement.Accounts),
		strings.Join(accounts, ", "),
	))
}
