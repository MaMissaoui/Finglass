package main

import (
	"os"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/fintsproduct"
)

// linkedTestProductID is the invented id scripts/tests/product-id.sh links in
// with -ldflags -X before it runs TestSelfCheckNeverPrintsTheProductID.
const linkedTestProductID = "TESTPRODUCT0000000000000"

func TestSelfCheckNeverPrintsTheProductID(t *testing.T) {
	if os.Getenv("FINGLASS_EXPECT_PRODUCT_ID_LINKED") != "1" {
		t.Skip("run by scripts/tests/product-id.sh with -ldflags -X")
	}
	if id, ok := fintsproduct.ID(); !ok || id != linkedTestProductID {
		t.Fatal("the linked test product id is not in effect")
	}

	line := finTsProductIDStatus()
	if strings.Contains(line, linkedTestProductID) {
		t.Fatal("the self-check product id line contains the product id")
	}
	if line != "FinTS product id: present" {
		t.Fatalf("the self-check product id line = %q, want present", line)
	}
}

func TestSelfCheckReportsAnAbsentProductID(t *testing.T) {
	if fintsproduct.Present() {
		t.Skip("this build has a linked product id")
	}
	if line := finTsProductIDStatus(); line != "FinTS product id: absent" {
		t.Fatalf("the self-check product id line = %q, want absent", line)
	}
}
