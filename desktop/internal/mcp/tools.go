package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Backend uses desktop IPC commands for profiles and MCP tool names for browser operations.
// Browser operations return their complete MCP payload and enforce running-profile checks.
type Backend interface {
	Invoke(context.Context, string, map[string]any) (any, error)
}

func (s *Server) invoke(ctx context.Context, command string, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.backend == nil {
		return nil, fmt.Errorf("MCP backend is unavailable")
	}
	value, err := s.backend.Invoke(ctx, command, args)
	if err != nil {
		if command == "profiles_launch" && errorCode(err) == "INTERNAL_ERROR" && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return nil, &ToolError{"LAUNCH_FAILED", err.Error()}
		}
		return nil, err
	}
	// Backends may return structs or typed slices, not just decoded JSON maps.
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode backend result: %w", err)
	}
	var out any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) callTool(ctx context.Context, t tool, raw any) (result any, err error) {
	started := time.Now()
	id := s.startActivity(t.name, raw)
	defer func() { s.finishActivity(id, started, result, err) }()
	if err = validate(raw, t.schema, "arguments"); err != nil {
		return nil, invalid("invalid tool arguments: " + err.Error())
	}
	args := clone(raw).(map[string]any)
	if err = securityCheck(t.name, args); err != nil {
		return nil, err
	}
	return s.dispatchTool(ctx, t.name, args)
}

func (s *Server) getProfile(ctx context.Context, id string) (map[string]any, error) {
	value, err := s.invoke(ctx, "profiles_get", map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, notFound(id)
	}
	profile, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid profile response")
	}
	return profile, nil
}

func redactProxy(value any) any {
	p, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	_, username := p["username"].(string)
	_, password := p["password"].(string)
	return map[string]any{"type": p["type"], "host": p["host"], "port": p["port"], "hasAuth": username && password}
}

func selectFields(value map[string]any, fields ...string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, key := range fields {
		out[key] = value[key]
	}
	return out
}

func (s *Server) dispatchTool(ctx context.Context, name string, args map[string]any) (any, error) {
	id, _ := args["profileId"].(string)
	idArgs := map[string]any{"id": id}
	switch name {
	case "list_profiles":
		value, err := s.invoke(ctx, "profiles_list", map[string]any{})
		if err != nil {
			return nil, err
		}
		profiles, ok := value.([]any)
		if !ok && value != nil {
			return nil, fmt.Errorf("invalid profiles response")
		}
		out := make([]any, 0, len(profiles))
		for _, value := range profiles {
			p, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid profile summary")
			}
			summary := selectFields(p, "id", "name", "tags", "lastOpenedAt", "isRunning", "icon", "timezone", "proxyCountry", "device")
			summary["proxy"] = redactProxy(p["proxy"])
			out = append(out, summary)
		}
		return map[string]any{"profiles": out}, nil
	case "launch_profile":
		if _, err := s.getProfile(ctx, id); err != nil {
			return nil, err
		}
		return s.invoke(ctx, "profiles_launch", idArgs)
	case "close_profile":
		if _, err := s.invoke(ctx, "profiles_close", idArgs); err != nil {
			return nil, err
		}
		return map[string]any{"closed": true}, nil
	case "create_profile":
		input := clone(args).(map[string]any)
		delete(input, "seed")
		delete(input, "profileId")
		value, err := s.invoke(ctx, "profiles_create", map[string]any{"input": input})
		if err != nil {
			return nil, err
		}
		p, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid profile response")
		}
		fingerprint, _ := p["fingerprint"].(map[string]any)
		return map[string]any{"id": p["id"], "name": p["name"], "proxy": redactProxy(p["proxy"]),
			"fingerprint": selectFields(fingerprint, "device", "userAgent", "locale", "timezone", "country")}, nil
	case "update_profile":
		existing, err := s.getProfile(ctx, id)
		if err != nil {
			return nil, err
		}
		patch := map[string]any{}
		for _, key := range []string{"name", "notes", "tags", "proxy"} {
			if value := args[key]; value != nil {
				patch[key] = value
			}
		}
		if partial, ok := args["fingerprint"].(map[string]any); ok {
			fingerprint, _ := clone(existing["fingerprint"]).(map[string]any)
			if fingerprint == nil {
				return nil, fmt.Errorf("invalid fingerprint response")
			}
			for _, key := range []string{"userAgent", "locale", "timezone", "country"} {
				if value := partial[key]; value != nil {
					fingerprint[key] = value
				}
			}
			patch["fingerprint"] = fingerprint
		}
		value, err := s.invoke(ctx, "profiles_update", map[string]any{"id": id, "patch": patch})
		if err != nil {
			return nil, err
		}
		p, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid profile response")
		}
		return map[string]any{"id": p["id"], "name": p["name"], "proxy": redactProxy(p["proxy"]), "appliesOnNextLaunch": true}, nil
	case "delete_profile":
		value, err := s.invoke(ctx, "profiles_list", map[string]any{})
		if err != nil {
			return nil, err
		}
		profiles, ok := value.([]any)
		if !ok && value != nil {
			return nil, fmt.Errorf("invalid profiles response")
		}
		for _, value := range profiles {
			if p, ok := value.(map[string]any); ok && p["id"] == id && p["isRunning"] == true {
				if _, err := s.invoke(ctx, "profiles_close", idArgs); err != nil {
					return nil, err
				}
				break
			}
		}
		if _, err := s.invoke(ctx, "profiles_delete", idArgs); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true}, nil
	case "list_fingerprint_options":
		devices, err := s.invoke(ctx, "fingerprint_devices", map[string]any{})
		if err != nil {
			return nil, err
		}
		locales, err := s.invoke(ctx, "fingerprint_locales", map[string]any{})
		if err != nil {
			return nil, err
		}
		d, err := catalogIDs(devices, "family")
		if err != nil {
			return nil, err
		}
		l, err := catalogIDs(locales, "locale")
		if err != nil {
			return nil, err
		}
		return map[string]any{"devices": d, "locales": l}, nil
	default:
		if name == "wait_for_selector" || name == "wait_for_navigation" || name == "wait_for_load" {
			if args["timeoutMs"] == nil {
				args["timeoutMs"] = 30000
			}
		}
		return s.invoke(ctx, name, args)
	}
}

func catalogIDs(value any, field string) ([]string, error) {
	entries, ok := value.([]any)
	if !ok && value != nil {
		return nil, fmt.Errorf("invalid fingerprint catalog response")
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if text, ok := entry.(string); ok {
			out = append(out, text)
			continue
		}
		object, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid fingerprint catalog entry")
		}
		text, ok := object[field].(string)
		if !ok {
			return nil, fmt.Errorf("invalid fingerprint catalog %s", field)
		}
		out = append(out, text)
	}
	return out, nil
}
