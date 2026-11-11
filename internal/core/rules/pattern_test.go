package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type corpusResult struct {
	Input   string `json:"input"`
	Matches bool   `json:"matches"`
}

type corpusEntry struct {
	Pattern string         `json:"pattern"`
	IsRegex bool           `json:"is_regex"`
	Results []corpusResult `json:"results"`
}

type knownDifference struct {
	Pattern     string
	Input       string
	WantIsRegex *bool
	WantMatches *bool
	Reason      string
}

func boolPointer(value bool) *bool {
	return &value
}

// knownDifferences lists the exact corpus cells where regexp2 cannot reproduce
// Rust's fancy-regex result. Every entry is asserted below.
var knownDifferences = []knownDifference{
	{
		Pattern:     "a{2,1}",
		Input:       "aa",
		WantIsRegex: boolPointer(false),
		WantMatches: boolPointer(false),
		Reason: "regexp2 rejects a repetition whose maximum is below its minimum, so Go falls back " +
			"to a substring test; Rust's fancy-regex compiles the swapped bounds and matches two a's",
	},
	{
		Pattern:     "a{2,1}",
		Input:       "aaa",
		WantMatches: boolPointer(false),
		Reason:      "Rust's swapped-bound repetition matches two or more a's; Go's substring fallback does not",
	},
	{
		Pattern:     "a{2,1}",
		Input:       "aaaa",
		WantMatches: boolPointer(false),
		Reason:      "Rust's swapped-bound repetition matches two or more a's; Go's substring fallback does not",
	},
	{
		Pattern:     "a{2,1}",
		Input:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaa!",
		WantMatches: boolPointer(false),
		Reason:      "Rust's swapped-bound repetition matches two or more a's; Go's substring fallback does not",
	},
}

func repoRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found above the test directory")
		}
		directory = parent
	}
}

func readCorpus(t *testing.T) []corpusEntry {
	t.Helper()

	path := filepath.Join(repoRoot(t), "testdata", "golden", "regex_corpus.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var corpus []corpusEntry
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(corpus) < 40 {
		t.Fatalf("%s has %d patterns, want at least 40", path, len(corpus))
	}
	return corpus
}

func differenceFor(pattern, input string) (knownDifference, bool) {
	for _, difference := range knownDifferences {
		if difference.Pattern == pattern && difference.Input == input && difference.WantMatches != nil {
			return difference, true
		}
	}
	return knownDifference{}, false
}

func kindDifferenceFor(pattern string) (knownDifference, bool) {
	for _, difference := range knownDifferences {
		if difference.Pattern == pattern && difference.WantIsRegex != nil {
			return difference, true
		}
	}
	return knownDifference{}, false
}

func TestPatternsMatchTheRustCorpus(t *testing.T) {
	corpus := readCorpus(t)
	for _, entry := range corpus {
		entry := entry
		t.Run(entry.Pattern, func(t *testing.T) {
			pattern := Compile(entry.Pattern)

			wantKind := entry.IsRegex
			if difference, ok := kindDifferenceFor(entry.Pattern); ok {
				wantKind = *difference.WantIsRegex
			}
			if pattern.IsRegex() != wantKind {
				t.Errorf("IsRegex() = %t, want %t", pattern.IsRegex(), wantKind)
			}

			for _, result := range entry.Results {
				want := result.Matches
				if difference, ok := differenceFor(entry.Pattern, result.Input); ok {
					want = *difference.WantMatches
				}
				if got := pattern.IsMatch(result.Input); got != want {
					t.Errorf("IsMatch(%q) = %t, want %t", result.Input, got, want)
				}
			}
		})
	}
}

func TestKnownDifferencesAreReal(t *testing.T) {
	corpus := readCorpus(t)
	byPattern := make(map[string]corpusEntry, len(corpus))
	for _, entry := range corpus {
		byPattern[entry.Pattern] = entry
	}

	for _, difference := range knownDifferences {
		difference := difference
		if difference.Reason == "" {
			t.Errorf("known difference for %q / %q has no reason", difference.Pattern, difference.Input)
		}

		pattern := Compile(difference.Pattern)
		if difference.WantIsRegex != nil && pattern.IsRegex() != *difference.WantIsRegex {
			t.Errorf("IsRegex() for %q = %t, want %t", difference.Pattern, pattern.IsRegex(), *difference.WantIsRegex)
		}

		if difference.WantMatches != nil {
			if difference.Input == "" {
				t.Errorf("known match difference for %q has no input", difference.Pattern)
				continue
			}
			if got := pattern.IsMatch(difference.Input); got != *difference.WantMatches {
				t.Errorf(
					"IsMatch(%q) for %q = %t, want %t",
					difference.Input,
					difference.Pattern,
					got,
					*difference.WantMatches,
				)
			}

			entry, ok := byPattern[difference.Pattern]
			if !ok {
				t.Errorf("known difference names %q, which is not in the corpus", difference.Pattern)
				continue
			}
			found := false
			for _, result := range entry.Results {
				if result.Input == difference.Input {
					found = true
					if result.Matches == *difference.WantMatches {
						t.Errorf(
							"known difference for %q / %q does not differ from Rust",
							difference.Pattern,
							difference.Input,
						)
					}
				}
			}
			if !found {
				t.Errorf("known difference names input %q, which is not in the corpus", difference.Input)
			}
		}
	}
}

func TestCatastrophicPatternReturnsQuickly(t *testing.T) {
	const pattern = `(a+)+$`

	start := time.Now()
	compiled := Compile(pattern)
	if compiled.IsMatch("aaaaaaaaaaaaaaaaaaaaaaaaaaaa!") {
		t.Fatal("the catastrophic pattern unexpectedly matched")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("catastrophic pattern took %s, want under 1s", elapsed)
	}
}

// The substring fallback lower-cases like Rust's str::to_lowercase (full
// Unicode mapping), not like strings.ToLower.
func TestSubstringFallbackUsesFullLowerCasing(t *testing.T) {
	cases := []struct {
		name, pattern, text string
		want                bool
	}{
		// "[" never compiles, so these patterns are substring tests.
		{"dotted capital I", "[İ", "x[i̇y", true},
		{"final sigma", "[ΟΔΟΣ", "[οδος", true},
		{"medial sigma stays", "[ΣΑ", "[σα", true},
	}
	for _, tc := range cases {
		p := Compile(tc.pattern)
		if p.IsRegex() {
			t.Fatalf("%s: %q compiled as a regex; the test needs the substring fallback", tc.name, tc.pattern)
		}
		if got := p.IsMatch(tc.text); got != tc.want {
			t.Errorf("%s: IsMatch(%q) on %q = %v, want %v", tc.name, tc.text, tc.pattern, got, tc.want)
		}
	}
}
