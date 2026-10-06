package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func detectProxyGeo(ctx context.Context, config map[string]any) (map[string]any, error) {
	host, port := text(config["host"]), int(number(config["port"]))
	if host == "" || port < 1 || port > 65535 {
		return nil, fmt.Errorf("proxy host and port are required")
	}
	scheme := text(config["type"])
	if scheme == "" {
		scheme = text(config["proxyType"])
	}
	if scheme != "socks5" && scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported proxy type: %s", scheme)
	}
	proxyURL := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, fmt.Sprint(port))}
	if username := text(config["username"]); username != "" {
		proxyURL.User = url.UserPassword(username, text(config["password"]))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipapi.co/json/", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Cloaksession/"+appVersion)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("proxy geo request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy geo HTTP %d", response.StatusCode)
	}
	return parseProxyGeo(io.LimitReader(response.Body, 1<<20))
}

func parseProxyGeo(reader io.Reader) (map[string]any, error) {
	var raw map[string]any
	if err := json.NewDecoder(reader).Decode(&raw); err != nil {
		return nil, fmt.Errorf("proxy geo JSON: %w", err)
	}
	if failure := raw["error"]; failure != nil && failure != false && failure != "" {
		return nil, fmt.Errorf("proxy geo: %v %v", failure, raw["reason"])
	}
	country, timezone := text(raw["country_code"]), text(raw["timezone"])
	if country == "" || timezone == "" {
		return nil, fmt.Errorf("proxy geo missing country_code or timezone")
	}
	name := text(raw["country_name"])
	if name == "" {
		name = country
	}
	result := map[string]any{"country": strings.ToLower(country), "countryName": name, "timezone": timezone, "city": text(raw["city"]), "ip": text(raw["ip"]), "latitude": nil, "longitude": nil}
	for _, key := range []string{"latitude", "longitude"} {
		if value, ok := raw[key].(float64); ok {
			result[key] = value
		}
	}
	return result, nil
}
