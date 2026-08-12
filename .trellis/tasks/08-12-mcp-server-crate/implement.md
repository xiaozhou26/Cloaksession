# Implement — mcp-server crate (Plan 3)

## Execution Order

按 SDD plan `docs/superpowers/plans/2026-08-12-plan3-mcp-server.md` 的 9 个任务顺序执行。每任务独立 TDD 循环（写失败测试 → 实现 → 测试通过 → clippy → commit）。

| Task | 内容 | 关键文件 | 测试 |
|------|------|----------|------|
| 1 | crate 骨架 + `MultizenError::Mcp` | `crates/multizen-core/src/error.rs`, `Cargo.toml`, `crates/mcp-server/{Cargo.toml,src/lib.rs+8占位}` | `cargo check -p mcp-server` |
| 2 | `BrowserDriver` trait（9 方法） | `crates/mcp-server/src/driver.rs` | 编译通过 |
| 3 | ActivityLog + sanitize_args | `crates/mcp-server/src/activity.rs`, `tests/activity.rs` | 5 单元测试 |
| 4 | 安全门纯函数 | `crates/mcp-server/src/security.rs`, `tests/security.rs` | 8 单元测试 |
| 5 | 工具入参 schema + MockBrowserDriver | `crates/mcp-server/src/schema.rs`, `tests/mock_driver.rs` | 编译通过 |
| 6 | 22 工具 handler | `crates/mcp-server/src/tools.rs`, `tests/tools.rs` | 4+ 工具测试 |
| 7 | token constant-time 比较 | `crates/mcp-server/src/token.rs` | 4 单元测试 |
| 8 | axum HttpTransport | `crates/mcp-server/src/transport.rs`, `tests/transport.rs` | 4 单元测试 |
| 9 | workspace 校验 + README | `crates/mcp-server/README.md` | `cargo test --workspace` + clippy clean |

## Validation Commands

每任务结束：
```bash
cargo test -p mcp-server --test <suite>
cargo clippy -p mcp-server --all-targets -- -D warnings
```
Task 9 全量：
```bash
cargo test --workspace
cargo clippy --workspace --all-targets -- -D warnings
```

## Risky Files / Rollback Points

- `crates/multizen-core/src/error.rs`：加 `Mcp` 变体，不破坏现有变体（Plan 1/2 依赖 `Db/Io/Serde/NotFound/AlreadyExists/Config/Launch/Cdp`）。rollback = revert 该单文件。
- workspace `Cargo.toml`：只加 `"crates/mcp-server",` 一个 member。rollback = revert 该行。
- Task 6（22 工具）是最大单块，若 clippy/test 失败优先检查 chromiumoxide 0.7 之外的 rmcp 0.1 API 漂移（参照 SDD plan 注释）。

## Pre-Start Checks

- [ ] 确认当前在 `multizen-browser-rs/` git 仓库，HEAD=d6a0074（Plan 2 末尾）。
- [ ] SDD plan 文档存在：`docs/superpowers/plans/2026-08-12-plan3-mcp-server.md`。
- [ ] `cargo test --workspace` 当前 baseline = 64 PASS（Plan 1+2）。
- [ ] trellis spec `.trellis/spec/` 已对齐 Rust workspace（注：mcp-server spec 待 crate 建后刷新）。

## Reference

代码与测试逐字以 SDD plan 为准：
`docs/superpowers/plans/2026-08-12-plan3-mcp-server.md`
