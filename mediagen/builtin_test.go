package mediagen

import (
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestBuildBuiltinRegistryFromConfig(t *testing.T) {
	t.Parallel()

	registry := BuildBuiltinRegistry(config.ModelsConfig{
		DefaultProvider: "minimax",
		Providers: map[string]config.ProviderConfig{
			"openai":  {APIKey: "sk-openai"},
			"minimax": {APIKey: "sk-minimax"},
		},
	})
	if registry == nil {
		t.Fatal("BuildBuiltinRegistry() = nil")
	}
	images := registry.ImageProviderInfo()
	if len(images) != 1 || images[0].ID != "openai" {
		t.Fatalf("ImageProviderInfo() = %#v", images)
	}
	music := registry.MusicProviderInfo()
	if len(music) != 1 || music[0].ID != "minimax" {
		t.Fatalf("MusicProviderInfo() = %#v", music)
	}
	if provider, err := registry.FindMusicProvider(""); err != nil || provider.ID() != "minimax" {
		t.Fatalf("FindMusicProvider() = %v, %v", provider, err)
	}
}

func TestBuildBuiltinRegistryIncludesFalFromEnv(t *testing.T) {
	t.Setenv("FAL_KEY", "fal_test_key")
	registry := BuildBuiltinRegistry(config.ModelsConfig{
		Providers: map[string]config.ProviderConfig{
			"openai": {APIKey: "sk-openai"},
		},
	})
	if registry == nil {
		t.Fatal("BuildBuiltinRegistry() = nil")
	}
	videos := registry.VideoProviderInfo()
	found := false
	for _, info := range videos {
		if info.ID == "fal" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("VideoProviderInfo() = %#v", videos)
	}
}

func TestBuildBuiltinRegistryIncludesRunwayFromEnv(t *testing.T) {
	t.Setenv("RUNWAYML_API_SECRET", "runway_secret")
	registry := BuildBuiltinRegistry(config.ModelsConfig{
		Providers: map[string]config.ProviderConfig{
			"openai": {APIKey: "sk-openai"},
		},
	})
	if registry == nil {
		t.Fatal("BuildBuiltinRegistry() = nil")
	}
	videos := registry.VideoProviderInfo()
	found := false
	for _, info := range videos {
		if info.ID == "runway" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("VideoProviderInfo() = %#v", videos)
	}
}
