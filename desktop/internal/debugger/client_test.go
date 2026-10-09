package debugger

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStdioFixture(t *testing.T) {
	if os.Getenv("CLOAKSESSION_DEBUG_FIXTURE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), maxMessageBytes)
	initialized := false
	pending := ""
	selectedPage := any(float64(0))
	selectionCalls := 0
	var mu sync.Mutex
	send := func(v any) { mu.Lock(); defer mu.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(v) }
	for scanner.Scan() {
		var m message
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			os.Exit(2)
		}
		respond := func(v any) { send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": v}) }
		switch m.Method {
		case "initialize":
			if marker := os.Getenv("CLOAKSESSION_DEBUG_HOLD_INITIALIZE"); marker != "" {
				_ = os.WriteFile(marker, []byte("initializing"), 0600)
				continue
			}
			var p map[string]any
			_ = json.Unmarshal(m.Params, &p)
			if p["clientInfo"] == nil || p["protocolVersion"] == nil {
				os.Exit(3)
			}
			respond(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}})
		case "notifications/initialized":
			initialized = true
		case "tools/list":
			if !initialized {
				os.Exit(4)
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info", "data": "fixture"}})
			send(map[string]any{"jsonrpc": "2.0", "id": "server-ping", "method": "ping"})
			respond(map[string]any{"tools": Definitions()})
		case "notifications/cancelled":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			pending = string(p.RequestID)
		case "tools/call":
			if !initialized {
				os.Exit(5)
			}
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(m.Params, &p)
			mode, _ := p.Arguments["fixture"].(string)
			if p.Name == "select_page" {
				selectionCalls++
				if forced := os.Getenv("CLOAKSESSION_DEBUG_SELECT_MODE"); forced != "" {
					mode = forced
				}
				if page, ok := p.Arguments["pageIdx"]; ok {
					selectedPage = page
				}
			}
			switch mode {
			case "hang":
				continue
			case "hold-handler":
				if marker, ok := p.Arguments["marker"].(string); ok {
					_ = os.WriteFile(marker, []byte("handling"), 0600)
				}
				time.Sleep(30 * time.Second)
				continue
			case "missing-content":
				respond(map[string]any{"isError": false})
				continue
			case "block-read":
				respond(map[string]any{})
				time.Sleep(10 * time.Second)
				continue
			case "exit":
				os.Exit(8)
			case "malformed":
				fmt.Println("not JSON")
				continue
			case "oversized":
				fmt.Println(strings.Repeat("x", maxMessageBytes+1))
				continue
			case "rpc-error":
				send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32602, "message": "fixture error"}})
				continue
			}
			result := map[string]any{"content": []any{map[string]any{"type": "text", "text": p.Name}}, "isError": mode == "tool-error", "structuredContent": map[string]any{"pid": os.Getpid(), "arguments": p.Arguments, "cancelled": pending, "selectedPage": selectedPage, "selectionCalls": selectionCalls}}
			if mode == "slow" {
				id := append(json.RawMessage{}, m.ID...)
				go func() {
					time.Sleep(75 * time.Millisecond)
					send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
				}()
			} else {
				respond(result)
			}
		}
	}
	os.Exit(0)
}
func fixtureClient(t *testing.T) *client {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStdioFixture$")
	cmd.Env = append(os.Environ(), "CLOAKSESSION_DEBUG_FIXTURE=1")
	c, err := startClient(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = c.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	return c
}
func fixtureCall(c *client, ctx context.Context, mode string) (map[string]any, error) {
	var result map[string]any
	err := c.request(ctx, "tools/call", map[string]any{"name": "list_scripts", "arguments": map[string]any{"fixture": mode}}, &result)
	return result, err
}
func TestClientInitializationNotificationsAndErrors(t *testing.T) {
	c := fixtureClient(t)
	if err := checkTools(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	result, err := fixtureCall(c, context.Background(), "tool-error")
	if err != nil || result["isError"] != true {
		t.Fatalf("%v %v", result, err)
	}
	_, err = fixtureCall(c, context.Background(), "rpc-error")
	var rpc *rpcError
	if !errors.As(err, &rpc) || rpc.Code != -32602 {
		t.Fatalf("%v", err)
	}
}
func TestClientConcurrentIDsAndCancellation(t *testing.T) {
	c := fixtureClient(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mode := "fast"
			if i%2 == 0 {
				mode = "slow"
			}
			result, err := fixtureCall(c, context.Background(), mode)
			if err != nil {
				t.Error(err)
				return
			}
			got := result["structuredContent"].(map[string]any)["arguments"].(map[string]any)["fixture"]
			if got != mode {
				t.Errorf("misrouted %v != %v", got, mode)
			}
		}(i)
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := fixtureCall(c, ctx, "hang"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	_, err := fixtureCall(c, context.Background(), "fast")
	if err == nil || !strings.Contains(err.Error(), "attach again") {
		t.Fatalf("timed-out session remained reusable: %v", err)
	}
	select {
	case <-c.exited:
	default:
		t.Fatal("timed-out child was not reaped")
	}
	c.mu.Lock()
	count := len(c.pending)
	c.mu.Unlock()
	if count != 0 {
		t.Fatalf("pending requests leaked: %d", count)
	}
}
func TestClientBrokenStreamsAndExit(t *testing.T) {
	for _, mode := range []string{"exit", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			c := fixtureClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := fixtureCall(c, ctx, mode); err == nil {
				t.Fatal("expected child error")
			}
			if c.err() == nil {
				t.Fatal("failure was not retained")
			}
		})
	}
}
func TestClientRequestLimit(t *testing.T) {
	c := fixtureClient(t)
	if err := c.request(context.Background(), "tools/call", strings.Repeat("x", maxRequestBytes+1), nil); err == nil {
		t.Fatal("accepted oversized request")
	}
	if _, err := fixtureCall(c, context.Background(), "fast"); err != nil {
		t.Fatal(err)
	}
}

func TestClientBlockedWriterIsBounded(t *testing.T) {
	c := fixtureClient(t)
	if _, err := fixtureCall(c, context.Background(), "block-read"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := c.request(ctx, "tools/call", strings.Repeat("x", 512<<10), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked writer: %v", err)
	}
	if c.err() == nil {
		t.Fatal("partially written stream was retained")
	}
}
