# Wails desktop

Cloaksession 1.4.0 uses Go for the complete application backend. Wails owns the desktop window and native dialogs, and `App.Invoke` calls the in-process `Service`. No Rust executable or Cargo toolchain is needed.

## Components

- `internal/store`: pure-Go SQLite, Profile/settings compatibility, fingerprint catalogs, encrypted `.mzar` archives.
- `internal/browser`: browser processes, proxy forwarding, CDP connections and automation. Chromix uses the bundled direct Playwright bridge.
- `internal/extensions`: CRX/ZIP/folder installation, shared cache, icons, and companion signals.
- `internal/mcp`: authenticated loopback HTTP JSON-RPC, tool catalog and activity events.
- `service.go`: desktop command dispatch and lifecycle coordination.
- `resources/playwright`: Node bridge and pinned `playwright-core` dependency.

## Build and develop

```sh
python3 scripts/build.py --prepare
cd desktop
GOTOOLCHAIN=go1.23.12 go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 dev
```

From the repository root, `python3 scripts/build.py --package` builds the platform package. Add `--universal` on macOS for Apple Silicon and Intel. Wails 2.11 tooling is pinned to Go 1.23.12 in CI.

Node.js 20+ is required when using the direct Playwright browser path. Browser executables remain locally supplied. Go controls the application and CDP operations; JavaScript remains the frontend and Playwright adapter language.

## Compatibility and paths

Existing `com.cloaksession.browser` data locations, SQLite schema, settings keys, engine user directories and MCP tokens are retained. `CLOAKSESSION_DATA_DIR` and `CLOAKSESSION_RESOURCE_DIR` provide isolated test/development overrides. Wails WebView preferences may differ from older desktop shells; Profile persistence is independent of WebView localStorage.

Packaged Node resources live in `Contents/Resources/playwright` on macOS and adjacent `resources/playwright` on Windows/Linux. The browser-core process used by 1.3.0 is removed.

## Verify

```sh
npm --prefix desktop/frontend ci --legacy-peer-deps
npm --prefix desktop/frontend run build
npm --prefix desktop/resources/playwright ci
cd desktop
GOTOOLCHAIN=go1.23.12 go test -race ./...
CLOAKSESSION_TEST_BROWSER="/absolute/path/to/chromium" \
  GOTOOLCHAIN=go1.23.12 go test -race -count=1 ./...
```

`testdata/legacy-rust-profile.mzar` is a synthetic old-format archive for cross-version import testing. It contains no user data or real credentials.
