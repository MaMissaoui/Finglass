package bind

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/fintsproduct"
)

// linkedTestProductID is the invented id scripts/tests/product-id.sh links in
// with -ldflags -X before it runs TestAboutDTONeverPrintsTheProductID.
const linkedTestProductID = "TESTPRODUCT0000000000000"

func TestAboutDTONeverPrintsTheProductID(t *testing.T) {
	if os.Getenv("FINGLASS_EXPECT_PRODUCT_ID_LINKED") != "1" {
		t.Skip("run by scripts/tests/product-id.sh with -ldflags -X")
	}
	const productID = linkedTestProductID
	if id, ok := fintsproduct.ID(); !ok || id != productID {
		t.Fatal("the linked test product id is not in effect")
	}

	info, err := (&AppInfo{}).Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	rendered := []string{
		fmt.Sprintf("%v", info),
		fmt.Sprintf("%+v", info),
		fmt.Sprintf("%#v", info),
		string(encoded),
	}
	for _, value := range rendered {
		if strings.Contains(value, productID) {
			t.Fatal("the About DTO contains the product id")
		}
	}
}
