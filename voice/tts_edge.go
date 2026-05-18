package voice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// Edge TTS constants
// ---------------------------------------------------------------------------

const (
	defaultEdgeVoice = "en-US-AriaNeural"
)

// ---------------------------------------------------------------------------
// Edge TTS provider
//
// Delegates to the external edge-tts CLI tool (pip install edge-tts).
// The tool communicates via WebSocket with Microsoft's speech service.
// ---------------------------------------------------------------------------

type edgeProvider struct {
	voice string
}

func newEdgeProvider(cfg EdgeConfig) (*edgeProvider, error) {
	voice := cfg.Voice
	if voice == "" {
		voice = defaultEdgeVoice
	}
	return &edgeProvider{voice: voice}, nil
}

func (p *edgeProvider) Name() string { return providerEdge }

func (p *edgeProvider) Synthesize(ctx context.Context, text string) (*AudioResult, error) {
	edgeTTS, err := exec.LookPath("edge-tts")
	if err != nil {
		return nil, fmt.Errorf("edge tts: edge-tts not found in PATH; install with: pip install edge-tts")
	}

	tmpDir, err := os.MkdirTemp("", "edge-tts-*")
	if err != nil {
		return nil, fmt.Errorf("edge tts: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outFile := filepath.Join(tmpDir, "output.mp3")
	cmd := exec.CommandContext(ctx, edgeTTS,
		"--voice", p.voice,
		"--text", text,
		"--write-media", outFile,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("edge tts: %w: %s", err, string(out))
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("edge tts: read output: %w", err)
	}

	return &AudioResult{
		Data:        data,
		ContentType: "audio/mpeg",
	}, nil
}
