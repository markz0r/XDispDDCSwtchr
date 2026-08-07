//go:build darwin && arm64

package darwin

import (
	"errors"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestConnectorName(t *testing.T) {
	tests := map[string]string{
		"IOService:/foo/dcp@2E00000/bar":    "external-dcp",
		"IOService:/foo/dcpext0@A2E/bar":    "external-dcpext0",
		"IOService:/unrecognised/transport": "external",
	}
	for path, want := range tests {
		if got := connectorName(path); got != want {
			t.Fatalf("connectorName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestParseCoreDisplayGetVCPReplyStandardAndCompact(t *testing.T) {
	standard := []byte{0x6e, 0x88, 0x02, 0x00, 0x60, 0x00, 0x19, 0x19, 0x19, 0x19, 0xd4}
	compact := []byte{0x00, 0x60, 0x00, 0x19, 0x19, 0x19, 0x19, 0xd4, 0x00, 0x00, 0x00}
	for name, frame := range map[string][]byte{"standard": standard, "compact": compact} {
		t.Run(name, func(t *testing.T) {
			parsed, err := parseCoreDisplayGetVCPReply(frame, ddc.VCPInputSource)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if parsed.Current != 0x1919 {
				t.Fatalf("current = 0x%04x", parsed.Current)
			}
		})
	}
}

func TestParseCoreDisplayGetVCPReplyRejectsMalformedCompactForms(t *testing.T) {
	tests := map[string][]byte{
		"non-zero tail": {0x00, 0x60, 0x00, 0x19, 0x19, 0x19, 0x19, 0xd4, 0x01, 0x00, 0x00},
		"bad checksum":  {0x00, 0x60, 0x00, 0x19, 0x19, 0x19, 0x19, 0xd5, 0x00, 0x00, 0x00},
		"wrong code":    {0x00, 0x61, 0x00, 0x19, 0x19, 0x19, 0x19, 0xd5, 0x00, 0x00, 0x00},
	}
	for name, frame := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseCoreDisplayGetVCPReply(frame, ddc.VCPInputSource)
			if !errors.Is(err, monitor.ErrMalformedReply) && !errors.Is(err, monitor.ErrChecksum) {
				t.Fatalf("expected protocol error, got %v", err)
			}
			raw, ok := ddc.ReplyBytes(err)
			if !ok || len(raw) != len(frame) {
				t.Fatalf("raw reply not retained: %x", raw)
			}
		})
	}
}

func TestNativeErrorClassification(t *testing.T) {
	if err := nativeError(-2, "missing"); !errors.Is(err, monitor.ErrPrivateSymbolUnavailable) {
		t.Fatalf("expected private symbol error, got %v", err)
	}
	if err := nativeError(-6, "gone"); !errors.Is(err, monitor.ErrEndpointNotFound) {
		t.Fatalf("expected endpoint error, got %v", err)
	}
	if err := nativeError(-9, "ambiguous"); !errors.Is(err, monitor.ErrAmbiguousMonitor) {
		t.Fatalf("expected ambiguous monitor error, got %v", err)
	}
}
