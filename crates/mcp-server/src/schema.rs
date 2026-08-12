//! MCP tool argument schemas.
//!
//! One struct per MCP tool's `args` object. Each derives `schemars::JsonSchema`
//! (so rmcp can expose the JSON schema to clients) and `serde::Deserialize`
//! (so incoming args objects can be parsed). All fields use `camelCase` on the
//! wire to match the TypeScript / MCP client conventions.
//!
//! Note: `multizen_core::ProxyConfig` and `multizen_core::PartialFingerprintInput`
//! do not derive `JsonSchema` (multizen-core has no schemars dependency). To keep
//! this crate self-contained without touching multizen-core, we define local
//! schema mirrors (`ProxyConfigSchema`, `PartialFingerprintSchema`) that carry
//! `JsonSchema` and are wire-compatible with the core types (same camelCase
//! field names, same `#[serde(rename = "type")]` for proxy_type).

use schemars::JsonSchema;
use serde::Deserialize;

/// Empty args marker. `Default` is the only derive that makes sense for a
/// zero-field struct; `JsonSchema` + `Deserialize` are derived so all arg
/// structs share a uniform shape for downstream generic code.
#[derive(Debug, Default, Deserialize, JsonSchema)]
pub struct ListProfilesArgs {}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct ProfileIdArgs {
    pub profile_id: String,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct NavigateArgs {
    pub profile_id: String,
    pub url: String,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct ClickArgs {
    pub profile_id: String,
    pub selector: String,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct TypeArgs {
    pub profile_id: String,
    pub selector: String,
    pub text: String,
}

/// Wire-compatible schema mirror of `multizen_core::ProxyConfig`.
///
/// Defined locally because multizen-core does not depend on schemars and we
/// intentionally avoid adding a schemars dependency to that crate. Field names
/// and the `#[serde(rename = "type")]` mirror the core type so that values
/// parsed here can be re-serialized and fed into core APIs unchanged.
#[derive(Debug, Clone, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct ProxyConfigSchema {
    #[serde(rename = "type")]
    pub proxy_type: String,
    pub host: String,
    pub port: u16,
    pub username: Option<String>,
    pub password: Option<String>,
}

/// Wire-compatible schema mirror of `multizen_core::PartialFingerprintInput`.
///
/// Only the fields we expose via MCP are listed; all are optional.
#[derive(Debug, Clone, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct PartialFingerprintSchema {
    pub user_agent: Option<String>,
    pub locale: Option<String>,
    pub timezone: Option<String>,
    pub country: Option<String>,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct CreateProfileArgs {
    pub name: String,
    pub notes: Option<String>,
    pub tags: Option<Vec<String>>,
    pub proxy: Option<ProxyConfigSchema>,
    pub fingerprint: Option<PartialFingerprintSchema>,
    pub seed: Option<String>,
}

/// All fields optional except `profile_id`. Mirrors the core `UpdateProfileInput`
/// surface that MCP clients are allowed to touch.
#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct UpdateProfileArgs {
    pub profile_id: String,
    pub name: Option<String>,
    pub notes: Option<String>,
    pub tags: Option<Vec<String>>,
    pub proxy: Option<ProxyConfigSchema>,
    pub fingerprint: Option<PartialFingerprintSchema>,
    pub seed: Option<String>,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct EvaluateJsArgs {
    pub profile_id: String,
    pub expression: String,
    pub session_id: Option<String>,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct WaitForSelectorArgs {
    pub profile_id: String,
    pub selector: String,
    #[serde(default = "default_wait_timeout_ms")]
    pub timeout_ms: u64,
}

fn default_wait_timeout_ms() -> u64 {
    30000
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct WaitForNavigationArgs {
    pub profile_id: String,
    pub timeout_ms: Option<u64>,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct ActivateTabArgs {
    pub profile_id: String,
    pub tab_id: String,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct CloseTabArgs {
    pub profile_id: String,
    pub tab_id: String,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct CdpSendArgs {
    pub profile_id: String,
    pub method: String,
    pub params: Option<serde_json::Value>,
    pub session_id: Option<String>,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct GetCookiesArgs {
    pub profile_id: String,
    pub urls: Vec<String>,
    pub session_id: Option<String>,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct SetCookiesArgs {
    pub profile_id: String,
    pub cookies: Vec<serde_json::Value>,
    pub session_id: Option<String>,
}

#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct NewTabArgs {
    pub profile_id: String,
    pub url: String,
}
