use mcp_server::security::*;
use serde_json::json;

#[test]
fn normalize_strips_tab_newline() {
    assert_eq!(normalize_url_for_scan("fi\tle://x"), "file://x");
    assert_eq!(normalize_url_for_scan("chr\nome://x"), "chrome://x");
}

#[test]
fn normalize_strips_leading_control_chars() {
    assert_eq!(normalize_url_for_scan("\u{0000}\u{001f}file://x"), "file://x");
}

#[test]
fn blocks_file_scheme() {
    assert!(has_blocked_scheme("file:///etc/passwd"));
    assert!(has_blocked_scheme("chrome://settings"));
    assert!(has_blocked_scheme("devtools://devtools"));
    assert!(has_blocked_scheme("view-source:https://x"));
}

#[test]
fn allows_http_https() {
    assert!(!has_blocked_scheme("https://example.com"));
    assert!(!has_blocked_scheme("http://example.com"));
    assert!(!has_blocked_scheme("about:blank"));
}

#[test]
fn assert_safe_url_rejects_tab_obfuscated_file() {
    // The TS attack: "fi\tle:" — after normalization becomes "file:" → blocked
    assert!(assert_safe_url("fi\tle://etc/passwd").is_err());
}

#[test]
fn cdp_deny_io_read() {
    assert!(!cdp_method_allowed("IO.read"));
    assert!(!cdp_method_allowed("Page.getResourceContent"));
    assert!(!cdp_method_allowed("Browser.close"));
    assert!(!cdp_method_allowed("Browser.crash"));
    assert!(!cdp_method_allowed("Fetch.enable"));
    assert!(!cdp_method_allowed("Fetch.continueRequest"));
    assert!(!cdp_method_allowed("DOMStorage.getItem"));
}

#[test]
fn cdp_allows_navigation() {
    assert!(cdp_method_allowed("Page.navigate"));
    assert!(cdp_method_allowed("Runtime.evaluate"));
    assert!(cdp_method_allowed("Target.getTargets"));
}

#[test]
fn assert_no_blocked_scheme_in_params_rejects_nested() {
    let params = json!({"url": "file://x"});
    assert!(assert_no_blocked_scheme_in_params(&params).is_err());
    let nested = json!({"frame": {"url": "chrome://x"}});
    assert!(assert_no_blocked_scheme_in_params(&nested).is_err());
    let ok = json!({"url": "https://x"});
    assert!(assert_no_blocked_scheme_in_params(&ok).is_ok());
}

#[test]
fn redacted_proxy_hides_credentials() {
    use multizen_core::ProxyConfig;
    let p = ProxyConfig { proxy_type:"socks5".into(), host:"h".into(), port:1080, username:Some("u".into()), password:Some("p".into()) };
    let r = redacted_proxy(&p);
    assert!(r.get("username").is_none() || r.get("username").unwrap().is_null());
    assert_eq!(r.get("hasAuth").unwrap().as_bool(), Some(true));
    assert_eq!(r.get("host").unwrap().as_str(), Some("h"));
}
