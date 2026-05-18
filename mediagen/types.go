package mediagen

import (
	"context"
	"strings"
)

type InputAsset struct {
	Buffer   []byte
	MIMEType string
	FileName string
	Source   string
}

type GeneratedAsset struct {
	Buffer   []byte
	MIMEType string
	FileName string
	Metadata map[string]any
}

type Provider interface {
	ID() string
	Label() string
}

type ImageProvider interface {
	Provider
	DefaultImageModel() string
	ImageModels() []string
	ImageCapabilities() ImageCapabilities
	GenerateImage(context.Context, ImageRequest) (*ImageResult, error)
}

type VideoProvider interface {
	Provider
	DefaultVideoModel() string
	VideoModels() []string
	VideoCapabilities() VideoCapabilities
	GenerateVideo(context.Context, VideoRequest) (*VideoResult, error)
}

type MusicProvider interface {
	Provider
	DefaultMusicModel() string
	MusicModels() []string
	MusicCapabilities() MusicCapabilities
	GenerateMusic(context.Context, MusicRequest) (*MusicResult, error)
}

type ImageCapabilities struct {
	MaxCount            int
	MaxInputImages      int
	SupportsEdit        bool
	SupportsSize        bool
	SupportsAspectRatio bool
	SupportsResolution  bool
	Sizes               []string
	AspectRatios        []string
	Resolutions         []string
}

type VideoCapabilities struct {
	MaxVideos            int
	MaxInputImages       int
	MaxInputVideos       int
	MaxDurationSeconds   int
	SupportedDurations   []int
	SupportsImageToVideo bool
	SupportsVideoToVideo bool
	SupportsSize         bool
	SupportsAspectRatio  bool
	SupportsResolution   bool
	SupportsAudio        bool
	Sizes                []string
	AspectRatios         []string
	Resolutions          []string
}

type MusicCapabilities struct {
	MaxTracks            int
	MaxInputImages       int
	MaxDurationSeconds   int
	SupportedDurations   []int
	SupportsLyrics       bool
	SupportsInstrumental bool
	SupportsDuration     bool
	SupportsFormat       bool
	Formats              []string
}

type ImageRequest struct {
	Provider    string
	Model       string
	Prompt      string
	Count       int
	Size        string
	AspectRatio string
	Resolution  string
	InputImages []InputAsset
	TimeoutMS   int
}

type VideoRequest struct {
	Provider        string
	Model           string
	Prompt          string
	DurationSeconds int
	Size            string
	AspectRatio     string
	Resolution      string
	Audio           bool
	InputImages     []InputAsset
	InputVideos     []InputAsset
	TimeoutMS       int
}

type MusicRequest struct {
	Provider        string
	Model           string
	Prompt          string
	Lyrics          string
	Instrumental    bool
	DurationSeconds int
	Format          string
	InputImages     []InputAsset
	TimeoutMS       int
}

type ImageResult struct {
	Images         []GeneratedAsset
	Model          string
	RevisedPrompts []string
	Metadata       map[string]any
}

type VideoResult struct {
	Videos   []GeneratedAsset
	Model    string
	Metadata map[string]any
}

type MusicResult struct {
	Tracks   []GeneratedAsset
	Lyrics   []string
	Model    string
	Metadata map[string]any
}

type ImageProviderInfo struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	DefaultModel string            `json:"default_model,omitempty"`
	Models       []string          `json:"models,omitempty"`
	Capabilities ImageCapabilities `json:"capabilities"`
}

type VideoProviderInfo struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	DefaultModel string            `json:"default_model,omitempty"`
	Models       []string          `json:"models,omitempty"`
	Capabilities VideoCapabilities `json:"capabilities"`
}

type MusicProviderInfo struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	DefaultModel string            `json:"default_model,omitempty"`
	Models       []string          `json:"models,omitempty"`
	Capabilities MusicCapabilities `json:"capabilities"`
}

func normalizeAssetFileName(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return strings.TrimSpace(name)
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	out = append(out, in...)
	return out
}

func cloneInts(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	out := make([]int, 0, len(in))
	out = append(out, in...)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyImageCapabilities(in ImageCapabilities) ImageCapabilities {
	return ImageCapabilities{
		MaxCount:            in.MaxCount,
		MaxInputImages:      in.MaxInputImages,
		SupportsEdit:        in.SupportsEdit,
		SupportsSize:        in.SupportsSize,
		SupportsAspectRatio: in.SupportsAspectRatio,
		SupportsResolution:  in.SupportsResolution,
		Sizes:               cloneStrings(in.Sizes),
		AspectRatios:        cloneStrings(in.AspectRatios),
		Resolutions:         cloneStrings(in.Resolutions),
	}
}

func copyVideoCapabilities(in VideoCapabilities) VideoCapabilities {
	return VideoCapabilities{
		MaxVideos:            in.MaxVideos,
		MaxInputImages:       in.MaxInputImages,
		MaxInputVideos:       in.MaxInputVideos,
		MaxDurationSeconds:   in.MaxDurationSeconds,
		SupportedDurations:   cloneInts(in.SupportedDurations),
		SupportsImageToVideo: in.SupportsImageToVideo,
		SupportsVideoToVideo: in.SupportsVideoToVideo,
		SupportsSize:         in.SupportsSize,
		SupportsAspectRatio:  in.SupportsAspectRatio,
		SupportsResolution:   in.SupportsResolution,
		SupportsAudio:        in.SupportsAudio,
		Sizes:                cloneStrings(in.Sizes),
		AspectRatios:         cloneStrings(in.AspectRatios),
		Resolutions:          cloneStrings(in.Resolutions),
	}
}

func copyMusicCapabilities(in MusicCapabilities) MusicCapabilities {
	return MusicCapabilities{
		MaxTracks:            in.MaxTracks,
		MaxInputImages:       in.MaxInputImages,
		MaxDurationSeconds:   in.MaxDurationSeconds,
		SupportedDurations:   cloneInts(in.SupportedDurations),
		SupportsLyrics:       in.SupportsLyrics,
		SupportsInstrumental: in.SupportsInstrumental,
		SupportsDuration:     in.SupportsDuration,
		SupportsFormat:       in.SupportsFormat,
		Formats:              cloneStrings(in.Formats),
	}
}
