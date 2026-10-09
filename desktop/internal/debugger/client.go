// Package debugger owns isolated js-reverse-mcp stdio clients for managed profiles.
package debugger

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const (
	maxMessageBytes = 8 << 20
	maxRequestBytes = 1 << 20
	requestTimeout  = 60 * time.Second
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("debug MCP error %d: %s", e.Code, e.Message) }

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type reply struct {
	result json.RawMessage
	err    error
}
type outbound struct {
	ctx  context.Context
	data []byte
	sent chan error
}

type client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	writes  chan outbound
	done    chan struct{}
	exited  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	next    uint64
	pending map[string]chan reply
	failure error
}

func startClient(cmd *exec.Cmd) (*client, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	// Discard logs rather than retaining unbounded or sensitive browser evidence.
	cmd.Stderr = io.Discard
	configureProcess(cmd)
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	c := &client{cmd: cmd, stdin: stdin, stdout: stdout, writes: make(chan outbound, 32), done: make(chan struct{}), exited: make(chan struct{}), pending: map[string]chan reply{}}
	go c.writeLoop()
	go c.readLoop()
	go func() {
		err := cmd.Wait()
		if err == nil {
			err = errors.New("debug MCP child exited")
		}
		c.fail(fmt.Errorf("debug MCP child exited: %w; attach again to reconnect", err))
		close(c.exited)
	}()
	return c, nil
}
func (c *client) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.failure = err
		close(c.done)
		c.mu.Unlock()
		_ = c.stdin.Close()
		_ = c.stdout.Close()
		killProcess(c.cmd)
	})
}
func (c *client) err() error { c.mu.Lock(); defer c.mu.Unlock(); return c.failure }
func (c *client) Close() {
	c.fail(errors.New("debug session detached"))
	select {
	case <-c.exited:
	case <-time.After(3 * time.Second):
	}
}
func (c *client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case item := <-c.writes:
			if err := item.ctx.Err(); err != nil {
				item.sent <- err
				continue
			}
			_, err := c.stdin.Write(item.data)
			item.sent <- err
			if err != nil {
				c.fail(fmt.Errorf("debug MCP write: %w", err))
				return
			}
		}
	}
}
func (c *client) send(ctx context.Context, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxRequestBytes {
		return errors.New("debug MCP request exceeds 1 MiB")
	}
	item := outbound{ctx: ctx, data: append(data, '\n'), sent: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.err()
	case c.writes <- item:
	}
	select {
	case err := <-item.sent:
		return err
	case <-c.done:
		return c.err()
	case <-ctx.Done():
		// A partially written request cannot safely be retried on the same stream.
		c.fail(fmt.Errorf("debug MCP write interrupted: %w; session closed, attach again to reconnect", ctx.Err()))
		return ctx.Err()
	}
}
func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	if id[0] == '"' {
		var value string
		return json.Unmarshal(id, &value) == nil
	}
	_, err := strconv.ParseInt(string(id), 10, 64)
	return err == nil
}

func (c *client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 4096), maxMessageBytes)
	for scanner.Scan() {
		var m message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil || m.JSONRPC != "2.0" {
			c.fail(errors.New("invalid debug MCP stdio message"))
			return
		}
		if len(m.ID) != 0 && !validID(m.ID) {
			c.fail(errors.New("invalid debug MCP message ID"))
			return
		}
		if m.Method != "" {
			if len(m.ID) == 0 {
				if m.Method == "notifications/cancelled" {
					var params struct {
						RequestID json.RawMessage `json:"requestId"`
					}
					if json.Unmarshal(m.Params, &params) == nil {
						c.mu.Lock()
						ch := c.pending[string(params.RequestID)]
						c.mu.Unlock()
						if ch != nil {
							select {
							case ch <- reply{err: context.Canceled}:
							default:
							}
						}
					}
				}
				continue
			}
			response := map[string]any{"jsonrpc": "2.0", "id": m.ID}
			if m.Method == "ping" {
				response["result"] = map[string]any{}
			} else {
				response["error"] = &rpcError{Code: -32601, Message: "client method not supported"}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := c.send(ctx, response)
			cancel()
			if err != nil {
				c.fail(err)
				return
			}
			continue
		}
		if len(m.ID) == 0 || string(m.ID) == "null" || (len(m.Result) == 0) == (m.Error == nil) {
			c.fail(errors.New("invalid debug MCP response"))
			return
		}
		c.mu.Lock()
		ch := c.pending[string(m.ID)]
		c.mu.Unlock()
		if ch != nil {
			r := reply{result: m.Result}
			if m.Error != nil {
				r.err = m.Error
			}
			select {
			case ch <- r:
			default:
			}
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.fail(fmt.Errorf("debug MCP stream ended: %w; attach again to reconnect", err))
}
func (c *client) request(ctx context.Context, method string, params any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.next++
	id := strconv.FormatUint(c.next, 10)
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.send(ctx, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(r.result, out)
	case <-c.done:
		return c.err()
	case <-ctx.Done():
		if method != "initialize" {
			cleanup, stop := context.WithTimeout(context.Background(), 250*time.Millisecond)
			_ = c.send(cleanup, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": json.RawMessage(id), "reason": ctx.Err().Error()}})
			stop()
		}
		if method == "tools/call" {
			err := fmt.Errorf("debug tool call interrupted: %w; session closed, attach again to reconnect", ctx.Err())
			c.fail(err)
			c.Close()
			return err
		}
		return ctx.Err()
	}
}
func (c *client) initialize(ctx context.Context) error {
	var result struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
	}
	err := c.request(ctx, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "cloaksession-debugger", "version": "1.0"}}, &result)
	if err != nil {
		return err
	}
	switch result.ProtocolVersion {
	case "2024-11-05", "2025-03-26", "2025-06-18":
	default:
		return fmt.Errorf("unsupported debug MCP protocol %q", result.ProtocolVersion)
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		return errors.New("debug MCP server does not support tools")
	}
	return c.send(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}
