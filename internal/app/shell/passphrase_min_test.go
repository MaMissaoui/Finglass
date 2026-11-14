package shell

import (
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// TestMinPassphraseLengthIsTheRealCheck ties the DTO's minimum (the live count
// in the passphrase field) to secret.NewPassphrase, counted in NFC code points:
// a decomposed umlaut counts once, as the frontend's passphraseLength does.
func TestMinPassphraseLengthIsTheRealCheck(t *testing.T) {
	decomposed := "Ü" // "Ü" as two code points before NFC
	short := decomposed + strings.Repeat("x", MinPassphraseLength-2)
	exact := decomposed + strings.Repeat("x", MinPassphraseLength-1)

	if passphrase, err := secret.NewPassphrase([]byte(short), secret.New); err == nil {
		passphrase.Wipe()
		t.Fatalf("a passphrase of %d characters was accepted", MinPassphraseLength-1)
	}
	passphrase, err := secret.NewPassphrase([]byte(exact), secret.New)
	if err != nil {
		t.Fatalf("a passphrase of %d characters was refused: %v", MinPassphraseLength, err)
	}
	passphrase.Wipe()
}
