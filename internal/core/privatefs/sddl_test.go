package privatefs

import (
	"errors"
	"strings"
	"testing"
)

// allowedSIDs returns a matcher for the given SID strings, ignoring case and
// the surrounding spaces. SDDL may print a SID as an alias (SY, BA, LA) or in
// its S-1-… form, so the caller lists both.
func allowedSIDs(sids ...string) func(sid string) bool {
	accepted := make(map[string]struct{}, len(sids))
	for _, sid := range sids {
		accepted[strings.ToUpper(strings.TrimSpace(sid))] = struct{}{}
	}
	return func(sid string) bool {
		_, ok := accepted[strings.ToUpper(strings.TrimSpace(sid))]
		return ok
	}
}

func TestCheckSDDL(t *testing.T) {
	const userSID = "S-1-5-21-100-200-300-1001"

	tests := []struct {
		name string
		sddl string
		// userSID is the SID the matcher treats as the current user.
		userSID string
		// userAlias is a further SID string the matcher accepts as the user,
		// for a run whose user prints as an alias.
		userAlias string
		wantErr   bool
	}{
		{
			name:    "private folder",
			sddl:    "D:P(A;OICI;FA;;;" + userSID + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)",
			userSID: userSID,
		},
		{
			name:    "private file",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;SY)(A;;FA;;;BA)",
			userSID: userSID,
		},
		{
			name:    "full SIDs for SYSTEM and Administrators",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;S-1-5-18)(A;;FA;;;S-1-5-32-544)",
			userSID: userSID,
		},
		{
			name:    "owner group and SACL around the DACL",
			sddl:    "O:S-1-5-21-100-200-300-1001G:S-1-5-21-100-200-300-513D:P(A;;FA;;;" + userSID + ")(A;;FA;;;SY)(A;;FA;;;BA)S:(AU;SA;FA;;;WD)",
			userSID: userSID,
		},
		{
			name:    "built-in administrator alias for the user",
			sddl:    "D:P(A;OICI;FA;;;LA)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)",
			userSID: userSID,
			// A runner that is the built-in Administrator prints its SID as
			// the LA alias, so the matcher must treat LA as the user.
			userAlias: "LA",
		},
		{
			name:    "built-in administrator alias unknown to the matcher",
			sddl:    "D:P(A;OICI;FA;;;LA)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "deny ACE",
			sddl:    "D:P(D;;FA;;;WD)(A;;FA;;;" + userSID + ")",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "audit ACE",
			sddl:    "D:P(AU;FA;FA;;;WD)(A;;FA;;;" + userSID + ")",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "object allow ACE",
			sddl:    "D:P(OA;;FA;ab721a53-1e2f-11d0-9819-00aa0040529b;;" + userSID + ")",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "Everyone alias",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;WD)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "Everyone full SID",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;S-1-1-0)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "Authenticated Users alias",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;AU)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "Authenticated Users full SID",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;S-1-5-11)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "Users alias",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;BU)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "Users full SID",
			sddl:    "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;S-1-5-32-545)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "another user",
			sddl:    "D:P(A;;FA;;;S-1-5-21-100-200-300-1002)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "missing DACL",
			sddl:    "O:S-1-5-21-100-200-300-1001G:S-1-5-21-100-200-300-513",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "null DACL",
			sddl:    "D:NO_ACCESS_CONTROL",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "empty DACL",
			sddl:    "D:P",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "empty SID",
			sddl:    "D:P(A;;FA;;;)",
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "unterminated ACE",
			sddl:    "D:P(A;;FA;;;" + userSID,
			userSID: userSID,
			wantErr: true,
		},
		{
			name:    "lowercase allow type",
			sddl:    "D:P(a;;FA;;;" + userSID + ")",
			userSID: userSID,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sids := []string{test.userSID, "SY", "S-1-5-18", "BA", "S-1-5-32-544"}
			if test.userAlias != "" {
				sids = append(sids, test.userAlias)
			}
			err := checkSDDL(test.sddl, allowedSIDs(sids...))
			if test.wantErr && err == nil {
				t.Fatal("checkSDDL accepted the DACL")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("checkSDDL error = %v", err)
			}
		})
	}
}

// TestCheckSDDLNamesTheRejectedSID pins the error text: a rejected ACE says
// which SID it carries, which is what makes a CI failure diagnosable. A SID is
// not a secret.
func TestCheckSDDLNamesTheRejectedSID(t *testing.T) {
	const (
		userSID    = "S-1-5-21-100-200-300-1001"
		rejectedID = "S-1-5-21-100-200-300-1002"
	)

	err := checkSDDL("D:P(A;;FA;;;"+rejectedID+")", allowedSIDs(userSID))
	if !errors.Is(err, errDACLNotPrivate) {
		t.Fatalf("checkSDDL error = %v, want %v", err, errDACLNotPrivate)
	}
	if !strings.Contains(err.Error(), rejectedID) {
		t.Fatalf("checkSDDL error = %v, want it to name %s", err, rejectedID)
	}
}
