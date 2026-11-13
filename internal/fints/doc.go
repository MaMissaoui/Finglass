// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

// Package fints is the read-only FinTS 3.0 wire codec, dialog and TAN flow.
//
// It knows the FinTS data syntax, the PIN/TAN message envelope, the
// allow-listed request segments, generic response parsing with return codes,
// typed read-only response values (TAN methods and media, accounts, TAN
// challenges, balances and raw MT940/camt statements), the bank parameter
// state, the dialog state machine and the balance/statement workflows. It has
// no network code and no statement parsing; transport, persistence and MT940
// parsing arrive in later slices behind the banksync gateway port.
//
// Everything in this package is offline. [ParseMessage] and [ParseResponse]
// treat their input as untrusted and never panic. A [Dialog] talks to the bank
// through the injected [Transport] interface only.
//
// # Secrets
//
// A PIN or TAN is a *secret.Text. [BuildMessage] writes its bytes into the
// returned message only inside [secret.Text.Use]; the bytes are never turned
// into a Go string, never formatted and never put into an error. The returned
// message contains the PIN (and TAN, when given), so the caller must wipe it
// with [Wipe] when done. [Dialog] wipes every request immediately after
// [Transport.Exchange] returns, also when it fails or panics.
//
// The dialog holds the caller's PIN pointer and never copies or wipes it.
// [Dialog.SubmitTAN] always wipes the caller's TAN before returning and stores
// no TAN. [DialogError], [BankParams] and every result type print only kinds,
// codes, counts and masked account tails.
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
// segments/response.rs and types.rs. The dialog state machine and bank
// parameters are ported from protocol.rs; the balance/statement parts are
// ported from workflow.rs and flow.rs.
//
// Deliberate differences from fints-rs:
//
//   - the securities-depot request and everything depot-related are dropped,
//     including HKWPD, ISIN and WKN parsing;
//   - HKCAZ for camt statements is added, with HICAZS camt formats and HICAZ
//     raw camt documents; the workflow chooses it only for the accepted camt
//     URNs;
//   - money is a decimal.Decimal, never a float;
//   - bank text stays in unexported fields, and formatting masks IBANs and
//     account numbers to their last four characters;
//   - HKTAN process 4 takes a closed TanSegmentRef instead of any string;
//   - text outside ISO-8859-1 is an error instead of being replaced, and text
//     is decoded as ISO-8859-1 rather than Windows-1252;
//   - empty input and a segment without its terminating apostrophe are parse
//     errors, and an unparseable HNVSD payload is an error rather than a
//     warning;
//   - a missing balance date stays zero instead of becoming the current date;
//   - constructors report bad arguments at encode time instead of panicking;
//   - the dialog is one type with a checked state instead of Rust's typestate
//     generic, so every invalid transition returns a typed error and never
//     sends;
//   - HKEND is sent best effort on error paths once a dialog id exists, and a
//     cancelled context does not suppress it;
//   - decoupled polling is one bounded loop with the spec's defaults (5 s
//     first, 2 s next, 1 s minimum, 60 polls absent, 120 hard cap) rather than
//     fints-rs's decoupled_params values;
//   - a TAN challenge is decoupled only for a 3955/3956 answer, not merely
//     because the selected method is decoupled;
//   - touchdown paging stops with a typed error after 100 pages.
//
// fints-rs has no anonymous or unsigned envelope variant, so none is ported:
// every message carries HNSHK/HNSHA with a PIN.
package fints
