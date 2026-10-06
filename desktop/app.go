package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App exposes the Go service directly to the Wails frontend.
type App struct {
	ctx      context.Context
	service  *Service
	startErr error
	started  chan struct{}
	dialogMu sync.Mutex
}

func newApp() *App { return &App{started: make(chan struct{})} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	defer close(a.started)
	data, err := dataDirectory()
	if err != nil {
		a.startErr = err
		return
	}
	resources, err := resourceDirectory()
	if err != nil {
		a.startErr = err
		return
	}
	a.service, a.startErr = newService(data, resources, a.dialog, func(event string, data any) { wailsruntime.EventsEmit(ctx, event, data) }, func(url string) error { wailsruntime.BrowserOpenURL(ctx, url); return nil })
}

func (a *App) domReady(ctx context.Context) {
	<-a.started
	if a.startErr != nil {
		_, _ = wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{Type: wailsruntime.ErrorDialog, Title: "Cloaksession could not start", Message: a.startErr.Error()})
	}
}

func (a *App) shutdown(context.Context) {
	<-a.started
	if a.service != nil {
		a.service.Close()
	}
}

func (a *App) Invoke(command string, args map[string]any) (any, error) {
	<-a.started
	if a.startErr != nil {
		return nil, a.startErr
	}
	return a.service.Invoke(a.ctx, command, args)
}

func (a *App) dialog(request dialogRequest) (string, error) {
	a.dialogMu.Lock()
	defer a.dialogMu.Unlock()
	if a.service != nil {
		select {
		case <-a.service.ctx.Done():
			return "", a.service.ctx.Err()
		default:
		}
	}
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

func resourceDirectory() (string, error) {
	if override := os.Getenv("CLOAKSESSION_RESOURCE_DIR"); override != "" {
		return override, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(executable)
	candidates := []string{filepath.Join(dir, "resources")}
	if runtime.GOOS == "darwin" {
		candidates = append([]string{filepath.Join(dir, "..", "Resources")}, candidates...)
	}
	cwd, _ := os.Getwd()
	candidates = append(candidates, filepath.Join(cwd, "resources"), filepath.Join(cwd, "desktop", "resources"), filepath.Join(dir, "..", "..", "resources"))
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "playwright", "bridge.mjs")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Playwright runtime resources are missing; build with scripts/build.py")
}
