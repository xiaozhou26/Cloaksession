# Wails / Direct Playwright Acceptance Evidence

本文记录 Wails 1.3.0 与直接 Playwright runtime 的构建契约、已执行检查和待验收项目。当前资源源目录仍为 `crates/desktop-core/resources/chromix/`，仅为路径兼容保留 `chromix` 名称；Node runtime 唯一 npm 依赖为锁定的 `playwright-core`。不再捆绑 Chromix Node SDK/vendor，也不自动下载浏览器。早期 SDK 的测试计数和 native smoke 记录可在 git 历史查看，不能作为当前实现的验收证据。

## 运行与指纹契约

- 保留 CloakBrowser、CFT 和 Chromix 引擎路径；Go Wails 负责窗口、对话框、事件和 Rust sidecar 生命周期，Rust 保留 Profile、代理、扩展、归档、CDP 与 MCP。
- 简单指纹 UI 使用 `options.fingerprintMode`：`random` 每次启动生成新 seed，`fixed` 使用相同 seed 重复启动，`custom` 提供高级字段及原始 options/args。
- `options.fingerprintSeed` 是非零 uint64 十进制字符串（`1` 至 `18446744073709551615`），不是 JavaScript Number。固定模式的可复现性要求相同内核版本和其他配置；不表示匿名、无泄漏或真实设备仿真。
- 全局 options 和 Profile `chromixOptions` 保留 JSON 持久化。旧 SDK 专有选项不能当作 Playwright 支持项，保存成功不等于可启动；bridge 应明确报告不支持的选项。
- 外部 Node.js 20+ 和本地浏览器仍为运行前提。普通 CI 不会下载或启动被管理的浏览器内核。

## 构建与发布契约

- `scripts/build.py` 固定 Wails CLI `v2.11.0`；CI 明确固定 Go **1.23.12**，不用浮动最新版。本机 Go 1.27.1 曾触发 Wails `package "context" without types` 错误，1.23.12 已通过构建。
- 前端与 runtime 使用 `npm ci`，Rust 使用 Cargo `--locked`。打包验证 runtime manifest/lock 仅含 `playwright-core`，执行 `npm ci --omit=dev` 清理旧依赖；仅复制 bridge、npm manifest/lock 和校验后的生产 `node_modules`，替换整个目标资源树，不复制旧 vendor 或测试文件。
- Windows NSIS 附件保留 `-setup.exe` 后缀，包含主程序、`desktop-core.exe` 和 `resources/chromix`。写入前检查窗口及两个可执行文件的独占写访问；文件被占用时提示完全退出/等待 sidecar 退出后 Retry，Cancel 在改写前中止，不强杀进程。
- macOS ZIP 中两个 universal 可执行文件位于 `Contents/MacOS`，资源位于 `Contents/Resources/chromix`；复制 runtime 后重新 ad-hoc 签名。Windows/Linux 的 sidecar 与主程序同目录，runtime 位于 `resources/chromix`。Linux 发布 tar.gz。
- Linux CI 为 Ubuntu 22.04 + WebKitGTK 4.0；Ubuntu 24.04 使用 4.1 开发包及 `--webkit2-41`。
- `v1.3.0` 发布前验证 tag、Cargo manifest/lock、前端 manifest/lock、Wails productVersion 和 Wails Go pin。全部测试及三平台构建成功后才统一创建 Release，并附 SHA-256 清单。

## 实际 Go/Rust 集成 CI

Linux test job 先构建前端嵌入 assets，再执行 `cargo build --locked -p desktop-core`，设置绝对路径：

- `CLOAKSESSION_TEST_CORE=${{ github.workspace }}/target/debug/desktop-core`
- `CLOAKSESSION_RESOURCE_DIR=${{ github.workspace }}/crates/desktop-core/resources`

随后执行 `go test -race -count=1 -mod=readonly -v ./...`，使 `TestRealCoreRoundTrip` 真正运行，不因缺少环境变量而跳过。测试覆盖真实 Rust 进程启动、JSON RPC、Profile CRUD、对话框桥接的 `.mzar` 导出/导入、错误返回和关闭；它不启动真实浏览器。

## 本次执行结果

| 检查 | 结果 |
| --- | --- |
| Python 构建/包装契约 | 16 项通过：版本、runtime manifest/lock、陈旧依赖拒绝、替换资源树、三平台布局、开发准备、universal 命令和附件命名 |
| `--check-version --tag v1.3.0` | 通过 |
| actionlint | 两个工作流通过 |
| `git diff --check` | 通过 |
| NSIS 3.13 fixture 编译 | `-WX` 通过，无 warnings；含 Retry/文件锁检查，但不是 Windows 安装运行验证 |
| 隔离目录中的真实 `npm ci --omit=dev` | 清除预置陈旧依赖，只安装 `playwright-core`；未修改其他 agent 的资源目录 |
| `cargo build --locked -p desktop-core` + Go 1.23.12 `go test -race -count=1 -mod=readonly -v ./...` | 本机通过，`TestRealCoreRoundTrip` 明确运行并通过，无 race 报告 |

| Rust workspace `cargo test --workspace --locked` | 163 通过，3 个真实浏览器测试默认忽略 |
| Node Playwright bridge | 18 通过，包含 seed 精度、随机模式、参数兼容和生命周期 |
| 前端参数目录 | 84 通过 |
| Chrome 前端交互回归 | 52 通过，桌面与 390px 宽度，包含指纹模式和横向溢出检查 |
| 真实 Wails → Rust 浏览器页面验证 | 引导、Profile CRUD、刷新持久化、MCP、Settings、最大 uint64 seed、随机按钮、自定义选项及模式保存通过 |
| 真实 Go → Rust → Playwright → Chrome | 启动、CDP、事件、关闭与归档回归通过；Rust driver 两次启动及持久化 smoke 也明确执行通过 |
| Go 宿主审查回归 | 初始化错误进入启动结果、阻塞 stdin 写入可被退出打断；race 与 vet 通过 |

已重新执行 `GOTOOLCHAIN=go1.23.12 python3 scripts/build.py --package --universal` 验证直接 Playwright runtime 的最终包，macOS arm64/x86_64 主程序及核心二进制均通过 `lipo` 校验，完整资源包签名验证通过。真实浏览器测试使用本机 Google Chrome；Chromix 补丁的实际指纹效果仍需匹配内核验证。

## 目标机器验收清单

1. 完全退出旧应用及所有 Profile，备份 `com.cloaksession.browser` 整个数据根目录，包括 SQLite/WAL、设置、MCP token、浏览器用户目录和扩展。
2. 安装后核对已有 Profile、引擎设置、原始 JSON 和令牌；分别启动三种引擎，确认原有 Cookie/登录状态仍在对应引擎子目录。
3. 连续启动 random 模式确认 seed 更新；固定模式确认相同 seed/配置复现；custom 模式确认高级字段生效和不支持选项明确报错。
4. 检查代理认证、扩展、归档、原生对话框、MCP、事件和退出后的 Rust/Node/browser 进程清理。
5. Windows 应用运行时启动安装器，确认先出现 Retry/Cancel 且未覆盖文件；退出后 Retry 成功；卸载后数据根目录仍在。验证实际更新入口及 `-setup.exe` 下载。
6. macOS 在 Intel/Apple Silicon 分别启动最终 universal 包；Linux 在匹配 WebKitGTK 的系统运行最终归档。WebView localStorage 偏好不保证跨旧 Tauri origin 自动迁移，数据库与浏览器用户目录不应重置。

Windows 安装/升级、Linux GUI、macOS 双架构启动以及真实内核/数据保留仍需目标机器验证。Windows 未配置 Authenticode，macOS 仅 ad-hoc 签名、未公证。配置了 CI 不等于远端工作流已经成功运行。
