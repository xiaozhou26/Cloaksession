# browser-launcher
Spawns CloakBrowser/CFT, passes `--fingerprint-*` / `--proxy-server` / `--user-data-dir` / `--load-extension` flags, runs the local SOCKS5 bridge (remote DNS), probes proxy geo via ipapi.co, manages session-restore prefs and singleton locks, and tracks running profiles in a registry. Does NOT issue CDP commands — that's `cdp-driver`.

## Storage quota

`FingerprintConfig.storage_quota` and `--fingerprint-storage-quota` use bytes.
The default is `2_000_000_000` bytes (2 GB), passed as
`--fingerprint-storage-quota=2000000000` without a minimum override.
Only the UI converts between bytes and decimal MB (`1 MB = 1_000_000` bytes).
An unset or zero quota omits the flag and leaves the engine default in effect.
These native fingerprint flags apply to CloakBrowser, not CFT or Chromix.

## Chromix SDK bridge

`BrowserLauncher::launch` is unchanged for CloakBrowser/CFT. Chromix uses
`launch_with_chromix(&self, profile_id: &str, binary_path: &Path,
companion_dir: Option<&Path>, config: &multizen_core::ChromixSettings,
runtime_dir: &Path, skip_download: bool) -> Result<LaunchedProfile>`.

Install the runtime with `npm ci --omit=dev --ignore-scripts` in
`crates/tauri-app/resources/chromix`. Ship that directory, including
`bridge.mjs` and `node_modules`, as `runtime_dir`. The lockfile pins a vendored copy of the official Chromix Node SDK at
GitHub commit `39b9ea1bd262c2eb6306f3e23279398a6476c845` (the source package
still reports version `0.1.0`) and `playwright-core@1.63.0`. Node >=20 must be available through
`config.node_path` (default `node`); Node itself is not bundled.

The bridge calls the official `launchPersistentContext`. The default user
profile is `<profile.data_dir>/engines/chromix`; top-level
`config.options.userDataDir` overrides it. All ordinary options, including
unknown SDK fields, `humanConfig`, nested launch/context options and custom
arguments, are forwarded without filtering or flattening. The measured-only
`devicePool` mode is rejected by this CDP sidecar because it requires the
SDK's direct runtime-verification entry point and matching evidence. Only the
host CDP arguments are reserved: remote-debugging, user-data-dir and
profile-directory arguments in any args/ignoreDefaultArgs layer are rejected.
CDP uses an OS-assigned port on `127.0.0.1`; control arguments are appended to
each effective args layer so nested SDK spreads cannot replace them. The port
reservation must be released for Chromium to bind it.

An omitted headless setting defaults to a visible browser. A profile proxy
is used only without explicit proxy options or proxy arguments. Enabled,
existing profile/companion extension directories default `extensionPaths`
only without explicit extension options/arguments. The profile start URL is
opened only in an empty context without an explicit positional URL; restored
pages are retained. No legacy fingerprint, storage quota, GPU backend,
GeoIP, session preference rewriting or SOCKS bridge policies are injected.
SDK persona and GeoIP behavior remain the SDK's responsibility.

Executable precedence follows the SDK persistent-context spread order:
`contextOptions.executablePath` > `launchOptions.executablePath` > the host
`binary_path` > `CLOAKBROWSER_BINARY_PATH` in the Node environment > SDK
cache/download resolution. `config.environment` overrides inherited Node
environment variables. The chosen explicit executable is also assigned to
SDK-supported `CLOAKBROWSER_BINARY_PATH` before launch, preventing the SDK's
initial `ensureBinary` call from downloading an unrelated executable. A
missing/non-executable chosen path is an error, not a download fallback.
An empty host `binary_path` leaves SDK resolution enabled. With
`skip_download`, public `binaryInfo(options)` must identify an installed
executable if no explicit path exists; the bridge never calls a downloader
to discover the cache. SDK `CHROMIX_CACHE_DIR`, release channel and version
environment/options are honored.

Node 0.1.0 offers only the Playwright surface through this sidecar. The sidecar rejects `mode: "measured"`, non-Playwright adapters and `devicePool`; measured device admission requires the SDK's direct runtime-verification entry point and matching evidence. `mode: "native"` and `adapter: "playwright"` are accepted. The official SDK owns its existing nested option precedence and ignored-field behavior.

Node receives JSON over stdin, emits ready/error/closed JSON over stdout,
and writes SDK logs to stderr. Environment and proxy data are not included
in Node argv. `LaunchedProfile.pid` is the supervising Node PID. Ready is
sent only after the SDK context and the loopback CDP endpoint are available;
only then is the profile marked opened/running. Startup has a 15-minute
budget including first download, with a 60-second CDP readiness check.
Close, EOF, cancelled startup, context exit and launcher drop are supervised;
normal shutdown calls `context.close()`. A stuck SDK shutdown has a
10-second Node limit and host-side termination escalation. Node's normal
exit invokes Playwright's browser-process cleanup hooks.

Tests: `cargo test -p browser-launcher --locked` and `npm test` in the runtime
directory use an injectable fake SDK and do not start a real browser.
