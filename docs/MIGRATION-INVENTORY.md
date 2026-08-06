# Legacy migration inventory

Status: replacement implementation, 2026-08-06.

The repository baseline mixed monitor input switching with global hotkeys, PIP
operations, arbitrary configured VCP values, and subprocess adapters. The new
implementation retains only the monitor input-source requirement.

| Baseline path or behaviour | Decision | Replacement/control |
| --- | --- | --- |
| `internal/ddc/cli_unix.go` executing `ddcutil` or `ddcctl` | Removed | Native backend contract and Apple Silicon IOKit/CoreGraphics bridge |
| Windows `ControlMyMonitor.exe` configuration/path | Removed | No external executable lookup or subprocess backend; native Windows work remains a later slice |
| Legacy Windows single-monitor DXVA2 setter | Removed | It did not meet stable identity, read, verification, correlation, or lifecycle requirements |
| `internal/hotkeys` and `robotn/gohook` | Removed | Bubble Tea is the interactive UI; hotkeys are out of scope |
| `internal/logic` PIP toggle/source/position/size/swap | Removed | VCP `0x60` is the only production monitor function |
| User-authored model/raw VCP mappings | Removed | Exact built-in profiles plus evidence-backed support records |
| Default-first-display and `MonitorName@N` matching | Removed | EDID-derived stable identity plus enumeration generation |
| macOS WindowServer plist symlink workaround | Removed | Native enumeration; no filesystem workaround or elevated install step |
| Committed legacy executables | Removed after replacement platform validation | Reproducible `go build -trimpath` output only |

Strict configuration tests reject the previous `backend`, `cli`, `hotkeys`,
`models`, `monitors`, and `pip` fields. This is a deliberate migration boundary;
there is no compatibility mode that could reactivate legacy external tools.

The replacement path is covered by parser, identity, DDC framing, profile,
support-matrix, service, CLI, TUI, diagnostic-redaction, qualification-command,
and native macOS tests. Transient validation records are emitted by
`build-support/script/validate.sh` or its PowerShell equivalent.
