package banksync

import "testing"

func TestCustomerIDDefaultsToTheUserID(t *testing.T) {
	config := NewFinTsConfig("Deutsche Bank", "37040044", "DEUTDEDBXXX", "testuser", "PRODUCT")
	if got := config.CustomerIDOrDefault(); got != "testuser" {
		t.Fatalf("customer id = %q, want %q", got, "testuser")
	}
	if config.HBCIVersion != "300" {
		t.Fatalf("HBCI version = %q, want %q", config.HBCIVersion, "300")
	}
}

func TestTheProductIDIsTheCallersValue(t *testing.T) {
	config := NewFinTsConfig("Bank", "37050198", "", "user", "REGISTERED-ID")
	if config.ProductID != "REGISTERED-ID" {
		t.Fatalf("product id = %q, want %q", config.ProductID, "REGISTERED-ID")
	}
}
