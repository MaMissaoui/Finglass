package fints

import (
	"bytes"
	"testing"
)

func TestParseHITANVersion6TaskReferenceFromDEG3(t *testing.T) {
	challenge, ok := ParseHITAN(parseOne(t,
		"HITAN:3:6:2+4+task-hash-1+task-ref-1+challenge-text+@3@ABC+20260315'"))
	if !ok {
		t.Fatal("ParseHITAN reported no challenge")
	}
	if challenge.TaskReference != "task-ref-1" {
		t.Fatalf("TaskReference = %q", challenge.TaskReference)
	}
	if challenge.Challenge() != "challenge-text" {
		t.Fatalf("Challenge = %q", challenge.Challenge())
	}
	if got := challenge.HHDUD(); !bytes.Equal(got, []byte("ABC")) {
		t.Fatalf("HHDUD = %v", got)
	}
}

func TestParseHITANVersion6HashNeverBecomesTaskReference(t *testing.T) {
	challenge, ok := ParseHITAN(parseOne(t,
		"HITAN:4:6:2+4+order-hash-only++challenge-text++'"))
	if !ok {
		t.Fatal("ParseHITAN reported no challenge")
	}
	if challenge.TaskReference != "" {
		t.Fatalf("TaskReference = %q, want empty", challenge.TaskReference)
	}
	if challenge.Challenge() != "challenge-text" {
		t.Fatalf("Challenge = %q", challenge.Challenge())
	}
	if challenge.HHDUD() != nil {
		t.Fatalf("HHDUD = %v, want nil", challenge.HHDUD())
	}
}

func TestParseHITANVersion7TaskReferenceFromDEG3(t *testing.T) {
	challenge, ok := ParseHITAN(parseOne(t,
		"HITAN:5:7:2+4+task-hash-7+task-ref-7+challenge-seven+@2@\x00\x01+20260315'"))
	if !ok {
		t.Fatal("ParseHITAN reported no challenge")
	}
	if challenge.TaskReference != "task-ref-7" {
		t.Fatalf("TaskReference = %q", challenge.TaskReference)
	}
	if challenge.Challenge() != "challenge-seven" {
		t.Fatalf("Challenge = %q", challenge.Challenge())
	}
	if got := challenge.HHDUD(); !bytes.Equal(got, []byte{0x00, 0x01}) {
		t.Fatalf("HHDUD = %v", got)
	}
}

func TestParseHITANVersion3To5(t *testing.T) {
	challenge, ok := ParseHITAN(parseOne(t, "HITAN:6:5:2+4+task-ref-2+challenge-text'"))
	if !ok {
		t.Fatal("ParseHITAN reported no challenge")
	}
	if challenge.TaskReference != "task-ref-2" {
		t.Fatalf("TaskReference = %q", challenge.TaskReference)
	}
	if challenge.Challenge() != "challenge-text" {
		t.Fatalf("Challenge = %q", challenge.Challenge())
	}
	if challenge.HHDUD() != nil {
		t.Fatalf("HHDUD = %v, want nil", challenge.HHDUD())
	}
}

func TestParseHITANWithoutContent(t *testing.T) {
	if _, ok := ParseHITAN(parseOne(t, "HITAN:7:6:2+4+order-hash+++'")); ok {
		t.Fatal("ParseHITAN accepted a challenge without reference or text")
	}
}

func TestParseTanMedia(t *testing.T) {
	media := ParseTanMedia(parseOne(t,
		"HITAB:6:6:4+A:1:My Phone+1:1:Second Phone+I:1:Old Phone+B:1:Blocked Phone'"))
	if len(media) != 2 {
		t.Fatalf("media = %d, want 2", len(media))
	}
	if media[0].Name() != "My Phone" {
		t.Fatalf("media[0].Name = %q", media[0].Name())
	}
	if media[1].Name() != "Second Phone" {
		t.Fatalf("media[1].Name = %q", media[1].Name())
	}
}

func TestParseTanMediaSkipsShortAndEmptyNames(t *testing.T) {
	media := ParseTanMedia(parseOne(t, "HITAB:7:6:4+A:1+1:1:+A::Named'"))
	if len(media) != 1 || media[0].Name() != "Named" {
		t.Fatalf("media = %#v", media)
	}
}

func TestResponseTANChallengeAndMedia(t *testing.T) {
	input := "HITAN:1:6:2+4+task-hash-1+task-ref-1+challenge-text+@3@ABC+20260315'" +
		"HITAB:2:6:4+A:1:My Phone'"
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	challenge, ok := response.TANChallenge()
	if !ok || challenge.TaskReference != "task-ref-1" {
		t.Fatalf("TANChallenge = %#v, %v", challenge, ok)
	}
	media := response.TanMedia()
	if len(media) != 1 || media[0].Name() != "My Phone" {
		t.Fatalf("TanMedia = %#v", media)
	}
}
