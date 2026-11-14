package banksync

import (
	"context"
	"fmt"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func TestPinFailureClassificationForBothAdapters(t *testing.T) {
	tests := []struct {
		name              string
		gateway           GatewayError
		testCodes         []string
		testLoginRejected bool
		testTANRejected   bool
		want              PinFailureClass
	}{
		{
			name:    "network keeps",
			gateway: NewGatewayNetworkError(),
			want:    PinFailureKeep,
		},
		{
			name:    "cancel keeps",
			gateway: NewGatewayCancelledError(),
			want:    PinFailureKeep,
		},
		{
			name:    "poll limit keeps",
			gateway: NewGatewayOtherError("poll-limit"),
			want:    PinFailureKeep,
		},
		{
			name:    "invalid state keeps",
			gateway: NewGatewayOtherError("invalid-state"),
			want:    PinFailureKeep,
		},
		{
			name:    "protocol keeps",
			gateway: NewGatewayOtherError("protocol"),
			want:    PinFailureKeep,
		},
		{
			name:      "ordinary bank code keeps",
			gateway:   NewGatewayBankError([]string{"9010"}),
			testCodes: []string{"9010"},
			want:      PinFailureKeep,
		},
		{
			name:              "explicit pin code removes",
			gateway:           NewGatewayBankError([]string{"9931"}),
			testCodes:         []string{"9931"},
			testLoginRejected: true,
			want:              PinFailureRemoveNow,
		},
		{
			name:              "gateway pin kind removes",
			gateway:           NewGatewayPinRejectedError(),
			testCodes:         []string{"9942"},
			testLoginRejected: true,
			want:              PinFailureRemoveNow,
		},
		{
			name:              "locked account removes",
			gateway:           NewGatewayBankError([]string{"3938"}),
			testCodes:         []string{"3938"},
			testLoginRejected: true,
			want:              PinFailureRemoveNow,
		},
		{
			name:      "other 99xx counts",
			gateway:   NewGatewayBankError([]string{"9999"}),
			testCodes: []string{"9999"},
			want:      PinFailureCount,
		},
		{
			name:      "other 93xx counts",
			gateway:   NewGatewayBankError([]string{"9310"}),
			testCodes: []string{"9310"},
			want:      PinFailureCount,
		},
		{
			name:            "a rejected TAN with a 99xx code keeps",
			gateway:         NewGatewayTANRejectedBankError([]string{"9941"}),
			testCodes:       []string{"9941"},
			testTANRejected: true,
			want:            PinFailureKeep,
		},
		{
			name:            "a rejected TAN with an explicit wrong-PIN code removes",
			gateway:         NewGatewayTANRejectedBankError([]string{"9941", "9931"}),
			testCodes:       []string{"9941", "9931"},
			testTANRejected: true,
			want:            PinFailureRemoveNow,
		},
		{
			name:              "pin kind without an explicit code counts",
			gateway:           NewGatewayPINRejectedBankError([]string{"9010"}),
			testCodes:         []string{"9010"},
			testLoginRejected: true,
			want:              PinFailureCount,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyPinFailure(PinFailureFromGateway(test.gateway)); got != test.want {
				t.Errorf("sync adapter class = %v, want %v", got, test.want)
			}
			if got := ClassifyPinFailure(PinFailureFromCodes(test.testCodes, test.testLoginRejected, test.testTANRejected)); got != test.want {
				t.Errorf("test adapter class = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPinFailureApplicationForBothAdaptersAndBothOptions(t *testing.T) {
	type testCase struct {
		name      string
		syncInput PinFailureInput
		testInput PinFailureInput
		class     PinFailureClass
	}
	tests := []testCase{
		{
			name:      "keep",
			syncInput: PinFailureFromGateway(NewGatewayNetworkError()),
			testInput: PinFailureFromCodes(nil, false, false),
			class:     PinFailureKeep,
		},
		{
			name:      "explicit",
			syncInput: PinFailureFromGateway(NewGatewayBankError([]string{"9931"})),
			testInput: PinFailureFromCodes([]string{"9931"}, true, false),
			class:     PinFailureRemoveNow,
		},
		{
			name:      "other",
			syncInput: PinFailureFromGateway(NewGatewayBankError([]string{"9999"})),
			testInput: PinFailureFromCodes([]string{"9999"}, false, false),
			class:     PinFailureCount,
		},
	}
	for _, option := range []string{"1", "2"} {
		for _, test := range tests {
			for adapter, input := range map[string]PinFailureInput{
				"sync": test.syncInput,
				"test": test.testInput,
			} {
				name := option + "/" + adapter + "/" + test.name
				t.Run(name, func(t *testing.T) {
					h := newHarness(t)
					storePin(t, h)
					savePinForgetAfterForTest(t, h, option)

					result := h.service.ApplyPinFailure(context.Background(), h.db, h.bank, input)
					switch test.class {
					case PinFailureKeep:
						if result != PinFailureIgnored {
							t.Fatalf("result = %v, want ignored", result)
						}
						if !h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
							t.Fatal("a keep class removed the PIN")
						}
					case PinFailureRemoveNow:
						if result != PinFailureRemoved {
							t.Fatalf("result = %v, want removed", result)
						}
						if h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
							t.Fatal("an explicit rejection kept the PIN")
						}
					case PinFailureCount:
						if option == "1" {
							if result != PinFailureRemoved {
								t.Fatalf("result = %v, want removed with option 1", result)
							}
							if h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
								t.Fatal("option 1 kept the PIN after an other rejection")
							}
						} else {
							if result != PinFailureCounted {
								t.Fatalf("result = %v, want counted with option 2", result)
							}
							if !h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
								t.Fatal("option 2 removed the PIN after the first other rejection")
							}
						}
					}
				})
			}
		}
	}
}

func TestPinRemovalOptionDefaultsToTheFirstRejection(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	if got := h.service.PinForgetAfter(context.Background(), h.db, h.bank); got != "1" {
		t.Fatalf("default option = %q, want 1", got)
	}
}

func TestAnOtherRejectionRemovesThePinAfterTheFirstRejectionByDefault(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	h.gateway.setFailure(gatewayFailure(NewGatewayBankError([]string{"9999"})))

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("the rejection sync succeeded")
	}
	if h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("the PIN survived the first rejection")
	}
	if hasPinRejections(t, h) {
		t.Fatal("the rejection count survived the removed PIN")
	}
	assertInt(t, auditCount(t, h.db, CodePinForgotten), 1)
}

func TestTheSecondRejectionRemovesThePinWithOptionTwo(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	savePinForgetAfterForTest(t, h, "2")

	h.gateway.setFailure(gatewayFailure(NewGatewayBankError([]string{"9999"})))
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("the first rejection sync succeeded")
	}
	if !h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("the PIN was removed after the first rejection")
	}
	if got := pinRejectionsForTest(t, h); got != 1 {
		t.Fatalf("rejection count = %d, want 1", got)
	}
	if auditHasAction(t, h.db, CodePinForgotten) {
		t.Fatal("the kept PIN was audited as forgotten")
	}

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("the second rejection sync succeeded")
	}
	if h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("the PIN survived the second rejection")
	}
	if hasPinRejections(t, h) {
		t.Fatal("the rejection count survived the removed PIN")
	}
	assertInt(t, auditCount(t, h.db, CodePinForgotten), 1)
}

func TestAnExplicitPinRejectionRemovesAtOnceWithOptionTwo(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	savePinForgetAfterForTest(t, h, "2")

	h.gateway.setFailure(gatewayFailure(NewGatewayBankError([]string{"9931"})))
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("the rejection sync succeeded")
	}
	if h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("the PIN survived an explicit rejection")
	}
	assertInt(t, auditCount(t, h.db, CodePinForgotten), 1)
}

func TestASuccessfulDialogResetsTheRejectionCount(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	savePinForgetAfterForTest(t, h, "2")
	h.gateway.setFailure(gatewayFailure(NewGatewayBankError([]string{"9999"})))
	h.gateway.setSnapshots([]AccountSnapshot{snapshot("DE89370400440532013000", "100.00", nil)})

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("the rejection sync succeeded")
	}
	if got := pinRejectionsForTest(t, h); got != 1 {
		t.Fatalf("rejection count = %d, want 1", got)
	}

	h.gateway.setFailure(nil)
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("successful sync: %v", err)
	}
	if hasPinRejections(t, h) {
		t.Fatal("the successful dialog kept the rejection count")
	}

	h.gateway.setFailure(gatewayFailure(NewGatewayBankError([]string{"9999"})))
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("the rejection sync succeeded")
	}
	if !h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("the PIN was removed after the reset count's first rejection")
	}
	if got := pinRejectionsForTest(t, h); got != 1 {
		t.Fatalf("rejection count after the reset = %d, want 1", got)
	}
}

func TestATransportFailureKeepsTheStoredPinAndTheCount(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	h.gateway.setFailure(gatewayFailure(NewGatewayNetworkError()))

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("the transport-failure sync succeeded")
	}
	if !h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("a transport failure removed the PIN")
	}
	if hasPinRejections(t, h) {
		t.Fatal("a transport failure changed the rejection count")
	}
}

func TestThePinRemovalOptionIsAuditedOnlyWhenItChanges(t *testing.T) {
	h := newHarness(t)
	background := context.Background()
	previous := h.service.PinForgetAfter(background, h.db, h.bank)

	if err := h.service.SavePinForgetAfter(background, h.db, h.bank, "2", previous); err != nil {
		t.Fatalf("save option: %v", err)
	}
	assertInt(t, auditCount(t, h.db, CodePinForgetAfter), 1)

	if err := h.service.SavePinForgetAfter(background, h.db, h.bank, "2", "2"); err != nil {
		t.Fatalf("save unchanged option: %v", err)
	}
	assertInt(t, auditCount(t, h.db, CodePinForgetAfter), 1)

	if err := h.service.SavePinForgetAfter(background, h.db, h.bank, "1", "2"); err != nil {
		t.Fatalf("change option back: %v", err)
	}
	assertInt(t, auditCount(t, h.db, CodePinForgetAfter), 2)
}

func TestResetLoginDropsThePinRemovalOption(t *testing.T) {
	h := newHarness(t)
	savePinForgetAfterForTest(t, h, "2")
	if err := h.service.Client().ResetLogin(context.Background(), h.db, h.bank, true); err != nil {
		t.Fatalf("ResetLogin: %v", err)
	}
	value, ok, err := settings.Get(context.Background(), h.db, PinForgetAfterKey(h.bank))
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	if ok {
		t.Fatalf("ResetLogin kept the option %q", value)
	}
}

func savePinForgetAfterForTest(t *testing.T, h *harness, value string) {
	t.Helper()
	if err := h.service.SavePinForgetAfter(
		context.Background(),
		h.db,
		h.bank,
		value,
		h.service.PinForgetAfter(context.Background(), h.db, h.bank),
	); err != nil {
		t.Fatalf("SavePinForgetAfter: %v", err)
	}
}

func pinRejectionsForTest(t *testing.T, h *harness) int {
	t.Helper()
	value, ok, err := settings.Get(context.Background(), h.db, PinRejectionsKey(h.bank))
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	if !ok {
		return 0
	}
	var count int
	if _, err := fmt.Sscanf(value, "%d", &count); err != nil {
		t.Fatalf("rejection count %q: %v", value, err)
	}
	return count
}

func hasPinRejections(t *testing.T, h *harness) bool {
	t.Helper()
	_, ok, err := settings.Get(context.Background(), h.db, PinRejectionsKey(h.bank))
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	return ok
}
