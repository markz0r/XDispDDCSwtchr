package qualification

import (
	"strings"
	"testing"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
)

func TestRecordValidationMatchingAndProfile(t *testing.T) {
	descriptor := monitor.Descriptor{
		ID: "mon", EDIDSHA256: strings.Repeat("a", 64), Manufacturer: "DEL", ProductCode: 1,
		Model: "Display", Connector: "direct", BackendName: "fake",
	}
	record, err := New(descriptor, []monitor.InputValue{
		{Logical: monitor.InputHDMI1, Raw: 0x11},
		{Logical: monitor.InputDisplayPort1, Raw: 0x0f},
	}, "(vcp(60(0F 11)))", time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(record, descriptor) || record.Inputs[0].Logical != monitor.InputDisplayPort1 || record.AcceptedAt.Location() != time.UTC {
		t.Fatalf("unexpected record: %+v", record)
	}
	if !MatchesCapabilities(record, "(vcp(60(0F 11)))") || MatchesCapabilities(record, "(vcp(60(11)))") {
		t.Fatal("capability digest matching failed")
	}
	base := &profiles.Profile{ReadSupported: true, RetryCount: 7, ReplyDelay: time.Second, WriteDelay: 2 * time.Second, Verification: profiles.VerifyAfterReconnect}
	profile := Profile(record, descriptor, base)
	if profile.Name != "user-qualified/Display" || profile.Inputs[monitor.InputHDMI1] != 0x11 || profile.RetryCount != 7 || profile.Verification != profiles.VerifyAfterReconnect {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	changed := descriptor
	changed.Connector = "dock"
	if Matches(record, changed) {
		t.Fatal("record matched a different endpoint")
	}
	if Key(record) == DescriptorKey(changed) || Key(record) != DescriptorKey(descriptor) {
		t.Fatal("endpoint qualification keys are not exact")
	}
}

func TestValidateRejectsUnsafeRecords(t *testing.T) {
	valid := Record{
		MonitorID: "mon", EDIDSHA256: strings.Repeat("a", 64), Manufacturer: "DEL", ProductCode: 1,
		Connector: "direct", BackendName: "fake", CapabilitySHA256: strings.Repeat("b", 64), AcceptedAt: time.Unix(1, 0),
		Inputs: []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}},
	}
	tests := []struct {
		name string
		edit func(*Record)
	}{
		{"identity", func(record *Record) { record.MonitorID = "" }},
		{"edid-digest", func(record *Record) { record.EDIDSHA256 = "bad" }},
		{"capability-digest", func(record *Record) { record.CapabilitySHA256 = "bad" }},
		{"unknown-input", func(record *Record) { record.Inputs[0].Logical = "vga-1" }},
		{"wide-raw", func(record *Record) { record.Inputs[0].Raw = 0x100 }},
		{"duplicate-logical", func(record *Record) {
			record.Inputs = append(record.Inputs, monitor.InputValue{Logical: monitor.InputHDMI1, Raw: 0x12})
		}},
		{"duplicate-raw", func(record *Record) {
			record.Inputs = append(record.Inputs, monitor.InputValue{Logical: monitor.InputHDMI2, Raw: 0x11})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := valid
			record.Inputs = append([]monitor.InputValue(nil), valid.Inputs...)
			tt.edit(&record)
			if err := Validate(record); err == nil {
				t.Fatalf("accepted invalid record: %+v", record)
			}
		})
	}
}

func TestNewRejectsAmbiguousAndInvalidIdentity(t *testing.T) {
	descriptor := monitor.Descriptor{
		ID: "mon", EDIDSHA256: strings.Repeat("a", 64), Manufacturer: "DEL", ProductCode: 1,
		Model: "Display", Connector: "direct", BackendName: "fake", IdentityState: monitor.IdentityAmbiguous,
	}
	inputs := []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}
	if _, err := New(descriptor, inputs, "(vcp(60(11)))", time.Now()); err != monitor.ErrAmbiguousMonitor {
		t.Fatalf("ambiguous identity error=%v", err)
	}
	descriptor.IdentityState = monitor.IdentityInvalidEDID
	if _, err := New(descriptor, inputs, "(vcp(60(11)))", time.Now()); err != monitor.ErrUnsupportedMonitor {
		t.Fatalf("invalid EDID identity error=%v", err)
	}
}
