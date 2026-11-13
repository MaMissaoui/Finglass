// Command finglass-probe runs a read-only FinTS feasibility probe against the
// user's own bank. It is a developer tool, not part of the desktop app.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/fintsprobe"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints/httptransport"
)

// productVersion travels in HKVVB. It is a probe constant, not the app's
// version.
const productVersion = "0.4"

// errNotTerminal reports that no terminal is available for a secret prompt.
var errNotTerminal = errors.New("not a terminal")

func main() {
	inputs, inputErrors := fintsprobe.ParseInputs(os.LookupEnv)
	if len(inputErrors) > 0 {
		for _, inputError := range inputErrors {
			fmt.Fprintln(os.Stderr, inputError.Error())
		}
		os.Exit(2)
	}

	terminal := probeTerminal{}

	// The product id is optional in the environment, so it can stay out of
	// shell history. It is read without echo and kept only in inputs.
	if inputs.ProductID == "" {
		raw, err := terminal.ReadSecret("Product id: ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Product id: cannot be read from the terminal")
			os.Exit(2)
		}
		inputs.ProductID = string(raw)
		wipe(raw)
		if !fintsprobe.ValidProductID(inputs.ProductID) {
			fmt.Fprintln(os.Stderr, "FINGLASS_FINTS_PRODUCT_ID: must be 1 to 30 ASCII letters or digits")
			os.Exit(2)
		}
	}

	rawPIN, err := terminal.ReadSecret("PIN: ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "PIN: cannot be read from the terminal")
		os.Exit(2)
	}
	pin := secret.NewText(rawPIN)
	wipe(rawPIN)

	transport, err := httptransport.New(inputs.URL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FINGLASS_PROBE_URL: invalid URL")
		os.Exit(2)
	}

	// Ctrl-C cancels the dialog cleanly (it still sends HKEND) instead of
	// killing the process mid-dialog.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := fintsprobe.Run(ctx, fintsprobe.Env{
		Inputs:         inputs,
		PIN:            pin,
		Transport:      transport,
		Terminal:       terminal,
		Today:          civil.FromTime(time.Now()),
		ProductVersion: productVersion,
	})
	stop()
	os.Exit(code)
}

// probeTerminal reads secrets without echo from /dev/tty, or from stdin when
// it is a terminal, and writes the probe's lines to stdout.
type probeTerminal struct{}

// Println writes one line to stdout.
func (probeTerminal) Println(line string) {
	_, _ = fmt.Fprintln(os.Stdout, line)
}

// ReadSecret reads one line without echo. The caller owns and wipes the bytes.
func (probeTerminal) ReadSecret(prompt string) ([]byte, error) {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer func() { _ = tty.Close() }()
		if term.IsTerminal(int(tty.Fd())) {
			return readPassword(tty, int(tty.Fd()), prompt)
		}
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return readPassword(os.Stderr, int(os.Stdin.Fd()), prompt)
	}
	return nil, errNotTerminal
}

func readPassword(output io.Writer, fd int, prompt string) ([]byte, error) {
	if _, err := fmt.Fprint(output, prompt); err != nil {
		return nil, err
	}
	value, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(output)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
