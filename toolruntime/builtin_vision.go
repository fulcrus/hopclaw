package toolruntime

// This file retains the root-package visionAnalyzer and imageComparer methods
// used by the exported VisionAnalyzer/ImageComparer in helpers_exported.go.
// The canonical tool implementations live in toolruntime/vision/.

import (
	"fmt"

	"github.com/fulcrus/hopclaw/media"
)

// visionAnalyzer returns a VisionAnalyzer, or an error if no media registry
// is configured.
func (b *Builtins) visionAnalyzer() (*media.VisionAnalyzer, error) {
	if b.mediaRegistry == nil {
		return nil, fmt.Errorf("vision tools require a media registry")
	}
	return media.NewVisionAnalyzer(b.mediaRegistry), nil
}

// imageComparer returns an ImageComparer, or an error if no media registry
// is configured.
func (b *Builtins) imageComparer() (*media.ImageComparer, error) {
	if b.mediaRegistry == nil {
		return nil, fmt.Errorf("vision tools require a media registry")
	}
	return media.NewImageComparer(b.mediaRegistry), nil
}
