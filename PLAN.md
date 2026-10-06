# Cloaksession 1.4.0 迁移记录

## 当前要求

- 桌面框架使用 Wails。
- 全部应用后端迁移到 Go，移除 Rust 源码、Cargo 工具链及核心辅助进程。
- 使用直接 Playwright 接入替代 Chromix SDK。
- 指纹界面提供随机、固定 seed 和自定义选择，将完整参数放入高级设置。
- 完成测试后通过 GitHub tag 构建并发布新版。

## 实现结构

- `desktop/internal/store`：纯 Go SQLite、兼容旧数据库、设置、指纹和加密归档。
- `desktop/internal/browser`：浏览器进程、CDP 操作、代理与 Playwright 生命周期。
- `desktop/internal/extensions`：扩展安装、缓存、图标与伴随扩展信号。
- `desktop/internal/mcp`：本地认证 HTTP MCP、工具目录与活动事件。
- `desktop/service.go`：Wails 直接调用的 Go 服务与生命周期协调。
- `desktop/resources/playwright`：直接 `playwright-core` 桥接，保留 Node 适配器及 React 前端。

旧 `chromix` / `chromixOptions` 设置键、引擎数据目录、MCP token 与 `.mzar` 格式保持兼容。随机模式每次启动生成新 seed；固定模式保存精确十进制 seed；自定义模式允许选择平台、语言、时区、屏幕和硬件参数。

## 发布状态

`v1.3.0` 曾推送以触发 Wails + Rust 核心构建，随后因新增纯 Go 要求取消工作流。`v1.4.0` 为完整 Go 后端版本。实际发布结果以 GitHub Actions 和 Releases 为准；本地验收记录见 `docs/ACCEPTANCE.md`。
