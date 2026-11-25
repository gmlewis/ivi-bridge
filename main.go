package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func defaultStateFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ivi-bridge", "history.json")
}

func defaultPrinterAddr() string {
	if v := os.Getenv("IVI_PRINTER"); v != "" {
		return v
	}
	return "192.168.1.100:8080"
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "upload" {
		runUploadCLI(os.Args[2:])
		return
	}

	printer := flag.String("printer", defaultPrinterAddr(), "IVI printer host:port (or IVI_PRINTER env var)")
	listen := flag.String("listen", "0.0.0.0:5000", "address:port to listen on (OctoPrint API + Device UI)")
	serialPort := flag.String("serial", os.Getenv("IVI_SERIAL"), "optional USB serial port for live Marlin control ('auto', '/dev/ttyACM0', '/dev/cu.usbmodem...', or IVI_SERIAL env var)")
	baud := flag.Int("baud", 115200, "USB serial baud rate")
	apiKey := flag.String("api-key", "", "optional X-Api-Key required from OrcaSlicer")
	token := flag.String("token", "", "optional IVI session token")
	autoTransform := flag.Bool("auto-transform", true, "automatically promote IVI Luban header on upload")
	uniqueNames := flag.Bool("unique-names", true, "append timestamp suffix if a filename was already uploaded")
	allowStart := flag.Bool("allow-start", false, "allow start/pause/resume/stop commands (OFF by default: IVI status reports IDLE mid-print)")
	dryRun := flag.Bool("dry-run", false, "forward nothing to the printer; log instead")
	stateFile := flag.String("state-file", defaultStateFile(), "path to persist upload history JSON (empty to disable)")
	flag.Parse()

	ivi := NewIVIClient(*printer, *dryRun, *allowStart, *token)
	serial := NewSerialController(*serialPort, *baud, *dryRun)
	defer serial.Close()

	bridge := NewBridgeServer(ivi, *apiKey, *autoTransform, *uniqueNames, *stateFile)
	bridge.Serial = serial

	log.Printf("[ivi-bridge] printer        : %s", ivi.BaseURL)
	log.Printf("[ivi-bridge] listening      : http://%s (OctoPrint API + Device UI)", *listen)
	if *serialPort != "" {
		log.Printf("[ivi-bridge] serial         : %s @ %d baud", *serialPort, *baud)
	}
	log.Printf("[ivi-bridge] auto-transform : %v | unique-names: %v | allow-start: %v | dry-run: %v",
		*autoTransform, *uniqueNames, *allowStart, *dryRun)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           bridge.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[ivi-bridge] server error: %v", err)
	}
}

func runUploadCLI(args []string) {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	printer := fs.String("printer", defaultPrinterAddr(), "IVI printer host:port (or IVI_PRINTER env var)")
	autoTransform := fs.Bool("auto-transform", true, "promote IVI Luban header before upload")
	dryRun := fs.Bool("dry-run", false, "transform and print summary without uploading")
	nameOverride := fs.String("name", "", "override filename on printer")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "usage: ivi-bridge upload [flags] <file.gcode>\n")
		os.Exit(2)
	}
	path := fs.Arg(0)
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read %s: %v", path, err)
	}

	payload := raw
	var meta GCodeMeta
	if *autoTransform {
		payload, meta, err = TransformGCode(raw, time.Now())
		if err != nil {
			log.Fatalf("transform: %v", err)
		}
		log.Printf("[ivi-bridge] transformed=%v lines=%d/%d nozzle=%sC bed=%sC bounds=(%.1f..%.1f, %.1f..%.1f, %.1f..%.1f)",
			meta.Transformed, meta.TotalLines, meta.ValidLines, meta.NozzleTemp, meta.BedTemp,
			meta.MinX, meta.MaxX, meta.MinY, meta.MaxY, meta.MinZ, meta.MaxZ)
	}

	name := *nameOverride
	if name == "" {
		name = filepath.Base(path)
	}
	ivi := NewIVIClient(*printer, *dryRun, false, "")
	resp, err := ivi.Upload(context.Background(), name, payload)
	if err != nil {
		log.Fatalf("upload failed: %v", err)
	}
	fmt.Printf("Uploaded %s (%d bytes): %s\n", name, len(payload), resp)
}
