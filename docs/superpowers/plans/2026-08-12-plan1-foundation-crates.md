# MultiZen Rust 重写 — Plan 1：地基层（multizen-core + profile-manager + settings-store）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在新仓库 `multizen-browser-rs` 中搭起 Rust workspace 骨架，并实现纯 Rust、不依赖浏览器的三个基础 crate：`multizen-core`（共享类型/错误/serde schema）、`profile-manager`（rusqlite + 迁移 + CRUD）、`settings-store`（JSON 应用设置）。

**Architecture:** Cargo workspace，三个 crate 按依赖单向引用：`multizen-core` 被另两个依赖。所有类型用 `serde` 派生，与前端 TS 类型字段名 1:1 对齐（camelCase），保证未来 Tauri commands 返回值能被 React 直接消费。profile-manager 用 `rusqlite` 静态链接 SQLite，schema 与现有 TS 版 `ProfileManager` 表结构一致（列名 snake_case，JSON 列存复合字段），支持 idempotent 迁移。所有逻辑用纯单元测试覆盖，不碰真实浏览器。

**Tech Stack:** Rust 1.80+、`serde` + `serde_json`、`rusqlite`（bundled feature）、`thiserror`、`uuid`、`chrono`、`tempfile`（测试）。

## Global Constraints

- 新仓库根目录：`multizen-browser-rs/`（由执行者在第一个任务 `git init`）。
- Rust edition 2021，workspace 用 `resolver = "2"`。
- 所有 serde struct 默认 `#[serde(rename_all = "camelCase")]`，与现有 TS `packages/types/src/index.ts` 字段名逐一对齐（已在 spec 中列出）。
- 列名沿用 TS 版 `ProfileManager` 的 snake_case：`id, name, notes, tags, proxy, fingerprint, data_dir, created_at, updated_at, last_opened_at, proxy_country, extensions, icon, start_url, search_provider`。
- 复合字段（`tags`, `proxy`, `fingerprint`, `extensions`）在 DB 中存 JSON 文本，与 TS 版一致。
- 时间戳用 ISO 8601 字符串（`chrono::DateTime<Utc>` 序列化为 RFC3339），与 TS 版 `new Date().toISOString()` 兼容。
- ProfileId 用 `uuid` v4 字符串。
- 错误统一走 `multizen-core::MultizenError`（thiserror），各 crate `?` 上抛。
- 每个任务结束 `git commit`，commit message 前缀 `feat:` / `test:` / `chore:`。

## File Structure

```
multizen-browser-rs/
├── Cargo.toml                          # workspace 根
├── crates/
│   ├── multizen-core/
│   │   ├── Cargo.toml
│   │   └── src/
│   │       ├── lib.rs                   # crate 入口，re-export
│   │       ├── error.rs                 # MultizenError
│   │       ├── profile.rs              # Profile / ProfileSummary / CreateProfileInput / UpdateProfileInput / ProxyConfig / DeviceFamily / ClientHints / FingerprintConfig / ExtensionConfig / LaunchedProfile / McpToolError
│   │       └── settings.rs             # AppSettings / BrowserEngine
│   ├── profile-manager/
│   │   ├── Cargo.toml
│   │   ├── src/
│   │   │   ├── lib.rs                   # re-export
│   │   │   ├── manager.rs               # ProfileManager struct + CRUD
│   │   │   ├── migrate.rs               # migrate() + idempotent ALTER
│   │   │   ├── row.rs                   # ProfileRow + rowToProfile + normalizeExtensions
│   │   │   └── fingerprint.rs           # default_fingerprint(seed)
│   │   └── tests/
│   │       └── manager.rs               # 集成测试（用 tempfile 建 DB）
│   └── settings-store/
│       ├── Cargo.toml
│       ├── src/
│       │   ├── lib.rs                   # SettingsStore + default_settings_path
│       │   └── defaults.rs              # DEFAULTS + 校验
│       └── tests/
│           └── store.rs
```

职责边界：
- `multizen-core`：**只含类型和错误**，不含任何 IO 逻辑。所有其他 crate 的公共签名引用这里的类型。
- `profile-manager`：拥有 SQLite 连接、迁移、CRUD、磁盘 profile 目录管理。`ProfileManager` 是唯一入口。
- `settings-store`：拥有 JSON 文件读写、缓存、字段校验。`SettingsStore` 是唯一入口。

---

### Task 1: 初始化 Cargo workspace 与三个 crate 骨架

**Files:**
- Create: `multizen-browser-rs/Cargo.toml`
- Create: `multizen-browser-rs/crates/multizen-core/Cargo.toml`
- Create: `multizen-browser-rs/crates/multizen-core/src/lib.rs`
- Create: `multizen-browser-rs/crates/profile-manager/Cargo.toml`
- Create: `multizen-browser-rs/crates/profile-manager/src/lib.rs`
- Create: `multizen-browser-rs/crates/settings-store/Cargo.toml`
- Create: `multizen-browser-rs/crates/settings-store/src/lib.rs`
- Create: `multizen-browser-rs/.gitignore`

**Interfaces:**
- Produces: 空 workspace 可编译，三个 crate 互相可见（`multizen-core` 可被另两个 `path` 依赖）。

- [ ] **Step 1: 建仓库与 workspace 根 Cargo.toml**

执行（在 `D:\Python\multizen-browser` 下，新仓库作为子目录，便于先在旧仓库里写 plan，后续可整体迁移）：

```bash
mkdir -p multizen-browser-rs/crates
cd multizen-browser-rs
git init
```

写 `multizen-browser-rs/Cargo.toml`：

```toml
[workspace]
resolver = "2"
members = [
    "crates/multizen-core",
    "crates/profile-manager",
    "crates/settings-store",
]
```

- [ ] **Step 2: multizen-core 骨架**

写 `crates/multizen-core/Cargo.toml`：

```toml
[package]
name = "multizen-core"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
thiserror = "1"
```

写 `crates/multizen-core/src/lib.rs`：

```rust
pub mod error;
pub mod profile;
pub mod settings;

pub use error::MultizenError;
```

- [ ] **Step 3: profile-manager 骨架**

写 `crates/profile-manager/Cargo.toml`：

```toml
[package]
name = "profile-manager"
version = "0.1.0"
edition = "2021"

[dependencies]
multizen-core = { path = "../multizen-core" }
rusqlite = { version = "0.32", features = ["bundled"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
uuid = { version = "1", features = ["v4"] }
chrono = { version = "0.4", features = ["serde"] }
thiserror = "1"

[dev-dependencies]
tempfile = "3"
```

写 `crates/profile-manager/src/lib.rs`（占位，下个 task 填充）：

```rust
pub mod manager;
pub mod migrate;
pub mod row;
pub mod fingerprint;

pub use manager::ProfileManager;
```

为避免编译失败，先给四个模块各建一个空文件占位：

```rust
// crates/profile-manager/src/manager.rs
// placeholder, filled in Task 4
```
```rust
// crates/profile-manager/src/migrate.rs
// placeholder, filled in Task 3
```
```rust
// crates/profile-manager/src/row.rs
// placeholder, filled in Task 4
```
```rust
// crates/profile-manager/src/fingerprint.rs
// placeholder, filled in Task 5
```

- [ ] **Step 4: settings-store 骨架**

写 `crates/settings-store/Cargo.toml`：

```toml
[package]
name = "settings-store"
version = "0.1.0"
edition = "2021"

[dependencies]
multizen-core = { path = "../multizen-core" }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
thiserror = "1"

[dev-dependencies]
tempfile = "3"
```

写 `crates/settings-store/src/lib.rs`（占位）：

```rust
pub mod defaults;
pub use defaults::{AppSettings, BrowserEngine, SettingsStore, default_settings_path};
```

```rust
// crates/settings-store/src/defaults.rs
// placeholder, filled in Task 6
```

- [ ] **Step 5: .gitignore**

写 `multizen-browser-rs/.gitignore`：

```
/target
**/*.rs.bk
```

- [ ] **Step 6: 验证 workspace 编译**

Run: `cargo check`
Expected: 编译通过（空模块可能有 `unused` 警告，但不应报错）。如果 `multizen-core/src/error.rs` / `profile.rs` / `settings.rs` 尚不存在导致编译失败，先建空文件（下个 task 填充）：

```rust
// crates/multizen-core/src/error.rs
// placeholder, filled in Task 2
```
```rust
// crates/multizen-core/src/profile.rs
// placeholder, filled in Task 2
```
```rust
// crates/multizen-core/src/settings.rs
// placeholder, filled in Task 6
```

- [ ] **Step 7: Commit**

```bash
cd multizen-browser-rs
git add -A
git commit -m "chore: init cargo workspace with three crate skeletons"
```

---

### Task 2: multizen-core 类型与错误

**Files:**
- Modify: `crates/multizen-core/src/error.rs`
- Modify: `crates/multizen-core/src/profile.rs`

**Interfaces:**
- Produces: `MultizenError`；`Profile`、`ProfileSummary`、`CreateProfileInput`、`UpdateProfileInput`、`ProxyConfig`、`DeviceFamily`、`ClientHints`、`FingerprintConfig`、`ExtensionConfig`、`LaunchedProfile`、`McpToolError`，全部 `Serialize + Deserialize`，camelCase。

- [ ] **Step 1: 写 error.rs**

`crates/multizen-core/src/error.rs`：

```rust
use thiserror::Error;

#[derive(Debug, Error)]
pub enum MultizenError {
    #[error("database error: {0}")]
    Db(#[from] rusqlite::Error),

    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    #[error("serde error: {0}")]
    Serde(#[from] serde_json::Error),

    #[error("profile not found: {0}")]
    NotFound(String),

    #[error("profile already exists: {0}")]
    AlreadyExists(String),

    #[error("config error: {0}")]
    Config(String),

    #[error("launch error: {0}")]
    Launch(String),
}

pub type Result<T> = std::result::Result<T, MultizenError>;
```

注意：`multizen-core` 还没引入 `rusqlite` 依赖。`MultizenError::Db` 的 `#[from]` 需要在 `Cargo.toml` 加 `rusqlite = { version = "0.32", features = ["bundled"] }`。更新 `crates/multizen-core/Cargo.toml` 的 `[dependencies]` 段，加上 `rusqlite`。

- [ ] **Step 2: 写 profile.rs**

`crates/multizen-core/src/profile.rs`（字段与 `packages/types/src/index.ts` 1:1 对齐）：

```rust
use serde::{Deserialize, Serialize};

pub type ProfileId = String;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProxyConfig {
    #[serde(rename = "type")]
    pub proxy_type: String, // "http" | "socks5"
    pub host: String,
    pub port: u16,
    pub username: Option<String>,
    pub password: Option<String>,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum DeviceFamily {
    #[serde(rename = "macbook-pro-14-m3")]
    MacbookPro14M3,
    #[serde(rename = "macbook-pro-14-m3-pro")]
    MacbookPro14M3Pro,
    #[serde(rename = "macbook-pro-16-m3-pro")]
    MacbookPro16M3Pro,
    #[serde(rename = "macbook-air-13-m3")]
    MacbookAir13M3,
    #[serde(rename = "macbook-air-15-m3")]
    MacbookAir15M3,
    #[serde(rename = "imac-24-m3")]
    Imac24M3,
    #[serde(rename = "mac-mini-m2")]
    MacMiniM2,
    #[serde(rename = "windows-laptop-intel")]
    WindowsLaptopIntel,
    #[serde(rename = "windows-laptop-intel-uhd")]
    WindowsLaptopIntelUhd,
    #[serde(rename = "windows-laptop-amd")]
    WindowsLaptopAmd,
    #[serde(rename = "windows-laptop-nvidia")]
    WindowsLaptopNvidia,
    #[serde(rename = "windows-laptop-nvidia-4050")]
    WindowsLaptopNvidia4050,
    #[serde(rename = "windows-desktop-nvidia")]
    WindowsDesktopNvidia,
    #[serde(rename = "windows-desktop-nvidia-4080")]
    WindowsDesktopNvidia4080,
    #[serde(rename = "windows-desktop-amd")]
    WindowsDesktopAmd,
    #[serde(rename = "windows-desktop-intel")]
    WindowsDesktopIntel,
    #[serde(rename = "linux-desktop-intel")]
    LinuxDesktopIntel,
    #[serde(rename = "linux-desktop-amd")]
    LinuxDesktopAmd,
    #[serde(rename = "linux-desktop-nvidia")]
    LinuxDesktopNvidia,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ClientHints {
    pub sec_ch_ua: String,
    pub sec_ch_ua_platform: String,
    pub sec_ch_ua_platform_version: String,
    pub sec_ch_ua_arch: String, // "arm" | "x86"
    pub sec_ch_ua_bitness: String, // "64" | "32"
    pub sec_ch_ua_mobile: String, // "?0" | "?1"
    pub sec_ch_ua_model: String,
    pub sec_ch_ua_full_version_list: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ScreenSize {
    pub width: u32,
    pub height: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct WebGlConfig {
    pub vendor: String,
    pub renderer: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FingerprintConfig {
    pub device: DeviceFamily,
    pub user_agent: String,
    pub platform: String, // "MacIntel" | "Win32" | "Linux x86_64"
    pub client_hints: ClientHints,
    pub locale: String,
    pub languages: Vec<String>,
    pub accept_language: String,
    pub timezone: String,
    pub country: String,
    pub screen: ScreenSize,
    pub avail_screen: Option<ScreenSize>,
    pub dpr: f64,
    pub webgl: WebGlConfig,
    pub hardware_concurrency: u32,
    pub device_memory: u32,
    pub fonts_dir: Option<String>,
    pub storage_quota: Option<u64>,
    pub seed: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExtensionConfig {
    pub id: String,
    pub name: String,
    pub version: String,
    pub enabled: bool,
    pub scope: String, // "shared" | "profile"
    pub dir: String,
    pub source: String, // "web-store" | "file" | "folder"
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Profile {
    pub id: ProfileId,
    pub name: String,
    pub notes: Option<String>,
    pub tags: Vec<String>,
    pub proxy: Option<ProxyConfig>,
    pub fingerprint: FingerprintConfig,
    pub extensions: Option<Vec<ExtensionConfig>>,
    pub icon: Option<String>,
    pub start_url: Option<String>,
    pub search_provider: Option<String>,
    pub data_dir: String,
    pub created_at: String,
    pub updated_at: String,
    pub last_opened_at: Option<String>,
    pub proxy_country: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProfileSummary {
    pub id: ProfileId,
    pub name: String,
    pub tags: Vec<String>,
    pub last_opened_at: Option<String>,
    pub is_running: bool,
    pub icon: Option<String>,
    pub proxy: Option<ProxyConfig>,
    pub timezone: Option<String>,
    pub proxy_country: Option<String>,
    pub device: Option<DeviceFamily>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CreateProfileInput {
    pub name: String,
    pub notes: Option<String>,
    pub tags: Option<Vec<String>>,
    pub icon: Option<String>,
    pub start_url: Option<String>,
    pub search_provider: Option<String>,
    pub proxy: Option<ProxyConfig>,
    pub fingerprint: Option<PartialFingerprintInput>,
    pub extensions: Option<Vec<ExtensionConfig>>,
}

/// Partial fingerprint patch — all fields optional, merges over existing.
/// serde flattens into a map so we can merge without defining every field twice.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PartialFingerprintInput {
    pub user_agent: Option<String>,
    pub locale: Option<String>,
    pub timezone: Option<String>,
    pub country: Option<String>,
    // other fields left as None → keep existing on merge
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateProfileInput {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub tags: Option<Vec<String>>,
    pub icon: Option<Option<String>>, // None=keep, Some(None)=clear, Some(Some)=set
    pub start_url: Option<Option<String>>,
    pub search_provider: Option<Option<String>>,
    pub proxy: Option<Option<ProxyConfig>>,
    pub fingerprint: Option<PartialFingerprintInput>,
    pub extensions: Option<Vec<ExtensionConfig>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LaunchedProfile {
    pub id: ProfileId,
    pub cdp_endpoint: String,
    pub pid: u32,
    pub started_at: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct McpToolError {
    pub code: String,
    pub message: String,
    pub details: Option<serde_json::Value>,
}
```

- [ ] **Step 3: 编译验证**

Run: `cargo check -p multizen-core`
Expected: 编译通过。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat(core): add MultizenError and profile types matching TS schema"
```

---

### Task 3: profile-manager 迁移模块

**Files:**
- Modify: `crates/profile-manager/src/migrate.rs`
- Create: `crates/profile-manager/tests/migrate.rs`

**Interfaces:**
- Consumes: `rusqlite::Connection`
- Produces: `pub fn run_migrations(conn: &Connection) -> Result<()>` — 幂等，可重复调用。

- [ ] **Step 1: 写失败测试**

`crates/profile-manager/tests/migrate.rs`：

```rust
use multizen_core::MultizenError;
use profile_manager::migrate::run_migrations;
use rusqlite::Connection;

fn open_mem() -> Connection {
    Connection::open_in_memory().unwrap()
}

#[test]
fn creates_profiles_table_with_all_columns() {
    let conn = open_mem();
    run_migrations(&conn).unwrap();
    let cols: Vec<String> = conn
        .prepare("PRAGMA table_info(profiles)")
        .unwrap()
        .query_map([], |r| r.get::<_, String>(1))
        .unwrap()
        .filter_map(Result::ok)
        .collect();
    for expected in [
        "id", "name", "notes", "tags", "proxy", "fingerprint", "data_dir",
        "created_at", "updated_at", "last_opened_at", "proxy_country",
        "extensions", "icon", "start_url", "search_provider",
    ] {
        assert!(cols.iter().any(|c| c == expected), "missing column: {expected}");
    }
}

#[test]
fn is_idempotent() {
    let conn = open_mem();
    run_migrations(&conn).unwrap();
    run_migrations(&conn).unwrap(); // must not error
    // index exists
    let idx: i64 = conn
        .query_row("SELECT count(*) FROM sqlite_master WHERE name='idx_profiles_name'", [], |r| r.get(0))
        .unwrap();
    assert_eq!(idx, 1);
}

#[test]
fn adds_missing_columns_to_old_schema() {
    // Simulate an old DB that only has the original columns (pre proxy_country etc.)
    let conn = open_mem();
    conn.execute_batch(
        "CREATE TABLE profiles (
            id TEXT PRIMARY KEY, name TEXT NOT NULL, notes TEXT,
            tags TEXT NOT NULL DEFAULT '[]', proxy TEXT, fingerprint TEXT NOT NULL,
            data_dir TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
            last_opened_at TEXT
        );
        CREATE INDEX idx_profiles_name ON profiles(name);",
    )
    .unwrap();
    run_migrations(&conn).unwrap();
    let cols: Vec<String> = conn
        .prepare("PRAGMA table_info(profiles)")
        .unwrap()
        .query_map([], |r| r.get::<_, String>(1))
        .unwrap()
        .filter_map(Result::ok)
        .collect();
    assert!(cols.iter().any(|c| c == "proxy_country"));
    assert!(cols.iter().any(|c| c == "extensions"));
    assert!(cols.iter().any(|c| c == "icon"));
    assert!(cols.iter().any(|c| c == "start_url"));
    assert!(cols.iter().any(|c| c == "search_provider"));
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cargo test -p profile-manager --test migrate`
Expected: FAIL — `run_migrations` 未实现（占位空模块）。

- [ ] **Step 3: 实现 migrate.rs**

`crates/profile-manager/src/migrate.rs`：

```rust
use multizen_core::{MultizenError, Result};
use rusqlite::Connection;

pub fn run_migrations(conn: &Connection) -> Result<()> {
    conn.execute_batch(
        "CREATE TABLE IF NOT EXISTS profiles (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            notes TEXT,
            tags TEXT NOT NULL DEFAULT '[]',
            proxy TEXT,
            fingerprint TEXT NOT NULL,
            data_dir TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            last_opened_at TEXT
        );
        CREATE INDEX IF NOT EXISTS idx_profiles_name ON profiles(name);",
    )?;
    add_column_if_missing(conn, "proxy_country")?;
    add_column_if_missing(conn, "extensions")?;
    add_column_if_missing(conn, "icon")?;
    add_column_if_missing(conn, "start_url")?;
    add_column_if_missing(conn, "search_provider")?;
    Ok(())
}

fn add_column_if_missing(conn: &Connection, col: &str) -> Result<()> {
    let mut stmt = conn.prepare("PRAGMA table_info(profiles)")?;
    let cols: Vec<String> = stmt
        .query_map([], |r| r.get::<_, String>(1))?
        .filter_map(Result::ok)
        .collect();
    if !cols.iter().any(|c| c == col) {
        conn.execute_batch(&format!("ALTER TABLE profiles ADD COLUMN {col} TEXT"))?;
    }
    Ok(())
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cargo test -p profile-manager --test migrate`
Expected: PASS（3 个测试）。

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(profile-manager): idempotent schema migrations"
```

---

### Task 4: profile-manager row 转换与 ProfileManager CRUD

**Files:**
- Modify: `crates/profile-manager/src/row.rs`
- Modify: `crates/profile-manager/src/manager.rs`
- Create: `crates/profile-manager/tests/manager.rs`

**Interfaces:**
- Consumes: `multizen_core::{Profile, ProfileId, ProfileSummary, CreateProfileInput, UpdateProfileInput, ProxyConfig, FingerprintConfig, ExtensionConfig, MultizenError}`；`run_migrations`
- Produces:
  - `pub struct ProfileManager { ... }`
  - `impl ProfileManager { pub fn new(db_path: &Path, profiles_root: &Path) -> Result<Self>`
  - `pub fn list(&self) -> Result<Vec<ProfileSummary>>`
  - `pub fn get(&self, id: &str) -> Result<Option<Profile>>`
  - `pub fn create(&self, input: CreateProfileInput) -> Result<Profile>`
  - `pub fn insert_imported(&self, profile: Profile) -> Result<Profile>`
  - `pub fn update(&self, id: &str, patch: UpdateProfileInput) -> Result<Profile>`
  - `pub fn set_proxy_country(&self, id: &str, country: Option<&str>) -> Result<()>`
  - `pub fn delete(&self, id: &str) -> Result<()>`
  - `pub fn mark_opened(&self, id: &str) -> Result<()>`
  - `pub fn all_extension_refs(&self) -> Result<Vec<ExtensionRef>>`
  - `pub fn close(self)` —— Drop 关闭连接

- [ ] **Step 1: 写 row.rs**

`crates/profile-manager/src/row.rs`：

```rust
use multizen_core::{ExtensionConfig, FingerprintConfig, Profile, ProxyConfig};
use serde::Deserialize;

#[derive(Debug, Clone)]
pub struct ProfileRow {
    pub id: String,
    pub name: String,
    pub notes: Option<String>,
    pub tags: String,
    pub proxy: Option<String>,
    pub fingerprint: String,
    pub data_dir: String,
    pub created_at: String,
    pub updated_at: String,
    pub last_opened_at: Option<String>,
    pub proxy_country: Option<String>,
    pub extensions: Option<String>,
    pub icon: Option<String>,
    pub start_url: Option<String>,
    pub search_provider: Option<String>,
}

pub fn row_to_profile(row: ProfileRow) -> Profile {
    let fingerprint: FingerprintConfig =
        serde_json::from_str(&row.fingerprint).expect("corrupt fingerprint JSON");
    let tags: Vec<String> =
        serde_json::from_str(&row.tags).unwrap_or_default();
    let proxy = row.proxy.as_deref().map(|s| {
        serde_json::from_str::<ProxyConfig>(s).expect("corrupt proxy JSON")
    });
    let extensions = normalize_extensions(row.extensions.as_deref());
    Profile {
        id: row.id,
        name: row.name,
        notes: row.notes,
        tags,
        proxy,
        fingerprint,
        extensions: if extensions.is_empty() { None } else { Some(extensions) },
        icon: row.icon,
        start_url: row.start_url,
        search_provider: row.search_provider,
        data_dir: row.data_dir,
        created_at: row.created_at,
        updated_at: row.updated_at,
        last_opened_at: row.last_opened_at,
        proxy_country: row.proxy_country,
    }
}

pub fn normalize_extensions(raw: Option<&str>) -> Vec<ExtensionConfig> {
    let Some(raw) = raw else { return Vec::new() };
    let Ok(parsed) = serde_json::from_str::<serde_json::Value>(raw) else {
        return Vec::new();
    };
    let Some(arr) = parsed.as_array() else { return Vec::new() };
    arr.iter()
        .map(|e| ExtensionConfig {
            id: e.get("id").and_then(|v| v.as_str()).unwrap_or("").to_string(),
            name: e.get("name").and_then(|v| v.as_str()).unwrap_or("Extension").to_string(),
            version: e.get("version").and_then(|v| v.as_str()).unwrap_or("").to_string(),
            scope: e.get("scope").and_then(|v| v.as_str()).unwrap_or("profile").to_string(),
            enabled: e.get("enabled").and_then(|v| v.as_bool()).unwrap_or(true),
            dir: e.get("dir").and_then(|v| v.as_str()).unwrap_or("").to_string(),
            source: e.get("source").and_then(|v| v.as_str()).unwrap_or("file").to_string(),
        })
        .collect()
}
```

- [ ] **Step 2: 写失败测试**

`crates/profile-manager/tests/manager.rs`：

```rust
use multizen_core::{CreateProfileInput, ProfileManager as _, UpdateProfileInput};
use profile_manager::ProfileManager;
use tempfile::TempDir;

fn make() -> (TempDir, ProfileManager) {
    let dir = TempDir::new().unwrap();
    let db = dir.path().join("test.db");
    let profiles_root = dir.path().join("profiles");
    let mgr = ProfileManager::new(&db, &profiles_root).unwrap();
    (dir, mgr)
}

#[test]
fn create_and_get_profile() {
    let (_dir, mgr) = make();
    let input = CreateProfileInput {
        name: "test".into(),
        notes: None,
        tags: Some(vec!["a".into()]),
        icon: None,
        start_url: None,
        search_provider: None,
        proxy: None,
        fingerprint: None,
        extensions: None,
    };
    let p = mgr.create(input).unwrap();
    assert_eq!(p.name, "test");
    assert_eq!(p.tags, vec!["a".to_string()]);
    let fetched = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(fetched.id, p.id);
}

#[test]
fn list_returns_summary_with_running_false() {
    let (_dir, mgr) = make();
    mgr.create(CreateProfileInput { name: "p1".into(), ..Default::default() }).unwrap();
    let list = mgr.list().unwrap();
    assert_eq!(list.len(), 1);
    assert!(!list[0].is_running);
}

#[test]
fn update_changes_name_and_clears_icon() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "orig".into(), icon: Some("🦊".into()), ..Default::default() }).unwrap();
    let updated = mgr.update(&p.id, UpdateProfileInput {
        name: Some("renamed".into()),
        icon: Some(None), // clear
        ..Default::default()
    }).unwrap();
    assert_eq!(updated.name, "renamed");
    assert_eq!(updated.icon, None);
}

#[test]
fn update_proxy_clears_proxy_country() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    mgr.set_proxy_country(&p.id, Some("US")).unwrap();
    let _ = mgr.update(&p.id, UpdateProfileInput {
        proxy: Some(Some(multizen_core::ProxyConfig {
            proxy_type: "http".into(), host: "1.1.1.1".into(), port: 8080,
            username: None, password: None,
        })),
        ..Default::default()
    }).unwrap();
    let after = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(after.proxy_country, None); // stale country cleared on proxy change
}

#[test]
fn delete_removes_row_and_data_dir() {
    let (dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    let data_dir = dir.path().join("profiles").join(&p.id);
    assert!(data_dir.exists());
    mgr.delete(&p.id).unwrap();
    assert!(mgr.get(&p.id).unwrap().is_none());
    assert!(!data_dir.exists());
}

#[test]
fn insert_imported_collides_on_existing_id() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    let result = mgr.insert_imported(p);
    assert!(result.is_err());
}

#[test]
fn mark_opened_sets_last_opened_at() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    assert!(mgr.get(&p.id).unwrap().unwrap().last_opened_at.is_none());
    mgr.mark_opened(&p.id).unwrap();
    assert!(mgr.get(&p.id).unwrap().unwrap().last_opened_at.is_some());
}
```

注意：测试用了 `CreateProfileInput { ..Default::default() }` 和 `UpdateProfileInput { ..Default::default() }`，需要两个 struct 派生 `Default`。回到 `crates/multizen-core/src/profile.rs`，给 `CreateProfileInput` 和 `UpdateProfileInput` 加 `#[derive(Default)]`（`PartialFingerprintInput` 已有）。`ProxyConfig` 也需 `Default` 以便测试构造，给它加 `#[derive(Default)]`。

- [ ] **Step 3: 运行测试确认失败**

Run: `cargo test -p profile-manager --test manager`
Expected: FAIL — `ProfileManager::new` 等未实现。

- [ ] **Step 4: 实现 manager.rs**

`crates/profile-manager/src/manager.rs`：

```rust
use std::path::{Path, PathBuf};
use std::fs;

use multizen_core::{
    CreateProfileInput, ExtensionConfig, MultizenError, Profile, ProfileId, ProfileSummary,
    Result, UpdateProfileInput,
};
use rusqlite::{params, Connection};
use uuid::Uuid;

use crate::fingerprint::default_fingerprint;
use crate::migrate::run_migrations;
use crate::row::{normalize_extensions, row_to_profile, ProfileRow};

pub struct ExtensionRef {
    pub profile_id: String,
    pub data_dir: String,
    pub ext: ExtensionConfig,
}

pub struct ProfileManager {
    conn: Connection,
    profiles_root: PathBuf,
}

impl ProfileManager {
    pub fn new(db_path: &Path, profiles_root: &Path) -> Result<Self> {
        if let Some(parent) = db_path.parent() {
            fs::create_dir_all(parent)?;
        }
        fs::create_dir_all(profiles_root)?;
        let conn = Connection::open(db_path)?;
        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "foreign_keys", "ON")?;
        run_migrations(&conn)?;
        Ok(Self { conn, profiles_root: profiles_root.to_path_buf() })
    }

    pub fn list(&self) -> Result<Vec<ProfileSummary>> {
        let mut stmt = self.conn.prepare(
            "SELECT id, name, tags, last_opened_at, proxy, fingerprint, proxy_country, icon
             FROM profiles ORDER BY updated_at DESC",
        )?;
        let rows = stmt.query_map([], |r| {
            Ok(ProfileRow {
                id: r.get(0)?,
                name: r.get(1)?,
                notes: None,
                tags: r.get::<_, String>(2)?,
                proxy: r.get(3)?,
                fingerprint: r.get::<_, String>(4)?,
                data_dir: String::new(),
                created_at: String::new(),
                updated_at: String::new(),
                last_opened_at: r.get(5)?,
                proxy_country: r.get(6)?,
                extensions: None,
                icon: r.get(7)?,
                start_url: None,
                search_provider: None,
            })
        })?;
        let mut out = Vec::new();
        for row in rows {
            let row = row?;
            let fingerprint: multizen_core::FingerprintConfig =
                serde_json::from_str(&row.fingerprint)?;
            let proxy = row.proxy.as_deref().map(|s| serde_json::from_str(s)).transpose()?;
            let tags: Vec<String> = serde_json::from_str(&row.tags).unwrap_or_default();
            out.push(ProfileSummary {
                id: row.id,
                name: row.name,
                tags,
                last_opened_at: row.last_opened_at,
                is_running: false,
                icon: row.icon,
                proxy,
                timezone: Some(fingerprint.timezone.clone()),
                proxy_country: row.proxy_country,
                device: Some(fingerprint.device),
            });
        }
        Ok(out)
    }

    pub fn get(&self, id: &str) -> Result<Option<Profile>> {
        let row = self.conn.query_row(
            "SELECT id, name, notes, tags, proxy, fingerprint, data_dir,
                    created_at, updated_at, last_opened_at, proxy_country,
                    extensions, icon, start_url, search_provider
             FROM profiles WHERE id = ?",
            params![id],
            |r| {
                Ok(ProfileRow {
                    id: r.get(0)?, name: r.get(1)?, notes: r.get(2)?,
                    tags: r.get(3)?, proxy: r.get(4)?, fingerprint: r.get(5)?,
                    data_dir: r.get(6)?, created_at: r.get(7)?, updated_at: r.get(8)?,
                    last_opened_at: r.get(9)?, proxy_country: r.get(10)?,
                    extensions: r.get(11)?, icon: r.get(12)?,
                    start_url: r.get(13)?, search_provider: r.get(14)?,
                })
            },
        );
        match row {
            Ok(r) => Ok(Some(row_to_profile(r))),
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(e.into()),
        }
    }

    pub fn create(&self, input: CreateProfileInput) -> Result<Profile> {
        let id = Uuid::new_v4().to_string();
        let now = chrono::Utc::now().to_rfc3339();
        let data_dir = self.profiles_root.join(&id);
        fs::create_dir_all(&data_dir)?;

        let mut fingerprint = default_fingerprint(&id);
        if let Some(patch) = input.fingerprint {
            if let Some(v) = patch.user_agent { fingerprint.user_agent = v; }
            if let Some(v) = patch.locale { fingerprint.locale = v; }
            if let Some(v) = patch.timezone { fingerprint.timezone = v; }
            if let Some(v) = patch.country { fingerprint.country = v; }
        }

        let profile = Profile {
            id: id.clone(),
            name: input.name,
            notes: input.notes,
            tags: input.tags.unwrap_or_default(),
            proxy: input.proxy,
            fingerprint: fingerprint.clone(),
            extensions: input.extensions,
            icon: input.icon,
            start_url: input.start_url,
            search_provider: input.search_provider,
            data_dir: data_dir.to_string_lossy().to_string(),
            created_at: now.clone(),
            updated_at: now,
            last_opened_at: None,
            proxy_country: None,
        };
        self.insert_row(&profile)?;
        Ok(profile)
    }

    pub fn insert_imported(&self, profile: Profile) -> Result<Profile> {
        if self.get(&profile.id)?.is_some() {
            return Err(MultizenError::AlreadyExists(profile.id));
        }
        fs::create_dir_all(&profile.data_dir)?;
        self.insert_row(&profile)?;
        Ok(profile)
    }

    fn insert_row(&self, profile: &Profile) -> Result<()> {
        self.conn.execute(
            "INSERT INTO profiles
             (id, name, notes, tags, proxy, fingerprint, extensions, icon,
              start_url, search_provider, data_dir, created_at, updated_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            params![
                profile.id, profile.name, profile.notes,
                serde_json::to_string(&profile.tags)?,
                profile.proxy.as_ref().map(|p| serde_json::to_string(p)).transpose()?,
                serde_json::to_string(&profile.fingerprint)?,
                profile.extensions.as_ref().map(|e| serde_json::to_string(e)).transpose()?,
                profile.icon, profile.start_url, profile.search_provider,
                profile.data_dir, profile.created_at, profile.updated_at,
            ],
        )?;
        Ok(())
    }

    pub fn update(&self, id: &str, patch: UpdateProfileInput) -> Result<Profile> {
        let existing = self.get(id)?.ok_or_else(|| MultizenError::NotFound(id.to_string()))?;
        let now = chrono::Utc::now().to_rfc3339();

        let proxy_changed = match (&patch.proxy, &existing.proxy) {
            (Some(Some(new)), Some(old)) => serde_json::to_string(new)? != serde_json::to_string(old)?,
            (Some(Some(_)), None) | (Some(None), Some(_)) => true,
            _ => false,
        };

        let merged = Profile {
            name: patch.name.unwrap_or(existing.name),
            notes: patch.notes.or(existing.notes),
            tags: patch.tags.unwrap_or(existing.tags),
            proxy: match patch.proxy {
                Some(None) => None,
                Some(Some(p)) => Some(p),
                None => existing.proxy,
            },
            fingerprint: existing.fingerprint.clone(), // merge handled below
            extensions: patch.extensions.or(existing.extensions),
            icon: match patch.icon {
                Some(None) => None,
                Some(Some(v)) => Some(v),
                None => existing.icon,
            },
            start_url: match patch.start_url {
                Some(None) => None,
                Some(Some(v)) => Some(v),
                None => existing.start_url,
            },
            search_provider: match patch.search_provider {
                Some(None) => None,
                Some(Some(v)) => Some(v),
                None => existing.search_provider,
            },
            updated_at: now,
            proxy_country: if proxy_changed { None } else { existing.proxy_country },
            ..existing.clone()
        };

        // merge fingerprint patch
        let mut fingerprint = existing.fingerprint;
        if let Some(p) = patch.fingerprint {
            if let Some(v) = p.user_agent { fingerprint.user_agent = v; }
            if let Some(v) = p.locale { fingerprint.locale = v; }
            if let Some(v) = p.timezone { fingerprint.timezone = v; }
            if let Some(v) = p.country { fingerprint.country = v; }
        }
        let merged = Profile { fingerprint, ..merged };

        self.conn.execute(
            "UPDATE profiles SET
               name = ?, notes = ?, tags = ?, proxy = ?, fingerprint = ?,
               extensions = ?, icon = ?, start_url = ?, search_provider = ?,
               updated_at = ?, proxy_country = ?
             WHERE id = ?",
            params![
                merged.name, merged.notes,
                serde_json::to_string(&merged.tags)?,
                merged.proxy.as_ref().map(|p| serde_json::to_string(p)).transpose()?,
                serde_json::to_string(&merged.fingerprint)?,
                merged.extensions.as_ref().map(|e| serde_json::to_string(e)).transpose()?,
                merged.icon, merged.start_url, merged.search_provider,
                merged.updated_at, merged.proxy_country, id,
            ],
        )?;
        Ok(merged)
    }

    pub fn set_proxy_country(&self, id: &str, country: Option<&str>) -> Result<()> {
        self.conn.execute(
            "UPDATE profiles SET proxy_country = ? WHERE id = ?",
            params![country, id],
        )?;
        Ok(())
    }

    pub fn delete(&self, id: &str) -> Result<()> {
        let existing = self.get(id)?;
        self.conn.execute("DELETE FROM profiles WHERE id = ?", params![id])?;
        if let Some(p) = existing {
            let _ = fs::remove_dir_all(&p.data_dir); // best-effort
        }
        Ok(())
    }

    pub fn mark_opened(&self, id: &str) -> Result<()> {
        let now = chrono::Utc::now().to_rfc3339();
        self.conn.execute(
            "UPDATE profiles SET last_opened_at = ? WHERE id = ?",
            params![now, id],
        )?;
        Ok(())
    }

    pub fn all_extension_refs(&self) -> Result<Vec<ExtensionRef>> {
        let mut stmt = self.conn.prepare("SELECT id, data_dir, extensions FROM profiles")?;
        let rows = stmt.query_map([], |r| {
            Ok((r.get::<_, String>(0)?, r.get::<_, String>(1)?, r.get::<_, Option<String>>(2)?))
        })?;
        let mut out = Vec::new();
        for row in rows {
            let (profile_id, data_dir, ext_raw) = row?;
            for ext in normalize_extensions(ext_raw.as_deref()) {
                out.push(ExtensionRef { profile_id: profile_id.clone(), data_dir: data_dir.clone(), ext });
            }
        }
        Ok(out)
    }
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cargo test -p profile-manager`
Expected: PASS（migrate 3 个 + manager 7 个）。

如有编译错误（例如 `CreateProfileInput` 未实现 `Default`），回到 `crates/multizen-core/src/profile.rs` 给对应 struct 加 `#[derive(Default)]` 并重新运行。

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(profile-manager): ProfileManager CRUD with rusqlite"
```

---

### Task 5: profile-manager default_fingerprint

**Files:**
- Modify: `crates/profile-manager/src/fingerprint.rs`
- Create: `crates/profile-manager/tests/fingerprint.rs`

**Interfaces:**
- Produces: `pub fn default_fingerprint(seed: &str) -> FingerprintConfig` — 返回一个合理的默认 Windows 桌面指纹（与 TS 版 `defaultFingerprint` 对齐：Windows 10 + Chrome 148 + en-US + America/New_York 占位）。seed 用于后续确定性扰动，本 task 先忽略（返回固定值 + 存 seed）。

- [ ] **Step 1: 写失败测试**

`crates/profile-manager/tests/fingerprint.rs`：

```rust
use profile_manager::fingerprint::default_fingerprint;

#[test]
fn default_is_windows_chrome_us() {
    let fp = default_fingerprint("abc");
    assert_eq!(fp.locale, "en-US");
    assert_eq!(fp.timezone, "America/New_York");
    assert_eq!(fp.country, "US");
    assert!(fp.user_agent.contains("Windows NT 10.0"));
    assert!(fp.user_agent.contains("Chrome/148"));
    assert_eq!(fp.platform, "Win32");
    assert_eq!(fp.client_hints.sec_ch_ua_platform, "Windows");
    assert_eq!(fp.dpr, 1.0);
    assert_eq!(fp.hardware_concurrency, 8);
    assert_eq!(fp.device_memory, 8);
    assert_eq!(fp.seed, Some("abc".to_string()));
}

#[test]
fn screen_and_webgl_populated() {
    let fp = default_fingerprint("x");
    assert!(fp.screen.width > 0 && fp.screen.height > 0);
    assert!(!fp.webgl.vendor.is_empty());
    assert!(!fp.webgl.renderer.is_empty());
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cargo test -p profile-manager --test fingerprint`
Expected: FAIL。

- [ ] **Step 3: 实现 fingerprint.rs**

`crates/profile-manager/src/fingerprint.rs`：

```rust
use multizen_core::{
    ClientHints, DeviceFamily, FingerprintConfig, ScreenSize, WebGlConfig,
};

pub fn default_fingerprint(seed: &str) -> FingerprintConfig {
    FingerprintConfig {
        device: DeviceFamily::WindowsDesktopIntel,
        user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36".into(),
        platform: "Win32".into(),
        client_hints: ClientHints {
            sec_ch_ua: r#""Chromium";v="148", "Google Chrome";v="148", "Not?A_Brand";v="99""#.into(),
            sec_ch_ua_platform: "Windows".into(),
            sec_ch_ua_platform_version: "10.0.0".into(),
            sec_ch_ua_arch: "x86".into(),
            sec_ch_ua_bitness: "64".into(),
            sec_ch_ua_mobile: "?0".into(),
            sec_ch_ua_model: "".into(),
            sec_ch_ua_full_version_list: r#""Chromium";v="148.0.0.0", "Google Chrome";v="148.0.0.0", "Not?A_Brand";v="99.0.0.0""#.into(),
        },
        locale: "en-US".into(),
        languages: vec!["en-US".into(), "en".into()],
        accept_language: "en-US,en;q=0.9".into(),
        timezone: "America/New_York".into(),
        country: "US".into(),
        screen: ScreenSize { width: 1920, height: 1080 },
        avail_screen: Some(ScreenSize { width: 1920, height: 1040 }),
        dpr: 1.0,
        webgl: WebGlConfig {
            vendor: "Google Inc. (Intel)".into(),
            renderer: "ANGLE (Intel, Intel(R) UHD Graphics 630 Direct3D11 vs_5_0 ps_5_0, D3D11)".into(),
        },
        hardware_concurrency: 8,
        device_memory: 8,
        fonts_dir: Some(r"C:\Windows\Fonts".into()),
        storage_quota: Some(2_000_000_000),
        seed: Some(seed.to_string()),
    }
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cargo test -p profile-manager --test fingerprint`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(profile-manager): default Windows fingerprint generator"
```

---

### Task 6: settings-store 实现

**Files:**
- Modify: `crates/multizen-core/src/settings.rs`
- Modify: `crates/settings-store/src/lib.rs`
- Modify: `crates/settings-store/src/defaults.rs`
- Create: `crates/settings-store/tests/store.rs`

**Interfaces:**
- Produces:
  - `multizen_core::AppSettings`、`multizen_core::BrowserEngine`
  - `settings_store::SettingsStore { pub fn new(json_path: &Path) -> Self; pub fn load(&mut self) -> Result<AppSettings>; pub fn update(&mut self, patch: AppSettingsPatch) -> Result<AppSettings> }`
  - `settings_store::default_settings_path(dir: &Path) -> PathBuf`

- [ ] **Step 1: 写 multizen-core/src/settings.rs**

```rust
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum BrowserEngine {
    Cft,
    Cloakbrowser,
}

impl Default for BrowserEngine {
    fn default() -> Self { BrowserEngine::Cloakbrowser }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppSettings {
    pub theme: String, // "dark"
    pub mcp_http_enabled: bool,
    pub mcp_http_port: u16,
    pub browser_engine: BrowserEngine,
    #[serde(default)]
    pub browser_binary_path: Option<String>,
    #[serde(default)]
    pub skip_browser_download: bool,
    pub auto_update: bool,
    pub usage_reporting: bool,
}

impl Default for AppSettings {
    fn default() -> Self {
        Self {
            theme: "dark".into(),
            mcp_http_enabled: true,
            mcp_http_port: 7777,
            browser_engine: BrowserEngine::Cloakbrowser,
            browser_binary_path: None,
            skip_browser_download: false,
            auto_update: true,
            usage_reporting: false,
        }
    }
}
```

更新 `crates/multizen-core/src/lib.rs` 的 `pub mod settings;` 已存在，无需改。

- [ ] **Step 2: 写失败测试**

`crates/settings-store/tests/store.rs`：

```rust
use multizen_core::{AppSettings, BrowserEngine};
use settings_store::{default_settings_path, SettingsStore};
use tempfile::TempDir;

#[test]
fn load_returns_defaults_when_file_missing() {
    let dir = TempDir::new().unwrap();
    let mut store = SettingsStore::new(&default_settings_path(dir.path()));
    let s = store.load().unwrap();
    assert_eq!(s.mcp_http_port, 7777);
    assert!(s.mcp_http_enabled);
    assert_eq!(s.browser_engine, BrowserEngine::Cloakbrowser);
    assert!(!s.usage_reporting);
}

#[test]
fn update_persists_and_caches() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    let mut store = SettingsStore::new(&path);
    let _ = store.load().unwrap();
    let mut patch = AppSettings::default();
    patch.mcp_http_port = 9999;
    patch.usage_reporting = true;
    let saved = store.update(patch.clone()).unwrap();
    assert_eq!(saved.mcp_http_port, 9999);
    // cache hit without re-reading file
    let cached = store.load().unwrap();
    assert_eq!(cached.mcp_http_port, 9999);
    // new store reads persisted file
    let mut store2 = SettingsStore::new(&path);
    let reloaded = store2.load().unwrap();
    assert_eq!(reloaded.mcp_http_port, 9999);
    assert!(reloaded.usage_reporting);
}

#[test]
fn load_recovers_from_corrupt_json() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    std::fs::write(&path, "{ not valid json").unwrap();
    let mut store = SettingsStore::new(&path);
    let s = store.load().unwrap();
    assert_eq!(s.mcp_http_port, 7777); // fell back to defaults
}

#[test]
fn load_normalizes_invalid_browser_engine() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    std::fs::write(&path, r#"{"mcpHttpPort": 7777, "browserEngine": "bogus"}"#).unwrap();
    let mut store = SettingsStore::new(&path);
    let s = store.load().unwrap();
    assert_eq!(s.browser_engine, BrowserEngine::Cloakbrowser); // reset to default
}

#[test]
fn load_clears_empty_browser_binary_path() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    std::fs::write(&path, r#"{"mcpHttpPort": 7777, "browserBinaryPath": "   "}"#).unwrap();
    let mut store = SettingsStore::new(&path);
    let s = store.load().unwrap();
    assert_eq!(s.browser_binary_path, None);
}
```

- [ ] **Step 3: 运行确认失败**

Run: `cargo test -p settings-store`
Expected: FAIL — `SettingsStore` 未实现。

- [ ] **Step 4: 实现 defaults.rs / lib.rs**

`crates/settings-store/src/defaults.rs`：

```rust
use std::path::{Path, PathBuf};

use multizen_core::{AppSettings, BrowserEngine, MultizenError, Result};
use serde::Deserialize;

#[derive(Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
struct RawSettings {
    theme: Option<String>,
    mcp_http_enabled: Option<bool>,
    mcp_http_port: Option<u16>,
    browser_engine: Option<String>,
    browser_binary_path: Option<String>,
    skip_browser_download: Option<bool>,
    auto_update: Option<bool>,
    usage_reporting: Option<bool>,
}

pub struct SettingsStore {
    json_path: PathBuf,
    cache: Option<AppSettings>,
}

impl SettingsStore {
    pub fn new(json_path: &Path) -> Self {
        if let Some(parent) = json_path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        Self { json_path: json_path.to_path_buf(), cache: None }
    }

    pub fn load(&mut self) -> Result<AppSettings> {
        if let Some(c) = &self.cache {
            return Ok(c.clone());
        }
        let raw: RawSettings = match std::fs::read_to_string(&self.json_path) {
            Ok(txt) => serde_json::from_str(&txt).unwrap_or_default(),
            Err(_) => RawSettings::default(),
        };
        let mut merged = AppSettings::default();
        if let Some(v) = raw.theme { merged.theme = v; }
        if let Some(v) = raw.mcp_http_enabled { merged.mcp_http_enabled = v; }
        if let Some(v) = raw.mcp_http_port { merged.mcp_http_port = v; }
        merged.browser_engine = match raw.browser_engine.as_deref() {
            Some("cft") => BrowserEngine::Cft,
            Some("cloakbrowser") => BrowserEngine::Cloakbrowser,
            _ => BrowserEngine::default(),
        };
        merged.browser_binary_path = raw
            .browser_binary_path
            .filter(|s| !s.trim().is_empty());
        merged.skip_browser_download = raw.skip_browser_download.unwrap_or(false);
        merged.auto_update = raw.auto_update.unwrap_or(true);
        merged.usage_reporting = raw.usage_reporting.unwrap_or(false);
        self.cache = Some(merged.clone());
        Ok(merged)
    }

    pub fn update(&mut self, patch: AppSettings) -> Result<AppSettings> {
        self.cache = Some(patch.clone());
        let json = serde_json::to_string_pretty(&patch)?;
        std::fs::write(&self.json_path, json)?;
        Ok(patch)
    }
}

pub fn default_settings_path(dir: &Path) -> PathBuf {
    dir.join("settings.json")
}
```

`crates/settings-store/src/lib.rs`：

```rust
pub mod defaults;
pub use defaults::{default_settings_path, SettingsStore};
```

注意：`MultizenError` 已在 `multizen-core` 定义，但 `settings-store` 依赖 `multizen-core` 已在 Cargo.toml 声明。`Result` 用 `multizen_core::Result`。

- [ ] **Step 5: 运行确认通过**

Run: `cargo test -p settings-store`
Expected: PASS（5 个测试）。

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(settings-store): JSON-backed AppSettings with defaults + validation"
```

---

### Task 7: workspace 全量校验与文档

**Files:**
- Create: `multizen-browser-rs/crates/multizen-core/README.md`
- Create: `multizen-browser-rs/crates/profile-manager/README.md`
- Create: `multizen-browser-rs/crates/settings-store/README.md`

- [ ] **Step 1: 全量测试**

Run: `cargo test --workspace`
Expected: 全部 PASS（migrate 3 + manager 7 + fingerprint 2 + settings 5 = 17 个测试）。

如有失败，逐一修复后重跑，不要跳过。

- [ ] **Step 2: clippy**

Run: `cargo clippy --workspace -- -D warnings`
Expected: 无警告。如有，修复（常见：`unwrap_or_default` 风格、未使用导入）。

- [ ] **Step 3: 写各 crate README**

每个 README 简述 crate 职责、公共 API、依赖关系。例：

`crates/multizen-core/README.md`：

```markdown
# multizen-core

Shared types, errors, and serde schema for the multizen-browser-rs workspace.

Exposes `MultizenError`, `Profile` / `ProfileSummary` / `CreateProfileInput` / `UpdateProfileInput`, `FingerprintConfig`, `ProxyConfig`, `AppSettings`, `BrowserEngine`.

All structs serialize with `rename_all = "camelCase"` to stay byte-compatible with the legacy TypeScript `packages/types` schema, so Tauri command return values can be consumed directly by the React UI.
```

`crates/profile-manager/README.md`：

```markdown
# profile-manager

SQLite-backed profile storage for multizen-browser-rs. 1:1 port of the legacy TS `packages/profile-manager`.

`ProfileManager::new(db_path, profiles_root)` opens (or creates) the DB, runs idempotent migrations, and exposes `list / get / create / insert_imported / update / set_proxy_country / delete / mark_opened / all_extension_refs`.

Schema columns are snake_case; `tags / proxy / fingerprint / extensions` are stored as JSON text, matching the TS version so an existing DB file can be opened without migration.
```

`crates/settings-store/README.md`：

```markdown
# settings-store

JSON-backed `AppSettings` with in-memory cache, defaults, and field validation (browser engine normalization, empty-path clearing). Mirrors the TS `packages/settings-store`.
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: READMEs for foundation crates + clippy clean"
```

---

## Self-Review 记录

- Spec 覆盖：Plan 1 覆盖 spec 中 `multizen-core` / `profile-manager` / `settings-store` 三个 crate。`cdp-driver` / `browser-launcher` / `behavioral` / `mcp-server` / `tauri-app` / UI 归入 Plan 2-4（本 plan 不涉及）。
- 占位符：无 TBD/TODO；每个步骤含完整代码。
- 类型一致性：`MultizenError` 变体名、`ProfileManager` 方法签名、`SettingsStore` 方法签名在各任务间一致。`CreateProfileInput` / `UpdateProfileInput` / `ProxyConfig` 派生 `Default` 以支持测试构造。

## 后续 Plan（占位，本 plan 不实现）

- Plan 2: `behavioral` + `browser-launcher` + `cdp-driver`（浏览器层，需 CloakBrowser 集成测试）
- Plan 3: `mcp-server`（rmcp，Streamable HTTP + SSE，8 工具）
- Plan 4: `tauri-app` + React UI 迁移
