package richedit

// OverlayLine describes one rendered row in a prompt-local overlay panel.
type OverlayLine struct {
	Text     string
	Selected bool
}

// OverlayPanel describes a panel rendered below the composer.
// When Modal is true, all key events are routed to the overlay controller.
// When Modal is false, the panel renders passively and only Esc is reserved
// to dismiss it while normal editing continues in the composer.
type OverlayPanel struct {
	Title   string
	Summary string
	Lines   []OverlayLine
	Actions string
	Tip     string
	Modal   bool
}

// OverlayResult is returned by an OverlayController after handling a key.
type OverlayResult struct {
	Handled bool
	Submit  string
}

// OverlayController handles modal picker and confirmation panels shown in the
// prompt area. It must avoid writing directly to the terminal; instead it
// should update its own state or return a slash command to submit.
type OverlayController interface {
	Panel() *OverlayPanel
	HandleOverlayKey(KeyEvent) (OverlayResult, error)
}
