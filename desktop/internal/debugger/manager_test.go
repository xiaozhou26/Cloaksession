package debugger

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureResources(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"reverse/bridge.mjs": "// fixture bridge\n",
		"reverse/node_modules/js-reverse-mcp/package.json":       `{"name":"js-reverse-mcp","version":"4.0.5","bin":{"js-reverse-mcp":"build/src/index.js"}}`,
		"reverse/node_modules/js-reverse-mcp/build/src/index.js": "// fixture CLI\n",
	}
	for path, data := range files {
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	name := "node"
	if runtime.GOOS == "windows" {
		name = "node.exe"
	}
	node := filepath.Join(root, name)
	if err := os.WriteFile(node, []byte("fixture binary"), 0700); err != nil {
		t.Fatal(err)
	}
	return root, node
}
func fixtureManager(t *testing.T) (*Manager, string) {
	t.Helper()
	resource, node := fixtureResources(t)
	m := New(resource, t.TempDir(), func(id string) (string, error) {
		if id == "a" {
			return "http://127.0.0.1:12345", nil
		}
		if id == "b" {
			return "http://127.0.0.1:12346", nil
		}
		return "", errors.New("unmanaged or closed profile")
	}, nil)
	m.start = func(cmd *exec.Cmd) (*client, error) {
		if len(cmd.Args) != 6 || cmd.Args[2] != "--browserUrl" || cmd.Args[4] != "--allowedRoots" || cmd.Dir != cmd.Args[5] {
			t.Errorf("unexpected launch contract: %v cwd=%s", cmd.Args, cmd.Dir)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestStdioFixture$")
		child.Env = append(os.Environ(), "CLOAKSESSION_DEBUG_FIXTURE=1")
		return startClient(child)
	}
	t.Cleanup(m.Close)
	return m, node
}
func TestManagerIsolationDetachAndReconnect(t *testing.T) {
	m, node := fixtureManager(t)
	ctx := context.Background()
	a, err := m.Attach(ctx, "a", node)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Attach(ctx, "b", node)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Attach(ctx, "a", node)
	if err != nil || again.DebugSessionID == a.DebugSessionID {
		t.Fatalf("attach reused a session: %v", err)
	}
	if len(m.Sessions()) != 3 {
		t.Fatal("same-profile sessions are not discoverable")
	}
	if a.AllowedRoot == b.AllowedRoot || a.DebugSessionID == b.DebugSessionID {
		t.Fatal("profiles share debugger state")
	}
	results := []map[string]any{}
	for _, s := range []Session{a, b, again} {
		r, e := m.Call(ctx, s.DebugSessionID, "list_scripts", map[string]any{"debugSessionId": "must-not-forward"})
		if e != nil {
			t.Fatal(e)
		}
		results = append(results, r["structuredContent"].(map[string]any))
		if results[len(results)-1]["arguments"].(map[string]any)["debugSessionId"] != nil {
			t.Fatal("routing argument leaked upstream")
		}
	}
	if results[0]["pid"] == results[1]["pid"] || results[0]["pid"] == results[2]["pid"] || results[1]["pid"] == results[2]["pid"] {
		t.Fatal("sessions share a child process")
	}
	for _, result := range results {
		if result["selectionCalls"] != float64(1) {
			t.Fatal("attach did not probe select_page exactly once", result)
		}
	}
	if _, err = m.Call(ctx, a.DebugSessionID, "select_page", map[string]any{"pageIdx": 7}); err != nil {
		t.Fatal(err)
	}
	untouched, err := m.Call(ctx, again.DebugSessionID, "select_page", nil)
	if err != nil || untouched["structuredContent"].(map[string]any)["selectedPage"] != float64(0) {
		t.Fatalf("same-profile selection leaked: %v %v", untouched, err)
	}
	if _, err = m.Call(ctx, "", "list_scripts", nil); err == nil {
		t.Fatal("implicit routing accepted")
	}
	if _, err = m.Call(ctx, a.DebugSessionID, "list_scripts", map[string]any{"fixture": "exit"}); err == nil {
		t.Fatal("unexpected exit hidden")
	}
	found := false
	for _, s := range m.Sessions() {
		if s.DebugSessionID == a.DebugSessionID {
			found = s.Status == "error" && s.Error != ""
		}
	}
	if !found {
		t.Fatal("failed session state unavailable")
	}
	reconnected, err := m.Attach(ctx, "a", node)
	if err != nil {
		t.Fatal(err)
	}
	if reconnected.DebugSessionID == a.DebugSessionID {
		t.Fatal("reconnect reused stale ID")
	}
	if _, err = m.Call(ctx, a.DebugSessionID, "list_scripts", nil); err == nil {
		t.Fatal("stale session accepted")
	}
	if len(m.Sessions()) != 4 {
		t.Fatal("attach replaced an existing or failed session")
	}
	if err = m.Detach(reconnected.DebugSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Call(ctx, again.DebugSessionID, "list_scripts", nil); err != nil {
		t.Fatal("detach affected another session of the same profile", err)
	}
	if _, err = m.Call(ctx, reconnected.DebugSessionID, "list_scripts", nil); err == nil {
		t.Fatal("detached session still callable")
	}
	additional, err := m.Attach(ctx, "a", node)
	if err != nil {
		t.Fatal(err)
	}
	if additional.DebugSessionID == again.DebugSessionID {
		t.Fatal("additional live session was reused")
	}
	m.mu.Lock()
	children := []*client{}
	for id := range m.profiles["a"] {
		children = append(children, m.sessions[id].client)
	}
	m.mu.Unlock()
	m.CloseProfile("a")
	for _, child := range children {
		select {
		case <-child.exited:
		default:
			t.Fatal("profile close did not reap every child")
		}
	}
	if sessions := m.Sessions(); len(sessions) != 1 || sessions[0].DebugSessionID != b.DebugSessionID {
		t.Fatal("profile close did not isolate cleanup", sessions)
	}
	if _, err = m.Call(ctx, b.DebugSessionID, "list_scripts", nil); err != nil {
		t.Fatal("detach affected other profile", err)
	}
	m.CloseProfile("b")
	if len(m.Sessions()) != 0 || len(m.profiles) != 0 {
		t.Fatal("profile close leaked child or profile index")
	}
	if _, err = m.Attach(ctx, "external", node); err == nil {
		t.Fatal("unmanaged profile accepted")
	}
}
func TestManagerConcurrentAttachAndPathConfinement(t *testing.T) {
	m, node := fixtureManager(t)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := m.Attach(context.Background(), "a", node)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- s.DebugSessionID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	unique := map[string]bool{}
	pids := map[any]bool{}
	for current := range ids {
		if unique[current] {
			t.Fatal("concurrent attaches share a session")
		}
		unique[current] = true
		result, err := m.Call(context.Background(), current, "list_scripts", nil)
		if err != nil {
			t.Fatal(err)
		}
		pid := result["structuredContent"].(map[string]any)["pid"]
		if pids[pid] {
			t.Fatal("concurrent attaches share a child")
		}
		pids[pid] = true
		id = current
	}
	if len(m.Sessions()) != 8 || len(unique) != 8 {
		t.Fatal("concurrent attach lost independent sessions")
	}
	for _, key := range []string{"outputFile", "filePath", "localFilePath"} {
		if _, err := m.Call(context.Background(), id, "evaluate_script", map[string]any{key: "../escape"}); err == nil {
			t.Fatalf("%s escaped allowedRoot", key)
		}
	}
	result, err := m.Call(context.Background(), id, "take_screenshot", map[string]any{"filePath": "safe.png"})
	if err != nil {
		t.Fatal(err)
	}
	path := result["structuredContent"].(map[string]any)["arguments"].(map[string]any)["filePath"].(string)
	if !filepath.IsAbs(path) || !inside(m.Sessions()[0].AllowedRoot, path) {
		t.Fatal(path)
	}
	m.Close()
	if _, err = m.Attach(context.Background(), "a", node); err == nil {
		t.Fatal("attach after shutdown accepted")
	}
}
func TestPathAndPackageValidation(t *testing.T) {
	resource, node := fixtureResources(t)
	if _, err := nodeExecutable(node); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"cmd.exe", "sh", "node --eval x", "./node"} {
		if _, err := nodeExecutable(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := bridgePath(resource); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(resource, "reverse", "node_modules", "js-reverse-mcp", "package.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifest, []byte(strings.ReplaceAll(string(data), "4.0.5", "4.0.6")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = bridgePath(resource); err == nil {
		t.Fatal("unpinned package accepted")
	}
	for _, bad := range []string{"https://127.0.0.1:1234", "http://example.com:1234", "http://127.0.0.1", "http://user@127.0.0.1:1234", "http://127.0.0.1:1234/?x=1"} {
		if validateEndpoint(bad) == nil {
			t.Fatal(bad)
		}
	}
	root := t.TempDir()
	outside := t.TempDir()
	if _, err = confinedPath(root, filepath.Join(outside, "out.txt")); err == nil {
		t.Fatal("absolute escape")
	}
	link := filepath.Join(root, "link")
	if err = os.Symlink(outside, link); err == nil {
		if _, err = confinedPath(root, filepath.Join(link, "out.txt")); err == nil {
			t.Fatal("symlink escape")
		}
	}
	t.Setenv("NODE_OPTIONS", "--require=untrusted.js")
	for _, entry := range safeEnvironment() {
		if strings.HasPrefix(strings.ToUpper(entry), "NODE_OPTIONS=") {
			t.Fatal("inherited Node injection")
		}
	}
}

func TestPinnedResourceStdioHandshake(t *testing.T) {
	resource, err := filepath.Abs(filepath.Join("..", "..", "resources"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(resource, "reverse", "node_modules", "js-reverse-mcp", "package.json")); os.IsNotExist(err) {
		t.Skip("reverse dependencies are not installed")
	}
	node, err := nodeExecutable("")
	if err != nil {
		t.Skip(err)
	}
	bridge, err := bridgePath(resource)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cmd := exec.Command(node, bridge, "--browserUrl", "http://127.0.0.1:1", "--allowedRoots", root)
	cmd.Dir, cmd.Env = root, safeEnvironment()
	c, err := startClient(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = c.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = checkTools(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err = checkBrowser(ctx, c); err == nil {
		t.Fatal("browser probe accepted an unreachable CDP endpoint")
	}
}

func TestManagerAttachProbeFailureCleansOnlyNewChild(t *testing.T) {
	for _, mode := range []string{"tool-error", "rpc-error", "missing-content", "malformed", "exit", "hang"} {
		t.Run(mode, func(t *testing.T) {
			m, node := fixtureManager(t)
			existing, err := m.Attach(context.Background(), "a", node)
			if err != nil {
				t.Fatal(err)
			}
			original := m.start
			var child *client
			m.start = func(cmd *exec.Cmd) (*client, error) {
				var err error
				child, err = original(cmd)
				return child, err
			}
			t.Setenv("CLOAKSESSION_DEBUG_SELECT_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
			defer cancel()
			if _, err = m.Attach(ctx, "a", node); err == nil {
				t.Fatal("attach accepted failed browser probe")
			}
			if mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("probe did not respect timeout: %v", err)
			}
			if child == nil {
				t.Fatal("fixture child never started")
			}
			select {
			case <-child.exited:
			default:
				t.Fatal("failed attach leaked child")
			}
			if sessions := m.Sessions(); len(sessions) != 1 || sessions[0].DebugSessionID != existing.DebugSessionID {
				t.Fatal("failed attach changed existing sessions", sessions)
			}
			if len(m.profiles["a"]) != 1 {
				t.Fatal("failed attach leaked profile index entry")
			}
			if _, err = m.Call(context.Background(), existing.DebugSessionID, "list_scripts", nil); err != nil {
				t.Fatal("failed attach disrupted existing child", err)
			}
		})
	}
}

func waitFixtureMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("fixture never reached hold point")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPendingAttachDoesNotBlockLifecycle(t *testing.T) {
	for _, operation := range []string{"profile", "all", "context"} {
		t.Run(operation, func(t *testing.T) {
			m, node := fixtureManager(t)
			b, err := m.Attach(context.Background(), "b", node)
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "initialize")
			children := make(chan *client, 4)
			m.start = func(cmd *exec.Cmd) (*client, error) {
				child := exec.Command(os.Args[0], "-test.run=^TestStdioFixture$")
				child.Env = append(os.Environ(), "CLOAKSESSION_DEBUG_FIXTURE=1")
				if cmd.Args[3] == "http://127.0.0.1:12345" {
					child.Env = append(child.Env, "CLOAKSESSION_DEBUG_HOLD_INITIALIZE="+marker)
				}
				c, err := startClient(child)
				if err == nil && cmd.Args[3] == "http://127.0.0.1:12345" {
					children <- c
				}
				return c, err
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			attached := make(chan error, 1)
			go func() { _, err := m.Attach(ctx, "a", node); attached <- err }()
			waitFixtureMarker(t, marker)
			child := <-children
			detached := make(chan error, 1)
			go func() { detached <- m.Detach(b.DebugSessionID) }()
			select {
			case err := <-detached:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("other-profile detach blocked on initialize")
			}
			// A second attach must progress while profile A remains in initialize.
			other := make(chan Session, 1)
			go func() {
				s, err := m.Attach(context.Background(), "b", node)
				if err != nil {
					t.Error(err)
				}
				other <- s
			}()
			var survivor Session
			select {
			case survivor = <-other:
			case <-time.After(2 * time.Second):
				t.Fatal("other-profile attach blocked on initialize")
			}
			closed := make(chan struct{})
			go func() {
				switch operation {
				case "profile":
					m.CloseProfile("a")
				case "all":
					m.Close()
				case "context":
					cancel()
				}
				close(closed)
			}()
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("close waited for initialize timeout")
			}
			select {
			case err := <-attached:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("attach cancellation: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled attach did not return")
			}
			select {
			case <-child.exited:
			default:
				t.Fatal("cancelled attach leaked child")
			}
			m.mu.Lock()
			pending := len(m.pending)
			m.mu.Unlock()
			if pending != 0 {
				t.Fatal("pending attach index leaked")
			}
			if operation != "all" {
				if _, err := m.Call(context.Background(), survivor.DebugSessionID, "list_scripts", nil); err != nil {
					t.Fatal("profile cancellation affected another profile", err)
				}
			} else if len(m.Sessions()) != 0 {
				t.Fatal("manager close leaked sessions")
			}
		})
	}
}

func TestCloseCancelsAllPendingProfiles(t *testing.T) {
	m, node := fixtureManager(t)
	dir := t.TempDir()
	children := make(chan *client, 2)
	m.start = func(cmd *exec.Cmd) (*client, error) {
		name := "a"
		if cmd.Args[3] == "http://127.0.0.1:12346" {
			name = "b"
		}
		child := exec.Command(os.Args[0], "-test.run=^TestStdioFixture$")
		child.Env = append(os.Environ(), "CLOAKSESSION_DEBUG_FIXTURE=1", "CLOAKSESSION_DEBUG_HOLD_INITIALIZE="+filepath.Join(dir, name))
		c, err := startClient(child)
		if err == nil {
			children <- c
		}
		return c, err
	}
	completed := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		go func(id string) { _, err := m.Attach(context.Background(), id, node); completed <- err }(id)
	}
	waitFixtureMarker(t, filepath.Join(dir, "a"))
	waitFixtureMarker(t, filepath.Join(dir, "b"))
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("close blocked on pending initializations")
	}
	for i := 0; i < 2; i++ {
		if err := <-completed; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		select {
		case <-(<-children).exited:
		default:
			t.Fatal("pending child leaked")
		}
	}
}

func TestToolCancellationFailsClosedWithoutBlockingResume(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			m, node := fixtureManager(t)
			a, err := m.Attach(context.Background(), "a", node)
			if err != nil {
				t.Fatal(err)
			}
			sibling, err := m.Attach(context.Background(), "a", node)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
			}
			defer cancel()
			marker := filepath.Join(t.TempDir(), "handler")
			failed := make(chan error, 1)
			go func() {
				_, err := m.Call(ctx, a.DebugSessionID, "list_scripts", map[string]any{"fixture": "hold-handler", "marker": marker})
				failed <- err
			}()
			waitFixtureMarker(t, marker)
			resumed := make(chan error, 1)
			go func() {
				_, err := m.Call(context.Background(), a.DebugSessionID, "pause_or_resume", map[string]any{"action": "resume"})
				resumed <- err
			}()
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-failed:
				want := context.Canceled
				if mode == "deadline" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) || !strings.Contains(err.Error(), "attach again") {
					t.Fatalf("unexpected call error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("uncancellable handler blocked caller")
			}
			select {
			case err := <-resumed:
				if err == nil || !strings.Contains(err.Error(), "attach again") {
					t.Fatal("resume did not reject failed session", err)
				}
			case <-time.After(time.Second):
				t.Fatal("resume remained queued")
			}
			m.mu.Lock()
			child := m.sessions[a.DebugSessionID].client
			m.mu.Unlock()
			select {
			case <-child.exited:
			default:
				t.Fatal("timed-out child leaked")
			}
			found := false
			for _, s := range m.Sessions() {
				if s.DebugSessionID == a.DebugSessionID {
					found = s.Status == "error" && strings.Contains(s.Error, "attach again")
				}
			}
			if !found {
				t.Fatal("failed session status not retained")
			}
			if _, err := m.Call(context.Background(), sibling.DebugSessionID, "list_scripts", nil); err != nil {
				t.Fatal("sibling session disrupted", err)
			}
			// The endpoint provider still owns the same browser; reconnect only replaces the child.
			replacement, err := m.Attach(context.Background(), "a", node)
			if err != nil || replacement.DebugSessionID == a.DebugSessionID {
				t.Fatalf("reattach failed: %v", err)
			}
		})
	}
}

func TestQueuedCallCancellationKeepsSessionUsable(t *testing.T) {
	m, node := fixtureManager(t)
	s, err := m.Attach(context.Background(), "a", node)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	session := m.sessions[s.DebugSessionID]
	m.mu.Unlock()
	session.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = m.Call(ctx, s.DebugSessionID, "pause_or_resume", map[string]any{"action": "resume"})
	<-session.gate
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if session.client.err() != nil {
		t.Fatal("unsent queued call closed session")
	}
	if _, err = m.Call(context.Background(), s.DebugSessionID, "list_scripts", nil); err != nil {
		t.Fatal(err)
	}
}
