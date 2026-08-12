//! Embedded MCP HTTP server startup.
//!
//! `start_embedded_mcp` spawns an axum server on the configured port
//! serving `mcp_server::transport::build_router`. The `driver` and
//! `activity` handles are stashed in a process-wide `OnceLock` so that
//! the rmcp ServiceServer dispatch (wired in P4.5+) can reach them.
//! Until then the transport handlers return a placeholder -32601
//! response, so the stashed state is currently unused — but storing
//! it now keeps P4.5 a pure handler-body change with no new plumbing.

use std::sync::{Arc, OnceLock};

use mcp_server::activity::ActivityLog;
use mcp_server::transport::build_router;
use tokio::net::TcpListener;

use crate::driver::TauriBrowserDriver;

/// Handles the rmcp dispatch will need once P4.5 wires it.
#[derive(Clone)]
pub struct McpState {
    pub driver: Arc<TauriBrowserDriver>,
    pub activity: Arc<ActivityLog>,
}

/// Process-wide stash for the MCP server's state. Populated by
/// `start_embedded_mcp`; read by transport handlers in P4.5+.
static MCP_STATE: OnceLock<McpState> = OnceLock::new();

/// Borrow the stashed MCP state, if it has been set.
pub fn mcp_state() -> Option<&'static McpState> {
    MCP_STATE.get()
}

/// Spawn the embedded MCP HTTP server on loopback:`port`.
///
/// Runs in a Tauri async-runtime task. Returns immediately; the server
/// runs until the process exits. Errors during bind/listen are logged
/// via `tracing::error!` and the task exits — the rest of the app
/// continues to run (MCP is an opt-in auxiliary service).
pub fn start_embedded_mcp(
    port: u16,
    token: String,
    driver: Arc<TauriBrowserDriver>,
    activity: Arc<ActivityLog>,
) {
    let state = McpState { driver, activity };
    if MCP_STATE.set(state).is_err() {
        tracing::warn!("mcp state already initialized; overwriting request ignored");
    }

    let router = build_router(Some(token), port);

    tauri::async_runtime::spawn(async move {
        let addr = std::net::SocketAddr::from(([127, 0, 0, 1], port));
        match TcpListener::bind(addr).await {
            Ok(listener) => {
                tracing::info!("mcp http server listening on {}", addr);
                if let Err(e) = axum::serve(listener, router).await {
                    tracing::error!("mcp axum serve exited: {e}");
                }
            }
            Err(e) => {
                tracing::error!("mcp http bind {addr} failed: {e}");
            }
        }
    });
}
