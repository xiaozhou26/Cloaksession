# desktop-core

Cloaksession's headless Rust integration process connects `profile-manager`, `browser-launcher`, `cdp-driver`, and `mcp-server` to the [Wails desktop host](../../desktop/). It has no WebView or Tauri dependency.

## Architecture

`AppState` owns settings, activity, updater state, MCP authentication, and `DesktopBrowserDriver`. The driver routes Profile persistence and browser operations through a dedicated launcher thread because SQLite's connection is not shared across threads. Frontend commands and authenticated MCP calls use the same driver.

The core retains the existing Profile database, browser data directories, settings, extensions, and MCP token under `com.cloaksession.browser`. `CLOAKSESSION_DATA_DIR` overrides the data root; `CLOAKSESSION_RESOURCE_DIR` points to bundled Node resources.

## Host protocol

The Wails host starts `desktop-core` as a subprocess. Each stdin/stdout line is a JSON object; diagnostics use stderr.

```json
{"id":1,"command":"profiles_list","args":{}}
{"id":1,"result":[]}
{"event":"profiles:running-changed","data":{"profileId":"example","kind":"closed"}}
```

Commands return `result` or a string `error`. Events keep their existing names and payloads. `core:ready` signals that database initialization and background-task startup are complete. Supported command namespaces cover Profiles, settings, fingerprint generation, proxy checks, extensions, encrypted archives, updates, activity, system information, and dialogs.

Native dialog requests go in the reverse direction:

```json
{"id":1,"dialog":{"kind":"open","title":"Choose archive","filters":[{"displayName":"Archive","pattern":"*.mzar"}]}}
{"id":1,"result":"/path/to/profile.mzar"}
```

A host reply has no `command` field. Cancellation returns `null`; dialog errors return `error`. Reading continues while commands await dialogs. EOF or the `shutdown` command terminates the core and requests managed browser shutdown.

## MCP and events

The embedded HTTP MCP service binds to loopback when enabled in settings. It uses the persisted bearer token and the existing tool registry. A background task forwards activity changes to Wails; browser lifecycle, proxy-country, extension, and update events also use the host channel.

## Verification

```sh
cargo check --locked -p desktop-core
cargo test --locked -p desktop-core
```

Tests cover registry sharing, transport events, host dialogs, malformed requests, command errors, startup, and EOF/shutdown behavior. Go integration tests additionally cover real Rust command execution and encrypted archive round trips; see [desktop/README.md](../../desktop/README.md).
