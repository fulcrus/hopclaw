package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/keychain"
)

const (
	targetStoreFilename       = "targets.json"
	targetKindRemote          = "remote"
	targetAuthTypeNone        = "none"
	targetAuthTypeBearer      = "bearer"
	managedTargetSecretPrefix = "target."
)

var targetNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type savedTargetFile struct {
	Targets []savedTargetProfile `json:"targets"`
}

type savedTargetProfile struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	BaseURL  string `json:"base_url"`
	AuthType string `json:"auth_type,omitempty"`
	AuthRef  string `json:"auth_ref,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
}

func targetsFilePath() string {
	return filepath.Join(daemon.StateDir(), targetStoreFilename)
}

func loadSavedTargetProfiles() ([]savedTargetProfile, error) {
	path := targetsFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file savedTargetFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode targets file %s: %w", path, err)
	}
	profiles := make([]savedTargetProfile, 0, len(file.Targets))
	for _, profile := range file.Targets {
		normalized, err := normalizeSavedTargetProfile(profile)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, normalized)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})
	return profiles, nil
}

func saveSavedTargetProfiles(profiles []savedTargetProfile) error {
	if err := daemon.EnsureStateDir(); err != nil {
		return err
	}
	normalized := make([]savedTargetProfile, 0, len(profiles))
	for _, profile := range profiles {
		item, err := normalizeSavedTargetProfile(profile)
		if err != nil {
			return err
		}
		normalized = append(normalized, item)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Name < normalized[j].Name
	})
	payload, err := json.MarshalIndent(savedTargetFile{Targets: normalized}, "", "  ")
	if err != nil {
		return err
	}
	path := targetsFilePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func getSavedTargetProfile(name string) (savedTargetProfile, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return savedTargetProfile{}, false, nil
	}
	profiles, err := loadSavedTargetProfiles()
	if err != nil {
		return savedTargetProfile{}, false, err
	}
	for _, profile := range profiles {
		if strings.EqualFold(profile.Name, name) {
			return profile, true, nil
		}
	}
	return savedTargetProfile{}, false, nil
}

func addSavedTargetProfile(profile savedTargetProfile) error {
	profile, err := normalizeSavedTargetProfile(profile)
	if err != nil {
		return err
	}
	profiles, err := loadSavedTargetProfiles()
	if err != nil {
		return err
	}
	for _, item := range profiles {
		if strings.EqualFold(item.Name, profile.Name) {
			return fmt.Errorf("remote %q already exists", profile.Name)
		}
	}
	profiles = append(profiles, profile)
	return saveSavedTargetProfiles(profiles)
}

func removeSavedTargetProfile(name string) (savedTargetProfile, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return savedTargetProfile{}, false, fmt.Errorf("remote name is required")
	}
	profiles, err := loadSavedTargetProfiles()
	if err != nil {
		return savedTargetProfile{}, false, err
	}
	filtered := make([]savedTargetProfile, 0, len(profiles))
	var removed savedTargetProfile
	found := false
	for _, profile := range profiles {
		if strings.EqualFold(profile.Name, name) {
			removed = profile
			found = true
			continue
		}
		filtered = append(filtered, profile)
	}
	if !found {
		return savedTargetProfile{}, false, nil
	}
	if err := saveSavedTargetProfiles(filtered); err != nil {
		return savedTargetProfile{}, false, err
	}
	return removed, true, nil
}

func updateSavedTargetProfile(name string, update func(savedTargetProfile) (savedTargetProfile, error)) (savedTargetProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return savedTargetProfile{}, fmt.Errorf("remote name is required")
	}
	profiles, err := loadSavedTargetProfiles()
	if err != nil {
		return savedTargetProfile{}, err
	}
	found := false
	var updated savedTargetProfile
	for i := range profiles {
		if !strings.EqualFold(profiles[i].Name, name) {
			continue
		}
		next, err := update(profiles[i])
		if err != nil {
			return savedTargetProfile{}, err
		}
		next, err = normalizeSavedTargetProfile(next)
		if err != nil {
			return savedTargetProfile{}, err
		}
		profiles[i] = next
		updated = next
		found = true
		break
	}
	if !found {
		return savedTargetProfile{}, fmt.Errorf("remote %q not found", name)
	}
	if err := saveSavedTargetProfiles(profiles); err != nil {
		return savedTargetProfile{}, err
	}
	return updated, nil
}

func normalizeSavedTargetProfile(profile savedTargetProfile) (savedTargetProfile, error) {
	name := strings.TrimSpace(profile.Name)
	if err := validateManagedTargetName(name); err != nil {
		return savedTargetProfile{}, err
	}
	baseURL, err := normalizeManagedTargetURL(profile.BaseURL)
	if err != nil {
		return savedTargetProfile{}, err
	}
	authType := strings.ToLower(strings.TrimSpace(profile.AuthType))
	if authType == "" {
		authType = targetAuthTypeNone
	}
	switch authType {
	case targetAuthTypeNone, targetAuthTypeBearer:
	default:
		return savedTargetProfile{}, fmt.Errorf("unsupported remote auth type %q", profile.AuthType)
	}
	authRef := strings.TrimSpace(profile.AuthRef)
	if authType == targetAuthTypeNone {
		authRef = ""
	}
	if profile.Insecure {
		parsed, err := parseGatewayURL(baseURL)
		if err != nil {
			return savedTargetProfile{}, err
		}
		if !strings.EqualFold(strings.TrimSpace(parsed.Scheme), "https") {
			return savedTargetProfile{}, fmt.Errorf("remote %q can only use insecure TLS with an https base URL", name)
		}
	}
	return savedTargetProfile{
		Name:     name,
		Kind:     targetKindRemote,
		BaseURL:  baseURL,
		AuthType: authType,
		AuthRef:  authRef,
		Insecure: profile.Insecure,
	}, nil
}

func validateManagedTargetName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remote name is required")
	}
	switch strings.ToLower(name) {
	case localTargetName, "standalone", "list", "login", "logout":
		return fmt.Errorf("remote name %q is reserved", name)
	}
	if !targetNamePattern.MatchString(name) {
		return fmt.Errorf("remote name %q is invalid; use letters, numbers, '.', '_' or '-'", name)
	}
	return nil
}

func normalizeSuggestedTargetName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
			lastDash = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case r == '.' || r == '_' || r == '-':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		return ""
	}
	return name
}

func normalizeManagedTargetURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("remote URL is required")
	}
	if !strings.Contains(value, "://") {
		if isLikelyLoopbackAddress(value) {
			value = "http://" + value
		} else {
			value = "https://" + value
		}
	}
	parsed, err := parseGatewayURL(value)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("remote URL %q is missing a host", raw)
	}
	switch scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("remote URL %q must use http or https", raw)
	}
	if !isLoopbackHost(host) && scheme != "https" {
		return "", fmt.Errorf("remote URL %q must use https for non-local hosts", raw)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func parseGatewayURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("parse remote URL %q: %w", value, err)
	}
	if strings.TrimSpace(parsed.Scheme) == "" {
		return nil, fmt.Errorf("remote URL %q must include a scheme", value)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("remote URL %q must include a host", value)
	}
	return parsed, nil
}

func isLikelyLoopbackAddress(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return false
		}
		return isLoopbackHost(parsed.Hostname())
	}
	host := value
	if strings.Contains(value, "/") {
		host = strings.SplitN(value, "/", 2)[0]
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return isLoopbackHost(host)
}

func managedTargetSecretKey(name string) string {
	return managedTargetSecretPrefix + strings.ToLower(strings.TrimSpace(name)) + ".bearer"
}

func saveManagedTargetSecret(name, value string) (string, error) {
	key := managedTargetSecretKey(name)
	if err := keychain.SaveSecret(key, value); err != nil {
		return "", err
	}
	return "keychain:" + key, nil
}

func deleteManagedTargetSecret(authRef string) error {
	authRef = strings.TrimSpace(authRef)
	if !strings.HasPrefix(authRef, "keychain:"+managedTargetSecretPrefix) {
		return nil
	}
	key := strings.TrimPrefix(authRef, "keychain:")
	if err := keychain.DeleteSecret(key); err != nil {
		if strings.Contains(err.Error(), keychain.ErrNotFound.Error()) {
			return nil
		}
		return err
	}
	return nil
}
