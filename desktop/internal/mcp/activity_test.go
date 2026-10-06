package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestActivityPendingFinishedAndSanitization(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	emitted := make(chan map[string]any, 4)
	var s *Server
	backend := backendFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		if args["cookies"].([]any)[0].(map[string]any)["value"] != "cookie-secret" {
			t.Error("backend cookie was redacted")
		}
		close(entered)
		<-release
		return map[string]any{"cookies": []any{map[string]any{"name": "session", "value": "cookie-secret"}}, "proxy": map[string]any{"username": "private-user", "password": "private-pass"}}, nil
	})
	s, h := testHTTP(t, backend, func(name string, value any) {
		if name != "activity:event" {
			t.Errorf("event name: %s", name)
		}
		_ = s.Recent(500)
		event := value.(map[string]any)
		emitted <- clone(event).(map[string]any)
		event["status"] = "mutated"
	})
	args := map[string]any{"profileId": "p", "cookies": []any{map[string]any{"name": "session", "value": "cookie-secret"}},
		"nested": map[string]any{"proxy": map[string]any{"username": "private-user", "password": "private-pass", "host": "h"}, "text": strings.Repeat("界", 100), "token": "token-secret"}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		out, failed := toolCall(t, h, "set_cookies", args)
		if failed || out["cookies"].([]any)[0].(map[string]any)["value"] != "cookie-secret" {
			t.Error(out)
		}
	}()
	<-entered
	pending := <-emitted
	if len(pending) != 8 || pending["status"] != "pending" || pending["profileId"] != "p" || pending["summary"] != nil || pending["durationMs"] != nil {
		t.Fatal(pending)
	}
	if len(pending["id"].(string)) != 36 {
		t.Fatal(pending)
	}
	if _, err := time.Parse(time.RFC3339Nano, pending["timestamp"].(string)); err != nil {
		t.Fatal(err)
	}
	if s.Recent(1)[0]["status"] != "pending" {
		t.Fatal("pending call missing")
	}
	data, _ := json.Marshal(pending)
	if strings.Contains(string(data), "cookie-secret") || bytesContainSecrets(string(data)) || strings.Contains(string(data), "token-secret") {
		t.Fatalf("activity secrets leaked: %s", data)
	}
	text := pending["args"].(map[string]any)["nested"].(map[string]any)["text"].(string)
	if len([]rune(text)) != 80 || !strings.HasSuffix(text, "...") {
		t.Fatal(text)
	}
	snapshot := s.Recent(1)
	snapshot[0]["args"].(map[string]any)["profileId"] = "changed"
	if s.Recent(1)[0]["args"].(map[string]any)["profileId"] != "p" {
		t.Fatal("history not isolated")
	}
	close(release)
	<-done
	finished := <-emitted
	if finished["id"] != pending["id"] || finished["timestamp"] != pending["timestamp"] || finished["status"] != "ok" || finished["durationMs"] == nil {
		t.Fatal(finished)
	}
	data, _ = json.Marshal(finished)
	if strings.Contains(string(data), "cookie-secret") || bytesContainSecrets(string(data)) {
		t.Fatalf("summary leaked: %s", data)
	}
	if pending["status"] != "pending" || s.Recent(1)[0]["status"] != "ok" {
		t.Fatal("events not independent snapshots")
	}
}

func TestActivityErrorsAndCapacity(t *testing.T) {
	s, h := testHTTP(t, backendFunc(func(context.Context, string, map[string]any) (any, error) {
		return nil, errors.New("cdp error: cookie-secret")
	}), nil)
	_, failed := toolCall(t, h, "click", map[string]any{"profileId": "p", "selector": "a"})
	if !failed {
		t.Fatal("expected error")
	}
	last := s.Recent(1)[0]
	if last["status"] != "error" || last["summary"] != "CDP_ERROR" {
		t.Fatal(last)
	}
	_, failed = toolCall(t, h, "navigate", map[string]any{"profileId": "p"})
	if !failed || s.Recent(1)[0]["status"] != "error" {
		t.Fatal("invalid input activity not finished")
	}
	for i := 0; i < 505; i++ {
		id := s.startActivity("list_profiles", map[string]any{"sequence": i, "profileId": "ignored"})
		s.finishActivity(id, time.Now(), map[string]any{"ok": true}, nil)
	}
	recent := s.Recent(999)
	if len(recent) != 500 || recent[0]["args"].(map[string]any)["sequence"] != 504 || recent[499]["args"].(map[string]any)["sequence"] != 5 {
		t.Fatal("incorrect ring order/capacity")
	}
	if recent[0]["profileId"] != nil || len(s.Recent(0)) != 0 || len(s.Recent(-1)) != 0 || len(s.Recent(3)) != 3 {
		t.Fatal("incorrect limit/profile semantics")
	}
	ids := map[any]bool{}
	for _, event := range recent {
		if ids[event["id"]] {
			t.Fatal("duplicate event id")
		}
		ids[event["id"]] = true
	}
	empty, _ := json.Marshal(New(nil, "", nil).Recent(10))
	if string(empty) != "[]" {
		t.Fatal(string(empty))
	}
}

func TestRequestContext(t *testing.T) {
	key := struct{}{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key, "present"))
	calls := 0
	s := New(backendFunc(func(got context.Context, _ string, _ map[string]any) (any, error) {
		calls++
		if got.Value(key) != "present" {
			t.Error("context lost")
		}
		return map[string]any{}, nil
	}), "token", nil)
	tool, _ := lookupTool("click")
	if _, err := s.callTool(ctx, tool, map[string]any{"profileId": "p", "selector": "a"}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := s.callTool(ctx, tool, map[string]any{"profileId": "p", "selector": "a"}); !errors.Is(err, context.Canceled) {
		t.Fatal(fmt.Sprint(err))
	}
	if calls != 1 {
		t.Fatal("canceled request reached backend")
	}
}
