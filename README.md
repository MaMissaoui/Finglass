<p align="center">
  <img src="assets/brand/finglass-logo.svg" alt="Finglass" width="420">
</p>

<p align="center"><b>A read-only, local-first view of your finances.</b><br>
See every account, booking, receipt and report in one place. Nothing in Finglass can move money.</p>

---

> **Status: alpha, macOS only.** Finglass is being migrated from Rust + Slint to Go + Wails v3
> ([`docs/PLAN.md`](docs/PLAN.md)). The Rust code is gone from `main`, and until the Go UI is finished, `main` builds
> no usable app. **0.3.4 is the release to use:** download its DMG from
> [Releases](https://github.com/MaMissaoui/Finglass/releases) or run `install-mac.command` (below). The last Rust
> source is the `v0.3.4` tag.

## What Finglass is

- **Read-only by design.** Finglass fetches balances and statements and imports bank files. It has no transfers,
  scheduled transfers or standing orders, and contains no code that could send them.
- **Local-first.** Your data stays on your computer in an encrypted database (SQLCipher). No account, no cloud, no
  telemetry.
- **Built in Go** with a [Wails v3](https://v3.wails.io) + Svelte UI (in progress; 0.3.4 is the Rust + Slint app).
- **German banking via FinTS** (in progress: the bank screens are in, the connection itself is not yet): balances and
  statements with pushTAN, chipTAN and photoTAN. Bank file imports (CSV, MT940, camt, OFX, QIF, XLSX) work without any
  bank connection.

Finglass is a sister project of [FinDesk](https://github.com/MaMissaoui/FinDesk) (Kotlin), rebuilt without the payment
features.

## Building

Needs Go (the version in `go.mod`), Node.js and [golangci-lint](https://golangci-lint.run). The checks CI runs:

```sh
bash scripts/ci/go.sh              # gofmt, go vet, golangci-lint, go test
bash scripts/ci/frontend.sh        # svelte-check, Vitest, build, Playwright screenshots
bash scripts/ci/go-package.sh      # universal, ad-hoc-signed Finglass.app with its --self-check
scripts/ci-local.sh                # all of CI's jobs on this Mac
```

The read-only FinTS probe is `cmd/finglass-probe`; see
[`internal/core/fintsprobe/README.md`](internal/core/fintsprobe/README.md).

The icons in `assets/icons` are generated from the SVGs in `assets/brand` by `scripts/render-icons.sh`.

## Install on this Mac

Double-click `install-mac.command` in Finder. It downloads and installs the newest published Finglass
release from GitHub — pre-releases included — so it needs no Rust and no checkout. From a terminal the same
install is:

```sh
scripts/install-mac.sh [options]
```

It installs into `/Applications` by default and picks the DMG for this Mac's architecture. The options are:

- `--install-dir <folder>` puts `Finglass.app` somewhere other than `/Applications`.
- `--version <tag>` installs that release, e.g. `v0.2.2` or `0.2.2`, instead of the newest.
- `--reinstall` installs even when the same version is already there.
- `--check-only` resolves, downloads and verifies the release without installing it.
- `--help` shows the usage.

Quit Finglass first: a previous `Finglass.app` is moved to the Trash, never deleted.

To build and install this checkout instead, use `--from-source`:

```sh
scripts/install-mac.sh --from-source [--force] [--no-update]
```

That builds the Rust app, so it works only on a checkout of `v0.3.4` or older until the Go app is packaged
(PLAN G9). On `main` it fast-forwards a clean checkout from
`origin/main` first; `--no-update` skips that. `--force` installs a checkout with uncommitted changes or a
HEAD that is neither a `v*` release tag nor the fetched `origin/main`.

## Releasing

There is no release workflow until the Go app is packaged (PLAN G9). 0.4.0 will be the first Go release.

## Third-party assets

- The UI font is [IBM Plex Sans](https://www.ibm.com/plex/) (Regular, Medium, SemiBold and Bold) under the SIL Open
  Font License 1.1. The font files and the licence text are in `frontend/public/fonts/` (`OFL.txt`).
- The filled icons in `frontend/src/assets/icons/` are from Google's
  [Material icons](https://fonts.google.com/icons), licensed under Apache-2.0.

## License

[MIT](LICENSE). Release 0.3.4 and older use [Slint](https://slint.dev) under its royalty-free license, which is why
their About screen shows the Slint badge.
