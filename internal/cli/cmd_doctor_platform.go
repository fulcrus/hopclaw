package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/internal/daemon"
)

func checkPlatformRuntime() checkResult {
	required := doctorRequiredGoVersion()
	path, err := exec.LookPath("go")
	if err != nil {
		detail := fmt.Sprintf("%s/%s, runtime %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
		if required != "" {
			detail += fmt.Sprintf(" (module requires %s)", required)
		}
		return checkResult{
			Category: "platform",
			Name:     "Go version",
			Status:   "warn",
			Detail:   detail + " — go toolchain not found in PATH",
			Fix:      "install Go and ensure the 'go' command is available in PATH",
		}
	}

	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return checkResult{
			Category: "platform",
			Name:     "Go version",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot query Go version: %v", err),
		}
	}

	current := strings.TrimSpace(string(out))
	detail := current
	if required != "" {
		detail += fmt.Sprintf(" (module requires %s)", required)
	}

	status := "ok"
	fix := ""
	if required != "" {
		if cmp, ok := compareDoctorGoVersions(current, required); ok && cmp < 0 {
			status = "warn"
			detail += " — runtime is older than the module requirement"
			fix = "rebuild or install HopClaw with Go " + required + " or newer"
		}
	}

	return checkResult{
		Category: "platform",
		Name:     "Go version",
		Status:   status,
		Detail:   detail,
		Fix:      fix,
	}
}

func checkDaemon() checkResult {
	mgr, err := daemon.NewServiceManager()
	if err != nil {
		return checkResult{
			Category: "platform",
			Name:     "System service",
			Status:   "warn",
			Detail:   err.Error(),
		}
	}

	status, err := mgr.Status()
	if err != nil {
		return checkResult{
			Category: "platform",
			Name:     "System service",
			Status:   "warn",
			Detail:   fmt.Sprintf("could not query: %v", err),
		}
	}

	if !status.Installed {
		return checkResult{
			Category: "platform",
			Name:     "System service",
			Status:   "warn",
			Detail:   "not installed; run 'hopclaw daemon install'",
		}
	}

	if !status.Running {
		return checkResult{
			Category: "platform",
			Name:     "System service",
			Status:   "warn",
			Detail:   "installed but not running; run 'hopclaw daemon start'",
		}
	}

	detail := fmt.Sprintf("running (PID %d)", status.PID)
	if status.PID == 0 {
		detail = "running"
	}
	return checkResult{
		Category: "platform",
		Name:     "System service",
		Status:   "ok",
		Detail:   detail,
	}
}

func checkPlatformNotes() checkResult {
	var notes []string

	switch runtime.GOOS {
	case "darwin":
		notes = append(notes, "macOS detected; if Gatekeeper blocks the binary, run: xattr -d com.apple.quarantine /path/to/hopclaw")
	case "linux":
		// Check for WSL2.
		if data, err := os.ReadFile("/proc/version"); err == nil {
			if strings.Contains(strings.ToLower(string(data)), "microsoft") {
				notes = append(notes, "WSL2 detected; ensure systemd is enabled (wsl --update) for daemon support")
			}
		}
	case "windows":
		notes = append(notes, "Windows detected; consider running under WSL2 for best experience")
	}

	if len(notes) == 0 {
		return checkResult{
			Category: "platform",
			Name:     "Platform notes",
			Status:   "ok",
			Detail:   fmt.Sprintf("%s/%s — no special notes", runtime.GOOS, runtime.GOARCH),
		}
	}

	return checkResult{
		Category: "platform",
		Name:     "Platform notes",
		Status:   "ok",
		Detail:   strings.Join(notes, "; "),
	}
}

func checkSandboxImage() checkResult {
	_, err := exec.LookPath("docker")
	if err != nil {
		return checkResult{
			Category: "platform",
			Name:     "Sandbox image",
			Status:   "ok",
			Detail:   "docker not found in PATH; sandbox features disabled",
		}
	}

	// Check if the default sandbox image is available locally.
	cmd := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "--filter", "reference=python:3.12-slim")
	out, err := cmd.Output()
	if err != nil {
		return checkResult{
			Category: "platform",
			Name:     "Sandbox image",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot query docker images: %v", err),
		}
	}

	if strings.TrimSpace(string(out)) == "" {
		return checkResult{
			Category: "platform",
			Name:     "Sandbox image",
			Status:   "warn",
			Detail:   "default sandbox image (python:3.12-slim) not found locally",
			Fix:      "run 'docker pull python:3.12-slim'",
		}
	}

	return checkResult{
		Category: "platform",
		Name:     "Sandbox image",
		Status:   "ok",
		Detail:   "default sandbox image available",
	}
}

func doctorRequiredGoVersion() string {
	data, err := os.ReadFile(filepath.Join("go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	return ""
}

func compareDoctorGoVersions(current, required string) (int, bool) {
	currentParts, ok := parseDoctorGoVersion(current)
	if !ok {
		return 0, false
	}
	requiredParts, ok := parseDoctorGoVersion(required)
	if !ok {
		return 0, false
	}
	limit := len(currentParts)
	if len(requiredParts) > limit {
		limit = len(requiredParts)
	}
	for i := 0; i < limit; i++ {
		var currentValue int
		if i < len(currentParts) {
			currentValue = currentParts[i]
		}
		var requiredValue int
		if i < len(requiredParts) {
			requiredValue = requiredParts[i]
		}
		switch {
		case currentValue < requiredValue:
			return -1, true
		case currentValue > requiredValue:
			return 1, true
		}
	}
	return 0, true
}

func parseDoctorGoVersion(raw string) ([]int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	for _, field := range strings.Fields(trimmed) {
		field = strings.TrimSpace(field)
		if strings.HasPrefix(field, "go") {
			trimmed = strings.TrimPrefix(field, "go")
			break
		}
	}
	end := len(trimmed)
	for i, r := range trimmed {
		if (r < '0' || r > '9') && r != '.' {
			end = i
			break
		}
	}
	trimmed = strings.Trim(trimmed[:end], ".")
	if trimmed == "" {
		return nil, false
	}
	segments := strings.Split(trimmed, ".")
	values := make([]int, 0, len(segments))
	for _, segment := range segments {
		value, err := strconv.Atoi(segment)
		if err != nil {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}
