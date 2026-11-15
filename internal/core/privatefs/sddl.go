package privatefs

import (
	"errors"
	"strings"
)

// The parser errors never carry an SDDL string or a SID: Check adds only the
// path before returning them.
var (
	errDACLMissing    = errors.New("the DACL is missing")
	errDACLMalformed  = errors.New("the DACL is not valid")
	errDACLNotPrivate = errors.New("the DACL grants access beyond the current user")
)

// checkSDDL accepts an SDDL string whose DACL allows only userSID, SYSTEM and
// Administrators. Every ACE must be an allow ACE. The well-known aliases and
// their full SIDs are both accepted.
func checkSDDL(sddl, userSID string) error {
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

		if err := checkACE(section[position+1:end], userSID); err != nil {
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

// checkACE accepts one allow ACE for userSID, SYSTEM or Administrators.
func checkACE(ace, userSID string) error {
	fields := strings.Split(ace, ";")
	if len(fields) < 6 || fields[0] != "A" {
		return errDACLNotPrivate
	}

	sid := strings.TrimSpace(fields[len(fields)-1])
	if sid == "" {
		return errDACLMalformed
	}
	if userSID != "" && strings.EqualFold(sid, userSID) {
		return nil
	}

	switch strings.ToUpper(sid) {
	case "SY", "S-1-5-18", "BA", "S-1-5-32-544":
		return nil
	default:
		return errDACLNotPrivate
	}
}
