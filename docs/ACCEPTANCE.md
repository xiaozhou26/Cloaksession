# Chromix Acceptance Evidence

本文件记录 Cloaksession 接入 Chromix Node SDK 的实现范围和本地验收证据。SDK 来源固定为 `https://github.com/xiaozhou26/Chromix` 的 commit `39b9ea1bd262c2eb6306f3e23279398a6476c845`。官方 Node SDK 的 15 个文件及 SHA-256 清单见 `crates/tauri-app/resources/chromix/vendor/UPSTREAM.json`，契约测试见 `crates/tauri-app/resources/chromix/test/upstream.test.mjs`。

## 实现范围

- 保留 CloakBrowser 和 Chrome for Testing，引擎设置新增 Chromix。
- Settings 保存全局 `nodePath`、完整 SDK options JSON 和 Node environment JSON。
- 每个 Profile 保存独立 `chromixOptions`，通过数据库迁移、导入、导出、创建、更新、读取和列表流程保留。
- 前端公开字段目录覆盖 identity、hardware、display、GPU、fonts、regional、storage、WebRTC、audio、codecs、timer、CSS/input preferences、noise、third-party cookies、FakeShadowRoot、Canvas Bridge 和 SDK launch options。
- 完整 SDK JSON 和原始 `options.args` 入口保留未知字段、嵌套对象、数组、`false`、`null`、空字符串、重复参数以及字符串 uint64 seed。
- Node sidecar 负责持久化上下文、loopback CDP ready handshake、关闭和进程清理；它拒绝保留 host CDP sidecar 无法支持的 measured `devicePool` 模式。

## 验证命令

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| Rust workspace | `cargo test --workspace --locked` | 通过；存在既有 `tauri-app/src/commands/update.rs` dead-code warnings |
| Rust 编译 | `cargo check --workspace --locked` | 通过；存在同上 warnings |
| Chromix bridge 和 SDK | `cd crates/tauri-app/resources/chromix && npm test` | 29/29 通过 |
| 前端构建 | `cd crates/tauri-app/ui && npm run build` | 通过；存在既有 bundle size warning |
| 指纹目录 | `cd crates/tauri-app/ui && node --experimental-strip-types src/lib/chromixFingerprint.test.mjs` | 84/84 通过 |
| UI 浏览器测试 | `cd crates/tauri-app/ui && npx playwright test --config playwright.config.ts` | 12/12 通过，desktop 1440x1000、390x844 responsive touch viewport，并检查设置页 320px 窄视图 |
| 空白检查 | `git diff --check` | 通过 |

## 真实内核证据

本机使用 Chromix Chromium `v152.0.7977.82` macOS arm64 可执行文件运行：

```bash
cd crates/tauri-app/resources/chromix
CHROMIX_TEST_BINARY=/path/to/Chromium node test/native-smoke.mjs
```

Smoke 已验证：

- sidecar 启动、ready handshake、CDP 连接和关闭后端点消失；
- 页面加载、输入和按钮交互；
- `zh-CN` locale、`Asia/Shanghai` timezone；
- `--fingerprint-hardware-concurrency=12` 实际暴露为 12；
- viewport `[1280, 720]`；
- 持久化 localStorage 和 `.chromix-fingerprint-seed`；
- 两次启动复用同一 seed。

该二进制的原生边界也已记录在 smoke：请求 `--fingerprint-device-memory=16` 时，`navigator.deviceMemory` 暴露为原生 `8`；请求 `--fingerprint-storage-quota=2048` 时，`navigator.storage.estimate().quota` 暴露为原生 `10 GiB`。这表示参数已进入 SDK/启动参数契约，但当前 macOS 构建没有按这两个请求值覆盖页面字段。SDK option-builder 契约仍严格验证和保留这些参数；更换为匹配的 Chromix patch build 后应重新运行 smoke 并更新实际结果。

## 未宣称的能力

- 未将 `devicePool` 伪装成普通 Profile 指纹配置。该入口需要 Python SDK、完整 evidence bundle、host compatibility 和直接 runtime verification。
- 未将普通 SDK JSON 保存等同于所有浏览器字段在所有平台和二进制版本上都生效。
- 未把 `humanize` 扩展到 Cloaksession 自己的 CDP/MCP 操作；它只作用于 SDK 管理的 Playwright page 对象。
- 未把旧 CloakBrowser 的指纹参数、代理桥、GeoIP 或 quota 单位转换套用到 Chromix。
