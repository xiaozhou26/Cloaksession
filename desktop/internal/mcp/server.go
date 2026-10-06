// Package mcp implements the local authenticated MCP HTTP endpoint.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBodyBytes = 1024 * 1024

type Server struct {
	backend    Backend
	token      string
	emit       func(string, any)
	mu         sync.Mutex
	httpServer *http.Server
	cancel     context.CancelFunc
	port       int
	closed     bool
	activityMu sync.Mutex
	events     []map[string]any
}

func New(backend Backend, token string, emit func(string, any)) *Server {
	return &Server{backend: backend, token: token, emit: emit, events: make([]map[string]any, 0, activityCapacity)}
}

// Start binds before returning, so bind failures are reported to the caller.
func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("MCP server is closed")
	}
	if s.httpServer != nil {
		return errors.New("MCP server is already started")
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid MCP port: %d", port)
	}
	if s.token == "" {
		return errors.New("MCP bearer token must not be empty")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	s.port = listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.httpServer = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024, BaseContext: func(net.Listener) context.Context { return ctx }}
	server := s.httpServer
	go func() { _ = server.Serve(listener) }()
	return nil
}

// Close drains active calls until ctx expires, then cancels and closes them.
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	server, cancel := s.httpServer, s.cancel
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	cancel()
	if err != nil {
		_ = server.Close()
	}
	return err
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, "GET, HEAD")
			return
		}
		writeJSON(w, map[string]any{"ok": true, "name": "multizen-mcp"})
		return
	}
	if r.URL.Path != "/mcp" && r.URL.Path != "/sse" {
		http.NotFound(w, r)
		return
	}
	if (r.URL.Path == "/mcp" && r.Method != http.MethodPost) || (r.URL.Path == "/sse" && r.Method != http.MethodGet) {
		if r.URL.Path == "/mcp" {
			methodNotAllowed(w, "POST")
		} else {
			methodNotAllowed(w, "GET")
		}
		return
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(r.Header.Values("Authorization")) != 1 || !strings.HasPrefix(auth, "Bearer ") || !tokenMatches(strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")), s.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	port := s.port
	s.mu.Unlock()
	// LocalAddrContextKey supports embedding this handler in another net/http server.
	if port == 0 {
		if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			_, p, err := net.SplitHostPort(addr.String())
			if err == nil {
				port, _ = strconv.Atoi(p)
			}
		}
	}
	p := strconv.Itoa(port)
	if port == 0 || (r.Host != "127.0.0.1:"+p && r.Host != "localhost:"+p) {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/sse" {
		http.Error(w, "sse not wired", http.StatusNotImplemented)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var request any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, rpcError(nil, -32700, "parse error: "+err.Error()))
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(w, rpcError(nil, -32700, "parse error: trailing data"))
		return
	}
	writeJSON(w, s.handleRPC(r.Context(), request))
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}
func rpcResult(id, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func (s *Server) handleRPC(ctx context.Context, value any) map[string]any {
	r, ok := value.(map[string]any)
	if !ok || r == nil {
		return rpcError(nil, -32600, "invalid request")
	}
	id := r["id"]
	if id != nil {
		switch id.(type) {
		case string, json.Number:
		default:
			return rpcError(nil, -32600, "invalid request id")
		}
	}
	method, ok := r["method"].(string)
	if !ok || r["jsonrpc"] != "2.0" {
		return rpcError(id, -32600, "invalid request")
	}
	switch method {
	case "initialize":
		return rpcResult(id, map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo": map[string]any{"name": "cloaksession", "version": "0.1.0"}})
	case "notifications/initialized", "ping":
		return rpcResult(id, map[string]any{})
	case "tools/list":
		return rpcResult(id, map[string]any{"tools": toolDefinitions()})
	case "tools/call":
		params, ok := r["params"].(map[string]any)
		if !ok {
			return rpcError(id, -32602, "tool params must be an object")
		}
		name, ok := params["name"].(string)
		if !ok || name == "" {
			return rpcError(id, -32602, "tool name must be a string")
		}
		t, ok := lookupTool(name)
		if !ok {
			return rpcError(id, -32602, "unknown MCP tool `"+name+"`")
		}
		args, exists := params["arguments"]
		if !exists {
			args = map[string]any{}
		}
		result, err := s.callTool(ctx, t, args)
		var text string
		if err == nil {
			var data []byte
			data, err = json.Marshal(result)
			text = string(data)
		}
		if err != nil {
			payload := map[string]any{"error": map[string]any{"code": errorCode(err), "message": err.Error()}}
			data, _ := json.Marshal(payload)
			text = string(data)
		}
		return rpcResult(id, map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil})
	default:
		return rpcError(id, -32601, "method not found: "+method)
	}
}
