# finglass-probe (Go)

`finglass-probe` is a small, read-only feasibility probe for the Go FinTS
client in `internal/fints`. It answers one question: can this bank and login
provide account balances and statements over FinTS PIN/TAN, including
pushTAN?

You run it yourself against your own bank. It is not part of the Finglass
desktop app and it is not published.

## What it prints

For every SEPA account that the login can see, it prints one line containing:

- the IBAN masked to its last four characters, with the original grouping kept;
- the account currency;
- whether a balance is `present` or `absent` (never the amount);
- the statement result: number of booked transactions, earliest and latest
  booking date, and the parsed format (`MT940` or `camt`).

Before the account loop it asks the bank for the SEPA account details (HKSPA).
Accounts without an IBAN, without balance/statement access, or without the
identifiers the bank's request versions need (for example securities depots and
some credit cards) are listed as
`IBAN <masked, or -> | skipped (no balance/statement access)` and do not count
for the decision.

At the end it prints either `RESULT: GO` or `RESULT: NO-GO (<reason>)`, where
the reason is one of `login rejected`, `TAN method unsupported`, `TAN timeout`,
`statements unavailable`, or `other: <error kind>`.

When the bank supplied return codes, the reason includes them, for example
`RESULT: NO-GO (login rejected: 9942)` or
`RESULT: NO-GO (other: BankError: 9010, 9050)`. Only the four-digit codes are
printed, never a bank message.

It never prints the PIN, a TAN, the product id, the user id, booking text,
counterparty names, full IBANs, amounts, account owners, or bank messages that
may contain account numbers. For a typed TAN method it prints the bank's
challenge, because you need it to produce the TAN.

Exit codes are `0` for GO, `1` for NO-GO, and `2` for invalid input.

## Requirements

You need a FinTS product registration id from the **Deutsche Kreditwirtschaft
(DK)**. The probe does not hardcode one and does not ship one. Register your
application and use the assigned id.

The product id is the DK registration id from your password manager. Paste it
only at the probe's `Product id: ` prompt, never on the command line (shell
history is a file), into a file, or into a chat.

The probe does not send payments. It has no transfer, scheduled-transfer,
standing-order, or other order code.

## Environment variables

| Variable | Required | Meaning |
|---|---|---|
| `FINGLASS_FINTS_PRODUCT_ID` | no | FinTS product registration id from the DK. When unset, the probe asks for it without echo. |
| `FINGLASS_PROBE_BLZ` | yes | Bank code (`Bankleitzahl`), exactly 8 digits |
| `FINGLASS_PROBE_USER` | yes | Online-banking login / user id |
| `FINGLASS_PROBE_URL` | yes | FinTS server URL. The probe has no bank registry: use the HTTPS endpoint from your bank. |
| `FINGLASS_PROBE_DAYS` | no | Statement period in days, 1–90. Defaults to 30. |

The PIN is read from the terminal without echo. For a non-decoupled TAN method
the TAN is read the same way. For pushTAN, confirm in your banking app when the
probe asks.

## Example

Build the probe:

```sh
go build -o build/finglass-probe ./cmd/finglass-probe
```

Then run it against your own bank:

```sh
FINGLASS_PROBE_BLZ=YOUR_BLZ \
FINGLASS_PROBE_USER=YOUR_LOGIN \
FINGLASS_PROBE_URL=https://fints.example.invalid/ \
FINGLASS_PROBE_DAYS=30 \
build/finglass-probe
```

The probe asks for the product id and the PIN without echo. The URL is
required; there is no lookup by bank code in the Go probe.

## TAN methods

- **pushTAN (decoupled):** the probe asks you to confirm in the banking app and
  polls at the interval reported by the bank, giving up after five minutes.
- **Typed TAN methods:** the probe prints the bank's challenge and reads the TAN
  without echo.
- **chipTAN flicker and photoTAN:** these are not supported. The probe ends the
  dialog and asks you to choose pushTAN in your bank settings.

## Reading the result

A wrong PIN is only one possible reason for `login rejected`. The bank codes
`9340`, `9910`, `9930`, `9931` and `9942` are PIN codes. Any other code in a
`login rejected` line usually is not a wrong PIN: check the code before
retrying, because every retry with a wrong PIN counts against the bank's PIN
limit.

A NO-GO with a bank code right after the login usually names what the bank
refused. Look the code up before retrying with the PIN.

`statements unavailable` means the balance may still be present but the
statement request or its parse failed. Accounts without an IBAN, without
balance/statement access, or without the identifiers the bank's request
versions need are listed as skipped and do not make the result a NO-GO.
