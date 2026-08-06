//go:build darwin && (!arm64 || !cgo)

package backend

import (
	"fmt"
	"runtime"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func newNative() (monitor.Backend, error) {
	requirement := "Apple Silicon and CGO"
	return nil, fmt.Errorf("%w: the macOS backend requires %s (running %s/%s)", monitor.ErrBackendUnavailable, requirement, runtime.GOOS, runtime.GOARCH)
}
