# mcp-server

Rust MCP server ([rmcp](https://docs.rs/rmpc) + axum) for `multizen-browser-rs`.
Exposes **23 tools** over Streamable HTTP (`POST /mcp`) plus a legacy SSE
endpoint (`GET /sse`) and `GET /healthz`.

## Tools

`list_profiles`, `launch_profile`, `close_profile`, `navigate`, `click`,
`type`, `extract`, `screenshot`, `create_profile`, `update_profile`,
`delete_profile`, `list_fingerprint_options`, `evaluate_js`,
`wait_for_selector`, `list_tabs`, `activate_tab`, `close_tab`,
`wait_for_navigation`, `wait_for_load`, `get_cookies`, `set_cookies`,
`new_tab`, and `cdp_send`.

`cdp_send` (raw CDP passthrough) is **hidden by default** and only registered
when the environment variable `MULTIZEN_MCP_ALLOW_RAW_CDP=1` is set.

## Security gates (engine-independent)

These checks run inside `mcp-server` before any CDP call reaches the browser,
so they apply regardless of which `BrowserDriver` implementation is plugged in:

- **`BLOCKED_URL_SCHEMES`** — `file:`, `chrome:`, `devtools:`,
  `view-source:` are rejected before navigation / evaluation.
- **`CDP_DENY_METHODS`** — exact deny list: `IO.read`,
  `Page.getResourceContent`, `Storage.getCookies`, `Network.getAllCookies`,
  `Browser.close`, `Browser.crash`. Prefix deny list: `DOMStorage.*`,
  `IndexedDB.*`, `CacheStorage.*`, `Fetch.*`.
- **URL normalization** — strips `\t` / `\r` / `\n` globally and all leading
  control characters (`<= 0x20`) before the scheme check, so control-char
  smuggling cannot bypass `BLOCKED_URL_SCHEMES`.
- **Proxy credential redaction** — proxy URLs with embedded credentials are
  redacted before being logged or echoed back.
- **ActivityLog argument sanitization** — sensitive tool args (cookies,
  tokens, etc.) are scrubbed before being written to the activity log.
- **Bearer-token auth** — constant-time comparison; missing/empty token is
  rejected without timing leakage.
- **DNS-rebinding guard** — the HTTP transport rejects requests whose
  `Host` header does not match the bound listener, preventing DNS-rebinding
  attacks against the local server.

## Architecture

`mcp-server` does **not** depend on a concrete browser backend. It defines a
`BrowserDriver` trait (`crates/mcp-server/src/driver.rs`) that the
`tauri-app` crate (Plan 4) implements and injects at wiring time. A
`MockBrowserDriver` is provided for tests so the crate's test suite runs
without a real Chromium.

## Endpoints

| Method | Path        | Purpose                          |
|--------|-------------|----------------------------------|
| POST   | `/mcp`      | Streamable HTTP MCP transport    |
| GET    | `/sse`      | Legacy SSE MCP transport         |
| GET    | `/healthz`  | Liveness / readiness probe      |

## Running

The crate is a library; it is launched by the `tauri-app` binary (Plan 4)
which constructs the axum router, wires the `BrowserDriver`, and binds the
listener. See the `tauri-app` crate for the full startup path.
