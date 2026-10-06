package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testClient(t *testing.T) (*coreClient, *bufio.Scanner, io.WriteCloser) {
	t.Helper()
	requests, input := io.Pipe()
	output, responses := io.Pipe()
	client := &coreClient{stdin: input, pending: make(map[uint64]chan coreMessage), ready: make(chan struct{}), done: make(chan struct{})}
	go client.read(output)
	t.Cleanup(func() {
		client.fail(errors.New("test complete"))
		input.Close()
		responses.Close()
		requests.Close()
		output.Close()
	})
	return client, bufio.NewScanner(requests), responses
}

func TestConcurrentRequestsAndEvents(t *testing.T) {
	client, requests, responses := testClient(t)
	events := make(chan string, 1)
	client.onEvent = func(name string, data json.RawMessage) { events <- name + ":" + string(data) }
	json.NewEncoder(responses).Encode(map[string]any{"event": "core:ready", "data": nil})
	var wg sync.WaitGroup
	for _, command := range []string{"profiles_list", "settings_get"} {
		wg.Add(1)
		go func(command string) {
			defer wg.Done()
			result, err := client.invoke(command, nil)
			if err != nil || string(result) != `"`+command+`"` {
				t.Errorf("invoke %s: %s, %v", command, result, err)
			}
		}(command)
	}
	messages := make([]map[string]any, 2)
	for i := range messages {
		if !requests.Scan() {
			t.Fatal("request missing")
		}
		if err := json.Unmarshal(requests.Bytes(), &messages[i]); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		json.NewEncoder(responses).Encode(map[string]any{"id": messages[i]["id"], "result": messages[i]["command"]})
	}
	json.NewEncoder(responses).Encode(map[string]any{"event": "profiles:running-changed", "data": map[string]any{"running": true}})
	if event := <-events; event != `profiles:running-changed:{"running":true}` {
		t.Fatalf("unexpected event %s", event)
	}
	wg.Wait()
}

func TestDialogRoundTrip(t *testing.T) {
	client, requests, responses := testClient(t)
	client.onDialog = func(request dialogRequest) (string, error) {
		if request.Kind != "save" || request.DefaultFilename != "test.mzar" {
			t.Errorf("unexpected dialog: %+v", request)
		}
		return "/tmp/test.mzar", nil
	}
	json.NewEncoder(responses).Encode(map[string]any{"id": 42, "dialog": dialogRequest{Kind: "save", DefaultFilename: "test.mzar"}})
	if !requests.Scan() {
		t.Fatal("dialog response missing")
	}
	var reply coreMessage
	json.Unmarshal(requests.Bytes(), &reply)
	if reply.ID != 42 || string(reply.Result) != `"/tmp/test.mzar"` || reply.Error != "" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestDialogCancellationAndErrors(t *testing.T) {
	for _, failure := range []error{nil, errors.New("picker unavailable")} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			client, requests, responses := testClient(t)
			client.onDialog = func(dialogRequest) (string, error) { return "", failure }
			json.NewEncoder(responses).Encode(map[string]any{"id": 7, "dialog": dialogRequest{Kind: "open"}})
			if !requests.Scan() {
				t.Fatal("dialog response missing")
			}
			var reply coreMessage
			if err := json.Unmarshal(requests.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if string(reply.Result) != "null" {
				t.Fatalf("cancel result: %s", reply.Result)
			}
			if failure != nil && reply.Error != failure.Error() {
				t.Fatalf("dialog error: %s", reply.Error)
			}
		})
	}
}

func TestCoreFailureRejectsPendingAndFutureRequests(t *testing.T) {
	client, requests, responses := testClient(t)
	json.NewEncoder(responses).Encode(map[string]any{"event": "core:ready", "data": nil})
	result := make(chan error, 1)
	go func() { _, err := client.invoke("profiles_list", nil); result <- err }()
	requests.Scan()
	client.fail(errors.New("core exited"))
	if err := <-result; err == nil || err.Error() != "core exited" {
		t.Fatalf("pending request: %v", err)
	}
	if _, err := client.invoke("settings_get", nil); err == nil {
		t.Fatal("future request should fail")
	}
}

func TestInitializationFailureReachesStartupResult(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	exited := make(chan struct{})
	close(exited)
	client := &coreClient{stdin: writer, ready: make(chan struct{}), done: make(chan struct{}), exited: exited, pending: make(map[uint64]chan coreMessage)}
	client.fail(errors.New("database initialization failed"))
	app := newApp()
	app.core = client
	go app.awaitCore()
	if _, err := app.Invoke("profiles_list", nil); err == nil || err.Error() != "database initialization failed" {
		t.Fatalf("startup result: %v", err)
	}
	if app.startErr == nil {
		t.Fatal("native startup dialog would miss initialization failure")
	}
}

func TestCloseInterruptsBlockedWriter(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	exited := make(chan struct{})
	close(exited)
	client := &coreClient{stdin: writer, exited: exited}
	writing := make(chan struct{})
	go func() { _ = client.write(map[string]any{"command": "blocked"}); close(writing) }()
	closed := make(chan struct{})
	go func() { client.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close blocked behind stdin write")
	}
	select {
	case <-writing:
	case <-time.After(time.Second):
		t.Fatal("pipe write was not interrupted")
	}
}

func TestDataDirectoryOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	t.Setenv("CLOAKSESSION_DATA_DIR", dir)
	got, err := dataDirectory()
	if err != nil || got != dir {
		t.Fatalf("dataDirectory = %s, %v", got, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}

func TestRealBrowserLifecycle(t *testing.T) {
	binary, browser := os.Getenv("CLOAKSESSION_TEST_CORE"), os.Getenv("CLOAKSESSION_TEST_BROWSER")
	if binary == "" || browser == "" {
		t.Skip("set CLOAKSESSION_TEST_CORE and CLOAKSESSION_TEST_BROWSER for browser integration")
	}
	data := t.TempDir()
	settings, _ := json.Marshal(map[string]any{
		"mcpHttpEnabled": false, "autoUpdate": false, "browserEngine": "chromix", "browserBinaryPath": browser,
		"chromix": map[string]any{"nodePath": "node", "environment": map[string]string{}, "options": map[string]any{"headless": true, "fingerprintMode": "fixed", "fingerprintSeed": "18446744073709551615"}},
	})
	if err := os.WriteFile(filepath.Join(data, "settings.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 32)
	client, err := startCore(binary, data, os.Getenv("CLOAKSESSION_RESOURCE_DIR"), func(name string, _ json.RawMessage) {
		select {
		case events <- name:
		default:
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.close()
	created, err := client.invoke("profiles_create", map[string]any{"input": map[string]any{"name": "Playwright lifecycle", "startUrl": "about:blank"}})
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &profile); err != nil {
		t.Fatal(err)
	}
	launched, err := client.invoke("profiles_launch", map[string]any{"id": profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	var launch struct {
		Endpoint string `json:"cdpEndpoint"`
	}
	json.Unmarshal(launched, &launch)
	if launch.Endpoint == "" {
		t.Fatalf("launch response: %s", launched)
	}
	response, err := http.Get(launch.Endpoint + "/json/version")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("CDP status: %s", response.Status)
	}
	if _, err := client.invoke("profiles_close", map[string]any{"id": profile.ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event != "chromium:status" && event != "profiles:running-changed" {
			t.Logf("first event: %s", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing browser events")
	}
	if _, err := client.invoke("profiles_delete", map[string]any{"id": profile.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestRealCoreRoundTrip(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_CORE")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_CORE to run the Rust sidecar integration test")
	}
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"mcpHttpEnabled":false,"autoUpdate":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	dialogs := make(chan dialogRequest, 4)
	answers := make(chan string, 4)
	client, err := startCore(binary, data, os.Getenv("CLOAKSESSION_RESOURCE_DIR"), nil, func(request dialogRequest) (string, error) {
		dialogs <- request
		return <-answers, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.close)
	if err := client.waitReady(); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"profiles_list", "settings_get", "system_info", "fingerprint_generate", "fingerprint_devices", "fingerprint_locales", "update_status", "activity_recent"} {
		args := map[string]any{}
		if command == "fingerprint_generate" {
			args["seed"] = "test"
		}
		result, err := client.invoke(command, args)
		if err != nil || !json.Valid(result) {
			t.Errorf("%s: %s, %v", command, result, err)
		}
	}
	if _, err := client.invoke("invalid_command", nil); err == nil {
		t.Fatal("unknown command should fail")
	}
	savedSettings, err := client.invoke("settings_update", map[string]any{"patch": map[string]any{"chromix": map[string]any{"nodePath": "node", "environment": map[string]string{}, "options": map[string]any{"fingerprintMode": "fixed", "fingerprintSeed": "18446744073709551615"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(savedSettings, []byte(`"18446744073709551615"`)) {
		t.Fatalf("uint64 seed changed: %s", savedSettings)
	}
	created, err := client.invoke("profiles_create", map[string]any{"input": map[string]any{"name": "Wails integration"}})
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(created, &profile); err != nil || profile.ID == "" {
		t.Fatalf("create: %s, %v", created, err)
	}
	updated, err := client.invoke("profiles_update", map[string]any{"id": profile.ID, "patch": map[string]any{"name": "Updated via Wails"}})
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(updated, &profile)
	if profile.Name != "Updated via Wails" {
		t.Fatalf("update: %s", updated)
	}
	archivePath := filepath.Join(data, "profile.mzar")
	answers <- archivePath
	if exported, err := client.invoke("profiles_export_archive", map[string]any{"id": profile.ID, "passphrase": "integration-passphrase"}); err != nil {
		t.Fatal(err)
	} else {
		var status struct {
			OK bool `json:"ok"`
		}
		json.Unmarshal(exported, &status)
		if !status.OK {
			t.Fatalf("export: %s", exported)
		}
	}
	if request := <-dialogs; request.Kind != "save" {
		t.Fatalf("export dialog: %+v", request)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatal(err)
	}
	answers <- archivePath
	imported, err := client.invoke("profiles_import_archive", map[string]any{"passphrase": "integration-passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	var importedProfile struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}
	json.Unmarshal(imported, &importedProfile)
	if !importedProfile.OK || importedProfile.ID == "" || importedProfile.ID == profile.ID {
		t.Fatalf("import: %s", imported)
	}
	if request := <-dialogs; request.Kind != "open" {
		t.Fatalf("import dialog: %+v", request)
	}
	if _, err := client.invoke("profiles_delete", map[string]any{"id": importedProfile.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.invoke("profiles_delete", map[string]any{"id": profile.ID}); err != nil {
		t.Fatal(err)
	}
	deleted, err := client.invoke("profiles_get", map[string]any{"id": profile.ID})
	if err != nil || string(deleted) != "null" {
		t.Fatalf("delete: %s, %v", deleted, err)
	}
	finished := make(chan struct{})
	go func() { client.close(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("sidecar shutdown timed out")
	}
}
