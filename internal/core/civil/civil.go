// Package civil is a calendar date in the proleptic Gregorian calendar.
//
// A Date is a year, month and day, with no time zone or time of day. It is the
// date type the money and format packages use for display and parsing.
package civil

import (
	"errors"
	"fmt"
	"time"
)

// Date is a calendar date. The zero value means "no date".
type Date struct {
	year  int
	month time.Month
	day   int
}

// New returns the date, or false when the date is impossible or the year is
// outside 0..9999.
func New(year int, month time.Month, day int) (Date, bool) {
	if year < 0 || year > 9999 || month < time.January || month > time.December {
		return Date{}, false
	}
	if day < 1 || day > daysInMonth(year, month) {
		return Date{}, false
	}
	return Date{year: year, month: month, day: day}, true
}

// FromTime returns t's own calendar date in t's location.
func FromTime(t time.Time) Date {
	year, month, day := t.Date()
	return Date{year: year, month: month, day: day}
}

// ParseISO parses exactly "YYYY-MM-DD".
func ParseISO(s string) (Date, error) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return Date{}, errors.New("invalid ISO date")
	}
	year, ok := digits(s[0:4])
	if !ok {
		return Date{}, errors.New("invalid ISO date")
	}
	month, ok := digits(s[5:7])
	if !ok {
		return Date{}, errors.New("invalid ISO date")
	}
	day, ok := digits(s[8:10])
	if !ok {
		return Date{}, errors.New("invalid ISO date")
	}
	date, ok := New(year, time.Month(month), day)
	if !ok {
		return Date{}, errors.New("invalid ISO date")
	}
	return date, nil
}

func digits(text string) (int, bool) {
	value := 0
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return 0, false
		}
		value = value*10 + int(text[index]-'0')
	}
	return value, true
}

// Year returns the year.
func (d Date) Year() int { return d.year }

// Month returns the month.
func (d Date) Month() time.Month { return d.month }

// Day returns the day of the month.
func (d Date) Day() int { return d.day }

// String formats the date as "YYYY-MM-DD".
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.year, int(d.month), d.day)
}

// Compare returns -1, 0 or 1 when d is before, equal to or after other.
func (d Date) Compare(other Date) int {
	switch {
	case d.year < other.year:
		return -1
	case d.year > other.year:
		return 1
	case d.month < other.month:
		return -1
	case d.month > other.month:
		return 1
	case d.day < other.day:
		return -1
	case d.day > other.day:
		return 1
	default:
		return 0
	}
}

// Before reports whether d is before other.
func (d Date) Before(other Date) bool {
	return d.Compare(other) < 0
}

// After reports whether d is after other.
func (d Date) After(other Date) bool {
	return d.Compare(other) > 0
}

// AddDays returns the date n days after d, or before d when n is negative.
func (d Date) AddDays(n int) Date {
	if d.IsZero() {
		return d
	}
	t := time.Date(d.year, d.month, d.day+n, 0, 0, 0, 0, time.UTC)
	return FromTime(t)
}

// IsZero reports whether d is the zero value, "no date".
func (d Date) IsZero() bool {
	return d.year == 0 && d.month == 0 && d.day == 0
}

func daysInMonth(year int, month time.Month) int {
	switch month {
	case time.January, time.March, time.May, time.July, time.August, time.October, time.December:
		return 31
	case time.April, time.June, time.September, time.November:
		return 30
	case time.February:
		if isLeapYear(year) {
			return 29
		}
		return 28
	default:
		return 0
	}
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
