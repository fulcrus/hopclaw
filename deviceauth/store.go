package deviceauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Store constants
// ---------------------------------------------------------------------------

const (
	storeVersion   = 1
	storeFilePerms = 0o600
	storeDirPerms  = 0o700
	storeFileName  = "device-auth.json"
	identitySubdir = "identity"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrNotFound indicates a requested record does not exist.
	ErrNotFound = fmt.Errorf("not found")

	// ErrVersionMismatch indicates the store file has an incompatible version.
	ErrVersionMismatch = fmt.Errorf("store version mismatch")
)

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store persists device identities and tokens to disk as JSON.
type Store struct {
	mu      sync.Mutex // guards data and file I/O
	dataDir string
	data    StoreData
	loaded  bool
}

// NewStore creates a new file-backed device auth store rooted at dataDir.
func NewStore(dataDir string) *Store {
	return &Store{
		dataDir: dataDir,
		data: StoreData{
			Version: storeVersion,
			Tokens:  make(map[string]*DeviceToken),
			Devices: make(map[string]*DeviceIdentity),
		},
	}
}

// ---------------------------------------------------------------------------
// Load / Save
// ---------------------------------------------------------------------------

// Load reads the store data from disk. If the file does not exist the store
// starts empty. Returns an error if the file exists but is corrupt or has an
// incompatible version.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			s.loaded = true
			return nil
		}
		return fmt.Errorf("read store file: %w", err)
	}

	var data StoreData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("unmarshal store data: %w", err)
	}
	if data.Version != storeVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrVersionMismatch, data.Version, storeVersion)
	}

	if data.Tokens == nil {
		data.Tokens = make(map[string]*DeviceToken)
	}
	if data.Devices == nil {
		data.Devices = make(map[string]*DeviceIdentity)
	}
	data.Tokens = normalizeTokenMap(data.Tokens)

	s.data = data
	s.loaded = true
	return nil
}

// Save writes the current store data to disk atomically by writing to a
// temporary file then renaming.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked performs the save while the caller already holds s.mu.
func (s *Store) saveLocked() error {
	if err := s.ensureDir(); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store data: %w", err)
	}

	target := s.filePath()
	tmp := target + ".tmp"

	if err := os.WriteFile(tmp, raw, storeFilePerms); err != nil {
		return fmt.Errorf("write temp store file: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		logging.DebugIfErr(os.Remove(tmp), "remove temp device store failed")
		return fmt.Errorf("rename store file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Token CRUD
// ---------------------------------------------------------------------------

// GetToken returns the token for the given device/role pair, if one exists.
func (s *Store) GetToken(deviceID string, role DeviceRole) (*DeviceToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.data.Tokens[tokenMapKey(deviceID, role)]
	return t, ok
}

// SetToken stores a device token, keyed by its device/role pair, and persists
// to disk.
func (s *Store) SetToken(token *DeviceToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	token.DeviceID = strings.TrimSpace(token.DeviceID)
	token.Role = DeviceRole(strings.TrimSpace(string(token.Role)))
	if token.DeviceID == "" {
		return fmt.Errorf("device id is required")
	}
	if !IsValidRole(token.Role) {
		return fmt.Errorf("invalid role %q", token.Role)
	}
	token.UpdatedAt = time.Now().UTC()
	s.data.Tokens[tokenMapKey(token.DeviceID, token.Role)] = token
	return s.saveLocked()
}

// DeleteToken removes the token for the given device/role pair and persists
// to disk.
func (s *Store) DeleteToken(deviceID string, role DeviceRole) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := tokenMapKey(deviceID, role)
	if _, ok := s.data.Tokens[key]; !ok {
		return fmt.Errorf("token for device %q role %q: %w", deviceID, role, ErrNotFound)
	}
	delete(s.data.Tokens, key)
	return s.saveLocked()
}

// ListTokens returns tokens for a device. When deviceID is empty, it returns
// every stored token.
func (s *Store) ListTokens(deviceID string) []*DeviceToken {
	s.mu.Lock()
	defer s.mu.Unlock()

	deviceID = strings.TrimSpace(deviceID)
	result := make([]*DeviceToken, 0, len(s.data.Tokens))
	for _, token := range s.data.Tokens {
		if token == nil {
			continue
		}
		if deviceID != "" && token.DeviceID != deviceID {
			continue
		}
		result = append(result, token)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].DeviceID != result[j].DeviceID {
			return result[i].DeviceID < result[j].DeviceID
		}
		return result[i].Role < result[j].Role
	})
	return result
}

// ---------------------------------------------------------------------------
// Device CRUD
// ---------------------------------------------------------------------------

// PrimaryDeviceID returns the store's preferred local device identifier.
func (s *Store) PrimaryDeviceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.data.DeviceID)
}

// SetPrimaryDeviceID stores the preferred local device identifier and persists it.
func (s *Store) SetPrimaryDeviceID(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return fmt.Errorf("device id is required")
	}
	s.data.DeviceID = deviceID
	return s.saveLocked()
}

// RegisterDevice adds a device identity to the store and persists to disk.
func (s *Store) RegisterDevice(device *DeviceIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	device.DeviceID = strings.TrimSpace(device.DeviceID)
	if device.DeviceID == "" {
		return fmt.Errorf("device id is required")
	}
	if existing, ok := s.data.Devices[device.DeviceID]; ok && existing != nil {
		if strings.TrimSpace(device.Name) == "" {
			device.Name = existing.Name
		}
		if strings.TrimSpace(device.Platform) == "" {
			device.Platform = existing.Platform
		}
		if strings.TrimSpace(device.DeviceFamily) == "" {
			device.DeviceFamily = existing.DeviceFamily
		}
		if device.CreatedAt.IsZero() {
			device.CreatedAt = existing.CreatedAt
		}
		if device.LastSeenAt.IsZero() {
			device.LastSeenAt = existing.LastSeenAt
		}
		if !device.Trusted {
			device.Trusted = existing.Trusted
		}
	}
	if device.CreatedAt.IsZero() {
		device.CreatedAt = time.Now().UTC()
	}
	if device.LastSeenAt.IsZero() {
		device.LastSeenAt = device.CreatedAt
	}
	s.data.Devices[device.DeviceID] = device
	return s.saveLocked()
}

// GetDevice returns the device identity for the given ID.
func (s *Store) GetDevice(deviceID string) (*DeviceIdentity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.data.Devices[deviceID]
	return d, ok
}

// UpdateLastSeen updates the LastSeenAt timestamp for a device.
func (s *Store) UpdateLastSeen(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.data.Devices[deviceID]
	if !ok {
		return fmt.Errorf("device %q: %w", deviceID, ErrNotFound)
	}
	d.LastSeenAt = time.Now().UTC()
	return s.saveLocked()
}

// ListDevices returns all registered device identities.
func (s *Store) ListDevices() []*DeviceIdentity {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*DeviceIdentity, 0, len(s.data.Devices))
	for _, d := range s.data.Devices {
		result = append(result, d)
	}
	return result
}

// TrustDevice marks a device as trusted and persists to disk.
func (s *Store) TrustDevice(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.data.Devices[deviceID]
	if !ok {
		return fmt.Errorf("device %q: %w", deviceID, ErrNotFound)
	}
	d.Trusted = true
	return s.saveLocked()
}

// RevokeDevice marks a device as untrusted and persists to disk.
func (s *Store) RevokeDevice(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.data.Devices[deviceID]
	if !ok {
		return fmt.Errorf("device %q: %w", deviceID, ErrNotFound)
	}
	d.Trusted = false
	return s.saveLocked()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *Store) filePath() string {
	return filepath.Join(s.dataDir, identitySubdir, storeFileName)
}

func (s *Store) ensureDir() error {
	dir := filepath.Join(s.dataDir, identitySubdir)
	if err := os.MkdirAll(dir, storeDirPerms); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	return nil
}

func tokenMapKey(deviceID string, role DeviceRole) string {
	return strings.TrimSpace(deviceID) + ":" + strings.TrimSpace(string(role))
}

func normalizeTokenMap(tokens map[string]*DeviceToken) map[string]*DeviceToken {
	if len(tokens) == 0 {
		return make(map[string]*DeviceToken)
	}
	out := make(map[string]*DeviceToken, len(tokens))
	for key, token := range tokens {
		if token == nil {
			continue
		}
		token.DeviceID = strings.TrimSpace(token.DeviceID)
		if token.DeviceID == "" {
			continue
		}
		if !IsValidRole(token.Role) {
			role := DeviceRole(strings.TrimSpace(key))
			if IsValidRole(role) {
				token.Role = role
			}
		}
		if !IsValidRole(token.Role) {
			continue
		}
		out[tokenMapKey(token.DeviceID, token.Role)] = token
	}
	return out
}
