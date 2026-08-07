package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
	"github.com/markz0r/XDispDDCSwtchr/internal/qualification"
	"github.com/markz0r/XDispDDCSwtchr/internal/testutil"
)

type noSleep struct{}

func (noSleep) Sleep(context.Context, time.Duration) error { return nil }

type recordingSleeper struct {
	delays []time.Duration
}

type failingSleeper struct{ err error }

func (s failingSleeper) Sleep(context.Context, time.Duration) error { return s.err }

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func (s *recordingSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.delays = append(s.delays, delay)
	return nil
}

func newTestService(t *testing.T) (*Service, *testutil.FakeBackend, monitor.MonitorRef) {
	t.Helper()
	backend := &testutil.FakeBackend{
		Descriptors: []monitor.Descriptor{{ID: "mon-test", Manufacturer: "DEL", ProductCode: 0x4308, Model: "DELL U4025QW", EDIDSHA256: "hash", Connector: "test", BackendName: "fake"}},
		Inputs:      map[string]uint16{"mon-test": 0x1b},
	}
	registry, err := profiles.NewRegistry(profiles.Profile{
		Name: "Dell U4025QW", Match: profiles.EDIDMatcher{Manufacturer: "DEL", ProductCode: 0x4308, ModelNames: []string{"DELL U4025QW"}},
		Inputs:        map[monitor.Input]uint16{monitor.InputUSBC1: 0x1b, monitor.InputHDMI1: 0x11, monitor.InputDisplayPort1: 0x0f},
		ReadSupported: true, RetryCount: 3, ReplyDelay: 50 * time.Millisecond, WriteDelay: 150 * time.Millisecond, Verification: profiles.VerifyImmediate,
	})
	if err != nil {
		t.Fatal(err)
	}
	matrix := &profiles.SupportMatrix{SchemaVersion: 1, Records: []profiles.SupportRecord{{
		ID: "test", OS: "test", Architecture: "test", OSVersion: "1", OSBuild: "1", Backend: "fake", ApplicationCommit: "test-commit", Profile: "Dell U4025QW", EDIDSHA256: "hash", Connector: "test", Inputs: []string{"usb-c-1", "hdmi-1"}, Verification: profiles.VerifyImmediate, QualificationRecord: "evidence.json", EvidenceSHA256: "evidence-hash",
	}}}
	svc := New(backend, registry, matrix, profiles.Platform{OS: "test", Architecture: "test", Version: "1", Build: "1", ApplicationCommit: "test-commit"})
	svc.SetSleeperForTest(noSleep{})
	snapshot, err := svc.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return svc, backend, monitor.MonitorRef{ID: "mon-test", Generation: snapshot.Generation}
}

func TestDiscoverDecoratesQualifiedProfile(t *testing.T) {
	svc, _, _ := newTestService(t)
	snapshot := svc.Snapshot()
	if got := snapshot.Monitors[0]; got.ProfileName != "Dell U4025QW" || got.SupportState != monitor.SupportSupported {
		t.Fatalf("unexpected descriptor: %+v", got)
	}
}

func TestGetAndSwitchInput(t *testing.T) {
	svc, backend, ref := newTestService(t)
	got, err := svc.GetInput(context.Background(), ref)
	if err != nil || got.Logical != monitor.InputUSBC1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	result, err := svc.Switch(context.Background(), ref, monitor.InputHDMI1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verification != monitor.VerificationConfirmed || backend.Inputs[ref.ID] != 0x11 {
		t.Fatalf("result=%+v input=%x", result, backend.Inputs[ref.ID])
	}
}

func TestDetectInputsUsesProfileThenMCCSAndNeverExpandsQualification(t *testing.T) {
	svc, backend, ref := newTestService(t)
	backend.Capabilities = map[string]string{
		ref.ID: "(prot(monitor)cmds(01 02 03 E3 F3)vcp(10 60(1B 0F 11 12 19) DF))",
	}
	report, err := svc.DetectInputs(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Advisory || !report.InputSourceAdvertised || report.Raw == "" || len(report.Inputs) != 5 {
		t.Fatalf("unexpected report: %+v", report)
	}
	want := []monitor.InputCandidate{
		{Raw: 0x1b, Logical: monitor.InputUSBC1, MappingSource: monitor.InputMappingProfile, WriteQualified: true},
		{Raw: 0x0f, Logical: monitor.InputDisplayPort1, MappingSource: monitor.InputMappingProfile},
		{Raw: 0x11, Logical: monitor.InputHDMI1, MappingSource: monitor.InputMappingProfile, WriteQualified: true},
		{Raw: 0x12, Logical: monitor.InputHDMI2, MappingSource: monitor.InputMappingMCCSStandard},
		{Raw: 0x19, MappingSource: monitor.InputMappingUnmapped},
	}
	for index := range want {
		if report.Inputs[index] != want[index] {
			t.Fatalf("candidate[%d]=%+v want %+v", index, report.Inputs[index], want[index])
		}
	}
	if backend.CapabilitiesCalls != 1 || len(backend.SetCalls) != 0 {
		t.Fatalf("capability_calls=%d writes=%+v", backend.CapabilitiesCalls, backend.SetCalls)
	}
}

func TestDetectInputsWorksForUnprofiledMonitorAndReportsCapabilityErrors(t *testing.T) {
	registry, _ := profiles.NewRegistry()
	backend := &testutil.FakeBackend{
		Descriptors:  []monitor.Descriptor{{ID: "unknown", Manufacturer: "ACM", ProductCode: 1, Model: "Unknown", BackendName: "fake"}},
		Inputs:       map[string]uint16{"unknown": 0x11},
		Capabilities: map[string]string{"unknown": "(vcp(60(0F 11 1B)))"},
	}
	svc := New(backend, registry, &profiles.SupportMatrix{SchemaVersion: 1}, profiles.Platform{})
	snapshot, err := svc.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ref := monitor.MonitorRef{ID: "unknown", Generation: snapshot.Generation}
	report, err := svc.DetectInputs(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Inputs) != 3 || report.Inputs[0].Logical != monitor.InputDisplayPort1 ||
		report.Inputs[1].Logical != monitor.InputHDMI1 || report.Inputs[2].MappingSource != monitor.InputMappingUnmapped {
		t.Fatalf("unexpected unprofiled report: %+v", report)
	}

	backend.CapabilitiesError = monitor.ErrTransactionTimeout
	if _, err := svc.DetectInputs(context.Background(), ref); !errors.Is(err, monitor.ErrTransactionTimeout) {
		t.Fatalf("capability error changed: %v", err)
	}
}

func TestApproveInputsPersistsExactUserQualificationAndEnablesReviewedMappings(t *testing.T) {
	descriptor := monitor.Descriptor{
		ID: "unknown", Manufacturer: "ACM", ProductCode: 1, Model: "Unknown",
		EDIDSHA256: strings.Repeat("a", 64), Connector: "display-service:42", BackendName: "fake",
	}
	backend := &testutil.FakeBackend{
		Descriptors: []monitor.Descriptor{descriptor}, Inputs: map[string]uint16{"unknown": 0x11},
		Capabilities: map[string]string{"unknown": "(prot(monitor)vcp(60(0F 11 1B)))"},
	}
	registry, _ := profiles.NewRegistry()
	svc := New(backend, registry, &profiles.SupportMatrix{SchemaVersion: 1}, profiles.Platform{})
	svc.SetSleeperForTest(noSleep{})
	svc.SetClockForTest(fixedClock{now: time.Unix(100, 0)})
	var saved qualification.Record
	if err := svc.ConfigureUserQualifications(nil, func(record qualification.Record) error {
		saved = record
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ref := monitor.MonitorRef{ID: descriptor.ID, Generation: snapshot.Generation}
	mappings := []monitor.InputValue{
		{Logical: monitor.InputHDMI1, Raw: 0x11},
		{Logical: monitor.InputUSBC1, Raw: 0x1b},
	}
	if err := svc.ApproveInputs(context.Background(), ref, mappings); err != nil {
		t.Fatal(err)
	}
	if saved.MonitorID != descriptor.ID || saved.EDIDSHA256 != descriptor.EDIDSHA256 || !saved.AcceptedAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("unexpected persisted record: %+v", saved)
	}
	if len(backend.SetCalls) != 0 {
		t.Fatalf("approval wrote to the monitor: %+v", backend.SetCalls)
	}
	if got := svc.Snapshot().Monitors[0]; got.SupportState != monitor.SupportUserQualified || got.ProfileName != "user-qualified/Unknown" {
		t.Fatalf("unexpected approved descriptor: %+v", got)
	}
	inputs, err := svc.Inputs(ref)
	if err != nil || len(inputs) != 2 {
		t.Fatalf("inputs=%+v err=%v", inputs, err)
	}
	if _, err := svc.Switch(context.Background(), ref, monitor.InputDisplayPort1); !errors.Is(err, monitor.ErrUnknownInput) {
		t.Fatalf("skipped candidate became switchable: %v", err)
	}
	result, err := svc.Switch(context.Background(), ref, monitor.InputUSBC1)
	if err != nil || result.Verification != monitor.VerificationConfirmed || backend.Inputs[ref.ID] != 0x1b {
		t.Fatalf("result=%+v input=0x%x err=%v", result, backend.Inputs[ref.ID], err)
	}
	report, err := svc.DetectInputs(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if report.Inputs[1].MappingSource != monitor.InputMappingUserQualification || !report.Inputs[1].WriteQualified ||
		report.Inputs[0].WriteQualified {
		t.Fatalf("unexpected post-approval candidates: %+v", report.Inputs)
	}
	backend.Capabilities[ref.ID] = "(vcp(60(11 1B)))"
	if _, err := svc.Switch(context.Background(), ref, monitor.InputHDMI1); !errors.Is(err, monitor.ErrUserQualificationStale) {
		t.Fatalf("changed capabilities did not invalidate local qualification: %v", err)
	}
	if len(backend.SetCalls) != 1 {
		t.Fatalf("stale local qualification wrote to monitor: %+v", backend.SetCalls)
	}
}

func TestApproveInputsRejectsUnadvertisedAmbiguousAndUnpersistedMappings(t *testing.T) {
	descriptor := monitor.Descriptor{
		ID: "unknown", Manufacturer: "ACM", ProductCode: 1, Model: "Unknown",
		EDIDSHA256: strings.Repeat("a", 64), Connector: "direct", BackendName: "fake",
	}
	newService := func(t *testing.T, saver func(qualification.Record) error) (*Service, *testutil.FakeBackend, monitor.MonitorRef) {
		t.Helper()
		backend := &testutil.FakeBackend{Descriptors: []monitor.Descriptor{descriptor}, Inputs: map[string]uint16{"unknown": 0x11}, Capabilities: map[string]string{"unknown": "(vcp(60(11 12)))"}}
		registry, _ := profiles.NewRegistry()
		svc := New(backend, registry, &profiles.SupportMatrix{SchemaVersion: 1}, profiles.Platform{})
		if err := svc.ConfigureUserQualifications(nil, saver); err != nil {
			t.Fatal(err)
		}
		snapshot, err := svc.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return svc, backend, monitor.MonitorRef{ID: descriptor.ID, Generation: snapshot.Generation}
	}

	tests := []struct {
		name     string
		mappings []monitor.InputValue
	}{
		{"unadvertised", []monitor.InputValue{{Logical: monitor.InputDisplayPort1, Raw: 0x0f}}},
		{"unknown-logical", []monitor.InputValue{{Logical: "vga-1", Raw: 0x11}}},
		{"duplicate-logical", []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}, {Logical: monitor.InputHDMI1, Raw: 0x12}}},
		{"empty", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := false
			svc, backend, ref := newService(t, func(qualification.Record) error { saved = true; return nil })
			if err := svc.ApproveInputs(context.Background(), ref, tt.mappings); err == nil {
				t.Fatal("unsafe mapping was accepted")
			}
			if saved || len(backend.SetCalls) != 0 || svc.Snapshot().Monitors[0].SupportState == monitor.SupportUserQualified {
				t.Fatalf("unsafe approval changed state: saved=%v writes=%+v", saved, backend.SetCalls)
			}
		})
	}

	svc, _, ref := newService(t, nil)
	if err := svc.ApproveInputs(context.Background(), ref, []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}); !errors.Is(err, monitor.ErrQualificationUnavailable) {
		t.Fatalf("missing saver error=%v", err)
	}
	persistErr := errors.New("disk full")
	svc, _, ref = newService(t, func(qualification.Record) error { return persistErr })
	if err := svc.ApproveInputs(context.Background(), ref, []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}); !errors.Is(err, persistErr) {
		t.Fatalf("persistence error=%v", err)
	}
	if svc.Snapshot().Monitors[0].SupportState == monitor.SupportUserQualified {
		t.Fatal("failed persistence enabled the monitor")
	}
}

func TestDiscoverLoadsOnlyExactUserQualificationAndDoesNotOverrideReleaseSupport(t *testing.T) {
	descriptor := monitor.Descriptor{
		ID: "mon", Manufacturer: "DEL", ProductCode: 0x4308, Model: "DELL U4025QW",
		EDIDSHA256: strings.Repeat("a", 64), Connector: "direct", BackendName: "fake",
	}
	record, err := qualification.New(descriptor, []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}, "(vcp(60(11)))", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := profiles.NewRegistry()
	backend := &testutil.FakeBackend{Descriptors: []monitor.Descriptor{descriptor}, Inputs: map[string]uint16{"mon": 0x11}}
	svc := New(backend, registry, &profiles.SupportMatrix{SchemaVersion: 1}, profiles.Platform{})
	if err := svc.ConfigureUserQualifications([]qualification.Record{record}, nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Discover(context.Background())
	if err != nil || snapshot.Monitors[0].SupportState != monitor.SupportUserQualified {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}

	backend.Descriptors[0].Connector = "dock"
	snapshot, err = svc.Discover(context.Background())
	if err != nil || snapshot.Monitors[0].SupportState != monitor.SupportBackendExperimental {
		t.Fatalf("mismatched endpoint was accepted: snapshot=%+v err=%v", snapshot, err)
	}
	backend.Descriptors[0] = descriptor
	backend.Descriptors[0].IdentityState = monitor.IdentityAmbiguous
	snapshot, err = svc.Discover(context.Background())
	if err != nil || snapshot.Monitors[0].SupportState != monitor.SupportAmbiguous {
		t.Fatalf("ambiguous identity was locally qualified: snapshot=%+v err=%v", snapshot, err)
	}
	ref := monitor.MonitorRef{ID: descriptor.ID, Generation: snapshot.Generation}
	if err := svc.ApproveInputs(context.Background(), ref, []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}); !errors.Is(err, monitor.ErrAmbiguousMonitor) {
		t.Fatalf("ambiguous identity approval error=%v", err)
	}
}

func TestStaleAndUnqualifiedSwitchesAreRejected(t *testing.T) {
	svc, _, ref := newTestService(t)
	stale := ref
	stale.Generation--
	if _, err := svc.GetInput(context.Background(), stale); !errors.Is(err, monitor.ErrStaleEnumeration) {
		t.Fatalf("got %v", err)
	}
	svc.mu.Lock()
	svc.snapshot.Monitors[0].SupportState = monitor.SupportBackendExperimental
	svc.mu.Unlock()
	if _, err := svc.Switch(context.Background(), ref, monitor.InputHDMI1); !errors.Is(err, monitor.ErrUnqualifiedSlice) {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.Inputs(ref); !errors.Is(err, monitor.ErrUnqualifiedSlice) {
		t.Fatalf("unqualified inputs were exposed: %v", err)
	}
}

func TestSupportRecordLimitsExposedAndSwitchableInputs(t *testing.T) {
	svc, _, ref := newTestService(t)
	inputs, err := svc.Inputs(ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("got %d qualified inputs, want 2: %+v", len(inputs), inputs)
	}
	if _, err := svc.Switch(context.Background(), ref, monitor.InputDisplayPort1); !errors.Is(err, monitor.ErrUnqualifiedSlice) {
		t.Fatalf("unqualified profile input was switchable: %v", err)
	}
}

func TestReadRetriesOnlyStableTransientCategories(t *testing.T) {
	svc, backend, ref := newTestService(t)
	sleeper := &recordingSleeper{}
	svc.SetSleeperForTest(sleeper)
	backend.GetErrors = []error{monitor.ErrTransactionTimeout, monitor.ErrChecksum, nil}

	value, err := svc.GetInput(context.Background(), ref)
	if err != nil || value.Logical != monitor.InputUSBC1 {
		t.Fatalf("value=%+v err=%v", value, err)
	}
	if backend.GetCalls != 3 || len(sleeper.delays) != 2 {
		t.Fatalf("calls=%d delays=%v", backend.GetCalls, sleeper.delays)
	}

	backend.GetCalls = 0
	backend.GetErrors = []error{monitor.ErrPermissionDenied, nil}
	if _, err := svc.GetInput(context.Background(), ref); !errors.Is(err, monitor.ErrPermissionDenied) {
		t.Fatalf("non-retryable error changed: %v", err)
	}
	if backend.GetCalls != 1 {
		t.Fatalf("permission error was retried %d times", backend.GetCalls)
	}
}

func TestWriteRetriesNACKButNotAmbiguousTimeout(t *testing.T) {
	svc, backend, ref := newTestService(t)
	svc.SetSleeperForTest(&recordingSleeper{})
	backend.SetErrors = []error{monitor.ErrTransactionNACK, nil}
	result, err := svc.Switch(context.Background(), ref, monitor.InputHDMI1)
	if err != nil || result.Attempts != 2 || result.Verification != monitor.VerificationConfirmed {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	backend.Inputs[ref.ID] = 0x1b
	backend.SetErrors = []error{monitor.ErrTransactionTimeout, nil}
	result, err = svc.Switch(context.Background(), ref, monitor.InputHDMI1)
	if !errors.Is(err, monitor.ErrTransactionTimeout) || result.Attempts != 1 {
		t.Fatalf("ambiguous write was retried: result=%+v err=%v", result, err)
	}
}

func TestDiscoverClassifiesUnprofiledAndInvalidQualification(t *testing.T) {
	registry, _ := profiles.NewRegistry()
	backend := &testutil.FakeBackend{Descriptors: []monitor.Descriptor{
		{ID: "unknown", Manufacturer: "ACM", ProductCode: 1, Model: "Unknown", BackendName: "fake"},
		{ID: "dell", Manufacturer: "DEL", ProductCode: 0x4308, Model: "DELL U4025QW", EDIDSHA256: "hash", Connector: "test", BackendName: "fake"},
	}, Inputs: map[string]uint16{"dell": 0x19}}
	matrix := &profiles.SupportMatrix{SchemaVersion: 1, Records: []profiles.SupportRecord{{
		ID: "invalid", OS: "test", Architecture: "test", OSVersion: "1", OSBuild: "1", Backend: "fake", ApplicationCommit: "commit",
		Profile: "Dell U4025QW", EDIDSHA256: "hash", Connector: "test", Inputs: []string{"usb-c-2"}, Verification: profiles.VerifyImmediate,
		QualificationRecord: "evidence", EvidenceSHA256: "hash",
	}}}
	svc := New(backend, registry, matrix, profiles.Platform{OS: "test", Architecture: "test", Version: "1", Build: "1", ApplicationCommit: "commit"})
	snapshot, err := svc.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Monitors[0].SupportState != monitor.SupportUnprofiled || snapshot.Monitors[1].SupportState != monitor.SupportBackendExperimental {
		t.Fatalf("unexpected support states: %+v", snapshot.Monitors)
	}
	backend.EnumerateError = monitor.ErrBackendUnavailable
	if _, err := svc.Discover(context.Background()); !errors.Is(err, monitor.ErrBackendUnavailable) {
		t.Fatalf("enumeration error changed: %v", err)
	}
}

func TestLookupAndReadFailuresAreTyped(t *testing.T) {
	svc, backend, ref := newTestService(t)
	unknown := monitor.MonitorRef{ID: "missing", Generation: ref.Generation}
	if _, err := svc.GetInput(context.Background(), unknown); !errors.Is(err, monitor.ErrMonitorNotFound) {
		t.Fatalf("unknown monitor: %v", err)
	}
	svc.mu.Lock()
	svc.snapshot.Monitors[0].ProfileName = "missing-profile"
	svc.mu.Unlock()
	if _, err := svc.Inputs(ref); !errors.Is(err, monitor.ErrUnsupportedMonitor) {
		t.Fatalf("missing input profile: %v", err)
	}
	if _, err := svc.GetInput(context.Background(), ref); !errors.Is(err, monitor.ErrReadUnsupported) {
		t.Fatalf("missing read profile: %v", err)
	}
	svc.mu.Lock()
	svc.snapshot.Monitors[0].ProfileName = "Dell U4025QW"
	svc.mu.Unlock()
	backend.OpenError = monitor.ErrPermissionDenied
	if _, err := svc.GetInput(context.Background(), ref); !errors.Is(err, monitor.ErrPermissionDenied) {
		t.Fatalf("open error changed: %v", err)
	}
}

func TestSwitchVerificationTransitionsAndFailureBoundaries(t *testing.T) {
	newVariant := func(t *testing.T, mode profiles.VerificationMode) (*Service, *testutil.FakeBackend, monitor.MonitorRef) {
		t.Helper()
		profile := profiles.Profile{
			Name: "test-profile", Match: profiles.EDIDMatcher{Manufacturer: "TST", ProductCode: 1, ModelNames: []string{"Test"}},
			Inputs: map[monitor.Input]uint16{monitor.InputHDMI1: 0x11, monitor.InputHDMI2: 0x12}, ReadSupported: true,
			RetryCount: 1, ReplyDelay: time.Millisecond, WriteDelay: time.Millisecond, Verification: mode,
		}
		registry, err := profiles.NewRegistry(profile)
		if err != nil {
			t.Fatal(err)
		}
		backend := &testutil.FakeBackend{Descriptors: []monitor.Descriptor{{
			ID: "mon", Manufacturer: "TST", ProductCode: 1, Model: "Test", EDIDSHA256: "hash", Connector: "test", BackendName: "fake",
		}}, Inputs: map[string]uint16{"mon": 0x11}}
		matrix := &profiles.SupportMatrix{SchemaVersion: 1, Records: []profiles.SupportRecord{{
			ID: "record", OS: "test", Architecture: "test", OSVersion: "1", OSBuild: "1", Backend: "fake", ApplicationCommit: "commit",
			Profile: profile.Name, EDIDSHA256: "hash", Connector: "test", Inputs: []string{"hdmi-1", "hdmi-2"}, Verification: mode,
			QualificationRecord: "evidence", EvidenceSHA256: "hash",
		}}}
		svc := New(backend, registry, matrix, profiles.Platform{OS: "test", Architecture: "test", Version: "1", Build: "1", ApplicationCommit: "commit"})
		svc.SetSleeperForTest(noSleep{})
		svc.SetClockForTest(fixedClock{now: time.Unix(100, 0)})
		snapshot, err := svc.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return svc, backend, monitor.MonitorRef{ID: "mon", Generation: snapshot.Generation}
	}

	t.Run("accepted-without-readback", func(t *testing.T) {
		svc, _, ref := newVariant(t, profiles.VerifyNone)
		result, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2)
		if err != nil || result.Verification != monitor.VerificationWriteAccepted || result.Duration != 0 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	for _, endpointError := range []error{monitor.ErrDisconnected, monitor.ErrEndpointNotFound} {
		t.Run(endpointError.Error(), func(t *testing.T) {
			svc, backend, ref := newVariant(t, profiles.VerifyImmediate)
			backend.GetErrors = []error{endpointError}
			result, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2)
			if err != nil || result.Verification != monitor.VerificationDisplayPathChanged {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}

	t.Run("readback-error", func(t *testing.T) {
		svc, backend, ref := newVariant(t, profiles.VerifyImmediate)
		backend.GetErrors = []error{monitor.ErrPermissionDenied}
		if _, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2); !errors.Is(err, monitor.ErrPermissionDenied) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("successful-write-with-malformed-readback-is-assumed-once", func(t *testing.T) {
		svc, backend, ref := newVariant(t, profiles.VerifyImmediate)
		rawReply := []byte{0x00, 0x60, 0x00, 0x19, 0x19, 0x19, 0x19, 0x00, 0x00, 0x00, 0x00}
		_, readErr := ddc.ParseGetVCPReply(rawReply, ddc.VCPInputSource)
		backend.GetErrors = []error{readErr, readErr}
		result, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2)
		if err != nil {
			t.Fatalf("switch: %v", err)
		}
		if result.Verification != monitor.VerificationAssumedSuccess || result.Observed != nil {
			t.Fatalf("result=%+v", result)
		}
		if result.VerificationAttempts != 2 || result.VerificationIssue == nil || result.VerificationIssue.Category != monitor.VerificationIssueMalformedReply {
			t.Fatalf("verification diagnostic=%+v", result)
		}
		if result.VerificationIssue.RawReplyHex != "0060001919191900000000" {
			t.Fatalf("raw reply=%q", result.VerificationIssue.RawReplyHex)
		}
		if len(backend.SetCalls) != 1 {
			t.Fatalf("set calls=%d, want exactly one", len(backend.SetCalls))
		}
	})

	t.Run("successful-write-with-read-unsupported-is-assumed", func(t *testing.T) {
		svc, backend, ref := newVariant(t, profiles.VerifyImmediate)
		backend.GetErrors = []error{monitor.ErrReadUnsupported}
		result, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2)
		if err != nil || result.Verification != monitor.VerificationAssumedSuccess || result.VerificationAttempts != 1 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if result.VerificationIssue == nil || result.VerificationIssue.Category != monitor.VerificationIssueReadUnsupported {
			t.Fatalf("issue=%+v", result.VerificationIssue)
		}
	})

	t.Run("readback-mismatch", func(t *testing.T) {
		svc, backend, ref := newVariant(t, profiles.VerifyImmediate)
		backend.IgnoreSets = true
		result, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2)
		if err == nil || result.Verification != monitor.VerificationUnknown || result.Observed == nil || result.Observed.Raw != 0x11 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("open-set-sleep-and-logical-errors", func(t *testing.T) {
		svc, backend, ref := newVariant(t, profiles.VerifyImmediate)
		if _, err := svc.Switch(context.Background(), ref, monitor.InputDisplayPort1); !errors.Is(err, monitor.ErrUnknownInput) {
			t.Fatalf("unknown input: %v", err)
		}
		backend.OpenError = monitor.ErrPermissionDenied
		if _, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2); !errors.Is(err, monitor.ErrPermissionDenied) {
			t.Fatalf("open error: %v", err)
		}
		backend.OpenError = nil
		backend.SetError = monitor.ErrWriteUnsupported
		if _, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2); !errors.Is(err, monitor.ErrWriteUnsupported) {
			t.Fatalf("set error: %v", err)
		}
		backend.SetError = nil
		svc.SetSleeperForTest(failingSleeper{err: context.Canceled})
		if _, err := svc.Switch(context.Background(), ref, monitor.InputHDMI2); !errors.Is(err, context.Canceled) {
			t.Fatalf("sleep error: %v", err)
		}
	})
}

func TestRealSleeperHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (realSleeper{}).Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestEndpointLocksSerializeSameMonitorAndAllowIndependentMonitors(t *testing.T) {
	t.Run("same-monitor", func(t *testing.T) {
		svc, backend, ref := newTestService(t)
		started := make(chan string, 2)
		release := make(chan struct{}, 2)
		backend.OperationStarted = started
		backend.OperationRelease = release
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		wg.Add(2)
		for range 2 {
			go func() {
				defer wg.Done()
				_, _ = svc.GetInput(ctx, ref)
			}()
		}
		waitForOperation(t, ctx, started)
		release <- struct{}{}
		waitForOperation(t, ctx, started)
		release <- struct{}{}
		wg.Wait()
		if backend.MaxActive != 1 {
			t.Fatalf("same endpoint max concurrency=%d", backend.MaxActive)
		}
	})

	t.Run("independent-monitors", func(t *testing.T) {
		profile := profiles.Profile{
			Name: "test", Match: profiles.EDIDMatcher{Manufacturer: "TST", ProductCode: 1, ModelNames: []string{"Test"}},
			Inputs: map[monitor.Input]uint16{monitor.InputHDMI1: 0x11}, ReadSupported: true, Verification: profiles.VerifyImmediate,
		}
		registry, _ := profiles.NewRegistry(profile)
		started := make(chan string, 2)
		release := make(chan struct{}, 2)
		backend := &testutil.FakeBackend{
			Descriptors: []monitor.Descriptor{
				{ID: "one", Manufacturer: "TST", ProductCode: 1, Model: "Test", EDIDSHA256: "one", Connector: "one", BackendName: "fake"},
				{ID: "two", Manufacturer: "TST", ProductCode: 1, Model: "Test", EDIDSHA256: "two", Connector: "two", BackendName: "fake"},
			},
			Inputs: map[string]uint16{"one": 0x11, "two": 0x11}, OperationStarted: started, OperationRelease: release,
		}
		matrix := &profiles.SupportMatrix{SchemaVersion: 1}
		for _, id := range []string{"one", "two"} {
			matrix.Records = append(matrix.Records, profiles.SupportRecord{
				ID: id, OS: "test", Architecture: "test", OSVersion: "1", OSBuild: "1", Backend: "fake", ApplicationCommit: "commit",
				Profile: "test", EDIDSHA256: id, Connector: id, Inputs: []string{"hdmi-1"}, Verification: profiles.VerifyImmediate,
				QualificationRecord: "evidence", EvidenceSHA256: "hash",
			})
		}
		svc := New(backend, registry, matrix, profiles.Platform{OS: "test", Architecture: "test", Version: "1", Build: "1", ApplicationCommit: "commit"})
		snapshot, err := svc.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		for _, descriptor := range snapshot.Monitors {
			ref := monitor.MonitorRef{ID: descriptor.ID, Generation: snapshot.Generation}
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = svc.GetInput(ctx, ref)
			}()
		}
		waitForOperation(t, ctx, started)
		waitForOperation(t, ctx, started)
		if backend.MaxActive != 2 {
			t.Fatalf("independent endpoint max concurrency=%d", backend.MaxActive)
		}
		release <- struct{}{}
		release <- struct{}{}
		wg.Wait()
	})
}

func waitForOperation(t *testing.T, ctx context.Context, started <-chan string) string {
	t.Helper()
	select {
	case id := <-started:
		return id
	case <-ctx.Done():
		t.Fatalf("operation did not start: %v", ctx.Err())
		return ""
	}
}
