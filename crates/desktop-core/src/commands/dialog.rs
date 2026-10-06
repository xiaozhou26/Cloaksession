//! Native file dialogs delegated to the desktop host.

use crate::runtime::{AppHandle, State};
use std::path::PathBuf;

use crate::AppState;

pub async fn dialog_pick_browser_binary(
    _state: State<'_, AppState>,
    app: AppHandle,
) -> Result<Option<PathBuf>, String> {
    let path = app
        .dialog()
        .file()
        .add_filter("Browser binary", &["exe", "app", "sh"])
        .pick_file()
        .await?;
    Ok(path)
}

pub async fn dialog_pick_directory(
    _state: State<'_, AppState>,
    app: AppHandle,
) -> Result<Option<PathBuf>, String> {
    let path = app.dialog().file().pick_folder().await?;
    Ok(path)
}
