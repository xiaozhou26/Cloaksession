use multizen_core::error::Result;
use rusqlite::Connection;

pub fn run_migrations(conn: &Connection) -> Result<()> {
    conn.execute_batch(
        "CREATE TABLE IF NOT EXISTS profiles (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            notes TEXT,
            tags TEXT NOT NULL DEFAULT '[]',
            proxy TEXT,
            fingerprint TEXT NOT NULL,
            data_dir TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            last_opened_at TEXT
        );
        CREATE INDEX IF NOT EXISTS idx_profiles_name ON profiles(name);",
    )?;
    add_column_if_missing(conn, "proxy_country", "TEXT")?;
    add_column_if_missing(conn, "extensions", "TEXT")?;
    add_column_if_missing(conn, "icon", "TEXT")?;
    add_column_if_missing(conn, "start_url", "TEXT")?;
    add_column_if_missing(conn, "search_provider", "TEXT")?;
    add_column_if_missing(
        conn,
        "chromix_options",
        "TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(chromix_options) AND json_type(chromix_options) = 'object')",
    )?;
    Ok(())
}

fn add_column_if_missing(conn: &Connection, col: &str, definition: &str) -> Result<()> {
    let mut stmt = conn.prepare("PRAGMA table_info(profiles)")?;
    let cols: Vec<String> = stmt
        .query_map([], |r| r.get::<_, String>(1))?
        .filter_map(|r| r.ok())
        .collect();
    if !cols.iter().any(|c| c == col) {
        conn.execute_batch(&format!(
            "ALTER TABLE profiles ADD COLUMN {col} {definition}"
        ))?;
    }
    Ok(())
}
