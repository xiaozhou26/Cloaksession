use mcp_server::activity::{sanitize_args, ActivityLog};
use serde_json::json;

#[test]
fn sanitize_truncates_long_text() {
    let args = json!({"text": "x".repeat(200)});
    let s = sanitize_args(args);
    let t = s.get("text").unwrap().as_str().unwrap();
    assert!(t.len() <= 80, "long text truncated to <=80, got {}", t.len());
    assert!(t.ends_with("..."));
}

#[test]
fn sanitize_redacts_proxy_credentials() {
    let args = json!({"proxy": {"type":"socks5","host":"h","port":1080,"username":"secret","password":"hunter2"}});
    let s = sanitize_args(args);
    let p = s.get("proxy").unwrap();
    assert!(p.get("username").is_none() || p.get("username").unwrap().is_null());
    assert!(p.get("password").is_none() || p.get("password").unwrap().is_null());
    assert_eq!(p.get("host").unwrap().as_str(), Some("h")); // host preserved
}

#[test]
fn sanitize_redacts_cookie_values() {
    let args = json!({"cookies": [{"name":"sid","value":"verysecrettoken"}]});
    let s = sanitize_args(args);
    let c = s.get("cookies").unwrap().get(0).unwrap();
    assert_ne!(c.get("value").unwrap().as_str(), Some("verysecrettoken"));
}

#[tokio::test]
async fn log_starts_and_finishes_event() {
    let log = ActivityLog::new();
    let id = log.start_call("navigate", Some("p1".into()), json!({"url":"https://x"}));
    log.finish(&id, "ok", Some("navigated".into()), Some(120)).await;
    let recent = log.recent(10).await;
    assert_eq!(recent.len(), 1);
    assert_eq!(recent[0].tool, "navigate");
    assert_eq!(recent[0].status, "ok");
    assert_eq!(recent[0].duration_ms, Some(120));
}

#[tokio::test]
async fn log_caps_at_500() {
    let log = ActivityLog::new();
    for _ in 0..600 {
        let id = log.start_call("x", None, json!({}));
        log.finish(&id, "ok", None, Some(1)).await;
    }
    assert_eq!(log.recent(1000).await.len(), 500);
}
