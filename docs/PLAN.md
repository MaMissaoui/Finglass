# Finglass plan

Finglass is a read-only, local-first personal finance desktop app in Rust with a Slint UI. It carries over what
[FinDesk](https://github.com/MaMissaoui/FinDesk) does, except payments. This file is the work queue: keep the checkboxes
in line with the code.

## Scope

**In:** accounts, bank file imports, live bank sync (read-only FinTS: balances and statements), transactions,
categories and rules, dashboard, receipts with OCR, reports and tax exports, backups, profiles, an encrypted database,
EN/DE/FR, installers for macOS, Windows and Linux.

**Platforms:** macOS first. Until Finglass does everything FinDesk does except payments, CI builds and tests on macOS
only, and Windows and Linux are neither built nor tested. Code stays portable, with no macOS-only APIs outside
platform-guarded code. Once that point is reached, Windows and Linux go back into the CI matrix and get their
installers.

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
- [ ] Once the DK registration is in and its terms for the FinTS institute list are known, decide how Finglass gets
      bank server URLs: the user loads the institute list they received (Finglass never redistributes it), or, only
      if the terms allow it, the list ships with the app. Until then the add-bank dialog asks for the server URL.

### M1 — Foundation

- [x] M1a: data folder (`FINGLASS_HOME`, profile slugs), passphrase rules (NFC, at least 10 characters, no surrounding
      whitespace), encrypted database (create, open, change passphrase; one connection, passphrase dropped after
      open), versioned migrations, settings, audit log.
- [x] M1b: domain tables as migration 2, designed from FinDesk's tables so M8 maps one to one (no payment tables);
      money type in integer minor units; repositories for accounts, groups, categories, rules and transactions.
- [x] M1c: profiles and the remaining repositories (#12).
- [x] Profile management in the app: list, create, rename, delete and the live switch (M8d-1 #29, M8d-2 #36).

### M2 — App shell

- [x] Unlock screen: create, unlock and change the passphrase (#11).
- [x] Main window with sidebar navigation, light and dark theme (#11).
- [x] M2c: top bar (sidebar toggle, language and theme switch) and the collapsible icon sidebar (#33).
- [x] M2d: the standard widgets follow the app's light/dark choice (#35).
- [x] Translations EN/DE/FR (a typed key enum and one table per language, with a test that every key exists in all
      three) (#7).
- [x] Settings and About (with the AboutSlint badge) (#11, #25).
- [x] Headless screenshot tests with Slint's software renderer, if it can render to an image in tests. Otherwise a
      documented manual check. (Software renderer; `FINGLASS_SCREENSHOTS=<dir> cargo test -p finglass --test
      screenshots`.)

### M3 — Accounts and imports

- [x] Manual accounts (IBAN check, currency, account type), groups and the account panel (#18, #26).
- [x] Parsers for CSV (bank presets and detection), MT940, camt.052/053, OFX, QIF and XLSX. FinDesk's test files are
      reused as fixtures.
- [x] Import service with duplicate detection (bank booking id or running balance) (#6).
- [x] Import dialog with preview and summary on the Transactions page (#39).

### M4 — Transactions, categories, dashboard

- [x] Transactions view state (#22).
- [x] Transactions page: list, filters, panel, selection, details (#34); import, re-apply rules, rule from booking,
      CSV export, receipts (#39).
- [x] Categories, rules and rule import/export; auto-categorisation on import (#6, #16, #25).
- [x] Dashboard: balance, monthly income and expenses, expenses by category, budgets. Totals per currency, primary
      currency for overviews (#9, #23).

### M5 — Live sync, read-only (after M0 go)

- [ ] Bank connections:
  - bank directory lookup (M5d): search by name, BLZ, BIC or IBAN in the Bundesbank's bank code file, shipped
    unchanged with "Quelle: Deutsche Bundesbank" (its terms allow passing it on unaltered). Server URLs wait for the
    DK registration decision above;
  - TAN methods: pushTAN, photoTAN and QR; chipTAN flicker waits for M5b, since FinDesk's flicker rendering comes
    from HBCI4Java and the code's format depends on the adapter;
  - TAN media.
- [ ] "Remember PIN": only in the OS credential store (`keyring`), saved only after a successful dialog, and deleted
      when it is rejected, when the login changes or when the bank is removed.
- [x] Bank message text is never stored unredacted, and never in the audit log (#21).
- [x] M5a core: `sync` (gateway port with fakes, client, service, PIN handling with profile-scoped keys and
      `credentials/bank-<id>` markers, redaction). A server-URL change also forgets the PIN (stricter than FinDesk).
      Booking texts are stored as the bank sent them; error, warning and note texts are masked.
- [ ] M5b prerequisites, from the M5c-3 review: a sync is one FinTS dialog per login, so two syncs of one bank must
      never overlap (the UI keeps the live claim; re-check once a real gateway can block for minutes).
- [ ] FinTS protocol trace (FinDesk's Settings switch and "save trace"): only the i18n keys exist. It records what
      the adapter sends and receives, so it comes with M5b, with the same masking as the bank message text.
- [ ] M5b: the fints-rs adapter (after the M0 probe). Login state (system id, bank parameter data) goes into
      `bank_connections`' server-state columns cleared by `clear_login_state`, never into a side file.
- [x] M5c-1: `app::banking` (bank-login state, sync one/all, history start) and the prompt bridge, with an
      `UnavailableGateway` until M5b (#31).
- [x] M5c-2: the macOS Keychain store; `delete_profile` calls `forget_pins_for_profile` with it (#37).
- [x] M5c-3: bank UI on the Accounts page (add, edit, sync, sync all, history, PIN and TAN prompts) (#42).
      `FinTsClient::new` gets `AppPaths::profile_dir(slug)`. A profile switch cancels an open prompt. Online banks
      are deleted through `BankingState::remove_bank`; one profile `Busy` is shared by sync and the Accounts deletes.
- [ ] Follow-up: statement imports and the FinDesk import don't take the profile `Busy` yet.

### M6 — Receipts and OCR

- [x] Receipt storage per profile, PDF text extraction, Tesseract OCR for images and scans.
- [x] Receipt parser (amount, date, merchant) and matching to bookings; Documents page (#10, #13, #14, #28).

### M7 — Reports and tax

- [x] Reports by period: categories, recurring costs, cost per year and month (#17, #20, #24).
- [x] Tax groups and missing receipts; PDF and XLSX export. Printing goes through the system PDF viewer.

### M8 — Backups and FinDesk import

- [x] M8a: backup and two-phase restore of the encrypted database and the documents (core).
- [x] M8b: Settings section for backup and restore; `apply_pending_restore` called at startup, before the unlock
      window; after a restore staged with the backup's passphrase, the unlock screen says the backup's passphrase is
      now needed (reuse FinDesk's wording from `UnlockViewModel.kt`) (#29).
- [x] Unlock screen: list of leftover `pre-restore-*` folders, moved-aside deleted profiles and a staged restore that
      still hold unencrypted documents, display only as in FinDesk; the label names the profile (M8f #40). Finglass
      never has plaintext database copies.
- [x] Single-instance lock taken before `app::startup::prepare`, so a second launch can't apply a staged restore
      while the first instance has the database open (#32).
- [x] One-time import from a FinDesk database. It reads a copy you select, with its passphrase (SQLCipher 4, `cipher`
      sqlcipher, `legacy` 4), and brings over accounts, transactions, categories, rules and documents. FinDesk's
      payment orders are never imported.
  - [x] M8c core: `findesk_import` (copy opened, one transaction, ids kept, hashes verbatim, documents confined to
        FinDesk's `documents/`, no payment orders, PIN flags or capabilities) (#15).
  - [x] M8d-3: Settings entry, folder picker starting in FinDesk's data folder, passphrase, report, reload (#38).
  - [ ] Follow-up: run the empty-target check inside the import transaction, not just before it.

- [ ] M8g: first-run migration from the FinDesk install on this Mac: offered once when Finglass has no data and
      FinDesk is found; every FinDesk profile becomes a Finglass profile with the same name, FinDesk's data is only
      read.

### M9 — Packaging and release

- [x] macOS: `Finglass.app` and a DMG (candidate: `cargo-packager`), with the icons from `assets/icons`, and a CI job
      that builds it and launches the app once (#27).
- [ ] After full functionality: Windows and Linux back in CI, MSI/EXE and DEB/RPM installers, each launched once in
      CI.
- [ ] Signing (macOS ad-hoc first) and a release workflow.

## Done

- [x] Repository, name, brand assets (icon, favicon, macOS icon, wordmark), workspace skeleton and CI.
