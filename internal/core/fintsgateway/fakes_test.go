package fintsgateway

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

const (
	testBLZ      = "99999999"
	testUser     = "test-user"
	testBIC      = "TESTDEFFXXX"
	testIBAN     = "DE00999999990000000001"
	testIBAN2    = "DE00999999990000000002"
	testProduct  = "TESTPRODUCT0000000000000"
	testURL      = "https://fints.example.invalid/"
	testPIN      = "12345"
	testTAN      = "246810"
	testSystemID = "test-system"
)

var allowedSegmentTypes = map[string]struct{}{
	"HNHBK": {}, "HNHBS": {}, "HNVSK": {}, "HNVSD": {}, "HNSHK": {}, "HNSHA": {},
	"HKIDN": {}, "HKVVB": {}, "HKSYN": {}, "HKEND": {}, "HKTAN": {}, "HKTAB": {},
	"HKSPA": {}, "HKSAL": {}, "HKKAZ": {}, "HKCAZ": {},
}

type exchangeRecord struct {
	types            []string
	securityFunction string
	hktanVersions    []uint16
	hktanProcesses   []string
	segments         []fints.ParsedSegment
}

func (r exchangeRecord) has(segmentType string) bool {
	for _, value := range r.types {
		if value == segmentType {
			return true
		}
	}
	return false
}

func (r exchangeRecord) segment(segmentType string) (fints.ParsedSegment, bool) {
	for _, segment := range r.segments {
		if segment.Type() == segmentType {
			return segment, true
		}
	}
	return fints.ParsedSegment{}, false
}

type fakeBank struct {
	t       *testing.T
	mu      sync.Mutex
	records []exchangeRecord
	handler func(call int, request exchangeRecord) (string, error)
}

func newFakeBank(t *testing.T, handler func(call int, request exchangeRecord) (string, error)) *fakeBank {
	t.Helper()
	return &fakeBank{t: t, handler: handler}
}

func (b *fakeBank) Exchange(ctx context.Context, request []byte) ([]byte, error) {
	b.t.Helper()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if problem := envelopeProblem(request); problem != "" {
		b.t.Errorf("request envelope: %s", problem)
	}
	outer, err := fints.ParseMessage(request)
	if err != nil {
		b.t.Fatalf("parse request: %v", err)
	}
	segments, err := flattenSegments(outer)
	if err != nil {
		b.t.Fatalf("flatten request: %v", err)
	}
	record := exchangeRecord{segments: segments}
	for _, segment := range segments {
		segmentType := segment.Type()
		record.types = append(record.types, segmentType)
		if _, allowed := allowedSegmentTypes[segmentType]; !allowed {
			b.t.Errorf("request contains non-allow-listed segment %q", segmentType)
		}
		switch segmentType {
		case "HNSHK":
			record.securityFunction = segment.DEG(2).Text(0)
		case "HKTAN":
			record.hktanVersions = append(record.hktanVersions, segment.Version())
			record.hktanProcesses = append(record.hktanProcesses, segment.DEG(1).Text(0))
		}
	}

	b.mu.Lock()
	b.records = append(b.records, record)
	call := len(b.records) - 1
	b.mu.Unlock()

	body, err := b.handler(call, record)
	return []byte(body), err
}

func flattenSegments(segments []fints.ParsedSegment) ([]fints.ParsedSegment, error) {
	flattened := make([]fints.ParsedSegment, 0, len(segments))
	for _, segment := range segments {
		if segment.Type() != "HNVSD" {
			flattened = append(flattened, segment)
			continue
		}
		inner, err := fints.ParseMessage(segment.DEG(1).Binary(0))
		if err != nil {
			return nil, err
		}
		nested, err := flattenSegments(inner)
		if err != nil {
			return nil, err
		}
		flattened = append(flattened, nested...)
	}
	return flattened, nil
}

func (b *fakeBank) snapshotRecords() []exchangeRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]exchangeRecord(nil), b.records...)
}

func (b *fakeBank) countSegments(segmentType string) int {
	count := 0
	for _, record := range b.snapshotRecords() {
		if record.has(segmentType) {
			count++
		}
	}
	return count
}

func (b *fakeBank) countHKTANProcess(process string) int {
	count := 0
	for _, record := range b.snapshotRecords() {
		for _, value := range record.hktanProcesses {
			if value == process {
				count++
			}
		}
	}
	return count
}

// ---- wire helpers ----

func responseMessage(dialogID string, segments ...string) string {
	return "HNHBK:1:3+000000000000+300+" + dialogID + "+1'" +
		strings.Join(segments, "") +
		"HNHBS:99:1+1'"
}

func okResponse(dialogID string, payload ...string) string {
	segments := []string{"HIRMG:2:2+0010::OK'"}
	segments = append(segments, payload...)
	return responseMessage(dialogID, segments...)
}

func executedResponse(dialogID string, payload ...string) string {
	segments := []string{"HIRMG:2:2+0020::Ausgefuehrt'"}
	segments = append(segments, payload...)
	return responseMessage(dialogID, segments...)
}

func bankErrorResponse(dialogID, code string) string {
	return responseMessage(dialogID, "HIRMG:2:2+"+code+"::Fehler'")
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

func allowedSecurityFunctions(functions ...string) string {
	return "HIRMS:6:2:4+3920::Erlaubt:" + strings.Join(functions, ":") + "'"
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

func hitabs(version int) string {
	return fmt.Sprintf("HITABS:8:%d:4+1+1+1+1'", version)
}

func hispas(version int) string {
	return fmt.Sprintf("HISPAS:8:%d:4+1+1+1+1'", version)
}

func hitansMethod(securityFunction, name string, maxPolls, firstWait, nextWait int) string {
	maxPollsText := ""
	if maxPolls >= 0 {
		maxPollsText = fmt.Sprintf("%d", maxPolls)
	}
	return fmt.Sprintf(
		"%s:2:TECH-%s:DKID-%s:1.0:%s:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:%s:%d:%d:J:J",
		securityFunction, securityFunction, securityFunction, name,
		maxPollsText, firstWait, nextWait,
	)
}

func hitans(methods ...string) string {
	return "HITANS:5:7:4+1+1+1+N:N:0:" + strings.Join(methods, ":") + "'"
}

func hiuPd(segmentNumber int, accountNumber string, accountType, iban, currency, owner, product, transactions string) string {
	return fmt.Sprintf(
		"HIUPD:%d:6:4+%s::280:%s+%s+test-customer+%s+%s+%s++%s+%s'",
		segmentNumber, accountNumber, testBLZ, iban, accountType, currency, owner, product, transactions,
	)
}

func hispa(segmentNumber int, iban, bic, accountNumber string) string {
	return fmt.Sprintf(
		"HISPA:%d:4:4+J:%s:%s:%s::280:%s'",
		segmentNumber, iban, bic, accountNumber, testBLZ,
	)
}

func hisalPayload() string {
	return "HISAL:5:7:4+1::280:" + testBLZ +
		"+Girokonto+EUR+C:1234,56:EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'"
}

func hikazPayload(booked string) string {
	return "HIKAZ:5:7:4+@" + fmt.Sprint(len(booked)) + "@" + booked + "+@0@'"
}

func hicazPayload(iban, booked string) string {
	return "HICAZ:5:1:3+" + iban + ":" + testBIC +
		"+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.053.001.02" +
		"+@" + fmt.Sprint(len(booked)) + "@" + booked + "+@0@'"
}

func escapeFixtureText(value string) string {
	replacer := strings.NewReplacer("?", "??", "'", "?'", "+", "?+", ":", "?:", "@", "?@")
	return replacer.Replace(value)
}

const mt940Fixture = ":20:STARTUMS\n" +
	":25:DE00999999990000000001\n" +
	":28C:1\n" +
	":60F:C260101EUR0,00\n" +
	":61:2601010101D45,67N066NONREF\n" +
	":86:166?00KARTENZAHLUNG?100005?20Bakery?32Bakery GmbH\n" +
	":61:2601020102C500,00N066REF2\n" +
	":86:166?00GUTSCHRIFT?100005?20Salary?32Employer AG\n" +
	":62F:C260102EUR454,33"

func icamtDocument(iban, amount, sign, date, valueDate, reference, partner, purpose string) string {
	party := "Dbtr"
	if sign == "DBIT" {
		party = "Cdtr"
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">\n" +
		"<BkToCstmrStmt><Stmt><Acct><Id><IBAN>" + iban + "</IBAN></Id></Acct>\n" +
		"<Ntry><Amt Ccy=\"EUR\">" + amount + "</Amt><CdtDbtInd>" + sign + "</CdtDbtInd>" +
		"<Sts>BOOK</Sts><BookgDt><Dt>" + date + "</Dt></BookgDt>" +
		"<ValDt><Dt>" + valueDate + "</Dt></ValDt>" +
		"<AcctSvcrRef>" + reference + "</AcctSvcrRef>" +
		"<NtryDtls><TxDtls><RltdPties><" + party + "><Nm>" + partner + "</Nm></" + party + "></RltdPties>" +
		"<RmtInf><Ustrd>" + purpose + "</Ustrd></RmtInf></TxDtls></NtryDtls>" +
		"</Ntry></Stmt></BkToCstmrStmt>\n</Document>"
}

// ---- interactions ----

type testInteraction struct {
	mu            sync.Mutex
	pin           *secret.Text
	tan           *secret.Text
	tanMethod     *string
	answerNil     bool
	remember      bool
	confirm       bool
	pinAsked      int
	tanAsked      int
	confirmAsked  int
	tanOptions    [][]banksync.TanMethod
	tanChallenges []string
	confirmTexts  []string
}

func newTestInteraction(pin string) *testInteraction {
	return &testInteraction{
		pin:     secret.NewText([]byte(pin)),
		tan:     secret.NewText([]byte(testTAN)),
		confirm: true,
	}
}

func (i *testInteraction) Pin(string, string, bool) *banksync.PinEntry {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.pinAsked++
	if i.pin == nil {
		return nil
	}
	entry := banksync.NewPinEntry(i.pin, i.remember)
	return &entry
}

func (i *testInteraction) SelectTanMethod(options []banksync.TanMethod) *string {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.tanOptions = append(i.tanOptions, append([]banksync.TanMethod(nil), options...))
	if i.answerNil {
		return nil
	}
	if i.tanMethod != nil {
		value := *i.tanMethod
		return &value
	}
	if len(options) == 0 {
		return nil
	}
	value := options[0].ID
	return &value
}

func (i *testInteraction) SelectTanMedium(options []string) *string {
	if len(options) == 0 {
		return nil
	}
	value := options[len(options)-1]
	return &value
}

func (i *testInteraction) Tan(challenge banksync.TanChallenge) *secret.Text {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.tanAsked++
	i.tanChallenges = append(i.tanChallenges, challenge.Message)
	return i.tan
}

func (i *testInteraction) ConfirmDecoupled(message string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.confirmAsked++
	i.confirmTexts = append(i.confirmTexts, message)
	return i.confirm
}

func (i *testInteraction) Info(string) {}

func (i *testInteraction) answerTanMethod(id string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	value := id
	i.tanMethod = &value
}

// ---- gateway construction ----

func newTestGateway(t *testing.T, bank *fakeBank) *Gateway {
	t.Helper()
	return New(
		WithTransportFactory(func(string) (fints.Transport, error) { return bank, nil }),
		WithSleeper(noopSleeper{}),
		WithClock(func() time.Time { return time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC) }),
	)
}

type noopSleeper struct{}

func (noopSleeper) Sleep(context.Context, time.Duration) error { return nil }

func testConnection() banksync.FinTsConnection {
	return banksync.FinTsConnection{
		BankName:   "Test Bank",
		BLZ:        testBLZ,
		UserID:     testUser,
		CustomerID: testUser,
		ServerURL:  testURL,
		ProductID:  testProduct,
	}
}

func date(year int, month time.Month, day int) civil.Date {
	value, ok := civil.New(year, month, day)
	if !ok {
		panic("invalid test date")
	}
	return value
}

// envelopeProblem reports the envelope faults real banks rejected: an empty
// dialog id in HNHBK (HVB 9120/9050) and the customer named as message
// receiver (2) instead of sender (1) in HNVSK or HNSHK.
func envelopeProblem(request []byte) string {
	text := string(request)
	header := text
	if end := strings.IndexByte(text, '\''); end >= 0 {
		header = text[:end]
	}
	if strings.HasPrefix(header, "HNHBK:") && strings.Contains(header, "+300++") {
		return "HNHBK has an empty dialog id"
	}
	if strings.Contains(text, "+998+1+2::") || strings.Contains(text, "+1+1+2::") {
		return "a security header names the customer as message receiver"
	}
	return ""
}
