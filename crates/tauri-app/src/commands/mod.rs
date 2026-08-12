//! Tauri command modules. Each module groups related `#[tauri::command]`
//! functions. Commands are registered in `main.rs` via
//! `tauri::generate_handler![]`. Function names use snake_case (Tauri 2.x
//! does not accept colons); the frontend (P4.6) maps the old `ns:action`
//! IPC names to these.

pub mod activity;
pub mod archive;
pub mod companion;
pub mod dialog;
pub mod extensions;
pub mod fingerprint;
pub mod profiles;
pub mod proxy;
pub mod settings;
pub mod system;
pub mod update;
