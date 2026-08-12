//! Companion extension poller — the "Add to Cloaksession" button channel.
//!
//! The companion extension injects an "Add to Cloaksession" button on Chrome Web
//! Store detail pages. When clicked, the content script writes the extension
//! id to `<html data-mz-add-ext="…">`. This module polls that attribute via
//! CDP `Runtime.evaluate` (through `BrowserSession::poll_companion_signal`)
//! and, when a signal is found, installs the extension into the profile and
//! relaunches it (Chromium only reads `--load-extension` at startup).
//!
//! The poller runs as a background task spawned after `profiles_launch`
//! succeeds. It polls every 600ms (matching the original Electron app) and
//! exits when the profile is closed or the signal is consumed.

use std::sync::Arc;
use std::time::Duration;

use mcp_server::driver::BrowserDriver;
use serde::Serialize;
use tauri::Emitter;
use tracing::{info, warn};

use crate::driver::TauriBrowserDriver;
use crate::registry::ProfileRegistry;

const POLL_INTERVAL_MS: u64 = 600;
const URL_FILTER: &str = "chromewebstore.google.com";

/// Payload for the `extensions:installed` push event.
/// Mirrors the frontend `ExtensionInstalledEvent` discriminated union.
#[derive(Clone, Serialize)]
#[serde(untagged)]
pub enum ExtensionInstalledPayload {
    Ok {
        ok: bool,
        profile_id: String,
        extension: multizen_core::ExtensionConfig,
    },
    Err {
        ok: bool,
        profile_id: String,
        error: String,
    },
}

/// Spawn the companion poller as a background task. The poller runs until the
/// profile is no longer running (checked via `driver.is_running`) or a signal
/// is consumed and processed (install + relaunch). After a successful
/// relaunch, a fresh poller is spawned for the new session.
pub fn spawn_companion_poller(
    app: tauri::AppHandle,
    driver: Arc<TauriBrowserDriver>,
    registry: Arc<ProfileRegistry>,
    profile_id: String,
) {
    tauri::async_runtime::spawn(async move {
        // Get the session for this profile. If the session is gone (profile
        // closed between launch and poller start), exit silently.
        let session = match registry.get(&profile_id).await {
            Some(s) => s,
            None => {
                tracing::debug!(
                    profile = %profile_id,
                    "companion poller: no session, exiting"
                );
                return;
            }
        };

        info!(profile = %profile_id, "companion poller started");

        loop {
            // Stop if the profile is no longer running.
            if !driver.is_running(&profile_id) {
                tracing::debug!(
                    profile = %profile_id,
                    "companion poller: profile not running, exiting"
                );
                return;
            }

            // Poll Chrome Web Store pages for the companion signal.
            match session.poll_companion_signal(URL_FILTER).await {
                Ok(Some(payload)) => {
                    info!(
                        profile = %profile_id,
                        payload = %payload,
                        "companion signal received"
                    );
                    // Process the signal: parse, install, relaunch.
                    let relaunched = process_companion_signal(
                        &app,
                        &driver,
                        &profile_id,
                        &payload,
                    )
                    .await;
                    // If the profile was relaunched, spawn a fresh poller
                    // for the new session and exit this one.
                    if relaunched {
                        spawn_companion_poller(
                            app.clone(),
                            driver.clone(),
                            registry.clone(),
                            profile_id.clone(),
                        );
                    }
                    return;
                }
                Ok(None) => {
                    // No signal — keep polling.
                }
                Err(e) => {
                    warn!(
                        profile = %profile_id,
                        error = %e,
                        "companion poll: CDP error (non-fatal)"
                    );
                }
            }

            tokio::time::sleep(Duration::from_millis(POLL_INTERVAL_MS)).await;
        }
    });
}

/// Process a companion signal: parse the extension id, install the extension,
/// emit `extensions:installed`, and relaunch the profile so Chromium picks
/// up the new `--load-extension` entry.
///
/// Returns `true` if the profile was successfully relaunched (caller should
/// spawn a fresh poller), `false` otherwise.
async fn process_companion_signal(
    app: &tauri::AppHandle,
    driver: &TauriBrowserDriver,
    profile_id: &str,
    payload: &str,
) -> bool {
    // Parse `{ id: string, n: number }` — the content script's payload.
    let ext_id = match serde_json::from_str::<serde_json::Value>(payload) {
        Ok(v) => v
            .get("id")
            .and_then(|id| id.as_str())
            .map(|s| s.to_string()),
        Err(e) => {
            warn!(profile = %profile_id, error = %e, "companion: malformed payload");
            emit_install_error(app, profile_id, "Invalid companion signal");
            return false;
        }
    };

    let Some(ext_id) = ext_id else {
        warn!(profile = %profile_id, "companion: no id in payload");
        emit_install_error(app, profile_id, "No extension ID in companion signal");
        return false;
    };

    info!(profile = %profile_id, ext_id = %ext_id, "installing from companion");

    // Install the extension.
    match crate::commands::extensions::install_from_web_store(driver, profile_id, &ext_id).await {
        Ok(updated) => {
            // Find the just-installed extension config for the event.
            let cfg = updated.iter().find(|e| e.id == ext_id);
            let extension = cfg.cloned().unwrap_or_else(|| multizen_core::ExtensionConfig {
                id: ext_id.clone(),
                name: ext_id.clone(),
                version: String::new(),
                enabled: true,
                scope: "shared".to_string(),
                dir: String::new(),
                source: "web-store".to_string(),
            });
            let _ = app.emit(
                "extensions:installed",
                &ExtensionInstalledPayload::Ok {
                    ok: true,
                    profile_id: profile_id.to_string(),
                    extension,
                },
            );
        }
        Err(e) => {
            warn!(
                profile = %profile_id,
                ext_id = %ext_id,
                error = %e,
                "companion install failed"
            );
            emit_install_error(app, profile_id, &e);
            return false;
        }
    }

    // Relaunch the profile so Chromium picks up the new extension via
    // --load-extension. The original Electron app does the same: close +
    // reopen (session restore brings tabs back).
    info!(profile = %profile_id, "companion: relaunching profile for extension reload");
    if let Err(e) = BrowserDriver::close(driver, profile_id).await {
        warn!(profile = %profile_id, error = %e, "companion: close for relaunch failed");
        emit_install_error(
            app,
            profile_id,
            &format!("Added it, but couldn't reopen the profile — launch it again. ({e})"),
        );
        return false;
    }
    // Brief delay to let the process fully exit before relaunching.
    tokio::time::sleep(Duration::from_millis(500)).await;
    match BrowserDriver::launch(driver, profile_id).await {
        Ok(_) => {
            info!(profile = %profile_id, "companion: profile relaunched");
            // The `BrowserDriver::launch` call registered a new session in
            // the registry. The fresh poller (spawned by the caller) will
            // pick it up.
            true
        }
        Err(e) => {
            warn!(
                profile = %profile_id,
                error = %e,
                "companion: relaunch failed"
            );
            emit_install_error(
                app,
                profile_id,
                &format!("Added it, but the profile didn't reopen — launch it again. ({e})"),
            );
            false
        }
    }
}

fn emit_install_error(app: &tauri::AppHandle, profile_id: &str, error: &str) {
    let _ = app.emit(
        "extensions:installed",
        &ExtensionInstalledPayload::Err {
            ok: false,
            profile_id: profile_id.to_string(),
            error: error.to_string(),
        },
    );
}
