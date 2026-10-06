package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func mustJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := decodeJSON([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func jsonEqual(t *testing.T, want, got any) {
	t.Helper()
	a, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("JSON mismatch\nwant: %s\ngot:  %s", a, b)
	}
}
func createTestProfile(t *testing.T, s *Store) map[string]any {
	t.Helper()
	p, err := s.ProfileCreate(map[string]any{"name": "Test profile"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLegacyDatabaseFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy-profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "profiles.db"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.ProfileGet("fixture")
		if err != nil {
			s.Close()
			t.Fatal(err)
		}
		if p["notes"] != "Persisted notes" || p["dataDir"] != "/legacy/custom/data" || p["lastOpenedAt"] != "2024-03-01T00:00:00+00:00" {
			t.Fatal(p)
		}
		if p["fingerprint"].(map[string]any)["storageQuota"] != json.Number("18446744073709551615") {
			t.Fatal("fixture number rounded")
		}
		jsonEqual(t, map[string]any{}, p["chromixOptions"])
		var value string
		if err = s.db.QueryRow("SELECT value FROM preserved_custom_table").Scan(&value); err != nil || value != "do not delete" {
			t.Fatal(value, err)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyDatabaseMigration(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE profiles(id TEXT PRIMARY KEY,name TEXT NOT NULL,notes TEXT,tags TEXT NOT NULL DEFAULT '[]',proxy TEXT,fingerprint TEXT NOT NULL,data_dir TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,last_opened_at TEXT); CREATE TABLE child(profile_id TEXT REFERENCES profiles(id));`)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := json.Marshal(FingerprintGenerate("legacy"))
	legacyDir := filepath.Join(dir, "custom-location")
	if err = os.Mkdir(legacyDir, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO profiles(id,name,notes,tags,fingerprint,data_dir,created_at,updated_at,last_opened_at) VALUES(?,?,?,?,?,?,?,?,?)`, "legacy", "Old", "keep me", `["legacy"]`, string(fp), legacyDir, "created", "updated", "opened")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.ProfileGet("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if p["dataDir"] != legacyDir || p["notes"] != "keep me" || p["lastOpenedAt"] != "opened" || p["createdAt"] != "created" {
		t.Fatalf("legacy data changed: %#v", p)
	}
	jsonEqual(t, map[string]any{}, p["chromixOptions"])
	jsonEqual(t, FingerprintGenerate("legacy"), p["fingerprint"])
	rows, err := s.db.Query("PRAGMA table_info(profiles)")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		count++
	}
	rows.Close()
	if count != 16 {
		t.Fatalf("got %d columns", count)
	}
	for _, invalid := range []string{"null", "false", "42", `"text"`, "[]", "not-json"} {
		if _, err = s.db.Exec("UPDATE profiles SET chromix_options=? WHERE id='legacy'", invalid); err == nil {
			t.Fatalf("accepted invalid options %s", invalid)
		}
	}
	if _, err = s.db.Exec("INSERT INTO child VALUES ('missing')"); err == nil {
		t.Fatal("foreign keys disabled")
	}
	if _, err = s.db.Exec("INSERT INTO child VALUES ('legacy')"); err != nil {
		t.Fatal(err)
	}
	if err = s.ProfileDelete("legacy"); err == nil {
		t.Fatal("delete ignored foreign key")
	}
	if _, err = os.Stat(legacyDir); err != nil {
		t.Fatal("failed delete removed directory")
	}
	if _, err = s.db.Exec("DELETE FROM child"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err = s.ProfileGet("legacy")
	if err != nil || p == nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	if err = s.ProfileDelete("legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory not removed: %v", err)
	}
}

const arbitraryOptions = `{"false":false,"null":null,"empty":{},"list":[],"nested":{"future":[1,"text",false,null]},"max":18446744073709551615,"precise":0.123456789012345678901234567890,"exponent":1.234567890123456789e+40}`

func TestProfilesJSONPrecisionAndLifecycle(t *testing.T) {
	s := openTestStore(t)
	options := mustJSON(t, arbitraryOptions)
	fp := FingerprintGenerate("custom")
	fp["storageQuota"] = json.Number("18446744073709551615")
	fp["fontsDir"] = "/custom/fonts"
	p, err := s.ProfileCreate(map[string]any{"name": "Profile", "notes": "note", "tags": []string{"tag"}, "fingerprint": fp, "chromixOptions": options, "proxy": map[string]any{"type": "socks5", "host": "localhost", "port": 1080}, "icon": "star"})
	if err != nil {
		t.Fatal(err)
	}
	id := text(p["id"])
	if len(id) != 36 {
		t.Fatal(id)
	}
	if p["dataDir"] != filepath.Join(s.DataDir, "profiles", id) {
		t.Fatal(p["dataDir"])
	}
	options["max"] = 0
	p, err = s.ProfileGet(id)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, mustJSON(t, arbitraryOptions), p["chromixOptions"])
	jsonEqual(t, fp, p["fingerprint"])
	if err = s.SetProxyCountry(id, "JP"); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkOpened(id); err != nil {
		t.Fatal(err)
	}
	p, err = s.ProfileUpdate(id, map[string]any{"name": "Renamed", "notes": nil, "icon": nil})
	if err != nil {
		t.Fatal(err)
	}
	if p["notes"] != "note" || p["icon"] != nil || p["proxyCountry"] != "JP" || p["lastOpenedAt"] == nil {
		t.Fatal(p)
	}
	p, err = s.ProfileUpdate(id, map[string]any{"proxy": nil, "chromixOptions": map[string]any{"new": false}})
	if err != nil {
		t.Fatal(err)
	}
	if p["proxyCountry"] != nil {
		t.Fatal("proxy country not invalidated")
	}
	jsonEqual(t, map[string]any{"new": false}, p["chromixOptions"])
	list, err := s.ProfilesList()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	if list[0]["isRunning"] != false || list[0]["device"] != "windows-desktop-intel" {
		t.Fatal(list)
	}
	if _, ok := list[0]["dataDir"]; ok {
		t.Fatal("list should return summaries")
	}
	if _, err = s.ProfileUpdate(id, map[string]any{"chromixOptions": []any{}}); err == nil {
		t.Fatal("accepted array options")
	}
	if _, err = s.ProfileUpdate(id, map[string]any{"fingerprint": map[string]any{"locale": "ja-JP"}}); err == nil {
		t.Fatal("accepted incomplete replacement fingerprint")
	}
	p, err = s.ProfileGet(id)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, map[string]any{"new": false}, p["chromixOptions"])
	if p, err = s.ProfileGet("missing"); err != nil || p != nil {
		t.Fatal(p, err)
	}
	if _, err = s.ProfileUpdate("missing", map[string]any{}); err == nil {
		t.Fatal("updated missing profile")
	}
	if err = s.ProfileDelete(id); err != nil {
		t.Fatal(err)
	}
	if err = s.ProfileDelete(id); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(s.DataDir, "profiles", id)); !os.IsNotExist(err) {
		t.Fatal("profile directory remains")
	}
}
func TestPartialFingerprintCreateAndExtensionNormalization(t *testing.T) {
	s := openTestStore(t)
	p, err := s.ProfileCreate(map[string]any{"name": "Partial", "fingerprint": map[string]any{"locale": "ja-JP", "timezone": "Asia/Tokyo"}})
	if err != nil {
		t.Fatal(err)
	}
	fp := p["fingerprint"].(map[string]any)
	if fp["locale"] != "ja-JP" || fp["country"] != "US" {
		t.Fatal("legacy partial input behavior changed")
	}
	_, err = s.db.Exec("UPDATE profiles SET extensions=? WHERE id=?", `[{"id":"extension"},null]`, p["id"])
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.ProfileGet(text(p["id"]))
	if err != nil {
		t.Fatal(err)
	}
	exts := p["extensions"].([]any)
	first := exts[0].(map[string]any)
	if first["enabled"] != true || first["source"] != "file" || first["scope"] != "profile" || first["name"] != "Extension" {
		t.Fatal(exts)
	}
}
func TestSettingsCompatibilityAndPrecision(t *testing.T) {
	s := openTestStore(t)
	got, err := s.SettingsGet()
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, defaultSettings(), got)
	patch := mustJSON(t, `{"browserEngine":"chromix","browserBinaryPath":"/chrome","chromix":{"nodePath":"/node","options":`+arbitraryOptions+`,"environment":{"CHROMIX_CACHE_DIR":"/cache"}},"autoUpdate":false}`)
	got, err = s.SettingsUpdate(patch)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, patch["chromix"], got["chromix"])
	got["chromix"].(map[string]any)["nodePath"] = "mutation"
	got, err = s.SettingsUpdate(map[string]any{"theme": "light", "browserBinaryPath": nil})
	if err != nil {
		t.Fatal(err)
	}
	if got["browserBinaryPath"] != "/chrome" || got["autoUpdate"] != false {
		t.Fatal(got)
	}
	dir := s.DataDir
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	got, err = other.SettingsGet()
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, patch["chromix"], got["chromix"])
	for _, bad := range []map[string]any{{"chromix": map[string]any{"options": nil}}, {"chromix": map[string]any{"options": []any{}}}, {"chromix": map[string]any{"environment": map[string]any{"BAD": 42}}}, {"mcpHttpPort": 65536}, {"browserEngine": "invalid"}} {
		if _, err = other.SettingsUpdate(bad); err == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
	got, err = other.SettingsGet()
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, patch["chromix"], got["chromix"])
	got, err = other.SettingsUpdate(map[string]any{"chromix": map[string]any{"options": map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, map[string]any{"nodePath": "node", "options": map[string]any{}, "environment": map[string]any{}}, got["chromix"])
}
func TestSettingsLegacyFallbacks(t *testing.T) {
	for _, raw := range []string{`{"browserEngine":"bogus","browserBinaryPath":"   "}`, `{ not valid`, `{"chromix":{"options":[]}}`} {
		t.Run(raw, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, err := s.SettingsGet()
			if err != nil {
				t.Fatal(err)
			}
			jsonEqual(t, defaultSettings(), got)
			b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
			if string(b) != raw {
				t.Fatal("load overwrote existing settings")
			}
		})
	}
}
func TestConcurrentStoreOperations(t *testing.T) {
	s := openTestStore(t)
	p := createTestProfile(t, s)
	id := text(p["id"])
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("future%d", i)
			if _, err := s.SettingsUpdate(map[string]any{key: i}); err != nil {
				t.Error(err)
			}
			if _, err := s.ProfileUpdate(id, map[string]any{"name": key}); err != nil {
				t.Error(err)
			}
			if err := s.MarkOpened(id); err != nil {
				t.Error(err)
			}
			if _, err := s.ProfilesList(); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	got, err := s.SettingsGet()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, ok := got[fmt.Sprintf("future%d", i)]; !ok {
			t.Fatal("lost settings patch")
		}
	}
}
func TestFingerprintCatalogAndReconcile(t *testing.T) {
	fp := FingerprintGenerate("seed")
	if err := validateFingerprint(fp); err != nil {
		t.Fatal(err)
	}
	if len(FingerprintDevices().([]map[string]any)) != 19 || len(FingerprintLocales().([]map[string]any)) != 20 {
		t.Fatal("catalog mismatch")
	}
	for _, l := range FingerprintLocales().([]map[string]any) {
		if FingerprintLocaleForCountry(strings.ToLower(text(l["country"]))) != l["id"] {
			t.Fatal(l)
		}
	}
	for country, want := range map[string]any{" sg ": "en-GB", "CH": "de-DE", "MX": "es-ES", "AE": "ar-SA", "UA": "ru-RU", "ZZ": nil} {
		if got := FingerprintLocaleForCountry(country); got != want {
			t.Fatalf("%s: %v != %v", country, got, want)
		}
	}
	out, err := FingerprintReconcile(fp, map[string]any{"localeId": "en-GB", "country": "sg", "timezone": "Asia/Singapore", "device": "linux-desktop-amd", "screen": map[string]any{"width": 1440, "height": 900}, "hardwareConcurrency": 16, "deviceMemory": 32})
	if err != nil {
		t.Fatal(err)
	}
	if out["locale"] != "en-GB" || out["country"] != "SG" || out["acceptLanguage"] != "en-GB,en;q=0.9" || out["platform"] != fp["platform"] {
		t.Fatal(out)
	}
	jsonEqual(t, fp["availScreen"], out["availScreen"])
	if fp["country"] != "US" {
		t.Fatal("mutated input")
	}
	for _, patch := range []map[string]any{{"device": "unknown"}, {"screen": map[string]any{"width": -1, "height": 2}}, {"hardwareConcurrency": 1.5}, {"localeId": 42}} {
		if _, err = FingerprintReconcile(fp, patch); err == nil {
			t.Fatal(patch)
		}
	}
	a := FingerprintLocales().([]map[string]any)
	a[0]["timezones"].([]string)[0] = "mutated"
	if reflect.DeepEqual(a, FingerprintLocales()) {
		t.Fatal("catalog aliases internal storage")
	}
}
