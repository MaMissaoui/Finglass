package prefs

import (
	"context"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func database(t *testing.T) *db.Database {
	t.Helper()

	path := t.TempDir() + "/finglass.db"
	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	opened, err := db.Create(path, passphrase)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = opened.Close()
	})
	return opened
}

func TestDefaultsWhenUnset(t *testing.T) {
	ctx := context.Background()
	opened := database(t)

	loaded, err := Load(ctx, opened, i18n.Fr)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Theme != ThemeSystem {
		t.Errorf("theme = %v, want system", loaded.Theme)
	}
	if loaded.Language != i18n.Fr {
		t.Errorf("language = %v, want French", loaded.Language)
	}
	if loaded.SidebarCollapsed {
		t.Error("the sidebar starts expanded")
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	ctx := context.Background()
	opened := database(t)

	if err := SaveTheme(ctx, opened, ThemeDark); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	if err := SaveLanguage(ctx, opened, i18n.De); err != nil {
		t.Fatalf("SaveLanguage: %v", err)
	}
	if err := SaveSidebarCollapsed(ctx, opened, true); err != nil {
		t.Fatalf("SaveSidebarCollapsed: %v", err)
	}

	loaded, err := Load(ctx, opened, i18n.En)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Theme != ThemeDark {
		t.Errorf("theme = %v, want dark", loaded.Theme)
	}
	if loaded.Language != i18n.De {
		t.Errorf("language = %v, want German", loaded.Language)
	}
	if !loaded.SidebarCollapsed {
		t.Error("sidebar = expanded, want collapsed")
	}
}

func TestSidebarFlagRoundTrips(t *testing.T) {
	ctx := context.Background()
	opened := database(t)

	if err := SaveSidebarCollapsed(ctx, opened, true); err != nil {
		t.Fatalf("SaveSidebarCollapsed(true): %v", err)
	}
	loaded, err := Load(ctx, opened, i18n.En)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.SidebarCollapsed {
		t.Error("sidebar = expanded, want collapsed")
	}

	if err := SaveSidebarCollapsed(ctx, opened, false); err != nil {
		t.Fatalf("SaveSidebarCollapsed(false): %v", err)
	}
	loaded, err = Load(ctx, opened, i18n.En)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SidebarCollapsed {
		t.Error("sidebar = collapsed, want expanded")
	}
}

func TestUnknownSidebarValueFallsBackToExpanded(t *testing.T) {
	ctx := context.Background()
	opened := database(t)

	if err := settings.Set(ctx, opened, SidebarCollapsedKey, "sometimes"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	loaded, err := Load(ctx, opened, i18n.En)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SidebarCollapsed {
		t.Error("sidebar = collapsed, want expanded")
	}
}

func TestEveryChangeWritesAnAuditEntry(t *testing.T) {
	ctx := context.Background()
	opened := database(t)

	if err := SaveTheme(ctx, opened, ThemeLight); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	if err := SaveLanguage(ctx, opened, i18n.Fr); err != nil {
		t.Fatalf("SaveLanguage: %v", err)
	}
	if err := SaveSidebarCollapsed(ctx, opened, true); err != nil {
		t.Fatalf("SaveSidebarCollapsed: %v", err)
	}

	entries, err := audit.Recent(ctx, opened, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}

	actions := make(map[string]bool)
	for _, entry := range entries {
		actions[entry.Action] = true
		if entry.Actor != audit.ActorUser {
			t.Errorf("actor = %v, want user", entry.Actor)
		}
	}
	for _, action := range []string{"UI_THEME_CHANGED", "UI_LANGUAGE_CHANGED", "UI_SIDEBAR_CHANGED"} {
		if !actions[action] {
			t.Errorf("missing audit action %s", action)
		}
	}
}

func TestUnknownStoredValuesFallBack(t *testing.T) {
	ctx := context.Background()
	opened := database(t)

	if err := settings.Set(ctx, opened, ThemeKey, "sepia"); err != nil {
		t.Fatalf("Set theme: %v", err)
	}
	if err := settings.Set(ctx, opened, LanguageKey, "es"); err != nil {
		t.Fatalf("Set language: %v", err)
	}

	loaded, err := Load(ctx, opened, i18n.En)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Theme != ThemeSystem {
		t.Errorf("theme = %v, want system", loaded.Theme)
	}
	if loaded.Language != i18n.En {
		t.Errorf("language = %v, want English", loaded.Language)
	}
}

func TestCodesRoundTrip(t *testing.T) {
	for _, theme := range AllThemes {
		parsed, ok := ThemeFromCode(theme.Code())
		if !ok || parsed != theme {
			t.Errorf("ThemeFromCode(%q) = %v, %v, want %v", theme.Code(), parsed, ok, theme)
		}
	}
	if parsed, ok := ThemeFromCode("SYSTEM"); !ok || parsed != ThemeSystem {
		t.Errorf("ThemeFromCode(SYSTEM) = %v, %v", parsed, ok)
	}
	if _, ok := ThemeFromCode("sepia"); ok {
		t.Error("ThemeFromCode(sepia) succeeded")
	}
}
