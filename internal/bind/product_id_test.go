package bind

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	bankingcore "github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/core/fintsproduct"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
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

// A product id prompted for one test is just as secret as a linked one.
func TestAboutAndBankingDTOsNeverPrintAPromptedProductID(t *testing.T) {
	const prompted = "PROMPTEDPRODUCT1234567890"

	info, err := (&AppInfo{}).Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	state := bankingcore.BankingState{
		Language: i18n.En,
		ConnectionTest: &bankingcore.ConnectionTestResult{
			BankID:   1,
			Reason:   i18n.Text(i18n.En, i18n.TestConnectionProductID),
			HasTrace: true,
		},
	}
	dto := bankingDTO(state, i18n.En)
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal banking DTO: %v", err)
	}
	for _, value := range []string{
		fmt.Sprintf("%v", info),
		fmt.Sprintf("%+v", info),
		fmt.Sprintf("%#v", info),
		fmt.Sprintf("%v", dto),
		fmt.Sprintf("%+v", dto),
		fmt.Sprintf("%#v", dto),
		string(encoded),
	} {
		if strings.Contains(value, prompted) {
			t.Fatal("a DTO contains the prompted product id")
		}
	}
}
