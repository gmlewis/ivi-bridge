# OrcaSlicer Profiles for the IVI Closed-Loop Delta (0.3 mm Nozzle)

Tuned user presets for **OrcaSlicer 2.5+** on the IVI Closed-Loop Delta 3D printer,
verified on print-in-place 4-elbow herringbone planetary gearboxes (`0.10 mm` gear
clearance) and structural housings.

## Included Profiles

- `machine/IVI 3D 0.3 nozzle - Tuned.json` — `4.5 mm` retraction @ `35 mm/s`, `0.3 mm` Slope Lift Z-hop, OctoPrint connection template (`print_host: 127.0.0.1:5000`).
- `filament/IVI3D Generic PLA - 220C.json` — `220 °C`, `0.985` flow, `5.0 mm` retraction @ `40 mm/s`, `1.5 mm` wipe enabled, `0.3 mm` Slope Lift Z-hop.
- `filament/IVI3D Tuned PETG - 240C.json` — `240 °C` (`245 °C` first layer), `0.97` flow, `6 mm³/s` max volumetric speed, `0.40` pressure advance.
- `process/0.30mm Normal Quality @IVI 3D - Tuned.json` — Arachne walls, outer-before-inner wall sequence, precise outer wall, `0.15 mm` elephant-foot compensation, `-0.02 mm` XY contour / `+0.06 mm` XY hole compensation, `gyroid` sparse infill, arc fitting enabled.

## Installation

Run the installer script (works on both macOS and Linux). It copies the user
profiles, syncs `machine_start_gcode` / `machine_end_gcode` into both your
non-cached user profile (`IVI 3D 0.3 nozzle - Copy.json`) and the `system/IVI3D`
profile, bumps the `IVI3D.json` bundle version, and deletes any stale
`system/IVI3D.opc` cache so OrcaSlicer never serves old G-code:

```sh
./install.sh
```

Then restart OrcaSlicer (if open), open **Printer Settings → Connection**, and
verify **Hostname / IP** points to your `ivi-bridge` address (e.g.
`127.0.0.1:5000` locally, or `<your-bridge-host>:5000` over LAN/Tailscale).
