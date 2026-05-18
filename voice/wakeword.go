package voice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Wake Word Detection
// ---------------------------------------------------------------------------

var defaultWakeWords = []string{"hopclaw", "claude", "computer"}

// WakeWordConfig manages voice wake word settings.
type WakeWordConfig struct {
	mu       sync.RWMutex
	Words    []string `json:"words"`
	Enabled  bool     `json:"enabled"`
	filePath string
}

// LoadWakeWordConfig loads wake word configuration from disk.
func LoadWakeWordConfig() *WakeWordConfig {
	home, _ := os.UserHomeDir()
	fp := filepath.Join(home, ".hopclaw", "settings", "voicewake.json")

	cfg := &WakeWordConfig{
		Words:    defaultWakeWords,
		Enabled:  false,
		filePath: fp,
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, cfg)
	cfg.filePath = fp
	return cfg
}

// ContainsWakeWord checks if the given text contains any wake word.
func (c *WakeWordConfig) ContainsWakeWord(text string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.Enabled {
		return false
	}

	lower := strings.ToLower(text)
	for _, w := range c.Words {
		if strings.Contains(lower, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// SetWords updates the wake word list and persists to disk.
func (c *WakeWordConfig) SetWords(words []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Words = words
	return c.save()
}

// SetEnabled enables or disables wake word detection.
func (c *WakeWordConfig) SetEnabled(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Enabled = enabled
	return c.save()
}

// GetWords returns a copy of the current wake word list.
func (c *WakeWordConfig) GetWords() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]string, len(c.Words))
	copy(result, c.Words)
	return result
}

func (c *WakeWordConfig) save() error {
	dir := filepath.Dir(c.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(c.filePath, data, 0o644)
}
