# Implement — tauri-app crate + React UI migration (Plan 4)

## Execution Order

按 SDD plan `docs/superpowers/plans/2026-08-12-plan4-tauri-ui.md` 的 8 个任务顺序执行。每任务独立 TDD 循环（写失败测试 → 实现 → 测试通过 → clippy → commit）。

| Task | 内容 | 关键文件 | 测试/验证 |
|------|------|----------|----------|
| 1 | tauri-app 骨架 + tauri.conf.json | `crates/tauri-app/{Cargo.toml,tauri.conf.json,src/main.rs}` | `cargo check -p tauri-app` |
| 2 | ProfileRegistry + TauriBrowserDriver | `src/registry.rs`, `src/driver.rs` | 单元测试（Mock 或真实 launcher）|
| 3 | Tauri commands（profiles/settings/fingerprint/proxy/system/activity）| `src/commands/*.rs` | commands 注册 + 单测 |
| 4 | mcp-token 文件 + 内嵌 MCP server | `src/mcp_embed.rs` | token 生成 + axum spawn 单测 |
| 5 | push events | `src/main.rs` + commands | emit 单测 |
| 6 | 前端 IPC 层 | `ui/lib/ipc.ts` | TS 编译 + 类型检查 |
| 7 | 前端组件搬迁 | `ui/src/**` 从 `apps/desktop/src/renderer/src` 迁移 | Vite build 成功 |
| 8 | 端到端启动 + 全量校验 | workspace | `cargo test --workspace` + clippy clean + Tauri builder 构建成功 |

## Validation Commands

每任务结束：
```bash
cargo test -p tauri-app --test <suite>   # 若有测试
cargo clippy -p tauri-app --all-targets -- -D warnings
```
Task 8 全量：
```bash
cargo test --workspace
cargo clippy --workspace --all-targets -- -D warnings
cd ui && npm run build    # Vite build 成功
```

## Risky Files / Rollback Points

- workspace `Cargo.toml`：只加 `"crates/tauri-app",` 一个 member。
- `tauri.conf.json`：Tauri 2.x 配置格式，若版本漂移需调整。
- Task 7（前端组件迁移）是最大单块，若组件迁移出问题优先回退到 Task 6 完成状态（IPC 层已就绪）。
- Tauri 2.x API 漂移：参照 SDD plan 注释 + Tauri 2.x 官方文档调整。

## Pre-Start Checks

- [ ] 确认当前在 `multizen-browser-rs/` git 仓库，HEAD=0c04346（Plan 3 末尾）。
- [ ] SDD plan 文档存在：`docs/superpowers/plans/2026-08-12-plan4-tauri-ui.md`。
- [ ] `cargo test --workspace` 当前 baseline = 106 PASS（Plan 1+2+3）。
- [ ] Plan 3 final review 的 1 Important + 6 Minor 记录在 progress.md，本 plan 顺带处理。

## Plan 3 Follow-ups to Address in This Plan

- IMPORTANT（Task 2/3 实现 TauriBrowserDriver/commands 时）：10 个内部 CDP 工具跳过 `cdp_method_allowed`——route through shared gated helper 或确认豁免并文档化。
- MINOR（Task 4 接通内嵌 MCP 时）：transport port 硬编码 7777 → 参数化从 settings.mcpHttpPort。
- 其他 Minor 视情况在 Task 8 cleanup 处理。

## Reference

代码与测试逐字以 SDD plan 为准：
`docs/superpowers/plans/2026-08-12-plan4-tauri-ui.md`
