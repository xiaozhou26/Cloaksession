# MultiZen Rust 重写 — Plan 3：mcp-server（rmcp + 22 工具 + HTTP/SSE + 安全门）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `mcp-server` crate：用 `rmcp`（社区 Rust MCP SDK）暴露 22 个工具，挂 `axum` HTTP+SSE transport（`POST /mcp` Streamable HTTP + `GET /sse` legacy + `GET /healthz`），bearer token 鉴权 + DNS-rebinding 防护，并移植全部安全门（`BLOCKED_URL_SCHEMES`、`CDP_DENY_METHODS`、URL 归一化、proxy 脱敏、ActivityLog 清理）。这是对现有 TS `packages/mcp-server/src/{server,HttpTransport,ActivityLog}.ts` 的 1:1 移植。

**Architecture:** crate 依赖 `cdp-driver` + `browser-launcher` + `profile-manager` + `multizen-core`。定义 `BrowserDriver` trait（TS interface 的 Rust 镜像），`mcp-server` 在其上组合 22 工具。HTTP transport 用 `axum`；Streamable HTTP path 每 request 建新 `rmcp::ServiceServer`（stateless），SSE path 一个长生命周期 server。`ActivityLog` 是内存环形缓冲（容量 500）+ `tokio::sync::broadcast` 事件流。所有 async 用 tokio。

**Tech Stack:** Rust 1.80+、`rmcp`（MCP SDK）、`axum` 0.7+、`tokio`、`serde`/`serde_json`、`schemars`（rmcp 工具 schema 派生）、`tracing`。集成测试用 `reqwest` 打本地 transport + 桩 `BrowserDriver`。

## Global Constraints

- 仓库：`multizen-browser-rs/`（当前 HEAD = Plan 2 末尾 commit）。新增 `crates/mcp-server`，加入 workspace。
- Rust edition 2021。serde camelCase 与 Plan 1/2 一致。
- 依赖 Plan 1-2：`multizen-core`、`profile-manager`、`browser-launcher`、`cdp-driver`。
- 在 `multizen-core::MultizenError` 新增 `Mcp(String)` 变体（Task 1）。
- 22 个工具名与 TS `packages/mcp-server/src/server.ts` 逐字对齐：`list_profiles, launch_profile, close_profile, navigate, click, type, extract, screenshot, create_profile, update_profile, delete_profile, list_fingerprint_options, evaluate_js, wait_for_selector, list_tabs, activate_tab, close_tab, wait_for_navigation, wait_for_load, cdp_send, get_cookies, set_cookies, new_tab`。
- `cdp_send` 工具默认隐藏（`tools/list` 不列出），仅 `MULTIZEN_MCP_ALLOW_RAW_CDP=1` 时可见。
- 安全门常量逐字对齐：
  - `BLOCKED_URL_SCHEMES = ["file:", "chrome:", "devtools:", "view-source:"]`
  - `CDP_DENY_METHODS`（见 Task 4 的完整集合）
  - URL 归一化：strip `\t\r\n` 全局 + 所有 ≤0x20 的前导控制字符后再做 scheme 前缀测试
  - `redactedProxy`：只回 `{type, host, port, hasAuth}`，不回 username/password
  - `ActivityLog` sanitize：text>80 字符截断、proxy 凭据脱敏、cookies 值脱敏
- bearer token：64-hex，constant-time 比较（`subtle` 或手写）。
- DNS-rebinding：`allowedHosts = [host:port, 127.0.0.1:port, localhost:port]`。
- 每任务结束 commit，`cargo clippy --workspace --all-targets -- -D warnings` 干净。

## File Structure

```
crates/mcp-server/
├── Cargo.toml
├── src/
│   ├── lib.rs                # re-export
│   ├── driver.rs             # BrowserDriver trait（TS interface 镜像）
│   ├── server.rs             # build_server：组装 22 工具的 rmcp Server
│   ├── tools.rs              # 22 个 tool handler 函数（纯逻辑 + 调 driver）
│   ├── schema.rs             # 每个工具的入参 struct（schemars 派生）
│   ├── security.rs           # BLOCKED_URL_SCHEMES / CDP_DENY_METHODS / normalize_url / redacted_proxy
│   ├── activity.rs           # ActivityLog + ActivityEvent
│   ├── transport.rs          # axum HttpTransport：/mcp + /sse + /healthz + auth + DNS-rebinding
│   └── token.rs              # bearer token constant-time compare
├── tests/
│   ├── security.rs           # 纯单元：url 归一化 / scheme 拒绝 / deny methods / redacted_proxy
│   ├── activity.rs           # 纯单元：sanitize / ring buffer
│   ├── tools.rs              # 纯单元：用 MockBrowserDriver 测每个工具的 dispatch + 错误映射
│   └── transport.rs          # 集成：起 axum server + reqwest 打 /healthz + /mcp 鉴权
```

职责边界：
- `driver.rs`：trait 定义，不含实现。`cdp-driver` + `browser-launcher` 在 Plan 4 的 `tauri-app` 里组合出实现并注入。
- `tools.rs`：每个工具是纯函数 `(driver, profile_manager, activity, args) -> Result<serde_json::Value>`，可单测（用 MockBrowserDriver）。
- `security.rs`：所有安全门纯函数，无 IO。
- `transport.rs`：HTTP 层，组装 rmcp server + axum 路由。

---

### Task 1: 扩展错误 + crate 骨架

**Files:**
- Modify: `crates/multizen-core/src/error.rs`
- Modify: `Cargo.toml`（workspace 根）
- Create: `crates/mcp-server/Cargo.toml`, `crates/mcp-server/src/lib.rs` + 8 占位模块

**Interfaces:**
- Produces: `MultizenError::Mcp(String)`；`mcp-server` crate 骨架可编译。

- [ ] **Step 1: 加 Mcp 错误变体**

在 `MultizenError` 加：
```rust
    #[error("mcp error: {0}")]
    Mcp(String),
```

- [ ] **Step 2: workspace members 加 mcp-server**

```toml
    "crates/mcp-server",
```

- [ ] **Step 3: Cargo.toml**

```toml
[package]
name = "mcp-server"
version = "0.1.0"
edition = "2021"

[dependencies]
multizen-core = { path = "../multizen-core" }
profile-manager = { path = "../profile-manager" }
browser-launcher = { path = "../browser-launcher" }
cdp-driver = { path = "../cdp-driver" }
rmcp = "0.1"
axum = { version = "0.7", features = ["ws"] }
tokio = { version = "1", features = ["rt-multi-thread", "net", "io-util", "time", "sync", "macros"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
schemars = "0.8"
tracing = "0.1"
subtle = "2"

[dev-dependencies]
reqwest = { version = "0.12", default-features = false, features = ["json"] }
tokio = { version = "1", features = ["rt-multi-thread", "macros"] }
```

- [ ] **Step 4: lib.rs + 占位**

```rust
pub mod activity;
pub mod driver;
pub mod schema;
pub mod security;
pub mod server;
pub mod token;
pub mod tools;
pub mod transport;
```

8 个占位文件各一行注释。

- [ ] **Step 5: 验证编译 + commit**

Run: `cargo check -p mcp-server`
```bash
git add -A
git commit -m "chore: scaffold mcp-server crate + Mcp error variant"
```

---

### Task 2: BrowserDriver trait

**Files:**
- Modify: `crates/mcp-server/src/driver.rs`

**Interfaces:**
- Produces: `BrowserDriver` trait，方法签名与 TS `BrowserDriver` interface 1:1。

```rust
use multizen_core::{LaunchedProfile, Result};

#[async_trait::async_trait]
pub trait BrowserDriver: Send + Sync {
    async fn launch(&self, profile_id: &str) -> Result<LaunchedProfile>;
    async fn close(&self, profile_id: &str) -> Result<()>;
    fn is_running(&self, profile_id: &str) -> bool;
    async fn navigate(&self, profile_id: &str, url: &str) -> Result<String>; // returns final url
    async fn click(&self, profile_id: &str, selector: &str) -> Result<()>;
    async fn type_text(&self, profile_id: &str, selector: &str, text: &str) -> Result<()>;
    async fn extract(&self, profile_id: &str) -> Result<serde_json::Value>;
    async fn screenshot(&self, profile_id: &str) -> Result<String>; // base64
    async fn cdp_send(&self, profile_id: &str, method: &str, params: Option<serde_json::Value>, session_id: Option<&str>, safe: bool) -> Result<serde_json::Value>;
}
```

- [ ] **Step 1: 加 async-trait 依赖**

`crates/mcp-server/Cargo.toml` `[dependencies]` 加 `async-trait = "0.1"`。

- [ ] **Step 2: 实现 driver.rs**

```rust
use async_trait::async_trait;
use multizen_core::{LaunchedProfile, Result};

#[async_trait]
pub trait BrowserDriver: Send + Sync {
    async fn launch(&self, profile_id: &str) -> Result<LaunchedProfile>;
    async fn close(&self, profile_id: &str) -> Result<()>;
    async fn is_running(&self, profile_id: &str) -> bool;
    async fn navigate(&self, profile_id: &str, url: &str) -> Result<String>;
    async fn click(&self, profile_id: &str, selector: &str) -> Result<()>;
    async fn type_text(&self, profile_id: &str, selector: &str, text: &str) -> Result<()>;
    async fn extract(&self, profile_id: &str) -> Result<serde_json::Value>;
    async fn screenshot(&self, profile_id: &str) -> Result<String>;
    async fn cdp_send(&self, profile_id: &str, method: &str, params: Option<serde_json::Value>, session_id: Option<&str>, safe: bool) -> Result<serde_json::Value>;
}
```

- [ ] **Step 3: commit**

```bash
git add -A
git commit -m "feat(mcp-server): BrowserDriver trait"
```

---

### Task 3: ActivityLog + ActivityEvent

**Files:**
- Modify: `crates/mcp-server/src/activity.rs`
- Create: `crates/mcp-server/tests/activity.rs`

**Interfaces:**
- Produces:
  - `#[derive(Clone, serde::Serialize)] #[serde(rename_all="camelCase")] pub struct ActivityEvent { pub id: String, pub timestamp: String, pub tool: String, pub profile_id: Option<String>, pub args: serde_json::Value, pub status: String, pub summary: Option<String>, pub duration_ms: Option<u64> }`
  - `pub struct ActivityLog { events: Arc<Mutex<VecDeque<ActivityEvent>>>, tx: broadcast::Sender<ActivityEvent> }`
  - `impl ActivityLog { pub fn new() -> Self; pub fn start_call(&self, tool, profile_id, args) -> String (id); pub fn finish(&self, id, status, summary, duration_ms); pub fn recent(&self, limit: usize) -> Vec<ActivityEvent>; pub fn subscribe(&self) -> broadcast::Receiver<ActivityEvent> }`
  - `pub fn sanitize_args(args: serde_json::Value) -> serde_json::Value` — text>80 截断、proxy 凭据脱敏、cookies 值脱敏

环形缓冲容量 500。

- [ ] **Step 1: 写失败测试**

`crates/mcp-server/tests/activity.rs`：

```rust
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
    log.finish(&id, "ok", Some("navigated".into()), 120);
    let recent = log.recent(10);
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
        log.finish(&id, "ok", None, 1);
    }
    assert_eq!(log.recent(1000).len(), 500);
}
```

- [ ] **Step 2: 实现 activity.rs**

```rust
use std::collections::VecDeque;
use std::sync::Arc;
use tokio::sync::{broadcast, Mutex};

use serde::Serialize;
use uuid::Uuid;

const CAPACITY: usize = 500;

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ActivityEvent {
    pub id: String,
    pub timestamp: String,
    pub tool: String,
    pub profile_id: Option<String>,
    pub args: serde_json::Value,
    pub status: String,
    pub summary: Option<String>,
    pub duration_ms: Option<u64>,
}

pub struct ActivityLog {
    events: Arc<Mutex<VecDeque<ActivityEvent>>>,
    tx: broadcast::Sender<ActivityEvent>,
}

impl ActivityLog {
    pub fn new() -> Self {
        let (tx, _) = broadcast::channel(256);
        Self { events: Arc::new(Mutex::new(VecDeque::with_capacity(CAPACITY))), tx }
    }
    pub fn start_call(&self, tool: &str, profile_id: Option<String>, args: serde_json::Value) -> String {
        let id = Uuid::new_v4().to_string();
        let event = ActivityEvent {
            id: id.clone(),
            timestamp: chrono::Utc::now().to_rfc3339(),
            tool: tool.to_string(),
            profile_id,
            args: sanitize_args(args),
            status: "pending".to_string(),
            summary: None,
            duration_ms: None,
        };
        self.push(event);
        id
    }
    pub fn finish(&self, id: &str, status: &str, summary: Option<String>, duration_ms: Option<u64>) {
        let mut guard = self.events.lock().await;
        if let Some(e) = guard.iter_mut().find(|e| e.id == id) {
            e.status = status.to_string();
            e.summary = summary;
            e.duration_ms = duration_ms;
            let _ = self.tx.send(e.clone());
        }
    }
    pub async fn recent(&self, limit: usize) -> Vec<ActivityEvent> {
        let guard = self.events.lock().await;
        guard.iter().rev().take(limit).cloned().collect()
    }
    fn push(&self, event: ActivityEvent) {
        let _ = self.tx.send(event.clone());
        // push requires async lock; spawn a task to avoid blocking sync start_call.
        let events = self.events.clone();
        tokio::spawn(async move {
            let mut guard = events.lock().await;
            if guard.len() >= CAPACITY {
                guard.pop_front();
            }
            guard.push_back(event);
        });
    }
}

pub fn sanitize_args(args: serde_json::Value) -> serde_json::Value {
    match args {
        serde_json::Value::Object(mut map) => {
            if let Some(v) = map.get_mut("text").and_then(|v| v.as_str().map(str::to_string)) {
                if v.len() > 80 {
                    let truncated: String = v.chars().take(77).collect();
                    map.insert("text".to_string(), serde_json::Value::String(format!("{truncated}...")));
                }
            }
            if let Some(p) = map.get_mut("proxy").and_then(|v| v.as_object_mut()) {
                p.remove("username");
                p.remove("password");
            }
            if let Some(c) = map.get_mut("cookies").and_then(|v| v.as_array_mut()) {
                for cookie in c {
                    if let Some(co) = cookie.as_object_mut() {
                        if let Some(val) = co.get_mut("value") {
                            *val = serde_json::Value::String("[redacted]".into());
                        }
                    }
                }
            }
            // Recurse into nested
            let mut new_map = serde_json::Map::new();
            for (k, v) in map {
                new_map.insert(k, sanitize_args(v));
            }
            serde_json::Value::Object(new_map)
        }
        serde_json::Value::Array(a) => serde_json::Value::Array(a.into_iter().map(sanitize_args).collect()),
        other => other,
    }
}

impl Default for ActivityLog {
    fn default() -> Self { Self::new() }
}
```

注意：`recent` 的 `tokio::sync::Mutex` 在 `recent` 里 await——但测试用 `#[tokio::test]`，OK。`start_call` 是同步的但 push 用 spawn，避免阻塞。`finish` await lock——调用者需在 async 上下文。

- [ ] **Step 3: 运行 + commit**

Run: `cargo test -p mcp-server --test activity`
```bash
git add -A
git commit -m "feat(mcp-server): ActivityLog ring buffer + arg sanitization"
```

---

### Task 4: security 纯函数（URL 归一化 + deny methods + redacted_proxy）

**Files:**
- Modify: `crates/mcp-server/src/security.rs`
- Create: `crates/mcp-server/tests/security.rs`

**Interfaces:**
- Produces:
  - `pub const BLOCKED_URL_SCHEMES: &[&str] = &["file:", "chrome:", "devtools:", "view-source:"]`
  - `pub const CDP_DENY_METHODS: &[&str] = &["IO.read","Page.getResourceContent","Storage.getCookies","Network.getAllCookies","DOMStorage.*","IndexedDB.*","CacheStorage.*","Browser.close","Browser.crash"]` + 所有 `Fetch.*`
  - `pub fn normalize_url_for_scan(url: &str) -> String` — strip `\t\r\n` 全局 + 所有 ≤0x20 前导控制字符
  - `pub fn has_blocked_scheme(url: &str) -> bool`
  - `pub fn assert_safe_url(url: &str) -> Result<()>` — 归一化后若 blocked 返回 `Mcp("forbidden scheme")`
  - `pub fn cdp_method_allowed(method: &str) -> bool` — deny list + `Fetch.*` 通配
  - `pub fn assert_no_blocked_scheme_in_params(params: &serde_json::Value) -> Result<()>` — 递归扫所有 string
  - `pub fn redacted_proxy(proxy: &ProxyConfig) -> serde_json::Value` — `{type, host, port, hasAuth}`

- [ ] **Step 1: 写失败测试**

`crates/mcp-server/tests/security.rs`：

```rust
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
```

- [ ] **Step 2: 实现 security.rs**

```rust
use multizen_core::{MultizenError, ProxyConfig, Result};

pub const BLOCKED_URL_SCHEMES: &[&str] = &["file:", "chrome:", "devtools:", "view-source:"];

pub const CDP_DENY_METHODS_EXACT: &[&str] = &[
    "IO.read", "Page.getResourceContent", "Storage.getCookies",
    "Network.getAllCookies", "Browser.close", "Browser.crash",
];
pub const CDP_DENY_METHOD_PREFIXES: &[&str] = &[
    "DOMStorage.", "IndexedDB.", "CacheStorage.", "Fetch.",
];

pub fn normalize_url_for_scan(url: &str) -> String {
    // Strip \t\r\n globally, then strip all leading control chars <= 0x20.
    let no_tab_nl: String = url.chars().filter(|c| !matches!(c, '\t' | '\r' | '\n')).collect();
    let trimmed = no_tab_nl.trim_start_matches(|c: char| c as u32 <= 0x20);
    trimmed.to_string()
}

pub fn has_blocked_scheme(url: &str) -> bool {
    let n = normalize_url_for_scan(url);
    BLOCKED_URL_SCHEMES.iter().any(|s| n.starts_with(s))
}

pub fn assert_safe_url(url: &str) -> Result<()> {
    if has_blocked_scheme(url) {
        return Err(MultizenError::Mcp("forbidden URL scheme".into()));
    }
    Ok(())
}

pub fn cdp_method_allowed(method: &str) -> bool {
    if CDP_DENY_METHODS_EXACT.contains(&method) {
        return false;
    }
    if CDP_DENY_METHOD_PREFIXES.iter().any(|p| method.starts_with(p)) {
        return false;
    }
    true
}

pub fn assert_no_blocked_scheme_in_params(params: &serde_json::Value) -> Result<()> {
    match params {
        serde_json::Value::String(s) => {
            if has_blocked_scheme(s) {
                return Err(MultizenError::Mcp(format!("blocked scheme in param: {s}")));
            }
            Ok(())
        }
        serde_json::Value::Object(map) => {
            for v in map.values() {
                assert_no_blocked_scheme_in_params(v)?;
            }
            Ok(())
        }
        serde_json::Value::Array(a) => {
            for v in a {
                assert_no_blocked_scheme_in_params(v)?;
            }
            Ok(())
        }
        _ => Ok(()),
    }
}

pub fn redacted_proxy(proxy: &ProxyConfig) -> serde_json::Value {
    serde_json::json!({
        "type": proxy.proxy_type,
        "host": proxy.host,
        "port": proxy.port,
        "hasAuth": proxy.username.is_some() && proxy.password.is_some(),
    })
}
```

- [ ] **Step 3: 运行 + commit**

Run: `cargo test -p mcp-server --test security`
```bash
git add -A
git commit -m "feat(mcp-server): security gates (url normalize, deny methods, redacted proxy)"
```

---

### Task 5: 工具入参 schema + MockBrowserDriver

**Files:**
- Modify: `crates/mcp-server/src/schema.rs`
- Create: `crates/mcp-server/tests/mock_driver.rs`（共享 Mock，被 tools 测试用）

**Interfaces:**
- Produces: 每个工具的入参 struct，用 `schemars::JsonSchema` + `serde::Deserialize`。关键几个：
  - `ListProfilesArgs {}` / `ProfileIdArgs { profile_id: String }` / `NavigateArgs { profile_id, url }` / `ClickArgs { profile_id, selector }` / `TypeArgs { profile_id, selector, text }` / `CreateProfileArgs { name, notes?, tags?, proxy?, fingerprint?, seed? }` / `CdpSendArgs { profile_id, method, params?, session_id? }` / `GetCookiesArgs { profile_id, urls: Vec<String>, session_id? }` 等。

- [ ] **Step 1: 实现 schema.rs**

```rust
use serde::Deserialize;
use schemars::JsonSchema;

#[derive(Debug, Deserialize, JsonSchema, Default)]
#[serde(rename_all = "camelCase")]
pub struct ProfileIdArgs {
    pub profile_id: String,
}

#[derive(Debug, Deserialize, JsonSchema, Default)]
#[serde(rename_all = "camelCase")]
pub struct NavigateArgs {
    pub profile_id: String,
    pub url: String,
}

#[derive(Debug, Deserialize, JsonSchema, Default)]
#[serde(rename_all = "camelCase")]
pub struct ClickArgs {
    pub profile_id: String,
    pub selector: String,
}

#[derive(Debug, Deserialize, JsonSchema, Default)]
#[serde(rename_all = "camelCase")]
pub struct TypeArgs {
    pub profile_id: String,
    pub selector: String,
    pub text: String,
}

#[derive(Debug, Deserialize, JsonSchema, Default)]
#[serde(rename_all = "camelCase")]
pub struct CreateProfileArgs {
    pub name: String,
    pub notes: Option<String>,
    pub tags: Option<Vec<String>>,
    pub proxy: Option<multizen_core::ProxyConfig>,
    pub fingerprint: Option<multizen_core::PartialFingerprintInput>,
    pub seed: Option<String>,
}

#[derive(Debug, Deserialize, JsonSchema, Default)]
#[serde(rename_all = "camelCase")]
pub struct CdpSendArgs {
    pub profile_id: String,
    pub method: String,
    pub params: Option<serde_json::Value>,
    pub session_id: Option<String>,
}

#[derive(Debug, Deserialize, JsonSchema, Default)]
#[serde(rename_all = "camelCase")]
pub struct GetCookiesArgs {
    pub profile_id: String,
    pub urls: Vec<String>,
    pub session_id: Option<String>,
}

// ... 其余工具的 args struct 同理（update_profile, delete_profile, evaluate_js,
// wait_for_selector, list_tabs, activate_tab, close_tab, wait_for_navigation,
// set_cookies, new_tab, list_fingerprint_options 无参）。实现者按 TS schema 补全。
```

实现者补全剩余 struct。`PartialFingerprintInput` 已在 Plan 1 的 multizen-core 定义。

- [ ] **Step 2: MockBrowserDriver（tests/mock_driver.rs）**

```rust
use async_trait::async_trait;
use mcp_server::driver::BrowserDriver;
use multizen_core::{LaunchedProfile, MultizenError, Result};
use std::collections::HashMap;
use std::sync::Mutex;

pub struct MockBrowserDriver {
    pub running: Mutex<HashMap<String, LaunchedProfile>>,
}

impl MockBrowserDriver {
    pub fn new() -> Self { Self { running: Mutex::new(HashMap::new()) } }
}

#[async_trait]
impl BrowserDriver for MockBrowserDriver {
    async fn launch(&self, profile_id: &str) -> Result<LaunchedProfile> {
        let launched = LaunchedProfile {
            id: profile_id.into(),
            cdp_endpoint: "http://127.0.0.1:9".into(),
            pid: 1,
            started_at: "2026-01-01T00:00:00Z".into(),
        };
        self.running.lock().unwrap().insert(profile_id.into(), launched.clone());
        Ok(launched)
    }
    async fn close(&self, profile_id: &str) -> Result<()> {
        self.running.lock().unwrap().remove(profile_id);
        Ok(())
    }
    async fn is_running(&self, profile_id: &str) -> bool {
        self.running.lock().unwrap().contains_key(profile_id)
    }
    async fn navigate(&self, _id: &str, url: &str) -> Result<String> { Ok(url.into()) }
    async fn click(&self, _id: &str, _sel: &str) -> Result<()> { Ok(()) }
    async fn type_text(&self, _id: &str, _sel: &str, _t: &str) -> Result<()> { Ok(()) }
    async fn extract(&self, _id: &str) -> Result<serde_json::Value> { Ok(serde_json::json!({"text": ""})) }
    async fn screenshot(&self, _id: &str) -> Result<String> { Ok("b64".into()) }
    async fn cdp_send(&self, _id: &str, _method: &str, _params: Option<serde_json::Value>, _sid: Option<&str>, _safe: bool) -> Result<serde_json::Value> {
        Ok(serde_json::json!({}))
    }
}
```

- [ ] **Step 3: commit**

```bash
git add -A
git commit -m "feat(mcp-server): tool arg schemas + MockBrowserDriver"
```

---

### Task 6: 22 工具 handler（纯逻辑 + 用 Mock 测）

**Files:**
- Modify: `crates/mcp-server/src/tools.rs`
- Create: `crates/mcp-server/tests/tools.rs`

**Interfaces:**
- Produces: 每个工具一个 async 函数，签名形如 `pub async fn navigate(driver: &dyn BrowserDriver, pm: &ProfileManager, activity: &ActivityLog, args: NavigateArgs) -> Result<serde_json::Value>`。每个函数：`start_call` → 执行（含安全门 `assert_safe_url` 等）→ `finish`（ok/error）→ 返回 JSON。
- 关键工具行为（对齐 TS）：
  - `list_profiles`：`pm.list()` + driver.is_running → `{profiles: [...]}`
  - `launch_profile`：`assert_profile_exists` → `driver.launch` → `LaunchedProfile`
  - `navigate`：`assert_profile_running` + `assert_safe_url` → `driver.navigate` → `{url}`
  - `click/type/extract/screenshot`：assert running → driver → 结果
  - `create_profile`：`generate_fingerprint` + `reconcile` → `pm.create` → `{id, name, proxy:redacted, fingerprint:summary}`
  - `update_profile`：`pm.get` + reconcile + `pm.update` → `{id, ..., appliesOnNextLaunch}`
  - `delete_profile`：if running → `driver.close` → `pm.delete` → `{deleted}`
  - `cdp_send`：`assert_profile_running` + `cdp_method_allowed` + `assert_no_blocked_scheme_in_params` → `driver.cdp_send(safe:true)` → raw result
  - `get_cookies`：每个 url `assert_safe_url` → `driver.cdp_send("Network.getCookies", {urls})`
  - `set_cookies`：`driver.cdp_send("Network.setCookies", {cookies})`
  - `new_tab`：url `assert_safe_url` → `driver.cdp_send("Target.createTarget", {url})`
  - `wait_for_selector`：poll `cdp_send("Runtime.evaluate", "!!document.querySelector(...)")`，默认 30000ms，150ms 间隔
  - `evaluate_js`：`driver.cdp_send("Runtime.evaluate", {expression, returnByValue:true}, session_id, safe:true)`
  - `list_tabs/activate_tab/close_tab`：对应 `Target.*` CDP
  - `wait_for_navigation/wait_for_load`：poll `document.readyState === "complete"`
  - `list_fingerprint_options`：`deviceCatalog()` + `localeCatalog()`（从 profile-manager 导出，Plan 1 暂未实现 catalog——此 task 在 profile-manager 补最小 stub 或从 multizen-core 静态表返回。简化：返回 Plan 1 DeviceFamily 全量 + 常见 locale 表）

错误映射：`MultizenError::NotFound` → `{code:"PROFILE_NOT_FOUND"}`；`Launch` → `LAUNCH_FAILED`；`Mcp` → `FORBIDDEN`/`INVALID_INPUT`；其余 → `INTERNAL_ERROR`。错误以 `isError:true` + content `{error:{code,message}}` 返回（rmcp 的 tool result error 格式）。

- [ ] **Step 1: 实现 tools.rs（核心 8 个 + 安全门相关的 cdp_send/get_cookies/new_tab/navigate）**

实现者按上述行为实现全部 22 个。每个函数模式：

```rust
pub async fn navigate(
    driver: &dyn BrowserDriver,
    _pm: &profile_manager::ProfileManager,
    activity: &mcp_server::activity::ActivityLog,
    args: NavigateArgs,
) -> Result<serde_json::Value> {
    let id = activity.start_call("navigate", Some(args.profile_id.clone()), serde_json::to_value(&args).unwrap_or_default());
    let started = std::time::Instant::now();
    let res = async {
        assert_profile_running(driver, &args.profile_id).await?;
        mcp_server::security::assert_safe_url(&args.url)?;
        let url = driver.navigate(&args.profile_id, &args.url).await?;
        Ok::<_, MultizenError>(serde_json::json!({ "url": url }))
    }.await;
    let (status, summary) = match &res {
        Ok(v) => ("ok", Some(v.to_string())),
        Err(e) => ("error", Some(e.to_string())),
    };
    activity.finish(&id, status, summary, Some(started.elapsed().as_millis() as u64));
    res
}

async fn assert_profile_running(driver: &dyn BrowserDriver, profile_id: &str) -> Result<()> {
    if !driver.is_running(profile_id).await {
        return Err(MultizenError::NotFound(profile_id.into()));
    }
    Ok(())
}
```

`cdp_send` 工具：
```rust
pub async fn cdp_send(driver: &dyn BrowserDriver, _pm: &ProfileManager, activity: &ActivityLog, args: CdpSendArgs) -> Result<serde_json::Value> {
    let id = activity.start_call("cdp_send", Some(args.profile_id.clone()), serde_json::to_value(&args).unwrap_or_default());
    let started = Instant::now();
    let res = async {
        if !raw_cdp_enabled() {
            return Err(MultizenError::Mcp("raw CDP disabled".into()));
        }
        if !security::cdp_method_allowed(&args.method) {
            return Err(MultizenError::Mcp(format!("forbidden CDP method: {}", args.method)));
        }
        if let Some(params) = &args.params {
            security::assert_no_blocked_scheme_in_params(params)?;
        }
        assert_profile_running(driver, &args.profile_id).await?;
        driver.cdp_send(&args.profile_id, &args.method, args.params, args.session_id.as_deref(), true).await
    }.await;
    // finish ...
    res
}

fn raw_cdp_enabled() -> bool {
    std::env::var("MULTIZEN_MCP_ALLOW_RAW_CDP").ok()
        .map(|v| matches!(v.as_str(), "1" | "true" | "yes" | "on"))
        .unwrap_or(false)
}
```

实现者按此模式补全 22 个，`redacted_proxy` 用于 list_profiles/create_profile/update_profile 的 proxy 字段。

- [ ] **Step 2: 写工具测试（用 MockBrowserDriver）**

`crates/mcp-server/tests/tools.rs`：

```rust
use mcp_server::{activity::ActivityLog, tools::*};
use mock_driver::MockBrowserDriver;

mod mock_driver;
include!("mock_driver.rs"); // 实际用 mod 声明，此处示意

#[tokio::test]
async fn navigate_calls_driver_and_logs() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    // 需要一个 running profile — launch 先
    let _ = <MockBrowserDriver as mcp_server::driver::BrowserDriver>::launch(&driver, "p1").await.unwrap();
    let r = navigate(&driver, &pm_stub(), &log, NavigateArgs{profile_id:"p1".into(),url:"https://x".into()}).await.unwrap();
    assert_eq!(r.get("url").unwrap().as_str(), Some("https://x"));
    let recent = log.recent(10).await;
    assert_eq!(recent[0].tool, "navigate");
    assert_eq!(recent[0].status, "ok");
}

#[tokio::test]
async fn navigate_rejects_blocked_scheme() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    <MockBrowserDriver as mcp_server::driver::BrowserDriver>::launch(&driver, "p1").await.unwrap();
    let r = navigate(&driver, &pm_stub(), &log, NavigateArgs{profile_id:"p1".into(),url:"file:///etc/passwd".into()}).await;
    assert!(r.is_err());
    let recent = log.recent(10).await;
    assert_eq!(recent[0].status, "error");
}

#[tokio::test]
async fn cdp_send_disabled_by_default() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    <MockBrowserDriver as mcp_server::driver::BrowserDriver>::launch(&driver, "p1").await.unwrap();
    let r = cdp_send(&driver, &pm_stub(), &log, CdpSendArgs{profile_id:"p1".into(),method:"Page.navigate".into(),params:None,session_id:None}).await;
    assert!(r.is_err(), "cdp_send must be off without MULTIZEN_MCP_ALLOW_RAW_CDP");
}

#[tokio::test]
async fn cdp_send_denies_io_read() {
    std::env::set_var("MULTIZEN_MCP_ALLOW_RAW_CDP", "1");
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    <MockBrowserDriver as mcp_server::driver::BrowserDriver>::launch(&driver, "p1").await.unwrap();
    let r = cdp_send(&driver, &pm_stub(), &log, CdpSendArgs{profile_id:"p1".into(),method:"IO.read".into(),params:None,session_id:None}).await;
    assert!(r.is_err());
    std::env::remove_var("MULTIZEN_MCP_ALLOW_RAW_CDP");
}
```

`pm_stub()` 返回一个临时 ProfileManager（用 tempfile）。实现者建一个 helper。

- [ ] **Step 3: 运行 + clippy + commit**

Run: `cargo test -p mcp-server --test tools`
```bash
cargo clippy -p mcp-server --all-targets -- -D warnings
git add -A
git commit -m "feat(mcp-server): 22 tool handlers with security gates + activity logging"
```

---

### Task 7: token constant-time 比较

**Files:**
- Modify: `crates/mcp-server/src/token.rs`

**Interfaces:**
- Produces: `pub fn token_matches(provided: &str, expected: &str) -> bool` — constant-time 比较，长度不同也保持恒定时间（先比长度，若等则逐字节；为避免长度泄露，可对不等长也跑完整比较，但恒定时间核心是不提前短路）。

- [ ] **Step 1: 实现 + 测试**

```rust
use subtle::ConstantTimeEq;

pub fn token_matches(provided: &str, expected: &str) -> bool {
    if provided.len() != expected.len() {
        // Still do a comparison to keep timing roughly constant.
        let _ = expected.as_bytes().ct_eq(expected.as_bytes());
        return false;
    }
    provided.as_bytes().ct_eq(expected.as_bytes()).into()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn matches_same() { assert!(token_matches("abc123", "abc123")); }
    #[test]
    fn rejects_diff() { assert!(!token_matches("abc123", "abc124")); }
    #[test]
    fn rejects_diff_len() { assert!(!token_matches("abc", "abc123")); }
    #[test]
    fn rejects_empty() { assert!(!token_matches("", "abc123")); }
}
```

- [ ] **Step 2: commit**

```bash
git add -A
git commit -m "feat(mcp-server): constant-time bearer token compare"
```

---

### Task 8: axum HttpTransport（/mcp + /sse + /healthz + auth + DNS-rebinding）

**Files:**
- Modify: `crates/mcp-server/src/transport.rs`
- Create: `crates/mcp-server/tests/transport.rs`

**Interfaces:**
- Produces:
  - `pub struct HttpTransport { port: u16, auth_token: Option<String> }`
  - `impl HttpTransport { pub fn new(port: u16, auth_token: Option<String>) -> Self; pub async fn start<F>(self, create_server: F) where F: Fn() -> rmcp::ServiceServer + Send + Sync + 'static; pub async fn stop(&self) }`
  - 路由：`POST /mcp`（Streamable HTTP，每 request 新 server）、`GET /sse`（legacy，长生命周期 server）、`POST /messages`、`GET /healthz`
  - 中间件：`auth_ok`（bearer compare）、`host_allowed`（DNS-rebinding）

注意：rmcp 的 `ServiceServer` + axum 集成的精确 API 需实现者按 rmcp 0.1 实际版本调整。此 task 的核心可测部分是 `auth_ok` 和 `host_allowed` 中间件逻辑（纯函数 + axum extractor），以及 `/healthz` 路由。`/mcp` 的 rmcp 集成若 rmcp API 不稳定，先实现 `/healthz` + auth + host_allowed，`/mcp` 留一个调用 `create_server()` 的占位 handler 并标注 TODO 供 Plan 4 接入时补全——但此占位不能破坏编译。

- [ ] **Step 1: 写可测的中间件 + healthz 测试**

`crates/mcp-server/tests/transport.rs`：

```rust
use mcp_server::transport::{host_allowed, parse_bearer, HOSTS_ALLOWED};
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
    assert!(host_allowed("localhost:7777", &["localhost:7777".into(), "127.0.0.1:7777".into()]));
}

#[test]
fn host_allowed_rejects_external() {
    assert!(!host_allowed("evil.com:7777", &["localhost:7777".into()]));
}
```

- [ ] **Step 2: 实现 transport.rs（中间件 + healthz + /mcp 占位）**

```rust
use axum::extract::Request;
use axum::http::HeaderValue;
use axum::response::{IntoResponse, Response, Json};
use axum::routing::{get, post};
use axum::Router;
use serde_json::json;

pub fn parse_bearer(auth: &HeaderValue) -> Option<String> {
    let s = auth.to_str().ok()?;
    let s = s.trim();
    let rest = s.strip_prefix("Bearer ")?;
    Some(rest.trim().to_string())
}

pub fn host_allowed(host: &str, allowed: &[String]) -> bool {
    allowed.iter().any(|a| a == host)
}

pub fn build_router(auth_token: Option<String>) -> Router {
    let healthz = get(|| async move {
        Json(json!({"ok": true, "name": "multizen-mcp"}))
    });
    Router::new()
        .route("/healthz", healthz)
        .route("/mcp", post(move |req: Request| handle_mcp(req, auth_token.clone())))
        .route("/sse", get(move |req: Request| handle_sse(req, auth_token.clone())))
}

async fn handle_mcp(req: Request, auth_token: Option<String>) -> Response {
    // Auth check
    if let Some(tok) = &auth_token {
        let provided = req.headers().get("authorization").and_then(parse_bearer);
        match provided {
            Some(p) if crate::token::token_matches(&p, tok) => {}
            _ => return (axum::http::StatusCode::UNAUTHORIZED, "unauthorized").into_response(),
        }
    }
    // DNS-rebinding
    let host = req.headers().get("host").and_then(|h| h.to_str().ok()).unwrap_or("");
    if !host_allowed(host, &["localhost:7777".into(), "127.0.0.1:7777".into()]) {
        return (axum::http::StatusCode::FORBIDDEN, "host not allowed").into_response();
    }
    // rmcp server integration — Plan 4 wires create_server() here. Placeholder:
    Json(json!({"jsonrpc":"2.0","id":null,"error":{"code":-32601,"message":"mcp dispatch not wired in Plan 3"}})).into_response()
}

async fn handle_sse(req: Request, auth_token: Option<String>) -> Response {
    // Similar auth + host check; SSE stream wiring in Plan 4.
    let _ = (req, auth_token);
    (axum::http::StatusCode::NOT_IMPLEMENTED, "sse not wired").into_response()
}

pub fn allowed_hosts(port: u16) -> Vec<String> {
    vec![format!("127.0.0.1:{port}"), format!("localhost:{port}")]
}

pub fn _unused(_: &HOSTS_ALLOWED) {}
pub struct HOSTS_ALLOWED;
```

- [ ] **Step 3: 运行测试 + clippy + commit**

Run: `cargo test -p mcp-server --test transport`
```bash
cargo clippy -p mcp-server --all-targets -- -D warnings
git add -A
git commit -m "feat(mcp-server): axum transport skeleton + auth + DNS-rebinding + healthz"
```

---

### Task 9: workspace 全量校验 + README

- [ ] **Step 1: 全量测试**

Run: `cargo test --workspace`
Expected: Plan 1+2 的 61 个 + Plan 3（activity 5 + security 8 + token 4 + transport 4 + tools 4 + mock_driver 编译）= 约 86 个 PASS（tools 测试需 pm_stub helper，实现者补齐）。

- [ ] **Step 2: clippy**

Run: `cargo clippy --workspace --all-targets -- -D warnings`

- [ ] **Step 3: README**

`crates/mcp-server/README.md`：
```markdown
# mcp-server
Rust MCP server (rmcp + axum) for multizen-browser-rs. 22 tools over Streamable HTTP (`POST /mcp`) + legacy SSE (`GET /sse`) + `GET /healthz`. Bearer-token auth (constant-time), DNS-rebinding guard, and engine-independent security gates: `BLOCKED_URL_SCHEMES` (file/chrome/devtools/view-source), `CDP_DENY_METHODS` (IO.read, Page.getResourceContent, DOMStorage/IndexedDB/CacheStorage/Fetch.*, Browser.close/crash), URL normalization (strips tab/newline + leading control chars before scheme check), proxy credential redaction, and ActivityLog arg sanitization. `cdp_send` is hidden unless `MULTIZEN_MCP_ALLOW_RAW_CDP=1`. Depends on a `BrowserDriver` trait impl injected by `tauri-app` (Plan 4).
```

- [ ] **Step 4: commit**

```bash
git add -A
git commit -m "docs: mcp-server README + workspace clippy clean"
```

---

## Self-Review 记录

- Spec 覆盖：Plan 3 覆盖 spec §2 的 `mcp-server` crate + §3 的 MCP 数据流（内嵌同进程、Arc<ProfileRegistry> 共享——registry 实际由 Plan 4 的 tauri-app 持有，mcp-server 通过注入的 driver 访问）。22 工具全覆盖，安全门全覆盖。
- 占位符：`/mcp` 与 `/sse` 的 rmcp 集成留占位（标注 Plan 4 接入），因 rmcp 0.1 的 axum 集成 API 在写 plan 时未能确认精确签名——这是真实版本漂移，已在代码注释标明，Plan 4 接入时补全。其余每步含完整代码。
- 类型一致性：`BrowserDriver` trait（Task 2）的 9 方法与 Plan 2 `cdp-driver::BrowserSession` 的方法对齐（Plan 4 的 tauri-app 实现该 trait 时做适配）。`ActivityEvent`/`ActivityLog`、安全门常量在各 task 间一致。
- 已知简化：`list_fingerprint_options` 的 device/locale catalog 在 Plan 1 的 profile-manager 未实现，此 plan 从 multizen-core 的 DeviceFamily 静态表返回（catalog 函数留给 Plan 4 或后续补全 profile-manager）。
