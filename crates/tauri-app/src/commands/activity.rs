//! Activity log commands. `recent` returns the last N events from the
//! in-memory ring buffer (P3.3 `ActivityLog::recent`). The buffer lives on
//! the async runtime and is safe to share across threads (interior-sync
//! via `std::sync::Mutex`).

use mcp_server::activity::ActivityEvent;
use tauri::State;

use crate::AppState;

#[tauri::command]
pub async fn activity_recent(
    state: State<'_, AppState>,
    limit: Option<usize>,
) -> Result<Vec<ActivityEvent>, String> {
    let n = limit.unwrap_or(100).min(500);
    Ok(state.activity.recent(n).await)
}
