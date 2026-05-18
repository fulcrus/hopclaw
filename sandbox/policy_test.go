package sandbox

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidateImage
// ---------------------------------------------------------------------------

func TestValidateImage_EmptyName(t *testing.T) {
	err := ValidateImage("", nil)
	if err == nil {
		t.Fatal("expected error for empty image name")
	}
}

func TestValidateImage_EmptyAllowedList(t *testing.T) {
	if err := ValidateImage("anything:latest", nil); err != nil {
		t.Fatalf("empty allowed list should permit any image, got: %v", err)
	}
}

func TestValidateImage_Allowed(t *testing.T) {
	allowed := []string{"python:3.12-slim", "node:20-slim"}
	if err := ValidateImage("python:3.12-slim", allowed); err != nil {
		t.Fatalf("expected allowed image to pass, got: %v", err)
	}
}

func TestValidateImage_NotAllowed(t *testing.T) {
	allowed := []string{"python:3.12-slim", "node:20-slim"}
	err := ValidateImage("ubuntu:latest", allowed)
	if err == nil {
		t.Fatal("expected error for disallowed image")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateImage_WhitespaceHandling(t *testing.T) {
	err := ValidateImage("  ", nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only image name")
	}
}

// ---------------------------------------------------------------------------
// SanitizeEnv
// ---------------------------------------------------------------------------

func TestSanitizeEnv_Nil(t *testing.T) {
	result := SanitizeEnv(nil)
	if result != nil {
		t.Fatalf("expected nil for nil input, got: %v", result)
	}
}

func TestSanitizeEnv_Empty(t *testing.T) {
	result := SanitizeEnv(map[string]string{})
	if result != nil {
		t.Fatalf("expected nil for empty input, got: %v", result)
	}
}

func TestSanitizeEnv_RemovesDangerous(t *testing.T) {
	env := map[string]string{
		"MY_VAR":          "safe",
		"AWS_SECRET_KEY":  "secret",
		"OPENAI_API_KEY":  "key",
		"ANTHROPIC_KEY":   "key",
		"GITHUB_TOKEN":    "token",
		"SSH_AUTH_SOCK":   "/tmp/agent",
		"DOCKER_HOST":     "tcp://...",
		"CUSTOM_SETTING":  "value",
		"HOME":            "/root",
		"PATH":            "/usr/bin",
		"AZURE_TENANT_ID": "tenant",
		"GCP_PROJECT":     "proj",
		"GOOGLE_KEY":      "key",
		"GPG_AGENT_INFO":  "info",
	}
	result := SanitizeEnv(env)

	if _, ok := result["AWS_SECRET_KEY"]; ok {
		t.Error("AWS_SECRET_KEY should be removed")
	}
	if _, ok := result["OPENAI_API_KEY"]; ok {
		t.Error("OPENAI_API_KEY should be removed")
	}
	if _, ok := result["ANTHROPIC_KEY"]; ok {
		t.Error("ANTHROPIC_KEY should be removed")
	}
	if _, ok := result["GITHUB_TOKEN"]; ok {
		t.Error("GITHUB_TOKEN should be removed")
	}
	if _, ok := result["SSH_AUTH_SOCK"]; ok {
		t.Error("SSH_AUTH_SOCK should be removed")
	}
	if _, ok := result["DOCKER_HOST"]; ok {
		t.Error("DOCKER_HOST should be removed")
	}
	if _, ok := result["HOME"]; ok {
		t.Error("HOME should be removed")
	}
	if _, ok := result["PATH"]; ok {
		t.Error("PATH should be removed")
	}
	if _, ok := result["AZURE_TENANT_ID"]; ok {
		t.Error("AZURE_TENANT_ID should be removed")
	}
	if _, ok := result["GCP_PROJECT"]; ok {
		t.Error("GCP_PROJECT should be removed")
	}
	if _, ok := result["GOOGLE_KEY"]; ok {
		t.Error("GOOGLE_KEY should be removed")
	}
	if _, ok := result["GPG_AGENT_INFO"]; ok {
		t.Error("GPG_AGENT_INFO should be removed")
	}

	if v, ok := result["MY_VAR"]; !ok || v != "safe" {
		t.Error("MY_VAR should be preserved")
	}
	if v, ok := result["CUSTOM_SETTING"]; !ok || v != "value" {
		t.Error("CUSTOM_SETTING should be preserved")
	}
}

func TestSanitizeEnv_CaseInsensitive(t *testing.T) {
	env := map[string]string{
		"aws_secret": "secret",
		"Openai_Key": "key",
		"safe_var":   "ok",
	}
	result := SanitizeEnv(env)

	if _, ok := result["aws_secret"]; ok {
		t.Error("aws_secret (lowercase) should be removed")
	}
	if _, ok := result["Openai_Key"]; ok {
		t.Error("Openai_Key (mixed case) should be removed")
	}
	if v, ok := result["safe_var"]; !ok || v != "ok" {
		t.Error("safe_var should be preserved")
	}
}

func TestSanitizeEnv_AllDangerous(t *testing.T) {
	env := map[string]string{
		"AWS_KEY": "secret",
		"HOME":    "/root",
	}
	result := SanitizeEnv(env)
	if result != nil {
		t.Fatalf("expected nil when all keys are dangerous, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// ValidateMountPath
// ---------------------------------------------------------------------------

func TestValidateMountPath_Safe(t *testing.T) {
	cases := []string{"/tmp/workspace", "/home/user/data", "/opt/myapp"}
	for _, p := range cases {
		if err := ValidateMountPath(p); err != nil {
			t.Errorf("ValidateMountPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateMountPath_DangerousExact(t *testing.T) {
	cases := []string{"/etc", "/proc", "/sys", "/dev", "/run", "/var/run", "/root", "/boot", "/"}
	for _, p := range cases {
		if err := ValidateMountPath(p); err == nil {
			t.Errorf("ValidateMountPath(%q) = nil, want error", p)
		}
	}
}

func TestValidateMountPath_DangerousSubPath(t *testing.T) {
	cases := []string{"/etc/passwd", "/proc/self", "/var/run/docker.sock", "/sys/kernel"}
	for _, p := range cases {
		if err := ValidateMountPath(p); err == nil {
			t.Errorf("ValidateMountPath(%q) = nil, want error for sub-path", p)
		}
	}
}

func TestValidateMountPath_DockerSocket(t *testing.T) {
	for _, p := range []string{"/var/run/docker.sock", "/run/docker.sock"} {
		if err := ValidateMountPath(p); err == nil {
			t.Errorf("ValidateMountPath(%q) = nil, want error (docker socket)", p)
		}
	}
}

func TestValidateMountPath_TrailingSlash(t *testing.T) {
	// filepath.Clean normalises these; they must still be blocked.
	if err := ValidateMountPath("/etc/"); err == nil {
		t.Error("ValidateMountPath(\"/etc/\") = nil, want error")
	}
	if err := ValidateMountPath("/tmp/safe/"); err != nil {
		t.Errorf("ValidateMountPath(\"/tmp/safe/\") = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// BuildDockerArgs
// ---------------------------------------------------------------------------

func TestBuildDockerArgs_Defaults(t *testing.T) {
	cfg := Config{}
	req := ExecRequest{
		Command: []string{"echo", "hello"},
	}
	args := BuildDockerArgs(cfg, req)

	assertContains(t, args, "--rm")
	assertContains(t, args, "--read-only")
	assertContains(t, args, "--security-opt=no-new-privileges")
	assertContainsSequence(t, args, "--network", defaultNetworkMode)
	assertContainsSequence(t, args, "--memory", defaultMemoryLimit)
	assertContainsSequence(t, args, "--cpus", defaultCPULimit)
	assertContainsSequence(t, args, "-w", defaultWorkDir)

	// Default image should be used.
	if args[0] != "run" {
		t.Fatalf("expected first arg to be 'run', got %q", args[0])
	}
	assertContains(t, args, defaultImage)
	assertContains(t, args, "echo")
	assertContains(t, args, "hello")
}

func TestBuildDockerArgs_CustomImage(t *testing.T) {
	cfg := Config{}
	req := ExecRequest{
		Image:   "node:20-slim",
		Command: []string{"node", "-e", "console.log('hi')"},
	}
	args := BuildDockerArgs(cfg, req)

	assertContains(t, args, "node:20-slim")
	assertNotContains(t, args, defaultImage)
}

func TestBuildDockerArgs_WithStdin(t *testing.T) {
	cfg := Config{}
	req := ExecRequest{
		Command: []string{"cat"},
		Stdin:   "some input",
	}
	args := BuildDockerArgs(cfg, req)

	assertContains(t, args, "-i")
}

func TestBuildDockerArgs_WithoutStdin(t *testing.T) {
	cfg := Config{}
	req := ExecRequest{
		Command: []string{"echo", "hello"},
	}
	args := BuildDockerArgs(cfg, req)

	assertNotContains(t, args, "-i")
}

func TestBuildDockerArgs_EnvSanitized(t *testing.T) {
	cfg := Config{}
	req := ExecRequest{
		Command: []string{"env"},
		Env: map[string]string{
			"SAFE_VAR": "value",
			"AWS_KEY":  "secret",
		},
	}
	args := BuildDockerArgs(cfg, req)

	// SAFE_VAR should appear as -e argument.
	found := false
	for _, arg := range args {
		if arg == "SAFE_VAR=value" {
			found = true
		}
		if strings.Contains(arg, "AWS_KEY") {
			t.Error("AWS_KEY should have been sanitized out")
		}
	}
	if !found {
		t.Error("expected SAFE_VAR=value to appear in args")
	}
}

func TestBuildDockerArgs_SeccompProfile(t *testing.T) {
	cfg := Config{SeccompProfile: "/etc/docker/seccomp.json"}
	req := ExecRequest{Command: []string{"ls"}}
	args := BuildDockerArgs(cfg, req)

	want := "--security-opt=seccomp=/etc/docker/seccomp.json"
	assertContains(t, args, want)
}

func TestBuildDockerArgs_NoSeccompProfile(t *testing.T) {
	cfg := Config{}
	req := ExecRequest{Command: []string{"ls"}}
	args := BuildDockerArgs(cfg, req)

	for _, arg := range args {
		if strings.Contains(arg, "seccomp=") {
			t.Errorf("expected no seccomp arg when SeccompProfile is empty, got %q", arg)
		}
	}
}

func TestBuildDockerArgs_CustomConfig(t *testing.T) {
	cfg := Config{
		MemoryLimit: "512m",
		CPULimit:    "2.0",
		NetworkMode: "bridge",
		WorkDir:     "/app",
	}
	req := ExecRequest{
		Command: []string{"ls"},
	}
	args := BuildDockerArgs(cfg, req)

	assertContainsSequence(t, args, "--network", "bridge")
	assertContainsSequence(t, args, "--memory", "512m")
	assertContainsSequence(t, args, "--cpus", "2.0")
	assertContainsSequence(t, args, "-w", "/app")
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertContains(t *testing.T, args []string, target string) {
	t.Helper()
	for _, arg := range args {
		if arg == target {
			return
		}
	}
	t.Errorf("expected args to contain %q, got %v", target, args)
}

func assertNotContains(t *testing.T, args []string, target string) {
	t.Helper()
	for _, arg := range args {
		if arg == target {
			t.Errorf("expected args NOT to contain %q, got %v", target, args)
			return
		}
	}
}

func assertContainsSequence(t *testing.T, args []string, key, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected args to contain %q %q in sequence, got %v", key, value, args)
}
