package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func nodeFixture(t *testing.T, source string) string {
	t.Helper()
	if _, e := exec.LookPath("node"); e != nil {
		t.Skip("Node is required for Playwright protocol fixtures")
	}
	dir := t.TempDir()
	if e := os.Mkdir(filepath.Join(dir, "playwright"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "playwright", "bridge.mjs"), []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	return dir
}

const fakeBridge = `import readline from 'node:readline';import http from 'node:http';
let server;const lines=readline.createInterface({input:process.stdin});
lines.on('line',line=>{const r=JSON.parse(line);if(r.type==='close'){process.stdout.write(JSON.stringify({type:'closed'})+'\n');server?.close(()=>process.exit(0));return}
server=http.createServer((q,s)=>{s.setHeader('content-type','application/json');s.end(JSON.stringify({webSocketDebuggerUrl:process.env.TEST_WS}))});server.listen(r.cdpPort,'127.0.0.1',()=>{process.stdout.write(JSON.stringify({type:'ready',cdpEndpoint:'http://127.0.0.1:'+r.cdpPort})+'\n');if(process.env.TEST_EXIT)setTimeout(()=>process.exit(0),200)})});
lines.on('close',()=>{server?.close(()=>process.exit(0))});`

func TestPlaywrightLifecycle(t *testing.T) {
	endpoint, _, _ := fakeCDP(t)
	for _, mode := range []string{"close", "eof", "exit"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "eof" {
				testPlaywrightEOF(t, endpoint, false)
				return
			}
			dir := nodeFixture(t, fakeBridge)
			m := New(dir, nil)
			defer m.Shutdown()
			env := object{"TEST_WS": "ws" + strings.TrimPrefix(endpoint, "http") + "/ws"}
			if mode == "exit" {
				env["TEST_EXIT"] = "1"
			}
			profile := object{"id": "p", "dataDir": t.TempDir()}
			settings := object{"browserEngine": "chromix", "chromix": object{"environment": env}}
			info, e := m.Launch(profile, settings, "")
			if e != nil {
				t.Fatal(e)
			}
			if !m.IsRunning("p") || info["pid"] == nil {
				t.Fatal(info)
			}
			h, e := m.get("p")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = m.Tool(context.Background(), "evaluate_js", object{"profileId": "p", "expression": "42"}); e != nil {
				t.Fatal(e)
			}
			if mode == "close" {
				if e = m.CloseProfile("p"); e != nil {
					t.Fatal(e)
				}
			} else {
				select {
				case <-h.finished:
				case <-time.After(4 * time.Second):
					t.Fatal("bridge lifecycle was not observed")
				}
			}
			if m.IsRunning("p") {
				t.Fatal("profile still running")
			}
		})
	}
}
func TestPlaywrightStartupFailures(t *testing.T) {
	for _, source := range []string{`console.log('not json')`, `console.log(JSON.stringify({type:'error',message:'launch denied'}))`, `console.log(JSON.stringify({type:'ready',cdpEndpoint:'http://127.0.0.1:1'}))`, ``} {
		t.Run(source, func(t *testing.T) {
			m := New(nodeFixture(t, source), nil)
			defer m.Shutdown()
			if _, e := m.Launch(object{"id": "p", "dataDir": t.TempDir()}, object{"browserEngine": "chromix"}, ""); e == nil {
				t.Fatal("bad bridge accepted")
			}
			if m.IsRunning("p") {
				t.Fatal("failed launch registered")
			}
		})
	}
}
func TestShutdownCancelsStartup(t *testing.T) {
	m := New(nodeFixture(t, `process.stdin.resume();setInterval(()=>{},1000)`), nil)
	done := make(chan error, 1)
	go func() {
		_, e := m.Launch(object{"id": "p", "dataDir": t.TempDir()}, object{"browserEngine": "chromix"}, "")
		done <- e
	}()
	time.Sleep(100 * time.Millisecond)
	closed := make(chan struct{})
	go func() { m.Shutdown(); close(closed) }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled launch succeeded")
		}
	case <-time.After(17 * time.Second):
		t.Fatal("startup cancellation hung")
	}
	<-closed
	if _, e := m.Launch(object{"id": "p", "dataDir": t.TempDir()}, object{}, ""); e == nil {
		t.Fatal("launch after shutdown succeeded")
	}
}
func TestRealPlaywrightHeadless(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_BROWSER")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_BROWSER for actual Playwright browser test")
	}
	resources, e := filepath.Abs(filepath.Join("..", "..", "resources"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(resources, "playwright", "node_modules", "playwright-core")); e != nil {
		t.Skip("install bundled playwright-core to run real bridge test")
	}
	m := New(resources, nil)
	defer m.Shutdown()
	profile := object{"id": "playwright", "dataDir": t.TempDir(), "startUrl": "about:blank", "chromixOptions": object{"fingerprintMode": "fixed", "fingerprintSeed": "18446744073709551615"}}
	settings := object{"browserEngine": "chromix", "browserBinaryPath": binary, "chromix": object{"options": object{"headless": true, "args": []any{"--no-sandbox"}}}}
	if _, e = m.Launch(profile, settings, ""); e != nil {
		t.Fatal(e)
	}
	v, e := m.Tool(context.Background(), "evaluate_js", object{"profileId": "playwright", "expression": "6*7"})
	if e != nil || number(obj(obj(v)["result"]), "value", 0) != 42 {
		t.Fatal(v, e)
	}
	if e = m.CloseProfile("playwright"); e != nil {
		t.Fatal(e)
	}
}

// Launch passes a script path rather than test flags to the helper executable.
func TestMain(m *testing.M) {
	if os.Getenv("CLOAKSESSION_TEST_EOF_HELPER") == "1" {
		if err := runEOFBridge(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runEOFBridge() error {
	decoder := json.NewDecoder(os.Stdin)
	var request object
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", int(number(request, "cdpPort", 0))))
	if err != nil {
		return err
	}
	closeReceived, exit := make(chan struct{}), make(chan struct{})
	var closeOnce, exitOnce, eofOnce sync.Once
	var eofErr error
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(object{"webSocketDebuggerUrl": os.Getenv("TEST_WS")})
	})
	mux.HandleFunc("/eof", func(w http.ResponseWriter, r *http.Request) {
		eofOnce.Do(func() { eofErr = os.Stdout.Close() })
		if eofErr != nil {
			http.Error(w, eofErr.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-closeReceived:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})
	mux.HandleFunc("/exit", func(w http.ResponseWriter, r *http.Request) { exitOnce.Do(func() { close(exit) }) })
	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	if err := json.NewEncoder(os.Stdout).Encode(object{"type": "ready", "cdpEndpoint": "http://" + listener.Addr().String()}); err != nil {
		return err
	}
	go func() {
		var command object
		if decoder.Decode(&command) == nil && str(command, "type") == "close" {
			closeOnce.Do(func() { close(closeReceived) })
		}
	}()
	// Stdin EOF must not end the helper: the test releases graceful cleanup or
	// leaves it alive to exercise the production shutdown deadline.
	<-exit
	return nil
}

func testPlaywrightEOF(t *testing.T, endpoint string, forceCleanup bool) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Mkdir(filepath.Join(dir, "playwright"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "playwright", "bridge.mjs"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	m := New(dir, nil)
	defer m.Shutdown()
	settings := object{"browserEngine": "chromix", "chromix": object{"nodePath": executable, "environment": object{"CLOAKSESSION_TEST_EOF_HELPER": "1", "TEST_WS": "ws" + strings.TrimPrefix(endpoint, "http") + "/ws"}}}
	if _, err = m.Launch(object{"id": "p", "dataDir": t.TempDir()}, settings, ""); err != nil {
		t.Fatal(err)
	}
	h, err := m.get("p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Tool(context.Background(), "evaluate_js", object{"profileId": "p", "expression": "42"}); err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	get := func(path string) (int, error) {
		response, err := client.Get(h.endpoint + path)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		return response.StatusCode, nil
	}
	if status, err := get("/eof"); err != nil || status != http.StatusOK {
		t.Fatalf("close helper stdout: status=%d error=%v", status, err)
	}
	// A close command received by a still-live helper proves stdout EOF, not
	// process exit or a protocol 'closed' event, initiated the supervisor stop.
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := get("/state")
		if err != nil {
			t.Fatalf("helper exited before EOF was observed: %v", err)
		}
		if status == http.StatusOK {
			break
		}
		if status != http.StatusAccepted {
			t.Fatalf("unexpected helper state status: %d", status)
		}
		if time.Now().After(deadline) {
			t.Fatal("stdout EOF did not trigger the bridge close command within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !h.alive() || !h.stopping.Load() || m.IsRunning("p") {
		t.Fatal("EOF must mark a still-live process as stopping, not running")
	}
	if !forceCleanup {
		// The process may exit before the HTTP response is flushed.
		_, _ = get("/exit")
	}
	cleanupLimit := 5 * time.Second
	if forceCleanup {
		cleanupLimit = 18 * time.Second
	}
	select {
	case <-h.finished:
	case <-time.After(cleanupLimit):
		t.Fatalf("EOF observed, but process cleanup exceeded %s (forced=%t)", cleanupLimit, forceCleanup)
	}
	if h.alive() || m.IsRunning("p") {
		t.Fatal("EOF cleanup left the helper running")
	}
	if !forceCleanup && !h.cmd.ProcessState.Success() {
		t.Fatalf("EOF helper did not exit cleanly: %s", h.cmd.ProcessState)
	}
	h.cdp.mu.Lock()
	pending := len(h.cdp.pending)
	h.cdp.mu.Unlock()
	select {
	case <-h.cdp.done:
	default:
		t.Fatal("EOF cleanup left CDP connected")
	}
	if pending != 0 {
		t.Fatal("EOF cleanup left pending CDP calls")
	}
}

func TestPlaywrightEOFCleanupTimeout(t *testing.T) {
	endpoint, _, _ := fakeCDP(t)
	testPlaywrightEOF(t, endpoint, true)
}
