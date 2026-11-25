# ivi-bridge

<p align="center">
  <img src="assets/ivi-bridge-mascot.png" alt="ivi-bridge Go Gopher Mascot with Delta 3D Printer" width="520" />
</p>

Zero-dependency Go bridge (Wi-Fi + USB Marlin serial), tuned OrcaSlicer profiles,
native desktop control panel (`gpuix-svelte` + Bun), and native Android app
(Kotlin + Jetpack Compose) for the **IVI Closed-Loop Delta 3D Printer**.

Multiple clients — OrcaSlicer on macOS/Linux, desktop `ivi-control-panel` on
macOS/Linux, and Android phones/tablets over LAN or Tailscale — can all connect
concurrently to a single `ivi-bridge` instance with zero port conflicts.

Note that this project was written by Gemini 4 Neon with my guidance.

## Architecture

```text
  OrcaSlicer (macOS / Linux)  ──┐
  ivi-control-panel (Desktop) ──┼──▶  ivi-bridge (Go, port 5000)  ──┬── Wi-Fi :8080 (G-code upload)
  ivi-control-panel (Android) ──┘                                   └── USB Serial  (live Marlin control)
```

- **Wi-Fi (`:8080`)** handles fast, unauthenticated `.gcode` uploads (with automatic IVI Luban header promotion and duplicate-filename protection).
- **USB Serial (`--serial auto`)** holds a single mutex-protected `115200` baud Marlin connection for live nozzle/bed temperatures, `X/Y/Z` coordinates, homing (`G28`), jogging, heating, filament extrude/retract with custom speed, Z babystepping (`M290`), part fan, motors off (`M84`), emergency stop (`M112`), and a live G-code console — and feeds live temperatures into OrcaSlicer's `/api/printer` endpoint.

## Repository Layout

- **Root (`*.go`)** — Zero-dependency Go HTTP/OctoPrint bridge + pure-Go Linux/macOS USB serial Marlin controller.
- **[`orcaslicer/`](orcaslicer/)** — Tuned OrcaSlicer machine, PLA (`220 °C`), PETG (`240 °C`), and process presets for the IVI `0.3 mm` nozzle.
- **[`ivi-control-panel/`](ivi-control-panel/)** — Cross-platform native desktop app (`gpuix-svelte` + `bun`) with custom delta-printer app icon.
- **[`ivi-control-panel-android/`](ivi-control-panel-android/)** — Native Android app (Kotlin + Jetpack Compose, adaptive phone/tablet Material 3 dark UI).
- **[`systemd/`](systemd/)** — Example `systemd --user` service unit (`--serial auto`, restarts automatically across reboots).
- **[`PROTOCOL.md`](PROTOCOL.md)** — Reverse-engineered IVI Wi-Fi HTTP & USB Marlin serial protocol notes.

## Quick Start: Bridge Server (Go)

```sh
go build -o bin/ivi-bridge .
./bin/ivi-bridge --printer <printer-ip>:8080 --listen 0.0.0.0:5000 --serial auto
```

Or run a one-shot CLI upload (transforms Luban header + uploads over Wi-Fi):

```sh
./bin/ivi-bridge upload --printer <printer-ip>:8080 path/to/part.gcode
```

To run persistently on Linux across reboots, copy [`systemd/ivi-bridge.service.example`](systemd/ivi-bridge.service.example) to `~/.config/systemd/user/ivi-bridge.service`, set your printer IP, and run `systemctl --user enable --now ivi-bridge.service` (with `loginctl enable-linger $USER`).

## Quick Start: Desktop Control Panel (`gpuix-svelte` + `bun`)

```sh
cd ivi-control-panel
bun install
bun run start        # dev mode with hot reload
bun run build        # standalone macOS .app bundle with custom Dock icon
```

Set your bridge URL in the top bar (saved locally in `~/.config/ivi-control-panel/config.json`, or override via `IVI_BRIDGE_URL`).

## Quick Start: Android App (Kotlin + Jetpack Compose)

```sh
cd ivi-control-panel-android
gradle assembleDebug
adb install -r --user 0 app/build/outputs/apk/debug/app-debug.apk
```

Enter your `ivi-bridge` URL (e.g. `http://<bridge-host>:5000` over LAN or Tailscale) in the top bar and tap **Connect** — it is saved automatically on-device.

## OrcaSlicer Setup

1. Install the presets from [`orcaslicer/`](orcaslicer/README.md).
2. In **Printer Settings → Connection / Physical Printer**:
   - **Host Type:** `OctoPrint`
   - **Hostname / IP:** `<bridge-host>:5000` (or `127.0.0.1:5000` locally)
   - **API Key:** *(leave blank)*
3. Click **Upload** (not *Upload and Print*), then start the job from the IVI touchscreen's **Files** screen.

## Known IVI Firmware Limits

- Wi-Fi HTTP (`kore` on port `8080`) only implements `GET /api/v1/status` (which reports `IDLE` even mid-print) and unauthenticated `POST /api/v1/upload`; remote print-control endpoints return `404` and `/api/v1/connect` returns `412 token err`.
- Full live telemetry and motion/heater/extruder control work over the IVI's **USB serial** port (`--serial auto`); see [`PROTOCOL.md`](PROTOCOL.md) for details.
