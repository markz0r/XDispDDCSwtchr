package hotkeys

import (
	"log"
	"os"
	"strings"

	hook "github.com/robotn/gohook"
)

type Handler func()

type comboBinding struct {
	combo   string
	keySets [][]uint16
	handler Handler
}

func Register(bindings map[string]Handler) func() {
	debug := hotkeyDebugEnabled()
	parsed := make([]comboBinding, 0, len(bindings))

	for combo, handler := range bindings {
		keySets, unknown := parseCombo(combo)
		if len(keySets) == 0 {
			log.Printf("hotkeys: skipping empty combo %q", combo)
			continue
		}
		if len(unknown) > 0 {
			log.Printf("hotkeys: skipping combo %q (unknown keys: %s)", combo, strings.Join(unknown, ","))
			continue
		}
		parsed = append(parsed, comboBinding{combo: combo, keySets: keySets, handler: handler})
		if debug {
			log.Printf("hotkeys: registered combo=%q keysets=%v", combo, keySets)
		}
	}

	evChan := hook.Start()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pressed := map[uint16]bool{}
		fired := map[string]bool{}
		for e := range evChan {
			switch e.Kind {
			case hook.KeyDown, hook.KeyHold:
				pressed[e.Keycode] = true
				if debug {
					log.Printf("hotkeys: event kind=%d keycode=%d rawcode=%d keychar=%q mask=0x%x", e.Kind, e.Keycode, e.Rawcode, e.Keychar, e.Mask)
				}
				for _, cb := range parsed {
					matched := comboMatched(cb.keySets, pressed)
					if matched && !fired[cb.combo] {
						fired[cb.combo] = true
						log.Printf("hotkeys: matched %q", cb.combo)
						go cb.handler()
					}
					if !matched {
						fired[cb.combo] = false
					}
				}
			case hook.KeyUp:
				delete(pressed, e.Keycode)
				if debug {
					log.Printf("hotkeys: event kind=%d keycode=%d rawcode=%d keychar=%q mask=0x%x", e.Kind, e.Keycode, e.Rawcode, e.Keychar, e.Mask)
				}
				for _, cb := range parsed {
					if !comboMatched(cb.keySets, pressed) {
						fired[cb.combo] = false
					}
				}
			}
		}
	}()

	return func() {
		hook.End()
		<-done
	}
}

func parseCombo(combo string) ([][]uint16, []string) {
	parts := strings.Split(strings.ToLower(combo), "+")
	keySets := make([][]uint16, 0, len(parts))
	unknown := make([]string, 0)
	for _, part := range parts {
		k := strings.TrimSpace(part)
		if k == "" {
			continue
		}
		codes := keyCodesForToken(k)
		if len(codes) == 0 {
			unknown = append(unknown, k)
			continue
		}
		keySets = append(keySets, codes)
	}
	return keySets, unknown
}

func keyCodesForToken(token string) []uint16 {
	switch token {
	case "control":
		token = "ctrl"
	case "option":
		token = "alt"
	case "command", "meta":
		token = "cmd"
	}

	unique := map[uint16]bool{}
	add := func(code uint16) {
		if code != 0 {
			unique[code] = true
		}
	}

	switch token {
	case "ctrl":
		add(hook.Keycode["ctrl"])
		add(3613) // libuiohook right-control code
	case "alt":
		add(hook.Keycode["alt"])
		add(hook.Keycode["ralt"])
	case "shift":
		add(hook.Keycode["shift"])
		add(hook.Keycode["rshift"])
	case "cmd":
		add(hook.Keycode["cmd"])
		add(hook.Keycode["rcmd"])
	default:
		add(hook.Keycode[token])
	}

	out := make([]uint16, 0, len(unique))
	for code := range unique {
		out = append(out, code)
	}
	return out
}

func comboMatched(keySets [][]uint16, pressed map[uint16]bool) bool {
	for _, set := range keySets {
		found := false
		for _, code := range set {
			if pressed[code] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hotkeyDebugEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("XDISP_DEBUG_HOTKEYS")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
