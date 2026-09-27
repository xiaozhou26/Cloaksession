# @xiaoxiaofeihh/chromix

Drive the Chromix Chromium engine with a **CloakBrowser-compatible API**.
Function names, option names (camelCase), and return types (Playwright
`Browser` / `BrowserContext` via `playwright-core`) match the
[`cloakbrowser`](https://github.com/CloakHQ/CloakBrowser) wrapper, so existing
CloakBrowser scripts can migrate by changing the import:

```diff
- import { launch } from 'cloakbrowser';
+ import { launch } from '@xiaoxiaofeihh/chromix';
```

```javascript
import { launch } from '@xiaoxiaofeihh/chromix';

const browser = await launch({
  proxy: 'http://user:pass@residential-proxy:port',
  geoip: true,       // match timezone + locale to proxy IP
  headless: false,
  humanize: true,    // human-like mouse, keyboard, scroll
});
const page = await browser.newPage();
await page.goto('https://example.com');
await browser.close();
```

Convenience wrappers:

```javascript
import {
  launchContext,
  launchPersistentContext,
} from '@xiaoxiaofeihh/chromix';

const context = await launchContext({
  userAgent: 'Custom UA',
  viewport: { width: 1920, height: 1080 },
});
const persistentContext = await launchPersistentContext({
  userDataDir: './chrome-profile',
  headless: false,
});
```

## Install

```bash
npm install @xiaoxiaofeihh/chromix playwright-core
```

The unscoped npm name `chromix` belongs to an unrelated project. This SDK is
published under the `@xiaoxiaofeihh` scope; use the full scoped name when
installing or importing it.

The SDK uses the lightweight `yauzl` ZIP reader and loads an installed
`playwright-core` or `playwright` package at launch time. On first launch, the
Chromix binary is downloaded from this repository's GitHub Release,
SHA256-verified when the release manifest is available, and cached under
`~/.cache/chromix`. Point `CLOAKBROWSER_BINARY_PATH` at a local build to skip
the download.

### Binary platforms

The SDK resolves Linux x64/ARM64, Windows x64/ARM64, and macOS x64/ARM64.
On Windows, `process.platform === "win32"` with `process.arch === "arm64"`
selects `win-arm64` and `chromix-win-arm64.zip`; `process.arch === "x64"`
keeps selecting `win-x64` and `chromix-win-x64.zip`. Use native ARM64 Node.js
on Windows ARM64: an x64 Node.js process running under emulation still reports
`x64`. A missing ARM64 asset does not trigger an x64 fallback.

Both Windows ZIPs contain `chromix/chromix.cmd` and `chromix/chrome.exe`.
`ensureBinary()` returns `chrome.exe` for Playwright; the cache is isolated
by release tag and platform (`~/.cache/chromix/<tag>/win-arm64/`). Downloads
require the matching asset in the selected release or `CHROMIX_DOWNLOAD_HOST`;
SDK support alone does not publish an ARM64 browser. Windows ARM64 Widevine
CDM discovery is not supported; an x64 CDM is not reused for ARM64.

## Puppeteer

Install a `puppeteer-core` version compatible with your Node runtime, then use
the separate native adapter (no Playwright driver is required):

```javascript
import { launchContext } from '@xiaoxiaofeihh/chromix/puppeteer';

const context = await launchContext({
  executablePath: process.env.CHROMIX_BROWSER_PATH,
  args: ['--fingerprint=42'],
});
try {
  const page = await context.newPage();
  await page.goto('https://example.com');
} finally {
  await context.close(); // also closes this entry point's owned browser
}
```

Exports `launch`, `launchContext`, `launchPersistentContext`, `connect` and
`buildLaunchOptions`. Persistent profiles share the Playwright seed format.
The default `defaultViewport` is `null`; explicit `launchOptions` are Puppeteer
options, not Playwright `contextOptions`. `connect` cannot change launch identity;
failed connection preparation disconnects without closing the caller's browser.
Humanized wheel calls keep Puppeteer's `{deltaX, deltaY}` API. Measured device
admission and browser-wide HTTP proxy authentication are not implemented here.

## Encrypted Cookie migration

```javascript
import { exportCookies, importCookies } from '@xiaoxiaofeihh/chromix/cookies';

const options = { passphrase: process.env.COOKIE_PASSPHRASE };
await exportCookies(sourceContext, 'cookies.enc', options);
await importCookies(emptyDestinationContext, 'cookies.enc', options);
```

Accepts live Chromium Playwright or Puppeteer contexts. The same functions,
plus `encryptCookies` / `decryptCookies`, are exported from the root and
`/puppeteer`. The authenticated AES-GCM/scrypt file format interoperates with
Python; the passphrase must contain 12–1024 UTF-8 bytes. Exports never overwrite
an existing file. Imports require an empty context, preserve CHIPS/host-only and
security attributes, skip expired entries and verify readback. Import failure
can leave partial contents; there is no destructive clearing or rollback.
This is not a native OSCrypt/profile-database portability switch. See the
[complete format and evidence boundaries](../../docs/functionality-followup.md).

## Options

CloakBrowser options work unchanged: `headless, proxy, args, stealthArgs,
timezone, locale, geoip, humanize, humanPreset, humanConfig, userAgent,
viewport, colorScheme, extensionPaths, browserVersion, releaseChannel,
licenseKey, contextOptions, launchOptions, userDataDir` (+ `startMaximized`).

Persistent contexts create `.chromix-fingerprint-seed` inside `userDataDir` on
first stealth launch and reuse it thereafter. The file is one decimal 32-bit
seed followed by a newline, uses the same format as the Python SDK, and is
published atomically for concurrent first launches. An explicit
`--fingerprint=...` in `args`, `launchOptions.args`, or `contextOptions.args`
wins without creating or rewriting the file; `stealthArgs: false` also skips
seed I/O. Defaults claim the native persona: `linux`, `windows`, or `macos`.

Default page viewport geometry is native. Public fingerprint mode supplies
CPU/RAM 8/8, platform-specific screen/taskbar defaults and a 102400 MiB quota.
The older seeded synthetic viewport/hardware pools require explicit
`args: ['--uxr-synthetic-device-tests=true']` and remain separate test templates.

Explicit synthetic seeds accept nonzero decimal uint64 strings without rounding
through JavaScript `Number`; Python and Node derive identical geometry. Malformed
seeds, conflicting screen/taskbar aliases, invalid work areas and incomplete
viewport pairs fail. `--uxr-viewport-width`/`--uxr-viewport-height` override the
UI-strip template. A configured viewport sends screen and DPR together, including
DPR 1; `viewport: null` removes inherited screen/DPR defaults. The
[launch display backend](../../docs/persona-cross-process-design.md) needs a
browser rebuilt from the current patch stack.

### Public fingerprint flags

GPU vendor/renderer, CPU/RAM, screen/taskbar, brand/version/platform version,
timezone/locale, quota, Windows font metrics, WebRTC IP/auto, noise/off,
third-party cookies and `FakeShadowRoot` are available through `args`, along
with GPU mode, restricted fonts, graph audio isolation, millisecond clock
resolution, codec restrictions and effective CSS/input preferences. Voice tables
are synthetic fixtures. See the [complete flag contract](../../docs/fingerprint-flags.md)
for defaults and limitations. Updating this SDK does not add native features
to an old executable; use a browser rebuilt from the matching patch stack.

```javascript
const browser = await launch({ args: [
  '--fingerprint=42',
  '--fingerprint-brand=Edge',
  '--fingerprint-brand-version=152.0.0.0',
  '--fingerprint-noise=false',
  '--fingerprint-allow-3p-cookies',
  '--enable-blink-features=FakeShadowRoot',
] });
```

`--fingerprint=off` accepts `false/0/disable/disabled` and strips the injected
platform. Explicit timezone/locale and `geoip: true` still apply regional
settings; omit them for a native-persona comparison. `noise=false` keeps seeds
while disabling existing perturbations; it does not install four independent
Canvas/WebGL/audio/client-rect noise implementations.

Ordinary launches default to `--fingerprint-gpu-backend=native`; explicit WebGL
name hints require `compatibility`. The SDK no longer adds `--ignore-gpu-blocklist`.
Use `--fingerprint-audio-render=isolated` with the fingerprint seed or an explicit
audio seed, and `--fingerprint-timer-resolution=7` for **7 milliseconds**.
`fontsDir` supplies a parsed default family whitelist before validating
`--fingerprint-font-policy=restricted`; Linux also loads the actual directory
through Fontconfig. See [backend policy](../../docs/backend-policy.md).

## Measured device launch

`launchContext({devicePool: {python: 'python', host: 'record.json',
records: ['record.json'], seed: '42'}})` validates entire evidence bundles and
the native host, then verifies five live contexts before returning. The persistent
variant uses `launchPersistentContext` with `userDataDir` and binds record/seed.
Install the matching Python SDK into the selected interpreter first:
`python -m pip install ./sdk/python` from this checkout. Set
`CLOAKBROWSER_BINARY_PATH` to the exact collected executable. Evidence defaults to
a 24-hour age limit. Other field/launch/context overrides are rejected;
browser-returning `launch` does not support measured mode. See
[device pool documentation](../../docs/device-pool.md) for the full contract.

Environment variables: `CLOAKBROWSER_BINARY_PATH`, `CLOAKBROWSER_VERSION`,
`CLOAKBROWSER_RELEASE_CHANNEL`, `CLOAKBROWSER_GEOIP_TIMEOUT_SECONDS`,
`CLOAKBROWSER_WIDEVINE_CDM` / `CLOAKBROWSER_WIDEVINE=0` (DRM), and
`CHROMIX_CACHE_DIR` / `CHROMIX_DOWNLOAD_HOST` (cache / release host override).

## Intentional differences from CloakBrowser

1. `licenseKey` is accepted and ignored (one open tier).
2. `geoip` queries ip-api.com instead of a local GeoLite2 database.
3. Puppeteer uses `@xiaoxiaofeihh/chromix/puppeteer`, with the boundaries above.
4. Widevine/DRM is enabled automatically when a CDM is present (installed
   Chrome or `CLOAKBROWSER_WIDEVINE_CDM`); on Linux, fetch one with
   `python -m chromix widevine`.

High-risk engine ports are available only through explicit `args`:

```javascript
const browser = await launch({ args: [
  '--fingerprint-devtools-runtime-suppression',
  '--fingerprint-canvas-bridge=127.0.0.1:9228',
  '--fingerprint-canvas-bridge-unsafe',
] });
```

Runtime suppression can break console/binding-based automation. Canvas Bridge
removes the sandbox from bridge renderer processes and forwards canvas/WebGL
operations to the configured endpoint.

### Proxy and GeoIP behavior

GeoIP is metadata, not a routing mechanism. The lookup uses the effective
HTTP/HTTPS/SOCKS proxy, including `launchOptions.proxy` overrides, and does not
inherit environment proxies or `NO_PROXY` bypasses. Failed lookups do not
fall back to the host connection. Metadata transport supports SOCKS4/4a/5/5h
and SOCKS5 credentials; that transport alone does not extend Chromium's proxy
backend. With a browser built from patches `0154`–`0157`, the launch SDK also
supports native SOCKS5 TCP username/password authentication via the high-level
`proxy` option. Credentials are endpoint-bound in the launch environment, not
argv or `page.authenticate`; context-specific SOCKS credentials are rejected.
Unrelated launches scrub inherited auth, including Windows case aliases. There
is no UDP ASSOCIATE implementation or matching-native-build acceptance yet.
SOCKS5/4a metadata lookups use remote
destination DNS; SOCKS4 uses local IPv4 DNS. Lookup accepts one raw
`--proxy-server` route, not PAC/auto-detect, route lists, empty raw proxies,
raw proxy credentials or a proxy conflicting with `--no-proxy-server`.
Simultaneous raw/Playwright proxy endpoints must match; omitted default ports
and equivalent IPv6 spellings are normalized. Supply credentials in the high-level option.

With a proxy, the SDK defaults to the native
`--force-webrtc-ip-handling-policy=disable_non_proxied_udp` unless an explicit
native policy was supplied. This is a WebRTC policy, not a guarantee about
all DNS, HTTP, QUIC or operating-system traffic.

`--fingerprint-webrtc-ip=<IPv4|IPv6|auto>` is supported. Auto resolves before
launch through the effective proxy. `geoip: true` reuses its one lookup to inject
the exit IP unless an explicit IP wins; off mode skips IP injection. The browser
rewrites local candidate/SDP/stats presentation, not sockets or STUN success.
Remote addresses, zero placeholders and relay allocations remain native.
`webrtc-fake-srflx` and `webrtc-fake-srflx-allow-udp` (including `uxr` equivalents)
remain rejected. The HTTP metadata service is not independent proof of an exit
route. Bare-browser auto has a separate bounded HTTPS startup resolver; see
the [full resolution contract](../../docs/fingerprint-flags.md#webrtc-ip-and-proxy-resolution).

GeoIP lookup failures now reject with `Error`. The timeout defaults to 10
seconds and accepts values greater than zero and at most 60. Creating a
later context with another proxy does not recompute browser-level locale
or timezone/IP.

## CLI

After installation, the package provides the `chromix` executable:

```bash
npx chromix --version
npx chromix install       # pre-download the binary
npx chromix info          # binary / cache info
npx chromix clear-cache
```

Run the registry package without installing it first:

```bash
npx @xiaoxiaofeihh/chromix --version
```

## Versioning

The npm package follows SemVer independently of Chromium's four-part version.
The source checkout targets Chromium `153.0.8010.36` on Linux/Windows and retains
macOS `152.0.7977.82` until its upstream 153 platform release exists. The actual
binary release selected by `stable` or `latest` is shown by `chromix info`.

## License

The Node SDK is available under the BSD 3-Clause License. See [`LICENSE`](LICENSE).
