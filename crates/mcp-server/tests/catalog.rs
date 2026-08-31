//! Regression checks for MCP tool catalog visibility.

#[test]
fn raw_cdp_is_hidden_without_explicit_enablement() {
    std::env::remove_var("MULTIZEN_MCP_ALLOW_RAW_CDP");
    assert!(!mcp_server::server::tool_definitions()
        .iter()
        .any(|tool| tool["name"] == "cdp_send"));
}
