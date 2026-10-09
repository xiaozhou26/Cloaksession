package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/xiaozhou26/Cloaksession/desktop/internal/debugger"
)

func TestCatalog(t *testing.T) {
	_, h := testHTTP(t, nil, nil)
	expected := []string{"list_profiles", "launch_profile", "close_profile", "navigate", "click", "type", "extract", "screenshot", "create_profile", "update_profile", "delete_profile", "list_fingerprint_options", "evaluate_js", "wait_for_selector", "list_tabs", "activate_tab", "close_tab", "wait_for_navigation", "wait_for_load", "cdp_send", "get_cookies", "set_cookies", "new_tab"}
	expected = append(expected, "attach_debug_session", "detach_debug_session", "list_browser_sessions", "list_windows")
	for _, tool := range debugger.Definitions() {
		expected = append(expected, tool.Name)
	}
	for _, env := range []string{"", "0", "false", "TRUE", "1", "true", "yes", "on"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("MULTIZEN_MCP_ALLOW_RAW_CDP", env)
			tools := rpc(t, h, "tools/list", nil)["result"].(map[string]any)["tools"].([]any)
			var names []string
			for _, value := range tools {
				tool := value.(map[string]any)
				name := tool["name"].(string)
				names = append(names, name)
				if tool["description"] == "" {
					t.Fatal("missing description")
				}
				schema := tool["inputSchema"].(map[string]any)
				if schema["type"] != "object" {
					t.Fatal(schema)
				}
				if debugger.IsTool(name) || name == "detach_debug_session" || name == "list_windows" {
					if schema["required"].([]any)[0] != "debugSessionId" {
						t.Fatalf("%s missing explicit session routing", name)
					}
				} else if name != "list_profiles" && name != "create_profile" && name != "list_fingerprint_options" && name != "list_browser_sessions" {
					if schema["required"].([]any)[0] != "profileId" {
						t.Fatalf("%s missing profileId", name)
					}
				}
			}
			want := append([]string{}, expected...)
			if !rawCDPEnabled() {
				want = append(want[:19], want[20:]...)
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("catalog %v, want %v", names, want)
			}
		})
	}
}

type invocation struct {
	name string
	args map[string]any
}

func TestProfileMappings(t *testing.T) {
	proxy := map[string]any{"type": "socks5", "host": "proxy", "port": 1080, "username": "private-user", "password": "private-pass"}
	profile := map[string]any{"id": "p", "name": "Profile", "proxy": proxy, "fingerprint": map[string]any{"device": "macbook-pro-14-m3", "userAgent": "agent", "locale": "en-US", "timezone": "UTC", "country": "US", "webgl": map[string]any{"renderer": "retained"}}}
	for _, tc := range []struct {
		name     string
		args     map[string]any
		commands []string
		check    func(*testing.T, []invocation, map[string]any)
	}{
		{"list_profiles", map[string]any{}, []string{"profiles_list"}, func(t *testing.T, calls []invocation, out map[string]any) {
			p := out["profiles"].([]any)[0].(map[string]any)
			if p["isRunning"] != true || p["proxy"].(map[string]any)["hasAuth"] != true {
				t.Fatal(out)
			}
			if _, exists := p["fingerprint"]; exists {
				t.Fatal("extra profile fields leaked")
			}
		}},
		{"launch_profile", map[string]any{"profileId": "p"}, []string{"profiles_get", "profiles_launch"}, nil},
		{"close_profile", map[string]any{"profileId": "p"}, []string{"profiles_close"}, func(t *testing.T, _ []invocation, out map[string]any) {
			if out["closed"] != true {
				t.Fatal(out)
			}
		}},
		{"create_profile", map[string]any{"name": "New", "notes": "note", "tags": []any{"tag"}, "proxy": proxy, "fingerprint": map[string]any{"locale": "fr-FR"}, "seed": "ignored", "startUrl": "https://example.com"}, []string{"profiles_create"}, func(t *testing.T, calls []invocation, out map[string]any) {
			input := calls[0].args["input"].(map[string]any)
			if input["name"] != "New" || input["notes"] != "note" || input["seed"] != nil || input["startUrl"] != "https://example.com" {
				t.Fatal(input)
			}
			if input["proxy"].(map[string]any)["password"] != "private-pass" {
				t.Fatal("backend credentials redacted")
			}
			if out["fingerprint"].(map[string]any)["webgl"] != nil {
				t.Fatal("full fingerprint leaked")
			}
		}},
		{"update_profile", map[string]any{"profileId": "p", "name": "New", "notes": "", "tags": []any{}, "proxy": nil, "seed": "ignored", "fingerprint": map[string]any{"locale": "fr-FR", "country": nil}, "startUrl": "ignored"}, []string{"profiles_get", "profiles_update"}, func(t *testing.T, calls []invocation, out map[string]any) {
			patch := calls[1].args["patch"].(map[string]any)
			if _, exists := patch["proxy"]; exists {
				t.Fatal("null proxy must mean keep")
			}
			if patch["seed"] != nil || patch["startUrl"] != nil || patch["notes"] != "" {
				t.Fatal(patch)
			}
			fp := patch["fingerprint"].(map[string]any)
			if fp["locale"] != "fr-FR" || fp["country"] != "US" || fp["userAgent"] != "agent" || fp["webgl"].(map[string]any)["renderer"] != "retained" {
				t.Fatal(fp)
			}
			if out["appliesOnNextLaunch"] != true {
				t.Fatal(out)
			}
		}},
		{"delete_profile", map[string]any{"profileId": "p"}, []string{"profiles_list", "profiles_close", "profiles_delete"}, func(t *testing.T, _ []invocation, out map[string]any) {
			if out["deleted"] != true {
				t.Fatal(out)
			}
		}},
		{"list_fingerprint_options", map[string]any{}, []string{"fingerprint_devices", "fingerprint_locales"}, func(t *testing.T, _ []invocation, out map[string]any) {
			if !reflect.DeepEqual(out, map[string]any{"devices": []any{"macbook-pro-14-m3"}, "locales": []any{"en-US"}}) {
				t.Fatal(out)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []invocation
			_, h := testHTTP(t, backendFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
				calls = append(calls, invocation{name, clone(args).(map[string]any)})
				switch name {
				case "profiles_list":
					p := clone(profile).(map[string]any)
					p["isRunning"] = true
					return []map[string]any{p}, nil
				case "profiles_get", "profiles_create", "profiles_update":
					return profile, nil
				case "profiles_launch":
					return map[string]any{"profileId": "p", "pid": 123, "cdpEndpoint": "http://127.0.0.1:9222"}, nil
				case "fingerprint_devices":
					return []map[string]any{{"family": "macbook-pro-14-m3", "label": "Mac"}}, nil
				case "fingerprint_locales":
					return []map[string]any{{"locale": "en-US", "label": "English"}}, nil
				default:
					return nil, nil
				}
			}), nil)
			out, failed := toolCall(t, h, tc.name, tc.args)
			if failed {
				t.Fatal(out)
			}
			names := make([]string, 0, len(calls))
			for _, call := range calls {
				names = append(names, call.name)
				if strings.HasPrefix(call.name, "profiles_") && call.name != "profiles_list" && call.name != "profiles_create" {
					if call.args["id"] != "p" || call.args["profileId"] != nil {
						t.Fatal(call)
					}
				}
			}
			if !reflect.DeepEqual(names, tc.commands) {
				t.Fatalf("calls %v, want %v", names, tc.commands)
			}
			data, _ := json.Marshal(out)
			if bytesContainSecrets(string(data)) {
				t.Fatalf("credentials leaked: %s", data)
			}
			if tc.check != nil {
				tc.check(t, calls, out)
			}
		})
	}
	if profile["fingerprint"].(map[string]any)["locale"] != "en-US" {
		t.Fatal("original fingerprint mutated")
	}
}

func bytesContainSecrets(s string) bool {
	return strings.Contains(s, "private-user") || strings.Contains(s, "private-pass")
}

func TestBrowserMappings(t *testing.T) {
	t.Setenv("MULTIZEN_MCP_ALLOW_RAW_CDP", "true")
	for name, extra := range map[string]map[string]any{
		"navigate": {"url": "https://example.com"}, "click": {"selector": "#go"}, "type": {"selector": "input", "text": "hello"},
		"extract": {}, "screenshot": {}, "evaluate_js": {"expression": "1+1", "sessionId": "session"},
		"wait_for_selector": {"selector": "#go"}, "list_tabs": {}, "activate_tab": {"tabId": "tab"}, "close_tab": {"tabId": "tab"},
		"wait_for_navigation": {}, "wait_for_load": {"timeoutMs": 5}, "cdp_send": {"method": "Runtime.evaluate", "params": map[string]any{"expression": "2"}, "sessionId": "session"},
		"get_cookies": {"urls": []any{"https://example.com"}}, "set_cookies": {"cookies": []any{map[string]any{"name": "n", "value": "v"}}}, "new_tab": {"url": "about:blank"},
	} {
		t.Run(name, func(t *testing.T) {
			args := clone(extra).(map[string]any)
			args["profileId"] = "p"
			_, h := testHTTP(t, backendFunc(func(_ context.Context, command string, got map[string]any) (any, error) {
				if command != name || got["profileId"] != "p" {
					t.Fatalf("unexpected route: %s %v", command, got)
				}
				for k, expected := range args {
					b, _ := json.Marshal(expected)
					g, _ := json.Marshal(got[k])
					if string(b) != string(g) {
						t.Fatalf("%s: %s != %s", k, b, g)
					}
				}
				if name == "wait_for_selector" || name == "wait_for_navigation" {
					if got["timeoutMs"] != 30000 {
						t.Fatal("missing timeout default")
					}
				}
				return map[string]any{"backend": name, "result": map[string]any{"value": 42}}, nil
			}), nil)
			out, failed := toolCall(t, h, name, args)
			if failed || out["backend"] != name || out["result"].(map[string]any)["value"] != float64(42) {
				t.Fatal(out)
			}
		})
	}
}

func TestArgumentValidation(t *testing.T) {
	_, h := testHTTP(t, backendFunc(func(context.Context, string, map[string]any) (any, error) {
		t.Error("invalid arguments reached backend")
		return nil, nil
	}), nil)
	for _, tc := range []struct {
		name string
		args any
	}{
		{"list_profiles", nil}, {"navigate", []any{}}, {"navigate", map[string]any{"profileId": "p"}},
		{"navigate", map[string]any{"profileId": 2, "url": "https://example.com"}},
		{"type", map[string]any{"profileId": "p", "selector": "a", "text": nil}},
		{"list_tabs", map[string]any{}}, {"create_profile", map[string]any{"name": nil}},
		{"create_profile", map[string]any{"name": "a", "tags": []any{2}}},
		{"create_profile", map[string]any{"name": "a", "proxy": map[string]any{"host": "h", "type": "http", "port": 65536}}},
		{"create_profile", map[string]any{"name": "a", "proxy": map[string]any{"host": "h", "port": 80}}},
		{"update_profile", map[string]any{"profileId": "p", "fingerprint": map[string]any{"locale": 2}}},
		{"wait_for_selector", map[string]any{"profileId": "p", "selector": "a", "timeoutMs": nil}},
		{"wait_for_selector", map[string]any{"profileId": "p", "selector": "a", "timeoutMs": -1}},
		{"wait_for_selector", map[string]any{"profileId": "p", "selector": "a", "timeoutMs": 1.5}},
		{"get_cookies", map[string]any{"profileId": "p", "urls": []any{nil}}},
		{"set_cookies", map[string]any{"profileId": "p", "cookies": "invalid"}},
	} {
		out, failed := toolCall(t, h, tc.name, tc.args)
		if !failed || out["error"].(map[string]any)["code"] != "INVALID_INPUT" {
			t.Fatalf("%s %v: %v", tc.name, tc.args, out)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{errors.New("profile not found: p"), "PROFILE_NOT_FOUND"}, {errors.New("profile already exists: p"), "ALREADY_EXISTS"},
		{errors.New("launch error: failed"), "LAUNCH_FAILED"}, {errors.New("mcp error: forbidden"), "FORBIDDEN"},
		{errors.New("cdp error: protocol"), "CDP_ERROR"}, {errors.New("config error: invalid"), "INVALID_INPUT"},
		{errors.New("database error: unavailable"), "INTERNAL_ERROR"}, {errors.New("io error: unavailable"), "INTERNAL_ERROR"},
		{errors.New("serde error: malformed"), "INTERNAL_ERROR"}, {errors.New("unexpected"), "INTERNAL_ERROR"},
		{context.Canceled, "INTERNAL_ERROR"}, {context.DeadlineExceeded, "INTERNAL_ERROR"},
		{&ToolError{"FORBIDDEN", "blocked"}, "FORBIDDEN"},
	} {
		t.Run(tc.code+tc.err.Error(), func(t *testing.T) {
			_, h := testHTTP(t, backendFunc(func(context.Context, string, map[string]any) (any, error) {
				return nil, fmt.Errorf("backend: %w", tc.err)
			}), nil)
			out, failed := toolCall(t, h, "click", map[string]any{"profileId": "p", "selector": "a"})
			if !failed || out["error"].(map[string]any)["code"] != tc.code || !strings.Contains(out["error"].(map[string]any)["message"].(string), tc.err.Error()) {
				t.Fatal(out)
			}
		})
	}
	for _, name := range []string{"launch_profile", "update_profile"} {
		_, h := testHTTP(t, backendFunc(func(_ context.Context, command string, _ map[string]any) (any, error) {
			if command != "profiles_get" {
				t.Fatal(command)
			}
			return nil, nil
		}), nil)
		out, failed := toolCall(t, h, name, map[string]any{"profileId": "p"})
		if !failed || out["error"].(map[string]any)["code"] != "PROFILE_NOT_FOUND" {
			t.Fatal(out)
		}
	}
}

func TestProfileFailuresStopDispatch(t *testing.T) {
	for _, failing := range []string{"profiles_get", "profiles_close", "fingerprint_devices", "fingerprint_locales"} {
		t.Run(failing, func(t *testing.T) {
			var calls []string
			_, h := testHTTP(t, backendFunc(func(_ context.Context, command string, _ map[string]any) (any, error) {
				calls = append(calls, command)
				if command == failing {
					return nil, errors.New("failure")
				}
				if command == "profiles_list" {
					return []map[string]any{{"id": "p", "isRunning": true}}, nil
				}
				return []string{"x"}, nil
			}), nil)
			name := "list_fingerprint_options"
			if failing == "profiles_get" {
				name = "launch_profile"
			}
			if failing == "profiles_close" {
				name = "delete_profile"
			}
			_, failed := toolCall(t, h, name, map[string]any{"profileId": "p"})
			if !failed || calls[len(calls)-1] != failing {
				t.Fatal(calls)
			}
		})
	}
}

func TestUnsignedTimeoutBoundaries(t *testing.T) {
	tool, _ := lookupTool("wait_for_selector")
	for _, tc := range []struct {
		value string
		valid bool
	}{{"0", true}, {"18446744073709551615", true}, {"18446744073709551616", false}, {"1.0", false}, {"1e2", false}, {"-1", false}} {
		err := validate(map[string]any{"profileId": "p", "selector": "a", "timeoutMs": json.Number(tc.value)}, tool.schema, "arguments")
		if (err == nil) != tc.valid {
			t.Errorf("%s: %v", tc.value, err)
		}
	}
}

func TestGoBackendErrors(t *testing.T) {
	for _, tc := range []struct{ message, code string }{
		{`no active browser session for profile "p"; call launch first`, "PROFILE_NOT_FOUND"},
		{"CDP error -32000: missing target", "CDP_ERROR"},
		{"CDP connection closed", "CDP_ERROR"},
		{"JavaScript exception: evaluation failed", "CDP_ERROR"},
	} {
		if got := errorCode(errors.New(tc.message)); got != tc.code {
			t.Errorf("%s: %s", tc.message, got)
		}
	}
	_, h := testHTTP(t, backendFunc(func(_ context.Context, command string, _ map[string]any) (any, error) {
		if command == "profiles_get" {
			return map[string]any{"id": "p"}, nil
		}
		return nil, errors.New("browser exited during startup")
	}), nil)
	out, failed := toolCall(t, h, "launch_profile", map[string]any{"profileId": "p"})
	if !failed || out["error"].(map[string]any)["code"] != "LAUNCH_FAILED" {
		t.Fatal(out)
	}
}

func TestDeleteInactiveProfileDoesNotClose(t *testing.T) {
	var calls []string
	_, h := testHTTP(t, backendFunc(func(_ context.Context, command string, _ map[string]any) (any, error) {
		calls = append(calls, command)
		if command == "profiles_list" {
			return []map[string]any{{"id": "p", "isRunning": false}}, nil
		}
		return nil, nil
	}), nil)
	out, failed := toolCall(t, h, "delete_profile", map[string]any{"profileId": "p"})
	if failed || out["deleted"] != true || !reflect.DeepEqual(calls, []string{"profiles_list", "profiles_delete"}) {
		t.Fatal(out, calls)
	}
}
