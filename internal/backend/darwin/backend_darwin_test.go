//go:build darwin && arm64

package darwin

import (
	"errors"
	"testing"

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
