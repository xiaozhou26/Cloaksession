//! Settings get/update. `SettingsStore::load`/`update` require `&mut self`,
//! so the store is wrapped in a `tokio::sync::Mutex` inside `AppState`.
//!
//! `settings_update` accepts a partial JSON patch (only the fields the
//! frontend wants to change). It loads the current settings, merges the
//! non-null fields from the patch, and writes the result back. This
//! prevents toggling one field from resetting others (e.g. flipping
//! `autoUpdate` must not clear `browserBinaryPath`).

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
    patch: serde_json::Value,
) -> Result<AppSettings, String> {
    let mut store = state.settings.lock().await;
    let current = store.load().map_err(|e| e.to_string())?;

    // Merge: start from current settings as JSON, override with non-null
    // fields from the patch, then deserialize back to AppSettings.
    let mut current_json = serde_json::to_value(&current).map_err(|e| e.to_string())?;
    if let Some(obj) = current_json.as_object_mut() {
        if let Some(patch_obj) = patch.as_object() {
            for (key, value) in patch_obj {
                // Skip null values — the frontend sends null to mean "don't change".
                if value.is_null() {
                    continue;
                }
                obj.insert(key.clone(), value.clone());
            }
        }
    }

    let merged: AppSettings = serde_json::from_value(current_json).map_err(|e| e.to_string())?;
    store.update(merged).map_err(|e| e.to_string())
}
