# Chromix 内核与前端指纹配置

## 用户确定的需求

- 将 https://github.com/xiaozhou26/Chromix 作为可选浏览器内核启动。
- 前端直接编辑官方 Node SDK 支持的完整自定义指纹，使用分组表单和原始参数编辑入口。
- 保存的配置进入真实 SDK 启动流程，每个 Profile 独立保存配置。

## 实现决定

- 保留既有 CloakBrowser 默认值与 CFT 路径，增加 `chromix` 引擎。
- 使用官方 `@xiaoxiaofeihh/chromix` 和 `playwright-core`，通过 Node 子进程持有持久化浏览器上下文。SDK 依赖锁版本，打包资源包含 SDK；Node 运行环境由用户安装或在设置指定路径。
- 应用设置 `chromix` 保存 `nodePath`、`options`、`environment`。Profile 的 `chromixOptions` 保存独立的 SDK 选项，SQLite 自动迁移旧数据。
- 启动配置按全局选项 → Profile 顶层覆盖合并；数组和嵌套对象整体替换，保留 `false`、`null`、字符串种子和未知 SDK 字段。
- 前端提供完整公开指纹参数目录、字段类型和单位，并保留 JSON / 原始 args 编辑。设置页面用于全局默认值，创建/编辑 Profile 的 Chromix fingerprint 页面用于独立配置。
- Chromix 默认采用 SDK 原生配置，旧 FingerprintConfig 的 Windows 设备模板与 CloakBrowser 特有单位不自动转换。Chromix 参数通过独立表单编辑。
- SDK 负责代理、GeoIP、持久化种子、下载缓存与其余官方选项。管理应用负责 Profile 生命周期、进程通信和本地 CDP 连接。CDP 调试端口由管理应用保留。
- Chromix 的 CDP bootstrap 跳过旧引擎的指纹和语言覆盖，确保用户 SDK 参数保持有效。
- 前端支持 JSON 可序列化选项；函数与回调属于用户脚本接口。SDK `humanize` 作用于 SDK 持有的 Playwright 对象，既有 MCP 操作继续使用项目自身的 CDP 驱动。
- `devicePool`、代理、字体和原生指纹的实际适用范围遵循上游源码；旧版可执行文件不会通过更新管理界面获得新增原生实现。

## 验收清单

- [x] 核对官方 SDK 与公开指纹参数目录的版本和覆盖。
- [x] 全局设置和 Profile 配置持久化、迁移、导入、清空与未知字段回归。
- [x] SDK 桥接参数完整透传、失败清理、正常关闭和下载开关回归。
- [x] 前端创建和编辑 Profile、全局设置、切换引擎及 JSON 错误处理浏览器验证。
- [x] 桌面与移动视口的表单操作和共享页面回归。
- [x] Rust 测试、前端构建与 Node 桥接测试。
- [x] 记录真实 Chromix 内核的本地启动证据或明确的未验证项。

实现与测试证据已更新到 `docs/ACCEPTANCE.md`；本文件不代表发布或部署成功。
