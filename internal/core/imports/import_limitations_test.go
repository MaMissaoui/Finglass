package imports

import (
	"strings"
	"testing"
)

func limitationToday(t *testing.T) civilDate {
	t.Helper()
	return testDate(t, 2026, 11, 1)
}

func TestTwoDigitYearsResolveTo19xxWhen20xxWouldBeMoreThanAYearAhead(t *testing.T) {
	today := limitationToday(t)
	tests := []struct {
		yy   uint8
		date civilDate
		want int
	}{
		{99, today, 1999},
		{28, today, 1928},
		{27, today, 2027},
		{26, today, 2026},
		{0, today, 2000},
		{99, testDate(t, 2099, 6, 1), 2099},
		{0, testDate(t, 2099, 6, 1), 2000},
	}
	for _, test := range tests {
		got, err := ResolveTwoDigitYear(test.yy, test.date)
		if err != nil {
			t.Fatalf("ResolveTwoDigitYear(%d): %v", test.yy, err)
		}
		if got != test.want {
			t.Errorf("ResolveTwoDigitYear(%d) = %d, want %d", test.yy, got, test.want)
		}
	}
}

func TestTwoDigitYearRejectsOutOfRangeInput(t *testing.T) {
	today := limitationToday(t)
	if _, err := ResolveTwoDigitYear(100, today); err == nil {
		t.Error("ResolveTwoDigitYear(100) succeeded")
	}
	if _, err := ResolveTwoDigitYear(255, today); err == nil {
		t.Error("ResolveTwoDigitYear(255) succeeded")
	}
}

func TestMT940ValueDateWithAYearBefore1928ResolvesTo19xx(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C991231EUR0,00\n:61:991231D45,67N066NONREF\n:86:?20Old booking\n:62F:C991231EUR0,00"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if want := testDate(t, 1999, 12, 31); statement.Bookings[0].Date != want {
		t.Errorf("date = %s, want %s", statement.Bookings[0].Date, want)
	}
	if statement.Bookings[0].ValueDate == nil || *statement.Bookings[0].ValueDate != testDate(t, 1999, 12, 31) {
		t.Errorf("value date = %v", statement.Bookings[0].ValueDate)
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s, want %s", got, want)
	}
}

func TestMT940ValueDateWithARecentTwoDigitYearStays20xx(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C260115EUR0,00\n:61:260115D45,67N066NONREF\n:86:?20Recent booking\n:62F:C260115EUR0,00"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := testDate(t, 2026, 1, 15); statement.Bookings[0].Date != want {
		t.Errorf("date = %s, want %s", statement.Bookings[0].Date, want)
	}
	if statement.Bookings[0].ValueDate == nil || *statement.Bookings[0].ValueDate != testDate(t, 2026, 1, 15) {
		t.Errorf("value date = %v", statement.Bookings[0].ValueDate)
	}
}

func TestQIFTwoDigitYearsFollowTheSameRule(t *testing.T) {
	text := "!Type:Bank\nD12/31/99\nT-10.00\nPOld\n^\nD01/15/26\nT-20.00\nPRecent\n^"
	statement, err := ParseQIFWithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []civilDate{testDate(t, 1999, 12, 31), testDate(t, 2026, 1, 15)}
	for index, booking := range statement.Bookings {
		if booking.Date != want[index] {
			t.Errorf("date[%d] = %s, want %s", index, booking.Date, want[index])
		}
	}
}

func TestCSVDatesAreParsedStrictly(t *testing.T) {
	today := limitationToday(t)
	tests := []struct {
		input string
		want  *civilDate
	}{
		{"31.02.2026", nil},
		{"29.02.2028", dateValue(testDate(t, 2028, 2, 29))},
		{"29.02.2027", nil},
		{"31.12.99", dateValue(testDate(t, 1999, 12, 31))},
		{"15.01.26", dateValue(testDate(t, 2026, 1, 15))},
		{"2026-02-30", nil},
		{"15.01.2026", dateValue(testDate(t, 2026, 1, 15))},
	}
	for _, test := range tests {
		got := ParseDateWithToday(stringPointer(test.input), today)
		if !sameDatePointer(got, test.want) {
			t.Errorf("ParseDateWithToday(%q) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestCSVDatesWithATimeAndAOneDigitDayOrMonthAreParsed(t *testing.T) {
	today := limitationToday(t)
	tests := []struct {
		input string
		want  *civilDate
	}{
		{"1.10.2026 12:00", dateValue(testDate(t, 2026, 10, 1))},
		{"01.10.2026 12:00", dateValue(testDate(t, 2026, 10, 1))},
		{"2026-10-01T12:00:00", dateValue(testDate(t, 2026, 10, 1))},
		{"1.10.2026", dateValue(testDate(t, 2026, 10, 1))},
		{"31.02.2026 12:00", nil},
	}
	for _, test := range tests {
		got := ParseDateWithToday(stringPointer(test.input), today)
		if !sameDatePointer(got, test.want) {
			t.Errorf("ParseDateWithToday(%q) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestCSVTwoDigitYearsFollowTwoDigitYearAtParseTime(t *testing.T) {
	today := limitationToday(t)
	tests := []struct {
		input string
		want  *civilDate
	}{
		{"31.12.99", dateValue(testDate(t, 1999, 12, 31))},
		{"15.01.26", dateValue(testDate(t, 2026, 1, 15))},
		{"29.02.28", dateValue(testDate(t, 1928, 2, 29))},
		{"29.02.27", nil},
		{"31.02.26", nil},
	}
	for _, test := range tests {
		got := ParseDateWithToday(stringPointer(test.input), today)
		if !sameDatePointer(got, test.want) {
			t.Errorf("ParseDateWithToday(%q) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestCSVRowWithAnImpossibleDateIsSkippedOnItsOwnLine(t *testing.T) {
	text := "Buchungstag;Betrag;Verwendungszweck\n03.10.2026;-45,67;ok\n31.02.2026;-10,00;impossible\n04.10.2026;-5,00;also ok\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount 0 = %s", got)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "-5.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
	if len(statement.Skipped) != 1 {
		t.Fatalf("skipped = %v", statement.Skipped)
	}
	if !strings.HasPrefix(statement.Skipped[0], "Line 3") || !strings.Contains(statement.Skipped[0], "no valid date") {
		t.Errorf("skipped[0] = %q", statement.Skipped[0])
	}
}

func TestTabFileWhoseFieldsContainCommasIsNotReadAsCommaSeparated(t *testing.T) {
	text := "Buchungstag\tBetrag, EUR\tVerwendungszweck\n03.10.2026\t-45,67\tMiete, Oktober\n04.10.2026\t-10,00\tBrot, Butter\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv == nil || statement.Csv.Delimiter != '\t' {
		t.Fatalf("delimiter = %v", statement.Csv)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount 0 = %s", got)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "-10.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
	purposes := []string{}
	for _, booking := range statement.Bookings {
		if booking.Purpose != nil {
			purposes = append(purposes, *booking.Purpose)
		}
	}
	if len(purposes) != 2 || purposes[0] != "Miete, Oktober" || purposes[1] != "Brot, Butter" {
		t.Errorf("purposes = %q", purposes)
	}
}

func TestATabAndCommaTieGoesToTheDelimiterWithARecognisableHeader(t *testing.T) {
	text := "Buchungstag\tBetrag, EUR\n03.10.2026\t-45,67\n04.10.2026\t-10,00\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.Delimiter != '\t' {
		t.Errorf("delimiter = %q", statement.Csv.Delimiter)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount 0 = %s", got)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "-10.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
}

func TestQuotedSemicolonsOfVaryingNumberDoNotHandTheFileToTheComma(t *testing.T) {
	text := "Datum;Betrag;Text\n03.10.2026;-45,67;\"a;b\"\n04.10.2026;-10,00;\"a;b;c\"\n05.10.2026;-1,50;plain\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.Delimiter != ';' {
		t.Errorf("delimiter = %q", statement.Csv.Delimiter)
	}
	purposes := []string{}
	for _, booking := range statement.Bookings {
		if booking.Purpose != nil {
			purposes = append(purposes, *booking.Purpose)
		}
	}
	want := []string{"a;b", "a;b;c", "plain"}
	if len(purposes) != len(want) {
		t.Fatalf("purposes = %q", purposes)
	}
	for index := range want {
		if purposes[index] != want[index] {
			t.Errorf("purpose[%d] = %q, want %q", index, purposes[index], want[index])
		}
	}
}

func TestSemicolonFileWithQuotedCommasAndSemicolonsKeepsItsDelimiter(t *testing.T) {
	text := "\"Datum\";\"Betrag\";\"Text\"\n\"03.10.2026\";\"-45,67\";\"a;b, c\"\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.Delimiter != ';' {
		t.Fatalf("delimiter = %q", statement.Csv.Delimiter)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].Purpose == nil || *statement.Bookings[0].Purpose != "a;b, c" {
		t.Errorf("purpose = %v", statement.Bookings[0].Purpose)
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
}

func TestCommaFileWithAQuotedMultiLinePurposeImportsTheRightFields(t *testing.T) {
	text := "Date,Description,Payee,Debit,Credit,Balance\r\n2026-09-30,\"Card payment\nLondon\",Pret A Manger,4.50,,100.00\r\n2026-09-29,Salary,ACME Ltd,,\"1,500.00\",104.50\r\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.Delimiter != ',' {
		t.Fatalf("delimiter = %q", statement.Csv.Delimiter)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-4.50"); got.Cmp(want) != 0 {
		t.Errorf("amount 0 = %s", got)
	}
	if statement.Bookings[0].PartnerName == nil || *statement.Bookings[0].PartnerName != "Pret A Manger" {
		t.Errorf("partner 0 = %v", statement.Bookings[0].PartnerName)
	}
	if statement.Bookings[0].Purpose == nil || *statement.Bookings[0].Purpose != "Card payment London" {
		t.Errorf("purpose 0 = %v", statement.Bookings[0].Purpose)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "1500.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
	if statement.Bookings[1].PartnerName == nil || *statement.Bookings[1].PartnerName != "ACME Ltd" {
		t.Errorf("partner 1 = %v", statement.Bookings[1].PartnerName)
	}
}

func TestCSVSplittingStopsAfterTheRequestedNonBlankRecordsAndKeepsBlankOnesBefore(t *testing.T) {
	records := SplitRecords("a;b\n\nc;d\ne;f\n", ';', 2)
	if len(records) != 3 {
		t.Fatalf("records = %d", len(records))
	}
	if len(records[1]) != 1 || records[1][0] != "" {
		t.Errorf("records[1] = %q", records[1])
	}
	nonBlank := [][]string{}
	for _, row := range records {
		if !rowAllBlank(row) {
			nonBlank = append(nonBlank, row)
		}
	}
	if len(nonBlank) != 2 || nonBlank[0][0] != "a" || nonBlank[0][1] != "b" || nonBlank[1][0] != "c" || nonBlank[1][1] != "d" {
		t.Errorf("non-blank = %q", nonBlank)
	}
}

func TestAFileOf5000BookingRowsStillImportsAllOfThem(t *testing.T) {
	var text strings.Builder
	text.WriteString("Buchungstag;Betrag;Verwendungszweck\n")
	for index := 0; index < 5000; index++ {
		text.WriteString("03.10.2026;-1,00;Row ")
		text.WriteString(intToString(index))
		text.WriteByte('\n')
	}
	statement, err := ParseCSV([]byte(text.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 5000 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
}
