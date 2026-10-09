package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Opt in with CLOAKSESSION_TEST_REVERSE_BROWSER pointing to a local Chrome binary.
func TestReverseIntegration(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_REVERSE_BROWSER")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_REVERSE_BROWSER to an existing Chrome executable")
	}
	var err error
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("invalid CLOAKSESSION_TEST_REVERSE_BROWSER %q: %v", binary, err)
	}

	t.Run("attach_tools_pages_scripts", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "discovery")
		catalog := reverseObject(t, h.invoke(t, "debugger_tools", nil))
		names := make(map[string]bool)
		for _, tool := range reverseObjects(t, catalog["tools"]) {
			name := text(tool["name"])
			if name == "" || names[name] || text(tool["description"]) == "" {
				t.Fatalf("invalid tool definition: %v", tool)
			}
			names[name] = true
			if reverseObject(t, tool["inputSchema"])["type"] != "object" {
				t.Fatalf("invalid schema: %v", tool)
			}
		}
		for _, name := range []string{"select_page", "select_frame", "list_scripts", "break_on_xhr", "get_paused_info", "pause_or_resume", "list_network_requests", "get_request_initiator", "get_websocket_messages"} {
			if !names[name] {
				t.Errorf("missing reverse tool %q", name)
			}
		}
		sessions := reverseObjects(t, reverseObject(t, h.invoke(t, "debugger_sessions", nil))["sessions"])
		if len(sessions) != 1 || sessions[0]["debugSessionId"] != a.id || sessions[0]["profileId"] != a.profileID || sessions[0]["status"] != "attached" {
			t.Fatalf("session not registered: %v", sessions)
		}
		scripts := a.wait(t, "list_scripts", map[string]any{"filter": "/fixture.js"}, func(data map[string]any) bool {
			return len(reverseObjects(t, data["scripts"])) > 0
		})
		script := reverseObjects(t, scripts["scripts"])[0]
		if script["url"] != h.url+"/fixture.js" || text(script["scriptId"]) == "" {
			t.Fatalf("fixture script missing: %v", scripts)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		source, err := a.call(ctx, "get_script_source", map[string]any{"url": h.url + "/fixture.js", "startLine": 1, "endLine": 50})
		if err != nil {
			t.Fatal(err)
		}
		reverseToolData(t, "get_script_source", source)
		if !strings.Contains(reverseJSON(t, source["content"]), "reverseSendXHR") {
			t.Fatalf("source does not contain fixture function: %v", source)
		}
		search := a.tool(t, "search_in_sources", map[string]any{"query": "progress += 7;", "urlFilter": h.url + "/fixture.js", "caseSensitive": true})
		matches := reverseObjects(t, search["matches"])
		if number(search["totalMatches"]) != 1 || len(matches) != 1 || matches[0]["url"] != h.url+"/fixture.js" || number(matches[0]["lineNumber"]) <= 0 || !strings.Contains(text(matches[0]["lineContent"]), "progress += 7;") {
			t.Fatalf("source search did not locate the exact statement: %v", search)
		}
		exported := a.tool(t, "save_script_source", map[string]any{"url": h.url + "/fixture.js", "filePath": "fixture-source.js", "format": false})
		filename := text(exported["filename"])
		reverseAssertWithin(t, a.allowedRoot, filename)
		actual, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile(filepath.Join("testdata", "reverse", "fixture.js"))
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(expected) || exported["formatted"] != false {
			t.Fatalf("export is not the complete unformatted fixture: %v", exported)
		}
		for _, path := range []string{"../outside-source.js", filepath.Join(h.dataDir, "outside-absolute.js")} {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			result, err := a.call(ctx, "save_script_source", map[string]any{"url": h.url + "/fixture.js", "filePath": path, "format": false})
			cancel()
			if err == nil || !strings.Contains(err.Error(), "outside the debug allowedRoot") {
				t.Fatalf("out-of-root export was not rejected: path=%q result=%v error=%v", path, result, err)
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(a.allowedRoot, path)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("rejected export created %q: %v", path, err)
			}
		}
	})

	t.Run("windows_and_stable_target_selection", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "windows")
		url := h.url + "/?profile=windows"
		a.evaluate(t, `() => { window.reverseTabMarker = "original"; return true; }`)
		original := reverseFindURL(t, a.windowTabs(t), url)
		originalID := text(original["targetId"])
		a.tool(t, "new_page", map[string]any{"url": url, "timeout": 10000})
		a.evaluate(t, `() => { window.reverseTabMarker = "duplicate"; return true; }`)
		var duplicateID string
		for _, tab := range a.windowTabs(t) {
			if tab["url"] == url && tab["targetId"] != originalID {
				duplicateID = text(tab["targetId"])
			}
		}
		if duplicateID == "" {
			t.Fatal("list_windows did not distinguish duplicate-URL targets")
		}
		// Change the page snapshot after retaining target IDs, without refreshing them.
		a.tool(t, "new_page", map[string]any{"url": h.url + "/?profile=extra", "timeout": 10000})
		for _, item := range []struct{ id, marker string }{{originalID, "original"}, {duplicateID, "duplicate"}, {originalID, "original"}} {
			a.tool(t, "select_page", map[string]any{"targetId": item.id})
			value := reverseObject(t, a.evaluate(t, "() => ({url: location.href, marker: window.reverseTabMarker})"))
			if value["url"] != url || value["marker"] != item.marker {
				t.Fatalf("targetId selected the wrong duplicate tab: %v", value)
			}
			selected := 0
			for _, tab := range a.windowTabs(t) {
				if tab["selected"] == true {
					selected++
					if tab["targetId"] != item.id {
						t.Fatalf("list_windows selected the wrong target: %v", tab)
					}
				}
			}
			if selected != 1 {
				t.Fatalf("list_windows has %d selected tabs", selected)
			}
		}
	})

	t.Run("text_breakpoint_and_step", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "step")
		breakpoint := a.tool(t, "set_breakpoint_on_text", map[string]any{"text": "progress += 7;", "urlFilter": h.url + "/fixture.js"})
		id := text(breakpoint["breakpointId"])
		if id == "" || breakpoint["url"] != h.url+"/fixture.js" || number(breakpoint["lineNumber"]) <= 0 {
			t.Fatalf("invalid text breakpoint: %v", breakpoint)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = a.call(ctx, "pause_or_resume", map[string]any{"action": "resume"})
		})
		if value := a.evaluate(t, "() => window.reverseFixture.scheduleStep()"); value != "step-scheduled" {
			t.Fatalf("step probe not scheduled: %v", value)
		}
		paused := a.wait(t, "get_paused_info", map[string]any{"includeScopes": true}, func(data map[string]any) bool { return data["paused"] == true })
		hits, ok := paused["hitBreakpoints"].([]any)
		if !ok || len(hits) != 1 || hits[0] != id {
			t.Fatalf("text breakpoint did not cause the pause: %v", paused)
		}
		frames := reverseObjects(t, paused["callFrames"])
		if len(frames) == 0 || frames[0]["functionName"] != "reverseStepProbe" {
			t.Fatalf("wrong paused function: %v", paused)
		}
		before := reverseObject(t, frames[0]["location"])
		if number(before["lineNumber"])+1 != number(breakpoint["lineNumber"]) {
			t.Fatalf("pause missed text location: %v", paused)
		}
		if value := a.evaluate(t, "() => progress"); number(value) != 10 {
			t.Fatalf("unexpected local before step: %v", value)
		}
		stepped := a.tool(t, "step", map[string]any{"direction": "over"})
		frame := reverseObject(t, stepped["callFrame"])
		after := reverseObject(t, frame["location"])
		if stepped["direction"] != "over" || frame["functionName"] != "reverseStepProbe" || after["scriptId"] != before["scriptId"] || number(after["lineNumber"]) <= number(before["lineNumber"]) {
			t.Fatalf("step did not advance in fixture source: %v", stepped)
		}
		paused = a.tool(t, "get_paused_info", map[string]any{"includeScopes": true})
		frames = reverseObjects(t, paused["callFrames"])
		if len(frames) == 0 {
			t.Fatal("step lost the paused stack")
		}
		locals := a.tool(t, "evaluate_script", map[string]any{"function": "() => progress", "frameIndex": frames[0]["frameIndex"], "confirm": true})
		if number(locals["value"]) != 17 {
			t.Fatalf("step did not execute the statement: %v", locals)
		}
		removed := a.tool(t, "remove_breakpoint", map[string]any{"action": "remove_code", "breakpointId": id, "confirm": true})
		if reverseObject(t, removed["removed"])["breakpointId"] != id {
			t.Fatalf("wrong code breakpoint removed: %v", removed)
		}
		a.tool(t, "pause_or_resume", map[string]any{"action": "resume"})
		a.wait(t, "evaluate_script", map[string]any{"function": "() => window.reverseFixture.stepResult ?? null", "confirm": true, "mainWorld": true}, func(data map[string]any) bool { return number(data["value"]) == 17 })
	})

	t.Run("select_child_frame", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "frames")
		frames := a.wait(t, "select_frame", nil, func(data map[string]any) bool {
			return len(reverseObjects(t, data["frames"])) == 2
		})
		child := reverseFindURL(t, frames["frames"], h.url+"/frame.html")
		selected := a.tool(t, "select_frame", map[string]any{"frameIdx": child["frameIdx"]})
		if reverseObject(t, selected["selectedFrame"])["isMainFrame"] != false {
			t.Fatalf("child frame was not selected: %v", selected)
		}
		if value := a.evaluate(t, "() => window.reverseFrameMarker"); value != "reverse-child-context" {
			t.Fatalf("evaluation did not use child frame: %v", value)
		}
		a.tool(t, "select_frame", map[string]any{"frameIdx": 0})
		if value := a.evaluate(t, "() => window.reverseFixture.ready"); value != true {
			t.Fatalf("main frame was not restored: %v", value)
		}
		crossURL := strings.Replace(h.url, "127.0.0.1", "localhost", 1) + "/frame.html"
		a.evaluate(t, fmt.Sprintf(`() => { const frame = document.createElement("iframe"); frame.id = "cross-site"; frame.src = %q; frame.onload = () => { frame.dataset.loaded = "yes"; }; document.body.append(frame); return true; }`, crossURL))
		a.wait(t, "evaluate_script", map[string]any{"function": `() => document.querySelector("#cross-site").dataset.loaded === "yes"`, "confirm": true, "mainWorld": true}, func(data map[string]any) bool { return data["value"] == true })
		frames = a.tool(t, "select_frame", nil)
		cross := reverseFindURL(t, frames["frames"], crossURL)
		// With site-per-process the cross-site child must have its own CDP target.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		targets, err := h.service.browser.Call(ctx, a.profileID, "Target.getTargets", nil)
		if err != nil {
			t.Fatal(err)
		}
		var oopif bool
		for _, target := range reverseObjects(t, reverseObject(t, targets)["targetInfos"]) {
			if target["type"] == "iframe" && target["url"] == crossURL {
				oopif = true
			}
		}
		if !oopif {
			t.Fatalf("cross-site fixture did not create an OOPIF: %v", targets)
		}
		selected = a.tool(t, "select_frame", map[string]any{"frameIdx": cross["frameIdx"]})
		if reverseObject(t, selected["selectedFrame"])["url"] != crossURL {
			t.Fatalf("wrong cross-site frame selected: %v", selected)
		}
		value := reverseObject(t, a.evaluate(t, "() => ({marker: window.reverseFrameMarker, origin: location.origin})"))
		if value["marker"] != "reverse-child-context" || value["origin"] != strings.TrimSuffix(crossURL, "/frame.html") {
			t.Fatalf("evaluation missed cross-site frame: %v", value)
		}
		a.tool(t, "select_frame", map[string]any{"frameIdx": 0})
		if value := a.evaluate(t, "() => window.reverseFixture.ready"); value != true {
			t.Fatalf("main frame not restored after OOPIF: %v", value)
		}
	})

	t.Run("xhr_breakpoint_pause_resume", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "xhr")
		const token = "xhr-paused"
		pattern := "/api/echo?token=" + token
		a.tool(t, "list_network_requests", nil)
		breakpoint := a.tool(t, "break_on_xhr", map[string]any{"url": pattern})
		if breakpoint["kind"] != "xhr" || breakpoint["urlPattern"] != pattern {
			t.Fatalf("unexpected XHR breakpoint: %v", breakpoint)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = a.call(ctx, "pause_or_resume", map[string]any{"action": "resume"})
		})
		a.scheduleXHR(t, token)
		paused := a.wait(t, "get_paused_info", map[string]any{"includeScopes": true}, func(data map[string]any) bool { return data["paused"] == true })
		if paused["reason"] != "XHR" {
			t.Fatalf("pause was not caused by the XHR breakpoint: %v", paused)
		}
		frames := reverseObjects(t, paused["callFrames"])
		if len(frames) == 0 || frames[0]["functionName"] != "reverseSendXHR" || text(frames[0]["callFrameId"]) == "" {
			t.Fatalf("missing actual fixture stack: %v", paused)
		}
		locals := a.tool(t, "evaluate_script", map[string]any{"function": "() => ({token, payload})", "frameIndex": frames[0]["frameIndex"], "confirm": true, "mainWorld": true})
		value := reverseObject(t, locals["value"])
		if value["token"] != token || value["payload"] != reversePayload(token) {
			t.Fatalf("paused locals differ from the real request: %v", locals)
		}
		h.mu.Lock()
		_, reachedServer := h.requests[token]
		h.mu.Unlock()
		if reachedServer {
			t.Fatal("XHR reached the server before execution was resumed")
		}
		removed := a.tool(t, "remove_breakpoint", map[string]any{"action": "remove_xhr", "url": pattern, "confirm": true})
		if reverseObject(t, removed["removed"])["url"] != pattern {
			t.Fatalf("wrong breakpoint removed: %v", removed)
		}
		resumed := a.tool(t, "pause_or_resume", map[string]any{"action": "resume"})
		if resumed["state"] != "running" {
			t.Fatalf("resume did not report running state: %v", resumed)
		}
		a.waitHTTP(t, token)
		h.assertRequest(t, token)
	})

	t.Run("http_body_and_initiator", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "http")
		const token = "http-evidence"
		a.tool(t, "list_network_requests", nil)
		a.scheduleXHR(t, token)
		a.waitHTTP(t, token)
		h.assertRequest(t, token)
		request := a.request(t, token)
		if request["method"] != "POST" || request["resourceType"] != "xhr" {
			t.Fatalf("incorrect captured request: %v", request)
		}
		details := a.tool(t, "list_network_requests", map[string]any{"reqid": request["reqid"]})
		if reverseObject(t, details["request"])["pending"] != false {
			t.Fatalf("completed request still pending: %v", details)
		}
		for _, part := range []string{"requestBody", "responseBody"} {
			exported := a.tool(t, "list_network_requests", map[string]any{"reqid": request["reqid"], "outputPart": part, "outputFile": part + ".json"})
			filename := text(reverseObject(t, exported["export"])["filename"])
			reverseAssertWithin(t, a.allowedRoot, filename)
			body, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if part == "requestBody" && string(body) != reversePayload(token) {
				t.Fatalf("captured request body = %s", body)
			}
			if part == "responseBody" {
				var response map[string]any
				if err := json.Unmarshal(body, &response); err != nil || response["echo"] != reversePayload(token) || response["token"] != token {
					t.Fatalf("captured response body = %s, error = %v", body, err)
				}
			}
		}
		initiator := a.tool(t, "get_request_initiator", map[string]any{"requestId": request["reqid"]})
		evidence := reverseObject(t, initiator["initiator"])
		if evidence["type"] != "script" {
			t.Fatalf("no JavaScript initiator captured: %v", initiator)
		}
		stack := reverseObjects(t, reverseObject(t, evidence["stack"])["callFrames"])
		found := false
		for _, frame := range stack {
			if frame["functionName"] == "reverseSendXHR" && frame["url"] == h.url+"/fixture.js" {
				found = true
			}
		}
		if !found {
			t.Fatalf("initiator does not identify fixture source: %v", initiator)
		}
	})

	t.Run("websocket_sent_received_frames", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "websocket")
		const token = "socket-evidence"
		a.tool(t, "get_websocket_messages", nil)
		if value := a.evaluate(t, `() => window.reverseFixture.openSocket("`+token+`")`); value != token {
			t.Fatalf("socket was not scheduled: %v", value)
		}
		connections := a.wait(t, "get_websocket_messages", map[string]any{"urlFilter": "token=" + token}, func(data map[string]any) bool {
			items := reverseObjects(t, data["connections"])
			return len(items) == 1 && number(items[0]["frameCount"]) >= 2
		})
		connection := reverseObjects(t, connections["connections"])[0]
		for direction, payload := range map[string]string{"sent": "client:" + token, "received": "server:client:" + token} {
			data := a.tool(t, "get_websocket_messages", map[string]any{"wsid": connection["wsid"], "direction": direction, "show_content": true})
			frames := reverseObjects(t, data["frames"])
			if len(frames) != 1 || frames[0]["direction"] != direction || frames[0]["payloadData"] != payload || number(frames[0]["opcode"]) != 1 {
				t.Fatalf("incorrect %s frame: %v", direction, data)
			}
		}
		if value := a.evaluate(t, `() => window.reverseFixture.sockets["`+token+`"].received`); value != "server:client:"+token {
			t.Fatalf("browser did not receive echo: %v", value)
		}
	})

	t.Run("detach_keeps_browser_alive", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a := h.attach(t, "detach")
		a.evaluate(t, `() => { window.reverseSurvivesDetach = "still-here"; return true; }`)
		detached := reverseObject(t, h.invoke(t, "debugger_detach", map[string]any{"debugSessionId": a.id}))
		if detached["detached"] != true || !h.service.browser.IsRunning(a.profileID) {
			t.Fatalf("detach stopped the managed browser: %v", detached)
		}
		if sessions := reverseObjects(t, reverseObject(t, h.invoke(t, "debugger_sessions", nil))["sessions"]); len(sessions) != 0 {
			t.Fatalf("detached session retained: %v", sessions)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.call(ctx, "select_page", nil); err == nil {
			t.Fatal("detached session still accepts tool calls")
		}
		result := reverseObject(t, h.invoke(t, "evaluate_js", map[string]any{"profileId": a.profileID, "expression": "window.reverseSurvivesDetach"}))
		if reverseObject(t, result["result"])["value"] != "still-here" {
			t.Fatalf("browser page did not survive detach: %v", result)
		}
		again := reverseObject(t, h.invoke(t, "debugger_attach", map[string]any{"profileId": a.profileID}))
		if text(again["debugSessionId"]) == "" || again["debugSessionId"] == a.id {
			t.Fatalf("reattach did not create a new session: %v", again)
		}
		a.id = text(again["debugSessionId"])
		if value := a.evaluate(t, "() => window.reverseSurvivesDetach"); value != "still-here" {
			t.Fatalf("reattach replaced the browser page: %v", value)
		}
	})

	t.Run("two_profiles_are_isolated", func(t *testing.T) {
		h := newReverseHarness(t, binary)
		a, b := h.attach(t, "profile-a"), h.attach(t, "profile-b")
		if a.id == b.id || a.profileID == b.profileID || a.endpoint == b.endpoint || a.allowedRoot == b.allowedRoot || a.profileDir == b.profileDir {
			t.Fatalf("profiles share a session, endpoint, or directory: A=%+v B=%+v", a, b)
		}
		for _, item := range []struct {
			session *reverseSession
			marker  string
		}{{a, "alpha"}, {b, "beta"}} {
			item.session.evaluate(t, fmt.Sprintf(`() => { localStorage.setItem("reverse-marker", %q); document.cookie = "reverse_marker=" + %q + "; Path=/; SameSite=Lax"; return true; }`, item.marker, item.marker))
			item.session.tool(t, "list_network_requests", nil)
		}
		for _, item := range []struct {
			session *reverseSession
			marker  string
		}{{a, "alpha"}, {b, "beta"}} {
			value := reverseObject(t, item.session.evaluate(t, `() => ({storage: localStorage.getItem("reverse-marker"), cookie: document.cookie})`))
			if value["storage"] != item.marker || value["cookie"] != "reverse_marker="+item.marker {
				t.Fatalf("profile storage/cookie leaked: %v", value)
			}
		}
		a.scheduleXHR(t, "only-profile-a")
		a.waitHTTP(t, "only-profile-a")
		a.request(t, "only-profile-a")
		other := b.tool(t, "list_network_requests", map[string]any{"urlFilter": "token=only-profile-a"})
		if len(reverseObjects(t, other["requests"])) != 0 {
			t.Fatalf("profile A network evidence leaked to B: %v", other)
		}
		b.tool(t, "new_page", map[string]any{"url": h.url + "/?profile=b-second", "timeout": 10000})
		pages := b.tool(t, "select_page", nil)
		if reverseFindURL(t, pages["pages"], h.url+"/?profile=b-second")["selected"] != true {
			t.Fatalf("B new page was not selected: %v", pages)
		}
		if value := a.evaluate(t, "() => location.href"); value != h.url+"/?profile=profile-a" {
			t.Fatalf("B page selection changed A context: %v", value)
		}
		h.invoke(t, "debugger_detach", map[string]any{"debugSessionId": a.id})
		if value := b.evaluate(t, "() => localStorage.getItem('reverse-marker')"); value != "beta" {
			t.Fatalf("detaching A disrupted B: %v", value)
		}
		if !h.service.browser.IsRunning(a.profileID) || !h.service.browser.IsRunning(b.profileID) {
			t.Fatal("detaching one session closed a browser")
		}
	})
}

type reverseHarness struct {
	service      *Service
	url, dataDir string
	mu           sync.Mutex
	requests     map[string]string
}

type reverseSession struct {
	h                                                *reverseHarness
	id, profileID, profileDir, endpoint, allowedRoot string
}

func newReverseHarness(t *testing.T, binary string) *reverseHarness {
	t.Helper()
	h := &reverseHarness{dataDir: t.TempDir(), requests: make(map[string]string)}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join("testdata", "reverse"))))
	mux.HandleFunc("/api/echo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		token := r.URL.Query().Get("token")
		h.mu.Lock()
		h.requests[token] = string(body)
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token, "echo": string(body)})
	})
	var socketMu sync.Mutex
	sockets := make(map[*websocket.Conn]bool)
	upgrader := websocket.Upgrader{HandshakeTimeout: 3 * time.Second}
	mux.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		socketMu.Lock()
		sockets[conn] = true
		socketMu.Unlock()
		defer func() {
			_ = conn.Close()
			socketMu.Lock()
			delete(sockets, conn)
			socketMu.Unlock()
		}()
		conn.SetReadLimit(8192)
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		kind, payload, err := conn.ReadMessage()
		if err != nil || kind != websocket.TextMessage {
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		_ = conn.WriteMessage(websocket.TextMessage, append([]byte("server:"), payload...))
	})
	server := httptest.NewServer(mux)
	h.url = server.URL
	t.Cleanup(func() {
		socketMu.Lock()
		for conn := range sockets {
			_ = conn.Close()
		}
		socketMu.Unlock()
		server.Close()
	})
	settings, err := json.Marshal(map[string]any{
		"mcpHttpEnabled": false, "autoUpdate": false, "browserEngine": "cft",
		"browserBinaryPath": binary, "headless": true,
		"browserArgs": []string{"--site-per-process", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--no-default-browser-check"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dataDir, "settings.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	resources, err := filepath.Abs("resources")
	if err != nil {
		t.Fatal(err)
	}
	h.service, err = newService(h.dataDir, resources, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.service.Close)
	return h
}

func (h *reverseHarness) invoke(t *testing.T, command string, args map[string]any) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	value, err := h.service.Invoke(ctx, command, args)
	if err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	return value
}

func (h *reverseHarness) attach(t *testing.T, name string) *reverseSession {
	t.Helper()
	profile := reverseObject(t, h.invoke(t, "profiles_create", map[string]any{"input": map[string]any{"name": "Reverse test " + name, "startUrl": "about:blank"}}))
	a := &reverseSession{h: h, profileID: text(profile["id"]), profileDir: text(profile["dataDir"])}
	reverseAssertWithin(t, h.dataDir, a.profileDir)
	launched := reverseObject(t, h.invoke(t, "profiles_launch", map[string]any{"id": a.profileID}))
	a.endpoint = text(launched["cdpEndpoint"])
	if number(launched["pid"]) <= 0 || !strings.HasPrefix(a.endpoint, "http://127.0.0.1:") {
		t.Fatalf("invalid managed launch: %v", launched)
	}
	attached := reverseObject(t, h.invoke(t, "debugger_attach", map[string]any{"profileId": a.profileID}))
	a.id, a.allowedRoot = text(attached["debugSessionId"]), text(attached["allowedRoot"])
	if a.id == "" || attached["profileId"] != a.profileID || attached["status"] != "attached" {
		t.Fatalf("invalid attached session: %v", attached)
	}
	reverseAssertWithin(t, h.dataDir, a.allowedRoot)
	a.tool(t, "navigate_page", map[string]any{"type": "url", "url": h.url + "/?profile=" + name, "timeout": 10000})
	pages := a.tool(t, "select_page", nil)
	page := reverseFindURL(t, pages["pages"], h.url+"/?profile="+name)
	selection := a.tool(t, "select_page", map[string]any{"pageIdx": page["pageIdx"]})
	if reverseFindURL(t, selection["pages"], h.url+"/?profile="+name)["selected"] != true {
		t.Fatalf("page not selected: %v", selection)
	}
	a.wait(t, "evaluate_script", map[string]any{"function": "() => window.reverseFixture?.ready === true && document.querySelector('iframe').contentWindow.reverseFrameMarker === 'reverse-child-context'", "confirm": true, "mainWorld": true}, func(data map[string]any) bool { return data["value"] == true })
	return a
}

func (a *reverseSession) windowTabs(t *testing.T) []map[string]any {
	t.Helper()
	result := reverseObject(t, a.h.invoke(t, "list_windows", map[string]any{"debugSessionId": a.id}))
	data := reverseToolData(t, "select_page", result)
	windows := reverseObjects(t, data["windows"])
	if len(windows) == 0 {
		t.Fatal("list_windows omitted actual browser windows")
	}
	var tabs []map[string]any
	seenWindows, seenTargets := make(map[float64]bool), make(map[string]bool)
	for _, window := range windows {
		id, ok := window["windowId"].(float64)
		if !ok || id <= 0 || seenWindows[id] {
			t.Fatalf("invalid native window ID: %v", window)
		}
		seenWindows[id] = true
		for _, tab := range reverseObjects(t, window["tabs"]) {
			targetID := text(tab["targetId"])
			if targetID == "" || seenTargets[targetID] || text(tab["url"]) == "" {
				t.Fatalf("invalid window/target mapping: %v", tab)
			}
			seenTargets[targetID] = true
			// Cross-check the native mapping using the managed browser connection.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			actual, err := a.h.service.browser.Call(ctx, a.profileID, "Browser.getWindowForTarget", map[string]any{"targetId": targetID})
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			if reverseObject(t, actual)["windowId"] != id {
				t.Fatalf("incorrect native window for target %s: %v", targetID, window)
			}
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

func (a *reverseSession) call(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	value, err := a.h.service.Invoke(ctx, "debugger_call", map[string]any{"debugSessionId": a.id, "name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s returned %T, not an MCP result", name, value)
	}
	return result, nil
}

func (a *reverseSession) tool(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := a.call(ctx, name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return reverseToolData(t, name, result)
}

func (a *reverseSession) wait(t *testing.T, name string, args map[string]any, ready func(map[string]any) bool) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var last map[string]any
	for {
		result, err := a.call(ctx, name, args)
		if err != nil {
			t.Fatalf("waiting for %s: %v; last result: %v", name, err, last)
		}
		last = result
		// A breakpoint's timer may not have fired at the first poll.
		if name == "get_paused_info" && result["isError"] == true && strings.Contains(reverseJSON(t, result), "Execution is not paused") {
		} else if data := reverseToolData(t, name, result); ready(data) {
			return data
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s: %v", name, last)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (a *reverseSession) evaluate(t *testing.T, function string) any {
	t.Helper()
	return a.tool(t, "evaluate_script", map[string]any{"function": function, "confirm": true, "mainWorld": true})["value"]
}

func (a *reverseSession) scheduleXHR(t *testing.T, token string) {
	t.Helper()
	if value := a.evaluate(t, fmt.Sprintf("() => window.reverseFixture.scheduleXHR(%q)", token)); value != token {
		t.Fatalf("XHR not scheduled: %v", value)
	}
}

func (a *reverseSession) waitHTTP(t *testing.T, token string) {
	t.Helper()
	data := a.wait(t, "evaluate_script", map[string]any{"function": fmt.Sprintf("() => window.reverseFixture.http[%q]", token), "confirm": true, "mainWorld": true}, func(data map[string]any) bool {
		return reverseObject(t, data["value"])["state"] == "done"
	})
	value := reverseObject(t, data["value"])
	body := reverseObject(t, value["body"])
	if number(value["status"]) != 200 || body["token"] != token || body["echo"] != reversePayload(token) {
		t.Fatalf("XHR response mismatch: %v", data)
	}
}

func (a *reverseSession) request(t *testing.T, token string) map[string]any {
	t.Helper()
	data := a.wait(t, "list_network_requests", map[string]any{"urlFilter": "/api/echo?token=" + token}, func(data map[string]any) bool { return len(reverseObjects(t, data["requests"])) == 1 })
	request := reverseObjects(t, data["requests"])[0]
	if request["url"] != a.h.url+"/api/echo?token="+token {
		t.Fatalf("wrong request captured: %v", request)
	}
	if _, ok := request["reqid"].(float64); !ok {
		t.Fatalf("missing numeric reqid: %v", request)
	}
	return request
}

func (h *reverseHarness) assertRequest(t *testing.T, token string) {
	t.Helper()
	h.mu.Lock()
	body, ok := h.requests[token]
	h.mu.Unlock()
	if !ok || body != reversePayload(token) {
		t.Fatalf("server did not receive the exact request: %q", body)
	}
}

func reversePayload(token string) string {
	return fmt.Sprintf(`{"token":%q,"message":"reverse-body-你好"}`, token)
}

func reverseToolData(t *testing.T, name string, result map[string]any) map[string]any {
	t.Helper()
	if result["isError"] == true {
		t.Fatalf("%s MCP error: %s", name, reverseJSON(t, result))
	}
	if _, ok := result["content"].([]any); !ok {
		t.Fatalf("%s omitted MCP content: %v", name, result)
	}
	structured := reverseObject(t, result["structuredContent"])
	if structured["ok"] != true || structured["tool"] != name {
		t.Fatalf("%s invalid structured result: %v", name, result)
	}
	return reverseObject(t, structured["data"])
}

func reverseObject(t *testing.T, value any) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(reverseJSON(t, value)), &result); err != nil || result == nil {
		t.Fatalf("expected object, got %v: %v", value, err)
	}
	return result
}

func reverseObjects(t *testing.T, value any) []map[string]any {
	t.Helper()
	var result []map[string]any
	if err := json.Unmarshal([]byte(reverseJSON(t, value)), &result); err != nil || result == nil {
		t.Fatalf("expected object list, got %v: %v", value, err)
	}
	return result
}

func reverseJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func reverseFindURL(t *testing.T, value any, url string) map[string]any {
	t.Helper()
	for _, item := range reverseObjects(t, value) {
		if item["url"] == url {
			return item
		}
	}
	t.Fatalf("URL %q missing from %v", url, value)
	return nil
}

func reverseAssertWithin(t *testing.T, root, path string) {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if path == "" || err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("test path %q is not confined below %q: %v", path, root, err)
	}
}
