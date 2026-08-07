//go:build qualification

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/backend"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/platform"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
	"github.com/markz0r/XDispDDCSwtchr/internal/verification"
)

const qualificationSchemaVersion = 2

type exactMonitor struct {
	ID         string
	Model      string
	EDIDSHA256 string
}

type captureOptions struct {
	exactMonitor
	Label    monitor.Input
	OSDLabel string
}

type writeOptions struct {
	exactMonitor
	Source            monitor.Input
	Target            monitor.Input
	UnsafeAllowWrite  bool
	AcknowledgeSwitch bool
	RecoveryMethod    string
}

type record struct {
	SchemaVersion        int                        `json:"schema_version"`
	GeneratedAt          time.Time                  `json:"generated_at"`
	Operation            string                     `json:"operation"`
	OS                   platform.Info              `json:"os"`
	Monitor              monitor.Descriptor         `json:"monitor"`
	Label                monitor.Input              `json:"label,omitempty"`
	OSDLabel             string                     `json:"osd_label,omitempty"`
	OSDInputs            []osdInputChoice           `json:"osd_inputs,omitempty"`
	Determination        *determinationAssessment   `json:"determination,omitempty"`
	Source               monitor.InputValue         `json:"source,omitempty"`
	Target               monitor.InputValue         `json:"target,omitempty"`
	ObservedRaw          *uint16                    `json:"observed_raw,omitempty"`
	Verification         monitor.VerificationResult `json:"verification,omitempty"`
	VerificationAttempts int                        `json:"verification_attempts,omitempty"`
	VerificationIssue    *monitor.VerificationIssue `json:"verification_issue,omitempty"`
	RecoveryMethod       string                     `json:"recovery_method,omitempty"`
}

type osdInputChoice struct {
	OSDLabel string        `json:"osd_label"`
	Logical  monitor.Input `json:"logical"`
}

type fieldAssessment struct {
	Status      string `json:"status"`
	Instruction string `json:"instruction"`
}

type determinationAssessment struct {
	CurrentRaw         fieldAssessment `json:"current_raw"`
	CurrentOSDLabel    fieldAssessment `json:"current_osd_label"`
	SafeTarget         fieldAssessment `json:"safe_connected_target"`
	ConnectionPath     fieldAssessment `json:"connection_path"`
	RecoveryMethod     fieldAssessment `json:"recovery_method"`
	AutomaticRawWrites bool            `json:"automatic_raw_writes"`
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr *os.File) int {
	if len(args) < 2 || args[0] != "input" {
		usage(stderr)
		return 2
	}
	native, err := backend.New()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 4
	}
	registry, err := profiles.NewRegistry()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	current, err := platform.Current()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 4
	}
	var result record
	switch args[1] {
	case "preflight":
		options, parseErr := parseExact(args[2:], "input preflight", stderr)
		if parseErr != nil {
			return 2
		}
		result, err = preflight(ctx, native, current, options)
	case "capture":
		options, parseErr := parseCapture(args[2:], stderr)
		if parseErr != nil {
			return 2
		}
		result, err = capture(ctx, native, current, options)
	case "write":
		options, parseErr := parseWrite(args[2:], stderr)
		if parseErr != nil {
			return 2
		}
		fmt.Fprintln(stderr, "WARNING: qualification-only VCP 0x60 write may switch the display away from this host.")
		result, err = write(ctx, native, registry, current, options)
	default:
		usage(stderr)
		return 2
	}
	if err != nil {
		if result.Operation != "" {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if encodeErr := encoder.Encode(result); encodeErr != nil {
				fmt.Fprintln(stderr, encodeErr)
				return 2
			}
		}
		fmt.Fprintln(stderr, err)
		return qualificationExitCode(err)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

func parseCapture(args []string, stderr *os.File) (captureOptions, error) {
	flags := flag.NewFlagSet("input capture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options captureOptions
	flags.StringVar(&options.ID, "monitor", "", "exact stable monitor ID")
	flags.StringVar(&options.Model, "expected-model", "", "exact expected model")
	flags.StringVar(&options.EDIDSHA256, "expected-edid-sha256", "", "exact expected EDID SHA-256")
	label := flags.String("label", "", "operator-observed logical input label")
	flags.StringVar(&options.OSDLabel, "osd-label", "", "exact input label visible in the monitor OSD")
	if err := flags.Parse(args); err != nil {
		return captureOptions{}, err
	}
	options.Label = monitor.Input(*label)
	if flags.NArg() != 0 || options.ID == "" || options.Model == "" || options.EDIDSHA256 == "" ||
		!knownInput(options.Label) || strings.TrimSpace(options.OSDLabel) == "" {
		return captureOptions{}, errors.New("capture requires exact --monitor, --expected-model, --expected-edid-sha256, a known logical --label, and the exact visible --osd-label")
	}
	if choices := osdInputs(options.Model); len(choices) > 0 && !modelHasInput(choices, options.Label) {
		return captureOptions{}, fmt.Errorf("%s does not have logical input %s; run input preflight for valid choices", options.Model, options.Label)
	}
	return options, nil
}

func parseExact(args []string, name string, stderr *os.File) (exactMonitor, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options exactMonitor
	flags.StringVar(&options.ID, "monitor", "", "exact stable monitor ID")
	flags.StringVar(&options.Model, "expected-model", "", "exact expected model")
	flags.StringVar(&options.EDIDSHA256, "expected-edid-sha256", "", "exact expected EDID SHA-256")
	if err := flags.Parse(args); err != nil {
		return exactMonitor{}, err
	}
	if flags.NArg() != 0 || options.ID == "" || options.Model == "" || options.EDIDSHA256 == "" {
		return exactMonitor{}, errors.New("preflight requires exact --monitor, --expected-model, and --expected-edid-sha256")
	}
	return options, nil
}

func parseWrite(args []string, stderr *os.File) (writeOptions, error) {
	flags := flag.NewFlagSet("input write", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options writeOptions
	flags.StringVar(&options.ID, "monitor", "", "exact stable monitor ID")
	flags.StringVar(&options.Model, "expected-model", "", "exact expected model")
	flags.StringVar(&options.EDIDSHA256, "expected-edid-sha256", "", "exact expected EDID SHA-256")
	source := flags.String("source-input", "", "operator-confirmed current logical input")
	target := flags.String("target-input", "", "single target logical input")
	flags.BoolVar(&options.UnsafeAllowWrite, "unsafe-allow-input-write", false, "enable qualification-only VCP 0x60 write")
	flags.BoolVar(&options.AcknowledgeSwitch, "acknowledge-switch-away", false, "acknowledge display may become unavailable")
	flags.StringVar(&options.RecoveryMethod, "recovery-method", "", "operator recovery method")
	if err := flags.Parse(args); err != nil {
		return writeOptions{}, err
	}
	options.Source = monitor.Input(*source)
	options.Target = monitor.Input(*target)
	if flags.NArg() != 0 || options.ID == "" || options.Model == "" || options.EDIDSHA256 == "" ||
		!knownInput(options.Source) || !knownInput(options.Target) || options.Source == options.Target ||
		!options.UnsafeAllowWrite || !options.AcknowledgeSwitch || strings.TrimSpace(options.RecoveryMethod) == "" {
		return writeOptions{}, errors.New("write requires exact identity, distinct known source/target inputs, both safety acknowledgements, and a recovery method")
	}
	return options, nil
}

func capture(ctx context.Context, native monitor.Backend, current platform.Info, options captureOptions) (record, error) {
	descriptor, ref, err := selectExact(ctx, native, options.exactMonitor)
	if err != nil {
		return record{}, err
	}
	session, err := native.Open(ctx, ref)
	if err != nil {
		return record{}, err
	}
	raw, readErr := session.GetInputRaw(ctx)
	closeErr := session.Close()
	if readErr != nil {
		return record{}, readErr
	}
	if closeErr != nil {
		return record{}, closeErr
	}
	return record{
		SchemaVersion: qualificationSchemaVersion, GeneratedAt: time.Now().UTC(), Operation: "input-capture",
		OS: current, Monitor: descriptor, Label: options.Label, OSDLabel: strings.TrimSpace(options.OSDLabel), ObservedRaw: &raw,
	}, nil
}

func preflight(ctx context.Context, native monitor.Backend, current platform.Info, options exactMonitor) (record, error) {
	descriptor, ref, err := selectExact(ctx, native, options)
	if err != nil {
		return record{}, err
	}
	session, err := native.Open(ctx, ref)
	if err != nil {
		return record{}, err
	}
	raw, readErr := session.GetInputRaw(ctx)
	closeErr := session.Close()
	if readErr != nil {
		return record{}, readErr
	}
	if closeErr != nil {
		return record{}, closeErr
	}
	return record{
		SchemaVersion: qualificationSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Operation:     "input-preflight",
		OS:            current,
		Monitor:       descriptor,
		ObservedRaw:   &raw,
		OSDInputs:     osdInputs(descriptor.Model),
		Determination: &determinationAssessment{
			CurrentRaw:         fieldAssessment{Status: "automatic", Instruction: "Read-only DDC/CI VCP 0x60 value captured above."},
			CurrentOSDLabel:    fieldAssessment{Status: "operator-confirmation-required", Instruction: "Open Input Source in the monitor OSD and record the selected visible label; DDC/CI does not return this text."},
			SafeTarget:         fieldAssessment{Status: "operator-confirmation-required", Instruction: "Choose a different listed input only after confirming that it has a live connected source."},
			ConnectionPath:     fieldAssessment{Status: "partially-automatic", Instruction: fmt.Sprintf("The backend endpoint is %q; record whether the video cable is direct or traverses a named dock, adapter, or KVM.", descriptor.Connector)},
			RecoveryMethod:     fieldAssessment{Status: "operator-confirmation-required", Instruction: "Use and test a physical OSD path back to the current input; do not depend on DDC remaining reachable after a switch."},
			AutomaticRawWrites: false,
		},
	}, nil
}

func write(ctx context.Context, native monitor.Backend, registry *profiles.Registry, current platform.Info, options writeOptions) (record, error) {
	descriptor, ref, err := selectExact(ctx, native, options.exactMonitor)
	if err != nil {
		return record{}, err
	}
	profile, ok := registry.Match(descriptor.Manufacturer, descriptor.ProductCode, descriptor.Model)
	if !ok {
		return record{}, monitor.ErrUnsupportedMonitor
	}
	source, err := profile.Resolve(options.Source)
	if err != nil {
		return record{}, err
	}
	target, err := profile.Resolve(options.Target)
	if err != nil {
		return record{}, err
	}
	session, err := native.Open(ctx, ref)
	if err != nil {
		return record{}, err
	}
	defer session.Close()
	observed, err := session.GetInputRaw(ctx)
	if err != nil {
		return record{}, err
	}
	if observed != source.Raw {
		return record{}, fmt.Errorf("current raw input 0x%02x does not match declared source %s (0x%02x)", observed, source.Logical, source.Raw)
	}
	if err := session.SetInputRaw(ctx, target.Raw); err != nil {
		return record{}, err
	}
	result := record{
		SchemaVersion: qualificationSchemaVersion, GeneratedAt: time.Now().UTC(), Operation: "input-write",
		OS: current, Monitor: descriptor, Source: source, Target: target,
		Verification: monitor.VerificationWriteAccepted, RecoveryMethod: options.RecoveryMethod,
	}
	timer := time.NewTimer(profile.WriteDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return record{}, ctx.Err()
	case <-timer.C:
	}
	readBack, attempts, err := qualificationReadWithRetry(ctx, session, profile)
	result.VerificationAttempts = attempts
	if err != nil {
		result.VerificationIssue = verification.Issue(err)
		if errors.Is(err, monitor.ErrDisconnected) || errors.Is(err, monitor.ErrEndpointNotFound) {
			result.Verification = monitor.VerificationDisplayPathChanged
			return result, nil
		}
		if verification.AssumableReadback(err) {
			result.Verification = monitor.VerificationAssumedSuccess
			return result, fmt.Errorf("%w: successful write had no valid display read-back: %v", monitor.ErrQualificationIncomplete, err)
		}
		return result, err
	}
	result.ObservedRaw = &readBack
	if readBack != target.Raw {
		result.Verification = monitor.VerificationUnknown
		return result, fmt.Errorf("%w: write verification read 0x%02x, expected 0x%02x", monitor.ErrQualificationIncomplete, readBack, target.Raw)
	}
	result.Verification = monitor.VerificationConfirmed
	return result, nil
}

func qualificationReadWithRetry(ctx context.Context, session monitor.RawInputSession, profile *profiles.Profile) (uint16, int, error) {
	for attempt := 1; ; attempt++ {
		raw, err := session.GetInputRaw(ctx)
		if err == nil {
			return raw, attempt, nil
		}
		if attempt > profile.RetryCount || !qualificationRetryableRead(err) {
			return 0, attempt, err
		}
		timer := time.NewTimer(profile.ReplyDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, attempt, ctx.Err()
		case <-timer.C:
		}
	}
}

func qualificationRetryableRead(err error) bool {
	return errors.Is(err, monitor.ErrTransactionNACK) ||
		errors.Is(err, monitor.ErrTransactionTimeout) ||
		errors.Is(err, monitor.ErrMalformedReply) ||
		errors.Is(err, monitor.ErrChecksum)
}

func selectExact(ctx context.Context, native monitor.Backend, expected exactMonitor) (monitor.Descriptor, monitor.MonitorRef, error) {
	snapshot, err := native.Enumerate(ctx)
	if err != nil {
		return monitor.Descriptor{}, monitor.MonitorRef{}, err
	}
	for _, descriptor := range snapshot.Monitors {
		if descriptor.ID != expected.ID {
			continue
		}
		if descriptor.Model != expected.Model || descriptor.EDIDSHA256 != expected.EDIDSHA256 {
			return monitor.Descriptor{}, monitor.MonitorRef{}, fmt.Errorf("selected monitor identity does not match expected model and EDID hash")
		}
		return descriptor, monitor.MonitorRef{ID: descriptor.ID, Generation: snapshot.Generation}, nil
	}
	return monitor.Descriptor{}, monitor.MonitorRef{}, monitor.ErrMonitorNotFound
}

func knownInput(input monitor.Input) bool {
	switch input {
	case monitor.InputDisplayPort1, monitor.InputDisplayPort2, monitor.InputHDMI1, monitor.InputHDMI2,
		monitor.InputUSBC1, monitor.InputUSBC2, monitor.InputThunderbolt1:
		return true
	default:
		return false
	}
}

func osdInputs(model string) []osdInputChoice {
	model = strings.ToUpper(strings.Join(strings.Fields(model), " "))
	var choices []osdInputChoice
	switch model {
	case "U4025QW", "DELL U4025QW":
		choices = []osdInputChoice{
			{OSDLabel: "Thunderbolt (140W)", Logical: monitor.InputThunderbolt1},
			{OSDLabel: "DP", Logical: monitor.InputDisplayPort1},
			{OSDLabel: "HDMI", Logical: monitor.InputHDMI1},
		}
	case "S3423DWC", "DELL S3423DWC":
		choices = []osdInputChoice{
			{OSDLabel: "USB-C", Logical: monitor.InputUSBC1},
			{OSDLabel: "HDMI 1", Logical: monitor.InputHDMI1},
			{OSDLabel: "HDMI 2", Logical: monitor.InputHDMI2},
		}
	case "LG ULTRAGEAR+", "45GX950A", "45GX950A-B":
		choices = []osdInputChoice{
			{OSDLabel: "HDMI-1", Logical: monitor.InputHDMI1},
			{OSDLabel: "HDMI-2", Logical: monitor.InputHDMI2},
			{OSDLabel: "DisplayPort-1", Logical: monitor.InputDisplayPort1},
			{OSDLabel: "DisplayPort-2", Logical: monitor.InputDisplayPort2},
		}
	}
	return append([]osdInputChoice(nil), choices...)
}

func modelHasInput(choices []osdInputChoice, input monitor.Input) bool {
	for _, choice := range choices {
		if choice.Logical == input {
			return true
		}
	}
	return false
}

func qualificationExitCode(err error) int {
	switch {
	case errors.Is(err, monitor.ErrNoMonitors), errors.Is(err, monitor.ErrMonitorNotFound), errors.Is(err, monitor.ErrUnsupportedMonitor), errors.Is(err, monitor.ErrUnknownInput):
		return 3
	case errors.Is(err, monitor.ErrBackendUnavailable), errors.Is(err, monitor.ErrPrivateSymbolUnavailable), errors.Is(err, monitor.ErrEndpointNotFound), errors.Is(err, monitor.ErrPermissionDenied):
		return 4
	case errors.Is(err, monitor.ErrDisconnected), errors.Is(err, monitor.ErrMalformedReply), errors.Is(err, monitor.ErrChecksum), errors.Is(err, monitor.ErrTransactionTimeout):
		return 5
	default:
		return 6
	}
}

func usage(output *os.File) {
	fmt.Fprintln(output, "usage: xdispddcswtchr-qualify input preflight|capture|write [guarded options]")
}
