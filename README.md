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

## License

[MIT](LICENSE). The UI uses [Slint](https://slint.dev) under its royalty-free license, which is why the About screen
shows the Slint badge.
