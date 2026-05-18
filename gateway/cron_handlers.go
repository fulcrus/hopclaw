package gateway

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/fulcrus/hopclaw/cron"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

const (
	cronIDBytes = 8 // 16 hex chars
)

type cronJobCreateRequest struct {
	Name         string         `json:"name"`
	Enabled      *bool          `json:"enabled,omitempty"`
	Schedule     cron.Schedule  `json:"schedule"`
	Payload      cron.Payload   `json:"payload"`
	Delivery     *cron.Delivery `json:"delivery,omitempty"`
	SessionKey   string         `json:"session_key,omitempty"`
	Model        string         `json:"model,omitempty"`
	AutomationID string         `json:"automation_id,omitempty"`
}

type cronJobPatchRequest struct {
	Name         *string        `json:"name,omitempty"`
	Enabled      *bool          `json:"enabled,omitempty"`
	Schedule     *cron.Schedule `json:"schedule,omitempty"`
	Payload      *cron.Payload  `json:"payload,omitempty"`
	Delivery     *cron.Delivery `json:"delivery,omitempty"`
	SessionKey   *string        `json:"session_key,omitempty"`
	Model        *string        `json:"model,omitempty"`
	AutomationID *string        `json:"automation_id,omitempty"`
}

type cronJobResponse struct {
	Job cron.Job `json:"job"`
}

type cronJobListResponse struct {
	Items []cron.Job `json:"items"`
	Count int        `json:"count"`
}

type cronServiceStatusResponse struct {
	Running  bool `json:"running"`
	JobCount int  `json:"job_count"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateCronID() (string, error) {
	b := make([]byte, cronIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
