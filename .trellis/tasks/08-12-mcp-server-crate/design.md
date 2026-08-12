# Design — mcp-server crate (Plan 3)

## Architecture

`mcp-server` 依赖 `cdp-driver` + `browser-launcher` + `profile-manager` + `multizen-core`。定义 `BrowserDriver` trait（TS interface 的 Rust 镜像），`mcp-server` 在其上组合 22 工具。HTTP transport 用 `axum`；Streamable HTTP path 每 request 建新 `rmcp::ServiceServer`（stateless），SSE path 一个长生命周期 server。`ActivityLog` 是内存环形缓冲（容量 500）+ `tokio::sync::broadcast` 事件流。所有 async 用 tokio。

## File Structure

```
crates/mcp-server/
├── Cargo.toml
├── src/
│   ├── lib.rs                # re-export 8 模块
│   ├── driver.rs             # BrowserDriver trait（TS interface 镜像）
│   ├── server.rs             # build_server：组装 22 工具的 rmcp Server
│   ├── tools.rs              # 22 个 tool handler（纯逻辑 + 调 driver）
│   ├── schema.rs             # 每个工具的入参 struct（schemars 派生）
│   ├── security.rs           # BLOCKED_URL_SCHEMES / CDP_DENY_METHODS / normalize_url / redacted_proxy
│   ├── activity.rs           # ActivityLog + ActivityEvent
│   ├── transport.rs          # axum HttpTransport：/mcp + /sse + /healthz + auth + DNS-rebinding
│   └── token.rs              # bearer token constant-time compare
├── tests/
│   ├── security.rs           # 纯单元：url 归一化 / scheme 拒绝 / deny methods / redacted_proxy
│   ├── activity.rs           # 纯单元：sanitize / ring buffer
│   ├── tools.rs              # 纯单元：用 MockBrowserDriver 测每个工具 dispatch + 错误映射
│   └── transport.rs          # 集成：起 axum + reqwest 打 /healthz + /mcp 鉴权
```

## Module Boundaries

- `driver.rs`：trait 定义，不含实现。`cdp-driver` + `browser-launcher` 在 Plan 4 的 `tauri-app` 里组合出实现并注入。
- `tools.rs`：每个工具是纯函数 `(driver, profile_manager, activity, args) -> Result<serde_json::Value>`，可单测（用 MockBrowserDriver）。
- `security.rs`：所有安全门纯函数，无 IO。
- `transport.rs`：HTTP 层，组装 rmcp server + axum 路由；`parse_bearer` / `host_allowed` 为纯函数 + axum extractor，可单测。

## Data Flow

MCP 客户端 → `POST /mcp`（axum）→ auth 中间件（bearer constant-time）+ host_allowed（DNS-rebinding）→ 每 request 新 `rmcp::ServiceServer` → 路由到 22 工具之一 → 工具执行 `start_call`（ActivityLog）→ 安全门（assert_safe_url / cdp_method_allowed / assert_no_blocked_scheme_in_params）→ 调 `BrowserDriver` 方法 → `finish`（ActivityLog）→ 返回 JSON。

## Key Contracts

- `BrowserDriver` trait（9 方法）：`launch/close/is_running/navigate/click/type_text/extract/screenshot/cdp_send`，签名与 TS 1:1。
- `ActivityEvent`：`{id, timestamp, tool, profile_id?, args, status, summary?, duration_ms?}`，camelCase 序列化。
- 安全门常量逐字对齐 TS（见 `implement.md` 引用的 SDD plan Task 4）。
- 错误映射：`MultizenError::NotFound→PROFILE_NOT_FOUND`；`Launch→LAUNCH_FAILED`；`Mcp→FORBIDDEN/INVALID_INPUT`；其余→`INTERNAL_ERROR`。错误以 rmcp tool result error 格式返回。

## Compatibility / Migration

本 crate 是全新 Rust 实现，不修改旧 TS `packages/mcp-server`（旧码在重写完成后废弃）。与 Plan 1/2 的 crate 通过 `multizen-core` 类型（`LaunchedProfile`/`ProxyConfig`/`PartialFingerprintInput`/`MultizenError`）和 `profile-manager::ProfileManager` 接口对接。

## Trade-offs

- `/mcp` 与 `/sse` 的 rmcp 集成：rmcp 0.1 的 axum 集成 API 在写 SDD plan 时未能确认精确签名。决策：auth + host_allowed + healthz 必须可测，rmcp 集成若 API 不稳定留占位由 Plan 4 接入。代价是 Plan 3 结束时 `/mcp` 不真正分发工具，但安全门和工具逻辑已完整可测。
- `list_fingerprint_options`：profile-manager 尚未实现 device/locale catalog。决策：从 multizen-core `DeviceFamily` 静态表返回简化版，完整 catalog 留给后续。

## Reference

完整逐任务实现（含每步代码、测试、commit 信息）见：
`docs/superpowers/plans/2026-08-12-plan3-mcp-server.md`（SDD plan，9 任务）。本 design 只描述架构与边界，代码以 SDD plan 为准。
