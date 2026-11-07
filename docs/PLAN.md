# Finglass plan

Finglass is a read-only, local-first personal finance desktop app in Rust with a Slint UI. It carries over what
[FinDesk](https://github.com/MaMissaoui/FinDesk) does, except payments. This file is the work queue: keep the checkboxes
in line with the code.

## Scope

**In:** accounts, bank file imports, live bank sync (read-only FinTS: balances and statements), transactions,
categories and rules, dashboard, receipts with OCR, reports and tax exports, backups, profiles, an encrypted database,
EN/DE/FR, installers for macOS, Windows and Linux.

**Out, by design:**

- Transfers, scheduled transfers and standing orders. No code for them, not even read-only lists; no FinTS order
  segments (`HKCCS`, `HKCSE`, `HKCDE` and relatives) are ever built.
- Cloud sync, accounts, telemetry, ads.
- Currency conversion: totals never mix currencies.

## Architecture

- Cargo workspace:
  - `crates/finglass-core` holds all logic and has no UI dependency. Its tests run headless.
  - `crates/finglass` is the Slint app. It renders state from the core and sends actions back, with no logic of its
    own.
- **Data folder:** `FINGLASS_HOME`, otherwise `~/.finglass`. Profiles live in `profiles/<slug>/`. Tests and dev runs
  always set `FINGLASS_HOME` to a temporary folder.
- **Database:** SQLite through `rusqlite` with SQLCipher 4 (`bundled-sqlcipher`). Versioned migrations run through
  `PRAGMA user_version`, one numbered step per change. FinDesk's create-missing-tables approach is not used.
- **Secrets:** passphrases, PINs and TANs are held in `secrecy`/`zeroize` types from input to use. They are never
  logged and never formatted into errors.
- **Concurrency:** the Slint event loop stays free. Work runs on background threads, and results return through
  `slint::invoke_from_event_loop`. Each action can be awaited in tests.
- **Bank sync:** a `FintsGateway` trait is the port, with one adapter over the chosen FinTS library. Tests use fakes and
  never contact a bank.

## Milestones

Each milestone is specified by Claude, implemented by OpenCode, reviewed by Claude and merged as one or more PRs.

### M0 — FinTS feasibility probe (go/no-go for live sync)

- [x] `finglass-probe`, a small CLI on the FinTS library (candidate: `fints-rs`). It runs one dialog: account list,
      balance and statements for the last 30 days, with pushTAN. It prints counts and dates only, never booking text,
      account numbers or the PIN.
- [ ] You run it against your bank. The code is never run against a real bank by an agent.
- [ ] Decision recorded here:
  - **Go:** live sync goes ahead in M5.
  - **No-go:** Finglass ships with file imports first, and M5 waits for a library or a port of the needed parts.
- [ ] Apply for a FinTS product registration with the Deutsche Kreditwirtschaft under the name Finglass. It must be in
      place before live sync ships.

### M1 — Foundation

- [x] M1a: data folder (`FINGLASS_HOME`, profile slugs), passphrase rules (NFC, at least 10 characters, no surrounding
      whitespace), encrypted database (create, open, change passphrase; one connection, passphrase dropped after
      open), versioned migrations, settings, audit log.
- [ ] M1b: domain tables as migration 2, designed from FinDesk's tables so M8 maps one to one (no payment tables);
      money type in integer minor units; repositories for accounts, groups, categories, rules and transactions.
- [ ] Profile management (list, create, rename, delete) with the app shell.

### M2 — App shell

- [ ] Unlock screen: create, unlock and change the passphrase.
- [ ] Main window with sidebar navigation, light and dark theme.
- [ ] Translations EN/DE/FR (a typed key enum and one table per language, with a test that every key exists in all
      three).
- [ ] Settings and About (with the AboutSlint badge).
- [ ] Headless screenshot tests with Slint's software renderer, if it can render to an image in tests. Otherwise a
      documented manual check.

### M3 — Accounts and imports

- [ ] Manual accounts (IBAN check, currency, account type).
- [ ] Parsers for CSV (bank presets and detection), MT940, camt.052/053, OFX, QIF and XLSX. FinDesk's test files are
      reused as fixtures.
- [ ] Import preview, duplicate detection (bank booking id or running balance) and an import summary.

### M4 — Transactions, categories, dashboard

- [ ] Transaction list with filters (period, account, category, amount, text) and sorting.
- [ ] Categories, rules and rule import/export; auto-categorisation on import.
- [ ] Dashboard: balance, monthly income and expenses, expenses by category, budgets. Totals per currency, primary
      currency for overviews.

### M5 — Live sync, read-only (after M0 go)

- [ ] Bank connections:
  - bank directory lookup;
  - TAN methods: pushTAN, chipTAN flicker, photoTAN and QR;
  - TAN media.
- [ ] "Remember PIN": only in the OS credential store (`keyring`), saved only after a successful dialog, and deleted
      when it is rejected, when the login changes or when the bank is removed.
- [ ] Bank message text is never stored unredacted, and never in the audit log.

### M6 — Receipts and OCR

- [ ] Receipt storage per profile, PDF text extraction, Tesseract OCR for images and scans.
- [ ] Receipt parser (amount, date, merchant) and matching to bookings.

### M7 — Reports and tax

- [ ] Reports by period: categories, recurring costs, cost per year and month.
- [ ] Tax groups and missing receipts; PDF and XLSX export. Printing goes through the system PDF viewer.

### M8 — Backups and FinDesk import

- [ ] Backup and restore of the encrypted database and the documents.
- [ ] One-time import from a FinDesk database. It reads a copy you select, with its passphrase (SQLCipher 4, `cipher`
      sqlcipher, `legacy` 4), and brings over accounts, transactions, categories, rules and documents. FinDesk's
      payment orders are never imported.

### M9 — Packaging and release

- [ ] Installers: DMG, MSI/EXE, DEB/RPM (candidate: `cargo-packager`), with the icons from `assets/icons`.
- [ ] A CI job that builds them on all three systems and launches each installed app once.
- [ ] Signing (macOS ad-hoc first) and a release workflow.

## Done

- [x] Repository, name, brand assets (icon, favicon, macOS icon, wordmark), workspace skeleton and CI.
