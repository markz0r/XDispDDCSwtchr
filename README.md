# XDispDDCSwtchr

`xdispddcswtchr` is a terminal application and command-line tool for switching
the input source of external monitors through DDC/CI VCP `0x60`.

The production binary talks directly to native operating-system interfaces. It
does not execute or depend on `ControlMyMonitor.exe`, `ddcutil`, `ddcctl`, or
another monitor-control utility. Global hotkeys, PIP controls, and arbitrary VCP
writes are intentionally outside the product scope.

## Current implementation status

| Slice | State |
| --- | --- |
| macOS 26.6, Apple Silicon native discovery and VCP `0x60` read | Implemented and read-validated on the attached Dell S3423DWC and Dell U4025QW |
| macOS input write | Implemented behind qualification and support-record gates; physical qualification is not yet complete |
| Windows native backend | Planned, not implemented in this incremental slice |
| Linux native backend | Planned, not implemented in this incremental slice |

An implemented backend is not automatically a supported hardware claim. Normal
writes are enabled only when the exact OS build, architecture, application
commit, backend, profile, EDID hash, connector, and qualified input appear in
the embedded support matrix. Otherwise the display remains
`backend-experimental` and the normal CLI/TUI refuses to write.

## Build

Go 1.26.5 is the pinned toolchain. Bubble Tea v2.0.8 is the only direct runtime
dependency.

```sh
go build -trimpath -o xdispddcswtchr ./cmd/xdispddcswtchr
./xdispddcswtchr version
```

Run `./xdispddcswtchr` from a terminal for the Bubble Tea interface. In a
non-interactive stream, specify a CLI command.

## Commands

```text
xdispddcswtchr list [--json]
xdispddcswtchr input list --monitor <stable-id> [--json]
xdispddcswtchr input get --monitor <stable-id> [--json]
xdispddcswtchr input set --monitor <stable-id> --input <logical-input> [--json]
xdispddcswtchr inspect --all --json
xdispddcswtchr diagnose --monitor <stable-id> --output <path>
xdispddcswtchr version
```

`list`, `input get`, and `inspect` are safe read paths. `input set` cannot bypass
the support matrix and accepts logical input names only; the production binary
has no raw VCP command.

## Configuration

Configuration is optional and schema-versioned:

```json
{
  "schema_version": 1,
  "preferred_monitor_id": "mon-example",
  "input_aliases": {
    "work": "usb-c-1",
    "desktop": "displayport-1"
  },
  "diagnostic_redaction": true
}
```

The default path is `~/Library/Application Support/xdispddcswtchr/config.json`
on macOS, the platform user-configuration directory elsewhere, or the path in
`XDISPDDCSWTCHR_CONFIG`. The old `backend`, `cli`, `hotkeys`, `models`,
`monitors`, and `pip` fields are rejected rather than silently accepted.

## Validation

Repository-owned entrypoints write all transient output beneath
`test-artifacts/`:

```sh
./build-support/script/validate.sh quick
./build-support/script/validate.sh full
./build-support/script/validate.sh platform
```

PowerShell exposes the same modes:

```powershell
./build-support/script/validate.ps1 -Mode quick
./build-support/script/validate.ps1 -Mode full
./build-support/script/validate.ps1 -Mode platform
```

Hardware writes require a separately tagged qualification binary, an exact
monitor identity, explicit switch-away acknowledgement, an operator-supplied
recovery method, and a declared source/target pair. It never scans candidate
raw values. See [hardware qualification](docs/HARDWARE-QUALIFICATION.md).

## Platform implementation notes

The Apple Silicon backend uses public CoreGraphics display enumeration and
IOKit registry ownership, plus dynamically resolved display-service symbols for
the minimum DDC transaction surface. Private symbols are treated as a versioned
compatibility boundary: absence returns a typed error and never falls back to an
external executable. See [macOS compatibility](docs/MACOS-COMPATIBILITY.md) and
the [migration inventory](docs/MIGRATION-INVENTORY.md).
The production module and licence review is recorded in
[dependencies](docs/DEPENDENCIES.md).

Primary references:

- [Apple CoreGraphics display APIs](https://developer.apple.com/documentation/coregraphics/display-functions)
- [Apple IOKit documentation](https://developer.apple.com/documentation/iokit)
- [Go release history](https://go.dev/doc/devel/release)
- [Bubble Tea releases](https://github.com/charmbracelet/bubbletea/releases)
- [Dell S3423DWC documentation](https://www.dell.com/support/product-details/en-au/product/dell-s3423dwc-monitor/docs)
- [Dell U4025QW documentation](https://www.dell.com/support/product-details/en-au/product/dell-u4025qw-monitor/docs)

The implementation contract and complete acceptance criteria are in
[PLAN.MD](PLAN.MD).
