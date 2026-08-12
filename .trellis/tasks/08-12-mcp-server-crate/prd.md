# Implement mcp-server crate (Plan 3)

## Goal

实现 `mcp-server` Rust crate：用 `rmcp` 暴露 22 个 MCP 工具，挂 `axum` HTTP+SSE transport（`POST /mcp` + `GET /sse` + `GET /healthz`），bearer token 鉴权 + DNS-rebinding 防护，并移植全部安全门。这是对现有 TS `packages/mcp-server/src/{server,HttpTransport,ActivityLog}.ts` 的 1:1 Rust 移植，是 Electron→Tauri + 全 Rust 后端重写的 Plan 3。

## Background

- Plan 1（foundation crates：multizen-core/profile-manager/settings-store）+ Plan 2（browser layer：behavioral/browser-launcher/cdp-driver）已完成并合入 master（HEAD=d6a0074，64/64 测试通过，clippy clean）。
- 本 crate 依赖 `cdp-driver` + `browser-launcher` + `profile-manager` + `multizen-core`。
- 详细的逐任务实现计划（含完整代码）在 `docs/superpowers/plans/2026-08-12-plan3-mcp-server.md`（SDD plan，9 个任务）。本 PRD 提炼需求与验收；技术设计见 `design.md`；执行顺序见 `implement.md`。

## Requirements

### R1: crate 骨架 + 错误变体
- 在 `multizen-core::MultizenError` 新增 `Mcp(String)` 变体。
- 新增 `crates/mcp-server`，加入 workspace，`Cargo.toml` 含 rmcp/axum/tokio/serde/schemars/subtle 依赖。
- `lib.rs` re-export 8 个模块（driver/server/tools/schema/security/activity/transport/token）。

### R2: BrowserDriver trait
- 定义 `BrowserDriver` async trait（9 方法），签名与 TS `BrowserDriver` interface 1:1：launch/close/is_running/navigate/click/type_text/extract/screenshot/cdp_send。
- 实现由 Plan 4 的 tauri-app 注入；本 crate 只定义 trait + MockBrowserDriver（测试用）。

### R3: ActivityLog + sanitize
- `ActivityLog`：内存环形缓冲（容量 500）+ `tokio::sync::broadcast` 事件流。
- `sanitize_args`：text>80 字符截断、proxy 凭据脱敏（移除 username/password）、cookies 值脱敏（`[redacted]`）。

### R4: 安全门（纯函数，无 IO）
- `BLOCKED_URL_SCHEMES = ["file:", "chrome:", "devtools:", "view-source:"]`
- `CDP_DENY_METHODS`：IO.read / Page.getResourceContent / Storage.getCookies / Network.getAllCookies / Browser.close / Browser.crash + DOMStorage.* / IndexedDB.* / CacheStorage.* / Fetch.* 通配
- `normalize_url_for_scan`：strip `\t\r\n` 全局 + 所有 ≤0x20 前导控制字符后再做 scheme 前缀测试
- `assert_safe_url` / `cdp_method_allowed` / `assert_no_blocked_scheme_in_params`（递归）/ `redacted_proxy`（只回 `{type,host,port,hasAuth}`）

### R5: 22 工具 handler
- 工具名逐字对齐 TS：`list_profiles, launch_profile, close_profile, navigate, click, type, extract, screenshot, create_profile, update_profile, delete_profile, list_fingerprint_options, evaluate_js, wait_for_selector, list_tabs, activate_tab, close_tab, wait_for_navigation, wait_for_load, cdp_send, get_cookies, set_cookies, new_tab`。
- 每个工具：`start_call` → 执行（含安全门）→ `finish`（ok/error）→ 返回 JSON。
- `cdp_send` 默认隐藏（`tools/list` 不列），仅 `MULTIZEN_MCP_ALLOW_RAW_CDP=1` 时可见且仍过 deny list。
- 错误映射：NotFound→`PROFILE_NOT_FOUND`；Launch→`LAUNCH_FAILED`；Mcp→`FORBIDDEN`/`INVALID_INPUT`；其余→`INTERNAL_ERROR`。

### R6: token constant-time 比较
- `token_matches(provided, expected)` 用 `subtle::ConstantTimeEq`，长度不等也保持恒定时间。

### R7: axum HttpTransport
- 路由：`POST /mcp`（Streamable HTTP）、`GET /sse`（legacy）、`GET /healthz`。
- 中间件：`parse_bearer` + `token_matches` 鉴权；`host_allowed` DNS-rebinding 防护（`allowedHosts = [127.0.0.1:port, localhost:port]`）。
- `/mcp` 与 `/sse` 的 rmcp 集成若 rmcp 0.1 API 不稳定可留占位（Plan 4 接入），但 auth + host_allowed + healthz 必须可测且不破坏编译。

### R8: workspace 校验 + README
- `cargo test --workspace` 全 PASS（Plan 1+2 的 64 + Plan 3 新增）。
- `cargo clippy --workspace --all-targets -- -D warnings` clean。
- `crates/mcp-server/README.md` 描述 crate 职责 + 安全门。

## Acceptance Criteria

- [ ] `crates/mcp-server` 存在，`cargo check -p mcp-server` 通过，workspace 编译。
- [ ] `MultizenError::Mcp(String)` 已加，被 tools/transport 使用。
- [ ] `BrowserDriver` trait 9 方法 + MockBrowserDriver 通过 `cargo test -p mcp-server`。
- [ ] ActivityLog 环形缓冲容量 500、sanitize 三类脱敏单元测试全 PASS。
- [ ] 安全门纯函数单元测试全 PASS：URL 归一化（tab/newline/前导控制字符）、4 个 blocked scheme、CDP deny（含 Fetch.* 通配）、`assert_no_blocked_scheme_in_params` 递归、`redacted_proxy` 隐藏凭据。
- [ ] 22 工具全部实现，关键工具（navigate/cdp_send/get_cookies/new_tab）的安全门路径有测试覆盖；`cdp_send` 默认禁用、`MULTIZEN_MCP_ALLOW_RAW_CDP=1` 开启后仍拒 IO.read。
- [ ] `token_matches` 恒定时间比较单元测试 PASS（同/异/异长/空）。
- [ ] axum transport：`/healthz` 200、`parse_bearer`、`host_allowed` 单元测试 PASS；`/mcp` 无 token 返 401、非允许 host 返 403。
- [ ] `cargo test --workspace` 全 PASS，`cargo clippy --workspace --all-targets -- -D warnings` clean。
- [ ] `crates/mcp-server/README.md` 已写。

## Out of Scope

- `/mcp` 与 `/sse` 的完整 rmcp ServiceServer 集成（若 rmcp 0.1 API 不稳定，留占位由 Plan 4 接入）。
- `BrowserDriver` 的真实实现（Plan 4 的 tauri-app 组合 cdp-driver + browser-launcher 注入）。
- `list_fingerprint_options` 的完整 device/locale catalog（profile-manager 尚未实现 catalog；此 plan 从 multizen-core DeviceFamily 静态表返回）。
- Plan 4（tauri-app + React UI 迁移）。

## Key Decisions

- crate 边界：`mcp-server` 只定义 `BrowserDriver` trait + 工具逻辑 + 安全门 + transport，不直接依赖 cdp-driver 的具体实现。
- 安全门为纯函数，便于单测；transport 的鉴权/DNS-rebinding 中间件也为纯函数 + axum extractor。
- `cdp_send` 默认隐藏，环境变量显式开启，且开启后仍过 deny list（不绕过安全门）。
- 错误以 rmcp tool result error 格式返回（`isError:true` + content `{error:{code,message}}`）。

## Risks / Deferred

- rmcp 0.1 的 axum 集成 API 在写 SDD plan 时未能确认精确签名——`/mcp` 与 `/sse` 的 rmcp 集成可能需 Plan 4 接入时补全（已在代码注释标注）。
- `list_fingerprint_options` 的 catalog 是简化版（静态表），完整 catalog 留给后续补全 profile-manager。
