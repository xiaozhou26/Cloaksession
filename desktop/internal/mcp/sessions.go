package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

const (
	maxHTTPSessions    = 256
	maxSessionRequests = 128
	sessionIdleTimeout = 30 * time.Minute
)

type httpSession struct {
	protocol string
	lastUsed time.Time
	active   map[string]context.CancelFunc
}

func supportedProtocol(version string) bool {
	switch version {
	case "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25":
		return true
	}
	return false
}

func negotiatedProtocol(request map[string]any) string {
	params, _ := request["params"].(map[string]any)
	version, _ := params["protocolVersion"].(string)
	if version == "" {
		return "2024-11-05"
	}
	if supportedProtocol(version) {
		return version
	}
	return "2025-06-18"
}

func requestKey(id any) (string, bool) {
	switch value := id.(type) {
	case string:
		return "s:" + value, true
	case json.Number:
		return "n:" + string(value), true
	}
	return "", false
}

// The caller holds mu; active requests keep their session alive.
func (s *Server) expireSessions(now time.Time) {
	for id, session := range s.sessions {
		if len(session.active) == 0 && now.Sub(session.lastUsed) >= sessionIdleTimeout {
			delete(s.sessions, id)
		}
	}
}

// The caller holds mu. Missing headers remain compatible with stateless clients.
func (s *Server) sessionForHTTP(w http.ResponseWriter, r *http.Request) (*httpSession, bool) {
	if s.closed {
		http.Error(w, "MCP server is closed", http.StatusServiceUnavailable)
		return nil, false
	}
	s.expireSessions(time.Now())
	ids, versions := r.Header.Values("Mcp-Session-Id"), r.Header.Values("MCP-Protocol-Version")
	if len(ids) > 1 || len(versions) > 1 || len(ids) == 1 && ids[0] == "" || len(versions) == 1 && !supportedProtocol(versions[0]) {
		http.Error(w, "invalid MCP session or protocol header", http.StatusBadRequest)
		return nil, false
	}
	var session *httpSession
	if len(ids) == 1 {
		session = s.sessions[ids[0]]
		if session == nil {
			http.Error(w, "MCP session not found", http.StatusNotFound)
			return nil, false
		}
		if len(versions) == 1 && versions[0] != session.protocol {
			http.Error(w, "MCP protocol does not match session", http.StatusBadRequest)
			return nil, false
		}
		session.lastUsed = time.Now()
	}
	return session, true
}

func (s *Server) handleHTTPRPC(w http.ResponseWriter, r *http.Request, value any) {
	request, ok := value.(map[string]any)
	if !ok || request == nil {
		writeJSON(w, rpcError(nil, -32600, "invalid request"))
		return
	}
	method, validMethod := request["method"].(string)
	id, hasID := request["id"]
	key, validID := requestKey(id)
	if request["jsonrpc"] != "2.0" || !validMethod || method == "" || hasID && id != nil && !validID {
		writeJSON(w, rpcError(nil, -32600, "invalid request"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.mu.Lock()
	session, valid := s.sessionForHTTP(w, r)
	if !valid {
		s.mu.Unlock()
		return
	}
	if !hasID {
		if method == "notifications/cancelled" && session != nil {
			params, _ := request["params"].(map[string]any)
			if target, ok := requestKey(params["requestId"]); ok {
				if cancel := session.active[target]; cancel != nil {
					cancel()
				}
			}
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if method == "initialize" {
		if session != nil || !validID {
			s.mu.Unlock()
			writeJSON(w, rpcError(id, -32600, "initialize requires a request ID and no existing session"))
			return
		}
		if len(s.sessions) >= maxHTTPSessions {
			s.mu.Unlock()
			http.Error(w, "MCP session capacity reached; delete an unused session", http.StatusServiceUnavailable)
			return
		}
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			s.mu.Unlock()
			http.Error(w, "cannot create MCP session", http.StatusInternalServerError)
			return
		}
		sessionID := hex.EncodeToString(random[:])
		protocol := negotiatedProtocol(request)
		s.sessions[sessionID] = &httpSession{protocol: protocol, lastUsed: time.Now(), active: make(map[string]context.CancelFunc)}
		s.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", sessionID)
		w.Header().Set("MCP-Protocol-Version", protocol)
		writeJSON(w, s.handleRPC(r.Context(), request))
		return
	}
	ctx := r.Context()
	if session != nil && validID {
		if _, exists := session.active[key]; exists {
			s.mu.Unlock()
			writeJSON(w, rpcError(id, -32600, "request ID is already active in this session"))
			return
		}
		if len(session.active) >= maxSessionRequests {
			s.mu.Unlock()
			http.Error(w, "MCP active request capacity reached", http.StatusTooManyRequests)
			return
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		session.active[key] = cancel
		defer func() {
			cancel()
			s.mu.Lock()
			delete(session.active, key)
			session.lastUsed = time.Now()
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	writeJSON(w, s.handleRPC(ctx, request))
}

func (s *Server) deleteHTTPSession(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, valid := s.sessionForHTTP(w, r)
	if !valid {
		return
	}
	if session == nil {
		http.Error(w, "Mcp-Session-Id is required", http.StatusBadRequest)
		return
	}
	delete(s.sessions, r.Header.Get("Mcp-Session-Id"))
	for _, cancel := range session.active {
		cancel()
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) closeSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		delete(s.sessions, id)
		for _, cancel := range session.active {
			cancel()
		}
	}
}
