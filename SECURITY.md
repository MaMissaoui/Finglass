# Security

Finglass handles bank logins and financial data, so security reports are welcome.

## Reporting a vulnerability

Please use GitHub's [private vulnerability reporting](https://github.com/MaMissaoui/Finglass/security/advisories/new)
rather than a public issue. Don't include real account data, PINs or TANs in a report.

## Keeping dependencies current

A known vulnerability that affects the shipped app is fixed the same day: a fix PR, merged when CI is green, and a
patch release. "Affects the shipped app" means the Go standard library or toolchain; a Go module that `govulncheck`
reports as reachable; a frontend production dependency; or the native code compiled into the app (SQLite/SQLCipher
and libtomcrypt bundled in `go-sqlcipher`, the Wails macOS bridge).

Findings in development tools only are fixed with the next routine update. Every Dependabot alert is resolved, also
when `govulncheck` says the code is not reachable; reachability only sets the urgency.

Automation keeps this honest: Dependabot watches Go, npm and GitHub Actions (security PRs are separate and come
first); a daily Security workflow runs `govulncheck` on macOS, Linux and Windows plus `npm audit`, and opens an issue
when it fails; CI and release builds use the latest Go and Node patch releases; and every release binary is scanned
with `govulncheck -mode=binary` before it is published.

Native code is checked by hand, because no scanner sees it: on every SQLite or SQLCipher security advisory and before
each release, compare the versions bundled in `go-sqlcipher` with upstream.

## Design principles

- **No payment code.** Finglass is read-only: it cannot send transfers, scheduled transfers or standing orders.
- **Encrypted at rest.** The database is encrypted with SQLCipher. The passphrase is asked at every start and is never
  stored.
- **Bank PINs** are only kept when you tick "Remember PIN", and only in the operating system's credential store
  (macOS Keychain, Windows Credential Manager, Linux Secret Service). TANs are never stored. Neither is ever logged.
- **Secrets are wiped from memory** after use where the code holds them.
- **No network access** except to the bank servers you configure.
