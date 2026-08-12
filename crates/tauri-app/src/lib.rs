//! tauri-app library core — the integration seam between Plan 2
//! (cdp-driver / browser-launcher) and Plan 3 (mcp-server BrowserDriver).
//!
//! The binary target (`src/main.rs`) wraps this lib for the Tauri shell.

pub mod driver;
pub mod registry;

pub use driver::TauriBrowserDriver;
pub use registry::ProfileRegistry;
