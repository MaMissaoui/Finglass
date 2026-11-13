// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

// Package fints is the read-only FinTS 3.0 wire codec.
//
// It knows the FinTS data syntax, the PIN/TAN message envelope, the
// allow-listed request segments, generic response parsing with return codes,
// and typed read-only response values: TAN methods and media, accounts, TAN
// challenges, balances and raw MT940/camt statements. It has no network code,
// no dialog state machine, no BPD/UPD cache, no TAN flow and no statement
// parsing; those arrive in later slices behind the banksync gateway port.
//
// Everything in this package is offline. [ParseMessage] and [ParseResponse]
// treat their input as untrusted and never panic.
//
// # Secrets
//
// A PIN or TAN is a *secret.Text. [BuildMessage] writes its bytes into the
// returned message only inside [secret.Text.Use]; the bytes are never turned
// into a Go string, never formatted and never put into an error. The returned
// message contains the PIN (and TAN, when given), so the caller must wipe it
// with [Wipe] when done.
//
// # Read-only scope
//
// Outgoing segments can only be created through the constructors in this
// package, and those exist only for the read-only message types: dialog
// identification and end, processing preparation, synchronisation, TAN
// handling, TAN media, SEPA accounts, balance, MT940 statements and camt
// statements. The encoder checks the segment type against a fixed allowlist
// again, so even an internal bug cannot emit an order segment. There is no
// payment code and no securities-depot request.
//
// # Port
//
// The wire syntax is ported from fints-rs 0.2.0's serializer.rs and parser.rs,
// the envelope from message.rs, the request builders from segments/builder.rs
// and the read-only response helpers and typed response parsers from
// segments/response.rs and types.rs.
//
// Deliberate differences from fints-rs:
//
//   - the securities-depot request and everything depot-related are dropped,
//     including HIWPD, ISIN and WKN parsing;
//   - HKCAZ for camt statements is added, with HICAZS camt formats and HICAZ
//     raw camt documents;
//   - money is a decimal.Decimal, never a float;
//   - bank text stays in unexported fields, and formatting masks IBANs and
//     account numbers to their last four characters;
//   - HKTAN process 4 takes a closed TanSegmentRef instead of any string, and
//     process S is not ported;
//   - text outside ISO-8859-1 is an error instead of being replaced, and text
//     is decoded as ISO-8859-1 rather than Windows-1252;
//   - empty input and a segment without its terminating apostrophe are parse
//     errors, and an unparseable HNVSD payload is an error rather than a
//     warning;
//   - a missing balance date stays zero instead of becoming the current date;
//   - constructors report bad arguments at encode time instead of panicking.
//
// fints-rs has no anonymous or unsigned envelope variant, so none is ported:
// every message carries HNSHK/HNSHA with a PIN.
package fints
