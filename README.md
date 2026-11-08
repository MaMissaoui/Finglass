<p align="center">
  <img src="assets/brand/finglass-logo.svg" alt="Finglass" width="420">
</p>

<p align="center"><b>A read-only, local-first view of your finances.</b><br>
See every account, booking, receipt and report in one place. Nothing in Finglass can move money.</p>

---

> **Status: alpha, macOS only.** Everything FinDesk does, minus payments, is in, except the live bank sync (FinTS),
> which comes next. Download the DMG from [Releases](https://github.com/MaMissaoui/Finglass/releases) or install from
> a checkout (below). The plan is in [`docs/PLAN.md`](docs/PLAN.md).

## What Finglass is

- **Read-only by design.** Finglass fetches balances and statements and imports bank files. It has no transfers,
  scheduled transfers or standing orders, and contains no code that could send them.
- **Local-first.** Your data stays on your computer in an encrypted database (SQLCipher). No account, no cloud, no
  telemetry.
- **Built in Rust** with a native [Slint](https://slint.dev) UI, for a small, fast app on macOS, Windows and Linux.
- **German banking via FinTS** (in progress: the bank screens are in, the connection itself is not yet): balances and
  statements with pushTAN, chipTAN and photoTAN. Bank file imports (CSV, MT940, camt, OFX, QIF, XLSX) work without any
  bank connection.

Finglass is a sister project of [FinDesk](https://github.com/MaMissaoui/FinDesk) (Kotlin), rebuilt in Rust without the
payment features.

## Building

Needs Rust 1.92 or newer.

```sh
cargo run -p finglass            # the app
cargo test --workspace           # tests
cargo clippy --workspace --all-targets -- -D warnings
cargo fmt --all --check
```

On Linux, install the UI's build dependencies first (Debian/Ubuntu):

```sh
sudo apt-get install libfontconfig1-dev libxkbcommon-dev libxcb-shape0-dev libxcb-xfixes0-dev
```

The icons in `assets/icons` are generated from the SVGs in `assets/brand` by `scripts/render-icons.sh`.

## Screenshots from your own data

The headless screenshot test renders seeded fixtures by default. To review every page with your own data, point it at
a profile folder and give it the passphrase on the command line. It copies the database (and its `-wal`/`-shm` files)
into a temporary folder, opens only that copy, and removes it afterwards. Quit Finglass first so the copy is
consistent. The passphrase is read into a zeroizing secret and never written to a file, a log or the output.

```sh
cd ~/Source/Github/Finglass
read -rs 'p?Finglass passphrase: '; echo
FINGLASS_SCREENSHOT_DATA="$HOME/.finglass" FINGLASS_SCREENSHOT_PASSPHRASE="$p" \
  FINGLASS_SCREENSHOTS=/absolute/output/dir cargo test -p finglass --test screenshots renders_real_data
unset p
```

For another profile, use `~/.finglass/profiles/<slug>`. The output folder must be outside the checkout; the test
refuses a folder inside the workspace, a data folder without `finglass.db`, and a passphrase that does not open the
copy.

## Building the macOS app

`Finglass.app` and the DMG are built with [cargo-packager](https://github.com/crabnebula-dev/cargo-packager)
0.11.8, pinned the same way in CI:

```sh
cargo install cargo-packager --version 0.11.8 --locked
./scripts/package-macos.sh
```

The script writes `target/packager/Finglass.app` and `target/packager/Finglass_<version>_<arch>.dmg`,
ad-hoc signs and verifies the app, and prints both paths. It needs no `sudo` and writes only under
`target/`. The first DMG build also lets cargo-packager fetch its `create-dmg` helper.

The bundle declares macOS 11.0 as its minimum: Slint's winit backend uses the Accessibility framework,
which exists since macOS 11, and Rust's arm64 macOS target cannot target anything older. It is ad-hoc
signed only, so Gatekeeper still warns when it is downloaded; distribution needs a Developer ID
signature and notarization.

The `Package` workflow runs the script on `macos-latest`, launches the app from the mounted DMG once
with a fresh `FINGLASS_HOME`, and uploads the DMG as an artifact.

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

That needs Rust 1.92+ and cargo-packager 0.11.8. On `main` it fast-forwards a clean checkout from
`origin/main` first; `--no-update` skips that. `--force` installs a checkout with uncommitted changes or a
HEAD that is neither a `v*` release tag nor the fetched `origin/main`.

## Releasing

A release is a pushed `v<version>` tag on `main`, where `<version>` is the workspace version in `Cargo.toml`.

- The `Package` workflow builds the DMG, runs its launch and install checks, and then publishes it with a SHA-256 file
  as a GitHub pre-release.
- The release notes come from `docs/releases/v<version>.md`.
- The workflow refuses a tag that doesn't match the version, and a release without notes.

## Third-party assets

- The UI font is [IBM Plex Sans](https://www.ibm.com/plex/) (Regular, Medium, SemiBold and Bold) under the SIL Open
  Font License 1.1. The font files and the licence text are in `crates/finglass-ui/ui/fonts/` (`OFL.txt`).
- The filled icons in `crates/finglass-ui/ui/icons/` are from Google's
  [Material icons](https://fonts.google.com/icons), licensed under Apache-2.0.

## License

[MIT](LICENSE). The UI uses [Slint](https://slint.dev) under its royalty-free license, which is why the About screen
shows the Slint badge.
