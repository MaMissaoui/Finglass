package fints

import "testing"

func TestBlocksPINRepeat(t *testing.T) {
	blocked := []string{"9340", "9910", "9930", "9931", "9942", "3938", "9300", "9399", "9900", "9999"}
	for _, code := range blocked {
		if !BlocksPINRepeat([]string{"9050", code}) {
			t.Fatalf("%s must block a PIN repeat", code)
		}
	}
	for _, codes := range [][]string{{"9050", "9210"}, {"9010"}, {"3060", "3920"}, {"9800"}, {"9400"}, nil} {
		if BlocksPINRepeat(codes) {
			t.Fatalf("%v must not block a PIN repeat", codes)
		}
	}
}
