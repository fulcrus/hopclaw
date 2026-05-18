package voice

import (
	"context"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Interruption constants
// ---------------------------------------------------------------------------

const defaultSilenceTimeout = 2 * time.Second

// ---------------------------------------------------------------------------
// InterruptionConfig
// ---------------------------------------------------------------------------

// InterruptionConfig holds settings for TTS interruption behaviour.
type InterruptionConfig struct {
	Enabled        bool          `json:"enabled" yaml:"enabled"`
	SilenceTimeout time.Duration `json:"silence_timeout" yaml:"silence_timeout"`
}

// ---------------------------------------------------------------------------
// InterruptionController
// ---------------------------------------------------------------------------

// InterruptionController manages TTS interruption when user speech is detected.
// When a VAD (voice activity detection) signal arrives during TTS playback the
// controller cancels the current playback context, allowing the audio pipeline
// to stop immediately.
type InterruptionController struct {
	mu          sync.Mutex
	active      bool               // whether TTS is currently playing
	cancelFn    context.CancelFunc // cancel current TTS playback
	onInterrupt func()             // optional callback when interrupted

	// Config
	enabled        bool
	silenceTimeout time.Duration // pause before considering the user's turn complete
}

// NewInterruptionController creates a new interruption controller.
func NewInterruptionController(cfg InterruptionConfig) *InterruptionController {
	timeout := cfg.SilenceTimeout
	if timeout <= 0 {
		timeout = defaultSilenceTimeout
	}
	return &InterruptionController{
		enabled:        cfg.Enabled,
		silenceTimeout: timeout,
	}
}

// SetOnInterrupt registers a callback that fires when TTS playback is
// interrupted by user speech.
func (ic *InterruptionController) SetOnInterrupt(fn func()) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.onInterrupt = fn
}

// StartPlayback marks TTS as active and returns a derived context that will be
// cancelled if the user starts speaking. The caller must invoke the returned
// cancel func when playback finishes normally (or defer it).
func (ic *InterruptionController) StartPlayback(ctx context.Context) (context.Context, context.CancelFunc) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	playCtx, cancel := context.WithCancel(ctx)
	ic.active = true
	ic.cancelFn = cancel

	// Wrap cancel so we also clear state when playback ends normally.
	done := func() {
		ic.mu.Lock()
		defer ic.mu.Unlock()
		ic.active = false
		ic.cancelFn = nil
		cancel()
	}
	return playCtx, done
}

// OnUserSpeechDetected should be called when VAD detects user speech. If TTS
// is currently active and interruption is enabled, it cancels the playback
// context and invokes the on-interrupt callback.
func (ic *InterruptionController) OnUserSpeechDetected() {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if !ic.enabled || !ic.active {
		return
	}

	ic.active = false
	if ic.cancelFn != nil {
		ic.cancelFn()
		ic.cancelFn = nil
	}
	if ic.onInterrupt != nil {
		ic.onInterrupt()
	}
}

// IsPlaying reports whether TTS is currently active.
func (ic *InterruptionController) IsPlaying() bool {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	return ic.active
}

// WaitForSilence blocks until no speech has been detected for the configured
// silence timeout, indicating the user's turn is complete. It returns early if
// the context is cancelled.
func (ic *InterruptionController) WaitForSilence(ctx context.Context) error {
	ic.mu.Lock()
	timeout := ic.silenceTimeout
	ic.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
