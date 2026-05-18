package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ---------------------------------------------------------------------------
// Local whisper STT constants
// ---------------------------------------------------------------------------

const (
	whisperBinaryName    = "whisper"
	whisperCppBinaryName = "main" // whisper.cpp default binary name
	localOutputFormat    = "json"
	localTempFilePattern = "stt-audio-*"
)

// ---------------------------------------------------------------------------
// Local whisper STT provider
// ---------------------------------------------------------------------------

type localProvider struct {
	binaryPath string
}

func newLocalProvider(_ ProviderConfig) (*localProvider, error) {
	binaryPath, err := resolveWhisperBinary()
	if err != nil {
		return nil, err
	}
	return &localProvider{binaryPath: binaryPath}, nil
}

func (p *localProvider) Name() string { return providerLocal }

// ---------------------------------------------------------------------------
// Binary resolution
// ---------------------------------------------------------------------------

// resolveWhisperBinary searches the system PATH for a whisper CLI binary.
// It first looks for the standard "whisper" command, then falls back to
// the whisper.cpp "main" binary.
func resolveWhisperBinary() (string, error) {
	// Try the standard whisper Python CLI first.
	if path, err := exec.LookPath(whisperBinaryName); err == nil {
		return path, nil
	}

	// Fall back to whisper.cpp main binary.
	if path, err := exec.LookPath(whisperCppBinaryName); err == nil {
		return path, nil
	}

	return "", ErrLocalWhisperNotAvailable
}

// ---------------------------------------------------------------------------
// Transcribe
// ---------------------------------------------------------------------------

func (p *localProvider) Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Write audio data to a temporary file because the whisper CLI requires
	// a file path as input.
	ext := filepath.Ext(req.Filename)
	tmpFile, err := os.CreateTemp("", localTempFilePattern+ext)
	if err != nil {
		return nil, fmt.Errorf("local stt: creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	limitedAudio := io.LimitReader(req.Audio, maxAudioSize+1)
	written, err := io.Copy(tmpFile, limitedAudio)
	if err != nil {
		return nil, fmt.Errorf("local stt: writing audio to temp file: %w", err)
	}
	if written > maxAudioSize {
		return nil, fmt.Errorf("local stt: audio exceeds maximum size of %d bytes", maxAudioSize)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("local stt: closing temp file: %w", err)
	}

	args := p.buildArgs(req, tmpFile.Name())

	cmd := exec.CommandContext(ctx, p.binaryPath, args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("local stt: whisper exited with error: %s", exitErr.Stderr)
		}
		return nil, fmt.Errorf("local stt: running whisper: %w", err)
	}

	return parseLocalOutput(output)
}

// ---------------------------------------------------------------------------
// Argument building
// ---------------------------------------------------------------------------

func (p *localProvider) buildArgs(req TranscribeRequest, audioPath string) []string {
	args := []string{audioPath, "--output_format", localOutputFormat}

	if req.Language != "" {
		args = append(args, "--language", req.Language)
	}

	if req.Prompt != "" {
		args = append(args, "--initial_prompt", req.Prompt)
	}

	return args
}

// ---------------------------------------------------------------------------
// Output parsing
// ---------------------------------------------------------------------------

// localWhisperOutput represents the JSON output from the whisper CLI.
type localWhisperOutput struct {
	Text     string                `json:"text"`
	Language string                `json:"language"`
	Segments []localWhisperSegment `json:"segments"`
}

// localWhisperSegment represents a single segment in the whisper CLI JSON output.
type localWhisperSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func parseLocalOutput(output []byte) (*TranscribeResult, error) {
	var whisperOut localWhisperOutput
	if err := json.Unmarshal(output, &whisperOut); err != nil {
		return nil, fmt.Errorf("local stt: parsing whisper output: %w", err)
	}

	segments := make([]Segment, len(whisperOut.Segments))
	for i, s := range whisperOut.Segments {
		segments[i] = Segment{
			Start: time.Duration(s.Start * float64(time.Second)),
			End:   time.Duration(s.End * float64(time.Second)),
			Text:  s.Text,
		}
	}

	return &TranscribeResult{
		Text:     whisperOut.Text,
		Language: whisperOut.Language,
		Segments: segments,
	}, nil
}
