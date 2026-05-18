package gateway

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/internal/update"
)

func (g *Gateway) handleStatus(w http.ResponseWriter, r *http.Request) {
	reports := g.capabilityReports(r.Context())
	projection := controlplane.ProjectOperationalHealth(g.operationalWarning)
	payload := statusResponse{
		OK:              projection.OK,
		State:           projection.State,
		Summary:         projection.Summary,
		Version:         g.config.Version,
		Uptime:          time.Since(g.startAt).String(),
		CapabilityCount: len(reports),
		Warnings:        projection.Warnings,
		UserSurface:     g.userSurfaceSummary(),
	}
	payload.ActiveRuns, payload.QueuedRuns = g.statusRunCounts(r.Context())
	payload.ConnectedChannels = g.statusChannelResponses()
	if last := update.LastCheckResult(); last != nil {
		payload.Update = last
	}
	gwJSON(w, http.StatusOK, payload)
}

func (g *Gateway) currentOperationalWarnings() []controlplane.OperationalWarning {
	if g == nil || g.operationalWarning == nil {
		return nil
	}
	return g.operationalWarning.OperationalWarnings()
}

func (g *Gateway) statusRunCounts(ctx context.Context) (active int, queued int) {
	if g == nil || g.runtime == nil {
		return 0, 0
	}

	runs, err := g.runtime.ListRuns(ctx, agent.RunListFilter{})
	if err != nil {
		log.Warn("list runs for operator status failed", "error", err)
		return 0, 0
	}

	for _, run := range runs {
		if run == nil {
			continue
		}
		switch run.Status {
		case agent.RunQueued:
			queued++
		case agent.RunRunning, agent.RunStreaming:
			active++
		}
	}
	return active, queued
}

func (g *Gateway) statusChannelResponses() []statusChannelResponse {
	if g == nil {
		return nil
	}
	if registry := g.extensionRegistry(); registry != nil {
		if items := registry.ChannelHealth(); len(items) > 0 {
			out := make([]statusChannelResponse, 0, len(items))
			for _, item := range items {
				out = append(out, statusChannelResponse{
					Name:   item.Name,
					Status: string(item.State),
				})
			}
			sortStatusChannels(out)
			return out
		}
	}
	if g.channelHealth != nil {
		items := g.channelHealth.Status()
		if len(items) > 0 {
			out := make([]statusChannelResponse, 0, len(items))
			for _, item := range items {
				out = append(out, statusChannelResponse{
					Name:   item.Name,
					Status: string(item.State),
				})
			}
			sortStatusChannels(out)
			return out
		}
	}
	if g.channels == nil {
		return nil
	}

	names := g.channels.Names()
	out := make([]statusChannelResponse, 0, len(names))
	for _, name := range names {
		adapter, ok := g.channels.Get(name)
		if !ok || adapter == nil {
			continue
		}
		out = append(out, statusChannelResponse{
			Name:   name,
			Status: string(adapter.Status()),
		})
	}
	sortStatusChannels(out)
	return out
}

func sortStatusChannels(items []statusChannelResponse) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
}

func (g *Gateway) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	reports := g.capabilityReports(r.Context())
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: reports, Count: len(reports)})
}

func (g *Gateway) capabilityReports(ctx context.Context) []captypes.Report {
	if registry := g.extensionRegistry(); registry != nil {
		items := registry.Capabilities(ctx)
		reports := make([]captypes.Report, 0, len(items))
		for _, item := range items {
			reports = append(reports, captypes.Report{
				Manifest: item.Manifest,
				Health:   item.Health,
			})
		}
		return reports
	}
	if g.capabilities == nil {
		return nil
	}
	return g.capabilities.Reports(ctx)
}

func (g *Gateway) handleBrowserSessions(w http.ResponseWriter, _ *http.Request) {
	g.handleCapabilitySessionsWithName(w, "browser")
}

func (g *Gateway) handleCapabilitySessions(w http.ResponseWriter, r *http.Request) {
	g.handleCapabilitySessionsWithName(w, r.PathValue("name"))
}

func (g *Gateway) handleCapabilitySessionsWithName(w http.ResponseWriter, capabilityName string) {
	if g.capabilities == nil {
		gwJSON(w, http.StatusOK, countedItemsResponse{Items: []any{}, Count: 0})
		return
	}
	sessions := g.capabilities.ListCapabilitySessions(strings.TrimSpace(capabilityName))
	if sessions == nil {
		sessions = make([]*captypes.SessionHandle, 0)
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: sessions, Count: len(sessions)})
}

func (g *Gateway) handleCloseBrowserSession(w http.ResponseWriter, r *http.Request) {
	g.handleCloseCapabilitySessionWithName(w, r, "browser")
}

func (g *Gateway) handleCloseCapabilitySession(w http.ResponseWriter, r *http.Request) {
	g.handleCloseCapabilitySessionWithName(w, r, r.PathValue("name"))
}

func (g *Gateway) handleCloseCapabilitySessionWithName(w http.ResponseWriter, r *http.Request, capabilityName string) {
	if g.capabilities == nil {
		gwError(w, http.StatusServiceUnavailable, "capabilities not available")
		return
	}
	sessionID := r.PathValue("id")
	if err := g.capabilities.CloseCapabilitySession(r.Context(), strings.TrimSpace(capabilityName), sessionID); err != nil {
		gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	gwJSON(w, http.StatusOK, capabilitySessionOKResponse{
		OK:         true,
		SessionID:  sessionID,
		Capability: strings.TrimSpace(capabilityName),
	})
}
