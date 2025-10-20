package hotkeys

import (
	"strings"

	hook "github.com/robotn/gohook"
)

type Handler func()

func Register(bindings map[string]Handler) func() {
	evChan := hook.Start()
	stop := func() { hook.End() }
	go func() {
		pressed := map[string]bool{}
		for e := range evChan {
			switch e.Kind {
			case hook.KeyDown:
				k := normalizeKey(e)
				if k == "" {
					continue
				}
				pressed[k] = true
				for combo, handler := range bindings {
					if matchCombo(combo, pressed) {
						go handler()
					}
				}
			case hook.KeyUp:
				k := normalizeKey(e)
				if k == "" {
					continue
				}
				delete(pressed, k)
			}
		}
	}()
	return stop
}

func matchCombo(combo string, pressed map[string]bool) bool {
	parts := strings.Split(strings.ToLower(combo), "+")
	for _, p := range parts {
		if !pressed[p] {
			return false
		}
	}
	return true
}

func normalizeKey(e hook.Event) string {
	if e.Keychar != 0 {
		return strings.ToLower(string(e.Keychar))
	}
	if e.Keycode != 0 {
		return strings.ToLower(hook.RawcodetoKeychar(e.Keycode))
	}
	return ""
}
