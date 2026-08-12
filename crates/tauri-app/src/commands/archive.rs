//! Profile archive export/import — MZAR format.
//!
//! Binary layout (big-endian where applicable):
//! ```text
//! [4B magic "MZAR"]
//! [2B version (u16 BE)]
//! [16B salt (scrypt)]
//! [12B nonce (AES-256-GCM)]
//! [N  ciphertext]
//! [16B GCM auth tag]
//! ```
//!
//! Plaintext (before encryption):
//! ```text
//! [4B manifest_len (u32 BE)]
//! [manifest_len bytes: manifest JSON (UTF-8)]
//! [4B file_len (u32 BE)][file_len bytes]  ← repeated per file in manifest.files order
//! [4B file_len (u32 BE)][file_len bytes]  ← repeated per extension file in manifest.extensions order
//! ```
//!
//! Encryption: AES-256-GCM, key derived via scrypt(passphrase, salt, 32)
//! with Node-default params N=16384, r=8, p=1. No AAD. The tag is the
//! final 16 bytes of the file.

use std::path::{Path, PathBuf};

use aes_gcm::{aead::Aead, Aes256Gcm, KeyInit, Nonce};
use multizen_core::Profile;
use rand::RngCore;
use scrypt::scrypt as scrypt_kdf;
use serde::{Deserialize, Serialize};
use sha2::Digest;
use tauri::{AppHandle, State};
use tauri_plugin_dialog::DialogExt;

use crate::AppState;

const MAGIC: &[u8; 4] = b"MZAR";
const VERSION: u16 = 2;
const SUPPORTED_VERSIONS: &[u16] = &[1, 2];
const KEY_LEN: usize = 32;
const SALT_LEN: usize = 16;
const NONCE_LEN: usize = 12;
const TAG_LEN: usize = 16;

// scrypt parameters matching Node.js crypto.scrypt defaults.
// N=16384 (2^14), r=8, p=1 — encoded as log2(N)=14 in `Params::new`.
const SCRYPT_LOG2_N: u8 = 14;
const SCRYPT_R: u32 = 8;
const SCRYPT_P: u32 = 1;

// ---------------------------------------------------------------------------
// Manifest types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct FileMeta {
    path: String,
    size: u64,
    sha256: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct BundledExtension {
    id: String,
    version: String,
    files: Vec<FileMeta>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ArchiveManifest {
    magic: String,
    version: u16,
    profile: Profile,
    files: Vec<FileMeta>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    extensions: Vec<BundledExtension>,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Recursively walk `dir`, collecting relative paths, sizes, and SHA-256
/// checksums. Returns `(FileMeta list, file contents in the same order)`.
type CollectedFile = (PathBuf, Vec<u8>);
type CollectedFiles = (Vec<FileMeta>, Vec<CollectedFile>);

fn collect_files(dir: &Path) -> Result<CollectedFiles, String> {
    let mut metas = Vec::new();
    let mut contents = Vec::new();
    collect_files_inner(dir, dir, &mut metas, &mut contents)?;
    Ok((metas, contents))
}

fn collect_files_inner(
    root: &Path,
    dir: &Path,
    metas: &mut Vec<FileMeta>,
    contents: &mut Vec<(PathBuf, Vec<u8>)>,
) -> Result<(), String> {
    for entry in std::fs::read_dir(dir).map_err(|e| format!("readdir {}: {e}", dir.display()))? {
        let entry = entry.map_err(|e| format!("readdir entry: {e}"))?;
        let path = entry.path();
        if path.is_dir() {
            collect_files_inner(root, &path, metas, contents)?;
        } else if path.is_file() {
            let rel = path
                .strip_prefix(root)
                .map_err(|e| format!("strip_prefix: {e}"))?
                .to_string_lossy()
                .replace('\\', "/");
            let data = std::fs::read(&path)
                .map_err(|e| format!("read {}: {e}", path.display()))?;
            let size = data.len() as u64;
            let mut hasher = sha2::Sha256::new();
            hasher.update(&data);
            let sha256 = hasher.finalize();
            let sha256_hex: String = sha256.iter().map(|b| format!("{b:02x}")).collect();
            metas.push(FileMeta { path: rel, size, sha256: sha256_hex });
            contents.push((path, data));
        }
    }
    Ok(())
}

/// Derive a 32-byte AES key from passphrase + salt via scrypt.
fn derive_key(passphrase: &str, salt: &[u8]) -> Result<[u8; KEY_LEN], String> {
    let mut key = [0u8; KEY_LEN];
    let params = scrypt::Params::new(
        SCRYPT_LOG2_N,
        SCRYPT_R,
        SCRYPT_P,
        KEY_LEN,
    )
    .map_err(|e| format!("scrypt params: {e}"))?;
    scrypt_kdf(passphrase.as_bytes(), salt, &params, &mut key)
        .map_err(|e| format!("scrypt: {e}"))?;
    Ok(key)
}

/// Write a u32 in big-endian.
fn put_u32_be(n: u32) -> [u8; 4] {
    n.to_be_bytes()
}

/// Read a u32 from big-endian bytes.
fn read_u32_be(buf: &[u8]) -> u32 {
    u32::from_be_bytes([buf[0], buf[1], buf[2], buf[3]])
}

/// Sanitize a segment for use as a directory name: alphanumeric + `._-`,
/// 1–128 chars, not `.` or `..`.
fn is_safe_segment(s: &str) -> bool {
    !s.is_empty()
        && s.len() <= 128
        && s != "."
        && s != ".."
        && s.chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '.' || c == '_' || c == '-')
        && s.chars().next().map(|c| c.is_ascii_alphanumeric()).unwrap_or(false)
}

/// Write a file inside `base_dir`, rejecting path traversal.
fn write_guarded(base_dir: &Path, rel_path: &str, content: &[u8]) -> Result<(), String> {
    let abs = base_dir.join(rel_path);
    // Canonicalize base_dir for a reliable prefix check.
    let canonical_base = base_dir.canonicalize().map_err(|e| format!("canonicalize base: {e}"))?;
    if let Some(parent) = abs.parent() {
        std::fs::create_dir_all(parent).map_err(|e| format!("mkdir parent: {e}"))?;
    }
    let canonical_abs = abs.canonicalize().unwrap_or(abs.clone());
    if canonical_abs == canonical_base {
        return Err(format!("path traversal: {}", rel_path));
    }
    let prefix = canonical_base.to_string_lossy();
    let abs_str = canonical_abs.to_string_lossy();
    if !abs_str.starts_with(&*prefix) {
        return Err(format!("path traversal: {}", rel_path));
    }
    std::fs::write(&abs, content).map_err(|e| format!("write {}: {e}", abs.display()))?;
    Ok(())
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

#[tauri::command]
pub async fn profiles_export_archive(
    state: State<'_, AppState>,
    app: AppHandle,
    id: String,
    passphrase: String,
) -> Result<ResultExport, String> {
    if passphrase.len() < 8 {
        return Ok(ResultExport::Failure {
            reason: "Passphrase must be at least 8 characters".to_string(),
        });
    }

    let profile = match state.driver.get_profile(&id).await {
        Ok(Some(p)) => p,
        Ok(None) => return Ok(ResultExport::Failure { reason: "not_found".into() }),
        Err(e) => return Err(e.to_string()),
    };

    // Save dialog.
    let safe_name: String = profile
        .name
        .chars()
        .filter(|c| c.is_alphanumeric() || *c == '-' || *c == '_')
        .collect();
    let default_name = if safe_name.is_empty() { "profile".into() } else { safe_name };
    let picked = app
        .dialog()
        .file()
        .add_filter("Cloaksession archive", &["mzar"])
        .set_file_name(format!("{default_name}.mzar"))
        .blocking_save_file();
    let Some(save_path) = picked else {
        return Ok(ResultExport::Failure { reason: "cancelled".into() });
    };
    let save_path = save_path.into_path().map_err(|e| e.to_string())?;

    // Collect profile data-dir files.
    let data_dir = PathBuf::from(&profile.data_dir);
    let (file_metas, file_contents) = if data_dir.exists() {
        collect_files(&data_dir)?
    } else {
        (Vec::new(), Vec::new())
    };

    // Collect shared extension files.
    let mut bundled_exts = Vec::new();
    let mut ext_file_contents: Vec<Vec<u8>> = Vec::new();
    if let Some(exts) = &profile.extensions {
        for ext in exts.iter().filter(|e| e.scope == "shared") {
            let ext_dir = PathBuf::from(&ext.dir);
            if !ext_dir.exists() {
                continue;
            }
            let (metas, contents) = collect_files(&ext_dir)?;
            for (_, data) in &contents {
                ext_file_contents.push(data.clone());
            }
            bundled_exts.push(BundledExtension {
                id: ext.id.clone(),
                version: ext.version.clone(),
                files: metas,
            });
        }
    }

    // Build manifest.
    let manifest = ArchiveManifest {
        magic: "MZAR".to_string(),
        version: VERSION,
        profile: profile.clone(),
        files: file_metas,
        extensions: bundled_exts,
    };
    let manifest_json = serde_json::to_vec(&manifest).map_err(|e| format!("serialize manifest: {e}"))?;

    // Build plaintext blob.
    let mut plaintext = Vec::new();
    plaintext.extend_from_slice(&put_u32_be(manifest_json.len() as u32));
    plaintext.extend_from_slice(&manifest_json);
    for (_, data) in &file_contents {
        plaintext.extend_from_slice(&put_u32_be(data.len() as u32));
        plaintext.extend_from_slice(data);
    }
    for data in &ext_file_contents {
        plaintext.extend_from_slice(&put_u32_be(data.len() as u32));
        plaintext.extend_from_slice(data);
    }

    // Encrypt.
    let mut salt = [0u8; SALT_LEN];
    let mut nonce_bytes = [0u8; NONCE_LEN];
    rand::thread_rng().fill_bytes(&mut salt);
    rand::thread_rng().fill_bytes(&mut nonce_bytes);

    let key = derive_key(&passphrase, &salt)?;
    let cipher = Aes256Gcm::new_from_slice(&key).map_err(|e| format!("aes key: {e}"))?;
    let nonce = Nonce::from_slice(&nonce_bytes);
    let ciphertext_with_tag = cipher
        .encrypt(nonce, plaintext.as_ref())
        .map_err(|e| format!("encrypt: {e}"))?;
    // aes-gcm crate appends the 16-byte tag at the end of the ciphertext.
    let (ciphertext, tag) = ciphertext_with_tag.split_at(ciphertext_with_tag.len() - TAG_LEN);

    // Write file: magic(4) + version(2 BE) + salt(16) + nonce(12) + ciphertext + tag(16).
    let mut out = Vec::with_capacity(4 + 2 + SALT_LEN + NONCE_LEN + ciphertext.len() + TAG_LEN);
    out.extend_from_slice(MAGIC);
    out.extend_from_slice(&VERSION.to_be_bytes());
    out.extend_from_slice(&salt);
    out.extend_from_slice(&nonce_bytes);
    out.extend_from_slice(ciphertext);
    out.extend_from_slice(tag);

    tokio::fs::write(&save_path, &out)
        .await
        .map_err(|e| format!("write archive: {e}"))?;

    Ok(ResultExport::Success {
        path: save_path.to_string_lossy().to_string(),
    })
}

pub enum ResultExport {
    Success { path: String },
    Failure { reason: String },
}

impl Serialize for ResultExport {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        use serde::ser::SerializeMap;
        let mut map = serializer.serialize_map(Some(2))?;
        match self {
            ResultExport::Success { path } => {
                map.serialize_entry("ok", &true)?;
                map.serialize_entry("path", path)?;
            }
            ResultExport::Failure { reason } => {
                map.serialize_entry("ok", &false)?;
                map.serialize_entry("reason", reason)?;
            }
        }
        map.end()
    }
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

#[tauri::command]
pub async fn profiles_import_archive(
    state: State<'_, AppState>,
    app: AppHandle,
    passphrase: String,
) -> Result<ResultImport, String> {
    // Open dialog.
    let picked = app
        .dialog()
        .file()
        .add_filter("Cloaksession archive", &["mzar"])
        .blocking_pick_file();
    let Some(file_path) = picked else {
        return Ok(ResultImport { ok: false, reason: "cancelled".into(), id: None });
    };
    let file_path = file_path.into_path().map_err(|e| e.to_string())?;

    let buf = tokio::fs::read(&file_path)
        .await
        .map_err(|e| format!("read archive: {e}"))?;

    // Parse header.
    if buf.len() < 4 + 2 + SALT_LEN + NONCE_LEN + TAG_LEN {
        return Ok(ResultImport { ok: false, reason: "File too small".into(), id: None });
    }
    if &buf[0..4] != MAGIC {
        return Ok(ResultImport { ok: false, reason: "Not a Cloaksession archive".into(), id: None });
    }
    let version = u16::from_be_bytes([buf[4], buf[5]]);
    if !SUPPORTED_VERSIONS.contains(&version) {
        return Ok(ResultImport { ok: false, reason: format!("Unsupported archive version {version}"), id: None });
    }
    let salt = &buf[6..6 + SALT_LEN];
    let nonce_bytes = &buf[6 + SALT_LEN..6 + SALT_LEN + NONCE_LEN];
    let body_start = 6 + SALT_LEN + NONCE_LEN;
    let ciphertext = &buf[body_start..buf.len() - TAG_LEN];
    let tag = &buf[buf.len() - TAG_LEN..];

    // Decrypt.
    let key = derive_key(&passphrase, salt).map_err(|e| e.to_string())?;
    let cipher = Aes256Gcm::new_from_slice(&key).map_err(|e| format!("aes key: {e}"))?;
    // aes-gcm expects ciphertext + tag concatenated.
    let mut ct_with_tag = Vec::with_capacity(ciphertext.len() + TAG_LEN);
    ct_with_tag.extend_from_slice(ciphertext);
    ct_with_tag.extend_from_slice(tag);
    let nonce = Nonce::from_slice(nonce_bytes);
    let plaintext = cipher
        .decrypt(nonce, ct_with_tag.as_ref())
        .map_err(|_| "Wrong passphrase or corrupted archive.".to_string())?;

    // Parse manifest.
    if plaintext.len() < 4 {
        return Ok(ResultImport { ok: false, reason: "Corrupted archive (too small)".into(), id: None });
    }
    let manifest_len = read_u32_be(&plaintext[0..4]) as usize;
    if plaintext.len() < 4 + manifest_len {
        return Ok(ResultImport { ok: false, reason: "Corrupted archive (manifest truncated)".into(), id: None });
    }
    let manifest_json = &plaintext[4..4 + manifest_len];
    let manifest: ArchiveManifest = serde_json::from_slice(manifest_json)
        .map_err(|e| format!("Corrupted archive (manifest JSON): {e}"))?;

    // Read remaining file chunks.
    let mut offset = 4 + manifest_len;
    let read_chunk = |off: &mut usize| -> Result<Vec<u8>, String> {
        if plaintext.len() < *off + 4 {
            return Err("Corrupted archive (file length truncated)".into());
        }
        let len = read_u32_be(&plaintext[*off..*off + 4]) as usize;
        *off += 4;
        if plaintext.len() < *off + len {
            return Err("Corrupted archive (file content truncated)".into());
        }
        let data = plaintext[*off..*off + len].to_vec();
        *off += len;
        Ok(data)
    };

    // Read profile data-dir files.
    let mut profile_files: Vec<(FileMeta, Vec<u8>)> = Vec::new();
    for fm in &manifest.files {
        let data = read_chunk(&mut offset)?;
        // Verify SHA-256.
        let mut hasher = sha2::Sha256::new();
        hasher.update(&data);
        let sha256_hex: String = hasher.finalize().iter().map(|b| format!("{b:02x}")).collect();
        if sha256_hex != fm.sha256 {
            return Ok(ResultImport { ok: false, reason: format!("Checksum mismatch: {}", fm.path), id: None });
        }
        profile_files.push((fm.clone(), data));
    }

    // Read extension files.
    let mut ext_files: Vec<(BundledExtension, Vec<Vec<u8>>)> = Vec::new();
    for be in &manifest.extensions {
        let mut files = Vec::new();
        for fm in &be.files {
            let data = read_chunk(&mut offset)?;
            let mut hasher = sha2::Sha256::new();
            hasher.update(&data);
            let sha256_hex: String = hasher.finalize().iter().map(|b| format!("{b:02x}")).collect();
            if sha256_hex != fm.sha256 {
                return Ok(ResultImport { ok: false, reason: format!("Checksum mismatch: {}", fm.path), id: None });
            }
            files.push(data);
        }
        ext_files.push((be.clone(), files));
    }

    // Decide target id.
    let existing = state.driver.list_profiles().await.map_err(|e| e.to_string())?;
    let id_taken: std::collections::HashSet<String> = existing.into_iter().map(|s| s.id).collect();
    let target_id = if is_safe_segment(&manifest.profile.id) && !id_taken.contains(&manifest.profile.id) {
        manifest.profile.id.clone()
    } else {
        uuid::Uuid::new_v4().to_string()
    };

    // Build restored profile.
    let profiles_root = state.driver.profiles_root().to_path_buf();
    let restored_data_dir = profiles_root.join(&target_id);
    let mut restored = manifest.profile.clone();
    restored.id = target_id.clone();
    restored.data_dir = restored_data_dir.to_string_lossy().to_string();

    // Insert into DB (creates the data_dir).
    state
        .driver
        .insert_imported(restored.clone())
        .await
        .map_err(|e| format!("insert imported: {e}"))?;

    // Write profile data-dir files.
    for (fm, data) in &profile_files {
        if let Err(e) = write_guarded(&restored_data_dir, &fm.path, data) {
            tracing::warn!(error = %e, path = %fm.path, "import: write profile file failed");
        }
    }

    // Write extension files.
    let extensions_root = state.driver.extensions_root().to_path_buf();
    for (be, files) in &ext_files {
        let ext_id = if is_safe_segment(&be.id) { be.id.clone() } else { continue };
        let ext_ver = if is_safe_segment(&be.version) { be.version.clone() } else { "0".to_string() };
        let dest_dir = extensions_root.join(&ext_id).join(&ext_ver);
        if dest_dir.exists() {
            // Skip writing but we already verified checksums above.
            continue;
        }
        std::fs::create_dir_all(&dest_dir).ok();
        for (fm, data) in be.files.iter().zip(files.iter()) {
            if let Err(e) = write_guarded(&dest_dir, &fm.path, data) {
                tracing::warn!(error = %e, path = %fm.path, "import: write ext file failed");
            }
        }
    }

    Ok(ResultImport {
        ok: true,
        reason: String::new(),
        id: Some(target_id),
    })
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ResultImport {
    pub ok: bool,
    pub reason: String,
    pub id: Option<String>,
}
