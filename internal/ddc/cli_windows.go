//go:build windows

package ddc

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"time"
)

// Security: Validate inputs to prevent command injection
var (
	safeMonitorIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_.\\-]+$`)
	safeVCPCodeRegex   = regexp.MustCompile(`^(0x)?[0-9a-fA-F]+$`)
)

func validateInputSafety(monitorID, vcpCode string, value uint16) error {
	// Monitor ID should only contain safe characters (Windows allows backslashes in device paths)
	if monitorID != "" && !safeMonitorIDRegex.MatchString(monitorID) {
		return fmt.Errorf("invalid monitor ID '%s': contains unsafe characters", monitorID)
	}

	// VCP code should only be hex/decimal
	if !safeVCPCodeRegex.MatchString(vcpCode) {
		return fmt.Errorf("invalid VCP code '%s': must be hex (0x60) or decimal (96)", vcpCode)
	}

	return nil
}

type CLIBackend struct {
	LinuxDdcutilPath    string // ignored on windows
	MacDdcctlPath       string // ignored on windows
	WinControlMyMonPath string
}

func (b *CLIBackend) SetVCP(monitorID string, vcpCode string, value uint16) error {
	// Security: Validate inputs to prevent command injection
	if err := validateInputSafety(monitorID, vcpCode, value); err != nil {
		return err
	}

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
