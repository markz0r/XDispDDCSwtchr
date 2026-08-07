package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

type stubService struct {
	snapshot      monitor.Snapshot
	input         monitor.InputValue
	inputsErr     error
	discoverErr   error
	readErr       error
	capability    monitor.CapabilityReport
	capabilityErr error
	approveErr    error
	switchErr     error
	switches      int
	approvals     int
	approved      []monitor.InputValue
}

func (s *stubService) Discover(context.Context) (monitor.Snapshot, error) {
	return s.snapshot, s.discoverErr
}
func (s *stubService) Inputs(monitor.MonitorRef) ([]monitor.InputValue, error) {
	return []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}, s.inputsErr
}
func (s *stubService) GetInput(context.Context, monitor.MonitorRef) (monitor.InputValue, error) {
	return s.input, s.readErr
}
func (s *stubService) DetectInputs(context.Context, monitor.MonitorRef) (monitor.CapabilityReport, error) {
	return s.capability, s.capabilityErr
}
func (s *stubService) ApproveInputs(_ context.Context, ref monitor.MonitorRef, mappings []monitor.InputValue) error {
	s.approvals++
	s.approved = append([]monitor.InputValue(nil), mappings...)
	if s.approveErr == nil {
		for index := range s.snapshot.Monitors {
			if s.snapshot.Monitors[index].ID == ref.ID {
				s.snapshot.Monitors[index].SupportState = monitor.SupportUserQualified
			}
		}
	}
	return s.approveErr
}
func (s *stubService) Switch(_ context.Context, ref monitor.MonitorRef, input monitor.Input) (monitor.SwitchResult, error) {
	s.switches++
	observed := monitor.InputValue{Logical: input, Raw: 0x11}
	return monitor.SwitchResult{Monitor: ref, Target: observed, Observed: &observed, Verification: monitor.VerificationConfirmed}, s.switchErr
}

func TestInitDiscoveryAndCompletionErrors(t *testing.T) {
	service := &stubService{snapshot: monitor.Snapshot{Generation: 1}, discoverErr: monitor.ErrBackendUnavailable}
	model := New(service, "")
	message := model.Init()().(discoveryCompletedMsg)
	if !errors.Is(message.Err, monitor.ErrBackendUnavailable) || message.OperationID != 1 {
		t.Fatalf("unexpected discovery message: %+v", message)
	}
	updated, cmd := model.Update(message)
	model = updated.(Model)
	if cmd != nil || model.busy || !errors.Is(model.lastError, monitor.ErrBackendUnavailable) || model.status != "Discovery failed" {
		t.Fatalf("unexpected error state: %+v", model)
	}
	updated, _ = model.Update(discoveryCompletedMsg{OperationID: 99, Snapshot: monitor.Snapshot{Generation: 9}})
	if updated.(Model).snapshot.Generation != 0 {
		t.Fatal("stale discovery completion changed state")
	}
}

func TestReadWriteWindowAndUnknownMessageStates(t *testing.T) {
	ref := monitor.MonitorRef{ID: "mon", Generation: 1}
	model := New(&stubService{}, "")
	model.snapshot = monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon", SupportState: monitor.SupportSupported}}}
	model.busy = true
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 3, Height: 2})
	model = updated.(Model)
	if model.width != 3 || model.height != 2 {
		t.Fatalf("window size not applied: %+v", model)
	}
	updated, _ = model.Update(inputReadCompletedMsg{OperationID: 1, Monitor: ref, Err: monitor.ErrChecksum})
	model = updated.(Model)
	if model.busy || !errors.Is(model.lastError, monitor.ErrChecksum) || model.status != "Input read failed" {
		t.Fatalf("read failure state: %+v", model)
	}
	model.busy = true
	model.confirmInput = monitor.InputHDMI1
	updated, _ = model.Update(inputWriteCompletedMsg{OperationID: 1, Monitor: ref, Err: monitor.ErrWriteUnsupported})
	model = updated.(Model)
	if model.busy || model.confirmInput != "" || !errors.Is(model.lastError, monitor.ErrWriteUnsupported) {
		t.Fatalf("write failure state: %+v", model)
	}
	updated, _ = model.Update(struct{}{})
	if !errors.Is(updated.(Model).lastError, monitor.ErrWriteUnsupported) {
		t.Fatal("unknown message changed state")
	}
	updated, _ = model.Update(inputWriteCompletedMsg{OperationID: 99, Monitor: ref})
	if !errors.Is(updated.(Model).lastError, monitor.ErrWriteUnsupported) {
		t.Fatal("stale write changed state")
	}
}

func TestAssumedSuccessIsAdvisoryNotFailureOrCurrentInput(t *testing.T) {
	ref := monitor.MonitorRef{ID: "mon", Generation: 1}
	model := New(&stubService{}, "")
	model.snapshot = monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon", SupportState: monitor.SupportSupported}}}
	previous := monitor.InputValue{Logical: monitor.InputHDMI1, Raw: 0x11}
	model.current = &previous
	model.busy = true
	result := monitor.SwitchResult{
		Monitor: ref, Target: monitor.InputValue{Logical: monitor.InputThunderbolt1, Raw: 0x19},
		Verification:      monitor.VerificationAssumedSuccess,
		VerificationIssue: &monitor.VerificationIssue{Category: monitor.VerificationIssueMalformedReply},
	}
	updated, _ := model.Update(inputWriteCompletedMsg{OperationID: 1, Monitor: ref, Result: result})
	model = updated.(Model)
	if model.busy || model.lastError != nil || model.current != nil {
		t.Fatalf("unexpected assumed-success state: %+v", model)
	}
	if !strings.Contains(model.status, "Assumed success [no read-back provided by display]") || !strings.Contains(model.status, "Press r") {
		t.Fatalf("status=%q", model.status)
	}
	if strings.Contains(model.View().Content, "Error:") {
		t.Fatalf("assumed success rendered an error banner: %q", model.View().Content)
	}
}

func TestViewAndNavigationKeyStates(t *testing.T) {
	service := &stubService{
		snapshot: monitor.Snapshot{Generation: 2, Monitors: []monitor.Descriptor{
			{ID: "one", Model: "One", Connector: "a", SupportState: monitor.SupportSupported},
			{ID: "two", Model: "Two", Connector: "b", SupportState: monitor.SupportSupported},
		}},
		input: monitor.InputValue{Logical: monitor.InputHDMI1, Raw: 0x11},
	}
	model := New(service, "two")
	model.snapshot = service.snapshot
	model.monitorCursor = model.preferredIndex()
	model.inputs = []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}, {Logical: monitor.InputHDMI2, Raw: 0x12}}
	model.current = &service.input
	model.detected = []monitor.InputCandidate{{Raw: 0x11, Logical: monitor.InputHDMI1, MappingSource: monitor.InputMappingProfile}}
	model.busy = false
	model.lastError = monitor.ErrChecksum
	model.showHelp = true
	view := model.View()
	for _, text := range []string{"XDispDDCSwtchr", "> Two", "hdmi-1 (current)", "Advertised inputs (advisory)", "0x11", "Error:", "up/down"} {
		if !strings.Contains(view.Content, text) {
			t.Fatalf("view missing %q: %s", text, view.Content)
		}
	}
	if !view.AltScreen || view.WindowTitle == "" {
		t.Fatalf("view metadata missing: %+v", view)
	}

	updated, _ := model.handleKey("?")
	if updated.(Model).showHelp {
		t.Fatal("help did not toggle")
	}
	if _, cmd := model.handleKey("q"); cmd == nil {
		t.Fatal("quit command missing")
	}
	updated, _ = model.handleKey("right")
	model = updated.(Model)
	if model.inputCursor != 1 {
		t.Fatal("right did not move input")
	}
	model.confirmInput = monitor.InputHDMI2
	updated, _ = model.handleKey("left")
	model = updated.(Model)
	if model.inputCursor != 0 || model.confirmInput != "" {
		t.Fatal("left did not move/reset confirmation")
	}
	updated, cmd := model.handleKey("up")
	model = updated.(Model)
	if model.monitorCursor != 0 || cmd == nil {
		t.Fatal("up did not select monitor")
	}
	model.busy = false
	updated, cmd = model.handleKey("down")
	model = updated.(Model)
	if model.monitorCursor != 1 || cmd == nil {
		t.Fatal("down did not select monitor")
	}
	model.busy = false
	updated, cmd = model.handleKey("g")
	model = updated.(Model)
	if !model.busy || cmd == nil || cmd().(inputReadCompletedMsg).Monitor.ID != "two" {
		t.Fatal("get did not start read")
	}
	model.busy = false
	service.capability = monitor.CapabilityReport{Inputs: []monitor.InputCandidate{{Raw: 0x11, Logical: monitor.InputHDMI1}}}
	updated, cmd = model.handleKey("c")
	model = updated.(Model)
	if !model.busy || cmd == nil || cmd().(capabilityReadCompletedMsg).Monitor.ID != "two" {
		t.Fatal("capability detection did not start")
	}
	model.busy = false
	updated, cmd = model.handleKey("r")
	model = updated.(Model)
	if !model.busy || cmd == nil || model.current != nil || model.inputs != nil || model.detected != nil {
		t.Fatal("refresh did not reset discovery state")
	}
}

func TestCapabilityCompletionAndStaleSuppression(t *testing.T) {
	ref := monitor.MonitorRef{ID: "mon", Generation: 1}
	model := New(&stubService{}, "")
	model.snapshot = monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon"}}}
	model.busy = true
	report := monitor.CapabilityReport{Inputs: []monitor.InputCandidate{{Raw: 0x1b, MappingSource: monitor.InputMappingUnmapped}}}
	updated, _ := model.Update(capabilityReadCompletedMsg{OperationID: 1, Monitor: ref, Report: report})
	model = updated.(Model)
	if model.busy || len(model.detected) != 1 || model.lastError != nil || !strings.Contains(model.status, "advisory") {
		t.Fatalf("unexpected capability state: %+v", model)
	}
	updated, _ = model.Update(capabilityReadCompletedMsg{OperationID: 99, Monitor: ref, Err: monitor.ErrChecksum})
	if updated.(Model).lastError != nil {
		t.Fatal("stale capability message changed state")
	}
	model.busy = true
	updated, _ = model.Update(capabilityReadCompletedMsg{OperationID: 1, Monitor: ref, Err: monitor.ErrChecksum})
	if !errors.Is(updated.(Model).lastError, monitor.ErrChecksum) || updated.(Model).status != "Capability detection failed" {
		t.Fatalf("capability error state: %+v", updated)
	}
}

func TestEmptyAndUnqualifiedSelectionStates(t *testing.T) {
	service := &stubService{inputsErr: monitor.ErrUnqualifiedSlice}
	model := New(service, "missing")
	model.snapshot = monitor.Snapshot{Generation: 1}
	updated, cmd := model.selectMonitor()
	if cmd != nil || updated.(Model).currentRef() != (monitor.MonitorRef{}) {
		t.Fatal("empty selection produced work")
	}
	model.snapshot.Monitors = []monitor.Descriptor{{ID: "mon", SupportState: monitor.SupportBackendExperimental}}
	updated, cmd = model.selectMonitor()
	model = updated.(Model)
	if cmd == nil || model.inputs != nil || model.status != "Reading VCP 0x60..." {
		t.Fatalf("unexpected unqualified selection: %+v", model)
	}
	model.busy = false
	if _, cmd := model.handleKey("enter"); cmd != nil {
		t.Fatal("empty inputs started write")
	}
	model.monitorCursor = -1
	if model.currentRef() != (monitor.MonitorRef{}) {
		t.Fatal("invalid cursor returned reference")
	}
}

func TestDiscoveryReadAndStaleMessageSuppression(t *testing.T) {
	service := &stubService{
		snapshot: monitor.Snapshot{Generation: 2, Monitors: []monitor.Descriptor{{ID: "mon-a", Model: "Dell", ProfileName: "Dell", SupportState: monitor.SupportSupported}}},
		input:    monitor.InputValue{Logical: monitor.InputHDMI1, Raw: 0x11},
	}
	model := New(service, "")
	updated, cmd := model.Update(discoveryCompletedMsg{OperationID: 1, Snapshot: service.snapshot})
	model = updated.(Model)
	if cmd == nil || len(model.inputs) != 1 || !model.busy {
		t.Fatalf("unexpected discovery state: %+v", model)
	}
	message := cmd().(inputReadCompletedMsg)
	updated, _ = model.Update(message)
	model = updated.(Model)
	if model.current == nil || model.current.Raw != 0x11 || model.busy {
		t.Fatalf("unexpected read state: %+v", model)
	}
	updated, _ = model.Update(inputReadCompletedMsg{OperationID: 999, Err: context.Canceled})
	if updated.(Model).lastError != nil {
		t.Fatal("stale message changed model")
	}
}

func TestSwitchRequiresConfirmationAndPreventsDuplicate(t *testing.T) {
	service := &stubService{snapshot: monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon-a", ProfileName: "Dell", SupportState: monitor.SupportSupported}}}}
	model := New(service, "")
	model.snapshot = service.snapshot
	model.inputs = []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}
	model.busy = false
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmd != nil || model.confirmInput != monitor.InputHDMI1 {
		t.Fatal("first enter did not arm confirmation")
	}
	updated, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmd == nil || !model.busy {
		t.Fatal("second enter did not start switch")
	}
	updated, duplicate := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if duplicate != nil {
		t.Fatal("busy model started duplicate switch")
	}
	message := cmd().(inputWriteCompletedMsg)
	updated, _ = updated.(Model).Update(message)
	if service.switches != 1 || updated.(Model).lastError != nil {
		t.Fatalf("unexpected switch completion: switches=%d model=%+v", service.switches, updated)
	}
}

func TestExperimentalSliceCannotSwitch(t *testing.T) {
	service := &stubService{snapshot: monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon-a", SupportState: monitor.SupportBackendExperimental}}}}
	model := New(service, "")
	model.snapshot = service.snapshot
	model.inputs = []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}}
	model.busy = false
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || updated.(Model).lastError != monitor.ErrUnqualifiedSlice {
		t.Fatal("experimental switch was not blocked")
	}
}

func TestUnqualifiedMonitorAutomaticallyCompletesGuidedQualification(t *testing.T) {
	service := &stubService{
		snapshot: monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{
			ID: "mon-a", Model: "Unknown", Connector: "direct", SupportState: monitor.SupportBackendExperimental,
		}}},
		input: monitor.InputValue{Raw: 0x0f},
		capability: monitor.CapabilityReport{Inputs: []monitor.InputCandidate{
			{Raw: 0x0f, Logical: monitor.InputDisplayPort1, MappingSource: monitor.InputMappingMCCSStandard},
			{Raw: 0x1b, MappingSource: monitor.InputMappingUnmapped},
		}},
	}
	model := New(service, "")

	discovery := model.Init()().(discoveryCompletedMsg)
	updated, readCmd := model.Update(discovery)
	model = updated.(Model)
	if readCmd == nil {
		t.Fatal("discovery did not start input read")
	}
	updated, capabilityCmd := model.Update(readCmd())
	model = updated.(Model)
	if capabilityCmd == nil || !model.busy || !strings.Contains(model.status, "not qualified") {
		t.Fatalf("unqualified read did not start capabilities: %+v", model)
	}
	updated, _ = model.Update(capabilityCmd())
	model = updated.(Model)
	if !model.qualificationReview || model.qualificationChoice == 0 || model.qualificationIndex != 0 {
		t.Fatalf("guided review did not start with detected suggestion: %+v", model)
	}
	for _, text := range []string{"Resolve unqualified monitor", "Candidate 1 of 2", "displayport-1", "does not write"} {
		if !strings.Contains(model.View().Content, text) {
			t.Fatalf("qualification view missing %q: %s", text, model.View().Content)
		}
	}

	updated, _ = model.handleKey("enter")
	model = updated.(Model)
	if model.qualificationIndex != 1 || model.qualificationChoice != 0 || len(model.qualificationMappings) != 1 {
		t.Fatalf("first mapping was not accepted and unknown was not default-skipped: %+v", model)
	}
	updated, _ = model.handleKey("right")
	model = updated.(Model)
	updated, _ = model.handleKey("enter")
	model = updated.(Model)
	if model.lastError == nil || model.qualificationIndex != 1 {
		t.Fatalf("duplicate logical label was accepted: %+v", model)
	}
	updated, _ = model.handleKey("left")
	model = updated.(Model)
	updated, _ = model.handleKey("enter")
	model = updated.(Model)
	if model.qualificationReview || !model.qualificationConfirm || len(model.qualificationMappings) != 1 {
		t.Fatalf("review did not reach explicit confirmation: %+v", model)
	}
	for _, text := range []string{"Confirm local user qualification", "user-qualified, not vendor/release-qualified", "enter saves"} {
		if !strings.Contains(model.View().Content, text) {
			t.Fatalf("confirmation view missing %q: %s", text, model.View().Content)
		}
	}

	updated, approvalCmd := model.handleKey("enter")
	model = updated.(Model)
	if approvalCmd == nil || !model.busy {
		t.Fatal("confirmation did not start persistence")
	}
	completion := approvalCmd().(qualificationCompletedMsg)
	updated, rediscoverCmd := model.Update(completion)
	model = updated.(Model)
	if service.approvals != 1 || len(service.approved) != 1 || service.approved[0] != (monitor.InputValue{Logical: monitor.InputDisplayPort1, Raw: 0x0f}) || rediscoverCmd == nil {
		t.Fatalf("approval was not exact: approvals=%d mappings=%+v model=%+v", service.approvals, service.approved, model)
	}
	updated, readCmd = model.Update(rediscoverCmd())
	model = updated.(Model)
	if model.snapshot.Monitors[0].SupportState != monitor.SupportUserQualified || readCmd == nil {
		t.Fatalf("saved qualification was not rediscovered: %+v", model)
	}
	updated, followup := model.Update(readCmd())
	model = updated.(Model)
	if followup != nil || model.busy || model.qualificationReview {
		t.Fatalf("user-qualified monitor re-entered qualification: %+v", model)
	}

	updated, firstEnter := model.handleKey("enter")
	model = updated.(Model)
	if firstEnter != nil || model.confirmInput != monitor.InputHDMI1 {
		t.Fatalf("user-qualified switch was not armed: %+v", model)
	}
	updated, switchCmd := model.handleKey("enter")
	if switchCmd == nil || !updated.(Model).busy {
		t.Fatal("user-qualified switch did not start")
	}
}

func TestQualificationCanBeCancelledAndRequiresAtLeastOneMapping(t *testing.T) {
	model := New(&stubService{}, "")
	model.snapshot = monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon", SupportState: monitor.SupportUnprofiled}}}
	model.detected = []monitor.InputCandidate{{Raw: 0x1b, MappingSource: monitor.InputMappingUnmapped}}
	model.busy = false
	model = model.beginQualification()
	updated, _ := model.handleKey("enter")
	model = updated.(Model)
	if model.qualificationConfirm || model.lastError == nil || !strings.Contains(model.status, "No mappings accepted") {
		t.Fatalf("empty qualification was accepted: %+v", model)
	}
	updated, _ = model.handleKey("a")
	model = updated.(Model)
	if !model.qualificationReview {
		t.Fatal("review could not be restarted")
	}
	updated, _ = model.handleKey("esc")
	model = updated.(Model)
	if model.qualificationReview || model.qualificationConfirm || !strings.Contains(model.status, "cancelled") {
		t.Fatalf("qualification was not cancelled: %+v", model)
	}
}

func TestAmbiguousAndInvalidIdentitiesAreNeverPromptedForQualification(t *testing.T) {
	for _, identity := range []monitor.IdentityState{monitor.IdentityAmbiguous, monitor.IdentityInvalidEDID} {
		descriptor := monitor.Descriptor{ID: "mon", IdentityState: identity, SupportState: monitor.SupportUnprofiled}
		model := New(&stubService{}, "")
		model.snapshot = monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{descriptor}}
		model.busy = false
		if model.needsQualification() {
			t.Fatalf("identity %s was eligible for qualification", identity)
		}
		updated, cmd := model.handleKey("a")
		if cmd != nil || updated.(Model).qualificationReview {
			t.Fatalf("identity %s started qualification", identity)
		}
	}
}
