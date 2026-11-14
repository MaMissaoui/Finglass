// Package connectiontest runs the read-only FinTS probe against one stored bank
// login and returns a result the app can show.
//
// It never imports, stores or changes anything: no account, balance, booking,
// TAN method, capability, system id or remembered PIN. The result carries a
// localized reason, four-digit bank codes, counts and the bank's warning/error
// texts for on-screen display; the redacted trace is kept in memory by the
// caller.
package connectiontest

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/fintsprobe"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
	"github.com/MaMissaoui/Finglass/internal/fints/httptransport"
	"github.com/MaMissaoui/Finglass/internal/fints/trace"
)

// Other names an outcome that is not one of fintsprobe's NO-GO categories.
// They contain no bank value.
const (
	// OtherInvalidLogin is invalid login data (bank code, user id or URL).
	OtherInvalidLogin = "InvalidLogin"
	// OtherProductIDMissing means no FinTS product id was supplied.
	OtherProductIDMissing = "ProductIDMissing"
	// OtherProductIDInvalid means the supplied FinTS product id is malformed.
	OtherProductIDInvalid = "ProductIDInvalid"
	// OtherCancelled means the user cancelled a prompt or the dialog.
	OtherCancelled = "Cancelled"
	// OtherTransport means the transport could not be built.
	OtherTransport = "Transport"
)

// TransportFactory builds the transport for one server URL. Production uses
// [DefaultTransportFactory]; tests inject a fake and never open a socket.
type TransportFactory func(serverURL string) (fints.Transport, error)

// DefaultTransportFactory wraps [httptransport.New].
func DefaultTransportFactory(serverURL string) (fints.Transport, error) {
	return httptransport.New(serverURL)
}

// Input is everything one connection test needs.
type Input struct {
	// BankName is the bank's display name, used only for the PIN prompt.
	BankName string
	// BLZ is the German bank code.
	BLZ string
	// UserID is the online-banking login.
	UserID string
	// ServerURL is the bank's FinTS server URL.
	ServerURL string
	// ProductID is the FinTS product id. The caller passes ownership to Run,
	// which wipes it on every path.
	ProductID *secret.Text
	// Interaction asks the user for the PIN and TAN and shows notices.
	Interaction banksync.BankingInteraction
	// SavedPin returns the PIN the user saved for this bank, or nil. When it is
	// set and returns a value, the PIN prompt is skipped. It is called only at
	// the prompt, after every early check, and never stores anything.
	SavedPin func() *secret.Text
	// Factory builds the transport. Nil uses [DefaultTransportFactory].
	Factory TransportFactory
	// Sleeper waits between decoupled polls. Nil uses the dialog's default.
	Sleeper fints.Sleeper
	// Today is the statement period's end date.
	Today civil.Date
	// ProductVersion is the HKVVB product version.
	ProductVersion string
	// Language localizes the notice the adapter shows. Its zero value is
	// English.
	Language i18n.Language
}

// Outcome is the result of one connection test. It contains no account
// identifier and no secret. Its bank messages are the bank's own warning/error
// texts: show them on screen only and never log, store, audit or trace them.
type Outcome struct {
	// Go reports whether the probe saw a usable read-only login.
	Go bool
	// Kind is the NO-GO category.
	Kind fintsprobe.NoGoKind
	// Other is the kind name for Kind == NoGoOther, or one of the Other
	// constants above.
	Other string
	// Codes are the bank's four-digit return codes.
	Codes []string
	// Messages are the bank's warning and error texts for on-screen display.
	Messages []BankMessage
	// Probed is the number of accounts the probe attempted.
	Probed int
	// Skipped is the number of accounts the probe skipped.
	Skipped int
	// Cancelled reports a user cancellation.
	Cancelled bool
	// StrongSync reports that the probe used the strong-sync fallback.
	StrongSync bool
	// StrongSyncTAN is how the bank asked for the TAN in the strong sync:
	// "", "typed" or "decoupled".
	StrongSyncTAN string
	// UsedSavedPin reports that the test used the PIN the user saved earlier
	// instead of asking for it. The test still stored nothing.
	UsedSavedPin bool
	// Trace is the redacted trace of the exchanges, in memory only.
	Trace []byte
}

// String prints the decision, kind, counts and message count. It never prints
// the trace, a bank value or a bank message text.
func (o Outcome) String() string {
	decision := fmt.Sprintf("NO-GO (%d:%s)", o.Kind, o.Other)
	if o.Go {
		decision = "GO"
	}
	return fmt.Sprintf(
		"connectiontest.Outcome{decision:%s codes:%d messages:%d probed:%d skipped:%d cancelled:%t trace:%t}",
		decision, len(o.Codes), len(o.Messages), o.Probed, o.Skipped, o.Cancelled, len(o.Trace) > 0,
	)
}

// GoString prints the same masked form as String.
func (o Outcome) GoString() string {
	return o.String()
}

// Format writes the masked form for every fmt verb.
func (o Outcome) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(o.String()))
}

// Run performs one read-only probe against input's bank login.
//
// It validates the inputs through [fintsprobe.ParseInputs], uses a PIN the user
// saved earlier when input.SavedPin supplies one, else asks for it with
// offerRemember false, builds the transport, records a redacted trace and runs
// [fintsprobe.Probe]. It may use a PIN saved earlier, but still never stores a
// PIN, a TAN or the product id, and wipes the callers' secrets on every path.
func Run(ctx context.Context, input Input) Outcome {
	defer input.ProductID.Wipe()

	productID, productOK := readProductID(input.ProductID)
	inputs, inputErrors := fintsprobe.ParseInputs(func(name string) (string, bool) {
		switch name {
		case fintsprobe.ProductIDVar:
			if productOK {
				return productID, true
			}
			return "", false
		case fintsprobe.BLZVar:
			return input.BLZ, true
		case fintsprobe.UserVar:
			return input.UserID, true
		case fintsprobe.URLVar:
			return input.ServerURL, true
		default:
			return "", false
		}
	})
	if len(inputErrors) > 0 {
		for _, inputError := range inputErrors {
			if inputError.Variable == fintsprobe.ProductIDVar {
				return Outcome{Kind: fintsprobe.NoGoOther, Other: OtherProductIDInvalid}
			}
		}
		return Outcome{Kind: fintsprobe.NoGoOther, Other: OtherInvalidLogin}
	}
	if !productOK {
		return Outcome{Kind: fintsprobe.NoGoOther, Other: OtherProductIDMissing}
	}
	if input.Interaction == nil {
		return Outcome{Kind: fintsprobe.NoGoOther, Other: OtherInvalidLogin}
	}

	var pin *secret.Text
	usedSavedPin := false
	if input.SavedPin != nil {
		pin = input.SavedPin()
		usedSavedPin = pin != nil
	}
	if pin == nil {
		entry := input.Interaction.Pin(input.BankName, input.UserID, false)
		if entry == nil || entry.Pin() == nil || entry.IsEmpty() {
			if entry != nil && entry.Pin() != nil {
				entry.Pin().Wipe()
			}
			return Outcome{Kind: fintsprobe.NoGoOther, Other: OtherCancelled, Cancelled: true}
		}
		pin = entry.Pin()
	}
	defer pin.Wipe()

	factory := input.Factory
	if factory == nil {
		factory = DefaultTransportFactory
	}
	transport, err := factory(inputs.URL)
	if err != nil || transport == nil {
		return Outcome{
			Kind:         fintsprobe.NoGoOther,
			Other:        OtherTransport,
			UsedSavedPin: usedSavedPin,
		}
	}
	collector := newMessageCollector(transport)
	recorder := trace.NewRecorder(collector)

	probeContext, cancel := context.WithCancel(ctx)
	defer cancel()
	adapter := newProbeInteraction(input.Interaction, input.Language, cancel)

	result := fintsprobe.Probe(probeContext, fintsprobe.ProbeEnv{
		Inputs:         inputs,
		PIN:            pin,
		Transport:      recorder,
		Sleeper:        input.Sleeper,
		Interaction:    adapter,
		Today:          input.Today,
		ProductVersion: input.ProductVersion,
	})
	adapter.finish()

	var traceBytes bytes.Buffer
	_, _ = recorder.WriteTo(&traceBytes)

	outcome := Outcome{
		Go:            result.Decision.Go,
		Kind:          result.Decision.Reason.Kind,
		Other:         result.Decision.Reason.Other,
		Codes:         append([]string(nil), result.Decision.Reason.Codes...),
		Messages:      collector.snapshot(),
		Probed:        result.Probed,
		Skipped:       result.Skipped,
		Cancelled:     adapter.wasCancelled(),
		StrongSync:    result.Path == fintsprobe.ProbePathStrongSync,
		StrongSyncTAN: result.StrongSyncTAN,
		UsedSavedPin:  usedSavedPin,
		Trace:         traceBytes.Bytes(),
	}
	if !outcome.Go {
		if outcome.Cancelled || (outcome.Kind == fintsprobe.NoGoOther && outcome.Other == OtherCancelled) {
			outcome.Cancelled = true
			outcome.Kind = fintsprobe.NoGoOther
			outcome.Other = OtherCancelled
		}
	}
	return outcome
}

// readProductID copies the product id into a string without keeping the secret
// beyond the call.
func readProductID(productID *secret.Text) (string, bool) {
	if productID == nil {
		return "", false
	}
	value := ""
	if err := productID.Use(func(bytes []byte) error {
		value = string(bytes)
		return nil
	}); err != nil {
		return "", false
	}
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// probeInteraction adapts fintsprobe's Interaction to the app's bridge.
type probeInteraction struct {
	interaction banksync.BankingInteraction
	language    i18n.Language
	cancel      context.CancelFunc
	dismiss     interface{ DismissDecoupled() }

	mu        sync.Mutex
	done      bool
	opened    bool
	cancelled bool
	wait      sync.WaitGroup
}

func newProbeInteraction(
	interaction banksync.BankingInteraction,
	language i18n.Language,
	cancel context.CancelFunc,
) *probeInteraction {
	dismiss, _ := interaction.(interface{ DismissDecoupled() })
	return &probeInteraction{
		interaction: interaction,
		language:    language,
		cancel:      cancel,
		dismiss:     dismiss,
	}
}

// TAN passes the bank's challenge through unchanged. A nil TAN cancels in the
// probe, exactly as the app's TAN dialog does.
func (p *probeInteraction) TAN(_ context.Context, challenge string) (*secret.Text, error) {
	return p.interaction.Tan(banksync.TanChallenge{Message: challenge}), nil
}

// ConfirmInApp shows the decoupled confirmation in a goroutine. A false answer
// while the probe still waits cancels its child context. A late false answer
// after the probe ended is ignored.
func (p *probeInteraction) ConfirmInApp(_ context.Context, challenge string) {
	p.mu.Lock()
	if p.done {
		p.mu.Unlock()
		return
	}
	p.opened = true
	p.wait.Add(1)
	p.mu.Unlock()

	go func() {
		defer p.wait.Done()
		confirmed := p.interaction.ConfirmDecoupled(challenge)

		p.mu.Lock()
		active := !p.done
		if !confirmed && active {
			p.cancelled = true
		}
		p.mu.Unlock()

		if !confirmed && active && p.cancel != nil {
			p.cancel()
		}
	}()
}

// ConfirmRetry shows the retry confirmation synchronously. It blocks until the
// person answers; a cancelled context shows nothing and answers false. It does
// not set cancelled itself: the probe turns a false answer into "Cancelled".
func (p *probeInteraction) ConfirmRetry(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	return p.interaction.ConfirmDecoupled(
		i18n.Text(p.language, i18n.TestConnectionApproveThenRetry),
	)
}

// Notice shows the probe's fixed notices as localized app text.
func (p *probeInteraction) Notice(notice fintsprobe.Notice) {
	switch notice {
	case fintsprobe.NoticeTanMethodUnsupported:
		p.interaction.Info(i18n.Text(p.language, i18n.TestConnectionTanMethodUnsupported))
	case fintsprobe.NoticeStrongSync:
		p.interaction.Info(i18n.Text(p.language, i18n.TestConnectionStrongSyncNotice))
	}
}

// finish marks the probe done, dismisses a still-open confirmation and waits
// for the confirmation goroutine. No goroutine outlives Run.
func (p *probeInteraction) finish() {
	p.mu.Lock()
	p.done = true
	opened := p.opened
	dismiss := p.dismiss
	p.mu.Unlock()

	if !opened || dismiss == nil {
		p.wait.Wait()
		return
	}
	// The goroutine may not have posted its prompt yet, so a single dismiss
	// could miss it and leave Run waiting for the prompt timeout. Dismiss until
	// the goroutine has returned.
	exited := make(chan struct{})
	go func() {
		p.wait.Wait()
		close(exited)
	}()
	for {
		dismiss.DismissDecoupled()
		select {
		case <-exited:
			return
		case <-time.After(dismissRetry):
		}
	}
}

// dismissRetry is how often finish re-dismisses a decoupled prompt that was not
// posted yet.
const dismissRetry = 20 * time.Millisecond

// wasCancelled reports whether a confirmation returned false while the probe
// still waited.
func (p *probeInteraction) wasCancelled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancelled
}
