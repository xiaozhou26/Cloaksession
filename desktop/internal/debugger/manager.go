package debugger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Session struct {
	DebugSessionID string `json:"debugSessionId"`
	ProfileID      string `json:"profileId"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
	AllowedRoot    string `json:"allowedRoot"`
	Error          string `json:"error,omitempty"`
}
type session struct {
	info     Session
	endpoint string
	client   *client
	gate     chan struct{}
}

type pendingAttach struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Manager struct {
	resourceDir, dataDir string
	endpoint             func(string) (string, error)
	emit                 func(string, any)
	mu                   sync.Mutex
	sessions             map[string]*session
	profiles             map[string]map[string]struct{}
	pending              map[string]map[*pendingAttach]struct{}
	closingProfiles      map[string]int
	closed               bool
	start                func(*exec.Cmd) (*client, error)
}

func New(resourceDir, dataDir string, endpoint func(string) (string, error), emit func(string, any)) *Manager {
	return &Manager{resourceDir: resourceDir, dataDir: dataDir, endpoint: endpoint, emit: emit, sessions: map[string]*session{}, profiles: map[string]map[string]struct{}{}, pending: map[string]map[*pendingAttach]struct{}{}, closingProfiles: map[string]int{}, start: startClient}
}
func (m *Manager) event(info Session) {
	if m.emit != nil {
		m.emit("debugger:session-changed", info)
	}
}
func snapshot(s *session) Session {
	info := s.info
	if err := s.client.err(); err != nil {
		info.Status = "error"
		info.Error = err.Error()
	}
	return info
}
func (m *Manager) Sessions() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, snapshot(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}
func (m *Manager) Attach(ctx context.Context, profileID, nodePath string) (Session, error) {
	if profileID == "" {
		return Session{}, errors.New("profileId is required")
	}
	ready, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pending := &pendingAttach{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	if m.closed || m.closingProfiles[profileID] != 0 {
		m.mu.Unlock()
		return Session{}, errors.New("debugger or profile is closing")
	}
	if m.pending[profileID] == nil {
		m.pending[profileID] = map[*pendingAttach]struct{}{}
	}
	m.pending[profileID][pending] = struct{}{}
	m.mu.Unlock()
	var child *client
	published := false
	defer func() {
		if published {
			return
		}
		if child != nil {
			child.Close()
		}
		m.mu.Lock()
		delete(m.pending[profileID], pending)
		if len(m.pending[profileID]) == 0 {
			delete(m.pending, profileID)
		}
		close(pending.done)
		m.mu.Unlock()
	}()
	if err := ready.Err(); err != nil {
		return Session{}, err
	}
	endpoint, err := m.endpoint(profileID)
	if err != nil {
		return Session{}, err
	}
	if err = validateEndpoint(endpoint); err != nil {
		return Session{}, err
	}

	node, err := nodeExecutable(nodePath)
	if err != nil {
		return Session{}, err
	}
	bridge, err := bridgePath(m.resourceDir)
	if err != nil {
		return Session{}, err
	}
	root, err := filepath.Abs(m.dataDir)
	if err != nil {
		return Session{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Session{}, err
	}
	digest := sha256.Sum256([]byte(profileID))
	allowed := filepath.Join(root, "debugger", hex.EncodeToString(digest[:16]))
	if _, err = confinedPath(root, allowed); err != nil {
		return Session{}, err
	}
	if err = os.MkdirAll(allowed, 0700); err != nil {
		return Session{}, err
	}
	allowed, err = filepath.EvalSymlinks(allowed)
	if err != nil || !inside(root, allowed) {
		return Session{}, errors.New("debug allowedRoot escaped the configuration directory")
	}
	cmd := exec.Command(node, bridge, "--browserUrl", endpoint, "--allowedRoots", allowed)
	cmd.Dir = allowed
	cmd.Env = safeEnvironment()
	if err = ready.Err(); err != nil {
		return Session{}, err
	}
	child, err = m.start(cmd)
	if err != nil {
		return Session{}, fmt.Errorf("start reverse debugger: %w", err)
	}
	if err = child.initialize(ready); err != nil {
		return Session{}, err
	}
	if err = checkTools(ready, child); err != nil {
		return Session{}, err
	}
	if err = checkBrowser(ready, child); err != nil {
		return Session{}, err
	}
	current, e := m.endpoint(profileID)
	if e != nil || current != endpoint {
		return Session{}, errors.New("managed profile closed or changed while attaching")
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return Session{}, err
	}
	info := Session{DebugSessionID: hex.EncodeToString(id[:]), ProfileID: profileID, Status: "attached", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), AllowedRoot: allowed}
	s := &session{info: info, endpoint: endpoint, client: child, gate: make(chan struct{}, 1)}
	m.mu.Lock()
	if err = ready.Err(); err != nil {
		m.mu.Unlock()
		return Session{}, err
	}
	published = true
	m.sessions[info.DebugSessionID] = s
	if m.profiles[profileID] == nil {
		m.profiles[profileID] = map[string]struct{}{}
	}
	m.profiles[profileID][info.DebugSessionID] = struct{}{}
	delete(m.pending[profileID], pending)
	if len(m.pending[profileID]) == 0 {
		delete(m.pending, profileID)
	}
	close(pending.done)
	m.mu.Unlock()
	m.event(info)
	go func() {
		<-child.done
		m.mu.Lock()
		active := m.sessions[info.DebugSessionID] == s
		m.mu.Unlock()
		if active {
			m.event(snapshot(s))
		}
	}()
	return info, nil
}
func checkBrowser(ctx context.Context, c *client) error {
	var result struct {
		Content []map[string]any `json:"content"`
		IsError bool             `json:"isError"`
	}
	if err := c.request(ctx, "tools/call", map[string]any{"name": "select_page", "arguments": map[string]any{}}, &result); err != nil {
		return fmt.Errorf("attach reverse browser: %w", err)
	}
	if result.IsError {
		return errors.New("attach reverse browser: select_page returned an MCP tool error")
	}
	if result.Content == nil {
		return errors.New("attach reverse browser: invalid select_page result")
	}
	return nil
}

func checkTools(ctx context.Context, c *client) error {
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 8; page++ {
		var result struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := c.request(ctx, "tools/list", params, &result); err != nil {
			return err
		}
		for _, t := range result.Tools {
			if !IsTool(t.Name) || seen[t.Name] {
				return fmt.Errorf("unexpected reverse tool %q", t.Name)
			}
			seen[t.Name] = true
		}
		if result.NextCursor == "" {
			if len(seen) != len(Definitions()) {
				return errors.New("reverse package must expose all 24 pinned tools")
			}
			return nil
		}
		if result.NextCursor == cursor {
			return errors.New("reverse tools cursor did not advance")
		}
		cursor = result.NextCursor
	}
	return errors.New("reverse tools pagination exceeded limit")
}

// removeLocked only changes indexes; child cleanup must run without m.mu.
func (m *Manager) removeLocked(id string) *session {
	s := m.sessions[id]
	if s != nil {
		delete(m.sessions, id)
		delete(m.profiles[s.info.ProfileID], id)
		if len(m.profiles[s.info.ProfileID]) == 0 {
			delete(m.profiles, s.info.ProfileID)
		}
	}
	return s
}
func (m *Manager) stopSession(s *session) {
	s.client.Close()
	info := s.info
	info.Status = "detached"
	m.event(info)
}
func (m *Manager) Detach(id string) error {
	if id == "" {
		return errors.New("debugSessionId is required")
	}
	m.mu.Lock()
	s := m.removeLocked(id)
	m.mu.Unlock()
	if s == nil {
		return errors.New("debug session not found")
	}
	m.stopSession(s)
	return nil
}
func (m *Manager) CloseProfile(profileID string) {
	m.mu.Lock()
	m.closingProfiles[profileID]++
	pending := make([]*pendingAttach, 0, len(m.pending[profileID]))
	for p := range m.pending[profileID] {
		p.cancel()
		pending = append(pending, p)
	}
	sessions := make([]*session, 0, len(m.profiles[profileID]))
	for id := range m.profiles[profileID] {
		sessions = append(sessions, m.removeLocked(id))
	}
	m.mu.Unlock()
	m.drain(sessions, pending)
	m.mu.Lock()
	m.closingProfiles[profileID]--
	if m.closingProfiles[profileID] == 0 {
		delete(m.closingProfiles, profileID)
	}
	m.mu.Unlock()
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	pending := []*pendingAttach{}
	for _, entries := range m.pending {
		for p := range entries {
			p.cancel()
			pending = append(pending, p)
		}
	}
	sessions := make([]*session, 0, len(m.sessions))
	for id := range m.sessions {
		sessions = append(sessions, m.removeLocked(id))
	}
	m.mu.Unlock()
	m.drain(sessions, pending)
}
func (m *Manager) drain(sessions []*session, pending []*pendingAttach) {
	var stopped sync.WaitGroup
	for _, s := range sessions {
		stopped.Add(1)
		go func(s *session) { defer stopped.Done(); m.stopSession(s) }(s)
	}
	for _, p := range pending {
		<-p.done
	}
	stopped.Wait()
}
func (m *Manager) Call(ctx context.Context, id, name string, args map[string]any) (map[string]any, error) {
	if id == "" {
		return nil, errors.New("debugSessionId is required; attach a managed profile first")
	}
	if !IsTool(name) {
		return nil, fmt.Errorf("unknown reverse tool %q", name)
	}
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	if s == nil {
		return nil, errors.New("debug session not found; attach again to reconnect")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.client.done:
		return nil, s.client.err()
	}
	endpoint, err := m.endpoint(s.info.ProfileID)
	if err != nil || endpoint != s.endpoint {
		m.CloseProfile(s.info.ProfileID)
		return nil, errors.New("managed profile is no longer running in this debug session")
	}
	cleaned := map[string]any{}
	for k, v := range args {
		if k != "debugSessionId" {
			cleaned[k] = v
		}
	}
	for _, key := range []string{"filePath", "outputFile", "localFilePath"} {
		if value, exists := cleaned[key]; exists {
			path, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string", key)
			}
			path, err = confinedPath(s.info.AllowedRoot, path)
			if err != nil {
				return nil, err
			}
			cleaned[key] = path
		}
	}
	var result map[string]any
	if err = s.client.request(ctx, "tools/call", map[string]any{"name": name, "arguments": cleaned}, &result); err != nil {
		return nil, err
	}
	if _, ok := result["content"].([]any); !ok {
		return nil, errors.New("invalid reverse MCP tool result")
	}
	if flag, exists := result["isError"]; exists {
		if _, ok := flag.(bool); !ok {
			return nil, errors.New("invalid reverse MCP isError flag")
		}
	}
	// Keep content blocks, structuredContent, and upstream isError unchanged.
	return result, nil
}

func (m *Manager) Windows(ctx context.Context, id string) (map[string]any, error) {
	return m.Call(ctx, id, "select_page", map[string]any{"includeWindows": true})
}
