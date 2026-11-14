package banking

import (
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestAskProductIDCopiesTheAnswerWipesTheInputAndChecksTheID(t *testing.T) {
	bridge := NewPromptBridge(time.Second, nil)
	result := make(chan *secret.Text, 1)
	go func() { result <- bridge.AskProductID() }()

	prompt := waitPromptForTest(t, bridge, ProductID)
	if prompt.BankName != "" || prompt.UserID != "" {
		t.Fatalf("product prompt carries bank data: %+v", prompt)
	}

	bridge.AnswerProductID(prompt.ID+1, []byte("LATE"))
	bridge.AnswerProductID(prompt.ID, []byte("INVENTED123"))
	text := <-result
	if text == nil || text.IsEmpty() {
		t.Fatal("AskProductID returned no product id")
	}
	var value string
	_ = text.Use(func(bytes []byte) error {
		value = string(bytes)
		return nil
	})
	if value != "INVENTED123" {
		t.Fatalf("product id = %q", value)
	}
	text.Wipe()
}

func TestAnswerProductIDWipesTheInputBytes(t *testing.T) {
	bridge := NewPromptBridge(time.Second, nil)
	result := make(chan *secret.Text, 1)
	go func() { result <- bridge.AskProductID() }()
	prompt := waitPromptForTest(t, bridge, ProductID)

	raw := []byte("INVENTED456")
	bridge.AnswerProductID(prompt.ID, raw)
	for index, value := range raw {
		if value != 0 {
			t.Fatalf("byte %d = %d after AnswerProductID, want 0", index, value)
		}
	}
	text := <-result
	if text == nil || text.IsEmpty() {
		t.Fatal("AskProductID returned no product id")
	}
	text.Wipe()
}

func TestAnEmptyProductIDCancelsAndDismissDecoupledOnlyAnswersDecoupled(t *testing.T) {
	bridge := NewPromptBridge(time.Second, nil)

	decoupled := make(chan bool, 1)
	go func() { decoupled <- bridge.ConfirmDecoupled("Invented challenge") }()
	prompt := waitPromptForTest(t, bridge, Decoupled)
	bridge.DismissDecoupled()
	if <-decoupled {
		t.Fatal("DismissDecoupled confirmed the prompt")
	}

	product := make(chan *secret.Text, 1)
	go func() { product <- bridge.AskProductID() }()
	prompt = waitPromptForTest(t, bridge, ProductID)
	bridge.DismissDecoupled()
	if text := <-product; text != nil {
		t.Fatal("DismissDecoupled answered a product-id prompt")
	}

	go func() { product <- bridge.AskProductID() }()
	prompt = waitPromptForTest(t, bridge, ProductID)
	bridge.AnswerProductID(prompt.ID, nil)
	if text := <-product; text != nil {
		t.Fatal("an empty product id did not cancel")
	}
}

func waitPromptForTest(t *testing.T, bridge *PromptBridge, kind PromptKind) BankingPrompt {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if prompt := bridge.Prompt(); prompt != nil && prompt.Kind == kind {
			return *prompt
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for prompt kind %d", kind)
		}
		time.Sleep(time.Millisecond)
	}
}
