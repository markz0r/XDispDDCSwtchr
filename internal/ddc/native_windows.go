//go:build windows

package ddc

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Minimal Windows native DDC using Dxva2.dll via syscall.
// We enumerate monitors and call SetVCPFeature on the first match.
// monitorID can be "DISPLAY1", "DISPLAY2" (index-style) or empty for first.

type winNative struct{}

func NewWinNative() Backend { return &winNative{} }

func (w *winNative) SetVCP(monitorID string, vcpCode string, value uint16) error {
	// Parse hex code like "0x60"
	var code uint32
	_, err := fmt.Sscanf(vcpCode, "0x%X", &code)
	if err != nil {
		return fmt.Errorf("bad vcp code '%s': %w", vcpCode, err)
	}

	// Load DLLs
	user32 := windows.NewLazySystemDLL("user32.dll")
	dxva2 := windows.NewLazySystemDLL("dxva2.dll")
	gacm := windows.NewLazySystemDLL("gdi32.dll")

	procEnumDisplayMonitors := user32.NewProc("EnumDisplayMonitors")
	procGetDC := user32.NewProc("GetDC")
	procReleaseDC := user32.NewProc("ReleaseDC")
	procGetNumberOfPhysicalMonitorsFromHMONITOR := dxva2.NewProc("GetNumberOfPhysicalMonitorsFromHMONITOR")
	procGetPhysicalMonitorsFromHMONITOR := dxva2.NewProc("GetPhysicalMonitorsFromHMONITOR")
	procDestroyPhysicalMonitors := dxva2.NewProc("DestroyPhysicalMonitors")
	procSetVCPFeature := dxva2.NewProc("SetVCPFeature")

	_ = gacm // keep ref; older Windows needed gdi32 linked when using HMONITOR

	type HMONITOR uintptr
	type HDC uintptr

	type PHYSICAL_MONITOR struct {
		Handle windows.Handle
		Desc   [128]uint16
	}

	var targetIndex int = -1
	if monitorID != "" {
		var i int
		_, err := fmt.Sscanf(monitorID, "DISPLAY%d", &i)
		if err == nil && i >= 1 {
			targetIndex = i - 1
		}
	}

	var chosen HMONITOR
	index := 0
	cb := windows.NewCallback(func(hMon HMONITOR, hdc HDC, lprc *struct{ left, top, right, bottom int32 }, data uintptr) uintptr {
		if targetIndex == -1 || index == targetIndex {
			chosen = hMon
			return 0 // stop enumeration
		}
		index++
		return 1 // continue
	})

	// EnumDisplayMonitors(NULL, NULL, MonitorEnumProc, 0)
	ret, _, callErr := procEnumDisplayMonitors.Call(0, 0, cb, 0)
	if ret == 0 {
		return fmt.Errorf("EnumDisplayMonitors failed: %v", callErr)
	}
	if chosen == 0 {
		return errors.New("no monitor found matching monitorID")
	}

	// Get number of physical monitors
	var count uint32
	ret, _, callErr = procGetNumberOfPhysicalMonitorsFromHMONITOR.Call(uintptr(chosen), uintptr(unsafe.Pointer(&count)))
	if ret == 0 {
		return fmt.Errorf("GetNumberOfPhysicalMonitorsFromHMONITOR failed: %v", callErr)
	}
	if count == 0 {
		return errors.New("no physical monitors")
	}

	arr := make([]PHYSICAL_MONITOR, count)
	ret, _, callErr = procGetPhysicalMonitorsFromHMONITOR.Call(uintptr(chosen), uintptr(count), uintptr(unsafe.Pointer(&arr[0])))
	if ret == 0 {
		return fmt.Errorf("GetPhysicalMonitorsFromHMONITOR failed: %v", callErr)
	}
	defer procDestroyPhysicalMonitors.Call(uintptr(count), uintptr(unsafe.Pointer(&arr[0])))

	// For simplicity, act on the first physical monitor on that HMONITOR
	pm := arr[0]
	ret, _, callErr = procSetVCPFeature.Call(uintptr(pm.Handle), uintptr(code), uintptr(value))
	if ret == 0 {
		return fmt.Errorf("SetVCPFeature failed: %v", callErr)
	}
	return nil
}
