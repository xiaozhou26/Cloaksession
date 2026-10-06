package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ToolError lets backends provide a stable MCP error code without package dependencies.
// Backends may alternatively implement interface{ MCPCode() string } on their errors.
type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string   { return e.Message }
func (e *ToolError) MCPCode() string { return e.Code }

func invalid(message string) error   { return &ToolError{"INVALID_INPUT", "config error: " + message} }
func forbidden(message string) error { return &ToolError{"FORBIDDEN", "mcp error: " + message} }
func notFound(id string) error       { return &ToolError{"PROFILE_NOT_FOUND", "profile not found: " + id} }

func errorCode(err error) string {
	var coded interface{ MCPCode() string }
	if errors.As(err, &coded) {
		return coded.MCPCode()
	}
	if errors.Is(err, os.ErrNotExist) {
		return "PROFILE_NOT_FOUND"
	}
	if errors.Is(err, os.ErrExist) {
		return "ALREADY_EXISTS"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "INTERNAL_ERROR"
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		message := strings.ToLower(e.Error())
		if strings.HasPrefix(message, "no active browser session for profile ") {
			return "PROFILE_NOT_FOUND"
		}
		if strings.HasPrefix(message, "cdp ") || strings.HasPrefix(message, "javascript exception:") {
			return "CDP_ERROR"
		}
		for prefix, code := range map[string]string{
			"profile not found:": "PROFILE_NOT_FOUND", "profile already exists:": "ALREADY_EXISTS",
			"launch error:": "LAUNCH_FAILED", "mcp error:": "FORBIDDEN", "cdp error:": "CDP_ERROR", "config error:": "INVALID_INPUT",
		} {
			if strings.HasPrefix(message, prefix) {
				return code
			}
		}
	}
	return "INTERNAL_ERROR"
}

func tokenMatches(provided, expected string) bool {
	a, b := sha256.Sum256([]byte(provided)), sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1 && expected != ""
}

func rawCDPEnabled() bool {
	switch os.Getenv("MULTIZEN_MCP_ALLOW_RAW_CDP") {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func hasBlockedScheme(value string) bool {
	value = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, value)
	value = strings.ToLower(strings.TrimLeftFunc(value, func(r rune) bool { return r <= 0x20 }))
	for _, prefix := range []string{"file:", "chrome:", "devtools:", "view-source:"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func scanBlocked(value any) error {
	switch v := value.(type) {
	case string:
		if hasBlockedScheme(v) {
			return forbidden("forbidden URL scheme")
		}
	case map[string]any:
		for _, child := range v {
			if err := scanBlocked(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := scanBlocked(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func cdpMethodAllowed(method string) bool {
	for _, exact := range []string{"IO.read", "Page.getResourceContent", "Storage.getCookies", "Network.getAllCookies", "Browser.close", "Browser.crash"} {
		if method == exact {
			return false
		}
	}
	for _, prefix := range []string{"DOMStorage.", "IndexedDB.", "CacheStorage.", "Fetch."} {
		if strings.HasPrefix(method, prefix) {
			return false
		}
	}
	return true
}

func securityCheck(name string, args map[string]any) error {
	switch name {
	case "navigate", "new_tab":
		return scanBlocked(args["url"])
	case "get_cookies":
		return scanBlocked(args["urls"])
	case "set_cookies":
		return scanBlocked(args["cookies"])
	case "cdp_send":
		if !rawCDPEnabled() {
			return forbidden("raw CDP disabled")
		}
		method, _ := args["method"].(string)
		if !cdpMethodAllowed(method) {
			return forbidden(fmt.Sprintf("forbidden CDP method: %s", method))
		}
		return scanBlocked(args["params"])
	}
	return nil
}
