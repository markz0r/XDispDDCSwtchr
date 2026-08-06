//go:build !darwin

package backend

import (
	"fmt"
	"runtime"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func newNative() (monitor.Backend, error) {
	return nil, fmt.Errorf("%w: native %s backend is not implemented in this release", monitor.ErrBackendUnavailable, runtime.GOOS)
}
