package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
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

func TestSelfCheckFailsWhenTheDashboardServiceIsMissing(t *testing.T) {
	original := expectedServices
	t.Cleanup(func() { expectedServices = original })

	filtered := make([]expectedService, 0, len(original))
	for _, expected := range original {
		if expected.name != "Dashboard" {
			filtered = append(filtered, expected)
		}
	}
	expectedServices = filtered

	err := checkServices()
	if err == nil {
		t.Fatal("checkServices accepted a registry without the Dashboard service")
	}
	if !strings.Contains(err.Error(), "Dashboard") {
		t.Fatalf("checkServices error = %q, want it to name Dashboard", err)
	}
}

func TestSelfCheckFailsWhenTheAccountsServiceIsMissing(t *testing.T) {
	original := expectedServices
	t.Cleanup(func() { expectedServices = original })

	filtered := make([]expectedService, 0, len(original))
	for _, expected := range original {
		if expected.name != "Accounts" {
			filtered = append(filtered, expected)
		}
	}
	expectedServices = filtered

	err := checkServices()
	if err == nil {
		t.Fatal("checkServices accepted a registry without the Accounts service")
	}
	if !strings.Contains(err.Error(), "Accounts") {
		t.Fatalf("checkServices error = %q, want it to name Accounts", err)
	}
}

func TestSelfCheckFailsWhenTheBankingServiceIsMissing(t *testing.T) {
	original := expectedServices
	t.Cleanup(func() { expectedServices = original })

	filtered := make([]expectedService, 0, len(original))
	for _, expected := range original {
		if expected.name != "Banking" {
			filtered = append(filtered, expected)
		}
	}
	expectedServices = filtered

	err := checkServices()
	if err == nil {
		t.Fatal("checkServices accepted a registry without the Banking service")
	}
	if !strings.Contains(err.Error(), "Banking") {
		t.Fatalf("checkServices error = %q, want it to name Banking", err)
	}
}

// Wails logs bound-call arguments, passphrases included, at Debug.
func TestTheWailsLoggerNeverLogsDebug(t *testing.T) {
	options := applicationOptions(fstest.MapFS{})
	if options.Logger == nil {
		t.Fatal("no explicit Wails logger: Wails would pick its own by build tag")
	}
	if options.Logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("the Wails logger has Debug enabled and would log call arguments")
	}
	if options.LogLevel < slog.LevelInfo {
		t.Fatalf("LogLevel = %v, want at least Info", options.LogLevel)
	}
}
