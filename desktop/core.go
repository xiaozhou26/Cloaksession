package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type fileFilter struct {
	DisplayName string `json:"displayName"`
	Pattern     string `json:"pattern"`
}

type dialogRequest struct {
	Kind            string       `json:"kind"`
	Title           string       `json:"title"`
	Filters         []fileFilter `json:"filters"`
	DefaultFilename string       `json:"defaultFilename"`
}

type coreMessage struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Event  string          `json:"event"`
	Data   json.RawMessage `json:"data"`
	Dialog *dialogRequest  `json:"dialog"`
}

type coreClient struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	mu        sync.Mutex
	pending   map[uint64]chan coreMessage
	nextID    atomic.Uint64
	ready     chan struct{}
	done      chan struct{}
	exited    chan struct{}
	readyOnce sync.Once
	stopOnce  sync.Once
	err       error
	onEvent   func(string, json.RawMessage)
	onDialog  func(dialogRequest) (string, error)
}

func startCore(binary, dataDir, resourceDir string, onEvent func(string, json.RawMessage), onDialog func(dialogRequest) (string, error)) (*coreClient, error) {
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "CLOAKSESSION_DATA_DIR="+dataDir, "CLOAKSESSION_RESOURCE_DIR="+resourceDir)
	cmd.Stderr = os.Stderr
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	client := &coreClient{cmd: cmd, stdin: stdin, pending: make(map[uint64]chan coreMessage), ready: make(chan struct{}), done: make(chan struct{}), exited: make(chan struct{}), onEvent: onEvent, onDialog: onDialog}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start browser core: %w", err)
	}
	go func() {
		client.read(stdout)
		err := cmd.Wait()
		if err == nil {
			err = errors.New("browser core stopped")
		}
		client.fail(err)
		close(client.exited)
	}()
	return client, nil
}

func (c *coreClient) write(message any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return json.NewEncoder(c.stdin).Encode(message)
}

func (c *coreClient) read(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var message coreMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			c.fail(fmt.Errorf("invalid browser core response: %w", err))
			c.kill()
			return
		}
		switch {
		case message.Dialog != nil:
			go c.handleDialog(message.ID, *message.Dialog)
		case message.Event == "core:ready":
			c.readyOnce.Do(func() { close(c.ready) })
		case message.Event != "":
			if c.onEvent != nil {
				c.onEvent(message.Event, message.Data)
			}
		default:
			c.mu.Lock()
			ch := c.pending[message.ID]
			delete(c.pending, message.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- message
			}
		}
	}
	if err := scanner.Err(); err != nil {
		c.fail(fmt.Errorf("read browser core response: %w", err))
		c.kill()
	}
}

func (c *coreClient) handleDialog(id uint64, request dialogRequest) {
	var path string
	var err error
	if c.onDialog == nil {
		err = errors.New("native dialogs are unavailable")
	} else {
		path, err = c.onDialog(request)
	}
	response := map[string]any{"id": id, "result": nil}
	if err != nil {
		response["error"] = err.Error()
	} else if path != "" {
		response["result"] = path
	}
	if err := c.write(response); err != nil {
		c.fail(err)
	}
}

func (c *coreClient) fail(err error) {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		for id, ch := range c.pending {
			ch <- coreMessage{Error: err.Error()}
			delete(c.pending, id)
		}
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *coreClient) waitReady() error {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case <-c.ready:
		return nil
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	case <-timer.C:
		return errors.New("browser core startup timed out")
	}
}

func (c *coreClient) invoke(command string, args map[string]any) (json.RawMessage, error) {
	if err := c.waitReady(); err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	ch := make(chan coreMessage, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if args == nil {
		args = map[string]any{}
	}
	if err := c.write(map[string]any{"id": id, "command": command, "args": args}); err != nil {
		c.fail(err)
	}
	response := <-ch
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	return response.Result, nil
}

func (c *coreClient) kill() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

func (c *coreClient) close() {
	// Closing the pipe interrupts blocked writes without waiting for writeMu.
	_ = c.stdin.Close()
	select {
	case <-c.exited:
	case <-time.After(5 * time.Second):
		c.kill()
		<-c.exited
	}
}
