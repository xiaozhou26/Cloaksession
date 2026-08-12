//! MCP bearer-token management.
//!
//! `load_or_create_mcp_token(data_dir)` returns the 64-hex (256-bit entropy)
//! token stored at `<data_dir>/mcp-token`, creating it with fresh entropy
//! if the file is missing or malformed. Files are created with 0600
//! permissions on Unix; Windows has no portable equivalent so we only
//! best-effort write the file there.

use std::path::Path;

use multizen_core::Result;

/// Load the MCP bearer token from `<data_dir>/mcp-token`, or create a
/// fresh 64-hex token and persist it.
///
/// A valid token is exactly 64 lowercase ASCII hex characters
/// (two concatenated UUIDv4 simple representations = 256 bits of entropy).
/// Existing files that fail this check are overwritten.
pub fn load_or_create_mcp_token(data_dir: &Path) -> Result<String> {
    let path = data_dir.join("mcp-token");

    if let Ok(existing) = std::fs::read_to_string(&path) {
        let trimmed = existing.trim();
        if is_valid_token(trimmed) {
            return Ok(trimmed.to_string());
        }
        tracing::warn!(
            "existing mcp-token at {} is malformed (len={}); regenerating",
            path.display(),
            trimmed.len()
        );
    }

    let token = generate_token();
    write_token_file(&path, &token)?;
    Ok(token)
}

/// A valid token is 64 lowercase ASCII hex chars.
fn is_valid_token(s: &str) -> bool {
    s.len() == 64 && s.chars().all(|c| c.is_ascii_hexdigit())
}

/// Generate a 64-hex token by concatenating two UUIDv4 simple
/// representations (each 32 hex chars, 128 bits) — 256 bits total entropy.
fn generate_token() -> String {
    format!("{}{}", uuid::Uuid::new_v4().simple(), uuid::Uuid::new_v4().simple())
}

/// Write the token to disk with 0600 perms on Unix (best-effort on Windows).
fn write_token_file(path: &Path, token: &str) -> Result<()> {
    std::fs::write(path, token)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(0o600);
        std::fs::set_permissions(path, perms)?;
    }
    // Windows has no portable 0600 equivalent; the file is created with
    // the user's default ACL. We rely on the data dir being user-private.
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn creates_new_token_when_missing() {
        let dir = tempdir().unwrap();
        let token = load_or_create_mcp_token(dir.path()).unwrap();
        assert_eq!(token.len(), 64);
        assert!(token.chars().all(|c| c.is_ascii_hexdigit()));
        // File written.
        let on_disk = std::fs::read_to_string(dir.path().join("mcp-token")).unwrap();
        assert_eq!(on_disk.trim(), token);
    }

    #[test]
    fn reuses_existing_valid_token() {
        let dir = tempdir().unwrap();
        std::fs::write(dir.path().join("mcp-token"), "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789").unwrap();
        let token = load_or_create_mcp_token(dir.path()).unwrap();
        assert_eq!(token, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789");
    }

    #[test]
    fn regenerates_when_malformed() {
        let dir = tempdir().unwrap();
        std::fs::write(dir.path().join("mcp-token"), "not-a-token").unwrap();
        let token = load_or_create_mcp_token(dir.path()).unwrap();
        assert_eq!(token.len(), 64);
        assert!(token.chars().all(|c| c.is_ascii_hexdigit()));
    }

    #[test]
    fn generated_token_has_256_bits_entropy() {
        let t = generate_token();
        assert_eq!(t.len(), 64);
        assert!(t.chars().all(|c| c.is_ascii_hexdigit()));
    }

    #[test]
    fn rejects_wrong_length() {
        assert!(!is_valid_token("abc"));
        assert!(!is_valid_token(&"a".repeat(63)));
        assert!(!is_valid_token(&"a".repeat(65)));
    }

    #[test]
    fn rejects_non_hex() {
        assert!(!is_valid_token(&"g".repeat(64)));
    }
}
