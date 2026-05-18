package browserd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ---------------------------------------------------------------------------
// Browser Profile Management
// ---------------------------------------------------------------------------

const defaultProfileName = "default"

// browserProfile represents a named, persistent browser profile.
type browserProfile struct {
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	Driver    string `json:"driver"`            // "local" | "remote-cdp"
	CDPUrl    string `json:"cdp_url,omitempty"` // remote CDP endpoint
	CreatedAt string `json:"created_at,omitempty"`
}

// profileStore manages persistent browser profiles on disk.
type profileStore struct {
	mu       sync.RWMutex
	dir      string                     // base directory for profiles
	profiles map[string]*browserProfile // name → profile
}

// profileColors is a palette for visually distinguishing profiles.
var profileColors = []string{
	"#4A90D9", "#50C878", "#FF6B6B", "#FFB347",
	"#9B59B6", "#1ABC9C", "#E74C3C", "#F39C12",
}

func newProfileStore(baseDir string) (*profileStore, error) {
	dir := filepath.Join(baseDir, "browser", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create profile dir: %w", err)
	}

	ps := &profileStore{
		dir:      dir,
		profiles: make(map[string]*browserProfile),
	}
	_ = ps.load()
	return ps, nil
}

// UserDataDir returns the Chrome user-data directory for a profile.
func (ps *profileStore) UserDataDir(profileName string) string {
	return filepath.Join(ps.dir, profileName, "user-data")
}

// Create creates a new named profile.
func (ps *profileStore) Create(name string, opts browserProfile) (*browserProfile, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, exists := ps.profiles[name]; exists {
		return nil, fmt.Errorf("profile %q already exists", name)
	}

	p := &browserProfile{
		Name:   name,
		Color:  opts.Color,
		Driver: "local",
	}
	if opts.CDPUrl != "" {
		p.Driver = "remote-cdp"
		p.CDPUrl = opts.CDPUrl
	}
	if p.Color == "" {
		p.Color = ps.nextColor()
	}

	if p.Driver == "local" {
		if err := os.MkdirAll(ps.UserDataDir(name), 0o755); err != nil {
			return nil, fmt.Errorf("create user-data dir: %w", err)
		}
	}

	ps.profiles[name] = p
	_ = ps.save()
	return p, nil
}

// Delete removes a profile. The default profile cannot be deleted.
func (ps *profileStore) Delete(name string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if name == defaultProfileName {
		return fmt.Errorf("cannot delete default profile")
	}
	if _, exists := ps.profiles[name]; !exists {
		return fmt.Errorf("profile %q not found", name)
	}

	// Move to trash instead of hard delete.
	trashDir := filepath.Join(ps.dir, ".trash", name)
	_ = os.MkdirAll(filepath.Dir(trashDir), 0o755)
	if err := os.Rename(filepath.Join(ps.dir, name), trashDir); err != nil {
		_ = os.RemoveAll(filepath.Join(ps.dir, name))
	}

	delete(ps.profiles, name)
	_ = ps.save()
	return nil
}

// List returns all registered profiles.
func (ps *profileStore) List() []*browserProfile {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	result := make([]*browserProfile, 0, len(ps.profiles))
	for _, p := range ps.profiles {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Get returns a profile by name.
func (ps *profileStore) Get(name string) (*browserProfile, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.profiles[name]
	return p, ok
}

func (ps *profileStore) load() error {
	data, err := os.ReadFile(filepath.Join(ps.dir, "profiles.json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &ps.profiles)
}

func (ps *profileStore) save() error {
	data, _ := json.MarshalIndent(ps.profiles, "", "  ")
	return os.WriteFile(filepath.Join(ps.dir, "profiles.json"), data, 0o644)
}

func (ps *profileStore) nextColor() string {
	used := make(map[string]bool)
	for _, p := range ps.profiles {
		used[p.Color] = true
	}
	for _, c := range profileColors {
		if !used[c] {
			return c
		}
	}
	return profileColors[0]
}
