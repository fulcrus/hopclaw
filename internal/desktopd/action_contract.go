package desktopd

const (
	actionStatusAttempted = "attempted"
	actionStatusVerified  = "verified"
	actionStatusRecovered = "recovered"
	actionStatusFailed    = "failed"
)

func annotateActionResult(data map[string]any, status, transport, strategy string, confidence float64, evidence map[string]any) map[string]any {
	if data == nil {
		data = make(map[string]any)
	}
	if status != "" {
		data["action_status"] = status
	}
	if transport != "" {
		data["transport"] = transport
	}
	if strategy != "" {
		data["strategy"] = strategy
	}
	if confidence > 0 {
		data["confidence"] = confidence
	}
	if len(evidence) > 0 {
		data["evidence"] = evidence
	}
	return data
}
