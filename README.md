<p align="center">
  <img src="assets/brand/finglass-logo.svg" alt="Finglass" width="420">
</p>

<p align="center"><b>A read-only, local-first view of your finances.</b><br>
See every account, booking, receipt and report in one place. Nothing in Finglass can move money.</p>

---

> **Status: pre-alpha.** The repository holds the project skeleton, the brand assets and the plan
> ([`docs/PLAN.md`](docs/PLAN.md)). The app opens a placeholder window; features land milestone by milestone.

## What Finglass is

- **Read-only by design.** Finglass fetches balances and statements and imports bank files. It has no transfers,
  scheduled transfers or standing orders, and contains no code that could send them.
- **Local-first.** Your data stays on your computer in an encrypted database (SQLCipher). No account, no cloud, no
  telemetry.
- **Built in Rust** with a native [Slint](https://slint.dev) UI, for a small, fast app on macOS, Windows and Linux.
- **German banking via FinTS** (planned): balances and statements with pushTAN, chipTAN and photoTAN. Bank file imports
  (CSV, MT940, camt, OFX, QIF, XLSX) work without any bank connection.

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

## License

[MIT](LICENSE). The UI uses [Slint](https://slint.dev) under its royalty-free license, which is why the About screen
shows the Slint badge.
