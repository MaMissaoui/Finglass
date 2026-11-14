package fintsgateway

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/model"
)

func TestAccountTypeCoversEveryBoundary(t *testing.T) {
	tests := []struct {
		code string
		want model.AccountType
	}{
		{"", model.AccountTypeChecking},
		{"0", model.AccountTypeChecking},
		{"1", model.AccountTypeChecking},
		{"9", model.AccountTypeChecking},
		{"10", model.AccountTypeSavings},
		{"19", model.AccountTypeSavings},
		{"20", model.AccountTypeSavings},
		{"29", model.AccountTypeSavings},
		{"30", model.AccountTypeDepot},
		{"39", model.AccountTypeDepot},
		{"40", model.AccountTypeLoan},
		{"49", model.AccountTypeLoan},
		{"50", model.AccountTypeCreditCard},
		{"59", model.AccountTypeCreditCard},
		{"60", model.AccountTypeDepot},
		{"69", model.AccountTypeDepot},
		{"70", model.AccountTypeSavings},
		{"79", model.AccountTypeSavings},
		{"80", model.AccountTypeChecking},
		{"100", model.AccountTypeChecking},
		{"G", model.AccountTypeChecking},
	}
	for _, test := range tests {
		if got := accountType(test.code); got != test.want.AsStr() {
			t.Errorf("accountType(%q) = %q, want %q", test.code, got, test.want.AsStr())
		}
	}
}
