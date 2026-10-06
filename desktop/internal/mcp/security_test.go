package mcp

import (
	"context"
	"testing"
)

func TestBlockedSchemes(t *testing.T) {
	for _, value := range []string{"file:///etc/passwd", "chrome://settings", "devtools://devtools", "view-source:https://example.com", "fi\tle://x", "chr\nome://x", "\x00\x1f file://x", "\r\nFiLe://x", "CHROME://settings", "VIEW-SOURCE:https://x"} {
		if !hasBlockedScheme(value) {
			t.Errorf("allowed %q", value)
		}
	}
	for _, value := range []string{"https://example.com", "http://example.com", "about:blank", "https://example.com/file:test", "text file://x"} {
		if hasBlockedScheme(value) {
			t.Errorf("blocked %q", value)
		}
	}
}

func TestSecurityGates(t *testing.T) {
	_, h := testHTTP(t, backendFunc(func(context.Context, string, map[string]any) (any, error) {
		t.Error("forbidden call reached backend")
		return nil, nil
	}), nil)
	for _, name := range []string{"navigate", "new_tab"} {
		for _, url := range []string{"file:///etc/passwd", "CHROME://settings", "\x00fi\tle://x", "view-source:https://example.com"} {
			out, failed := toolCall(t, h, name, map[string]any{"profileId": "p", "url": url})
			assertForbidden(t, out, failed)
		}
	}
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"get_cookies", map[string]any{"profileId": "p", "urls": []any{"https://example.com", "devtools://x"}}},
		{"set_cookies", map[string]any{"profileId": "p", "cookies": []any{map[string]any{"nested": []any{map[string]any{"url": "file://x"}}}}}},
	} {
		out, failed := toolCall(t, h, tc.name, tc.args)
		assertForbidden(t, out, failed)
	}
	t.Setenv("MULTIZEN_MCP_ALLOW_RAW_CDP", "")
	out, failed := toolCall(t, h, "cdp_send", map[string]any{"profileId": "p", "method": "Page.navigate", "params": map[string]any{"url": "https://example.com"}})
	assertForbidden(t, out, failed)
	t.Setenv("MULTIZEN_MCP_ALLOW_RAW_CDP", "1")
	for _, method := range []string{"IO.read", "Page.getResourceContent", "Storage.getCookies", "Network.getAllCookies", "Browser.close", "Browser.crash", "DOMStorage.getItem", "IndexedDB.requestDatabase", "CacheStorage.requestCacheNames", "Fetch.enable"} {
		out, failed := toolCall(t, h, "cdp_send", map[string]any{"profileId": "p", "method": method})
		assertForbidden(t, out, failed)
	}
	for _, params := range []any{"file://x", []any{1, "chrome://x"}, map[string]any{"nested": []any{map[string]any{"other": "view-source:https://x"}}}} {
		out, failed := toolCall(t, h, "cdp_send", map[string]any{"profileId": "p", "method": "Page.navigate", "params": params})
		assertForbidden(t, out, failed)
	}
}

func assertForbidden(t *testing.T, out map[string]any, failed bool) {
	t.Helper()
	if !failed || out["error"].(map[string]any)["code"] != "FORBIDDEN" {
		t.Fatal(out)
	}
}

func TestTokenComparison(t *testing.T) {
	for _, tc := range []struct {
		a, b    string
		matches bool
	}{{"abc", "abc", true}, {"abc", "abd", false}, {"a", "abc", false}, {"", "abc", false}, {"", "", false}} {
		if tokenMatches(tc.a, tc.b) != tc.matches {
			t.Errorf("token comparison %q %q", tc.a, tc.b)
		}
	}
}
