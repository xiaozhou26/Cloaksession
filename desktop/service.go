package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xiaozhou26/Cloaksession/desktop/internal/browser"
	"github.com/xiaozhou26/Cloaksession/desktop/internal/extensions"
	"github.com/xiaozhou26/Cloaksession/desktop/internal/mcp"
	"github.com/xiaozhou26/Cloaksession/desktop/internal/store"
)

const appVersion = "1.4.0"

//go:embed resources/companion/*
var companionAssets embed.FS

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

type Service struct {
	store        *store.Store
	browser      *browser.Manager
	extensions   *extensions.Manager
	mcp          *mcp.Server
	update       *updater
	ctx          context.Context
	cancel       context.CancelFunc
	emit         func(string, any)
	dialog       func(dialogRequest) (string, error)
	openURL      func(string) error
	dataDir      string
	companionDir string
	token        string
	mcpURL       string
	settings     map[string]any
	closeOnce    sync.Once
	lifecycleMu  sync.Mutex
	closing      bool
	active       sync.WaitGroup
	background   sync.WaitGroup
	companionMu  sync.Mutex
	companions   map[string]context.CancelFunc
	profileMu    sync.Mutex
	profileLocks map[string]*sync.Mutex
	mcpError     string
}

func newService(dataDir, resourceDir string, dialog func(dialogRequest) (string, error), emit func(string, any), openURL func(string) error) (*Service, error) {
	if emit == nil {
		emit = func(string, any) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{ctx: ctx, cancel: cancel, dataDir: dataDir, dialog: dialog, emit: emit, openURL: openURL, companions: make(map[string]context.CancelFunc), profileLocks: make(map[string]*sync.Mutex)}
	var err error
	s.store, err = store.Open(dataDir)
	if err != nil {
		cancel()
		return nil, err
	}
	s.settings, err = s.store.SettingsGet()
	if err != nil {
		s.Close()
		return nil, err
	}
	s.companionDir = filepath.Join(dataDir, "companion")
	if err = os.MkdirAll(s.companionDir, 0700); err != nil {
		s.Close()
		return nil, err
	}
	for _, name := range []string{"manifest.json", "cs.js"} {
		data, readErr := companionAssets.ReadFile("resources/companion/" + name)
		if readErr != nil {
			s.Close()
			return nil, readErr
		}
		if err = os.WriteFile(filepath.Join(s.companionDir, name), data, 0600); err != nil {
			s.Close()
			return nil, err
		}
	}
	s.browser = browser.New(resourceDir, emit)
	s.extensions = extensions.New(dataDir, s.store)
	if err := s.extensions.SweepOrphans(); err != nil {
		fmt.Fprintf(os.Stderr, "extension cleanup: %v\n", err)
	}
	s.token, err = mcp.LoadOrCreateToken(dataDir)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.mcp = mcp.New(s, s.token, emit)
	if enabled, _ := s.settings["mcpHttpEnabled"].(bool); enabled {
		port := int(number(s.settings["mcpHttpPort"]))
		if port == 0 {
			port = 7777
		}
		if err = s.mcp.Start(port); err != nil {
			s.mcpError = fmt.Sprintf("MCP port %d is unavailable: %v", port, err)
		} else {
			s.mcpURL = fmt.Sprintf("http://127.0.0.1:%d", port)
		}
	}
	s.update = newUpdater(emit, openURL)
	if automatic, _ := s.settings["autoUpdate"].(bool); automatic {
		s.background.Add(1)
		go func() {
			defer s.background.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(8 * time.Second):
				_, _ = s.update.check(ctx)
			}
		}()
	}
	s.background.Add(1)
	go func() { defer s.background.Done(); s.backfillProxyCountries() }()
	return s, nil
}

func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.closing = true
		s.lifecycleMu.Unlock()
		s.cancel()
		if s.mcp != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.mcp.Close(ctx)
			cancel()
		}
		if s.browser != nil {
			s.browser.Shutdown()
		}
		s.active.Wait()
		s.background.Wait()
		if s.store != nil {
			_ = s.store.Close()
		}
	})
}

func (s *Service) Invoke(ctx context.Context, command string, args map[string]any) (any, error) {
	s.lifecycleMu.Lock()
	if s.closing {
		s.lifecycleMu.Unlock()
		return nil, errors.New("Cloaksession is shutting down")
	}
	s.active.Add(1)
	s.lifecycleMu.Unlock()
	defer s.active.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	if args == nil {
		args = map[string]any{}
	}
	select {
	case <-s.ctx.Done():
		return nil, errors.New("Cloaksession is shutting down")
	default:
	}
	if command == "profiles_delete" || command == "profiles_close" {
		s.stopCompanion(text(args["id"]))
	}
	switch command {
	case "profiles_delete", "profiles_launch", "profiles_close", "profiles_export_archive":
		unlock := s.lockProfile(text(args["id"]))
		defer unlock()
	}
	switch command {
	case "profiles_list":
		profiles, err := s.store.ProfilesList()
		if err != nil {
			return nil, err
		}
		for _, p := range profiles {
			p["isRunning"] = s.browser.IsRunning(text(p["id"]))
		}
		return profiles, nil
	case "profiles_get":
		return s.store.ProfileGet(text(args["id"]))
	case "profiles_create":
		input, err := objectArg(args, "input")
		if err != nil {
			return nil, err
		}
		return s.store.ProfileCreate(input)
	case "profiles_update":
		patch, err := objectArg(args, "patch")
		if err != nil {
			return nil, err
		}
		return s.store.ProfileUpdate(text(args["id"]), patch)
	case "profiles_delete":
		id := text(args["id"])
		s.stopCompanion(id)
		if err := s.browser.CloseProfile(id); err != nil {
			return nil, err
		}
		return nil, s.store.ProfileDelete(id)
	case "profiles_launch":
		id := text(args["id"])
		profile, err := s.store.ProfileGet(id)
		if err != nil {
			return nil, err
		}
		if profile == nil {
			return nil, fmt.Errorf("profile not found: %s", id)
		}
		result, err := s.browser.Launch(profile, s.settings, s.companionDir)
		if err == nil {
			err = s.store.MarkOpened(id)
			s.startCompanion(id)
		}
		return result, err
	case "profiles_close":
		s.stopCompanion(text(args["id"]))
		return nil, s.browser.CloseProfile(text(args["id"]))
	case "settings_get":
		return s.store.SettingsGet()
	case "settings_update":
		patch, err := objectArg(args, "patch")
		if err != nil {
			return nil, err
		}
		return s.store.SettingsUpdate(patch)
	case "fingerprint_generate":
		return store.FingerprintGenerate(text(args["seed"])), nil
	case "fingerprint_devices":
		return store.FingerprintDevices(), nil
	case "fingerprint_locales":
		return store.FingerprintLocales(), nil
	case "fingerprint_reconcile":
		current, err := objectArg(args, "fingerprint")
		if err != nil {
			return nil, err
		}
		patch, err := objectArg(args, "patch")
		if err != nil {
			return nil, err
		}
		return store.FingerprintReconcile(current, patch)
	case "fingerprint_locale_for_country":
		return store.FingerprintLocaleForCountry(text(args["country"])), nil
	case "dialog_pick_browser_binary":
		return s.pick(dialogRequest{Kind: "open", Title: "Choose browser executable"})
	case "dialog_pick_directory":
		return s.pick(dialogRequest{Kind: "directory", Title: "Choose directory"})
	case "system_info":
		platform := runtime.GOOS
		if platform == "darwin" {
			platform = "macos"
		}
		return map[string]any{"appVersion": appVersion, "platform": platform, "mcpAuthToken": s.token, "mcpHttpUrl": s.mcpURL, "mcpError": s.mcpError}, nil
	case "activity_recent":
		limit := int(number(args["limit"]))
		if limit <= 0 {
			limit = 100
		}
		return s.mcp.Recent(limit), nil
	case "proxy_detect_geo":
		proxy, err := objectArg(args, "proxy")
		if err != nil {
			return nil, err
		}
		return detectProxyGeo(ctx, proxy)
	case "profiles_export_archive":
		return s.exportArchive(args)
	case "profiles_import_archive":
		return s.importArchive(args)
	case "extensions_list":
		return s.extensions.List(text(args["profileId"]))
	case "extensions_store_entries":
		return s.extensions.StoreEntries()
	case "extensions_remove":
		return s.extensions.Remove(text(args["profileId"]), text(args["extId"]))
	case "extensions_toggle":
		enabled, ok := args["enabled"].(bool)
		if !ok {
			return nil, errors.New("enabled must be a boolean")
		}
		return s.extensions.Toggle(text(args["profileId"]), text(args["extId"]), enabled)
	case "extensions_icon":
		ext, err := objectArg(args, "ext")
		if err != nil {
			return nil, err
		}
		var id *string
		if value, ok := args["profileId"].(string); ok {
			id = &value
		}
		return s.extensions.Icon(ext, id)
	case "extensions_prepare_from_web_store", "extensions_add_from_web_store", "extensions_prepare_from_file", "extensions_add_from_file", "extensions_prepare_from_folder", "extensions_add_from_folder":
		return s.prepareExtension(ctx, command, args)
	case "update_status":
		return s.update.status(), nil
	case "update_last_checked":
		return s.update.lastChecked(), nil
	case "update_check":
		return s.update.check(ctx)
	case "update_install":
		return nil, s.update.install()
	case "update_download":
		return nil, s.update.downloadPage(text(args["version"]))
	default:
		return s.browser.Tool(ctx, command, args)
	}
}

func (s *Service) pick(request dialogRequest) (any, error) {
	if s.dialog == nil {
		return nil, errors.New("native dialogs are unavailable")
	}
	type result struct {
		path string
		err  error
	}
	reply := make(chan result, 1)
	go func() { path, err := s.dialog(request); reply <- result{path, err} }()
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case value := <-reply:
		if value.path == "" {
			return nil, value.err
		}
		return value.path, value.err
	}
}

func (s *Service) exportArchive(args map[string]any) (any, error) {
	id, passphrase := text(args["id"]), text(args["passphrase"])
	if len(passphrase) < 8 {
		return nil, errors.New("passphrase must have at least 8 characters")
	}
	if s.browser.IsRunning(id) {
		return nil, errors.New("close the profile before exporting its archive")
	}
	profile, err := s.store.ProfileGet(id)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, errors.New("profile not found")
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, text(profile["name"]))
	picked, err := s.pick(dialogRequest{Kind: "save", Title: "Export profile archive", DefaultFilename: name + ".mzar", Filters: []fileFilter{{"Cloaksession archive", "*.mzar"}}})
	if err != nil {
		return nil, err
	}
	if picked == nil {
		return map[string]any{"ok": false, "reason": "cancelled"}, nil
	}
	path := picked.(string)
	if err = s.store.ExportArchive(id, passphrase, path); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "path": path}, nil
}

func (s *Service) importArchive(args map[string]any) (any, error) {
	picked, err := s.pick(dialogRequest{Kind: "open", Title: "Import profile archive", Filters: []fileFilter{{"Cloaksession archive", "*.mzar"}}})
	if err != nil {
		return nil, err
	}
	if picked == nil {
		return map[string]any{"ok": false, "reason": "cancelled"}, nil
	}
	id, err := s.store.ImportArchive(text(args["passphrase"]), picked.(string))
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id}, nil
}

func (s *Service) prepareExtension(ctx context.Context, command string, args map[string]any) (any, error) {
	var extension map[string]any
	var err error
	switch {
	case strings.HasSuffix(command, "web_store"):
		extension, err = s.extensions.PrepareFromWebStore(ctx, text(args["urlOrId"]))
	default:
		request := dialogRequest{Kind: "open", Title: "Choose extension", Filters: []fileFilter{{"Extension package", "*.crx;*.zip"}}}
		if strings.HasSuffix(command, "folder") {
			request.Kind = "directory"
			request.Filters = nil
		}
		picked, pickErr := s.pick(request)
		if pickErr != nil {
			return nil, pickErr
		}
		if picked == nil {
			if strings.Contains(command, "_add_") {
				return s.extensions.List(text(args["profileId"]))
			}
			return nil, nil
		}
		if request.Kind == "directory" {
			extension, err = s.extensions.PrepareFromFolder(picked.(string))
		} else {
			extension, err = s.extensions.PrepareFromFile(picked.(string))
		}
	}
	if err != nil {
		return nil, err
	}
	if strings.Contains(command, "_add_") {
		return s.extensions.Add(text(args["profileId"]), extension)
	}
	return extension, nil
}

func (s *Service) backfillProxyCountries() {
	profiles, err := s.store.ProfilesList()
	if err != nil {
		return
	}
	for _, profile := range profiles {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		if profile["proxyCountry"] != nil && text(profile["proxyCountry"]) != "" {
			continue
		}
		proxy, ok := profile["proxy"].(map[string]any)
		if !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 6*time.Second)
		geo, err := detectProxyGeo(ctx, proxy)
		cancel()
		if err != nil {
			continue
		}
		id, country := text(profile["id"]), text(geo["country"])
		if s.store.SetProxyCountry(id, country) == nil {
			s.emit("profiles:proxy-country-updated", map[string]any{"id": id, "country": country})
		}
	}
}

func (s *Service) lockProfile(id string) func() {
	s.profileMu.Lock()
	lock := s.profileLocks[id]
	if lock == nil {
		lock = &sync.Mutex{}
		s.profileLocks[id] = lock
	}
	s.profileMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (s *Service) stopCompanion(id string) {
	s.companionMu.Lock()
	if cancel := s.companions[id]; cancel != nil {
		cancel()
		delete(s.companions, id)
	}
	s.companionMu.Unlock()
}

func (s *Service) startCompanion(id string) {
	if s.ctx.Err() != nil {
		return
	}
	s.stopCompanion(id)
	ctx, cancel := context.WithCancel(s.ctx)
	s.companionMu.Lock()
	s.companions[id] = cancel
	s.companionMu.Unlock()
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		defer cancel()
		for ctx.Err() == nil && s.browser.IsRunning(id) {
			err := s.extensions.PollCompanion(ctx, id, func(expression string) (any, error) {
				if !s.browser.IsRunning(id) {
					cancel()
					return nil, errors.New("profile closed")
				}
				return s.evaluateCompanion(ctx, id, expression)
			}, func(event string, data any) {
				if payload, ok := data.(map[string]any); ok {
					if value, exists := payload["profile_id"]; exists {
						payload["profileId"] = value
						delete(payload, "profile_id")
					}
				}
				s.emit(event, data)
			})
			if err != nil || ctx.Err() != nil {
				return
			}
			unlock := s.lockProfile(id)
			if ctx.Err() != nil {
				unlock()
				return
			}
			if err = s.browser.CloseProfile(id); err == nil && ctx.Err() == nil {
				var profile map[string]any
				profile, err = s.store.ProfileGet(id)
				if err == nil && profile != nil && ctx.Err() == nil {
					_, err = s.browser.Launch(profile, s.settings, s.companionDir)
				}
			}
			unlock()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				s.emit("extensions:installed", map[string]any{"ok": false, "profileId": id, "error": "Extension added; relaunch failed: " + err.Error()})
				return
			}
		}
	}()
}

func (s *Service) evaluateCompanion(ctx context.Context, id, expression string) (any, error) {
	result, err := s.browser.Call(ctx, id, "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	payload, _ := result.(map[string]any)
	targets, _ := payload["targetInfos"].([]any)
	for _, entry := range targets {
		target, _ := entry.(map[string]any)
		if text(target["type"]) != "page" || !strings.HasPrefix(text(target["url"]), "https://chromewebstore.google.com/") {
			continue
		}
		attached, err := s.browser.Call(ctx, id, "Target.attachToTarget", map[string]any{"targetId": target["targetId"], "flatten": true})
		if err != nil {
			continue
		}
		attachment, _ := attached.(map[string]any)
		session := text(attachment["sessionId"])
		if session == "" {
			continue
		}
		value, err := s.browser.Tool(ctx, "evaluate_js", map[string]any{"profileId": id, "sessionId": session, "expression": expression})
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _ = s.browser.Call(cleanup, id, "Target.detachFromTarget", map[string]any{"sessionId": session})
		cancel()
		if err != nil {
			continue
		}
		valueMap, _ := value.(map[string]any)
		remote, _ := valueMap["result"].(map[string]any)
		if signal, ok := remote["value"].(string); ok && signal != "" {
			return signal, nil
		}
	}
	return nil, nil
}

func text(value any) string { s, _ := value.(string); return s }
func number(value any) float64 {
	switch v := value.(type) {
	case json.Number:
		n, _ := v.Float64()
		return n
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	}
	return 0
}
func objectArg(args map[string]any, key string) (map[string]any, error) {
	value, ok := args[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return value, nil
}
