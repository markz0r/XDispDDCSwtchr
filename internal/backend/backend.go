package backend

import "github.com/markz0r/XDispDDCSwtchr/internal/monitor"

func New() (monitor.Backend, error) {
	return newNative()
}
