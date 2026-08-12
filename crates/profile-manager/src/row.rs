use multizen_core::{ExtensionConfig, FingerprintConfig, Profile, ProxyConfig};

#[derive(Debug, Clone)]
pub struct ProfileRow {
    pub id: String,
    pub name: String,
    pub notes: Option<String>,
    pub tags: String,
    pub proxy: Option<String>,
    pub fingerprint: String,
    pub data_dir: String,
    pub created_at: String,
    pub updated_at: String,
    pub last_opened_at: Option<String>,
    pub proxy_country: Option<String>,
    pub extensions: Option<String>,
    pub icon: Option<String>,
    pub start_url: Option<String>,
    pub search_provider: Option<String>,
}

pub fn row_to_profile(row: ProfileRow) -> Profile {
    let fingerprint: FingerprintConfig =
        serde_json::from_str(&row.fingerprint).expect("corrupt fingerprint JSON");
    let tags: Vec<String> =
        serde_json::from_str(&row.tags).unwrap_or_default();
    let proxy = row.proxy.as_deref().map(|s| {
        serde_json::from_str::<ProxyConfig>(s).expect("corrupt proxy JSON")
    });
    let extensions = normalize_extensions(row.extensions.as_deref());
    Profile {
        id: row.id,
        name: row.name,
        notes: row.notes,
        tags,
        proxy,
        fingerprint,
        extensions: if extensions.is_empty() { None } else { Some(extensions) },
        icon: row.icon,
        start_url: row.start_url,
        search_provider: row.search_provider,
        data_dir: row.data_dir,
        created_at: row.created_at,
        updated_at: row.updated_at,
        last_opened_at: row.last_opened_at,
        proxy_country: row.proxy_country,
    }
}

pub fn normalize_extensions(raw: Option<&str>) -> Vec<ExtensionConfig> {
    let Some(raw) = raw else { return Vec::new() };
    let Ok(parsed) = serde_json::from_str::<serde_json::Value>(raw) else {
        return Vec::new();
    };
    let Some(arr) = parsed.as_array() else { return Vec::new() };
    arr.iter()
        .map(|e| ExtensionConfig {
            id: e.get("id").and_then(|v| v.as_str()).unwrap_or("").to_string(),
            name: e.get("name").and_then(|v| v.as_str()).unwrap_or("Extension").to_string(),
            version: e.get("version").and_then(|v| v.as_str()).unwrap_or("").to_string(),
            scope: e.get("scope").and_then(|v| v.as_str()).unwrap_or("profile").to_string(),
            enabled: e.get("enabled").and_then(|v| v.as_bool()).unwrap_or(true),
            dir: e.get("dir").and_then(|v| v.as_str()).unwrap_or("").to_string(),
            source: e.get("source").and_then(|v| v.as_str()).unwrap_or("file").to_string(),
        })
        .collect()
}
