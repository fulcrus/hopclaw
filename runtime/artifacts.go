package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
)

// ErrArtifactPruneNoSelector indicates that a prune request was missing both a
// retention window and any explicit selector, so the runtime refuses to delete
// the entire artifact store.
var ErrArtifactPruneNoSelector = errors.New("artifact prune requires retention, before, or at least one selector")

type ArtifactPruneRequest struct {
	Filter    artifact.ListFilter `json:"filter"`
	Retention time.Duration       `json:"retention,omitempty"`
}

func (s *Service) ListArtifacts(ctx context.Context, filter artifact.ListFilter) ([]*artifact.Blob, error) {
	if s.artifacts == nil {
		return nil, agent.ErrArtifactStoreNil
	}
	return s.artifacts.List(ctx, filter)
}

func (s *Service) PruneArtifacts(ctx context.Context, req ArtifactPruneRequest) (*artifact.PruneResult, error) {
	if s.artifacts == nil {
		return nil, agent.ErrArtifactStoreNil
	}

	filter := req.Filter
	retention := req.Retention
	if retention <= 0 {
		retention = s.retention
	}
	if filter.Before.IsZero() && retention > 0 {
		filter.Before = s.nowUTC().Add(-retention)
	}
	if filter.Before.IsZero() && !filter.HasSelector() {
		return nil, ErrArtifactPruneNoSelector
	}

	result, err := artifact.Prune(ctx, s.artifacts, filter)
	if err != nil {
		return nil, err
	}
	logging.LogIfErr(ctx, s.publish(ctx, eventbus.NewArtifactPrunedEvent(
		strings.TrimSpace(filter.RunID),
		strings.TrimSpace(filter.SessionID),
		eventbus.ArtifactPrunedAttrs{
			DeletedCount: result.DeletedCount,
			DeletedIDs:   append([]string(nil), result.DeletedIDs...),
			Cutoff:       result.Cutoff,
			Kind:         strings.TrimSpace(filter.Kind),
			RunID:        strings.TrimSpace(filter.RunID),
			SessionID:    strings.TrimSpace(filter.SessionID),
			ToolName:     strings.TrimSpace(filter.ToolName),
			ToolCallID:   strings.TrimSpace(filter.ToolCallID),
		},
		nil,
	)), "publish artifact event failed")
	return result, nil
}
