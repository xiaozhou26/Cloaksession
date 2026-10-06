package extensions

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const testManifest = `{"manifest_version":3,"name":"Test extension","version":"1.2.3","icons":{"16":"small.png","128":"icon.svg"}}`
const testWebID = "abcdefghijklmnopabcdefghijklmnop"

type diskStore struct {
	path string
	mu   sync.Mutex
}

func newDiskStore(t *testing.T) *diskStore {
	t.Helper()
	s := &diskStore{path: filepath.Join(t.TempDir(), "profiles.json")}
	data := `{"one":{"id":"one","name":"First","extensions":null},"two":{"id":"two","name":"Second","extensions":[]}}`
	if err := os.WriteFile(s.path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *diskStore) read() (map[string]map[string]any, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var profiles map[string]map[string]any
	err = json.Unmarshal(data, &profiles)
	return profiles, err
}

func (s *diskStore) ProfileGet(id string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles, err := s.read()
	if err != nil {
		return nil, err
	}
	profile, ok := profiles[id]
	if !ok {
		return nil, errors.New("profile not found")
	}
	return profile, nil
}

func (s *diskStore) ProfileUpdate(id string, patch map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles, err := s.read()
	if err != nil {
		return nil, err
	}
	profile, ok := profiles[id]
	if !ok {
		return nil, errors.New("profile not found")
	}
	for key, value := range patch {
		profile[key] = value
	}
	data, err := json.Marshal(profiles)
	if err != nil {
		return nil, err
	}
	return profile, os.WriteFile(s.path, data, 0600)
}

func (s *diskStore) ProfilesList() ([]map[string]any, error) {
	// The production list may contain summaries without extensions.
	return []map[string]any{{"id": "one"}, {"id": "two"}}, nil
}

func putFile(t *testing.T, root, name, content string) string {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}

type zipEntry struct {
	name, content string
	mode          os.FileMode
}

func makeZIP(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, entry.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func wrapCRX(payload []byte, version uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("Cr24")
	binary.Write(&buf, binary.LittleEndian, version)
	if version == 2 {
		binary.Write(&buf, binary.LittleEndian, uint32(8))
		binary.Write(&buf, binary.LittleEndian, uint32(3))
		buf.Write([]byte{'P', 'K', 3, 4, 0, 0, 0, 0, 1, 2, 3})
	} else {
		binary.Write(&buf, binary.LittleEndian, uint32(8))
		buf.Write([]byte{'P', 'K', 3, 4, 0, 0, 0, 0})
	}
	buf.Write(payload)
	return buf.Bytes()
}

func TestPrepareZIPAndCRX(t *testing.T) {
	zipData := makeZIP(t, zipEntry{"manifest.json", testManifest, 0}, zipEntry{"icon.svg", "<svg/>", 0})
	for _, version := range []uint32{0, 2, 3} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			data := zipData
			if version != 0 {
				data = wrapCRX(zipData, version)
			}
			root := t.TempDir()
			file := putFile(t, root, "input.crx", string(data))
			m := New(root, nil)
			ext, err := m.PrepareFromFile(file)
			if err != nil {
				t.Fatal(err)
			}
			id := shortHash(data)
			want := map[string]any{"id": id, "name": "Test extension", "version": "1.2.3", "scope": "shared", "enabled": true, "source": "file", "dir": filepath.Join(root, "extensions", id)}
			if !reflect.DeepEqual(ext, want) {
				t.Fatalf("got %#v; want %#v", ext, want)
			}
			if _, err := os.Stat(filepath.Join(text(ext, "dir"), "manifest.json")); err != nil {
				t.Fatal(err)
			}
			putFile(t, text(ext, "dir"), "cached-marker", "keep")
			again, err := New(root, nil).PrepareFromFile(file)
			if err != nil || !reflect.DeepEqual(again, ext) {
				t.Fatalf("cache reuse: %v, %v", again, err)
			}
			if _, err := os.Stat(filepath.Join(text(ext, "dir"), "cached-marker")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestArchivePathSafety(t *testing.T) {
	entries := []zipEntry{
		{"../escape", "bad", 0}, {"safe/../../escape", "bad", 0}, {"/absolute", "bad", 0},
		{`..\escape`, "bad", 0}, {`C:\escape`, "bad", 0}, {`safe\..\escape`, "bad", 0},
		{"link", "../escape", os.ModeSymlink | 0777}, {"pipe", "", os.ModeNamedPipe | 0600},
	}
	for _, entry := range entries {
		t.Run(entry.name, func(t *testing.T) {
			root := t.TempDir()
			data := makeZIP(t, zipEntry{"manifest.json", testManifest, 0}, entry)
			m := New(root, nil)
			if _, err := m.PrepareFromFile(putFile(t, root, "unsafe.zip", string(data))); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			files, err := os.ReadDir(m.root)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Fatalf("partial extraction remains: %v", files)
			}
			if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
				t.Fatal("archive escaped extraction root")
			}
		})
	}
}

func TestInvalidArchivesAndManifests(t *testing.T) {
	badCRX := wrapCRX(makeZIP(t, zipEntry{"manifest.json", testManifest, 0}), 3)
	binary.LittleEndian.PutUint32(badCRX[8:12], ^uint32(0))
	cases := [][]byte{
		[]byte("Cr24"), []byte("not a zip"), badCRX,
		makeZIP(t, zipEntry{"other.json", "{}", 0}),
		makeZIP(t, zipEntry{"manifest.json", "{", 0}),
		makeZIP(t, zipEntry{"manifest.json", "null", 0}),
		makeZIP(t, zipEntry{"manifest.json", testManifest, 0}, zipEntry{"manifest.json", testManifest, 0}),
	}
	for i, data := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			root := t.TempDir()
			if _, err := New(root, nil).PrepareFromFile(putFile(t, root, "bad.zip", string(data))); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
}

func TestFolderLabelsAndLegacyIDs(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "unpacked")
	putFile(t, folder, "manifest.json", `{"name":"__MSG_AppName__","version":"2","default_locale":"fr","key":"AbC+/KEY="}`)
	putFile(t, folder, "_locales/fr/messages.json", `{"appname":{"message":"Nom localisé"}}`)
	m := New(root, nil)
	ext, err := m.PrepareFromFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	if text(ext, "id") != "abc+/key=" || text(ext, "name") != "Nom localisé" || text(ext, "dir") != folder || text(ext, "source") != "folder" {
		t.Fatalf("unexpected folder config: %v", ext)
	}
	putFile(t, folder, "manifest.json", `{"name":"__MSG_unknown__"}`)
	ext, err = m.PrepareFromFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	if text(ext, "id") != shortHash([]byte(folder)) || ext["name"] != ext["id"] {
		t.Fatalf("unexpected fallback: %v", ext)
	}
	putFile(t, folder, "_locales/en/messages.json", `{"unknown":{"message":"English fallback"}}`)
	ext, err = m.PrepareFromFolder(folder)
	if err != nil || ext["name"] != "English fallback" {
		t.Fatalf("locale fallback: %v %v", ext, err)
	}
	if ext, err := m.PrepareFromFolder(""); ext != nil || err != nil {
		t.Fatal("cancelled folder selection must return nil")
	}
	if ext, err := m.PrepareFromFile(""); ext != nil || err != nil {
		t.Fatal("cancelled file selection must return nil")
	}
	if _, err := m.PrepareFromFolder(t.TempDir()); err == nil {
		t.Fatal("folder without manifest accepted")
	}
}

func TestIconsAndResourceSafety(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "extension")
	putFile(t, folder, "manifest.json", testManifest)
	putFile(t, folder, "icon.svg", "<svg/>")
	putFile(t, folder, "small.png", "small")
	m := New(root, nil)
	ext := map[string]any{"dir": folder}
	icon, err := m.Icon(ext, nil)
	if err != nil || icon != "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString([]byte("<svg/>")) {
		t.Fatalf("unexpected icon: %v %v", icon, err)
	}
	putFile(t, folder, "manifest.json", `{"action":{"default_icon":"small.png"}}`)
	icon, err = m.Icon(ext, nil)
	if err != nil || icon != "data:image/png;base64,c21hbGw=" {
		t.Fatalf("action icon: %v %v", icon, err)
	}
	outside := putFile(t, root, "secret.png", "private")
	if err := os.Symlink(outside, filepath.Join(folder, "linked.png")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../secret.png", outside, "linked.png", `..\secret.png`} {
		manifest, _ := json.Marshal(map[string]any{"icons": map[string]any{"128": name}})
		putFile(t, folder, "manifest.json", string(manifest))
		if icon, err := m.Icon(ext, nil); err != nil || icon != nil {
			t.Fatalf("unsafe icon %s: %v %v", name, icon, err)
		}
	}
	if icon, err := m.Icon(map[string]any{}, nil); err != nil || icon != nil {
		t.Fatal("missing icon must be null")
	}
}

func TestProfilePersistenceAndSharedReferences(t *testing.T) {
	root := t.TempDir()
	store := newDiskStore(t)
	m := New(root, store)
	data := makeZIP(t, zipEntry{"manifest.json", testManifest, 0})
	ext, err := m.PrepareFromFile(putFile(t, root, "source.zip", string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := m.StoreEntries(); err != nil || len(entries) != 0 {
		t.Fatal("prepared but unreferenced extension should not be listed")
	}
	for _, profile := range []string{"one", "two"} {
		if _, err := m.Add(profile, ext); err != nil {
			t.Fatal(err)
		}
	}
	if exts, err := m.Add("one", ext); err != nil || len(exts) != 1 {
		t.Fatalf("upsert: %v %v", exts, err)
	}
	if entries, err := m.StoreEntries(); err != nil || len(entries) != 1 {
		t.Fatalf("store dedup: %v %v", entries, err)
	}
	if _, err := m.Toggle("one", text(ext, "id"), false); err != nil {
		t.Fatal(err)
	}
	m = New(root, &diskStore{path: store.path})
	one, err := m.List("one")
	if err != nil || one[0]["enabled"] != false {
		t.Fatalf("persisted disabled flag: %v %v", one, err)
	}
	two, err := m.List("two")
	if err != nil || two[0]["enabled"] != true {
		t.Fatalf("other profile changed: %v %v", two, err)
	}
	profile, _ := store.ProfileGet("one")
	if profile["name"] != "First" {
		t.Fatal("unrelated profile field changed")
	}
	if list, err := m.Remove("one", text(ext, "id")); err != nil || len(list) != 0 || list == nil {
		t.Fatalf("remove: %v %v", list, err)
	}
	if _, err := os.Stat(text(ext, "dir")); err != nil {
		t.Fatal("removal deleted shared files")
	}
	if _, err := m.List("missing"); err == nil {
		t.Fatal("store error swallowed")
	}
}

func TestConcurrentProfileUpdates(t *testing.T) {
	m := New(t.TempDir(), newDiskStore(t))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := m.Add("one", map[string]any{"id": fmt.Sprint(i), "dir": "/legacy/extension"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	exts, err := m.List("one")
	if err != nil || len(exts) != 16 {
		t.Fatalf("lost update: %d %v", len(exts), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestWebStoreDownloadAndCache(t *testing.T) {
	root := t.TempDir()
	m := New(root, nil)
	calls := 0
	data := wrapCRX(makeZIP(t, zipEntry{"manifest.json", testManifest, 0}), 3)
	m.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "clients2.google.com" || req.URL.Query().Get("x") != "id="+testWebID+"&installsource=ondemand&uc" || req.URL.Query().Get("acceptformat") != "crx2,crx3" {
			t.Fatalf("wrong download request: %s", req.URL)
		}
		if req.Header.Get("Authorization") != "" || req.Header.Get("User-Agent") == "" {
			t.Fatal("unexpected download headers")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})
	for _, input := range []string{testWebID, "https://chromewebstore.google.com/detail/test/" + testWebID + "?hl=en#x", "https://chrome.google.com/webstore/detail/test/" + testWebID} {
		ext, err := m.PrepareFromWebStore(context.Background(), input)
		if err != nil || ext["id"] != testWebID || ext["source"] != "web-store" {
			t.Fatalf("web store: %v %v", ext, err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected cache reuse; downloads=%d", calls)
	}
	for _, input := range []string{"", "../bad", strings.Repeat("/", 32), "https://chromewebstore.google.com/detail/short"} {
		if _, err := m.PrepareFromWebStore(context.Background(), input); err == nil {
			t.Fatalf("invalid ID accepted: %q", input)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.PrepareFromWebStore(ctx, testWebID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
}

func TestWebStoreFailures(t *testing.T) {
	for _, status := range []int{204, 404, 500, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			m := New(t.TempDir(), nil)
			m.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})
			if _, err := m.PrepareFromWebStore(context.Background(), testWebID); err == nil {
				t.Fatal("failed download accepted")
			}
		})
	}
}

func TestCompanionInstallAndEvents(t *testing.T) {
	root := t.TempDir()
	putFile(t, filepath.Join(root, "extensions", testWebID), "manifest.json", testManifest)
	m := New(root, newDiskStore(t))
	var event map[string]any
	err := m.PollCompanion(context.Background(), "one", func(script string) (any, error) {
		if !strings.Contains(script, "location.hostname") || !strings.Contains(script, "removeAttribute") {
			t.Fatal("poll must restrict origin and consume signal")
		}
		return map[string]any{"result": map[string]any{"type": "string", "value": `{"id":"` + testWebID + `","n":1}`}}, nil
	}, func(name string, value any) {
		if name != "extensions:installed" {
			t.Fatalf("unexpected event %s", name)
		}
		event = value.(map[string]any)
	})
	if err != nil || event["ok"] != true || event["profileId"] != "one" || event["profile_id"] != nil {
		t.Fatalf("install event: %v %v", event, err)
	}
	if list, err := m.List("one"); err != nil || len(list) != 1 {
		t.Fatalf("install persistence: %v %v", list, err)
	}
	for _, payload := range []any{"bad json", `{}`, `{"id":"../../bad"}`, []any{1}} {
		err := m.PollCompanion(context.Background(), "one", func(string) (any, error) { return payload, nil }, func(_ string, value any) { event = value.(map[string]any) })
		if err == nil || event["ok"] != false || event["error"] == "" || event["profileId"] != "one" || event["profile_id"] != nil {
			t.Fatalf("invalid signal: %v %v", event, err)
		}
	}
}

func TestCompanionCancellationAndReload(t *testing.T) {
	root := t.TempDir()
	putFile(t, filepath.Join(root, "extensions", testWebID), "manifest.json", testManifest)
	m := New(root, newDiskStore(t))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.PollCompanion(ctx, "one", func(string) (any, error) { return nil, errors.New("navigating") }, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	events := make(chan map[string]any, 2)
	stop := m.StartCompanionPoller(context.Background(), "one", func(string) (any, error) { return `{"id":"` + testWebID + `"}`, nil }, func(_ string, value any) { events <- value.(map[string]any) }, func(_ context.Context, id string) error {
		if id != "one" {
			t.Errorf("wrong reload profile: %s", id)
		}
		return errors.New("reload failed")
	})
	defer stop()
	for _, ok := range []bool{true, false} {
		select {
		case event := <-events:
			if event["ok"] != ok || event["profileId"] != "one" || event["profile_id"] != nil {
				t.Fatalf("reload event ordering: %v", event)
			}
		case <-time.After(time.Second):
			t.Fatal("missing companion event")
		}
	}
}

func TestCompanionResumesAfterReload(t *testing.T) {
	root := t.TempDir()
	putFile(t, filepath.Join(root, "extensions", testWebID), "manifest.json", testManifest)
	m := New(root, newDiskStore(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resumed := make(chan struct{})
	var evaluations, reloads int
	stop := m.StartCompanionPoller(ctx, "one", func(string) (any, error) {
		evaluations++
		if evaluations == 1 {
			return `{"id":"` + testWebID + `"}`, nil
		}
		if reloads != 1 {
			t.Errorf("poll resumed before reload: %d", reloads)
		}
		cancel()
		close(resumed)
		return map[string]any{"result": map[string]any{"type": "object", "subtype": "null", "value": nil}}, nil
	}, nil, func(context.Context, string) error {
		reloads++
		return nil
	})
	defer stop()
	select {
	case <-resumed:
	case <-time.After(time.Second):
		t.Fatal("poll did not resume after reload")
	}
}

func TestRejectSymlinkedCacheAndManifest(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	putFile(t, outside, "manifest.json", testManifest)
	if err := os.MkdirAll(filepath.Join(root, "extensions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "extensions", testWebID)); err != nil {
		t.Fatal(err)
	}
	m := New(root, nil)
	if _, err := m.PrepareFromWebStore(context.Background(), testWebID); err == nil {
		t.Fatal("symlinked cache directory accepted")
	}
	folder := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "manifest.json"), filepath.Join(folder, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareFromFolder(folder); err == nil {
		t.Fatal("manifest outside extension directory accepted")
	}
}

func TestSweepOrphans(t *testing.T) {
	root := t.TempDir()
	store := newDiskStore(t)
	m := New(root, store)
	for _, name := range []string{"enabled", "disabled", "other-profile", "orphan", "old.tmp_unpacked", ".unpacking-interrupted", "external-name"} {
		putFile(t, m.root, name+"/manifest.json", testManifest)
	}
	for _, ext := range []map[string]any{
		{"id": "enabled", "dir": filepath.Join(m.root, "enabled"), "enabled": true},
		{"id": "disabled", "dir": filepath.Join(m.root, "disabled"), "enabled": false},
		{"id": "temp", "dir": filepath.Join(m.root, "old.tmp_unpacked")},
		{"id": "external", "dir": filepath.Join(t.TempDir(), "external-name"), "scope": "profile"},
	} {
		if _, err := m.Add("one", ext); err != nil {
			t.Fatal(err)
		}
	}
	// Different directories with the same ID must both remain referenced.
	if _, err := m.Add("two", map[string]any{"id": "enabled", "dir": filepath.Join(m.root, "other-profile")}); err != nil {
		t.Fatal(err)
	}
	putFile(t, m.root, "keep.txt", "not a directory")
	putFile(t, m.root, "file.tmp_unpacked", "not a directory")
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if err := m.SweepOrphans(); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"enabled", "disabled", "other-profile", "external-name", "keep.txt", "file.tmp_unpacked"} {
		if _, err := os.Stat(filepath.Join(m.root, name)); err != nil {
			t.Errorf("referenced directory or regular file %s removed: %v", name, err)
		}
	}
	for _, name := range []string{"orphan", "old.tmp_unpacked", ".unpacking-interrupted"} {
		if _, err := os.Lstat(filepath.Join(m.root, name)); !os.IsNotExist(err) {
			t.Errorf("orphan %s remains: %v", name, err)
		}
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("sweep changed profile data: %v", err)
	}
}

func TestSweepOrphansPathSafety(t *testing.T) {
	root := t.TempDir()
	m := New(root, newDiskStore(t))
	outside := t.TempDir()
	marker := putFile(t, outside, "precious/keep.txt", "keep")
	putFile(t, m.root, "orphan/manifest.json", testManifest)
	for _, link := range []string{"outside-link", "link.tmp_unpacked", "orphan/nested-link"} {
		if err := os.Symlink(outside, filepath.Join(m.root, link)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Add("one", map[string]any{"id": "../../precious", "dir": filepath.Join(m.root, "..", "..", "precious")}); err != nil {
		t.Fatal(err)
	}
	if err := m.SweepOrphans(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("sweep modified outside target: %q %v", data, err)
	}
	for _, link := range []string{"outside-link", "link.tmp_unpacked"} {
		if _, err := os.Lstat(filepath.Join(m.root, link)); err != nil {
			t.Fatalf("symlink entry should be left untouched: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(m.root, "orphan")); !os.IsNotExist(err) {
		t.Fatalf("orphan containing nested symlink remains: %v", err)
	}
	linked := New(t.TempDir(), newDiskStore(t))
	if err := os.Symlink(outside, linked.root); err != nil {
		t.Fatal(err)
	}
	if err := linked.SweepOrphans(); err == nil {
		t.Fatal("symlinked extensions root accepted")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("symlinked-root sweep removed external directory: %v", err)
	}
}

type failingSweepStore struct {
	Store
	listErr error
	getErr  error
}

func (s failingSweepStore) ProfilesList() ([]map[string]any, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.Store.ProfilesList()
}

func (s failingSweepStore) ProfileGet(id string) (map[string]any, error) {
	if id == "two" && s.getErr != nil {
		return nil, s.getErr
	}
	return s.Store.ProfileGet(id)
}

func TestSweepOrphansMissingRootAndStoreFailures(t *testing.T) {
	if err := New(t.TempDir(), nil).SweepOrphans(); err != nil {
		t.Fatalf("missing root should be a no-op: %v", err)
	}
	failure := errors.New("store failed")
	base := newDiskStore(t)
	for _, store := range []Store{nil, failingSweepStore{Store: base, listErr: failure}, failingSweepStore{Store: base, getErr: failure}} {
		m := New(t.TempDir(), store)
		marker := putFile(t, m.root, "orphan/keep.txt", "keep")
		err := m.SweepOrphans()
		if err == nil || (store != nil && !errors.Is(err, failure)) {
			t.Fatalf("store failure was swallowed: %v", err)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("sweep removed data after failing to collect references: %v", err)
		}
	}
}
