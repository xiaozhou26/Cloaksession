package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type observedCall struct {
	method, session string
	params          object
}

func fakeCDP(t *testing.T) (string, *[]observedCall, *sync.Mutex) {
	t.Helper()
	calls := []observedCall{}
	mu := &sync.Mutex{}
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"webSocketDebuggerUrl":%q}`, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws")
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, e := upgrader.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		for {
			var message object
			if e = c.ReadJSON(&message); e != nil {
				return
			}
			method := str(message, "method")
			params := obj(message["params"])
			mu.Lock()
			calls = append(calls, observedCall{method, str(message, "sessionId"), params})
			mu.Unlock()
			result := object{}
			switch method {
			case "Target.getTargets":
				result["targetInfos"] = []any{object{"targetId": "first", "type": "page"}, object{"targetId": "second", "type": "page"}}
			case "Target.attachToTarget":
				result["sessionId"] = "session-" + str(params, "targetId")
			case "Target.createTarget":
				result["targetId"] = "second"
			case "Runtime.evaluate":
				if str(params, "expression") == "never" {
					continue
				}
				result["result"] = object{"type": "number", "value": 42}
			case "Test.error":
				_ = c.WriteJSON(object{"id": message["id"], "error": object{"code": -32000, "message": "expected failure"}})
				continue
			}
			if e = c.WriteJSON(object{"id": message["id"], "result": result}); e != nil {
				return
			}
		}
	})
	return server.URL, &calls, mu
}
func TestCDPRoutingCancellationAndErrors(t *testing.T) {
	endpoint, calls, mu := fakeCDP(t)
	ctx := context.Background()
	client, e := connectCDP(ctx, endpoint, "cloakbrowser", object{})
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	if _, e = client.call(ctx, "Runtime.evaluate", object{"expression": "42"}, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = client.call(ctx, "Target.activateTarget", object{"targetId": "second"}, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = client.call(ctx, "Runtime.evaluate", object{"expression": "42"}, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = client.call(ctx, "Runtime.evaluate", object{"expression": "42"}, "explicit-session"); e != nil {
		t.Fatal(e)
	}
	if _, e = client.call(ctx, "Runtime.enable", nil, ""); e == nil {
		t.Fatal("CloakBrowser risky enable allowed")
	}
	if _, e = client.call(ctx, "Test.error", nil, ""); e == nil || !strings.Contains(e.Error(), "expected failure") {
		t.Fatal(e)
	}
	cancelled, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, e = client.call(cancelled, "Runtime.evaluate", object{"expression": "never"}, ""); e == nil {
		t.Fatal("cancellation ignored")
	}
	client.mu.Lock()
	pending := len(client.pending)
	client.mu.Unlock()
	if pending != 0 {
		t.Fatal("pending request leaked")
	}
	if _, e = client.call(ctx, "Runtime.evaluate", object{"expression": "42"}, ""); e != nil {
		t.Fatal("connection unusable after timeout", e)
	}
	mu.Lock()
	defer mu.Unlock()
	sessions := []string{}
	for _, call := range *calls {
		if call.method == "Runtime.evaluate" {
			sessions = append(sessions, call.session)
		}
		if call.method == "Runtime.enable" || call.method == "Network.enable" {
			t.Fatal("unsafe auto-enable")
		}
	}
	if len(sessions) < 3 || sessions[0] != "session-first" || sessions[1] != "session-second" || sessions[2] != "explicit-session" {
		t.Fatal(sessions)
	}
}
func TestCDPDisconnectReleasesPending(t *testing.T) {
	endpoint, _, _ := fakeCDP(t)
	client, e := connectCDP(context.Background(), endpoint, "cft", object{})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := client.request(context.Background(), "Runtime.evaluate", object{"expression": "never"}, "session")
		done <- e
	}()
	time.Sleep(10 * time.Millisecond)
	client.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("disconnect should fail")
		}
	case <-time.After(time.Second):
		t.Fatal("pending request hung")
	}
}
func TestToolShapes(t *testing.T) {
	endpoint, _, _ := fakeCDP(t)
	client, e := connectCDP(context.Background(), endpoint, "cloakbrowser", object{})
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	m := New("", nil)
	m.profiles["test"] = &instance{cdp: client, processDone: make(chan struct{}), tools: make(chan struct{}, 1)}
	cases := []struct {
		name, key string
		args      object
	}{{"evaluate_js", "result", object{"expression": "42"}}, {"list_tabs", "targetInfos", object{}}, {"new_tab", "targetId", object{"url": "about:blank"}}, {"activate_tab", "activated", object{"tabId": "first"}}, {"close_tab", "closed", object{"tabId": "second"}}, {"set_cookies", "set", object{"cookies": []any{}}}}
	for _, tc := range cases {
		tc.args["profileId"] = "test"
		r, e := m.Tool(context.Background(), tc.name, tc.args)
		if e != nil {
			t.Fatal(tc.name, e)
		}
		if _, ok := obj(r)[tc.key]; !ok {
			t.Fatal(tc.name, r)
		}
	}
}
