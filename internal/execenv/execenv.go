package execenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/keychain"
)

type ChildEnvProfile string

const (
	ModuleExecProfile    ChildEnvProfile = "module_exec"
	InstallerExecProfile ChildEnvProfile = "installer_exec"
)

type SecretPresence struct {
	Resolved bool
	Source   string
}

type SecretResolver struct {
	LookupEnv     func(string) (string, bool)
	ResolveSecret func(string) (string, error)
}

func DefaultSecretResolver() SecretResolver {
	return SecretResolver{
		LookupEnv: os.LookupEnv,
		ResolveSecret: func(value string) (string, error) {
			return keychain.ResolveSecret(value)
		},
	}
}

func (r SecretResolver) Presence(value string) SecretPresence {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return SecretPresence{}
	}
	switch {
	case strings.HasPrefix(trimmed, "env:"):
		key := strings.TrimSpace(strings.TrimPrefix(trimmed, "env:"))
		if key == "" {
			return SecretPresence{}
		}
		lookup := r.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		resolved, ok := lookup(key)
		return SecretPresence{
			Resolved: ok && resolved != "",
			Source:   "env",
		}
	case strings.HasPrefix(trimmed, "keychain:"):
		resolve := r.ResolveSecret
		if resolve == nil {
			resolve = keychain.ResolveSecret
		}
		_, err := resolve(trimmed)
		return SecretPresence{
			Resolved: err == nil,
			Source:   "keychain",
		}
	default:
		return SecretPresence{
			Resolved: true,
			Source:   "literal",
		}
	}
}

func (r SecretResolver) Resolve(value string) (string, error) {
	resolve := r.ResolveSecret
	if resolve == nil {
		resolve = keychain.ResolveSecret
	}
	return resolve(strings.TrimSpace(value))
}

func (r SecretResolver) ResolveMap(values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := r.Resolve(values[key])
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", key, err)
		}
		out[key] = value
	}
	return out, nil
}

func BuildChildEnv(profile ChildEnvProfile, requiredSystemEnv []string, explicitEnv map[string]string, injectedEnv map[string]string, overlay map[string]string) []string {
	merged := make(map[string]string)
	for _, key := range baselineKeys(profile) {
		if value, ok := lookupEnvWithOverlay(key, overlay); ok && value != "" {
			merged[key] = value
		}
	}
	for _, key := range requiredSystemEnv {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if value, ok := lookupEnvWithOverlay(trimmed, overlay); ok && value != "" {
			merged[trimmed] = value
		}
	}
	for key, value := range explicitEnv {
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		merged[key] = value
	}
	for key, value := range injectedEnv {
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		merged[key] = value
	}
	for key, value := range overlay {
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		merged[key] = value
	}
	if strings.TrimSpace(merged["PATH"]) == "" {
		merged["PATH"] = defaultPATH()
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+merged[key])
	}
	return out
}

func BaselineKeys(profile ChildEnvProfile) []string {
	keys := append([]string(nil), baselineKeys(profile)...)
	sort.Strings(keys)
	return keys
}

func ParseEnvPairs(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func LookPathWithEnv(name string, overlay map[string]string) (string, error) {
	if len(overlay) == 0 || strings.TrimSpace(overlay["PATH"]) == "" {
		return exec.LookPath(name)
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		return exec.LookPath(name)
	}
	pathValue := overlay["PATH"]
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && isExecutable(info.Mode()) {
			return candidate, nil
		}
		if runtime.GOOS == "windows" {
			for _, ext := range filepath.SplitList(os.Getenv("PATHEXT")) {
				if ext == "" {
					continue
				}
				winCandidate := candidate + ext
				if info, err := os.Stat(winCandidate); err == nil && !info.IsDir() {
					return winCandidate, nil
				}
			}
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func baselineKeys(profile ChildEnvProfile) []string {
	keys := []string{
		"PATH",
		"HOME",
		"USER",
		"LOGNAME",
		"SHELL",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"TERM",
		"TMPDIR",
		"TEMP",
		"TMP",
		"TZ",
		"XDG_RUNTIME_DIR",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"ALL_PROXY",
		"SSL_CERT_FILE",
		"SSL_CERT_DIR",
		"SSH_AUTH_SOCK",
	}
	if profile == InstallerExecProfile {
		keys = append(keys,
			"NODE_EXTRA_CA_CERTS",
			"NPM_CONFIG_REGISTRY",
			"NPM_CONFIG_PROXY",
			"NPM_CONFIG_HTTPS_PROXY",
			"PIP_INDEX_URL",
			"PIP_EXTRA_INDEX_URL",
			"GIT_SSH_COMMAND",
		)
	}
	return keys
}

func lookupEnvWithOverlay(key string, overlay map[string]string) (string, bool) {
	if len(overlay) > 0 {
		if value := strings.TrimSpace(overlay[key]); value != "" {
			return value, true
		}
	}
	return os.LookupEnv(key)
}

func defaultPATH() string {
	if value := strings.TrimSpace(os.Getenv("PATH")); value != "" {
		return value
	}
	return "/usr/local/bin:/usr/bin:/bin"
}

func isExecutable(mode os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return mode&0o111 != 0
}
