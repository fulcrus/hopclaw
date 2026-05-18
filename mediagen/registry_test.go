package mediagen

import (
	"context"
	"testing"
)

func TestRegistryFindProviders(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubImageVideoProvider{id: "openai"})
	registry.Register(stubMusicProvider{id: "minimax"})

	if provider, err := registry.FindImageProvider(""); err != nil || provider.ID() != "openai" {
		t.Fatalf("FindImageProvider() = %v, %v", provider, err)
	}
	if provider, err := registry.FindVideoProvider("openai"); err != nil || provider.ID() != "openai" {
		t.Fatalf("FindVideoProvider() = %v, %v", provider, err)
	}
	if provider, err := registry.FindMusicProvider("minimax"); err != nil || provider.ID() != "minimax" {
		t.Fatalf("FindMusicProvider() = %v, %v", provider, err)
	}
}

func TestRegistryProviderInfo(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubImageVideoProvider{id: "openai"})

	images := registry.ImageProviderInfo()
	if len(images) != 1 || images[0].ID != "openai" {
		t.Fatalf("ImageProviderInfo() = %#v", images)
	}
	videos := registry.VideoProviderInfo()
	if len(videos) != 1 || videos[0].Capabilities.MaxVideos != 1 {
		t.Fatalf("VideoProviderInfo() = %#v", videos)
	}
}

type stubImageVideoProvider struct {
	id string
}

func (p stubImageVideoProvider) ID() string                { return p.id }
func (p stubImageVideoProvider) Label() string             { return p.id }
func (p stubImageVideoProvider) DefaultImageModel() string { return "img" }
func (p stubImageVideoProvider) ImageModels() []string     { return []string{"img"} }
func (p stubImageVideoProvider) ImageCapabilities() ImageCapabilities {
	return ImageCapabilities{MaxCount: 1}
}
func (p stubImageVideoProvider) GenerateImage(context.Context, ImageRequest) (*ImageResult, error) {
	return &ImageResult{}, nil
}
func (p stubImageVideoProvider) DefaultVideoModel() string { return "vid" }
func (p stubImageVideoProvider) VideoModels() []string     { return []string{"vid"} }
func (p stubImageVideoProvider) VideoCapabilities() VideoCapabilities {
	return VideoCapabilities{MaxVideos: 1}
}
func (p stubImageVideoProvider) GenerateVideo(context.Context, VideoRequest) (*VideoResult, error) {
	return &VideoResult{}, nil
}

type stubMusicProvider struct {
	id string
}

func (p stubMusicProvider) ID() string                { return p.id }
func (p stubMusicProvider) Label() string             { return p.id }
func (p stubMusicProvider) DefaultMusicModel() string { return "music" }
func (p stubMusicProvider) MusicModels() []string     { return []string{"music"} }
func (p stubMusicProvider) MusicCapabilities() MusicCapabilities {
	return MusicCapabilities{MaxTracks: 1}
}
func (p stubMusicProvider) GenerateMusic(context.Context, MusicRequest) (*MusicResult, error) {
	return &MusicResult{}, nil
}
