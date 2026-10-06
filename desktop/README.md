# Wails desktop host

The Go host owns the desktop window, native file dialogs, and the Rust `desktop-core` subprocess. React calls `main.App.Invoke`; commands and events cross a private JSON-lines stdin/stdout channel. The core's stderr is reserved for logs. The host closes stdin on exit and waits for the core to stop managed browsers.

## Build and development

From the repository root:

```sh
python3 scripts/build.py --prepare
cd desktop
GOTOOLCHAIN=go1.23.12 go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 dev
```

Build complete distributable artifacts using `python3 scripts/build.py --package`. On macOS, add `--universal` for both Apple Silicon and Intel. The build script stages the Rust executable and Node runtime resources together with the Wails executable.

Wails 2.11's build tooling is verified with Go 1.23.12. A newer Go installation can select that toolchain using `GOTOOLCHAIN=go1.23.12`.

## Runtime paths

The data directory keeps the existing `com.cloaksession.browser` identifier:

- Windows: `%LOCALAPPDATA%\com.cloaksession.browser`
- macOS: `~/Library/Application Support/com.cloaksession.browser`
- Linux: `$XDG_DATA_HOME/com.cloaksession.browser`, or `~/.local/share/com.cloaksession.browser`

`CLOAKSESSION_DATA_DIR`, `CLOAKSESSION_CORE_BINARY`, and `CLOAKSESSION_RESOURCE_DIR` override paths for isolated development and tests. Packaged builds load `desktop-core` beside the application executable. macOS resources live in `Contents/Resources`; Windows and Linux use the adjacent `resources` directory.

## Tests

Build frontend assets before testing the Go package because they are embedded at compile time:

```sh
npm --prefix desktop/frontend ci --legacy-peer-deps
npm --prefix desktop/frontend run build
cargo build --locked -p desktop-core
cd desktop
go test -race ./...
CLOAKSESSION_TEST_CORE="$PWD/../target/debug/desktop-core" \
  CLOAKSESSION_RESOURCE_DIR="$PWD/../crates/desktop-core/resources" \
  go test -race -run TestRealCoreRoundTrip -v
```

The real-core test uses a temporary data directory with MCP and automatic updates disabled. It verifies commands, Profile creation/update/deletion, encrypted archive export/import through host dialog requests, errors, and process shutdown.
