package mcp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type sessionResponse struct {
	status int
	header http.Header
	body   []byte
	err    error
}

func sessionRequest(h *httptest.Server, method, session, protocol, body string) sessionResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, method, h.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		return sessionResponse{err: err}
	}
	r.Header.Set("Authorization", "Bearer test-token")
	if session != "" {
		r.Header.Set("Mcp-Session-Id", session)
	}
	if protocol != "" {
		r.Header.Set("MCP-Protocol-Version", protocol)
	}
	resp, err := h.Client().Do(r)
	if err != nil {
		return sessionResponse{err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return sessionResponse{status: resp.StatusCode, header: resp.Header, body: data, err: err}
}

func assertSessionStatus(t *testing.T, response sessionResponse, status int) {
	t.Helper()
	if response.err != nil || response.status != status {
		t.Fatalf("HTTP = %d, want %d, error = %v, body = %s", response.status, status, response.err, response.body)
	}
	if (status == http.StatusAccepted || status == http.StatusNoContent) && len(response.body) != 0 {
		t.Fatalf("expected empty HTTP %d body: %s", status, response.body)
	}
}

func initializeSession(t *testing.T, h *httptest.Server, version string) string {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, version)
	response := sessionRequest(h, "POST", "", "", body)
	assertSessionStatus(t, response, http.StatusOK)
	id := response.header.Get("Mcp-Session-Id")
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("invalid random session ID %q: %v", id, err)
	}
	var rpc map[string]any
	if err := json.Unmarshal(response.body, &rpc); err != nil {
		t.Fatal(err)
	}
	expected := version
	if version == "" {
		expected = "2024-11-05"
	} else if !supportedProtocol(version) {
		expected = "2025-06-18"
	}
	if response.header.Get("MCP-Protocol-Version") != expected || rpc["result"].(map[string]any)["protocolVersion"] != expected {
		t.Fatalf("protocol negotiation mismatch: %v %s", response.header, response.body)
	}
	return id
}

func reverseRequest(id, label string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"tools/call","params":{"name":"list_scripts","arguments":{"debugSessionId":%q}}}`, id, label)
}

func cancelRequest(id string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":%s,"reason":"user stopped"}}`, id)
}

func waitBackend(t *testing.T, entered <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-entered:
		return ctx
	case <-time.After(2 * time.Second):
		t.Fatal("backend not entered")
		return nil
	}
}

func waitResponse(t *testing.T, done <-chan sessionResponse) sessionResponse {
	t.Helper()
	select {
	case response := <-done:
		return response
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request did not finish")
		return sessionResponse{}
	}
}

func TestHTTPSessionCancellationIsolation(t *testing.T) {
	entered := make(chan context.Context, 2)
	s, h := testHTTP(t, backendFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		if name != "list_scripts" {
			return nil, fmt.Errorf("unexpected backend command %s", name)
		}
		entered <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}), nil)
	t.Cleanup(s.closeSessions)
	a := initializeSession(t, h, "2025-06-18")
	b := initializeSession(t, h, "2025-06-18")
	if a == b {
		t.Fatal("clients share an HTTP session")
	}
	doneA, doneB := make(chan sessionResponse, 1), make(chan sessionResponse, 1)
	go func() { doneA <- sessionRequest(h, "POST", a, "2025-06-18", reverseRequest("0", "a")) }()
	ctxA := waitBackend(t, entered)
	go func() { doneB <- sessionRequest(h, "POST", b, "2025-06-18", reverseRequest("0", "b")) }()
	ctxB := waitBackend(t, entered)

	duplicate := sessionRequest(h, "POST", a, "", reverseRequest("0", "duplicate"))
	assertSessionStatus(t, duplicate, http.StatusOK)
	if !strings.Contains(string(duplicate.body), "already active") {
		t.Fatalf("duplicate request replaced cancellation entry: %s", duplicate.body)
	}
	for _, tc := range []struct{ session, id string }{{"", "0"}, {a, `"0"`}, {a, "999"}} {
		assertSessionStatus(t, sessionRequest(h, "POST", tc.session, "", cancelRequest(tc.id)), http.StatusAccepted)
		if ctxA.Err() != nil || ctxB.Err() != nil {
			t.Fatal("unscoped, differently typed, or unknown ID cancelled an active request")
		}
	}
	assertSessionStatus(t, sessionRequest(h, "POST", a, "", cancelRequest("0")), http.StatusAccepted)
	responseA := waitResponse(t, doneA)
	assertSessionStatus(t, responseA, http.StatusOK)
	if ctxA.Err() != context.Canceled || !strings.Contains(string(responseA.body), `"isError":true`) {
		t.Fatalf("cancellation did not reach downstream context: %v %s", ctxA.Err(), responseA.body)
	}
	if ctxB.Err() != nil {
		t.Fatal("same request ID in another session was cancelled")
	}
	s.mu.Lock()
	remainingA, remainingB := len(s.sessions[a].active), len(s.sessions[b].active)
	s.mu.Unlock()
	if remainingA != 0 || remainingB != 1 {
		t.Fatalf("active registry = %d, %d", remainingA, remainingB)
	}
	assertSessionStatus(t, sessionRequest(h, "POST", b, "", cancelRequest("0")), http.StatusAccepted)
	assertSessionStatus(t, waitResponse(t, doneB), http.StatusOK)
	if ctxB.Err() != context.Canceled {
		t.Fatal("second client's own cancellation was not forwarded")
	}
}

func TestHTTPSessionCompletionCleanupAndNativePayload(t *testing.T) {
	payload := map[string]any{"content": []any{map[string]any{"type": "text", "text": "native"}}, "structuredContent": map[string]any{"ok": false, "data": map[string]any{"sentinel": 17}}, "isError": true}
	s, h := testHTTP(t, backendFunc(func(context.Context, string, map[string]any) (any, error) {
		return payload, nil
	}), nil)
	id := initializeSession(t, h, "2025-03-26")
	for i := 0; i < 2; i++ {
		response := sessionRequest(h, "POST", id, "", reverseRequest(`"reused"`, "a"))
		assertSessionStatus(t, response, http.StatusOK)
		var rpc map[string]any
		if err := json.Unmarshal(response.body, &rpc); err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(payload)
		got, _ := json.Marshal(rpc["result"])
		if string(got) != string(want) {
			t.Fatalf("native payload changed: %s", response.body)
		}
		s.mu.Lock()
		count := len(s.sessions[id].active)
		s.mu.Unlock()
		if count != 0 {
			t.Fatalf("completed request retained: %d", count)
		}
		assertSessionStatus(t, sessionRequest(h, "POST", id, "", cancelRequest(`"reused"`)), http.StatusAccepted)
	}
	assertSessionStatus(t, sessionRequest(h, "DELETE", id, "", ""), http.StatusNoContent)
	s.mu.Lock()
	count := len(s.sessions)
	s.mu.Unlock()
	if count != 0 {
		t.Fatalf("DELETE retained session: %d", count)
	}
	assertSessionStatus(t, sessionRequest(h, "POST", id, "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`), http.StatusNotFound)
}

func TestHTTPNotificationsNeverDispatchTools(t *testing.T) {
	calls := make(chan struct{}, 1)
	_, h := testHTTP(t, backendFunc(func(context.Context, string, map[string]any) (any, error) {
		calls <- struct{}{}
		return nil, nil
	}), nil)
	id := initializeSession(t, h, "2025-06-18")
	for _, session := range []string{"", id} {
		for _, body := range []string{
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","method":"notifications/unknown","params":{}}`,
			`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":{}}}`,
			`{"jsonrpc":"2.0","method":"notifications/cancelled"}`,
			`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"list_scripts","arguments":{"debugSessionId":"a"}}}`,
		} {
			assertSessionStatus(t, sessionRequest(h, "POST", session, "", body), http.StatusAccepted)
		}
	}
	select {
	case <-calls:
		t.Fatal("notification dispatched a tool")
	default:
	}
}

func TestHTTPSessionDeleteCancelsActiveRequests(t *testing.T) {
	entered := make(chan context.Context, 1)
	s, h := testHTTP(t, backendFunc(func(ctx context.Context, _ string, _ map[string]any) (any, error) {
		entered <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}), nil)
	t.Cleanup(s.closeSessions)
	id := initializeSession(t, h, "2025-06-18")
	done := make(chan sessionResponse, 1)
	go func() { done <- sessionRequest(h, "POST", id, "", reverseRequest("1", "a")) }()
	ctx := waitBackend(t, entered)
	assertSessionStatus(t, sessionRequest(h, "DELETE", id, "2025-06-18", ""), http.StatusNoContent)
	assertSessionStatus(t, waitResponse(t, done), http.StatusOK)
	if ctx.Err() != context.Canceled {
		t.Fatal("DELETE did not cancel backend context")
	}
	assertSessionStatus(t, sessionRequest(h, "DELETE", id, "", ""), http.StatusNotFound)
	assertSessionStatus(t, sessionRequest(h, "DELETE", "", "", ""), http.StatusBadRequest)
}

func TestHTTPSessionHTTPDisconnectAndServerClose(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			entered := make(chan context.Context, 1)
			s, h := testHTTP(t, backendFunc(func(ctx context.Context, _ string, _ map[string]any) (any, error) {
				entered <- ctx
				<-ctx.Done()
				return nil, ctx.Err()
			}), nil)
			t.Cleanup(s.closeSessions)
			id := initializeSession(t, h, "2025-06-18")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r, err := http.NewRequestWithContext(ctx, "POST", h.URL+"/mcp", strings.NewReader(reverseRequest(`"disconnect"`, "a")))
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer test-token")
			r.Header.Set("Mcp-Session-Id", id)
			done := make(chan struct{})
			go func() {
				defer close(done)
				response, err := h.Client().Do(r)
				if err == nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
			}()
			backendCtx := waitBackend(t, entered)
			if shutdown {
				if err := s.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case <-backendCtx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("transport shutdown did not cancel backend")
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP client did not finish")
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				s.mu.Lock()
				session := s.sessions[id]
				clean := session == nil || len(session.active) == 0
				s.mu.Unlock()
				if clean {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("disconnected request retained in registry")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}

func TestHTTPSessionHeaderValidation(t *testing.T) {
	_, h := testHTTP(t, nil, nil)
	id := initializeSession(t, h, "2025-06-18")
	for _, header := range []string{"Mcp-Session-Id", "MCP-Protocol-Version"} {
		for _, values := range [][]string{{""}, {"a", "b"}} {
			r, err := http.NewRequest("POST", h.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer test-token")
			r.Header.Set("Mcp-Session-Id", id)
			r.Header.Del(header)
			for _, value := range values {
				r.Header.Add(header, value)
			}
			response, err := h.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("accepted %s headers %v: %d", header, values, response.StatusCode)
			}
		}
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	response := sessionRequest(h, "POST", id, "", initialize)
	assertSessionStatus(t, response, http.StatusOK)
	if response.header.Get("Mcp-Session-Id") != "" || !strings.Contains(string(response.body), "no existing session") {
		t.Fatalf("reinitialization replaced session: %s", response.body)
	}
}

func TestHTTPSessionProtocolAndBounds(t *testing.T) {
	s, h := testHTTP(t, nil, nil)
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for _, version := range []string{"", "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25", "future"} {
		id := initializeSession(t, h, version)
		assertSessionStatus(t, sessionRequest(h, "POST", id, "", ping), http.StatusOK)
		assertSessionStatus(t, sessionRequest(h, "DELETE", id, "", ""), http.StatusNoContent)
	}
	id := initializeSession(t, h, "2025-06-18")
	assertSessionStatus(t, sessionRequest(h, "POST", id, "2025-06-18", ping), http.StatusOK)
	assertSessionStatus(t, sessionRequest(h, "POST", id, "2024-11-05", ping), http.StatusBadRequest)
	assertSessionStatus(t, sessionRequest(h, "POST", id, "unsupported", ping), http.StatusBadRequest)
	assertSessionStatus(t, sessionRequest(h, "POST", "missing", "", ping), http.StatusNotFound)
	assertSessionStatus(t, sessionRequest(h, "POST", "", "2025-03-26", ping), http.StatusOK)
	assertSessionStatus(t, sessionRequest(h, "POST", "", "", ping), http.StatusOK)

	s.mu.Lock()
	s.sessions[id].lastUsed = time.Now().Add(-sessionIdleTimeout)
	s.mu.Unlock()
	assertSessionStatus(t, sessionRequest(h, "POST", id, "", ping), http.StatusNotFound)
	s.mu.Lock()
	for i := 0; i < maxHTTPSessions; i++ {
		s.sessions[fmt.Sprint(i)] = &httpSession{protocol: "2025-06-18", lastUsed: time.Now(), active: make(map[string]context.CancelFunc)}
	}
	s.mu.Unlock()
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	assertSessionStatus(t, sessionRequest(h, "POST", "", "", initialize), http.StatusServiceUnavailable)
	s.mu.Lock()
	s.sessions["0"].lastUsed = time.Now().Add(-sessionIdleTimeout)
	s.mu.Unlock()
	newID := initializeSession(t, h, "2025-06-18")
	s.mu.Lock()
	count := len(s.sessions)
	_, oldExists := s.sessions["0"]
	for i := 0; i < maxSessionRequests; i++ {
		s.sessions[newID].active[fmt.Sprint(i)] = func() {}
	}
	s.sessions[newID].lastUsed = time.Now().Add(-sessionIdleTimeout)
	s.mu.Unlock()
	if count != maxHTTPSessions || oldExists {
		t.Fatalf("session capacity/expiry broken: %d, old exists %v", count, oldExists)
	}
	assertSessionStatus(t, sessionRequest(h, "POST", newID, "", ping), http.StatusTooManyRequests)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	count = len(s.sessions)
	s.mu.Unlock()
	if count != 0 {
		t.Fatal("server close retained sessions")
	}
	assertSessionStatus(t, sessionRequest(h, "POST", "", "", ping), http.StatusServiceUnavailable)
}
