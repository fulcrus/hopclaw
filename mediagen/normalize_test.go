package mediagen

import (
	"context"
	"testing"
)

func TestNormalizeImageRequestDerivesSizeFromAspectRatio(t *testing.T) {
	t.Parallel()

	resolved := NormalizeImageRequest(normalizeImageProvider{}, ImageRequest{
		Prompt:      "draw a skyline",
		AspectRatio: "16:10",
	})
	if resolved.Request.Size != "1536x1024" {
		t.Fatalf("Request.Size = %q, want 1536x1024", resolved.Request.Size)
	}
	if resolved.Request.AspectRatio != "" {
		t.Fatalf("Request.AspectRatio = %q, want empty", resolved.Request.AspectRatio)
	}
	if resolved.Normalization.Size == nil || resolved.Normalization.Size.DerivedFrom != "aspect_ratio" {
		t.Fatalf("Normalization.Size = %#v", resolved.Normalization.Size)
	}
	metadata := resolved.Metadata()
	if metadata["normalization"] == nil {
		t.Fatalf("Metadata() = %#v", metadata)
	}
}

func TestNormalizeVideoRequestNormalizesResolutionDurationAndAudio(t *testing.T) {
	t.Parallel()

	resolved := NormalizeVideoRequest(normalizeVideoProvider{}, VideoRequest{
		Prompt:          "animate a kite",
		Resolution:      "4K",
		DurationSeconds: 5,
		Audio:           true,
	})
	if resolved.Request.Resolution != "1080P" {
		t.Fatalf("Request.Resolution = %q, want 1080P", resolved.Request.Resolution)
	}
	if resolved.Request.DurationSeconds != 4 {
		t.Fatalf("Request.DurationSeconds = %d, want 4", resolved.Request.DurationSeconds)
	}
	if resolved.Request.Audio {
		t.Fatal("Request.Audio = true, want false")
	}
	if resolved.Normalization.DurationSeconds == nil || resolved.Normalization.DurationSeconds.Applied != 4 {
		t.Fatalf("Normalization.DurationSeconds = %#v", resolved.Normalization.DurationSeconds)
	}
	if len(resolved.Ignored) != 1 || resolved.Ignored[0].Key != "audio" {
		t.Fatalf("Ignored = %#v", resolved.Ignored)
	}
}

func TestNormalizeMusicRequestNormalizesFormat(t *testing.T) {
	t.Parallel()

	resolved := NormalizeMusicRequest(normalizeMusicProvider{}, MusicRequest{
		Prompt: "write a warm synthwave cue",
		Format: "wav",
	})
	if resolved.Request.Format != "mp3" {
		t.Fatalf("Request.Format = %q, want mp3", resolved.Request.Format)
	}
	if resolved.Normalization.Format == nil || resolved.Normalization.Format.Applied != "mp3" {
		t.Fatalf("Normalization.Format = %#v", resolved.Normalization.Format)
	}
}

type normalizeImageProvider struct{}

func (normalizeImageProvider) ID() string                { return "image" }
func (normalizeImageProvider) Label() string             { return "image" }
func (normalizeImageProvider) DefaultImageModel() string { return "img" }
func (normalizeImageProvider) ImageModels() []string     { return []string{"img"} }
func (normalizeImageProvider) ImageCapabilities() ImageCapabilities {
	return ImageCapabilities{
		SupportsSize:        true,
		SupportsAspectRatio: false,
		Sizes:               []string{"1024x1024", "1536x1024", "1024x1536"},
	}
}
func (normalizeImageProvider) GenerateImage(context.Context, ImageRequest) (*ImageResult, error) {
	return nil, nil
}

type normalizeVideoProvider struct{}

func (normalizeVideoProvider) ID() string                { return "video" }
func (normalizeVideoProvider) Label() string             { return "video" }
func (normalizeVideoProvider) DefaultVideoModel() string { return "vid" }
func (normalizeVideoProvider) VideoModels() []string     { return []string{"vid"} }
func (normalizeVideoProvider) VideoCapabilities() VideoCapabilities {
	return VideoCapabilities{
		SupportsResolution: true,
		SupportsAudio:      false,
		Resolutions:        []string{"720P", "1080P"},
		SupportedDurations: []int{4, 8, 12},
	}
}
func (normalizeVideoProvider) GenerateVideo(context.Context, VideoRequest) (*VideoResult, error) {
	return nil, nil
}

type normalizeMusicProvider struct{}

func (normalizeMusicProvider) ID() string                { return "music" }
func (normalizeMusicProvider) Label() string             { return "music" }
func (normalizeMusicProvider) DefaultMusicModel() string { return "music" }
func (normalizeMusicProvider) MusicModels() []string     { return []string{"music"} }
func (normalizeMusicProvider) MusicCapabilities() MusicCapabilities {
	return MusicCapabilities{
		SupportsFormat: true,
		Formats:        []string{"mp3"},
	}
}
func (normalizeMusicProvider) GenerateMusic(context.Context, MusicRequest) (*MusicResult, error) {
	return nil, nil
}
