package banksync

import (
	"reflect"
	"testing"
)

// stringPointer returns a pointer to value for optional fields.
func stringPointer(value string) *string {
	return &value
}

func TestDetectKeepsOnlyReadJobsFinglassKnows(t *testing.T) {
	var capabilities BankCapabilities
	detected := capabilities.Detect(func(job string) bool {
		return job == JobBalance || job == "SOME_UNKNOWN_JOB"
	})
	wanted := BankCapabilities{Jobs: map[string]struct{}{JobBalance: {}}}
	if !reflect.DeepEqual(detected, wanted) {
		t.Fatalf("detected = %#v, want %#v", detected, wanted)
	}
	if !detected.SupportsBalance() {
		t.Fatal("detected does not support balances")
	}
	if detected.SupportsStatements() {
		t.Fatal("detected supports statements")
	}
}

func TestSerializeAndParseRoundTrip(t *testing.T) {
	capabilities := BankCapabilities{Jobs: map[string]struct{}{
		JobBalance:         {},
		JobStatementsMT940: {},
	}}
	serialized := capabilities.Serialize()
	parsed := ParseCapabilities(&serialized)
	if !reflect.DeepEqual(parsed, &capabilities) {
		t.Fatalf("parse(serialize()) = %#v, want %#v", parsed, capabilities)
	}

	stored := "SaldoReq,KUmsAll"
	if got := ParseCapabilities(&stored).Serialize(); got != "KUmsAll,SaldoReq" {
		t.Fatalf("serialize = %q, want %q", got, "KUmsAll,SaldoReq")
	}
}

func TestNullOrBlankMeansUnknown(t *testing.T) {
	if ParseCapabilities(nil) != nil {
		t.Fatal("parse(nil) is not nil")
	}
	if parsed := ParseCapabilities(stringPointer("")); parsed != nil {
		t.Fatalf("parse(empty) = %#v, want nil", parsed)
	}
	if parsed := ParseCapabilities(stringPointer("   ")); parsed != nil {
		t.Fatalf("parse(blank) = %#v, want nil", parsed)
	}
	var empty BankCapabilities
	serialized := empty.Serialize()
	if parsed := ParseCapabilities(&serialized); parsed != nil {
		t.Fatalf("parse(default) = %#v, want nil", parsed)
	}
}

func TestStatementSupportCombinesJobs(t *testing.T) {
	capabilities := BankCapabilities{Jobs: map[string]struct{}{JobStatementsCAMT: {}}}
	if !capabilities.SupportsStatements() {
		t.Fatal("does not support statements")
	}
	if capabilities.SupportsMT940() {
		t.Fatal("supports MT940")
	}
	if !capabilities.SupportsCAMT() {
		t.Fatal("does not support CAMT")
	}
}

func TestOnlyReadJobsAreProbed(t *testing.T) {
	probed := make(map[string]struct{}, len(Probed))
	for _, job := range Probed {
		probed[job] = struct{}{}
	}
	wanted := map[string]struct{}{
		JobStatementsMT940: {},
		JobStatementsCAMT:  {},
		JobBalance:         {},
		JobAccounts:        {},
		JobTANMedia:        {},
	}
	if !reflect.DeepEqual(probed, wanted) {
		t.Fatalf("probed = %#v, want %#v", probed, wanted)
	}
}
