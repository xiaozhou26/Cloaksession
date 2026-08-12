//! Extension management commands.
//!
//! Extensions are unpacked into a shared `<data_dir>/extensions/<ext_id>/`
//! directory and referenced by `ExtensionConfig.dir` across profiles. The
//! on-disk layout is shared so installing the same web-store extension in
//! two profiles only downloads/unpacks once.
//!
//! Profile DB mutations (the `extensions` JSON column) go through the
//! launcher thread via `driver.set_extensions` / `driver.list_extensions`.
//! File I/O (download, unpack, dialog picking) and HTTP are `Send` work
//! done on the Tauri async runtime.

use std::path::{Path, PathBuf};

use base64::Engine;
use multizen_core::ExtensionConfig;
use sha2::Digest;
use tauri::{AppHandle, State};
use tauri_plugin_dialog::DialogExt;

use crate::AppState;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Parse a Chrome Web Store URL or raw 32-char extension ID into the ID.
/// Accepts:
///   - `https://chromewebstore.google.com/detail/<name>/<id>`
///   - `https://chrome.google.com/webstore/detail/<name>/<id>`
///   - `<id>` (raw 32-char lowercase a–p)
fn parse_web_store_id(input: &str) -> Result<String, String> {
    let trimmed = input.trim();
    // Raw 32-char id (lowercase a-p, per Chrome's id alphabet).
    if trimmed.len() == 32 && trimmed.chars().all(|c| c.is_ascii_alphanumeric()) {
        return Ok(trimmed.to_lowercase());
    }
    // URL — strip query/fragment first, then the id is the last path segment.
    if trimmed.starts_with("http") {
        // Remove query string and fragment.
        let no_query = trimmed.split(['?', '#']).next().unwrap_or(trimmed);
        if let Some(id) = no_query.rsplit('/').next() {
            let id = id.trim();
            if id.len() == 32 && id.chars().all(|c| c.is_ascii_alphanumeric()) {
                return Ok(id.to_lowercase());
            }
        }
    }
    Err(format!(
        "Could not parse a 32-char extension ID from: {input}"
    ))
}

/// Download the .crx for `id` from the Chrome Web Store update URL.
/// Uses the `x=` parameter encoding (matching the browser's own update
/// endpoint) — directly splicing `id=` in the URL query returns 204 for
/// many extensions. `prodversion` should be a real Chromium version; the
/// hardcoded "131.0" works but a real engine version is preferable when
/// available (the original Electron app reads it from the browser).
async fn download_crx(id: &str) -> Result<Vec<u8>, String> {
    let inner = format!("id={id}&installsource=ondemand&uc");
    // Encode the inner query string as a single value for the `x=` param.
    // Use percent-encoding for the reserved chars (&, =) so Google's endpoint
    // parses them as literal characters within the `x` value, not as
    // top-level query separators.
    use percent_encoding::{utf8_percent_encode, NON_ALPHANUMERIC};
    let encoded_inner = utf8_percent_encode(&inner, NON_ALPHANUMERIC);
    let url = format!(
        "https://clients2.google.com/service/update2/crx?response=redirect&acceptformat=crx2,crx3&prodversion=131.0&x={encoded_inner}"
    );
    let client = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::limited(5))
        .timeout(std::time::Duration::from_secs(60))
        .connect_timeout(std::time::Duration::from_secs(15))
        .user_agent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0.0.0 Safari/537.36")
        .build()
        .map_err(|e| format!("HTTP client build failed: {e}"))?;
    let resp = client
        .get(&url)
        .send()
        .await
        .map_err(|e| format!("Web Store download failed: {e}"))?;
    let status = resp.status();
    if status == 204 {
        return Err(format!(
            "Extension {id} isn't available from the Web Store (it may be delisted or region-restricted). Try uploading the .crx/.zip instead."
        ));
    }
    if !status.is_success() {
        return Err(format!(
            "Web Store returned HTTP {status} for extension {id}"
        ));
    }
    let bytes = resp
        .bytes()
        .await
        .map_err(|e| format!("Failed to read CRX body: {e}"))?;
    if bytes.len() < 16 {
        return Err(format!(
            "Extension {id} isn't available from the Web Store (empty response). Try uploading the .crx/.zip instead."
        ));
    }
    Ok(bytes.to_vec())
}

/// Find the start of the ZIP body in a .crx buffer. CRX2 has a fixed
/// header; CRX3 uses protobuf. The robust approach: scan for the ZIP
/// local file header magic `PK\x03\x04`. Falls back to treating the
/// whole buffer as a plain ZIP.
fn find_zip_start(buf: &[u8]) -> usize {
    // CRX2: magic "Cr24", version u32, pub_key_len u32, sig_len u32
    if buf.len() >= 16 && &buf[0..4] == b"Cr24" {
        let pk_len = u32::from_le_bytes([buf[4], buf[5], buf[6], buf[7]]) as usize;
        let sig_len = u32::from_le_bytes([buf[8], buf[9], buf[10], buf[11]]) as usize;
        let offset = 16 + pk_len + sig_len;
        if offset + 4 <= buf.len() && &buf[offset..offset + 4] == b"PK\x03\x04" {
            return offset;
        }
    }
    // Scan for ZIP local file header magic (handles CRX3 and plain renamed zips).
    const MAGIC: &[u8] = &[0x50, 0x4b, 0x03, 0x04]; // PK\x03\x04
    for i in 0..buf.len().saturating_sub(4) {
        if &buf[i..i + 4] == MAGIC {
            return i;
        }
    }
    0
}

/// Unpack a .crx/.zip byte buffer into `extensions_root/<ext_id>/`.
/// If the target dir already exists, it's reused (no re-download needed).
async fn unpack_extension(
    bytes: &[u8],
    ext_id: &str,
    extensions_root: &Path,
) -> Result<PathBuf, String> {
    let target = extensions_root.join(ext_id);
    if target.exists() {
        return Ok(target);
    }

    let zip_start = find_zip_start(bytes);
    let zip_bytes = &bytes[zip_start..];

    // Extract on a blocking thread (zip extraction is CPU-bound + sync I/O).
    let target_clone = target.clone();
    let zip_vec = zip_bytes.to_vec();
    tokio::task::spawn_blocking(move || {
        let cursor = std::io::Cursor::new(zip_vec);
        let mut archive = zip::ZipArchive::new(cursor)
            .map_err(|e| format!("Failed to open ZIP archive: {e}"))?;
        // Extract into a temp sibling dir, then rename atomically.
        let tmp = target_clone.with_extension("tmp_unpacked");
        if tmp.exists() {
            std::fs::remove_dir_all(&tmp).ok();
        }
        std::fs::create_dir_all(&tmp).map_err(|e| format!("mkdir failed: {e}"))?;
        for i in 0..archive.len() {
            let mut file = archive
                .by_index(i)
                .map_err(|e| format!("zip read entry {i}: {e}"))?;
            let name = file
                .enclosed_name()
                .ok_or_else(|| format!("zip entry {i} has unsafe path"))?;
            let outpath = tmp.join(name);
            if file.is_dir() {
                std::fs::create_dir_all(&outpath).map_err(|e| format!("mkdir entry: {e}"))?;
            } else {
                if let Some(parent) = outpath.parent() {
                    std::fs::create_dir_all(parent).map_err(|e| format!("mkdir parent: {e}"))?;
                }
                let mut outfile = std::fs::File::create(&outpath)
                    .map_err(|e| format!("create file {outpath:?}: {e}"))?;
                std::io::copy(&mut file, &mut outfile)
                    .map_err(|e| format!("write file {outpath:?}: {e}"))?;
            }
        }
        std::fs::rename(&tmp, &target_clone).map_err(|e| {
            // If rename fails (e.g. cross-device), try remove+copy.
            let _ = std::fs::remove_dir_all(&target_clone);
            format!("rename to {target_clone:?}: {e}")
        })?;
        Ok::<(), String>(())
    })
    .await
    .map_err(|e| format!("unpack task panicked: {e}"))??;

    Ok(target)
}

/// Read `manifest.json` from an unpacked extension dir and return
/// `(name, version)`. Falls back to the ext_id if `name` is missing or
/// is a `__MSG_*__` placeholder.
fn read_manifest_meta(dir: &Path) -> (String, String) {
    let manifest_path = dir.join("manifest.json");
    let Ok(content) = std::fs::read_to_string(&manifest_path) else {
        return (String::new(), String::new());
    };
    let Ok(json) = serde_json::from_str::<serde_json::Value>(&content) else {
        return (String::new(), String::new());
    };
    let name = json
        .get("name")
        .and_then(|v| v.as_str())
        .map(|s| s.to_string())
        .unwrap_or_default();
    let version = json
        .get("version")
        .and_then(|v| v.as_str())
        .map(|s| s.to_string())
        .unwrap_or_default();
    (name, version)
}

/// Build an `ExtensionConfig` from source + id + dir + manifest meta.
fn build_config(source: &str, ext_id: &str, dir: PathBuf) -> ExtensionConfig {
    let (name, version) = read_manifest_meta(&dir);
    ExtensionConfig {
        id: ext_id.to_string(),
        name: if name.is_empty() || name.starts_with("__MSG_") {
            ext_id.to_string()
        } else {
            name
        },
        version,
        enabled: true,
        scope: "shared".to_string(),
        dir: dir.to_string_lossy().to_string(),
        source: source.to_string(),
    }
}

/// Short hex hash for file/folder-sourced extensions that lack a web-store id.
fn short_hash(input: &[u8]) -> String {
    let mut hasher = sha2::Sha256::new();
    hasher.update(input);
    let hash = hasher.finalize();
    hash[..16]
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect::<String>()
}

/// Read the extension's icon as a data URI. Returns `None` if no icon
/// is found or the file can't be read.
fn read_icon_data_uri(dir: &Path) -> Option<String> {
    let manifest_path = dir.join("manifest.json");
    let content = std::fs::read_to_string(&manifest_path).ok()?;
    let json: serde_json::Value = serde_json::from_str(&content).ok()?;
    let icons = json
        .get("icons")
        .and_then(|v| v.as_object())
        .or_else(|| {
            json.get("browser_action")
                .and_then(|v| v.get("default_icon"))
                .and_then(|v| v.as_object())
        })?;
    // Pick the largest numeric key.
    let (icon_path, mime) = icons
        .iter()
        .filter_map(|(k, v)| {
            v.as_str().map(|s| {
                let size: u32 = k.parse().unwrap_or(0);
                let lower = s.to_lowercase();
                let m = if lower.ends_with(".png") {
                    "image/png"
                } else if lower.ends_with(".jpg") || lower.ends_with(".jpeg") {
                    "image/jpeg"
                } else if lower.ends_with(".svg") {
                    "image/svg+xml"
                } else {
                    "image/png"
                };
                (size, s.to_string(), m)
            })
        })
        .max_by_key(|(size, _, _)| *size)
        .map(|(_, path, mime)| (path, mime))?;
    let bytes = std::fs::read(dir.join(icon_path)).ok()?;
    let b64 = base64::engine::general_purpose::STANDARD.encode(&bytes);
    Some(format!("data:{mime};base64,{b64}"))
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

#[tauri::command]
pub async fn extensions_list(
    state: State<'_, AppState>,
    profile_id: String,
) -> Result<Vec<ExtensionConfig>, String> {
    state
        .driver
        .list_extensions(&profile_id)
        .await
        .map_err(|e| e.to_string())
}

/// Install a web-store extension by id: download CRX (if not cached),
/// unpack, and upsert into the profile's extension list. Returns the
/// updated extension list. Shared between the `extensions_add_from_web_store`
/// Tauri command and the companion poller.
pub async fn install_from_web_store(
    driver: &crate::driver::TauriBrowserDriver,
    profile_id: &str,
    ext_id: &str,
) -> Result<Vec<ExtensionConfig>, String> {
    let extensions_root = driver.extensions_root().to_path_buf();

    // If already installed, skip download.
    let target = extensions_root.join(ext_id);
    if !target.exists() {
        let bytes = download_crx(ext_id).await?;
        unpack_extension(&bytes, ext_id, &extensions_root).await?;
    }

    let cfg = build_config("web-store", ext_id, target);

    // Upsert into profile's extension list.
    let mut exts = driver
        .list_extensions(profile_id)
        .await
        .map_err(|e| e.to_string())?;
    exts.retain(|e| e.id != cfg.id);
    exts.push(cfg);

    driver
        .set_extensions(profile_id, exts)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn extensions_add_from_web_store(
    state: State<'_, AppState>,
    profile_id: String,
    url_or_id: String,
) -> Result<Vec<ExtensionConfig>, String> {
    let ext_id = parse_web_store_id(&url_or_id)?;
    install_from_web_store(&state.driver, &profile_id, &ext_id).await
}

#[tauri::command]
pub async fn extensions_add_from_file(
    state: State<'_, AppState>,
    app: AppHandle,
    profile_id: String,
) -> Result<Vec<ExtensionConfig>, String> {
    let picked = app
        .dialog()
        .file()
        .add_filter("Extension", &["crx", "zip"])
        .blocking_pick_file();
    let Some(file_path) = picked else {
        // User cancelled — return current list unchanged.
        return state
            .driver
            .list_extensions(&profile_id)
            .await
            .map_err(|e| e.to_string());
    };
    let file_path = file_path.into_path().map_err(|e| e.to_string())?;
    let bytes = tokio::fs::read(&file_path)
        .await
        .map_err(|e| format!("Failed to read file: {e}"))?;
    let ext_id = short_hash(&bytes);
    let extensions_root = state.driver.extensions_root().to_path_buf();

    let target = extensions_root.join(&ext_id);
    if !target.exists() {
        unpack_extension(&bytes, &ext_id, &extensions_root).await?;
    }

    let cfg = build_config("file", &ext_id, target);

    let mut exts = state
        .driver
        .list_extensions(&profile_id)
        .await
        .map_err(|e| e.to_string())?;
    exts.retain(|e| e.id != cfg.id);
    exts.push(cfg);

    state
        .driver
        .set_extensions(&profile_id, exts)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn extensions_add_from_folder(
    state: State<'_, AppState>,
    app: AppHandle,
    profile_id: String,
) -> Result<Vec<ExtensionConfig>, String> {
    let picked = app.dialog().file().blocking_pick_folder();
    let Some(folder_path) = picked else {
        return state
            .driver
            .list_extensions(&profile_id)
            .await
            .map_err(|e| e.to_string());
    };
    let dir = folder_path.into_path().map_err(|e| e.to_string())?;

    // Try to get the extension id from manifest.key; fall back to hash.
    let manifest_path = dir.join("manifest.json");
    let ext_id = if let Ok(content) = std::fs::read_to_string(&manifest_path) {
        if let Ok(json) = serde_json::from_str::<serde_json::Value>(&content) {
            json.get("key")
                .and_then(|v| v.as_str())
                .map(|s| s.to_lowercase())
                .unwrap_or_else(|| short_hash(dir.to_string_lossy().as_bytes()))
        } else {
            short_hash(dir.to_string_lossy().as_bytes())
        }
    } else {
        short_hash(dir.to_string_lossy().as_bytes())
    };

    let cfg = build_config("folder", &ext_id, dir);

    let mut exts = state
        .driver
        .list_extensions(&profile_id)
        .await
        .map_err(|e| e.to_string())?;
    exts.retain(|e| e.id != cfg.id);
    exts.push(cfg);

    state
        .driver
        .set_extensions(&profile_id, exts)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn extensions_remove(
    state: State<'_, AppState>,
    profile_id: String,
    ext_id: String,
) -> Result<Vec<ExtensionConfig>, String> {
    let mut exts = state
        .driver
        .list_extensions(&profile_id)
        .await
        .map_err(|e| e.to_string())?;
    exts.retain(|e| e.id != ext_id);
    // Do NOT delete the on-disk dir — it's shared across profiles.
    state
        .driver
        .set_extensions(&profile_id, exts)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn extensions_toggle(
    state: State<'_, AppState>,
    profile_id: String,
    ext_id: String,
    enabled: bool,
) -> Result<Vec<ExtensionConfig>, String> {
    let mut exts = state
        .driver
        .list_extensions(&profile_id)
        .await
        .map_err(|e| e.to_string())?;
    for e in &mut exts {
        if e.id == ext_id {
            e.enabled = enabled;
        }
    }
    state
        .driver
        .set_extensions(&profile_id, exts)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn extensions_store_entries(
    state: State<'_, AppState>,
) -> Result<Vec<ExtensionConfig>, String> {
    state
        .driver
        .store_entries()
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn extensions_prepare_from_web_store(
    state: State<'_, AppState>,
    url_or_id: String,
) -> Result<ExtensionConfig, String> {
    let ext_id = parse_web_store_id(&url_or_id)?;
    let extensions_root = state.driver.extensions_root().to_path_buf();

    let target = extensions_root.join(&ext_id);
    if !target.exists() {
        let bytes = download_crx(&ext_id).await?;
        unpack_extension(&bytes, &ext_id, &extensions_root).await?;
    }

    Ok(build_config("web-store", &ext_id, target))
}

#[tauri::command]
pub async fn extensions_prepare_from_file(
    state: State<'_, AppState>,
    app: AppHandle,
) -> Result<Option<ExtensionConfig>, String> {
    let picked = app
        .dialog()
        .file()
        .add_filter("Extension", &["crx", "zip"])
        .blocking_pick_file();
    let Some(file_path) = picked else {
        return Ok(None);
    };
    let file_path = file_path.into_path().map_err(|e| e.to_string())?;
    let bytes = tokio::fs::read(&file_path)
        .await
        .map_err(|e| format!("Failed to read file: {e}"))?;
    let ext_id = short_hash(&bytes);
    let extensions_root = state.driver.extensions_root().to_path_buf();

    let target = extensions_root.join(&ext_id);
    if !target.exists() {
        unpack_extension(&bytes, &ext_id, &extensions_root).await?;
    }

    Ok(Some(build_config("file", &ext_id, target)))
}

#[tauri::command]
pub async fn extensions_prepare_from_folder(
    _state: State<'_, AppState>,
    app: AppHandle,
) -> Result<Option<ExtensionConfig>, String> {
    let picked = app.dialog().file().blocking_pick_folder();
    let Some(folder_path) = picked else {
        return Ok(None);
    };
    let dir = folder_path.into_path().map_err(|e| e.to_string())?;

    let manifest_path = dir.join("manifest.json");
    let ext_id = if let Ok(content) = std::fs::read_to_string(&manifest_path) {
        if let Ok(json) = serde_json::from_str::<serde_json::Value>(&content) {
            json.get("key")
                .and_then(|v| v.as_str())
                .map(|s| s.to_lowercase())
                .unwrap_or_else(|| short_hash(dir.to_string_lossy().as_bytes()))
        } else {
            short_hash(dir.to_string_lossy().as_bytes())
        }
    } else {
        short_hash(dir.to_string_lossy().as_bytes())
    };

    Ok(Some(build_config("folder", &ext_id, dir)))
}

#[tauri::command]
pub async fn extensions_icon(
    _state: State<'_, AppState>,
    ext: ExtensionConfig,
    _profile_id: Option<String>,
) -> Result<Option<String>, String> {
    // _profile_id is unused — icon is read purely from ext.dir. Kept as
    // a parameter (not removed) so the frontend's {ext, profileId} call
    // doesn't send an unexpected-key error.
    let dir = PathBuf::from(&ext.dir);
    if !dir.exists() {
        return Ok(None);
    }
    // Sync file I/O in an async fn — the reads are tiny (icons are <100KB).
    Ok(read_icon_data_uri(&dir))
}
