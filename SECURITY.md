# Security

Finglass handles bank logins and financial data, so security reports are welcome.

## Reporting a vulnerability

Please use GitHub's [private vulnerability reporting](https://github.com/MaMissaoui/Finglass/security/advisories/new)
rather than a public issue. Don't include real account data, PINs or TANs in a report.

## Design principles

- **No payment code.** Finglass is read-only: it cannot send transfers, scheduled transfers or standing orders.
- **Encrypted at rest.** The database is encrypted with SQLCipher. The passphrase is asked at every start and is never
  stored.
- **Bank PINs** are only kept when you tick "Remember PIN", and only in the operating system's credential store
  (macOS Keychain, Windows Credential Manager, Linux Secret Service). TANs are never stored. Neither is ever logged.
- **Secrets are wiped from memory** after use where the code holds them.
- **No network access** except to the bank servers you configure.
