package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

type tool struct {
	name, description string
	schema            map[string]any
}

func object(properties map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func stringSchema() map[string]any { return map[string]any{"type": "string"} }
func optional(s map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{s, map[string]any{"type": "null"}}}
}
func array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func catalog() []tool {
	profile := func(extra map[string]any, required ...string) map[string]any {
		p := map[string]any{"profileId": stringSchema()}
		for k, v := range extra {
			p[k] = v
		}
		return object(p, append([]string{"profileId"}, required...)...)
	}
	proxy := object(map[string]any{
		"type": stringSchema(), "host": stringSchema(),
		"port":     map[string]any{"type": "integer", "minimum": 0, "maximum": 65535},
		"username": optional(stringSchema()), "password": optional(stringSchema()),
	}, "type", "host", "port")
	fingerprint := object(map[string]any{
		"userAgent": optional(stringSchema()), "locale": optional(stringSchema()),
		"timezone": optional(stringSchema()), "country": optional(stringSchema()),
	})
	management := func(create bool) map[string]any {
		p := map[string]any{"name": optional(stringSchema()), "notes": optional(stringSchema()),
			"tags": optional(array(stringSchema())), "proxy": optional(proxy),
			"fingerprint": optional(fingerprint), "seed": optional(stringSchema())}
		if create {
			p["name"] = stringSchema()
			return object(p, "name")
		}
		return profile(p)
	}
	timeout := map[string]any{"type": "integer", "minimum": 0, "maximum": uint64(math.MaxUint64)}
	selectorTimeout := map[string]any{"type": "integer", "minimum": 0, "maximum": uint64(math.MaxUint64), "default": 30000}
	return []tool{
		{"list_profiles", "List local browser profiles and their running state.", object(map[string]any{})},
		{"launch_profile", "Launch a browser profile.", profile(nil)},
		{"close_profile", "Close a browser profile.", profile(nil)},
		{"navigate", "Navigate a running profile to a web URL.", profile(map[string]any{"url": stringSchema()}, "url")},
		{"click", "Click an element in a running profile.", profile(map[string]any{"selector": stringSchema()}, "selector")},
		{"type", "Type text into an element in a running profile.", profile(map[string]any{"selector": stringSchema(), "text": stringSchema()}, "selector", "text")},
		{"extract", "Extract the active page from a running profile.", profile(nil)},
		{"screenshot", "Capture a screenshot from a running profile.", profile(nil)},
		{"create_profile", "Create a local browser profile.", management(true)},
		{"update_profile", "Update a local browser profile.", management(false)},
		{"delete_profile", "Delete a local browser profile.", profile(nil)},
		{"list_fingerprint_options", "List supported fingerprint options.", object(map[string]any{})},
		{"evaluate_js", "Evaluate JavaScript in a running profile.", profile(map[string]any{"expression": stringSchema(), "sessionId": optional(stringSchema())}, "expression")},
		{"wait_for_selector", "Wait for a selector in a running profile.", profile(map[string]any{"selector": stringSchema(), "timeoutMs": selectorTimeout}, "selector")},
		{"list_tabs", "List tabs in a running profile.", profile(nil)},
		{"activate_tab", "Activate a tab in a running profile.", profile(map[string]any{"tabId": stringSchema()}, "tabId")},
		{"close_tab", "Close a tab in a running profile.", profile(map[string]any{"tabId": stringSchema()}, "tabId")},
		{"wait_for_navigation", "Wait for navigation in a running profile.", profile(map[string]any{"timeoutMs": optional(timeout)})},
		{"wait_for_load", "Wait for page load in a running profile.", profile(map[string]any{"timeoutMs": optional(timeout)})},
		{"cdp_send", "Send an allow-listed CDP request.", profile(map[string]any{"method": stringSchema(), "params": map[string]any{}, "sessionId": optional(stringSchema())}, "method")},
		{"get_cookies", "Read cookies through the controlled browser session.", profile(map[string]any{"urls": array(stringSchema()), "sessionId": optional(stringSchema())}, "urls")},
		{"set_cookies", "Set cookies through the controlled browser session.", profile(map[string]any{"cookies": array(map[string]any{}), "sessionId": optional(stringSchema())}, "cookies")},
		{"new_tab", "Open a new tab in a running profile.", profile(map[string]any{"url": stringSchema()}, "url")},
	}
}

func toolDefinitions() []map[string]any {
	out := make([]map[string]any, 0, 23)
	for _, t := range catalog() {
		if t.name == "cdp_send" && !rawCDPEnabled() {
			continue
		}
		t.schema["$schema"] = "http://json-schema.org/draft-07/schema#"
		out = append(out, map[string]any{"name": t.name, "description": t.description, "inputSchema": t.schema})
	}
	return out
}

func lookupTool(name string) (tool, bool) {
	for _, t := range catalog() {
		if t.name == name {
			return t, true
		}
	}
	return tool{}, false
}

// Validate the same schema advertised to clients; unknown fields remain compatible with serde.
func validate(value any, schema map[string]any, path string) error {
	if choices, ok := schema["anyOf"].([]any); ok {
		for _, choice := range choices {
			if validate(value, choice.(map[string]any), path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s has an invalid type", path)
	}
	bad := func() error { return fmt.Errorf("%s must be %s", path, schema["type"]) }
	switch schema["type"] {
	case "null":
		if value != nil {
			return bad()
		}
	case "string":
		if _, ok := value.(string); !ok {
			return bad()
		}
	case "integer":
		var n uint64
		switch v := value.(type) {
		case json.Number:
			var err error
			n, err = strconv.ParseUint(string(v), 10, 64)
			if err != nil {
				return bad()
			}
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= float64(math.MaxUint64) || v != math.Trunc(v) {
				return bad()
			}
			n = uint64(v)
		case int:
			if v < 0 {
				return bad()
			}
			n = uint64(v)
		default:
			return bad()
		}
		if min, ok := schema["minimum"].(int); ok && n < uint64(min) {
			return bad()
		}
		if max, ok := schema["maximum"].(int); ok && n > uint64(max) {
			return bad()
		}
		if max, ok := schema["maximum"].(uint64); ok && n > max {
			return bad()
		}
	case "array":
		values, ok := value.([]any)
		if !ok {
			return bad()
		}
		for i, v := range values {
			if err := validate(v, schema["items"].(map[string]any), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "object":
		values, ok := value.(map[string]any)
		if !ok || values == nil {
			return bad()
		}
		if required, ok := schema["required"].([]string); ok {
			for _, k := range required {
				if _, exists := values[k]; !exists {
					return fmt.Errorf("missing required field %s.%s", path, k)
				}
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		keys := make([]string, 0, len(properties))
		for k := range properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v, exists := values[k]; exists {
				if err := validate(v, properties[k].(map[string]any), path+"."+k); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
