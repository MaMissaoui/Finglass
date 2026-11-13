package fints

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// fakeExchange is what the fake bank records about one request. It keeps the
// segment types, their numbers and the message number, never a data element.
type fakeExchange struct {
	types         []string
	numbers       []uint16
	messageNumber uint16
	// securityFunction is HNSHK's security function code (metadata only).
	securityFunction string
	request          []byte
}

type fakeReply struct {
	body string
	err  error
}

// fakeBank is a scripted Transport for the offline dialog tests.
//
// It parses each request with ParseMessage, records only segment types,
// segment numbers and the message number, checks the expected shape and
// answers with canned wire strings. It keeps the request slice it received
// without copying it, so a test can prove the dialog wiped it.
type fakeBank struct {
	t        *testing.T
	expected [][]string
	replies  []fakeReply
	handler  func(call int, request fakeExchange) (string, error)
	records  []fakeExchange
}

func newFakeBank(t *testing.T) *fakeBank {
	t.Helper()
	return &fakeBank{t: t}
}

func (b *fakeBank) expect(calls ...[]string) {
	b.expected = calls
}

func (b *fakeBank) reply(body string) {
	b.replies = append(b.replies, fakeReply{body: body})
}

func (b *fakeBank) replyError(err error) {
	b.replies = append(b.replies, fakeReply{err: err})
}

// Exchange implements Transport.
func (b *fakeBank) Exchange(_ context.Context, request []byte) ([]byte, error) {
	b.t.Helper()
	record, err := inspectRequest(request)
	if err != nil {
		b.t.Fatalf("inspect request: %v", err)
	}
	call := len(b.records)
	b.records = append(b.records, record)

	if b.expected != nil {
		if call >= len(b.expected) {
			b.t.Fatalf("unexpected exchange %d: %s", call, strings.Join(record.types, ","))
		}
		got := strings.Join(record.types, ",")
		want := strings.Join(b.expected[call], ",")
		if got != want {
			b.t.Fatalf("exchange %d segments = %s, want %s", call, got, want)
		}
	}
	for _, segmentType := range record.types {
		if err := checkOutgoingType(segmentType); err != nil {
			b.t.Errorf("exchange %d sent non-allow-listed segment %q", call, segmentType)
		}
	}

	if b.handler != nil {
		body, err := b.handler(call, record)
		return []byte(body), err
	}
	if call >= len(b.replies) {
		b.t.Fatalf("exchange %d has no scripted reply", call)
	}
	reply := b.replies[call]
	if reply.err != nil {
		return nil, reply.err
	}
	return []byte(reply.body), nil
}

func (b *fakeBank) assertRequestsWiped(t *testing.T) {
	t.Helper()
	for exchangeIndex, record := range b.records {
		for byteIndex, value := range record.request {
			if value != 0 {
				t.Fatalf(
					"exchange %d request byte %d = %d after the dialog call returned, want 0",
					exchangeIndex, byteIndex, value,
				)
			}
		}
	}
}

func (b *fakeBank) assertSegmentTypes(t *testing.T, expected ...[]string) {
	t.Helper()
	if len(b.records) != len(expected) {
		t.Fatalf("exchanges = %d, want %d", len(b.records), len(expected))
	}
	for index, shape := range expected {
		got := strings.Join(b.records[index].types, ",")
		want := strings.Join(shape, ",")
		if got != want {
			t.Fatalf("exchange %d segments = %s, want %s", index, got, want)
		}
	}
}

func inspectRequest(request []byte) (fakeExchange, error) {
	outer, err := ParseMessage(request)
	if err != nil {
		return fakeExchange{}, err
	}
	segments, err := flattenResponses(outer, 0)
	if err != nil {
		return fakeExchange{}, err
	}

	record := fakeExchange{request: request}
	for _, segment := range segments {
		record.types = append(record.types, segment.Type())
		record.numbers = append(record.numbers, segment.Number())
		if segment.Type() == "HNHBK" {
			record.messageNumber = parseUint16(segment.DEG(4).Text(0))
		}
		if segment.Type() == "HNSHK" {
			record.securityFunction = segment.DEG(2).Text(0)
		}
	}
	return record, nil
}

// requestShape is the flattened segment order of a request with the given
// business segments.
func requestShape(business ...string) []string {
	types := []string{"HNHBK", "HNVSK", "HNSHK"}
	types = append(types, business...)
	types = append(types, "HNSHA", "HNHBS")
	return types
}

// fakeSleeper records poll waits and never sleeps.
type fakeSleeper struct {
	waits   []time.Duration
	onSleep func(call int, ctx context.Context) error
}

func (s *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	s.waits = append(s.waits, d)
	if s.onSleep != nil {
		return s.onSleep(len(s.waits)-1, ctx)
	}
	return nil
}

func (s *fakeSleeper) durations() []time.Duration {
	return append([]time.Duration(nil), s.waits...)
}

// ---- canned wire responses ----

func responseMessage(dialogID string, segments ...string) string {
	return "HNHBK:1:3+000000000000+300+" + dialogID + "+1'" +
		strings.Join(segments, "") +
		"HNHBS:99:1+1'"
}

func successResponse(dialogID string) string {
	return responseMessage(dialogID, "HIRMG:2:2+0010::OK'")
}

func executedResponse(dialogID string, payload ...string) string {
	segments := []string{"HIRMG:2:2+0020::Ausgefuehrt'"}
	segments = append(segments, payload...)
	return responseMessage(dialogID, segments...)
}

func codeResponse(dialogID, code string) string {
	return responseMessage(dialogID, fmt.Sprintf("HIRMG:2:2+%s::Fehler'", code))
}

func tanRequiredResponse(dialogID, taskReference, challenge string) string {
	return responseMessage(
		dialogID,
		"HIRMS:3:2:4+0030::TAN erforderlich'",
		"HITAN:4:7:3+4+task-hash+"+taskReference+"+"+challenge+"+@0@'",
	)
}

func decoupledResponse(dialogID, taskReference, challenge string) string {
	return responseMessage(
		dialogID,
		"HIRMS:3:2:4+3955::Im App bestaetigen'",
		"HITAN:4:7:3+4+task-hash+"+taskReference+"+"+challenge+"+@0@'",
	)
}

func decoupledPendingResponse(dialogID string) string {
	return responseMessage(dialogID, "HIRMS:3:2:4+3956::Noch nicht bestaetigt'")
}

func touchdownResponse(dialogID, point string, payload ...string) string {
	segments := []string{"HIRMS:3:2:4+3040::Mehr Daten:" + point + "'"}
	segments = append(segments, payload...)
	return responseMessage(dialogID, segments...)
}

func syncResponse(extra ...string) string {
	segments := []string{
		"HISYN:2:4:1+test-system'",
		"HIBPA:3:3+42+280+0+1+1+0'",
		"HIUPA:4:4+test-customer+7+55'",
	}
	segments = append(segments, extra...)
	segments = append(segments, "HIRMG:90:2+0010::OK'")
	return responseMessage("dialog-sync", segments...)
}

func decoupledHitans(maxPolls, firstWait, nextWait int) string {
	return fmt.Sprintf(
		"HITANS:5:7:4+1+1+1+N:N:0"+
			":942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:%d:%d:%d:J:J'",
		maxPolls, firstWait, nextWait,
	)
}

func chipHitans(firstWait, nextWait int) string {
	return fmt.Sprintf(
		"HITANS:5:7:4+1+1+1+N:N:0"+
			":942:2:TECH-CHIP:DKID-CHIP:1.0:chipTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1::%d:%d:J:J'",
		firstWait, nextWait,
	)
}

func allowedSecurityFunction(securityFunction string) string {
	return "HIRMS:6:2:4+3920::Erlaubt:" + securityFunction + "'"
}

func hipins(operations string) string {
	return "HIPINS:7:1:4+1+1+0+" + operations + "'"
}

func hisals(version int) string {
	return fmt.Sprintf("HISALS:8:%d:4+1+1+1+1'", version)
}

func hikazs(version int) string {
	return fmt.Sprintf("HIKAZS:8:%d:4+1+1+1+1'", version)
}

func hicazs(version int, format string) string {
	return fmt.Sprintf("HICAZS:8:%d:4+1+1+1+%s'", version, escapeFixtureText(format))
}

func hisalPayload() string {
	return "HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:1234,56:EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'"
}

func hikazPayload(booked, pending string) string {
	return "HIKAZ:5:7:4+@" + fmt.Sprint(len(booked)) + "@" + booked +
		"+@" + fmt.Sprint(len(pending)) + "@" + pending + "'"
}

func hicazPayload(booked, pending string) string {
	return "HICAZ:5:1:3+" + testIBAN + ":" + testBIC +
		"+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02" +
		"+@" + fmt.Sprint(len(booked)) + "@" + booked +
		"+@" + fmt.Sprint(len(pending)) + "@" + pending + "'"
}

func escapeFixtureText(value string) string {
	replacer := strings.NewReplacer("?", "??", "'", "?'", "+", "?+", ":", "?:", "@", "?@")
	return replacer.Replace(value)
}

// ---- dialog helpers ----

func newTestDialog(t *testing.T, bank *fakeBank, options ...func(*DialogConfig)) *Dialog {
	t.Helper()
	config := DialogConfig{
		BLZ:            testBLZ,
		UserID:         testUserID,
		CustomerID:     testCustomerID,
		SystemID:       "",
		ProductID:      testProductID,
		ProductVersion: "1.0",
		PIN:            secretTextForTest(testPIN),
		Transport:      bank,
		Sleeper:        &fakeSleeper{},
		Source:         fixedSource(),
	}
	for _, option := range options {
		option(&config)
	}
	dialog, err := NewDialog(config)
	if err != nil {
		t.Fatalf("NewDialog: %v", err)
	}
	return dialog
}

// newSyncedDialog seeds the parameters from a sync response without exercising
// the sync messages. Dialog tests that are not about Sync use it.
func newSyncedDialog(t *testing.T, bank *fakeBank, body string, options ...func(*DialogConfig)) *Dialog {
	t.Helper()
	dialog := newTestDialog(t, bank, options...)
	response, err := ParseResponse([]byte(body))
	if err != nil {
		t.Fatalf("ParseResponse(sync body): %v", err)
	}
	dialog.params.IngestResponse(response)
	dialog.params.SelectSecurityFunction()
	dialog.systemID = dialog.params.SystemID
	dialog.state = DialogSynced
	dialog.dialogID = ""
	dialog.messageNum = 1
	return dialog
}

func secretTextForTest(value string) *secret.Text {
	return secret.NewText([]byte(value))
}
