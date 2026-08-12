# Implement tauri-app crate + React UI migration (Plan 4)

## Goal

实现 `tauri-app` crate（Tauri 2.x 壳：main.rs + Tauri commands 胶水 + 内嵌 mcp-server + `ProfileRegistry` 单例 + `TauriBrowserDriver` 实现 `BrowserDriver` trait），并把现有 React 19 + Tailwind v4 前端从 Electron 迁移到 Tauri 系统 WebView（`invoke` 替换 `window.multizen.*` IPC，Tauri event 替换 push channel）。这是 Electron→Tauri + 全 Rust 后端重写的最后一个 Plan，收尾整个重写。

## Background

- Plan 1（foundation）+ Plan 2（browser layer）+ Plan 3（mcp-server）已完成并合入 master（HEAD=0c04346，106/106 测试通过，clippy clean）。
- 本 crate 依赖全部前 3 个 plan 的 crate：`multizen-core` + `profile-manager` + `settings-store` + `browser-launcher` + `cdp-driver` + `mcp-server`。
- 详细逐任务实现计划在 `docs/superpowers/plans/2026-08-12-plan4-tauri-ui.md`（SDD plan，8 任务）。本 PRD 提炼需求与验收；技术设计见 `design.md`；执行顺序见 `implement.md`。
- Plan 3 final review 的 1 Important + 6 Minor 在本 Plan 顺带处理（见 Risks/Deferred）。

## Requirements

### R1: tauri-app crate 骨架
- 新增 `crates/tauri-app`，加入 workspace。
- `Cargo.toml` 含 `tauri` 2.x + `tokio` + `serde` + 依赖前 3 plan 的 crate。
- `tauri.conf.json` 指向前端 build 输出（`ui/dist`）。
- `main.rs` tauri::Builder 入口，注册 commands + 启动内嵌 mcp + 生命周期。

### R2: ProfileRegistry + TauriBrowserDriver
- `ProfileRegistry`：`Arc<Mutex<HashMap<ProfileId, BrowserSession>>>` 单例，UI 与 MCP 共享同一 CDP session（spec §3）。
- `TauriBrowserDriver`：实现 `mcp_server::BrowserDriver` trait，组合 `BrowserLauncher`（Plan 2）+ cdp-driver session 池（launch → connect BrowserSession 存入 registry，close → 移除，navigate/click/... → 从 registry 取 session 调对应方法）。

### R3: Tauri commands（1:1 映射现有 ipcMain.handle）
- `profiles:list/get/create/update/delete/launch/close/export/import`
- `settings:get/update`、`dialog:pickBrowserBinary/pickDirectory`
- `fingerprint:generate/devices/locales/reconcile/localeForCountry`
- `proxy:detectGeo`
- `system:info`（mcpHttpUrl, mcpAuthToken, appVersion, platform）
- `activity:recent`
- 通道名保留冒号前缀（降低 React 迁移面）。

### R4: mcp-token 文件 + 内嵌 MCP server
- 启动时生成 64-hex mcp-token 写文件（`~/.multizen/mcp-token` 或 settings 指定路径）。
- 在 tokio runtime spawn `mcp-server` 的 axum（端口 = settings.mcpHttpPort，token = mcp-token 文件），同进程。
- `/mcp` 的 rmcp ServiceServer 集成在此接通（Plan 3 留的占位）。

### R5: push events（Tauri event 替换 push channel）
- `profiles:running-changed`、`proxy-country-updated`、`activity:event`、`chromium:status`。
- `app.emit(event, payload)`，前端 `listen`。

### R6: 前端迁移 — IPC 层（`ui/lib/ipc.ts`）
- 从 `apps/desktop/src/renderer/src/lib/ipc.ts` 迁移，替换 `window.multizen.*` 为 `@tauri-apps/api` 的 `invoke` + `listen`。
- 保留通道名 1:1，降低组件迁移面。

### R7: 前端迁移 — 组件搬迁
- 从 `apps/desktop/src/renderer/src/**` 迁移 React 19 + Tailwind v4 组件到 `ui/`。
- 删 Electron 特定代码（`window.multizen`、IPC import、electron 预加载依赖）。
- Vite build 输出到 `ui/dist`，`tauri.conf.json` 指向。

### R8: 端到端启动 + 全量校验
- `cargo test --workspace` 全 PASS。
- `cargo clippy --workspace --all-targets -- -D warnings` clean。
- `tauri-app` 能启动（至少 main.rs 编译 + Tauri builder 构建成功；真实窗口启动需手动验证）。

## Acceptance Criteria

- [ ] `crates/tauri-app` 存在，`cargo check -p tauri-app` 通过，workspace 编译。
- [ ] `ProfileRegistry` 单例 + `TauriBrowserDriver` 实现 `BrowserDriver` trait 全 9 方法。
- [ ] Tauri commands 全部注册（profiles/settings/fingerprint/proxy/system/activity），通道名保留冒号前缀。
- [ ] mcp-token 文件生成 + 内嵌 MCP axum spawn（端口/token 从 settings）。
- [ ] push events 4 个（running-changed/proxy-country-updated/activity:event/chromium:status）通过 `app.emit`。
- [ ] 前端 IPC 层迁移：`ui/lib/ipc.ts` 用 `invoke`/`listen`，通道名 1:1。
- [ ] 前端组件迁移：React 19 + Tailwind v4 组件迁到 `ui/`，无 Electron 特定代码，Vite build 到 `ui/dist`。
- [ ] `cargo test --workspace` 全 PASS，`cargo clippy --workspace --all-targets -- -D warnings` clean。
- [ ] `tauri-app` main.rs 编译 + Tauri builder 构建成功。

## Out of Scope

- 真实窗口启动的端到端手动测试（需桌面环境，标为手动验证项）。
- CloakBrowser 集成测试（仍是 Plan 2 的 ignored 测试，需真实浏览器）。
- 新 UI 功能（纯迁移，不加新特性）。
- Plan 3 的 rmcp `/mcp` 完整集成若 rmcp 0.1 API 仍不稳定，可继续留占位但需尝试接通。

## Key Decisions

- Tauri 系统 WebView 渲染管理面板（UI），被管理窗口仍是 CloakBrowser 进程（Plan 2 spawn）——不是 Tauri 窗口管理浏览器。
- 通道名保留冒号前缀 1:1 映射，降低 React 迁移面（只改 IPC 层，组件尽量不动）。
- `ProfileRegistry` 单例：UI commands 与 MCP server 共享同一 CDP session（spec §3 的核心约束）。
- 内嵌 MCP 同进程 tokio runtime，不另起进程。

## Risks / Deferred

- **Plan 3 final review 的 1 Important + 6 Minor**（见 progress.md 账本）在本 Plan 顺带处理：
  - IMPORTANT: 10 个内部 CDP 工具跳过 `cdp_method_allowed`——在本 plan 的 TauriBrowserDriver/commands 实现时，route through shared gated helper（或确认豁免并文档化）。
  - MINOR: transport port 硬编码 7777 → 本 plan Task 4 接通内嵌 MCP 时参数化。
  - MINOR: 其他 Minor（wait_for_selector JS 转义、error_json 映射、unused deps、env-var 测试竞态）视情况在 cleanup task 处理。
- Tauri 2.x API 漂移风险：写 SDD plan 时确认的 Tauri 2.x API 可能在实现时需调整。
- 前端迁移面大（apps/desktop/src/renderer/src/** 全量），Task 7 是最大单块。
