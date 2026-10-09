# Reverse debugging

Cloaksession connects an independent `js-reverse-mcp` process to an already running managed browser profile. The existing MCP endpoint exposes its 24 debugging tools with an explicit `debugSessionId`, alongside four debugging lifecycle tools and the existing browser tools.

## Start from the desktop

1. Launch a profile in **All profiles**.
2. Open **MCP → Reverse debugging**, select the running profile, and click **Attach debugger**.
3. Select a page inside its browser window. **List frames** exposes iframe execution contexts.
4. Use **Copy agent instructions** to give an AI client the exact session ID and artifact directory.
5. **Detach debugger** disconnects the debugging process and keeps the managed browser running.

Each attach creates a separate debugging process and page/frame selection, including repeated attachments to the same profile. Selecting an existing session in the UI explicitly shares its selection with callers using that ID. The underlying browser page is still shared: navigation, page state changes, and debugger pauses affect that page across connections. Separate profiles provide separate browser state.

## MCP lifecycle

| Tool | Arguments | Result |
|---|---|---|
| `list_profiles` | `{}` | Managed profiles and running state |
| `launch_profile` | `profileId` | Browser PID and CDP endpoint |
| `attach_debug_session` | `profileId` | `debugSessionId`, `profileId`, `status`, `allowedRoot`, creation time |
| `list_browser_sessions` | `{}` | Existing attached/error debug sessions |
| `list_windows` | `debugSessionId` | Native window IDs and page target IDs in `structuredContent.data.windows` |
| `detach_debug_session` | `debugSessionId` | Detach result |

`list_browser_sessions` lists debugging attachments; `list_profiles` discovers browsers that have not yet been attached. Browser CDP endpoints remain internal connection details; clients use the Cloaksession MCP endpoint and its bearer token.

HTTP MCP initialization returns an `Mcp-Session-Id` header. Clients echo it on subsequent calls and on `notifications/cancelled`; request IDs are scoped to that HTTP session. Notifications return HTTP 202 with no RPC body. DELETE releases the HTTP session and cancels its active requests. Legacy calls without the session header still work, while cancellation requires a session. HTTP transport sessions and browser `debugSessionId` values have separate lifetimes.

Every reverse tool requires `debugSessionId`. `select_page` accepts the upstream `pageIdx` or the stable `targetId` returned by `list_windows`. Target selection using `targetId` does not rely on URLs, so duplicate URLs remain distinguishable. Closed targets return an error; refresh the window list. Frame indices and source IDs expire when the page/frame changes.

## Debugging workflow

1. `select_page` and, when needed, `select_frame` choose the execution context.
2. `list_network_requests` starts the HTTP evidence collector; `get_websocket_messages` starts WebSocket capture. Reproduce or reload the action after activation to capture its events.
3. `list_scripts`, `search_in_sources`, and `get_script_source` locate code. `save_script_source` exports complete source.
4. `break_on_xhr` or `set_breakpoint_on_text` sets a breakpoint before reproducing the operation.
5. `get_paused_info` exposes the paused stack. `evaluate_script` can inspect the selected paused frame; `step` accepts `over`, `into`, or `out`.
6. `pause_or_resume` with `action: "resume"` resumes execution explicitly.
7. `list_network_requests` inspects headers/bodies or exports precise evidence; `get_request_initiator` traces the initiating JavaScript stack.
8. `get_websocket_messages` lists connections and reads received/sent frames.

The remaining tools are `new_page`, `navigate_page`, `click_element`, `take_screenshot`, `list_breakpoints`, `remove_breakpoint`, `clear_network_requests`, `clear_site_data`, and `list_console_messages`. Inspect `tools/list` for the pinned argument schemas, including upstream confirmation fields. Tool results preserve `content`, `structuredContent`, and `isError`.

## Runtime and ownership

- Runtime: `js-reverse-mcp@4.0.5`, with its pinned `@zhizhuodemao/patchright@1.61.1-mcp.2` driver.
- Node: `^20.19.0 || ^22.12.0 || >=23`; the existing Chromix Node path is used when no debugger-specific path is configured.
- Resources: `resources/reverse/bridge.mjs`, `windows.mjs`, and pinned `node_modules` beside the desktop executable (inside app Resources on macOS).
- Installation uses `npm ci --omit=dev --omit=optional`. The reverse runtime does not download another browser or install the optional CloakBrowser package.
- Artifacts are confined to a profile-specific directory under the application data directory's `debugger/` folder. Use the returned `allowedRoot`; relative export paths resolve there.
- Closing/deleting a profile, restarting it for extension changes, or quitting the application cleans up associated debugging processes.
- An exited child leaves an error session visible. Detach it and attach again to obtain a new session ID.
- Cancelling or timing out a tool call that reached the child invalidates that debug session and disconnects its child. Attach again to recover; the managed browser remains open. This prevents an upstream stuck handler from retaining the tool mutex and blocking Resume indefinitely.

Patchright controls protocol behavior when attaching. Existing browser launch flags, profile state, proxies, and kernel fingerprint behavior remain owned by Cloaksession. Enabling debugging/network domains changes browser instrumentation; this integration does not guarantee a site's automation detection outcome.

The window adapter reads titles from CDP target metadata, including while JavaScript is paused. Same-process iframe debugging reuses the page CDP session; out-of-process iframes retain their own session.

## Build and test

From the repository root:

```powershell
python scripts/build.py --prepare
npm --prefix desktop/resources/reverse test
python -m unittest discover -s scripts -p 'test_*.py'
$env:GOTOOLCHAIN = 'go1.23.12'
cd desktop
go test ./...
```

Real-browser verification is opt-in and uses temporary profiles and local HTTP/WebSocket fixtures:

```powershell
$env:CLOAKSESSION_TEST_REVERSE_BROWSER = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
go test -count=1 -v -run '^TestReverseIntegration$' .
```

The fixed SDK dependency reports an OAuth-client advisory in npm audit. This integration uses stdio with an empty MCP client capability set and does not use the SDK OAuth client; upgrades should be evaluated with the pinned upstream runtime and full debugger regressions.

## Local verification (2026-10-09)

- Windows Wails executable and staged reverse resources built successfully with Go 1.23.12.
- Nine real-browser integration subtests passed in one complete serial run using the configured Chromium 154.0.8037.97 binary: discovery/source export, native window/stable target selection, text breakpoint/step, same-process and cross-site iframe, XHR pause/resume, HTTP body/initiator, WebSocket frames, detach/reconnect, and profile isolation.
- Reverse Node tests: 14 passed. Python build/staging tests: 28 passed.
- New debugger UI and responsive-layout tests: 8 passed. The final complete UI run had 76/78 passes; both failed startup-loading cases passed on targeted rerun.
- A full Go test and vet run passed before the final Service-lock adjustment. The new Service-lock regression passed after the adjustment. A later full rerun encountered loopback connection timeouts in existing updater/CDP tests; final debugger/MCP/storage/extension packages passed. Both failed updater/CDP tests passed on targeted rerun, and the final `go vet ./...` passed.
- Windows race-test executables exited with `0xc0000139` before tests in this environment. CI is configured to run the race detector and the real reverse integration suite on Linux; this working-tree implementation has not been pushed or run in cloud CI.

Repeated runs exposed intermittent local loopback connection failures at different ports in both existing and new tests. A successful complete run proves the covered operations, while repeated-run stability remains an environment/transport follow-up item.
