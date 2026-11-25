package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

func (s *BridgeServer) requireSerial(w http.ResponseWriter, r *http.Request) bool {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return false
	}
	if s.Serial == nil || !s.Serial.Snapshot().Enabled {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "USB serial control is disabled (start ivi-bridge with --serial auto)"})
		return false
	}
	return true
}

func (s *BridgeServer) handleSerialGCode(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	var body struct {
		GCode string   `json:"gcode"`
		Lines []string `json:"lines"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	var lines []string
	if strings.TrimSpace(body.GCode) != "" {
		for _, raw := range strings.Split(body.GCode, "\n") {
			if trimmed := strings.TrimSpace(raw); trimmed != "" {
				lines = append(lines, trimmed)
			}
		}
	}
	for _, raw := range body.Lines {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no gcode lines provided"})
		return
	}
	allResponses := make([]string, 0, len(lines))
	for _, line := range lines {
		timeout := 15 * time.Second
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "G28") || strings.HasPrefix(upper, "G29") || strings.HasPrefix(upper, "M109") || strings.HasPrefix(upper, "M190") {
			timeout = 90 * time.Second
		}
		res, err := s.Serial.SendGCode(r.Context(), line, timeout)
		allResponses = append(allResponses, res...)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "responses": allResponses})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"responses": allResponses,
		"serial":    s.Serial.Snapshot(),
	})
}

func (s *BridgeServer) handleSerialJog(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	var body struct {
		Axis     string  `json:"axis"`
		Distance float64 `json:"distance"`
		Feedrate float64 `json:"feedrate"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	axis := strings.ToUpper(strings.TrimSpace(body.Axis))
	if axis != "X" && axis != "Y" && axis != "Z" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "axis must be X, Y, or Z"})
		return
	}
	if math.IsNaN(body.Distance) || math.IsInf(body.Distance, 0) || body.Distance == 0 || math.Abs(body.Distance) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "distance must be non-zero and within +/-200 mm"})
		return
	}
	feed := body.Feedrate
	if feed <= 0 {
		if axis == "Z" {
			feed = 1800
		} else {
			feed = 3000
		}
	}
	if feed > 12000 {
		feed = 12000
	}

	seq := []string{
		"G91",
		fmt.Sprintf("G1 %s%.3f F%.0f", axis, body.Distance, feed),
		"G90",
		"M114",
	}
	for _, cmd := range seq {
		if _, err := s.Serial.SendGCode(r.Context(), cmd, 15*time.Second); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}
	s.Serial.MarkMoved()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialHome(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	// Delta printers always home all three towers together with G28.
	if _, err := s.Serial.SendGCode(r.Context(), "G28", 60*time.Second); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Serial.SendGCode(r.Context(), "M114", 5*time.Second)
	s.Serial.MarkHomed()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialTemp(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	var body struct {
		Heater string  `json:"heater"` // "nozzle" or "bed"
		Target float64 `json:"target"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	heater := strings.ToLower(strings.TrimSpace(body.Heater))
	target := math.Round(body.Target)
	if math.IsNaN(target) || target < 0 {
		target = 0
	}
	var cmd string
	switch heater {
	case "nozzle", "tool0", "hotend":
		if target > maxNozzleTemp {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("nozzle target exceeds safety limit (%d C)", maxNozzleTemp)})
			return
		}
		cmd = fmt.Sprintf("M104 S%.0f", target)
	case "bed":
		if target > maxBedTemp {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("bed target exceeds safety limit (%d C)", maxBedTemp)})
			return
		}
		cmd = fmt.Sprintf("M140 S%.0f", target)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "heater must be 'nozzle' or 'bed'"})
		return
	}
	if _, err := s.Serial.SendGCode(r.Context(), cmd, 10*time.Second); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Serial.SendGCode(r.Context(), "M105", 5*time.Second)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialExtrude(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	var body struct {
		Amount float64 `json:"amount"` // +mm extrude, -mm retract
		Speed  float64 `json:"speed"`  // mm/min (feedrate F)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	if math.IsNaN(body.Amount) || body.Amount == 0 || math.Abs(body.Amount) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be non-zero and within +/-200 mm"})
		return
	}
	speed := body.Speed
	if speed <= 0 {
		speed = 180 // 3 mm/s default
	}
	if speed > 1800 {
		speed = 1800
	}
	seq := []string{
		"M83",
		fmt.Sprintf("G1 E%.2f F%.0f", body.Amount, speed),
		"M82",
		"M114",
	}
	for _, cmd := range seq {
		if _, err := s.Serial.SendGCode(r.Context(), cmd, 30*time.Second); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialFan(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	var body struct {
		Percent int `json:"percent"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	if _, err := s.Serial.SetFan(r.Context(), body.Percent); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialBabystep(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	var body struct {
		Delta float64 `json:"delta"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	if math.IsNaN(body.Delta) || body.Delta == 0 || math.Abs(body.Delta) > 0.5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "babystep delta must be non-zero and within +/-0.5 mm"})
		return
	}
	cmd := fmt.Sprintf("M290 Z%.3f", body.Delta)
	if _, err := s.Serial.SendGCode(r.Context(), cmd, 10*time.Second); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialMotorsOff(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	if _, err := s.Serial.SendGCode(r.Context(), "M84", 10*time.Second); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.Serial.MarkMotorsOff()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialEStop(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	if err := s.Serial.EmergencyStop(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}

func (s *BridgeServer) handleSerialConsoleClear(w http.ResponseWriter, r *http.Request) {
	if !s.requireSerial(w, r) {
		return
	}
	s.Serial.ClearConsole()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serial": s.Serial.Snapshot()})
}
