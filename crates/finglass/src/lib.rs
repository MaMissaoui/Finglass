//! The Finglass desktop app: a read-only, local-first view of your finances.
//!
//! This library holds the page controllers that render core state into the generated Slint UI
//! from `finglass-ui`. The binary in `main.rs` only handles `--version` and hands the paths to
//! [`run`].

mod accounts;
mod banking;
mod dashboard;
mod documents;
mod jobs;
mod menu;
mod migration;
mod nav;
pub mod reports;
mod settings;
mod shell;
pub mod transactions;

pub use finglass_ui::*;

/// Starts the app shell with the resolved data-folder paths.
pub fn run(paths: finglass_core::paths::AppPaths) -> Result<(), slint::PlatformError> {
    shell::run(paths)
}

#[cfg(test)]
mod tests {
    /// The same eight colours as `MonogramColours` in `theme.slint`.
    const MONOGRAM_COLOURS: [u32; 8] = [
        0xB45309, 0x0E7490, 0x7C3AED, 0xBE123C, 0x15803D, 0x1D4ED8, 0xA21CAF, 0xC2410C,
    ];

    fn relative_luminance(hex: u32) -> f64 {
        let channels = [(hex >> 16) & 0xFF, (hex >> 8) & 0xFF, hex & 0xFF].map(|channel| {
            let channel = f64::from(channel) / 255.0;
            if channel <= 0.04045 {
                channel / 12.92
            } else {
                ((channel + 0.055) / 1.055).powf(2.4)
            }
        });
        0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
    }

    #[test]
    fn monogram_colours_reach_white_contrast() {
        for hex in MONOGRAM_COLOURS {
            let ratio = 1.05 / (relative_luminance(hex) + 0.05);
            assert!(
                ratio >= 4.5,
                "#{hex:06X} has only {ratio:.2}:1 contrast against white letters"
            );
        }
    }
}
