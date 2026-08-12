# multizen-ui

React 19 + Tailwind v4 + Vite frontend for the MultiZen Tauri shell. Migrated
from the Electron renderer in `apps/desktop/src/renderer/`.

## Structure

```
src/
├── App.tsx                      — root: profile list, sheets, drawers, onboarding
├── main.tsx                     — entry + ErrorBoundary + global error loggers
├── styles.css                   — Tailwind v4 theme + base layers
├── types.ts                     — TS mirrors of Rust serde (camelCase) types
├── lib/
│   ├── ipc.ts                   — Tauri invoke + listen wrappers (19 cmds, 3 events)
│   ├── persisted.ts              — localStorage-backed UI state
│   ├── cn.ts, emojiTint.ts, …    — UI helpers migrated from the renderer
├── components/                   — atoms / profile / screens / mcp / activity / …
├── data/                         — extension catalog (static)
└── public/logo.png               — Cube logo
```

## IPC contract

`lib/ipc.ts` exports 7 namespaces mapping 1:1 to the Tauri commands registered
in `crates/tauri-app/src/lib.rs`. Command channel names are **snake_case**
(`profiles_list`, not `profiles:list`) — Tauri 2.x uses the function name.
Push event names are **colon** strings emitted by Rust
(`profiles:running-changed`, `chromium:status`, `activity:event`).

Stubs: `extensions.*` / `update.*` / `chromium.*` /
`profiles.exportArchive` / `importArchive` return `Promise.reject` or a
no-op unlisten — the backend does not register these in the MVP. Calling
components wrap them in try/catch and surface the error to the user.

## Develop

```bash
npm install --legacy-peer-deps   # emoji-mart peers React <=18
npm run dev                      # Vite dev server (frontend only)
npm run build                    # tsc -b && vite build → dist/
```

`--legacy-peer-deps` is required because `emoji-mart` declares a React <=18
peer dep; it works fine under React 19 at runtime.

## Type fidelity

`types.ts` mirrors the Rust types with `#[serde(rename_all = "camelCase")]`.
`Option<T>` output fields are typed loosely (`T?: … | null`) where the Rust
double-`Option` (keep-vs-clear) semantics can't be expressed in TS; this is
acceptable for the UI and tracked for tightening if a consumer needs the
distinction.

## Known runtime gaps (tracked)

- `onRunningChanged`: aligned to the `{kind, profileId, reason}` shape; the
  `Closing` variant is defined but not yet emitted (atomic close emits
  `Closed`), so the "Terminating…" badge is reserved for a future closing
  phase.
- `fingerprint.devices()/locales()`: return catalog objects
  (`DeviceCatalogEntry[]` / `LocaleCatalogEntry[]`); `screens` are a static
  heuristic (display-only — the injected screen still comes from
  `default_fingerprint()`).
