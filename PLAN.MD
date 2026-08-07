# PLAN.md — Cross-Platform Monitor Input Source Switcher

## 1. Document status

- **Status:** Implementation and validation plan
- **Plan revision:** 2
- **Last authoritative-source review:** 2026-08-07
- **Primary language:** Go
- **Required Go baseline:** Go 1.26.5 (`go 1.26.0`, pinned CI toolchain)
- **Distributed command:** `xdispddcswtchr`
- **Required user interface:** Terminal UI built with [Charmbracelet Bubble Tea](https://github.com/charmbracelet/bubbletea)
- **Planned operating systems:**
  - Ubuntu 26.04
  - Windows 11
  - macOS Tahoe 26 on Apple Silicon (`darwin/arm64`)
- **Planned monitors:**
  - Dell S3423DWC
  - Dell U4025QW
  - LG UltraGear OLED 45GX950A-B
- **Primary operation:** Switch the active monitor video input using DDC/CI VCP feature code `0x60`

This plan is intentionally limited to input-source switching. Any monitor feature that is not required to discover a monitor, identify it reliably, determine its available inputs, or switch its active input is out of scope.

The terms in this document have strict meanings:

- **Planned:** present on the roadmap, with no support claim.
- **Build-validated:** builds and automated tests pass on the named OS runner, without attached monitor hardware.
- **Experimental:** the native backend is usable for qualification but has not passed the complete hardware gate for the named platform/monitor/connection slice.
- **Supported:** the exact platform, OS build range, monitor identity, connector/topology, and input mappings are recorded in the tracked qualification matrix and all required gates pass.

Support is incremental. A release may support only a subset of the planned matrix. No platform, monitor, connector, dock path, input mapping, or OS point release becomes supported merely because the source compiles or a similar configuration was tested.

### 1.1 Current repository baseline

The controlled legacy replacement is complete for the current incremental
slice:

- The module and validation scripts pin Go 1.26.5 and Bubble Tea v2.
- The normal runtime has no hotkey, PIP/PBP, arbitrary-VCP, or external
  monitor-control executable path.
- Common domain, EDID, profiles, support matrix, service, CLI, TUI,
  diagnostics, deterministic fake backend, and guarded qualification code are
  implemented with automated tests.
- Apple Silicon macOS native discovery and VCP `0x60` reads have read-only
  evidence for the attached Dell S3423DWC and U4025QW; normal writes remain
  support-matrix gated and are not yet hardware-qualified.
- DDC/CI capability retrieval and bounded `vcp(60(...))` parsing are
  implemented as advisory read paths. The TUI can resolve an advisory into an
  exact, persisted local user qualification after reviewing every value; this
  remains distinct from release support qualification.
- Windows and Linux native backends remain planned.

---

## 2. Objective

Implement a self-contained Go application that discovers supported external monitors and switches their active video input in a consistent manner across Linux, Windows, and Apple Silicon macOS.

The implementation must:

1. Present the same monitor and input model on every supported operating system.
2. Use native operating-system interfaces for discovery and DDC/CI transport.
3. Avoid runtime dependencies on third-party monitor-control programs.
4. Minimise third-party Go dependencies.
5. Use Bubble Tea for the interactive terminal user interface.
6. Support reliable monitor identity without depending on transient OS enumeration indexes.
7. Support monitor-specific input mappings and transport quirks through validated profiles.
8. Fail safely when discovery is ambiguous, DDC/CI is unavailable, or an input mapping is not verified.
9. Include focused diagnostics for EDID identity, DDC transport, and VCP code `0x60`.
10. Produce repeatable automated and hardware-validation evidence sufficient to decide whether each support-matrix slice is releasable.
11. Never claim support for an OS, monitor, connector, or input mapping that has only build or simulated-test coverage.

---

## 3. Scope

### 3.1 In scope

- Enumerating active external monitors.
- Reading EDID data required for stable identity and profile matching.
- Correlating an OS display object with the correct DDC/CI endpoint.
- Reading VCP feature code `0x60` where the monitor supports it.
- Writing VCP feature code `0x60`.
- Mapping logical input names to verified monitor-specific raw VCP values.
- Selecting a monitor and input through a Bubble Tea TUI.
- Providing non-interactive commands for scripting and diagnostics.
- Supporting hotplug, power-cycle, sleep/resume, and transient monitor disappearance sufficiently for the input-switching use case.
- Generating a diagnostic bundle focused on input switching.
- Packaging any Linux udev rules or setup instructions needed for unprivileged I²C access.
- Hardware qualification for the three target monitor models.
- Repository-owned implementation, validation, and qualification commands that support a repeatable test/fix loop.

### 3.2 Explicitly out of scope

The first release must not implement normal user-facing controls for:

- Brightness
- Contrast
- Audio volume
- Colour temperature
- RGB gain
- Power state
- KVM or USB upstream switching
- PIP/PBP configuration
- OLED maintenance or pixel-care functions
- Gaming modes
- Refresh-rate control
- HDR control
- Arbitrary VCP read/write
- Internal laptop displays
- Intel macOS
- Mobile operating systems
- Network-controlled monitors
- Global operating-system hotkeys or background hotkey daemons
- PIP/PBP switching, positioning, sizing, or swapping
- Backward compatibility with the legacy hotkey/PIP/external-tool settings schema
- Runtime use of `ddcutil`, `ddcctl`, or `ControlMyMonitor.exe`

Raw VCP access may exist only in guarded diagnostic code required to qualify VCP `0x60`. It must not become a general-purpose monitor-control API.

---

## 4. Mandatory design principles

### 4.1 Input switching is the only monitor function

The public application API and normal CLI/TUI workflow must expose only:

- Monitor discovery
- Monitor identification
- Available input listing
- Current input read, where supported
- Input switching
- Focused diagnostics

The internal DDC transport may implement generic `GetVCP` and `SetVCP` primitives because VCP framing is generic. These primitives must remain internal and must not result in public arbitrary-VCP commands.

### 4.2 Common OS-independent application model

Core discovery and switching behaviour must not depend on:

- A particular OS monitor identifier
- A particular executable
- A specific third-party library
- Monitor enumeration order
- Human-readable OS monitor descriptions
- `/dev/i2c-*` numbering
- Windows `HMONITOR` values
- macOS `CGDirectDisplayID` values
- The presence of `ddcutil`, Dell Display Manager, LG Switch, MonitorControl, BetterDisplay, or similar utilities

Every platform backend must produce a common descriptor and implement the same backend/session contracts.

### 4.3 Native OS interfaces first

Production builds must use project-owned native backends:

- **Linux:** DRM/sysfs plus `i2c-dev`
- **Windows:** `QueryDisplayConfig`, Display Configuration device information, GDI correlation, SetupAPI where needed, and `Dxva2.dll`
- **macOS Apple Silicon:** CoreGraphics/IOKit correlation plus dynamically resolved CoreDisplay functions

Third-party monitor-control utilities may be used for development comparison and hardware qualification only.

### 4.4 Stable identity

Never persist or display a transient enumeration index as the monitor’s primary identifier.

The preferred identity material is:

```text
EDID manufacturer ID
+ EDID product code
+ EDID serial number
+ connector/topology discriminator when necessary
```

The generated ID must be:

- Deterministic across process restarts.
- Stable across ordinary enumeration-order changes.
- Distinct for multiple identical monitors when adequate identity or topology data is available.
- Marked ambiguous when the system cannot safely distinguish identical monitors.

### 4.5 Minimal dependencies

The dependency policy is:

- Go standard library by default.
- Bubble Tea v2.0.8 is the required UI dependency.
- `golang.org/x/sys` may be used where it materially improves syscall, ioctl, handle, or ABI safety.
- CGO and system frameworks are allowed for the Apple Silicon macOS backend.
- No other module may be added without a documented reason in the pull request or architecture decision record.

Do not add generic CLI, EDID, MCCS, DDC, device-enumeration, logging, configuration, or styling libraries unless the local implementation would be meaningfully less safe or maintainable.

Development and CI tools are not runtime dependencies, but they must be version-pinned, licence-reviewed, and invoked reproducibly. GitHub Actions must be pinned to full commit SHAs.

### 4.6 Legacy replacement policy

The new architecture replaces rather than adapts the existing application design.

The completed migration must remove from production source and release artifacts:

- `github.com/robotn/gohook` and its transitive hotkey dependencies.
- Legacy hotkey registration and action dispatch.
- PIP/PBP actions and model fields.
- Subprocess-based `ddcutil`, `ddcctl`, and `ControlMyMonitor.exe` backends.
- Configuration-controlled arbitrary VCP codes and raw input mappings.
- The macOS WindowServer plist symlink workaround.
- Committed executable artifacts, including both existing command-name spellings.

The legacy JSON schema must fail with a specific unsupported-schema error and a migration explanation. It must not be partially accepted, silently ignored, or converted into verified monitor profiles.

Migration work must not overwrite unrelated staged or unstaged user changes. Legacy removals should be isolated so their intent and licence/dependency effects remain reviewable.

---

## 5. User interface requirement

### 5.1 Bubble Tea is mandatory

The interactive terminal interface must use:

- Repository: <https://github.com/charmbracelet/bubbletea>
- Current v2 module path: `charm.land/bubbletea/v2`
- Pinned version for this plan revision: `v2.0.8`

Pin the tagged release in `go.mod` and commit `go.sum`. Review transitive pseudo-versions as part of the dependency gate; do not replace the direct Bubble Tea version with an untagged branch or floating pseudo-version.

The implementation must use Bubble Tea’s model architecture:

- `Init`
- `Update`
- `View`
- Commands/messages for asynchronous work

Use the actual v2 API:

- `View() tea.View`, with terminal features declared on the returned view.
- `tea.KeyPressMsg` for normal key-down handling.
- `tea.WithContext` to connect program lifetime to application cancellation.
- Direct message injection in tests; terminal I/O is not required for state-machine tests.

### 5.2 Dependency restraint within the Charm ecosystem

Initial implementation should use Bubble Tea core only.

Do not add Bubbles, Lip Gloss, Huh, or other Charm modules unless a concrete UI requirement cannot be met cleanly with Bubble Tea and the standard library. Any addition requires a documented dependency decision.

### 5.3 TUI screens and states

The TUI must support these states:

1. **Starting**
   - Initialise logging where enabled.
   - Load embedded monitor profiles and local configuration.
   - Start asynchronous monitor discovery.

2. **Discovering**
   - Show that monitor discovery is in progress.
   - Do not block the Bubble Tea update loop.

3. **Monitor selection**
   - Show stable ID, model, connector, current input where known, and support status.
   - Clearly distinguish:
     - Supported and profiled
     - Discovered but unprofiled
     - Ambiguous identity
     - DDC endpoint unavailable
     - Permission denied

4. **Input selection**
   - List only verified logical inputs from the selected monitor profile.
   - Show the current input where it can be read.
   - Do not place unreviewed raw values in the selectable input list. An
     explicit read-only capability diagnostic shows them in a clearly labelled
     advisory section and cannot itself initiate a write.
   - When the selected monitor is not release-qualified, automatically offer a
     guided local qualification: review every advertised raw value, accept or
     correct its logical label, or skip it.
   - Unknown/vendor values default to `skip`; duplicate logical labels and raw
     values are rejected.
   - Show an explicit final summary before persisting. Explain that persistence
     enables only this exact local identity/endpoint and does not create a
     release support claim or perform a monitor write.

5. **Switching**
   - Run the hardware operation as a Bubble Tea command.
   - Prevent duplicate writes while an operation is in progress.
   - Show the target monitor and input.

6. **Result**
   - Distinguish:
     - Confirmed success
     - Assumed success because Set succeeded but the display provided no valid
       read-back after the configured attempts
     - Write accepted; read-back unavailable because the display path changed
     - Failed
     - Timed out
     - Backend unavailable
     - Monitor disconnected

7. **Diagnostics**
   - Show identity and transport information relevant to VCP `0x60`.
   - Permit export of a diagnostic JSON file.
   - Do not expose general arbitrary-VCP controls.

8. **Help**
   - Show keyboard controls and concise explanations.

### 5.4 Suggested key bindings

| Key | Action |
|---|---|
| `up`, `k` | Move selection up |
| `down`, `j` | Move selection down |
| `enter` | Select monitor/input or confirm action |
| `esc` | Return to previous view |
| `r` | Re-enumerate monitors |
| `d` | Open focused diagnostics |
| `c` | Read and show monitor-advertised VCP `0x60` candidates (advisory) |
| `a` | Review and resolve the current advisory into a local user qualification |
| `?` | Toggle help |
| `q`, `ctrl+c` | Quit |

Key bindings must remain testable as application messages rather than being tightly coupled to terminal I/O.

### 5.5 Bubble Tea concurrency rules

- Hardware discovery and DDC calls must execute in `tea.Cmd` functions.
- `Update` must never perform blocking device I/O.
- Results must be returned as typed messages.
- Context cancellation must stop in-progress operations where the OS API permits it.
- Re-enumeration must invalidate stale native handles.
- The UI state must not hold live OS handles.
- The backend session owns native handles and must close them deterministically.
- Every asynchronous request must carry an operation ID and monitor enumeration generation.
- Completion messages from superseded operation IDs or stale generations must be ignored.
- Native calls that cannot be interrupted must finish in an owned worker; cancellation may suppress their result but must not allow handles to be closed while the native call is still using them.

Example message categories:

```go
type discoveryCompletedMsg struct {
    OperationID uint64
    Snapshot    monitor.Snapshot
    Err         error
}

type inputReadCompletedMsg struct {
    OperationID uint64
    Monitor     monitor.MonitorRef
    Input       monitor.InputValue
    Err         error
}

type inputWriteCompletedMsg struct {
    OperationID uint64
    Monitor     monitor.MonitorRef
    Result      monitor.SwitchResult
    Err         error
}
```

### 5.6 Logging

Because a Bubble Tea application controls terminal output:

- Do not write diagnostic logs directly to stdout while the TUI is active.
- Normal user-visible status belongs in the Bubble Tea model.
- Debug logging may be written to a file when explicitly enabled.
- Logging must not include raw OS handles or unnecessarily expose host-specific information.
- Diagnostic export is separate from debug logging.

---

## 6. Proposed repository structure

```text
cmd/
    xdispddcswtchr/
        main.go

    xdispddcswtchr-qualify/
        main_qualification.go

internal/
    app/
        app.go
        commands.go
        messages.go
        state.go

    tui/
        model.go
        update.go
        view.go
        keys.go
        monitor_list.go
        input_list.go
        diagnostics.go
        errors.go

    monitor/
        descriptor.go
        identity.go
        input.go
        profile.go
        service.go
        verification.go

    edid/
        parse.go
        checksum.go
        identity.go

    ddc/
        transport.go
        input_source.go
        packet.go
        capabilities.go
        retry.go
        locking.go
        errors.go

    backend/
        backend.go
        registry.go

        linux/
            backend_linux.go
            enumerate_linux.go
            drm_linux.go
            i2c_linux.go
            ioctl_linux.go
            correlate_linux.go

        windows/
            backend_windows.go
            enumerate_windows.go
            dxva2_windows.go
            setupapi_windows.go
            correlate_windows.go
            types_windows.go

        darwin/
            backend_darwin.go
            enumerate_darwin.go
            coredisplay_darwin.go
            bridge_darwin.c
            bridge_darwin.h
            bridge_types_darwin.go

    profiles/
        registry.go
        dell_s3423dwc.go
        dell_u4025qw.go
        lg_45gx950a.go

    config/
        config.go
        paths.go

    diagnostic/
        bundle.go
        redact.go
        export.go

    testutil/
        fake_backend.go
        fake_transport.go
        fixtures.go

assets/
    linux/
        60-xdispddcswtchr-i2c.rules

build-support/
    script/
        validate.sh
        validate.ps1

qualification/
    support-matrix.json
    <monitor>/<os>/<date>/

testdata/
    edid/
    ddc/
    capabilities/
    diagnostics/

docs/
    HARDWARE-QUALIFICATION.md
    LINUX-INSTALL.md
    MACOS-COMPATIBILITY.md
    WINDOWS-NOTES.md

PLAN.md
README.md
LICENSE
go.mod
go.sum
```

`cmd/xdispddcswtchr-qualify` must require the `qualification` build tag and must not be included in release packaging. It is the only executable permitted to issue an unprofiled raw value, and even then the VCP code is fixed internally to `0x60`.

Transient validation output belongs under gitignored `test-artifacts/`; sanitized evidence used to justify support claims belongs under tracked `qualification/`.

Development-only comparison adapters, if created, belong outside the production backend:

```text
internal/devtools/
    ddcutil_linux.go
```

They must be excluded from normal builds through build tags or separate developer commands.

---

## 7. Core interfaces

### 7.1 Backend interface

```go
type Generation uint64

type Snapshot struct {
    Generation Generation
    Monitors    []Descriptor
}

type MonitorRef struct {
    ID         string
    Generation Generation
}

type Backend interface {
    Name() string
    Enumerate(ctx context.Context) (Snapshot, error)
    Open(ctx context.Context, ref MonitorRef) (RawInputSession, error)
}
```

The backend instance maintains an internal, short-lived map from `MonitorRef` to native endpoint information. This avoids exporting `any`, OS handles, device paths, or private framework objects in the common descriptor.

Every successful enumeration advances the generation. `Open` must reject a reference whose generation is no longer current, even if the same stable ID is present in a newer snapshot. This prevents an old UI selection from being applied after hotplug or topology change.

### 7.2 Input session

```go
type RawInputSession interface {
    GetInputRaw(ctx context.Context) (uint16, error)
    SetInputRaw(ctx context.Context, value uint16) error
    Close() error
}
```

`Close` must be idempotent. Native backends operate on raw values only. Profile matching, logical input resolution, retry policy, and verification belong to the common input service.

### 7.3 Monitor descriptor

```go
type Descriptor struct {
    ID            string
    Manufacturer  string
    Model         string
    Serial        string
    ProductCode   uint16
    Connector     string
    EDID          []byte
    ProfileName   string
    IdentityState IdentityState
    SupportState  SupportState
    BackendName   string
}
```

The descriptor must not contain live OS handles, native device paths, or its enumeration generation. Callers pair the descriptor ID with the containing snapshot generation to create a `MonitorRef`.

### 7.4 Logical input model

```go
type Input string

const (
    InputDisplayPort1 Input = "displayport-1"
    InputDisplayPort2 Input = "displayport-2"
    InputHDMI1        Input = "hdmi-1"
    InputHDMI2        Input = "hdmi-2"
    InputUSBC1        Input = "usb-c-1"
    InputUSBC2        Input = "usb-c-2"
    InputThunderbolt1 Input = "thunderbolt-1"
)

type InputValue struct {
    Logical Input
    Raw     uint16
}
```

A logical input name must be resolved through a monitor profile. The tool must not assume a universal raw VCP value for a connector.

### 7.5 Verification result

```go
type VerificationResult string

const (
    VerificationUnknown            VerificationResult = "unknown"
    VerificationWriteAccepted      VerificationResult = "write-accepted"
    VerificationConfirmed          VerificationResult = "confirmed"
    VerificationAssumedSuccess     VerificationResult = "assumed-success"
    VerificationDisplayPathChanged VerificationResult = "display-path-changed"
)

type SwitchResult struct {
    Monitor              MonitorRef
    Target               InputValue
    Verification         VerificationResult
    Observed             *InputValue
    Attempts             int
    VerificationAttempts int
    VerificationIssue    *VerificationIssue
    Duration             time.Duration
}
```

A successful DDC write followed by loss of the current display path may be a successful switch rather than a failure.

`VerificationConfirmed`, `VerificationAssumedSuccess`,
`VerificationDisplayPathChanged`, and profile-authorised
`VerificationWriteAccepted` are successful normal-application command
outcomes. Assumed success is permitted only after Set returned success and a
display-side read-back failure remained after the configured attempts. It must
carry a stable issue category, human detail, and raw reply hex when bytes were
received. Cancellation, permission/backend/configuration errors, and any Set
failure remain errors. `VerificationUnknown` means a valid read-back differed
from the target and must produce the verification-failure CLI exit class.

The TUI renders assumed success exactly as `Assumed success [no read-back
provided by display]`, clears rather than guesses the current input, suppresses
the error banner, and advises `r` rediscovery. The CLI returns exit `0` and JSON
schema version `2`. Qualification-only output may record this partial result,
but assumed success is never sufficient release-support evidence.

### 7.6 Identity and support states

`IdentityState` must distinguish:

- `hardware-stable`
- `topology-scoped`
- `ambiguous`
- `invalid-edid`

`SupportState` must distinguish:

- `supported`
- `backend-experimental`
- `unprofiled`
- `ambiguous`
- `endpoint-unavailable`
- `permission-denied`

Normal switching is permitted only for a supported slice backed by a matching
qualification record. Qualification tooling is the guarded route for exercising
experimental backends and candidate profile values.

---

## 8. Monitor identity design

### 8.1 EDID fields required

Implement a limited project-owned EDID parser for:

- Header validation
- Base-block and per-extension checksum validation
- Manufacturer ID
- Little-endian product code
- Numeric serial number
- Display name descriptor
- Text serial descriptor where present
- EDID version/revision
- Declared extension count and complete-block bounds
- Raw EDID retention for diagnostics

Do not implement unrelated timing, audio, colour, or display-mode parsing.

A valid 128-byte base block is mandatory for hardware identity. Each complete extension block is validated independently. A corrupt extension is reported but does not invalidate an otherwise valid base identity; a malformed header, truncated base block, or invalid base checksum is `invalid-edid` and cannot produce a qualified descriptor.

### 8.2 Stable ID algorithm

Preferred canonical identity input:

```text
manufacturer + product code + unique non-zero numeric serial
```

Second hardware-stable input, when the numeric serial is zero, duplicated, or otherwise unusable:

```text
manufacturer + product code + validated normalised text serial
```

Topology-scoped fallback input:

```text
manufacturer + product code + model name + stable connector/topology key
```

Normal representation:

```text
mon-<lowercase-base32-of-first-128-bits-of-sha256-canonical-identity>
```

Requirements:

- Normalise manufacturer and model strings.
- Normalise text serials, reject blank/placeholder values, and test uniqueness within the snapshot.
- Reject malformed base EDID rather than silently generating misleading identity.
- Preserve a diagnostic distinction between invalid checksum and unavailable EDID.
- Hash all canonical identity material so serial numbers and native topology paths are not embedded in the normal ID.
- Mark connector/topology-derived IDs `topology-scoped`; they may be used in the current generation but must not be persisted as hardware-stable preferences.
- If two monitors remain indistinguishable within a snapshot, mark both ambiguous and prohibit normal switching. Do not use enumeration order as a tie-breaker.

### 8.3 Connector naming

Expose common connector labels where feasible:

- `hdmi-a-1`
- `displayport-1`
- `usb-c-1`
- `thunderbolt-1`
- `unknown`

Connector labels are descriptive and may contribute to fallback identity. They are not themselves stable monitor IDs.

---

## 9. OS-independent discovery contract

Each backend must implement the following conceptual pipeline:

1. Enumerate active external display paths.
2. Obtain EDID data.
3. Parse and validate EDID identity.
4. Determine the native DDC communication endpoint.
5. Correlate display path, EDID identity, and DDC endpoint.
6. Construct a stable common descriptor.
7. Match a monitor profile.
8. Match the platform/monitor/connector slice against the qualification support matrix.
9. Determine whether logical input switching is safe to expose.
10. Return one generation-stamped snapshot with focused errors for partial discovery failures.

A backend must not report a fully operable monitor solely because it found:

- An I²C adapter
- An `HMONITOR`
- A `PHYSICAL_MONITOR`
- A CoreGraphics display
- A `DCPAVServiceProxy`

It must correlate the communication endpoint to the intended display.

Discovery must not depend on a successful MCCS capability-string request. Capability strings are advisory and often incomplete or malformed.

---

## 10. DDC/CI protocol layer

### 10.1 Required VCP operation

```go
const VCPInputSource byte = 0x60
```

Internal protocol API:

```go
type Transport interface {
    GetVCP(
        ctx context.Context,
        code byte,
    ) (current uint16, maximum uint16, err error)

    SetVCP(
        ctx context.Context,
        code byte,
        value uint16,
    ) error

    Close() error
}
```

Only the input service may call these methods in production application paths.

### 10.2 Locking

- Use one lock per physical DDC endpoint or I²C adapter as appropriate.
- The lock covers the entire request/response operation, including retry delay.
- Do not allow concurrent reads/writes to the same monitor endpoint.
- Ensure locks are removed or retired when an enumeration generation becomes stale.
- Do not hold a global lock while operating independent monitors.

### 10.3 Retry and timeout policy

Retry policy belongs in the monitor profile with backend defaults.

The implementation must distinguish:

- Permission denied
- Device disconnected
- DDC NACK
- Malformed reply
- Checksum failure
- Timeout
- Unsupported VCP read
- Unsupported VCP write
- Backend unavailable
- Ambiguous endpoint
- Stale enumeration
- Private macOS symbol unavailable

Do not retry indefinitely. Input switching may cause the monitor to stop responding because it has switched away from the current host.

All timing must be injectable behind a project-owned clock/sleeper seam. Unit and contract tests must not use real retry sleeps. A retry classifier must make the retry/no-retry decision from stable error categories rather than matching human-readable error strings.

Timeout semantics must be explicit per backend:

- Before entering a native call, reject an already-cancelled context.
- Where the OS operation is cancellable, cancel it directly.
- Where a native DLL/framework call cannot be cancelled, allow the owned worker to finish, suppress stale delivery, and retain its resources until completion.
- Never close a descriptor, handle, or native object while a call may still reference it.

### 10.4 Capability string

Capability retrieval is optional at the backend boundary and diagnostic. The
Apple Silicon macOS backend implements the DDC/CI `0xF3` request and multi-part
`0xE3` reply sequence. The CLI exposes `input detect` (with `input probe` and
`input capabilities` aliases), and diagnostic bundles retain the raw string and
typed VCP `0x60` candidates.

Rules:

1. Discovery must not fail because capability retrieval fails.
2. Profile matching must use EDID identity, not the capability string.
3. Known profile mappings override capability-string guesses.
4. Capability data may confirm but must not automatically authorise untested
   input values. Local authorisation requires explicit review of every
   advertised value and final user confirmation in the TUI.
5. Store the raw string in diagnostic output when successfully retrieved.
6. Bound each fragment to 32 data bytes and the assembled string to 16 KiB;
   reject checksum, source, opcode, offset, length, and malformed-text errors.
7. Label only MCCS values represented by the common application model. Preserve
   unknown vendor values as raw candidates.
8. Mark detected candidates write-qualified only when the embedded exact
   support-matrix gate or an exact persisted local user qualification
   authorises the same mapping.
9. A local approval must be scoped to stable monitor ID, EDID digest,
   manufacturer/product/model, connector, backend, reviewed capability digest,
   and acceptance time. Any identity or endpoint mismatch invalidates it.
10. Before every locally qualified write, retrieve the capability string again
    and require it to match the reviewed digest and still advertise the target
    raw value. Failure blocks the write and requires a new review.

### 10.5 Protocol conformance tests

The repository must contain normative golden vectors for the DDC/CI request and reply forms used by VCP `0x60`:

- Get VCP request.
- Get VCP reply with continuous and non-continuous type metadata.
- Set VCP request.
- Capabilities request at multiple offsets and capabilities reply fragments.
- Length and source-address validation.
- Checksum generation and rejection.
- Truncated, oversized, malformed, unsupported, and unexpected-code replies.
- Standard 11-byte Get VCP replies and the exact Apple CoreDisplay compact
  representation reconstructed only at the macOS backend boundary.
- Compact-form rejection for non-zero padding, wrong code, bad checksum, and
  every shape other than the exact eight-byte suffix plus three zero bytes.

Seed fuzz targets from these vectors for packet and capability parsers. Normal application packages must not export a generic VCP API, even though the internal transport framing is generic.

---

## 11. Monitor profile model

Profiles contain only information required for input switching.

```go
type Profile struct {
    Name  string
    Match EDIDMatcher

    Inputs map[Input]uint16

    ReadSupported bool

    RetryCount int
    ReplyDelay time.Duration
    WriteDelay time.Duration

    Verification VerificationMode
}

type EDIDMatcher struct {
    Manufacturer string
    ProductCode  uint16
    ProductCodes []uint16 // additional observed EDID/PNP variants
    ModelNames   []string
}

type VerificationMode uint8

const (
    VerifyNone VerificationMode = iota
    VerifyImmediate
    VerifyAfterReconnect
)
```

Profiles describe hardware identity and verified logical-to-raw mappings. They
do not, by themselves, make every OS/connector combination supported. A
release-supported switch requires a matching entry in
`qualification/support-matrix.json`. A separate local `user-qualified` path may
enable only user-reviewed mappings on an exact identity/endpoint, without
altering the embedded profile or making a release support claim.

The tracked support matrix keys a claim by:

- OS family, architecture, and tested OS version/build.
- Backend name and application commit.
- Monitor profile and hashed EDID identity.
- Video connector and direct/docked topology class.
- Set of qualified logical inputs and verification behaviour.
- Qualification evidence path.

### 11.1 Profile matching rules

Order:

1. Manufacturer ID and product code.
2. Optional EDID revision or firmware discriminator if required.
3. Model name as a secondary check.
4. Never match by model display name alone where product code is available.

### 11.2 Input mapping rules

- Every logical-to-raw mapping must be physically verified.
- Do not infer mappings from a generic MCCS table alone.
- Do not publish example values as supported values.
- Profile inputs should include only connectors that exist and were tested.
- Firmware-dependent differences require separate profile variants or a documented discriminator.
- If an input cannot be distinguished reliably, do not expose it by logical name.
- Two logical labels must not map to the same raw value unless one is canonical and the others are explicit display aliases; aliases do not create separate physical-input claims.

Example only — values are unverified and must not be used:

```go
var DellU4025QW = Profile{
    Name: "Dell U4025QW",
    Inputs: map[Input]uint16{
        InputDisplayPort1: 0x0f,
        InputDisplayPort2: 0x10,
        InputHDMI1:        0x11,
        InputHDMI2:        0x12,
        InputUSBC1:        0x1b,
    },
}
```

---

## 12. Linux implementation plan

### 12.1 Planned platform

- Ubuntu 26.04
- Linux build tag
- Native implementation without required runtime utilities

### 12.2 Native interfaces

Use:

- `/sys/class/drm`
- DRM connector `status`
- DRM connector `edid`
- DRM connector-to-DDC/I²C relationships where exposed
- `/sys/class/i2c-dev`
- `/dev/i2c-*`
- `I2C_FUNCS` to confirm adapter capabilities
- `i2c-dev` ioctls for bounded DDC/CI transactions

Preferred discovery path:

```text
/sys/class/drm/<connector>/
    status
    enabled
    edid
    ddc -> associated I²C adapter
```

When the kernel exposes the connector’s DDC link, use it rather than scanning all I²C adapters.

If the relationship is absent or inconsistent:

1. Enumerate candidate I²C adapters.
2. Read candidate EDID data where possible.
3. Correlate candidate EDID with DRM connector EDID.
4. Reject ambiguity rather than selecting the first matching bus.

Opening a bus is not evidence that it belongs to the monitor. Before a session becomes operable, the backend must match the DRM connector EDID to the candidate adapter identity and verify the required adapter functionality. DDC request and reply operations must use bounded buffers and the profile’s explicit inter-operation delays.

### 12.3 Linux dependencies

Preferred:

- Standard library
- `golang.org/x/sys/unix` only where needed for safe ioctl definitions and calls

No Linux C library is required.

No runtime dependency on:

- `ddcutil`
- `libddcutil`
- `i2cdetect`
- `i2cget`
- `i2cset`
- `unix755/dccli`

### 12.4 Permissions

Normal operation must not require root.

Installation may require elevated privileges to:

- Load or configure `i2c-dev`
- Install a udev rule
- Apply device ACLs
- Add the user to an appropriate group

Deliver:

```text
assets/linux/60-xdispddcswtchr-i2c.rules
docs/LINUX-INSTALL.md
```

The installation design must be least-privilege:

- Never install mode `0666` for `/dev/i2c-*`.
- Prefer an active-seat ACL (`TAG+="uaccess"`) or a dedicated group with mode `0660`, according to what Ubuntu 26.04 and the relevant adapter topology support.
- Document that generic I²C access can expose unrelated hardware and that group membership may therefore be broader than monitor control.
- The application opens only the already-correlated display adapter and never probes unrelated buses during normal switching.
- Validate the packaged rule on Ubuntu 26.04; do not ship a speculative rule copied from another distribution.

The application must clearly distinguish:

- Monitor discovered but I²C endpoint unreadable
- Permission denied
- `i2c-dev` unavailable
- No correlated DDC endpoint
- DDC/CI disabled in the monitor OSD

### 12.5 Linux fallback policy

A `ddcutil` subprocess adapter may be implemented only as a development and comparison tool.

It must:

- Be excluded from normal production builds.
- Never be required for discovery.
- Never be required for switching.
- Never determine the core monitor ID.
- Be documented as a diagnostic comparator, not a supported runtime dependency.

---

## 13. Windows implementation plan

### 13.1 Native APIs

Use native Windows interfaces:

- `GetDisplayConfigBufferSizes`
- `QueryDisplayConfig` with `QDC_ONLY_ACTIVE_PATHS`
- `DisplayConfigGetDeviceInfo` with `DISPLAYCONFIG_DEVICE_INFO_GET_TARGET_NAME` and source-name requests
- `EnumDisplayMonitors`
- `GetMonitorInfo` and GDI source-device name correlation
- SetupAPI against `monitorDevicePath` where the full EDID is required
- `GetNumberOfPhysicalMonitorsFromHMONITOR`
- `GetPhysicalMonitorsFromHMONITOR`
- `GetVCPFeatureAndVCPFeatureReply`
- `SetVCPFeature`
- `DestroyPhysicalMonitor` or `DestroyPhysicalMonitors`
- Optional capability-string functions for diagnostics

Required high-level sequence:

1. Enumerate active CCD display paths, retrying `ERROR_INSUFFICIENT_BUFFER` because topology may change between sizing and query.
2. Resolve each target’s `DISPLAYCONFIG_TARGET_DEVICE_NAME`, validate `edidIdsValid`, and retain `monitorDevicePath`, output technology, and connector instance internally.
3. Resolve the source GDI device name and correlate it to the `HMONITOR` returned by `EnumDisplayMonitors`/`GetMonitorInfo`.
4. Use SetupAPI through the target monitor device path to obtain the complete EDID when required; do not enumerate raw registry locations directly.
5. Resolve the `HMONITOR` to the complete `PHYSICAL_MONITOR` array.
6. Correlate the physical handle set to the stable target descriptor; if multiple handles cannot be proven distinct, return `ErrAmbiguousMonitor` rather than selecting the first.
7. Open a generation-bound input session, read or write only VCP code `0x60`, and destroy every acquired physical monitor handle.

### 13.2 Structure correctness

Represent the documented structure correctly:

```c
typedef struct _PHYSICAL_MONITOR {
    HANDLE hPhysicalMonitor;
    WCHAR  szPhysicalMonitorDescription[128];
} PHYSICAL_MONITOR;
```

The allocated array length must equal the count returned by `GetNumberOfPhysicalMonitorsFromHMONITOR`.

Do not:

- Treat the structure as an arbitrary 128-byte buffer.
- Read the handle from the first byte.
- Assume one physical monitor per `HMONITOR`.
- Persist `HMONITOR` or physical monitor handles.
- Use the physical monitor description as a stable ID.
- Select `PHYSICAL_MONITOR[0]` merely because it is first.

### 13.3 Windows implementation dependencies

Implement directly using:

- Standard-library dynamic DLL loading where safe, or
- `golang.org/x/sys/windows`

Do not import or require:

- `wufeng5702/ddcci`
- `unix755/displayController`
- `unix755/dccli`
- Third-party DLLs
- Dell Display Manager
- LG Switch

### 13.4 Handle ownership

The Windows `RawInputSession` owns physical monitor handles.

Requirements:

- Close all handles on normal completion.
- Close partially acquired handles on error.
- Make `Close` idempotent.
- Do not use a handle after re-enumeration invalidates its generation.
- Include handle-lifecycle tests using wrapper interfaces/fakes.
- Test the 64-bit structure size, UTF-16 buffer bounds, partial-array cleanup, and every `GetLastError` path on a native Windows runner.

Microsoft documents that low-level monitor configuration calls can take tens of milliseconds and may not be cancellable once entered. The backend must follow the owned-worker timeout rules in Section 10.3 instead of closing a handle out from under an active DLL call.

---

## 14. macOS Apple Silicon implementation plan

### 14.1 Planned platform

Only:

```go
//go:build darwin && arm64
```

Intel macOS is not supported and should not be included in the initial codebase.

Before implementing the full backend, complete a read-only feasibility gate on the current macOS 26.6 Apple Silicon host:

1. Enumerate CoreGraphics external displays.
2. Enumerate the observed `DCPAVServiceProxy` services.
3. Dynamically resolve the required private CoreDisplay symbols.
4. Correlate each service to the attached Dell U4025QW and S3423DWC EDID without relying on enumeration order.
5. Read VCP `0x60` from each monitor without writing any value.
6. Record the OS build, symbol availability, service topology, correlation evidence, timings, and errors in a sanitized feasibility artifact.

Failure of symbol resolution, correlation, or safe read blocks the proposed macOS backend design. It must not be deferred until release qualification.

### 14.2 Native system interfaces

Use:

- CoreGraphics for display enumeration
- IOKit registry services for display/service correlation
- CoreDisplay private interfaces for DDC I²C transactions

Expected private functions include:

```c
IOAVServiceCreateWithService
IOAVServiceReadI2C
IOAVServiceWriteI2C
```

Expected service discovery includes `DCPAVServiceProxy`.

The implementation must account for known Apple Silicon bridge behaviour, including the possibility of a non-standard I²C address on some built-in HDMI paths.

### 14.3 C bridge

All CoreFoundation, IOKit, CoreDisplay, and private-symbol interaction must be isolated in:

```text
internal/backend/darwin/
    bridge_darwin.c
    bridge_darwin.h
```

The bridge should expose a narrow project-owned ABI such as:

```c
int xdispddcswtchr_display_count(...);
int xdispddcswtchr_display_identity(...);
int xdispddcswtchr_ddc_open(...);
int xdispddcswtchr_ddc_get_vcp(...);
int xdispddcswtchr_ddc_set_vcp(...);
void xdispddcswtchr_ddc_close(...);
```

Requirements:

- Do not expose CoreFoundation or IOKit object pointers in common Go types.
- Define ownership rules for every returned object/buffer.
- Convert native errors into stable project error codes.
- Keep bridge functions limited to discovery, identity correlation, and VCP `0x60` transport.

### 14.4 Dynamic symbol resolution

Use `dlopen` and `dlsym` for private CoreDisplay functions.

This prevents hard process-load failure if Apple removes or renames a symbol.

Return typed errors:

```go
var (
    ErrBackendUnavailable      = errors.New("monitor DDC backend unavailable")
    ErrPrivateSymbolUnavailable = errors.New("required CoreDisplay symbol unavailable")
    ErrDisplayServiceNotFound  = errors.New("DDC service not found for display")
    ErrDDCTransactionFailed    = errors.New("DDC transaction failed")
)
```

### 14.5 macOS compatibility policy

The CoreDisplay backend relies on private, undocumented behaviour and may break on a macOS update.

Required mitigation:

- Detect unsupported symbols and registry topology cleanly.
- Do not crash or panic when the backend is unavailable.
- Maintain a compatibility table by macOS release/build.
- Test every supported Tahoe point release.
- Keep the private API surface minimal.
- Sign and notarise release builds where distribution requirements demand it.
- Document the private-API risk in `docs/MACOS-COMPATIBILITY.md`.

### 14.6 macOS dependencies

Use:

- Go standard library
- CGO
- System frameworks shipped with macOS
- Project-owned C bridge

Do not require:

- Rust
- `ddc-macos-rs`
- `ddcctl`
- Homebrew
- MonitorControl
- BetterDisplay
- Dell Display Manager
- LG Switch

`ddc-macos-rs` is a reference implementation only.

---

## 15. Service workflow

The platform-independent input service performs:

```text
Resolve MonitorRef from current snapshot
        ↓
Validate descriptor generation
        ↓
Match verified monitor profile
        ↓
Match qualified platform/monitor/connector support slice
or exact persisted local user qualification
        ↓
Resolve logical input to raw value
        ↓
For a local qualification, re-read and match reviewed capabilities
        ↓
Open OS-specific input session
        ↓
Acquire endpoint lock
        ↓
Write VCP 0x60
        ↓
Apply profile-specific delay
        ↓
Optionally verify or detect display-path change
        ↓
Release lock and native resources
        ↓
Return SwitchResult to TUI/CLI
```

### 15.1 Write verification

Verification must be profile-controlled.

- Some monitors support immediate read-back.
- Some stop responding temporarily.
- Some disappear because the active input changed away from the current host.
- A successful write followed by display disappearance must not automatically be reported as failure.

The user-visible result should distinguish:

- Confirmed switch
- Assumed success with no display-provided valid read-back
- Write accepted, no read-back
- Display path changed
- Switch failed
- Switch result unknown

---

## 16. CLI and TUI commands

The Bubble Tea TUI is the default when no subcommand is supplied:

```text
xdispddcswtchr
```

If stdin or stdout is not a terminal and no subcommand is supplied, print concise usage to stderr and exit with the usage/configuration code instead of attempting to start an interactive TUI.

Use the standard library for subcommand and flag parsing. Human-readable output may change for clarity; every JSON response must be emitted from a typed, versioned schema. JSON is written only to stdout and errors/diagnostics only to stderr.

Non-interactive commands must use the same service layer.

### 16.1 List monitors

```text
xdispddcswtchr list
xdispddcswtchr list --json
```

Example:

```text
ID                                  MODEL              CONNECTOR       CURRENT
mon-7c4q2k6d4g...                    Dell U4025QW       usb-c-1         usb-c-1
mon-2m9x5w8p1j...                    LG 45GX950A-B      displayport-1   unknown
```

### 16.2 List verified inputs

```text
xdispddcswtchr input list --monitor <id>
```

### 16.3 Read current input

```text
xdispddcswtchr input get --monitor <id>
xdispddcswtchr input get --monitor <id> --json
```

### 16.4 Switch input

```text
xdispddcswtchr input set \
    --monitor <id> \
    --input hdmi-1
```

### 16.5 Focused diagnostics

```text
xdispddcswtchr inspect --all --json
xdispddcswtchr input probe --monitor <id>
xdispddcswtchr diagnose --monitor <id> --output diagnostic.json
```

### 16.6 Guarded qualification command

Before capture or write qualification, the separately built qualification
binary must expose an identity-scoped, read-only `input preflight` operation.
It reports the observed raw VCP `0x60` value, the documented physical input
choices for a known monitor model, and a machine-readable assessment of which
facts remain operator-confirmed. Preflight must state explicitly that current
OSD text, a live safe target, external dock/adapter/KVM identity, and a physical
recovery method cannot be proven by a VCP read. It must never scan or write
candidate raw values.

To record a value after an operator manually selects an input through the monitor OSD:

```text
xdispddcswtchr-qualify input capture \
    --monitor <id> \
    --expected-model <exact-model> \
    --expected-edid-sha256 <sha256> \
    --label hdmi-1 \
    --osd-label 'HDMI 1'
```

This executable is built explicitly with `-tags qualification` and is not
packaged. Do not implement a normal public command named `capture`, `get-vcp`,
or `set-vcp`.

A raw input write, if required for qualification, must:

- Be in an explicitly diagnostic command.
- Require a conspicuous unsafe flag.
- Warn that the display may become unavailable.
- Accept only VCP `0x60`.
- Never accept an arbitrary VCP code.
- Exist only in `cmd/xdispddcswtchr-qualify` behind the `qualification` build tag.
- Require exact stable monitor ID, expected EDID identity hash, expected model, raw value, and an explicit switch-away acknowledgement.
- Require the operator to state a recovery method before the write is enabled.

### 16.7 Exit classes

The process exit classes are stable public behaviour:

| Code | Class | Examples |
|---:|---|---|
| `0` | Success | Read completed; switch confirmed; normal application assumed success after a successful Set; qualified write accepted; display path changed as expected |
| `2` | Usage or configuration | Invalid flags, non-TTY default invocation, unsupported/legacy configuration schema |
| `3` | Selection or support | No monitor, not found, ambiguous identity, unsupported monitor/input, unqualified slice |
| `4` | Backend or permission | Backend unavailable, private symbol missing, endpoint missing, permission denied, DDC disabled |
| `5` | Transport | Timeout, malformed reply, checksum, disconnection, rejected write |
| `6` | Verification | A valid read-back differed from the target, or qualification evidence remained incomplete (including assumed success) |

Wrapped native errors must map to one of these classes while retaining detailed diagnostic context.

---

## 17. Diagnostics

### 17.1 Diagnostic bundle schema

```json
{
  "schema_version": 1,
  "generated_at": "",
  "application": {
    "version": "",
    "commit": ""
  },
  "os": {
    "name": "",
    "version": "",
    "architecture": ""
  },
  "backend": {
    "name": "",
    "transport": "",
    "endpoint": ""
  },
  "support": {
    "state": "",
    "qualification_record": null
  },
  "redaction": {
    "enabled": true,
    "method": "bundle-scoped-sha256"
  },
  "monitor": {
    "id": "",
    "manufacturer": "",
    "product_code": 0,
    "serial": "",
    "model_name": "",
    "connector": "",
    "identity_state": ""
  },
  "edid": {
    "raw": "",
    "checksum_valid": false
  },
  "input_source": {
    "vcp_code": "0x60",
    "read_supported": false,
    "current_raw": null,
    "current_logical": null,
    "profile_values": {}
  },
  "capabilities_raw": "",
  "timings": {
    "read_ms": null,
    "write_ms": null
  },
  "errors": []
}
```

### 17.2 Diagnostic safety

- A safe probe must not cycle through input values.
- Do not automatically switch away from the user’s active input.
- Record endpoint paths only in diagnostic output, not normal UI.
- Clearly mark potentially identifying system topology data.
- Provide a redacted export option if diagnostic bundles may be shared.
- Do not include arbitrary environment variables or unrelated host information.
- Redaction is the default for exported bundles. Unredacted export requires an explicit flag and warning.
- Redaction must hash serials and native topology paths consistently within one bundle so correlation remains possible without exposing the original values.
- Schema version, application commit, OS build, backend name, support state, and redaction state are mandatory.

---

## 18. Hardware qualification

### 18.1 General process

For each model:

1. Run the read-only feasibility gate and record OS/application/backend versions.
2. Record EDID over each physical connector using a hashed identity in shareable artifacts.
3. Record product code, numeric/text serial behaviour, model string, block lengths, and per-block checksum state.
4. Confirm DDC/CI is enabled in the monitor OSD.
5. Read VCP `0x60` without changing it.
6. Manually select each physical input through the OSD.
7. Capture the corresponding raw value using the read-only capture command.
8. Verify switching to and from each input with an explicit recovery method.
9. Determine whether read-back works after switching or the display path disappears as expected.
10. Measure required delays, attempts, retry decisions, and recovery time.
11. Test direct and applicable docked connections as distinct support slices.
12. Sanitize and review the evidence, then commit only verified mappings and support-matrix entries.

Every tracked qualification record must include:

- Qualification schema version and timestamp.
- Operator attestation.
- Application version and commit.
- OS name, architecture, version, and build.
- Backend name and relevant private-symbol/API availability.
- Hashed EDID identity, manufacturer, product code, model, firmware if observable, and checksum state.
- Connector, cable, adapter/dock, source host, and topology description.
- Before/target/observed logical and raw input values.
- Read, write, verification, disappearance, and recovery timings.
- Attempt count, classified warnings/errors, and pass/fail result for every required scenario.
- Explicit redaction metadata and a path to any non-committed sensitive source capture.

A qualification record is immutable evidence. Corrections create a new record and update the support matrix; they do not rewrite historical results.

### 18.2 Dell S3423DWC

Qualification requirements:

- The operator-provided Windows capability enumeration records USB-C `0x1B`,
  HDMI-1 `0x11`, and HDMI-2 `0x12`; these values populate the model profile.
- Treat EDID/PNP product codes `0xD155` and `0xD15B` as observed identity
  variants only when the exact Dell model name also matches.
- Confirm EDID identity and product code.
- Confirm input values for every required HDMI, DisplayPort, and USB-C path.
- Confirm DDC/CI OSD requirement.
- Test PIP/PBP disabled.
- Separately document behaviour with PIP/PBP enabled.
- Confirm whether video input switching has any automatic USB effect, without claiming KVM control.

### 18.3 Dell U4025QW

Qualification requirements:

- The operator-provided Windows capability enumeration records DisplayPort-1
  `0x0F`, HDMI-1 `0x11`, and an otherwise unlabeled `0x19`. The profile maps
  `0x19` to Thunderbolt-1 because Thunderbolt is the model's only remaining
  documented physical video input; retain this as an explicit inference until
  the OSD label is observed during a write/recovery test.
- Treat EDID/PNP product codes `0x4308` and `0x4318` as observed identity
  variants only when the exact Dell model name also matches.
- Confirm EDID identity over Thunderbolt/USB-C, DisplayPort, and HDMI.
- Test each connector independently.
- Determine whether Thunderbolt and USB-C use one or multiple raw VCP values.
- Test direct and docked topologies.
- Determine whether changing video input automatically changes USB/KVM routing.
- Report only video input switching; do not claim direct KVM control.
- Test PBP disabled and enabled as separate states.

### 18.4 LG UltraGear OLED 45GX950A-B

Qualification requirements:

- The operator-provided Windows capability enumeration for `GSM9E9E` records
  HDMI-1 `0x11`, HDMI-2 `0x12`, DisplayPort-1 `0x0F`, and DisplayPort-2 `0x10`;
  these values populate the model profile exactly as supplied.
- Current LG specifications describe one physical DisplayPort input plus USB-C.
  Determine whether the reported DisplayPort-2 value represents USB-C before
  making a physical-connector or normal-runtime support claim.
- Confirm VCP `0x60` can be read and written.
- Capture the capability string where available.
- Determine raw values for each physical connector.
- Test with HDR enabled and disabled.
- Test with variable refresh rate enabled and disabled.
- Determine whether dual-mode or gaming modes change the endpoint or raw input values.
- Do not depend on LG Switch at runtime.
- LG Switch may be used only to compare expected behaviour during qualification.

### 18.5 Qualification matrix

Test:

- HDMI
- DisplayPort
- USB-C
- Thunderbolt where applicable
- Direct connection
- Each supported dock
- Monitor power cycle
- Host sleep/resume
- Hotplug/unplug
- Multiple external monitors
- Multiple monitors of the same model
- Missing or zero EDID serial
- DDC/CI disabled in OSD
- PIP/PBP enabled and disabled
- HDR enabled and disabled
- Variable refresh rate enabled and disabled
- Switching away from the host running the tool
- Switching back to that host
- Temporary monitor disappearance after switch

### 18.6 Initial incremental qualification slice

The first candidate slice is:

- macOS 26.6 build 25G72 on Apple Silicon.
- Native CoreGraphics/IOKit/CoreDisplay backend.
- Attached Dell U4025QW.
- Attached Dell S3423DWC.

Only inputs, physical connectors, and direct/docked paths that complete the full qualification process may be marked supported. The LG monitor, Windows 11, Ubuntu 26.04, other macOS 26 point releases, and untested dock paths remain `planned` or `experimental` until their own evidence is committed.

Testing unrelated monitor functions is not required.

---

## 19. Error model

Define stable errors that the UI and CLI can classify:

```go
var (
    ErrNoMonitors              = errors.New("no external monitors found")
    ErrMonitorNotFound         = errors.New("monitor not found")
    ErrAmbiguousMonitor        = errors.New("monitor identity is ambiguous")
    ErrUnsupportedMonitor      = errors.New("monitor has no verified input profile")
    ErrUnqualifiedSlice        = errors.New("platform and monitor combination is not qualified")
    ErrQualificationUnavailable = errors.New("user qualification persistence is unavailable")
    ErrUserQualificationStale  = errors.New("saved user qualification no longer matches advertised capabilities")
    ErrUnknownInput            = errors.New("input is not defined by the monitor profile")
    ErrUnsupportedConfigSchema = errors.New("configuration schema is unsupported")
    ErrPermissionDenied        = errors.New("permission denied")
    ErrBackendUnavailable      = errors.New("DDC backend unavailable")
    ErrEndpointNotFound        = errors.New("DDC endpoint not found")
    ErrDDCDisabled             = errors.New("DDC/CI may be disabled")
    ErrReadUnsupported         = errors.New("reading input source is unsupported")
    ErrWriteUnsupported        = errors.New("writing input source is unsupported")
    ErrTransactionTimeout      = errors.New("DDC transaction timed out")
    ErrMalformedReply          = errors.New("malformed DDC reply")
    ErrChecksum                = errors.New("DDC checksum failure")
    ErrDisconnected            = errors.New("monitor disconnected")
    ErrStaleEnumeration        = errors.New("monitor enumeration is stale")
    ErrPrivateSymbolUnavailable = errors.New("required private macOS symbol unavailable")
)
```

OS-specific errors should wrap one of these categories while preserving native detail for diagnostics.

All classification must use typed values and `errors.Is`/`errors.As`. Human-readable messages are not a programmatic interface and must not be parsed to decide retries, exit codes, TUI states, or support status.

---

## 20. Configuration

Use `encoding/json` from the standard library. The initial format is strict JSON with mandatory `schema_version: 1` whenever a configuration file is present.

Decode with unknown-field rejection, reject trailing JSON values, validate all enum/range/path fields, and report the precise field responsible for failure. An absent configuration file is valid and uses built-in defaults.

Configuration path precedence is:

1. Explicit global `--config <path>`.
2. `XDISPDDCSWTCHR_CONFIG`.
3. The OS-appropriate user configuration directory.
4. No file; built-in defaults.

Configuration may include:

- Preferred monitor ID
- Preferred input aliases
- TUI behaviour
- Debug log path
- Diagnostic redaction preference
- Application-managed, exact local user-qualification records

Do not permit normal configuration to override embedded profile values
silently. Local user qualifications are a separate state and profile namespace,
are visibly labelled `user-qualified`, and never become release support data.

The legacy schema containing `hotkeys`, PIP/PBP, backend executable paths, arbitrary VCP codes, raw input maps, or `backend: cli` is rejected with `ErrUnsupportedConfigSchema`. There is no compatibility mode.

Local user qualifications must:

- Be created or revised only through the guided TUI review.
- Be stored separately from embedded profiles and the tracked support matrix.
- Be written atomically with owner-only permissions.
- Contain exact identity, endpoint, capability digest, accepted mappings, and
  acceptance timestamp.
- Be ignored when identity or endpoint matching fails.
- Be revalidated against a fresh capability read before every write.
- Display a clear TUI warning and `user-qualified` support state.
- Never merge into release profiles or support claims without guarded hardware
  evidence and a reviewed support-matrix change.

---

## 21. Testing strategy

Testing is organised as a repeatable implementation loop and a set of escalating gates. A higher gate never substitutes for a failed lower gate.

### 21.1 Repository-owned validation entrypoints

Provide equivalent Unix and PowerShell entrypoints:

```text
build-support/script/validate.sh
build-support/script/validate.ps1
```

Both accept the same logical modes and write transient output only beneath gitignored `test-artifacts/`.

Canonical invocation:

```text
./build-support/script/validate.sh quick
./build-support/script/validate.sh full
./build-support/script/validate.sh platform
./build-support/script/validate.sh hardware-read \
    --monitor-id <id> \
    --expected-model <model> \
    --expected-edid-sha256 <sha256>
./build-support/script/validate.sh hardware-write \
    --monitor-id <id> \
    --expected-model <model> \
    --expected-edid-sha256 <sha256> \
    --source-input <logical-input> \
    --target-input <logical-input> \
    --acknowledge-switch-away \
    --recovery-method <description>

./build-support/script/validate.ps1 -Mode quick
./build-support/script/validate.ps1 -Mode full
./build-support/script/validate.ps1 -Mode platform
```

The PowerShell entrypoint must expose equivalent named hardware arguments. Both
scripts fail non-zero on a failed required check, print the artifact directory,
write a machine-readable summary, and must not rewrite source, `go.mod`, or
`go.sum` as a side effect of validation.

#### `quick`

Run on every implementation iteration:

1. `gofmt -d` over tracked Go files and fail if a diff is produced.
2. `go mod tidy -diff`.
3. `go mod verify`.
4. `go vet ./...`.
5. `go test -short -shuffle=on -count=1 ./...`.

The script must preserve and print the shuffle seed on failure.

#### `full`

Run before completing a phase:

1. Everything in `quick`.
2. `go test -shuffle=on -count=1 ./...`.
3. `go test -race -shuffle=on -count=1 ./...` where the platform/toolchain supports the race detector.
4. Repeat concurrency-sensitive service and locking tests at least ten times.
5. Produce per-package and merged coverage artifacts.
6. Run regression fuzz corpora without time-based fuzzing.
7. Run CLI and TUI end-to-end tests against the fake backend.

#### `platform`

Run on the native OS runner:

1. Everything in `full` that the platform supports.
2. Build `./cmd/xdispddcswtchr` with `-trimpath` and release build flags.
3. Exercise `version`, help, strict configuration rejection, non-TTY behaviour, and JSON output without hardware.
4. Validate build metadata, linked system dependencies, and absence of third-party monitor-control runtime dependencies.
5. Archive test JSON, coverage, executable metadata, and failure diagnostics.

#### `hardware-read`

Run only on an explicitly selected host/monitor:

1. Enumerate and correlate the selected stable ID.
2. Validate the expected model and hashed EDID identity.
3. Read VCP `0x60` and generate a sanitized diagnostic artifact.
4. Retrieve and parse the capability string into a separate JSON artifact and
   require VCP `0x60` to advertise at least one candidate value for capability
   qualification runs.
5. Perform no monitor write.

#### `hardware-write`

Run only after `hardware-read` passes. Require exact stable monitor ID, expected EDID identity hash, expected model, source input, target input, explicit switch-away acknowledgement, and an operator recovery method. Never enumerate or cycle candidate raw values automatically.

### 21.2 Unit, parser, and profile tests

Test:

- EDID base/extension lengths, checksums, manufacturer decoding, little-endian product code, descriptors, malformed data, and sanitized fixtures.
- Hardware-stable ID generation, duplicate serials, text-serial fallback, topology-scoped identity, ambiguity, and hash determinism.
- DDC golden frames, malformed replies, checksums, bounds, unsupported result codes, and wrong VCP-code replies.
- Capability-string compact/spaced syntax, nesting, duplicates, bounds, and malformed input.
- Capability report precedence (profile, MCCS standard, unmapped), qualification
  isolation, local user-qualification persistence/revalidation, CLI text/JSON,
  and diagnostic inclusion.
- Profile matching, duplicate matcher rejection, logical/raw mapping, aliases, unverified values, and support-matrix matching.
- Retry classification using an injected clock/sleeper and no real sleeps.
- Verification state transitions and `SwitchResult` contents.
- Typed error wrapping, exit-code classification, JSON schemas, and diagnostic redaction.

Captured EDID fixtures must either be approved unredacted evidence or be sanitized by replacing serial-bearing fields and recomputing checksums. Each fixture records provenance and whether it is captured or synthetic-derived.

### 21.3 Backend and service contract tests

Every backend adapter and the fake backend must pass the applicable common contract suite:

- Enumeration returns one generation-stamped snapshot with no native handles.
- `Open` rejects unknown IDs and stale generations.
- Re-enumeration invalidates prior references.
- Session close is idempotent.
- Concurrent operations on one endpoint are serialized.
- Independent endpoints may operate concurrently.
- Cancellation follows the owned-worker rules.
- Native resources remain owned until any uncancellable call returns.
- Native resources are released on success and every partial-failure path.
- Switching is rejected for ambiguous monitors and for unprofiled,
  experimental, or unqualified slices unless an exact persisted user
  qualification matches and its reviewed capabilities are freshly revalidated.
- Display-path disappearance, reconnect, timeout, and duplicate completion messages produce deterministic results.

Linux tests use fake sysfs trees and an injected ioctl adapter. Windows tests run natively and cover structure sizes, UTF-16 bounds, CCD retry/correlation, partial handle acquisition, and cleanup. macOS tests cover the C ABI, symbol lookup, CF/IOKit ownership, service correlation, and missing-symbol behaviour.

### 21.4 TUI and CLI tests

Use Bubble Tea v2 messages directly to test:

- Discovery start/completion and empty state.
- Monitor and input selection.
- `tea.KeyPressMsg` key handling.
- Write in progress and duplicate-write prevention.
- Confirmed, accepted, assumed-success, display-path-changed, failed, and
  unknown results, including exact advisory wording and current-input clearing.
- Operation-ID and generation mismatch suppression.
- Re-enumeration, cancellation, help, quit, and very small terminal sizes.
- Semantic view content without brittle full-screen snapshots.
- Automatic unqualified-monitor prompting; accept/relabel/skip review of every
  candidate; duplicate rejection; cancel/retry; explicit persistence summary;
  and post-save rediscovery.

CLI tests cover:

- Standard-library subcommand parsing and help.
- Strict configuration schema and precedence.
- stdout/stderr separation.
- Versioned JSON output decoded into expected typed structures.
- Stable exit classes.
- No-subcommand non-TTY behaviour.
- Proof that production commands expose no arbitrary VCP code or unprofiled raw write.

### 21.5 Coverage, fuzzing, and analysis gates

- Maintain at least 90% statement coverage for EDID, DDC framing, identity, profiles, and the service layer.
- Maintain at least 80% statement coverage across common non-platform packages.
- Native glue has no numeric target, but every error and resource-lifecycle path must have a seam-based test.
- Commit seed corpora for EDID, DDC reply, and capability-string fuzzers.
- Run bounded scheduled fuzz jobs separately from deterministic PR gates and retain any minimizing crash input as a regression fixture.
- Run pinned `govulncheck ./...`, dependency review, and licence review during release validation.

### 21.6 CI matrix

Use explicit runner labels rather than moving `-latest` aliases:

| Job | Runner | Purpose |
|---|---|---|
| Linux | `ubuntu-26.04` | Linux build, unit/contract tests, race test, and packaging checks |
| Windows | `windows-2025` | Windows build, ABI/handle tests, and packaging checks |
| macOS | `macos-26` | Apple Silicon CGO build, C-bridge tests, and packaging checks |

As of the source review date, GitHub lists `ubuntu-26.04` as a public-preview
hosted runner. Revalidate every label before implementing CI. If the Ubuntu
runner is unavailable or unsuitable, use a controlled self-hosted Ubuntu 26.04
runner; do not substitute an older distribution and call the Ubuntu 26.04 gate
passed.

The Windows hosted runner provides build and API evidence, not Windows 11 monitor qualification. Hosted runners have no DDC hardware and must never run hardware writes.

CI requirements:

- Pin GitHub Actions to verified full commit SHAs.
- Pin Go 1.26.5 and set `GOTOOLCHAIN=local`.
- Use `-mod=readonly` outside the explicit tidy-diff check.
- Upload test, coverage, build metadata, and diagnostic artifacts on success/failure as appropriate.
- Hardware jobs require manual approval and specifically labelled self-hosted machines.
- A missing hardware runner leaves the slice planned/experimental; it does not make ordinary CI fail and does not create a support claim.

### 21.7 Required implementation loop

For every behavioural change:

1. Add or update the smallest test that demonstrates the missing behaviour or defect.
2. Run the affected package test and confirm the expected failure when practical.
3. Implement the behaviour.
4. Repeat the focused test until it passes.
5. Run `quick` and fix every failure.
6. Run `full` before declaring the phase complete.
7. Run `platform`, then applicable `hardware-read` and `hardware-write` gates before changing support status.

Environment limitations and unavailable hardware must be recorded as blockers or unvalidated slices, never as successful verification.

---

## 22. Security and operational requirements

- Normal operation must not require administrator/root privileges.
- Installation may use elevation for Linux udev rules or macOS packaging.
- Raw input writes must be guarded.
- Do not expose arbitrary VCP writes.
- Validate numeric input values before passing them to native APIs.
- Bound all buffer sizes from native calls.
- Validate UTF-16 and C-string conversions.
- Avoid retaining native handles in long-lived UI state.
- Use deterministic cleanup.
- Treat EDID and capability strings as untrusted binary/text input.
- Fuzz EDID and capability parsers.
- Do not use shell command construction in production backends.
- Development subprocess adapters must use direct argument arrays, not a shell.
- Diagnostic bundles must support redaction of serial numbers and topology paths.
- GitHub Actions must be pinned to full commit SHAs and use least-privilege workflow permissions.
- Qualification jobs must not run on untrusted pull-request code with access to physical hardware.
- The production binary must not contain the qualification-only raw-write command.
- Linux installation documentation must explicitly disclose the security effect of granting I²C device access.
- Treat configuration, EDID, capability strings, diagnostic imports, and native text buffers as untrusted input.

---

## 23. Packaging and release

### 23.1 Linux

Deliver:

- Native executable
- Optional `.deb`
- Udev rule
- Installation and troubleshooting documentation
- Clear instructions for `i2c-dev`
- No dependency on `ddcutil`
- A support claim only for Ubuntu versions, monitor identities, and connection paths present in the qualification matrix

### 23.2 Windows

Deliver:

- Native `.exe`
- No third-party DLL
- Optional installer
- Documentation for DDC/CI OSD configuration
- Architecture target documented, likely `amd64` initially unless `arm64` Windows support is explicitly added
- Windows 11 hardware qualification separate from Windows Server CI build evidence

### 23.3 macOS

Deliver:

- `darwin/arm64` executable or application bundle
- CGO-linked system frameworks only
- Code signing and notarisation for distributed releases
- Compatibility notes for private CoreDisplay usage
- No Homebrew package dependency at runtime
- Compatibility metadata restricted to tested macOS point releases/builds because the private API may change between updates

### 23.4 Reproducibility

- Pin Go module versions.
- Commit `go.sum`.
- Record build flags.
- Generate checksums for release artifacts.
- Produce an SBOM if releases are distributed externally.
- Review transitive Bubble Tea dependencies as part of release preparation.
- Generate release artifacts only from a commit for which the required `platform` gate passes.
- Publish the support matrix and qualification-record hashes with the release.
- Do not imply that all planned platforms or monitors are supported when an incremental release contains fewer qualified slices.

---

## 24. Implementation phases

Each phase uses the loop in Section 21: `quick` during implementation, `full`
before phase exit, the native `platform` gate for platform work, and the guarded
hardware modes before a support claim. Lack of hardware evidence may leave a
backend `experimental`; it must not be treated as a failed automated build or a
supported slice.

### Phase 0 — Controlled migration, validation harness, and macOS risk gate

Deliverables:

- Inventory the existing hotkey/PIP/external-utility implementation and map each
  retained requirement to this plan.
- Introduce the pinned Go/Bubble Tea toolchain and repository-owned validation
  entry points without overwriting unrelated staged or uncommitted work.
- Add CI skeletons and build-tag checks for every planned platform.
- Run the read-only macOS 26.6 feasibility gate from Section 14.1 against the
  attached Dell displays and record the result as an evidence artifact.
- Remove legacy runtime paths, configuration acceptance, workaround plist, and
  committed executables only after replacement tests cover the intended path.

Exit criteria:

- The migration inventory and strict legacy-config rejection test exist.
- `quick` passes on the development host and CI definitions are syntax-valid.
- Existing unrelated work remains unchanged.
- The macOS evidence records whether service correlation and VCP `0x60` reads
  are feasible. A failed gate stops macOS write implementation and triggers a
  documented backend re-plan; it does not justify guessed private APIs.

### Phase 1 — Core domain and deterministic test harness

Deliverables:

- Generation-aware snapshots and monitor references.
- Descriptor, input, support, verification, and typed error models.
- Raw backend/session contracts plus the logical service layer.
- Fake backends, transports, clocks, sleepers, lock observers, and cancellation
  controls shared by unit, service, TUI, and CLI tests.

Exit criteria:

- Backend contract suites pass for normal, stale, disconnected, partial, and
  ambiguous-monitor cases.
- Retry timing and lock ownership are deterministic under tests.
- Common packages contain no OS-native types and meet Section 21 coverage gates.

### Phase 2 — EDID, DDC/CI, profile, and support foundations

Deliverables:

- Bounded EDID parser, checksum handling, identity derivation, and ambiguity
  detection.
- DDC/CI framing, checksum, reply correlation, retry classification, and golden
  vectors.
- Strict versioned configuration, profile matching, logical/raw mapping, and
  support-matrix loading.
- Seed corpora and fuzz targets for all untrusted binary/text parsers.

Exit criteria:

- Valid, corrupt, truncated, extension, duplicate, and topology-fallback EDID
  cases pass.
- Golden DDC vectors and bounded fuzz smoke tests pass.
- No profile or input is exposed as supported without matching qualification
  evidence.

### Phase 3 — CLI, Bubble Tea TUI, diagnostics, and capture on fake hardware

Deliverables:

- The `xdispddcswtchr` CLI with stable exit codes and versioned JSON output.
- Bubble Tea v2 discovery, selection, switch, result, diagnostic, and help states.
- Generation-aware asynchronous commands with stale-completion suppression.
- Diagnostic redaction and a separately built, confirmation-gated qualification
  capture command.

Exit criteria:

- Fake end-to-end workflows pass through both the CLI and TUI.
- Normal binaries cannot perform arbitrary raw VCP writes.
- Golden JSON, non-TTY behavior, cancellation, resize, hotplug-message, and race
  tests pass in `full` mode.

### Phase 4 — Apple Silicon macOS backend and initial Dell slice

Deliverables:

- CoreGraphics enumeration, IOKit/CoreDisplay correlation, minimal C bridge,
  dynamic symbol resolution, VCP `0x60` get/set, and typed compatibility errors.
- Native resource-lifecycle and missing-symbol tests.
- Qualification of Dell S3423DWC and Dell U4025QW inputs available on the
  macOS 26.6 build and Apple Silicon host recorded by Phase 0.

Exit criteria:

- `full` and macOS `platform` gates pass with the supported Xcode/SDK.
- Missing symbols, disconnected displays, and partial opens fail safely.
- Every exposed Dell input has read/write/recovery evidence and a matching
  immutable support record; any unqualified input remains hidden or experimental.
- At least one qualified macOS-monitor-connection slice is eligible to become
  the first incremental release slice.

### Phase 5 — Hardening and release of the first supported slice

Deliverables:

- Hotplug, stale-generation, sleep/resume, power-cycle, identical-monitor, and
  multiple-monitor handling.
- Resource-leak review, race tests, sustained-operation tests, and fuzz corpus
  regression.
- macOS packaging, signing/notarisation where applicable, checksums, SBOM,
  licence inventory, installation docs, and known-issues matrix.

Exit criteria:

- The exact first slice passes Section 25's per-slice support gate.
- Packages contain neither the qualification writer nor external monitor-control
  utilities.
- Published claims match the support matrix and identify unqualified slices as
  planned or experimental.

### Phase 6 — Windows backend and qualification

Deliverables:

- CCD/GDI/SetupAPI identity correlation, complete physical-monitor enumeration,
  Dxva2 VCP transport, handle ownership, and Windows diagnostics.
- Native ABI, UTF-16, retry, partial-cleanup, and unavailable-monitor tests.
- Windows 11 hardware qualification for available target monitors and connection
  paths.

Exit criteria:

- `full` and Windows `platform` gates pass.
- Hardware read/write/recovery evidence exists for each slice added to the
  support matrix.
- No `ddcci`, `displayController`, or third-party runtime utility is required.

### Phase 7 — Linux backend and qualification

Deliverables:

- DRM connector discovery, EDID reading, connector-to-I²C correlation, native
  `i2c-dev` DDC/CI, adapter locking, Linux diagnostics, and least-privilege udev
  packaging.
- Mockable ioctl/short-transfer tests and permission-error classification.
- Ubuntu 26.04 non-root hardware qualification for available target monitors and
  connection paths.

Exit criteria:

- `full` and Linux `platform` gates pass.
- The installed permission model works without root or world-writable I²C nodes.
- Hardware read/write/recovery evidence exists for each slice added to the
  support matrix, with no `ddcutil` runtime dependency.

### Phase 8 — LG profile qualification by platform

Deliverables:

- Verified LG UltraGear OLED 45GX950A-B mappings, delays, and verification modes.
- HDR, VRR, dual-mode, direct-connection, and applicable dock/adapter evidence.
- Separate support records for each OS/version/architecture/connection slice.

Exit criteria:

- Every exposed logical input is physically verified on the slice where it is
  claimed.
- Mode-dependent limitations are represented in the profile and compatibility
  data, not only prose.

### Phase 9 — Broader matrix and release maturity

Deliverables:

- Additional OS point releases, architectures where planned, docks/adapters, and
  monitor firmware revisions qualified incrementally.
- Reproducible multi-platform release automation, provenance, checksums, SBOMs,
  licence review, installation/troubleshooting documentation, and published
  support-matrix history.

Exit criteria:

- Every released artifact is traceable to the commit, toolchain, CI run, and
  evidence records that justified its support claims.
- All roadmap-wide criteria in Section 26 are met before the project is described
  as complete across all planned platforms and monitors.

---

## 25. Acceptance criteria

### 25.1 Cross-platform criteria

- The same common descriptor and logical input types are used on all platforms.
- The TUI uses the pinned Bubble Tea v2 release.
- Discovery and switching do not require a third-party monitor-control utility.
- A monitor operation uses a stable ID plus the current enumeration generation,
  never an enumeration index or stale native handle.
- Unsupported or ambiguous monitors cannot be switched through a guessed logical mapping.
- An unqualified monitor can be made locally usable only through explicit,
  complete TUI review; that state is labelled `user-qualified`, scoped to its
  exact identity/endpoint, and revalidated before every write.
- Only VCP `0x60` is used by normal application operations.
- Hardware I/O never blocks the Bubble Tea update loop.
- Native handles are not retained in UI state.
- CLI JSON, exit codes, and diagnostic schemas are versioned and covered by golden tests.
- Diagnostic output can explain discovery, permission, transport, profile, and
  qualification failures without exposing raw EDID or serial data by default.
- `quick` and `full` validation pass for the release commit, with the required
  coverage, race, analyzer, vulnerability, fuzz-smoke, and cross-build gates.
- Legacy hotkey/PIP configuration is rejected, and neither the legacy runtime
  path nor committed executable artifacts remain in a release.

### 25.2 Per-slice support gate

An OS/version/architecture/monitor/firmware/connection slice is `supported` only
when all of the following are true:

- The exact release commit passes `quick`, `full`, and that OS's native
  `platform` gate.
- Discovery and identity correlation are verified against captured diagnostics.
- Every advertised logical input has a confirmed raw value and guarded write
  evidence; a return path or documented display-path-change recovery is tested.
- Disconnect, stale-generation, timeout, permission/unavailability, and
  resource-cleanup behavior pass for the slice.
- An immutable, schema-versioned qualification record is reviewed and its hash
  appears in the support matrix.
- The packaged normal binary contains no qualification-only raw writer or
  external monitor-control utility.

### 25.3 Linux criteria

- The Ubuntu 26.04 native build and test gate passes.
- Uses native DRM/sysfs and `i2c-dev`.
- Operates without root after documented installation setup.
- Does not require `ddcutil`.
- Serialises transactions per I²C adapter.
- The installed rule grants only the documented device access and does not make
  I²C nodes world-writable.

### 25.4 Windows criteria

- The Windows 11 native build and test gate passes.
- Uses native Windows monitor configuration APIs.
- Correctly allocates and releases physical monitor structures/handles.
- Correlates CCD target, GDI/SetupAPI identity, EDID, and every returned physical
  monitor rather than trusting descriptions or element zero.
- Does not require third-party DLLs or Go monitor-control libraries.

### 25.5 macOS criteria

- The exact qualified macOS 26 point release and Apple Silicon native gate passes.
- Uses a narrow CGO bridge.
- Resolves private CoreDisplay functions dynamically.
- Fails gracefully when symbols or services are unavailable.
- Does not require Homebrew, Rust, or external monitor-control programs.

### 25.6 Monitor criteria

For each target monitor:

- EDID matcher is verified.
- Each exposed logical input has a confirmed raw value.
- Switching to each exposed input succeeds on each supported OS/connection path
  covered by that support record.
- Switching away from the current host is reported correctly even when read-back becomes unavailable.
- Known dock/adapter, monitor-menu mode, HDR, VRR, or dual-mode limitations are
  documented and machine-associated with the affected slice.

### 25.7 Incremental release acceptance

- At least one slice passes Sections 25.1, 25.2, and its platform/monitor criteria.
- Release artifacts, checksums, provenance, SBOM, licence inventory, support
  matrix, and known issues all identify the same commit and version.
- Planned, build-validated, and experimental slices are not described as supported.
- A platform without a qualified slice may be omitted from that release without
  blocking release of an independently qualified slice.

---

## 26. Definition of done

### 26.1 Incremental release done

An incremental release is done when:

1. The common implementation passes all Section 25.1 checks.
2. At least one exact slice passes the per-slice gate and applicable platform and
   monitor criteria.
3. The Bubble Tea interface and CLI complete discovery, selection, and input
   switching without blocking or acting on stale generations.
4. The release support matrix contains only evidence-backed mappings and points
   to immutable qualification records.
5. No third-party monitor-control executable/library or qualification-only raw
   writer is present in normal runtime packages.
6. The only normal monitor write is VCP `0x60`.
7. Error handling covers ambiguity, permissions, unavailable backend, stale
   enumeration, disconnect, unsupported read/write, timeout, malformed reply,
   failed write, and display-path-change-after-success.
8. Diagnostic export is sufficient to investigate identity, correlation,
   transport, profile, and qualification state while applying default redaction.
9. Packaging, provenance, checksums, SBOM, dependency/licence, security, and
   resource-lifecycle reviews are complete for every artifact actually released.
10. Documentation accurately distinguishes supported, experimental,
    build-validated, and planned slices.

### 26.2 Roadmap done

The complete roadmap is done only when:

1. Linux, Windows, and Apple Silicon macOS each have at least one supported slice
   satisfying their platform criteria.
2. Dell S3423DWC, Dell U4025QW, and LG UltraGear OLED 45GX950A-B profiles contain
   only physically verified mappings for every slice claimed.
3. The documented Ubuntu 26.04 non-root path, Windows 11 path, and qualified
   macOS 26 point-release path are reproducibly packaged and validated.
4. Hotplug, sleep/resume, power-cycle, multiple-monitor, and identical-monitor
   recovery evidence exists for every generally supported backend.
5. No remaining planned acceptance criterion is represented as completed by a
   cross-build, mock, or unavailable-hardware result alone.

---

# Appendices

## Appendix A — Reference repository assessment

The following repositories are non-authoritative implementation references, not
production architecture dependencies. Before using one, record the reviewed
commit, licence at that commit, and review date in the implementing change. A
reference review older than 120 days must be refreshed; historical code must not
override the current vendor ABI, SDK headers, kernel documentation, or VESA
standard.

| Repository | Actual platform | Language | Licence | Intended use |
|---|---|---:|---:|---|
| `rockowitz/ddcutil` | Linux | C | GPL-2.0 | Behaviour, timing, retry, correlation, and troubleshooting reference; optional development comparator |
| `unix755/dccli` | Windows | Go | GPL-3.0 | Windows CLI reference only; it is not a Linux implementation |
| `unix755/displayController` | Windows | Go | GPL-3.0 | Windows API reference; do not incorporate into a proprietary implementation |
| `haimgel/ddc-macos-rs` | macOS Intel and Apple Silicon | Rust | MIT | Primary Apple Silicon CoreDisplay reference |
| `kfix/ddcctl` | macOS | C/Objective-C | GPL-3.0 | Historical macOS reference; maintenance mode |
| `wufeng5702/ddcci` | Windows | Go | MIT | Windows API/marshalling audit reference; do not import without a full review |
| `charmbracelet/bubbletea` | Cross-platform terminal UI | Go | MIT | Required interactive user-interface framework |

Corrections from the initial research:

- `unix755/dccli` is Windows-only, not Linux.
- It is a Go CLI over `github.com/unix755/displayController`.
- `wufeng5702/ddcci` is Windows-only.
- Its module path is `github.com/niluan304/ddcci`, despite the current repository owner being `wufeng5702`.
- The current Bubble Tea v2 module path is `charm.land/bubbletea/v2`.

### Licence policy

- Do not copy source from GPL-licensed projects into this project unless the project’s licensing strategy is intentionally made compatible and reviewed.
- Invoking `ddcutil` as a separate developer tool is different from linking to `libddcutil`; production architecture avoids both.
- MIT-licensed reference code may be studied, but local native implementation is still preferred when small and auditable.
- Preserve all required licence notices for Bubble Tea and any other distributed dependency.

---

## Appendix B — Rationale for avoiding third-party runtime utilities

A runtime dependency on a platform-specific utility would create inconsistent product behaviour:

- Linux discovery could depend on `ddcutil`, while Windows and macOS use different mechanisms.
- Utility output formats may change.
- Packaging and permissions become harder to control.
- Monitor identity could be based on tool-specific indexing.
- Error classification becomes process-output parsing.
- Availability differs by OS and package source.
- GPL linking or source incorporation may affect distribution decisions.

Native backends provide a common contract while allowing unavoidable OS differences to remain isolated.

`ddcutil` remains valuable as a Linux behavioural reference because it handles:

- Hotplug and resume
- Temporary I²C permission failures
- Adapter and driver quirks
- Retry and sleep timing
- EDID-to-I²C mapping
- USB Monitor Control Class devices

Those behaviours should inform testing and hardening without becoming a required runtime dependency.

---

## Appendix C — Windows reference implementation warning

The reviewed `wufeng5702/ddcci` implementation is useful as an API reference but should not be adopted without correction and audit.

Its physical monitor construction has historically allocated a byte buffer and interpreted it as a `PHYSICAL_MONITOR` result. The Windows API expects an array of structures sized according to the count returned by `GetNumberOfPhysicalMonitorsFromHMONITOR`.

The project implementation must use the documented layout and correct pointer/array semantics.

Physical monitor descriptions are not stable identifiers. EDID and display-device correlation are required.

---

## Appendix D — macOS private API note

Apple Silicon DDC/CI control commonly relies on private CoreDisplay symbols rather than a stable public DDC/CI API.

Reference implementations use functions such as:

```c
IOAVServiceCreateWithService
IOAVServiceReadI2C
IOAVServiceWriteI2C
```

and discover `DCPAVServiceProxy` objects.

Consequences:

- Compatibility can change in a macOS update.
- Symbols must be dynamically resolved.
- Registry assumptions must be validated at runtime.
- Failure must be reported as backend incompatibility rather than a crash.
- Compatibility testing is a release requirement, not an optional enhancement.

---

## Appendix E — Input-source VCP notes

The expected MCCS feature is:

```text
0x60 — Input Source
```

Common historical values may include DisplayPort, HDMI, and USB-C values, but target monitor profiles must not be populated from generic tables alone.

Reasons:

- Firmware may use different values.
- Capability strings may be incomplete or malformed.
- Connector labels may not correspond one-to-one with raw values.
- Thunderbolt and USB-C may share or split values.
- PIP/PBP and dual-mode behaviour may alter semantics.
- Some monitors permit write but not reliable read-back.

Only hardware-confirmed values should be exposed.

---

## Appendix F — Monitor support notes

### Dell monitors

For the supported Dell monitors:

- DDC/CI must be enabled in the monitor OSD.
- Exact input values must be captured from hardware.
- PIP/PBP may alter input-source behaviour.
- USB/KVM routing must not be assumed to be controlled by standard VCP input switching.

### Dell S3423DWC

- DDC/CI transport is supported by vendor tooling/documentation.
- Tracked capability evidence maps USB-C `0x1B`, HDMI-1 `0x11`, and HDMI-2
  `0x12`; write/readback and recovery evidence remain required per platform.
- PIP/PBP and USB behaviour require qualification.

### Dell U4025QW

- DDC/CI input switching is expected.
- Tracked capability evidence maps DP-1 `0x0F`, HDMI-1 `0x11`, and infers
  Thunderbolt-1 `0x19` from the exhaustive documented physical-input set.
- Thunderbolt/USB-C, DisplayPort, and HDMI must be tested separately.
- KVM or USB upstream switching may be automatic firmware behaviour but is outside the tool’s claimed function.

### LG UltraGear OLED 45GX950A-B

- Vendor software exists for Windows and macOS, but it must not be a runtime dependency.
- A model-specific public MCCS/VCP table is not assumed.
- Tracked capability evidence maps HDMI-1 `0x11`, HDMI-2 `0x12`, DP-1 `0x0F`,
  and the supplied DP-2 label to `0x10`; its relationship to the model's
  physical USB-C input is unresolved.
- VCP `0x60`, input values, dual-mode behaviour, HDR, VRR, and gaming-mode effects require physical qualification.
- OLED-specific functions are outside scope.

---

## Appendix G — Example diagnostic output

```json
{
  "schema_version": 1,
  "generated_at": "2026-08-06T00:00:00Z",
  "application": {
    "version": "0.1.0",
    "commit": "0123456789abcdef0123456789abcdef01234567"
  },
  "os": {
    "name": "linux",
    "version": "ubuntu-26.04",
    "architecture": "amd64"
  },
  "backend": {
    "name": "linux-native",
    "transport": "i2c-dev",
    "endpoint": "redacted:9f1b..."
  },
  "support": {
    "state": "backend-experimental",
    "qualification_record": null
  },
  "redaction": {
    "enabled": true,
    "method": "bundle-scoped-sha256"
  },
  "monitor": {
    "id": "mon-7c4q2k6d4g...",
    "manufacturer": "DEL",
    "product_code": 4660,
    "serial": "redacted:30a7...",
    "model_name": "DELL U4025QW",
    "connector": "displayport-1",
    "identity_state": "hardware-stable"
  },
  "edid": {
    "raw": "",
    "checksum_valid": true
  },
  "input_source": {
    "vcp_code": "0x60",
    "read_supported": true,
    "current_raw": 17,
    "current_logical": "hdmi-1",
    "profile_values": {
      "displayport-1": 15,
      "hdmi-1": 17
    }
  },
  "capabilities_raw": "",
  "timings": {
    "read_ms": 42,
    "write_ms": null
  },
  "errors": []
}
```

Values in this example are illustrative and do not establish a supported monitor mapping.

---

## Appendix H — Source and documentation references

### Source-currency gate

The links below were reviewed on 2026-08-06. Each implementation or release
change must record the exact official pages/specification editions it relied on
and a review date no more than 120 days old. Recheck vendor documentation,
current SDK/kernel headers, release notes, runner labels, tool releases, and
monitor manuals when the gate is run; do not infer current behavior from an old
blog post, forum answer, search excerpt, or reference repository.

Where a private API has no vendor contract, current-host symbol/service evidence
and a pinned, recently reviewed reference implementation may inform an
experimental design. Only the repository's own qualification evidence can turn
that design into a support claim.

### Required UI framework

- Bubble Tea repository: <https://github.com/charmbracelet/bubbletea>
- Bubble Tea v2.0.8 release: <https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.8>
- Bubble Tea v2.0.8 package documentation: <https://pkg.go.dev/charm.land/bubbletea/v2@v2.0.8>
- Go release history: <https://go.dev/doc/devel/release>
- Go vulnerability management and `govulncheck`: <https://go.dev/doc/security/vuln/>

### Build and CI documentation

- GitHub-hosted runners: <https://docs.github.com/en/actions/reference/runners/github-hosted-runners>
- Current runner image labels and contents: <https://github.com/actions/runner-images>
- Full-SHA action pinning guidance: <https://docs.github.com/en/actions/how-tos/create-and-publish-actions/manage-custom-actions>

### Reference implementations

- Linux `ddcutil`: <https://github.com/rockowitz/ddcutil>
- Windows `dccli`: <https://github.com/unix755/dccli>
- Windows `displayController`: <https://github.com/unix755/displayController>
- Apple Silicon/macOS `ddc-macos-rs`: <https://github.com/haimgel/ddc-macos-rs>
- Historical macOS `ddcctl`: <https://github.com/kfix/ddcctl>
- Windows `ddcci`: <https://github.com/wufeng5702/ddcci>

### Primary platform and standards documentation

- Linux kernel `i2c-dev` userspace interface: <https://docs.kernel.org/i2c/dev-interface.html>
- Linux DRM/KMS documentation: <https://docs.kernel.org/gpu/drm-kms.html>
- Microsoft `QueryDisplayConfig`: <https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-querydisplayconfig>
- Microsoft `DISPLAYCONFIG_TARGET_DEVICE_NAME`: <https://learn.microsoft.com/en-us/windows/win32/api/wingdi/ns-wingdi-displayconfig_target_device_name>
- Microsoft physical-monitor enumeration: <https://learn.microsoft.com/en-us/windows/win32/api/physicalmonitorenumerationapi/nf-physicalmonitorenumerationapi-getphysicalmonitorsfromhmonitor>
- Microsoft low-level monitor configuration: <https://learn.microsoft.com/en-us/windows/win32/monitor/using-the-low-level-monitor-configuration-functions>
- Apple Quartz Display Services: <https://developer.apple.com/documentation/coregraphics/quartz-display-services>
- Apple IOKit: <https://developer.apple.com/documentation/iokit>
- VESA free standards, including DDC/CI, MCCS, and EDID material: <https://vesa.org/vesa-standards/>

The macOS backend must additionally review the currently installed Xcode/SDK
headers and macOS release notes because CoreDisplay DDC entry points are private
and are not promised by Apple documentation.

### Target monitor documentation

- Dell U4025QW manuals and documents: <https://www.dell.com/support/product-details/en-us/product/u4025qw-monitor/resources/manuals>
- Dell S3423DWC manuals and documents: <https://www.dell.com/support/product-details/en-us/product/s3423dwc-monitor/resources/manuals>
- LG 45GX950A-B product support and current manuals: <https://www.lg.com/us/support/product/lg-45GX950A-B.AUS>

Implementation changes must cite the applicable official platform documentation
for native ABI, structure, buffer, handle, and lifecycle decisions. Qualification
changes must cite the current vendor manual and the captured hardware record; a
vendor manual alone does not verify VCP mappings.
