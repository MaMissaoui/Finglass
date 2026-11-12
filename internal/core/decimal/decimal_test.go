package decimal

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, text string) Decimal {
	t.Helper()
	value, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return value
}

func TestParseMatchesRustDecimal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"-0", "0"},
		{"-0.00", "0.00"},
		{"+5", "5"},
		{".5", "0.5"},
		{"5.", "5"},
		{"-.5", "-0.5"},
		{"1_000", "1000"},
		{"1__0.5", "10.5"},
		{"1._5", "1.5"},
		{"1_", "1"},
		{"1.50", "1.50"},
		{strings.Repeat("0", 42) + "1.5", "1.5"},
		{"1e5", "100000"},
		{"1E+2", "100"},
		{"1e-5", "0.00001"},
		{"1.5e2", "150"},
		{"1.25e1", "12.5"},
		{"1.0e2", "100"},
		{"100e-2", "1.00"},
		{"12.3456789e3", "12345.6789"},
		{".5e1", "5"},
		{"5.e1", "50"},
		{"1_0e2", "1000"},
		{"0e5", "0"},
	}
	for _, test := range tests {
		value, err := Parse(test.input)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", test.input, err)
			continue
		}
		if got := value.String(); got != test.want {
			t.Errorf("Parse(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestParseRejectsInvalidInput(t *testing.T) {
	inputs := []string{
		"_1", "-", ".", "+", "1.2.3", "e5", "1e", "1ee2", "1e5.5", "1e1_0",
		"1e29", " 1", "1 ",
		"79228162514264337593543950336",
		strings.Repeat("9", 29),
		strings.Repeat("1", 30),
		"99999999999999999999999999999.5",
		"1e-29", "5e-29", "-9.9e28", "9.9e28",
	}
	for _, input := range inputs {
		if value, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) = %q, want an error", input, value.String())
		}
	}
}

func TestParseRoundsExcessFractionDigits(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0." + strings.Repeat("0", 27) + "1", "0." + strings.Repeat("0", 27) + "1"},
		{"0." + strings.Repeat("0", 28) + "1", "0." + strings.Repeat("0", 28)},
		{"0." + strings.Repeat("0", 29) + "5", "0." + strings.Repeat("0", 28)},
		{"0." + strings.Repeat("1", 29), "0." + strings.Repeat("1", 28)},
		{"1." + strings.Repeat("5", 30), "1." + strings.Repeat("5", 27) + "6"},
		{"-0." + strings.Repeat("0", 31) + "1", "0." + strings.Repeat("0", 28)},
		{"0.5e-28", "0." + strings.Repeat("0", 27) + "1"},
		{"1e-28", "0." + strings.Repeat("0", 27) + "1"},
	}
	for _, test := range tests {
		value, err := Parse(test.input)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", test.input, err)
			continue
		}
		if got := value.String(); got != test.want {
			t.Errorf("Parse(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestParseMatchesMoreRustDecimalEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"11111_11111_11111.11111_11111_11111", "111111111111111.11111111111111"},
		{"11111_11111_11111.11111_11111_11115", "111111111111111.11111111111112"},
		{"11111_11111_11111.11111_11111_11195", "111111111111111.11111111111120"},
		{"99999_99999_99999.99999_99999_99995", "1000000000000000.0000000000000"},
		{"99999_99999_99999_99999_99999_999.99", "10000000000000000000000000000"},
		{"79_228_162_514_264_337_593_543_950_33.56", "7922816251426433759354395034"},
		{"7.92281625142643375935439503356", "7.922816251426433759354395034"},
		{"7.9_228_162_514_264_337_593_543_950_335_6", "7.922816251426433759354395034"},
		{"0.00000_00000_00000_00000_00000_00100", "0.0000000000000000000000000001"},
		{"1.1111111111111111111111111111____", "1.1111111111111111111111111111"},
		{"1e28", "10000000000000000000000000000"},
		{"1.5e28", "15000000000000000000000000000"},
		{"-1.5e28", "-15000000000000000000000000000"},
	}
	for _, test := range tests {
		value, err := Parse(test.input)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", test.input, err)
			continue
		}
		if got := value.String(); got != test.want {
			t.Errorf("Parse(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestParseScaleMatchesRust(t *testing.T) {
	tests := []struct {
		input string
		scale int32
	}{
		{"1e-5", 5},
		{"1.50", 2},
		{"-0.00", 2},
		{"5.", 0},
		{"1e-28", 28},
	}
	for _, test := range tests {
		value := mustParse(t, test.input)
		if value.Scale() != test.scale {
			t.Errorf("Parse(%q).Scale() = %d, want %d", test.input, value.Scale(), test.scale)
		}
	}
}

func TestZeroSignMatchesRust(t *testing.T) {
	zero := mustParse(t, "0")
	zeroPointZero := mustParse(t, "0.00")
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Neg(0)", NegValue(zero).String(), "-0"},
		{"Neg(0.00)", NegValue(zeroPointZero).String(), "-0.00"},
		{"Trunc(-0.5)", mustParse(t, "-0.5").Trunc().String(), "-0"},
		{"RoundDP(-0.004, 2)", mustParse(t, "-0.004").RoundDP(2).String(), "0.00"},
		{"RoundDP(Neg(0.000), 2)", NegValue(mustParse(t, "0.000")).RoundDP(2).String(), "-0.00"},
		{"Rescale(Neg(0), 2)", NegValue(zero).Rescale(2).String(), "-0.00"},
		{"Sub(-1.5, -1.5)", mustParse(t, "-1.5").Sub(mustParse(t, "-1.5")).String(), "0.0"},
		{"Mul(-1, 0)", FromInt(-1).Mul(Zero).String(), "0"},
		{"Mul(-1, 0.00)", FromInt(-1).Mul(zeroPointZero).String(), "0"},
		{"Add(Neg(0.00), 0)", NegValue(zeroPointZero).Add(Zero).String(), "0"},
		{"Normalize(Neg(0.0))", NegValue(mustParse(t, "0.0")).Normalize().String(), "0"},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}

	for _, value := range []Decimal{Zero, zeroPointZero, NegValue(zero)} {
		if value.Sign() != 0 {
			t.Errorf("%q.Sign() = %d, want 0", value.String(), value.Sign())
		}
	}
}

func NegValue(value Decimal) Decimal {
	return value.Neg()
}

func TestRescaleMatchesRust(t *testing.T) {
	tests := []struct {
		input string
		scale int32
		want  string
	}{
		{"2.345", 2, "2.35"},
		{"-2.345", 2, "-2.35"},
		{"2.355", 2, "2.36"},
		{"1.5", 2, "1.50"},
		{"0.125", 2, "0.13"},
		{"2.345", 0, "2"},
		{"1.5", 0, "2"},
		{"0.125", 0, "0"},
		{"-0.005", 2, "-0.01"},
		{"-0.004", 2, "-0.00"},
	}
	for _, test := range tests {
		if got := mustParse(t, test.input).Rescale(test.scale).String(); got != test.want {
			t.Errorf("Rescale(%q, %d) = %q, want %q", test.input, test.scale, got, test.want)
		}
	}
}

func TestRoundDPMatchesRust(t *testing.T) {
	tests := []struct {
		input string
		dp    int32
		want  string
	}{
		{"1.5", 2, "1.5"},
		{"2.355", 2, "2.36"},
		{"2.345", 1, "2.3"},
		{"-2.355", 1, "-2.4"},
		{"0.125", 1, "0.1"},
		{"-0.005", 2, "-0.01"},
	}
	for _, test := range tests {
		if got := mustParse(t, test.input).RoundDP(test.dp).String(); got != test.want {
			t.Errorf("RoundDP(%q, %d) = %q, want %q", test.input, test.dp, got, test.want)
		}
	}
}

func TestNormalizeMatchesRust(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.500", "1.5"},
		{"-0.00", "0"},
		{"100", "100"},
		{"0.000", "0"},
	}
	for _, test := range tests {
		if got := mustParse(t, test.input).Normalize().String(); got != test.want {
			t.Errorf("Normalize(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestTruncMatchesRust(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2.9", "2"},
		{"-2.9", "-2"},
	}
	for _, test := range tests {
		if got := mustParse(t, test.input).Trunc().String(); got != test.want {
			t.Errorf("Trunc(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestArithmeticMatchesRust(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"1.50+2.5", mustParse(t, "1.50").Add(mustParse(t, "2.5")).String(), "4.00"},
		{"1.50-1.5", mustParse(t, "1.50").Sub(mustParse(t, "1.5")).String(), "0.00"},
		{"1.50*2.0", mustParse(t, "1.50").Mul(mustParse(t, "2.0")).String(), "3.000"},
		{"New(-1000, 2)", New(-1000, 2).String(), "-10.00"},
		{"FromInt(5)", FromInt(5).String(), "5"},
		{"Zero", Zero.String(), "0"},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}

	if got := mustParse(t, "1.50").Cmp(mustParse(t, "1.5")); got != 0 {
		t.Errorf("Cmp(1.50, 1.5) = %d, want 0", got)
	}
}

func TestInt64RejectsValuesThatDoNotFit(t *testing.T) {
	value := mustParse(t, strings.Repeat("9", 28))
	if _, ok := value.Int64(); ok {
		t.Errorf("Int64(%q) fits, want overflow", value.String())
	}
	if got, ok := mustParse(t, "12.9").Int64(); !ok || got != 12 {
		t.Errorf("Int64(12.9) = %d, %v; want 12, true", got, ok)
	}
}
