package banksync

import (
	"sort"
	"strings"
)

// The business transactions a bank login offers, detected from the bank's
// BPD/UPD during a dialog.
//
// Only read jobs are represented. An unknown set (never synced, or a login that
// reports nothing) is treated optimistically: the operation is attempted and
// the bank decides.
type BankCapabilities struct {
	// Jobs holds the BPD/UPD job names the login supports, limited to [Probed].
	Jobs map[string]struct{}
}

// The read jobs Finglass uses.
const (
	// JobStatementsMT940 is the MT940 statement job.
	JobStatementsMT940 = "KUmsAll"
	// JobStatementsCAMT is the CAMT statement job.
	JobStatementsCAMT = "KUmsAllCamt"
	// JobBalance is the balance job.
	JobBalance = "SaldoReq"
	// JobAccounts is the account list job.
	JobAccounts = "HKKAU"
	// JobTANMedia is the TAN medium list job.
	JobTANMedia = "HKTAN"
)

// Probed lists the read jobs Finglass uses. Only these are probed and stored.
var Probed = []string{
	JobStatementsMT940,
	JobStatementsCAMT,
	JobBalance,
	JobAccounts,
	JobTANMedia,
}

// Supports reports whether the login supports job.
func (c BankCapabilities) Supports(job string) bool {
	_, ok := c.Jobs[job]
	return ok
}

// SupportsMT940 reports whether the login supports MT940 statements.
func (c BankCapabilities) SupportsMT940() bool {
	return c.Supports(JobStatementsMT940)
}

// SupportsCAMT reports whether the login supports CAMT statements.
func (c BankCapabilities) SupportsCAMT() bool {
	return c.Supports(JobStatementsCAMT)
}

// SupportsStatements reports whether the login supports any statement format.
func (c BankCapabilities) SupportsStatements() bool {
	return c.SupportsMT940() || c.SupportsCAMT()
}

// SupportsBalance reports whether the login supports balances.
func (c BankCapabilities) SupportsBalance() bool {
	return c.Supports(JobBalance)
}

// Serialize returns a stable form for the bank_connections.capabilities column:
// the jobs sorted and joined with commas, like Rust's BTreeSet.
func (c BankCapabilities) Serialize() string {
	jobs := make([]string, 0, len(c.Jobs))
	for job := range c.Jobs {
		jobs = append(jobs, job)
	}
	sort.Strings(jobs)
	return strings.Join(jobs, ",")
}

// Detect builds capabilities from a support predicate, keeping only jobs
// Finglass knows.
func DetectCapabilities(isSupported func(job string) bool) BankCapabilities {
	jobs := make(map[string]struct{}, len(Probed))
	for _, job := range Probed {
		if isSupported(job) {
			jobs[job] = struct{}{}
		}
	}
	return BankCapabilities{Jobs: jobs}
}

// Detect is [DetectCapabilities] as a method, matching Rust's
// BankCapabilities::detect.
func (BankCapabilities) Detect(isSupported func(job string) bool) BankCapabilities {
	return DetectCapabilities(isSupported)
}

// ParseCapabilities parses a stored value. A missing, blank or empty set means
// unknown.
func ParseCapabilities(serialized *string) *BankCapabilities {
	if serialized == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*serialized)
	if trimmed == "" {
		return nil
	}
	jobs := make(map[string]struct{})
	for _, job := range strings.Split(trimmed, ",") {
		job = strings.TrimSpace(job)
		if job != "" {
			jobs[job] = struct{}{}
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	return &BankCapabilities{Jobs: jobs}
}

// Parse is [ParseCapabilities] as a method, matching Rust's
// BankCapabilities::parse.
func (BankCapabilities) Parse(serialized *string) *BankCapabilities {
	return ParseCapabilities(serialized)
}
