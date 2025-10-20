//go:build darwin || linux

package ddc

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
)

type CLIBackend struct {
	LinuxDdcutilPath string
	MacDdcctlPath    string
	WinControlMyMonPath string // ignored on unix
}

func (b *CLIBackend) SetVCP(monitorID string, vcpCode string, value uint16) error {
	switch runtime.GOOS {
	case "linux":
		tool := b.LinuxDdcutilPath
		if tool == "" { tool = "ddcutil" }
		cmd := exec.Command(tool, "setvcp", vcpCode, fmt.Sprint(value))
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ddcutil failed: %v (%s)", err, errb.String())
		}
		return nil
	case "darwin":
		tool := b.MacDdcctlPath
		if tool == "" { tool = "ddcctl" }
		cmd := exec.Command(tool, "-d", "1", "-c", vcpCode, "-v", fmt.Sprint(value))
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ddcctl failed: %v (%s)", err, errb.String())
		}
		return nil
	default:
		return fmt.Errorf("unsupported unix OS: %s", runtime.GOOS)
	}
}
