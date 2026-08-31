//! Embedded MCP HTTP server startup.

use std::sync::{Arc, OnceLock};
use std::time::{Duration, Instant};

use async_trait::async_trait;
use mcp_server::activity::ActivityLog;
use mcp_server::driver::BrowserDriver;
use mcp_server::schema::*;
use mcp_server::server::McpDispatcher;
use mcp_server::transport::build_router;
use multizen_core::{MultizenError, Result};
use serde_json::{json, Value};
use tokio::net::TcpListener;

use crate::driver::TauriBrowserDriver;

#[derive(Clone)]
pub struct McpState {
    pub driver: Arc<TauriBrowserDriver>,
    pub activity: Arc<ActivityLog>,
}

static MCP_STATE: OnceLock<McpState> = OnceLock::new();

pub fn mcp_state() -> Option<&'static McpState> {
    MCP_STATE.get()
}

#[derive(Clone)]
struct TauriMcpDispatcher {
    driver: Arc<TauriBrowserDriver>,
    activity: Arc<ActivityLog>,
}

impl TauriMcpDispatcher {
    fn profile_id(arguments: &Value) -> Result<String> {
        serde_json::from_value::<ProfileIdArgs>(arguments.clone())
            .map(|args| args.profile_id)
            .map_err(|e| MultizenError::Config(format!("invalid tool arguments: {e}")))
    }

    fn parse<T: serde::de::DeserializeOwned>(arguments: &Value) -> Result<T> {
        serde_json::from_value(arguments.clone())
            .map_err(|e| MultizenError::Config(format!("invalid tool arguments: {e}")))
    }

    fn assert_running(&self, profile_id: &str) -> Result<()> {
        if self.driver.is_running(profile_id) {
            Ok(())
        } else {
            Err(MultizenError::NotFound(profile_id.to_string()))
        }
    }

    fn activity_profile_id(name: &str, arguments: &Value) -> Option<String> {
        if matches!(name, "list_profiles" | "create_profile" | "list_fingerprint_options") {
            None
        } else {
            arguments
                .get("profileId")
                .and_then(Value::as_str)
                .map(str::to_string)
        }
    }

    async fn dispatch(&self, name: &str, arguments: Value) -> Result<Value> {
        match name {
            "list_profiles" => {
                let profiles = self.driver.list_profiles().await?;
                let profiles = profiles
                    .into_iter()
                    .map(|profile| {
                        json!({
                            "id": profile.id,
                            "name": profile.name,
                            "tags": profile.tags,
                            "lastOpenedAt": profile.last_opened_at,
                            "isRunning": self.driver.is_running(&profile.id),
                            "icon": profile.icon,
                            "proxy": profile.proxy.as_ref().map(mcp_server::security::redacted_proxy),
                            "timezone": profile.timezone,
                            "proxyCountry": profile.proxy_country,
                            "device": profile.device,
                        })
                    })
                    .collect::<Vec<_>>();
                Ok(json!({ "profiles": profiles }))
            }
            "launch_profile" => {
                let profile_id = Self::profile_id(&arguments)?;
                if self.driver.get_profile(&profile_id).await?.is_none() {
                    return Err(MultizenError::NotFound(profile_id));
                }
                Ok(serde_json::to_value(self.driver.launch(&profile_id).await?)?)
            }
            "close_profile" => {
                let profile_id = Self::profile_id(&arguments)?;
                self.driver.close(&profile_id).await?;
                Ok(json!({ "closed": true }))
            }
            "navigate" => {
                let args: NavigateArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                mcp_server::security::assert_safe_url(&args.url)?;
                Ok(json!({ "url": self.driver.navigate(&args.profile_id, &args.url).await? }))
            }
            "click" => {
                let args: ClickArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                self.driver.click(&args.profile_id, &args.selector).await?;
                Ok(json!({ "clicked": true }))
            }
            "type" => {
                let args: TypeArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                self.driver.type_text(&args.profile_id, &args.selector, &args.text).await?;
                Ok(json!({ "typed": true }))
            }
            "extract" => {
                let profile_id = Self::profile_id(&arguments)?;
                self.assert_running(&profile_id)?;
                Ok(json!({ "data": self.driver.extract(&profile_id).await? }))
            }
            "screenshot" => {
                let profile_id = Self::profile_id(&arguments)?;
                self.assert_running(&profile_id)?;
                Ok(json!({ "data": self.driver.screenshot(&profile_id).await? }))
            }
            "create_profile" => {
                let input: multizen_core::CreateProfileInput = serde_json::from_value(arguments)?;
                let profile = self.driver.create_profile(input).await?;
                Ok(json!({
                    "id": profile.id,
                    "name": profile.name,
                    "proxy": profile.proxy.as_ref().map(mcp_server::security::redacted_proxy),
                    "fingerprint": {
                        "device": profile.fingerprint.device,
                        "userAgent": profile.fingerprint.user_agent,
                        "locale": profile.fingerprint.locale,
                        "timezone": profile.fingerprint.timezone,
                        "country": profile.fingerprint.country,
                    },
                }))
            }
            "update_profile" => {
                let args: UpdateProfileArgs = Self::parse(&arguments)?;
                let existing = self.driver.get_profile(&args.profile_id).await?
                    .ok_or_else(|| MultizenError::NotFound(args.profile_id.clone()))?;
                let fingerprint = if let Some(p) = args.fingerprint {
                    let mut fp = existing.fingerprint;
                    if let Some(v) = p.user_agent { fp.user_agent = v; }
                    if let Some(v) = p.locale { fp.locale = v; }
                    if let Some(v) = p.timezone { fp.timezone = v; }
                    if let Some(v) = p.country { fp.country = v; }
                    Some(fp)
                } else { None };
                let proxy = args.proxy.map(|p| Some(multizen_core::ProxyConfig::from(p)));
                let patch = multizen_core::UpdateProfileInput {
                    name: args.name,
                    notes: args.notes,
                    tags: args.tags,
                    icon: None,
                    start_url: None,
                    search_provider: None,
                    proxy,
                    fingerprint,
                    extensions: None,
                };
                let profile = self.driver.update_profile(&args.profile_id, patch).await?;
                Ok(json!({
                    "id": profile.id,
                    "name": profile.name,
                    "proxy": profile.proxy.as_ref().map(mcp_server::security::redacted_proxy),
                    "appliesOnNextLaunch": true,
                }))
            }
            "delete_profile" => {
                let profile_id = Self::profile_id(&arguments)?;
                if self.driver.is_running(&profile_id) {
                    self.driver.close(&profile_id).await?;
                }
                self.driver.delete_profile(&profile_id).await?;
                Ok(json!({ "deleted": true }))
            }
            "list_fingerprint_options" => Ok(json!({
                "devices": [
                    "macbook-pro-14-m3", "macbook-pro-14-m3-pro", "macbook-pro-16-m3-pro",
                    "macbook-air-13-m3", "macbook-air-15-m3", "imac-24-m3", "mac-mini-m2",
                    "windows-laptop-intel", "windows-laptop-intel-uhd", "windows-laptop-amd",
                    "windows-laptop-nvidia", "windows-laptop-nvidia-4050", "windows-desktop-nvidia",
                    "windows-desktop-nvidia-4080", "windows-desktop-amd", "windows-desktop-intel",
                    "linux-desktop-intel", "linux-desktop-amd", "linux-desktop-nvidia"
                ],
                "locales": ["en-US", "en-GB", "zh-CN", "zh-TW", "ja-JP", "ko-KR", "de-DE", "fr-FR", "es-ES", "pt-BR", "ru-RU", "it-IT", "nl-NL", "pl-PL", "tr-TR", "ar-SA", "hi-IN", "id-ID", "th-TH", "vi-VN"]
            })),
            "evaluate_js" => {
                let args: EvaluateJsArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                self.driver.cdp_send(&args.profile_id, "Runtime.evaluate", Some(json!({
                    "expression": args.expression,
                    "returnByValue": true,
                })), args.session_id.as_deref(), true).await
            }
            "wait_for_selector" => {
                let args: WaitForSelectorArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                let deadline = Instant::now() + Duration::from_millis(args.timeout_ms.max(1));
                loop {
                    let result = self.driver.cdp_send(&args.profile_id, "Runtime.evaluate", Some(json!({
                        "expression": format!("!!document.querySelector({:?})", args.selector),
                        "returnByValue": true,
                    })), None, true).await?;
                    if result.get("result").and_then(|v| v.get("value")).and_then(Value::as_bool).unwrap_or(false) {
                        return Ok(json!({ "found": true }));
                    }
                    if Instant::now() >= deadline {
                        return Ok(json!({ "found": false, "timedOut": true }));
                    }
                    tokio::time::sleep(Duration::from_millis(150)).await;
                }
            }
            "list_tabs" => {
                let profile_id = Self::profile_id(&arguments)?;
                self.assert_running(&profile_id)?;
                self.driver.cdp_send(&profile_id, "Target.getTargets", None, None, true).await
            }
            "activate_tab" => {
                let args: ActivateTabArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                self.driver.activate_tab(&args.profile_id, &args.tab_id).await?;
                Ok(json!({ "activated": true }))
            }
            "close_tab" => {
                let args: CloseTabArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                self.driver.close_tab(&args.profile_id, &args.tab_id).await?;
                Ok(json!({ "closed": true }))
            }
            "wait_for_navigation" | "wait_for_load" => {
                let args: WaitForNavigationArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                let deadline = Instant::now() + Duration::from_millis(args.timeout_ms.unwrap_or(30000).max(1));
                loop {
                    let result = self.driver.cdp_send(&args.profile_id, "Runtime.evaluate", Some(json!({
                        "expression": "document.readyState === 'complete'",
                        "returnByValue": true,
                    })), None, true).await?;
                    let ready = result.get("result").and_then(|v| v.get("value")).and_then(Value::as_bool).unwrap_or(false);
                    if ready {
                        return Ok(if name == "wait_for_load" { json!({ "loaded": true }) } else { json!({ "ready": true }) });
                    }
                    if Instant::now() >= deadline {
                        return Ok(if name == "wait_for_load" { json!({ "loaded": false, "timedOut": true }) } else { json!({ "ready": false, "timedOut": true }) });
                    }
                    tokio::time::sleep(Duration::from_millis(150)).await;
                }
            }
            "cdp_send" => {
                let args: CdpSendArgs = Self::parse(&arguments)?;
                if !std::env::var("MULTIZEN_MCP_ALLOW_RAW_CDP").ok().map(|v| matches!(v.as_str(), "1" | "true" | "yes" | "on")).unwrap_or(false) {
                    return Err(MultizenError::Mcp("raw CDP disabled".into()));
                }
                if !mcp_server::security::cdp_method_allowed(&args.method) {
                    return Err(MultizenError::Mcp(format!("forbidden CDP method: {}", args.method)));
                }
                if let Some(params) = &args.params {
                    mcp_server::security::assert_no_blocked_scheme_in_params(params)?;
                }
                self.assert_running(&args.profile_id)?;
                self.driver.cdp_send(&args.profile_id, &args.method, args.params, args.session_id.as_deref(), true).await
            }
            "get_cookies" => {
                let args: GetCookiesArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                for url in &args.urls { mcp_server::security::assert_safe_url(url)?; }
                self.driver.cdp_send(&args.profile_id, "Network.getCookies", Some(json!({ "urls": args.urls })), args.session_id.as_deref(), true).await
            }
            "set_cookies" => {
                let args: SetCookiesArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                for cookie in &args.cookies { mcp_server::security::assert_no_blocked_scheme_in_params(cookie)?; }
                self.driver.cdp_send(&args.profile_id, "Network.setCookies", Some(json!({ "cookies": args.cookies })), args.session_id.as_deref(), true).await?;
                Ok(json!({ "set": true }))
            }
            "new_tab" => {
                let args: NewTabArgs = Self::parse(&arguments)?;
                self.assert_running(&args.profile_id)?;
                mcp_server::security::assert_safe_url(&args.url)?;
                let target_id = self.driver.new_tab(&args.profile_id, &args.url).await?;
                Ok(json!({ "targetId": target_id }))
            }
            _ => Err(MultizenError::Mcp(format!("unknown MCP tool `{name}`"))),
        }
    }
}

#[async_trait]
impl McpDispatcher for TauriMcpDispatcher {
    async fn call_tool(&self, name: &str, arguments: Value) -> Result<Value> {
        let activity_id = self.activity.start_call(
            name,
            Self::activity_profile_id(name, &arguments),
            arguments.clone(),
        );
        let started = Instant::now();
        let result = self.dispatch(name, arguments).await;
        let (status, summary) = match &result {
            Ok(value) => ("ok", Some(value.to_string())),
            Err(error) => ("error", Some(error.to_string())),
        };
        self.activity
            .finish(&activity_id, status, summary, Some(started.elapsed().as_millis() as u64))
            .await;
        result
    }
}

pub fn start_embedded_mcp(
    port: u16,
    token: String,
    driver: Arc<TauriBrowserDriver>,
    activity: Arc<ActivityLog>,
) {
    let state = McpState {
        driver: driver.clone(),
        activity: activity.clone(),
    };
    if MCP_STATE.set(state).is_err() {
        tracing::warn!("mcp state already initialized; keeping the first state");
    }

    let dispatcher: Arc<dyn McpDispatcher> = Arc::new(TauriMcpDispatcher { driver, activity });
    let router = build_router(Some(token), port, dispatcher);

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
                tracing::error!("mcp axum bind {addr} failed: {e}");
            }
        }
    });
}
