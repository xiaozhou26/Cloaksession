package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineMapping(t *testing.T) {
	profile := object{"id": "alpha", "dataDir": t.TempDir(), "startUrl": "https://example.com", "fingerprint": object{"seed": "exact-seed", "device": "windows-desktop-nvidia", "locale": "en-GB", "timezone": "Europe/London", "userAgent": "Test UA", "hardwareConcurrency": 12, "deviceMemory": 16, "screen": object{"width": 1920, "height": 1080}, "availScreen": object{"height": 1040}, "clientHints": object{"secChUa": `"Chromium";v="148", "Google Chrome";v="148"`}, "storageQuota": 123456}}
	for engine, suffix := range map[string]string{"cft": "", "cloakbrowser": "engines/cloakbrowser", "chromix": "engines/chromix"} {
		want := filepath.Join(str(profile, "dataDir"), filepath.FromSlash(suffix))
		if got := profileDir(profile, engine); got != want {
			t.Fatalf("%s directory: %s", engine, got)
		}
	}
	args := strings.Join(spawnArgs(profile, "cloakbrowser", 9222, "socks5://127.0.0.1:8888", "", object{}), "\n")
	for _, flag := range []string{"--fingerprint=" + seedValue("alpha", obj(profile["fingerprint"])), "--fingerprint-platform=windows", "--fingerprint-device-memory=8", "--fingerprint-brand=Chrome", "--fingerprint-taskbar-height=40", "--fingerprint-storage-quota=123456", "--remote-debugging-address=127.0.0.1", "--proxy-server=socks5://127.0.0.1:8888", "--fingerprint-webrtc-ip=auto", "--disable-features=Translate,DnsOverHttps"} {
		if !strings.Contains(args, flag) {
			t.Errorf("missing %s in %s", flag, args)
		}
	}
	cft := strings.Join(spawnArgs(profile, "cft", 1234, "", "", object{}), "\n")
	if strings.Contains(cft, "--fingerprint=") || !strings.Contains(cft, "--user-agent=Test UA") {
		t.Fatal(cft)
	}
	profile["startUrl"] = "file:///secret"
	args = strings.Join(spawnArgs(profile, "cft", 1234, "", "", object{}), "\n")
	if strings.Contains(args, "file://") {
		t.Fatal("unsafe start URL accepted")
	}
}
func TestPlaywrightRequestPreservesSeedAndOverrides(t *testing.T) {
	profile := object{"id": "p", "dataDir": t.TempDir(), "fingerprint": object{"seed": "18446744073709551615"}, "chromixOptions": object{"fingerprintMode": "fixed", "fingerprintSeed": "18446744073709551615"}}
	settings := object{"chromix": object{"options": object{"fingerprintMode": "random", "headless": true}}}
	for _, mode := range []string{"random", "fixed", "custom"} {
		obj(profile["chromixOptions"])["fingerprintMode"] = mode
		r := playwrightRequest(clone(profile), clone(settings), "/browser", "", 1234)
		options := obj(r["options"])
		if options["fingerprintSeed"] != "18446744073709551615" || options["fingerprintMode"] != mode {
			t.Fatal(options)
		}
	}
	if obj(obj(settings["chromix"])["options"])["fingerprintMode"] != "random" {
		t.Fatal("mutated settings")
	}
}
func TestProxyPrecedence(t *testing.T) {
	auth := object{"server": "socks5://127.0.0.1:1080", "username": "user", "password": "pass", "bypass": "localhost"}
	request := object{"proxy": auth, "options": object{"args": []string{"--proxy-server=http://override:80"}}}
	bridge, e := bridgePlaywrightProxy(request)
	if e != nil || bridge != nil {
		t.Fatalf("raw proxy: %v %v", bridge, e)
	}
	obj(request["options"])["contextOptions"] = object{"proxy": auth}
	bridge, e = bridgePlaywrightProxy(request)
	if e != nil || bridge == nil {
		t.Fatalf("explicit auth proxy: %v %v", bridge, e)
	}
	defer bridge.Close()
	proxy := obj(obj(obj(request["options"])["contextOptions"])["proxy"])
	if str(proxy, "server") != bridge.URL() || proxy["username"] != nil || proxy["bypass"] != "localhost" {
		t.Fatal(proxy)
	}
	obj(request["options"])["contextOptions"] = object{"proxy": false}
	if b, e := bridgePlaywrightProxy(request); e != nil || b != nil {
		t.Fatal("explicit false proxy should disable inheritance")
	}
}
func TestPreferencesPreserved(t *testing.T) {
	dir := t.TempDir()
	if e := os.MkdirAll(filepath.Join(dir, "Default"), 0700); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "Default", "Preferences")
	if e := os.WriteFile(p, []byte(`{"extensions":{"test":true},"profile":{"name":"Saved"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := prepareDataDir(dir); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(p)
	var result object
	_ = json.Unmarshal(data, &result)
	if obj(result["profile"])["name"] != "Saved" || obj(result["extensions"])["test"] != true {
		t.Fatal(result)
	}
}
func TestBehavioralTotalsAndKeys(t *testing.T) {
	for _, delta := range []float64{-500, 0, 1234} {
		total := 0.0
		for _, step := range scrollSteps(delta, 42) {
			total += step
		}
		if total-delta > 1e-8 || delta-total > 1e-8 {
			t.Fatal(total, delta)
		}
	}
	if _, e := keyDefinition("Enter"); e != nil {
		t.Fatal(e)
	}
	if _, e := keyDefinition("UnknownKey"); e == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestExtensionsAndReservedArguments(t *testing.T) {
	companion, enabled, disabled := t.TempDir(), t.TempDir(), t.TempDir()
	profile := object{"id": "p", "dataDir": t.TempDir(), "extensions": []any{object{"enabled": true, "dir": enabled}, object{"enabled": false, "dir": disabled}, object{"enabled": true, "dir": filepath.Join(enabled, "missing")}}}
	settings := object{"browserArgs": []any{"--remote-debugging-port=12345", "--user-data-dir=/other", "--headless=new"}, "proxyLocation": object{"latitude": 12.5, "longitude": -40.25}}
	args := strings.Join(spawnArgs(profile, "cloakbrowser", 9222, "socks5://127.0.0.1:1", companion, settings), "\n")
	if !strings.Contains(args, "--load-extension="+companion+","+enabled) || strings.Contains(args, disabled) || strings.Contains(args, "--remote-debugging-port=12345") || strings.Contains(args, "--user-data-dir=/other") || !strings.Contains(args, "--fingerprint-location=12.5,-40.25") {
		t.Fatal(args)
	}
	request := playwrightRequest(profile, object{}, "", companion, 9222)
	if got := request["extensionPaths"].([]string); len(got) != 2 || got[0] != companion || got[1] != enabled {
		t.Fatal(got)
	}
}

func TestBinaryEnvironmentPrecedence(t *testing.T) {
	t.Setenv("MULTIZEN_BROWSER_BINARY", "/legacy/browser")
	t.Setenv("CLOAKBROWSER_BINARY_PATH", "/cloak/browser")
	if defaultBinary("chromix") != "/legacy/browser" {
		t.Fatal("legacy override was not preserved")
	}
	t.Setenv("MULTIZEN_BROWSER_BINARY", "")
	if defaultBinary("cft") != "/cloak/browser" {
		t.Fatal("CloakBrowser override was not preserved")
	}
	t.Setenv("CLOAKBROWSER_BINARY_PATH", "")
	if defaultBinary("chromix") != "" {
		t.Fatal("Playwright installed binary fallback must remain available")
	}
}
