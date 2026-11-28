#!/usr/bin/env bash
# install.sh — Install IVI 3D OrcaSlicer profiles cleanly on macOS or Linux,
# syncing start/end G-code into both user and system profiles, bumping the
# IVI3D system bundle version, and purging any stale OrcaSlicer .opc cache.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ "$(uname -s)" == "Darwin" ]]; then
  ORCA_DIR="$HOME/Library/Application Support/OrcaSlicer"
else
  ORCA_DIR="$HOME/.config/OrcaSlicer"
fi

USER_DIR="$ORCA_DIR/user/default"
SYS_DIR="$ORCA_DIR/system"

if [[ ! -d "$ORCA_DIR" ]]; then
  echo "Error: OrcaSlicer config directory not found at: $ORCA_DIR" >&2
  exit 1
fi

echo "Installing IVI 3D OrcaSlicer profiles into: $ORCA_DIR"

mkdir -p "$USER_DIR/machine" "$USER_DIR/filament" "$USER_DIR/process"
# Remove obsolete IVI 3D process profiles so only the canonical Normal & Strong
# profiles appear in OrcaSlicer.
rm -fv \
  "$USER_DIR/process/0.30mm Normal Quality @IVI 3D - Copy.json" \
  "$USER_DIR/process/0.30mm Normal Quality @IVI 3D - Copy.info" \
  "$USER_DIR/process/0.30mm Normal Quality @IVI 3D - Tuned.json" \
  "$USER_DIR/process/0.30mm Normal Quality @IVI 3D - Tuned.info"
cp -v "$SCRIPT_DIR/filament/"*.json "$USER_DIR/filament/"
cp -v "$SCRIPT_DIR/process/"*.json "$USER_DIR/process/"
cp -v "$SCRIPT_DIR/machine/"*.json "$USER_DIR/machine/"

# Sync machine_start_gcode and machine_end_gcode from the repo's canonical
# Tuned profile into user's "IVI 3D 0.3 nozzle - Copy.json" (never .opc-cached!)
# and into system/IVI3D/machine/IVI 3D 0.3 nozzle.json (if installed), then
# bump system/IVI3D.json version and remove any stale system/IVI3D.opc cache.
python3 - "$SCRIPT_DIR/machine/IVI 3D 0.3 nozzle - Tuned.json" "$USER_DIR/machine/IVI 3D 0.3 nozzle - Copy.json" "$SYS_DIR/IVI3D/machine/IVI 3D 0.3 nozzle.json" "$SYS_DIR/IVI3D.json" <<'PY'
import json
import os
import sys

src_path, user_copy_path, sys_machine_path, sys_manifest_path = sys.argv[1:5]

with open(src_path, "r", encoding="utf-8") as f:
    src = json.load(f)

start_gc = src["machine_start_gcode"]
end_gc = src["machine_end_gcode"]

for target in (user_copy_path, sys_machine_path):
    if not os.path.isfile(target):
        continue
    with open(target, "r", encoding="utf-8") as f:
        data = json.load(f)
    changed = (
        data.get("machine_start_gcode") != start_gc
        or data.get("machine_end_gcode") != end_gc
    )
    data["machine_start_gcode"] = start_gc
    data["machine_end_gcode"] = end_gc
    with open(target, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=4, ensure_ascii=True)
        f.write("\n")
    print(f"{'Updated' if changed else 'Verified'} G-code in: {target}")

if os.path.isfile(sys_manifest_path):
    with open(sys_manifest_path, "r", encoding="utf-8") as f:
        manifest = json.load(f)
    ver = manifest.get("version", "01.00.00.00")
    parts = ver.split(".")
    if len(parts) == 4 and parts[-1].isdigit():
        parts[-1] = f"{int(parts[-1]) + 1:02d}"
        manifest["version"] = ".".join(parts)
        with open(sys_manifest_path, "w", encoding="utf-8") as f:
            json.dump(manifest, f, indent=4, ensure_ascii=True)
            f.write("\n")
        print(f"Bumped {sys_manifest_path} version: {ver} -> {manifest['version']}")
PY

if [[ -f "$SYS_DIR/IVI3D.opc" ]]; then
  rm -fv "$SYS_DIR/IVI3D.opc"
fi

echo "Done! Restart OrcaSlicer if it is currently open."
