package ddc

import (
	"fmt"
	"runtime"

	"github.com/markz0r/XDispDDCSwtchr/internal/config"
)

type Backend interface {
	SetVCP(monitorID string, vcpCode string, value uint16) error
}

// MakeNative returns a native backend when available for the current OS.
func MakeNative(cfg *config.Settings) (Backend, error) {
	switch runtime.GOOS {
	case "windows":
		return NewWinNative(), nil
	case "darwin":
		return NewDarwinNative()
	case "linux":
		return NewLinuxNative()
	default:
		return nil, fmt.Errorf("no native backend for %s", runtime.GOOS)
	}
}

