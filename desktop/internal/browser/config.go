package browser

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type object = map[string]any

func obj(v any) object {
	m, _ := v.(map[string]any)
	if m == nil {
		return object{}
	}
	return m
}
func str(m object, key string) string { s, _ := m[key].(string); return s }
func text(m object, key, fallback string) string {
	if s := str(m, key); s != "" {
		return s
	}
	return fallback
}
func number(m object, key string, fallback float64) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint16:
		return float64(v)
	case json.Number:
		n, e := v.Float64()
		if e == nil {
			return n
		}
	}
	return fallback
}
func boolean(m object, key string) bool { b, _ := m[key].(bool); return b }
func clone(m object) object {
	b, _ := json.Marshal(m)
	var out object
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	_ = d.Decode(&out)
	if out == nil {
		return object{}
	}
	return out
}
func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	if a, ok := v.([]string); ok {
		r := make([]any, len(a))
		for i, s := range a {
			r[i] = s
		}
		return r
	}
	return nil
}
func quote(v any) string { b, _ := json.Marshal(v); return string(b) }
func profileDir(profile object, engine string) string {
	d := str(profile, "dataDir")
	if engine == "cft" {
		return d
	}
	return filepath.Join(d, "engines", engine)
}
func extensionPaths(profile object, companion string) []string {
	out := []string{}
	add := func(p string) {
		if st, e := os.Stat(p); e == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	if companion != "" {
		add(companion)
	}
	for _, v := range array(profile["extensions"]) {
		e := obj(v)
		if boolean(e, "enabled") {
			add(str(e, "dir"))
		}
	}
	return out
}
func seedValue(id string, fp object) string {
	seed, ok := fp["seed"].(string)
	if !ok {
		seed = id
	}
	d := sha256.Sum256([]byte(seed))
	return strconv.FormatUint(uint64(10000+binary.BigEndian.Uint32(d[:4])%90000), 10)
}
func fingerprintArgs(id string, fp object) []string {
	device := str(fp, "device")
	platform := "macos"
	if strings.HasPrefix(device, "windows") {
		platform = "windows"
	}
	if strings.HasPrefix(device, "linux") {
		platform = "linux"
	}
	screen := obj(fp["screen"])
	mem := number(fp, "deviceMemory", 8)
	if mem > 0 {
		mem = math.Min(8, math.Pow(2, math.Round(math.Log2(mem))))
	}
	args := []string{"--fingerprint=" + seedValue(id, fp), "--fingerprint-platform=" + platform, "--fingerprint-locale=" + text(fp, "locale", "en-US"), "--fingerprint-timezone=" + text(fp, "timezone", "UTC"), "--fingerprint-user-agent=" + str(fp, "userAgent"), fmt.Sprintf("--fingerprint-screen-width=%g", number(screen, "width", 1280)), fmt.Sprintf("--fingerprint-screen-height=%g", number(screen, "height", 800)), fmt.Sprintf("--fingerprint-hardware-concurrency=%g", number(fp, "hardwareConcurrency", 8)), fmt.Sprintf("--fingerprint-device-memory=%g", mem), "--fingerprint-noise=false"}
	hints := obj(fp["clientHints"])
	for _, entry := range strings.Split(str(hints, "secChUa"), ",") {
		for _, brand := range [][2]string{{"Google Chrome", "Chrome"}, {"Microsoft Edge", "Edge"}, {"Opera", "Opera"}, {"Vivaldi", "Vivaldi"}, {"Brave", "Brave"}} {
			if strings.Contains(entry, `"`+brand[0]+`"`) {
				p := strings.Split(entry, `v="`)
				if len(p) == 2 {
					args = append(args, "--fingerprint-brand="+brand[1], "--fingerprint-brand-version="+strings.Split(p[1], `"`)[0])
				}
			}
		}
	}
	for _, p := range [][2]string{{"gpu-vendor", str(obj(fp["webgl"]), "vendor")}, {"gpu-renderer", str(obj(fp["webgl"]), "renderer")}, {"platform-version", str(hints, "secChUaPlatformVersion")}, {"fonts-dir", str(fp, "fontsDir")}} {
		if p[1] != "" {
			args = append(args, "--fingerprint-"+p[0]+"="+p[1])
		}
	}
	if q := number(fp, "storageQuota", 0); q > 0 {
		args = append(args, fmt.Sprintf("--fingerprint-storage-quota=%.0f", q))
	}
	if platform == "windows" {
		if avail := obj(fp["availScreen"]); len(avail) > 0 {
			h := number(screen, "height", 0) - number(avail, "height", 0)
			if h > 0 {
				args = append(args, fmt.Sprintf("--fingerprint-taskbar-height=%g", h), "--fingerprint-windows-font-metrics")
			}
		}
	}
	return args
}
func spawnArgs(profile object, engine string, port int, proxy, companion string, settings object) []string {
	fp := obj(profile["fingerprint"])
	screen := obj(fp["screen"])
	args := []string{"--user-data-dir=" + profileDir(profile, engine), "--remote-debugging-address=127.0.0.1", fmt.Sprintf("--remote-debugging-port=%d", port), "--no-first-run", "--no-default-browser-check", "--restore-last-session", "--disable-features=Translate", "--lang=" + text(fp, "locale", "en-US"), "--accept-lang=" + text(fp, "acceptLanguage", "en-US,en"), fmt.Sprintf("--window-size=%g,%g", number(screen, "width", 1280), number(screen, "height", 800)), fmt.Sprintf("--force-device-scale-factor=%g", number(fp, "dpr", 1))}
	if runtime.GOOS == "darwin" {
		args = append(args, "--use-mock-keychain")
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--password-store=basic")
	}
	if engine == "cloakbrowser" {
		args = append(args, fingerprintArgs(str(profile, "id"), fp)...)
		if proxy != "" {
			args = append(args, "--fingerprint-webrtc-ip=auto")
			if geo := obj(settings["proxyLocation"]); geo["latitude"] != nil && geo["longitude"] != nil {
				args = append(args, fmt.Sprintf("--fingerprint-location=%g,%g", number(geo, "latitude", 0), number(geo, "longitude", 0)))
			}
		}
	} else {
		if ua := str(fp, "userAgent"); ua != "" {
			args = append(args, "--user-agent="+ua)
		}
		args = append(args, "--test-type=gpu")
	}
	if proxy != "" {
		args[6] = "--disable-features=Translate,DnsOverHttps,DnsOverHttpsUpgrade,EncryptedClientHello,AsyncDns,DnsHttpsSvcb,DnsHttpsSvcbAlpn,NetworkPrediction"
		args = append(args, "--proxy-server="+proxy, "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--enforce-webrtc-ip-permission-check", "--dns-over-https-mode=off", "--dns-prefetch-disable", "--disable-async-dns", "--no-prerender", "--no-pings", "--disable-background-networking", "--disable-component-update", "--disable-domain-reliability", "--disable-client-side-phishing-detection")
	}
	if exts := extensionPaths(profile, companion); len(exts) > 0 {
		joined := strings.Join(exts, ",")
		args = append(args, "--load-extension="+joined, "--disable-extensions-except="+joined)
	}
	if boolean(settings, "headless") {
		args = append(args, "--headless=new")
	}
	for _, v := range array(settings["browserArgs"]) {
		if s, ok := v.(string); ok && !reservedArg(s) {
			args = append(args, s)
		}
	}
	if u := str(profile, "startUrl"); safeStartURL(u) {
		args = append(args, u)
	}
	return args
}
func reservedArg(s string) bool {
	for _, k := range []string{"--remote-debugging", "--user-data-dir", "--profile-directory"} {
		if strings.HasPrefix(strings.TrimSpace(s), k) {
			return true
		}
	}
	return false
}
func safeStartURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || s == "about:blank"
}
func reservePort() (net.Listener, int, error) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, 0, e
	}
	return l, l.Addr().(*net.TCPAddr).Port, nil
}
func prepareDataDir(dir string) error {
	if err := cleanSingletonLocks(dir); err != nil {
		return err
	}
	if e := os.MkdirAll(filepath.Join(dir, "Default"), 0700); e != nil {
		return e
	}
	path := filepath.Join(dir, "Default", "Preferences")
	prefs := object{}
	if b, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(b, &prefs); e != nil {
			return fmt.Errorf("read browser preferences: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if prefs == nil {
		prefs = object{}
	}
	session := obj(prefs["session"])
	session["restore_on_startup"] = 1
	prefs["session"] = session
	p := obj(prefs["profile"])
	p["exit_type"] = "Normal"
	p["exited_cleanly"] = true
	prefs["profile"] = p
	b, e := json.Marshal(prefs)
	if e != nil {
		return e
	}
	if e = os.WriteFile(path+".tmp", b, 0600); e != nil {
		return e
	}
	return os.Rename(path+".tmp", path)
}

func defaultBinary(engine string) string {
	for _, key := range []string{"MULTIZEN_BROWSER_BINARY", "CLOAKBROWSER_BINARY_PATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	if engine == "chromix" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return "cloakbrowser.exe"
	case "darwin":
		return "/Applications/CloakBrowser.app/Contents/MacOS/CloakBrowser"
	default:
		return "cloakbrowser"
	}
}
