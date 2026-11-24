# Attack Shark X11 Battery Tray

An ultra-lightweight, zero-dependency **Windows system tray** utility written in pure Go that displays the live battery level of an **Attack Shark X11** wireless gaming mouse connected via its **2.4 GHz USB dongle** (or wired USB-C).

No manufacturer bloatware, no background runtimes, no third-party packages — compiles to a single **~1.85 MB** native Windows executable using Win32 APIs directly.

---

## Features

- **Dynamic System Tray Icon** — Renders a crisp pixel-aligned percentage readout and color-coded charge bar (green / amber / red / cyan when charging), automatically adapting to Windows light and dark taskbar themes.
- **Passive HID Monitoring & Auto-Reconnect** — Listens directly to the dongle's HID event stream (`VID 0x1D57`, `PID 0xFA60` / `0xFA55` / `0x22A9`, `Usage Page 0x000A`) and recovers cleanly when the dongle is unplugged, reconnected, or when Explorer restarts.
- **Sleep State Awareness** — Retains the last known percentage with a sleeping indicator whenever the mouse stops emitting heartbeats.
- **Low-Battery Notification** — Sends a native Windows tray warning once per discharge cycle when battery drops to **15% or below**.
- **Start with Windows** — One-click toggle in the tray context menu (`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`).
- **Single-Instance Guard** — Prevents duplicate tray icons via a named Win32 mutex.
- **CLI Diagnostic Modes** — Includes `--once` and `--raw` flags for quick terminal checks or packet inspection.

---

## Requirements

- **OS:** Windows 10 or Windows 11 (x64)
- **Build:** Go 1.22+ *(only required to build from source)*

---

## Building

Run the included PowerShell build script from the repository root:

```powershell
.\build.ps1
```

This outputs two standalone binaries in `bin\`:

| Binary | Purpose |
|--------|---------|
| `bin\attack-shark-battery.exe` | Silent GUI system tray application (no console window) |
| `bin\attack-shark-battery-cli.exe` | Console build for terminal queries and raw HID diagnostics |

---

## Usage

### System Tray Mode (Recommended)

Launch `bin\attack-shark-battery.exe`. Click (left or right) on the tray icon to:

- Inspect the current battery percentage, power state, and last update timestamp
- Force a manual dongle reconnect
- Toggle **Start with Windows**
- Exit the application

### CLI Mode

```powershell
# Read a single battery report and exit
.\bin\attack-shark-battery-cli.exe --once

# Stream incoming HID packets continuously
.\bin\attack-shark-battery-cli.exe --raw
```

Example output:

```text
Attack Shark X11 détectée (VID=0x1D57 PID=0xFA60)
[15:30:30] Batterie : 92% — Sur batterie (brut: 03-07-40-01-5C)
```

---

## Testing

```powershell
go vet ./...
go test -v ./...
```

---

## Project Structure

```text
.
├── cmd/tray/           # Entry point, CLI flags, single-instance guard, HID monitor loop
├── internal/
│   ├── autostart/      # Windows HKCU Run registry toggle
│   ├── hid/            # Win32 SetupAPI/HID enumeration, overlapped reader, packet parser
│   ├── icon/           # Pure-Go 32x32 RGBA HICON renderer with theme detection
│   └── tray/           # Native Win32 Shell_NotifyIconW window, popup menu, notifications
└── build.ps1           # Release build script
```

---

## HID Protocol Reference

The Attack Shark X11 2.4 GHz receiver (`1D57:FA60`) periodically broadcasts 5-byte HID Input Reports on **Usage Page `0x000A`**:

```text
03 <device_id> <40|41> <state> <percentage>
```

| Byte | Meaning |
|------|---------|
| `0` | Report ID (`0x03`) |
| `1` | Device / model identifier |
| `2` | Battery event command (`0x40` or `0x41`) |
| `3` | Power state: `0x00` sleeping, `0x01` discharging, `0x02` fully charged, `0x03` charging |
| `4` | Battery percentage (`0x00`–`0x64`, i.e., `0`–`100%`) |
