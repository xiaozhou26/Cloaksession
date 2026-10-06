//! Headless desktop-core JSON-lines stdio entry point.

fn main() {
    if let Err(error) = desktop_core::run() {
        eprintln!("desktop-core: {error}");
        std::process::exit(1);
    }
}
