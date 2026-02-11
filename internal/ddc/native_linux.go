//go:build linux

package ddc

import "fmt"

// Placeholder for Linux native DDC over /dev/i2c-* (DDC/CI over I2C).
// Implement by opening the correct I2C bus and issuing DDC/CI packets.
// For now, return a helpful message to use CLI backend (ddcutil).

type linuxNative struct{}

func NewLinuxNative() (Backend, error) {
	return &linuxNative{}, fmt.Errorf("native linux DDC not implemented; use CLI backend via ddcutil")
}

func (l *linuxNative) SetVCP(monitorID string, vcpCode string, value uint16) error {
	return fmt.Errorf("native linux DDC not implemented")
}
