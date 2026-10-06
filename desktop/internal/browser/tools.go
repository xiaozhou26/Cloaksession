package browser

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type toolSession struct {
	c       *cdpClient
	session string
}

func (t toolSession) call(ctx context.Context, method string, params object) (object, error) {
	return t.c.call(ctx, method, params, t.session)
}
func (t toolSession) evaluate(ctx context.Context, expression string) (any, error) {
	r, e := t.call(ctx, "Runtime.evaluate", object{"expression": expression, "returnByValue": true, "awaitPromise": true, "userGesture": true})
	if e != nil {
		return nil, e
	}
	if exception := obj(r["exceptionDetails"]); len(exception) > 0 {
		return nil, fmt.Errorf("JavaScript exception: %s", text(obj(exception["exception"]), "description", text(exception, "text", "evaluation failed")))
	}
	return obj(r["result"])["value"], nil
}

// Tool returns the payload shape used by the MCP browser tools. Authorization is
// enforced by the MCP transport; Call and Tool are also used by the desktop UI.
func (m *Manager) Tool(ctx context.Context, tool string, args map[string]any) (any, error) {
	id := text(args, "profileId", str(args, "profile_id"))
	h, e := m.get(id)
	if e != nil {
		return nil, e
	}
	timeout := number(args, "timeoutMs", 30000)
	if timeout < 1 {
		timeout = 1
	}
	if timeout > 3600000 {
		timeout = 3600000
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	select {
	case h.tools <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-h.tools }()
	t := toolSession{c: h.cdp, session: text(args, "sessionId", str(args, "session_id"))}
	tool = strings.TrimPrefix(tool, "browser_")
	switch tool {
	case "cdp_send", "raw_cdp":
		return t.call(ctx, str(args, "method"), obj(args["params"]))
	case "evaluate_js", "evaluate":
		return t.call(ctx, "Runtime.evaluate", object{"expression": str(args, "expression"), "returnByValue": true, "awaitPromise": true})
	case "list_tabs":
		return t.call(ctx, "Target.getTargets", nil)
	case "new_tab":
		return t.call(ctx, "Target.createTarget", object{"url": text(args, "url", "about:blank")})
	case "activate_tab":
		_, e = t.call(ctx, "Target.activateTarget", object{"targetId": text(args, "tabId", str(args, "targetId"))})
		return object{"activated": true}, e
	case "close_tab":
		_, e = t.call(ctx, "Target.closeTarget", object{"targetId": text(args, "tabId", str(args, "targetId"))})
		return object{"closed": true}, e
	case "get_cookies":
		p := object{}
		if urls, ok := args["urls"]; ok {
			p["urls"] = urls
		}
		return t.call(ctx, "Network.getCookies", p)
	case "set_cookies":
		_, e = t.call(ctx, "Network.setCookies", object{"cookies": args["cookies"]})
		return object{"set": true}, e
	case "clear_cookies":
		_, e = t.call(ctx, "Network.clearBrowserCookies", nil)
		return object{"cleared": true}, e
	case "navigate":
		url := str(args, "url")
		if url == "" {
			return nil, errors.New("url is required")
		}
		r, e := t.call(ctx, "Page.navigate", object{"url": url})
		if e != nil {
			return nil, e
		}
		if message := str(r, "errorText"); message != "" {
			return nil, errors.New(message)
		}
		if e = t.wait(ctx, "document.readyState === 'complete'"); e != nil {
			return nil, e
		}
		value, e := t.evaluate(ctx, "location.href")
		return object{"url": value}, e
	case "reload":
		_, e = t.call(ctx, "Page.reload", object{"ignoreCache": boolean(args, "ignoreCache")})
		if e == nil {
			e = t.wait(ctx, "document.readyState === 'complete'")
		}
		return object{"reloaded": true}, e
	case "extract":
		value, e := t.evaluate(ctx, "({url:location.href,title:document.title,text:document.body?document.body.innerText.slice(0,8000):''})")
		return object{"data": value}, e
	case "screenshot":
		params := object{"format": "png"}
		if format := str(args, "format"); format != "" {
			params["format"] = format
		}
		if q, ok := args["quality"]; ok {
			params["quality"] = q
		}
		if boolean(args, "fullPage") {
			r, e := t.call(ctx, "Page.getLayoutMetrics", nil)
			if e != nil {
				return nil, e
			}
			size := obj(r["cssContentSize"])
			if len(size) == 0 {
				size = obj(r["contentSize"])
			}
			params["captureBeyondViewport"] = true
			params["clip"] = object{"x": number(size, "x", 0), "y": number(size, "y", 0), "width": number(size, "width", 1280), "height": number(size, "height", 800), "scale": 1}
		}
		r, e := t.call(ctx, "Page.captureScreenshot", params)
		return object{"data": r["data"]}, e
	case "wait_for_selector", "wait_for_navigation", "wait_for_load", "wait":
		key, expression := "ready", "document.readyState === 'complete'"
		if tool == "wait_for_selector" {
			key = "found"
			expression = "!!document.querySelector(" + quote(str(args, "selector")) + ")"
		}
		if tool == "wait_for_load" {
			key = "loaded"
		}
		if tool == "wait" {
			duration := number(args, "ms", number(args, "durationMs", 1000))
			e = pause(ctx, time.Duration(math.Max(0, duration))*time.Millisecond)
			return object{"waited": e == nil}, e
		}
		e = t.wait(ctx, expression)
		if e != nil && errors.Is(e, context.DeadlineExceeded) && parent.Err() == nil {
			return object{key: false, "timedOut": true}, nil
		}
		return object{key: e == nil}, e
	case "click", "hover":
		x, y, e := t.point(ctx, args)
		if e != nil {
			return nil, e
		}
		if e = t.move(ctx, x, y); e != nil {
			return nil, e
		}
		if tool == "hover" {
			return object{"hovered": true}, nil
		}
		button := text(args, "button", "left")
		if button != "left" && button != "right" && button != "middle" {
			return nil, errors.New("button must be left, right, or middle")
		}
		count := int(number(args, "clickCount", 1))
		if count < 1 || count > 3 {
			return nil, errors.New("clickCount must be between 1 and 3")
		}
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			if _, e = t.call(ctx, "Input.dispatchMouseEvent", object{"type": kind, "x": x, "y": y, "button": button, "clickCount": count}); e != nil {
				return nil, e
			}
		}
		return object{"clicked": true}, nil
	case "type", "type_text":
		if e = t.focus(ctx, str(args, "selector")); e != nil {
			return nil, e
		}
		if boolean(args, "clear") {
			if e = t.key(ctx, "ControlOrMeta+A"); e != nil {
				return nil, e
			}
			if e = t.key(ctx, "Backspace"); e != nil {
				return nil, e
			}
		}
		value := str(args, "text")
		rng := lcg(uint64(len(value)))
		for _, ch := range value {
			if ch == '\n' {
				e = t.key(ctx, "Enter")
			} else if ch == '\t' {
				e = t.key(ctx, "Tab")
			} else {
				_, e = t.call(ctx, "Input.dispatchKeyEvent", object{"type": "keyDown", "key": string(ch), "text": string(ch)})
				if e == nil {
					_, e = t.call(ctx, "Input.dispatchKeyEvent", object{"type": "keyUp", "key": string(ch)})
				}
			}
			if e != nil {
				return nil, e
			}
			delay := math.Max(40, math.Min(400, 110+((rng()+rng()+rng())-1.5)*70))
			if unicode.IsSpace(ch) {
				delay += 60
			} else if strings.ContainsRune(".,!?", ch) {
				delay += 90
			}
			if v, ok := args["delayMs"]; ok {
				delay = number(object{"delay": v}, "delay", delay)
			}
			if e = pause(ctx, time.Duration(math.Max(0, delay))*time.Millisecond); e != nil {
				return nil, e
			}
		}
		return object{"typed": true}, nil
	case "press", "press_key", "key":
		if s := str(args, "selector"); s != "" {
			if e = t.focus(ctx, s); e != nil {
				return nil, e
			}
		}
		e = t.key(ctx, text(args, "key", str(args, "keys")))
		return object{"pressed": true}, e
	case "scroll":
		x, y := number(args, "x", 100), number(args, "y", 100)
		if str(args, "selector") != "" {
			x, y, e = t.point(ctx, args)
			if e != nil {
				return nil, e
			}
		}
		dx, dy := number(args, "deltaX", 0), number(args, "deltaY", 0)
		if _, ok := args["deltaY"]; !ok {
			amount := number(args, "amount", 500)
			switch text(args, "direction", "down") {
			case "up":
				dy = -amount
			case "left":
				dx = -amount
			case "right":
				dx = amount
			default:
				dy = amount
			}
		}
		steps := scrollSteps(dy, math.Float64bits(dy))
		for _, step := range steps {
			_, e = t.call(ctx, "Input.dispatchMouseEvent", object{"type": "mouseWheel", "x": x, "y": y, "deltaX": dx / float64(len(steps)), "deltaY": step})
			if e != nil {
				return nil, e
			}
			if e = pause(ctx, 12*time.Millisecond); e != nil {
				return nil, e
			}
		}
		return object{"scrolled": true}, nil
	case "select", "select_option":
		values := array(args["values"])
		if len(values) == 0 {
			values = []any{args["value"]}
		}
		expression := `(()=>{const e=document.querySelector(` + quote(str(args, "selector")) + `);if(!e)throw Error('element not found');if(e.tagName!=='SELECT')throw Error('element is not a select');const values=` + quote(values) + `;let matched=0;for(const o of e.options){o.selected=values.includes(o.value)||values.includes(o.label);if(o.selected)matched++}if(!matched)throw Error('option not found');e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));return Array.from(e.selectedOptions,o=>o.value)})()`
		value, e := t.evaluate(ctx, expression)
		return object{"selected": true, "values": value}, e
	default:
		return nil, fmt.Errorf("unknown browser tool %q", tool)
	}
}
func (t toolSession) wait(ctx context.Context, expression string) error {
	for {
		value, e := t.evaluate(ctx, expression)
		if e != nil {
			return e
		}
		if b, _ := value.(bool); b {
			return nil
		}
		if e = pause(ctx, 100*time.Millisecond); e != nil {
			return e
		}
	}
}
func (t toolSession) focus(ctx context.Context, selector string) error {
	if selector == "" {
		return errors.New("selector is required")
	}
	v, e := t.evaluate(ctx, `(()=>{const e=document.querySelector(`+quote(selector)+`);if(!e)return false;e.scrollIntoView({block:'center',inline:'center'});e.focus();return document.activeElement===e})()`)
	if e != nil {
		return e
	}
	if v != true {
		return fmt.Errorf("element %q not found or not focusable", selector)
	}
	return nil
}
func (t toolSession) point(ctx context.Context, args object) (float64, float64, error) {
	if selector := str(args, "selector"); selector != "" {
		v, e := t.evaluate(ctx, `(()=>{const e=document.querySelector(`+quote(selector)+`);if(!e)throw Error('element not found');e.scrollIntoView({block:'center',inline:'center'});const r=e.getBoundingClientRect();if(!r.width||!r.height)throw Error('element is not visible');return {x:r.x+r.width/2,y:r.y+r.height/2}})()`)
		if e != nil {
			return 0, 0, e
		}
		point := obj(v)
		return number(point, "x", 0), number(point, "y", 0), nil
	}
	if _, ok := args["x"]; !ok {
		return 0, 0, errors.New("selector or coordinates are required")
	}
	return number(args, "x", 0), number(args, "y", 0), nil
}
func (t toolSession) move(ctx context.Context, x, y float64) error {
	rng := lcg(math.Float64bits(x) ^ math.Float64bits(y))
	x0, y0 := x-4, y-4
	jitter := (rng() - .5) * .3
	cx, cy := (x0+x)/2-(y-y0)*jitter, (y0+y)/2+(x-x0)*jitter
	for i := 1; i <= 12; i++ {
		u := math.Pow(float64(i)/12, .7)
		v := 1 - u
		px, py := v*v*x0+2*v*u*cx+u*u*x, v*v*y0+2*v*u*cy+u*u*y
		if _, e := t.call(ctx, "Input.dispatchMouseEvent", object{"type": "mouseMoved", "x": px, "y": py, "button": "none"}); e != nil {
			return e
		}
		if e := pause(ctx, 3*time.Millisecond); e != nil {
			return e
		}
	}
	return nil
}
func lcg(seed uint64) func() float64 {
	if seed == 0 {
		seed = 1
	}
	return func() float64 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return float64(seed>>33) / float64(uint64(1)<<31)
	}
}
func scrollSteps(delta float64, seed uint64) []float64 {
	n := int(math.Min(14, 6+math.Abs(delta)/120))
	rng := lcg(seed)
	values := make([]float64, n)
	total := 0.0
	for i := range values {
		values[i] = .5 + rng()
		total += values[i]
	}
	for i := range values {
		values[i] *= delta / total
	}
	return values
}

type keyDef struct {
	key, code string
	virtual   int
	text      string
}

func keyDefinition(key string) (keyDef, error) {
	defs := map[string]keyDef{"Enter": {"Enter", "Enter", 13, "\r"}, "Tab": {"Tab", "Tab", 9, ""}, "Backspace": {"Backspace", "Backspace", 8, ""}, "Delete": {"Delete", "Delete", 46, ""}, "Escape": {"Escape", "Escape", 27, ""}, "Esc": {"Escape", "Escape", 27, ""}, "ArrowLeft": {"ArrowLeft", "ArrowLeft", 37, ""}, "ArrowUp": {"ArrowUp", "ArrowUp", 38, ""}, "ArrowRight": {"ArrowRight", "ArrowRight", 39, ""}, "ArrowDown": {"ArrowDown", "ArrowDown", 40, ""}, "Home": {"Home", "Home", 36, ""}, "End": {"End", "End", 35, ""}, "PageUp": {"PageUp", "PageUp", 33, ""}, "PageDown": {"PageDown", "PageDown", 34, ""}, "Space": {" ", "Space", 32, " "}}
	if d, ok := defs[key]; ok {
		return d, nil
	}
	if utf8.RuneCountInString(key) == 1 {
		r, _ := utf8.DecodeRuneInString(key)
		code := ""
		vk := int(unicode.ToUpper(r))
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			code = "Key" + strings.ToUpper(key)
		} else if r >= '0' && r <= '9' {
			code = "Digit" + key
		}
		return keyDef{key, code, vk, key}, nil
	}
	if len(key) > 1 && key[0] == 'F' {
		var n int
		if _, e := fmt.Sscanf(key, "F%d", &n); e == nil && n >= 1 && n <= 12 {
			return keyDef{key, key, 111 + n, ""}, nil
		}
	}
	return keyDef{}, fmt.Errorf("unsupported key %q", key)
}
func (t toolSession) key(ctx context.Context, key string) error {
	parts := strings.Split(key, "+")
	mods := 0
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(part) {
		case "alt":
			mods |= 1
		case "ctrl", "control":
			mods |= 2
		case "meta", "cmd", "command":
			mods |= 4
		case "shift":
			mods |= 8
		case "controlormeta":
			v, e := t.evaluate(ctx, "navigator.platform")
			if e != nil {
				return e
			}
			if strings.HasPrefix(fmt.Sprint(v), "Mac") {
				mods |= 4
			} else {
				mods |= 2
			}
		default:
			return fmt.Errorf("unsupported key modifier %q", part)
		}
	}
	d, e := keyDefinition(parts[len(parts)-1])
	if e != nil {
		return e
	}
	params := object{"type": "keyDown", "key": d.key, "code": d.code, "windowsVirtualKeyCode": d.virtual, "modifiers": mods}
	if mods&6 != 0 && strings.EqualFold(d.key, "a") {
		params["commands"] = []string{"selectAll"}
	}
	if mods&7 == 0 && d.text != "" {
		params["text"] = d.text
	}
	if _, e = t.call(ctx, "Input.dispatchKeyEvent", params); e != nil {
		return e
	}
	delete(params, "text")
	delete(params, "commands")
	params["type"] = "keyUp"
	_, e = t.call(ctx, "Input.dispatchKeyEvent", params)
	return e
}
