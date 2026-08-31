use multizen_core::FingerprintConfig;
use std::path::Path;

const GENERATED_CHROME_VERSION: &str = "148.0.0.0";

fn is_managed_chrome_user_agent(user_agent: &str) -> bool {
    let Some(chrome) = user_agent.split("Chrome/").nth(1) else {
        return false;
    };
    let version = chrome.split_whitespace().next().unwrap_or_default();
    version.split('.').count() == 4
        && version
            .split('.')
            .all(|part| part.bytes().all(|b| b.is_ascii_digit()))
}

/// Update Cloaksession's generated Chrome UA and Client Hints to match the
/// runtime major version. Explicitly custom user agents are left unchanged.
pub fn synchronize_managed_fingerprint_version(
    fp: &mut FingerprintConfig,
    runtime_version: &str,
) -> bool {
    if !is_managed_chrome_user_agent(&fp.user_agent) {
        return false;
    }

    let current_version = fp
        .user_agent
        .split("Chrome/")
        .nth(1)
        .and_then(|rest| rest.split_whitespace().next())
        .unwrap_or(GENERATED_CHROME_VERSION)
        .to_string();
    let current_major = current_version.split('.').next().unwrap_or_default();
    if !fp
        .client_hints
        .sec_ch_ua
        .contains(&format!(r#"v="{current_major}""#))
        || !fp
            .client_hints
            .sec_ch_ua_full_version_list
            .contains(&current_version)
    {
        return false;
    }

    let major = match runtime_version.split('.').next() {
        Some(value) if !value.is_empty() && value.bytes().all(|b| b.is_ascii_digit()) => value,
        _ => return false,
    };
    let reduced_version = format!("{major}.0.0.0");

    fp.user_agent = fp.user_agent.replace(
        &format!("Chrome/{current_version}"),
        &format!("Chrome/{reduced_version}"),
    );
    let current_major = current_version.split('.').next().unwrap_or_default();
    fp.client_hints.sec_ch_ua = fp.client_hints.sec_ch_ua.replace(
        &format!(r#"v="{current_major}""#),
        &format!(r#"v="{major}""#),
    );
    fp.client_hints.sec_ch_ua_full_version_list = fp
        .client_hints
        .sec_ch_ua_full_version_list
        .replace(&current_version, &reduced_version);
    true
}

/// Pure parser for a Chromium version string. Extracts `N.N.N.N`.
pub fn parse_version_output(text: &str) -> Option<String> {
    regex_lite(text)
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

/// Read a Chromium version from the executable's embedded file metadata.
///
/// This deliberately does not execute the browser. Some Windows Chromium
/// builds turn `--version` into a normal browser launch, which creates an
/// extra process tree before the real profile launch.
pub fn detect_chromium_version(binary: &Path) -> Option<String> {
    #[cfg(windows)]
    {
        return windows_file_version(binary);
    }
    #[cfg(not(windows))]
    {
        let _ = binary;
        None
    }
}

#[cfg(windows)]
fn windows_file_version(binary: &Path) -> Option<String> {
    use std::os::windows::ffi::OsStrExt;
    use std::ptr;

    #[repr(C)]
    struct VsFixedFileInfo {
        signature: u32,
        struct_version: u32,
        file_version_ms: u32,
        file_version_ls: u32,
        product_version_ms: u32,
        product_version_ls: u32,
        file_flags_mask: u32,
        file_flags: u32,
        file_os: u32,
        file_type: u32,
        file_subtype: u32,
        file_date_ms: u32,
        file_date_ls: u32,
    }

    #[link(name = "version")]
    extern "system" {
        fn GetFileVersionInfoSizeW(filename: *const u16, handle: *mut u32) -> u32;
        fn GetFileVersionInfoW(
            filename: *const u16,
            handle: u32,
            len: u32,
            data: *mut std::ffi::c_void,
        ) -> i32;
        fn VerQueryValueW(
            block: *const std::ffi::c_void,
            sub_block: *const u16,
            buffer: *mut *mut std::ffi::c_void,
            len: *mut u32,
        ) -> i32;
    }

    let wide: Vec<u16> = binary
        .as_os_str()
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();
    let mut handle = 0u32;
    let size = unsafe { GetFileVersionInfoSizeW(wide.as_ptr(), &mut handle) };
    if size == 0 {
        return None;
    }

    let mut data = vec![0u8; size as usize];
    let loaded = unsafe { GetFileVersionInfoW(wide.as_ptr(), 0, size, data.as_mut_ptr().cast()) };
    if loaded == 0 {
        return None;
    }

    let root: [u16; 2] = [b'\\' as u16, 0];
    let mut value = ptr::null_mut();
    let mut value_len = 0u32;
    let queried = unsafe {
        VerQueryValueW(
            data.as_ptr().cast(),
            root.as_ptr(),
            &mut value,
            &mut value_len,
        )
    };
    if queried == 0 || value.is_null() || value_len < 1 {
        return None;
    }

    let info = unsafe { (value as *const VsFixedFileInfo).read_unaligned() };
    if info.signature != 0xFEEF04BD {
        return None;
    }

    let major = info.file_version_ms >> 16;
    let minor = info.file_version_ms & 0xFFFF;
    let build = info.file_version_ls >> 16;
    let patch = info.file_version_ls & 0xFFFF;
    Some(format!("{major}.{minor}.{build}.{patch}"))
}
