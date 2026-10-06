package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type cdpReply struct {
	result object
	err    error
}
type cdpClient struct {
	conn        *websocket.Conn
	engine      string
	fingerprint object
	next        atomic.Uint64
	mu          sync.Mutex
	pending     map[uint64]chan cdpReply
	done        chan struct{}
	closeOnce   sync.Once
	writes      chan struct{}
	targets     chan struct{}
	active      string
	sessions    map[string]string
}

func connectCDP(ctx context.Context, endpoint, engine string, fp object) (*cdpClient, error) {
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	var last error
	for {
		req, e := http.NewRequestWithContext(ctx, "GET", endpoint+"/json/version", nil)
		if e != nil {
			return nil, e
		}
		resp, e := client.Do(req)
		if e == nil {
			var version object
			e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&version)
			_ = resp.Body.Close()
			if e == nil && resp.StatusCode == 200 && str(version, "webSocketDebuggerUrl") != "" {
				dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
				conn, _, dialErr := dialer.DialContext(ctx, str(version, "webSocketDebuggerUrl"), nil)
				if dialErr == nil {
					c := &cdpClient{conn: conn, engine: engine, fingerprint: fp, pending: map[uint64]chan cdpReply{}, done: make(chan struct{}), writes: make(chan struct{}, 1), targets: make(chan struct{}, 1), sessions: map[string]string{}}
					conn.SetReadLimit(64 << 20)
					go c.read()
					return c, nil
				}
				e = dialErr
			} else if e == nil {
				e = fmt.Errorf("invalid browser CDP version response (%d)", resp.StatusCode)
			}
		}
		last = e
		if e = pause(ctx, 100*time.Millisecond); e != nil {
			return nil, fmt.Errorf("connect browser CDP: %w (last response: %v)", e, last)
		}
	}
}
func (c *cdpClient) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- cdpReply{err: errors.New("CDP connection closed")}
			delete(c.pending, id)
		}
		c.mu.Unlock()
	})
}
func (c *cdpClient) read() {
	defer c.Close()
	for {
		var msg struct {
			ID     uint64 `json:"id"`
			Result object `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    any    `json:"data"`
			} `json:"error"`
		}
		if e := c.conn.ReadJSON(&msg); e != nil {
			return
		}
		if msg.ID == 0 {
			continue
		}
		reply := cdpReply{result: msg.Result}
		if reply.result == nil {
			reply.result = object{}
		}
		if msg.Error != nil {
			reply.err = fmt.Errorf("CDP error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- reply
		}
	}
}
func (c *cdpClient) request(ctx context.Context, method string, params object, session string) (object, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if method == "" {
		return nil, errors.New("CDP method is required")
	}
	if c.engine == "cloakbrowser" && (method == "Runtime.enable" || method == "Network.enable") {
		return nil, fmt.Errorf("%s is unsafe for CloakBrowser", method)
	}
	id := c.next.Add(1)
	reply := make(chan cdpReply, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, errors.New("CDP connection closed")
	default:
	}
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if params == nil {
		params = object{}
	}
	message := object{"id": id, "method": method, "params": params}
	if session != "" {
		message["sessionId"] = session
	}
	select {
	case c.writes <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("CDP connection closed")
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.conn.SetWriteDeadline(deadline)
	e := c.conn.WriteJSON(message)
	<-c.writes
	if e != nil {
		c.Close()
		return nil, e
	}
	select {
	case r := <-reply:
		return r.result, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("CDP connection closed")
	}
}
func browserMethod(method string) bool {
	return strings.HasPrefix(method, "Target.") || strings.HasPrefix(method, "Browser.") || strings.HasPrefix(method, "SystemInfo.") || strings.HasPrefix(method, "Storage.")
}
func (c *cdpClient) call(ctx context.Context, method string, params object, explicit string) (object, error) {
	select {
	case c.targets <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("CDP connection closed")
	}
	defer func() { <-c.targets }()
	session := explicit
	if !browserMethod(method) && session == "" {
		var e error
		session, e = c.activeSession(ctx)
		if e != nil {
			return nil, e
		}
	}
	result, e := c.request(ctx, method, params, session)
	if e != nil {
		return nil, e
	}
	switch method {
	case "Target.activateTarget":
		c.active = str(params, "targetId")
	case "Target.createTarget":
		c.active = str(result, "targetId")
	case "Target.closeTarget":
		id := str(params, "targetId")
		delete(c.sessions, id)
		if c.active == id {
			c.active = ""
		}
	}
	return result, nil
}
func (c *cdpClient) activeSession(ctx context.Context) (string, error) {
	targets, e := c.request(ctx, "Target.getTargets", nil, "")
	if e != nil {
		return "", e
	}
	pages := []object{}
	for _, v := range array(targets["targetInfos"]) {
		t := obj(v)
		if str(t, "type") == "page" {
			pages = append(pages, t)
		}
	}
	found := false
	for _, p := range pages {
		if str(p, "targetId") == c.active {
			found = true
			break
		}
	}
	if !found {
		c.active = ""
		if len(pages) > 0 {
			c.active = str(pages[0], "targetId")
		}
	}
	if c.active == "" {
		r, e := c.request(ctx, "Target.createTarget", object{"url": "about:blank"}, "")
		if e != nil {
			return "", e
		}
		c.active = str(r, "targetId")
	}
	return c.attach(ctx, c.active)
}
func (c *cdpClient) attach(ctx context.Context, target string) (string, error) {
	if session := c.sessions[target]; session != "" {
		return session, nil
	}
	r, e := c.request(ctx, "Target.attachToTarget", object{"targetId": target, "flatten": true}, "")
	if e != nil {
		return "", e
	}
	session := str(r, "sessionId")
	if session == "" {
		return "", errors.New("CDP did not return a sessionId")
	}
	if c.engine == "cft" {
		if e = c.bootstrap(ctx, session); e != nil {
			_, _ = c.request(ctx, "Target.detachFromTarget", object{"sessionId": session}, "")
			return "", e
		}
	}
	c.sessions[target] = session
	return session, nil
}
func (c *cdpClient) bootstrapExisting(ctx context.Context) error {
	select {
	case c.targets <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.targets }()
	r, e := c.request(ctx, "Target.getTargets", nil, "")
	if e != nil {
		return e
	}
	for _, v := range array(r["targetInfos"]) {
		p := obj(v)
		if str(p, "type") == "page" {
			if _, e = c.attach(ctx, str(p, "targetId")); e != nil {
				return e
			}
		}
	}
	return nil
}
func (c *cdpClient) bootstrap(ctx context.Context, session string) error {
	fp := c.fingerprint
	script := `(()=>{const f=` + quote(fp) + `;const def=(o,p,v)=>{if(v!==undefined&&v!==null)Object.defineProperty(o,p,{get:()=>v,configurable:true})};for(const [p,v] of Object.entries({platform:f.platform,hardwareConcurrency:f.hardwareConcurrency,deviceMemory:f.deviceMemory,languages:f.languages,language:f.locale})){try{def(navigator,p,v)}catch{}};for(const [p,v] of Object.entries(f.screen||{})){try{def(screen,p,v)}catch{}};try{def(window,'devicePixelRatio',f.dpr)}catch{};for(const C of [globalThis.WebGLRenderingContext,globalThis.WebGL2RenderingContext]){if(!C)continue;const original=C.prototype.getParameter;C.prototype.getParameter=function(p){if(p===37445&&f.webgl?.vendor)return f.webgl.vendor;if(p===37446&&f.webgl?.renderer)return f.webgl.renderer;return original.call(this,p)}};if(document.documentElement&&f.locale)document.documentElement.lang=f.locale})()`
	if _, e := c.request(ctx, "Page.addScriptToEvaluateOnNewDocument", object{"source": script}, session); e != nil {
		return e
	}
	if _, e := c.request(ctx, "Runtime.evaluate", object{"expression": script}, session); e != nil {
		return e
	}
	if ua := str(fp, "userAgent"); ua != "" {
		if _, e := c.request(ctx, "Emulation.setUserAgentOverride", object{"userAgent": ua, "acceptLanguage": str(fp, "acceptLanguage"), "platform": str(fp, "platform")}, session); e != nil {
			return e
		}
	}
	if timezone := str(fp, "timezone"); timezone != "" {
		if _, e := c.request(ctx, "Emulation.setTimezoneOverride", object{"timezoneId": timezone}, session); e != nil {
			return e
		}
	}
	if locale := str(fp, "locale"); locale != "" {
		if _, e := c.request(ctx, "Emulation.setLocaleOverride", object{"locale": locale}, session); e != nil {
			return e
		}
	}
	return nil
}
func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Call dispatches arbitrary CDP methods to the browser or the active page.
func (m *Manager) Call(ctx context.Context, profileID, method string, params map[string]any) (any, error) {
	h, e := m.get(profileID)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return h.cdp.call(ctx, method, params, "")
}
