// One rule for two-digit years in every import format (CSV, MT940, QIF).
// Ports crates/finglass-core/src/import/two_digit_year.rs.
package imports

import (
	"fmt"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
)

// ResolveTwoDigitYear resolves a two-digit year yy (0..=99) to a full year:
// 2000+yy unless that is later than today's year + 1, in which case it is
// 1900+yy.
func ResolveTwoDigitYear(yy uint8, today civil.Date) (int, error) {
	if yy > 99 {
		return 0, Invalid(fmt.Sprintf("two-digit year out of range: %d", yy))
	}
	year := 2000 + int(yy)
	if year > today.Year()+1 {
		return 1900 + int(yy), nil
	}
	return year, nil
}
