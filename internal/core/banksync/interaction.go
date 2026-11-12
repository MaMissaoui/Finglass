package banksync

import (
	"fmt"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// TanMethod is one TAN procedure offered by the bank.
type TanMethod struct {
	// ID is the procedure id.
	ID string
	// Name is the name shown to the user.
	Name string
	// MediumName is the TAN medium, when the bank names one.
	MediumName *string
	// TANFormat is the TAN format, when the bank names one.
	TANFormat *string
}

// PinEntry is the PIN the user entered. Remember is honoured only for syncs.
type PinEntry struct {
	pin      *secret.Text
	remember bool
}

// NewPinEntry builds an entry from the user's input.
func NewPinEntry(pin *secret.Text, remember bool) PinEntry {
	return PinEntry{pin: pin, remember: remember}
}

// Pin returns the secret PIN.
func (p PinEntry) Pin() *secret.Text {
	return p.pin
}

// Remember reports whether the user ticked "Remember PIN".
func (p PinEntry) Remember() bool {
	return p.remember
}

// IsEmpty reports whether the user submitted an empty PIN.
func (p PinEntry) IsEmpty() bool {
	return p.pin == nil || p.pin.IsEmpty()
}

// String never contains the PIN.
func (p PinEntry) String() string {
	return fmt.Sprintf("PinEntry(remember=%t)", p.remember)
}

// GoString never contains the PIN.
func (p PinEntry) GoString() string {
	return p.String()
}

// Format never contains the PIN, for every fmt verb.
func (p PinEntry) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(p.String()))
}

// TanChallenge is a TAN challenge from the bank.
type TanChallenge struct {
	// Message is the bank's message (the challenge text).
	Message string
	// Image is a photoTAN or QR-TAN image, when the bank sent one.
	Image []byte
	// ImageMIMEType is the image's MIME type.
	ImageMIMEType *string
	// FlickerCode is a chipTAN flicker code, when the bank sent one.
	FlickerCode *string
}

// String never prints the challenge's data.
func (TanChallenge) String() string {
	return "TanChallenge"
}

// GoString never prints the challenge's data.
func (c TanChallenge) GoString() string {
	return c.String()
}

// Format never prints the challenge's data, for every fmt verb.
func (c TanChallenge) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(c.String()))
}

// BankingInteraction holds the questions the bank asks during a FinTS dialog.
//
// The banking code calls these methods from a background goroutine and blocks
// until the user answers; returning nil (or false) cancels the operation.
// Implementations may therefore be called from another goroutine and must be
// safe for that, for example by dispatching to the UI thread.
type BankingInteraction interface {
	// Pin asks for the online-banking PIN. The PIN is only kept when the user
	// opts in, and only for syncs.
	Pin(bankName, userID string, offerRemember bool) *PinEntry

	// SelectTanMethod picks one of the TAN procedures the bank offers.
	SelectTanMethod(options []TanMethod) *string

	// SelectTanMedium picks a TAN medium (for example a registered phone or TAN
	// generator).
	SelectTanMedium(options []string) *string

	// Tan enters a TAN for a challenge. The answer stays a secret for its whole
	// life.
	Tan(challenge TanChallenge) *secret.Text

	// ConfirmDecoupled handles decoupled procedures (for example pushTAN): the
	// user confirms in the app and the dialog continues.
	ConfirmDecoupled(message string) bool

	// Info shows an informational message from the bank.
	Info(message string)
}
