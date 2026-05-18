package mediagen

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

var (
	imageResolutionOrder = []string{"1K", "2K", "4K"}
	videoResolutionOrder = []string{"360P", "480P", "720P", "1080P", "2K", "4K"}
)

type IgnoredOverride struct {
	Key   string
	Value any
}

type StringNormalization struct {
	Requested       string
	Applied         string
	DerivedFrom     string
	SupportedValues []string
}

type IntNormalization struct {
	Requested       int
	Applied         int
	SupportedValues []int
}

type ImageNormalization struct {
	Size        *StringNormalization
	AspectRatio *StringNormalization
	Resolution  *StringNormalization
}

type VideoNormalization struct {
	Size            *StringNormalization
	AspectRatio     *StringNormalization
	Resolution      *StringNormalization
	DurationSeconds *IntNormalization
}

type MusicNormalization struct {
	Format          *StringNormalization
	DurationSeconds *IntNormalization
}

type NormalizedImageRequest struct {
	Request       ImageRequest
	Normalization ImageNormalization
	Ignored       []IgnoredOverride
}

type NormalizedVideoRequest struct {
	Request       VideoRequest
	Normalization VideoNormalization
	Ignored       []IgnoredOverride
}

type NormalizedMusicRequest struct {
	Request       MusicRequest
	Normalization MusicNormalization
	Ignored       []IgnoredOverride
}

func NormalizeImageRequest(provider ImageProvider, req ImageRequest) NormalizedImageRequest {
	resolved := NormalizedImageRequest{
		Request: ImageRequest{
			Provider:    strings.TrimSpace(req.Provider),
			Model:       strings.TrimSpace(req.Model),
			Prompt:      strings.TrimSpace(req.Prompt),
			Count:       req.Count,
			Size:        strings.TrimSpace(req.Size),
			AspectRatio: strings.TrimSpace(req.AspectRatio),
			Resolution:  strings.TrimSpace(req.Resolution),
			InputImages: append([]InputAsset(nil), req.InputImages...),
			TimeoutMS:   req.TimeoutMS,
		},
	}

	if provider == nil {
		return resolved
	}
	caps := provider.ImageCapabilities()
	resolved.Request.Size, resolved.Request.AspectRatio, resolved.Request.Resolution =
		normalizeGeometry(
			resolved.Request.Size,
			resolved.Request.AspectRatio,
			resolved.Request.Resolution,
			geometryCapabilities{
				SupportsSize:        caps.SupportsSize,
				SupportsAspectRatio: caps.SupportsAspectRatio,
				SupportsResolution:  caps.SupportsResolution,
				Sizes:               caps.Sizes,
				AspectRatios:        caps.AspectRatios,
				Resolutions:         caps.Resolutions,
				ResolutionOrder:     imageResolutionOrder,
			},
			&resolved.Normalization.Size,
			&resolved.Normalization.AspectRatio,
			&resolved.Normalization.Resolution,
			&resolved.Ignored,
		)

	return resolved
}

func NormalizeVideoRequest(provider VideoProvider, req VideoRequest) NormalizedVideoRequest {
	resolved := NormalizedVideoRequest{
		Request: VideoRequest{
			Provider:        strings.TrimSpace(req.Provider),
			Model:           strings.TrimSpace(req.Model),
			Prompt:          strings.TrimSpace(req.Prompt),
			DurationSeconds: req.DurationSeconds,
			Size:            strings.TrimSpace(req.Size),
			AspectRatio:     strings.TrimSpace(req.AspectRatio),
			Resolution:      strings.TrimSpace(req.Resolution),
			Audio:           req.Audio,
			InputImages:     append([]InputAsset(nil), req.InputImages...),
			InputVideos:     append([]InputAsset(nil), req.InputVideos...),
			TimeoutMS:       req.TimeoutMS,
		},
	}
	if provider == nil {
		return resolved
	}
	caps := provider.VideoCapabilities()
	resolved.Request.Size, resolved.Request.AspectRatio, resolved.Request.Resolution =
		normalizeGeometry(
			resolved.Request.Size,
			resolved.Request.AspectRatio,
			resolved.Request.Resolution,
			geometryCapabilities{
				SupportsSize:        caps.SupportsSize,
				SupportsAspectRatio: caps.SupportsAspectRatio,
				SupportsResolution:  caps.SupportsResolution,
				Sizes:               caps.Sizes,
				AspectRatios:        caps.AspectRatios,
				Resolutions:         caps.Resolutions,
				ResolutionOrder:     videoResolutionOrder,
			},
			&resolved.Normalization.Size,
			&resolved.Normalization.AspectRatio,
			&resolved.Normalization.Resolution,
			&resolved.Ignored,
		)
	if resolved.Request.Audio && !caps.SupportsAudio {
		resolved.Ignored = append(resolved.Ignored, IgnoredOverride{Key: "audio", Value: true})
		resolved.Request.Audio = false
	}
	if resolved.Request.DurationSeconds > 0 {
		applied, changed := normalizeDuration(
			resolved.Request.DurationSeconds,
			caps.SupportedDurations,
			caps.MaxDurationSeconds,
		)
		if changed {
			resolved.Normalization.DurationSeconds = &IntNormalization{
				Requested:       resolved.Request.DurationSeconds,
				Applied:         applied,
				SupportedValues: cloneInts(caps.SupportedDurations),
			}
		}
		resolved.Request.DurationSeconds = applied
	}
	return resolved
}

func NormalizeMusicRequest(provider MusicProvider, req MusicRequest) NormalizedMusicRequest {
	resolved := NormalizedMusicRequest{
		Request: MusicRequest{
			Provider:        strings.TrimSpace(req.Provider),
			Model:           strings.TrimSpace(req.Model),
			Prompt:          strings.TrimSpace(req.Prompt),
			Lyrics:          strings.TrimSpace(req.Lyrics),
			Instrumental:    req.Instrumental,
			DurationSeconds: req.DurationSeconds,
			Format:          strings.TrimSpace(req.Format),
			InputImages:     append([]InputAsset(nil), req.InputImages...),
			TimeoutMS:       req.TimeoutMS,
		},
	}
	if provider == nil {
		return resolved
	}
	caps := provider.MusicCapabilities()
	if resolved.Request.Format != "" {
		switch {
		case !caps.SupportsFormat:
			resolved.Ignored = append(resolved.Ignored, IgnoredOverride{Key: "format", Value: resolved.Request.Format})
			resolved.Request.Format = ""
		case len(caps.Formats) > 0:
			applied, changed := normalizeFormat(resolved.Request.Format, caps.Formats)
			if changed {
				resolved.Normalization.Format = &StringNormalization{
					Requested:       resolved.Request.Format,
					Applied:         applied,
					SupportedValues: cloneStrings(caps.Formats),
				}
			}
			resolved.Request.Format = applied
		}
	}
	if resolved.Request.DurationSeconds > 0 {
		switch {
		case !caps.SupportsDuration:
			resolved.Ignored = append(resolved.Ignored, IgnoredOverride{Key: "duration_seconds", Value: resolved.Request.DurationSeconds})
			resolved.Request.DurationSeconds = 0
		default:
			applied, changed := normalizeDuration(
				resolved.Request.DurationSeconds,
				caps.SupportedDurations,
				caps.MaxDurationSeconds,
			)
			if changed {
				resolved.Normalization.DurationSeconds = &IntNormalization{
					Requested:       resolved.Request.DurationSeconds,
					Applied:         applied,
					SupportedValues: cloneInts(caps.SupportedDurations),
				}
			}
			resolved.Request.DurationSeconds = applied
		}
	}
	return resolved
}

func (n NormalizedImageRequest) Metadata() map[string]any {
	return buildNormalizationMetadata(n.Normalization.metadata(), ignoredOverridesMetadata(n.Ignored))
}

func (n NormalizedVideoRequest) Metadata() map[string]any {
	return buildNormalizationMetadata(n.Normalization.metadata(), ignoredOverridesMetadata(n.Ignored))
}

func (n NormalizedMusicRequest) Metadata() map[string]any {
	return buildNormalizationMetadata(n.Normalization.metadata(), ignoredOverridesMetadata(n.Ignored))
}

func (n ImageNormalization) metadata() map[string]any {
	out := map[string]any{}
	if entry := stringNormalizationMetadata(n.Size); len(entry) > 0 {
		out["size"] = entry
	}
	if entry := stringNormalizationMetadata(n.AspectRatio); len(entry) > 0 {
		out["aspect_ratio"] = entry
	}
	if entry := stringNormalizationMetadata(n.Resolution); len(entry) > 0 {
		out["resolution"] = entry
	}
	return out
}

func (n VideoNormalization) metadata() map[string]any {
	out := map[string]any{}
	if entry := stringNormalizationMetadata(n.Size); len(entry) > 0 {
		out["size"] = entry
	}
	if entry := stringNormalizationMetadata(n.AspectRatio); len(entry) > 0 {
		out["aspect_ratio"] = entry
	}
	if entry := stringNormalizationMetadata(n.Resolution); len(entry) > 0 {
		out["resolution"] = entry
	}
	if entry := intNormalizationMetadata(n.DurationSeconds); len(entry) > 0 {
		out["duration_seconds"] = entry
	}
	return out
}

func (n MusicNormalization) metadata() map[string]any {
	out := map[string]any{}
	if entry := stringNormalizationMetadata(n.Format); len(entry) > 0 {
		out["format"] = entry
	}
	if entry := intNormalizationMetadata(n.DurationSeconds); len(entry) > 0 {
		out["duration_seconds"] = entry
	}
	return out
}

type geometryCapabilities struct {
	SupportsSize        bool
	SupportsAspectRatio bool
	SupportsResolution  bool
	Sizes               []string
	AspectRatios        []string
	Resolutions         []string
	ResolutionOrder     []string
}

func normalizeGeometry(
	size string,
	aspectRatio string,
	resolution string,
	caps geometryCapabilities,
	sizeNorm **StringNormalization,
	aspectNorm **StringNormalization,
	resolutionNorm **StringNormalization,
	ignored *[]IgnoredOverride,
) (string, string, string) {
	if size != "" {
		if caps.SupportsSize {
			applied := resolveClosestSize(size, aspectRatio, caps.Sizes)
			if applied != "" {
				if applied != size {
					*sizeNorm = &StringNormalization{Requested: size, Applied: applied}
				}
				size = applied
			}
		} else {
			translated := false
			if caps.SupportsAspectRatio && aspectRatio == "" {
				if applied := resolveClosestAspectRatio("", size, caps.AspectRatios); applied != "" {
					*aspectNorm = &StringNormalization{Applied: applied, DerivedFrom: "size"}
					aspectRatio = applied
					translated = true
				}
			}
			if !translated {
				*ignored = append(*ignored, IgnoredOverride{Key: "size", Value: size})
			}
			size = ""
		}
	}

	if aspectRatio != "" {
		if caps.SupportsAspectRatio {
			applied := resolveClosestAspectRatio(aspectRatio, size, caps.AspectRatios)
			if applied != "" {
				if *aspectNorm == nil && applied != aspectRatio {
					*aspectNorm = &StringNormalization{Requested: aspectRatio, Applied: applied}
				}
				aspectRatio = applied
			}
		} else {
			translated := false
			if size == "" && caps.SupportsSize {
				if applied := resolveClosestSize("", aspectRatio, caps.Sizes); applied != "" {
					*sizeNorm = &StringNormalization{Applied: applied, DerivedFrom: "aspect_ratio"}
					size = applied
					translated = true
				}
			}
			if !translated {
				*ignored = append(*ignored, IgnoredOverride{Key: "aspect_ratio", Value: aspectRatio})
			}
			aspectRatio = ""
		}
	}

	if resolution != "" {
		if caps.SupportsResolution {
			applied := resolveClosestResolution(resolution, caps.Resolutions, caps.ResolutionOrder)
			if applied != "" {
				if applied != resolution {
					*resolutionNorm = &StringNormalization{
						Requested:       resolution,
						Applied:         applied,
						SupportedValues: cloneStrings(caps.Resolutions),
					}
				}
				resolution = applied
			}
		} else {
			*ignored = append(*ignored, IgnoredOverride{Key: "resolution", Value: resolution})
			resolution = ""
		}
	}

	return size, aspectRatio, resolution
}

type parsedAspectRatio struct {
	width  float64
	height float64
	value  float64
}

type parsedSize struct {
	width       float64
	height      float64
	aspectRatio float64
	area        float64
}

func parseAspectRatioValue(raw string) *parsedAspectRatio {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var width, height float64
	if _, err := fmt.Sscanf(raw, "%f:%f", &width, &height); err != nil {
		return nil
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	return &parsedAspectRatio{
		width:  width,
		height: height,
		value:  width / height,
	}
}

func parseSizeValue(raw string) *parsedSize {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var width, height float64
	if _, err := fmt.Sscanf(raw, "%fx%f", &width, &height); err != nil {
		return nil
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	return &parsedSize{
		width:       width,
		height:      height,
		aspectRatio: width / height,
		area:        width * height,
	}
}

func deriveAspectRatioFromSize(size string) string {
	parsed := parseSizeValue(size)
	if parsed == nil {
		return ""
	}
	width := int(parsed.width)
	height := int(parsed.height)
	divisor := greatestCommonDivisor(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}

func greatestCommonDivisor(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func resolveClosestAspectRatio(requestedAspectRatio string, requestedSize string, supported []string) string {
	supported = normalizedStrings(supported)
	if len(supported) == 0 {
		return firstNonEmpty(requestedAspectRatio, deriveAspectRatioFromSize(requestedSize))
	}
	requestedAspectRatio = strings.TrimSpace(requestedAspectRatio)
	if requestedAspectRatio != "" && slices.Contains(supported, requestedAspectRatio) {
		return requestedAspectRatio
	}
	requested := parseAspectRatioValue(requestedAspectRatio)
	if requested == nil {
		requested = parseAspectRatioValue(deriveAspectRatioFromSize(requestedSize))
	}
	if requested == nil {
		return ""
	}

	best := ""
	bestPrimary := math.Inf(1)
	bestSecondary := math.Inf(1)
	for _, candidate := range supported {
		parsed := parseAspectRatioValue(candidate)
		if parsed == nil {
			continue
		}
		primary := math.Abs(math.Log(parsed.value / requested.value))
		secondary := math.Abs(parsed.width*requested.height - requested.width*parsed.height)
		if primary < bestPrimary || (primary == bestPrimary && (secondary < bestSecondary || (secondary == bestSecondary && candidate < best))) {
			best = candidate
			bestPrimary = primary
			bestSecondary = secondary
		}
	}
	return best
}

func resolveClosestSize(requestedSize string, requestedAspectRatio string, supported []string) string {
	supported = normalizedStrings(supported)
	if len(supported) == 0 {
		return strings.TrimSpace(requestedSize)
	}
	requestedSize = strings.TrimSpace(requestedSize)
	if requestedSize != "" && slices.Contains(supported, requestedSize) {
		return requestedSize
	}
	requested := parseSizeValue(requestedSize)
	requestedAspect := parseAspectRatioValue(requestedAspectRatio)
	if requested == nil && requestedAspect == nil {
		return ""
	}

	best := ""
	bestPrimary := math.Inf(1)
	bestSecondary := math.Inf(1)
	for _, candidate := range supported {
		parsed := parseSizeValue(candidate)
		if parsed == nil {
			continue
		}
		referenceAspect := parsed.aspectRatio
		targetAspect := 0.0
		if requested != nil {
			targetAspect = requested.aspectRatio
		} else if requestedAspect == nil {
			continue
		} else {
			targetAspect = requestedAspect.value
		}
		primary := math.Abs(math.Log(referenceAspect / targetAspect))
		secondary := parsed.area
		if requested != nil {
			secondary = math.Abs(math.Log(parsed.area / requested.area))
		}
		if primary < bestPrimary || (primary == bestPrimary && (secondary < bestSecondary || (secondary == bestSecondary && candidate < best))) {
			best = candidate
			bestPrimary = primary
			bestSecondary = secondary
		}
	}
	return best
}

func resolveClosestResolution(requested string, supported []string, order []string) string {
	supported = normalizedStrings(supported)
	if len(supported) == 0 {
		return strings.TrimSpace(requested)
	}
	requested = strings.TrimSpace(requested)
	if requested != "" && slices.Contains(supported, requested) {
		return requested
	}
	if requested == "" {
		return ""
	}
	if len(order) == 0 {
		return ""
	}
	requestedIndex := slices.Index(order, requested)
	if requestedIndex < 0 {
		return ""
	}

	best := ""
	bestPrimary := math.MaxInt
	bestSecondary := math.MaxInt
	for _, candidate := range supported {
		index := slices.Index(order, candidate)
		if index < 0 {
			continue
		}
		primary := absDistance(index - requestedIndex)
		if primary < bestPrimary || (primary == bestPrimary && (index < bestSecondary || (index == bestSecondary && candidate < best))) {
			best = candidate
			bestPrimary = primary
			bestSecondary = index
		}
	}
	return best
}

func normalizeDuration(requested int, supported []int, max int) (int, bool) {
	if requested <= 0 {
		return requested, false
	}
	applied := requested
	if len(supported) > 0 {
		best := supported[0]
		bestDistance := absDistance(best - requested)
		for _, candidate := range supported[1:] {
			distance := absDistance(candidate - requested)
			if distance < bestDistance || (distance == bestDistance && candidate < best) {
				best = candidate
				bestDistance = distance
			}
		}
		applied = best
	} else if max > 0 && requested > max {
		applied = max
	}
	return applied, applied != requested
}

func normalizeFormat(requested string, supported []string) (string, bool) {
	supported = normalizedStrings(supported)
	if len(supported) == 0 {
		return strings.TrimSpace(requested), false
	}
	requested = strings.TrimSpace(requested)
	for _, candidate := range supported {
		if strings.EqualFold(candidate, requested) {
			return candidate, candidate != requested
		}
	}
	return supported[0], supported[0] != requested
}

func normalizedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func buildNormalizationMetadata(normalization map[string]any, ignored []map[string]any) map[string]any {
	out := map[string]any{}
	if len(normalization) > 0 {
		out["normalization"] = normalization
	}
	if len(ignored) > 0 {
		out["ignored_overrides"] = ignored
	}
	return out
}

func stringNormalizationMetadata(entry *StringNormalization) map[string]any {
	if entry == nil {
		return nil
	}
	out := map[string]any{}
	if entry.Requested != "" {
		out["requested"] = entry.Requested
	}
	if entry.Applied != "" {
		out["applied"] = entry.Applied
	}
	if entry.DerivedFrom != "" {
		out["derived_from"] = entry.DerivedFrom
	}
	if len(entry.SupportedValues) > 0 {
		out["supported_values"] = cloneStrings(entry.SupportedValues)
	}
	return out
}

func intNormalizationMetadata(entry *IntNormalization) map[string]any {
	if entry == nil {
		return nil
	}
	out := map[string]any{}
	if entry.Requested > 0 {
		out["requested"] = entry.Requested
	}
	if entry.Applied > 0 {
		out["applied"] = entry.Applied
	}
	if len(entry.SupportedValues) > 0 {
		out["supported_values"] = cloneInts(entry.SupportedValues)
	}
	return out
}

func ignoredOverridesMetadata(in []IgnoredOverride) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		entry := map[string]any{"key": item.Key}
		if item.Value != nil {
			entry["value"] = item.Value
		}
		out = append(out, entry)
	}
	return out
}

func absDistance(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
