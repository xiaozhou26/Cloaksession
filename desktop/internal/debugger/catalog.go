package debugger

import (
	_ "embed"
	"encoding/json"
)

// Tool describes the pinned upstream tool schema with explicit session routing.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Captured from js-reverse-mcp@4.0.5 tools/list over initialized stdio.
//
//go:embed tool_schemas.json
var toolSchemas []byte

func Definitions() []Tool {
	var tools []Tool
	if err := json.Unmarshal(toolSchemas, &tools); err != nil {
		panic(err)
	}
	for i := range tools {
		props := tools[i].InputSchema["properties"].(map[string]any)
		props["debugSessionId"] = map[string]any{"type": "string", "minLength": 1, "description": "Explicit ID from attach_debug_session; never inferred from the active UI profile."}
		required := []string{"debugSessionId"}
		if original, ok := tools[i].InputSchema["required"].([]any); ok {
			for _, key := range original {
				required = append(required, key.(string))
			}
		}
		tools[i].InputSchema["required"] = required
		if tools[i].Name == "select_page" {
			props["targetId"] = map[string]any{"type": "string", "minLength": 1, "description": "Stable CDP page target ID from list_windows; use instead of pageIdx."}
			props["includeWindows"] = map[string]any{"type": "boolean", "description": "Include native window IDs and stable page target IDs."}
		}
	}
	return tools
}

var toolNames = func() map[string]bool {
	names := map[string]bool{}
	for _, tool := range Definitions() {
		names[tool.Name] = true
	}
	return names
}()

func IsTool(name string) bool { return toolNames[name] }
