//go:build darwin && arm64 && cgo

package backend

import darwinbackend "github.com/markz0r/XDispDDCSwtchr/internal/backend/darwin"
import "github.com/markz0r/XDispDDCSwtchr/internal/monitor"

func newNative() (monitor.Backend, error) {
	return darwinbackend.New()
}
