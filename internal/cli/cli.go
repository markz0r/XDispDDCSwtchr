package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/config"
	"github.com/markz0r/XDispDDCSwtchr/internal/diagnostics"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/platform"
	"github.com/markz0r/XDispDDCSwtchr/internal/qualification"
)

const JSONSchemaVersion = 2

type Service interface {
	ConfigureUserQualifications([]qualification.Record, func(qualification.Record) error) error
	Discover(context.Context) (monitor.Snapshot, error)
	Inputs(monitor.MonitorRef) ([]monitor.InputValue, error)
	GetInput(context.Context, monitor.MonitorRef) (monitor.InputValue, error)
	DetectInputs(context.Context, monitor.MonitorRef) (monitor.CapabilityReport, error)
	Switch(context.Context, monitor.MonitorRef, monitor.Input) (monitor.SwitchResult, error)
}

type Runtime struct {
	Service  Service
	Platform platform.Info
}

type Dependencies struct {
	NewRuntime func() (Runtime, error)
	Version    string
	Commit     string
	Now        func() time.Time
}

type Runner struct {
	deps   Dependencies
	stdout io.Writer
	stderr io.Writer
}

func New(deps Dependencies, stdout, stderr io.Writer) *Runner {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Runner{deps: deps, stdout: stdout, stderr: stderr}
}

func (r *Runner) Run(ctx context.Context, args []string) int {
	root := flag.NewFlagSet("xdispddcswtchr", flag.ContinueOnError)
	root.SetOutput(r.stderr)
	configPath := root.String("config", "", "configuration file")
	help := root.Bool("help", false, "show help")
	if err := root.Parse(args); err != nil {
		return 2
	}
	remaining := root.Args()
	if *help || len(remaining) == 0 {
		r.usage()
		if *help {
			return 0
		}
		return 2
	}
	if remaining[0] == "version" {
		fmt.Fprintf(r.stdout, "xdispddcswtchr %s (%s)\n", r.deps.Version, r.deps.Commit)
		return 0
	}

	path, err := config.ResolvePath(*configPath)
	if err != nil {
		return r.fail(err)
	}
	settings, err := config.Load(path)
	if err != nil {
		return r.fail(err)
	}
	runtime, err := r.deps.NewRuntime()
	if err != nil {
		return r.fail(err)
	}
	if err := runtime.Service.ConfigureUserQualifications(settings.UserQualifications, nil); err != nil {
		return r.fail(err)
	}

	switch remaining[0] {
	case "list":
		return r.list(ctx, runtime.Service, remaining[1:])
	case "input":
		return r.input(ctx, runtime.Service, settings, remaining[1:])
	case "inspect":
		return r.inspect(ctx, runtime, remaining[1:])
	case "diagnose":
		return r.diagnose(ctx, runtime, settings, remaining[1:])
	case "help":
		r.usage()
		return 0
	default:
		return r.fail(fmt.Errorf("unknown command %q", remaining[0]))
	}
}

type monitorJSON struct {
	monitor.Descriptor
	Current *monitor.InputValue `json:"current,omitempty"`
	Error   string              `json:"error,omitempty"`
}

type listJSON struct {
	SchemaVersion int                `json:"schema_version"`
	Generation    monitor.Generation `json:"generation"`
	Monitors      []monitorJSON      `json:"monitors"`
}

func (r *Runner) list(ctx context.Context, service Service, args []string) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	snapshot, err := service.Discover(ctx)
	if err != nil {
		return r.fail(err)
	}
	result := listJSON{SchemaVersion: JSONSchemaVersion, Generation: snapshot.Generation, Monitors: make([]monitorJSON, 0, len(snapshot.Monitors))}
	for _, descriptor := range snapshot.Monitors {
		value, readErr := service.GetInput(ctx, monitor.MonitorRef{ID: descriptor.ID, Generation: snapshot.Generation})
		item := monitorJSON{Descriptor: descriptor}
		if readErr == nil || errors.Is(readErr, monitor.ErrUnknownInput) {
			item.Current = &value
		}
		if readErr != nil {
			item.Error = readErr.Error()
		}
		result.Monitors = append(result.Monitors, item)
	}
	if *asJSON {
		return r.writeJSON(result)
	}
	fmt.Fprintln(r.stdout, "ID\tMODEL\tCONNECTOR\tCURRENT\tSUPPORT")
	for _, item := range result.Monitors {
		current := "unknown"
		if item.Current != nil {
			if item.Current.Logical != "" {
				current = string(item.Current.Logical)
			} else {
				current = fmt.Sprintf("raw-0x%02x", item.Current.Raw)
			}
		}
		fmt.Fprintf(r.stdout, "%s\t%s\t%s\t%s\t%s\n", item.ID, item.Model, item.Connector, current, item.SupportState)
	}
	return 0
}

func (r *Runner) input(ctx context.Context, service Service, settings config.Settings, args []string) int {
	if len(args) == 0 {
		return r.fail(errors.New("input requires list, get, detect, probe, capabilities, or set"))
	}
	switch args[0] {
	case "list":
		return r.inputList(ctx, service, args[1:])
	case "get":
		return r.inputGet(ctx, service, args[1:])
	case "detect", "probe", "capabilities":
		return r.inputDetect(ctx, service, args[1:])
	case "set":
		return r.inputSet(ctx, service, settings, args[1:])
	default:
		return r.fail(fmt.Errorf("unknown input command %q", args[0]))
	}
}

func (r *Runner) inputDetect(ctx context.Context, service Service, args []string) int {
	flags := flag.NewFlagSet("input detect", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	monitorID := flags.String("monitor", "", "stable monitor ID")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || *monitorID == "" || flags.NArg() != 0 {
		return 2
	}
	_, descriptor, ref, err := discoverMonitor(ctx, service, *monitorID)
	if err != nil {
		return r.fail(err)
	}
	report, err := service.DetectInputs(ctx, ref)
	if err != nil {
		return r.fail(err)
	}
	if *asJSON {
		return r.writeJSON(struct {
			SchemaVersion int                      `json:"schema_version"`
			Monitor       monitor.Descriptor       `json:"monitor"`
			Capabilities  monitor.CapabilityReport `json:"capabilities"`
		}{JSONSchemaVersion, descriptor, report})
	}
	fmt.Fprintf(r.stdout, "VCP 0x60 advertised: %t\n", report.InputSourceAdvertised)
	fmt.Fprintln(r.stdout, "RAW\tLOGICAL\tMAPPING\tWRITE-QUALIFIED")
	for _, candidate := range report.Inputs {
		logical := string(candidate.Logical)
		if logical == "" {
			logical = "unknown"
		}
		fmt.Fprintf(r.stdout, "0x%02x\t%s\t%s\t%t\n", candidate.Raw, logical, candidate.MappingSource, candidate.WriteQualified)
	}
	if len(report.Inputs) == 0 {
		fmt.Fprintln(r.stdout, "(no input values advertised)")
	}
	fmt.Fprintln(r.stdout, "Advisory only: monitor capability data does not authorise writes.")
	return 0
}

func (r *Runner) inputList(ctx context.Context, service Service, args []string) int {
	flags := flag.NewFlagSet("input list", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	monitorID := flags.String("monitor", "", "stable monitor ID")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || *monitorID == "" || flags.NArg() != 0 {
		return 2
	}
	snapshot, descriptor, ref, err := discoverMonitor(ctx, service, *monitorID)
	_ = snapshot
	if err != nil {
		return r.fail(err)
	}
	inputs, err := service.Inputs(ref)
	if err != nil {
		return r.fail(err)
	}
	if *asJSON {
		return r.writeJSON(struct {
			SchemaVersion int                  `json:"schema_version"`
			Monitor       monitor.Descriptor   `json:"monitor"`
			Inputs        []monitor.InputValue `json:"inputs"`
		}{JSONSchemaVersion, descriptor, inputs})
	}
	for _, value := range inputs {
		fmt.Fprintf(r.stdout, "%s\t0x%02x\n", value.Logical, value.Raw)
	}
	return 0
}

func (r *Runner) inputGet(ctx context.Context, service Service, args []string) int {
	flags := flag.NewFlagSet("input get", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	monitorID := flags.String("monitor", "", "stable monitor ID")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || *monitorID == "" || flags.NArg() != 0 {
		return 2
	}
	_, descriptor, ref, err := discoverMonitor(ctx, service, *monitorID)
	if err != nil {
		return r.fail(err)
	}
	value, err := service.GetInput(ctx, ref)
	if err != nil && !errors.Is(err, monitor.ErrUnknownInput) {
		return r.fail(err)
	}
	if *asJSON {
		return r.writeJSON(struct {
			SchemaVersion int                `json:"schema_version"`
			Monitor       monitor.Descriptor `json:"monitor"`
			Input         monitor.InputValue `json:"input"`
			Warning       string             `json:"warning,omitempty"`
		}{JSONSchemaVersion, descriptor, value, errorString(err)})
	}
	logical := string(value.Logical)
	if logical == "" {
		logical = "unknown"
	}
	fmt.Fprintf(r.stdout, "%s (raw 0x%02x)\n", logical, value.Raw)
	return 0
}

func (r *Runner) inputSet(ctx context.Context, service Service, settings config.Settings, args []string) int {
	flags := flag.NewFlagSet("input set", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	monitorID := flags.String("monitor", "", "stable monitor ID")
	input := flags.String("input", "", "logical input")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || *monitorID == "" || *input == "" || flags.NArg() != 0 {
		return 2
	}
	logical := *input
	if alias, ok := settings.InputAliases[logical]; ok {
		logical = alias
	}
	_, _, ref, err := discoverMonitor(ctx, service, *monitorID)
	if err != nil {
		return r.fail(err)
	}
	result, err := service.Switch(ctx, ref, monitor.Input(logical))
	if err != nil {
		if result.Verification == monitor.VerificationUnknown && result.Attempts > 0 {
			fmt.Fprintln(r.stderr, err)
			return 6
		}
		return r.fail(err)
	}
	if *asJSON {
		return r.writeJSON(struct {
			SchemaVersion int                  `json:"schema_version"`
			Result        monitor.SwitchResult `json:"result"`
		}{JSONSchemaVersion, result})
	}
	if result.Verification == monitor.VerificationAssumedSuccess {
		fmt.Fprintln(r.stdout, "Assumed success [no read-back provided by display]")
		fmt.Fprintln(r.stdout, "Run input get or list after the display path is available to verify the current input.")
		return 0
	}
	fmt.Fprintf(r.stdout, "%s: %s\n", result.Target.Logical, result.Verification)
	return 0
}

func (r *Runner) inspect(ctx context.Context, runtime Runtime, args []string) int {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	all := flags.Bool("all", false, "inspect all monitors")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || !*all || !*asJSON || flags.NArg() != 0 {
		return 2
	}
	snapshot, err := runtime.Service.Discover(ctx)
	if err != nil {
		return r.fail(err)
	}
	return r.writeJSON(struct {
		SchemaVersion int              `json:"schema_version"`
		OS            platform.Info    `json:"os"`
		Snapshot      monitor.Snapshot `json:"snapshot"`
	}{JSONSchemaVersion, runtime.Platform, snapshot})
}

func (r *Runner) diagnose(ctx context.Context, runtime Runtime, settings config.Settings, args []string) int {
	flags := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	monitorID := flags.String("monitor", "", "stable monitor ID")
	output := flags.String("output", "", "diagnostic JSON output path")
	unredacted := flags.Bool("include-sensitive", false, "include serial and raw EDID")
	if err := flags.Parse(args); err != nil || *monitorID == "" || *output == "" || flags.NArg() != 0 {
		return 2
	}
	_, descriptor, ref, err := discoverMonitor(ctx, runtime.Service, *monitorID)
	if err != nil {
		return r.fail(err)
	}
	inputs, inputsErr := runtime.Service.Inputs(ref)
	started := time.Now()
	current, readErr := runtime.Service.GetInput(ctx, ref)
	duration := time.Since(started)
	capabilityReport, capabilityErr := runtime.Service.DetectInputs(ctx, ref)
	if inputsErr != nil && !errors.Is(inputsErr, monitor.ErrUnqualifiedSlice) && readErr == nil {
		readErr = inputsErr
	}
	var currentPointer *monitor.InputValue
	if readErr == nil || errors.Is(readErr, monitor.ErrUnknownInput) {
		currentPointer = &current
	}
	redact := true
	if settings.DiagnosticRedaction != nil {
		redact = *settings.DiagnosticRedaction
	}
	if *unredacted {
		redact = false
	}
	bundle := diagnostics.Build(diagnostics.BuildOptions{
		Now: r.deps.Now(), Version: r.deps.Version, Commit: r.deps.Commit, Platform: runtime.Platform,
		Descriptor: descriptor, Inputs: inputs, Current: currentPointer, ReadDuration: duration, ReadError: readErr, Redact: redact,
		Capabilities: &capabilityReport, CapabilityError: capabilityErr,
	})
	encoded, err := diagnostics.Marshal(bundle)
	if err != nil {
		return r.fail(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(*output, encoded, 0o600); err != nil {
		return r.fail(fmt.Errorf("write diagnostic: %w", err))
	}
	fmt.Fprintln(r.stdout, *output)
	if readErr != nil && !errors.Is(readErr, monitor.ErrUnknownInput) {
		return ExitCode(readErr)
	}
	return 0
}

func discoverMonitor(ctx context.Context, service Service, id string) (monitor.Snapshot, monitor.Descriptor, monitor.MonitorRef, error) {
	snapshot, err := service.Discover(ctx)
	if err != nil {
		return monitor.Snapshot{}, monitor.Descriptor{}, monitor.MonitorRef{}, err
	}
	for _, descriptor := range snapshot.Monitors {
		if descriptor.ID == id {
			return snapshot, descriptor, monitor.MonitorRef{ID: id, Generation: snapshot.Generation}, nil
		}
	}
	return snapshot, monitor.Descriptor{}, monitor.MonitorRef{}, monitor.ErrMonitorNotFound
}

func (r *Runner) writeJSON(value any) int {
	encoder := json.NewEncoder(r.stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return r.fail(err)
	}
	return 0
}

func (r *Runner) fail(err error) int {
	fmt.Fprintln(r.stderr, err)
	return ExitCode(err)
}

func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp), errors.Is(err, monitor.ErrUnsupportedConfigSchema):
		return 2
	case errors.Is(err, monitor.ErrNoMonitors), errors.Is(err, monitor.ErrMonitorNotFound),
		errors.Is(err, monitor.ErrAmbiguousMonitor), errors.Is(err, monitor.ErrUnsupportedMonitor),
		errors.Is(err, monitor.ErrUnqualifiedSlice), errors.Is(err, monitor.ErrUnknownInput):
		return 3
	case errors.Is(err, monitor.ErrBackendUnavailable), errors.Is(err, monitor.ErrPrivateSymbolUnavailable),
		errors.Is(err, monitor.ErrEndpointNotFound), errors.Is(err, monitor.ErrPermissionDenied), errors.Is(err, monitor.ErrDDCDisabled):
		return 4
	case errors.Is(err, monitor.ErrTransactionTimeout), errors.Is(err, monitor.ErrMalformedReply),
		errors.Is(err, monitor.ErrChecksum), errors.Is(err, monitor.ErrDisconnected),
		errors.Is(err, monitor.ErrReadUnsupported), errors.Is(err, monitor.ErrCapabilitiesUnsupported),
		errors.Is(err, monitor.ErrWriteUnsupported):
		return 5
	default:
		return 2
	}
}

func (r *Runner) usage() {
	lines := []string{
		"usage: xdispddcswtchr [--config path] <command>",
		"commands:",
		"  list [--json]",
		"  input list --monitor <id> [--json]",
		"  input get --monitor <id> [--json]",
		"  input detect --monitor <id> [--json]",
		"  input set --monitor <id> --input <logical> [--json]",
		"  inspect --all --json",
		"  diagnose --monitor <id> --output <path>",
		"  version",
	}
	fmt.Fprintln(r.stderr, strings.Join(lines, "\n"))
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
