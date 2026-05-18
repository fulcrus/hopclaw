// Package mediagen implements image.generate, video.generate, and music.generate
// builtin tools on top of the pluggable github.com/fulcrus/hopclaw/mediagen
// provider registry.
package mediagen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/media"
	gen "github.com/fulcrus/hopclaw/mediagen"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
	ReadArtifact(ctx context.Context, uri string) ([]byte, string, error)
	PutArtifact(ctx context.Context, call agent.ToolCall, req artifact.PutRequest) (*artifact.Blob, error)
	MediaGenerationRegistry() *gen.Registry
	MaxReadBytes() int
}

type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:             "image.generate",
				Description:      "Generate or edit images with configured media-generation providers.",
				InputSchema:      imageGenerateInputSchema(),
				OutputSchema:     generationOutputSchema("image"),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				ExecutionKey:     "image:generate",
			},
			Handler: handleImageGenerate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "video.generate",
				Description:      "Generate or transform videos with configured media-generation providers.",
				InputSchema:      videoGenerateInputSchema(),
				OutputSchema:     generationOutputSchema("video"),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				ExecutionKey:     "video:generate",
			},
			Handler: handleVideoGenerate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "music.generate",
				Description:      "Generate music tracks with configured media-generation providers.",
				InputSchema:      musicGenerateInputSchema(),
				OutputSchema:     generationOutputSchema("music"),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				ExecutionKey:     "music:generate",
			},
			Handler: handleMusicGenerate,
		},
	}
}

func handleImageGenerate(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	registry := rt.MediaGenerationRegistry()
	action := strings.TrimSpace(stringValue(call.Input["action"]))
	if action == "" {
		action = "generate"
	}
	if action == "list" {
		return rt.JSONResult(call, map[string]any{
			"kind":      "image",
			"providers": registryProviderInfo(registry, "image"),
		})
	}
	if registry == nil || len(registry.ImageProviders()) == 0 {
		return notConfigured(rt, call, "image", "No image-generation providers are configured.")
	}

	prompt := strings.TrimSpace(stringValue(call.Input["prompt"]))
	if prompt == "" {
		return contextengine.ToolResult{}, fmt.Errorf("image.generate: prompt is required")
	}
	providerID := strings.TrimSpace(stringValue(call.Input["provider"]))
	references, err := parseAssetRefs(call.Input["references"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("image.generate references: %w", err)
	}
	inputImages, err := loadAssets(ctx, rt, references)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	baseReq := gen.ImageRequest{
		Provider:    providerID,
		Model:       strings.TrimSpace(stringValue(call.Input["model"])),
		Prompt:      prompt,
		Count:       intValue(call.Input["count"], 1),
		Size:        strings.TrimSpace(stringValue(call.Input["size"])),
		AspectRatio: strings.TrimSpace(stringValue(call.Input["aspect_ratio"])),
		Resolution:  strings.TrimSpace(stringValue(call.Input["resolution"])),
		InputImages: inputImages,
		TimeoutMS:   intValue(call.Input["timeout_ms"], 0),
	}
	providers, err := selectImageProviders(registry, providerID)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	attempts := make([]generationAttempt, 0, len(providers))
	for _, provider := range providers {
		req := baseReq
		req.Provider = provider.ID()
		normalized := gen.NormalizeImageRequest(provider, req)
		if supportErr := validateImageRequest(provider, normalized.Request); supportErr != nil {
			attempts = append(attempts, generationAttempt{
				Provider: provider.ID(),
				Model:    firstNonEmpty(normalized.Request.Model, provider.DefaultImageModel()),
				Error:    supportErr.Error(),
			})
			if providerID != "" {
				break
			}
			continue
		}
		result, callErr := provider.GenerateImage(ctx, normalized.Request)
		if callErr != nil {
			attempts = append(attempts, generationAttempt{
				Provider: provider.ID(),
				Model:    firstNonEmpty(normalized.Request.Model, provider.DefaultImageModel()),
				Error:    callErr.Error(),
			})
			if providerID != "" {
				break
			}
			continue
		}
		if result != nil {
			result.Metadata = mergeMetadata(result.Metadata, normalized.Metadata(), attemptsMetadata(attempts))
		}
		return buildImageResult(ctx, rt, call, provider, result)
	}
	return contextengine.ToolResult{}, generationFailure("image.generate", attempts)
}

func handleVideoGenerate(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	registry := rt.MediaGenerationRegistry()
	action := strings.TrimSpace(stringValue(call.Input["action"]))
	if action == "" {
		action = "generate"
	}
	if action == "list" {
		return rt.JSONResult(call, map[string]any{
			"kind":      "video",
			"providers": registryProviderInfo(registry, "video"),
		})
	}
	if registry == nil || len(registry.VideoProviders()) == 0 {
		return notConfigured(rt, call, "video", "No video-generation providers are configured.")
	}

	prompt := strings.TrimSpace(stringValue(call.Input["prompt"]))
	if prompt == "" {
		return contextengine.ToolResult{}, fmt.Errorf("video.generate: prompt is required")
	}
	providerID := strings.TrimSpace(stringValue(call.Input["provider"]))
	imageRefs, err := parseAssetRefs(call.Input["input_images"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("video.generate input_images: %w", err)
	}
	videoRefs, err := parseAssetRefs(call.Input["input_videos"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("video.generate input_videos: %w", err)
	}
	inputImages, err := loadAssets(ctx, rt, imageRefs)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	inputVideos, err := loadAssets(ctx, rt, videoRefs)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	baseReq := gen.VideoRequest{
		Provider:        providerID,
		Model:           strings.TrimSpace(stringValue(call.Input["model"])),
		Prompt:          prompt,
		DurationSeconds: intValue(call.Input["duration_seconds"], 0),
		Size:            strings.TrimSpace(stringValue(call.Input["size"])),
		AspectRatio:     strings.TrimSpace(stringValue(call.Input["aspect_ratio"])),
		Resolution:      strings.TrimSpace(stringValue(call.Input["resolution"])),
		Audio:           boolValue(call.Input["audio"], false),
		InputImages:     inputImages,
		InputVideos:     inputVideos,
		TimeoutMS:       intValue(call.Input["timeout_ms"], 0),
	}
	providers, err := selectVideoProviders(registry, providerID)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	attempts := make([]generationAttempt, 0, len(providers))
	for _, provider := range providers {
		req := baseReq
		req.Provider = provider.ID()
		normalized := gen.NormalizeVideoRequest(provider, req)
		if supportErr := validateVideoRequest(provider, normalized.Request); supportErr != nil {
			attempts = append(attempts, generationAttempt{
				Provider: provider.ID(),
				Model:    firstNonEmpty(normalized.Request.Model, provider.DefaultVideoModel()),
				Error:    supportErr.Error(),
			})
			if providerID != "" {
				break
			}
			continue
		}
		result, callErr := provider.GenerateVideo(ctx, normalized.Request)
		if callErr != nil {
			attempts = append(attempts, generationAttempt{
				Provider: provider.ID(),
				Model:    firstNonEmpty(normalized.Request.Model, provider.DefaultVideoModel()),
				Error:    callErr.Error(),
			})
			if providerID != "" {
				break
			}
			continue
		}
		if result != nil {
			result.Metadata = mergeMetadata(result.Metadata, normalized.Metadata(), attemptsMetadata(attempts))
		}
		return buildVideoResult(ctx, rt, call, provider, result)
	}
	return contextengine.ToolResult{}, generationFailure("video.generate", attempts)
}

func handleMusicGenerate(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	registry := rt.MediaGenerationRegistry()
	action := strings.TrimSpace(stringValue(call.Input["action"]))
	if action == "" {
		action = "generate"
	}
	if action == "list" {
		return rt.JSONResult(call, map[string]any{
			"kind":      "music",
			"providers": registryProviderInfo(registry, "music"),
		})
	}
	if registry == nil || len(registry.MusicProviders()) == 0 {
		return notConfigured(rt, call, "music", "No music-generation providers are configured.")
	}

	prompt := strings.TrimSpace(stringValue(call.Input["prompt"]))
	if prompt == "" {
		return contextengine.ToolResult{}, fmt.Errorf("music.generate: prompt is required")
	}
	providerID := strings.TrimSpace(stringValue(call.Input["provider"]))
	imageRefs, err := parseAssetRefs(call.Input["input_images"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("music.generate input_images: %w", err)
	}
	inputImages, err := loadAssets(ctx, rt, imageRefs)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	baseReq := gen.MusicRequest{
		Provider:        providerID,
		Model:           strings.TrimSpace(stringValue(call.Input["model"])),
		Prompt:          prompt,
		Lyrics:          strings.TrimSpace(stringValue(call.Input["lyrics"])),
		Instrumental:    boolValue(call.Input["instrumental"], false),
		DurationSeconds: intValue(call.Input["duration_seconds"], 0),
		Format:          strings.TrimSpace(stringValue(call.Input["format"])),
		InputImages:     inputImages,
		TimeoutMS:       intValue(call.Input["timeout_ms"], 0),
	}
	providers, err := selectMusicProviders(registry, providerID)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	attempts := make([]generationAttempt, 0, len(providers))
	for _, provider := range providers {
		req := baseReq
		req.Provider = provider.ID()
		normalized := gen.NormalizeMusicRequest(provider, req)
		if supportErr := validateMusicRequest(provider, normalized.Request); supportErr != nil {
			attempts = append(attempts, generationAttempt{
				Provider: provider.ID(),
				Model:    firstNonEmpty(normalized.Request.Model, provider.DefaultMusicModel()),
				Error:    supportErr.Error(),
			})
			if providerID != "" {
				break
			}
			continue
		}
		result, callErr := provider.GenerateMusic(ctx, normalized.Request)
		if callErr != nil {
			attempts = append(attempts, generationAttempt{
				Provider: provider.ID(),
				Model:    firstNonEmpty(normalized.Request.Model, provider.DefaultMusicModel()),
				Error:    callErr.Error(),
			})
			if providerID != "" {
				break
			}
			continue
		}
		if result != nil {
			result.Metadata = mergeMetadata(result.Metadata, normalized.Metadata(), attemptsMetadata(attempts))
		}
		return buildMusicResult(ctx, rt, call, provider, result)
	}
	return contextengine.ToolResult{}, generationFailure("music.generate", attempts)
}

func buildImageResult(ctx context.Context, rt Runtime, call agent.ToolCall, provider gen.ImageProvider, result *gen.ImageResult) (contextengine.ToolResult, error) {
	if result == nil || len(result.Images) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("image.generate: provider returned no images")
	}
	artifacts, err := storeAssets(ctx, rt, call, "image.generated", provider.ID(), firstNonEmpty(result.Model, provider.DefaultImageModel()), result.Images)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	structured := map[string]any{
		"kind":      "image",
		"provider":  provider.ID(),
		"model":     firstNonEmpty(result.Model, provider.DefaultImageModel()),
		"count":     len(artifacts),
		"artifacts": artifactSummaries(artifacts),
	}
	mergeMap(structured, result.Metadata)
	if len(result.RevisedPrompts) > 0 {
		structured["revised_prompts"] = append([]string(nil), result.RevisedPrompts...)
	}
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultmodel.ToolResultOK,
		TranscriptText: fmt.Sprintf("generated %d image(s) via %s/%s", len(artifacts), provider.ID(), firstNonEmpty(result.Model, provider.DefaultImageModel())),
		Summary:        fmt.Sprintf("%d image(s) ready", len(artifacts)),
		Structured:     structured,
		Artifacts:      artifacts,
		ArtifactURI:    artifacts[0].URI,
	}.Normalized(), nil
}

func buildVideoResult(ctx context.Context, rt Runtime, call agent.ToolCall, provider gen.VideoProvider, result *gen.VideoResult) (contextengine.ToolResult, error) {
	if result == nil || len(result.Videos) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("video.generate: provider returned no videos")
	}
	artifacts, err := storeAssets(ctx, rt, call, "video.generated", provider.ID(), firstNonEmpty(result.Model, provider.DefaultVideoModel()), result.Videos)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	structured := map[string]any{
		"kind":      "video",
		"provider":  provider.ID(),
		"model":     firstNonEmpty(result.Model, provider.DefaultVideoModel()),
		"count":     len(artifacts),
		"artifacts": artifactSummaries(artifacts),
	}
	mergeMap(structured, result.Metadata)
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultmodel.ToolResultOK,
		TranscriptText: fmt.Sprintf("generated %d video(s) via %s/%s", len(artifacts), provider.ID(), firstNonEmpty(result.Model, provider.DefaultVideoModel())),
		Summary:        fmt.Sprintf("%d video(s) ready", len(artifacts)),
		Structured:     structured,
		Artifacts:      artifacts,
		ArtifactURI:    artifacts[0].URI,
	}.Normalized(), nil
}

func buildMusicResult(ctx context.Context, rt Runtime, call agent.ToolCall, provider gen.MusicProvider, result *gen.MusicResult) (contextengine.ToolResult, error) {
	if result == nil || len(result.Tracks) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("music.generate: provider returned no tracks")
	}
	artifacts, err := storeAssets(ctx, rt, call, "music.generated", provider.ID(), firstNonEmpty(result.Model, provider.DefaultMusicModel()), result.Tracks)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	structured := map[string]any{
		"kind":      "music",
		"provider":  provider.ID(),
		"model":     firstNonEmpty(result.Model, provider.DefaultMusicModel()),
		"count":     len(artifacts),
		"artifacts": artifactSummaries(artifacts),
	}
	mergeMap(structured, result.Metadata)
	var blocks []resultmodel.ResultBlock
	if len(result.Lyrics) > 0 {
		structured["lyrics"] = append([]string(nil), result.Lyrics...)
		blocks = append(blocks, resultmodel.ResultBlock{
			Kind:    resultmodel.ResultBlockText,
			Title:   "Lyrics",
			Content: strings.Join(result.Lyrics, "\n\n"),
		})
	}
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultmodel.ToolResultOK,
		TranscriptText: fmt.Sprintf("generated %d track(s) via %s/%s", len(artifacts), provider.ID(), firstNonEmpty(result.Model, provider.DefaultMusicModel())),
		Summary:        fmt.Sprintf("%d track(s) ready", len(artifacts)),
		Structured:     structured,
		Blocks:         blocks,
		Artifacts:      artifacts,
		ArtifactURI:    artifacts[0].URI,
	}.Normalized(), nil
}

func storeAssets(ctx context.Context, rt Runtime, call agent.ToolCall, kind, provider, model string, items []gen.GeneratedAsset) ([]resultmodel.ResultArtifact, error) {
	out := make([]resultmodel.ResultArtifact, 0, len(items))
	for index, item := range items {
		mimeType := firstNonEmpty(strings.TrimSpace(item.MIMEType), defaultContentType(kind))
		fileName := strings.TrimSpace(item.FileName)
		if fileName == "" {
			fileName = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(kind, ".generated"), index+1, fileExtForContentType(mimeType))
		}
		blob, err := rt.PutArtifact(ctx, call, artifact.PutRequest{
			Kind:        kind,
			ContentType: mimeType,
			Body:        item.Buffer,
			Metadata: map[string]any{
				"provider":  provider,
				"model":     model,
				"index":     index + 1,
				"file_name": fileName,
			},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, resultmodel.ResultArtifact{
			Kind:        kind,
			Name:        fileName,
			URI:         blob.URI,
			ContentType: mimeType,
			SizeBytes:   blob.Size,
			Metadata: map[string]any{
				"provider": provider,
				"model":    model,
				"index":    index + 1,
			},
		})
	}
	return out, nil
}

func loadAssets(ctx context.Context, rt Runtime, refs []assetRef) ([]gen.InputAsset, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]gen.InputAsset, 0, len(refs))
	for _, ref := range refs {
		asset, err := loadAsset(ctx, rt, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func loadAsset(ctx context.Context, rt Runtime, ref assetRef) (gen.InputAsset, error) {
	switch {
	case strings.TrimSpace(ref.ArtifactURI) != "":
		body, contentType, err := rt.ReadArtifact(ctx, strings.TrimSpace(ref.ArtifactURI))
		if err != nil {
			return gen.InputAsset{}, fmt.Errorf("load artifact %q: %w", ref.ArtifactURI, err)
		}
		if len(body) == 0 {
			return gen.InputAsset{}, fmt.Errorf("artifact %q is empty", ref.ArtifactURI)
		}
		mimeType := firstNonEmpty(strings.TrimSpace(ref.MIMEType), strings.TrimSpace(contentType), media.DetectMIMETypeFromBytes(body))
		fileName := strings.TrimSpace(ref.FileName)
		if fileName == "" {
			fileName = "artifact" + fileExtForContentType(mimeType)
		}
		return gen.InputAsset{
			Buffer:   body,
			MIMEType: mimeType,
			FileName: fileName,
			Source:   strings.TrimSpace(ref.ArtifactURI),
		}, nil
	case strings.TrimSpace(ref.Path) != "":
		resolved, err := rt.ResolvePath(strings.TrimSpace(ref.Path))
		if err != nil {
			return gen.InputAsset{}, err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return gen.InputAsset{}, err
		}
		if max := rt.MaxReadBytes(); max > 0 && info.Size() > int64(max) {
			return gen.InputAsset{}, fmt.Errorf("input %q exceeds max read size %d bytes", rt.DisplayPath(resolved), max)
		}
		body, err := os.ReadFile(resolved)
		if err != nil {
			return gen.InputAsset{}, err
		}
		mimeType := firstNonEmpty(strings.TrimSpace(ref.MIMEType), media.DetectMIMETypeFromBytes(body))
		fileName := firstNonEmpty(strings.TrimSpace(ref.FileName), filepath.Base(resolved))
		return gen.InputAsset{
			Buffer:   body,
			MIMEType: mimeType,
			FileName: fileName,
			Source:   rt.DisplayPath(resolved),
		}, nil
	default:
		return gen.InputAsset{}, fmt.Errorf("asset reference must include path or artifact_uri")
	}
}

type assetRef struct {
	Path        string
	ArtifactURI string
	MIMEType    string
	FileName    string
}

type generationAttempt struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Error    string `json:"error,omitempty"`
}

func selectImageProviders(registry *gen.Registry, preferred string) ([]gen.ImageProvider, error) {
	if registry == nil {
		return nil, gen.ErrNoImageProvider
	}
	if preferred != "" {
		provider, err := registry.FindImageProvider(preferred)
		if err != nil {
			return nil, err
		}
		return []gen.ImageProvider{provider}, nil
	}
	return registry.ImageProviders(), nil
}

func selectVideoProviders(registry *gen.Registry, preferred string) ([]gen.VideoProvider, error) {
	if registry == nil {
		return nil, gen.ErrNoVideoProvider
	}
	if preferred != "" {
		provider, err := registry.FindVideoProvider(preferred)
		if err != nil {
			return nil, err
		}
		return []gen.VideoProvider{provider}, nil
	}
	return registry.VideoProviders(), nil
}

func selectMusicProviders(registry *gen.Registry, preferred string) ([]gen.MusicProvider, error) {
	if registry == nil {
		return nil, gen.ErrNoMusicProvider
	}
	if preferred != "" {
		provider, err := registry.FindMusicProvider(preferred)
		if err != nil {
			return nil, err
		}
		return []gen.MusicProvider{provider}, nil
	}
	return registry.MusicProviders(), nil
}

func validateImageRequest(provider gen.ImageProvider, req gen.ImageRequest) error {
	caps := provider.ImageCapabilities()
	if req.Count > 0 && caps.MaxCount > 0 && req.Count > caps.MaxCount {
		return fmt.Errorf("requested count %d exceeds provider max %d", req.Count, caps.MaxCount)
	}
	if len(req.InputImages) > 0 && !caps.SupportsEdit {
		return fmt.Errorf("image edit references are not supported")
	}
	if caps.MaxInputImages > 0 && len(req.InputImages) > caps.MaxInputImages {
		return fmt.Errorf("input_images exceeds provider max %d", caps.MaxInputImages)
	}
	return nil
}

func validateVideoRequest(provider gen.VideoProvider, req gen.VideoRequest) error {
	caps := provider.VideoCapabilities()
	if len(req.InputImages) > 0 && !caps.SupportsImageToVideo {
		return fmt.Errorf("image-to-video is not supported")
	}
	if len(req.InputVideos) > 0 && !caps.SupportsVideoToVideo {
		return fmt.Errorf("video-to-video is not supported")
	}
	if caps.MaxInputImages > 0 && len(req.InputImages) > caps.MaxInputImages {
		return fmt.Errorf("input_images exceeds provider max %d", caps.MaxInputImages)
	}
	if caps.MaxInputVideos > 0 && len(req.InputVideos) > caps.MaxInputVideos {
		return fmt.Errorf("input_videos exceeds provider max %d", caps.MaxInputVideos)
	}
	if caps.MaxVideos > 0 && caps.MaxVideos < 1 {
		return fmt.Errorf("video generation is disabled")
	}
	return nil
}

func validateMusicRequest(provider gen.MusicProvider, req gen.MusicRequest) error {
	caps := provider.MusicCapabilities()
	if len(req.InputImages) > 0 && caps.MaxInputImages == 0 {
		return fmt.Errorf("image-conditioned music generation is not supported")
	}
	if caps.MaxInputImages > 0 && len(req.InputImages) > caps.MaxInputImages {
		return fmt.Errorf("input_images exceeds provider max %d", caps.MaxInputImages)
	}
	if req.Instrumental && !caps.SupportsInstrumental {
		return fmt.Errorf("instrumental generation is not supported")
	}
	if req.Lyrics != "" && !caps.SupportsLyrics {
		return fmt.Errorf("lyrics input is not supported")
	}
	if caps.MaxTracks > 0 && caps.MaxTracks < 1 {
		return fmt.Errorf("music generation is disabled")
	}
	return nil
}

func attemptsMetadata(attempts []generationAttempt) map[string]any {
	if len(attempts) == 0 {
		return nil
	}
	items := make([]map[string]any, 0, len(attempts))
	for _, attempt := range attempts {
		item := map[string]any{
			"provider": attempt.Provider,
		}
		if attempt.Model != "" {
			item["model"] = attempt.Model
		}
		if attempt.Error != "" {
			item["error"] = attempt.Error
		}
		items = append(items, item)
	}
	return map[string]any{
		"attempts":      items,
		"fallback_used": true,
	}
}

func mergeMetadata(base map[string]any, extras ...map[string]any) map[string]any {
	if len(base) == 0 && len(extras) == 0 {
		return nil
	}
	out := cloneMetadataMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for _, extra := range extras {
		for key, value := range extra {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneMetadataMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func generationFailure(toolName string, attempts []generationAttempt) error {
	if len(attempts) == 0 {
		return fmt.Errorf("%s: no provider could handle the request", toolName)
	}
	parts := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		parts = append(parts, fmt.Sprintf("%s/%s: %s", attempt.Provider, firstNonEmpty(attempt.Model, "default"), attempt.Error))
	}
	return fmt.Errorf("%s: all providers failed: %s", toolName, strings.Join(parts, "; "))
}

func parseAssetRefs(value any) ([]assetRef, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		ref, err := parseAssetRefString(typed)
		if err != nil {
			return nil, err
		}
		return []assetRef{ref}, nil
	case map[string]any:
		ref, err := parseAssetRefMap(typed)
		if err != nil {
			return nil, err
		}
		return []assetRef{ref}, nil
	case []any:
		out := make([]assetRef, 0, len(typed))
		for _, item := range typed {
			refs, err := parseAssetRefs(item)
			if err != nil {
				return nil, err
			}
			out = append(out, refs...)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected asset reference or array, got %T", value)
	}
}

func parseAssetRefString(value string) (assetRef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return assetRef{}, fmt.Errorf("empty asset reference")
	}
	if strings.HasPrefix(value, "artifact://") {
		return assetRef{ArtifactURI: value}, nil
	}
	return assetRef{Path: value}, nil
}

func parseAssetRefMap(value map[string]any) (assetRef, error) {
	ref := assetRef{
		Path:        firstNonEmpty(stringValue(value["path"]), stringValue(value["file"])),
		ArtifactURI: firstNonEmpty(stringValue(value["artifact_uri"]), stringValue(value["uri"])),
		MIMEType:    strings.TrimSpace(stringValue(value["mime_type"])),
		FileName:    strings.TrimSpace(stringValue(value["file_name"])),
	}
	if strings.TrimSpace(ref.Path) == "" && strings.TrimSpace(ref.ArtifactURI) == "" {
		return assetRef{}, fmt.Errorf("asset reference must include path or artifact_uri")
	}
	return ref, nil
}

func notConfigured(rt Runtime, call agent.ToolCall, kind, message string) (contextengine.ToolResult, error) {
	return rt.JSONResult(call, map[string]any{
		"kind":      kind,
		"status":    "not_configured",
		"message":   message,
		"providers": registryProviderInfo(rt.MediaGenerationRegistry(), kind),
	})
}

func registryProviderInfo(registry *gen.Registry, kind string) any {
	if registry == nil {
		return []any{}
	}
	switch kind {
	case "image":
		return registry.ImageProviderInfo()
	case "video":
		return registry.VideoProviderInfo()
	case "music":
		return registry.MusicProviderInfo()
	default:
		return []any{}
	}
}

func artifactSummaries(items []resultmodel.ResultArtifact) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"name":         item.Name,
			"uri":          item.URI,
			"content_type": item.ContentType,
			"size_bytes":   item.SizeBytes,
		})
	}
	return out
}

func mergeMap(dst map[string]any, extra map[string]any) {
	if len(extra) == 0 {
		return
	}
	for key, value := range extra {
		if _, exists := dst[key]; exists {
			continue
		}
		dst[key] = value
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultContentType(kind string) string {
	switch kind {
	case "image.generated":
		return "image/png"
	case "video.generated":
		return "video/mp4"
	case "music.generated":
		return "audio/mpeg"
	default:
		return "application/octet-stream"
	}
}

func fileExtForContentType(contentType string) string {
	switch strings.TrimSpace(strings.ToLower(contentType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/webm":
		return ".webm"
	case "video/mp4":
		return ".mp4"
	case "audio/wav", "audio/wave":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	default:
		return ".bin"
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", value)
	}
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case nil:
		return fallback
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func boolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case nil:
		return fallback
	case bool:
		return typed
	default:
		return fallback
	}
}

func generationOutputSchema(kind string) map[string]any {
	return objectSchema(map[string]any{
		"kind":     stringSchema("Generation domain."),
		"provider": stringSchema("Provider identifier."),
		"model":    stringSchema("Provider model."),
		"count":    integerSchema("Number of generated artifacts."),
		"artifacts": arraySchema(objectSchema(map[string]any{
			"name":         stringSchema("Artifact display name."),
			"uri":          stringSchema("Artifact URI."),
			"content_type": stringSchema("Artifact MIME type."),
			"size_bytes":   integerSchema("Artifact size in bytes."),
		}), "Generated artifacts."),
	}, "kind", "provider", "model", "count")
}

func imageGenerateInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"action":       enumStringSchema("Action to perform.", "generate", "list"),
		"provider":     stringSchema("Optional provider id. Omit to use the first configured image-generation provider."),
		"model":        stringSchema("Optional provider-specific model override."),
		"prompt":       stringSchema("Generation prompt."),
		"count":        integerSchema("Number of images to generate."),
		"size":         stringSchema("Provider-specific output size such as 1024x1024."),
		"aspect_ratio": stringSchema("Optional aspect ratio hint."),
		"resolution":   stringSchema("Optional provider-specific resolution hint."),
		"timeout_ms":   integerSchema("Optional request timeout override in milliseconds."),
		"references":   assetArraySchema("Optional reference images. Each item can be a path string, artifact URI, or object."),
	})
}

func videoGenerateInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"action":           enumStringSchema("Action to perform.", "generate", "list"),
		"provider":         stringSchema("Optional provider id."),
		"model":            stringSchema("Optional provider-specific model override."),
		"prompt":           stringSchema("Generation prompt."),
		"duration_seconds": integerSchema("Optional target duration in seconds."),
		"size":             stringSchema("Optional size such as 1280x720."),
		"aspect_ratio":     stringSchema("Optional aspect ratio hint."),
		"resolution":       stringSchema("Optional provider-specific resolution hint."),
		"audio":            booleanSchema("Whether to request generated audio when the provider supports it."),
		"timeout_ms":       integerSchema("Optional request timeout override in milliseconds."),
		"input_images":     assetArraySchema("Optional image reference inputs."),
		"input_videos":     assetArraySchema("Optional video reference inputs."),
	})
}

func musicGenerateInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"action":           enumStringSchema("Action to perform.", "generate", "list"),
		"provider":         stringSchema("Optional provider id."),
		"model":            stringSchema("Optional provider-specific model override."),
		"prompt":           stringSchema("Generation prompt."),
		"lyrics":           stringSchema("Optional lyrics input."),
		"instrumental":     booleanSchema("Request instrumental output with no vocals."),
		"duration_seconds": integerSchema("Optional target duration in seconds."),
		"format":           stringSchema("Optional output format."),
		"timeout_ms":       integerSchema("Optional request timeout override in milliseconds."),
		"input_images":     assetArraySchema("Optional image references for providers that support conditioned generation."),
	})
}

func assetArraySchema(description string) map[string]any {
	return arraySchema(map[string]any{
		"oneOf": []map[string]any{
			{"type": "string"},
			objectSchema(map[string]any{
				"path":         stringSchema("Workspace-relative file path."),
				"artifact_uri": stringSchema("Artifact URI reference."),
				"mime_type":    stringSchema("Optional MIME type override."),
				"file_name":    stringSchema("Optional file name override."),
			}),
		},
	}, description)
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enumStringSchema(description string, values ...string) map[string]any {
	out := stringSchema(description)
	if len(values) > 0 {
		out["enum"] = values
	}
	return out
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func arraySchema(items map[string]any, description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       items,
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	out := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}
