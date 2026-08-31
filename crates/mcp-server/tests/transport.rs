use async_trait::async_trait;
use mcp_server::server::{handle_json_rpc, McpDispatcher};
use mcp_server::transport::{host_allowed, parse_bearer};
use multizen_core::Result;
use axum::http::HeaderValue;
use serde_json::{json, Value};

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

struct TestDispatcher;

#[async_trait]
impl McpDispatcher for TestDispatcher {
    async fn call_tool(&self, name: &str, arguments: Value) -> Result<Value> {
        Ok(json!({"name": name, "arguments": arguments}))
    }
}

#[tokio::test]
async fn json_rpc_tools_list_returns_mcp_tools() {
    std::env::remove_var("MULTIZEN_MCP_ALLOW_RAW_CDP");
    let response = handle_json_rpc(
        &TestDispatcher,
        json!({"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}),
    )
    .await;
    assert_eq!(response["result"]["tools"][0]["name"], "list_profiles");
    assert!(response["result"]["tools"][0]["inputSchema"].is_object());
}

#[tokio::test]
async fn json_rpc_tools_call_dispatches_to_backend() {
    let response = handle_json_rpc(
        &TestDispatcher,
        json!({
            "jsonrpc":"2.0",
            "id":"call-1",
            "method":"tools/call",
            "params":{"name":"navigate","arguments":{"profileId":"p1","url":"https://example.com"}}
        }),
    )
    .await;
    assert_eq!(response["id"], "call-1");
    assert_eq!(response["result"]["isError"], false);
    let text = response["result"]["content"][0]["text"].as_str().unwrap();
    assert!(text.contains("navigate"));
}
