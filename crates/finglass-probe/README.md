# finglass-probe

`finglass-probe` is a small, read-only feasibility probe for the Rust FinTS
client crate [`fints-rs`](https://crates.io/crates/fints-rs). It answers one
question: can this bank and login provide account balances and statements over
FinTS PIN/TAN, including pushTAN?

You run it yourself against your own bank. It is not part of the Finglass
desktop app and it is not published.

## What it prints

For every SEPA account that the login can see, it prints one line containing:

- the IBAN masked to its last four characters, with the original grouping kept;
- the account currency;
- whether a balance is `present` or `absent` (never the amount);
- the statement result: number of booked transactions, earliest and latest
  booking date, and the parsed format (`MT940`).

At the end it prints either `RESULT: GO` or `RESULT: NO-GO (<reason>)`, where
the reason is one of `login rejected`, `TAN method unsupported`, `TAN timeout`,
`statements unavailable`, or `other: <error kind>`.

It never prints the PIN, a TAN, the user id, booking text, counterparty names,
full IBANs, or bank messages that may contain account numbers.

Exit codes are `0` for GO, `1` for NO-GO, and `2` for invalid input.

## Requirements

You need a FinTS product registration id from the **Deutsche Kreditwirtschaft
(DK)**. The probe does not hardcode one and does not ship one. Register your
application and use the assigned id in `FINGLASS_FINTS_PRODUCT_ID`.

The probe does not send payments. It has no transfer, scheduled-transfer,
standing-order, or other order code.

## Environment variables

| Variable | Required | Meaning |
|---|---|---|
| `FINGLASS_FINTS_PRODUCT_ID` | yes | FinTS product registration id from the DK |
| `FINGLASS_PROBE_BLZ` | yes | Bank code (`Bankleitzahl`), exactly 8 digits |
| `FINGLASS_PROBE_USER` | yes | Online-banking login / user id |
| `FINGLASS_PROBE_URL` | no | FinTS server URL. If omitted, the URL is looked up from the BLZ in the crate's bank registry. An unknown BLZ without a URL is an error. |
| `FINGLASS_PROBE_DAYS` | no | Statement period in days, 1–90. Defaults to 30. |

The PIN is read from the terminal without echo. For a non-decoupled TAN method
the TAN is read the same way. For pushTAN, confirm in your banking app when the
probe asks.

## Example

```sh
FINGLASS_FINTS_PRODUCT_ID=YOUR_REGISTRATION_ID \
FINGLASS_PROBE_BLZ=12030000 \
FINGLASS_PROBE_USER=YOUR_LOGIN \
FINGLASS_PROBE_DAYS=30 \
cargo run -p finglass-probe
```

The probe reads the PIN from the terminal. To use a non-registry FinTS endpoint,
also set `FINGLASS_PROBE_URL=https://example.invalid/fints`.

## TAN methods

- **pushTAN (decoupled):** the probe asks you to confirm in the banking app and
  polls at the interval reported by the bank, giving up after five minutes.
- **Typed TAN methods:** the probe prints the bank's challenge and reads the TAN
  without echo.
- **chipTAN flicker and photoTAN:** these are not supported. The probe ends the
  dialog and asks you to choose pushTAN in your bank settings.
