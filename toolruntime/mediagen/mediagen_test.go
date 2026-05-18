package mediagen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	gen "github.com/fulcrus/hopclaw/mediagen"
)

func TestHandleImageGenerateList(t *testing.T) {
	t.Parallel()

	registry := gen.NewRegistry()
	registry.Register(stubImageProvider{id: "openai"})
	rt := &stubRuntime{registry: registry, store: artifact.NewInMemoryStore()}

	result, err := handleImageGenerate(context.Background(), rt, agent.ToolCall{
		ID:   "call-1",
		Name: "image.generate",
		Input: map[string]any{
			"action": "list",
		},
	})
	if err != nil {
		t.Fatalf("handleImageGenerate(list) error = %v", err)
	}
	if result.Content == "" {
		t.Fatal("expected list payload content")
	}
}

func TestHandleImageGenerateStoresArtifact(t *testing.T) {
	t.Parallel()

	registry := gen.NewRegistry()
	registry.Register(stubImageProvider{id: "openai"})
	rt := &stubRuntime{registry: registry, store: artifact.NewInMemoryStore()}

	result, err := handleImageGenerate(context.Background(), rt, agent.ToolCall{
		ID:   "call-1",
		Name: "image.generate",
		Input: map[string]any{
			"prompt": "draw a bridge",
		},
	})
	if err != nil {
		t.Fatalf("handleImageGenerate() error = %v", err)
	}
	if result.ArtifactURI == "" {
		t.Fatal("expected artifact uri")
	}
	body, _, err := rt.store.Read(context.Background(), result.ArtifactURI)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(body) != "image-bytes" {
		t.Fatalf("artifact body = %q", string(body))
	}
}

func TestHandleImageGenerateFallsBackAcrossProviders(t *testing.T) {
	t.Parallel()

	registry := gen.NewRegistry()
	registry.Register(failingImageProvider{id: "broken"})
	registry.Register(stubImageProvider{id: "openai"})
	rt := &stubRuntime{registry: registry, store: artifact.NewInMemoryStore()}

	result, err := handleImageGenerate(context.Background(), rt, agent.ToolCall{
		ID:   "call-1",
		Name: "image.generate",
		Input: map[string]any{
			"prompt": "draw a harbor",
		},
	})
	if err != nil {
		t.Fatalf("handleImageGenerate() error = %v", err)
	}
	attempts, ok := result.Structured["attempts"].([]map[string]any)
	if !ok || len(attempts) != 1 {
		t.Fatalf("attempts = %#v", result.Structured["attempts"])
	}
	if attempts[0]["provider"] != "broken" {
		t.Fatalf("attempt provider = %#v", attempts[0]["provider"])
	}
	if fallbackUsed, ok := result.Structured["fallback_used"].(bool); !ok || !fallbackUsed {
		t.Fatalf("fallback_used = %#v", result.Structured["fallback_used"])
	}
}

func TestHandleImageGenerateAppliesNormalizationMetadata(t *testing.T) {
	t.Parallel()

	provider := &capturingImageProvider{id: "openai"}
	registry := gen.NewRegistry()
	registry.Register(provider)
	rt := &stubRuntime{registry: registry, store: artifact.NewInMemoryStore()}

	result, err := handleImageGenerate(context.Background(), rt, agent.ToolCall{
		ID:   "call-2",
		Name: "image.generate",
		Input: map[string]any{
			"prompt":       "draw a studio portrait",
			"aspect_ratio": "16:10",
		},
	})
	if err != nil {
		t.Fatalf("handleImageGenerate() error = %v", err)
	}
	if provider.last.Size != "1536x1024" {
		t.Fatalf("provider.last.Size = %q, want 1536x1024", provider.last.Size)
	}
	normalization, ok := result.Structured["normalization"].(map[string]any)
	if !ok {
		t.Fatalf("normalization = %#v", result.Structured["normalization"])
	}
	size, ok := normalization["size"].(map[string]any)
	if !ok || size["derived_from"] != "aspect_ratio" {
		t.Fatalf("normalization.size = %#v", normalization["size"])
	}
}

func TestLoadAssetFromPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "input.png")
	if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rt := &stubRuntime{root: root, maxReadBytes: 10}
	asset, err := loadAsset(context.Background(), rt, assetRef{Path: "input.png"})
	if err != nil {
		t.Fatalf("loadAsset() error = %v", err)
	}
	if asset.FileName != "input.png" {
		t.Fatalf("FileName = %q", asset.FileName)
	}
}

type stubRuntime struct {
	root         string
	registry     *gen.Registry
	store        artifact.Store
	maxReadBytes int
}

func (r *stubRuntime) JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error) {
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    "json",
		Structured: payload,
	}.Normalized(), nil
}

func (r *stubRuntime) ResolvePath(input string) (string, error) {
	return filepath.Join(r.root, input), nil
}

func (r *stubRuntime) DisplayPath(absPath string) string { return absPath }

func (r *stubRuntime) ReadArtifact(ctx context.Context, uri string) ([]byte, string, error) {
	if r.store == nil {
		return nil, "", nil
	}
	return r.store.Read(ctx, uri)
}

func (r *stubRuntime) PutArtifact(ctx context.Context, _ agent.ToolCall, req artifact.PutRequest) (*artifact.Blob, error) {
	return r.store.Put(ctx, req)
}

func (r *stubRuntime) MediaGenerationRegistry() *gen.Registry { return r.registry }
func (r *stubRuntime) MaxReadBytes() int                      { return r.maxReadBytes }

type stubImageProvider struct {
	id string
}

func (p stubImageProvider) ID() string    { return p.id }
func (p stubImageProvider) Label() string { return p.id }
func (p stubImageProvider) DefaultImageModel() string {
	return "gpt-image-1"
}
func (p stubImageProvider) ImageModels() []string {
	return []string{"gpt-image-1"}
}
func (p stubImageProvider) ImageCapabilities() gen.ImageCapabilities {
	return gen.ImageCapabilities{MaxCount: 1}
}
func (p stubImageProvider) GenerateImage(context.Context, gen.ImageRequest) (*gen.ImageResult, error) {
	return &gen.ImageResult{
		Model: "gpt-image-1",
		Images: []gen.GeneratedAsset{{
			Buffer:   []byte("image-bytes"),
			MIMEType: "image/png",
			FileName: "generated.png",
		}},
	}, nil
}

type failingImageProvider struct {
	id string
}

func (p failingImageProvider) ID() string                { return p.id }
func (p failingImageProvider) Label() string             { return p.id }
func (p failingImageProvider) DefaultImageModel() string { return "broken-model" }
func (p failingImageProvider) ImageModels() []string     { return []string{"broken-model"} }
func (p failingImageProvider) ImageCapabilities() gen.ImageCapabilities {
	return gen.ImageCapabilities{MaxCount: 1}
}
func (p failingImageProvider) GenerateImage(context.Context, gen.ImageRequest) (*gen.ImageResult, error) {
	return nil, errors.New("provider offline")
}

type capturingImageProvider struct {
	id   string
	last gen.ImageRequest
}

func (p *capturingImageProvider) ID() string                { return p.id }
func (p *capturingImageProvider) Label() string             { return p.id }
func (p *capturingImageProvider) DefaultImageModel() string { return "gpt-image-1" }
func (p *capturingImageProvider) ImageModels() []string     { return []string{"gpt-image-1"} }
func (p *capturingImageProvider) ImageCapabilities() gen.ImageCapabilities {
	return gen.ImageCapabilities{
		MaxCount:            1,
		SupportsSize:        true,
		SupportsAspectRatio: false,
		Sizes:               []string{"1024x1024", "1536x1024", "1024x1536"},
	}
}
func (p *capturingImageProvider) GenerateImage(_ context.Context, req gen.ImageRequest) (*gen.ImageResult, error) {
	p.last = req
	return &gen.ImageResult{
		Model: "gpt-image-1",
		Images: []gen.GeneratedAsset{{
			Buffer:   []byte("image-bytes"),
			MIMEType: "image/png",
			FileName: "generated.png",
		}},
	}, nil
}
