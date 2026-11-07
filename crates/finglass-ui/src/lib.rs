//! The compiled Slint UI for Finglass.
//!
//! `build.rs` compiles `ui/app.slint` once for the whole workspace. Every window, global and
//! model the app and the screenshot test use is reachable as `finglass_ui::<Type>`.

slint::include_modules!();
