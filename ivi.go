package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"
)

var iviStateToOcto = map[string]string{
	"IDLE":      "Operational",
	"PRINTING":  "Printing",
	"RUNNING":   "Printing",
	"PAUSED":    "Paused",
	"PAUSE":     "Paused",
	"FINISHED":  "Operational",
	"COMPLETED": "Operational",
	"BUSY":      "Printing",
}

// IVIClient talks to the IVI printer's embedded kore HTTP server on port 8080.
type IVIClient struct {
	BaseURL    string
	DryRun     bool
	AllowStart bool

	statusHTTP *http.Client
	uploadHTTP *http.Client

	mu    sync.Mutex
	token string
}

func NewIVIClient(host string, dryRun, allowStart bool, token string) *IVIClient {
	base := strings.TrimRight(host, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &IVIClient{
		BaseURL:    base,
		DryRun:     dryRun,
		AllowStart: allowStart,
		token:      token,
		statusHTTP: &http.Client{Timeout: 4 * time.Second},
		uploadHTTP: &http.Client{Timeout: 5 * time.Minute},
	}
}

type PrinterStatus struct {
	Reachable bool   `json:"reachable"`
	RawState  string `json:"raw_state"`
	OctoState string `json:"octo_state"`
	Error     string `json:"error,omitempty"`
}

func (c *IVIClient) Status(ctx context.Context) PrinterStatus {
	if c.DryRun {
		return PrinterStatus{
			Reachable: true,
			RawState:  "IDLE",
			OctoState: "Operational",
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/status", nil)
	if err != nil {
		return PrinterStatus{Reachable: false, OctoState: "Offline", Error: err.Error()}
	}
	resp, err := c.statusHTTP.Do(req)
	if err != nil {
		return PrinterStatus{Reachable: false, OctoState: "Offline", Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	var doc struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(body, &doc)
	raw := strings.ToUpper(strings.TrimSpace(doc.Status))
	if raw == "" {
		raw = "IDLE"
	}
	octo, ok := iviStateToOcto[raw]
	if !ok {
		octo = "Operational"
	}
	return PrinterStatus{
		Reachable: true,
		RawState:  raw,
		OctoState: octo,
	}
}

// Upload sends raw G-code bytes to POST /api/v1/upload (unauthenticated on IVI).
func (c *IVIClient) Upload(ctx context.Context, filename string, data []byte) (string, error) {
	if c.DryRun {
		log.Printf("[ivi] DRY-RUN upload %q (%d bytes)", filename, len(data))
		return "Upload successfully (dry-run)", nil
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok != "" {
		_ = mw.WriteField("token", tok)
	}

	// Preserve filename verbatim (spaces and parentheses work on IVI).
	safeName := strings.ReplaceAll(filename, `"`, `_`)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, safeName))
	h.Set("Content-Type", "application/octet-stream")
	fw, err := mw.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("create multipart part: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return "", fmt.Errorf("write multipart payload: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.uploadHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /api/v1/upload: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(respBody))
	log.Printf("[ivi] upload %q (%d bytes) -> HTTP %d %q", filename, len(data), resp.StatusCode, text)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return text, fmt.Errorf("printer returned HTTP %d: %s", resp.StatusCode, text)
	}
	low := strings.ToLower(text)
	if !strings.Contains(low, "success") && !strings.Contains(low, "complete") {
		return text, fmt.Errorf("unexpected upload response (HTTP %d): %s", resp.StatusCode, text)
	}
	return text, nil
}

// Command forwards a control command (start_print, pause_print, resume_print,
// stop_print, execute_code) when AllowStart is enabled.
func (c *IVIClient) Command(ctx context.Context, endpoint string, extra url.Values) (int, string, error) {
	if !c.AllowStart {
		return http.StatusConflict, "", fmt.Errorf(
			"%s disabled by default (IVI /api/v1/status reports IDLE mid-print; start from the printer's Files screen or restart bridge with --allow-start)",
			endpoint,
		)
	}
	if c.DryRun {
		log.Printf("[ivi] DRY-RUN command /api/v1/%s %v", endpoint, extra)
		return http.StatusOK, "dry-run", nil
	}

	form := url.Values{}
	for k, vals := range extra {
		for _, v := range vals {
			form.Add(k, v)
		}
	}
	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok != "" && form.Get("token") == "" {
		form.Set("token", tok)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/"+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.statusHTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(body))
	log.Printf("[ivi] command /api/v1/%s -> HTTP %d %q", endpoint, resp.StatusCode, text)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, text, fmt.Errorf("printer returned HTTP %d: %s", resp.StatusCode, text)
	}
	return resp.StatusCode, text, nil
}
