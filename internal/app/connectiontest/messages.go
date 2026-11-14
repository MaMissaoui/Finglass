package connectiontest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

// The collector keeps at most 20 messages and cuts one text to 300 runes.
const (
	maxBankMessages     = 20
	maxBankMessageRunes = 300
)

// BankMessage is one warning or error the bank returned during the test.
// Text is the bank's own text. It can carry account data: show it on screen
// only, never log, store, audit or trace it.
type BankMessage struct {
	Code    string // four digits
	Segment string // the request segment it answers, e.g. "HKTAN"; "" for message-level
	Text    string
}

// String prints only the code and segment, never the bank's text.
func (m BankMessage) String() string {
	return fmt.Sprintf("connectiontest.BankMessage{code:%s segment:%s}", m.Code, m.Segment)
}

// GoString prints only the code and segment, never the bank's text.
func (m BankMessage) GoString() string {
	return m.String()
}

// Format writes only the code and segment for every fmt verb.
func (m BankMessage) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(m.String()))
}

// messageCollector wraps a transport and remembers the bank's warning and
// error texts from each successful exchange.
//
// It is safe for concurrent use, although the probe exchanges on one
// goroutine.
type messageCollector struct {
	transport fints.Transport

	mu       sync.Mutex
	messages []BankMessage
	seen     map[string]struct{}
}

func newMessageCollector(transport fints.Transport) *messageCollector {
	return &messageCollector{
		transport: transport,
		seen:      make(map[string]struct{}),
	}
}

// Exchange forwards the call unchanged and collects after a successful
// response. On an error it collects nothing for this exchange.
func (c *messageCollector) Exchange(ctx context.Context, request []byte) ([]byte, error) {
	response, err := c.transport.Exchange(ctx, request)
	if err != nil {
		return response, err
	}
	c.collect(request, response)
	return response, nil
}

// snapshot returns the collected messages in wire order. The result is a copy.
func (c *messageCollector) snapshot() []BankMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) == 0 {
		return nil
	}
	return append([]BankMessage(nil), c.messages...)
}

func (c *messageCollector) collect(request, response []byte) {
	segmentTypes := requestSegmentTypes(request)
	parsed, err := fints.ParseResponse(response)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, code := range parsed.GlobalCodes {
		c.addLocked(code, "")
	}
	for _, segment := range parsed.Segments {
		if segment.Type() != "HIRMS" {
			continue
		}
		segmentName := ""
		if reference, ok := segment.Reference(); ok {
			segmentName = segmentTypes[reference]
		}
		for _, code := range fints.ReturnCodesOf(segment) {
			c.addLocked(code, segmentName)
		}
	}
}

func (c *messageCollector) addLocked(code fints.ReturnCode, segment string) {
	if len(c.messages) >= maxBankMessages {
		return
	}
	if !code.IsWarning() && !code.IsError() {
		return
	}
	if !isFourDigits(code.Code) {
		return
	}
	if !isSegmentName(segment) {
		segment = ""
	}
	text := cleanBankText(code.Text())
	key := code.Code + "\x00" + segment + "\x00" + text
	if _, exists := c.seen[key]; exists {
		return
	}
	c.seen[key] = struct{}{}
	c.messages = append(c.messages, BankMessage{
		Code:    code.Code,
		Segment: segment,
		Text:    text,
	})
}

// requestSegmentTypes maps the request's segment numbers to their types. A
// parse failure leaves the map empty, so the segment name stays unknown.
func requestSegmentTypes(request []byte) map[uint16]string {
	types := make(map[uint16]string)
	parsed, err := fints.ParseResponse(request)
	if err != nil {
		return types
	}
	for _, segment := range parsed.Segments {
		types[segment.Number()] = segment.Type()
	}
	return types
}

// cleanBankText removes control characters, collapses whitespace and cuts the
// text to maxBankMessageRunes, appending an ellipsis when it cuts.
func cleanBankText(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for _, character := range text {
		switch {
		case unicode.IsSpace(character):
			builder.WriteByte(' ')
		case unicode.IsControl(character):
			continue
		default:
			builder.WriteRune(character)
		}
	}
	cleaned := strings.Join(strings.Fields(builder.String()), " ")
	runes := []rune(cleaned)
	if len(runes) > maxBankMessageRunes {
		return string(runes[:maxBankMessageRunes]) + "…"
	}
	return cleaned
}

func isFourDigits(text string) bool {
	if len(text) != 4 {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}

func isSegmentName(text string) bool {
	if len(text) != 5 {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < 'A' || text[index] > 'Z' {
			return false
		}
	}
	return true
}
