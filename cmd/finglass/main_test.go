package main

import (
	"strings"
	"testing"
)

func TestSelfCheckRegistryHasEveryBoundService(t *testing.T) {
	if err := checkServices(); err != nil {
		t.Fatalf("checkServices: %v", err)
	}
}

func TestSelfCheckFailsWhenTheShellServiceIsMissing(t *testing.T) {
	original := expectedServices
	t.Cleanup(func() { expectedServices = original })

	filtered := make([]expectedService, 0, len(original))
	for _, expected := range original {
		if expected.name != "Shell" {
			filtered = append(filtered, expected)
		}
	}
	expectedServices = filtered

	err := checkServices()
	if err == nil {
		t.Fatal("checkServices accepted a registry without the Shell service")
	}
	if !strings.Contains(err.Error(), "Shell") {
		t.Fatalf("checkServices error = %q, want it to name Shell", err)
	}
}
