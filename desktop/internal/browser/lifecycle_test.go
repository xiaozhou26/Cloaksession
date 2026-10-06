package browser

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

const fakeBridge = `import readline from 'node:readline';import http from 'node:http';import fs from 'node:fs';
let server;const lines=readline.createInterface({input:process.stdin});
lines.on('line',line=>{const r=JSON.parse(line);if(r.type==='close'){process.stdout.write(JSON.stringify({type:'closed'})+'\n');server?.close(()=>process.exit(0));return}
server=http.createServer((q,s)=>{s.setHeader('content-type','application/json');s.end(JSON.stringify({webSocketDebuggerUrl:process.env.TEST_WS}))});server.listen(r.cdpPort,'127.0.0.1',()=>{process.stdout.write(JSON.stringify({type:'ready',cdpEndpoint:'http://127.0.0.1:'+r.cdpPort})+'\n');if(process.env.TEST_EOF)setTimeout(()=>fs.closeSync(1),200);if(process.env.TEST_EXIT)setTimeout(()=>process.exit(0),200)})});
lines.on('close',()=>{server?.close(()=>process.exit(0))});`

func TestPlaywrightLifecycle(t *testing.T) {
	endpoint, _, _ := fakeCDP(t)
	for _, mode := range []string{"close", "eof", "exit"} {
		t.Run(mode, func(t *testing.T) {
			dir := nodeFixture(t, fakeBridge)
			m := New(dir, nil)
			defer m.Shutdown()
			env := object{"TEST_WS": "ws" + strings.TrimPrefix(endpoint, "http") + "/ws"}
			if mode == "eof" {
				env["TEST_EOF"] = "1"
			}
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
