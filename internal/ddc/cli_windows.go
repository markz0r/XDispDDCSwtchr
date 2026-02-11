//go:build windows

package ddc

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type CLIBackend struct {
	LinuxDdcutilPath    string // ignored on windows
	MacDdcctlPath       string // ignored on windows
	WinControlMyMonPath string
}

func (b *CLIBackend) SetVCP(monitorID string, vcpCode string, value uint16) error {
	tool := b.WinControlMyMonPath
	if tool == "" {
		tool = "ControlMyMonitor.exe"
	}

	// Validate VCP code format (support both 0x60 and 96)
	if vcpCode == "" {
		return fmt.Errorf("invalid VCP code: empty")
	}

	// Add timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool, "/SetValue", monitorID, vcpCode, fmt.Sprint(value))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ControlMyMonitor timed out after 30s - monitor may not support DDC/CI")
		}
		return fmt.Errorf("ControlMyMonitor failed: %v (stdout=%s stderr=%s)", err, out.String(), errb.String())
	}
	return nil
}
