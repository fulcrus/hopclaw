package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	channelapi "github.com/fulcrus/hopclaw/channels"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type channelValidateRequest struct {
	Channel string         `json:"channel"`
	Config  map[string]any `json:"config"`
}

type channelValidateResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
	Status  string `json:"status,omitempty"`
}

type channelDetectResponse struct {
	Channels []detectedChannel `json:"channels"`
	Count    int               `json:"count"`
}

type detectedChannel struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Configured bool   `json:"configured"`
	Status     string `json:"status,omitempty"`
}

type channelTestMessageRequest struct {
	Channel  string `json:"channel"`
	TargetID string `json:"target_id"`
	Message  string `json:"message"`
}

type channelTestMessageResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleChannelsValidate validates channel credentials by attempting to connect.
//
//	POST /operator/channels/validate
func (g *Gateway) handleChannelsValidate(w http.ResponseWriter, r *http.Request) {
	if g.channels == nil {
		gwError(w, http.StatusServiceUnavailable, "channels not available")
		return
	}

	var req channelValidateRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	req.Channel = strings.TrimSpace(req.Channel)
	if req.Channel == "" {
		gwError(w, http.StatusBadRequest, "channel is required")
		return
	}

	// Check if channel adapter exists.
	adapter, ok := g.channels.Get(req.Channel)
	if !ok {
		gwJSON(w, http.StatusOK, channelValidateResponse{
			Valid:   false,
			Message: "channel adapter not found",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := adapter.Connect(ctx); err != nil {
		gwJSON(w, http.StatusOK, channelValidateResponse{
			Valid:   false,
			Message: err.Error(),
			Status:  string(adapter.Status()),
		})
		return
	}
	status := string(adapter.Status())
	gwJSON(w, http.StatusOK, channelValidateResponse{
		Valid:   true,
		Message: fmt.Sprintf("channel adapter reachable (%s)", status),
		Status:  status,
	})
}

// handleChannelsDetect auto-detects available channels from configuration.
//
//	POST /operator/channels/detect
func (g *Gateway) handleChannelsDetect(w http.ResponseWriter, _ *http.Request) {
	if g.channels == nil {
		gwError(w, http.StatusServiceUnavailable, "channels not available")
		return
	}

	names := g.channels.Names()
	detected := make([]detectedChannel, 0, len(names))
	for _, name := range names {
		adapter, _ := g.channels.Get(name)
		status := ""
		configured := false
		if adapter != nil {
			status = string(adapter.Status())
			configured = adapter.Status() == channelapi.StatusConnected || adapter.Status() == channelapi.StatusConnecting
		}
		detected = append(detected, detectedChannel{
			Name:       name,
			Type:       name,
			Configured: configured,
			Status:     status,
		})
	}
	sortDetectedChannelItems(detected)

	gwJSON(w, http.StatusOK, channelDetectResponse{
		Channels: detected,
		Count:    len(detected),
	})
}

// handleChannelsTestMessage sends a test message through a channel.
//
//	POST /operator/channels/test-message
func (g *Gateway) handleChannelsTestMessage(w http.ResponseWriter, r *http.Request) {
	if g.channels == nil {
		gwError(w, http.StatusServiceUnavailable, "channels not available")
		return
	}

	var req channelTestMessageRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	req.Channel = strings.TrimSpace(req.Channel)
	req.TargetID = strings.TrimSpace(req.TargetID)
	req.Message = strings.TrimSpace(req.Message)
	if req.Channel == "" {
		gwError(w, http.StatusBadRequest, "channel is required")
		return
	}
	if req.Message == "" {
		gwError(w, http.StatusBadRequest, "message is required")
		return
	}
	if req.TargetID == "" {
		gwError(w, http.StatusBadRequest, "target_id is required")
		return
	}

	adapter, ok := g.channels.Get(req.Channel)
	if !ok {
		gwError(w, http.StatusNotFound, "channel not found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := adapter.Send(ctx, channelapi.OutboundMessage{
		TargetID: req.TargetID,
		Content:  req.Message,
	}); err != nil {
		gwError(w, http.StatusBadGateway, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, channelTestMessageResponse{
		OK:      true,
		Message: "test message sent",
	})
}
