//! Native dialog commands backed by `tauri-plugin-dialog`. The plugin
//! exposes `AppHandle::dialog()` returning a `DialogExt` extension; from
//! there `.file()` returns a `FileDialogBuilder` with `blocking_pick_file`
//! / `blocking_pick_folder` for use from async or sync contexts. These
//! commands are sync from the frontend's perspective (the plugin handles
//! the platform dialog on a background thread under the hood).

use std::path::PathBuf;
use tauri::{AppHandle, State};
use tauri_plugin_dialog::DialogExt;

use crate::AppState;

#[tauri::command]
pub async fn dialog_pick_browser_binary(
    _state: State<'_, AppState>,
    app: AppHandle,
) -> Result<Option<PathBuf>, String> {
    let path = app
        .dialog()
        .file()
        .add_filter("Browser binary", &["exe", "app", "sh"])
        .blocking_pick_file();
    path.map(|p| p.into_path().map_err(|e| e.to_string()))
        .transpose()
}

#[tauri::command]
pub async fn dialog_pick_directory(
    _state: State<'_, AppState>,
    app: AppHandle,
) -> Result<Option<PathBuf>, String> {
    let path = app.dialog().file().blocking_pick_folder();
    path.map(|p| p.into_path().map_err(|e| e.to_string()))
        .transpose()
}
