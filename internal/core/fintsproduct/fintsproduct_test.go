package fintsproduct

import (
	"os"
	"strings"
	"testing"
)

func TestID(t *testing.T) {
	original := productID
	t.Cleanup(func() { productID = original })

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "valid", value: "TESTPRODUCT0000000000000", want: true},
		{name: "shortest", value: "A", want: true},
		{name: "longest", value: strings.Repeat("A", 30), want: true},
		{name: "too long", value: strings.Repeat("A", 31), want: false},
		{name: "question mark", value: "PRODUCT?ID", want: false},
		{name: "apostrophe", value: "PRODUCT'ID", want: false},
		{name: "plus", value: "PRODUCT+ID", want: false},
		{name: "colon", value: "PRODUCT:ID", want: false},
		{name: "at sign", value: "PRODUCT@ID", want: false},
		{name: "space", value: "PRODUCT ID", want: false},
		{name: "hyphen", value: "PRODUCT-ID", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetForTest(test.value)

			id, ok := ID()
			if ok != test.want {
				t.Fatalf("ID() present = %t, want %t", ok, test.want)
			}
			if test.want {
				if id != test.value {
					t.Fatal("ID() returned a different value than the configured one")
				}
			} else if id != "" {
				t.Fatal("ID() returned a value for an invalid product id")
			}
			if Present() != test.want {
				t.Fatalf("Present() = %t, want %t", Present(), test.want)
			}
		})
	}
}

// TestLinkerValue proves that -ldflags -X reaches productID. It only runs when
// scripts/tests/product-id.sh asks for it with a linker-set value.
func TestLinkerValue(t *testing.T) {
	if os.Getenv("FINGLASS_EXPECT_PRODUCT_ID_LINKED") != "1" {
		t.Skip("set FINGLASS_EXPECT_PRODUCT_ID_LINKED=1 and build with -ldflags -X")
	}
	if !Present() {
		t.Fatal("the linked product id is not present")
	}
	if id, ok := ID(); !ok || id == "" {
		t.Fatal("the linked product id is not valid")
	}
}
