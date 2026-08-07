//go:build qualification

package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/platform"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
	"github.com/markz0r/XDispDDCSwtchr/internal/testutil"
)

func qualificationBackend() (*testutil.FakeBackend, exactMonitor) {
	descriptor := monitor.Descriptor{
		ID: "mon-test", Manufacturer: "DEL", ProductCode: 0x4308, Model: "DELL U4025QW",
		EDIDSHA256: "expected-hash", Connector: "test", BackendName: "fake",
	}
	return &testutil.FakeBackend{Descriptors: []monitor.Descriptor{descriptor}, Inputs: map[string]uint16{"mon-test": 0x1b}}, exactMonitor{
		ID: descriptor.ID, Model: descriptor.Model, EDIDSHA256: descriptor.EDIDSHA256,
	}
}

func TestCaptureReadsOnlySelectedExactMonitor(t *testing.T) {
	backend, expected := qualificationBackend()
	record, err := capture(context.Background(), backend, platform.Info{OS: "test"}, captureOptions{exactMonitor: expected, Label: monitor.InputThunderbolt1, OSDLabel: "Thunderbolt (140W)"})
	if err != nil {
		t.Fatal(err)
	}
	if record.ObservedRaw == nil || *record.ObservedRaw != 0x1b || record.OSDLabel != "Thunderbolt (140W)" || len(backend.SetCalls) != 0 {
		t.Fatalf("unexpected capture: record=%+v sets=%+v", record, backend.SetCalls)
	}
}

func TestPreflightExplainsAutomaticAndOperatorFieldsWithoutWriting(t *testing.T) {
	backend, expected := qualificationBackend()
	record, err := preflight(context.Background(), backend, platform.Info{OS: "test"}, expected)
	if err != nil {
		t.Fatal(err)
	}
	if record.Operation != "input-preflight" || record.ObservedRaw == nil || *record.ObservedRaw != 0x1b || len(record.OSDInputs) != 3 {
		t.Fatalf("unexpected preflight record: %+v", record)
	}
	if record.Determination == nil || record.Determination.CurrentRaw.Status != "automatic" ||
		record.Determination.CurrentOSDLabel.Status != "operator-confirmation-required" || record.Determination.AutomaticRawWrites {
		t.Fatalf("unexpected determination assessment: %+v", record.Determination)
	}
	if len(backend.SetCalls) != 0 {
		t.Fatalf("preflight wrote input values: %+v", backend.SetCalls)
	}
}

func TestModelSpecificInputChoices(t *testing.T) {
	tests := []struct {
		model string
		want  []osdInputChoice
	}{
		{"DELL U4025QW", []osdInputChoice{{"Thunderbolt (140W)", monitor.InputThunderbolt1}, {"DP", monitor.InputDisplayPort1}, {"HDMI", monitor.InputHDMI1}}},
		{"S3423DWC", []osdInputChoice{{"USB-C", monitor.InputUSBC1}, {"HDMI 1", monitor.InputHDMI1}, {"HDMI 2", monitor.InputHDMI2}}},
		{"LG ULTRAGEAR+", []osdInputChoice{{"HDMI-1", monitor.InputHDMI1}, {"HDMI-2", monitor.InputHDMI2}, {"DisplayPort-1", monitor.InputDisplayPort1}, {"DisplayPort-2", monitor.InputDisplayPort2}}},
		{"unknown", nil},
	}
	for _, test := range tests {
		got := osdInputs(test.model)
		if len(got) != len(test.want) {
			t.Fatalf("%s: got %+v, want %+v", test.model, got, test.want)
		}
		for i := range got {
			if got[i] != test.want[i] {
				t.Fatalf("%s[%d]: got %+v, want %+v", test.model, i, got[i], test.want[i])
			}
		}
	}
}

func TestParseCaptureRequiresVisibleLabelAndValidModelInput(t *testing.T) {
	base := []string{
		"--monitor", "mon-test",
		"--expected-model", "DELL U4025QW",
		"--expected-edid-sha256", "expected-hash",
	}
	valid := append(append([]string{}, base...), "--label", "displayport-1", "--osd-label", "DP")
	options, err := parseCapture(valid, os.Stderr)
	if err != nil || options.Label != monitor.InputDisplayPort1 || options.OSDLabel != "DP" {
		t.Fatalf("valid capture arguments rejected: options=%+v err=%v", options, err)
	}
	missingOSD := append(append([]string{}, base...), "--label", "displayport-1")
	if _, err := parseCapture(missingOSD, os.Stderr); err == nil {
		t.Fatal("capture without the visible OSD label was accepted")
	}
	impossible := append(append([]string{}, base...), "--label", "hdmi-2", "--osd-label", "HDMI 2")
	if _, err := parseCapture(impossible, os.Stderr); err == nil {
		t.Fatal("model-specific impossible input was accepted")
	}
}

func TestGuardedWriteRequiresDeclaredSourceToMatch(t *testing.T) {
	registry, err := profiles.NewRegistry(profiles.Profile{
		Name: "Dell U4025QW", Match: profiles.EDIDMatcher{Manufacturer: "DEL", ProductCode: 0x4308, ModelNames: []string{"DELL U4025QW"}},
		Inputs:        map[monitor.Input]uint16{monitor.InputUSBC1: 0x1b, monitor.InputHDMI1: 0x11},
		ReadSupported: true, WriteDelay: 0, Verification: profiles.VerifyImmediate,
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, expected := qualificationBackend()
	options := writeOptions{
		exactMonitor: expected, Source: monitor.InputUSBC1, Target: monitor.InputHDMI1,
		UnsafeAllowWrite: true, AcknowledgeSwitch: true, RecoveryMethod: "monitor OSD",
	}
	record, err := write(context.Background(), backend, registry, platform.Info{OS: "test"}, options)
	if err != nil {
		t.Fatal(err)
	}
	if record.Verification != monitor.VerificationConfirmed || len(backend.SetCalls) != 1 || backend.SetCalls[0].Value != 0x11 {
		t.Fatalf("unexpected write: record=%+v sets=%+v", record, backend.SetCalls)
	}

	backend, _ = qualificationBackend()
	backend.Inputs["mon-test"] = 0x11
	if _, err := write(context.Background(), backend, registry, platform.Info{}, options); err == nil || len(backend.SetCalls) != 0 {
		t.Fatalf("source mismatch was not rejected: err=%v sets=%+v", err, backend.SetCalls)
	}
}

func TestGuardedWriteRecordsAssumedSuccessButRejectsQualificationEvidence(t *testing.T) {
	registry, err := profiles.NewRegistry(profiles.Profile{
		Name: "Dell U4025QW", Match: profiles.EDIDMatcher{Manufacturer: "DEL", ProductCode: 0x4308, ModelNames: []string{"DELL U4025QW"}},
		Inputs: map[monitor.Input]uint16{monitor.InputUSBC1: 0x1b, monitor.InputHDMI1: 0x11}, ReadSupported: true,
		RetryCount: 1, ReplyDelay: 0, WriteDelay: 0, Verification: profiles.VerifyImmediate,
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, expected := qualificationBackend()
	malformed := []byte{0x00, 0x60, 0x00, 0x19, 0x19, 0x19, 0x19, 0x00, 0x00, 0x00, 0x00}
	_, readErr := ddc.ParseGetVCPReply(malformed, ddc.VCPInputSource)
	backend.GetErrors = []error{nil, readErr, readErr}
	options := writeOptions{
		exactMonitor: expected, Source: monitor.InputUSBC1, Target: monitor.InputHDMI1,
		UnsafeAllowWrite: true, AcknowledgeSwitch: true, RecoveryMethod: "monitor OSD",
	}
	record, err := write(context.Background(), backend, registry, platform.Info{OS: "test"}, options)
	if !errors.Is(err, monitor.ErrQualificationIncomplete) {
		t.Fatalf("expected incomplete qualification, got %v", err)
	}
	if record.Verification != monitor.VerificationAssumedSuccess || record.VerificationAttempts != 2 || record.VerificationIssue == nil {
		t.Fatalf("record=%+v", record)
	}
	if record.VerificationIssue.RawReplyHex != "0060001919191900000000" || len(backend.SetCalls) != 1 {
		t.Fatalf("record=%+v set calls=%d", record, len(backend.SetCalls))
	}
}

func TestSelectExactRejectsIdentityMismatch(t *testing.T) {
	backend, expected := qualificationBackend()
	expected.EDIDSHA256 = "wrong"
	if _, _, err := selectExact(context.Background(), backend, expected); err == nil {
		t.Fatal("identity mismatch accepted")
	}
}
