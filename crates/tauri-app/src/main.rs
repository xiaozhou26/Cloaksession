#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

//! MultiZen Tauri shell entrypoint. Delegates to `tauri_app::run()` in the
//! lib crate — the `tauri::generate_handler!` macro must expand in the
//! same crate as the `#[tauri::command]` functions (it references their
//! generated `__cmd__<name>` helpers), so the handler registration lives
//! in `lib.rs`, not here.

fn main() {
    tauri_app::run();
}
