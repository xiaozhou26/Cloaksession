# Cloaksession

Cloaksession 是一个基于 **Rust + Tauri 2 + React** 的桌面浏览器环境管理工具。它为每个 Profile 管理独立的浏览器用户数据目录、代理和指纹配置，支持手动使用，也通过本地 **MCP（Model Context Protocol）** 接口供 AI 客户端操作浏览器。

本仓库提供管理应用及浏览器启动、Chrome DevTools Protocol（CDP，浏览器调试协议）控制逻辑，**不包含浏览器内核**。当前默认引擎为 CloakBrowser，另有 Chrome for Testing（CFT）兼容路径，并可选接入 Chromix Node SDK。以下以 Windows 为主要使用和开发环境；发布工作流包含其他平台，但不代表所有功能已经过跨平台验证。

- 源码：[xiaozhou26/Cloaksession](https://github.com/xiaozhou26/Cloaksession)
- 下载：[GitHub Releases](https://github.com/xiaozhou26/Cloaksession/releases)（以实际发布附件为准）

## 已实现的功能

- **Profile 管理**：创建、编辑、删除、启动和关闭环境，保存名称、标签、备注、图标、启动页等配置，并为浏览器设置会话恢复。
- **指纹配置**：设备预设、User-Agent、Client Hints、语言、时区、屏幕、DPR、CPU 核数、内存、WebGL、字体目录、存储配额和种子；具体生效范围取决于引擎。
- **独立代理**：HTTP / SOCKS5 上游及用户名、密码认证，通过本地 SOCKS5 桥接转发；提供代理出口 IP、国家和时区查询。
- **扩展管理**：从 Chrome Web Store 链接或 ID、`.crx` / `.zip` 文件、解包目录导入扩展，按 Profile 启用、停用和移除。
- **加密归档**：导入、导出 `.mzar`，包含 Profile 配置、用户数据目录和所引用的共享扩展，使用 scrypt 派生密钥及 AES-256-GCM 加密。
- **MCP 浏览器操作**：环境管理、导航、点击、输入、页面提取、截图、JavaScript 执行、标签页管理、Cookie 读写和等待条件，附工具调用活动记录。点击和输入接入了鼠标轨迹与按键间隔生成逻辑。
- **版本检查**：查询本仓库 GitHub Releases；Windows 可下载并启动 NSIS 安装程序。

## 浏览器引擎与限制

| 项目 | CloakBrowser（默认） | Chrome for Testing（`cft`） | Chromix（`chromix`） |
| --- | --- | --- | --- |
| 运行文件 | 需自行准备支持相应参数的 CloakBrowser 可执行文件 | 需自行准备 CFT / 兼容 Chromium 可执行文件 | 可填写本地 Chromix 可执行文件；留空时由 SDK 解析或下载 |
| 指纹应用方式 | 主要通过启动时的 `--fingerprint-*` 参数交给内核处理 | 标准启动参数、CDP User-Agent 覆盖及页面预加载脚本 | 固定 Chromix Node SDK 通过 sidecar 启动，前端完整编辑公开指纹 flags、SDK options 和原始 `args` |
| 当前覆盖范围 | 启动器传递平台、语言、时区、屏幕、GPU、字体、配额、种子等参数，最终行为由内核决定 | 当前脚本覆盖部分 navigator、屏幕和 WebGL 属性；未完整实现时区、字体、存储配额及 Client Hints 的等效覆盖 | 覆盖 SDK README 公开指纹参数、平台/品牌/硬件/屏幕/GPU/字体/区域/配额/WebRTC/音频/编解码器/CSS/input/噪声/Cookie/FakeShadowRoot/Canvas Bridge 等；实际效果依赖匹配的 Chromix 二进制 |
| 浏览器数据目录 | `profiles/<id>/engines/cloakbrowser/` | `profiles/<id>/` | `profiles/<id>/engines/chromix/` |

**引擎是应用级设置，不是每个 Profile 单独选择。** 切换引擎不会自动切换可执行文件，也不会迁移两套用户数据目录中的 Cookie 或登录状态；需要同时配置匹配的内核路径并重启应用。

### Chromix 配置

1. 在 **Settings → Browser engine** 选择 **Chromix**。
2. 在 **Chromix SDK configuration** 中设置 Node.js 可执行文件、全局 SDK options JSON 和 Node 环境 JSON。保存后重启应用。
3. 创建或编辑 Profile，在 **Chromix fingerprint** 中按 Identity、Hardware、Display、GPU、Fonts、Regional、Storage、Network、Media、Preferences、Privacy、SDK 和 Advanced 分组编辑公开参数。
4. 使用 **Raw options.args** 编辑完整原始启动参数。全局或 Profile 的 **SDK options JSON** 可保存 README 中的任意 JSON 字段，包含未知字段、嵌套对象、数组、`false`、`null`、空字符串、重复参数和字符串形式的 uint64 seed。
5. 启动时使用全局 options，再叠加当前 Profile 的 `chromixOptions`；Profile 的数组和嵌套对象按 SDK 选项整体覆盖。Cloaksession 自动提供每个 Profile 的用户目录、扩展目录、loopback CDP 端口和起始页。

Chromix 资源固定到 GitHub main commit `39b9ea1bd262c2eb6306f3e23279398a6476c845`，并包含上游文件哈希清单。资源目录为 `crates/tauri-app/resources/chromix/`；开发或打包钩子会执行 `npm ci`。Node.js 需要版本 20 或更高。`devicePool` 是要求 Python SDK、证据文件和直接 runtime verification 的 measured-device 入口，CDP sidecar 会明确拒绝该模式；它不属于普通指纹字段编辑。

注意以下边界：

- CloakBrowser 和 CFT 路径当前没有接通浏览器内核自动下载，必须准备本地可执行文件。Chromix 的 binary cache/download 由固定 Node SDK 管理，可在 Settings 中通过 SDK options、environment 和 Skip auto-download 控制。
- 指纹字段可以保存，并不表示所有字段在每个内核、页面和平台都能一致生效。设备预设只是配置，不等于虚拟机或真实硬件仿真。
- CDP 自动化、页面脚本覆盖和代理启动参数都不构成不可检测、匿名或防泄漏保证。请在实际浏览器版本和网络环境中自行验证。
- CDP 驱动的 `safe_cdp` 检查尚未拦截 chromiumoxide 的自动域启用；不能将其视为完整的保护层，某些 CloakBrowser 构建可能存在兼容性或崩溃风险。
- 默认配置含 Windows 设备信息和 `C:\Windows\Fonts` 字体路径，其他平台需要自行调整；CFT 路径也不能视为与 CloakBrowser 功能等价。

### 存储配额单位

`FingerprintConfig.storageQuota` 的 JSON 和数据库持久化单位是**字节**；界面的 **Storage quota (MB)** 使用十进制 MB，即 `1 MB = 1_000_000` 字节。

默认值为 `2_000_000_000` 字节（十进制 2 GB），界面显示 `2000` MB。CloakBrowser 启动参数也按**字节**传入，对应 **`--fingerprint-storage-quota=2000000000`**，不会再除以 `1_000_000` 或设置隐式下限。留空或设为零不传此参数，使用引擎默认值。

在本机配置的浏览器上验证时，`navigator.storage.estimate().quota` 返回与启动参数相同的字节数；更换内核版本后应重新核对其参数行为。

这是浏览器存储配额相关的指纹配置，**不是 Profile 目录的磁盘占用上限，也不会预分配 2 GB 空间**。

## Windows 快速开始

### 使用发布包

1. 从 [Releases](https://github.com/xiaozhou26/Cloaksession/releases) 下载实际提供的 Windows 安装包并安装；桌面界面需要 **Microsoft Edge WebView2 Runtime**。
2. 自行准备浏览器内核及其完整运行目录。Cloaksession 安装包不是 CloakBrowser / CFT 内核安装包。
3. 首次进入时完成欢迎引导、选择用量报告选项并命名第一个 Profile。此时只是创建配置，还没有启动浏览器。
4. 进入 **Settings → Browser engine / Browser binary**，选择匹配的引擎，通过 **Browse…** 指向实际可执行文件，建议使用绝对路径。
5. **完全退出并重新打开 Cloaksession**，再编辑 Profile 的代理、指纹和扩展，点击启动。一般 Profile 配置更改在该 Profile 下次启动时应用。

CloakBrowser/CFT 浏览器路径的优先级为：非空 `browserBinaryPath` 设置 → 环境变量 `MULTIZEN_BROWSER_BINARY` → 平台默认路径。Chromix 选择非空路径后交给 SDK；路径为空时按 `contextOptions.executablePath` → `launchOptions.executablePath` → `CLOAKBROWSER_BINARY_PATH` → SDK cache/download 解析。Windows 旧引擎默认仅使用 `cloakbrowser.exe` 这个文件名；若系统找不到它，启动会失败。

首次引导会保存 `usageReporting` 选择，默认关闭；当前源码没有接通界面所描述的每日心跳发送逻辑。不要据此推断应用完全不联网：代理查询会访问 `ipapi.co`，扩展下载访问 Google 服务，更新检查访问 GitHub。新建或缺少 `autoUpdate` 字段的设置由 `SettingsStore` 按开启自动检查读取，可在 Settings 中关闭。

### 从源码开发

准备以下环境：

- Git。
- Rust stable、Cargo，Windows 使用 MSVC 工具链。
- Visual Studio Build Tools 的 C++ 桌面开发组件和 Windows SDK。
- Microsoft Edge WebView2 Runtime（管理界面的 WebView，不是被管理的浏览器内核）。
- Node.js **22** 和 npm，与当前发布工作流一致。
- 可运行的 CloakBrowser、CFT 或与固定 SDK 匹配的 Chromix 二进制；仅编译和多数测试不需要实际启动内核。

在 PowerShell 中执行：

```powershell
git clone https://github.com/xiaozhou26/Cloaksession.git
cd Cloaksession
cd crates/tauri-app/ui
npm install --legacy-peer-deps
cd ..
.\ui\node_modules\.bin\tauri.cmd dev
```

这里使用 `crates/tauri-app/ui` 内安装的 **`@tauri-apps/cli`**，不要求全局安装 Tauri CLI 或 `cargo-tauri`。从 `crates/tauri-app` 执行可让 CLI 正确读取 `tauri.conf.json`。它会在 `ui` 目录自动运行 `npm run dev`，再启动 Rust 应用；Vite 开发地址为 `http://localhost:5173`。仅运行 Vite 不会提供 Tauri 后端命令。

### 构建安装包

已安装 UI 依赖后，从仓库根目录执行：

```powershell
cd crates/tauri-app
.\ui\node_modules\.bin\tauri.cmd build
```

构建钩子会在 `ui` 目录执行 `npm run build`，即 `tsc -b && vite build`，然后编译 Rust 并打包。默认 Cargo 目标目录下，Windows 主程序为 `target/release/tauri-app.exe`，安装包位于 `target/release/bundle/nsis/` 和 `target/release/bundle/msi/`；自定义 `CARGO_TARGET_DIR` 时位置相应改变。

发布行为以 [`.github/workflows/release.yml`](.github/workflows/release.yml) 为准：`v*` 标签推送或手动触发，使用 Rust stable、Node.js 22，在 `crates/tauri-app/ui` 执行 `npm install --legacy-peer-deps`，再由 `tauri-apps/tauri-action@v0` 在 `crates/tauri-app` 构建。当前矩阵包含 Windows、macOS universal（`--target universal-apple-darwin`，安装 arm64 / x86_64 两个 Rust target）及 Ubuntu 22.04；Linux 系统依赖列表也在该文件中。**工作流配置不是发布成功记录，macOS / Linux 的构建与运行兼容性仍需在目标平台验证。**

## 本地数据与备份

应用以 Tauri 的 `app_local_data_dir` 为数据根目录；当前标识符为 `com.cloaksession.browser`。Windows 通常位于：

`%LOCALAPPDATA%\com.cloaksession.browser\`

若系统路径解析失败，源码会回退到应用配置目录，再回退到当前工作目录。

| 文件或目录 | 内容 |
| --- | --- |
| `profiles.db` | SQLite Profile 配置，包含代理、指纹和扩展引用；启用 WAL，运行时可能伴有 `-wal` / `-shm` 文件 |
| `profiles/` | 每个 Profile 的浏览器用户数据；CloakBrowser 和 Chromix 使用引擎子目录，CFT 使用 Profile 根目录 |
| `settings.json` | 应用设置，JSON 键使用 camelCase |
| `mcp-token` | MCP Bearer 凭据，在应用启动时读取或生成 |
| `extensions/` | 共享的解包扩展文件 |
| `companion/` | 从应用内嵌资源写出的 Chrome Web Store 辅助扩展 |

界面的部分偏好保存在 WebView 的 localStorage。MCP 活动历史只保存在内存中，最多保留 500 条，应用重启后清空。

- **本地 SQLite、设置文件和令牌并未由应用整体加密**；代理凭据会随 Profile 配置写入数据库。浏览器自身如何保护 Cookie、密码取决于内核和操作系统。请保护好整个数据目录。
- 备份前关闭 Profile 和应用，避免正在写入的数据库、会话文件造成不一致。`.mzar` 是按 Profile 的加密导出，不是整个应用设置或 MCP 令牌的备份。
- 导出时设置口令并妥善保存；导入需要同一口令。归档复制浏览器文件，但不保证跨机器或跨操作系统后仍能解密登录凭据。
- 手动编辑 `settings.json` 前先退出应用；设置存在进程内缓存，修改文件不会热重载。

## MCP 连接

MCP 服务嵌入桌面应用运行，无需另外启动一个 `mcp-server` 可执行程序。默认随应用启动，仅绑定 **`127.0.0.1:7777`**。

| 项目 | 当前实现 |
| --- | --- |
| 请求地址 | `http://127.0.0.1:7777/mcp`，HTTP POST JSON-RPC |
| 认证 | 每次请求携带 `Authorization: Bearer `，后接应用中的完整令牌 |
| 令牌位置 | MCP / Settings 页面可复制，也持久化于数据目录中的 `mcp-token` |
| Host 检查 | 只接受 `127.0.0.1:端口` 或 `localhost:端口` |
| 健康检查 | `GET http://127.0.0.1:7777/healthz`，无需 Bearer；仅证明 HTTP 服务响应，不证明浏览器已就绪 |

### 配置客户端

1. 保持 Cloaksession 运行，在 **MCP** 页面复制地址、令牌或客户端配置，确认客户端支持 HTTP MCP 连接及自定义 Authorization 请求头。
2. 将配置合并到客户端现有配置，避免覆盖其他 MCP 服务，然后重新加载或重启客户端。面板示例中的服务名 `multizen` 是保留的标识，不是另一个需要安装的程序。
3. 先调用 `list_profiles`，再通过界面或 `launch_profile` 启动目标环境。导航、页面提取、Cookie 等操作需要运行中的 Profile。

**当前传输有兼容性限制**：后端实现了 `initialize`、`ping`、`tools/list`、`tools/call` 等 JSON-RPC 方法，初始化返回协议版本 `2024-11-05`；没有完整的流式会话实现。界面虽然展示了 “Streamable HTTP” 和旧版 SSE 配置，**`GET /sse` 在认证通过后实际返回 HTTP 501**，不能当作可用的旧版 SSE 服务。客户端及 `mcp-remote` 桥接兼容性需按具体版本验证，不保证面板列出的所有客户端可直接使用。

### 修改设置与排查

- `mcpHttpEnabled` 控制**下次应用启动时**是否创建服务。改变开关不会立即关闭或启动当前监听；`mcpHttpPort` 也只在启动时读取。修改后需要完全重启 Cloaksession，并同步客户端地址。
- 当前 Settings 界面没有端口输入框；需要改端口时，在应用退出后编辑 `settings.json` 的 `mcpHttpPort`，保留其他配置。
- **当前 `system_info` 返回的地址固定为 7777**，界面中的地址和 “listening” 不反映真实绑定结果。自定义端口、关闭服务或端口冲突后，应以实际配置、`/healthz` 请求及后端日志为准。
- 合法的 `mcp-token` 会跨重启复用；文件缺失或格式无效时重新生成。更换令牌后需重启应用，并更新所有客户端。不要分享令牌，也不要把服务通过端口转发暴露给不受信任的网络。
- `401` 通常是 Bearer 缺失或不匹配，`403` 是 Host 不在允许列表，连接失败则检查应用是否运行、是否启用 MCP 以及端口占用。

默认工具目录不公开 `cdp_send`。如确需低层 CDP，可在启动应用前设置环境变量 `MULTIZEN_MCP_ALLOW_RAW_CDP=1`；启用后仍有方法和 URL 参数限制。该开关只控制 `cdp_send`，并不会关闭其他工具内部使用的 CDP 或 `evaluate_js`。连接的客户端可以读取页面、操作 Cookie、创建和删除环境，请只交给可信客户端；活动记录也不应视为全面脱敏的审计日志。

## 项目结构

仓库是 Rust workspace，UI 位于 Tauri crate 内：

| 路径 | 职责 |
| --- | --- |
| [`crates/multizen-core`](crates/multizen-core/src) | Profile、指纹、设置、错误等共享数据类型；名称保留历史标识 |
| [`crates/profile-manager`](crates/profile-manager/src) | SQLite 持久化、迁移、默认指纹和 Profile 数据目录 |
| [`crates/settings-store`](crates/settings-store/src) | JSON 设置加载、默认值归一化和进程内缓存 |
| [`crates/browser-launcher`](crates/browser-launcher/src) | 浏览器进程、启动参数、版本检测、代理桥接、出口查询和会话恢复 |
| [`crates/behavioral`](crates/behavioral/src) | 鼠标路径、按键间隔和滚动曲线生成 |
| [`crates/cdp-driver`](crates/cdp-driver/src) | CDP 会话、目标页面、指纹 bootstrap、页面操作及低层请求 |
| [`crates/mcp-server`](crates/mcp-server/src) | MCP 工具元数据、参数 schema、JSON-RPC / HTTP、认证与活动记录 |
| [`crates/tauri-app`](crates/tauri-app/src) | 桌面入口、IPC 命令、内嵌 MCP、归档、扩展和更新集成 |
| [`crates/tauri-app/ui`](crates/tauri-app/ui/src) | React 19、TypeScript、Vite 6、Tailwind CSS 4 界面 |

UI 经 Tauri IPC 进入应用驱动，MCP HTTP 经工具分发进入同一驱动；驱动协调 Profile 持久化、浏览器启动器和 CDP 会话。MCP 层不内置大模型，模型和客户端由使用者选择。

## 检查与测试

在 Windows 开发依赖齐备后，从仓库根目录执行：

```powershell
npm --prefix crates/tauri-app/ui run build
cargo check --workspace
cargo test --workspace
```

前端构建包含 TypeScript 检查；当前 `package.json` 没有单独的 lint 或前端测试脚本。Rust 测试覆盖 Profile / 设置持久化、启动参数、代理桥接、行为算法、CDP 辅助逻辑和 MCP 工具与传输。普通测试不等同于真实内核的端到端验证。Chromix 资源测试还包括固定上游 SDK 契约测试；真实二进制 smoke 需要设置 `CHROMIX_TEST_BINARY`，并应按目标二进制重新核对硬件与存储字段。

需要真实浏览器的测试默认标记为忽略，仅加环境变量还不够，必须同时传 `-- --ignored`：

- `browser-launcher/tests/driver.rs`：设置 `RUN_CDP_INTEGRATION=1` 和指向实际内核的 `MULTIZEN_TEST_BINARY`，运行 `cargo test -p browser-launcher --test driver -- --ignored`。该测试会自行启动、关闭浏览器。
- `cdp-driver/tests/integration.rs`：先准备可连接的 CDP 浏览器，设置 `RUN_CDP_INTEGRATION=1`，必要时设置 `MULTIZEN_TEST_CDP`（默认 `http://127.0.0.1:9222`），运行 `cargo test -p cdp-driver --test integration -- --ignored`。测试会访问 `https://example.com`。

这些变量和部分内部包名仍使用 `MULTIZEN_*`，请按源码中的名称设置。发布工作流会在各平台打包前执行 `cargo test --locked -p browser-launcher --test args`，包含 quota 参数回归测试；它不执行完整 workspace 测试或真实浏览器测试。

## 许可证

Copyright 2026 Cloaksession contributors.

Cloaksession 采用 **Apache License 2.0**，完整条款见仓库 [LICENSE](LICENSE)，也可阅读 [Apache 官方许可证文本](https://www.apache.org/licenses/LICENSE-2.0)。

第三方依赖、浏览器内核（包括 CloakBrowser、Chromium / Chrome for Testing）、安装的扩展、图标及其他第三方资源仍受各自许可证和使用条款约束。本仓库的 Apache-2.0 声明不替代这些条款，也不授予第三方商标权；重新分发时请核查对应版本及所附的版权、许可证和 NOTICE 要求。
