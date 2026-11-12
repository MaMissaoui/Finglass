package banking

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func assertTextWiped(t *testing.T, text *secret.Text) {
	t.Helper()
	if text == nil {
		t.Fatal("secret.Text is nil")
	}
	if text.Len() != 0 {
		t.Fatalf("secret.Text still holds %d bytes", text.Len())
	}
	if err := text.Use(func([]byte) error { return nil }); !errors.Is(err, secret.ErrWiped) {
		t.Fatalf("Use after Wipe = %v, want ErrWiped", err)
	}
}

// openPromptForTest publishes a prompt without a waiting ask, so the discard
// paths can be exercised deterministically.
func openPromptForTest(bridge *PromptBridge, kind PromptKind) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.prompt = &BankingPrompt{Kind: kind}
}

func pinText(t *testing.T, entry *banksync.PinEntry) string {
	t.Helper()
	if entry == nil {
		t.Fatal("PinEntry is nil")
	}
	var text string
	if err := entry.Pin().Use(func(value []byte) error {
		text = string(value)
		return nil
	}); err != nil {
		t.Fatalf("read PIN: %v", err)
	}
	return text
}

func secretText(t *testing.T, text *secret.Text) string {
	t.Helper()
	if text == nil {
		t.Fatal("secret is nil")
	}
	var value string
	if err := text.Use(func(bytes []byte) error {
		value = string(bytes)
		return nil
	}); err != nil {
		t.Fatalf("read secret: %v", err)
	}
	return value
}

func TestCancelPendingUnblocksAWaitingPinPrompt(t *testing.T) {
	bridge := bridgeForTest(30 * time.Second)
	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- bridge.Pin("Bank", "user", true)
	}()

	waitUntil(t, func() bool {
		prompt := bridge.Prompt()
		return prompt != nil && prompt.Kind == Pin
	})
	started := time.Now()
	bridge.CancelPending()
	if bridge.Prompt() != nil {
		t.Fatal("the prompt is still open after CancelPending")
	}
	select {
	case answer := <-result:
		if answer != nil {
			t.Fatal("the cancelled PIN prompt returned an entry")
		}
	case <-time.After(time.Second):
		t.Fatal("the worker was not unblocked")
	}
	if time.Since(started) >= time.Second {
		t.Fatal("CancelPending took too long")
	}
	if bridge.Prompt() != nil {
		t.Fatal("the prompt reopened")
	}
}

func TestAfterCancelEveryNewAskReturnsNoneAtOnce(t *testing.T) {
	bridge := bridgeForTest(30 * time.Second)
	bridge.CancelPending()

	if bridge.Pin("Bank", "user", true) != nil {
		t.Fatal("Pin returned an entry after cancel")
	}
	if bridge.SelectTanMethod(nil) != nil {
		t.Fatal("SelectTanMethod returned a choice after cancel")
	}
	if bridge.SelectTanMedium(nil) != nil {
		t.Fatal("SelectTanMedium returned a choice after cancel")
	}
	if bridge.Tan(banksync.TanChallenge{Message: "Challenge"}) != nil {
		t.Fatal("Tan returned a TAN after cancel")
	}
	if bridge.ConfirmDecoupled("Confirm") {
		t.Fatal("ConfirmDecoupled returned true after cancel")
	}
}

func TestAnUnansweredPromptTimesOutAsCancelled(t *testing.T) {
	bridge := bridgeForTest(40 * time.Millisecond)
	started := time.Now()
	if bridge.Pin("Bank", "user", true) != nil {
		t.Fatal("the timed-out prompt returned an entry")
	}
	if time.Since(started) >= time.Second {
		t.Fatal("the prompt took too long to time out")
	}
	if bridge.Prompt() != nil {
		t.Fatal("the prompt is still open after the timeout")
	}
}

func TestAnAnsweredPinArrivesWithRemember(t *testing.T) {
	bridge := bridgeForTest(5 * time.Second)
	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- bridge.Pin("Bank", "user", true)
	}()

	waitUntil(t, func() bool {
		prompt := bridge.Prompt()
		return prompt != nil && prompt.Kind == Pin
	})
	bridge.AnswerPin([]byte("12345"), true)
	var entry *banksync.PinEntry
	select {
	case entry = <-result:
	case <-time.After(time.Second):
		t.Fatal("the worker did not receive the answer")
	}
	if got := pinText(t, entry); got != "12345" {
		t.Fatalf("PIN = %q", got)
	}
	if !entry.Remember() {
		t.Fatal("Remember = false")
	}
	entry.Pin().Wipe()
	if bridge.Prompt() != nil {
		t.Fatal("the prompt is still open")
	}
}

func TestAnEmptyPinCancels(t *testing.T) {
	bridge := bridgeForTest(5 * time.Second)
	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- bridge.Pin("Bank", "user", true)
	}()

	waitUntil(t, func() bool {
		prompt := bridge.Prompt()
		return prompt != nil && prompt.Kind == Pin
	})
	bridge.AnswerPin([]byte(""), false)
	select {
	case entry := <-result:
		if entry != nil {
			t.Fatal("an empty PIN returned an entry")
		}
	case <-time.After(time.Second):
		t.Fatal("the worker was not unblocked")
	}
}

func TestAWrongKindAnswerIsIgnored(t *testing.T) {
	bridge := bridgeForTest(5 * time.Second)
	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- bridge.Pin("Bank", "user", true)
	}()

	waitUntil(t, func() bool {
		prompt := bridge.Prompt()
		return prompt != nil && prompt.Kind == Pin
	})
	bridge.AnswerChoice(stringPointer("942"))
	bridge.AnswerTan([]byte("999999"))
	bridge.AnswerDecoupled(true)
	prompt := bridge.Prompt()
	if prompt == nil || prompt.Kind != Pin {
		t.Fatal("a wrong-kind answer closed the PIN prompt")
	}

	bridge.AnswerPin([]byte("12345"), false)
	var entry *banksync.PinEntry
	select {
	case entry = <-result:
	case <-time.After(time.Second):
		t.Fatal("the worker did not receive the answer")
	}
	if got := pinText(t, entry); got != "12345" {
		t.Fatalf("PIN = %q", got)
	}
	if entry.Remember() {
		t.Fatal("Remember = true")
	}
	entry.Pin().Wipe()
}

func TestOnChangeFiresWhenThePromptOpensAndCloses(t *testing.T) {
	var changes atomic.Int64
	bridge := NewPromptBridge(5*time.Second, func() {
		changes.Add(1)
	})
	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- bridge.Pin("Bank", "user", true)
	}()

	waitUntil(t, func() bool { return changes.Load() >= 1 })
	bridge.AnswerPin([]byte("12345"), false)
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("the worker did not receive the answer")
	}
	if changes.Load() < 2 {
		t.Fatalf("onChange fired %d times, want at least 2", changes.Load())
	}
}

func TestPromptsAndStateDebugNeverContainAPinOrTan(t *testing.T) {
	const pin = "PIN-SECRET-123"
	const tan = "TAN-SECRET-456"
	verbs := []string{"%v", "%+v", "%#v"}

	bridge := bridgeForTest(5 * time.Second)
	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- bridge.Pin("Bank", "user", true)
	}()
	waitUntil(t, func() bool {
		prompt := bridge.Prompt()
		return prompt != nil && prompt.Kind == Pin
	})
	for _, verb := range verbs {
		debug := fmt.Sprintf(verb, *bridge.Prompt())
		if strings.Contains(debug, pin) {
			t.Fatalf("%s prompt leaked the PIN: %s", verb, debug)
		}
	}
	bridge.AnswerPin([]byte(pin), false)
	select {
	case entry := <-result:
		entry.Pin().Wipe()
	case <-time.After(time.Second):
		t.Fatal("the worker did not receive the answer")
	}

	resultTan := make(chan *secret.Text, 1)
	go func() {
		resultTan <- bridge.Tan(banksync.TanChallenge{Message: "Enter the TAN"})
	}()
	waitUntil(t, func() bool {
		prompt := bridge.Prompt()
		return prompt != nil && prompt.Kind == Tan
	})
	for _, verb := range verbs {
		debug := fmt.Sprintf(verb, *bridge.Prompt())
		if strings.Contains(debug, tan) {
			t.Fatalf("%s prompt leaked the TAN: %s", verb, debug)
		}
	}
	bridge.AnswerTan([]byte(tan))
	select {
	case answer := <-resultTan:
		if answer != nil {
			answer.Wipe()
		}
	case <-time.After(time.Second):
		t.Fatal("the worker did not receive the TAN")
	}

	allPrompts := []BankingPrompt{
		{Kind: Pin, BankName: "Bank", UserID: "user", OfferRemember: true},
		{Kind: ChooseTanMethod, TanMethods: []banksync.TanMethod{{ID: "942", Name: "chipTAN"}}},
		{Kind: ChooseTanMedium, TanMediums: []string{"phone"}},
		{Kind: Tan, Challenge: banksync.TanChallenge{Message: "Enter the TAN"}},
		{Kind: Decoupled, Message: "Confirm in the app"},
	}
	for _, prompt := range allPrompts {
		for _, verb := range verbs {
			debug := fmt.Sprintf(verb, prompt)
			if strings.Contains(debug, pin) || strings.Contains(debug, tan) {
				t.Fatalf("%s prompt leaked a secret: %s", verb, debug)
			}
		}
	}

	// Format the bridge itself while it holds a pending secret, but with no
	// worker running, so fmt reads a quiescent value.
	pinBridge := bridgeForTest(time.Second)
	openPromptForTest(pinBridge, Pin)
	pinAnswer := secret.NewText([]byte(pin))
	pinBridge.answerPinText(pinAnswer, true)
	for _, verb := range verbs {
		debug := fmt.Sprintf(verb, pinBridge)
		if strings.Contains(debug, pin) {
			t.Fatalf("%s bridge leaked the PIN: %s", verb, debug)
		}
	}
	pinBridge.CancelPending()
	assertTextWiped(t, pinAnswer)

	tanBridge := bridgeForTest(time.Second)
	openPromptForTest(tanBridge, Tan)
	tanAnswer := secret.NewText([]byte(tan))
	tanBridge.answerTanText(tanAnswer)
	for _, verb := range verbs {
		debug := fmt.Sprintf(verb, tanBridge)
		if strings.Contains(debug, tan) {
			t.Fatalf("%s bridge leaked the TAN: %s", verb, debug)
		}
	}
	tanBridge.CancelPending()
	assertTextWiped(t, tanAnswer)

	state := New(i18n.En)
	for _, verb := range verbs {
		debug := fmt.Sprintf(verb, state)
		if strings.Contains(debug, pin) || strings.Contains(debug, tan) {
			t.Fatalf("%s state leaked a secret: %s", verb, debug)
		}
	}
}

// TestOnChangeCallingPromptAndInfoDoesNotDeadlock proves onChange runs without
// the bridge's lock held: Prompt and Info take that lock.
func TestOnChangeCallingPromptAndInfoDoesNotDeadlock(t *testing.T) {
	var calls atomic.Int64
	var bridge *PromptBridge
	bridge = NewPromptBridge(5*time.Second, func() {
		_ = bridge.Prompt()
		_ = bridge.CurrentInfo()
		calls.Add(1)
	})

	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- bridge.Pin("Bank", "user", true)
	}()
	// The prompt opening fires the first onChange and Info the second. If the
	// lock were held, Prompt or CurrentInfo would deadlock and this would time
	// out.
	waitUntil(t, func() bool { return bridge.Prompt() != nil })
	bridge.Info("Waiting for the bank")
	waitUntil(t, func() bool { return calls.Load() >= 2 })

	if got := stringValue(bridge.CurrentInfo()); got != "Waiting for the bank" {
		t.Fatalf("info = %q", got)
	}
	bridge.AnswerPin([]byte("12345"), false)
	select {
	case entry := <-result:
		if entry != nil {
			entry.Pin().Wipe()
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the worker did not finish")
	}
}

func TestAConcurrentSecondAskReturnsNilWhileTheFirstStaysOpen(t *testing.T) {
	bridge := bridgeForTest(5 * time.Second)
	first := make(chan *banksync.PinEntry, 1)
	go func() {
		first <- bridge.Pin("Bank", "user", true)
	}()
	waitUntil(t, func() bool {
		prompt := bridge.Prompt()
		return prompt != nil && prompt.Kind == Pin
	})

	second := make(chan *string, 1)
	go func() {
		second <- bridge.SelectTanMethod([]banksync.TanMethod{{ID: "942", Name: "chipTAN"}})
	}()
	select {
	case choice := <-second:
		if choice != nil {
			t.Fatal("the concurrent second ask returned an answer")
		}
	case <-time.After(time.Second):
		t.Fatal("the concurrent second ask blocked")
	}
	prompt := bridge.Prompt()
	if prompt == nil || prompt.Kind != Pin {
		t.Fatal("the first prompt is no longer open")
	}

	bridge.AnswerPin([]byte("12345"), false)
	select {
	case entry := <-first:
		if entry == nil {
			t.Fatal("the first ask did not receive its answer")
		}
		entry.Pin().Wipe()
	case <-time.After(time.Second):
		t.Fatal("the first ask did not finish")
	}
}

func TestAnAskAfterCancelPendingReturnsNil(t *testing.T) {
	bridge := bridgeForTest(30 * time.Second)
	bridge.CancelPending()
	started := time.Now()
	if bridge.Pin("Bank", "user", true) != nil {
		t.Fatal("Pin returned an entry after cancel")
	}
	if time.Since(started) >= time.Second {
		t.Fatal("an ask after cancel did not return at once")
	}
	if bridge.Prompt() != nil {
		t.Fatal("a prompt opened after cancel")
	}
}

func TestATimeoutClosesThePromptAndIgnoresALateAnswer(t *testing.T) {
	bridge := bridgeForTest(30 * time.Millisecond)
	if bridge.Pin("Bank", "user", true) != nil {
		t.Fatal("the timed-out prompt returned an entry")
	}
	if bridge.Prompt() != nil {
		t.Fatal("the prompt is still open after the timeout")
	}

	late := secret.NewText([]byte("PIN-LATE"))
	bridge.answerPinText(late, false)
	assertTextWiped(t, late)
	if bridge.Prompt() != nil {
		t.Fatal("the late answer reopened the prompt")
	}
}

// TestEveryDiscardPathWipesTheAnswerSecret covers every place where the bridge
// creates a secret.Text but does not hand it to the waiting worker.
func TestEveryDiscardPathWipesTheAnswerSecret(t *testing.T) {
	t.Run("no open prompt", func(t *testing.T) {
		bridge := bridgeForTest(time.Second)
		text := secret.NewText([]byte("PIN-SECRET"))
		bridge.answerPinText(text, false)
		assertTextWiped(t, text)

		tan := secret.NewText([]byte("TAN-SECRET"))
		bridge.answerTanText(tan)
		assertTextWiped(t, tan)
	})

	t.Run("wrong prompt type", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *secret.Text, 1)
		go func() {
			result <- bridge.Tan(banksync.TanChallenge{Message: "Enter the TAN"})
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Tan
		})

		text := secret.NewText([]byte("PIN-SECRET"))
		bridge.answerPinText(text, false)
		assertTextWiped(t, text)

		bridge.CancelPending()
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("the cancelled TAN worker did not finish")
		}
	})

	t.Run("after close", func(t *testing.T) {
		bridge := bridgeForTest(time.Second)
		bridge.CancelPending()
		text := secret.NewText([]byte("PIN-SECRET"))
		bridge.answerPinText(text, false)
		assertTextWiped(t, text)

		tan := secret.NewText([]byte("TAN-SECRET"))
		bridge.answerTanText(tan)
		assertTextWiped(t, tan)
	})

	t.Run("empty pin", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *banksync.PinEntry, 1)
		go func() {
			result <- bridge.Pin("Bank", "user", true)
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Pin
		})

		text := secret.NewText(nil)
		bridge.answerPinText(text, false)
		assertTextWiped(t, text)
		select {
		case entry := <-result:
			if entry != nil {
				t.Fatal("an empty PIN returned an entry")
			}
		case <-time.After(time.Second):
			t.Fatal("the worker was not unblocked")
		}
	})

	t.Run("pending answer cleared by CancelPending", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		openPromptForTest(bridge, Pin)

		text := secret.NewText([]byte("PIN-SECRET"))
		bridge.answerPinText(text, false)
		bridge.CancelPending()
		assertTextWiped(t, text)
		if bridge.Prompt() != nil {
			t.Fatal("CancelPending left the prompt open")
		}
	})

	t.Run("answer replaces an unconsumed earlier answer", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		openPromptForTest(bridge, Pin)

		first := secret.NewText([]byte("PIN-FIRST"))
		bridge.answerPinText(first, false)
		second := secret.NewText([]byte("PIN-SECOND"))
		bridge.answerPinText(second, false)
		assertTextWiped(t, first)
		if second.Len() == 0 {
			t.Fatal("the replacing answer was wiped before it was consumed")
		}

		bridge.CancelPending()
		assertTextWiped(t, second)
	})

	t.Run("answer reset at the start of ask", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		stale := secret.NewText([]byte("PIN-STALE"))
		entry := banksync.NewPinEntry(stale, false)
		bridge.mu.Lock()
		bridge.answer = &answer{kind: answerPin, pin: &entry}
		bridge.mu.Unlock()

		result := make(chan *banksync.PinEntry, 1)
		go func() {
			result <- bridge.Pin("Bank", "user", true)
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Pin
		})
		assertTextWiped(t, stale)

		bridge.CancelPending()
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("the worker did not finish")
		}
	})
}

func TestEachAnswerTypeReachesTheMatchingMethod(t *testing.T) {
	t.Run("pin", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *banksync.PinEntry, 1)
		go func() {
			result <- bridge.Pin("Bank", "user", true)
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Pin && prompt.BankName == "Bank" &&
				prompt.UserID == "user" && prompt.OfferRemember
		})
		bridge.AnswerPin([]byte("12345"), true)
		entry := <-result
		if got := pinText(t, entry); got != "12345" {
			t.Fatalf("PIN = %q", got)
		}
		if !entry.Remember() {
			t.Fatal("Remember = false")
		}
		entry.Pin().Wipe()
	})

	t.Run("tan method", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *string, 1)
		options := []banksync.TanMethod{
			{ID: "942", Name: "chipTAN optisch"},
			{ID: "921", Name: "pushTAN"},
		}
		go func() {
			result <- bridge.SelectTanMethod(options)
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == ChooseTanMethod && len(prompt.TanMethods) == 2
		})
		choice := "942"
		bridge.AnswerChoice(&choice)
		select {
		case got := <-result:
			if got == nil || *got != "942" {
				t.Fatalf("choice = %v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("the worker did not receive the choice")
		}
	})

	t.Run("tan medium", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *string, 1)
		go func() {
			result <- bridge.SelectTanMedium([]string{"phone", "generator"})
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == ChooseTanMedium && len(prompt.TanMediums) == 2
		})
		choice := "phone"
		bridge.AnswerChoice(&choice)
		select {
		case got := <-result:
			if got == nil || *got != "phone" {
				t.Fatalf("choice = %v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("the worker did not receive the choice")
		}
	})

	t.Run("tan", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *secret.Text, 1)
		challenge := banksync.TanChallenge{Message: "Bitte TAN", FlickerCode: stringPointer("123")}
		go func() {
			result <- bridge.Tan(challenge)
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Tan && prompt.Challenge.Message == "Bitte TAN"
		})
		bridge.AnswerTan([]byte("999999"))
		select {
		case answer := <-result:
			if got := secretText(t, answer); got != "999999" {
				t.Fatalf("TAN = %q", got)
			}
			answer.Wipe()
		case <-time.After(time.Second):
			t.Fatal("the worker did not receive the TAN")
		}
	})

	t.Run("decoupled", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan bool, 1)
		go func() {
			result <- bridge.ConfirmDecoupled("Confirm in the app")
		}()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Decoupled && prompt.Message == "Confirm in the app"
		})
		bridge.AnswerDecoupled(true)
		select {
		case confirmed := <-result:
			if !confirmed {
				t.Fatal("ConfirmDecoupled = false")
			}
		case <-time.After(time.Second):
			t.Fatal("the worker did not receive the confirmation")
		}
	})
}

func TestInfoIgnoresBlankTextAndClearInfoNotifies(t *testing.T) {
	var changes atomic.Int64
	bridge := NewPromptBridge(time.Second, func() { changes.Add(1) })

	bridge.Info("   ")
	bridge.Info("\t\n")
	if bridge.CurrentInfo() != nil {
		t.Fatal("blank info was stored")
	}
	if changes.Load() != 0 {
		t.Fatalf("blank info fired onChange %d times", changes.Load())
	}

	bridge.Info("Waiting for the bank")
	if got := stringValue(bridge.CurrentInfo()); got != "Waiting for the bank" {
		t.Fatalf("info = %q", got)
	}
	if changes.Load() != 1 {
		t.Fatalf("onChange fired %d times, want 1", changes.Load())
	}
	// As in Rust, every non-blank Info fires onChange, even with the same text.
	bridge.Info("Waiting for the bank")
	if changes.Load() != 2 {
		t.Fatalf("repeated info fired onChange %d times in all, want 2", changes.Load())
	}
	bridge.ClearInfo()
	if bridge.CurrentInfo() != nil {
		t.Fatal("ClearInfo left the info in place")
	}
	if changes.Load() != 3 {
		t.Fatalf("onChange fired %d times, want 3", changes.Load())
	}
}

func TestBankingStateCloneIsDeep(t *testing.T) {
	serverURL := "https://fints.example/pintan"
	syncFrom, ok := civil.New(2026, time.January, 2)
	if !ok {
		t.Fatal("civil.New failed")
	}
	lastSync := time.Now().UTC()
	errorText := "error"
	message := "message"
	step := "step"
	bankID := model.BankConnectionID(7)
	state := &BankingState{
		Language:       i18n.En,
		Error:          &errorText,
		Message:        &message,
		SyncingBank:    &bankID,
		SyncStep:       &step,
		SyncAll:        &SyncAllProgress{Position: 1, Total: 2, BankName: "Bank"},
		CanRememberPin: true,
		Banks: []BankItem{{
			ID:               bankID,
			BankName:         "Bank",
			BLZ:              "37040044",
			UserID:           "user",
			ServerURL:        &serverURL,
			Offline:          false,
			IncludeInSyncAll: true,
			SyncFrom:         &syncFrom,
			LastSync:         &lastSync,
			HasSavedPin:      true,
		}},
	}

	cloned := state.Clone()
	if cloned == state {
		t.Fatal("Clone returned the same pointer")
	}
	if cloned.Banks[0].ServerURL == state.Banks[0].ServerURL {
		t.Fatal("ServerURL pointers are shared")
	}
	if cloned.Banks[0].SyncFrom == state.Banks[0].SyncFrom {
		t.Fatal("SyncFrom pointers are shared")
	}
	if cloned.Banks[0].LastSync == state.Banks[0].LastSync {
		t.Fatal("LastSync pointers are shared")
	}
	if cloned.Error == state.Error || cloned.Message == state.Message || cloned.SyncStep == state.SyncStep {
		t.Fatal("message pointers are shared")
	}
	if cloned.SyncingBank == state.SyncingBank || cloned.SyncAll == state.SyncAll {
		t.Fatal("progress pointers are shared")
	}

	*cloned.Error = "changed"
	*cloned.Message = "changed"
	*cloned.SyncStep = "changed"
	*cloned.SyncingBank = 9
	cloned.SyncAll.BankName = "changed"
	*cloned.Banks[0].ServerURL = "https://other.example"
	*cloned.Banks[0].SyncFrom = syncFrom.AddDays(1)
	cloned.Banks[0].LastSync = nil
	cloned.Banks[0].BankName = "changed"
	cloned.Banks = append(cloned.Banks, BankItem{ID: 8})
	cloned.CanRememberPin = false

	if *state.Error != "error" || *state.Message != "message" || *state.SyncStep != "step" {
		t.Fatal("mutating the clone changed the original's strings")
	}
	if *state.SyncingBank != 7 {
		t.Fatal("mutating the clone changed the original's SyncingBank")
	}
	if state.SyncAll.BankName != "Bank" {
		t.Fatal("mutating the clone changed the original's SyncAll")
	}
	if *state.Banks[0].ServerURL != serverURL {
		t.Fatal("mutating the clone changed the original's ServerURL")
	}
	if *state.Banks[0].SyncFrom != syncFrom {
		t.Fatal("mutating the clone changed the original's SyncFrom")
	}
	if state.Banks[0].LastSync == nil {
		t.Fatal("mutating the clone cleared the original's LastSync")
	}
	if state.Banks[0].BankName != "Bank" {
		t.Fatal("mutating the clone changed the original's bank name")
	}
	if len(state.Banks) != 1 {
		t.Fatal("mutating the clone changed the original's bank slice")
	}
	if !state.CanRememberPin {
		t.Fatal("mutating the clone changed the original's CanRememberPin")
	}
}

func TestTheCallerMayWipeItsBufferRightAfterAnswering(t *testing.T) {
	textOf := func(t *testing.T, text *secret.Text) string {
		t.Helper()
		var got string
		if err := text.Use(func(b []byte) error { got = string(b); return nil }); err != nil {
			t.Fatalf("the delivered secret is unusable: %v", err)
		}
		return got
	}

	t.Run("pin", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *banksync.PinEntry, 1)
		go func() { result <- bridge.Pin("Bank", "user", true) }()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Pin
		})

		buffer := []byte("12345")
		bridge.AnswerPin(buffer, true)
		clear(buffer)

		select {
		case entry := <-result:
			if entry == nil {
				t.Fatal("no PIN arrived")
			}
			defer entry.Pin().Wipe()
			if got := textOf(t, entry.Pin()); got != "12345" {
				t.Fatalf("PIN = %q after the caller wiped its buffer", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the worker did not finish")
		}
	})

	t.Run("tan", func(t *testing.T) {
		bridge := bridgeForTest(5 * time.Second)
		result := make(chan *secret.Text, 1)
		go func() { result <- bridge.Tan(banksync.TanChallenge{Message: "Enter the TAN"}) }()
		waitUntil(t, func() bool {
			prompt := bridge.Prompt()
			return prompt != nil && prompt.Kind == Tan
		})

		buffer := []byte("654321")
		bridge.AnswerTan(buffer)
		clear(buffer)

		select {
		case tan := <-result:
			if tan == nil {
				t.Fatal("no TAN arrived")
			}
			defer tan.Wipe()
			if got := textOf(t, tan); got != "654321" {
				t.Fatalf("TAN = %q after the caller wiped its buffer", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the worker did not finish")
		}
	})
}
