package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const companionPollInterval = 600 * time.Millisecond

// CompanionPollScript reads and clears a signal only on Chrome Web Store pages.
const CompanionPollScript = `(function(){if(location.hostname!=="chromewebstore.google.com")return null;var e=document.documentElement;if(!e)return null;var v=e.getAttribute("data-mz-add-ext");if(v)e.removeAttribute("data-mz-add-ext");return v;})()`

// PollCompanion waits for one signal and installs it, emitting extensions:installed.
// evaluate must run the expression on candidate browser pages and return the first
// non-null JavaScript value (or a CDP Runtime.evaluate result). Cancel ctx when the
// profile closes. Transient evaluation errors are retried, as pages may navigate.
// The caller owns the browser restart after a successful return.
func (m *Manager) PollCompanion(ctx context.Context, profileID string, evaluate func(string) (any, error), emit func(string, any)) error {
	if evaluate == nil {
		return errors.New("companion evaluator is required")
	}
	ticker := time.NewTicker(companionPollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		value, err := evaluate(CompanionPollScript)
		if err == nil {
			value = companionValue(value)
			if value != nil && value != "" {
				return m.installCompanionSignal(ctx, profileID, value, emit)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func companionValue(value any) any {
	for depth := 0; depth < 4; depth++ {
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		if result, ok := object["result"]; ok {
			value = result
			continue
		}
		if result, ok := object["value"]; ok {
			return result
		}
		if object["subtype"] == "null" || object["type"] == "undefined" {
			return nil
		}
		return value
	}
	return value
}

func (m *Manager) installCompanionSignal(ctx context.Context, profileID string, value any, emit func(string, any)) error {
	fail := func(err error) error {
		if ctx.Err() == nil {
			emitCompanionError(emit, profileID, err)
		}
		return err
	}
	payload, ok := value.(string)
	if !ok {
		return fail(errors.New("Invalid companion signal"))
	}
	var signal map[string]any
	if err := json.Unmarshal([]byte(payload), &signal); err != nil {
		return fail(errors.New("Invalid companion signal"))
	}
	id := text(signal, "id")
	if id == "" {
		return fail(errors.New("No extension ID in companion signal"))
	}
	extension, err := m.PrepareFromWebStore(ctx, id)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := m.Add(profileID, extension); err != nil {
		return fail(err)
	}
	if emit != nil {
		emit("extensions:installed", map[string]any{"ok": true, "profileId": profileID, "extension": extension})
	}
	return nil
}

// StartCompanionPoller polls in the background and returns its cancellation function.
// reload must close/relaunch the profile and make evaluate target the new session.
// On successful reload, polling resumes; without reload it stops after one install.
func (m *Manager) StartCompanionPoller(ctx context.Context, profileID string, evaluate func(string) (any, error), emit func(string, any), reload func(context.Context, string) error) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		for {
			if err := m.PollCompanion(ctx, profileID, evaluate, emit); err != nil || reload == nil || ctx.Err() != nil {
				return
			}
			if err := reload(ctx, profileID); err != nil {
				if ctx.Err() == nil {
					emitCompanionError(emit, profileID, fmt.Errorf("Added it, but the profile didn't reopen — launch it again. (%w)", err))
				}
				return
			}
		}
	}()
	return cancel
}

func emitCompanionError(emit func(string, any), profileID string, err error) {
	if emit != nil {
		emit("extensions:installed", map[string]any{"ok": false, "profileId": profileID, "error": err.Error()})
	}
}
