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
sudo ln -sfn /Library/Preferences/com.apple.windowserver.displays.plist /Library/Preferences/com.apple.windowserver.plist
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

## Platform-Specific Notes

### Linux (ddcutil CLI backend)
- Requires `ddcutil` installed: `sudo apt-get install ddcutil`
- May require `i2c-dev` kernel module: `sudo modprobe i2c-dev`
- Verify DDC/CI support: `ddcutil detect`
- **Limitation**: `ddcutil` currently ignores monitor ID and affects all monitors
- **Performance**: Commands may take 1-2 seconds per execution
- **Timeout**: 30-second timeout prevents hanging on unsupported monitors

### macOS (ddcctl CLI backend)
- Requires `ddcctl` installed: `brew install ddcctl`
- **Workaround**: Some ddcctl builds require a plist symlink:
  ```bash
  sudo ln -sfn /Library/Preferences/com.apple.windowserver.displays.plist \
               /Library/Preferences/com.apple.windowserver.plist
  ```
- **Monitor ID Format**: Use `MonitorName@N` where N is the display number (e.g., `LG45GX950A@4`)
- **VCP Code 0x60**: Input switching uses `-i` flag for better compatibility
- **Timeout**: 30-second timeout prevents hanging on unsupported monitors

### Windows (ControlMyMonitor CLI backend)
- Requires [ControlMyMonitor.exe](https://www.nirsoft.net/utils/control_my_monitor.html) in PATH
- **Monitor ID Format**: Use monitor name from ControlMyMonitor (e.g., `"\\.\DISPLAY1"`)
- **VCP Code Format**: Supports both hex (`0x60`) and decimal (`96`)
- **Timeout**: 30-second timeout prevents hanging on unsupported monitors

### Windows (Native backend)
- Enable with `"backend": "native"` in settings JSON
- Uses Windows DDC/CI API directly (no external tools required)
- **Limitation**: If multiple physical monitors share one logical display, only the first is controlled
- **VCP Code Format**: Supports both hex (`0x60`) and decimal (`96`)
- **Monitor ID Format**: Use `DISPLAY1`, `DISPLAY2`, etc., or leave empty for first monitor

## Troubleshooting

### Commands timeout or hang
- Verify your monitor supports DDC/CI (check monitor OSD settings)
- Test manually with platform tools (`ddcutil`, `ddcctl`, or `ControlMyMonitor.exe`)
- Some monitors require DDC/CI to be explicitly enabled in settings

### Monitor not responding to commands
- Check that the correct monitor ID is configured
- Verify VCP codes match your monitor's capabilities
- Some monitors only respond when powered on (not in standby)

### macOS plist errors
- Run the symlink command shown above
- Ensure you have appropriate permissions

## Notes
- Linux may require `i2c-dev` access for future native backend; for CLI make sure `ddcutil` works manually.
- macOS requires appropriate entitlement/permissions for low-level IOKit access; CLI uses `ddcctl`.
```
