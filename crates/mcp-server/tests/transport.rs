use mcp_server::transport::{host_allowed, parse_bearer};
use axum::http::HeaderValue;

#[test]
fn parse_bearer_extracts_token() {
    let h = HeaderValue::from_static("Bearer abc123");
    assert_eq!(parse_bearer(&h), Some("abc123".to_string()));
}

#[test]
fn parse_bearer_rejects_missing_prefix() {
    let h = HeaderValue::from_static("abc123");
    assert_eq!(parse_bearer(&h), None);
}

#[test]
fn host_allowed_accepts_localhost() {
    assert!(host_allowed(
        "localhost:7777",
        &["localhost:7777".into(), "127.0.0.1:7777".into()]
    ));
}

#[test]
fn host_allowed_rejects_external() {
    assert!(!host_allowed(
        "evil.com:7777",
        &["localhost:7777".into()]
    ));
}
