package fints

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func messageInput(pin, tan string) MessageInput {
	input := MessageInput{
		BLZ:              testBLZ,
		UserID:           testUserID,
		CustomerID:       testCustomerID,
		SystemID:         testSystemID,
		DialogID:         testDialogID,
		MessageNumber:    1,
		SecurityFunction: "999",
		PIN:              secret.NewText([]byte(pin)),
		Source:           fixedSource(),
	}
	if tan != "" {
		input.TAN = secret.NewText([]byte(tan))
	}
	return input
}

func buildTestMessage(t *testing.T, input MessageInput, segments ...Segment) []byte {
	t.Helper()
	message, err := BuildMessage(input, segments...)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	return message
}

func TestBuildMessageStructure(t *testing.T) {
	message := buildTestMessage(t, messageInput(testPIN, ""),
		NewHKIDN(testBLZ, testCustomerID, testSystemID),
		NewHKVVB(0, 0, testProductID, "1.0"),
	)

	if !bytes.HasPrefix(message, []byte("HNHBK:1:3+")) {
		t.Fatalf("message does not start with HNHBK:1:3: %q", message)
	}
	for _, expected := range []string{"HNVSK:998:3+", "HNVSD:999:1+", "HNSHK:2:4+", "HKIDN:3:2+", "HKVVB:4:3+", "HNSHA:5:2+", "HNHBS:6:1+"} {
		if !bytes.Contains(message, []byte(expected)) {
			t.Fatalf("message does not contain %q", expected)
		}
	}
	if !bytes.HasSuffix(message, []byte("'")) {
		t.Fatal("message does not end with a segment terminator")
	}

	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	types := make([]string, 0, len(response.Segments))
	for _, segment := range response.Segments {
		types = append(types, segment.Type())
	}
	want := []string{"HNHBK", "HNVSK", "HNSHK", "HKIDN", "HKVVB", "HNSHA", "HNHBS"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("segment types = %v, want %v", types, want)
	}
	if got := response.Segments[len(response.Segments)-1].Type(); got != "HNHBS" {
		t.Fatalf("last segment = %q", got)
	}
}

func TestMessageSizeInHeader(t *testing.T) {
	message := buildTestMessage(t, messageInput(testPIN, ""), NewHKIDN(testBLZ, testCustomerID, testSystemID))
	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	header, ok := response.findSegment("HNHBK")
	if !ok {
		t.Fatal("no HNHBK")
	}
	sizeText := header.DEG(1).Text(0)
	if len(sizeText) != 12 {
		t.Fatalf("message size %q is not 12 digits", sizeText)
	}
	size, err := strconv.Atoi(sizeText)
	if err != nil {
		t.Fatalf("message size %q: %v", sizeText, err)
	}
	if size != len(message) {
		t.Fatalf("header size %d, message length %d", size, len(message))
	}
}

func TestMessageSegmentNumbers(t *testing.T) {
	message := buildTestMessage(t, messageInput(testPIN, ""),
		NewHKIDN(testBLZ, testCustomerID, testSystemID),
		NewHKVVB(0, 0, testProductID, "1.0"),
	)
	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}

	wantNumbers := map[string]uint16{
		"HNHBK": 1,
		"HNSHK": 2,
		"HKIDN": 3,
		"HKVVB": 4,
		"HNSHA": 5,
		"HNHBS": 6,
	}
	for segmentType, want := range wantNumbers {
		segment, ok := response.findSegment(segmentType)
		if !ok {
			t.Fatalf("no %s segment", segmentType)
		}
		if segment.Number() != want {
			t.Fatalf("%s number = %d, want %d", segmentType, segment.Number(), want)
		}
	}

	trailer, ok := response.findSegment("HNHBS")
	if !ok {
		t.Fatal("no HNHBS")
	}
	if got := trailer.DEG(1).Text(0); got != "1" {
		t.Fatalf("HNHBS message number = %q, want 1", got)
	}
}

func TestEnvelopeSeparatesUserIDAndCustomerID(t *testing.T) {
	message := buildTestMessage(t, messageInput(testPIN, ""),
		NewHKIDN(testBLZ, testCustomerID, testSystemID),
	)
	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}

	// The signature and encryption key names identify the login (user id).
	shk, ok := response.findSegment("HNSHK")
	if !ok {
		t.Fatal("no HNSHK")
	}
	if got := shk.DEG(11).Text(2); got != testUserID {
		t.Fatalf("HNSHK key name user id = %q, want %q", got, testUserID)
	}
	vsk, ok := response.findSegment("HNVSK")
	if !ok {
		t.Fatal("no HNVSK")
	}
	if got := vsk.DEG(7).Text(2); got != testUserID {
		t.Fatalf("HNVSK key name user id = %q, want %q", got, testUserID)
	}

	// HKIDN carries the customer id.
	idn, ok := response.findSegment("HKIDN")
	if !ok {
		t.Fatal("no HKIDN")
	}
	if got := idn.DEG(2).Text(0); got != testCustomerID {
		t.Fatalf("HKIDN customer id = %q, want %q", got, testCustomerID)
	}
	if got := idn.DEG(2).Text(0); got == testUserID {
		t.Fatal("HKIDN carries the user id, want the customer id")
	}
}

func TestBuildMessageWithoutBusinessSegments(t *testing.T) {
	message := buildTestMessage(t, messageInput(testPIN, ""))
	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	types := make([]string, 0, len(response.Segments))
	for _, segment := range response.Segments {
		types = append(types, segment.Type())
	}
	want := []string{"HNHBK", "HNVSK", "HNSHK", "HNSHA", "HNHBS"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("segment types = %v, want %v", types, want)
	}
	if got := response.Segments[3].Number(); got != 3 {
		t.Fatalf("HNSHA number = %d, want 3", got)
	}
	if got := response.Segments[4].Number(); got != 4 {
		t.Fatalf("HNHBS number = %d, want 4", got)
	}
}

func TestBuildMessageIsDeterministic(t *testing.T) {
	first := buildTestMessage(t, messageInput(testPIN, testTAN),
		NewHKIDN(testBLZ, testCustomerID, testSystemID),
		NewHKVVB(0, 0, testProductID, "1.0"),
	)
	second := buildTestMessage(t, messageInput(testPIN, testTAN),
		NewHKIDN(testBLZ, testCustomerID, testSystemID),
		NewHKVVB(0, 0, testProductID, "1.0"),
	)
	if !bytes.Equal(first, second) {
		t.Fatal("two builds with the same Source differ")
	}
}

func TestBuildMessageUsesTheInjectedClockAndReference(t *testing.T) {
	message := buildTestMessage(t, messageInput(testPIN, ""))
	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	shk, ok := response.findSegment("HNSHK")
	if !ok {
		t.Fatal("no HNSHK")
	}
	if got := shk.DEG(3).Text(0); got != "1234567" {
		t.Fatalf("HNSHK security reference = %q", got)
	}
	if got := shk.DEG(8).Text(1); got != "20260329" {
		t.Fatalf("HNSHK date = %q", got)
	}
	if got := shk.DEG(8).Text(2); got != "123456" {
		t.Fatalf("HNSHK time = %q", got)
	}

	vsk, ok := response.findSegment("HNVSK")
	if !ok {
		t.Fatal("no HNVSK")
	}
	if got := vsk.DEG(5).Text(1); got != "20260329" {
		t.Fatalf("HNVSK date = %q", got)
	}
	if got := vsk.DEG(5).Text(2); got != "123456" {
		t.Fatalf("HNVSK time = %q", got)
	}

	sha, ok := response.findSegment("HNSHA")
	if !ok {
		t.Fatal("no HNSHA")
	}
	if got := sha.DEG(1).Text(0); got != "1234567" {
		t.Fatalf("HNSHA security reference = %q", got)
	}
}

func TestDefaultSourceProducesSevenDigitReference(t *testing.T) {
	input := messageInput(testPIN, "")
	input.Source = Source{}
	message, err := BuildMessage(input)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	sha, ok := response.findSegment("HNSHA")
	if !ok {
		t.Fatal("no HNSHA")
	}
	reference := sha.DEG(1).Text(0)
	if len(reference) != 7 {
		t.Fatalf("default security reference %q is not seven digits", reference)
	}
	value, err := strconv.Atoi(reference)
	if err != nil {
		t.Fatalf("security reference %q: %v", reference, err)
	}
	if value < 1_000_000 || value > 9_999_999 {
		t.Fatalf("security reference %d outside 1_000_000..9_999_999", value)
	}
}

func TestSecretsAppearOnceEscapedAndRoundTrip(t *testing.T) {
	message := buildTestMessage(t, messageInput(testPIN, testTAN))

	escapedPIN := escapedForFixture(t, testPIN)
	escapedTAN := escapedForFixture(t, testTAN)
	if count := bytes.Count(message, []byte(escapedPIN)); count != 1 {
		t.Fatalf("escaped PIN appears %d times, want 1", count)
	}
	if count := bytes.Count(message, []byte(escapedTAN)); count != 1 {
		t.Fatalf("escaped TAN appears %d times, want 1", count)
	}
	if bytes.Contains(message, []byte(testPIN)) {
		t.Fatal("the raw PIN appears in the message")
	}

	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	sha, ok := response.findSegment("HNSHA")
	if !ok {
		t.Fatal("no HNSHA")
	}
	signature := sha.DEG(3)
	if signature.Len() != 2 {
		t.Fatalf("signature has %d elements, want 2", signature.Len())
	}
	if got := signature.Text(0); got != testPIN {
		t.Fatalf("PIN round-tripped as %q", got)
	}
	if got := signature.Text(1); got != testTAN {
		t.Fatalf("TAN round-tripped as %q", got)
	}
}

func TestEmptyTANWritesAnEmptyTANField(t *testing.T) {
	input := messageInput(testPIN, "")
	input.TAN = secret.NewText(nil)
	message := buildTestMessage(t, input)

	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	sha, ok := response.findSegment("HNSHA")
	if !ok {
		t.Fatal("no HNSHA")
	}
	signature := sha.DEG(3)
	if signature.Len() != 2 {
		t.Fatalf("signature has %d elements, want 2 (empty TAN field)", signature.Len())
	}
	if got := signature.Text(0); got != testPIN {
		t.Fatalf("PIN round-tripped as %q", got)
	}
	if got := signature.Text(1); got != "" {
		t.Fatalf("empty TAN round-tripped as %q", got)
	}
}

func TestMessageInputAndErrorsNeverRevealSecrets(t *testing.T) {
	input := messageInput(testPIN, testTAN)
	escapedPIN := escapedForFixture(t, testPIN)
	escapedTAN := escapedForFixture(t, testTAN)

	message := buildTestMessage(t, input)
	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	sha, ok := response.findSegment("HNSHA")
	if !ok {
		t.Fatal("no HNSHA")
	}
	signature := sha.DEG(3)

	// Every exported type that holds the secrets, raw or parsed.
	holders := []struct {
		name  string
		value any
	}{
		{name: "MessageInput", value: input},
		{name: "ParsedSegment", value: sha},
		{name: "DEG", value: signature},
		{name: "DataElement", value: signature.At(0)},
	}
	for _, holder := range holders {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			got := fmt.Sprintf(format, holder.value)
			for _, forbidden := range []string{testPIN, testTAN, escapedPIN, escapedTAN} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("%s of %s leaked %q: %q", format, holder.name, forbidden, got)
				}
			}
		}
	}

	if _, err := BuildMessage(MessageInput{SecurityFunction: "999", PIN: nil}); err == nil {
		t.Fatal("BuildMessage accepted a nil PIN")
	} else {
		for _, forbidden := range []string{testPIN, testTAN, escapedPIN, escapedTAN} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("error leaked %q: %v", forbidden, err)
			}
		}
	}

	wiped := messageInput(testPIN, testTAN)
	wiped.PIN.Wipe()
	_, err = BuildMessage(wiped)
	if err == nil {
		t.Fatal("BuildMessage accepted a wiped PIN")
	}
	for _, forbidden := range []string{testPIN, testTAN, escapedPIN, escapedTAN} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("wiped-PIN error leaked %q: %v", forbidden, err)
		}
	}
}

func TestWipeZeroesTheBuffer(t *testing.T) {
	buffer := []byte("pin-tan-message")
	Wipe(buffer)
	for index, value := range buffer {
		if value != 0 {
			t.Fatalf("byte %d = %d after Wipe", index, value)
		}
	}

	message := buildTestMessage(t, messageInput(testPIN, testTAN))
	Wipe(message)
	for index, value := range message {
		if value != 0 {
			t.Fatalf("message byte %d = %d after Wipe", index, value)
		}
	}
}

func TestBuildMessageRejectsAnInvalidSegment(t *testing.T) {
	if _, err := BuildMessage(messageInput(testPIN, ""), NewHKSAL(7, "", testBIC, nil)); err == nil {
		t.Fatal("BuildMessage accepted an empty IBAN")
	}
}

// A PIN and TAN made only of characters that need escaping, long enough to
// outgrow any default buffer, still build without a reallocation after they
// are written (BuildMessage refuses that with errBufferGrew) and round-trip.
func TestLongFullyEscapedSecretsBuildWithoutReallocation(t *testing.T) {
	pin := strings.Repeat("?'+:@", 2000)
	tan := strings.Repeat("@:", 3000)
	message := buildTestMessage(t, messageInput(pin, tan))

	response, err := ParseResponse(message)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	sha, ok := response.findSegment("HNSHA")
	if !ok {
		t.Fatal("no HNSHA")
	}
	signature := sha.DEG(3)
	if signature.Text(0) != pin || signature.Text(1) != tan {
		t.Fatal("a long escaped PIN or TAN did not round-trip")
	}
}
