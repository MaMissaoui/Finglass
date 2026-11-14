// Bridges blocking banksync.BankingInteraction calls from a worker goroutine to
// prompts the UI answers.
//
// This is the Go port of FinDesk's UiBankingInteraction and Rust's
// banking::bridge, minus ConfirmPayee (Finglass has no payment code). The
// worker blocks inside PromptBridge.ask while the prompt is open; the UI reads a
// copy through Prompt() and answers. No PIN or TAN ever lives in a published
// prompt: secrets travel only through the answer methods, and the bridge's lock
// is released while the worker waits.
package banking

import (
	"strings"
	"sync"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// PromptKind names the Rust BankingPrompt variant a prompt carries.
type PromptKind int

const (
	// Pin asks for the online-banking PIN.
	Pin PromptKind = iota
	// ChooseTanMethod asks the user to pick a TAN procedure.
	ChooseTanMethod
	// ChooseTanMedium asks the user to pick a TAN medium.
	ChooseTanMedium
	// Tan asks for a TAN for a challenge.
	Tan
	// Decoupled asks the user to confirm in the bank's app.
	Decoupled
	// ProductID asks for the FinTS product registration id without echo. It
	// carries no bank data.
	ProductID
)

// BankingPrompt is a question from the bank waiting for the user. It never
// carries a secret.
//
// Only the fields of the variant named by Kind are meaningful; the other fields
// stay at their zero values.
//
// ID is the bridge's sequence number for this prompt. Every answer must carry
// it: an answer meant for an earlier prompt of the same kind can then never
// answer the next one. Rust has no such token; this is a deliberate deviation.
type BankingPrompt struct {
	// ID is the bridge's prompt sequence number.
	ID uint64
	// Kind says which Rust variant this prompt is.
	Kind PromptKind

	// BankName is the bank's display name (Pin).
	BankName string
	// UserID is the online-banking login (Pin).
	UserID string
	// OfferRemember says whether the UI may offer to remember the PIN (Pin).
	OfferRemember bool

	// TanMethods are the bank's TAN procedures (ChooseTanMethod).
	TanMethods []banksync.TanMethod
	// TanMediums are the bank's TAN media (ChooseTanMedium).
	TanMediums []string

	// Challenge is the TAN challenge (Tan).
	Challenge banksync.TanChallenge

	// Message is the decoupled confirmation text (Decoupled).
	Message string
}

type answerKind int

const (
	answerPin answerKind = iota
	answerChoice
	answerTan
	answerDecoupled
	answerProductID
)

// answer is the only place a secret ever lives, and it is consumed immediately.
type answer struct {
	kind      answerKind
	pin       *banksync.PinEntry
	choice    *string
	tan       *secret.Text
	decoupled bool
	productID *secret.Text
}

// wipe destroys every secret this answer still holds.
func (a *answer) wipe() {
	if a == nil {
		return
	}
	if a.tan != nil {
		a.tan.Wipe()
		a.tan = nil
	}
	if a.pin != nil {
		if pin := a.pin.Pin(); pin != nil {
			pin.Wipe()
		}
		a.pin = nil
	}
	if a.productID != nil {
		a.productID.Wipe()
		a.productID = nil
	}
}

// PromptBridge turns blocking banking callbacks into a prompt the UI can read
// and answer.
//
// It is safe for concurrent use and is never called under the profile mutex:
// the worker blocks inside ask while the UI reads Prompt() and answers.
//
// onChange is called whenever the prompt opens or closes or info changes,
// always without holding the bridge's lock, so a callback may call Prompt() or
// CurrentInfo() again.
type PromptBridge struct {
	mu       sync.Mutex
	nextID   uint64
	prompt   *BankingPrompt
	info     *string
	answer   *answer
	closed   bool
	changed  chan struct{}
	timeout  time.Duration
	onChange func()
}

// NewPromptBridge builds a bridge. A waiting prompt times out after timeout and
// counts as cancelled.
func NewPromptBridge(timeout time.Duration, onChange func()) *PromptBridge {
	if onChange == nil {
		onChange = func() {}
	}
	return &PromptBridge{
		changed:  make(chan struct{}),
		timeout:  timeout,
		onChange: onChange,
	}
}

// Prompt returns the open prompt, if any. The result is a deep copy, so the
// caller may keep or modify it.
func (b *PromptBridge) Prompt() *BankingPrompt {
	b.mu.Lock()
	defer b.mu.Unlock()
	return clonePrompt(b.prompt)
}

// CurrentInfo returns the bank's current informational message, if any.
//
// Rust names this getter `info()`, but Go has no method overloading and the
// BankingInteraction method Info(string) takes the same name; only the name
// differs, the behaviour is Rust's.
func (b *PromptBridge) CurrentInfo() *string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return cloneString(b.info)
}

// ClearInfo clears the bank's informational message.
func (b *PromptBridge) ClearInfo() {
	changed := false
	b.mu.Lock()
	if b.info != nil {
		b.info = nil
		changed = true
		b.notifyLocked()
	}
	b.mu.Unlock()
	if changed {
		b.onChange()
	}
}

// AnswerPin answers the open PIN prompt. A nil or empty pin cancels.
//
// The caller owns pin and may wipe it after the call; the bridge copies it into
// a secret.Text and wipes that text unless it is handed to the waiting worker.
// An answer whose id is not the open prompt's id is ignored.
func (b *PromptBridge) AnswerPin(id uint64, pin []byte, remember bool) {
	b.answerPinText(id, secret.NewText(pin), remember)
}

// answerPinText is the body of AnswerPin, split out so tests can hold the
// secret.Text the bridge would have created and verify that it is wiped on every
// discard path.
func (b *PromptBridge) answerPinText(id uint64, text *secret.Text, remember bool) {
	if text == nil || text.Len() == 0 {
		if text != nil {
			text.Wipe()
		}
		b.answerIf(id, func(kind PromptKind) bool { return kind == Pin }, answer{kind: answerPin})
		return
	}
	entry := banksync.NewPinEntry(text, remember)
	b.answerIf(id, func(kind PromptKind) bool { return kind == Pin }, answer{kind: answerPin, pin: &entry})
}

// AnswerChoice answers a TAN-procedure or TAN-medium prompt. A nil choice
// cancels. An answer whose id is not the open prompt's id is ignored.
func (b *PromptBridge) AnswerChoice(id uint64, choice *string) {
	b.answerIf(
		id,
		func(kind PromptKind) bool { return kind == ChooseTanMethod || kind == ChooseTanMedium },
		answer{kind: answerChoice, choice: choice},
	)
}

// AnswerTan answers a TAN prompt. A nil tan cancels.
//
// The caller owns tan and may wipe it after the call; the bridge copies it into
// a secret.Text and wipes that text unless it is handed to the waiting worker.
// An answer whose id is not the open prompt's id is ignored.
func (b *PromptBridge) AnswerTan(id uint64, tan []byte) {
	var text *secret.Text
	if tan != nil {
		text = secret.NewText(tan)
	}
	b.answerTanText(id, text)
}

// answerTanText is the body of AnswerTan, split out so tests can hold the
// secret.Text the bridge would have created and verify that it is wiped on every
// discard path.
func (b *PromptBridge) answerTanText(id uint64, text *secret.Text) {
	b.answerIf(id, func(kind PromptKind) bool { return kind == Tan }, answer{kind: answerTan, tan: text})
}

// AnswerProductID answers the product-id prompt. An empty value cancels. The
// input bytes are wiped after the bridge has copied them, and an answer whose
// id is not the open prompt's id is ignored.
func (b *PromptBridge) AnswerProductID(id uint64, value []byte) {
	if len(value) == 0 {
		b.answerIf(
			id,
			func(kind PromptKind) bool { return kind == ProductID },
			answer{kind: answerProductID},
		)
		return
	}
	text := secret.NewText(value)
	for index := range value {
		value[index] = 0
	}
	b.answerIf(
		id,
		func(kind PromptKind) bool { return kind == ProductID },
		answer{kind: answerProductID, productID: text},
	)
}

// AnswerDecoupled answers a decoupled-confirmation prompt. An answer whose id is
// not the open prompt's id is ignored.
func (b *PromptBridge) AnswerDecoupled(id uint64, confirmed bool) {
	b.answerIf(
		id,
		func(kind PromptKind) bool { return kind == Decoupled },
		answer{kind: answerDecoupled, decoupled: confirmed},
	)
}

// DismissDecoupled answers only a pending decoupled prompt with false, as the
// connection test does when its polling ends before the user acts. A late
// answer is ignored by the ordinary id and kind check.
func (b *PromptBridge) DismissDecoupled() {
	b.mu.Lock()
	if b.closed || b.prompt == nil || b.prompt.Kind != Decoupled {
		b.mu.Unlock()
		return
	}
	id := b.prompt.ID
	b.mu.Unlock()
	b.AnswerDecoupled(id, false)
}

// CancelPending cancels the open prompt and every later one. The worker is
// unblocked at once and every new ask returns nil; the bridge is closed for
// good. The shell calls this on a profile switch or when the window closes.
func (b *PromptBridge) CancelPending() {
	var pending *answer
	wasOpen := false
	b.mu.Lock()
	b.closed = true
	pending = b.answer
	b.answer = nil
	if b.prompt != nil {
		b.prompt = nil
		wasOpen = true
	}
	b.notifyLocked()
	b.mu.Unlock()

	pending.wipe()
	if wasOpen {
		b.onChange()
	}
}

// answerIf sets an answer only when the open prompt has the expected id and
// kind; otherwise the answer's secret is wiped and the answer is ignored.
func (b *PromptBridge) answerIf(id uint64, matches func(PromptKind) bool, value answer) {
	b.mu.Lock()
	if b.closed || b.prompt == nil || b.prompt.ID != id || !matches(b.prompt.Kind) {
		b.mu.Unlock()
		value.wipe()
		return
	}
	previous := b.answer
	b.answer = &value
	b.notifyLocked()
	b.mu.Unlock()

	previous.wipe()
}

// ask publishes prompt, waits for an answer or a timeout, then closes the
// prompt. Only one prompt is open at a time; a concurrent ask is refused instead
// of replacing the open one.
func (b *PromptBridge) ask(prompt BankingPrompt) *answer {
	// The deadline is fixed once, at the start; Info changes do not extend it.
	deadline := time.Now().Add(b.timeout)

	b.mu.Lock()
	if b.closed || b.prompt != nil {
		b.mu.Unlock()
		return nil
	}
	previous := b.answer
	b.answer = nil
	b.nextID++
	prompt.ID = b.nextID
	published := clonePrompt(&prompt)
	b.prompt = published
	b.notifyLocked()
	b.mu.Unlock()

	previous.wipe()
	b.onChange()

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()

	for {
		b.mu.Lock()
		if b.closed {
			wasOpen := b.prompt != nil
			b.prompt = nil
			pending := b.answer
			b.answer = nil
			b.notifyLocked()
			b.mu.Unlock()

			pending.wipe()
			if wasOpen {
				b.onChange()
			}
			return nil
		}
		if pending := b.answer; pending != nil {
			b.answer = nil
			b.prompt = nil
			b.notifyLocked()
			b.mu.Unlock()
			b.onChange()
			return pending
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			b.prompt = nil
			pending := b.answer
			b.answer = nil
			b.notifyLocked()
			b.mu.Unlock()

			pending.wipe()
			b.onChange()
			return nil
		}
		changed := b.changed
		b.mu.Unlock()

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(remaining)

		select {
		case <-changed:
		case <-timer.C:
		}
	}
}

// notifyLocked closes and replaces the change channel. The caller holds the
// lock.
func (b *PromptBridge) notifyLocked() {
	close(b.changed)
	b.changed = make(chan struct{})
}

// Pin asks for the online-banking PIN.
func (b *PromptBridge) Pin(bankName, userID string, offerRemember bool) *banksync.PinEntry {
	result := b.ask(BankingPrompt{
		Kind:          Pin,
		BankName:      bankName,
		UserID:        userID,
		OfferRemember: offerRemember,
	})
	if result == nil {
		return nil
	}
	if result.kind != answerPin {
		result.wipe()
		return nil
	}
	return result.pin
}

// SelectTanMethod picks one of the TAN procedures the bank offers.
func (b *PromptBridge) SelectTanMethod(options []banksync.TanMethod) *string {
	result := b.ask(BankingPrompt{
		Kind:       ChooseTanMethod,
		TanMethods: append([]banksync.TanMethod(nil), options...),
	})
	if result == nil {
		return nil
	}
	if result.kind != answerChoice {
		result.wipe()
		return nil
	}
	return result.choice
}

// SelectTanMedium picks a TAN medium.
func (b *PromptBridge) SelectTanMedium(options []string) *string {
	result := b.ask(BankingPrompt{
		Kind:       ChooseTanMedium,
		TanMediums: append([]string(nil), options...),
	})
	if result == nil {
		return nil
	}
	if result.kind != answerChoice {
		result.wipe()
		return nil
	}
	return result.choice
}

// Tan enters a TAN for a challenge.
func (b *PromptBridge) Tan(challenge banksync.TanChallenge) *secret.Text {
	result := b.ask(BankingPrompt{
		Kind:      Tan,
		Challenge: cloneChallenge(challenge),
	})
	if result == nil {
		return nil
	}
	if result.kind != answerTan {
		result.wipe()
		return nil
	}
	return result.tan
}

// AskProductID asks for the FinTS product registration id without echo. The
// prompt carries no bank data. A nil answer cancels.
func (b *PromptBridge) AskProductID() *secret.Text {
	result := b.ask(BankingPrompt{Kind: ProductID})
	if result == nil {
		return nil
	}
	if result.kind != answerProductID {
		result.wipe()
		return nil
	}
	return result.productID
}

// ConfirmDecoupled handles decoupled procedures (for example pushTAN).
func (b *PromptBridge) ConfirmDecoupled(message string) bool {
	result := b.ask(BankingPrompt{
		Kind:    Decoupled,
		Message: message,
	})
	if result == nil {
		return false
	}
	if result.kind != answerDecoupled {
		result.wipe()
		return false
	}
	return result.decoupled
}

// Info shows an informational message from the bank. Blank text is ignored;
// any other text replaces the current info and calls onChange, as Rust does.
func (b *PromptBridge) Info(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	b.mu.Lock()
	b.info = cloneString(&message)
	b.notifyLocked()
	b.mu.Unlock()
	b.onChange()
}

func clonePrompt(prompt *BankingPrompt) *BankingPrompt {
	if prompt == nil {
		return nil
	}
	copied := *prompt
	if prompt.TanMethods != nil {
		copied.TanMethods = make([]banksync.TanMethod, len(prompt.TanMethods))
		for index, method := range prompt.TanMethods {
			copied.TanMethods[index] = cloneTanMethod(method)
		}
	}
	if prompt.TanMediums != nil {
		copied.TanMediums = append([]string(nil), prompt.TanMediums...)
	}
	copied.Challenge = cloneChallenge(prompt.Challenge)
	return &copied
}

func cloneTanMethod(method banksync.TanMethod) banksync.TanMethod {
	copied := method
	copied.MediumName = cloneString(method.MediumName)
	copied.TANFormat = cloneString(method.TANFormat)
	return copied
}

func cloneChallenge(challenge banksync.TanChallenge) banksync.TanChallenge {
	copied := challenge
	if challenge.Image != nil {
		copied.Image = append([]byte(nil), challenge.Image...)
	}
	copied.ImageMIMEType = cloneString(challenge.ImageMIMEType)
	copied.FlickerCode = cloneString(challenge.FlickerCode)
	return copied
}

// The bridge is the interaction the banking code calls from its worker.
var _ banksync.BankingInteraction = (*PromptBridge)(nil)
