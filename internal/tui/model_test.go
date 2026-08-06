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
	snapshot    monitor.Snapshot
	input       monitor.InputValue
	inputsErr   error
	discoverErr error
	readErr     error
	switchErr   error
	switches    int
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
	model.busy = false
	model.lastError = monitor.ErrChecksum
	model.showHelp = true
	view := model.View()
	for _, text := range []string{"XDispDDCSwtchr", "> Two", "hdmi-1 (current)", "Error:", "up/down"} {
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
	updated, cmd = model.handleKey("r")
	model = updated.(Model)
	if !model.busy || cmd == nil || model.current != nil || model.inputs != nil {
		t.Fatal("refresh did not reset discovery state")
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
