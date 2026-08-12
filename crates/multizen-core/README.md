# multizen-core

Shared types, errors, and serde schema for the multizen-browser-rs workspace.

Exposes `MultizenError`, `Profile` / `ProfileSummary` / `CreateProfileInput` / `UpdateProfileInput`, `FingerprintConfig`, `ProxyConfig`, `AppSettings`, `BrowserEngine`.

All structs serialize with `rename_all = "camelCase"` to stay byte-compatible with the legacy TypeScript `packages/types` schema, so Tauri command return values can be consumed directly by the React UI.
