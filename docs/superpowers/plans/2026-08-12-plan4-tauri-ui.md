# MultiZen Rust 重写 — Plan 4：tauri-app + React UI 迁移

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `tauri-app` crate（Tauri 壳：main.rs + Tauri commands 胶水 + 内嵌 mcp-server + `ProfileRegistry` 单例 + `BrowserDriver` 实现），并把现有 React 19 + Tailwind v4 前端迁移到 Tauri 系统 WebView（用 `invoke` 替换 `window.multizen.*` IPC，用 Tauri event listener 替换 push channel）。这是对 TS `apps/desktop/src/main/index.ts` + `preload/index.ts` + `renderer/src/**` 的移植。

**Architecture:** `tauri-app` 是集成层：持 `Arc<ProfileManager>`、`Arc<BrowserLauncher>`、`Arc<cdp-driver BrowserSession 池>`、`Arc<ActivityLog>`、`SettingsStore`、内嵌 `mcp-server` axum（同进程 tokio runtime）。Tauri commands 暴露给 UI（1:1 映射现有 `ipcMain.handle` 通道）。`BrowserDriver` trait 由一个 `TauriBrowserDriver` 实现（组合 launcher + cdp-driver session 池）。前端从 `apps/desktop/src/renderer/src/**` 迁移，替换 IPC 层。

**Tech Stack:** Rust 1.80+、`tauri` 2.x、`tokio`、`serde`；前端 React 19 + Tailwind v4 + Vite（从现有 renderer 迁移，`tauri.conf.json` 指向前端 build 输出）。

## Global Constraints

- 仓库：`multizen-browser-rs/`（当前 HEAD = Plan 3 末尾）。新增 `crates/tauri-app` + `ui/` 目录。
- Rust edition 2021。serde camelCase 与前 3 个 plan 一致。
- Tauri commands 通道名与现有 `ipcMain.handle` 1:1（保留冒号前缀：`profiles:list`、`settings:get` 等），降低 React 迁移面。
- push channel 用 Tauri event（`app.emit("profiles:running-changed", payload)`），前端 `listen`。
- `ProfileRegistry` 单例：`Arc<Mutex<HashMap<ProfileId, BrowserSession>>>`，UI 与 MCP 共享同一 CDP session（spec §3）。
- 内嵌 MCP：`tauri-app` 启动时在 tokio runtime 里 spawn `mcp-server` 的 axum（端口 = settings.mcpHttpPort，token = mcp-token 文件）。
- Tauri 系统 WebView（WebView2/WKWebView/WebKitGTK）渲染 UI——这是管理面板，**不是**被管理的浏览器窗口。被管理窗口仍是 CloakBrowser 进程（Plan 2 spawn）。
- 前端：保留 React 19 + Tailwind v4，删 Electron 特定代码（`window.multizen`、IPC import），改用 `@tauri-apps/api` 的 `invoke` + `listen`。
- 每任务 commit，`cargo clippy --workspace --all-targets -- -D warnings` 干净。

## File Structure

```
crates/tauri-app/
├── Cargo.toml
├── tauri.conf.json
├── src/
│   ├── main.rs               # tauri::Builder 入口，注册 commands + 启动 mcp + 生命周期
│   ├── registry.rs           # ProfileRegistry（Arc<Mutex<HashMap>>）+ Arc 共享
│   ├── driver.rs             # TauriBrowserDriver: impl BrowserDriver（组合 launcher + cdp-driver session 池）
│   ├── commands/
│   │   ├── mod.rs
│   │   ├── profiles.rs       # profiles:list/get/create/update/delete/launch/close/export/import
│   │   ├── settings.rs       # settings:get/update, dialog:pickBrowserBinary/pickDirectory
│   │   ├── fingerprint.rs    # fingerprint:generate/devices/locales/reconcile/localeForCountry
│   │   ├── proxy.rs          # proxy:detectGeo
│   │   ├── system.rs         # system:info（mcpHttpUrl, mcpAuthToken, appVersion, platform）
│   │   ├── activity.rs       # activity:recent
│   │   ├── chromium.rs       # chromium:status/retry（launcher 状态）
│   │   ├── extensions.rs     # extensions:*（暂桩，扩展管理留后续）
│   │   └── update.rs          # update:*（暂桩，Tauri updater 或留空）
│   ├── mcp_embed.rs          # 启动内嵌 mcp-server axum + token 文件管理
│   └── token.rs              # mcp-token 文件 64-hex 0600
└── tests/
    └── driver.rs             # TauriBrowserDriver 单元（用 temp ProfileManager + mock 二进制路径）

ui/                           # 从 apps/desktop/src/renderer 迁移
├── package.json
├── vite.config.ts
├── tailwind.config.ts
├── index.html
└── src/
    ├── main.tsx
    ├── App.tsx               # section 状态机（profiles/mcp/settings），无 router
    ├── lib/
    │   ├── ipc.ts            # invoke + listen 封装（替代 window.multizen）
    │   ├── cn.ts / emojiTint.ts / parseProxy.ts / persisted.ts / profileEmoji.ts / relativeTime.ts
    ├── types.ts              # MultizenApi 镜像 → ipc.ts 的类型
    └── components/            # 从 renderer/src/components 直接迁移（Constellation/Sheets/Settings/McpPanel/LeftRail 等）
```

职责边界：
- `tauri-app`：集成层，不实现新业务逻辑——profile/CDP/mcp 逻辑全在 Plan 1-3 crate。它只做：注册 commands、维护 `ProfileRegistry` 单例、impl `BrowserDriver`、启动 mcp、生命周期。
- `ui/`：纯前端，与 Rust 交互只经 `lib/ipc.ts`。

---

### Task 1: tauri-app crate 骨架 + tauri.conf.json

**Files:**
- Modify: `Cargo.toml`（workspace 加 tauri-app）
- Create: `crates/tauri-app/Cargo.toml`, `src/main.rs`, `tauri.conf.json`
- Create: `ui/` 初始（package.json, vite.config.ts, index.html, src/main.tsx 占位）

**Interfaces:**
- Produces: `cargo build -p tauri-app` 编译通过，`tauri dev` 能启动空窗口。

- [ ] **Step 1: workspace members 加 tauri-app**

- [ ] **Step 2: Cargo.toml**

```toml
[package]
name = "tauri-app"
version = "0.1.0"
edition = "2021"

[dependencies]
multizen-core = { path = "../multizen-core" }
profile-manager = { path = "../profile-manager" }
settings-store = { path = "../settings-store" }
browser-launcher = { path = "../browser-launcher" }
cdp-driver = { path = "../cdp-driver" }
mcp-server = { path = "../mcp-server" }
behavioral = { path = "../behavioral" }
tauri = { version = "2", features = ["protocol-asset"] }
tokio = { version = "1", features = ["rt-multi-thread", "macros"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
async-trait = "0.1"
uuid = { version = "1", features = ["v4"] }
chrono = { version = "0.4", features = ["serde"] }
tracing = "0.1"

[build-dependencies]
tauri-build = { version = "2" }
```

- [ ] **Step 3: main.rs 最小**

```rust
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
fn main() {
    tauri::Builder::default()
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
```

- [ ] **Step 4: tauri.conf.json**

```json
{
  "build": {
    "beforeDevCommand": "cd ui && pnpm dev",
    "devUrl": "http://localhost:5173",
    "beforeBuildCommand": "cd ui && pnpm build",
    "frontendDist": "../ui/dist"
  },
  "app": {
    "windows": [{ "title": "MultiZen", "width": 1200, "height": 800 }]
  }
}
```

- [ ] **Step 5: ui/ 初始**

`ui/package.json`：react 19 + tailwind v4 + vite + `@tauri-apps/api`。
`ui/src/main.tsx`：渲染 `<App/>` 占位。

- [ ] **Step 6: 验证 + commit**

Run: `cargo check -p tauri-app`
```bash
git add -A
git commit -m "chore: scaffold tauri-app crate + ui dir"
```

---

### Task 2: ProfileRegistry + TauriBrowserDriver

**Files:**
- Modify: `crates/tauri-app/src/registry.rs`, `src/driver.rs`

**Interfaces:**
- Produces:
  - `pub struct ProfileRegistry { sessions: Arc<Mutex<HashMap<String, cdp_driver::session::BrowserSession>>> }`
  - `impl ProfileRegistry { pub fn new() -> Self; pub async fn get_or_connect(&self, profile_id: &str, endpoint: &str, engine: BrowserEngine) -> Result<...>; pub async fn remove(&self, profile_id: &str) }`
  - `pub struct TauriBrowserDriver { launcher: Arc<BrowserLauncher>, registry: Arc<ProfileRegistry> }`
  - `impl BrowserDriver for TauriBrowserDriver`：launch → launcher.launch → registry.get_or_connect；navigate/click/type/extract/screenshot/evaluate/cdp_send → 取 session 调对应方法；close → launcher.close + registry.remove。

- [ ] **Step 1: 实现 registry.rs + driver.rs**

```rust
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;
use multizen_core::{BrowserEngine, Result};
use cdp_driver::session::BrowserSession;

pub struct ProfileRegistry {
    sessions: Arc<Mutex<HashMap<String, Arc<BrowserSession>>>>,
}

impl ProfileRegistry {
    pub fn new() -> Self {
        Self { sessions: Arc::new(Mutex::new(HashMap::new())) }
    }
    pub async fn get_or_connect(&self, profile_id: &str, endpoint: &str, engine: BrowserEngine) -> Result<Arc<BrowserSession>> {
        if let Some(s) = self.sessions.lock().await.get(profile_id) {
            return Ok(s.clone());
        }
        let session = Arc::new(BrowserSession::connect(endpoint, engine).await?);
        self.sessions.lock().await.insert(profile_id.into(), session.clone());
        Ok(session)
    }
    pub async fn remove(&self, profile_id: &str) {
        self.sessions.lock().await.remove(profile_id);
    }
}
```

`driver.rs`：impl `BrowserDriver`，每个方法取 `registry.get_or_connect` 后调 `BrowserSession` 方法。`launch`：`launcher.launch(...)` → `registry.get_or_connect(launched.cdp_endpoint)` → 返回 `LaunchedProfile`。`close`：`registry.remove` → `launcher.close`。

- [ ] **Step 2: 验证编译 + commit**

```bash
git add -A
git commit -m "feat(tauri-app): ProfileRegistry + TauriBrowserDriver impl"
```

---

### Task 3: Tauri commands（profiles + settings + system + fingerprint + proxy + activity）

**Files:**
- Modify: `crates/tauri-app/src/commands/*.rs`

**Interfaces:** 1:1 映射现有 `ipcMain.handle`。每个 command 用 `#[tauri::command]`，state 用 `tauri::State<'_, AppState>` 拿 `Arc<ProfileManager>` 等。

- [ ] **Step 1: AppState + 注册**

`main.rs`：
```rust
pub struct AppState {
    pub pm: Arc<profile_manager::ProfileManager>,
    pub settings: tokio::sync::Mutex<settings_store::SettingsStore>,
    pub driver: Arc<TauriBrowserDriver>,
    pub activity: Arc<mcp_server::activity::ActivityLog>,
    pub mcp_token: tokio::sync::Mutex<Option<String>>,
}

fn main() {
    // init AppState（paths 从 tauri path API 拿）
    tauri::Builder::default()
        .manage(AppState { ... })
        .invoke_handler(tauri::generate_handler![
            commands::profiles::list, commands::profiles::get, commands::profiles::create,
            commands::profiles::update, commands::profiles::delete, commands::profiles::launch,
            commands::profiles::close,
            commands::settings::get, commands::settings::update,
            commands::fingerprint::generate, commands::fingerprint::devices,
            commands::fingerprint::locales, commands::fingerprint::reconcile,
            commands::fingerprint::locale_for_country,
            commands::proxy::detect_geo,
            commands::system::info,
            commands::activity::recent,
        ])
        .run(tauri::generate_context!())
        .expect("error");
}
```

- [ ] **Step 2: 各 command 实现**

`commands/profiles.rs` 例：
```rust
#[tauri::command]
pub async fn list(state: tauri::State<'_, crate::AppState>) -> Result<Vec<multizen_core::ProfileSummary>, String> {
    let mut summaries = state.pm.list().map_err(|e| e.to_string())?;
    for s in &mut summaries {
        s.is_running = state.driver.is_running(&s.id).await;
    }
    Ok(summaries)
}

#[tauri::command]
pub async fn launch(state: tauri::State<'_, crate::AppState>, id: String) -> Result<multizen_core::LaunchedProfile, String> {
    state.driver.launch(&id).await.map_err(|e| e.to_string())
}
```

其余 command 同理。`system::info` 返回 `{mcpHttpUrl, mcpAuthToken, appVersion, platform}`。

- [ ] **Step 3: commit**

```bash
git add -A
git commit -m "feat(tauri-app): Tauri commands (profiles/settings/fingerprint/proxy/system/activity)"
```

---

### Task 4: mcp-token 文件 + 内嵌 MCP server 启动

**Files:**
- Modify: `crates/tauri-app/src/token.rs`, `src/mcp_embed.rs`, `src/main.rs`

**Interfaces:**
- `load_or_create_mcp_token(data_dir: &Path) -> Result<String>` — 64-hex，写 `mcp-token` 文件 0600（Windows 尽力），正则校验。
- `start_embedded_mcp(port: u16, token: String, pm: Arc<ProfileManager>, driver: Arc<TauriBrowserDriver>, activity: Arc<ActivityLog>)` — spawn tokio task 跑 `mcp_server::transport` axum，`create_server` 闭包每次建 rmcp server 注入 driver。

- [ ] **Step 1: token.rs**

```rust
use std::path::Path;
use multizen_core::{MultizenError, Result};

pub fn load_or_create_mcp_token(data_dir: &Path) -> Result<String> {
    let path = data_dir.join("mcp-token");
    if let Ok(existing) = std::fs::read_to_string(&path) {
        let trimmed = existing.trim();
        if trimmed.len() == 64 && trimmed.chars().all(|c| c.is_ascii_hexdigit()) {
            return Ok(trimmed.to_string());
        }
    }
    let token: String = (0..64).map(|_| {
        let n: u32 = rand_u32() % 16;
        std::char::from_digit(n, 16).unwrap()
    }).collect();
    std::fs::write(&path, &token)?;
    Ok(token)
}

fn rand_u32() -> u32 {
    // Use std random via /dev/urandom or OS. Simplified: uuid-based entropy.
    uuid::Uuid::new_v4().as_u128() as u32
}
```

- [ ] **Step 2: mcp_embed.rs + main.rs 集成**

main.rs 在 setup hook 里：`load_or_create_mcp_token` → 若 `settings.mcpHttpEnabled` → `start_embedded_mcp`。

- [ ] **Step 3: commit**

```bash
git add -A
git commit -m "feat(tauri-app): mcp-token management + embedded MCP server startup"
```

---

### Task 5: push events（running-changed / proxy-country-updated / activity:event / chromium:status）

**Files:**
- Modify: `crates/tauri-app/src/main.rs`, `src/driver.rs`

**Interfaces:**
- `BrowserLauncher` 的事件 → Tauri `app.emit("profiles:running-changed", payload)`。
- `ActivityLog` 的 broadcast → `app.emit("activity:event", event)`。
- launcher 状态变化 → `app.emit("chromium:status", ...)`。

- [ ] **Step 1: 事件桥接**

main.rs setup 里 spawn task：subscribe `activity_log` broadcast → `app.emit`。driver 在 launch/close 后 emit `profiles:running-changed`。

- [ ] **Step 2: commit**

```bash
git add -A
git commit -m "feat(tauri-app): bridge push events to Tauri emit"
```

---

### Task 6: 前端迁移 — IPC 层（lib/ipc.ts）

**Files:**
- Create: `ui/src/lib/ipc.ts`, `ui/src/types.ts`

**Interfaces:**
- `ipc.ts` 导出 `profiles`/`settings`/`fingerprint`/`proxy`/`system`/`activity` 命名空间，每个方法调 `invoke("channel", args)`。
- push 监听封装：`onRunningChanged(cb)` → `listen("profiles:running-changed", e => cb(e.payload))`，返回 unsub。

- [ ] **Step 1: ipc.ts**

```typescript
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

export const profiles = {
  list: () => invoke<ProfileSummary[]>("profiles:list"),
  get: (id: string) => invoke<Profile | null>("profiles:get", { id }),
  create: (input: CreateProfileInput) => invoke<Profile>("profiles:create", { input }),
  update: (id: string, patch: UpdateProfileInput) => invoke<Profile>("profiles:update", { id, patch }),
  delete: (id: string) => invoke<void>("profiles:delete", { id }),
  launch: (id: string) => invoke<LaunchedProfile>("profiles:launch", { id }),
  close: (id: string) => invoke<void>("profiles:close", { id }),
};
export const onRunningChanged = (cb: (p: RunningStateChange) => void): Promise<UnlistenFn> =>
  listen<RunningStateChange>("profiles:running-changed", e => cb(e.payload));
// ... 其余命名空间同理
```

- [ ] **Step 2: commit**

```bash
git add -A
git commit -m "feat(ui): IPC layer via tauri invoke + listen"
```

---

### Task 7: 前端迁移 — 组件搬迁

**Files:** 复制 `apps/desktop/src/renderer/src/{App.tsx, components/**, lib/**, types.ts, styles.css}` 到 `ui/src/`，全局替换 `window.multizen.X.Y(...)` → `ipc.X.Y(...)`，替换 `window.multizen.onX(cb)` → `onX(cb)`。

- [ ] **Step 1: 复制组件目录]

```bash
cp -r apps/desktop/src/renderer/src/components ui/src/
cp apps/desktop/src/renderer/src/App.tsx ui/src/
cp apps/desktop/src/renderer/src/styles.css ui/src/
cp apps/desktop/src/renderer/src/main.tsx ui/src/
```

- [ ] **Step 2: 全局替换 window.multizen → ipc import]

每个组件顶部 `import { profiles, settings, ... } from "@/lib/ipc"`，调用替换。用 sed/脚本批处理 + 手工校验。

- [ ] **Step 3: 删 Electron 特定代码]

删 `electron` import、`contextBridge`、`ipcRenderer` 引用。

- [ ] **Step 4: vite + tailwind 配置 + pnpm build 通过]

Run: `cd ui && pnpm install && pnpm build`
Expected: 前端 build 成功。

- [ ] **Step 5: commit**

```bash
git add -A
git commit -m "feat(ui): migrate renderer components to tauri invoke + listen"
```

---

### Task 8: 端到端启动 + 全量校验

- [ ] **Step 1: tauri dev 启动]

Run: `cargo tauri dev`
Expected: Tauri 窗口启动，UI 渲染，profiles:list 工作（连 temp ProfileManager）。

- [ ] **Step 2: cargo test --workspace + clippy]

Run: `cargo test --workspace && cargo clippy --workspace --all-targets -- -D warnings`

- [ ] **Step 3: READMEs]

`crates/tauri-app/README.md` + `ui/README.md`。

- [ ] **Step 4: commit]

```bash
git add -A
git commit -m "docs: tauri-app + ui READMEs, full workspace verified"
```

---

## Self-Review 记录

- Spec 覆盖：Plan 4 覆盖 spec §2 的 `tauri-app` crate + §3 的 `ProfileRegistry` 单例、内嵌 MCP、Tauri commands、push events；§4 的构建/发布（tauri dev + bundler）。前端迁移覆盖 spec §1 的 React 保留。
- 占位符：`extensions:*` 与 `update:*` commands 留桩（标注后续），因为扩展管理与 Tauri updater 集成超出 MVP 范围，spec §MVP 未把它们列为必须。Plan 1-3 无前向依赖这两个。
- 类型一致性：`AppState` 字段类型与 Plan 1-3 的 `Arc<ProfileManager>` / `Arc<BrowserLauncher>` / `Arc<ActivityLog>` 一致。`TauriBrowserDriver` impl `mcp_server::driver::BrowserDriver`（Plan 3 Task 2 定义）。Tauri command 通道名与 React `invoke` 字符串 1:1。
- 已知简化：扩展管理（extensions:*）与自动更新（update:*）留桩；`browser-launcher` 的二进制下载/manifest（Plan 2 已不含，归 CloakBrowser 分发）；`/mcp` rmcp 集成在 Plan 3 留占位，Plan 4 接入时补全 `create_server` 闭包。

## 完成态

Plan 1-4 完成后，新仓库 `multizen-browser-rs` 实现了 spec 全部 MVP：Tauri 壳 + Rust 全栈后端 + React UI 迁移 + CloakBrowser CDP 驱动 + 22 工具 MCP server + behavioral injection。后续可在新仓库继续迭代 extensions/update/cloud sync 等 roadmap 项。
