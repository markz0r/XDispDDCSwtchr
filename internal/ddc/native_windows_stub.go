//go:build !windows

package ddc

func NewWinNative() Backend { return nil }
