package main

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reFilenameDuration = regexp.MustCompile(`_(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?(?:_\d{6})?\.gcode$`)

// estimateDurationFromFilename parses OrcaSlicer duration tokens embedded in
// filenames such as `PGB-CARTRIDGE_PLA_7h42m.gcode` or `part_PLA_25m37s.gcode`.
func estimateDurationFromFilename(name string) float64 {
	m := reFilenameDuration.FindStringSubmatch(strings.ToLower(strings.TrimSpace(name)))
	if len(m) != 5 {
		return 0
	}
	var total float64
	if m[1] != "" {
		if v, err := strconv.Atoi(m[1]); err == nil {
			total += float64(v) * 86400
		}
	}
	if m[2] != "" {
		if v, err := strconv.Atoi(m[2]); err == nil {
			total += float64(v) * 3600
		}
	}
	if m[3] != "" {
		if v, err := strconv.Atoi(m[3]); err == nil {
			total += float64(v) * 60
		}
	}
	if m[4] != "" {
		if v, err := strconv.Atoi(m[4]); err == nil {
			total += float64(v)
		}
	}
	return total
}

func recordEstDurationSec(rec UploadRecord) float64 {
	if rec.Meta.EstTimeSec > 0 {
		return rec.Meta.EstTimeSec
	}
	return estimateDurationFromFilename(rec.SentName)
}

// ScreenPrintTiming returns when the current touchscreen print began and
// whether the IDLE -> WORKING_SCREEN transition was directly observed.
func (sc *SerialController) ScreenPrintTiming() (time.Time, bool) {
	if sc == nil {
		return time.Time{}, false
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.screenPrintStartedAt, sc.screenPrintObserved
}

// inferTouchscreenJob correlates live passive USB telemetry (WorkingStatus, Z,
// NozzleTarget, BedTarget) with upload history to identify which file is
// printing on the IVI touchscreen and compute live % completion + ETA.
func (s *BridgeServer) inferTouchscreenJob(ss SerialState) *PrintJobStatus {
	if !ss.Connected || ss.WorkingStatus == "" || ss.WorkingStatus == "IDLE" {
		s.mu.Lock()
		s.pinnedScreenFile = ""
		s.mu.Unlock()
		return nil
	}

	now := s.Now()
	hist := s.getHistory()
	s.mu.Lock()
	pinned := s.pinnedScreenFile
	s.mu.Unlock()

	var matched *UploadRecord
	if pinned != "" {
		for i := range hist {
			if hist[i].SentName == pinned || hist[i].OriginalName == pinned {
				matched = &hist[i]
				break
			}
		}
	}
	if matched == nil && len(hist) > 0 {
		// Prefer the most recent upload whose MaxZ and target temps are compatible
		// with live telemetry; fall back to the most recent upload.
		for i := range hist {
			rec := &hist[i]
			if rec.Meta.MaxZ > 0 && ss.Z > rec.Meta.MaxZ+1.5 && ss.Z < 200 {
				continue
			}
			if ss.BedTarget > 0 && rec.Meta.BedTemp != "" {
				if bt, err := strconv.ParseFloat(rec.Meta.BedTemp, 64); err == nil && math.Abs(bt-ss.BedTarget) > 15 {
					continue
				}
			}
			matched = rec
			break
		}
		if matched == nil {
			matched = &hist[0]
		}
	}

	startedAt, observed := s.Serial.ScreenPrintTiming()
	job := &PrintJobStatus{
		Active:   true,
		Source:   "touchscreen",
		State:    "Printing",
		CurrentZ: ss.Z,
	}
	if matched == nil {
		job.FileName = "Touchscreen Print (" + ss.WorkingStatus + ")"
		if !startedAt.IsZero() {
			job.StartedAt = startedAt
			job.ElapsedSec = int(now.Sub(startedAt).Seconds())
		}
		return job
	}

	job.FileName = matched.SentName
	job.TotalLines = matched.Meta.TotalLines
	job.MaxZ = matched.Meta.MaxZ

	estSec := recordEstDurationSec(*matched)
	if estSec > 0 {
		job.EstTotalSec = int(math.Round(estSec))
	}

	// Z-height progress (valid once printing on bed, Z < 200mm to exclude home Z=234mm).
	hasZ := matched.Meta.MaxZ > 0.5 && ss.Z >= 0 && ss.Z < 200
	zPct := 0.0
	if hasZ {
		zPct = (ss.Z / matched.Meta.MaxZ) * 100.0
		if zPct > 99.5 {
			zPct = 99.5
		}
		if zPct < 0 {
			zPct = 0
		}
	}

	// Determine best elapsed time:
	// 1. If we directly observed IDLE -> WORKING_SCREEN, use that exact start time.
	// 2. Otherwise (e.g. ivi-bridge restarted mid-print), if the file was uploaded
	//    recently (within 1.5x estSec), use upload timestamp as approximate start.
	effectiveStart := startedAt
	hasReliableStart := observed && !startedAt.IsZero()
	if !hasReliableStart && !matched.Timestamp.IsZero() {
		age := now.Sub(matched.Timestamp).Seconds()
		maxAge := 24 * 3600.0
		if estSec > 0 {
			maxAge = math.Max(estSec*1.5, 3600.0)
		}
		if age >= 0 && age <= maxAge {
			effectiveStart = matched.Timestamp
			hasReliableStart = true
		}
	}
	if !effectiveStart.IsZero() {
		job.StartedAt = effectiveStart
		job.ElapsedSec = int(math.Max(0, now.Sub(effectiveStart).Seconds()))
	}

	if estSec > 0 {
		tPct := 0.0
		if hasReliableStart {
			tPct = (float64(job.ElapsedSec) / estSec) * 100.0
			if tPct > 99.5 {
				tPct = 99.5
			}
		}
		switch {
		case hasReliableStart && hasZ && ss.Z > 0.3:
			// Blend OrcaSlicer's time estimate (65%) with live Z progress (35%)
			// so ETA tracks both geometry complexity and real-world speed.
			pct := 0.65*tPct + 0.35*zPct
			job.Completion = pct
			remByTime := math.Max(0, estSec-float64(job.ElapsedSec))
			remByZ := estSec * math.Max(0, 1.0-zPct/100.0)
			left := 0.65*remByTime + 0.35*remByZ
			job.EstimatedLeftSec = int(math.Round(left))
		case hasReliableStart:
			job.Completion = tPct
			job.EstimatedLeftSec = int(math.Round(math.Max(0, estSec-float64(job.ElapsedSec))))
		case hasZ:
			job.Completion = zPct
			left := estSec * math.Max(0, 1.0-zPct/100.0)
			job.EstimatedLeftSec = int(math.Round(left))
			job.ElapsedSec = int(math.Round(estSec * (zPct / 100.0)))
		}
		if job.EstimatedLeftSec > 0 {
			job.EstFinishAt = now.Add(time.Duration(job.EstimatedLeftSec) * time.Second)
		}
	} else if hasZ {
		job.Completion = zPct
		if job.ElapsedSec > 30 && zPct > 1.0 {
			left := float64(job.ElapsedSec) * (100.0 - zPct) / zPct
			job.EstimatedLeftSec = int(math.Round(left))
			job.EstFinishAt = now.Add(time.Duration(job.EstimatedLeftSec) * time.Second)
		}
	}

	return job
}

// handleScreenJobPin lets any dashboard override which uploaded file is
// currently running on the IVI touchscreen (in case an older file was started).
func (s *BridgeServer) handleScreenJobPin(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		FileName string `json:"file_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	s.mu.Lock()
	s.pinnedScreenFile = strings.TrimSpace(body.FileName)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "file_name": body.FileName})
}
