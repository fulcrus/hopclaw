package stt

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Sherpa ONNX STT provider constants
// ---------------------------------------------------------------------------

const (
	providerSherpa = "sherpa"

	sherpaBinaryName = "sherpa-onnx-offline"

	// sherpaModelTokens is the filename for the tokens model file.
	sherpaModelTokens = "tokens.txt"

	// sherpaModelEncoder is the filename for the encoder model file.
	sherpaModelEncoder = "encoder-epoch-99-avg-1.onnx"

	// sherpaModelDecoder is the filename for the decoder model file.
	sherpaModelDecoder = "decoder-epoch-99-avg-1.onnx"

	// sherpaModelJoiner is the filename for the joiner model file.
	sherpaModelJoiner = "joiner-epoch-99-avg-1.onnx"
)

// ---------------------------------------------------------------------------
// Sherpa ONNX STT provider
// ---------------------------------------------------------------------------

// SherpaProvider uses the sherpa-onnx-offline CLI for local speech-to-text
// transcription. It requires no API key and runs entirely on the local machine.
type SherpaProvider struct {
	modelPath string // path to sherpa-onnx model directory
}

// NewSherpaProvider returns a provider that invokes the sherpa-onnx-offline CLI
// with model files located under modelPath.
func NewSherpaProvider(modelPath string) *SherpaProvider {
	return &SherpaProvider{
		modelPath: modelPath,
	}
}

// Name returns "sherpa".
func (p *SherpaProvider) Name() string { return providerSherpa }

// Transcribe calls the sherpa-onnx-offline CLI with the audio file specified
// in req.Filename and returns the transcribed text. The audio data from
// req.Audio is ignored; this provider reads from the filesystem path in
// req.Filename directly.
func (p *SherpaProvider) Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	if req.Filename == "" {
		return nil, fmt.Errorf("sherpa stt: filename is required")
	}

	binaryPath, err := exec.LookPath(sherpaBinaryName)
	if err != nil {
		return nil, fmt.Errorf("sherpa stt: %s not found in PATH: %w", sherpaBinaryName, err)
	}

	tokens := filepath.Join(p.modelPath, sherpaModelTokens)
	encoder := filepath.Join(p.modelPath, sherpaModelEncoder)
	decoder := filepath.Join(p.modelPath, sherpaModelDecoder)
	joiner := filepath.Join(p.modelPath, sherpaModelJoiner)

	args := []string{
		"--tokens", tokens,
		"--encoder", encoder,
		"--decoder", decoder,
		"--joiner", joiner,
		req.Filename,
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sherpa stt: execution failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	text := strings.TrimSpace(stdout.String())

	return &TranscribeResult{
		Text: text,
	}, nil
}
