package civil

import (
	"testing"
	"time"
)

func TestNewAcceptsValidDatesAndRejectsImpossibleOnes(t *testing.T) {
	valid := []struct {
		year  int
		month time.Month
		day   int
	}{
		{2028, time.February, 29},
		{2000, time.February, 29},
		{2027, time.February, 28},
		{2026, time.November, 5},
		{9999, time.December, 31},
		{0, time.January, 1},
	}
	for _, test := range valid {
		if _, ok := New(test.year, test.month, test.day); !ok {
			t.Errorf("New(%d, %d, %d) = false, want true", test.year, test.month, test.day)
		}
	}

	invalid := []struct {
		year  int
		month time.Month
		day   int
	}{
		{2027, time.February, 29},
		{1900, time.February, 29},
		{2026, time.February, 31},
		{2026, time.April, 31},
		{2026, time.Month(13), 1},
		{2026, time.January, 0},
		{-1, time.January, 1},
		{10000, time.January, 1},
	}
	for _, test := range invalid {
		if _, ok := New(test.year, test.month, test.day); ok {
			t.Errorf("New(%d, %d, %d) = true, want false", test.year, test.month, test.day)
		}
	}
}

func TestFromTimeUsesTheTimeOwnCalendarDate(t *testing.T) {
	location := time.FixedZone("test", 2*60*60)
	moment := time.Date(2026, time.November, 5, 0, 30, 0, 0, location)
	if got := FromTime(moment).String(); got != "2026-11-05" {
		t.Errorf("FromTime = %q, want 2026-11-05", got)
	}
}

func TestParseISOIsStrict(t *testing.T) {
	date, err := ParseISO("2026-11-05")
	if err != nil {
		t.Fatalf("ParseISO: %v", err)
	}
	if date.Year() != 2026 || date.Month() != time.November || date.Day() != 5 {
		t.Errorf("ParseISO = %v", date)
	}

	for _, input := range []string{
		"", "2026-11-5", "2026-1-05", "05.11.2026", "2026/11/05",
		"2026-13-01", "2026-02-30", "abcd-ef-gh", "2026-11-05T00:00",
	} {
		if _, err := ParseISO(input); err == nil {
			t.Errorf("ParseISO(%q) succeeded, want an error", input)
		}
	}
}

func TestAccessorsAndString(t *testing.T) {
	date, _ := New(2026, time.November, 5)
	if date.Year() != 2026 || date.Month() != time.November || date.Day() != 5 {
		t.Errorf("accessors = %d, %d, %d", date.Year(), date.Month(), date.Day())
	}
	if got := date.String(); got != "2026-11-05" {
		t.Errorf("String = %q, want 2026-11-05", got)
	}
	early, _ := New(999, time.January, 2)
	if got := early.String(); got != "0999-01-02" {
		t.Errorf("String = %q, want 0999-01-02", got)
	}
}

func TestCompareBeforeAfter(t *testing.T) {
	early, _ := New(2026, time.November, 5)
	late, _ := New(2026, time.November, 6)
	equal, _ := New(2026, time.November, 5)

	if early.Compare(late) != -1 || late.Compare(early) != 1 || early.Compare(equal) != 0 {
		t.Error("Compare does not order dates")
	}
	if !early.Before(late) || early.Before(equal) || late.Before(early) {
		t.Error("Before is wrong")
	}
	if !late.After(early) || equal.After(early) || early.After(late) {
		t.Error("After is wrong")
	}
}

func TestAddDaysCrossesMonthsAndLeapDays(t *testing.T) {
	date, _ := New(2028, time.February, 28)
	if got := date.AddDays(1).String(); got != "2028-02-29" {
		t.Errorf("AddDays(1) = %q, want 2028-02-29", got)
	}
	if got := date.AddDays(2).String(); got != "2028-03-01" {
		t.Errorf("AddDays(2) = %q, want 2028-03-01", got)
	}
	if got := date.AddDays(-1).String(); got != "2028-02-27" {
		t.Errorf("AddDays(-1) = %q, want 2028-02-27", got)
	}
	first, _ := New(2026, time.January, 1)
	if got := first.AddDays(-1).String(); got != "2025-12-31" {
		t.Errorf("AddDays(-1) = %q, want 2025-12-31", got)
	}
}

func TestZeroValueMeansNoDate(t *testing.T) {
	var date Date
	if !date.IsZero() {
		t.Error("zero Date is not IsZero")
	}
	other, _ := New(0, time.January, 1)
	if other.IsZero() {
		t.Error("0000-01-01 is IsZero")
	}
}
