# OrcaSlicer Profiles for the IVI Closed-Loop Delta (0.3 mm Nozzle)

Tuned user presets for **OrcaSlicer 2.5+** on the IVI Closed-Loop Delta 3D printer,
empirically dialed in over a large series of test prints for ultra-tight
print-in-place **4-elbow herringbone planetary gearboxes** and high-torque
structural actuator parts.

## Verified Print-in-Place Gear Results

Using the **`0.30mm Normal @IVI 3D`** profile on the IVI's `0.3 mm` nozzle:

- **`module = 1.0`, `tolerance = 0.0010` (1 micron!) test print:** Printed in place
  and **worked** — smooth meshing with zero wobble.
- **Full-size `PGB-CARTRIDGE` (`module = 1.5`), `tolerance = 0.0010` (1 micron!):**
  Also **worked** with zero wobble, though internal friction was high (stiff to
  turn by hand).
- **Next iteration (`0.30mm Strong @IVI 3D`, `tolerance = 0.0020` / 2 microns):**
  Combined with the high-torque QDD actuator redesign, bumping gear tolerance to
  `0.0020` targets lower internal friction while the **4-elbow herringbone**
  geometry keeps the sun, planets, and ring firmly locked axially with extremely
  quiet operation.

## Included Profiles

### Process Profiles (three clean choices — no clutter)
- `process/0.30mm Fast @IVI 3D.json` — **rapid dimensional fit-checks & photo-ready prototypes**
  where strength is a non-goal (`0.22 mm` layer height, `2` wall loops, `12%`
  `lightning` infill, `3` top / `2` bottom layers, fast `65–85 mm/s` internal
  speeds + `1200–1800 mm/s²` acceleration, while keeping a calm `40 mm/s` outer
  wall and `25 mm/s` `monotonicline` top surface for a silky-smooth photo finish).
- `process/0.30mm Normal @IVI 3D.json` — everyday prototypes and non-load parts
  (`0.15 mm` layer height, `3` wall loops, `15%` gyroid infill, `5` top / `4` bottom shell layers).
- `process/0.30mm Strong @IVI 3D.json` — high-torque actuator gears, housings,
  carriers, and output flanges (`0.15 mm` layer height, `4` wall loops = `1.2 mm` solid shell, `30%`
  gyroid infill, `5` top / `5` bottom shell layers).

All three process profiles share **every hard-won dimensional & anti-clog optimization** listed below.

### Machine & Filament Profiles
- `machine/IVI 3D 0.3 nozzle - Tuned.json` — tuned retraction, Slope Lift Z-hop,
  silent `2 mm/s` nozzle re-prime start G-code, clean shear-wipe end G-code, and
  OctoPrint/`ivi-bridge` connection template.
- `filament/IVI3D Generic PLA - 220C.json` — `220 °C`, `0.985` flow ratio,
  `5.0 mm` retraction @ `40 mm/s`, `1.5 mm` wipe, `0.3 mm` Slope Lift Z-hop.
- `filament/IVI3D Tuned PETG - 240C.json` — `240 °C` (`245 °C` first layer),
  `0.97` flow ratio, `6 mm³/s` max volumetric speed, `0.40` pressure advance.

---

## Critical Parameters Changed from Stock Defaults (and Why)

### 1. Process Settings (`0.30mm Normal @IVI 3D` & `0.30mm Strong @IVI 3D`)

| Parameter | Stock Default | Tuned Value | Why We Changed It |
| --- | --- | --- | --- |
| `slice_closing_radius` | `0.049` mm | **`0`** | **Critical gear fix:** Non-zero closing radius bridges tiny gaps between tightly spaced gear teeth (such as `0.001–0.002` tolerance herringbone gears), causing OrcaSlicer to drop entire planet-gear polygons on intermediate layers (observed as Layer-6 planet dropouts). Setting `0` preserves true 1:1 CAD tooth separation. |
| `resolution` | `0.012` mm | **`0.01`** mm | Preserves fine involute tooth curvature without over-segmenting arcs. |
| `xy_contour_compensation` / `xy_hole_compensation` | `0` (or negative in experiments) | **`0`** / **`0`** | Keeps true 1:1 CAD clearances; negative XY contour compensation combined with tight gears caused thin tooth features to collapse in the slicer. |
| `wall_generator` | `classic` | **`arachne`** | **Critical anti-clog & tooth-accuracy fix:** Classic walls leave hundreds of tiny disconnected gap-fill dots inside gear teeth, triggering rapid micro-retractions and partial clogs on a `0.3 mm` nozzle. Arachne dynamically varies line width to fill gear teeth cleanly in continuous paths. |
| `wall_sequence` | `inner/outer` | **`outer wall/inner wall`** | Prints the outer perimeter first against open air instead of getting pushed outward by inner walls, dramatically improving dimensional accuracy on tight print-in-place gears and bearing seats. |
| `precise_outer_wall` | `0` | **`1`** | Improves outer-wall dimensional fidelity on circular bores and gear profiles. |
| `seam_slope_type` | `none` / default | **`none`** | Prevents scarf/slope seam artifacts from smearing into narrow gear clearances. |
| `enable_arc_fitting` | `0` | **`1`** | Emits smooth `G2`/`G3` arcs for circular bores, bearing seats, and filleted lobes, giving smoother motion on the delta kinematics. |
| `reduce_infill_retraction` | `1` (disabled to `0` in an early test) | **`1`** | **Critical `0.3 mm` nozzle anti-clog fix:** Setting this to `0` forced hundreds of rapid micro-retractions across tiny gear-tooth islands on solid top layers, pulling heat up into the cold break (scorching PLA brown and causing a foamy, sandpaper-rough partial clog on the top `~1 mm`). Keeping `1` eliminates unnecessary internal retractions. |
| `reduce_crossing_wall` | `0` | **`1`** | Routes travel moves inside parts instead of crossing gear teeth, eliminating wispy stringing between meshing gears. |
| `wall_loops` | `6` | **`3`** (Normal) / **`4`** (Strong) | `6` walls choked narrow gear teeth; `3` walls (`0.9 mm`) is ideal for normal prototypes and `4` walls (`1.2 mm`) gives high structural strength without excessive micro-extrusion inside teeth. |
| `top_shell_layers` / `bottom_shell_layers` | `7` / `4` | **`5` / `4`** (Normal), **`5` / `5`** (Strong) | Solid, clean caps without over-cooking filament on prolonged tiny top-layer islands. |
| `sparse_infill_pattern` / `sparse_infill_density` | `grid` / `15%` | **`gyroid`** / **`15%`** (Normal) or **`30%`** (Strong) | Grid infill crosses over itself on every layer (causing nozzle collisions on a delta); gyroid never self-intersects and provides isotropic strength. |
| `infill_wall_overlap` | `25%` | **`15%`** | Reduces over-extrusion bumps telegraphing through outer walls and gear flanks. |
| `top_surface_pattern` | `monotonic` | **`monotonicline`** | Lays down uniform, glossy top passes with minimal ridge artifacts. |
| `top_surface_speed` | `30` mm/s | **`20`** mm/s | Slower final top pass produces a noticeably smoother, glossier finish (note: **do not** enable Ironing on a `0.3 mm` nozzle — ultra-low ironing flow causes heat-creep clogs). |
| `top_surface_line_width` | `0.30` mm | **`0.28`** mm | Slightly tighter top-layer spacing eliminates micro-grooves between lines. |
| `initial_layer_print_height` / `initial_layer_line_width` | `0.3` / `0.3` mm | **`0.2`** / **`0.36`** mm | Better first-layer squish and adhesion on the IVI bed without over-squashing gear clearances. |
| `elefant_foot_compensation` | `0` | **`0.15`** mm | Prevents first-layer flare from fusing print-in-place gears together at `Z = 0` (works together with the CAD `0.35 mm` bottom chamfer). |
| `travel_speed` | `80` mm/s | **`150`** mm/s | Faster non-print moves cut oozing and stringing across gear gaps. |

### 2. Machine Settings (`IVI 3D 0.3 nozzle - Tuned.json`)

| Parameter | Stock Default | Tuned Value | Why We Changed It |
| --- | --- | --- | --- |
| `machine_end_gcode` retraction + wipe | Small/no retraction | **`G1 E-6.5 F2400` + `G1 X3 Y3 Z0.4 F6000`** | Immediately kills nozzle ooze pressure at the end of a print and shears the strand cleanly so the hotend does not leave a blob or string on the finished top surface. |
| `machine_start_gcode` re-prime | None | **`G1 E7.0 F120`** | Re-primes the `6.5 mm` end-of-print retraction plus `0.5 mm` of pressure at a gentle, silent **`2 mm/s`** (`F120`), which is safe for the `0.3 mm` nozzle (faster priming caused extruder skipping/clicking) and ensures the intro line starts cleanly every print. |
| `retraction_length` / `retraction_speed` | Stock | **`4.5` mm @ `35` mm/s** (`5.0` mm @ `40` mm/s in PLA filament override) | Tuned to stop stringing on the IVI's extruder path without grinding filament. |
| `z_hop` / `z_hop_types` | Stock | **`0.3` mm / `Slope Lift`** | Prevents the nozzle from clipping fine gear teeth during travel moves while shearing strands cleanly on lift. |

### 3. Filament Settings (`IVI3D Generic PLA - 220C.json`)

| Parameter | Stock Default | Tuned Value | Why We Changed It |
| --- | --- | --- | --- |
| `nozzle_temperature` | `200–210 °C` | **`220 °C`** | Gives strong inter-layer bonding and smooth flow through the narrow `0.3 mm` nozzle orifice. |
| `filament_flow_ratio` | `1.00` | **`0.985`** | Slight flow trim prevents over-extrusion from welding tight `0.001–0.002` print-in-place gear teeth together and keeps top surfaces smooth. |
| `filament_wipe` / `filament_wipe_distance` | Off | **`1` / `1.5` mm** | Wipes residual ooze onto the inner wall before travel moves. |

---

## Installation

Run the installer script (works on both macOS and Linux). It:
1. Removes obsolete process profiles (`Normal Quality - Copy` / `Normal Quality - Tuned`) so only **`0.30mm Normal @IVI 3D`** and **`0.30mm Strong @IVI 3D`** appear.
2. Installs the canonical filament, process, and machine profiles.
3. Syncs `machine_start_gcode` / `machine_end_gcode` into both your non-cached user profile (`IVI 3D 0.3 nozzle - Copy.json`) and the `system/IVI3D` profile.
4. Bumps the `system/IVI3D.json` bundle version and deletes any stale `system/IVI3D.opc` cache so OrcaSlicer never serves old G-code.

```sh
./install.sh
```

Then restart OrcaSlicer (if open), open **Printer Settings → Connection**, and
verify **Hostname / IP** points to your `ivi-bridge` address (e.g.
`127.0.0.1:5000` locally, or `<your-bridge-host>:5000` over LAN/Tailscale).
