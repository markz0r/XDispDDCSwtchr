//go:build darwin

package ddc

import "fmt"

// Placeholder for macOS native DDC (IOKit framebuffer I2C). Many open-source
// implementations exist, but require careful entitlement and transport setup.
// We expose a constructor now and return a clear error until implemented.

type darwinNative struct{}

func NewDarwinNative() (Backend, error) {
	return &darwinNative{}, fmt.Errorf("native macOS DDC not implemented yet; use CLI backend via ddcctl")
}

func (d *darwinNative) SetVCP(monitorID string, vcpCode string, value uint16) error {
	return fmt.Errorf("native macOS DDC not implemented")
}

