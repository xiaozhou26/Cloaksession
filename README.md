# Cloaksession

Cloaksession 是一个基于 **Wails v2（Go）+ React** 的桌面浏览器环境管理工具。它为每个 Profile 管理独立的浏览器用户数据目录、代理和指纹配置，支持手动使用，也通过本地 **MCP（Model Context Protocol）** 接口供 AI 客户端操作浏览器。

本仓库提供管理应用及浏览器启动、Chrome DevTools Protocol（CDP，浏览器调试协议）控制逻辑，**不包含浏览器内核**。当前默认引擎为 CloakBrowser，另有 Chrome for Testing（CFT）兼容路径，并可通过 Node / Playwright bridge 启动本地 Chromix。以下以 Windows 为主要使用和开发环境；发布工作流包含其他平台，但不代表所有功能已经过跨平台验证。

- 源码：[xiaozhou26/Cloaksession](https://github.com/xiaozhou26/Cloaksession)
- 下载：[GitHub Releases](https://github.com/xiaozhou26/Cloaksession/releases)（以实际发布附件为准）

## 功能范围与迁移状态

**1.4.0 正在迁移为全 Go 后端**：Wails 主程序直接提供存储、浏览器、MCP、扩展和归档服务，不再启动 Rust 核心进程。下列是保留的功能范围；纯 Go 实现及最终安装包的通过情况以 [验收记录](docs/ACCEPTANCE.md) 为准，旧版验证不能替代本次回归。

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
| 运行文件 | 需自行准备支持相应参数的 CloakBrowser 可执行文件 | 需自行准备 CFT / 兼容 Chromium 可执行文件 | 需准备本地 Chromix / Chromium 可执行文件；不会自动下载 |
| 指纹应用方式 | 主要通过启动时的 `--fingerprint-*` 参数交给内核处理 | 标准启动参数、CDP User-Agent 覆盖及页面预加载脚本 | Node `playwright-core` bridge 启动持久化上下文，传递公开指纹 flags、Playwright options 和原始 `args` |
| 当前覆盖范围 | 启动器传递平台、语言、时区、屏幕、GPU、字体、配额、种子等参数，最终行为由内核决定 | 当前脚本覆盖部分 navigator、屏幕和 WebGL 属性；未完整实现时区、字体、存储配额及 Client Hints 的等效覆盖 | 保留 Chromix 公开指纹参数编辑、平台/品牌/硬件/屏幕/GPU/字体/区域/配额/WebRTC/音频/编解码器/CSS/input/噪声/Cookie/FakeShadowRoot/Canvas Bridge 等；实际效果依赖匹配的 Chromix 二进制 |
| 浏览器数据目录 | `profiles/<id>/engines/cloakbrowser/` | `profiles/<id>/` | `profiles/<id>/engines/chromix/` |

**引擎是应用级设置，不是每个 Profile 单独选择。** 切换引擎不会自动切换可执行文件，也不会迁移两套用户数据目录中的 Cookie 或登录状态；需要同时配置匹配的内核路径并重启应用。

### Chromix 配置

1. 在 **Settings → Browser engine** 选择 **Chromix**，设置本地浏览器路径。
2. 先选择全局默认指纹模式；Node 路径、Playwright JSON 和环境变量收在 **Advanced**，需要时再展开，保存全局启动设置后重启应用。
3. 创建或编辑 Profile，选择简单指纹模式：**Random each launch** 每次启动生成新 seed；**Fixed seed** 保存同一 seed，并提供 **Random seed** 按钮生成一次新值；**Custom** 提供平台、语言、时区、屏幕、CPU 和内存选择。完整参数与原始 JSON 收在 **Advanced**。随机和固定模式默认只传 seed，已有的显式高级覆盖仍然保留；自定义与未选择模式的旧配置会继续使用 Profile 指纹字段。配置保存在 `chromixOptions`，启动请求中的契约为 `options.fingerprintMode: random | fixed | custom` 和 `options.fingerprintSeed`（uint64 十进制字符串）。原始 JSON 的未知字段、嵌套对象、数组、`false`、`null`、空字符串和字符串 uint64 seed 可继续保存；**能保存不等于可启动**，bridge 会校验实际支持的选项。
4. 全局 options 叠加 Profile options；Profile 的数组/嵌套对象按选项整体覆盖。bridge 合并顶层、`launchOptions`、`contextOptions`，原始 `args` 用于传递浏览器支持的 flags，并保留 host 管理的用户目录、扩展和 loopback CDP。
5. 旧 SDK 专有选项（如 `devicePool`、`humanize`、`geoip`、`browserVersion`、`releaseChannel` 等）不再由 bridge 实现，会明确报错，需移除或改为实际 Playwright 选项/浏览器参数；不会伪装成成功。

运行资源位于 `desktop/resources/playwright/`，使用锁定的 `playwright-core`，不再捆绑 Chromix Node SDK/vendor。构建会执行 `npm ci`；运行需要外部 Node.js 20+ 和本地浏览器。固定/自定义 seed 必须是 `1` 至 `18446744073709551615` 的十进制字符串，不能用 JavaScript Number 保存；random 在每次启动时重新生成，不复用旧 seed。Fixed 的可复现性需保持相同内核和其他配置，seed 不构成匿名或唯一设备保证。

注意以下边界：

- 三种引擎都需要本地浏览器；Chromix bridge 仅使用已有可执行文件，`playwright-core` 不会自动下载内核。
- 指纹字段可以保存，并不表示所有字段在每个内核、页面和平台都能一致生效。设备预设只是配置，不等于虚拟机或真实硬件仿真。
- CDP 自动化、页面脚本覆盖和代理启动参数都不构成不可检测、匿名或防泄漏保证。请在实际浏览器版本和网络环境中自行验证。
- Go CDP 驱动和真实浏览器的域启用、指纹生效及进程清理需要重新回归；不能沿用旧版驱动的兼容性结论。
- 默认配置含 Windows 设备信息和 `C:\Windows\Fonts` 字体路径，其他平台需要自行调整；CFT 路径也不能视为与 CloakBrowser 功能等价。

### 存储配额单位

`FingerprintConfig.storageQuota` 的 JSON 和数据库持久化单位是**字节**；界面的 **Storage quota (MB)** 使用十进制 MB，即 `1 MB = 1_000_000` 字节。

默认值为 `2_000_000_000` 字节（十进制 2 GB），界面显示 `2000` MB。CloakBrowser 启动参数也按**字节**传入，对应 **`--fingerprint-storage-quota=2000000000`**，不会再除以 `1_000_000` 或设置隐式下限。留空或设为零不传此参数，使用引擎默认值。

Chromix 的公开 quota flag 使用 **MiB**；bridge 将 Profile 中的字节数向上换算，例如 `2147483648` 字节转换为 `--fingerprint-storage-quota=2048`。Advanced 中直接填写的原始 flag 保持原值。随机和固定 seed 模式默认不传 Profile quota。详见 [Playwright runtime 契约](desktop/resources/playwright/README.md)。

此前在本机 CloakBrowser 上验证时，`navigator.storage.estimate().quota` 返回与其启动参数相同的字节数；更换内核版本后应重新核对其参数行为。

这是浏览器存储配额相关的指纹配置，**不是 Profile 目录的磁盘占用上限，也不会预分配 2 GB 空间**。

## Windows 快速开始

### 使用发布包

1. 从 [Releases](https://github.com/xiaozhou26/Cloaksession/releases) 下载实际提供的 Windows 安装包并安装；桌面界面需要 **Microsoft Edge WebView2 Runtime**。
2. 自行准备浏览器内核及其完整运行目录。Cloaksession 安装包不是 CloakBrowser / CFT 内核安装包。
3. 首次进入时完成欢迎引导、选择用量报告选项并命名第一个 Profile。此时只是创建配置，还没有启动浏览器。
4. 进入 **Settings → Browser engine / Browser binary**，选择匹配的引擎，通过 **Browse…** 指向实际可执行文件，建议使用绝对路径。
5. **完全退出并重新打开 Cloaksession**，再编辑 Profile 的代理、指纹和扩展，点击启动。一般 Profile 配置更改在该 Profile 下次启动时应用。

CloakBrowser/CFT 浏览器路径的优先级为：非空 `browserBinaryPath` 设置 → 环境变量 `MULTIZEN_BROWSER_BINARY` → 平台默认路径。Chromix 按合并后的 `executablePath`（`contextOptions` → `launchOptions` → 顶层）→ 非空 `browserBinaryPath` → `CLOAKBROWSER_BINARY_PATH` → Playwright 已安装的默认可执行文件解析；文件缺失会明确报错，不下载。Windows 旧引擎默认仅使用 `cloakbrowser.exe` 这个文件名；若系统找不到它，启动会失败。

首次引导会保存 `usageReporting` 选择，默认关闭；当前源码没有接通界面所描述的每日心跳发送逻辑。不要据此推断应用完全不联网：代理查询会访问 `ipapi.co`，扩展下载访问 Google 服务，更新检查访问 GitHub。新建或缺少 `autoUpdate` 字段的设置由 `SettingsStore` 按开启自动检查读取，可在 Settings 中关闭。

### 从源码开发

桌面入口位于 `desktop/`，Go module 为 `github.com/xiaozhou26/Cloaksession/desktop`。当前目标版本为 **1.4.0**，Wails CLI 和 Go 依赖均固定为 **v2.11.0**。所有原 Rust 应用逻辑改由 Go 服务实现；无需 Rust、Cargo 或独立核心可执行文件。SQLite 使用 Go 驱动 `modernc.org/sqlite`（当前依赖 v1.34.5）；Wails 的系统 WebView 仍需要平台原生构建工具。

准备以下环境：

- Git、Go **1.23.12**（CI 显式固定；1.4.0 的完整验证见验收记录）、Python **3.11+**。新版 Go 未必兼容固定 Wails 的分析工具；若遇到 `package "context" without types`，使用 Go 1.23.12 或设置 `GOTOOLCHAIN=go1.23.12`。
- Node.js **22** 和 npm（Chromix 运行时至少需要 Node 20；安装包不内置 Node 或浏览器内核）。
- Windows：WebView2 Runtime；运行 `go test -race` 需支持 CGO 的 C 工具链（例如 MinGW-w64）；生成安装包还需 NSIS 3（`choco install nsis -y`）。应用内嵌 WebView2 bootstrapper，可在缺失 Runtime 时提示安装，安装 Runtime 仍需网络。
- macOS：Xcode Command Line Tools；universal 由 Wails 编译 Go arm64 / x86_64 并合并，不安装 Rust target。
- Ubuntu **22.04**：`sudo apt-get install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev libssl-dev`，这是 CI 的 Linux 基线。
- Ubuntu **24.04**：将 WebKit 开发包替换为 `libwebkit2gtk-4.1-dev`，构建加 `--webkit2-41`，Go 测试和 Wails dev 加 `-tags webkit2_41`。4.0 和 4.1 构建不可混用。
- 可运行的 CloakBrowser、CFT 或支持相应指纹 flags 的 Chromix 二进制；仅编译和多数测试不需要真实内核。

从仓库根目录执行（Windows 可将 `python3` 替换为 `python`）：

```powershell
git clone https://github.com/xiaozhou26/Cloaksession.git
cd Cloaksession
python scripts/build.py --prepare
cd desktop
go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 dev -skipbindings
```

`--prepare` 用锁定的 npm 依赖构建前端 assets，放置 Playwright/companion 资源与图标，供 Wails dev 使用。修改 Go 代码由 Wails dev 重编译；资源/依赖更改后重新执行 prepare。Vite 位于 `desktop/frontend`；只运行 Vite 不提供 Go 后端。

### 构建与发布

从仓库根目录执行：

```powershell
python scripts/build.py                         # 本机 Go 编译并放置资源
python scripts/build.py --test --package        # Go race 测试、编译并打包
python scripts/build.py --package --universal   # 仅 macOS，Go 双架构
python scripts/build.py --check-version --tag v1.4.0
```

脚本运行前端 `npm ci --legacy-peer-deps`、Playwright runtime `npm ci --omit=dev` 和 `npm run build`，再执行固定 CLI `go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 build`。`--test` 在编译前运行 Go race 测试。macOS universal 使用 `-platform darwin/universal` 并通过 `lipo` 校验主程序双架构；没有第二个业务后端可执行文件。这些是锁定依赖的可重复构建步骤，不承诺逐字节可复现。

| 平台 | 可运行输出与资源 | `desktop/build/dist/` 发布附件 |
| --- | --- | --- |
| Windows x64 | `desktop/build/bin/Cloaksession.exe`、`resources/playwright/`、`resources/companion/` | `Cloaksession-1.4.0-windows-amd64-setup.exe`（NSIS） |
| macOS universal | `desktop/build/bin/Cloaksession.app/Contents/MacOS/Cloaksession` 和 `Contents/Resources/{playwright,companion}/` | `Cloaksession-1.4.0-macos-universal.zip` |
| Linux x64 | `desktop/build/bin/Cloaksession`、`resources/playwright/`、`resources/companion/` | `Cloaksession-1.4.0-linux-amd64.tar.gz` |

Playwright 资源源目录为 `desktop/resources/playwright`，只包含 bridge、npm manifest/lock 和生产 `playwright-core` 依赖；companion 源目录为 `desktop/resources/companion`。打包不会携带已删除的 SDK/vendor 或旧核心可执行文件。每次执行 `npm ci` 清理旧依赖，替换目标资源树；不要只复制主程序。

Windows 安装器在写入前检查窗口及文件锁，提示完全退出所有浏览器会话和管理应用后 **Retry**，**Cancel** 在写入前中止，不强杀进程。升级清理旧版 helper 和旧资源路径，不删除独立用户数据，附件保留更新器识别的 **`-setup.exe`** 后缀。Linux 解压后运行 `Cloaksession/Cloaksession`，需要匹配 GTK/WebKitGTK 库。macOS ZIP 解压后可将 `.app` 放入 Applications；复制资源后重新 ad-hoc 签名，尚无 Developer ID 公证。Windows 未配置 Authenticode。

[build.yml](.github/workflows/build.yml) 执行 Python 包装契约、Node bridge、前端单元、TypeScript/Vite、Go race 和完整 Playwright UI 测试（当前套件 68 项，含原有 52 项及新增错误处理回归），随后在 Windows、macOS universal、Ubuntu 22.04 编译；没有 Rust 工具链、缓存或测试步骤。UI 测试模拟桌面桥，不是 Go 后端验收。安装 Chrome 后显式设置 `CLOAKSESSION_TEST_BROWSER` 并运行 `TestServiceRealBrowser`，验证 Go 服务通过 Playwright 启动浏览器、CDP 端点和关闭，不依赖旧核心进程。

[release.yml](.github/workflows/release.yml) 在版本标签上验证 Wails productVersion、前端及 runtime manifest/lock、Wails Go pin；复用全部测试与三平台打包，全部成功后才统一创建 Release，附三个附件及 `SHA256SUMS.txt`。既有 Release 不自动覆盖。**1.3.0 的发布已暂停，不应把旧标签/旧包视为纯 Go 发布；下一目标为 v1.4.0，需完成纯 Go 回归后再由维护者决定发布。** 本次迁移不创建或推送标签。

## 本地数据与备份

Go 存储层沿用数据目录标识 **`com.cloaksession.browser`** 和 `profiles.db` 文件。迁移前必须备份并验证旧库兼容，不因实现语言切换清空用户数据：

- Windows：`%LOCALAPPDATA%\com.cloaksession.browser\`。
- macOS：`~/Library/Application Support/com.cloaksession.browser/`。
- Linux：`$XDG_DATA_HOME/com.cloaksession.browser/`，未设置时使用 `~/.local/share/com.cloaksession.browser/`。
- `CLOAKSESSION_DATA_DIR` 可显式覆盖数据根目录（开发/测试时可用独立临时目录）；建议使用明确的绝对路径，避免误读其他目录。

升级前关闭所有浏览器 Profile 和管理应用，备份整个旧数据目录；保留 `profiles.db`、`settings.json`、`mcp-token`、`profiles/` 和 `extensions/`。安装/解压不会主动清空这些文件，卸载器也不会删除此目录。若旧版本因自定义路径或异常回退使用了其他位置，先备份，再显式指定原位置，不要在应用运行时复制数据库。

| 文件或目录 | 内容 |
| --- | --- |
| `profiles.db` | SQLite Profile 配置，包含代理、指纹和扩展引用；启用 WAL，运行时可能伴有 `-wal` / `-shm` 文件 |
| `profiles/` | 每个 Profile 的浏览器用户数据；CloakBrowser 和 Chromix 使用引擎子目录，CFT 使用 Profile 根目录 |
| `settings.json` | 应用设置，JSON 键使用 camelCase |
| `mcp-token` | MCP Bearer 凭据，在应用启动时读取或生成 |
| `extensions/` | 共享的解包扩展文件 |
| `companion/` | 由 companion 资源提供的 Chrome Web Store 辅助扩展 |

界面的部分偏好保存在 WebView 的 localStorage；Wails 与旧 Tauri WebView 的 origin / 存储可能不同，这些界面偏好不保证自动迁移，不能用它们是否重置判断 Profile 数据丢失。MCP 活动历史只保存在内存中，最多保留 500 条，应用重启后清空。

- **本地 SQLite、设置文件和令牌并未由应用整体加密**；代理凭据会随 Profile 配置写入数据库。浏览器自身如何保护 Cookie、密码取决于内核和操作系统。请保护好整个数据目录。
- 备份前关闭 Profile 和应用，避免正在写入的数据库、会话文件造成不一致。`.mzar` 是按 Profile 的加密导出，不是整个应用设置或 MCP 令牌的备份。
- 导出时设置口令并妥善保存；导入需要同一口令。归档复制浏览器文件，但不保证跨机器或跨操作系统后仍能解密登录凭据。
- 手动编辑 `settings.json` 前先退出应用；设置存在进程内缓存，修改文件不会热重载。

## MCP 连接

MCP 服务在 Wails Go 主进程中运行，无需另外启动核心或 MCP 可执行程序。默认随应用启动，仅绑定 **`127.0.0.1:7777`**。

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
- Go 服务的 `system_info` 返回实际配置并启动的 MCP 地址；关闭服务时不应显示可用端点。端口冲突会导致初始化失败，自定义端口后仍应通过 `/healthz` 和后端日志确认监听。
- 合法的 `mcp-token` 会跨重启复用；文件缺失或格式无效时重新生成。更换令牌后需重启应用，并更新所有客户端。不要分享令牌，也不要把服务通过端口转发暴露给不受信任的网络。
- `401` 通常是 Bearer 缺失或不匹配，`403` 是 Host 不在允许列表，连接失败则检查应用是否运行、是否启用 MCP 以及端口占用。

默认工具目录不公开 `cdp_send`。如确需低层 CDP，可在启动应用前设置环境变量 `MULTIZEN_MCP_ALLOW_RAW_CDP=1`；启用后仍有方法和 URL 参数限制。该开关只控制 `cdp_send`，并不会关闭其他工具内部使用的 CDP 或 `evaluate_js`。连接的客户端可以读取页面、操作 Cookie、创建和删除环境，请只交给可信客户端；活动记录也不应视为全面脱敏的审计日志。

## 项目结构

仓库以 `desktop/` Go module 为后端，前端仍使用 React / TypeScript：

| 路径 | 职责 |
| --- | --- |
| [`desktop`](desktop) | Wails 入口、Go 应用服务、绑定、原生窗口/对话框和事件 |
| [`desktop/internal`](desktop/internal) | Go 存储、浏览器进程/CDP/代理、MCP 和扩展等业务包 |
| [`desktop/frontend`](desktop/frontend/src) | React 19、TypeScript、Vite 6、Tailwind CSS 4 |
| [`desktop/resources/playwright`](desktop/resources/playwright) | 本地浏览器 Node/Playwright bridge 与锁定依赖 |
| [`desktop/resources/companion`](desktop/resources/companion) | Chrome Web Store 辅助扩展 |
| [`scripts/build.py`](scripts/build.py) | 版本核对、npm 安装、Go 构建和原生包装 |

UI 经 Wails binding 直接进入 Go 服务；MCP HTTP 进入同一应用业务层。SQLite 使用纯 Go 驱动，Chromix 路径保留 Node/Playwright bridge，Node 和浏览器仍是外部进程。这里的“全 Go”指应用后端不再有 Rust，不表示 UI、Node bridge 或系统 WebView 改成 Go，也不表示应用包含浏览器内核。

## 检查与测试

从仓库根目录执行：

```powershell
python scripts/build.py --check-version --tag v1.4.0
python -m unittest discover -s scripts -p "test_*.py"
npm --prefix desktop/frontend ci --legacy-peer-deps
npm --prefix desktop/resources/playwright ci
npm --prefix desktop/resources/playwright test
node --experimental-strip-types desktop/frontend/src/lib/chromixFingerprint.test.mjs
npm --prefix desktop/frontend run build
cd desktop
go test -race -count=1 -mod=readonly -v ./...
cd frontend
npx playwright install chrome
npx playwright test --config playwright.config.ts
```

Go 测试前必须先生成嵌入的前端 assets；Linux 还需安装 Wails WebKitGTK 依赖。Node bridge 测试、前端单元与 UI 回归不证明 Go 存储、MCP、扩展或真实浏览器生命周期已经通过。真实浏览器入口现为 `desktop/service_test.go` 的 `TestServiceRealBrowser`，设置 `CLOAKSESSION_TEST_BROWSER` 为本地 Chrome 可执行文件的绝对路径后运行 `go test -race -count=1 -mod=readonly -v -run '^TestServiceRealBrowser$' .`（工作目录 `desktop`）；未设置会跳过。CI 安装 Chrome 后明确运行该入口，不再使用旧 Rust sidecar 环境变量或 Cargo ignored 测试。

真实浏览器/指纹、历史 SQLite 迁移、`.mzar` 兼容、安装升级和进程清理仍须针对纯 Go 实现验证。[验收记录](docs/ACCEPTANCE.md) 区分本次执行结果与历史 1.3.0 证据；不能复用旧包的通过状态。

## 许可证

Copyright 2026 Cloaksession contributors.

Cloaksession 采用 **Apache License 2.0**，完整条款见仓库 [LICENSE](LICENSE)，也可阅读 [Apache 官方许可证文本](https://www.apache.org/licenses/LICENSE-2.0)。

第三方依赖、浏览器内核（包括 CloakBrowser、Chromium / Chrome for Testing）、安装的扩展、图标及其他第三方资源仍受各自许可证和使用条款约束。本仓库的 Apache-2.0 声明不替代这些条款，也不授予第三方商标权；重新分发时请核查对应版本及所附的版权、许可证和 NOTICE 要求。
