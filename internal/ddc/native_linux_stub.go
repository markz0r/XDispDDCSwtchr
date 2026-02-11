//go:build !linux

package ddc

import "fmt"

func NewLinuxNative() (Backend, error) {
	return nil, fmt.Errorf("native linux DDC not supported on this build")
}
