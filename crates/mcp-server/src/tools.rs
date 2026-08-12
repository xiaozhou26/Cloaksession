//! MCP tool handlers — pure logic layer.
//!
//! Each handler is an async function following the same shape:
//!   1. `activity.start_call(...)` to record the invocation in the activity log.
//!   2. Run security gates (`assert_profile_running`, `assert_safe_url`, CDP
//!      method/param checks, raw-CDP env gate, …) and the actual driver / PM
//!      calls.
//!   3. `activity.finish(...)` with status + summary + duration.
//!   4. Return the JSON payload (or an `Err` carrying a `MultizenError`).
//!
//! The handler layer is deliberately transport-agnostic: it returns
//! `Result<serde_json::Value>`. The rmcp wiring in `server.rs` is responsible
//! for turning those values into the final rmcp `ToolResult` (including the
//! `{isError:true, content:{error:{code,message}}}` shape on errors).
//!
//! `ProfileManager` methods are all *sync*; only the `BrowserDriver` calls
//! are async. The brief's examples showed `driver.is_running(...).await` —
//! that is wrong; `is_running` is sync per the trait contract.

use std::time::{Duration, Instant};

use multizen_core::{
    CreateProfileInput, DeviceFamily, MultizenError, PartialFingerprintInput, ProxyConfig, Result,
    UpdateProfileInput,
};
use profile_manager::ProfileManager;

use crate::activity::ActivityLog;
use crate::driver::BrowserDriver;
use crate::schema::{
    ActivateTabArgs, CdpSendArgs, ClickArgs, CloseTabArgs, CreateProfileArgs, EvaluateJsArgs,
    GetCookiesArgs, NavigateArgs, NewTabArgs, PartialFingerprintSchema, ProxyConfigSchema,
    SetCookiesArgs, TypeArgs, UpdateProfileArgs, WaitForNavigationArgs, WaitForSelectorArgs,
};
use crate::security;

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

/// Returns true if raw CDP access is enabled via env var. Default off.
fn raw_cdp_enabled() -> bool {
    std::env::var("MULTIZEN_MCP_ALLOW_RAW_CDP")
        .ok()
        .map(|v| matches!(v.as_str(), "1" | "true" | "yes" | "on"))
        .unwrap_or(false)
}

/// Assert the given profile is currently running under the driver.
/// `BrowserDriver::is_running` is sync — we do NOT `.await` it.
fn assert_profile_running(driver: &dyn BrowserDriver, profile_id: &str) -> Result<()> {
    if !driver.is_running(profile_id) {
        return Err(MultizenError::NotFound(profile_id.to_string()));
    }
    Ok(())
}

/// Schema mirror → core conversion. Same camelCase wire shape, so a
/// serde round-trip is the simplest correct path.
impl From<ProxyConfigSchema> for ProxyConfig {
    fn from(s: ProxyConfigSchema) -> Self {
        // serde_json round-trip keeps the `#[serde(rename = "type")]` mapping
        // consistent on both sides.
        let v = serde_json::to_value(&s).expect("ProxyConfigSchema serializable");
        serde_json::from_value(v).expect("ProxyConfig wire-compatible with ProxyConfigSchema")
    }
}

impl From<PartialFingerprintSchema> for PartialFingerprintInput {
    fn from(s: PartialFingerprintSchema) -> Self {
        let v = serde_json::to_value(&s).expect("PartialFingerprintSchema serializable");
        serde_json::from_value(v).expect("PartialFingerprint wire-compatible")
    }
}

/// Serialize a value with camelCase (the default for our core types) and
/// produce a JSON object suitable for returning to MCP clients.
fn redacted_proxy_value(proxy: Option<&ProxyConfig>) -> serde_json::Value {
    match proxy {
        Some(p) => security::redacted_proxy(p),
        None => serde_json::Value::Null,
    }
}

/// Map a `MultizenError` into a structured MCP error JSON value.
/// The rmcp layer (`server.rs`) is responsible for wrapping this into the
/// final `isError:true` tool result; here we just surface the structured
/// payload so callers can decide how to present it.
pub fn error_json(err: &MultizenError) -> serde_json::Value {
    let code = match err {
        MultizenError::NotFound(_) => "PROFILE_NOT_FOUND",
        MultizenError::Launch(_) => "LAUNCH_FAILED",
        MultizenError::AlreadyExists(_) => "ALREADY_EXISTS",
        MultizenError::Mcp(_) => "FORBIDDEN",
        MultizenError::Cdp(_) => "CDP_ERROR",
        MultizenError::Config(_) => "INVALID_INPUT",
        MultizenError::Db(_) | MultizenError::Io(_) | MultizenError::Serde(_) => "INTERNAL_ERROR",
    };
    serde_json::json!({ "error": { "code": code, "message": err.to_string() } })
}

// ---------------------------------------------------------------------------
// profile lifecycle
// ---------------------------------------------------------------------------

pub async fn list_profiles(
    driver: &dyn BrowserDriver,
    pm: &ProfileManager,
    activity: &ActivityLog,
    _args: crate::schema::ListProfilesArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call("list_profiles", None, serde_json::json!({}));
    let started = Instant::now();
    let res: Result<Vec<_>> = (|| {
        let mut summaries = pm.list()?;
        // Overlay live running state from the driver (sync call).
        for s in &mut summaries {
            s.is_running = driver.is_running(&s.id);
        }
        Ok(summaries)
    })();
    let out = match res {
        Ok(summaries) => {
            let arr: Vec<serde_json::Value> = summaries
                .iter()
                .map(|s| {
                    serde_json::json!({
                        "id": s.id,
                        "name": s.name,
                        "tags": s.tags,
                        "lastOpenedAt": s.last_opened_at,
                        "isRunning": s.is_running,
                        "icon": s.icon,
                        "proxy": redacted_proxy_value(s.proxy.as_ref()),
                        "timezone": s.timezone,
                        "proxyCountry": s.proxy_country,
                        "device": s.device,
                    })
                })
                .collect();
            Ok(serde_json::json!({ "profiles": arr }))
        }
        Err(e) => Err(e),
    };
    let (status, summary) = status_summary(&out);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    out
}

pub async fn launch_profile(
    driver: &dyn BrowserDriver,
    pm: &ProfileManager,
    activity: &ActivityLog,
    args: crate::schema::ProfileIdArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "launch_profile",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        if pm.get(&args.profile_id)?.is_none() {
            return Err(MultizenError::NotFound(args.profile_id.clone()));
        }
        let launched = driver.launch(&args.profile_id).await?;
        pm.mark_opened(&args.profile_id)?;
        Ok(serde_json::to_value(&launched).unwrap_or_default())
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn close_profile(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: crate::schema::ProfileIdArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "close_profile",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        driver.close(&args.profile_id).await?;
        Ok(serde_json::json!({ "closed": true }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

// ---------------------------------------------------------------------------
// page interaction
// ---------------------------------------------------------------------------

pub async fn navigate(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: NavigateArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "navigate",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        security::assert_safe_url(&args.url)?;
        let url = driver.navigate(&args.profile_id, &args.url).await?;
        Ok(serde_json::json!({ "url": url }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn click(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: ClickArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "click",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        driver.click(&args.profile_id, &args.selector).await?;
        Ok(serde_json::json!({ "clicked": true }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn type_text(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: TypeArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "type",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        driver
            .type_text(&args.profile_id, &args.selector, &args.text)
            .await?;
        Ok(serde_json::json!({ "typed": true }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn extract(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: crate::schema::ProfileIdArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "extract",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let data = driver.extract(&args.profile_id).await?;
        Ok(serde_json::json!({ "data": data }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn screenshot(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: crate::schema::ProfileIdArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "screenshot",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let data = driver.screenshot(&args.profile_id).await?;
        Ok(serde_json::json!({ "data": data }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

// ---------------------------------------------------------------------------
// profile management
// ---------------------------------------------------------------------------

pub async fn create_profile(
    _driver: &dyn BrowserDriver,
    pm: &ProfileManager,
    activity: &ActivityLog,
    args: CreateProfileArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "create_profile",
        None,
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = (|| {
        // `seed` is accepted on the wire but `CreateProfileInput` has no seed
        // field. The manager's `create` already calls `default_fingerprint(&id)`
        // internally and merges any `fingerprint` patch the caller supplies, so
        // we simply ignore `seed` here (documented in concerns). If a caller
        // wants a deterministic fingerprint they should pass an explicit
        // `fingerprint` patch.
        let input = CreateProfileInput {
            name: args.name.clone(),
            notes: args.notes.clone(),
            tags: args.tags.clone(),
            icon: None,
            start_url: None,
            search_provider: None,
            proxy: args.proxy.map(ProxyConfig::from),
            fingerprint: args.fingerprint.map(PartialFingerprintInput::from),
            extensions: None,
        };
        let profile = pm.create(input)?;
        let fingerprint_summary = serde_json::json!({
            "device": profile.fingerprint.device,
            "userAgent": profile.fingerprint.user_agent,
            "locale": profile.fingerprint.locale,
            "timezone": profile.fingerprint.timezone,
            "country": profile.fingerprint.country,
        });
        Ok(serde_json::json!({
            "id": profile.id,
            "name": profile.name,
            "proxy": redacted_proxy_value(profile.proxy.as_ref()),
            "fingerprint": fingerprint_summary,
        }))
    })();
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn update_profile(
    _driver: &dyn BrowserDriver,
    pm: &ProfileManager,
    activity: &ActivityLog,
    args: UpdateProfileArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "update_profile",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = (|| {
        // Map the flat `Option<T>` args surface to the core `Option<Option<T>>`
        // convention: None in args => keep (pass None); Some(v) in args =>
        // set (pass Some(Some(v))). We do not expose the "clear" semantic
        // (Some(None)) via this tool surface — callers who want to clear a
        // field use `delete_profile` + recreate.
        let proxy = args.proxy.map(|p| Some(ProxyConfig::from(p)));
        let icon = None;
        let start_url = None;
        let search_provider = None;
        // MCP exposes a partial fingerprint (a few fields); the core
        // `UpdateProfileInput` now expects a whole `FingerprintConfig`.
        // Fetch the existing profile, apply the partial patch, then pass
        // the complete config so no fields are lost.
        let fingerprint = if let Some(p) = args.fingerprint {
            let existing = pm.get(&args.profile_id)?
                .ok_or_else(|| MultizenError::NotFound(args.profile_id.clone()))?;
            let mut fp = existing.fingerprint;
            if let Some(v) = p.user_agent { fp.user_agent = v; }
            if let Some(v) = p.locale { fp.locale = v; }
            if let Some(v) = p.timezone { fp.timezone = v; }
            if let Some(v) = p.country { fp.country = v; }
            Some(fp)
        } else {
            None
        };
        let patch = UpdateProfileInput {
            name: args.name.clone(),
            notes: args.notes.clone(),
            tags: args.tags.clone(),
            icon,
            start_url,
            search_provider,
            proxy,
            fingerprint,
            extensions: None,
        };
        let profile = pm.update(&args.profile_id, patch)?;
        Ok(serde_json::json!({
            "id": profile.id,
            "name": profile.name,
            "proxy": redacted_proxy_value(profile.proxy.as_ref()),
            "appliesOnNextLaunch": true,
        }))
    })();
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn delete_profile(
    driver: &dyn BrowserDriver,
    pm: &ProfileManager,
    activity: &ActivityLog,
    args: crate::schema::ProfileIdArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "delete_profile",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        if driver.is_running(&args.profile_id) {
            driver.close(&args.profile_id).await?;
        }
        pm.delete(&args.profile_id)?;
        Ok(serde_json::json!({ "deleted": true }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn list_fingerprint_options(
    _driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    _args: crate::schema::ListProfilesArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call("list_fingerprint_options", None, serde_json::json!({}));
    let started = Instant::now();
    let res = Ok(serde_json::json!({
        "devices": all_device_families(),
        "locales": common_locales(),
    }));
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

fn all_device_families() -> Vec<&'static str> {
    // mirroring `multizen_core::DeviceFamily` variants via serde rename values
    vec![
        "macbook-pro-14-m3",
        "macbook-pro-14-m3-pro",
        "macbook-pro-16-m3-pro",
        "macbook-air-13-m3",
        "macbook-air-15-m3",
        "imac-24-m3",
        "mac-mini-m2",
        "windows-laptop-intel",
        "windows-laptop-intel-uhd",
        "windows-laptop-amd",
        "windows-laptop-nvidia",
        "windows-laptop-nvidia-4050",
        "windows-desktop-nvidia",
        "windows-desktop-nvidia-4080",
        "windows-desktop-amd",
        "windows-desktop-intel",
        "linux-desktop-intel",
        "linux-desktop-amd",
        "linux-desktop-nvidia",
    ]
}

fn common_locales() -> Vec<&'static str> {
    vec![
        "en-US", "en-GB", "zh-CN", "zh-TW", "ja-JP", "ko-KR", "de-DE", "fr-FR",
        "es-ES", "pt-BR", "ru-RU", "it-IT", "nl-NL", "pl-PL", "tr-TR", "ar-SA",
        "hi-IN", "id-ID", "th-TH", "vi-VN",
    ]
}

// ---------------------------------------------------------------------------
// CDP-backed tools
// ---------------------------------------------------------------------------

/// Defense-in-depth wrapper: verify the CDP method is allowed before
/// dispatching via `driver.cdp_send`. All internal CDP tools must route
/// through this helper so the allow-list is enforced uniformly (the public
/// `cdp_send` tool already checks; internal tools previously skipped it).
async fn cdp_send_safe(
    driver: &dyn BrowserDriver,
    profile_id: &str,
    method: &str,
    params: Option<serde_json::Value>,
    session_id: Option<&str>,
) -> Result<serde_json::Value> {
    if !security::cdp_method_allowed(method) {
        return Err(MultizenError::Mcp(format!(
            "CDP method `{method}` is not allowed"
        )));
    }
    driver
        .cdp_send(profile_id, method, params, session_id, true)
        .await
}

pub async fn evaluate_js(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: EvaluateJsArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "evaluate_js",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let params = serde_json::json!({
            "expression": args.expression,
            "returnByValue": true,
        });
        let result = cdp_send_safe(
            driver,
            &args.profile_id,
            "Runtime.evaluate",
            Some(params),
            args.session_id.as_deref(),
        )
        .await?;
        Ok(result)
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn wait_for_selector(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: WaitForSelectorArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "wait_for_selector",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let deadline = started + Duration::from_millis(args.timeout_ms.max(1));
        let interval = Duration::from_millis(150);
        let expr = format!("!!document.querySelector({:?})", args.selector);
        loop {
            let params = serde_json::json!({
                "expression": expr,
                "returnByValue": true,
            });
            let result = cdp_send_safe(
                driver,
                &args.profile_id,
                "Runtime.evaluate",
                Some(params),
                None,
            )
            .await?;
            let found = result
                .get("result")
                .and_then(|r| r.get("value"))
                .and_then(|v| v.as_bool())
                .unwrap_or(false);
            if found {
                return Ok(serde_json::json!({ "found": true }));
            }
            if Instant::now() >= deadline {
                return Ok(serde_json::json!({ "found": false, "timedOut": true }));
            }
            tokio::time::sleep(interval).await;
        }
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn list_tabs(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: crate::schema::ProfileIdArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "list_tabs",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let result = cdp_send_safe(
            driver,
            &args.profile_id,
            "Target.getTargets",
            None,
            None,
        )
        .await?;
        Ok(result)
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn activate_tab(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: ActivateTabArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "activate_tab",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let params = serde_json::json!({ "targetId": args.tab_id });
        let _ = cdp_send_safe(
            driver,
            &args.profile_id,
            "Target.activateTarget",
            Some(params),
            None,
        )
        .await?;
        Ok(serde_json::json!({ "activated": true }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn close_tab(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: CloseTabArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "close_tab",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let params = serde_json::json!({ "targetId": args.tab_id });
        let _ = cdp_send_safe(
            driver,
            &args.profile_id,
            "Target.closeTarget",
            Some(params),
            None,
        )
        .await?;
        Ok(serde_json::json!({ "closed": true }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn wait_for_navigation(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: WaitForNavigationArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "wait_for_navigation",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let timeout = args.timeout_ms.unwrap_or(30000).max(1);
        let deadline = started + Duration::from_millis(timeout);
        let interval = Duration::from_millis(150);
        loop {
            let ready = poll_ready_state(driver, &args.profile_id).await?;
            if ready {
                return Ok(serde_json::json!({ "ready": true }));
            }
            if Instant::now() >= deadline {
                return Ok(serde_json::json!({ "ready": false, "timedOut": true }));
            }
            tokio::time::sleep(interval).await;
        }
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn wait_for_load(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: WaitForNavigationArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "wait_for_load",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        let timeout = args.timeout_ms.unwrap_or(30000).max(1);
        let deadline = started + Duration::from_millis(timeout);
        let interval = Duration::from_millis(150);
        loop {
            let ready = poll_ready_state(driver, &args.profile_id).await?;
            if ready {
                return Ok(serde_json::json!({ "loaded": true }));
            }
            if Instant::now() >= deadline {
                return Ok(serde_json::json!({ "loaded": false, "timedOut": true }));
            }
            tokio::time::sleep(interval).await;
        }
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

async fn poll_ready_state(driver: &dyn BrowserDriver, profile_id: &str) -> Result<bool> {
    let params = serde_json::json!({
        "expression": "document.readyState === 'complete'",
        "returnByValue": true,
    });
    let result = cdp_send_safe(driver, profile_id, "Runtime.evaluate", Some(params), None)
        .await?;
    Ok(result
        .get("result")
        .and_then(|r| r.get("value"))
        .and_then(|v| v.as_bool())
        .unwrap_or(false))
}

pub async fn cdp_send(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: CdpSendArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "cdp_send",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        if !raw_cdp_enabled() {
            return Err(MultizenError::Mcp("raw CDP disabled".into()));
        }
        if !security::cdp_method_allowed(&args.method) {
            return Err(MultizenError::Mcp(format!(
                "forbidden CDP method: {}",
                args.method
            )));
        }
        if let Some(params) = &args.params {
            security::assert_no_blocked_scheme_in_params(params)?;
        }
        assert_profile_running(driver, &args.profile_id)?;
        driver
            .cdp_send(
                &args.profile_id,
                &args.method,
                args.params.clone(),
                args.session_id.as_deref(),
                true,
            )
            .await
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn get_cookies(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: GetCookiesArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "get_cookies",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        for u in &args.urls {
            security::assert_safe_url(u)?;
        }
        let params = serde_json::json!({ "urls": args.urls });
        let result = cdp_send_safe(
            driver,
            &args.profile_id,
            "Network.getCookies",
            Some(params),
            args.session_id.as_deref(),
        )
        .await?;
        Ok(result)
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn set_cookies(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: SetCookiesArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "set_cookies",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        // Surface blocked schemes anywhere in cookie values (best-effort).
        for c in &args.cookies {
            security::assert_no_blocked_scheme_in_params(c)?;
        }
        let params = serde_json::json!({ "cookies": args.cookies });
        let _ = cdp_send_safe(
            driver,
            &args.profile_id,
            "Network.setCookies",
            Some(params),
            args.session_id.as_deref(),
        )
        .await?;
        Ok(serde_json::json!({ "set": true }))
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

pub async fn new_tab(
    driver: &dyn BrowserDriver,
    _pm: &ProfileManager,
    activity: &ActivityLog,
    args: NewTabArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call(
        "new_tab",
        Some(args.profile_id.clone()),
        serde_json::to_value(&args).unwrap_or_default(),
    );
    let started = Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id)?;
        security::assert_safe_url(&args.url)?;
        let params = serde_json::json!({ "url": args.url });
        let result = cdp_send_safe(
            driver,
            &args.profile_id,
            "Target.createTarget",
            Some(params),
            None,
        )
        .await?;
        Ok(result)
    }
    .await;
    let (status, summary) = status_summary(&res);
    activity
        .finish(&id, status, summary, Some(started.elapsed().as_millis() as u64))
        .await;
    res
}

// ---------------------------------------------------------------------------
// small shared helpers
// ---------------------------------------------------------------------------

fn status_summary<T, E>(res: &std::result::Result<T, E>) -> (&'static str, Option<String>)
where
    T: std::fmt::Debug,
    E: std::fmt::Display,
{
    match res {
        Ok(v) => ("ok", Some(format!("{v:?}"))),
        Err(e) => ("error", Some(e.to_string())),
    }
}

// re-export the tool name list for server.rs wiring
pub const TOOL_NAMES: &[&str] = &[
    "list_profiles",
    "launch_profile",
    "close_profile",
    "navigate",
    "click",
    "type",
    "extract",
    "screenshot",
    "create_profile",
    "update_profile",
    "delete_profile",
    "list_fingerprint_options",
    "evaluate_js",
    "wait_for_selector",
    "list_tabs",
    "activate_tab",
    "close_tab",
    "wait_for_navigation",
    "wait_for_load",
    "cdp_send",
    "get_cookies",
    "set_cookies",
    "new_tab",
];

// allow DeviceFamily to be referenced in create_profile summary without
// pulling the full multizen-core re-export in scope at call sites.
fn _device_family_marker(_d: DeviceFamily) {}

// silence unused-import warning if a future refactor drops one of these uses
#[allow(dead_code)]
fn _unused_imports() {
    let _ = std::convert::identity::<ProxyConfig>;
}
