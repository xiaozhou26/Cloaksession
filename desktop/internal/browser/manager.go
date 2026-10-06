package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Manager owns browser processes, their proxy bridges, and CDP connections.
type Manager struct {
	resourceDir string
	emit        func(string, any)
	ctx         context.Context
	cancel      context.CancelFunc
	operations  sync.Mutex
	mu          sync.RWMutex
	profiles    map[string]*instance
}
type instance struct {
	id, engine, endpoint, startedAt string
	profile                         object
	cmd                             *exec.Cmd
	stdin                           io.WriteCloser
	processDone                     chan struct{}
	finished                        chan struct{}
	cdp                             *cdpClient
	bridge                          *proxyBridge
	stopOnce                        sync.Once
	stopping                        atomic.Bool
	reason                          string
	tools                           chan struct{}
}

func New(resourceDir string, emit func(string, any)) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{resourceDir: resourceDir, emit: emit, ctx: ctx, cancel: cancel, profiles: map[string]*instance{}}
}
func (m *Manager) event(name string, v any) {
	if m.emit != nil {
		m.emit(name, v)
	}
}
func (h *instance) info() object {
	return object{"id": h.id, "cdpEndpoint": h.endpoint, "pid": h.cmd.Process.Pid, "startedAt": h.startedAt}
}
func (h *instance) alive() bool {
	select {
	case <-h.processDone:
		return false
	default:
		return true
	}
}
func (m *Manager) IsRunning(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.profiles[id]
	return h != nil && h.alive() && !h.stopping.Load() && h.reason == ""
}
func (m *Manager) get(id string) (*instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.profiles[id]
	if h == nil || !h.alive() || h.stopping.Load() || h.reason != "" {
		return nil, fmt.Errorf("no active browser session for profile %q; call launch first", id)
	}
	return h, nil
}

// Launch is idempotent for a running profile. Settings and profile maps are copied.
func (m *Manager) Launch(profile map[string]any, settings map[string]any, companionDir string) (result map[string]any, err error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	id := str(profile, "id")
	if id == "" || str(profile, "dataDir") == "" {
		return nil, errors.New("profile id and dataDir are required")
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("launch error: %w", err)
			m.event("chromium:status", object{"profileId": id, "status": "failed", "error": err.Error()})
		}
	}()
	if e := m.ctx.Err(); e != nil {
		return nil, e
	}
	m.mu.RLock()
	old := m.profiles[id]
	m.mu.RUnlock()
	if old != nil {
		if old.alive() && !old.stopping.Load() {
			return old.info(), nil
		}
		<-old.finished
	}
	profile = clone(profile)
	settings = clone(settings)
	engine := text(settings, "browserEngine", "cloakbrowser")
	if engine != "cloakbrowser" && engine != "cft" && engine != "chromix" {
		return nil, fmt.Errorf("unsupported browser engine %q", engine)
	}
	binaryPath := text(settings, "browserBinaryPath", defaultBinary(engine))
	if engine != "chromix" {
		if err = prepareDataDir(profileDir(profile, engine)); err != nil {
			return nil, err
		}
	}
	reservation, port, e := reservePort()
	if e != nil {
		return nil, e
	}
	defer reservation.Close()
	h := &instance{id: id, engine: engine, profile: profile, endpoint: fmt.Sprintf("http://127.0.0.1:%d", port), startedAt: time.Now().UTC().Format(time.RFC3339Nano), processDone: make(chan struct{}), finished: make(chan struct{}), tools: make(chan struct{}, 1)}
	ctx, cancel := context.WithTimeout(m.ctx, 120*time.Second)
	defer cancel()
	var ready <-chan error
	if engine == "chromix" {
		request := playwrightRequest(profile, settings, binaryPath, companionDir, port)
		if h.bridge, e = bridgePlaywrightProxy(request); e != nil {
			return nil, e
		}
		config := obj(settings["chromix"])
		script := filepath.Join(m.resourceDir, "playwright", "bridge.mjs")
		if _, e = os.Stat(script); e != nil {
			h.bridge.Close()
			return nil, fmt.Errorf("Playwright bridge missing at %s: %w", script, e)
		}
		script, e = filepath.Abs(script)
		if e != nil {
			h.bridge.Close()
			return nil, e
		}
		h.cmd = exec.Command(text(config, "nodePath", "node"), script)
		h.cmd.Env = os.Environ()
		for key, v := range obj(config["environment"]) {
			if s, ok := v.(string); ok {
				h.cmd.Env = append(h.cmd.Env, key+"="+s)
			}
		}
		h.stdin, e = h.cmd.StdinPipe()
		if e != nil {
			h.bridge.Close()
			return nil, e
		}
		stdout, e := h.cmd.StdoutPipe()
		if e != nil {
			_ = h.stdin.Close()
			h.bridge.Close()
			return nil, e
		}
		h.cmd.Stderr = os.Stderr
		configureProcess(h.cmd)
		_ = reservation.Close()
		if e = h.cmd.Start(); e != nil {
			_ = h.stdin.Close()
			_ = stdout.Close()
			h.bridge.Close()
			return nil, fmt.Errorf("start Node (20+ required): %w", e)
		}
		channel := make(chan error, 1)
		ready = channel
		go superviseBridge(h, stdout, channel)
		go func() { _ = h.cmd.Wait(); close(h.processDone) }()
		payload, e := json.Marshal(request)
		if e == nil {
			e = writeControl(h.stdin, append(payload, '\n'))
		}
		if e != nil {
			h.stop()
			h.bridge.Close()
			return nil, e
		}
	} else {
		proxyURL := ""
		if p := profile["proxy"]; p != nil {
			config, e := parseProxy(p)
			if e != nil {
				return nil, e
			}
			h.bridge, e = startProxy(config)
			if e != nil {
				return nil, e
			}
			proxyURL = h.bridge.URL()
			if engine == "cloakbrowser" {
				if location, geoErr := proxyLocation(ctx, proxyURL, "https://ipapi.co/json/"); geoErr == nil {
					settings["proxyLocation"] = location
				}
			}
		}
		h.cmd = exec.Command(binaryPath, spawnArgs(profile, engine, port, proxyURL, companionDir, settings)...)
		h.cmd.Stderr = os.Stderr
		configureProcess(h.cmd)
		_ = reservation.Close()
		if e = h.cmd.Start(); e != nil {
			h.bridge.Close()
			return nil, fmt.Errorf("start browser: %w", e)
		}
		go func() { _ = h.cmd.Wait(); close(h.processDone) }()
	}
	startupFinished := make(chan struct{})
	defer close(startupFinished)
	go func() {
		select {
		case <-h.processDone:
			cancel()
		case <-startupFinished:
		}
	}()
	success := false
	defer func() {
		if !success {
			h.stop()
			h.bridge.Close()
			if h.cdp != nil {
				h.cdp.Close()
			}
		}
	}()
	if ready != nil {
		select {
		case e = <-ready:
			if e != nil {
				return nil, e
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-h.processDone:
			return nil, errors.New("Playwright bridge exited before ready")
		}
	}
	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second)
	defer connectCancel()
	h.cdp, e = connectCDP(connectCtx, h.endpoint, h.engine, obj(profile["fingerprint"]))
	if e != nil {
		return nil, e
	}
	if !h.alive() {
		return nil, errors.New("browser exited during startup")
	}
	if engine == "cft" {
		if e = h.cdp.bootstrapExisting(connectCtx); e != nil {
			return nil, e
		}
	}
	m.mu.Lock()
	m.profiles[id] = h
	m.mu.Unlock()
	success = true
	m.event("profiles:running-changed", object{"kind": "launched", "profileId": id, "profile_id": id})
	m.event("chromium:status", object{"profileId": id, "status": "started"})
	go m.watch(h)
	return h.info(), nil
}
func superviseBridge(h *instance, stdout io.Reader, ready chan<- error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	started := false
	for scanner.Scan() {
		var event object
		if e := json.Unmarshal(scanner.Bytes(), &event); e != nil {
			if !started {
				ready <- fmt.Errorf("invalid Playwright bridge JSON: %w", e)
			}
			go h.stop()
			return
		}
		if !started {
			if str(event, "type") != "ready" || str(event, "cdpEndpoint") != h.endpoint {
				ready <- fmt.Errorf("Playwright launch failed: %s", text(event, "message", "unexpected ready endpoint or event"))
				go h.stop()
				return
			}
			started = true
			ready <- nil
		} else if str(event, "type") != "ready" {
			go h.stop()
			return
		}
	}
	if !started {
		e := scanner.Err()
		if e == nil {
			e = io.EOF
		}
		ready <- fmt.Errorf("Playwright bridge closed before ready: %w", e)
	}
	go h.stop()
}
func writeControl(input io.WriteCloser, payload []byte) error {
	if pipe, ok := input.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = pipe.SetWriteDeadline(time.Now().Add(time.Second))
	}
	_, err := input.Write(payload)
	return err
}

func (h *instance) stop() {
	h.stopping.Store(true)
	h.stopOnce.Do(func() {
		if h.stdin != nil {
			_ = writeControl(h.stdin, []byte("{\"type\":\"close\"}\n"))
			_ = h.stdin.Close()
		} else if h.cdp != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, _ = h.cdp.request(ctx, "Browser.close", object{}, "")
			cancel()
		}
		delay := 3 * time.Second
		if h.stdin != nil {
			delay = 12 * time.Second
		}
		select {
		case <-h.processDone:
			return
		case <-time.After(delay):
		}
		terminateProcess(h.cmd)
		select {
		case <-h.processDone:
			return
		case <-time.After(2 * time.Second):
		}
		killProcess(h.cmd)
		<-h.processDone
	})
}
func (m *Manager) watch(h *instance) {
	<-h.processDone
	if h.cdp != nil {
		h.cdp.Close()
	}
	h.bridge.Close()
	m.mu.Lock()
	reason := h.reason
	if reason == "" {
		reason = "external-exit"
	}
	if m.profiles[h.id] == h {
		delete(m.profiles, h.id)
	}
	m.mu.Unlock()
	m.event("profiles:running-changed", object{"kind": "closed", "profileId": h.id, "profile_id": h.id, "reason": reason})
	m.event("chromium:status", object{"profileId": h.id, "status": "stopped"})
	close(h.finished)
}
func (m *Manager) CloseProfile(id string) error {
	m.operations.Lock()
	defer m.operations.Unlock()
	return m.closeProfile(id)
}
func (m *Manager) closeProfile(id string) error {
	m.mu.Lock()
	h := m.profiles[id]
	if h != nil {
		h.reason = "user-close"
	}
	m.mu.Unlock()
	if h != nil {
		h.bridge.Close()
		h.stop()
		<-h.finished
	}
	return nil
}
func (m *Manager) Shutdown() {
	m.cancel()
	m.operations.Lock()
	defer m.operations.Unlock()
	m.mu.RLock()
	ids := make([]string, 0, len(m.profiles))
	for id := range m.profiles {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.closeProfile(id)
	}
}
func playwrightRequest(profile, settings object, binaryPath, companion string, port int) object {
	options := clone(obj(obj(settings["chromix"])["options"]))
	for k, v := range obj(profile["chromixOptions"]) {
		options[k] = v
	}
	var proxy any
	if p := profile["proxy"]; p != nil {
		if config, e := parseProxy(p); e == nil {
			proxy = object{"server": config.scheme + "://" + config.address}
			if config.username != "" {
				obj(proxy)["username"] = config.username
			}
			if config.password != "" {
				obj(proxy)["password"] = config.password
			}
		} else {
			proxy = p
		}
	}
	return object{"type": "launch", "options": options, "binaryPath": binaryPath, "skipDownload": boolean(settings, "skipBrowserDownload"), "cdpPort": port, "userDataDir": profileDir(profile, "chromix"), "proxy": proxy, "extensionPaths": extensionPaths(profile, companion), "startUrl": profile["startUrl"], "profileId": str(profile, "id"), "fingerprint": profile["fingerprint"]}
}
func bridgePlaywrightProxy(request object) (*proxyBridge, error) {
	owner := request
	key := "proxy"
	explicit, raw := false, false
	options := obj(request["options"])
	for _, layer := range []object{options, obj(options["launchOptions"]), obj(options["contextOptions"])} {
		if _, ok := layer["proxy"]; ok {
			owner = layer
			explicit = true
		}
		for _, v := range array(layer["args"]) {
			s, _ := v.(string)
			k := strings.Fields(strings.SplitN(strings.TrimSpace(s), "=", 2)[0])
			if len(k) > 0 && (k[0] == "--proxy-server" || k[0] == "--proxy-pac-url" || k[0] == "--no-proxy-server") {
				raw = true
			}
		}
	}
	v := owner[key]
	if raw && !explicit || v == nil || v == false || v == "" {
		return nil, nil
	}
	p, e := parseProxy(v)
	if e != nil {
		return nil, e
	}
	if p.scheme != "socks5" || p.username == "" && p.password == "" {
		return nil, nil
	}
	b, e := startProxy(p)
	if e != nil {
		return nil, e
	}
	replacement := object{"server": b.URL()}
	if bypass, ok := obj(v)["bypass"]; ok {
		replacement["bypass"] = bypass
	}
	owner[key] = replacement
	return b, nil
}
