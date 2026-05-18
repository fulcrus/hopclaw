package tunnel

import (
	"context"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNew_ValidConfig(t *testing.T) {
	cfg := Config{
		Host:       "example.com",
		RemoteHost: "localhost",
		RemotePort: 8080,
	}
	tun, err := New(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tun == nil {
		t.Fatal("expected tunnel, got nil")
	}
	if tun.sshPath == "" {
		t.Fatal("expected sshPath to be set")
	}
}

func TestNew_MissingHost(t *testing.T) {
	cfg := Config{
		RemoteHost: "localhost",
		RemotePort: 8080,
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestNew_MissingRemoteHost(t *testing.T) {
	cfg := Config{
		Host:       "example.com",
		RemotePort: 8080,
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for missing remote host")
	}
}

func TestNew_InvalidRemotePort(t *testing.T) {
	cfg := Config{
		Host:       "example.com",
		RemoteHost: "localhost",
		RemotePort: 0,
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for zero remote port")
	}
}

func TestNew_NegativeRemotePort(t *testing.T) {
	cfg := Config{
		Host:       "example.com",
		RemoteHost: "localhost",
		RemotePort: -1,
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for negative remote port")
	}
}

// ---------------------------------------------------------------------------
// Config defaults
// ---------------------------------------------------------------------------

func TestNew_DefaultPort(t *testing.T) {
	cfg := Config{
		Host:       "example.com",
		RemoteHost: "localhost",
		RemotePort: 8080,
	}
	tun, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tun.config.Port != defaultSSHPort {
		t.Errorf("expected default port %d, got %d", defaultSSHPort, tun.config.Port)
	}
}

func TestNew_DefaultMaxRetries(t *testing.T) {
	cfg := Config{
		Host:       "example.com",
		RemoteHost: "localhost",
		RemotePort: 8080,
	}
	tun, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tun.config.MaxRetries != defaultMaxRetries {
		t.Errorf("expected default max retries %d, got %d", defaultMaxRetries, tun.config.MaxRetries)
	}
}

func TestNew_CustomPort(t *testing.T) {
	const customPort = 2222
	cfg := Config{
		Host:       "example.com",
		Port:       customPort,
		RemoteHost: "localhost",
		RemotePort: 8080,
	}
	tun, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tun.config.Port != customPort {
		t.Errorf("expected port %d, got %d", customPort, tun.config.Port)
	}
}

// ---------------------------------------------------------------------------
// SSH binary detection
// ---------------------------------------------------------------------------

func TestNew_SSHNotAvailable(t *testing.T) {
	// t.Setenv restores the original value when the test finishes.
	t.Setenv("PATH", "/nonexistent")

	cfg := Config{
		Host:       "example.com",
		RemoteHost: "localhost",
		RemotePort: 8080,
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error when ssh is not in PATH")
	}
}

// ---------------------------------------------------------------------------
// Argument building
// ---------------------------------------------------------------------------

func TestBuildArgs_Minimal(t *testing.T) {
	tun := &Tunnel{
		config: Config{
			Host:       "example.com",
			Port:       defaultSSHPort,
			LocalPort:  9090,
			RemoteHost: "localhost",
			RemotePort: 8080,
		},
	}

	args := tun.buildArgs()

	expected := []string{
		"-N",
		"-L", "9090:localhost:8080",
		"-p", strconv.Itoa(defaultSSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=" + strconv.Itoa(sshConnectTimeout),
		"example.com",
	}

	assertArgsEqual(t, expected, args)
}

func TestBuildArgs_WithUserAndKey(t *testing.T) {
	tun := &Tunnel{
		config: Config{
			Host:       "server.example.com",
			Port:       2222,
			User:       "deploy",
			KeyFile:    "/home/deploy/.ssh/id_ed25519",
			LocalPort:  3000,
			RemoteHost: "127.0.0.1",
			RemotePort: 443,
		},
	}

	args := tun.buildArgs()

	expected := []string{
		"-N",
		"-L", "3000:127.0.0.1:443",
		"-l", "deploy",
		"-p", "2222",
		"-i", "/home/deploy/.ssh/id_ed25519",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=" + strconv.Itoa(sshConnectTimeout),
		"server.example.com",
	}

	assertArgsEqual(t, expected, args)
}

func TestBuildArgs_AutoAssignLocalPort(t *testing.T) {
	tun := &Tunnel{
		config: Config{
			Host:       "example.com",
			Port:       defaultSSHPort,
			LocalPort:  0,
			RemoteHost: "db-host",
			RemotePort: 5432,
		},
	}

	args := tun.buildArgs()

	// LocalPort 0 should appear as "0" in the forward spec.
	found := false
	for _, a := range args {
		if a == "0:db-host:5432" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected forward spec '0:db-host:5432' in args: %v", args)
	}
}

func TestBuildArgs_HostIsLastArg(t *testing.T) {
	const host = "my-remote-host.io"
	tun := &Tunnel{
		config: Config{
			Host:       host,
			Port:       defaultSSHPort,
			RemoteHost: "localhost",
			RemotePort: 80,
		},
	}
	args := tun.buildArgs()
	if len(args) == 0 {
		t.Fatal("expected at least one arg")
	}
	if args[len(args)-1] != host {
		t.Errorf("expected last arg to be %q, got %q", host, args[len(args)-1])
	}
}

// ---------------------------------------------------------------------------
// Start / Stop state machine
// ---------------------------------------------------------------------------

func TestStart_AlreadyRunning(t *testing.T) {
	tun := &Tunnel{
		config: Config{
			Host:       "example.com",
			Port:       defaultSSHPort,
			RemoteHost: "localhost",
			RemotePort: 8080,
		},
		sshPath: findSSHOrSkip(t),
	}

	// Manually set running to simulate an active tunnel.
	tun.status.Running = true

	err := tun.Start(context.Background())
	if err != ErrTunnelRunning {
		t.Fatalf("expected ErrTunnelRunning, got %v", err)
	}
}

func TestStop_NotRunning(t *testing.T) {
	tun := &Tunnel{
		config: Config{
			Host:       "example.com",
			Port:       defaultSSHPort,
			RemoteHost: "localhost",
			RemotePort: 8080,
		},
		sshPath: findSSHOrSkip(t),
	}

	err := tun.Stop()
	if err != ErrTunnelStopped {
		t.Fatalf("expected ErrTunnelStopped, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

func TestGetStatus_Initial(t *testing.T) {
	tun := &Tunnel{
		config: Config{
			Host:       "example.com",
			Port:       defaultSSHPort,
			RemoteHost: "localhost",
			RemotePort: 8080,
			LocalPort:  9090,
		},
		status: Status{
			LocalPort:  9090,
			RemoteAddr: "localhost:8080",
		},
	}

	s := tun.GetStatus()
	if s.Running {
		t.Error("expected Running=false for new tunnel")
	}
	if s.LocalPort != 9090 {
		t.Errorf("expected local port 9090, got %d", s.LocalPort)
	}
	if s.RemoteAddr != "localhost:8080" {
		t.Errorf("expected remote addr 'localhost:8080', got %q", s.RemoteAddr)
	}
	if s.Retries != 0 {
		t.Errorf("expected 0 retries, got %d", s.Retries)
	}
}

func TestIsRunning_Initial(t *testing.T) {
	tun := &Tunnel{}
	if tun.IsRunning() {
		t.Error("expected IsRunning=false for zero-value tunnel")
	}
}

// ---------------------------------------------------------------------------
// Wait on non-started tunnel
// ---------------------------------------------------------------------------

func TestWait_NotStarted(t *testing.T) {
	tun := &Tunnel{}
	err := tun.Wait()
	if err != ErrTunnelStopped {
		t.Fatalf("expected ErrTunnelStopped, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Start and Stop integration (uses real ssh binary, but exits immediately)
// ---------------------------------------------------------------------------

func TestStartStop_Lifecycle(t *testing.T) {
	sshPath := findSSHOrSkip(t)

	tun := &Tunnel{
		config: Config{
			Host:       "127.0.0.1",
			Port:       defaultSSHPort,
			RemoteHost: "localhost",
			RemotePort: 8080,
			LocalPort:  0,
		},
		sshPath: sshPath,
		status: Status{
			LocalPort:  0,
			RemoteAddr: "localhost:8080",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := tun.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !tun.IsRunning() {
		t.Error("expected IsRunning=true after Start")
	}

	s := tun.GetStatus()
	if s.StartedAt.IsZero() {
		t.Error("expected StartedAt to be set")
	}

	err = tun.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if tun.IsRunning() {
		t.Error("expected IsRunning=false after Stop")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findSSHOrSkip locates the ssh binary or skips the test.
func findSSHOrSkip(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh not available, skipping test")
	}
	return path
}

// assertArgsEqual compares two string slices element-by-element.
func assertArgsEqual(t *testing.T, expected, actual []string) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("arg count mismatch: expected %d, got %d\nexpected: %v\nactual:   %v",
			len(expected), len(actual), expected, actual)
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Errorf("arg[%d] mismatch: expected %q, got %q", i, expected[i], actual[i])
		}
	}
}
