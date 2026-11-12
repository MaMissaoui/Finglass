package rustfmt

import "testing"

func TestQuoteMatchesRustDebug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"a\u00a0b", `"a\u{a0}b"`},
		{"a\u202fb", `"a\u{202f}b"`},
		{"e\u0301", `"e\u{301}"`},
		{"a\u00adb", `"a\u{ad}b"`},
		{"a\u200db", `"a\u{200d}b"`},
		{"😀", `"😀"`},
		{"\x00", `"\0"`},
		{"\u007f", `"\u{7f}"`},
		{"'", `"'"`},
		{"\u0001", `"\u{1}"`},
		{"a\u2028b", `"a\u{2028}b"`},
		{"\ufeffx", `"\u{feff}x"`},
		{"\ue000", `"\u{e000}"`},
		{"\u200b", `"\u{200b}"`},
		{"tab\tnl\n\"\\", `"tab\tnl\n\"\\"`},
	}
	for _, test := range tests {
		if got := Quote(test.input); got != test.want {
			t.Errorf("Quote(%q) = %s, want %s", test.input, got, test.want)
		}
	}
}
