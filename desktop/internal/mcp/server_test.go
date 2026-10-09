package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type backendFunc func(context.Context, string, map[string]any) (any, error)

func (f backendFunc) Invoke(ctx context.Context, name string, args map[string]any) (any, error) {
	return f(ctx, name, args)
}

func testHTTP(t *testing.T, backend Backend, emit func(string, any)) (*Server, *httptest.Server) {
	t.Helper()
	s := New(backend, "test-token", emit)
	h := httptest.NewServer(s)
	t.Cleanup(h.Close)
	return s, h
}

func request(t *testing.T, h *httptest.Server, method, path, host, auth, body string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, h.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		r.Host = host
	}
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	resp, err := h.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

func rpc(t *testing.T, h *httptest.Server, method string, params any) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "call-1", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	status, data := request(t, h, "POST", "/mcp", "", "Bearer test-token", string(body))
	if status != 200 {
		t.Fatalf("status %d: %s", status, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out["id"] != "call-1" {
		t.Fatalf("lost request id: %v", out)
	}
	return out
}

func toolCall(t *testing.T, h *httptest.Server, name string, args any) (map[string]any, bool) {
	t.Helper()
	response := rpc(t, h, "tools/call", map[string]any{"name": name, "arguments": args})
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing tool result: %v", response)
	}
	content := result["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" {
		t.Fatalf("wrong content type: %v", content)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content["text"].(string)), &payload); err != nil {
		t.Fatal(err)
	}
	return payload, result["isError"].(bool)
}

func TestHTTPBoundary(t *testing.T) {
	_, h := testHTTP(t, nil, nil)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(h.URL, "http://"))
	for _, tc := range []struct {
		name, path, method, host, auth string
		status                         int
	}{
		{"health public", "/healthz", "GET", "evil.example", "", 200},
		{"health method", "/healthz", "POST", "", "", 405},
		{"unknown", "/missing", "GET", "", "", 404},
		{"mcp method", "/mcp", "GET", "", "", 405},
		{"sse method", "/sse", "POST", "", "", 405},
		{"missing auth", "/mcp", "POST", "", "", 401},
		{"wrong token", "/mcp", "POST", "", "Bearer test-tokeN", 401},
		{"short token", "/mcp", "POST", "", "Bearer x", 401},
		{"empty bearer", "/mcp", "POST", "", "Bearer ", 401},
		{"lower bearer", "/mcp", "POST", "", "bearer test-token", 401},
		{"basic", "/mcp", "POST", "", "Basic test-token", 401},
		{"external", "/mcp", "POST", "evil.example:" + port, "Bearer test-token", 403},
		{"no port", "/mcp", "POST", "localhost", "Bearer test-token", 403},
		{"wrong port", "/mcp", "POST", "localhost:1", "Bearer test-token", 403},
		{"trailing dot", "/mcp", "POST", "localhost.:" + port, "Bearer test-token", 403},
		{"suffix", "/mcp", "POST", "localhost.evil:" + port, "Bearer test-token", 403},
		{"IPv6", "/mcp", "POST", "[::1]:" + port, "Bearer test-token", 403},
		{"localhost", "/mcp", "POST", "localhost:" + port, "Bearer test-token", 200},
		{"loopback", "/mcp", "POST", "127.0.0.1:" + port, "Bearer test-token", 200},
		{"sse missing auth", "/sse", "GET", "", "", 401},
		{"sse bad host", "/sse", "GET", "evil.example:" + port, "Bearer test-token", 403},
		{"sse unsupported", "/sse", "GET", "", "Bearer test-token", 501},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := request(t, h, tc.method, tc.path, tc.host, tc.auth, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
			if status != tc.status {
				t.Fatalf("status %d, want %d: %s", status, tc.status, body)
			}
		})
	}
	r, _ := http.NewRequest("POST", h.URL+"/mcp", strings.NewReader(`{}`))
	r.Header.Add("Authorization", "Bearer test-token")
	r.Header.Add("Authorization", "Bearer test-token")
	resp, err := h.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("duplicate Authorization accepted")
	}
	status, _ := request(t, h, "POST", "/mcp", "", "Bearer test-token", strings.Repeat(" ", maxBodyBytes+1))
	if status != 400 {
		t.Fatalf("oversized body: %d", status)
	}
}

func TestRPCProtocol(t *testing.T) {
	_, h := testHTTP(t, nil, nil)
	response := rpc(t, h, "initialize", map[string]any{})
	result := response["result"].(map[string]any)
	if result["protocolVersion"] != "2024-11-05" || result["serverInfo"].(map[string]any)["name"] != "cloaksession" {
		t.Fatal(result)
	}
	for _, method := range []string{"ping", "notifications/initialized"} {
		if len(rpc(t, h, method, nil)["result"].(map[string]any)) != 0 {
			t.Fatal(method)
		}
	}
	for _, tc := range []struct {
		body string
		code float64
	}{
		{`{`, -32700}, {`{} {}`, -32700}, {`null`, -32600}, {`[]`, -32600}, {`42`, -32600},
		{`{"jsonrpc":"1.0","method":"ping"}`, -32600}, {`{"jsonrpc":"2.0"}`, -32600},
		{`{"jsonrpc":"2.0","method":"ping","id":{}}`, -32600},
		{`{"jsonrpc":"2.0","id":1,"method":"missing"}`, -32601},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":42}}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"does_not_exist"}}`, -32602},
	} {
		status, body := request(t, h, "POST", "/mcp", "", "Bearer test-token", tc.body)
		var response map[string]any
		_ = json.Unmarshal(body, &response)
		if status != 200 || response["error"].(map[string]any)["code"] != tc.code {
			t.Fatalf("%s: %d %s", tc.body, status, body)
		}
	}
	_, data := request(t, h, "POST", "/mcp", "", "Bearer test-token", `{"jsonrpc":"2.0","method":"ping","id":9007199254740993}`)
	if !bytes.Contains(data, []byte(`9007199254740993`)) {
		t.Fatalf("rounded id: %s", data)
	}
	status, data := request(t, h, "POST", "/mcp", "", "Bearer test-token", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if status != http.StatusAccepted || len(data) != 0 {
		t.Fatalf("notification response: %d %s", status, data)
	}
}

func TestLifecycle(t *testing.T) {
	s := New(nil, "token", nil)
	if err := s.Start(-1); err == nil {
		t.Fatal("invalid port accepted")
	}
	if err := s.Start(65536); err == nil {
		t.Fatal("invalid port accepted")
	}
	if err := New(nil, "", nil).Start(0); err == nil {
		t.Fatal("empty token accepted")
	}
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	if err := s.Start(0); err == nil {
		t.Fatal("double start accepted")
	}
	addr := "127.0.0.1:" + strconv.Itoa(s.port)
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	other := New(nil, "token", nil)
	if err := other.Start(s.port); err == nil {
		_ = other.Close(context.Background())
		t.Fatal("occupied port accepted")
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("listener still open")
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(0); err == nil {
		t.Fatal("restart after close accepted")
	}
	if err := New(nil, "token", nil).Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownAndCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			entered, release, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			s := New(backendFunc(func(ctx context.Context, _ string, _ map[string]any) (any, error) {
				close(entered)
				select {
				case <-release:
					return map[string]any{"clicked": true}, nil
				case <-ctx.Done():
					close(canceled)
					return nil, ctx.Err()
				}
			}), "test-token", nil)
			if err := s.Start(0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close(context.Background()) })
			clientDone := make(chan struct{})
			go func() {
				defer close(clientDone)
				r, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/mcp", s.port), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"click","arguments":{"profileId":"p","selector":"a"}}}`))
				r.Header.Set("Authorization", "Bearer test-token")
				resp, err := http.DefaultClient.Do(r)
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}()
			<-entered
			closeDone := make(chan error, 1)
			if deadline {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer cancel()
				go func() { closeDone <- s.Close(ctx) }()
				if err := <-closeDone; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("close: %v", err)
				}
				select {
				case <-canceled:
				case <-time.After(time.Second):
					t.Fatal("backend not canceled")
				}
			} else {
				go func() { closeDone <- s.Close(context.Background()) }()
				select {
				case err := <-closeDone:
					t.Fatalf("shutdown did not drain: %v", err)
				case <-time.After(20 * time.Millisecond):
				}
				close(release)
				if err := <-closeDone; err != nil {
					t.Fatal(err)
				}
			}
			<-clientDone
		})
	}
}

func TestConcurrentHTTPActivity(t *testing.T) {
	s, h := testHTTP(t, backendFunc(func(context.Context, string, map[string]any) (any, error) {
		return map[string]any{"clicked": true}, nil
	}), func(string, any) {})
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			toolCall(t, h, "click", map[string]any{"profileId": "p", "selector": "a"})
			_ = s.Recent(500)
		}()
	}
	wg.Wait()
	if len(s.Recent(500)) != 40 {
		t.Fatal("lost activity")
	}
	for _, event := range s.Recent(500) {
		if event["status"] != "ok" {
			t.Fatal(event)
		}
	}
}
