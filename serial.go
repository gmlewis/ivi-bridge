package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxConsoleLines = 200
	maxNozzleTemp   = 275
	maxBedTemp      = 110
)

type ConsoleLine struct {
	Timestamp time.Time `json:"timestamp"`
	Dir       string    `json:"dir"` // "tx", "rx", "sys"
	Text      string    `json:"text"`
}

type SerialState struct {
	Enabled       bool          `json:"enabled"`
	Connected     bool          `json:"connected"`
	PortSpec      string        `json:"port_spec"`
	Port          string        `json:"port"`
	Baud          int           `json:"baud"`
	Firmware      string        `json:"firmware,omitempty"`
	WorkingStatus string        `json:"working_status,omitempty"`
	Homed         bool          `json:"homed"`
	AtHome        bool          `json:"at_home"`
	NozzleActual  float64       `json:"nozzle_actual"`
	NozzleTarget  float64       `json:"nozzle_target"`
	NozzlePower   int           `json:"nozzle_power"`
	BedActual     float64       `json:"bed_actual"`
	BedTarget     float64       `json:"bed_target"`
	BedPower      int           `json:"bed_power"`
	FanPercent    int           `json:"fan_percent"`
	X             float64       `json:"x"`
	Y             float64       `json:"y"`
	Z             float64       `json:"z"`
	E             float64       `json:"e"`
	LastUpdate    time.Time     `json:"last_update,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	Console       []ConsoleLine `json:"console"`
}

type SerialController struct {
	portSpec string
	baud     int
	dryRun   bool

	stopCh chan struct{}
	once   sync.Once

	cmdMu sync.Mutex // serializes G-code commands waiting for "ok"

	mu       sync.Mutex
	fd       int
	state    SerialState
	homeZ    float64
	hasHomeZ bool
	waiterCh chan string // receives lines while a command is waiting for "ok"
}

func NewSerialController(portSpec string, baud int, dryRun bool) *SerialController {
	if baud <= 0 {
		baud = 115200
	}
	spec := strings.TrimSpace(portSpec)
	enabled := spec != "" && !strings.EqualFold(spec, "off") && !strings.EqualFold(spec, "none")
	sc := &SerialController{
		portSpec: spec,
		baud:     baud,
		dryRun:   dryRun,
		stopCh:   make(chan struct{}),
		fd:       -1,
		state: SerialState{
			Enabled:  enabled,
			PortSpec: spec,
			Baud:     baud,
			Console:  make([]ConsoleLine, 0, 64),
		},
	}
	if enabled {
		go sc.runLoop()
	}
	return sc
}

func (sc *SerialController) Close() {
	sc.once.Do(func() {
		close(sc.stopCh)
		sc.closePort()
	})
}

func (sc *SerialController) Snapshot() SerialState {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	cp := sc.state
	cp.Console = make([]ConsoleLine, len(sc.state.Console))
	copy(cp.Console, sc.state.Console)
	return cp
}

func (sc *SerialController) ClearConsole() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.state.Console = sc.state.Console[:0]
}

func (sc *SerialController) appendConsoleLocked(dir, text string) {
	line := ConsoleLine{
		Timestamp: time.Now(),
		Dir:       dir,
		Text:      text,
	}
	sc.state.Console = append(sc.state.Console, line)
	if len(sc.state.Console) > maxConsoleLines {
		sc.state.Console = sc.state.Console[len(sc.state.Console)-maxConsoleLines:]
	}
}

func (sc *SerialController) resolvePort() (string, error) {
	if !strings.EqualFold(sc.portSpec, "auto") {
		return sc.portSpec, nil
	}
	ports := detectSerialPorts()
	if len(ports) == 0 {
		return "", errors.New("no USB serial port detected")
	}
	return ports[0], nil
}

func (sc *SerialController) closePort() {
	sc.mu.Lock()
	fd := sc.fd
	sc.fd = -1
	sc.state.Connected = false
	sc.mu.Unlock()
	if fd >= 0 {
		_ = syscall.Close(fd)
	}
}

func (sc *SerialController) runLoop() {
	if sc.dryRun {
		sc.mu.Lock()
		sc.state.Connected = true
		sc.state.Port = "dry-run"
		sc.state.Firmware = "Marlin (dry-run)"
		sc.state.WorkingStatus = "IDLE"
		sc.state.NozzleActual = 25.0
		sc.state.BedActual = 24.0
		sc.state.Z = 234.0
		sc.state.Homed = true
		sc.appendConsoleLocked("sys", "Serial connected (dry-run)")
		sc.mu.Unlock()
		return
	}

	for {
		select {
		case <-sc.stopCh:
			return
		default:
		}

		port, err := sc.resolvePort()
		if err != nil {
			sc.mu.Lock()
			sc.state.Connected = false
			sc.state.LastError = err.Error()
			sc.mu.Unlock()
			select {
			case <-sc.stopCh:
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		fd, err := openSerialPort(port, sc.baud)
		if err != nil {
			sc.mu.Lock()
			sc.state.Connected = false
			sc.state.Port = port
			sc.state.LastError = err.Error()
			sc.mu.Unlock()
			select {
			case <-sc.stopCh:
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		log.Printf("[serial] connected to %s @ %d baud (passive telemetry mode)", port, sc.baud)
		sc.mu.Lock()
		sc.fd = fd
		sc.state.Connected = true
		sc.state.Port = port
		sc.state.LastError = ""
		sc.appendConsoleLocked("sys", fmt.Sprintf("Connected to %s @ %d (passive)", port, sc.baud))
		sc.mu.Unlock()

		// Do NOT transmit M115/M105/M114 automatically on connect: the IVI
		// firmware already broadcasts temps, position, and Working Status
		// unsolicited every ~5s, and injecting serial traffic during a
		// touchscreen print (WORKING_SCREEN) causes a firmware error.
		sc.readLoop(fd)
		sc.closePort()

		select {
		case <-sc.stopCh:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (sc *SerialController) readLoop(fd int) {
	buf := make([]byte, 1024)
	var partial strings.Builder

	for {
		select {
		case <-sc.stopCh:
			return
		default:
		}

		n, err := syscall.Read(fd, buf)
		if err != nil {
			if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
				continue
			}
			log.Printf("[serial] read error: %v", err)
			sc.mu.Lock()
			sc.state.LastError = err.Error()
			sc.appendConsoleLocked("sys", "Disconnected: "+err.Error())
			sc.mu.Unlock()
			return
		}
		if n == 0 {
			continue
		}

		partial.Write(buf[:n])
		text := partial.String()
		for {
			idx := strings.IndexByte(text, '\n')
			if idx < 0 {
				break
			}
			line := strings.TrimSpace(strings.TrimSuffix(text[:idx], "\r"))
			text = text[idx+1:]
			if line != "" {
				sc.handleLine(line)
			}
		}
		partial.Reset()
		partial.WriteString(text)
	}
}

var (
	reNozzle   = regexp.MustCompile(`T:([0-9.]+)\s*/\s*([0-9.]+)`)
	reBed      = regexp.MustCompile(`B:([0-9.]+)\s*/\s*([0-9.]+)`)
	reNozPow   = regexp.MustCompile(`@:(\d+)`)
	reBedPow   = regexp.MustCompile(`B@:(\d+)`)
	rePosition = regexp.MustCompile(`X:(-?[0-9.]+)\s+Y:(-?[0-9.]+)\s+Z:(-?[0-9.]+)\s+E:(-?[0-9.]+)`)
)

func (sc *SerialController) handleLine(line string) {
	sc.mu.Lock()
	sc.parseLineLocked(line)
	// Avoid flooding the console with periodic M105/M114 responses unless a user command is active.
	waiter := sc.waiterCh
	isQuietPoll := strings.HasPrefix(line, "ok T:") || strings.HasPrefix(line, "T:")
	if !isQuietPoll {
		sc.appendConsoleLocked("rx", line)
	}
	sc.mu.Unlock()

	if waiter != nil {
		select {
		case waiter <- line:
		default:
		}
	}
}

func (sc *SerialController) parseLineLocked(line string) {
	now := time.Now()
	if m := reNozzle.FindStringSubmatch(line); len(m) == 3 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			sc.state.NozzleActual = v
			sc.state.LastUpdate = now
		}
		if v, err := strconv.ParseFloat(m[2], 64); err == nil {
			sc.state.NozzleTarget = v
		}
	}
	if m := reBed.FindStringSubmatch(line); len(m) == 3 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			sc.state.BedActual = v
			sc.state.LastUpdate = now
		}
		if v, err := strconv.ParseFloat(m[2], 64); err == nil {
			sc.state.BedTarget = v
		}
	}
	if m := reNozPow.FindStringSubmatch(line); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			sc.state.NozzlePower = v
		}
	}
	if m := reBedPow.FindStringSubmatch(line); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			sc.state.BedPower = v
		}
	}
	if m := rePosition.FindStringSubmatch(line); len(m) == 5 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			sc.state.X = v
		}
		if v, err := strconv.ParseFloat(m[2], 64); err == nil {
			sc.state.Y = v
		}
		if v, err := strconv.ParseFloat(m[3], 64); err == nil {
			sc.state.Z = v
		}
		if v, err := strconv.ParseFloat(m[4], 64); err == nil {
			sc.state.E = v
		}
		sc.updateAtHomeLocked()
		sc.state.LastUpdate = now
	}
	if strings.HasPrefix(line, "FIRMWARE_NAME:") {
		// Extract firmware name up to SOURCE_CODE_URL if present.
		fw := strings.TrimPrefix(line, "FIRMWARE_NAME:")
		if idx := strings.Index(fw, " SOURCE_CODE_URL:"); idx > 0 {
			fw = fw[:idx]
		}
		sc.state.Firmware = strings.TrimSpace(fw)
	}
	if strings.HasPrefix(line, "Homed:") {
		val := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "Homed:")))
		sc.state.Homed = val == "YES" || val == "1" || val == "TRUE"
		if sc.state.Homed && !sc.hasHomeZ && absFloat(sc.state.X) < 0.05 && absFloat(sc.state.Y) < 0.05 && sc.state.Z > 100 {
			sc.homeZ = sc.state.Z
			sc.hasHomeZ = true
		}
		sc.updateAtHomeLocked()
	}
	if strings.HasPrefix(line, "Working Status:") {
		sc.state.WorkingStatus = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "Working Status:")))
	}
}

// SendGCode sends a single G-code line and waits for Marlin's "ok" response.
func (sc *SerialController) SendGCode(ctx context.Context, gcode string, timeout time.Duration) ([]string, error) {
	return sc.sendInternal(ctx, gcode, timeout, true)
}

func (sc *SerialController) sendInternal(ctx context.Context, gcode string, timeout time.Duration, logTx bool) ([]string, error) {
	line := strings.TrimSpace(gcode)
	if line == "" {
		return nil, errors.New("empty gcode")
	}
	if !sc.state.Enabled {
		return nil, errors.New("serial control disabled (start ivi-bridge with --serial auto)")
	}
	if sc.dryRun {
		sc.mu.Lock()
		if logTx {
			sc.appendConsoleLocked("tx", line)
			sc.appendConsoleLocked("rx", "ok (dry-run)")
		}
		sc.mu.Unlock()
		return []string{"ok (dry-run)"}, nil
	}

	sc.cmdMu.Lock()
	defer sc.cmdMu.Unlock()

	sc.mu.Lock()
	fd := sc.fd
	ws := sc.state.WorkingStatus
	if fd < 0 || !sc.state.Connected {
		sc.mu.Unlock()
		return nil, errors.New("serial port not connected")
	}
	// Protect active touchscreen prints: never inject G-code over USB serial
	// while the printer reports a non-IDLE Working Status (e.g. WORKING_SCREEN).
	if ws != "" && ws != "IDLE" {
		sc.mu.Unlock()
		return nil, fmt.Errorf("serial commands blocked while printer is busy (Working Status: %s)", ws)
	}
	ch := make(chan string, 64)
	sc.waiterCh = ch
	if logTx {
		sc.appendConsoleLocked("tx", line)
	}
	sc.mu.Unlock()

	defer func() {
		sc.mu.Lock()
		if sc.waiterCh == ch {
			sc.waiterCh = nil
		}
		sc.mu.Unlock()
	}()

	payload := []byte(line + "\n")
	if _, err := syscall.Write(fd, payload); err != nil {
		sc.closePort()
		return nil, fmt.Errorf("serial write: %w", err)
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var responses []string
	for {
		select {
		case <-ctx.Done():
			return responses, ctx.Err()
		case <-timer.C:
			return responses, fmt.Errorf("timeout waiting for ok after %q", line)
		case resp := <-ch:
			responses = append(responses, resp)
			low := strings.ToLower(strings.TrimSpace(resp))
			if low == "ok" || strings.HasPrefix(low, "ok ") {
				return responses, nil
			}
			if strings.HasPrefix(low, "error:") {
				return responses, errors.New(resp)
			}
		}
	}
}

// EmergencyStop sends M112 immediately without waiting for the command queue.
func (sc *SerialController) EmergencyStop() error {
	if !sc.state.Enabled {
		return errors.New("serial control disabled")
	}
	if sc.dryRun {
		sc.mu.Lock()
		sc.appendConsoleLocked("tx", "M112")
		sc.appendConsoleLocked("sys", "Emergency stop sent (dry-run)")
		sc.mu.Unlock()
		return nil
	}
	sc.mu.Lock()
	fd := sc.fd
	sc.appendConsoleLocked("tx", "M112")
	sc.mu.Unlock()
	if fd < 0 {
		return errors.New("serial port not connected")
	}
	_, err := syscall.Write(fd, []byte("M112\n"))
	return err
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (sc *SerialController) updateAtHomeLocked() {
	if !sc.state.Homed {
		sc.state.AtHome = false
		return
	}
	if absFloat(sc.state.X) > 0.05 || absFloat(sc.state.Y) > 0.05 {
		sc.state.AtHome = false
		return
	}
	if sc.hasHomeZ {
		sc.state.AtHome = absFloat(sc.state.Z-sc.homeZ) <= 0.1
		return
	}
	sc.state.AtHome = sc.state.Z > 200
}

func (sc *SerialController) MarkHomed() {
	sc.mu.Lock()
	sc.state.Homed = true
	sc.homeZ = sc.state.Z
	sc.hasHomeZ = true
	sc.state.AtHome = true
	sc.mu.Unlock()
}

func (sc *SerialController) MarkMoved() {
	sc.mu.Lock()
	sc.state.AtHome = false
	sc.mu.Unlock()
}

func (sc *SerialController) MarkMotorsOff() {
	sc.mu.Lock()
	sc.state.Homed = false
	sc.state.AtHome = false
	sc.mu.Unlock()
}

func (sc *SerialController) SetFan(ctx context.Context, percent int) ([]string, error) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	cmd := "M107"
	if percent > 0 {
		pwm := (percent * 255) / 100
		cmd = fmt.Sprintf("M106 S%d", pwm)
	}
	res, err := sc.SendGCode(ctx, cmd, 5*time.Second)
	if err == nil {
		sc.mu.Lock()
		sc.state.FanPercent = percent
		sc.mu.Unlock()
	}
	return res, err
}
