# Cloaksession

Cloaksession 是一个基于 **Wails v2（Go）+ Rust + React** 的桌面浏览器环境管理工具。它为每个 Profile 管理独立的浏览器用户数据目录、代理和指纹配置，支持手动使用，也通过本地 **MCP（Model Context Protocol）** 接口供 AI 客户端操作浏览器。

本仓库提供管理应用及浏览器启动、Chrome DevTools Protocol（CDP，浏览器调试协议）控制逻辑，**不包含浏览器内核**。当前默认引擎为 CloakBrowser，另有 Chrome for Testing（CFT）兼容路径，并可通过 Node / Playwright bridge 启动本地 Chromix。以下以 Windows 为主要使用和开发环境；发布工作流包含其他平台，但不代表所有功能已经过跨平台验证。

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

运行资源位于 `crates/desktop-core/resources/chromix/`，使用锁定的 `playwright-core`，不再捆绑 Chromix Node SDK/vendor。构建会执行 `npm ci`；运行需要外部 Node.js 20+ 和本地浏览器。固定/自定义 seed 必须是 `1` 至 `18446744073709551615` 的十进制字符串，不能用 JavaScript Number 保存；random 在每次启动时重新生成，不复用旧 seed。Fixed 的可复现性需保持相同内核和其他配置，seed 不构成匿名或唯一设备保证。

注意以下边界：

- 三种引擎都需要本地浏览器；Chromix bridge 仅使用已有可执行文件，`playwright-core` 不会自动下载内核。
- 指纹字段可以保存，并不表示所有字段在每个内核、页面和平台都能一致生效。设备预设只是配置，不等于虚拟机或真实硬件仿真。
- CDP 自动化、页面脚本覆盖和代理启动参数都不构成不可检测、匿名或防泄漏保证。请在实际浏览器版本和网络环境中自行验证。
- CDP 驱动的 `safe_cdp` 检查尚未拦截 chromiumoxide 的自动域启用；不能将其视为完整的保护层，某些 CloakBrowser 构建可能存在兼容性或崩溃风险。
- 默认配置含 Windows 设备信息和 `C:\Windows\Fonts` 字体路径，其他平台需要自行调整；CFT 路径也不能视为与 CloakBrowser 功能等价。

### 存储配额单位

`FingerprintConfig.storageQuota` 的 JSON 和数据库持久化单位是**字节**；界面的 **Storage quota (MB)** 使用十进制 MB，即 `1 MB = 1_000_000` 字节。

默认值为 `2_000_000_000` 字节（十进制 2 GB），界面显示 `2000` MB。CloakBrowser 启动参数也按**字节**传入，对应 **`--fingerprint-storage-quota=2000000000`**，不会再除以 `1_000_000` 或设置隐式下限。留空或设为零不传此参数，使用引擎默认值。

Chromix 的公开 quota flag 使用 **MiB**；bridge 将 Profile 中的字节数向上换算，例如 `2147483648` 字节转换为 `--fingerprint-storage-quota=2048`。Advanced 中直接填写的原始 flag 保持原值。随机和固定 seed 模式默认不传 Profile quota。详见 [Playwright runtime 契约](crates/desktop-core/resources/chromix/README.md)。

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

桌面入口已迁移到 `desktop/`，Go module 为 `github.com/xiaozhou26/Cloaksession/desktop`。Rust workspace 保留浏览器业务，桌面后端包和可执行文件为 `desktop-core`。当前应用版本为 **1.3.0**，Wails CLI 和 Go 依赖均固定为 **v2.11.0**。

准备以下环境：

- Git、Rust stable / Cargo、Go **1.23.12**（CI 显式固定此版本，本机已验证）、Python **3.11+**。新版 Go 未必兼容固定 Wails 的分析工具；若遇到 `package "context" without types`，使用 Go 1.23.12 或设置 `GOTOOLCHAIN=go1.23.12`。
- Node.js **22** 和 npm（Chromix 运行时至少需要 Node 20；安装包不内置 Node 或浏览器内核）。
- Windows：MSVC C++ Build Tools、Windows SDK、WebView2 Runtime；生成安装包还需 NSIS 3（`choco install nsis -y`）。应用内嵌 WebView2 bootstrapper，可在缺失 Runtime 时提示安装，安装 Runtime 仍需网络。
- macOS：Xcode Command Line Tools；universal 构建需要 arm64 / x86_64 Rust target，脚本会调用 `rustup target add`。
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

`--prepare` 使用 `npm ci --legacy-peer-deps` 安装前端锁定依赖、`npm ci --omit=dev` 安装 Chromix runtime，并编译和放置 Rust sidecar。修改 Rust 代码后需重新执行；Wails dev 只自动重编译 Go / 前端。Vite 前端位于 `desktop/frontend`；只运行 Vite 不会提供 Go / Rust 后端。

### 构建与发布

从仓库根目录执行：

```powershell
python scripts/build.py                         # 本机编译并放置完整 runtime
python scripts/build.py --package               # 本机编译并创建发布包
python scripts/build.py --package --universal   # 仅 macOS，双架构 Go + Rust
python scripts/build.py --check-version --tag v1.3.0
```

构建脚本重新执行锁定的 npm 安装和 `cargo build --release --locked -p desktop-core`（显式指定 host target，避免 Cargo 配置意外交叉编译），运行前端构建，再通过固定版本的 `go run .../wails@v2.11.0 build` 构建 Go 桌面。macOS universal 同时编译两个 Rust target 并用 `lipo` 合并，Wails 使用 `-platform darwin/universal`；脚本校验两个可执行文件均包含双架构。尊重 Cargo metadata 返回的 target 目录，包括自定义 `CARGO_TARGET_DIR`。这些是锁定依赖的可重复构建步骤，不承诺逐字节可复现。

| 平台 | 可运行输出及 runtime 布局 | `desktop/build/dist/` 发布附件 |
| --- | --- | --- |
| Windows x64 | `desktop/build/bin/Cloaksession.exe`、同目录 `desktop-core.exe`、`resources/chromix/` | `Cloaksession-1.3.0-windows-amd64-setup.exe`（NSIS） |
| macOS universal | `desktop/build/bin/Cloaksession.app/Contents/MacOS/{Cloaksession,desktop-core}` 和 `Contents/Resources/chromix/` | `Cloaksession-1.3.0-macos-universal.zip` |
| Linux x64 | `desktop/build/bin/Cloaksession`、同目录 `desktop-core`、`resources/chromix/` | `Cloaksession-1.3.0-linux-amd64.tar.gz` |

资源保留兼容路径 `resources/chromix`，仅包含 Node bridge、npm manifest/lock 及生产 `node_modules/playwright-core`。每次构建执行 `npm ci --omit=dev` 清理旧依赖，打包仅复制清单文件与已校验的生产依赖，替换旧资源树，不携带已删除的 SDK/vendor；不要只复制主程序。Windows 安装器先检查窗口及主程序/sidecar 的独占写访问；存在运行中进程或文件锁时提示完全退出并点击 **Retry**，**Cancel** 在写文件前中止，不强杀进程。程序和 runtime 更新保留独立用户数据，文件名保留更新器识别的 **`-setup.exe`** 后缀。Linux 解压后运行 `Cloaksession/Cloaksession`，需要相应 GTK / WebKitGTK 运行库。macOS 解压后可将 `.app` 放入 Applications；脚本加入 runtime 后重新 ad-hoc 签名，**尚无 Developer ID 签名或公证**，Windows 也未配置 Authenticode，系统可能提示未受信任发行者。

[build.yml](.github/workflows/build.yml) 在 main / PR 上执行 Python 包装契约、Rust workspace、Chromix bridge、前端指纹目录、TypeScript/Vite、Playwright 和 Go race 测试；Linux 测试 job 显式构建 debug `desktop-core`，设置绝对路径 `CLOAKSESSION_TEST_CORE` / `CLOAKSESSION_RESOURCE_DIR`，让 `TestRealCoreRoundTrip` 实际运行而不是跳过，然后在 Windows、macOS universal、Ubuntu 22.04 编译并放置完整 runtime。[release.yml](.github/workflows/release.yml) 仅从 `v*` 标签发布（手动执行也必须选择标签）：先比对标签、Rust manifest/lock、前端 manifest/lock 和 Wails productVersion，并校验 Wails Go 依赖固定版本；复用同一测试与三平台构建流程，**全部成功后**才统一创建 GitHub Release，附三个安装/归档包及 `SHA256SUMS.txt`。已有同名 Release 不会被自动覆盖。

发布前同步以上版本文件并提交源码，再推送对应标签（例如 `v1.3.0`）。不要将其他 Rust library crate 或第三方 runtime 依赖的独立版本强制改成桌面版本。CI 配置不是发布成功或真实内核验收记录；各平台安装、升级与浏览器行为仍需运行验证。

## 本地数据与备份

Wails 桌面显式沿用旧版 Tauri 的数据目录标识 **`com.cloaksession.browser`**，不会因框架切换创建新的 Profile 数据库：

- Windows：`%LOCALAPPDATA%\com.cloaksession.browser\`。
- macOS：`~/Library/Application Support/com.cloaksession.browser/`。
- Linux：`$XDG_DATA_HOME/com.cloaksession.browser/`，未设置时使用 `~/.local/share/com.cloaksession.browser/`。
- `CLOAKSESSION_DATA_DIR` 可显式覆盖数据根目录（开发/测试时可用独立临时目录）；路径不可用会报错，不静默退回当前目录。

升级前关闭所有浏览器 Profile 和管理应用，备份整个旧数据目录；保留 `profiles.db`、`settings.json`、`mcp-token`、`profiles/` 和 `extensions/`。安装/解压不会主动清空这些文件，卸载器也不会删除此目录。若旧版本因自定义路径或异常回退使用了其他位置，先备份，再显式指定原位置，不要在应用运行时复制数据库。

| 文件或目录 | 内容 |
| --- | --- |
| `profiles.db` | SQLite Profile 配置，包含代理、指纹和扩展引用；启用 WAL，运行时可能伴有 `-wal` / `-shm` 文件 |
| `profiles/` | 每个 Profile 的浏览器用户数据；CloakBrowser 和 Chromix 使用引擎子目录，CFT 使用 Profile 根目录 |
| `settings.json` | 应用设置，JSON 键使用 camelCase |
| `mcp-token` | MCP Bearer 凭据，在应用启动时读取或生成 |
| `extensions/` | 共享的解包扩展文件 |
| `companion/` | 从应用内嵌资源写出的 Chrome Web Store 辅助扩展 |

界面的部分偏好保存在 WebView 的 localStorage；Wails 与旧 Tauri WebView 的 origin / 存储可能不同，这些界面偏好不保证自动迁移，不能用它们是否重置判断 Profile 数据丢失。MCP 活动历史只保存在内存中，最多保留 500 条，应用重启后清空。

- **本地 SQLite、设置文件和令牌并未由应用整体加密**；代理凭据会随 Profile 配置写入数据库。浏览器自身如何保护 Cookie、密码取决于内核和操作系统。请保护好整个数据目录。
- 备份前关闭 Profile 和应用，避免正在写入的数据库、会话文件造成不一致。`.mzar` 是按 Profile 的加密导出，不是整个应用设置或 MCP 令牌的备份。
- 导出时设置口令并妥善保存；导入需要同一口令。归档复制浏览器文件，但不保证跨机器或跨操作系统后仍能解密登录凭据。
- 手动编辑 `settings.json` 前先退出应用；设置存在进程内缓存，修改文件不会热重载。

## MCP 连接

MCP 服务嵌入桌面应用管理的 Rust `desktop-core` 进程运行，无需另外启动一个 `mcp-server` 可执行程序。默认随应用启动，仅绑定 **`127.0.0.1:7777`**。

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

仓库包含 Rust workspace 和独立的 Wails Go module：

| 路径 | 职责 |
| --- | --- |
| [`crates/multizen-core`](crates/multizen-core/src) | Profile、指纹、设置、错误等共享数据类型；名称保留历史标识 |
| [`crates/profile-manager`](crates/profile-manager/src) | SQLite 持久化、迁移、默认指纹和 Profile 数据目录 |
| [`crates/settings-store`](crates/settings-store/src) | JSON 设置加载、默认值归一化和进程内缓存 |
| [`crates/browser-launcher`](crates/browser-launcher/src) | 浏览器进程、启动参数、版本检测、代理桥接、出口查询和会话恢复 |
| [`crates/behavioral`](crates/behavioral/src) | 鼠标路径、按键间隔和滚动曲线生成 |
| [`crates/cdp-driver`](crates/cdp-driver/src) | CDP 会话、目标页面、指纹 bootstrap、页面操作及低层请求 |
| [`crates/mcp-server`](crates/mcp-server/src) | MCP 工具元数据、参数 schema、JSON-RPC / HTTP、认证与活动记录 |
| [`crates/desktop-core`](crates/desktop-core/src) | Rust sidecar、命令分发、内嵌 MCP、归档、扩展和更新集成 |
| [`desktop`](desktop) | Wails v2 Go 入口、原生窗口/对话框、sidecar 生命周期、绑定与事件桥接 |
| [`desktop/frontend`](desktop/frontend/src) | React 19、TypeScript、Vite 6、Tailwind CSS 4 界面 |
| [`scripts/build.py`](scripts/build.py) | 锁定依赖安装、双架构编译、runtime 放置与平台打包 |

UI 经 Wails Go binding 及 stdio JSON RPC 进入 Rust sidecar 的应用驱动，原生对话框和事件由 Go 桥接；MCP HTTP 经工具分发进入同一 Rust 驱动；驱动协调 Profile 持久化、浏览器启动器和 CDP 会话。MCP 层不内置大模型，模型和客户端由使用者选择。

## 检查与测试

在开发依赖齐备后，从仓库根目录执行：

```powershell
python scripts/build.py --check-version
python -m unittest discover -s scripts -p "test_*.py"
npm --prefix desktop/frontend ci --legacy-peer-deps
npm --prefix crates/desktop-core/resources/chromix ci
npm --prefix crates/desktop-core/resources/chromix test
node --experimental-strip-types desktop/frontend/src/lib/chromixFingerprint.test.mjs
npm --prefix desktop/frontend run build
cargo check --workspace --locked
cargo test --workspace --locked
cd desktop
go test -mod=readonly ./...
cd frontend
npx playwright install chrome
npx playwright test --config playwright.config.ts
```

前端构建包含 TypeScript 检查，`npm test` 运行 Playwright；浏览器 UI 测试使用模拟桌面桥，不等同于安装后的 Go/Rust 端到端验收。Go 测试前必须先构建嵌入的前端 assets。Rust 测试覆盖 Profile / 设置持久化、启动参数、代理桥接、行为算法、CDP 辅助逻辑和 MCP 工具与传输。Chromix 测试包括 bridge 选项和进程契约；真实二进制 smoke 需要设置 `CHROMIX_TEST_BINARY`，按目标内核重新核对硬件与存储字段。

需要真实浏览器的测试默认标记为忽略，仅加环境变量还不够，必须同时传 `-- --ignored`：

- `browser-launcher/tests/driver.rs`：设置 `RUN_CDP_INTEGRATION=1` 和指向实际内核的 `MULTIZEN_TEST_BINARY`，运行 `cargo test -p browser-launcher --test driver -- --ignored`。该测试会自行启动、关闭浏览器。
- `cdp-driver/tests/integration.rs`：先准备可连接的 CDP 浏览器，设置 `RUN_CDP_INTEGRATION=1`，必要时设置 `MULTIZEN_TEST_CDP`（默认 `http://127.0.0.1:9222`），运行 `cargo test -p cdp-driver --test integration -- --ignored`。测试会访问 `https://example.com`。

真实 Go/Rust RPC 集成（不启动浏览器）在 Linux CI 必跑；本地可从仓库根目录执行：

```bash
cargo build --locked -p desktop-core
export CLOAKSESSION_TEST_CORE="$PWD/target/debug/desktop-core"
export CLOAKSESSION_RESOURCE_DIR="$PWD/crates/desktop-core/resources"
cd desktop
GOTOOLCHAIN=go1.23.12 go test -race -count=1 -mod=readonly -v ./...
```

先构建前端 assets；Windows 将可执行文件改为 `desktop-core.exe` 并使用对应环境变量语法。此测试覆盖真实 sidecar 启动、命令往返、Profile CRUD、原生对话框桥接的归档导入/导出和关闭，仍不等于真实浏览器 GUI 验收。

这些变量和部分内部包名仍使用 `MULTIZEN_*`，请按源码中的名称设置。构建及发布工作流会执行锁定的完整 Rust workspace 测试（含 quota 回归）及前端/Go 测试，但不执行需要真实内核的 ignored 测试。

## 许可证

Copyright 2026 Cloaksession contributors.

Cloaksession 采用 **Apache License 2.0**，完整条款见仓库 [LICENSE](LICENSE)，也可阅读 [Apache 官方许可证文本](https://www.apache.org/licenses/LICENSE-2.0)。

第三方依赖、浏览器内核（包括 CloakBrowser、Chromium / Chrome for Testing）、安装的扩展、图标及其他第三方资源仍受各自许可证和使用条款约束。本仓库的 Apache-2.0 声明不替代这些条款，也不授予第三方商标权；重新分发时请核查对应版本及所附的版权、许可证和 NOTICE 要求。
