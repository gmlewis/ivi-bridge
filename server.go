package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	octoVersionServer = "1.9.3"
	octoVersionText   = "OctoPrint 1.9.3"
	maxUploadBytes    = 256 << 20 // 256 MiB
	maxHistoryEntries = 100
)

type UploadRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	OriginalName string    `json:"original_name"`
	SentName     string    `json:"sent_name"`
	Renamed      bool      `json:"renamed"`
	SizeBytes    int       `json:"size_bytes"`
	Meta         GCodeMeta `json:"meta"`
	Response     string    `json:"response"`
}

type BridgeServer struct {
	IVI           *IVIClient
	Serial        *SerialController
	APIKey        string
	AutoTransform bool
	UniqueNames   bool
	StateFile     string
	Now           func() time.Time

	mu        sync.Mutex
	history   []UploadRecord
	seenNames map[string]bool
}

func NewBridgeServer(ivi *IVIClient, apiKey string, autoTransform, uniqueNames bool, stateFile string) *BridgeServer {
	s := &BridgeServer{
		IVI:           ivi,
		APIKey:        apiKey,
		AutoTransform: autoTransform,
		UniqueNames:   uniqueNames,
		StateFile:     stateFile,
		Now:           time.Now,
		seenNames:     make(map[string]bool),
	}
	s.loadState()
	return s
}

func (s *BridgeServer) loadState() {
	if s.StateFile == "" {
		return
	}
	data, err := os.ReadFile(s.StateFile)
	if err != nil {
		return
	}
	var records []UploadRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return
	}
	s.history = records
	for _, r := range records {
		s.seenNames[strings.ToLower(r.SentName)] = true
	}
}

func (s *BridgeServer) saveStateLocked() {
	if s.StateFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.StateFile), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.history, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.StateFile, data, 0o644)
}

// chooseFilename sanitizes the filename for the IVI's kore HTTP server and
// appends a timestamp suffix if UniqueNames is enabled and the filename was
// already uploaded (mitigating the IVI's silent duplicate-filename quirk).
//
// The IVI firmware checks the file extension using the FIRST '.' in the
// filename (strchr instead of strrchr), so any extra '.' in the stem (such as
// "tol0.090") causes HTTP 412 "File type error". We replace interior dots in
// the stem with '_' so the only '.' precedes ".gcode".
func (s *BridgeServer) chooseFilename(orig string, now time.Time) (string, bool) {
	base := filepath.Base(strings.TrimSpace(orig))
	if base == "" || base == "." || base == "/" {
		base = "upload.gcode"
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if strings.Contains(stem, ".") {
		stem = strings.ReplaceAll(stem, ".", "_")
		base = stem + ext
	}
	if !s.UniqueNames {
		return base, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.seenNames[strings.ToLower(base)] {
		return base, false
	}
	stamp := now.Format("150405")
	candidate := fmt.Sprintf("%s_%s%s", stem, stamp, ext)
	for i := 2; s.seenNames[strings.ToLower(candidate)]; i++ {
		candidate = fmt.Sprintf("%s_%s_%d%s", stem, stamp, i, ext)
	}
	return candidate, true
}

func (s *BridgeServer) recordUpload(rec UploadRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seenNames[strings.ToLower(rec.SentName)] = true
	s.history = append([]UploadRecord{rec}, s.history...)
	if len(s.history) > maxHistoryEntries {
		s.history = s.history[:maxHistoryEntries]
	}
	s.saveStateLocked()
}

func (s *BridgeServer) getHistory() []UploadRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]UploadRecord, len(s.history))
	copy(cp, s.history)
	return cp
}

func (s *BridgeServer) authOK(r *http.Request) bool {
	if s.APIKey == "" {
		return true
	}
	return r.Header.Get("X-Api-Key") == s.APIKey
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *BridgeServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/files/local", s.handleFilesLocal)
	mux.HandleFunc("/api/files", s.handleFiles)
	mux.HandleFunc("/api/job", s.handleJob)
	mux.HandleFunc("/api/printer", s.handlePrinter)
	mux.HandleFunc("/api/connection", s.handleConnection)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/ivi/state", s.handleIVIState)
	mux.HandleFunc("/api/ivi/upload", s.handleIVIUpload)
	mux.HandleFunc("/api/ivi/settings", s.handleIVISettings)
	mux.HandleFunc("/api/ivi/command", s.handleIVICommand)
	mux.HandleFunc("/api/ivi/history/clear", s.handleIVIHistoryClear)
	mux.HandleFunc("/api/ivi/serial/gcode", s.handleSerialGCode)
	mux.HandleFunc("/api/ivi/serial/jog", s.handleSerialJog)
	mux.HandleFunc("/api/ivi/serial/home", s.handleSerialHome)
	mux.HandleFunc("/api/ivi/serial/temp", s.handleSerialTemp)
	mux.HandleFunc("/api/ivi/serial/extrude", s.handleSerialExtrude)
	mux.HandleFunc("/api/ivi/serial/fan", s.handleSerialFan)
	mux.HandleFunc("/api/ivi/serial/babystep", s.handleSerialBabystep)
	mux.HandleFunc("/api/ivi/serial/motors_off", s.handleSerialMotorsOff)
	mux.HandleFunc("/api/ivi/serial/estop", s.handleSerialEStop)
	mux.HandleFunc("/api/ivi/serial/console/clear", s.handleSerialConsoleClear)
	return mux
}

func (s *BridgeServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"api":    "0.1",
		"server": octoVersionServer,
		"text":   octoVersionText,
	})
}

func (s *BridgeServer) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// Expose recent bridge uploads so slicers see something useful.
	hist := s.getHistory()
	files := make([]map[string]any, 0, len(hist))
	for _, rec := range hist {
		files = append(files, map[string]any{
			"name":   rec.SentName,
			"path":   rec.SentName,
			"origin": "local",
			"size":   rec.SizeBytes,
			"date":   rec.Timestamp.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files": files,
		"free":  0,
	})
}

func (s *BridgeServer) handleFilesLocal(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method == http.MethodGet {
		s.handleFiles(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	rec, wantPrint, err := s.processUploadRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if wantPrint {
		code, detail, cmdErr := s.IVI.Command(r.Context(), "start_print", nil)
		if cmdErr != nil {
			if code == 0 {
				code = http.StatusConflict
			}
			writeJSON(w, code, map[string]any{
				"error":  "uploaded, but print NOT started",
				"detail": cmdErr.Error(),
				"raw":    detail,
				"file":   rec.SentName,
				"hint":   "The transformed file is on the printer; start it from the printer's Files screen.",
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"done": true,
		"files": map[string]any{
			"local": map[string]any{
				"name":   rec.SentName,
				"path":   rec.SentName,
				"origin": "local",
				"refs": map[string]string{
					"resource": "file",
					"download": "/downloads/files/local/" + rec.SentName,
				},
			},
		},
	})
}

func (s *BridgeServer) processUploadRequest(r *http.Request) (UploadRecord, bool, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return UploadRecord{}, false, fmt.Errorf("parse multipart form: %w", err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return UploadRecord{}, false, fmt.Errorf("missing 'file' field: %w", err)
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		return UploadRecord{}, false, fmt.Errorf("read upload: %w", err)
	}
	wantPrint := strings.EqualFold(strings.TrimSpace(r.FormValue("print")), "true")

	now := s.Now()
	payload := raw
	var meta GCodeMeta
	if s.AutoTransform {
		transformed, m, tErr := TransformGCode(raw, now)
		if tErr != nil {
			return UploadRecord{}, wantPrint, fmt.Errorf("transform gcode: %w", tErr)
		}
		payload = transformed
		meta = m
	}

	origName := header.Filename
	if origName == "" {
		origName = "upload.gcode"
	}
	sentName, renamed := s.chooseFilename(origName, now)

	respText, err := s.IVI.Upload(r.Context(), sentName, payload)
	if err != nil {
		return UploadRecord{}, wantPrint, err
	}

	rec := UploadRecord{
		Timestamp:    now,
		OriginalName: origName,
		SentName:     sentName,
		Renamed:      renamed,
		SizeBytes:    len(payload),
		Meta:         meta,
		Response:     respText,
	}
	s.recordUpload(rec)
	log.Printf("[bridge] uploaded %q as %q (transformed=%v, lines=%d/%d, nozzle=%sC, bed=%sC)",
		origName, sentName, meta.Transformed, meta.TotalLines, meta.ValidLines, meta.NozzleTemp, meta.BedTemp)
	return rec, wantPrint, nil
}

func (s *BridgeServer) handleJob(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method == http.MethodGet {
		st := s.IVI.Status(r.Context())
		lastFile := ""
		hist := s.getHistory()
		if len(hist) > 0 {
			lastFile = hist[0].SentName
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"state": st.OctoState,
			"job": map[string]any{
				"file": map[string]any{
					"name":   lastFile,
					"origin": "local",
				},
			},
			"progress": map[string]any{
				"completion":    nil,
				"printTime":     nil,
				"printTimeLeft": nil,
			},
		})
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
			return
		}
		mapping := map[string]string{
			"start":  "start_print",
			"cancel": "stop_print",
			"pause":  "pause_print",
			"resume": "resume_print",
		}
		ep, ok := mapping[body.Command]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported command: " + body.Command})
			return
		}
		code, _, err := s.IVI.Command(r.Context(), ep, nil)
		if err != nil {
			if code == 0 {
				code = http.StatusConflict
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func (s *BridgeServer) handlePrinter(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	st := s.IVI.Status(r.Context())
	nozzleActual, nozzleTarget := 0.0, 0.0
	bedActual, bedTarget := 0.0, 0.0
	if s.Serial != nil {
		ss := s.Serial.Snapshot()
		if ss.Connected {
			nozzleActual, nozzleTarget = ss.NozzleActual, ss.NozzleTarget
			bedActual, bedTarget = ss.BedActual, ss.BedTarget
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state": map[string]any{
			"text": st.OctoState,
			"flags": map[string]bool{
				"operational":   st.Reachable,
				"printing":      st.OctoState == "Printing",
				"paused":        st.OctoState == "Paused",
				"ready":         st.OctoState == "Operational",
				"error":         !st.Reachable,
				"closedOrError": !st.Reachable,
			},
		},
		"temperature": map[string]any{
			"tool0": map[string]float64{"actual": nozzleActual, "target": nozzleTarget},
			"bed":   map[string]float64{"actual": bedActual, "target": bedTarget},
		},
	})
}

func (s *BridgeServer) handleConnection(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	st := s.IVI.Status(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"current": map[string]any{
			"state":          st.OctoState,
			"port":           s.IVI.BaseURL,
			"baudrate":       nil,
			"printerProfile": "_default",
		},
		"options": map[string]any{
			"ports":     []string{s.IVI.BaseURL},
			"baudrates": []int{},
		},
	})
}

func (s *BridgeServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"appearance": map[string]string{"name": "IVI 3D Bridge"},
		"feature":    map[string]bool{},
	})
}

func (s *BridgeServer) handleIVIState(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	st := s.IVI.Status(ctx)
	s.mu.Lock()
	autoTransform := s.AutoTransform
	uniqueNames := s.UniqueNames
	s.mu.Unlock()
	var serialSnap SerialState
	if s.Serial != nil {
		serialSnap = s.Serial.Snapshot()
	} else {
		serialSnap.Console = []ConsoleLine{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"printer_url":    s.IVI.BaseURL,
		"dry_run":        s.IVI.DryRun,
		"auto_transform": autoTransform,
		"unique_names":   uniqueNames,
		"allow_start":    s.IVI.AllowStart,
		"status":         st,
		"serial":         serialSnap,
		"history":        s.getHistory(),
	})
}

func (s *BridgeServer) handleIVISettings(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		AutoTransform *bool `json:"auto_transform"`
		UniqueNames   *bool `json:"unique_names"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	s.mu.Lock()
	if body.AutoTransform != nil {
		s.AutoTransform = *body.AutoTransform
	}
	if body.UniqueNames != nil {
		s.UniqueNames = *body.UniqueNames
	}
	autoTransform := s.AutoTransform
	uniqueNames := s.UniqueNames
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{
		"auto_transform": autoTransform,
		"unique_names":   uniqueNames,
	})
}

func (s *BridgeServer) handleIVICommand(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		return
	}
	allowed := map[string]bool{
		"start_print":  true,
		"pause_print":  true,
		"resume_print": true,
		"stop_print":   true,
		"execute_code": true,
	}
	if !allowed[body.Endpoint] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported endpoint: " + body.Endpoint})
		return
	}
	var extra map[string][]string
	if body.Endpoint == "execute_code" {
		code := strings.TrimSpace(body.Code)
		if code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'code' for execute_code"})
			return
		}
		extra = map[string][]string{"code": {code}}
	}
	code, raw, err := s.IVI.Command(r.Context(), body.Endpoint, extra)
	if err != nil {
		if code == 0 {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]any{"error": err.Error(), "raw": raw})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "endpoint": body.Endpoint, "raw": raw})
}

func (s *BridgeServer) handleIVIHistoryClear(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.mu.Lock()
	s.history = nil
	s.seenNames = make(map[string]bool)
	s.saveStateLocked()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *BridgeServer) handleIVIUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	rec, _, err := s.processUploadRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *BridgeServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, dashboardHTML)
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>IVI 3D Bridge</title>
<style>
  :root { --bg:#121417; --card:#1b1f24; --border:#2b313a; --text:#e6edf3; --muted:#8b949e; --accent:#2f81f7; --ok:#3fb950; --warn:#d29922; --err:#f85149; }
  * { box-sizing:border-box; }
  body { margin:0; padding:20px; background:var(--bg); color:var(--text); font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; }
  .wrap { max-width:920px; margin:0 auto; }
  h1 { margin:0 0 4px; font-size:20px; }
  .sub { color:var(--muted); margin:0 0 18px; }
  .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(240px,1fr)); gap:12px; margin-bottom:16px; }
  .card { background:var(--card); border:1px solid var(--border); border-radius:10px; padding:14px 16px; }
  .label { font-size:12px; color:var(--muted); text-transform:uppercase; letter-spacing:.04em; }
  .val { font-size:16px; font-weight:600; margin-top:4px; }
  .pill { display:inline-block; padding:2px 8px; border-radius:999px; font-size:12px; font-weight:600; }
  .ok { background:rgba(63,185,80,.15); color:var(--ok); }
  .err { background:rgba(248,81,73,.15); color:var(--err); }
  .warn { background:rgba(210,153,34,.15); color:var(--warn); }
  .drop { border:2px dashed var(--border); border-radius:10px; padding:22px; text-align:center; cursor:pointer; transition:.15s; margin-bottom:16px; }
  .drop:hover, .drop.over { border-color:var(--accent); background:rgba(47,129,247,.06); }
  table { width:100%; border-collapse:collapse; font-size:13px; }
  th, td { text-align:left; padding:8px 10px; border-bottom:1px solid var(--border); }
  th { color:var(--muted); font-weight:600; }
  code { background:#0d1117; padding:2px 5px; border-radius:4px; font-size:12px; }
  #msg { margin-top:10px; font-size:13px; }
</style>
</head>
<body>
<div class="wrap">
  <h1>IVI 3D Closed-Loop Delta Bridge</h1>
  <p class="sub">OctoPrint-compatible Go bridge with automatic IVI Luban header promotion &amp; duplicate-filename protection.</p>

  <div class="grid">
    <div class="card"><div class="label">Printer</div><div class="val" id="printerUrl">...</div></div>
    <div class="card"><div class="label">Status</div><div class="val" id="printerStatus">...</div></div>
    <div class="card"><div class="label">Pipeline</div><div class="val" id="pipelineFlags">...</div></div>
  </div>

  <div class="card" style="margin-bottom:16px">
    <div class="label" style="margin-bottom:8px">Direct G-code Upload (auto-transforms OrcaSlicer G-code)</div>
    <div class="drop" id="drop">
      Drop a <code>.gcode</code> file here or click to choose
      <input type="file" id="fileInput" accept=".gcode,.gco,.g" style="display:none">
    </div>
    <div id="msg"></div>
  </div>

  <div class="card">
    <div class="label" style="margin-bottom:8px">Recent Uploads (start prints from the IVI touchscreen Files screen)</div>
    <table>
      <thead><tr><th>Time</th><th>Sent As</th><th>Transform</th><th>Temps</th><th>Lines</th><th>Size</th></tr></thead>
      <tbody id="histBody"><tr><td colspan="6" style="color:var(--muted)">Loading...</td></tr></tbody>
    </table>
  </div>
</div>
<script>
async function refresh() {
  try {
    const r = await fetch('/api/ivi/state');
    const d = await r.json();
    document.getElementById('printerUrl').textContent = d.printer_url + (d.dry_run ? ' (DRY-RUN)' : '');
    const st = d.status;
    document.getElementById('printerStatus').innerHTML = st.reachable
      ? '<span class="pill ok">Online (' + st.raw_state + ')</span>'
      : '<span class="pill err">Offline</span> <span style="font-size:12px;color:var(--muted)">' + (st.error||'') + '</span>';
    const flags = [];
    if (d.auto_transform) flags.push('<span class="pill ok">Auto-Header</span>');
    if (d.unique_names) flags.push('<span class="pill ok">Unique Names</span>');
    flags.push(d.allow_start ? '<span class="pill warn">Start Enabled</span>' : '<span class="pill">Safe Upload Only</span>');
    document.getElementById('pipelineFlags').innerHTML = flags.join(' ');

    const tb = document.getElementById('histBody');
    if (!d.history || d.history.length === 0) {
      tb.innerHTML = '<tr><td colspan="6" style="color:var(--muted)">No uploads yet.</td></tr>';
    } else {
      tb.innerHTML = d.history.map(h => {
        const t = new Date(h.timestamp).toLocaleTimeString();
        const badge = h.meta.transformed ? '<span class="pill ok">Promoted</span>'
          : (h.meta.already_ready ? '<span class="pill">Ready</span>' : '<span class="pill warn">Raw</span>');
        const temps = (h.meta.nozzle_temp || '-') + '° / ' + (h.meta.bed_temp || '-') + '°';
        const kb = (h.size_bytes / 1024).toFixed(1) + ' KB';
        const name = h.renamed ? '<code>' + h.sent_name + '</code> <span style="color:var(--muted)">(was ' + h.original_name + ')</span>' : '<code>' + h.sent_name + '</code>';
        return '<tr><td>' + t + '</td><td>' + name + '</td><td>' + badge + '</td><td>' + temps + '</td><td>' + (h.meta.total_lines||'-') + '</td><td>' + kb + '</td></tr>';
      }).join('');
    }
  } catch (e) {
    document.getElementById('printerStatus').innerHTML = '<span class="pill err">Bridge Error</span>';
  }
}

async function uploadFile(file) {
  const msg = document.getElementById('msg');
  msg.textContent = 'Transforming & uploading ' + file.name + '...';
  const fd = new FormData();
  fd.append('file', file, file.name);
  try {
    const r = await fetch('/api/ivi/upload', { method: 'POST', body: fd });
    const d = await r.json();
    if (!r.ok) throw new Error(d.error || ('HTTP ' + r.status));
    msg.innerHTML = '<span class="pill ok">Uploaded</span> <code>' + d.sent_name + '</code> (' + d.meta.total_lines + ' lines). Start it on the IVI touchscreen.';
    refresh();
  } catch (e) {
    msg.innerHTML = '<span class="pill err">Error</span> ' + e.message;
  }
}

const drop = document.getElementById('drop');
const fi = document.getElementById('fileInput');
drop.onclick = () => fi.click();
fi.onchange = () => { if (fi.files[0]) uploadFile(fi.files[0]); };
drop.ondragover = e => { e.preventDefault(); drop.classList.add('over'); };
drop.ondragleave = () => drop.classList.remove('over');
drop.ondrop = e => { e.preventDefault(); drop.classList.remove('over'); if (e.dataTransfer.files[0]) uploadFile(e.dataTransfer.files[0]); };

refresh();
setInterval(refresh, 5000);
</script>
</body>
</html>
`
