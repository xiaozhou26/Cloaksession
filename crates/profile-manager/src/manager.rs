use std::fs;
use std::path::{Path, PathBuf};

use multizen_core::{
    CreateProfileInput, ExtensionConfig, MultizenError, Profile, ProfileSummary,
    Result, UpdateProfileInput,
};
use rusqlite::{params, Connection};
use uuid::Uuid;

use crate::fingerprint::default_fingerprint;
use crate::migrate::run_migrations;
use crate::row::{normalize_extensions, row_to_profile, ProfileRow};

pub struct ExtensionRef {
    pub profile_id: String,
    pub data_dir: String,
    pub ext: ExtensionConfig,
}

pub struct ProfileManager {
    conn: Connection,
    profiles_root: PathBuf,
}

impl ProfileManager {
    pub fn new(db_path: &Path, profiles_root: &Path) -> Result<Self> {
        if let Some(parent) = db_path.parent() {
            fs::create_dir_all(parent)?;
        }
        fs::create_dir_all(profiles_root)?;
        let conn = Connection::open(db_path)?;
        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "foreign_keys", "ON")?;
        run_migrations(&conn)?;
        Ok(Self { conn, profiles_root: profiles_root.to_path_buf() })
    }

    pub fn list(&self) -> Result<Vec<ProfileSummary>> {
        let mut stmt = self.conn.prepare(
            "SELECT id, name, tags, last_opened_at, proxy, fingerprint, proxy_country, icon
             FROM profiles ORDER BY updated_at DESC",
        )?;
        let rows = stmt.query_map([], |r| {
            Ok(ProfileRow {
                id: r.get(0)?,
                name: r.get(1)?,
                notes: None,
                tags: r.get::<_, String>(2)?,
                proxy: r.get(4)?,
                fingerprint: r.get::<_, String>(5)?,
                data_dir: String::new(),
                created_at: String::new(),
                updated_at: String::new(),
                last_opened_at: r.get(3)?,
                proxy_country: r.get(6)?,
                extensions: None,
                icon: r.get(7)?,
                start_url: None,
                search_provider: None,
            })
        })?;
        let mut out = Vec::new();
        for row in rows {
            let row = row?;
            let fingerprint: multizen_core::FingerprintConfig =
                serde_json::from_str(&row.fingerprint)?;
            let proxy = row.proxy.as_deref().map(serde_json::from_str).transpose()?;
            let tags: Vec<String> = serde_json::from_str(&row.tags).unwrap_or_default();
            out.push(ProfileSummary {
                id: row.id,
                name: row.name,
                tags,
                last_opened_at: row.last_opened_at,
                is_running: false,
                icon: row.icon,
                proxy,
                timezone: Some(fingerprint.timezone.clone()),
                proxy_country: row.proxy_country,
                device: Some(fingerprint.device),
            });
        }
        Ok(out)
    }

    pub fn get(&self, id: &str) -> Result<Option<Profile>> {
        let row = self.conn.query_row(
            "SELECT id, name, notes, tags, proxy, fingerprint, data_dir,
                    created_at, updated_at, last_opened_at, proxy_country,
                    extensions, icon, start_url, search_provider
             FROM profiles WHERE id = ?",
            params![id],
            |r| {
                Ok(ProfileRow {
                    id: r.get(0)?, name: r.get(1)?, notes: r.get(2)?,
                    tags: r.get(3)?, proxy: r.get(4)?, fingerprint: r.get(5)?,
                    data_dir: r.get(6)?, created_at: r.get(7)?, updated_at: r.get(8)?,
                    last_opened_at: r.get(9)?, proxy_country: r.get(10)?,
                    extensions: r.get(11)?, icon: r.get(12)?,
                    start_url: r.get(13)?, search_provider: r.get(14)?,
                })
            },
        );
        match row {
            Ok(r) => Ok(Some(row_to_profile(r))),
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(e.into()),
        }
    }

    pub fn create(&self, input: CreateProfileInput) -> Result<Profile> {
        let id = Uuid::new_v4().to_string();
        let now = chrono::Utc::now().to_rfc3339();
        let data_dir = self.profiles_root.join(&id);
        fs::create_dir_all(&data_dir)?;

        let mut fingerprint = input
            .full_fingerprint
            .unwrap_or_else(|| default_fingerprint(&id));
        if let Some(patch) = input.fingerprint {
            if let Some(v) = patch.user_agent { fingerprint.user_agent = v; }
            if let Some(v) = patch.locale { fingerprint.locale = v; }
            if let Some(v) = patch.timezone { fingerprint.timezone = v; }
            if let Some(v) = patch.country { fingerprint.country = v; }
        }

        let profile = Profile {
            id: id.clone(),
            name: input.name,
            notes: input.notes,
            tags: input.tags.unwrap_or_default(),
            proxy: input.proxy,
            fingerprint: fingerprint.clone(),
            extensions: input.extensions,
            icon: input.icon,
            start_url: input.start_url,
            search_provider: input.search_provider,
            data_dir: data_dir.to_string_lossy().to_string(),
            created_at: now.clone(),
            updated_at: now,
            last_opened_at: None,
            proxy_country: None,
        };
        self.insert_row(&profile)?;
        Ok(profile)
    }

    pub fn insert_imported(&self, profile: Profile) -> Result<Profile> {
        if self.get(&profile.id)?.is_some() {
            return Err(MultizenError::AlreadyExists(profile.id));
        }
        fs::create_dir_all(&profile.data_dir)?;
        self.insert_row(&profile)?;
        Ok(profile)
    }

    fn insert_row(&self, profile: &Profile) -> Result<()> {
        self.conn.execute(
            "INSERT INTO profiles
             (id, name, notes, tags, proxy, fingerprint, extensions, icon,
              start_url, search_provider, data_dir, created_at, updated_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            params![
                profile.id, profile.name, profile.notes,
                serde_json::to_string(&profile.tags)?,
                profile.proxy.as_ref().map(serde_json::to_string).transpose()?,
                serde_json::to_string(&profile.fingerprint)?,
                profile.extensions.as_ref().map(serde_json::to_string).transpose()?,
                profile.icon, profile.start_url, profile.search_provider,
                profile.data_dir, profile.created_at, profile.updated_at,
            ],
        )?;
        Ok(())
    }

    pub fn update(&self, id: &str, patch: UpdateProfileInput) -> Result<Profile> {
        let existing = self.get(id)?.ok_or_else(|| MultizenError::NotFound(id.to_string()))?;
        let now = chrono::Utc::now().to_rfc3339();

        let proxy_changed = match (&patch.proxy, &existing.proxy) {
            (Some(Some(new)), Some(old)) => serde_json::to_string(new)? != serde_json::to_string(old)?,
            (Some(Some(_)), None) | (Some(None), Some(_)) => true,
            _ => false,
        };

        let mut merged = existing.clone();
        merged.name = patch.name.unwrap_or(existing.name);
        merged.notes = patch.notes.or(existing.notes);
        merged.tags = patch.tags.unwrap_or(existing.tags);
        merged.proxy = match patch.proxy {
            Some(None) => None,
            Some(Some(p)) => Some(p),
            None => existing.proxy,
        };
        merged.extensions = patch.extensions.or(existing.extensions);
        merged.icon = match patch.icon {
            Some(None) => None,
            Some(Some(v)) => Some(v),
            None => existing.icon,
        };
        merged.start_url = match patch.start_url {
            Some(None) => None,
            Some(Some(v)) => Some(v),
            None => existing.start_url,
        };
        merged.search_provider = match patch.search_provider {
            Some(None) => None,
            Some(Some(v)) => Some(v),
            None => existing.search_provider,
        };
        merged.updated_at = now;
        if proxy_changed {
            merged.proxy_country = None;
        }

        // Whole-replace fingerprint: the UI always holds a complete
        // FingerprintConfig (from reconcile/generate), so replace rather
        // than merge individual fields.
        if let Some(fp) = patch.fingerprint {
            merged.fingerprint = fp;
        }

        self.conn.execute(
            "UPDATE profiles SET
               name = ?, notes = ?, tags = ?, proxy = ?, fingerprint = ?,
               extensions = ?, icon = ?, start_url = ?, search_provider = ?,
               updated_at = ?, proxy_country = ?
             WHERE id = ?",
            params![
                merged.name, merged.notes,
                serde_json::to_string(&merged.tags)?,
                merged.proxy.as_ref().map(serde_json::to_string).transpose()?,
                serde_json::to_string(&merged.fingerprint)?,
                merged.extensions.as_ref().map(serde_json::to_string).transpose()?,
                merged.icon, merged.start_url, merged.search_provider,
                merged.updated_at, merged.proxy_country, id,
            ],
        )?;
        Ok(merged)
    }

    pub fn set_proxy_country(&self, id: &str, country: Option<&str>) -> Result<()> {
        self.conn.execute(
            "UPDATE profiles SET proxy_country = ? WHERE id = ?",
            params![country, id],
        )?;
        Ok(())
    }

    pub fn delete(&self, id: &str) -> Result<()> {
        let existing = self.get(id)?;
        self.conn.execute("DELETE FROM profiles WHERE id = ?", params![id])?;
        if let Some(p) = existing {
            let _ = fs::remove_dir_all(&p.data_dir); // best-effort
        }
        Ok(())
    }

    pub fn mark_opened(&self, id: &str) -> Result<()> {
        let now = chrono::Utc::now().to_rfc3339();
        self.conn.execute(
            "UPDATE profiles SET last_opened_at = ? WHERE id = ?",
            params![now, id],
        )?;
        Ok(())
    }

    pub fn all_extension_refs(&self) -> Result<Vec<ExtensionRef>> {
        let mut stmt = self.conn.prepare("SELECT id, data_dir, extensions FROM profiles")?;
        let rows = stmt.query_map([], |r| {
            Ok((r.get::<_, String>(0)?, r.get::<_, String>(1)?, r.get::<_, Option<String>>(2)?))
        })?;
        let mut out = Vec::new();
        for row in rows {
            let (profile_id, data_dir, ext_raw) = row?;
            for ext in normalize_extensions(ext_raw.as_deref()) {
                out.push(ExtensionRef { profile_id: profile_id.clone(), data_dir: data_dir.clone(), ext });
            }
        }
        Ok(out)
    }
}
