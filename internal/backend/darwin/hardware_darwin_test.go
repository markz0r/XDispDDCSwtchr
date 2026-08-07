//go:build darwin && arm64

package darwin

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/capabilities"
	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

// TestHardwareReadOnly is opt-in because CI has no monitors. It enumerates,
// reads VCP 0x60, and retrieves the capability string; it performs no write.
func TestHardwareReadOnly(t *testing.T) {
	if os.Getenv("XDISP_HARDWARE_READ") != "1" {
		t.Skip("set XDISP_HARDWARE_READ=1 on an explicitly selected host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	probe, probeErr := discoveryProbe()
	if probeErr != nil {
		t.Logf("discovery probe failed: %v", probeErr)
	} else {
		t.Logf("discovery probe:\n%s", probe)
	}
	snapshot, err := b.Enumerate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, display := range snapshot.Monitors {
		t.Logf("id=%s model=%q product=0x%04x connector=%s edid_sha256=%s", display.ID, display.Model, display.ProductCode, display.Connector, display.EDIDSHA256)
		opened, err := b.Open(ctx, monitor.MonitorRef{ID: display.ID, Generation: snapshot.Generation})
		if err != nil {
			t.Errorf("open %s: %v", display.Model, err)
			continue
		}
		nativeSession, ok := opened.(*session)
		if !ok {
			t.Fatalf("unexpected native session type %T", opened)
		}
		reply := make([]byte, 11)
		readErr := transaction(nativeSession.handle, ddc.EncodeGetVCP(ddc.VCPInputSource), reply, nativeSession.replyDelay)
		t.Logf("model=%q raw_reply=% x", display.Model, reply)
		var raw uint16
		if readErr == nil {
			parsed, parseErr := ddc.ParseGetVCPReply(reply, ddc.VCPInputSource)
			readErr = parseErr
			raw = ddc.NormalizeInputSourceValue(parsed.Current)
		}
		capabilityString, capabilityErr := nativeSession.Capabilities(ctx)
		if capabilityErr == nil {
			parsedCapabilities, parseErr := capabilities.Parse(capabilityString)
			if parseErr != nil {
				capabilityErr = parseErr
			} else if !parsedCapabilities.InputSourceAdvertised || len(parsedCapabilities.InputValues) == 0 {
				capabilityErr = monitor.ErrCapabilitiesUnsupported
			} else {
				t.Logf("model=%q capability_input_values=%#x", display.Model, parsedCapabilities.InputValues)
			}
		}
		closeErr := opened.Close()
		if readErr != nil {
			t.Errorf("read %s: %v", display.Model, readErr)
		} else {
			t.Logf("model=%q current_raw=0x%02x", display.Model, raw)
		}
		if capabilityErr != nil {
			t.Errorf("capabilities %s: %v", display.Model, capabilityErr)
		}
		if closeErr != nil {
			t.Errorf("close %s: %v", display.Model, closeErr)
		}
	}
	if len(snapshot.Monitors) != 2 {
		t.Fatalf("enumerated %d correlated monitors, want exactly 2", len(snapshot.Monitors))
	}
}
