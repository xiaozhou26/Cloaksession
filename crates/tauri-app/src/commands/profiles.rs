//! Profile CRUD + launch/close commands. The pm itself lives on the
//! dedicated launcher thread (its rusqlite Connection is `!Send + !Sync`),
//! so all profile CRUD is routed through `TauriBrowserDriver`'s async
//! helpers, which forward `LauncherCmd` variants over the channel and
//! await a oneshot reply. `launch`/`close` go through the `BrowserDriver`
//! trait impl. `is_running` (sync) is read from the driver's local cache.

use mcp_server::driver::BrowserDriver;
use multizen_core::{CreateProfileInput, LaunchedProfile, Profile, ProfileSummary, UpdateProfileInput};
use tauri::State;

use crate::AppState;

#[tauri::command]
pub async fn profiles_list(
    state: State<'_, AppState>,
) -> Result<Vec<ProfileSummary>, String> {
    let mut summaries = state.driver.list_profiles().await.map_err(|e| e.to_string())?;
    // Refresh the sync `is_running` cache flag for each summary. The driver
    // cache is updated on launch/close; an externally-killed process would
    // show stale `true` until the next close — tracked in P4.8.
    for s in &mut summaries {
        s.is_running = state.driver.is_running(&s.id);
    }
    Ok(summaries)
}

#[tauri::command]
pub async fn profiles_get(
    state: State<'_, AppState>,
    id: String,
) -> Result<Option<Profile>, String> {
    state.driver.get_profile(&id).await.map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn profiles_create(
    state: State<'_, AppState>,
    input: CreateProfileInput,
) -> Result<Profile, String> {
    state
        .driver
        .create_profile(input)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn profiles_update(
    state: State<'_, AppState>,
    id: String,
    patch: UpdateProfileInput,
) -> Result<Profile, String> {
    state
        .driver
        .update_profile(&id, patch)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn profiles_delete(
    state: State<'_, AppState>,
    id: String,
) -> Result<(), String> {
    state.driver.delete_profile(&id).await.map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn profiles_launch(
    state: State<'_, AppState>,
    id: String,
) -> Result<LaunchedProfile, String> {
    // `BrowserDriver::launch` returns the full LaunchedProfile.
    mcp_server::driver::BrowserDriver::launch(state.driver.as_ref(), &id)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn profiles_close(
    state: State<'_, AppState>,
    id: String,
) -> Result<(), String> {
    mcp_server::driver::BrowserDriver::close(state.driver.as_ref(), &id)
        .await
        .map_err(|e| e.to_string())
}
