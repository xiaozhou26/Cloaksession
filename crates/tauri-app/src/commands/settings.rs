//! Settings get/update. `SettingsStore::load`/`update` require `&mut self`,
//! so the store is wrapped in a `tokio::sync::Mutex` inside `AppState`.

use multizen_core::AppSettings;
use tauri::State;

use crate::AppState;

#[tauri::command]
pub async fn settings_get(
    state: State<'_, AppState>,
) -> Result<AppSettings, String> {
    let mut store = state.settings.lock().await;
    store.load().map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn settings_update(
    state: State<'_, AppState>,
    patch: AppSettings,
) -> Result<AppSettings, String> {
    let mut store = state.settings.lock().await;
    store.update(patch).map_err(|e| e.to_string())
}
