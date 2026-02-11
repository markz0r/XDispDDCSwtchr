//go:build !darwin

package ddc

import "fmt"

func NewDarwinNative() (Backend, error) {
	return nil, fmt.Errorf("native macOS DDC not supported on this build")
}
