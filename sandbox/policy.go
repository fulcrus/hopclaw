package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Dangerous environment variable keys
// ---------------------------------------------------------------------------

// dangerousEnvKeys lists environment variable prefixes and exact names that
// must never be forwarded into a sandbox container.
var dangerousEnvKeys = []string{
	"AWS_",
	"AZURE_",
	"GCP_",
	"GOOGLE_",
	"OPENAI_",
	"ANTHROPIC_",
	"GITHUB_TOKEN",
	"DOCKER_",
	"SSH_",
	"GPG_",
	"HOME",
	"USER",
	"LOGNAME",
	"HOSTNAME",
	"PATH",
}

// ---------------------------------------------------------------------------
// Dangerous mount paths
// ---------------------------------------------------------------------------

// dangerousMountPaths lists host paths that must never be bind-mounted into a
// container because they expose critical host resources, kernel interfaces, or
// credentials (including the Docker socket which grants host-root access).
var dangerousMountPaths = []string{
	"/",
	"/etc",
	"/proc",
	"/sys",
	"/dev",
	"/run",
	"/var/run",
	"/var/run/docker.sock",
	"/run/docker.sock",
	"/root",
	"/boot",
}

// ValidateMountPath returns an error when hostPath is, or is nested under, a
// dangerous host path. Call this before accepting any user-supplied bind-mount.
func ValidateMountPath(hostPath string) error {
	clean := filepath.Clean(hostPath)
	for _, dangerous := range dangerousMountPaths {
		if isSubPath(clean, dangerous) {
			return fmt.Errorf("sandbox: mounting %q is not allowed (dangerous host path)", hostPath)
		}
	}
	return nil
}

// isSubPath reports whether path equals base or is nested directly under it.
func isSubPath(path, base string) bool {
	return path == base || strings.HasPrefix(path, base+"/")
}

// ---------------------------------------------------------------------------
// ValidateImage
// ---------------------------------------------------------------------------

// ValidateImage checks whether image is present in the allowed list.
// An empty allowed list permits any image.
func ValidateImage(image string, allowed []string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("image name is required")
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, a := range allowed {
		if strings.TrimSpace(a) == image {
			return nil
		}
	}
	return fmt.Errorf("image %q is not in the allowed list", image)
}

// ---------------------------------------------------------------------------
// SanitizeEnv
// ---------------------------------------------------------------------------

// SanitizeEnv returns a copy of env with dangerous keys removed.
// Keys that match any prefix in dangerousEnvKeys are stripped.
func SanitizeEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		if isDangerousKey(key) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isDangerousKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, prefix := range dangerousEnvKeys {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// BuildDockerArgs
// ---------------------------------------------------------------------------

// BuildDockerArgs constructs the full argument list for a `docker run`
// invocation. It applies security constraints from cfg and merges per-request
// overrides from req.
func BuildDockerArgs(cfg Config, req ExecRequest) []string {
	cfg.applyDefaults()

	image := strings.TrimSpace(req.Image)
	if image == "" {
		image = cfg.Image
	}

	args := []string{
		"run",
		"--rm",
		"--network", cfg.NetworkMode,
		"--memory", cfg.MemoryLimit,
		"--cpus", cfg.CPULimit,
		"--read-only",
		"--security-opt=no-new-privileges",
		"-w", cfg.WorkDir,
	}

	if cfg.SeccompProfile != "" {
		args = append(args, "--security-opt=seccomp="+cfg.SeccompProfile)
	}

	if req.Stdin != "" {
		args = append(args, "-i")
	}

	sanitized := SanitizeEnv(req.Env)
	for key, value := range sanitized {
		args = append(args, "-e", key+"="+value)
	}

	args = append(args, image)
	args = append(args, req.Command...)

	return args
}
