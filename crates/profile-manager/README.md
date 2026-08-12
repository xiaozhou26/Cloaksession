# profile-manager

SQLite-backed profile storage for multizen-browser-rs. 1:1 port of the legacy TS `packages/profile-manager`.

`ProfileManager::new(db_path, profiles_root)` opens (or creates) the DB, runs idempotent migrations, and exposes `list / get / create / insert_imported / update / set_proxy_country / delete / mark_opened / all_extension_refs`.

Schema columns are snake_case; `tags / proxy / fingerprint / extensions` are stored as JSON text, matching the TS version so an existing DB file can be opened without migration.
