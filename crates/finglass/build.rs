fn main() {
    // One fixed style on every OS, so screenshots and layouts match across macOS, Windows and Linux.
    let config = slint_build::CompilerConfiguration::new().with_style("fluent".into());
    slint_build::compile_with_config("ui/app.slint", config).expect("Slint UI failed to compile");
}
