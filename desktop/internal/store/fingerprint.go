package store

import (
	"fmt"
	"strings"
)

// FingerprintGenerate preserves the legacy deterministic desktop fingerprint.
func FingerprintGenerate(seed string) map[string]any {
	return map[string]any{
		"device":    "windows-desktop-intel",
		"userAgent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36",
		"platform":  "Win32",
		"clientHints": map[string]any{
			"secChUa":         `"Chromium";v="148", "Google Chrome";v="148", "Not?A_Brand";v="99"`,
			"secChUaPlatform": "Windows", "secChUaPlatformVersion": "10.0.0", "secChUaArch": "x86", "secChUaBitness": "64", "secChUaMobile": "?0", "secChUaModel": "",
			"secChUaFullVersionList": `"Chromium";v="148.0.0.0", "Google Chrome";v="148.0.0.0", "Not?A_Brand";v="99.0.0.0"`,
		},
		"locale": "en-US", "languages": []any{"en-US", "en"}, "acceptLanguage": "en-US,en;q=0.9", "timezone": "America/New_York", "country": "US",
		"screen": map[string]any{"width": 1920, "height": 1080}, "availScreen": map[string]any{"width": 1920, "height": 1040}, "dpr": 1.0,
		"webgl":               map[string]any{"vendor": "Google Inc. (Intel)", "renderer": "ANGLE (Intel, Intel(R) UHD Graphics 630 Direct3D11 vs_5_0 ps_5_0, D3D11)"},
		"hardwareConcurrency": 8, "deviceMemory": 8, "fontsDir": `C:\Windows\Fonts`, "storageQuota": uint64(2000000000), "seed": seed,
	}
}

var deviceCatalog = []struct{ family, label string }{
	{"macbook-pro-14-m3", `MacBook Pro 14" (M3)`}, {"macbook-pro-14-m3-pro", `MacBook Pro 14" (M3 Pro)`},
	{"macbook-pro-16-m3-pro", `MacBook Pro 16" (M3 Pro)`}, {"macbook-air-13-m3", `MacBook Air 13" (M3)`},
	{"macbook-air-15-m3", `MacBook Air 15" (M3)`}, {"imac-24-m3", `iMac 24" (M3)`}, {"mac-mini-m2", "Mac mini (M2)"},
	{"windows-laptop-intel", "Windows Laptop (Intel)"}, {"windows-laptop-intel-uhd", "Windows Laptop (Intel UHD)"}, {"windows-laptop-amd", "Windows Laptop (AMD)"},
	{"windows-laptop-nvidia", "Windows Laptop (NVIDIA)"}, {"windows-laptop-nvidia-4050", "Windows Laptop (NVIDIA 4050)"},
	{"windows-desktop-nvidia", "Windows Desktop (NVIDIA)"}, {"windows-desktop-nvidia-4080", "Windows Desktop (NVIDIA 4080)"},
	{"windows-desktop-amd", "Windows Desktop (AMD)"}, {"windows-desktop-intel", "Windows Desktop (Intel)"},
	{"linux-desktop-intel", "Linux Desktop (Intel)"}, {"linux-desktop-amd", "Linux Desktop (AMD)"}, {"linux-desktop-nvidia", "Linux Desktop (NVIDIA)"},
}

func screenOption(w, h int, native bool) map[string]any {
	label := fmt.Sprintf("%d × %d", w, h)
	if native {
		label += " (native)"
	}
	return map[string]any{"width": w, "height": h, "label": label}
}
func FingerprintDevices() any {
	out := []map[string]any{}
	for _, d := range deviceCatalog {
		screens := []map[string]any{}
		switch d.family {
		case "macbook-pro-14-m3", "macbook-pro-14-m3-pro", "macbook-air-13-m3":
			screens = append(screens, screenOption(1512, 982, true))
		case "macbook-air-15-m3", "macbook-pro-16-m3-pro":
			screens = append(screens, screenOption(1728, 1117, true))
		}
		screens = append(screens, screenOption(1920, 1080, false))
		if strings.HasPrefix(d.family, "windows-desktop") || strings.HasPrefix(d.family, "linux-desktop") || d.family == "imac-24-m3" {
			screens = append(screens, screenOption(2560, 1440, false))
		}
		out = append(out, map[string]any{"family": d.family, "label": d.label, "screens": screens})
	}
	return out
}

var localeCatalog = []struct {
	id, label string
	timezones []string
}{
	{"en-US", "English (United States)", []string{"America/New_York", "America/Chicago", "America/Los_Angeles"}},
	{"en-GB", "English (United Kingdom)", []string{"Europe/London"}},
	{"zh-CN", "中文 (简体, 中国大陆)", []string{"Asia/Shanghai"}},
	{"zh-TW", "中文 (繁體, 台灣)", []string{"Asia/Taipei"}},
	{"ja-JP", "日本語 (日本)", []string{"Asia/Tokyo"}},
	{"ko-KR", "한국어 (대한민국)", []string{"Asia/Seoul"}},
	{"de-DE", "Deutsch (Deutschland)", []string{"Europe/Berlin"}},
	{"fr-FR", "Français (France)", []string{"Europe/Paris"}},
	{"es-ES", "Español (España)", []string{"Europe/Madrid"}},
	{"pt-BR", "Português (Brasil)", []string{"America/Sao_Paulo", "America/Manaus"}},
	{"ru-RU", "Русский (Россия)", []string{"Europe/Moscow"}},
	{"it-IT", "Italiano (Italia)", []string{"Europe/Rome"}},
	{"nl-NL", "Nederlands (Nederland)", []string{"Europe/Amsterdam"}},
	{"pl-PL", "Polski (Polska)", []string{"Europe/Warsaw"}},
	{"tr-TR", "Türkçe (Türkiye)", []string{"Europe/Istanbul"}},
	{"ar-SA", "العربية (السعودية)", []string{"Asia/Riyadh"}},
	{"hi-IN", "हिन्दी (भारत)", []string{"Asia/Kolkata"}},
	{"id-ID", "Bahasa Indonesia", []string{"Asia/Jakarta"}},
	{"th-TH", "ไทย (ประเทศไทย)", []string{"Asia/Bangkok"}},
	{"vi-VN", "Tiếng Việt", []string{"Asia/Ho_Chi_Minh"}},
}

func FingerprintLocales() any {
	out := []map[string]any{}
	for _, l := range localeCatalog {
		out = append(out, map[string]any{"id": l.id, "label": l.label, "locale": l.id, "country": countryFromLocale(l.id), "timezones": append([]string{}, l.timezones...)})
	}
	return out
}
func countryFromLocale(locale string) string {
	parts := strings.Split(locale, "-")
	r := parts[len(parts)-1]
	if len(r) == 2 && ((r[0] >= 'a' && r[0] <= 'z') || (r[0] >= 'A' && r[0] <= 'Z')) && ((r[1] >= 'a' && r[1] <= 'z') || (r[1] >= 'A' && r[1] <= 'Z')) {
		return strings.ToUpper(r)
	}
	return ""
}
func FingerprintReconcile(current, patch map[string]any) (map[string]any, error) {
	fp, err := cloneMap(current)
	if err != nil {
		return nil, err
	}
	patch, err = cloneMap(patch)
	if err != nil {
		return nil, err
	}
	if err = validateFingerprint(fp); err != nil {
		return nil, err
	}
	if v := patch["localeId"]; v != nil {
		locale, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("localeId must be a string")
		}
		lang := strings.Split(locale, "-")[0]
		fp["locale"] = locale
		fp["languages"] = []any{locale, lang}
		fp["acceptLanguage"] = locale + "," + lang + ";q=0.9"
		fp["country"] = countryFromLocale(locale)
	}
	for _, k := range []string{"timezone", "device", "screen", "hardwareConcurrency", "deviceMemory"} {
		if patch[k] != nil {
			fp[k] = patch[k]
		}
	}
	if patch["country"] != nil {
		country, ok := patch["country"].(string)
		if !ok {
			return nil, fmt.Errorf("country must be a string")
		}
		fp["country"] = strings.ToUpper(country)
	}
	if err = validateFingerprint(fp); err != nil {
		return nil, err
	}
	return fp, nil
}
func FingerprintLocaleForCountry(country string) any {
	code := strings.ToUpper(strings.TrimSpace(country))
	for _, l := range localeCatalog {
		if countryFromLocale(l.id) == code {
			return l.id
		}
	}
	switch code {
	case "SG", "MY", "PH", "HK", "SE", "NO", "DK", "FI", "IS":
		return "en-GB"
	case "AT", "CH", "LI":
		return "de-DE"
	case "MX", "AR", "CL", "CO", "PE", "UY", "EC":
		return "es-ES"
	case "AE", "QA", "KW", "BH", "OM", "JO", "LB", "EG", "IQ", "MA", "DZ", "TN", "LY":
		return "ar-SA"
	case "UA", "BY", "KZ", "UZ", "GE", "AM", "AZ":
		return "ru-RU"
	}
	return nil
}

// Fingerprint methods also expose the stateless helpers through Store.
func (s *Store) FingerprintGenerate(seed string) map[string]any { return FingerprintGenerate(seed) }
func (s *Store) FingerprintDevices() any                        { return FingerprintDevices() }
func (s *Store) FingerprintLocales() any                        { return FingerprintLocales() }
func (s *Store) FingerprintReconcile(current, patch map[string]any) (map[string]any, error) {
	return FingerprintReconcile(current, patch)
}
func (s *Store) FingerprintLocaleForCountry(country string) any {
	return FingerprintLocaleForCountry(country)
}
