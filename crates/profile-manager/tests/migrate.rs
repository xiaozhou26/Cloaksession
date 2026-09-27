use profile_manager::migrate::run_migrations;
use rusqlite::Connection;

fn open_mem() -> Connection {
    Connection::open_in_memory().unwrap()
}

#[test]
fn creates_profiles_table_with_all_columns() {
    let conn = open_mem();
    run_migrations(&conn).unwrap();
    let cols: Vec<String> = conn
        .prepare("PRAGMA table_info(profiles)")
        .unwrap()
        .query_map([], |r| r.get::<_, String>(1))
        .unwrap()
        .filter_map(Result::ok)
        .collect();
    for expected in [
        "id",
        "name",
        "notes",
        "tags",
        "proxy",
        "fingerprint",
        "data_dir",
        "created_at",
        "updated_at",
        "last_opened_at",
        "proxy_country",
        "extensions",
        "icon",
        "start_url",
        "search_provider",
        "chromix_options",
    ] {
        assert!(
            cols.iter().any(|c| c == expected),
            "missing column: {expected}"
        );
    }
}

#[test]
fn is_idempotent() {
    let conn = open_mem();
    run_migrations(&conn).unwrap();
    run_migrations(&conn).unwrap(); // must not error

    // index exists
    let idx: i64 = conn
        .query_row(
            "SELECT count(*) FROM sqlite_master WHERE name='idx_profiles_name'",
            [],
            |r| r.get(0),
        )
        .unwrap();
    assert_eq!(idx, 1);
}

#[test]
fn adds_missing_columns_to_old_schema() {
    // Simulate an old DB that only has the original columns (pre proxy_country etc.)
    let conn = open_mem();
    conn.execute_batch(
        "CREATE TABLE profiles (
            id TEXT PRIMARY KEY, name TEXT NOT NULL, notes TEXT,
            tags TEXT NOT NULL DEFAULT '[]', proxy TEXT, fingerprint TEXT NOT NULL,
            data_dir TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
            last_opened_at TEXT
        );
        CREATE INDEX idx_profiles_name ON profiles(name);",
    )
    .unwrap();
    run_migrations(&conn).unwrap();
    let cols: Vec<String> = conn
        .prepare("PRAGMA table_info(profiles)")
        .unwrap()
        .query_map([], |r| r.get::<_, String>(1))
        .unwrap()
        .filter_map(Result::ok)
        .collect();
    assert!(cols.iter().any(|c| c == "proxy_country"));
    assert!(cols.iter().any(|c| c == "extensions"));
    assert!(cols.iter().any(|c| c == "icon"));
    assert!(cols.iter().any(|c| c == "start_url"));
    assert!(cols.iter().any(|c| c == "search_provider"));
    assert!(cols.iter().any(|c| c == "chromix_options"));
}

#[test]
fn chromix_options_column_defaults_to_json_object_and_rejects_other_types() {
    let conn = open_mem();
    run_migrations(&conn).unwrap();
    conn.execute_batch(
        "INSERT INTO profiles (id, name, fingerprint, data_dir, created_at, updated_at)
         VALUES ('p', 'test', '{}', '/tmp/p', 'created', 'updated');",
    )
    .unwrap();
    let raw: String = conn
        .query_row(
            "SELECT chromix_options FROM profiles WHERE id = 'p'",
            [],
            |r| r.get(0),
        )
        .unwrap();
    assert_eq!(raw, "{}");

    for invalid in ["null", "false", "42", "\"text\"", "[]", "not-json"] {
        assert!(
            conn.execute(
                "UPDATE profiles SET chromix_options = ? WHERE id = 'p'",
                [invalid],
            )
            .is_err(),
            "{invalid}"
        );
    }
    assert!(conn
        .execute(
            "UPDATE profiles SET chromix_options = NULL WHERE id = 'p'",
            [],
        )
        .is_err());
    let expected =
        r#"{"unknown":{"disabled":false,"nullable":null,"seed":"18446744073709551615"}}"#;
    conn.execute(
        "UPDATE profiles SET chromix_options = ? WHERE id = 'p'",
        [expected],
    )
    .unwrap();
    run_migrations(&conn).unwrap();
    let raw: String = conn
        .query_row(
            "SELECT chromix_options FROM profiles WHERE id = 'p'",
            [],
            |r| r.get(0),
        )
        .unwrap();
    assert_eq!(raw, expected);
}
