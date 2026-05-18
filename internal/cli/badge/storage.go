package badge

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

// Load reads badge configuration from disk and refreshes terminal protocol support.
func (m *Manager) Load() error {
	if err := m.ensureStorageDirs(); err != nil {
		return err
	}

	m.protocol = richedit.DetectImageProtocol()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			m.config = DefaultConfig()
			return nil
		}
		return fmt.Errorf("read badge config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse badge config: %w", err)
	}

	m.config = m.sanitizeConfig(cfg)
	return nil
}

// Save persists the current badge configuration to disk.
func (m *Manager) Save() error {
	if err := m.ensureStorageDirs(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal badge config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(m.configPath, data, 0o644); err != nil {
		return fmt.Errorf("write badge config: %w", err)
	}
	return nil
}

// ImportImage imports a PNG or JPEG into a fixed custom slot.
func (m *Manager) ImportImage(slot int, path string) error {
	if err := validateCustomSlot(slot); err != nil {
		return err
	}
	if err := m.ensureStorageDirs(); err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open badge image %q: %w", path, err)
	}
	defer file.Close()

	src, format, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("decode badge image %q: %w", path, err)
	}
	if format != "png" && format != "jpeg" {
		return fmt.Errorf("badge image %q must be a PNG or JPEG", path)
	}

	scaled := scaleImageNearest(src, internalImageSize, internalImageSize)
	monochrome := thresholdImage(scaled)

	out, err := os.Create(m.customSlotPath(slot))
	if err != nil {
		return fmt.Errorf("create badge slot %d: %w", slot, err)
	}
	defer out.Close()

	if err := png.Encode(out, monochrome); err != nil {
		return fmt.Errorf("encode badge slot %d: %w", slot, err)
	}
	return nil
}

// RemoveImage deletes an imported image from a fixed custom slot.
func (m *Manager) RemoveImage(slot int) error {
	if err := validateCustomSlot(slot); err != nil {
		return err
	}
	if err := m.ensureStorageDirs(); err != nil {
		return err
	}

	err := os.Remove(m.customSlotPath(slot))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove badge slot %d: %w", slot, err)
	}
	if m.config.Current == fmt.Sprintf("custom-%d", slot) {
		m.config.Current = defaultCurrent
	}
	return nil
}

// ListSlots returns the full ordered slot inventory.
func (m *Manager) ListSlots() []Slot {
	slots := make([]Slot, 0, letterSlotCount+customSlotCount)
	for i := range letterSlotCount {
		slots = append(slots, Slot{
			Kind:     SlotLetter,
			Index:    i,
			Occupied: true,
		})
	}
	for i := range customSlotCount {
		slots = append(slots, Slot{
			Kind:     SlotCustom,
			Index:    i,
			Occupied: m.customSlotOccupied(i),
		})
	}
	return slots
}

// GetCurrentImage renders the currently selected badge using the configured color.
func (m *Manager) GetCurrentImage() (*image.RGBA, error) {
	return m.imageForID(m.config.Current, m.config.Color)
}

func (m *Manager) imageForID(id string, hexColor string) (*image.RGBA, error) {
	kind, index, normalized, err := parseBadgeID(m.config.Current)
	if err != nil {
		kind = SlotLetter
		index = 0
		normalized = defaultCurrent
	}

	if strings.TrimSpace(id) != "" {
		kind, index, normalized, err = parseBadgeID(id)
		if err != nil {
			kind = SlotLetter
			index = 0
			normalized = defaultCurrent
		}
	}

	_, fill, err := normalizeHexColor(hexColor)
	if err != nil {
		_, fill, _ = normalizeHexColor(defaultColor)
	}

	if kind == SlotLetter {
		return RenderLetter(rune('A'+index), hexColor), nil
	}
	if !m.customSlotOccupied(index) {
		return m.fallbackCustomImage(normalized, hexColor), nil
	}

	file, err := os.Open(m.customSlotPath(index))
	if err != nil {
		return m.fallbackCustomImage(normalized, hexColor), nil
	}
	defer file.Close()

	src, err := png.Decode(file)
	if err != nil {
		return m.fallbackCustomImage(normalized, hexColor), nil
	}
	return tintMonochromeImage(src, fill), nil
}

func (m *Manager) fallbackCustomImage(id string, hexColor string) *image.RGBA {
	if id == m.config.Current {
		m.config.Current = defaultCurrent
	}
	return RenderLetter('A', hexColor)
}

func scaleImageNearest(src image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	srcBounds := src.Bounds()
	if srcBounds.Empty() || width <= 0 || height <= 0 {
		return dst
	}

	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()
	for y := 0; y < height; y++ {
		srcY := srcBounds.Min.Y + (y*srcHeight)/height
		if srcY >= srcBounds.Max.Y {
			srcY = srcBounds.Max.Y - 1
		}
		for x := 0; x < width; x++ {
			srcX := srcBounds.Min.X + (x*srcWidth)/width
			if srcX >= srcBounds.Max.X {
				srcX = srcBounds.Max.X - 1
			}
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func thresholdImage(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if luminance8(r, g, b) >= 128 {
				continue
			}
			dst.SetRGBA(x-bounds.Min.X, y-bounds.Min.Y, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		}
	}
	return dst
}
