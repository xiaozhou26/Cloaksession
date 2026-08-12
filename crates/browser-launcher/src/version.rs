use std::path::Path;
use std::time::Duration;
use tokio::process::Command;

/// Pure parser for `chrome --version` output. Extracts `N.N.N.N`.
pub fn parse_version_output(stdout: &str) -> Option<String> {
    regex_lite(stdout)
}

/// Manual scan for `N.N.N.N` to avoid pulling in a regex dependency.
/// Finds the first run of digits-and-dots with exactly 3 dots and only
/// digits between them.
fn regex_lite(s: &str) -> Option<String> {
    let bytes = s.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i].is_ascii_digit() {
            // Try to read N.N.N.N starting here.
            let start = i;
            let mut dots = 0;
            while i < bytes.len() && (bytes[i].is_ascii_digit() || bytes[i] == b'.') {
                if bytes[i] == b'.' {
                    // Must be preceded by a digit (no leading dot, no double dot).
                    if i == start || bytes[i - 1] == b'.' {
                        dots = 0;
                        break;
                    }
                    dots += 1;
                }
                i += 1;
            }
            if dots == 3 && i > start {
                let cand = &s[start..i];
                // Ensure it doesn't end with a dot.
                if !cand.ends_with('.') {
                    return Some(cand.to_string());
                }
            }
        } else {
            i += 1;
        }
    }
    None
}

/// Run `binary --version` with a 2000ms timeout and parse the version.
pub async fn detect_chromium_version(binary: &Path) -> Option<String> {
    let output = tokio::time::timeout(
        Duration::from_millis(2000),
        Command::new(binary).arg("--version").output(),
    )
    .await
    .ok()?
    .ok()?;
    let stdout = String::from_utf8_lossy(&output.stdout);
    parse_version_output(&stdout)
}
