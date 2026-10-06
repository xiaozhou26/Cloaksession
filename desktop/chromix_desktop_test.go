package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// This opt-in test uses the real patched browser, including a visible window.
func TestChromixDesktopLaunch(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_CHROMIX")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_CHROMIX to the actual Chromix executable")
	}
	if runtime.GOOS == "darwin" {
		t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	}
	resources, err := filepath.Abs("resources")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"random", "fixed", "custom"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			options := map[string]any{"fingerprintMode": mode}
			if mode != "random" {
				options["fingerprintSeed"] = "18446744073709551615"
			}
			settings, _ := json.Marshal(map[string]any{
				"mcpHttpEnabled": false, "autoUpdate": false, "browserEngine": "chromix", "browserBinaryPath": binary,
				"chromix": map[string]any{"nodePath": "node", "options": options, "environment": map[string]any{}},
			})
			if err := os.WriteFile(filepath.Join(data, "settings.json"), settings, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := newService(data, resources, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			profile := invoke(t, s, "profiles_create", map[string]any{"input": map[string]any{"name": "Actual Chromix " + mode, "startUrl": "about:blank"}}).(map[string]any)
			id := text(profile["id"])
			invoke(t, s, "profiles_launch", map[string]any{"id": id})
			version, err := s.browser.Call(context.Background(), id, "Browser.getVersion", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("actual browser %s: %v", binary, version)
			result, err := s.browser.Tool(context.Background(), "evaluate_js", map[string]any{"profileId": id, "expression": `document.body.innerHTML='<input id="entry"><button>Save</button>'; document.querySelector('button').onclick=()=>{document.body.dataset.result=document.querySelector('#entry').value}; true`})
			if err != nil {
				t.Fatalf("fixture: %v %v", result, err)
			}
			invoke(t, s, "type", map[string]any{"profileId": id, "selector": "#entry", "text": "Actual Chromix"})
			invoke(t, s, "click", map[string]any{"profileId": id, "selector": "button"})
			result = invoke(t, s, "evaluate_js", map[string]any{"profileId": id, "expression": "document.body.dataset.result"})
			payload := result.(map[string]any)["result"].(map[string]any)
			if payload["value"] != "Actual Chromix" {
				t.Fatal(result)
			}
			invoke(t, s, "profiles_close", map[string]any{"id": id})
			if s.browser.IsRunning(id) {
				t.Fatal("Chromix still running after close")
			}
		})
	}
}
