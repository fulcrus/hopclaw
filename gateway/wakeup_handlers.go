package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/wakeup"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	wakeupIDBytes = 8 // 16 hex chars
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type wakeupTriggerCreateRequest struct {
	Name         string            `json:"name"`
	Schedule     string            `json:"schedule"`
	Channel      string            `json:"channel"`
	SessionKey   string            `json:"session_key,omitempty"`
	LegacyAgent  *string           `json:"agent,omitempty"`
	Message      string            `json:"message"`
	Model        string            `json:"model,omitempty"`
	AutomationID string            `json:"automation_id,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	Timezone     string            `json:"timezone,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type wakeupTriggerPatchRequest struct {
	Name         *string            `json:"name,omitempty"`
	Schedule     *string            `json:"schedule,omitempty"`
	Channel      *string            `json:"channel,omitempty"`
	SessionKey   *string            `json:"session_key,omitempty"`
	LegacyAgent  *string            `json:"agent,omitempty"`
	Message      *string            `json:"message,omitempty"`
	Model        *string            `json:"model,omitempty"`
	AutomationID *string            `json:"automation_id,omitempty"`
	Enabled      *bool              `json:"enabled,omitempty"`
	Timezone     *string            `json:"timezone,omitempty"`
	Metadata     *map[string]string `json:"metadata,omitempty"`
}

type wakeupTriggerView struct {
	ID                      string            `json:"id"`
	Name                    string            `json:"name"`
	Schedule                string            `json:"schedule"`
	Channel                 string            `json:"channel"`
	SessionKey              string            `json:"session_key"`
	Message                 string            `json:"message"`
	Model                   string            `json:"model,omitempty"`
	AutomationID            string            `json:"automation_id,omitempty"`
	Enabled                 bool              `json:"enabled"`
	Timezone                string            `json:"timezone,omitempty"`
	Metadata                map[string]string `json:"metadata,omitempty"`
	CreatedAt               *time.Time        `json:"created_at,omitempty"`
	LastRunAt               *time.Time        `json:"last_run_at,omitempty"`
	LastRunID               string            `json:"last_run_id,omitempty"`
	LastStatus              string            `json:"last_status,omitempty"`
	LastError               string            `json:"last_error,omitempty"`
	LastSummary             string            `json:"last_summary,omitempty"`
	LastVerificationStatus  string            `json:"last_verification_status,omitempty"`
	LastVerificationSummary string            `json:"last_verification_summary,omitempty"`
	NextRunAt               *time.Time        `json:"next_run_at,omitempty"`
}

type wakeupTriggerResponse struct {
	Trigger wakeupTriggerView `json:"trigger"`
}

type wakeupTriggerListResponse struct {
	Items   []wakeupTriggerView `json:"items"`
	Count   int                 `json:"count"`
	Running bool                `json:"running"`
}

type wakeupDeleteResponse struct {
	OK bool   `json:"ok"`
	ID string `json:"id"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (g *Gateway) handleWakeupList(w http.ResponseWriter, r *http.Request) {
	if g.wakeup == nil {
		gwError(w, http.StatusServiceUnavailable, "wakeup service not available")
		return
	}
	triggers := g.wakeup.List()
	items := make([]wakeupTriggerView, 0, len(triggers))
	for _, trigger := range triggers {
		items = append(items, wakeupTriggerViewFromTrigger(trigger))
	}
	gwJSON(w, http.StatusOK, wakeupTriggerListResponse{
		Items:   items,
		Count:   len(items),
		Running: g.wakeup.IsRunning(),
	})
}

func (g *Gateway) handleWakeupCreate(w http.ResponseWriter, r *http.Request) {
	if g.wakeup == nil {
		gwError(w, http.StatusServiceUnavailable, "wakeup service not available")
		return
	}

	var req wakeupTriggerCreateRequest
	if !decodeOperatorStrictJSONBody(w, r, &req) {
		return
	}
	if req.LegacyAgent != nil {
		gwError(w, http.StatusBadRequest, "agent is no longer supported; use session_key or automation_id")
		return
	}
	if strings.TrimSpace(req.Schedule) == "" {
		gwError(w, http.StatusBadRequest, "schedule is required")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		gwError(w, http.StatusBadRequest, "message is required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	id, err := generateWakeupID()
	if err != nil {
		gwError(w, http.StatusInternalServerError, "generate id failed")
		return
	}

	trigger := wakeup.Trigger{
		ID:           id,
		Name:         strings.TrimSpace(req.Name),
		Schedule:     strings.TrimSpace(req.Schedule),
		Channel:      strings.TrimSpace(req.Channel),
		SessionKey:   strings.TrimSpace(req.SessionKey),
		Message:      req.Message,
		Model:        strings.TrimSpace(req.Model),
		AutomationID: strings.TrimSpace(req.AutomationID),
		Enabled:      enabled,
		Timezone:     strings.TrimSpace(req.Timezone),
		Metadata:     req.Metadata,
		CreatedAt:    time.Now().UTC(),
	}

	if err := g.wakeup.Add(trigger); err != nil {
		status := http.StatusConflict
		if errors.Is(err, cron.ErrInvalidSchedule) {
			status = http.StatusBadRequest
		}
		gwError(w, status, err.Error())
		return
	}

	// Re-fetch to include computed NextRunAt.
	created, _ := g.wakeup.Get(id)
	if created != nil {
		trigger = *created
	}

	gwJSON(w, http.StatusCreated, wakeupTriggerResponse{Trigger: wakeupTriggerViewFromTrigger(trigger)})
}

func (g *Gateway) handleWakeupGet(w http.ResponseWriter, r *http.Request) {
	if g.wakeup == nil {
		gwError(w, http.StatusServiceUnavailable, "wakeup service not available")
		return
	}
	id := r.PathValue("id")
	trigger, err := g.wakeup.Get(id)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, wakeupTriggerResponse{Trigger: wakeupTriggerViewFromTrigger(*trigger)})
}

func (g *Gateway) handleWakeupUpdate(w http.ResponseWriter, r *http.Request) {
	if g.wakeup == nil {
		gwError(w, http.StatusServiceUnavailable, "wakeup service not available")
		return
	}
	id := r.PathValue("id")

	var req wakeupTriggerPatchRequest
	if !decodeOperatorStrictJSONBody(w, r, &req) {
		return
	}
	if req.LegacyAgent != nil {
		gwError(w, http.StatusBadRequest, "agent is no longer supported; use session_key or automation_id")
		return
	}
	current, err := g.wakeup.Get(id)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	updated := *current
	if req.Name != nil {
		updated.Name = strings.TrimSpace(*req.Name)
	}
	if req.Schedule != nil {
		updated.Schedule = strings.TrimSpace(*req.Schedule)
	}
	if req.Channel != nil {
		updated.Channel = strings.TrimSpace(*req.Channel)
	}
	if req.SessionKey != nil {
		updated.SessionKey = strings.TrimSpace(*req.SessionKey)
	}
	if req.Message != nil {
		updated.Message = *req.Message
	}
	if req.Model != nil {
		updated.Model = strings.TrimSpace(*req.Model)
	}
	if req.AutomationID != nil {
		updated.AutomationID = strings.TrimSpace(*req.AutomationID)
	}
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
	}
	if req.Timezone != nil {
		updated.Timezone = strings.TrimSpace(*req.Timezone)
	}
	if req.Metadata != nil {
		updated.Metadata = *req.Metadata
	}

	updateErr := g.wakeup.Update(id, func(t *wakeup.Trigger) {
		*t = updated
	})
	if updateErr != nil {
		status := http.StatusNotFound
		if errors.Is(updateErr, cron.ErrInvalidSchedule) {
			status = http.StatusBadRequest
		}
		gwError(w, status, updateErr.Error())
		return
	}
	trigger, err := g.wakeup.Get(id)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, wakeupTriggerResponse{Trigger: wakeupTriggerViewFromTrigger(*trigger)})
}

func (g *Gateway) handleWakeupDelete(w http.ResponseWriter, r *http.Request) {
	if g.wakeup == nil {
		gwError(w, http.StatusServiceUnavailable, "wakeup service not available")
		return
	}
	id := r.PathValue("id")
	if _, err := g.wakeup.Get(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := g.wakeup.Remove(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, wakeupDeleteResponse{OK: true, ID: id})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateWakeupID() (string, error) {
	b := make([]byte, wakeupIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func wakeupTriggerViewFromTrigger(trigger wakeup.Trigger) wakeupTriggerView {
	return wakeupTriggerView{
		ID:                      trigger.ID,
		Name:                    trigger.Name,
		Schedule:                trigger.Schedule,
		Channel:                 trigger.Channel,
		SessionKey:              trigger.SessionKey,
		Message:                 trigger.Message,
		Model:                   trigger.Model,
		AutomationID:            trigger.AutomationID,
		Enabled:                 trigger.Enabled,
		Timezone:                trigger.Timezone,
		Metadata:                trigger.Metadata,
		CreatedAt:               nonZeroTimePtr(trigger.CreatedAt),
		LastRunAt:               nonZeroTimePtr(trigger.LastRunAt),
		LastRunID:               trigger.LastRunID,
		LastStatus:              trigger.LastStatus,
		LastError:               trigger.LastError,
		LastSummary:             trigger.LastSummary,
		LastVerificationStatus:  trigger.LastVerificationStatus,
		LastVerificationSummary: trigger.LastVerificationSummary,
		NextRunAt:               nonZeroTimePtr(trigger.NextRunAt),
	}
}

func nonZeroTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copied := value
	return &copied
}
