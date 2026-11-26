package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOctoPrintUploadPipeline(t *testing.T) {
	var receivedName string
	var receivedBody string

	fakePrinter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"IDLE"}`))
		case "/api/v1/upload":
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Fatalf("printer parse multipart: %v", err)
			}
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("printer form file: %v", err)
			}
			defer f.Close()
			b, _ := io.ReadAll(f)
			receivedName = h.Filename
			receivedBody = string(b)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("Upload successfully"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakePrinter.Close()

	ivi := NewIVIClient(fakePrinter.URL, false, false, "")
	bridge := NewBridgeServer(ivi, "", true, true, "")
	bridge.Now = func() time.Time { return time.Date(2026, 11, 21, 18, 5, 6, 0, time.UTC) }
	ts := httptest.NewServer(bridge.Handler())
	defer ts.Close()

	// 1. Version handshake
	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatalf("GET /api/version: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// 2. Upload OrcaSlicer G-code via /api/files/local
	sampleGCode := "; HEADER_BLOCK_START\nM82 ;absolute extrusion mode\n;Header Start\n;material_type: PLA\n;nozzle_temperature(°C): 220\n;build_plate_temperature(°C): 60\n;Header End\n;Start GCode begin\nM104 S220\n;Start GCode end\nG1 X10 Y20 Z0.2\n"

	uploadOnce := func(filename string) (int, map[string]any) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", filename)
		_, _ = fw.Write([]byte(sampleGCode))
		_ = mw.WriteField("print", "false")
		_ = mw.Close()

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/files/local", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /api/files/local: %v", err)
		}
		defer r.Body.Close()
		var doc map[string]any
		_ = json.NewDecoder(r.Body).Decode(&doc)
		return r.StatusCode, doc
	}

	code, _ := uploadOnce("benchy.gcode")
	if code != http.StatusOK {
		t.Fatalf("first upload expected 200, got %d", code)
	}
	if receivedName != "benchy.gcode" {
		t.Fatalf("expected benchy.gcode, got %q", receivedName)
	}
	if !strings.HasPrefix(receivedBody, ";Header Start\n") {
		t.Fatalf("expected transformed G-code starting with ;Header Start, got:\n%s", receivedBody)
	}

	// 3. Second upload with same filename should be auto-renamed for IVI safety
	code2, _ := uploadOnce("benchy.gcode")
	if code2 != http.StatusOK {
		t.Fatalf("second upload expected 200, got %d", code2)
	}
	if receivedName != "benchy_180506.gcode" {
		t.Fatalf("expected duplicate to be renamed to benchy_180506.gcode, got %q", receivedName)
	}
}

func TestUploadAndPrintUSBSerial(t *testing.T) {
	fakePrinter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			_, _ = w.Write([]byte(`{"status":"IDLE"}`))
		case "/api/v1/upload":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("Upload successfully"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakePrinter.Close()

	ivi := NewIVIClient(fakePrinter.URL, false, false, "")
	bridge := NewBridgeServer(ivi, "", true, true, "")
	bridge.Serial = NewSerialController("auto", 115200, true) // dry-run USB serial
	defer bridge.Serial.Close()
	// Give dry-run serial loop a moment to mark Connected=true.
	deadline := time.Now().Add(2 * time.Second)
	for !bridge.Serial.Snapshot().Connected && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	ts := httptest.NewServer(bridge.Handler())
	defer ts.Close()

	sampleGCode := "; HEADER_BLOCK_START\nM82 ;absolute extrusion mode\n;Header Start\n;material_type: PLA\n;nozzle_temperature(°C): 220\n;build_plate_temperature(°C): 60\n;Header End\n;Start GCode begin\nG28\nG1 E7.0 F300\n;Start GCode end\nG1 X10 Y20 Z0.2\n"

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "gear.gcode")
	_, _ = fw.Write([]byte(sampleGCode))
	_ = mw.WriteField("print", "true")
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/files/local", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/files/local (print=true): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for Upload & Print with USB serial, got %d: %s", resp.StatusCode, string(b))
	}

	// Wait for dry-run print to complete and verify PrintJobSnapshot recorded completion.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pj := bridge.Serial.PrintJobSnapshot()
		if pj != nil && !pj.Active && pj.State == "Completed" {
			if pj.FileName != "gear.gcode" || pj.TotalLines < 3 || pj.SentLines != pj.TotalLines {
				t.Fatalf("unexpected completed print job status: %+v", pj)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for dry-run USB serial print to complete; status=%+v", bridge.Serial.PrintJobSnapshot())
}
