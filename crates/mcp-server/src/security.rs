use multizen_core::{MultizenError, ProxyConfig, Result};

pub const BLOCKED_URL_SCHEMES: &[&str] = &["file:", "chrome:", "devtools:", "view-source:"];

pub const CDP_DENY_METHODS_EXACT: &[&str] = &[
    "IO.read", "Page.getResourceContent", "Storage.getCookies",
    "Network.getAllCookies", "Browser.close", "Browser.crash",
];
pub const CDP_DENY_METHOD_PREFIXES: &[&str] = &[
    "DOMStorage.", "IndexedDB.", "CacheStorage.", "Fetch.",
];

pub fn normalize_url_for_scan(url: &str) -> String {
    // Strip \t\r\n globally, then strip all leading control chars <= 0x20.
    let no_tab_nl: String = url.chars().filter(|c| !matches!(c, '\t' | '\r' | '\n')).collect();
    let trimmed = no_tab_nl.trim_start_matches(|c: char| c as u32 <= 0x20);
    trimmed.to_string()
}

pub fn has_blocked_scheme(url: &str) -> bool {
    let n = normalize_url_for_scan(url);
    BLOCKED_URL_SCHEMES.iter().any(|s| n.starts_with(s))
}

pub fn assert_safe_url(url: &str) -> Result<()> {
    if has_blocked_scheme(url) {
        return Err(MultizenError::Mcp("forbidden URL scheme".into()));
    }
    Ok(())
}

pub fn cdp_method_allowed(method: &str) -> bool {
    if CDP_DENY_METHODS_EXACT.contains(&method) {
        return false;
    }
    if CDP_DENY_METHOD_PREFIXES.iter().any(|p| method.starts_with(p)) {
        return false;
    }
    true
}

pub fn assert_no_blocked_scheme_in_params(params: &serde_json::Value) -> Result<()> {
    match params {
        serde_json::Value::String(s) => {
            if has_blocked_scheme(s) {
                return Err(MultizenError::Mcp(format!("blocked scheme in param: {s}")));
            }
            Ok(())
        }
        serde_json::Value::Object(map) => {
            for v in map.values() {
                assert_no_blocked_scheme_in_params(v)?;
            }
            Ok(())
        }
        serde_json::Value::Array(a) => {
            for v in a {
                assert_no_blocked_scheme_in_params(v)?;
            }
            Ok(())
        }
        _ => Ok(()),
    }
}

pub fn redacted_proxy(proxy: &ProxyConfig) -> serde_json::Value {
    serde_json::json!({
        "type": proxy.proxy_type,
        "host": proxy.host,
        "port": proxy.port,
        "hasAuth": proxy.username.is_some() && proxy.password.is_some(),
    })
}
