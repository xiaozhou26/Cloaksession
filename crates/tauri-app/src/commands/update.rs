//! App self-update via GitHub Releases.
//!
//! Checks the GitHub Releases API for a newer version than the running app.
//! On Windows, downloads the NSIS setup exe to a temp dir and launches it
//! (the installer handles restart). On macOS, opens the download URL in
//! the browser (no in-app install).
//!
//! State is kept in a `Mutex<UpdateState>` behind `AppState`. The
//! `update:status` event is emitted on every state transition.

use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, State};
use tauri_plugin_dialog::DialogExt;

use crate::AppState;

const GITHUB_OWNER: &str = "xiaozhou26";
const GITHUB_REPO: &str = "Cloaksession";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", tag = "kind")]
pub enum UpdateStatus {
    Idle,
    Checking,
    Available { version: String, release_notes: Option<String> },
    Downloading {
        version: String,
        received: u64,
        total: u64,
        percent: u64,
    },
    Ready { version: String },
    NoUpdate,
    UpToDate,
    Error { message: String },
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct StatusEvent {
    status: UpdateStatus,
}

/// Persistent update state (behind a Mutex in AppState).
pub struct UpdateState {
    status: UpdateStatus,
    last_checked: u64, // epoch ms
    /// Path to the downloaded installer (Windows), stashed for `install()`.
    installer_path: Option<std::path::PathBuf>,
    /// Version string for the pending installer.
    pending_version: Option<String>,
}

impl Default for UpdateState {
    fn default() -> Self {
        Self {
            status: UpdateStatus::Idle,
            last_checked: 0,
            installer_path: None,
            pending_version: None,
        }
    }
}

// ---------------------------------------------------------------------------
// GitHub API types
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
struct GitHubRelease {
    tag_name: String,
    body: Option<String>,
    assets: Vec<GitHubAsset>,
}

#[derive(Debug, Deserialize)]
struct GitHubAsset {
    name: String,
    browser_download_url: String,
    size: u64,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Strip a leading `v` from a GitHub tag (e.g. `v0.3.1` → `0.3.1`).
/// The frontend's `updateLabel` adds the `v` prefix, so the stored
/// version must be bare to avoid `vv0.3.1`.
fn strip_v(tag: &str) -> &str {
    tag.strip_prefix('v').unwrap_or(tag)
}

/// Compare semantic version strings. Returns true if `latest` > `current`.
fn is_newer(latest: &str, current: &str) -> bool {
    let parse = |s: &str| -> Vec<u64> {
        s.trim_start_matches('v')
            .split('.')
            .filter_map(|p| p.split('-').next().and_then(|n| n.parse().ok()))
            .collect()
    };
    let l = parse(latest);
    let c = parse(current);
    for i in 0..l.len().max(c.len()) {
        let li = l.get(i).copied().unwrap_or(0);
        let ci = c.get(i).copied().unwrap_or(0);
        if li > ci {
            return true;
        }
        if li < ci {
            return false;
        }
    }
    false
}

/// Find the NSIS setup asset for Windows x64 from a GitHub release.
fn find_nsis_asset(release: &GitHubRelease) -> Option<&GitHubAsset> {
    release.assets.iter().find(|a| {
        let name = a.name.to_lowercase();
        name.ends_with("-setup.exe") || (name.starts_with("multizen") && name.ends_with(".exe") && name.contains("x64"))
    })
}

fn set_status(app: &AppHandle, state: &Mutex<UpdateState>, status: UpdateStatus) {
    let mut s = state.lock().unwrap_or_else(|e| e.into_inner());
    s.status = status.clone();
    let _ = app.emit("update:status", StatusEvent { status });
}

fn now_epoch_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

#[tauri::command]
pub async fn update_status(state: State<'_, AppState>) -> Result<UpdateStatus, String> {
    let s = state.update.lock().unwrap_or_else(|e| e.into_inner());
    Ok(s.status.clone())
}

#[tauri::command]
pub async fn update_last_checked(state: State<'_, AppState>) -> Result<u64, String> {
    let s = state.update.lock().unwrap_or_else(|e| e.into_inner());
    Ok(s.last_checked)
}

#[tauri::command]
pub async fn update_check(
    app: AppHandle,
    state: State<'_, AppState>,
) -> Result<UpdateStatus, String> {
    set_status(&app, &state.update, UpdateStatus::Checking);

    let current_version = env!("CARGO_PKG_VERSION");
    let url = format!(
        "https://api.github.com/repos/{GITHUB_OWNER}/{GITHUB_REPO}/releases/latest"
    );

    let client = reqwest::Client::builder()
        .user_agent(format!("Cloaksession/{current_version} (update-checker)"))
        .timeout(std::time::Duration::from_secs(15))
        .build()
        .map_err(|e| format!("HTTP client: {e}"))?;

    let resp = client
        .get(&url)
        .header("Accept", "application/vnd.github+json")
        .send()
        .await
        .map_err(|e| format!("GitHub API request failed: {e}"))?;

    if !resp.status().is_success() {
        let msg = format!("GitHub API returned HTTP {}", resp.status());
        set_status(&app, &state.update, UpdateStatus::Error { message: msg.clone() });
        // Update last_checked even on failure.
        {
            let mut s = state.update.lock().unwrap_or_else(|e| e.into_inner());
            s.last_checked = now_epoch_ms();
        }
        return Ok(UpdateStatus::Error { message: msg });
    }

    let release: GitHubRelease = resp
        .json()
        .await
        .map_err(|e| format!("Failed to parse GitHub release: {e}"))?;

    {
        let mut s = state.update.lock().unwrap_or_else(|e| e.into_inner());
        s.last_checked = now_epoch_ms();
    }

    let version = strip_v(&release.tag_name).to_string();

    if !is_newer(&release.tag_name, current_version) {
        set_status(&app, &state.update, UpdateStatus::UpToDate);
        return Ok(UpdateStatus::UpToDate);
    }

    // Check if we already have the installer downloaded.
    let already_downloaded = {
        let s = state.update.lock().unwrap_or_else(|e| e.into_inner());
        s.installer_path
            .as_ref()
            .map(|p| p.exists())
            .unwrap_or(false)
            && s.pending_version.as_deref() == Some(version.as_str())
    };

    if already_downloaded {
        set_status(&app, &state.update, UpdateStatus::Ready {
            version: version.clone(),
        });
        return Ok(UpdateStatus::Ready { version });
    }

    set_status(&app, &state.update, UpdateStatus::Available {
        version: version.clone(),
        release_notes: release.body.clone(),
    });

    // On Windows, auto-download the NSIS installer (like electron-updater's
    // autoDownload on non-Mac). On macOS, stay in `available` and let the
    // user open the download URL via `update.download`.
    #[cfg(target_os = "windows")]
    {
        if let Some(asset) = find_nsis_asset(&release) {
            download_installer(&app, &state, &version, asset).await;
        }
    }

    Ok(UpdateStatus::Available {
        version,
        release_notes: release.body,
    })
}

/// Download the NSIS installer and emit progress events.
#[cfg(target_os = "windows")]
async fn download_installer(
    app: &AppHandle,
    state: &State<'_, AppState>,
    version: &str,
    asset: &GitHubAsset,
) {
    let client = match reqwest::Client::builder()
        .user_agent(format!("Cloaksession/{version} (updater)"))
        .timeout(std::time::Duration::from_secs(300))
        .build()
    {
        Ok(c) => c,
        Err(e) => {
            set_status(app, &state.update, UpdateStatus::Error {
                message: format!("Download client build failed: {e}"),
            });
            return;
        }
    };

    let resp = match client.get(&asset.browser_download_url).send().await {
        Ok(r) => r,
        Err(e) => {
            set_status(app, &state.update, UpdateStatus::Error {
                message: format!("Download failed: {e}"),
            });
            return;
        }
    };

    if !resp.status().is_success() {
        set_status(app, &state.update, UpdateStatus::Error {
            message: format!("Download HTTP {}", resp.status()),
        });
        return;
    }

    let total = asset.size;
    let mut received = 0u64;
    let mut stream = resp.bytes_stream();
    use futures_util::StreamExt;

    let temp_dir = std::env::temp_dir();
    let installer_path = temp_dir.join(format!("multizen-{version}-setup.exe"));
    let file = match std::fs::File::create(&installer_path) {
        Ok(f) => f,
        Err(e) => {
            set_status(app, &state.update, UpdateStatus::Error {
                message: format!("Cannot create temp file: {e}"),
            });
            return;
        }
    };
    let mut writer = std::io::BufWriter::new(file);

    while let Some(chunk_result) = stream.next().await {
        match chunk_result {
            Ok(chunk) => {
                if let Err(e) = std::io::Write::write_all(&mut writer, &chunk) {
                    set_status(app, &state.update, UpdateStatus::Error {
                        message: format!("Write failed: {e}"),
                    });
                    return;
                }
                received += chunk.len() as u64;
                let percent = received.saturating_mul(100).checked_div(total).unwrap_or(0);
                set_status(app, &state.update, UpdateStatus::Downloading {
                    version: version.to_string(),
                    received,
                    total,
                    percent,
                });
            }
            Err(e) => {
                set_status(app, &state.update, UpdateStatus::Error {
                    message: format!("Download stream error: {e}"),
                });
                return;
            }
        }
    }

    use std::io::Write;
    let _ = writer.flush();

    {
        let mut s = state.update.lock().unwrap_or_else(|e| e.into_inner());
        s.installer_path = Some(installer_path.clone());
        s.pending_version = Some(version.to_string());
    }

    set_status(app, &state.update, UpdateStatus::Ready {
        version: version.to_string(),
    });
}

#[tauri::command]
pub async fn update_install(
    app: AppHandle,
    state: State<'_, AppState>,
) -> Result<(), String> {
    let (installer_path, version) = {
        let s = state.update.lock().unwrap_or_else(|e| e.into_inner());
        match (&s.installer_path, &s.pending_version) {
            (Some(p), Some(v)) => (p.clone(), v.clone()),
            _ => return Err("No downloaded installer to install".into()),
        }
    };

    if !installer_path.exists() {
        // Clear stale path.
        {
            let mut s = state.update.lock().unwrap_or_else(|e| e.into_inner());
            s.installer_path = None;
            s.pending_version = None;
        }
        return Err(format!("Installer not found: {}", installer_path.display()));
    }

    // Launch the NSIS installer. It will handle closing the app and
    // restarting. The `/S` flag runs it silently (no GUI), and `--restart`
    // (a custom flag the app can check) hints it to restart Cloaksession after.
    // We use the non-silent mode so the user sees the install progress.
    std::process::Command::new(&installer_path)
        .spawn()
        .map_err(|e| format!("Failed to launch installer: {e}"))?;

    // The installer will close this process; emit ready status so UI
    // stays consistent if the user doesn't quit immediately.
    set_status(&app, &state.update, UpdateStatus::Ready { version });
    Ok(())
}

#[tauri::command]
pub async fn update_download(
    _app: AppHandle,
    _state: State<'_, AppState>,
    version: String,
) -> Result<(), String> {
    // On macOS / non-Windows: open the GitHub release download URL.
    let url = format!(
        "https://github.com/{GITHUB_OWNER}/{GITHUB_REPO}/releases/tag/{version}"
    );
    _app.dialog()
        .message(format!("Download Cloaksession {version} from:\n{url}"))
        .title("Download update");
    // Try to open in browser.
    #[cfg(target_os = "windows")]
    {
        let _ = std::process::Command::new("cmd")
            .args(["/C", "start", "", &url])
            .spawn();
    }
    #[cfg(target_os = "macos")]
    {
        let _ = std::process::Command::new("open").arg(&url).spawn();
    }
    Ok(())
}
