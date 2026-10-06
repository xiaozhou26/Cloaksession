package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHeadlessChrome(t *testing.T) {
	binary := os.Getenv("CLOAKSESSION_TEST_BROWSER")
	if binary == "" {
		t.Skip("set CLOAKSESSION_TEST_BROWSER to a local Chromium executable")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Go CDP fixture</title><input id="text"><button id="button" onclick="document.body.dataset.clicked='yes'">Click</button><select id="select"><option value="a">A</option><option value="b">B</option></select><div style="height:3000px">Fixture text</div>`)
	}))
	defer server.Close()
	m := New("", nil)
	defer m.Shutdown()
	profile := object{"id": "test", "dataDir": t.TempDir(), "startUrl": "about:blank", "fingerprint": object{"locale": "en-US", "timezone": "UTC"}}
	settings := object{"browserEngine": "cft", "browserBinaryPath": binary, "headless": true, "browserArgs": []string{"--no-sandbox", "--disable-gpu"}}
	info, e := m.Launch(profile, settings, "")
	if e != nil {
		t.Fatal(e)
	}
	if !m.IsRunning("test") || info["cdpEndpoint"] == "" {
		t.Fatal(info)
	}
	again, e := m.Launch(profile, settings, "")
	if e != nil || info["pid"] != again["pid"] {
		t.Fatal(again, e)
	}
	call := func(name string, args object) object {
		t.Helper()
		args["profileId"] = "test"
		v, e := m.Tool(context.Background(), name, args)
		if e != nil {
			t.Fatalf("%s: %v", name, e)
		}
		return obj(v)
	}
	if r := call("navigate", object{"url": server.URL}); r["url"] != server.URL+"/" {
		t.Fatal(r)
	}
	call("click", object{"selector": "#button"})
	call("type", object{"selector": "#text", "text": "hello世界", "delayMs": 0})
	call("press", object{"selector": "#text", "key": "End"})
	call("hover", object{"selector": "#button"})
	call("select", object{"selector": "#select", "value": "b"})
	r := call("evaluate_js", object{"expression": "({clicked:document.body.dataset.clicked,text:document.querySelector('#text').value,selected:document.querySelector('#select').value})"})
	value := obj(obj(r["result"])["value"])
	if value["clicked"] != "yes" || value["text"] != "hello世界" || value["selected"] != "b" {
		t.Fatal(value)
	}
	call("type", object{"selector": "#text", "text": "replaced", "clear": true, "delayMs": 0})
	r = call("evaluate_js", object{"expression": "document.querySelector('#text').value"})
	if obj(r["result"])["value"] != "replaced" {
		t.Fatal(r)
	}
	if call("wait_for_selector", object{"selector": "#missing", "timeoutMs": 80})["timedOut"] != true {
		t.Fatal("missing timeout")
	}
	call("wait_for_load", object{})
	call("scroll", object{"deltaY": 400})
	call("set_cookies", object{"cookies": []any{object{"name": "go-test", "value": "yes", "url": server.URL}}})
	cookies := call("get_cookies", object{"urls": []any{server.URL}})
	if len(array(cookies["cookies"])) != 1 {
		t.Fatal(cookies)
	}
	screenshot := call("screenshot", object{})
	png, e := base64.StdEncoding.DecodeString(str(screenshot, "data"))
	if e != nil || len(png) < 8 || string(png[1:4]) != "PNG" {
		t.Fatal("invalid screenshot", e)
	}
	extracted := obj(call("extract", object{})["data"])
	if extracted["title"] != "Go CDP fixture" {
		t.Fatal(extracted)
	}
	tabs := call("list_tabs", object{})
	original := ""
	for _, v := range array(tabs["targetInfos"]) {
		target := obj(v)
		if str(target, "type") == "page" {
			original = str(target, "targetId")
			break
		}
	}
	created := call("new_tab", object{"url": "about:blank"})
	id := str(created, "targetId")
	if id == "" {
		t.Fatal(created)
	}
	call("activate_tab", object{"tabId": original})
	call("close_tab", object{"tabId": id})
	call("evaluate_js", object{"expression": "document.title"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = m.Tool(ctx, "evaluate_js", object{"profileId": "test", "expression": "1"}); e == nil {
		t.Fatal("cancellation ignored")
	}
	if e = m.CloseProfile("test"); e != nil {
		t.Fatal(e)
	}
	if m.IsRunning("test") {
		t.Fatal("still running")
	}
	if _, e = os.Stat(filepath.Join(str(profile, "dataDir"), "Default", "Preferences")); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Launch(profile, settings, ""); e != nil {
		t.Fatal(e)
	}
	h, e := m.get("test")
	if e != nil {
		t.Fatal(e)
	}
	killProcess(h.cmd)
	select {
	case <-h.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("external exit not observed")
	}
	if m.IsRunning("test") {
		t.Fatal("external exit still running")
	}
}
