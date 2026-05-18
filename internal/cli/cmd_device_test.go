package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDeviceCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"device", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(device --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"pair", "launch"} {
		if !strings.Contains(output, sub) {
			t.Fatalf("device help missing %q in %q", sub, output)
		}
	}
}

func TestRunDevicePairJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != operatorDevicesPairPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, operatorDevicesPairPath)
		}
		var req devicePairCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.DeviceID != "desk-1" {
			t.Fatalf("device_id = %q, want desk-1", req.DeviceID)
		}
		_ = json.NewEncoder(w).Encode(devicePairCreateResponse{
			DeviceID: req.DeviceID,
			Channel:  req.Channel,
			Code:     "654321",
			Status:   "pending",
		})
	}))
	defer srv.Close()

	restore := captureStdout(t)
	flagJSON = true
	defer func() { flagJSON = false }()

	if err := runDevicePair(context.Background(), "desktopd", devicePairOptions{
		GatewayURL: srv.URL,
		DeviceID:   "desk-1",
		Name:       "Desk One",
		Platform:   "Linux",
		Family:     "desktop",
	}); err != nil {
		t.Fatalf("runDevicePair() error = %v", err)
	}

	output := restore()
	var payload devicePairOutput
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode output: %v; output=%q", err, output)
	}
	if payload.PairingCode != "654321" {
		t.Fatalf("pairing_code = %q, want 654321", payload.PairingCode)
	}
	if !strings.Contains(payload.PreferredLaunch, "hopclaw devices launch desktopd") {
		t.Fatalf("preferred launch = %q", payload.PreferredLaunch)
	}
	if !strings.Contains(payload.FallbackLaunch, "hopclaw-desktopd") {
		t.Fatalf("fallback launch = %q", payload.FallbackLaunch)
	}
}

func TestRunDeviceLaunchPrint(t *testing.T) {
	tempDir := t.TempDir()
	name := "hopclaw-desktopd"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fake := filepath.Join(tempDir, name)
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(fake daemon): %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", tempDir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("Setenv(PATH): %v", err)
	}

	restore := captureStdout(t)

	if err := runDeviceLaunch(context.Background(), "desktopd", deviceLaunchOptions{
		GatewayURL:  "http://127.0.0.1:16280",
		PairingCode: "123456",
		DeviceID:    "desk-1",
		DeviceName:  "Desk One",
		PrintOnly:   true,
	}); err != nil {
		t.Fatalf("runDeviceLaunch() error = %v", err)
	}

	output := restore()
	if !strings.Contains(output, "123456") {
		t.Fatalf("launch output missing pairing code: %q", output)
	}
	if !strings.Contains(output, name) {
		t.Fatalf("launch output missing daemon name: %q", output)
	}
}

func captureStdout(t *testing.T) func() string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = old
		_ = w.Close()
		_ = r.Close()
	})
	return func() string {
		_ = w.Close()
		os.Stdout = old
		data, _ := io.ReadAll(r)
		_ = r.Close()
		return string(data)
	}
}
