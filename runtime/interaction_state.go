package runtime

import "strings"

// InteractionSessionState is the normalized runtime interaction state used by
// ingress, policy, and execution envelope selection.
type InteractionSessionState string

const (
	InteractionSessionStateIdle              InteractionSessionState = "idle"
	InteractionSessionStateRunning           InteractionSessionState = "running"
	InteractionSessionStateWaitingApproval   InteractionSessionState = "waiting_approval"
	InteractionSessionStateWaitingInput      InteractionSessionState = "waiting_input"
	InteractionSessionStateCompletedRecently InteractionSessionState = "completed_recently"
	InteractionSessionStateFailedRecently    InteractionSessionState = "failed_recently"
)

func (s InteractionSessionState) String() string {
	return string(s)
}

func (s InteractionSessionState) Normalize() InteractionSessionState {
	switch InteractionSessionState(strings.TrimSpace(string(s))) {
	case InteractionSessionStateRunning,
		InteractionSessionStateWaitingApproval,
		InteractionSessionStateWaitingInput,
		InteractionSessionStateCompletedRecently,
		InteractionSessionStateFailedRecently:
		return s
	default:
		return InteractionSessionStateIdle
	}
}
