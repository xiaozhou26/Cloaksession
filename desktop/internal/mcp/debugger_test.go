package mcp

import (
	"context"
	"reflect"
	"testing"

	"github.com/xiaozhou26/Cloaksession/desktop/internal/debugger"
)

func TestReverseExplicitRoutingAndPassthrough(t *testing.T) {
	t.Setenv("MULTIZEN_MCP_ALLOW_RAW_CDP", "")
	calls := 0
	_, h := testHTTP(t, backendFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		calls++
		if name != "list_scripts" || args["debugSessionId"] != "session-a" {
			t.Fatalf("wrong route %s %v", name, args)
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "upstream failure"}}, "structuredContent": map[string]any{"ok": false}, "isError": true}, nil
	}), nil)
	_, bad := toolCall(t, h, "list_scripts", map[string]any{})
	if !bad || calls != 0 {
		t.Fatal("missing session reached backend")
	}
	result := rpc(t, h, "tools/call", map[string]any{"name": "list_scripts", "arguments": map[string]any{"debugSessionId": "session-a"}})["result"].(map[string]any)
	if result["isError"] != true || result["structuredContent"].(map[string]any)["ok"] != false || result["content"].([]any)[0].(map[string]any)["text"] != "upstream failure" {
		t.Fatal("lost native MCP result", result)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	_, bad = toolCall(t, h, "cdp_send", map[string]any{"profileId": "p", "method": "Debugger.enable"})
	if !bad || calls != 1 {
		t.Fatal("raw CDP gate changed")
	}
}
func TestReverseLifecycleTools(t *testing.T) {
	for _, name := range []string{"attach_debug_session", "detach_debug_session", "list_browser_sessions", "list_windows"} {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{}
			if name == "attach_debug_session" {
				args = map[string]any{"profileId": "p", "nodePath": "untrusted.exe", "endpoint": "http://external"}
			} else if name != "list_browser_sessions" {
				args["debugSessionId"] = "s"
			}
			called := false
			s := New(backendFunc(func(_ context.Context, command string, got map[string]any) (any, error) {
				called = true
				if command != name {
					t.Fatal(command)
				}
				if name == "attach_debug_session" && !reflect.DeepEqual(got, map[string]any{"profileId": "p"}) {
					t.Fatal("runtime override forwarded", got)
				}
				return map[string]any{}, nil
			}), "token", nil)
			tool, ok := lookupTool(name)
			if !ok {
				t.Fatal(name)
			}
			if _, err := s.callTool(context.Background(), tool, args); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("lifecycle tool not dispatched")
			}
		})
	}
}
func TestPinnedReverseCatalogValidation(t *testing.T) {
	if len(debugger.Definitions()) != 24 {
		t.Fatal("expected exactly 24 reverse tools")
	}
	for _, tc := range []struct {
		name  string
		args  map[string]any
		valid bool
	}{
		{"get_paused_info", map[string]any{"debugSessionId": "s", "frameIndex": -1, "includeScopes": true}, true},
		{"get_paused_info", map[string]any{"debugSessionId": "s", "includeScopes": "yes"}, false},
		{"list_scripts", map[string]any{"debugSessionId": "s", "pageSize": 0}, false},
		{"pause_or_resume", map[string]any{"debugSessionId": "s", "action": "toggle"}, false},
		{"pause_or_resume", map[string]any{"debugSessionId": "s", "action": "resume"}, true},
	} {
		tool, ok := lookupTool(tc.name)
		if !ok {
			t.Fatal(tc.name)
		}
		err := validate(tc.args, tool.schema, "arguments")
		if (err == nil) != tc.valid {
			t.Fatalf("%s %v: %v", tc.name, tc.args, err)
		}
	}
}
