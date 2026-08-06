package diagnostics

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestBuildRedactsSerialAndEDID(t *testing.T) {
	raw := make([]byte, 128)
	raw[127] = 0
	bundle := Build(BuildOptions{
		Now: time.Unix(10, 0), Redact: true,
		Descriptor: monitor.Descriptor{ID: "mon-a", Serial: "secret", EDID: raw, EDIDSHA256: "hash"},
	})
	encoded, err := Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "secret") || strings.Contains(text, strings.Repeat("00", 128)) {
		t.Fatalf("diagnostic contains unredacted data: %s", text)
	}
	if !strings.Contains(text, `"schema_version": 1`) || !strings.Contains(text, `"vcp_code": "0x60"`) {
		t.Fatalf("diagnostic lacks schema fields: %s", text)
	}
}

func TestBuildUnredactedCurrentAndErrorStates(t *testing.T) {
	raw := make([]byte, 128)
	raw[0] = 1
	current := monitor.InputValue{Logical: monitor.InputHDMI1, Raw: 0x11}
	bundle := Build(BuildOptions{
		Now: time.Unix(20, 0), Version: "1", Commit: "commit", Redact: false,
		Descriptor: monitor.Descriptor{ID: "mon", Serial: "visible", EDID: raw, EDIDSHA256: "hash", BackendName: "fake"},
		Current:    &current, Inputs: []monitor.InputValue{current}, ReadDuration: 12 * time.Millisecond,
		ReadError: errors.New("read failed"), QualificationRecord: "record.json",
	})
	if bundle.Redaction.Enabled || bundle.Monitor.Serial != "visible" || bundle.EDID.Raw == "" || bundle.EDID.ChecksumValid {
		t.Fatalf("unexpected unredacted bundle: %+v", bundle)
	}
	if bundle.InputSource.CurrentRaw == nil || *bundle.InputSource.CurrentRaw != 0x11 || bundle.InputSource.ReadSupported ||
		len(bundle.Errors) != 1 || bundle.Timings.ReadMilliseconds != 12 || bundle.Support.QualificationRecord != "record.json" {
		t.Fatalf("unexpected input/error state: %+v", bundle)
	}
	if got := redact(""); got != "" {
		t.Fatalf("empty redaction=%q", got)
	}
}
