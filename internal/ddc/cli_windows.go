//go:build windows

package ddc

import (
	"bytes"
	"fmt"
	"os/exec"
)

type CLIBackend struct {
	LinuxDdcutilPath string // ignored on windows
	MacDdcctlPath    string // ignored on windows
	WinControlMyMonPath string
}

func (b *CLIBackend) SetVCP(monitorID string, vcpCode string, value uint16) error {
	tool := b.WinControlMyMonPath
	if tool == "" { tool = "ControlMyMonitor.exe" }
	cmd := exec.Command(tool, "/SetValue", monitorID, vcpCode, fmt.Sprint(value))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ControlMyMonitor failed: %v (%s)", err, errb.String())
	}
	return nil
}

