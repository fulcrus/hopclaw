package mediagen

import (
	"fmt"
	"sync"
)

var (
	ErrNoImageProvider = fmt.Errorf("no image generation provider available")
	ErrNoVideoProvider = fmt.Errorf("no video generation provider available")
	ErrNoMusicProvider = fmt.Errorf("no music generation provider available")
)

type Registry struct {
	mu             sync.RWMutex
	imageProviders []ImageProvider
	videoProviders []VideoProvider
	musicProviders []MusicProvider
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(provider Provider) {
	if r == nil || provider == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if typed, ok := provider.(ImageProvider); ok {
		r.imageProviders = append(r.imageProviders, typed)
	}
	if typed, ok := provider.(VideoProvider); ok {
		r.videoProviders = append(r.videoProviders, typed)
	}
	if typed, ok := provider.(MusicProvider); ok {
		r.musicProviders = append(r.musicProviders, typed)
	}
}

func (r *Registry) Empty() bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.imageProviders) == 0 && len(r.videoProviders) == 0 && len(r.musicProviders) == 0
}

func (r *Registry) ImageProviders() []ImageProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ImageProvider, 0, len(r.imageProviders))
	out = append(out, r.imageProviders...)
	return out
}

func (r *Registry) VideoProviders() []VideoProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]VideoProvider, 0, len(r.videoProviders))
	out = append(out, r.videoProviders...)
	return out
}

func (r *Registry) MusicProviders() []MusicProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]MusicProvider, 0, len(r.musicProviders))
	out = append(out, r.musicProviders...)
	return out
}

func (r *Registry) FindImageProvider(preferred string) (ImageProvider, error) {
	return findProvider(r.ImageProviders(), preferred, ErrNoImageProvider)
}

func (r *Registry) FindVideoProvider(preferred string) (VideoProvider, error) {
	return findProvider(r.VideoProviders(), preferred, ErrNoVideoProvider)
}

func (r *Registry) FindMusicProvider(preferred string) (MusicProvider, error) {
	return findProvider(r.MusicProviders(), preferred, ErrNoMusicProvider)
}

func (r *Registry) ImageProviderInfo() []ImageProviderInfo {
	providers := r.ImageProviders()
	if len(providers) == 0 {
		return nil
	}
	out := make([]ImageProviderInfo, 0, len(providers))
	for _, provider := range providers {
		out = append(out, ImageProviderInfo{
			ID:           provider.ID(),
			Label:        provider.Label(),
			DefaultModel: provider.DefaultImageModel(),
			Models:       cloneStrings(provider.ImageModels()),
			Capabilities: copyImageCapabilities(provider.ImageCapabilities()),
		})
	}
	return out
}

func (r *Registry) VideoProviderInfo() []VideoProviderInfo {
	providers := r.VideoProviders()
	if len(providers) == 0 {
		return nil
	}
	out := make([]VideoProviderInfo, 0, len(providers))
	for _, provider := range providers {
		out = append(out, VideoProviderInfo{
			ID:           provider.ID(),
			Label:        provider.Label(),
			DefaultModel: provider.DefaultVideoModel(),
			Models:       cloneStrings(provider.VideoModels()),
			Capabilities: copyVideoCapabilities(provider.VideoCapabilities()),
		})
	}
	return out
}

func (r *Registry) MusicProviderInfo() []MusicProviderInfo {
	providers := r.MusicProviders()
	if len(providers) == 0 {
		return nil
	}
	out := make([]MusicProviderInfo, 0, len(providers))
	for _, provider := range providers {
		out = append(out, MusicProviderInfo{
			ID:           provider.ID(),
			Label:        provider.Label(),
			DefaultModel: provider.DefaultMusicModel(),
			Models:       cloneStrings(provider.MusicModels()),
			Capabilities: copyMusicCapabilities(provider.MusicCapabilities()),
		})
	}
	return out
}

type providerWithID interface {
	ID() string
}

func findProvider[T providerWithID](providers []T, preferred string, missing error) (T, error) {
	var zero T
	if len(providers) == 0 {
		return zero, missing
	}
	if preferred == "" {
		return providers[0], nil
	}
	for _, provider := range providers {
		if provider.ID() == preferred {
			return provider, nil
		}
	}
	return zero, missing
}
