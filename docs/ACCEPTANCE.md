# Wails 1.4.0 / Pure Go Acceptance

1.4.0 将原 Rust 应用后端全部迁移到 Wails Go 主程序。资源已移动到 `desktop/resources/playwright` 和 `desktop/resources/companion`。当前文档区分构建契约、已执行检查和仍需验证的纯 Go 行为；**1.3.0 的 Rust/Go 集成、真实浏览器和 universal 包通过记录不是 1.4.0 的证据**。

1.3.0 标签的发布工作流因纯 Go 新要求取消。1.4.0 的通用测试、macOS 和 Linux 构建通过，Windows 安装包已生成，但 EOF 生命周期测试失败，阻止了 Release。修复版本为 `v1.4.1`；实际远端结果以 GitHub Actions 为准。

## 架构与数据保留

- Wails binding 直接调用 Go 应用服务，Go 包负责 SQLite/设置、浏览器/CDP/代理、MCP、扩展和归档。不再构建或运行 Rust 核心可执行文件。
- SQLite 使用 `modernc.org/sqlite` v1.34.5 的 Go 驱动。系统 WebView 与 race 检测仍可能需要平台原生工具链；“全 Go 后端”不表示 Wails 没有原生依赖。
- 前端仍为 React/TypeScript；Chromix 仍通过 Node `playwright-core` bridge 启动本地浏览器，不捆绑 Node、浏览器或 Chromix SDK/vendor。
- `options.fingerprintMode` 为 random/fixed/custom：random 每次启动新 seed；fixed 复用种子；custom 提供高级覆盖。`fingerprintSeed` 是非零 uint64 十进制字符串，不使用 JavaScript Number。
- 数据目录沿用 `com.cloaksession.browser`、`profiles.db`、`settings.json`、`mcp-token` 和已有 Profile/扩展目录。迁移必须验证旧库、未知 JSON 字段及 `.mzar` 兼容，不能以新 Go 代码能编译替代数据验收。

## 构建和发布契约

- CI 固定 Go 1.23.12、Node 22、Python 3.12；Wails CLI 固定 v2.11.0。没有 Rust/Cargo 安装、缓存、编译或测试步骤。
- `scripts/build.py` 通过 npm lock 安装前端和 `desktop/resources/playwright`，构建前端，再运行 Wails Go build；`--test` 可在构建前执行 Go race 测试。
- 版本检查核对 Wails productVersion、前端 package/lock、Playwright 资源 package/lock 和 Wails Go pin，目标均为 1.4.0（第三方依赖保持其自身版本）。
- Windows NSIS 包含 `Cloaksession.exe`、`resources/playwright`、`resources/companion`；Linux tar.gz 包含同样布局的 `Cloaksession`；macOS ZIP 中仅有业务主程序 `Contents/MacOS/Cloaksession`，资源位于 `Contents/Resources/{playwright,companion}`。
- macOS universal 仅合并 Go arm64/x86_64，`lipo` 校验主程序；复制资源后重新 ad-hoc 签名。包中不得含旧 `desktop-core` 可执行文件。
- 打包替换资源树，只复制 bridge、npm manifest/lock、生产依赖和 companion；`npm ci --omit=dev` 清理旧依赖。旧 SDK/vendor 不进入发布包。
- NSIS 写入前检查窗口和文件锁，提示退出后 Retry/Cancel；升级时清理旧 helper/资源，不强杀进程、不删除用户数据。附件保留 `-setup.exe` 后缀。
- Linux CI 为 Ubuntu 22.04 + WebKitGTK 4.0；Ubuntu 24.04 使用 4.1 和 `--webkit2-41`。
- Release 必须等待测试与三平台构建全部成功，再统一上传安装包/ZIP/tar.gz 和 SHA-256 清单；不覆盖已有 Release。

## CI 测试范围

- Python 包装契约、Node Playwright bridge、前端指纹单元、TypeScript/Vite、Go `go test -race -count=1 -mod=readonly -v ./...`。
- Chrome UI 回归运行完整 Playwright 配置（当前发现 72 项，含原有 52 项及新增回归），检查桌面/窄屏和指纹模式；模拟桌面桥的 UI 测试不证明真实 Go 后端行为。
- 实际 Go 浏览器测试为 `desktop/service_test.go` 的 `TestServiceRealBrowser`。CI 安装 Chrome 后设置 `CLOAKSESSION_TEST_BROWSER=$(command -v google-chrome)`，检查可执行文件存在，再运行 `go test -race -count=1 -mod=readonly -v -run '^TestServiceRealBrowser$' .`。它通过 Go 服务启动 Playwright/Chrome、检查 CDP 端点、关闭并删除 Profile；旧核心进程环境变量不再使用。

## 本轮验证状态

已通过：19 项 Python 包装契约、`--check-version --tag v1.4.0`、actionlint v1.7.7、空白检查，以及 NSIS 3.13 `-WX` 的 1.4.0 fixture 编译（payload 无旧核心可执行文件，含 Playwright/companion）。实际移动后的资源也已成功放置到隔离目录。

本轮前端/runtime 检查通过：18 项 Node bridge 测试、84 项前端指纹单元测试、TypeScript/Vite 构建，以及完整 Chrome Playwright UI 回归 **72/72**（原有 52 项及新增错误处理回归）。前端构建仍有既有大 chunk 提示；这些 UI 测试使用模拟 Wails bridge。

全部 Go 包使用 Go 1.23.12 和实际本机 Chrome 执行 `go test -race -count=1 ./...` 与 `go vet ./...` 通过。范围包括 Profile/设置/归档、独立旧版数据库及 Rust 加密归档 fixture、原生 CDP 和 Playwright 两条真实浏览器路径、导航/输入/截图/Cookie/标签页/持久化、代理认证、MCP HTTP/认证/活动、扩展包导入/清理/图标/伴随扩展，以及更新器和 GeoIP。

额外集成验证通过：Wails 页面首次引导、Profile CRUD、设置/指纹模式持久化、手机与桌面页面、界面按钮启停真实浏览器及事件刷新；运行中的 Go MCP HTTP 工具目录、创建/查询 Profile；并发启动和删除三轮后无残留浏览器；MCP 端口占用时桌面仍可修改设置，界面显示实际错误并支持保存新端口/关闭自启；未返回的原生对话框不会阻止服务取消退出。

`GOTOOLCHAIN=go1.23.12 python3 scripts/build.py --package --universal` 已实际完成 1.4.0 macOS 双架构构建，主程序 `lipo` 与完整包签名校验通过，包中不再包含 Rust 核心。Windows/Linux 本机仅验证 Go 后端交叉编译及打包契约，平台 Wails 构建由发布 CI 验证。

## 必须完成的目标机器验收

1. 关闭旧应用与所有 Profile，备份完整数据目录；用副本验证旧 SQLite、设置、MCP token、未知字段、Profile 数据及扩展均保留。
2. 验证三种引擎启动/退出、CDP、代理认证、Cookie/持久化、seed 模式、扩展及节点/浏览器进程清理。
3. 验证原生对话框、加密 `.mzar` 导入/导出、MCP 认证/Host/工具/活动记录、更新检查和安装入口。
4. Windows 在旧版运行时启动安装器，确认 Retry/Cancel 在覆盖前出现；退出后升级成功，旧 helper 不残留，卸载保留数据目录。
5. 在 Intel/Apple Silicon 实际启动最终 universal 包；Linux 在匹配 WebKitGTK 的机器启动；检查包只含 Go 主程序及正确的 Playwright/companion 资源。
6. 明确 WebView localStorage 偏好可能与旧 Tauri origin 不同；它们重置不代表数据库或浏览器 Profile 应重置。

Windows 未配置 Authenticode；macOS 仅 ad-hoc 签名、未公证。配置了 CI 不代表远端已经运行成功，生成安装包也不证明真实浏览器指纹效果。
