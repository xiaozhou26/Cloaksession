# Design — tauri-app crate + React UI migration (Plan 4)

## Architecture

`tauri-app` 是集成层：持 `Arc<ProfileManager>`、`Arc<BrowserLauncher>`、`Arc<cdp-driver BrowserSession 池>`（ProfileRegistry）、`Arc<ActivityLog>`、`SettingsStore`、内嵌 `mcp-server` axum（同进程 tokio runtime）。Tauri commands 暴露给 UI（1:1 映射现有 `ipcMain.handle` 通道，保留冒号前缀）。`BrowserDriver` trait（Plan 3 定义）由 `TauriBrowserDriver` 实现（组合 launcher + cdp-driver session 池）。前端从 `apps/desktop/src/renderer/src/**` 迁移，替换 IPC 层。

## File Structure

```
crates/tauri-app/
├── Cargo.toml
├── tauri.conf.json
├── src/
│   ├── main.rs               # tauri::Builder 入口，注册 commands + 启动 mcp + 生命周期
│   ├── registry.rs           # ProfileRegistry（Arc<Mutex<HashMap>>）+ Arc 共享
│   ├── driver.rs             # TauriBrowserDriver: impl BrowserDriver
│   ├── commands/
│   │   ├── mod.rs
│   │   ├── profiles.rs       # profiles:list/get/create/update/delete/launch/close/export/import
│   │   ├── settings.rs       # settings:get/update, dialog:pickBrowserBinary/pickDirectory
│   │   ├── fingerprint.rs    # fingerprint:generate/devices/locales/reconcile/localeForCountry
│   │   ├── proxy.rs          # proxy:detectGeo
│   │   ├── system.rs         # system:info
│   │   └── activity.rs       # activity:recent
│   └── mcp_embed.rs          # 内嵌 MCP server 启动 + mcp-token 文件
├── ui/                       # 前端（React 19 + Tailwind v4 + Vite）
│   ├── package.json
│   ├── vite.config.ts
│   ├── index.html
│   ├── src/
│   │   ├── main.tsx
│   │   ├── lib/ipc.ts        # invoke/listen 替换 window.multizen.*
│   │   └── components/        # 从 apps/desktop/src/renderer/src 迁移
│   └── dist/                 # Vite build 输出（tauri.conf.json 指向）
```

## Module Boundaries

- `registry.rs`：`ProfileRegistry` 单例 + Arc 共享状态（launcher + session 池 + activity + settings）。
- `driver.rs`：`TauriBrowserDriver` 实现 `mcp_server::BrowserDriver` trait，组合 `BrowserLauncher` + cdp-driver `BrowserSession`。
- `commands/`：Tauri commands，1:1 映射现有 ipcMain.handle 通道，调 ProfileManager/launcher/registry。
- `mcp_embed.rs`：生成 mcp-token 文件 + spawn mcp-server axum（同进程 tokio runtime）。
- `ui/`：前端，从 `apps/desktop/src/renderer/src` 迁移，删 Electron 特定代码，改 `@tauri-apps/api` 的 `invoke`/`listen`。

## Data Flow

Tauri 窗口（系统 WebView）渲染 UI → UI `invoke("profiles:list")` → Tauri command 调 `ProfileManager`/`launcher`/`registry` → 返回 JSON。push event：launcher/driver 状态变化 → `app.emit("profiles:running-changed", payload)` → UI `listen`。MCP 外部客户端 → `POST /mcp` → 内嵌 mcp-server axum → `TauriBrowserDriver`（同一 ProfileRegistry 单例）→ CDP session。

## Key Contracts

- `TauriBrowserDriver` 实现 `mcp_server::BrowserDriver` trait 全 9 方法（launch/close/is_running/navigate/click/type_text/extract/screenshot/cdp_send）。
- Tauri command 通道名 1:1 映射现有 ipcMain.handle（保留冒号前缀：`profiles:list` 等）。
- `ProfileRegistry`：UI commands 与 MCP server 共享同一 CDP session（spec §3 核心约束）。
- mcp-token：64-hex，写 `~/.multizen/mcp-token`（或 settings 指定）。

## Compatibility / Migration

- 前端从 `apps/desktop/src/renderer/src/**` 迁移，保留 React 19 + Tailwind v4，删 Electron 特定代码。
- 旧 TS `apps/desktop` + `packages/*` 在重写完成后废弃（本 plan 不删，后续清理）。
- Tauri 系统 WebView 渲染管理面板；被管理窗口仍是 CloakBrowser 进程（Plan 2 spawn），不是 Tauri 窗口。

## Trade-offs

- 通道名保留冒号前缀 1:1 映射：降低 React 迁移面（只改 IPC 层），代价是 Tauri command 名带冒号（需确认 Tauri 2.x 允许）。
- 内嵌 MCP 同进程 tokio runtime：简化部署，代价是 MCP 与 UI 共享 runtime（单点故障）。
- Plan 3 的 rmcp `/mcp` 完整集成：本 plan Task 4 尝试接通，若 rmcp 0.1 API 仍不稳定可继续留占位。

## Reference

完整逐任务实现（含每步代码、tauri.conf.json、command 代码、ipc.ts 代码）见：
`docs/superpowers/plans/2026-08-12-plan4-tauri-ui.md`（SDD plan，8 任务）。本 design 只描述架构与边界，代码以 SDD plan 为准。
