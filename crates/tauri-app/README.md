# tauri-app

MultiZen desktop shell — the Tauri 2.x binary that wires the Rust backend
(`multizen-core` / `profile-manager` / `browser-launcher` / `cdp-driver` /
`mcp-server`) to the React UI in [`ui/`](ui/).

## Architecture

```
tauri::Builder
  ├── manage(AppState)        — driver + settings + activity + mcp_token
  ├── invoke_handler(19 cmds) — profiles/settings/dialog/fingerprint/proxy/system/activity
  ├── plugin(dialog)          — native file/dir pickers
  └── setup hook              — load mcp-token, spawn embedded MCP axum, bridge push events
```

### AppState (`src/lib.rs`)

`AppState` is `Send + Sync`. `ProfileManager` is `!Send + !Sync` (sqlite
`Connection`), so it never lives in `AppState`. Instead a dedicated OS thread
owns the `ProfileManager` + `BrowserLauncher` for their whole lifetime;
`TauriBrowserDriver` holds only an `mpsc::Sender<LauncherCmd>` (always
`Send + Sync`) and routes every pm/launcher operation through that channel.

### Driver (`src/driver.rs`)

`TauriBrowserDriver` implements `mcp_server::driver::BrowserDriver`. The
launcher thread runs a `current_thread` tokio runtime + `LocalSet` command
loop. `LauncherCmd` carries launch/close **and** profile CRUD variants, each
with a `oneshot` reply channel. On `launch` success it emits
`profiles:running-changed` + `chromium:status` via an injected
`tauri::AppHandle` (`set_app`); on failure it emits `chromium:status failed`.

### Embedded MCP (`src/mcp_embed.rs`, `src/token.rs`)

On startup, `load_or_create_mcp_token` reads or generates a 256-bit hex token
(0600 on Unix). If `settings.mcp_http_enabled`, `start_embedded_mcp` spawns an
axum server on `settings.mcp_http_port` via `tauri::async_runtime::spawn`,
wired to `mcp_server::transport::build_router(token, port)`. rmcp dispatch is
still a placeholder (`-32601`); the driver/activity are stored in an
`OnceLock<McpState>` for the future wired dispatch.

### Push events

A bridging task subscribes to `ActivityLog::subscribe()` (broadcast) and
emits each `ActivityEvent` as `activity:event`. The driver emits
`profiles:running-changed` and `chromium:status` on launch/close.

## Build & run

```bash
# from the workspace root
cargo check -p tauri-app

# dev (needs a real CloakBrowser binary configured in Settings)
cargo tauri dev

# the UI is a separate Vite project
cd ui && npm install && npm run dev   # frontend only
```

## Test

```bash
cargo test -p tauri-app
cargo clippy -p tauri-app --all-targets -- -D warnings
```

## Scope (MVP)

Implemented: profiles CRUD + launch/close, settings get/update, dialog
pickers, fingerprint generate/devices/locales, system info, activity recent,
push events, embedded MCP transport, mcp-token management.

Stubs (return `Err`, runtime degrade): `fingerprint.reconcile`,
`fingerprint.locale_for_country`, `proxy.detect_geo`. Out of scope:
`extensions.*`, `update.*`, `chromium.status/retry`, profile
export/import archive — not registered in Plan 4.
