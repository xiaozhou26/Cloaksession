//! MCP request dispatch and tool metadata.

use async_trait::async_trait;
use multizen_core::Result;
use serde_json::{json, Map, Value};

use crate::schema::*;
use crate::tools::TOOL_NAMES;

fn raw_cdp_enabled() -> bool {
    std::env::var("MULTIZEN_MCP_ALLOW_RAW_CDP")
        .ok()
        .map(|value| matches!(value.as_str(), "1" | "true" | "yes" | "on"))
        .unwrap_or(false)
}

/// Backend implemented by the application integration layer.
#[async_trait]
pub trait McpDispatcher: Send + Sync {
    async fn call_tool(&self, name: &str, arguments: Value) -> Result<Value>;
}

/// Build the MCP tool metadata exposed by `tools/list`.
pub fn tool_definitions() -> Vec<Value> {
    TOOL_NAMES
        .iter()
        .filter(|name| *name != &"cdp_send" || raw_cdp_enabled())
        .map(|name| {
            json!({
                "name": name,
                "description": tool_description(name),
                "inputSchema": tool_schema(name),
            })
        })
        .collect()
}

fn tool_description(name: &str) -> &'static str {
    match name {
        "list_profiles" => "List local browser profiles and their running state.",
        "launch_profile" => "Launch a browser profile.",
        "close_profile" => "Close a browser profile.",
        "navigate" => "Navigate a running profile to a web URL.",
        "click" => "Click an element in a running profile.",
        "type" => "Type text into an element in a running profile.",
        "extract" => "Extract the active page from a running profile.",
        "screenshot" => "Capture a screenshot from a running profile.",
        "create_profile" => "Create a local browser profile.",
        "update_profile" => "Update a local browser profile.",
        "delete_profile" => "Delete a local browser profile.",
        "list_fingerprint_options" => "List supported fingerprint options.",
        "evaluate_js" => "Evaluate JavaScript in a running profile.",
        "wait_for_selector" => "Wait for a selector in a running profile.",
        "list_tabs" => "List tabs in a running profile.",
        "activate_tab" => "Activate a tab in a running profile.",
        "close_tab" => "Close a tab in a running profile.",
        "wait_for_navigation" => "Wait for navigation in a running profile.",
        "wait_for_load" => "Wait for page load in a running profile.",
        "cdp_send" => "Send an allow-listed CDP request.",
        "get_cookies" => "Read cookies through the controlled browser session.",
        "set_cookies" => "Set cookies through the controlled browser session.",
        "new_tab" => "Open a new tab in a running profile.",
        _ => "Cloaksession MCP tool.",
    }
}

fn tool_schema(name: &str) -> Value {
    let schema = match name {
        "list_profiles" | "list_fingerprint_options" => schemars::schema_for!(ListProfilesArgs),
        "launch_profile" | "close_profile" | "extract" | "screenshot" | "delete_profile" => {
            schemars::schema_for!(ProfileIdArgs)
        }
        "navigate" => schemars::schema_for!(NavigateArgs),
        "click" => schemars::schema_for!(ClickArgs),
        "type" => schemars::schema_for!(TypeArgs),
        "create_profile" => schemars::schema_for!(CreateProfileArgs),
        "update_profile" => schemars::schema_for!(UpdateProfileArgs),
        "evaluate_js" => schemars::schema_for!(EvaluateJsArgs),
        "wait_for_selector" => schemars::schema_for!(WaitForSelectorArgs),
        "wait_for_navigation" | "wait_for_load" => schemars::schema_for!(WaitForNavigationArgs),
        "activate_tab" => schemars::schema_for!(ActivateTabArgs),
        "close_tab" => schemars::schema_for!(CloseTabArgs),
        "cdp_send" => schemars::schema_for!(CdpSendArgs),
        "get_cookies" => schemars::schema_for!(GetCookiesArgs),
        "set_cookies" => schemars::schema_for!(SetCookiesArgs),
        "new_tab" => schemars::schema_for!(NewTabArgs),
        _ => schemars::schema_for!(ListProfilesArgs),
    };
    serde_json::to_value(schema).unwrap_or_else(|_| json!({"type":"object"}))
}

/// Dispatch one JSON-RPC request body and return its JSON-RPC response.
pub async fn handle_json_rpc(dispatcher: &dyn McpDispatcher, request: Value) -> Value {
    let id = request.get("id").cloned().unwrap_or(Value::Null);
    let method = request.get("method").and_then(Value::as_str).unwrap_or("");
    let params = request.get("params").cloned().unwrap_or_else(|| json!({}));

    match method {
        "initialize" => json!({
            "jsonrpc": "2.0",
            "id": id,
            "result": {
                "protocolVersion": "2024-11-05",
                "capabilities": {"tools": {"listChanged": false}},
                "serverInfo": {"name": "cloaksession", "version": env!("CARGO_PKG_VERSION")}
            }
        }),
        "notifications/initialized" | "ping" => json!({
            "jsonrpc": "2.0",
            "id": id,
            "result": {}
        }),
        "tools/list" => json!({
            "jsonrpc": "2.0",
            "id": id,
            "result": {"tools": tool_definitions()}
        }),
        "tools/call" => dispatch_tool(dispatcher, id, params).await,
        _ => json!({
            "jsonrpc": "2.0",
            "id": id,
            "error": {"code": -32601, "message": format!("method not found: {method}")}
        }),
    }
}

async fn dispatch_tool(dispatcher: &dyn McpDispatcher, id: Value, params: Value) -> Value {
    let name = params.get("name").and_then(Value::as_str).unwrap_or("");
    let arguments = params.get("arguments").cloned().unwrap_or_else(|| json!({}));
    match dispatcher.call_tool(name, arguments).await {
        Ok(value) => json!({
            "jsonrpc": "2.0",
            "id": id,
            "result": {"content": [{"type": "text", "text": serde_json::to_string(&value).unwrap_or_default()}], "isError": false}
        }),
        Err(error) => json!({
            "jsonrpc": "2.0",
            "id": id,
            "result": {"content": [{"type": "text", "text": error.to_string()}], "isError": true}
        }),
    }
}

/// Convert a JSON object into a typed argument value with a useful error.
pub fn object_args(arguments: Value) -> Result<Map<String, Value>> {
    arguments
        .as_object()
        .cloned()
        .ok_or_else(|| multizen_core::MultizenError::Config("tool arguments must be an object".into()))
}
