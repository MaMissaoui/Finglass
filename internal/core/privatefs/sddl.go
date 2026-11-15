package privatefs

import (
	"errors"
	"fmt"
	"strings"
)

// The parser errors never carry an SDDL string: Check adds only the path before
// returning them. A SID is not secret, so a rejected ACE names its SID.
var (
	errDACLMissing    = errors.New("the DACL is missing")
	errDACLMalformed  = errors.New("the DACL is not valid")
	errDACLNotPrivate = errors.New("the DACL grants access beyond the current user")
)

// checkSDDL accepts an SDDL string whose DACL allows only SIDs that allowed
// accepts. Every ACE must be an allow ACE.
//
// The comparison belongs to the caller: Windows prints a SID as an alias (LA,
// SY, BA) or in its S-1-… form, so text equality does not work there. Windows
// converts each ACE's SID to a real SID and compares; other builds and the
// tests match SID strings.
func checkSDDL(sddl string, allowed func(sid string) bool) error {
	section := daclSection(sddl)
	if section == "" {
		return errDACLMissing
	}

	open := strings.IndexByte(section, '(')
	if open < 0 {
		return errDACLMissing
	}

	found := false
	position := open
	for position < len(section) && section[position] == '(' {
		end := strings.IndexByte(section[position+1:], ')')
		if end < 0 {
			return errDACLMalformed
		}
		end += position + 1

		if err := checkACE(section[position+1:end], allowed); err != nil {
			return err
		}
		found = true
		position = end + 1
	}

	if !found {
		return errDACLMissing
	}

	// The only section that may follow the DACL is the SACL.
	if rest := strings.TrimSpace(section[position:]); rest != "" && !strings.HasPrefix(rest, "S:") {
		return errDACLMalformed
	}
	return nil
}

// daclSection returns the part of sddl after "D:", without a following SACL.
func daclSection(sddl string) string {
	start := strings.Index(sddl, "D:")
	if start < 0 {
		return ""
	}

	section := sddl[start+2:]
	if end := strings.Index(section, "S:"); end >= 0 {
		return section[:end]
	}
	return section
}

// checkACE accepts one allow ACE whose SID allowed accepts.
func checkACE(ace string, allowed func(sid string) bool) error {
	fields := strings.Split(ace, ";")
	if len(fields) < 6 || fields[0] != "A" {
		return errDACLNotPrivate
	}

	sid := strings.TrimSpace(fields[len(fields)-1])
	if sid == "" {
		return errDACLMalformed
	}
	if allowed(sid) {
		return nil
	}
	return fmt.Errorf("%w: %s", errDACLNotPrivate, sid)
}
