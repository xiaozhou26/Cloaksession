// Package store persists the desktop's legacy profiles and settings without a sidecar.
package store

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store owns profiles.db, settings.json, and the profile and extension directories.
// DataDir is absolute and must not be changed after Open.
type Store struct {
	DataDir  string
	mu       sync.Mutex
	db       *sql.DB
	settings map[string]any
	closed   bool
}

func Open(dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = defaultDataDir()
	}
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{dir, filepath.Join(dir, "profiles"), filepath.Join(dir, "extensions")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "profiles.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON"} {
		if _, err = db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DataDir: dir, db: db}, nil
}

func defaultDataDir() string {
	if dir := os.Getenv("CLOAKSESSION_DATA_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.Getenv("APPDATA")
		}
	case "darwin":
		if home != "" {
			base = filepath.Join(home, "Library", "Application Support")
		}
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" && home != "" {
			base = filepath.Join(home, ".local", "share")
		}
	}
	if base == "" {
		base, _ = os.Getwd()
	}
	return filepath.Join(base, "com.cloaksession.browser")
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS profiles (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, notes TEXT,
 tags TEXT NOT NULL DEFAULT '[]', proxy TEXT, fingerprint TEXT NOT NULL,
 data_dir TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 last_opened_at TEXT);
 CREATE INDEX IF NOT EXISTS idx_profiles_name ON profiles(name);`)
	if err != nil {
		return err
	}
	rows, err := tx.Query("PRAGMA table_info(profiles)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range []struct{ name, definition string }{
		{"proxy_country", "TEXT"}, {"extensions", "TEXT"}, {"icon", "TEXT"}, {"start_url", "TEXT"}, {"search_provider", "TEXT"},
		{"chromix_options", `TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(chromix_options) AND json_type(chromix_options) = 'object')`},
	} {
		if !columns[c.name] {
			if _, err = tx.Exec("ALTER TABLE profiles ADD COLUMN " + c.name + " " + c.definition); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func decodeJSON(b []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func cloneMap(m map[string]any) (map[string]any, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err = decodeJSON(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
func text(v any) string { s, _ := v.(string); return s }
func now() string       { return time.Now().UTC().Format(time.RFC3339Nano) }
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

var profileFields = []string{"id", "name", "notes", "tags", "proxy", "fingerprint", "dataDir", "createdAt", "updatedAt", "lastOpenedAt", "proxyCountry", "extensions", "icon", "startUrl", "searchProvider", "chromixOptions"}

const profileColumns = "id, name, notes, tags, proxy, fingerprint, data_dir, created_at, updated_at, last_opened_at, proxy_country, extensions, icon, start_url, search_provider, chromix_options"

var jsonFields = map[string]bool{"tags": true, "proxy": true, "fingerprint": true, "extensions": true, "chromixOptions": true}

type scanner interface{ Scan(...any) error }

func scanProfile(row scanner) (map[string]any, error) {
	values := make([]sql.NullString, len(profileFields))
	dest := make([]any, len(values))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string]any{}
	for i, k := range profileFields {
		v := values[i]
		out[k] = nil
		if !v.Valid {
			continue
		}
		if !jsonFields[k] {
			out[k] = v.String
			continue
		}
		var decoded any
		if err := decodeJSON([]byte(v.String), &decoded); err != nil {
			switch k {
			case "tags":
				decoded = []any{}
			case "extensions":
				decoded = nil
			default:
				return nil, fmt.Errorf("corrupt %s JSON: %w", k, err)
			}
		}
		out[k] = decoded
	}
	if !stringsArray(out["tags"]) {
		out["tags"] = []any{}
	}
	out["extensions"] = normalizeExtensions(out["extensions"])
	return out, nil
}
func normalizeExtensions(v any) any {
	entries, ok := v.([]any)
	if !ok || len(entries) == 0 {
		return nil
	}
	out := make([]any, 0, len(entries))
	for _, entry := range entries {
		obj, _ := entry.(map[string]any)
		e := map[string]any{"id": "", "name": "Extension", "version": "", "enabled": true, "scope": "profile", "dir": "", "source": "file"}
		for k := range e {
			if k == "enabled" {
				if b, ok := obj[k].(bool); ok {
					e[k] = b
				}
			} else if str, ok := obj[k].(string); ok {
				e[k] = str
			}
		}
		out = append(out, e)
	}
	return out
}

type queryer interface{ QueryRow(string, ...any) *sql.Row }

func getProfile(q queryer, id string) (map[string]any, error) {
	return scanProfile(q.QueryRow("SELECT "+profileColumns+" FROM profiles WHERE id = ?", id))
}
func (s *Store) ProfileGet(id string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return getProfile(s.db, id)
}
func (s *Store) ProfilesList() ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT " + profileColumns + " FROM profiles ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		summary := map[string]any{"isRunning": false}
		for _, k := range []string{"id", "name", "tags", "lastOpenedAt", "icon", "proxy", "proxyCountry", "chromixOptions"} {
			summary[k] = p[k]
		}
		fp, _ := p["fingerprint"].(map[string]any)
		summary["timezone"] = fp["timezone"]
		summary["device"] = fp["device"]
		out = append(out, summary)
	}
	return out, rows.Err()
}

func profileArgs(p map[string]any) ([]any, error) {
	args := make([]any, len(profileFields))
	for i, k := range profileFields {
		v := p[k]
		if jsonFields[k] && v != nil {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			args[i] = string(b)
		} else {
			args[i] = v
		}
	}
	return args, nil
}
func insertProfile(tx *sql.Tx, p map[string]any) error {
	args, err := profileArgs(p)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO profiles ("+profileColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", args...)
	return err
}
func (s *Store) ProfileCreate(input map[string]any) (map[string]any, error) {
	in, err := cloneMap(input)
	if err != nil {
		return nil, err
	}
	if _, ok := in["name"].(string); !ok {
		return nil, errors.New("name must be a string")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	stamp := now()
	p := map[string]any{"id": id, "name": in["name"], "notes": nil, "tags": []any{}, "proxy": nil, "fingerprint": FingerprintGenerate(id), "chromixOptions": map[string]any{}, "extensions": nil, "icon": nil, "startUrl": nil, "searchProvider": nil, "dataDir": filepath.Join(s.DataDir, "profiles", id), "createdAt": stamp, "updatedAt": stamp, "lastOpenedAt": nil, "proxyCountry": nil}
	for _, k := range []string{"notes", "tags", "proxy", "chromixOptions", "extensions", "icon", "startUrl", "searchProvider"} {
		if in[k] != nil {
			p[k] = in[k]
		}
	}
	if in["fingerprint"] != nil {
		fp, ok := in["fingerprint"].(map[string]any)
		if !ok {
			return nil, errors.New("fingerprint must be an object")
		}
		if validateFingerprint(fp) == nil {
			p["fingerprint"] = fp
		} else {
			base := p["fingerprint"].(map[string]any)
			for _, k := range []string{"userAgent", "locale", "timezone", "country"} {
				if v := fp[k]; v != nil {
					if _, ok := v.(string); !ok {
						return nil, fmt.Errorf("fingerprint.%s must be a string", k)
					}
					base[k] = v
				}
			}
		}
	}
	if err = validateProfile(p); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	dir := text(p["dataDir"])
	if err = os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	if err = insertProfile(tx, p); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return cloneMap(p)
}
func (s *Store) ProfileUpdate(id string, patch map[string]any) (map[string]any, error) {
	patch, err := cloneMap(patch)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := getProfile(tx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("profile not found: %s", id)
	}
	for _, k := range []string{"name", "notes", "tags", "extensions", "fingerprint", "chromixOptions"} {
		if patch[k] != nil {
			p[k] = patch[k]
		}
	}
	for _, k := range []string{"icon", "startUrl", "searchProvider", "proxy"} {
		if v, ok := patch[k]; ok {
			if k == "proxy" {
				old, _ := json.Marshal(p[k])
				next, _ := json.Marshal(v)
				if !bytes.Equal(old, next) {
					p["proxyCountry"] = nil
				}
			}
			p[k] = v
		}
	}
	p["updatedAt"] = now()
	if err = validateProfile(p); err != nil {
		return nil, err
	}
	args, err := profileArgs(p)
	if err != nil {
		return nil, err
	}
	args = append(args[1:], id)
	_, err = tx.Exec(`UPDATE profiles SET name=?,notes=?,tags=?,proxy=?,fingerprint=?,data_dir=?,created_at=?,updated_at=?,last_opened_at=?,proxy_country=?,extensions=?,icon=?,start_url=?,search_provider=?,chromix_options=? WHERE id=?`, args...)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}
func (s *Store) ProfileDelete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := getProfile(tx, id)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM profiles WHERE id=?", id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if p != nil {
		_ = os.RemoveAll(text(p["dataDir"]))
	}
	return nil
}
func (s *Store) MarkOpened(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE profiles SET last_opened_at=? WHERE id=?", now(), id)
	return err
}

// SetProxyCountry clears the cached country when country is empty.
func (s *Store) SetProxyCountry(id, country string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var value any
	if country != "" {
		value = country
	}
	_, err := s.db.Exec("UPDATE profiles SET proxy_country=? WHERE id=?", value, id)
	return err
}

func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".store-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (s *Store) SettingsGet() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadSettings(); err != nil {
		return nil, err
	}
	return cloneMap(s.settings)
}
func defaultSettings() map[string]any {
	return map[string]any{"theme": "dark", "mcpHttpEnabled": true, "mcpHttpPort": 7777, "browserEngine": "cloakbrowser", "browserBinaryPath": nil, "chromix": map[string]any{"nodePath": "node", "options": map[string]any{}, "environment": map[string]any{}}, "skipBrowserDownload": false, "autoUpdate": true, "usageReporting": false}
}
func (s *Store) loadSettings() error {
	if s.closed {
		return sql.ErrConnDone
	}
	if s.settings != nil {
		return nil
	}
	defaults := defaultSettings()
	var raw map[string]any
	if b, err := os.ReadFile(filepath.Join(s.DataDir, "settings.json")); err == nil && decodeJSON(b, &raw) == nil && raw != nil {
		for k, v := range raw {
			if v != nil {
				defaults[k] = v
			}
		}
		engine := text(defaults["browserEngine"])
		if engine != "cft" && engine != "chromix" && engine != "cloakbrowser" {
			defaults["browserEngine"] = "cloakbrowser"
		}
		if strings.TrimSpace(text(defaults["browserBinaryPath"])) == "" {
			defaults["browserBinaryPath"] = nil
		}
		if err := normalizeSettings(defaults); err != nil {
			defaults = defaultSettings()
		}
	}
	s.settings = defaults
	return nil
}
func (s *Store) SettingsUpdate(patch map[string]any) (map[string]any, error) {
	patch, err := cloneMap(patch)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.loadSettings(); err != nil {
		return nil, err
	}
	merged, err := cloneMap(s.settings)
	if err != nil {
		return nil, err
	}
	for k, v := range patch {
		if v != nil {
			merged[k] = v
		}
	}
	if err = normalizeSettings(merged); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = atomicWrite(filepath.Join(s.DataDir, "settings.json"), b); err != nil {
		return nil, err
	}
	s.settings = merged
	return cloneMap(merged)
}
