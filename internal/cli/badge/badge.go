package badge

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

const (
	letterSlotCount   = 26
	customSlotCount   = 24
	internalImageSize = 64
	minSizeCells      = 2
	maxSizeCells      = 6
	defaultCurrent    = "A"
	defaultColor      = "#00ff88"
	defaultSize       = 3
)

// SlotKind distinguishes between letter and custom badge slots.
type SlotKind int

const (
	SlotLetter SlotKind = iota
	SlotCustom
)

// Slot describes a single badge slot.
type Slot struct {
	Kind     SlotKind
	Index    int
	Occupied bool
}

// Config stores the persisted badge selection state.
type Config struct {
	Enabled bool   `json:"enabled"`
	Current string `json:"current"`
	Color   string `json:"color"`
	Size    int    `json:"size"`
}

// Manager owns badge configuration, storage, and protocol detection state.
type Manager struct {
	config     Config
	configPath string
	badgeDir   string
	protocol   richedit.ImageProtocol
}

// NewManager resolves the default HopClaw badge paths under ~/.hopclaw.
func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	root := filepath.Join(homeDir, ".hopclaw")
	return NewManagerWithPaths(filepath.Join(root, "avatar.json"), filepath.Join(root, "avatars")), nil
}

// NewManagerWithPaths returns a manager backed by explicit storage paths.
func NewManagerWithPaths(configPath, badgeDir string) *Manager {
	return &Manager{
		config:     DefaultConfig(),
		configPath: configPath,
		badgeDir:   badgeDir,
	}
}

// DefaultConfig returns the persisted defaults for badge settings.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Current: defaultCurrent,
		Color:   defaultColor,
		Size:    defaultSize,
	}
}

// Config returns a copy of the current in-memory badge configuration.
func (m *Manager) Config() Config {
	return m.config
}

// Protocol returns the most recently detected terminal image protocol.
func (m *Manager) Protocol() richedit.ImageProtocol {
	return m.protocol
}

// Current returns the ID of the currently selected badge.
func (m *Manager) Current() string {
	return m.config.Current
}

// Enabled reports whether the badge should be shown by default in new sessions.
func (m *Manager) Enabled() bool {
	return m.config.Enabled
}

// SetEnabled stores the persisted default visibility for future sessions.
func (m *Manager) SetEnabled(enabled bool) {
	m.config.Enabled = enabled
}

// SetCurrent switches the active badge to a letter A-Z or a populated custom slot.
func (m *Manager) SetCurrent(id string) error {
	kind, index, normalized, err := parseBadgeID(id)
	if err != nil {
		return err
	}
	if kind == SlotCustom && !m.customSlotOccupied(index) {
		return fmt.Errorf("custom slot %d is empty", index)
	}
	m.config.Current = normalized
	return nil
}

// SetColor validates and stores the configured badge tint as #rrggbb.
func (m *Manager) SetColor(hex string) error {
	normalized, _, err := normalizeHexColor(hex)
	if err != nil {
		return err
	}
	m.config.Color = normalized
	return nil
}

// SetSize validates and stores the configured badge width in terminal cells.
func (m *Manager) SetSize(cells int) error {
	if cells < minSizeCells || cells > maxSizeCells {
		return fmt.Errorf("badge size must be between %d and %d cells", minSizeCells, maxSizeCells)
	}
	m.config.Size = cells
	return nil
}

func (m *Manager) sanitizeConfig(cfg Config) Config {
	sanitized := DefaultConfig()
	sanitized.Enabled = cfg.Enabled

	if normalized, _, err := normalizeHexColor(cfg.Color); err == nil {
		sanitized.Color = normalized
	}
	if cfg.Size >= minSizeCells && cfg.Size <= maxSizeCells {
		sanitized.Size = cfg.Size
	}
	if _, index, normalized, err := parseBadgeID(cfg.Current); err == nil {
		if strings.HasPrefix(normalized, "custom-") {
			if m.customSlotOccupied(index) {
				sanitized.Current = normalized
			}
		} else {
			sanitized.Current = normalized
		}
	}

	return sanitized
}

func (m *Manager) ensureStorageDirs() error {
	if strings.TrimSpace(m.configPath) == "" {
		return fmt.Errorf("badge config path is not configured")
	}
	if strings.TrimSpace(m.badgeDir) == "" {
		return fmt.Errorf("badge directory is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o755); err != nil {
		return fmt.Errorf("create badge config directory: %w", err)
	}
	if err := os.MkdirAll(m.badgeDir, 0o755); err != nil {
		return fmt.Errorf("create badge directory: %w", err)
	}
	return nil
}

func (m *Manager) customSlotPath(slot int) string {
	return filepath.Join(m.badgeDir, customSlotFilename(slot))
}

func (m *Manager) customSlotOccupied(slot int) bool {
	if slot < 0 || slot >= customSlotCount {
		return false
	}
	info, err := os.Stat(m.customSlotPath(slot))
	return err == nil && !info.IsDir()
}

func validateCustomSlot(slot int) error {
	if slot < 0 || slot >= customSlotCount {
		return fmt.Errorf("custom slot must be between 0 and %d", customSlotCount-1)
	}
	return nil
}

func customSlotFilename(slot int) string {
	return fmt.Sprintf("custom-%d.png", slot)
}

func parseBadgeID(id string) (SlotKind, int, string, error) {
	id = strings.TrimSpace(id)
	if len(id) == 1 {
		upper := strings.ToUpper(id)
		r := rune(upper[0])
		if r >= 'A' && r <= 'Z' {
			return SlotLetter, int(r - 'A'), upper, nil
		}
	}
	if strings.HasPrefix(strings.ToLower(id), "custom-") {
		index, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(id), "custom-"))
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid badge id %q", id)
		}
		if err := validateCustomSlot(index); err != nil {
			return 0, 0, "", err
		}
		return SlotCustom, index, fmt.Sprintf("custom-%d", index), nil
	}
	return 0, 0, "", fmt.Errorf("invalid badge id %q", id)
}

func normalizeHexColor(value string) (string, color.RGBA, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", color.RGBA{}, fmt.Errorf("badge color is required")
	}
	if !strings.HasPrefix(value, "#") {
		return "", color.RGBA{}, fmt.Errorf("badge color must use #rgb or #rrggbb")
	}

	switch len(value) {
	case 4:
		value = "#" + strings.Repeat(string(value[1]), 2) +
			strings.Repeat(string(value[2]), 2) +
			strings.Repeat(string(value[3]), 2)
	case 7:
	default:
		return "", color.RGBA{}, fmt.Errorf("badge color must use #rgb or #rrggbb")
	}

	parsed, err := strconv.ParseUint(value[1:], 16, 32)
	if err != nil {
		return "", color.RGBA{}, fmt.Errorf("badge color must use #rgb or #rrggbb")
	}
	return value, color.RGBA{
		R: uint8(parsed >> 16),
		G: uint8(parsed >> 8),
		B: uint8(parsed),
		A: 0xff,
	}, nil
}

func tintMonochromeImage(src image.Image, fill color.RGBA) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	if bounds.Empty() {
		return dst
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			gray := luminance8(r, g, b)
			alpha := uint8((a * 255) / 0xffff)
			dst.SetRGBA(x-bounds.Min.X, y-bounds.Min.Y, color.RGBA{
				R: uint8((uint16(fill.R) * uint16(gray)) / 255),
				G: uint8((uint16(fill.G) * uint16(gray)) / 255),
				B: uint8((uint16(fill.B) * uint16(gray)) / 255),
				A: alpha,
			})
		}
	}
	return dst
}

func luminance8(r, g, b uint32) uint8 {
	r8 := uint32((r * 255) / 0xffff)
	g8 := uint32((g * 255) / 0xffff)
	b8 := uint32((b * 255) / 0xffff)
	return uint8((299*r8 + 587*g8 + 114*b8 + 500) / 1000)
}
