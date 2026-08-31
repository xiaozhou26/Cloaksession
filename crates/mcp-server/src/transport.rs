//! axum HTTP transport skeleton: /mcp + /sse + /healthz + auth + DNS-rebinding.
//!
//! rmcp 0.1 axum integration is not wired here (API not confirmable);
//! Plan 4 will dispatch to a real `rmcp::ServiceServer`. This module
//! provides the testable surface: `parse_bearer`, `host_allowed`,
//! `build_router`, `allowed_hosts`, plus placeholder handlers.

use std::sync::Arc;

use axum::body::{to_bytes, Body};
use axum::extract::Request;
use axum::http::HeaderValue;
use axum::response::{IntoResponse, Json, Response};
use axum::routing::{get, post};
use axum::Router;
use serde_json::json;

use crate::server::{handle_json_rpc, McpDispatcher};

/// Strip the `Bearer ` prefix from an Authorization header value and return the token.
pub fn parse_bearer(auth: &HeaderValue) -> Option<String> {
    let s = auth.to_str().ok()?;
    let s = s.trim();
    let rest = s.strip_prefix("Bearer ")?;
    Some(rest.trim().to_string())
}

/// Exact-match the request Host header against the allow-list (DNS-rebinding defense).
pub fn host_allowed(host: &str, allowed: &[String]) -> bool {
    allowed.iter().any(|a| a == host)
}

/// Default allow-list for a given port: loopback + localhost.
pub fn allowed_hosts(port: u16) -> Vec<String> {
    vec![format!("127.0.0.1:{port}"), format!("localhost:{port}")]
}

/// Build the axum router with `/healthz`, `/mcp`, and `/sse` routes.
///
/// `auth_token`, when set, gates `/mcp` and `/sse` via bearer comparison
/// using `token::token_matches` (constant-time). `port` is the port the
/// server is served on, used to build the DNS-rebinding Host allow-list.
pub fn build_router(
    auth_token: Option<String>,
    port: u16,
    dispatcher: Arc<dyn McpDispatcher>,
) -> Router {
    let mcp_token = auth_token.clone();
    let sse_token = auth_token.clone();
    Router::new()
        .route("/healthz", get(healthz))
        .route(
            "/mcp",
            post(move |req: Request| {
                handle_mcp(req, mcp_token.clone(), port, dispatcher.clone())
            }),
        )
        .route(
            "/sse",
            get(move |req: Request| handle_sse(req, sse_token.clone(), port)),
        )
}

async fn healthz() -> Json<serde_json::Value> {
    Json(json!({"ok": true, "name": "multizen-mcp"}))
}

async fn handle_mcp(
    mut req: Request,
    auth_token: Option<String>,
    port: u16,
    dispatcher: Arc<dyn McpDispatcher>,
) -> Response {
    // Auth check (constant-time bearer compare).
    if let Some(tok) = &auth_token {
        let provided = req.headers().get("authorization").and_then(parse_bearer);
        match provided {
            Some(p) if crate::token::token_matches(&p, tok) => {}
            _ => {
                return (
                    axum::http::StatusCode::UNAUTHORIZED,
                    "unauthorized",
                )
                    .into_response();
            }
        }
    }

    // DNS-rebinding defense: Host header must be in the allow-list.
    let host = req
        .headers()
        .get("host")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    if !host_allowed(host, &allowed_hosts(port)) {
        return (
            axum::http::StatusCode::FORBIDDEN,
            "host not allowed",
        )
            .into_response();
    }

    let body = std::mem::replace(req.body_mut(), Body::empty());
    let bytes = match to_bytes(body, 1024 * 1024).await {
        Ok(bytes) => bytes,
        Err(error) => {
            return (
                axum::http::StatusCode::BAD_REQUEST,
                format!("invalid request body: {error}"),
            )
                .into_response();
        }
    };
    let request_json: serde_json::Value = match serde_json::from_slice(&bytes) {
        Ok(value) => value,
        Err(error) => {
            return Json(json!({
                "jsonrpc": "2.0",
                "id": null,
                "error": {"code": -32700, "message": format!("parse error: {error}")}
            }))
            .into_response();
        }
    };

    Json(handle_json_rpc(dispatcher.as_ref(), request_json).await).into_response()
}

async fn handle_sse(req: Request, auth_token: Option<String>, port: u16) -> Response {
    // Auth + host mirror handle_mcp; SSE stream wiring lands in Plan 4.
    if let Some(tok) = &auth_token {
        let provided = req.headers().get("authorization").and_then(parse_bearer);
        match provided {
            Some(p) if crate::token::token_matches(&p, tok) => {}
            _ => {
                return (
                    axum::http::StatusCode::UNAUTHORIZED,
                    "unauthorized",
                )
                    .into_response();
            }
        }
    }

    let host = req
        .headers()
        .get("host")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    if !host_allowed(host, &allowed_hosts(port)) {
        return (
            axum::http::StatusCode::FORBIDDEN,
            "host not allowed",
        )
            .into_response();
    }

    (
        axum::http::StatusCode::NOT_IMPLEMENTED,
        "sse not wired",
    )
        .into_response()
}
