package store

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func stringFields(m map[string]any, required, optional []string) error {
	for _, k := range required {
		if _, ok := m[k].(string); !ok {
			return fmt.Errorf("%s must be a string", k)
		}
	}
	for _, k := range optional {
		if m[k] != nil {
			if _, ok := m[k].(string); !ok {
				return fmt.Errorf("%s must be a string or null", k)
			}
		}
	}
	return nil
}
func unsigned(v any, bits int) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = strconv.ParseUint(string(b), 10, bits)
	return err
}
func stringsArray(v any) bool {
	a, ok := v.([]any)
	if !ok {
		return false
	}
	for _, v := range a {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}
func validateProfile(p map[string]any) error {
	for _, key := range []string{"notes", "proxy", "extensions", "icon", "startUrl", "searchProvider", "lastOpenedAt", "proxyCountry"} {
		if _, exists := p[key]; !exists {
			p[key] = nil
		}
	}
	if err := stringFields(p, []string{"id", "name", "dataDir", "createdAt", "updatedAt"}, []string{"notes", "icon", "startUrl", "searchProvider", "lastOpenedAt", "proxyCountry"}); err != nil {
		return err
	}
	if !stringsArray(p["tags"]) {
		return fmt.Errorf("tags must be an array of strings")
	}
	if _, ok := p["chromixOptions"].(map[string]any); !ok {
		return fmt.Errorf("chromixOptions must be an object")
	}
	fp, ok := p["fingerprint"].(map[string]any)
	if !ok {
		return fmt.Errorf("fingerprint must be an object")
	}
	if err := validateFingerprint(fp); err != nil {
		return err
	}
	if p["proxy"] != nil {
		proxy, ok := p["proxy"].(map[string]any)
		if !ok {
			return fmt.Errorf("proxy must be an object")
		}
		if err := stringFields(proxy, []string{"type", "host"}, []string{"username", "password"}); err != nil {
			return err
		}
		if err := unsigned(proxy["port"], 16); err != nil {
			return fmt.Errorf("invalid proxy port: %w", err)
		}
		for _, k := range []string{"username", "password"} {
			if _, ok := proxy[k]; !ok {
				proxy[k] = nil
			}
		}
	}
	if p["extensions"] != nil {
		exts, ok := p["extensions"].([]any)
		if !ok {
			return fmt.Errorf("extensions must be an array")
		}
		for _, v := range exts {
			ext, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("extension must be an object")
			}
			if err := stringFields(ext, []string{"id", "name", "version", "scope", "dir", "source"}, nil); err != nil {
				return err
			}
			if _, ok := ext["enabled"].(bool); !ok {
				return fmt.Errorf("extension.enabled must be a boolean")
			}
		}
	}
	return nil
}
func validateFingerprint(fp map[string]any) error {
	for _, key := range []string{"availScreen", "fontsDir", "storageQuota", "seed"} {
		if _, exists := fp[key]; !exists {
			fp[key] = nil
		}
	}
	if err := stringFields(fp, []string{"device", "userAgent", "platform", "locale", "acceptLanguage", "timezone", "country"}, []string{"fontsDir", "seed"}); err != nil {
		return err
	}
	found := false
	for _, d := range deviceCatalog {
		if text(fp["device"]) == d.family {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown fingerprint device %q", fp["device"])
	}
	if !stringsArray(fp["languages"]) {
		return fmt.Errorf("fingerprint.languages must be an array of strings")
	}
	hints, ok := fp["clientHints"].(map[string]any)
	if !ok {
		return fmt.Errorf("clientHints must be an object")
	}
	if err := stringFields(hints, []string{"secChUa", "secChUaPlatform", "secChUaPlatformVersion", "secChUaArch", "secChUaBitness", "secChUaMobile", "secChUaModel", "secChUaFullVersionList"}, nil); err != nil {
		return err
	}
	gl, ok := fp["webgl"].(map[string]any)
	if !ok {
		return fmt.Errorf("webgl must be an object")
	}
	if err := stringFields(gl, []string{"vendor", "renderer"}, nil); err != nil {
		return err
	}
	for _, k := range []string{"screen", "availScreen"} {
		if k == "availScreen" && fp[k] == nil {
			continue
		}
		if err := validateScreen(fp[k]); err != nil {
			return err
		}
	}
	for _, k := range []string{"hardwareConcurrency", "deviceMemory"} {
		if err := unsigned(fp[k], 32); err != nil {
			return fmt.Errorf("invalid %s: %w", k, err)
		}
	}
	if fp["storageQuota"] != nil {
		if err := unsigned(fp["storageQuota"], 64); err != nil {
			return fmt.Errorf("invalid storageQuota: %w", err)
		}
	}
	b, err := json.Marshal(fp["dpr"])
	if err != nil {
		return err
	}
	if _, err = strconv.ParseFloat(string(b), 64); err != nil {
		return fmt.Errorf("dpr must be a number")
	}
	return nil
}
func validateScreen(v any) error {
	s, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("screen must be an object")
	}
	for _, k := range []string{"width", "height"} {
		if err := unsigned(s[k], 32); err != nil {
			return fmt.Errorf("invalid screen.%s: %w", k, err)
		}
	}
	return nil
}
func normalizeSettings(m map[string]any) error {
	if err := stringFields(m, []string{"theme", "browserEngine"}, []string{"browserBinaryPath"}); err != nil {
		return err
	}
	switch m["browserEngine"] {
	case "cft", "cloakbrowser", "chromix":
	default:
		return fmt.Errorf("unknown browserEngine")
	}
	for _, k := range []string{"mcpHttpEnabled", "skipBrowserDownload", "autoUpdate", "usageReporting"} {
		if _, ok := m[k].(bool); !ok {
			return fmt.Errorf("%s must be a boolean", k)
		}
	}
	if err := unsigned(m["mcpHttpPort"], 16); err != nil {
		return fmt.Errorf("invalid mcpHttpPort: %w", err)
	}
	c, ok := m["chromix"].(map[string]any)
	if !ok {
		return fmt.Errorf("chromix must be an object")
	}
	for k, v := range map[string]any{"nodePath": "node", "options": map[string]any{}, "environment": map[string]any{}} {
		if _, exists := c[k]; !exists {
			c[k] = v
		}
	}
	if _, ok := c["nodePath"].(string); !ok {
		return fmt.Errorf("chromix.nodePath must be a string")
	}
	if _, ok := c["options"].(map[string]any); !ok {
		return fmt.Errorf("chromix.options must be an object")
	}
	env, ok := c["environment"].(map[string]any)
	if !ok {
		return fmt.Errorf("chromix.environment must be an object")
	}
	for k, v := range env {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("chromix.environment.%s must be a string", k)
		}
	}
	return nil
}
