package banksync

import (
	"context"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

const systemIDPrefixForTest = "37050198|demo-user|https://fints.example/pintan|"

func readSystemIDSetting(t *testing.T, h *harness) (string, bool) {
	t.Helper()
	value, ok, err := settings.Get(context.Background(), h.db, systemIDKey(h.bank))
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	return value, ok
}

func TestSystemIDIsStoredAfterAFreshSync(t *testing.T) {
	h := newHarness(t)
	h.gateway.setSystemID("test-system", 7)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	value, ok := readSystemIDSetting(t, h)
	if !ok {
		t.Fatal("system id setting is missing")
	}
	assertString(t, value, systemIDPrefixForTest+"test-system|7")
}

func TestAMatchingStoredSystemIDIsReused(t *testing.T) {
	h := newHarness(t)
	if err := settings.Set(
		context.Background(), h.db, systemIDKey(h.bank),
		systemIDPrefixForTest+"stored-system|6",
	); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	connection := h.gateway.connectionSeen()
	assertString(t, connection.SystemID, "stored-system")
	assertInt(t, int(connection.HKTANVersion), 6)
	value, ok := readSystemIDSetting(t, h)
	if !ok {
		t.Fatal("system id setting is missing")
	}
	assertString(t, value, systemIDPrefixForTest+"stored-system|6")
}

func TestAStoredSystemIDForAnotherLoginIsDeleted(t *testing.T) {
	h := newHarness(t)
	if err := settings.Set(
		context.Background(), h.db, systemIDKey(h.bank),
		"37050198|other-user|https://fints.example/pintan|old-system|7",
	); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	connection := h.gateway.connectionSeen()
	assertString(t, connection.SystemID, "")
	if _, ok := readSystemIDSetting(t, h); ok {
		t.Fatal("the stale system id was not deleted")
	}
}

func TestASystemIDObtainedBeforeAFailureIsStored(t *testing.T) {
	h := newHarness(t)
	h.gateway.setSystemID("new-system", 7)
	h.gateway.setFailure(gatewayFailure(NewGatewayBankError([]string{"9010"})))

	_, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err == nil || bankingCode(err) != CodeFintsError {
		t.Fatalf("error = %v, want FINTS_ERROR", err)
	}

	value, ok := readSystemIDSetting(t, h)
	if !ok {
		t.Fatal("a system id from before the failure was lost")
	}
	assertString(t, value, systemIDPrefixForTest+"new-system|7")
}

func TestResetLoginForgetsTheSystemID(t *testing.T) {
	h := newHarness(t)
	if err := settings.Set(
		context.Background(), h.db, systemIDKey(h.bank),
		systemIDPrefixForTest+"stored-system|7",
	); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}

	if err := h.service.Client().ResetLogin(context.Background(), h.db, h.bank, false); err != nil {
		t.Fatalf("ResetLogin: %v", err)
	}

	if _, ok := readSystemIDSetting(t, h); ok {
		t.Fatal("ResetLogin kept the system id")
	}
}

func TestRemovingTheBankForgetsTheSystemID(t *testing.T) {
	h := newHarness(t)
	if err := settings.Set(
		context.Background(), h.db, systemIDKey(h.bank),
		systemIDPrefixForTest+"stored-system|7",
	); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}

	if _, err := h.service.RemoveBank(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("RemoveBank: %v", err)
	}

	if _, ok := readSystemIDSetting(t, h); ok {
		t.Fatal("the removed bank kept its system id")
	}
}
