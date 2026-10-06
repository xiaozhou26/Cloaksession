# cloaksession-ui

React 19 + Tailwind v4 + Vite frontend for Cloaksession 1.3.0 and its Wails v2 desktop shell.

## Structure

- `src/App.tsx`: profiles, sheets, activity, and onboarding.
- `src/lib/ipc.ts`: the public typed command and event API.
- `src/lib/wails.ts`: small handwritten host types and the Wails bridge; no generated bindings required.
- `src/types.ts`: mirrors of the Rust core's camelCase JSON types.
- `src/components/`: shared desktop and narrow-screen views.
- `tests/wailsMock.ts`: in-browser mock of the Wails host globals.
- `public/logo.png`: bundled app logo.

## IPC contract

Commands call `window.go.main.App.Invoke(command, args)` with snake_case command names and an argument object (empty when there are no arguments). The Go host returns decoded JSON directly: objects, arrays, strings, numbers, booleans, or null. The frontend does not parse the result again or expect a response envelope. Go string errors are converted to `Error` instances for existing UI error handlers.

Events register through `window.runtime.EventsOn(name, callback)`. Names remain colon-delimited (`profiles:running-changed`, `activity:event`, `profiles:proxy-country-updated`, `extensions:installed`, `update:status`). Wails passes the Rust payload directly, without a host event wrapper. The public listeners still return `Promise<() => void>`; the resolved function cancels only that subscription and is safe to call more than once. Components also clean up registrations that resolve after unmount, including React StrictMode's development remounts.

The update listener preserves the existing unwrapping of the Rust `{ status: UpdateStatus }` payload and accepts direct status payloads. The legacy Chromium bootstrap API remains intentionally stubbed: `chromium.status()` resolves ready, `chromium.retry()` rejects, and `onChromiumStatus()` is a no-op. Per-profile Rust `chromium:status` events describe launches, not the global bootstrap status, so they must not open a blocking download modal.

Other namespaces (profiles, settings, dialogs, activity, system, fingerprint, proxy, extensions, and updates) use the command bridge. Native dialogs and window dragging belong to Wails; draggable regions use `--wails-draggable`.

## Chromix through Playwright

The stored engine value remains `chromix`, but the runtime uses `playwright-core` to launch a local Chromix executable. Select that executable in Settings → Browser binary; there is no SDK browser downloader.

Global settings and profile sheets share three simple fingerprint modes:

- **Random each launch** stores `fingerprintMode: "random"`. The backend generates a fresh cryptographic seed on every launch without writing it back to the profile.
- **Fixed seed** stores `fingerprintMode: "fixed"` and an exact decimal `fingerprintSeed` string for reproducible launches with the same browser/configuration.
- **Custom** stores `fingerprintMode: "custom"` with the same seed contract and offers platform, language, timezone, screen, CPU and memory selections.

The **Random seed** button uses Web Crypto and BigInt to choose a seed once, not once per launch. Simple seeds range from 1 to 18446744073709551615; the native engine treats zero as disabling fingerprinting, so zero/off flags remain advanced-only. Invalid drafts do not replace the last valid seed.

Opening or saving unchanged legacy options does not introduce a mode or rewrite old flags. Profiles inherit global keys unless locally overridden; arrays and nested objects replace their global counterparts. Without an explicit mode, existing advanced settings and the backend's stable per-profile seed behavior remain intact. Explicit modes/seeds take precedence over legacy seed aliases, including nested argument arrays. Random and fixed modes are seed-only and omit old `profile.fingerprint` defaults; custom and absent legacy modes retain those profile fields. Explicit non-seed arguments are still preserved and applied. Choosing **Use global defaults / existing options** removes only the local mode and seed, not other overrides.

Node, environment JSON, raw Playwright options, and the full native parameter catalog are collapsed under **Advanced**. Legacy/unknown JSON keys are retained for compatibility, not promised runtime support: removed SDK-only features may be rejected by direct Playwright. The ordinary-engine fingerprint editor retains its existing stable noise-seed semantics.

## Develop and verify

```bash
npm install --legacy-peer-deps
npm run dev
npm run build
npm test
```

`--legacy-peer-deps` is needed for emoji-mart's React <=18 peer range; the app uses React 19. Vite uses a fixed development port of 5173 and emits the embedded production assets to `dist/`. Start the desktop shell separately for native IPC; a standalone Vite browser does not provide Wails globals.

Playwright starts its own Vite server on port 5174 and uses installed Google Chrome (`channel: "chrome"`; on macOS, `/Applications/Google Chrome.app`). The suite runs desktop (1440×1000) and mobile-width (390×844) viewports against a stateful Wails host mock, while exercising the real `ipc.ts` and `wails.ts` modules. It covers profile CRUD and launch/close, settings and Chromix validation/persistence, onboarding, MCP, payload delivery, isolated unsubscribe, cleanup, and command failures. These browser tests do not launch the native Go/Rust application or a managed browser profile.

The fingerprint parser has a separate Node test suite (requires TypeScript stripping support):

```bash
node --experimental-strip-types --test src/lib/chromixFingerprint.test.mjs
```
