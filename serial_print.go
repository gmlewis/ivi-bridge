package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"syscall"
	"time"
)

// PrintJobStatus describes an active or most-recent USB-serial print job.
type PrintJobStatus struct {
	Active           bool      `json:"active"`
	State            string    `json:"state"` // Printing, Paused, Cancelling, Completed, Cancelled, Error
	FileName         string    `json:"file_name"`
	TotalLines       int       `json:"total_lines"`
	SentLines        int       `json:"sent_lines"`
	Completion       float64   `json:"completion"`
	StartedAt        time.Time `json:"started_at"`
	ElapsedSec       int       `json:"elapsed_sec"`
	EstimatedLeftSec int       `json:"estimated_left_sec"`
	LastError        string    `json:"last_error,omitempty"`
}

// preparePrintLines strips comments, blank lines, and stray M73 lines so only
// clean, executable G-code commands are streamed over USB serial to Marlin.
func preparePrintLines(raw []byte) []string {
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	rawLines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(rawLines))
	for _, ln := range rawLines {
		if idx := strings.IndexByte(ln, ';'); idx >= 0 {
			ln = ln[:idx]
		}
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if reM73.MatchString(ln) {
			continue
		}
		out = append(out, ln)
	}
	return out
}

// IsPrinting reports whether a USB-serial print job is currently active
// (printing or paused).
func (sc *SerialController) IsPrinting() bool {
	if sc == nil {
		return false
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.printActive
}

// PrintJobSnapshot returns a copy of the active or most-recent USB print job
// status, or nil if no USB print has run since startup.
func (sc *SerialController) PrintJobSnapshot() *PrintJobStatus {
	if sc == nil {
		return nil
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.state.PrintJob == nil {
		return nil
	}
	cp := *sc.state.PrintJob
	if cp.Active && !cp.StartedAt.IsZero() {
		cp.ElapsedSec = int(time.Since(cp.StartedAt).Seconds())
		if cp.SentLines > 0 && cp.TotalLines > cp.SentLines {
			rate := float64(cp.ElapsedSec) / float64(cp.SentLines)
			cp.EstimatedLeftSec = int(rate * float64(cp.TotalLines-cp.SentLines))
		}
	}
	return &cp
}

// StartPrint begins streaming a G-code file directly to Marlin over USB serial.
// It refuses to start if USB serial is disabled/disconnected, another USB print
// is active, or the printer reports a non-IDLE Working Status (e.g. a
// touchscreen print is running).
func (sc *SerialController) StartPrint(filename string, gcode []byte) error {
	if sc == nil || !sc.state.Enabled {
		return errors.New("USB serial control is disabled (start ivi-bridge with --serial auto)")
	}
	lines := preparePrintLines(gcode)
	if len(lines) == 0 {
		return errors.New("gcode file contains no executable lines")
	}

	sc.mu.Lock()
	if !sc.state.Connected {
		sc.mu.Unlock()
		return errors.New("USB serial port is not connected to the printer")
	}
	if sc.printActive {
		activeName := ""
		if sc.state.PrintJob != nil {
			activeName = sc.state.PrintJob.FileName
		}
		sc.mu.Unlock()
		return fmt.Errorf("a USB serial print is already active (%s)", activeName)
	}
	ws := sc.state.WorkingStatus
	if ws != "" && ws != "IDLE" {
		sc.mu.Unlock()
		return fmt.Errorf("printer is currently busy (Working Status: %s)", ws)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sc.printActive = true
	sc.printPaused = false
	sc.printCancel = cancel
	sc.printWakeCh = make(chan struct{}, 1)
	sc.printDoneCh = make(chan struct{})
	sc.state.PrintJob = &PrintJobStatus{
		Active:     true,
		State:      "Printing",
		FileName:   filename,
		TotalLines: len(lines),
		StartedAt:  time.Now(),
	}
	sc.appendConsoleLocked("sys", fmt.Sprintf("USB print started: %s (%d lines)", filename, len(lines)))
	sc.mu.Unlock()

	log.Printf("[serial-print] starting %q (%d executable lines)", filename, len(lines))
	go sc.runPrintJob(ctx, filename, lines)
	return nil
}

// PausePrint pauses an active USB-serial print and parks the nozzle safely.
func (sc *SerialController) PausePrint() error {
	if sc == nil {
		return errors.New("serial controller unavailable")
	}
	sc.mu.Lock()
	if !sc.printActive {
		sc.mu.Unlock()
		return errors.New("no active USB print to pause")
	}
	if sc.printPaused {
		sc.mu.Unlock()
		return nil
	}
	sc.printPaused = true
	if sc.state.PrintJob != nil {
		sc.state.PrintJob.State = "Paused"
	}
	sc.appendConsoleLocked("sys", "USB print paused")
	sc.mu.Unlock()
	log.Printf("[serial-print] pause requested")
	return nil
}

// ResumePrint resumes a paused USB-serial print.
func (sc *SerialController) ResumePrint() error {
	if sc == nil {
		return errors.New("serial controller unavailable")
	}
	sc.mu.Lock()
	if !sc.printActive {
		sc.mu.Unlock()
		return errors.New("no active USB print to resume")
	}
	if !sc.printPaused {
		sc.mu.Unlock()
		return nil
	}
	sc.printPaused = false
	if sc.state.PrintJob != nil {
		sc.state.PrintJob.State = "Printing"
	}
	wake := sc.printWakeCh
	sc.appendConsoleLocked("sys", "USB print resumed")
	sc.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	log.Printf("[serial-print] resume requested")
	return nil
}

// CancelPrint stops an active USB-serial print, turns off heaters, retracts,
// lifts Z, and homes cleanly.
func (sc *SerialController) CancelPrint() error {
	if sc == nil {
		return errors.New("serial controller unavailable")
	}
	sc.mu.Lock()
	if !sc.printActive {
		sc.mu.Unlock()
		return errors.New("no active USB print to cancel")
	}
	cancel := sc.printCancel
	done := sc.printDoneCh
	wake := sc.printWakeCh
	if sc.state.PrintJob != nil {
		sc.state.PrintJob.State = "Cancelling"
	}
	sc.appendConsoleLocked("sys", "USB print cancel requested")
	sc.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
	log.Printf("[serial-print] cancelled")
	return nil
}

func (sc *SerialController) runPrintJob(ctx context.Context, filename string, lines []string) {
	defer func() {
		sc.mu.Lock()
		done := sc.printDoneCh
		sc.printActive = false
		sc.printPaused = false
		sc.printCancel = nil
		if sc.state.PrintJob != nil {
			sc.state.PrintJob.Active = false
			if !sc.state.PrintJob.StartedAt.IsZero() {
				sc.state.PrintJob.ElapsedSec = int(time.Since(sc.state.PrintJob.StartedAt).Seconds())
			}
			sc.state.PrintJob.EstimatedLeftSec = 0
		}
		sc.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()

	parked := false
	for i, line := range lines {
		// Handle pause loop before sending next line.
		for {
			sc.mu.Lock()
			paused := sc.printPaused
			wake := sc.printWakeCh
			sc.mu.Unlock()
			if !paused {
				break
			}
			if !parked {
				parked = true
				parkCtx, parkCancel := context.WithTimeout(context.Background(), 20*time.Second)
				for _, cmd := range []string{"G91", "G1 E-4.0 F2400", "G1 Z10 F3000", "G90"} {
					_, _ = sc.sendPrintLine(parkCtx, cmd)
				}
				parkCancel()
			}
			select {
			case <-ctx.Done():
				sc.finishCancelled(filename)
				return
			case <-wake:
			}
		}
		if parked {
			parked = false
			resCtx, resCancel := context.WithTimeout(context.Background(), 20*time.Second)
			for _, cmd := range []string{"G91", "G1 Z-10 F3000", "G1 E4.0 F1800", "G90"} {
				_, _ = sc.sendPrintLine(resCtx, cmd)
			}
			resCancel()
		}

		select {
		case <-ctx.Done():
			sc.finishCancelled(filename)
			return
		default:
		}

		if _, err := sc.sendPrintLine(ctx, line); err != nil {
			if ctx.Err() != nil {
				sc.finishCancelled(filename)
				return
			}
			sc.mu.Lock()
			if sc.state.PrintJob != nil {
				sc.state.PrintJob.State = "Error"
				sc.state.PrintJob.LastError = err.Error()
			}
			sc.appendConsoleLocked("sys", fmt.Sprintf("USB print error on line %d (%s): %v", i+1, line, err))
			sc.mu.Unlock()
			log.Printf("[serial-print] ERROR on %q line %d (%s): %v", filename, i+1, line, err)
			return
		}

		sent := i + 1
		sc.mu.Lock()
		if sc.state.PrintJob != nil {
			sc.state.PrintJob.SentLines = sent
			sc.state.PrintJob.Completion = float64(sent) * 100.0 / float64(len(lines))
			sc.state.PrintJob.ElapsedSec = int(time.Since(sc.state.PrintJob.StartedAt).Seconds())
		}
		sc.mu.Unlock()
	}

	sc.mu.Lock()
	if sc.state.PrintJob != nil {
		sc.state.PrintJob.State = "Completed"
		sc.state.PrintJob.Completion = 100.0
	}
	sc.appendConsoleLocked("sys", fmt.Sprintf("USB print completed: %s", filename))
	sc.mu.Unlock()
	log.Printf("[serial-print] completed %q (%d lines)", filename, len(lines))
}

func (sc *SerialController) finishCancelled(filename string) {
	sc.mu.Lock()
	if sc.state.PrintJob != nil {
		sc.state.PrintJob.State = "Cancelled"
	}
	sc.appendConsoleLocked("sys", fmt.Sprintf("USB print cancelled: %s (cooling down & homing)", filename))
	sc.mu.Unlock()

	if !sc.dryRun {
		stopCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		for _, cmd := range []string{
			"M104 S0",
			"M140 S0",
			"M106 S0",
			"G91",
			"G1 E-6.5 F2400",
			"G1 Z10 F3000",
			"G90",
			"G28",
			"M82",
		} {
			_, _ = sc.sendPrintLine(stopCtx, cmd)
		}
	}
}

// sendPrintLine transmits a single G-code line for the active USB print job
// and waits for Marlin's "ok", extending deadlines during heating (M109/M190)
// and busy processing.
func (sc *SerialController) sendPrintLine(ctx context.Context, line string) ([]string, error) {
	if sc.dryRun {
		return []string{"ok (dry-run)"}, nil
	}

	sc.cmdMu.Lock()
	defer sc.cmdMu.Unlock()

	sc.mu.Lock()
	fd := sc.fd
	if fd < 0 || !sc.state.Connected {
		sc.mu.Unlock()
		return nil, errors.New("serial port disconnected")
	}
	ch := make(chan string, 64)
	sc.waiterCh = ch
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

	timeout := 30 * time.Second
	upper := strings.ToUpper(line)
	switch {
	case strings.HasPrefix(upper, "M109") || strings.HasPrefix(upper, "M190"):
		timeout = 15 * time.Minute
	case strings.HasPrefix(upper, "G28") || strings.HasPrefix(upper, "G29") || strings.HasPrefix(upper, "M400") || strings.HasPrefix(upper, "G4"):
		timeout = 2 * time.Minute
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
			if strings.HasPrefix(low, "echo:busy:") || strings.HasPrefix(low, "t:") {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(timeout)
			}
			if strings.HasPrefix(low, "error:") || strings.HasPrefix(low, "!!") {
				return responses, fmt.Errorf("printer error: %s", resp)
			}
		}
	}
}
