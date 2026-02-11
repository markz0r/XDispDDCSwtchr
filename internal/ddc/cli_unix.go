//go:build darwin || linux

package ddc

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type CLIBackend struct {
	LinuxDdcutilPath    string
	MacDdcctlPath       string
	WinControlMyMonPath string // ignored on unix
}

func (b *CLIBackend) SetVCP(monitorID string, vcpCode string, value uint16) error {
	// Validate VCP code format
	normalizedCode := normalizeVCPCode(vcpCode)
	if normalizedCode == "" {
		return fmt.Errorf("invalid VCP code '%s': must be hex (0x60) or decimal (96)", vcpCode)
	}

	switch runtime.GOOS {
	case "linux":
		tool := b.LinuxDdcutilPath
		if tool == "" {
			tool = "ddcutil"
		}
		// Validate monitor ID if provided
		if monitorID != "" {
			log.Printf("ddcutil: setting VCP %s=%d (monitor ID ignored, affects all monitors)", vcpCode, value)
		}

		cmd := exec.Command(tool, "setvcp", vcpCode, fmt.Sprint(value))
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb

		// Add timeout to prevent hanging
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd = exec.CommandContext(ctx, tool, "setvcp", vcpCode, fmt.Sprint(value))
		cmd.Stdout, cmd.Stderr = &out, &errb

		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("ddcutil timed out after 30s - monitor may not support DDC/CI")
			}
			return fmt.Errorf("ddcutil failed: %v (%s)", err, errb.String())
		}
		return nil
	case "darwin":
		tool := b.MacDdcctlPath
		if tool == "" {
			tool = "ddcctl"
		}
		displayID, err := darwinDisplayID(tool, monitorID)
		if err != nil {
			return fmt.Errorf("failed to resolve monitor ID '%s': %w", monitorID, err)
		}

		code := normalizeVCPCode(vcpCode)
		if code == "60" {
			// Common ddcctl builds use -i to select input source.
			if err := runCommandWithTimeout(tool, 30*time.Second, "-d", displayID, "-i", fmt.Sprint(value)); err != nil {
				return fmt.Errorf("ddcctl input switch failed: %w", err)
			}
			return nil
		}

		if err := runCommandWithTimeout(tool, 30*time.Second, "-d", displayID, "-c", vcpCode, "-v", fmt.Sprint(value)); err != nil {
			return fmt.Errorf("ddcctl generic VCP failed: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported unix OS: %s", runtime.GOOS)
	}
}

func runCommand(tool string, args ...string) error {
	return runCommandWithTimeout(tool, 30*time.Second, args...)
}

func runCommandWithTimeout(tool string, timeout time.Duration, args ...string) error {
	stdout, stderr, err := runCommandCaptureWithTimeout(tool, timeout, args...)
	if err != nil {
		hint := darwinDDCctlHint(stdout, stderr)
		if hint != "" {
			return fmt.Errorf("exec %s %s: %v (stdout=%q stderr=%q) hint=%s", tool, strings.Join(args, " "), err, stdout, stderr, hint)
		}
		return fmt.Errorf("exec %s %s: %v (stdout=%q stderr=%q)", tool, strings.Join(args, " "), err, stdout, stderr)
	}
	return nil
}

func runCommandCapture(tool string, args ...string) (string, string, error) {
	return runCommandCaptureWithTimeout(tool, 30*time.Second, args...)
}

func runCommandCaptureWithTimeout(tool string, timeout time.Duration, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return "", "", fmt.Errorf("command timed out after %v", timeout)
	}

	return strings.TrimSpace(out.String()), strings.TrimSpace(errb.String()), err
}

func darwinDisplayID(tool, monitorID string) (string, error) {
	const fallback = "1"
	trimmed := strings.TrimSpace(monitorID)
	if trimmed == "" {
		log.Printf("warning: no monitor ID specified, using default display 1")
		return fallback, nil
	}

	if _, err := strconv.Atoi(trimmed); err == nil {
		return mapDarwinDisplayArg(tool, trimmed), nil
	}

	sep := strings.LastIndexAny(trimmed, "@#")
	if sep <= 0 || sep >= len(trimmed)-1 {
		log.Printf("warning: could not parse monitor ID '%s', using default display 1", trimmed)
		return fallback, nil
	}

	candidate := trimmed[sep+1:]
	if _, err := strconv.Atoi(candidate); err == nil {
		return mapDarwinDisplayArg(tool, candidate), nil
	}

	log.Printf("warning: could not extract display number from '%s', using default display 1", trimmed)
	return fallback, nil
}

func mapDarwinDisplayArg(tool, requested string) string {
	// ddcctl's -d typically expects 1..N external display index.
	// If requested number is larger than N but matches a reported dispID(#X),
	// map it to ordinal index so targets like "@4" still work.
	req, err := strconv.Atoi(requested)
	if err != nil || req <= 0 {
		return requested
	}

	dispIDs := darwinDispIDs(tool)
	if len(dispIDs) == 0 {
		return requested
	}
	if req <= len(dispIDs) {
		return requested
	}
	for i, id := range dispIDs {
		if id == requested {
			return strconv.Itoa(i + 1)
		}
	}
	return requested
}

func darwinDispIDs(tool string) []string {
	stdout, stderr, _ := runCommandCapture(tool)
	msg := stdout + "\n" + stderr
	re := regexp.MustCompile(`dispID\(#([0-9]+)\)`)
	matches := re.FindAllStringSubmatch(msg, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) == 2 {
			out = append(out, m[1])
		}
	}
	return out
}

func normalizeVCPCode(vcpCode string) string {
	s := strings.TrimSpace(strings.ToLower(vcpCode))
	if s == "" {
		return ""
	}

	// Support both hex (0x60) and decimal (96) formats
	s = strings.TrimPrefix(s, "0x")

	// Validate it's a valid hex string
	if _, err := strconv.ParseUint(s, 16, 16); err != nil {
		return ""
	}

	return s
}

func darwinDDCctlHint(stdout, stderr string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	msg := stdout + "\n" + stderr
	if !strings.Contains(msg, "Failed to parse WindowServer's preferences") {
		return ""
	}
	if _, err := os.Stat("/Library/Preferences/com.apple.windowserver.plist"); err == nil {
		return ""
	}
	if _, err := os.Stat("/Library/Preferences/com.apple.windowserver.displays.plist"); err == nil {
		return "ddcctl expects /Library/Preferences/com.apple.windowserver.plist but macOS has com.apple.windowserver.displays.plist; create a symlink to restore compatibility"
	}
	return ""
}
