# XDispDDCSwtchr

Cross‑platform hotkey → DDC/CI switcher.

- ✅ CLI backends: `ddcutil` (Linux), `ddcctl` (macOS), `ControlMyMonitor.exe` (Windows)
- ✅ JSON configuration of models, monitors, and hotkeys
- 🔄 Native backends scaffolding: Windows (implemented), macOS/Linux (stubs)

## Quick start

1) Edit `XDispDDCSwtchr-Settings.json`.
2) Build and run:

```bash
# Linux prerequisites for hotkey support
sudo apt-get install libx11-xcb-dev libxtst-dev libxkbcommon-dev libxkbcommon-x11-dev \
     libxinerama-dev libxrandr-dev libxcursor-dev

# Linux/macOS
go build -o xdispddcswtchr ./cmd/xdispddcswtchr
./xdispddcswtchr
```

```powershell
# Windows
go build -o xdispddcswtchr.exe .\cmd\xdispddcswtchr\
.\xdispddcswtchr.exe
```

By default the **CLI backend** is used. To prefer the native backend on Windows, set `"backend": "native"` in the JSON.

## Build tags
- No tags: CLI backend (all OSes)
- `-tags native`: Enable native backend selection logic. Currently Windows native is implemented; macOS/Linux are stubs.

## Notes
- Linux may require `i2c-dev` access for future native backend; for CLI make sure `ddcutil` works manually.
- macOS requires appropriate entitlement/permissions for low-level IOKit access; CLI uses `ddcctl`.
```
