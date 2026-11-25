package main

import "testing"

func TestParseSerialLines(t *testing.T) {
	sc := NewSerialController("", 115200, true)

	sc.handleLine("FIRMWARE_NAME:Marlin V1.3.0-alpha (Jan 12 2021 14:49:53) SOURCE_CODE_URL:https://github.com/MarlinFirmware/Marlin PROTOCOL_VERSION:1.0")
	sc.handleLine("ok T:219.85 /220.00 B:60.10 /60.00 @:64 B@:127")
	sc.handleLine("X:12.50 Y:-4.25 Z:105.00 E:33.10 Count A:100 B:200 C:300")
	sc.handleLine("Homed: YES")
	sc.handleLine("Working Status: IDLE")

	st := sc.Snapshot()
	if st.Firmware != "Marlin V1.3.0-alpha (Jan 12 2021 14:49:53)" {
		t.Fatalf("unexpected firmware: %q", st.Firmware)
	}
	if st.NozzleActual != 219.85 || st.NozzleTarget != 220.0 {
		t.Fatalf("unexpected nozzle temps: %+v", st)
	}
	if st.BedActual != 60.10 || st.BedTarget != 60.0 {
		t.Fatalf("unexpected bed temps: %+v", st)
	}
	if st.NozzlePower != 64 || st.BedPower != 127 {
		t.Fatalf("unexpected heater power: %d / %d", st.NozzlePower, st.BedPower)
	}
	if st.X != 12.50 || st.Y != -4.25 || st.Z != 105.00 || st.E != 33.10 {
		t.Fatalf("unexpected position: X=%.2f Y=%.2f Z=%.2f E=%.2f", st.X, st.Y, st.Z, st.E)
	}
	if !st.Homed || st.WorkingStatus != "IDLE" {
		t.Fatalf("unexpected homed/status: homed=%v status=%q", st.Homed, st.WorkingStatus)
	}
}
