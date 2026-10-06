# Desktop Playwright runtime

This runtime lives in `desktop/resources/playwright`; the only npm dependency is `playwright-core` **1.63.0**. The bridge calls `chromium.launchPersistentContext(userDataDir, options)` directly. Storage keys `chromix`, `chromixOptions`, and `engines/chromix` remain unchanged.

## Fingerprint contract

- `options.fingerprintMode`: `random`, `fixed`, or `custom`.
- Fixed/custom require `options.fingerprintSeed` as a **decimal string from 1 through 18446744073709551615**, inclusive. Never use a JavaScript number. Zero is rejected because the patched browser interprets `--fingerprint=0` as fingerprinting off.
- Random generates a fresh cryptographic seed on every launch. It ignores any stored `fingerprintSeed` and never changes the saved profile.
- Random/fixed are **seed-only by default**: old profile fingerprint fields do not inject screen, CPU, RAM, GPU, locale, timezone, user-agent, or quota overrides. The browser determines native/seed behavior; seed changes do not promise every fingerprint surface changes.
- Custom adds stored profile fingerprint fields as defaults. Explicit raw `--fingerprint-*` and corresponding `--uxr-*` arguments override those defaults. Raw arguments remain supported in random/fixed too; explicit locale/timezone/user-agent Playwright options also remain effective.
- An explicit mode replaces only seed arguments (`--fingerprint`, `--fingerprint-seed`, `--uxr-fingerprint-seed`). Other arguments, including audio seeds and persona overrides, are preserved.
- Without `fingerprintMode`, legacy profile defaults and raw seed/override arguments remain supported. `stealthArgs: false` suppresses automatic defaults; raw `--fingerprint=off` is the advanced opt-out. Fixed/custom cannot use zero as this opt-out.

### Quota units

Persisted `profile.fingerprint.storageQuota` remains **bytes**, as elsewhere in the persisted Go model. Only the Chromix adapter converts positive profile quotas to integer MiB with `ceil(bytes / 1048576)`: **2147483648 bytes becomes `--fingerprint-storage-quota=2048`**. Random/fixed do not inject this profile quota. Explicit raw quota arguments are already in the patched browser's **MiB** units and are never converted; an explicit zero is preserved.

Evidence checked in repository history at commit `cb5c7747bf2d85a84cafa3073ad45494c8dabba1`:

- `crates/tauri-app/resources/chromix/vendor/chromix/README.md`, lines 142–145, documents the public fingerprint default as **102400 MiB**.
- The matching `_fingerprint.js`, line 12, bounds the public quota argument at `(2^63 - 1) / 1048576`.
- The removed vendor manifest pins upstream Chromix commit `39b9ea1bd262c2eb6306f3e23279398a6476c845`.

This is the Chromix public flag contract, not a change to CloakBrowser's separate byte-valued launcher flags. Stock Chrome smoke tests do not verify patched quota or fingerprint effects.

## Launch compatibility

Use a local `executablePath`/`browserBinaryPath`, `CLOAKBROWSER_BINARY_PATH`, or an already-installed default Playwright Chromium executable. The runtime never downloads a browser, regardless of `skipDownload`. Unsupported SDK features (including measured mode, device pools, humanization, GeoIP lookup, and release-channel downloads) return actionable errors.

`launchOptions` and `contextOptions` are flattened in that precedence order; their argument lists are combined with top-level `args`. Supported shorthand includes `timezone`, `extensionPaths`, `startMaximized`, and proxy URLs. Host CDP and user-data-directory arguments are reserved. Authenticated SOCKS5 is adapted by the Go host to a loopback proxy. The Node control channel remains JSON-lines on stdout, with diagnostics on stderr.

## Verification

Run `npm ci --omit=dev` and `npm test` in this directory. `node test/native-smoke.mjs` runs the standalone smoke using `CHROMIX_TEST_BINARY` (or installed Google Chrome on macOS). From `desktop`, `CLOAKSESSION_TEST_BROWSER=/path/to/chromium go test -race ./...` runs the Go service, CDP and Playwright integration tests, including actual navigation and shutdown.
