package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App owns the browser core for the lifetime of the desktop window.
type App struct {
	ctx      context.Context
	core     *coreClient
	startErr error
	started  chan struct{}
	dialogMu sync.Mutex
}

func newApp() *App { return &App{started: make(chan struct{})} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	binary, resources, err := corePaths()
	if err != nil {
		a.startErr = err
		close(a.started)
		return
	}
	data, err := dataDirectory()
	if err != nil {
		a.startErr = err
		close(a.started)
		return
	}
	a.core, a.startErr = startCore(binary, data, resources, func(event string, data json.RawMessage) {
		var payload any
		if err := json.Unmarshal(data, &payload); err != nil {
			wailsruntime.LogError(ctx, fmt.Sprintf("decode %s event: %v", event, err))
			return
		}
		wailsruntime.EventsEmit(ctx, event, payload)
	}, a.dialog)
	if a.startErr != nil {
		close(a.started)
		return
	}
	go a.awaitCore()
}

func (a *App) awaitCore() {
	a.startErr = a.core.waitReady()
	close(a.started)
	if a.startErr != nil {
		a.core.close()
	}
}

func (a *App) domReady(ctx context.Context) {
	<-a.started
	if a.startErr != nil {
		_, _ = wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{
			Type:    wailsruntime.ErrorDialog,
			Title:   "Cloaksession could not start",
			Message: a.startErr.Error(),
		})
		return
	}
}

func (a *App) shutdown(context.Context) {
	<-a.started
	if a.core != nil {
		a.core.close()
	}
}

// Invoke forwards the existing typed frontend commands to the Rust core.
func (a *App) Invoke(command string, args map[string]any) (json.RawMessage, error) {
	<-a.started
	if a.startErr != nil {
		return nil, a.startErr
	}
	return a.core.invoke(command, args)
}

func (a *App) dialog(request dialogRequest) (string, error) {
	a.dialogMu.Lock()
	defer a.dialogMu.Unlock()
	filters := make([]wailsruntime.FileFilter, 0, len(request.Filters))
	for _, filter := range request.Filters {
		filters = append(filters, wailsruntime.FileFilter{DisplayName: filter.DisplayName, Pattern: filter.Pattern})
	}
	switch request.Kind {
	case "open":
		return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: request.Title, Filters: filters, TreatPackagesAsDirectories: true})
	case "directory":
		return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: request.Title})
	case "save":
		return wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{Title: request.Title, Filters: filters, DefaultFilename: request.DefaultFilename})
	default:
		return "", fmt.Errorf("unknown native dialog kind: %s", request.Kind)
	}
}

func dataDirectory() (string, error) {
	if override := os.Getenv("CLOAKSESSION_DATA_DIR"); override != "" {
		return override, os.MkdirAll(override, 0700)
	}
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	if base == "" {
		return "", fmt.Errorf("application data directory is unavailable")
	}
	path := filepath.Join(base, "com.cloaksession.browser")
	return path, os.MkdirAll(path, 0700)
}

func corePaths() (string, string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	name := "desktop-core"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Dir(executable)
	binary := os.Getenv("CLOAKSESSION_CORE_BINARY")
	resources := os.Getenv("CLOAKSESSION_RESOURCE_DIR")
	if binary == "" {
		binary = filepath.Join(dir, name)
		if _, err := os.Stat(binary); err != nil {
			// Wails dev runs from the desktop directory or build/bin.
			cwd, _ := os.Getwd()
			for _, root := range []string{cwd, filepath.Join(cwd, ".."), filepath.Join(dir, "..", "..", "..")} {
				candidate := filepath.Join(root, "target", "release", name)
				if _, err := os.Stat(candidate); err == nil {
					binary = candidate
					if resources == "" {
						resources = filepath.Join(root, "crates", "desktop-core", "resources")
					}
					break
				}
			}
		}
	}
	if resources == "" {
		resources = filepath.Join(dir, "resources")
		if runtime.GOOS == "darwin" {
			resources = filepath.Join(dir, "..", "Resources")
		}
	}
	if _, err := os.Stat(binary); err != nil {
		return "", "", fmt.Errorf("browser core unavailable at %s: build with scripts/build.py first: %w", binary, err)
	}
	return binary, resources, nil
}
