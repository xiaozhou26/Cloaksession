package mcp

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

const activityCapacity = 500

var fallbackID atomic.Uint64

func activityID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), fallbackID.Add(1))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func clone(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			out[k] = clone(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = clone(child)
		}
		return out
	default:
		return value
	}
}

func sanitize(value any) any {
	switch v := clone(value).(type) {
	case map[string]any:
		for k, child := range v {
			switch strings.ToLower(k) {
			case "password", "username", "authorization", "token", "accesstoken", "refreshtoken":
				delete(v, k)
				continue
			case "text":
				if text, ok := child.(string); ok && len(text) > 80 {
					r := []rune(text)
					if len(r) > 77 {
						child = string(r[:77]) + "..."
					}
				}
			case "cookies":
				if cookies, ok := child.([]any); ok {
					for _, cookie := range cookies {
						if c, ok := cookie.(map[string]any); ok {
							if _, exists := c["value"]; exists {
								c["value"] = "[redacted]"
							}
						}
					}
				}
			}
			v[k] = sanitize(child)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = sanitize(child)
		}
		return v
	default:
		return v
	}
}

func (s *Server) startActivity(tool string, args any) string {
	var profileID any
	if tool != "list_profiles" && tool != "create_profile" && tool != "list_fingerprint_options" {
		if fields, ok := args.(map[string]any); ok {
			if id, ok := fields["profileId"].(string); ok {
				profileID = id
			}
		}
	}
	id := activityID()
	event := map[string]any{"id": id, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "tool": tool,
		"profileId": profileID, "args": sanitize(args), "status": "pending", "summary": nil, "durationMs": nil}
	s.activityMu.Lock()
	if len(s.events) == activityCapacity {
		copy(s.events, s.events[1:])
		s.events = s.events[:activityCapacity-1]
	}
	s.events = append(s.events, event)
	s.activityMu.Unlock()
	s.emitActivity(event)
	return id
}

func (s *Server) finishActivity(id string, started time.Time, result any, err error) {
	status, summary := "ok", "completed"
	if err != nil {
		status, summary = "error", errorCode(err)
	} else if payload, ok := result.(map[string]any); ok && payload["content"] != nil && payload["structuredContent"] != nil {
		// Debug evidence stays in the tool response, outside the shared activity feed.
		if payload["isError"] == true {
			status, summary = "error", "debug tool failed"
		} else {
			summary = "debug tool completed"
		}
	} else if data, marshalErr := json.Marshal(sanitize(result)); marshalErr == nil {
		runes := []rune(string(data))
		if len(runes) > 1024 {
			runes = append(runes[:1021], '.', '.', '.')
		}
		summary = string(runes)
	}
	var finished map[string]any
	s.activityMu.Lock()
	for _, event := range s.events {
		if event["id"] == id {
			event["status"], event["summary"], event["durationMs"] = status, summary, time.Since(started).Milliseconds()
			finished = clone(event).(map[string]any)
			break
		}
	}
	s.activityMu.Unlock()
	if finished != nil {
		s.emitActivity(finished)
	}
}

func (s *Server) emitActivity(event map[string]any) {
	if s.emit != nil {
		s.emit("activity:event", clone(event))
	}
}

// Recent returns newest-first snapshots, including calls still pending.
func (s *Server) Recent(limit int) []map[string]any {
	if limit < 0 {
		limit = 0
	}
	if limit > activityCapacity {
		limit = activityCapacity
	}
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	if limit > len(s.events) {
		limit = len(s.events)
	}
	out := make([]map[string]any, 0, limit)
	for i := len(s.events) - 1; i >= len(s.events)-limit; i-- {
		out = append(out, clone(s.events[i]).(map[string]any))
	}
	return out
}
