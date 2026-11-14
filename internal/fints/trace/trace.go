package trace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

// traceHeader is the first line of every written trace.
const traceHeader = "# Finglass FinTS trace (redacted: no PIN, TAN, ids, names, amounts, bookings or bank text)"

// Option configures a [Recorder].
type Option func(*Recorder)

// WithClock sets the clock the recorder uses for exchange timestamps.
func WithClock(clock func() time.Time) Option {
	return func(recorder *Recorder) {
		if recorder == nil || recorder.state == nil || clock == nil {
			return
		}
		recorder.state.clock = clock
	}
}

// Recorder wraps a [fints.Transport] and records a redacted trace of every
// exchange.
//
// It keeps only the redacted text, never a request or a response. All methods
// are safe for concurrent use.
type Recorder struct {
	state *recorderState
}

// recorderState is held behind a pointer so a Recorder value and a *Recorder
// share one lock and one set of entries, and formatting a copy cannot print a
// transport or a raw buffer.
type recorderState struct {
	inner fints.Transport
	clock func() time.Time

	mu        sync.Mutex
	started   time.Time
	entries   []string
	exchanges int
}

// NewRecorder wraps inner and records its exchanges. A nil clock uses time.Now.
func NewRecorder(inner fints.Transport, options ...Option) *Recorder {
	state := &recorderState{inner: inner, clock: time.Now}
	recorder := &Recorder{state: state}
	for _, option := range options {
		if option != nil {
			option(recorder)
		}
	}
	state.started = state.clock()
	return recorder
}

// Exchange implements [fints.Transport]. It returns the inner transport's
// response and error unchanged.
func (r *Recorder) Exchange(ctx context.Context, request []byte) ([]byte, error) {
	state := r.recorderState()
	if state == nil {
		return nil, errors.New("trace: nil recorder")
	}

	startedAt := state.clock()
	response, err := state.inner.Exchange(ctx, request)

	requestLines := RedactMessage(request)
	var responseLines []string
	if err == nil {
		responseLines = RedactMessage(response)
	}

	state.mu.Lock()
	state.exchanges++
	number := state.exchanges
	state.entries = append(state.entries, exchangeEntry(number, state.started, startedAt, requestLines, responseLines, err))
	state.mu.Unlock()

	return response, err
}

// Note records a caller note line. The text passes through [RedactNote].
func (r *Recorder) Note(text string) {
	state := r.recorderState()
	if state == nil {
		return
	}
	line := "# note: " + RedactNote(text) + "\n"
	state.mu.Lock()
	state.entries = append(state.entries, line)
	state.mu.Unlock()
}

// WriteTo writes the redacted trace to w.
func (r *Recorder) WriteTo(w io.Writer) (int64, error) {
	state := r.recorderState()
	if state == nil {
		return 0, errors.New("trace: nil recorder")
	}

	state.mu.Lock()
	entries := append([]string(nil), state.entries...)
	state.mu.Unlock()

	var builder strings.Builder
	builder.WriteString(traceHeader)
	builder.WriteString("\n")
	for _, entry := range entries {
		builder.WriteString(entry)
	}

	written, err := io.WriteString(w, builder.String())
	return int64(written), err
}

// Len returns the number of recorded exchanges. Notes are not exchanges.
func (r Recorder) Len() int {
	state := r.recorderState()
	if state == nil {
		return 0
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.exchanges
}

// String prints only the recorder's exchange count.
func (r Recorder) String() string {
	return "trace.Recorder{exchanges: " + strconv.Itoa(r.Len()) + "}"
}

// GoString prints only the recorder's exchange count.
func (r Recorder) GoString() string {
	return r.String()
}

// Format prints only the recorder's exchange count, for every fmt verb.
func (r Recorder) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(r.String()))
}

func (r *Recorder) recorderState() *recorderState {
	if r == nil {
		return nil
	}
	return r.state
}

func exchangeEntry(
	number int,
	started time.Time,
	startedAt time.Time,
	requestLines, responseLines []string,
	err error,
) string {
	var builder strings.Builder
	builder.WriteString("# exchange ")
	builder.WriteString(strconv.Itoa(number))
	builder.WriteString(" at +")
	builder.WriteString(elapsedText(startedAt.Sub(started)))
	builder.WriteString("s\n")

	for _, line := range requestLines {
		builder.WriteString("> ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}

	if err != nil {
		builder.WriteString("# exchange ")
		builder.WriteString(strconv.Itoa(number))
		builder.WriteString(" error: ")
		builder.WriteString(errorKind(err))
		builder.WriteString("\n")
		return builder.String()
	}

	for _, line := range responseLines {
		builder.WriteString("< ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	return builder.String()
}

func elapsedText(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	return strconv.FormatFloat(elapsed.Seconds(), 'f', 3, 64)
}

// errorKind classifies an error without ever keeping its text.
func errorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	}

	var statusError fints.HTTPStatusError
	if errors.As(err, &statusError) {
		return "transport http " + strconv.Itoa(statusError.HTTPStatus())
	}
	return "transport"
}
