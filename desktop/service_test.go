package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testService(t *testing.T) *Service {
	t.Helper()
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"mcpHttpEnabled":false,"autoUpdate":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	resource, _ := filepath.Abs("resources")
	s, err := newService(data, resource, func(dialogRequest) (string, error) { return "", nil }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func invoke(t *testing.T, s *Service, command string, args map[string]any) any {
	t.Helper()
	result, err := s.Invoke(context.Background(), command, args)
	if err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	return result
}

func TestServiceProfileSettingsAndArchives(t *testing.T) {
	s := testService(t)
	for _, command := range []string{"profiles_list", "settings_get", "system_info", "fingerprint_generate", "fingerprint_devices", "fingerprint_locales", "activity_recent", "update_status", "extensions_store_entries"} {
		result := invoke(t, s, command, nil)
		if _, err := json.Marshal(result); err != nil {
			t.Fatal(err)
		}
	}
	created := invoke(t, s, "profiles_create", map[string]any{"input": map[string]any{"name": "Pure Go profile"}}).(map[string]any)
	id := text(created["id"])
	if id == "" {
		t.Fatal("profile ID missing")
	}
	updated := invoke(t, s, "profiles_update", map[string]any{"id": id, "patch": map[string]any{"name": "Renamed", "chromixOptions": map[string]any{"fingerprintMode": "fixed", "fingerprintSeed": "18446744073709551615"}}}).(map[string]any)
	if text(updated["name"]) != "Renamed" {
		t.Fatal(updated)
	}
	archive := filepath.Join(t.TempDir(), "profile.mzar")
	s.dialog = func(request dialogRequest) (string, error) { return archive, nil }
	exported := invoke(t, s, "profiles_export_archive", map[string]any{"id": id, "passphrase": "test-passphrase"}).(map[string]any)
	if exported["ok"] != true {
		t.Fatal(exported)
	}
	imported := invoke(t, s, "profiles_import_archive", map[string]any{"passphrase": "test-passphrase"}).(map[string]any)
	if imported["ok"] != true || imported["id"] == id {
		t.Fatal(imported)
	}
	invoke(t, s, "profiles_delete", map[string]any{"id": id})
	if profile := invoke(t, s, "profiles_get", map[string]any{"id": id}); profile != nil {
		if p, ok := profile.(map[string]any); !ok || p != nil {
			t.Fatal("profile still exists")
		}
	}
	if _, err := s.Invoke(context.Background(), "unknown_command", nil); err == nil {
		t.Fatal("unknown command succeeded")
	}
}

func TestImportsArchiveFromLegacyRustRelease(t *testing.T) {
	s := testService(t)
	archive, err := filepath.Abs("testdata/legacy-rust-profile.mzar")
	if err != nil {
		t.Fatal(err)
	}
	s.dialog = func(dialogRequest) (string, error) { return archive, nil }
	result := invoke(t, s, "profiles_import_archive", map[string]any{"passphrase": "legacy-test-passphrase"}).(map[string]any)
	if result["ok"] != true {
		t.Fatal(result)
	}
	profile := invoke(t, s, "profiles_get", map[string]any{"id": result["id"]}).(map[string]any)
	if profile["name"] != "Legacy Rust archive fixture" {
		t.Fatal(profile)
	}
	options := profile["chromixOptions"].(map[string]any)
	if options["fingerprintSeed"] != "18446744073709551615" {
		t.Fatal(options)
	}
	data, err := os.ReadFile(filepath.Join(text(profile["dataDir"]), "compatibility.txt"))
	if err != nil || string(data) != "legacy browser data\n" {
		t.Fatalf("%s %v", data, err)
	}
}

func TestServiceRealBrowser(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_BROWSER")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_BROWSER for the actual browser integration")
	}
	data := t.TempDir()
	settings, _ := json.Marshal(map[string]any{"mcpHttpEnabled": false, "autoUpdate": false, "browserEngine": "chromix", "browserBinaryPath": binary, "chromix": map[string]any{"nodePath": "node", "options": map[string]any{"headless": true, "fingerprintMode": "random"}, "environment": map[string]any{}}})
	os.WriteFile(filepath.Join(data, "settings.json"), settings, 0600)
	resource, _ := filepath.Abs("resources")
	s, err := newService(data, resource, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	profile := invoke(t, s, "profiles_create", map[string]any{"input": map[string]any{"name": "Pure Go browser", "startUrl": "about:blank"}}).(map[string]any)
	id := text(profile["id"])
	launched := invoke(t, s, "profiles_launch", map[string]any{"id": id}).(map[string]any)
	endpoint := text(launched["cdpEndpoint"])
	if endpoint == "" {
		t.Fatal(launched)
	}
	response, err := http.Get(endpoint + "/json/version")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	invoke(t, s, "profiles_close", map[string]any{"id": id})
	invoke(t, s, "profiles_delete", map[string]any{"id": id})
}

func TestConcurrentLaunchDeleteLeavesNoBrowser(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_BROWSER")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_BROWSER")
	}
	s := testService(t)
	s.settings["browserEngine"] = "chromix"
	s.settings["browserBinaryPath"] = binary
	s.settings["chromix"] = map[string]any{"nodePath": "node", "options": map[string]any{"headless": true, "fingerprintMode": "fixed", "fingerprintSeed": "42"}, "environment": map[string]any{}}
	for i := 0; i < 3; i++ {
		profile := invoke(t, s, "profiles_create", map[string]any{"input": map[string]any{"name": "concurrent lifecycle", "startUrl": "about:blank"}}).(map[string]any)
		id := text(profile["id"])
		launched := make(chan error, 1)
		go func() {
			_, err := s.Invoke(context.Background(), "profiles_launch", map[string]any{"id": id})
			launched <- err
		}()
		time.Sleep(20 * time.Millisecond)
		invoke(t, s, "profiles_delete", map[string]any{"id": id})
		<-launched
		if s.browser.IsRunning(id) {
			t.Fatal("deleted profile left a running browser")
		}
		if profile, err := s.store.ProfileGet(id); err != nil || profile != nil {
			t.Fatalf("deleted profile: %v %v", profile, err)
		}
	}
}

func TestShutdownCancelsOutstandingDialogs(t *testing.T) {
	s := testService(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s.dialog = func(dialogRequest) (string, error) { entered <- struct{}{}; <-release; return "", nil }
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := s.Invoke(context.Background(), "dialog_pick_directory", nil); results <- err }()
	}
	<-entered
	<-entered
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("shutdown blocked on native dialog")
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			t.Fatal("dialog invocation was not canceled")
		}
	}
	close(release)
}

func TestMCPPortConflictKeepsDesktopUsable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	data := t.TempDir()
	settings, _ := json.Marshal(map[string]any{"mcpHttpEnabled": true, "mcpHttpPort": listener.Addr().(*net.TCPAddr).Port, "autoUpdate": false})
	if err = os.WriteFile(filepath.Join(data, "settings.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	resource, _ := filepath.Abs("resources")
	s, err := newService(data, resource, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info := invoke(t, s, "system_info", nil).(map[string]any)
	if info["mcpHttpUrl"] != "" || info["mcpError"] == "" {
		t.Fatal(info)
	}
	invoke(t, s, "profiles_create", map[string]any{"input": map[string]any{"name": "Port conflict still usable"}})
	invoke(t, s, "settings_update", map[string]any{"patch": map[string]any{"mcpHttpEnabled": false}})
}

func TestUpdaterChecksErrorsAndVersions(t *testing.T) {
	for _, test := range []struct {
		body   string
		status int
		kind   string
	}{{`{"tag_name":"v9.0.0","body":"New release","assets":[]}`, 200, "available"}, {`{"tag_name":"v1.0.0","assets":[]}`, 200, "up-to-date"}, {`error`, 500, "error"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); w.Write([]byte(test.body)) }))
		u := newUpdater(nil, nil)
		u.api = server.URL
		_, err := u.check(context.Background())
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if u.status()["kind"] != test.kind {
			t.Fatal(u.status())
		}
		if u.lastChecked() == 0 {
			t.Fatal("missing check timestamp")
		}
	}
	var opened string
	u := newUpdater(nil, func(url string) error { opened = url; return nil })
	if err := u.downloadPage("v1.4.0"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(opened, "/v1.4.0") {
		t.Fatal(opened)
	}
	if err := u.downloadPage("../../bad"); err == nil {
		t.Fatal("invalid release version accepted")
	}
}

func TestProxyGeoParsing(t *testing.T) {
	result, err := parseProxyGeo(strings.NewReader(`{"country_code":"US","timezone":"America/New_York","latitude":1.5,"longitude":"ignored"}`))
	if err != nil || result["country"] != "us" || result["latitude"] != 1.5 || result["longitude"] != nil {
		t.Fatalf("%v %v", result, err)
	}
	if _, err := parseProxyGeo(strings.NewReader(`{"error":true,"reason":"blocked"}`)); err == nil {
		t.Fatal("API error ignored")
	}
}

func TestDataDirectoryOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	t.Setenv("CLOAKSESSION_DATA_DIR", path)
	actual, err := dataDirectory()
	if err != nil || actual != path {
		t.Fatalf("%s %v", actual, err)
	}
}

func TestServiceDebuggerBoundary(t *testing.T) {
	s := testService(t)
	for _, command := range []string{"debugger_sessions", "list_browser_sessions"} {
		result := invoke(t, s, command, nil)
		data, err := json.Marshal(result)
		if err != nil || string(data) != `{"sessions":[]}` {
			t.Fatalf("%s: %s %v", command, data, err)
		}
	}
	tools := invoke(t, s, "debugger_tools", nil)
	data, err := json.Marshal(tools)
	if err != nil || !strings.Contains(string(data), `"debugSessionId"`) {
		t.Fatalf("%s %v", data, err)
	}
	created := invoke(t, s, "profiles_create", map[string]any{"input": map[string]any{"name": "Closed debug profile"}}).(map[string]any)
	for _, command := range []string{"debugger_attach", "attach_debug_session"} {
		for _, id := range []string{"missing", text(created["id"])} {
			if _, err := s.Invoke(context.Background(), command, map[string]any{"profileId": id, "endpoint": "http://127.0.0.1:9222"}); err == nil {
				t.Fatalf("%s accepted closed/unmanaged profile %s", command, id)
			}
		}
	}
	for _, command := range []string{"debugger_detach", "debugger_windows", "list_windows", "list_scripts"} {
		if _, err := s.Invoke(context.Background(), command, nil); err == nil {
			t.Fatalf("%s accepted implicit session", command)
		}
	}
	if _, err := s.Invoke(context.Background(), "debugger_call", map[string]any{"name": "list_scripts", "arguments": map[string]any{}}); err == nil {
		t.Fatal("generic call accepted implicit session")
	}
}

func TestDebugAttachDoesNotWaitForProfileOperationLock(t *testing.T) {
	s := testService(t)
	p := invoke(t, s, "profiles_create", map[string]any{"input": map[string]any{"name": "locked profile"}}).(map[string]any)
	id := text(p["id"])
	unlock := s.lockProfile(id)
	defer unlock()
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go func() { _, err := s.Invoke(ctx, "debugger_attach", map[string]any{"profileId": id}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed browser attached")
		}
	case <-time.After(time.Second):
		t.Fatal("debug attach waited on the profile operation lock")
	}
}
