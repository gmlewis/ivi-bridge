# IVI Closed-Loop Delta — Wi-Fi HTTP API (Reverse-Engineered)

Reverse-engineered from the IVI 3licer app bundle (a rebranded Snapmaker Luban) and
verified against a live IVI printer.

## Transport

- Host: `http://<printer-ip>:8080`
- Port **8080** is the only listening port.
- Embedded server is **kore** (a C web framework). `GET /` returns `404`.
- No TLS. File uploads are **unauthenticated**.
- Responses are JSON, except uploads which return plain text (`"Upload successfully"`).

## Endpoints

| Endpoint | Method | Body | Notes |
|---|---|---|---|
| `/api/v1/status` | `GET` | — | Returns `{"status":"IDLE"}` (note: reports `IDLE` even mid-print!) |
| `/api/v1/upload` | `POST` | multipart `file` | **Works unauthenticated** (HTTP 201 `"Upload successfully"`) |
| `/api/v1/connect` | `POST` | form `token` | Returns `412 token err` on stock firmware without paired app token |
| `/api/v1/start_print`, `pause_print`, `resume_print`, `stop_print`, `execute_code` | `POST` | — | Return `404 Not Found` on stock IVI Wi-Fi firmware (not implemented in `kore`) |

## USB Serial Interface (Marlin)

Unlike the Wi-Fi HTTP server, the IVI's **USB port** exposes a full STM32 USB-CDC serial console (`115200` baud) running **Marlin** (`STM32F407VET6`, `AUTOREPORT_TEMP`, `AUTOLEVEL`, `BABYSTEPPING`, `ARCS`, `EMERGENCY_PARSER`).

Over USB serial, standard Marlin G-code works directly:
- Live temperature readouts (`M105` -> `ok T:28.80 /0.00 B:26.85 /0.00 @:0 B@:0`)
- Live position & delta motor counts (`M114` -> `X:0.00 Y:0.00 Z:234.46 E:0.00 Count A:... B:... C:...`)
- Homing (`G28`), relative/absolute X/Y/Z jogging (`G91` / `G1`), nozzle & bed heating (`M104` / `M140`), extruder feed/retract with custom speed (`G1 E... F...`), fan control (`M106` / `M107`), and baby-stepping (`M290`).

## Upload Wire Format

```sh
curl -X POST "http://<printer-ip>:8080/api/v1/upload" \
     -F "file=@/path/to/part.gcode;filename=part.gcode"
```

Filenames are preserved verbatim (including spaces and parentheses) and appear on the
printer's touchscreen **Files** list.

## Known Firmware Limits

- **No file listing:** No HTTP endpoint enumerates stored files; the printer touchscreen
  is the only place to see stored files.
- **No temperature read-out:** `/api/v1/status` returns only the state string.
- **Status reports `IDLE` mid-print:** Never gate safety-critical actions on `/api/v1/status`
  alone; `ivi-bridge` keeps `--allow-start` **off by default** for this reason.
- **Duplicate filename quirk:** Re-uploading a filename that already exists on the printer
  can silently keep the stale file; `ivi-bridge` enables `--unique-names` by default to
  append `_HHMMSS` automatically.
