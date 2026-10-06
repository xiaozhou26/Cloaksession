package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const releaseRepository = "https://github.com/xiaozhou26/Cloaksession"
const releaseAPI = "https://api.github.com/repos/xiaozhou26/Cloaksession/releases/latest"

type updater struct {
	mu        sync.Mutex
	checkMu   sync.Mutex
	current   map[string]any
	checked   int64
	installer string
	version   string
	emit      func(string, any)
	openURL   func(string) error
	client    *http.Client
	api       string
}

func newUpdater(emit func(string, any), openURL func(string) error) *updater {
	return &updater{current: map[string]any{"kind": "idle"}, emit: emit, openURL: openURL, client: &http.Client{Timeout: 5 * time.Minute}, api: releaseAPI}
}
func (u *updater) status() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	copy := make(map[string]any, len(u.current))
	for key, value := range u.current {
		copy[key] = value
	}
	return copy
}
func (u *updater) lastChecked() int64 { u.mu.Lock(); defer u.mu.Unlock(); return u.checked }
func (u *updater) set(status map[string]any) {
	u.mu.Lock()
	u.current = status
	u.mu.Unlock()
	if u.emit != nil {
		u.emit("update:status", map[string]any{"status": status})
	}
}
func (u *updater) failure(err error) (any, error) {
	u.set(map[string]any{"kind": "error", "message": err.Error()})
	return u.status(), nil
}

func (u *updater) check(ctx context.Context) (any, error) {
	u.checkMu.Lock()
	defer u.checkMu.Unlock()
	u.set(map[string]any{"kind": "checking"})
	u.mu.Lock()
	u.checked = time.Now().UnixMilli()
	u.mu.Unlock()
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, u.api, nil)
	if err != nil {
		return u.failure(err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Cloaksession/"+appVersion)
	response, err := u.client.Do(request)
	if err != nil {
		return u.failure(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return u.failure(fmt.Errorf("GitHub API returned HTTP %d", response.StatusCode))
	}
	var release struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&release); err != nil {
		return u.failure(err)
	}
	version := strings.TrimPrefix(release.Tag, "v")
	if !newerVersion(version, appVersion) {
		u.set(map[string]any{"kind": "up-to-date"})
		return u.status(), nil
	}
	u.mu.Lock()
	ready := u.version == version && u.installer != ""
	installer := u.installer
	u.mu.Unlock()
	if ready {
		if _, err := os.Stat(installer); err == nil {
			u.set(map[string]any{"kind": "ready", "version": version})
			return u.status(), nil
		}
	}
	u.set(map[string]any{"kind": "available", "version": version, "releaseNotes": release.Body})
	if runtime.GOOS == "windows" {
		for _, asset := range release.Assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), "-setup.exe") {
				if err = u.downloadInstaller(ctx, version, asset.URL, asset.Size); err != nil {
					return u.failure(err)
				}
				break
			}
		}
	}
	return u.status(), nil
}

func (u *updater) downloadInstaller(ctx context.Context, version, downloadURL string, total int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	response, err := u.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("installer download HTTP %d", response.StatusCode)
	}
	file, err := os.CreateTemp("", "cloaksession-*-setup.exe")
	if err != nil {
		return err
	}
	path := file.Name()
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if total == 0 {
		total = response.ContentLength
	}
	buffer := make([]byte, 128<<10)
	var received int64
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if _, err = file.Write(buffer[:n]); err != nil {
				return err
			}
			received += int64(n)
			percent := int64(0)
			if total > 0 {
				percent = received * 100 / total
			}
			u.set(map[string]any{"kind": "downloading", "version": version, "received": received, "total": max(total, 0), "percent": percent})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if total > 0 && received != total {
		return fmt.Errorf("incomplete installer: %d of %d bytes", received, total)
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	u.mu.Lock()
	u.installer = path
	u.version = version
	u.mu.Unlock()
	ok = true
	u.set(map[string]any{"kind": "ready", "version": version})
	return nil
}

func (u *updater) install() error {
	if runtime.GOOS != "windows" {
		return errors.New("automatic installation is available on Windows; open the release download instead")
	}
	u.mu.Lock()
	path := u.installer
	u.mu.Unlock()
	if path == "" {
		return errors.New("no downloaded installer is available")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	command := exec.Command(filepath.Clean(path))
	if err := command.Start(); err != nil {
		return err
	}
	go command.Wait()
	return nil
}

func (u *updater) downloadPage(version string) error {
	version = strings.TrimLeft(version, "v")
	if !validVersion(version) {
		return errors.New("invalid release version")
	}
	if u.openURL == nil {
		return errors.New("browser URL opener is unavailable")
	}
	return u.openURL(releaseRepository + "/releases/tag/v" + version)
}

func validVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}
func newerVersion(latest, current string) bool {
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")
	if !validVersion(latest) || !validVersion(current) {
		return false
	}
	a, b := strings.Split(latest, "."), strings.Split(current, ".")
	for i := range a {
		x, _ := strconv.ParseUint(a[i], 10, 32)
		y, _ := strconv.ParseUint(b[i], 10, 32)
		if x != y {
			return x > y
		}
	}
	return false
}
