package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/platform"
)

type stubService struct {
	snapshot    monitor.Snapshot
	discoverErr error
	current     monitor.InputValue
	inputs      []monitor.InputValue
	inputsErr   error
	currentErr  error
	switchErr   error
	switches    int
}

func (s *stubService) Discover(context.Context) (monitor.Snapshot, error) {
	return s.snapshot, s.discoverErr
}
func (s *stubService) Inputs(monitor.MonitorRef) ([]monitor.InputValue, error) {
	return s.inputs, s.inputsErr
}
func (s *stubService) GetInput(context.Context, monitor.MonitorRef) (monitor.InputValue, error) {
	return s.current, s.currentErr
}

func TestDiagnoseExperimentalSliceKeepsSuccessfulRawRead(t *testing.T) {
	service := &stubService{
		snapshot: monitor.Snapshot{Generation: 7, Monitors: []monitor.Descriptor{{
			ID: "mon-a", Model: "Dell", Serial: "sensitive", EDID: make([]byte, 128), EDIDSHA256: "hash",
			SupportState: monitor.SupportBackendExperimental, BackendName: "fake",
		}}},
		current: monitor.InputValue{Raw: 0x19}, inputsErr: monitor.ErrUnqualifiedSlice,
	}
	runner, stdout, stderr := newTestRunner(service)
	output := filepath.Join(t.TempDir(), "diagnostic.json")
	if code := runner.Run(context.Background(), []string{"diagnose", "--monitor", "mon-a", "--output", output}); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sensitive") || !strings.Contains(string(raw), `"current_raw": 25`) || !strings.Contains(string(raw), `"enabled": true`) {
		t.Fatalf("unexpected diagnostic: %s", raw)
	}
}
func (s *stubService) Switch(_ context.Context, ref monitor.MonitorRef, input monitor.Input) (monitor.SwitchResult, error) {
	s.switches++
	return monitor.SwitchResult{Monitor: ref, Target: monitor.InputValue{Logical: input}}, s.switchErr
}

func newTestRunner(service Service) (*Runner, *bytes.Buffer, *bytes.Buffer) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := New(Dependencies{
		Version: "1.2.3", Commit: "abc123",
		NewRuntime: func() (Runtime, error) {
			return Runtime{Service: service, Platform: platform.Info{OS: "test", Architecture: "test"}}, nil
		},
	}, stdout, stderr)
	return runner, stdout, stderr
}

func TestVersionDoesNotInitializeBackend(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	called := false
	runner := New(Dependencies{Version: "1", Commit: "c", NewRuntime: func() (Runtime, error) {
		called = true
		return Runtime{}, errors.New("should not run")
	}}, stdout, stderr)
	if code := runner.Run(context.Background(), []string{"version"}); code != 0 || called || !strings.Contains(stdout.String(), "1 (c)") {
		t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, stdout, stderr)
	}
}

func TestListJSONIsVersionedAndStdoutOnly(t *testing.T) {
	service := &stubService{
		snapshot: monitor.Snapshot{Generation: 4, Monitors: []monitor.Descriptor{{ID: "mon-a", Model: "Dell", SupportState: monitor.SupportSupported}}},
		current:  monitor.InputValue{Logical: monitor.InputHDMI1, Raw: 0x11},
	}
	runner, stdout, stderr := newTestRunner(service)
	if code := runner.Run(context.Background(), []string{"list", "--json"}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var output listJSON
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.SchemaVersion != 1 || output.Generation != 4 || len(output.Monitors) != 1 || stderr.Len() != 0 {
		t.Fatalf("unexpected output=%+v stderr=%q", output, stderr)
	}
}

func TestLegacyConfigRejectedBeforeBackendInitialization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, []byte(`{"backend":"cli","hotkeys":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := New(Dependencies{NewRuntime: func() (Runtime, error) {
		called = true
		return Runtime{}, nil
	}}, stdout, stderr)
	code := runner.Run(context.Background(), []string{"--config", path, "list"})
	if code != 2 || called || !strings.Contains(stderr.String(), "configuration schema is unsupported") {
		t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, stdout, stderr)
	}
}

func TestNormalInputSetCannotBypassQualification(t *testing.T) {
	service := &stubService{
		snapshot:  monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon-a", SupportState: monitor.SupportBackendExperimental}}},
		switchErr: monitor.ErrUnqualifiedSlice,
	}
	runner, _, stderr := newTestRunner(service)
	code := runner.Run(context.Background(), []string{"input", "set", "--monitor", "mon-a", "--input", "hdmi-1"})
	if code != 3 || service.switches != 1 || !strings.Contains(stderr.String(), "not qualified") {
		t.Fatalf("code=%d switches=%d stderr=%q", code, service.switches, stderr)
	}
}

func TestExitCodeClasses(t *testing.T) {
	tests := map[error]int{
		nil:                                 0,
		monitor.ErrMonitorNotFound:          3,
		monitor.ErrNoMonitors:               3,
		monitor.ErrPrivateSymbolUnavailable: 4,
		monitor.ErrPermissionDenied:         4,
		monitor.ErrChecksum:                 5,
		monitor.ErrReadUnsupported:          5,
		monitor.ErrUnsupportedConfigSchema:  2,
		errors.New("unknown"):               2,
	}
	for err, want := range tests {
		if got := ExitCode(err); got != want {
			t.Fatalf("ExitCode(%v)=%d want %d", err, got, want)
		}
	}
}

func TestInputCommandsAndInspectJSON(t *testing.T) {
	service := &stubService{
		snapshot: monitor.Snapshot{Generation: 9, Monitors: []monitor.Descriptor{{ID: "mon-a", Model: "Dell", SupportState: monitor.SupportSupported}}},
		current:  monitor.InputValue{Logical: monitor.InputHDMI1, Raw: 0x11},
		inputs:   []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: 0x11}, {Logical: monitor.InputHDMI2, Raw: 0x12}},
	}
	tests := []struct {
		name     string
		args     []string
		contains string
	}{
		{"input-list-json", []string{"input", "list", "--monitor", "mon-a", "--json"}, `"inputs"`},
		{"input-get-json", []string{"input", "get", "--monitor", "mon-a", "--json"}, `"raw": 17`},
		{"input-get-text", []string{"input", "probe", "--monitor", "mon-a"}, "hdmi-1 (raw 0x11)"},
		{"input-set-json", []string{"input", "set", "--monitor", "mon-a", "--input", "hdmi-2", "--json"}, `"result"`},
		{"inspect", []string{"inspect", "--all", "--json"}, `"generation": 9`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, stdout, stderr := newTestRunner(service)
			if code := runner.Run(context.Background(), tt.args); code != 0 || !strings.Contains(stdout.String(), tt.contains) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
	if service.switches != 1 {
		t.Fatalf("switches=%d", service.switches)
	}
}

func TestGetUnknownRawReturnsWarningAndListTextIncludesErrors(t *testing.T) {
	service := &stubService{
		snapshot: monitor.Snapshot{Generation: 1, Monitors: []monitor.Descriptor{{ID: "mon-a", Model: "Dell", SupportState: monitor.SupportBackendExperimental}}},
		current:  monitor.InputValue{Raw: 0x99}, currentErr: monitor.ErrUnknownInput,
	}
	runner, stdout, stderr := newTestRunner(service)
	if code := runner.Run(context.Background(), []string{"input", "get", "--monitor", "mon-a", "--json"}); code != 0 || !strings.Contains(stdout.String(), `"warning"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	runner, stdout, stderr = newTestRunner(service)
	if code := runner.Run(context.Background(), []string{"list"}); code != 0 || !strings.Contains(stdout.String(), "raw-0x99") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestCommandParsingAndDiscoveryFailures(t *testing.T) {
	service := &stubService{}
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"no-command", nil, 2},
		{"help", []string{"--help"}, 0},
		{"help-command", []string{"help"}, 0},
		{"unknown", []string{"unknown"}, 2},
		{"input-missing-subcommand", []string{"input"}, 2},
		{"input-unknown", []string{"input", "unknown"}, 2},
		{"input-list-invalid", []string{"input", "list"}, 2},
		{"input-get-invalid", []string{"input", "get"}, 2},
		{"input-set-invalid", []string{"input", "set"}, 2},
		{"inspect-invalid", []string{"inspect"}, 2},
		{"diagnose-invalid", []string{"diagnose"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, _, _ := newTestRunner(service)
			if got := runner.Run(context.Background(), tt.args); got != tt.code {
				t.Fatalf("got %d want %d", got, tt.code)
			}
		})
	}
	service.discoverErr = monitor.ErrNoMonitors
	runner, _, stderr := newTestRunner(service)
	if code := runner.Run(context.Background(), []string{"list"}); code != 3 || !strings.Contains(stderr.String(), "no external monitors") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	service.discoverErr = nil
	runner, _, stderr = newTestRunner(service)
	if code := runner.Run(context.Background(), []string{"input", "get", "--monitor", "missing"}); code != 3 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}
