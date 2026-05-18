package voice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// InterruptionController
// ---------------------------------------------------------------------------

func TestInterruptionControllerDisabled(t *testing.T) {
	t.Parallel()

	ic := NewInterruptionController(InterruptionConfig{Enabled: false})
	ctx, done := ic.StartPlayback(context.Background())
	defer done()

	if !ic.IsPlaying() {
		t.Fatal("expected IsPlaying to be true after StartPlayback")
	}

	// Speech detected while disabled should NOT cancel playback.
	ic.OnUserSpeechDetected()
	select {
	case <-ctx.Done():
		t.Fatal("expected context to NOT be cancelled when disabled")
	default:
	}
}

func TestInterruptionControllerEnabled(t *testing.T) {
	t.Parallel()

	interrupted := false
	ic := NewInterruptionController(InterruptionConfig{
		Enabled:        true,
		SilenceTimeout: 100 * time.Millisecond,
	})
	ic.SetOnInterrupt(func() { interrupted = true })

	ctx, done := ic.StartPlayback(context.Background())
	defer done()

	if !ic.IsPlaying() {
		t.Fatal("expected IsPlaying to be true")
	}

	ic.OnUserSpeechDetected()

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("expected context to be cancelled")
	}

	if !interrupted {
		t.Fatal("expected onInterrupt callback to fire")
	}
	if ic.IsPlaying() {
		t.Fatal("expected IsPlaying to be false after interruption")
	}
}

func TestInterruptionControllerSilenceTimeout(t *testing.T) {
	t.Parallel()

	ic := NewInterruptionController(InterruptionConfig{
		Enabled:        true,
		SilenceTimeout: 50 * time.Millisecond,
	})

	err := ic.WaitForSilence(context.Background())
	if err != nil {
		t.Fatalf("WaitForSilence() error = %v", err)
	}
}

func TestInterruptionControllerWaitForSilenceCancelled(t *testing.T) {
	t.Parallel()

	ic := NewInterruptionController(InterruptionConfig{
		Enabled:        true,
		SilenceTimeout: 10 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ic.WaitForSilence(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestInterruptionControllerDefaultTimeout(t *testing.T) {
	t.Parallel()

	ic := NewInterruptionController(InterruptionConfig{
		Enabled:        true,
		SilenceTimeout: 0,
	})

	if ic.silenceTimeout != defaultSilenceTimeout {
		t.Fatalf("silenceTimeout = %v, want %v", ic.silenceTimeout, defaultSilenceTimeout)
	}
}

func TestInterruptionControllerDoneClears(t *testing.T) {
	t.Parallel()

	ic := NewInterruptionController(InterruptionConfig{Enabled: true})
	_, done := ic.StartPlayback(context.Background())

	if !ic.IsPlaying() {
		t.Fatal("expected IsPlaying to be true")
	}

	done() // normal playback completion

	if ic.IsPlaying() {
		t.Fatal("expected IsPlaying to be false after done()")
	}
}

func TestInterruptionControllerNotActiveNoInterrupt(t *testing.T) {
	t.Parallel()

	ic := NewInterruptionController(InterruptionConfig{Enabled: true})
	// OnUserSpeechDetected when not playing should not panic.
	ic.OnUserSpeechDetected()
}

// ---------------------------------------------------------------------------
// WakeWordConfig
// ---------------------------------------------------------------------------

func TestWakeWordConfigContainsWakeWord(t *testing.T) {
	t.Parallel()

	cfg := &WakeWordConfig{
		Words:   []string{"hopclaw", "claude"},
		Enabled: true,
	}

	tests := []struct {
		text string
		want bool
	}{
		{"Hey HopClaw, what's up?", true},
		{"hey claude", true},
		{"nothing here", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := cfg.ContainsWakeWord(tt.text); got != tt.want {
			t.Fatalf("ContainsWakeWord(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestWakeWordConfigDisabled(t *testing.T) {
	t.Parallel()

	cfg := &WakeWordConfig{
		Words:   []string{"hopclaw"},
		Enabled: false,
	}

	if cfg.ContainsWakeWord("hey hopclaw") {
		t.Fatal("expected false when disabled")
	}
}

func TestWakeWordConfigGetWords(t *testing.T) {
	t.Parallel()

	cfg := &WakeWordConfig{
		Words:   []string{"a", "b", "c"},
		Enabled: true,
	}

	words := cfg.GetWords()
	if len(words) != 3 {
		t.Fatalf("len = %d, want 3", len(words))
	}
	// Verify it's a copy.
	words[0] = "modified"
	if cfg.Words[0] != "a" {
		t.Fatal("GetWords should return a copy")
	}
}

func TestWakeWordConfigSetWords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fp := filepath.Join(dir, "voicewake.json")

	cfg := &WakeWordConfig{
		Words:    []string{"original"},
		Enabled:  true,
		filePath: fp,
	}

	if err := cfg.SetWords([]string{"new1", "new2"}); err != nil {
		t.Fatalf("SetWords() error = %v", err)
	}

	words := cfg.GetWords()
	if len(words) != 2 || words[0] != "new1" || words[1] != "new2" {
		t.Fatalf("words = %v", words)
	}

	// Verify persisted.
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Fatal("expected file to exist after SetWords")
	}
}

func TestWakeWordConfigSetEnabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fp := filepath.Join(dir, "voicewake.json")

	cfg := &WakeWordConfig{
		Words:    []string{"hopclaw"},
		Enabled:  false,
		filePath: fp,
	}

	if err := cfg.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}

	if !cfg.Enabled {
		t.Fatal("expected Enabled to be true")
	}
}

// ---------------------------------------------------------------------------
// Edge provider
// ---------------------------------------------------------------------------

func TestEdgeProviderName(t *testing.T) {
	t.Parallel()

	p, err := newEdgeProvider(EdgeConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "edge" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "edge")
	}
}

func TestEdgeProviderDefaultVoice(t *testing.T) {
	t.Parallel()

	p, err := newEdgeProvider(EdgeConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.voice != defaultEdgeVoice {
		t.Fatalf("voice = %q, want %q", p.voice, defaultEdgeVoice)
	}
}

func TestEdgeProviderCustomVoice(t *testing.T) {
	t.Parallel()

	p, err := newEdgeProvider(EdgeConfig{Voice: "en-GB-SoniaNeural"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.voice != "en-GB-SoniaNeural" {
		t.Fatalf("voice = %q", p.voice)
	}
}

func TestEdgeProviderSynthesizeReturnsError(t *testing.T) {
	t.Parallel()

	p, _ := newEdgeProvider(EdgeConfig{})
	_, err := p.Synthesize(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error from edge stub")
	}
}

// ---------------------------------------------------------------------------
// OpenAI provider creation
// ---------------------------------------------------------------------------

func TestOpenAIProviderDefaults(t *testing.T) {
	t.Parallel()

	p, err := newOpenAIProvider(OpenAIConfig{APIKey: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.model != defaultOpenAIModel {
		t.Fatalf("model = %q, want %q", p.model, defaultOpenAIModel)
	}
	if p.voice != defaultOpenAIVoice {
		t.Fatalf("voice = %q, want %q", p.voice, defaultOpenAIVoice)
	}
}

func TestOpenAIProviderCustom(t *testing.T) {
	t.Parallel()

	p, err := newOpenAIProvider(OpenAIConfig{
		APIKey: "key",
		Model:  "tts-1-hd",
		Voice:  "nova",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.model != "tts-1-hd" {
		t.Fatalf("model = %q", p.model)
	}
	if p.voice != "nova" {
		t.Fatalf("voice = %q", p.voice)
	}
}

func TestOpenAIProviderMissingKey(t *testing.T) {
	t.Parallel()

	_, err := newOpenAIProvider(OpenAIConfig{})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

// ---------------------------------------------------------------------------
// ElevenLabs provider creation
// ---------------------------------------------------------------------------

func TestElevenLabsProviderDefaults(t *testing.T) {
	t.Parallel()

	p, err := newElevenLabsProvider(ElevenLabsConfig{APIKey: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.voiceID != defaultElevenLabsVoiceID {
		t.Fatalf("voiceID = %q, want %q", p.voiceID, defaultElevenLabsVoiceID)
	}
	if p.modelID != defaultElevenLabsModelID {
		t.Fatalf("modelID = %q, want %q", p.modelID, defaultElevenLabsModelID)
	}
}

func TestElevenLabsProviderMissingKey(t *testing.T) {
	t.Parallel()

	_, err := newElevenLabsProvider(ElevenLabsConfig{})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestElevenLabsProviderCustom(t *testing.T) {
	t.Parallel()

	p, err := newElevenLabsProvider(ElevenLabsConfig{
		APIKey:  "key",
		VoiceID: "custom-voice",
		ModelID: "eleven_multilingual_v2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.voiceID != "custom-voice" {
		t.Fatalf("voiceID = %q", p.voiceID)
	}
	if p.modelID != "eleven_multilingual_v2" {
		t.Fatalf("modelID = %q", p.modelID)
	}
}

// ---------------------------------------------------------------------------
// Synthesize fallback chain
// ---------------------------------------------------------------------------

func TestSynthesizeNoFallback(t *testing.T) {
	t.Parallel()

	cfg := Config{Provider: "edge", Fallback: ""}
	_, err := Synthesize(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error with no fallback")
	}
}
