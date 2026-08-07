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

An implemented backend is not automatically a supported hardware claim. A
display is `supported` only when the exact OS build, architecture, application
commit, backend, profile, EDID hash, connector, and qualified input appear in
the embedded support matrix.

An unqualified display is still usable through an explicit local decision. The
TUI safely reads its advertised VCP `0x60` values, requires the user to accept,
relabel, or skip every value, and shows a final persistence confirmation. The
saved record is scoped to the exact stable monitor ID, EDID hash, connector,
and backend. It enables only the accepted logical inputs and marks the display
`user-qualified`; it does not create or imply a release support claim. Before
each locally qualified switch, the application re-reads and matches the
reviewed capability string. A changed response blocks the write and requires a
new review.

The three target model profiles contain tracked, operator-provided VCP `0x60`
capability mappings. These mappings enable guarded hardware qualification; they
also provide suggestions during local review, but do not populate the release
support matrix automatically.

## Build

Go 1.26.5 is the pinned toolchain. Bubble Tea v2.0.8 is the only direct runtime
dependency.

```sh
go build -trimpath -o xdispddcswtchr ./cmd/xdispddcswtchr
./xdispddcswtchr version
```

Run `./xdispddcswtchr` from a terminal for the Bubble Tea interface. In a
non-interactive stream, specify a CLI command. For an unqualified monitor, the
TUI automatically performs read-only capability detection and starts the
guided review. Use left/right to choose a logical label or `skip`, enter to
accept each choice, then enter once more on the summary screen to persist it.
`esc` cancels without enabling writes. Press `c` to repeat detection and `a` to
restart the review manually.

## Commands

```text
xdispddcswtchr list [--json]
xdispddcswtchr input list --monitor <stable-id> [--json]
xdispddcswtchr input get --monitor <stable-id> [--json]
xdispddcswtchr input detect --monitor <stable-id> [--json]
xdispddcswtchr input set --monitor <stable-id> --input <logical-input> [--json]
xdispddcswtchr inspect --all --json
xdispddcswtchr diagnose --monitor <stable-id> --output <path>
xdispddcswtchr version
```

`list`, `input get`, `input detect`, and `inspect` are safe read paths. `input detect`
retrieves the monitor's DDC/CI capabilities string in bounded fragments
and extracts values advertised under `vcp(60(...))`. It labels the standard
DisplayPort/HDMI values and applies a matching monitor profile first for
vendor-specific values such as Dell USB-C/Thunderbolt codes.

Capability strings are advisory because real monitors can omit inputs or
advertise incorrect values. Detection alone never creates a support record or
enables a write. Only the TUI's explicit, complete user review can create an
exact local `user-qualified` record. `input set` accepts logical input names
only; the production binary has no raw VCP command.

Successful input writes have four distinct outcomes. `confirmed` means a valid
read-back matched the target; `display-path-changed` means the endpoint was
lost as expected after switching; `write-accepted` applies only when the active
profile intentionally requires no read-back; and `assumed-success` means the
native Set operation succeeded but the display did not provide a valid
read-back after the configured retries. The TUI renders the last case as
`Assumed success [no read-back provided by display]`, does not mark the target
as the current input, and prompts for rediscovery. CLI `assumed-success` is exit
code `0`; its JSON includes typed verification diagnostics and the raw reply
bytes when available.

CLI JSON output uses `schema_version: 2`. This is independent of the optional
configuration-file schema, which remains version `1`.

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
The TUI may add a `user_qualifications` array to this file. Those records
contain the reviewed identity, endpoint, capability digest, mappings, and
acceptance timestamp. They are application-managed safety records, not a place
for arbitrary raw VCP configuration; use the TUI to revise them.

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
raw values. Its `input preflight` command safely reads the current raw value,
lists the model's physical OSD choices, and identifies which topology and safety
facts still require operator confirmation. See [hardware
qualification](docs/HARDWARE-QUALIFICATION.md).

## Platform implementation notes

The Apple Silicon backend uses public CoreGraphics display enumeration and
IOKit registry ownership, plus dynamically resolved display-service symbols for
the minimum DDC transaction surface. Private symbols are treated as a versioned
compatibility boundary: absence returns a typed error and never falls back to an
external executable. The generic DDC parser remains strict. At the CoreDisplay
boundary only, the backend also accepts the exact checksummed eight-byte reply
suffix observed from Apple Silicon transports when the fixed buffer's remaining
three bytes are zero; it reconstructs the standard header and re-runs the same
strict parser. See [macOS compatibility](docs/MACOS-COMPATIBILITY.md) and
the [migration inventory](docs/MIGRATION-INVENTORY.md).
The production module and licence review is recorded in
[dependencies](docs/DEPENDENCIES.md).

Primary references:

- [Apple CoreGraphics display APIs](https://developer.apple.com/documentation/coregraphics/display-functions)
- [Apple IOKit documentation](https://developer.apple.com/documentation/iokit)
- [Go release history](https://go.dev/doc/devel/release)
- [Bubble Tea releases](https://github.com/charmbracelet/bubbletea/releases)
- [Current ddcutil DDC/CI implementation](https://github.com/rockowitz/ddcutil/tree/e1ace32d15b70af69e36004f0bfbb2f3a00e0be0/src)
- [ddcutil capability-string reliability notes](https://www.ddcutil.com/faq/)
- [Microsoft MCCS capability parser design](https://microsoft.github.io/PowerToys/modules/powerdisplay/mccsParserDesign/)
- [Dell S3423DWC documentation](https://www.dell.com/support/product-details/en-au/product/dell-s3423dwc-monitor/docs)
- [Dell U4025QW documentation](https://www.dell.com/support/product-details/en-au/product/dell-u4025qw-monitor/docs)

The implementation contract and complete acceptance criteria are in
[PLAN.MD](PLAN.MD).
