package media

import (
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrNoImageProvider is returned when no image provider is available.
	ErrNoImageProvider = fmt.Errorf("no image provider available")
	// ErrNoAudioProvider is returned when no audio provider is available.
	ErrNoAudioProvider = fmt.Errorf("no audio provider available")
	// ErrNoVideoProvider is returned when no video provider is available.
	ErrNoVideoProvider = fmt.Errorf("no video provider available")
)

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Registry holds available media understanding providers.
type Registry struct {
	mu        sync.RWMutex // guards providers
	providers []Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
}

// ImageProviders returns all registered providers that support image description.
func (r *Registry) ImageProviders() []ImageProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []ImageProvider
	for _, p := range r.providers {
		if ip, ok := p.(ImageProvider); ok {
			result = append(result, ip)
		}
	}
	return result
}

// AudioProviders returns all registered providers that support audio transcription.
func (r *Registry) AudioProviders() []AudioProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []AudioProvider
	for _, p := range r.providers {
		if ap, ok := p.(AudioProvider); ok {
			result = append(result, ap)
		}
	}
	return result
}

// VideoProviders returns all registered providers that support video analysis.
func (r *Registry) VideoProviders() []VideoProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []VideoProvider
	for _, p := range r.providers {
		if vp, ok := p.(VideoProvider); ok {
			result = append(result, vp)
		}
	}
	return result
}

// FindImageProvider returns the preferred image provider, or the first available.
// If preferred is empty, the first registered image provider is returned.
func (r *Registry) FindImageProvider(preferred string) (ImageProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var first ImageProvider
	for _, p := range r.providers {
		ip, ok := p.(ImageProvider)
		if !ok {
			continue
		}
		if preferred != "" && ip.ID() == preferred {
			return ip, nil
		}
		if first == nil {
			first = ip
		}
	}
	if first != nil {
		return first, nil
	}
	return nil, ErrNoImageProvider
}

// FindAudioProvider returns the preferred audio provider, or the first available.
// If preferred is empty, the first registered audio provider is returned.
func (r *Registry) FindAudioProvider(preferred string) (AudioProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var first AudioProvider
	for _, p := range r.providers {
		ap, ok := p.(AudioProvider)
		if !ok {
			continue
		}
		if preferred != "" && ap.ID() == preferred {
			return ap, nil
		}
		if first == nil {
			first = ap
		}
	}
	if first != nil {
		return first, nil
	}
	return nil, ErrNoAudioProvider
}

// FindVideoProvider returns the preferred video provider, or the first available.
// If preferred is empty, the first registered video provider is returned.
func (r *Registry) FindVideoProvider(preferred string) (VideoProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var first VideoProvider
	for _, p := range r.providers {
		vp, ok := p.(VideoProvider)
		if !ok {
			continue
		}
		if preferred != "" && vp.ID() == preferred {
			return vp, nil
		}
		if first == nil {
			first = vp
		}
	}
	if first != nil {
		return first, nil
	}
	return nil, ErrNoVideoProvider
}
