package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	autopkg "github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/watch"
)

const (
	watchIDBytes = 8
)

type watchCreateRequest struct {
	Name         string                  `json:"name"`
	Enabled      *bool                   `json:"enabled,omitempty"`
	Interval     string                  `json:"interval"`
	Source       watch.Source            `json:"source"`
	Delivery     *autopkg.DeliveryTarget `json:"delivery,omitempty"`
	Prompt       string                  `json:"prompt,omitempty"`
	SessionKey   string                  `json:"session_key,omitempty"`
	Model        string                  `json:"model,omitempty"`
	AutomationID string                  `json:"automation_id,omitempty"`
	FireOnStart  *bool                   `json:"fire_on_start,omitempty"`
}

type watchPatchRequest struct {
	Name         *string                 `json:"name,omitempty"`
	Enabled      *bool                   `json:"enabled,omitempty"`
	Interval     *string                 `json:"interval,omitempty"`
	Source       *watch.Source           `json:"source,omitempty"`
	Delivery     *autopkg.DeliveryTarget `json:"delivery,omitempty"`
	Prompt       *string                 `json:"prompt,omitempty"`
	SessionKey   *string                 `json:"session_key,omitempty"`
	Model        *string                 `json:"model,omitempty"`
	AutomationID *string                 `json:"automation_id,omitempty"`
	FireOnStart  *bool                   `json:"fire_on_start,omitempty"`
}

type watchResponse struct {
	Item watch.Watch `json:"item"`
}

type watchListResponse struct {
	Items []watch.Watch `json:"items"`
	Count int           `json:"count"`
}

type watchStatusResponse struct {
	Running    bool `json:"running"`
	WatchCount int  `json:"watch_count"`
}

func (g *Gateway) handleWatchList(w http.ResponseWriter, r *http.Request) {
	if g.watch == nil {
		gwError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}
	items := g.watch.Store().List()
	gwJSON(w, http.StatusOK, watchListResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleWatchCreate(w http.ResponseWriter, r *http.Request) {
	if g.watch == nil {
		gwError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}
	var req watchCreateRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	fireOnStart := false
	if req.FireOnStart != nil {
		fireOnStart = *req.FireOnStart
	}
	id, err := generateWatchID()
	if err != nil {
		gwError(w, http.StatusInternalServerError, "generate id failed")
		return
	}
	now := time.Now().UTC()
	item := watch.Watch{
		ID:           id,
		Name:         strings.TrimSpace(req.Name),
		Enabled:      enabled,
		Interval:     strings.TrimSpace(req.Interval),
		Source:       req.Source,
		Delivery:     normalizeDeliveryTarget(req.Delivery),
		Prompt:       strings.TrimSpace(req.Prompt),
		SessionKey:   strings.TrimSpace(req.SessionKey),
		Model:        strings.TrimSpace(req.Model),
		AutomationID: strings.TrimSpace(req.AutomationID),
		FireOnStart:  fireOnStart,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if enabled {
		item.NextCheckAt = now
	}
	if err := watch.Validate(item); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := g.watch.Store().Add(item); err != nil {
		gwError(w, http.StatusConflict, err.Error())
		return
	}
	if err := g.watch.Store().Save(); err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	g.watch.Rearm()
	gwJSON(w, http.StatusCreated, watchResponse{Item: item})
}

func (g *Gateway) handleWatchGet(w http.ResponseWriter, r *http.Request) {
	if g.watch == nil {
		gwError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}
	item, err := g.watch.Store().Get(r.PathValue("id"))
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, watchResponse{Item: *item})
}

func (g *Gateway) handleWatchUpdate(w http.ResponseWriter, r *http.Request) {
	if g.watch == nil {
		gwError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}
	var req watchPatchRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	now := time.Now().UTC()
	item, err := g.watch.Store().Get(id)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	updated := *item
	if req.Name != nil {
		updated.Name = strings.TrimSpace(*req.Name)
	}
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
		if updated.Enabled {
			updated.NextCheckAt = now
		} else {
			updated.NextCheckAt = time.Time{}
		}
	}
	if req.Interval != nil {
		updated.Interval = strings.TrimSpace(*req.Interval)
		if updated.Enabled {
			updated.NextCheckAt = now
		}
	}
	if req.Source != nil {
		updated.Source = *req.Source
	}
	if req.Prompt != nil {
		updated.Prompt = strings.TrimSpace(*req.Prompt)
	}
	if req.Delivery != nil {
		updated.Delivery = normalizeDeliveryTarget(req.Delivery)
	}
	if req.SessionKey != nil {
		updated.SessionKey = strings.TrimSpace(*req.SessionKey)
	}
	if req.Model != nil {
		updated.Model = strings.TrimSpace(*req.Model)
	}
	if req.AutomationID != nil {
		updated.AutomationID = strings.TrimSpace(*req.AutomationID)
	}
	if req.FireOnStart != nil {
		updated.FireOnStart = *req.FireOnStart
	}
	updated.UpdatedAt = now
	if err := watch.Validate(updated); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := g.watch.Store().Update(id, func(item *watch.Watch) {
		*item = updated
	}); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := g.watch.Store().Save(); err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	g.watch.Rearm()
	gwJSON(w, http.StatusOK, watchResponse{Item: updated})
}

func (g *Gateway) handleWatchDelete(w http.ResponseWriter, r *http.Request) {
	if g.watch == nil {
		gwError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}
	id := r.PathValue("id")
	if _, err := g.watch.Store().Get(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := g.watch.Store().Remove(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := g.watch.Store().Save(); err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	g.watch.Rearm()
	gwJSON(w, http.StatusOK, idOKResponse{OK: true, ID: id})
}

func (g *Gateway) handleWatchTrigger(w http.ResponseWriter, r *http.Request) {
	if g.watch == nil {
		gwError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}
	id := r.PathValue("id")
	if _, err := g.watch.Store().Get(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := g.watch.Trigger(r.Context(), id); err != nil {
		gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	gwJSON(w, http.StatusAccepted, idOKResponse{OK: true, ID: id})
}

func (g *Gateway) handleWatchStatus(w http.ResponseWriter, r *http.Request) {
	if g.watch == nil {
		gwError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}
	items := g.watch.Store().List()
	resp := watchStatusResponse{
		Running:    g.watch.IsRunning(),
		WatchCount: len(items),
	}
	gwJSON(w, http.StatusOK, resp)
}

func generateWatchID() (string, error) {
	buf := make([]byte, watchIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func normalizeDeliveryTarget(target *autopkg.DeliveryTarget) *watch.DeliveryTarget {
	if target == nil {
		return nil
	}
	channel := strings.TrimSpace(target.Channel)
	recipient := strings.TrimSpace(target.Target)
	if channel == "" && recipient == "" {
		return nil
	}
	return &watch.DeliveryTarget{
		Channel: channel,
		Target:  recipient,
	}
}
