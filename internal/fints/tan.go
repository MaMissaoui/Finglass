// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import "fmt"

// TanChallenge is a TAN challenge from HITAN.
type TanChallenge struct {
	// TaskReference is the reference to send back in HKTAN process 2 or S.
	TaskReference string

	challenge string
	hhduc     []byte
}

// Challenge returns the challenge text as sent by the bank.
//
// The challenge may carry account numbers; never log or audit it.
func (c TanChallenge) Challenge() string {
	return c.challenge
}

// HHDUD returns the optional HHD-UC or matrix bytes unchanged.
//
// The result is a copy.
func (c TanChallenge) HHDUD() []byte {
	if c.hhduc == nil {
		return nil
	}
	return append([]byte(nil), c.hhduc...)
}

// ChallengeHHDUD is the fints-rs-named form of [TanChallenge.HHDUD].
func (c TanChallenge) ChallengeHHDUD() []byte {
	return c.HHDUD()
}

// String returns only lengths, never the challenge text or binary content.
func (c TanChallenge) String() string {
	return fmt.Sprintf(
		"fints.TanChallenge{taskReference:%d challenge:%d hhduc:%d}",
		len(c.TaskReference), len(c.challenge), len(c.hhduc),
	)
}

// GoString returns only lengths, never bank text or binary content.
func (c TanChallenge) GoString() string {
	return c.String()
}

// Format writes only lengths, never bank text or binary content, for every fmt
// verb.
func (c TanChallenge) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(c.String()))
}

// ParseHITAN ports fints-rs's parse_hitan with the corrected v6/v7 layout:
// DEG 2 is the task hash value and DEG 3 is the task reference. It reports
// false when the segment carries neither a task reference nor a challenge.
func ParseHITAN(segment ParsedSegment) (TanChallenge, bool) {
	version := segment.Version()

	taskReference := ""
	challenge := ""
	var hhduc []byte
	if version >= 6 {
		// DEG 2 is the task hash value; the task reference is DEG 3.
		taskReference = segment.DEG(3).Text(0)
		challenge = segment.DEG(4).Text(0)
		hhduc = segment.DEG(5).Binary(0)
	} else {
		taskReference = segment.DEG(2).Text(0)
		challenge = segment.DEG(3).Text(0)
	}

	if taskReference == "" && challenge == "" {
		return TanChallenge{}, false
	}
	return TanChallenge{
		TaskReference: taskReference,
		challenge:     challenge,
		hhduc:         hhduc,
	}, true
}

// TANChallenge returns the first HITAN challenge, when there is one.
func (r Response) TANChallenge() (TanChallenge, bool) {
	segment, ok := r.findSegment("HITAN")
	if !ok {
		return TanChallenge{}, false
	}
	return ParseHITAN(segment)
}

// TanMedia is one active TAN medium from HITAB.
type TanMedia struct {
	name string
}

// Name returns the medium name as sent by the bank.
//
// The name may carry account numbers; never log or audit it.
func (m TanMedia) Name() string {
	return m.name
}

// String returns only the type, never the medium name.
func (m TanMedia) String() string {
	return "fints.TanMedia{}"
}

// GoString returns only the type, never the medium name.
func (m TanMedia) GoString() string {
	return m.String()
}

// Format writes only the type, never the medium name, for every fmt verb.
func (m TanMedia) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(m.String()))
}

// ParseTanMedia ports fints-rs's parse_hitab. It returns the names of active
// media, in wire order.
func ParseTanMedia(segment ParsedSegment) []TanMedia {
	media := make([]TanMedia, 0, segment.DEGCount())
	for index := 1; index < segment.DEGCount(); index++ {
		group := segment.DEG(index)
		if group.Len() < 3 {
			continue
		}
		status := group.Text(0)
		if status != "A" && status != "1" {
			continue
		}
		name := group.Text(2)
		if name == "" {
			continue
		}
		media = append(media, TanMedia{name: name})
	}
	return media
}

// ParseHITAB is the segment-named form of [ParseTanMedia].
func ParseHITAB(segment ParsedSegment) []TanMedia {
	return ParseTanMedia(segment)
}

// TanMedia returns the active TAN media from every HITAB segment, in wire
// order.
func (r Response) TanMedia() []TanMedia {
	media := make([]TanMedia, 0)
	for _, segment := range r.findSegments("HITAB") {
		media = append(media, ParseTanMedia(segment)...)
	}
	return media
}
