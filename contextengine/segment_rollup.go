package contextengine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const l2RollupBatchSize = 8

func MaybeGenerateL2(ctx context.Context, reader SegmentReader, writer SegmentWriter, episodeID string) error {
	episodeID = strings.TrimSpace(episodeID)
	if reader == nil || writer == nil || episodeID == "" {
		return nil
	}

	for {
		children, err := reader.UnparentedL1Segments(ctx, episodeID, l2RollupBatchSize)
		if err != nil {
			return err
		}
		if len(children) < l2RollupBatchSize {
			return nil
		}

		rollup := buildRollupSegment(2, children[:l2RollupBatchSize])
		if rollup.ID == "" {
			rollup.ID = newGeneratedSegmentID("seg-l2")
		}
		if err := writer.InsertSegment(ctx, rollup); err != nil {
			return err
		}
		for _, child := range children[:l2RollupBatchSize] {
			if strings.TrimSpace(child.ID) == "" {
				continue
			}
			if err := writer.UpdateParentSegmentID(ctx, child.ID, rollup.ID); err != nil {
				return err
			}
		}
	}
}

func GenerateL3EpisodeOverview(ctx context.Context, reader SegmentReader, writer SegmentWriter, episodeID string) error {
	episodeID = strings.TrimSpace(episodeID)
	if reader == nil || writer == nil || episodeID == "" {
		return nil
	}

	segments, err := reader.SegmentsByEpisode(ctx, episodeID)
	if err != nil {
		return err
	}
	inputs := selectOverviewSourceSegments(segments)
	if len(inputs) == 0 {
		return nil
	}

	overview := buildRollupSegment(3, inputs)
	if overview.ID == "" {
		overview.ID = newGeneratedSegmentID("seg-l3")
	}
	if len(inputs) == 1 && inputs[0].Level == 2 {
		overview.ParentSegmentID = inputs[0].ID
	}
	return writer.InsertSegment(ctx, overview)
}

func selectOverviewSourceSegments(segments []SummarySegment) []SummarySegment {
	if len(segments) == 0 {
		return nil
	}

	level2 := make([]SummarySegment, 0, len(segments))
	unparentedL1 := make([]SummarySegment, 0, len(segments))
	for _, segment := range segments {
		switch segment.Level {
		case 3:
			return nil
		case 2:
			level2 = append(level2, segment)
		case 1:
			if strings.TrimSpace(segment.ParentSegmentID) == "" {
				unparentedL1 = append(unparentedL1, segment)
			}
		}
	}

	if len(level2) == 0 && len(unparentedL1) == 0 {
		for _, segment := range segments {
			if segment.Level == 1 {
				unparentedL1 = append(unparentedL1, segment)
			}
		}
	}

	selected := append(level2[:0:0], level2...)
	selected = append(selected, unparentedL1...)
	sortSegmentsForRollup(selected)
	return selected
}

func buildRollupSegment(level int, source []SummarySegment) SummarySegment {
	if len(source) == 0 {
		return SummarySegment{}
	}

	segments := append([]SummarySegment(nil), source...)
	sortSegmentsForRollup(segments)
	decisions := aggregateSegmentValues(segments, func(segment SummarySegment) []string { return segment.Decisions })
	todos := aggregateSegmentValues(segments, func(segment SummarySegment) []string { return segment.TODOs })
	constraints := aggregateSegmentValues(segments, func(segment SummarySegment) []string { return segment.Constraints })
	entities := aggregateSegmentValues(segments, func(segment SummarySegment) []string { return segment.Entities })
	summary := buildRollupSummaryText(level, segments, decisions, todos, constraints, entities)

	return SummarySegment{
		ID:              newGeneratedSegmentID(fmt.Sprintf("seg-l%d", level)),
		SessionID:       strings.TrimSpace(segments[0].SessionID),
		EpisodeID:       strings.TrimSpace(segments[0].EpisodeID),
		Level:           level,
		SeqStart:        segments[0].SeqStart,
		SeqEnd:          segments[len(segments)-1].SeqEnd,
		TSStart:         rollupStartTime(segments),
		TSEnd:           rollupEndTime(segments),
		SummaryText:     summary,
		Decisions:       decisions,
		TODOs:           todos,
		Constraints:     constraints,
		Entities:        entities,
		ArtifactRefs:    extractArtifactRefs(entities),
		Keywords:        buildSegmentKeywords(summary, decisions, todos, constraints, entities),
		QualityScore:    segmentQualityScore(summary, decisions, todos, constraints, entities),
		ParentSegmentID: "",
		CreatedAt:       time.Now().UTC(),
	}
}

func buildRollupSummaryText(level int, segments []SummarySegment, decisions, todos, constraints, entities []string) string {
	highlights := make([]string, 0, len(segments))
	for _, segment := range segments {
		if text := strings.TrimSpace(segment.SummaryText); text != "" {
			highlights = append(highlights, softTrimContent(text, 220, 0))
		}
	}
	highlights = dedupeStrings(highlights)

	header := fmt.Sprintf("Roll-up covering seq %d-%d.", segments[0].SeqStart, segments[len(segments)-1].SeqEnd)
	if level == 3 {
		header = fmt.Sprintf("Episode overview covering seq %d-%d.", segments[0].SeqStart, segments[len(segments)-1].SeqEnd)
	}

	lines := []string{header}
	if len(highlights) > 0 {
		lines = append(lines, "Highlights: "+strings.Join(limitStrings(highlights, 2), " "))
	}
	if len(decisions) > 0 {
		lines = append(lines, "Decisions: "+strings.Join(limitStrings(decisions, 3), " | "))
	}
	if len(todos) > 0 {
		lines = append(lines, "TODOs: "+strings.Join(limitStrings(todos, 3), " | "))
	}
	if len(constraints) > 0 {
		lines = append(lines, "Constraints: "+strings.Join(limitStrings(constraints, 3), " | "))
	}
	if len(entities) > 0 {
		lines = append(lines, "Entities: "+strings.Join(limitStrings(entities, 4), " | "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func aggregateSegmentValues(segments []SummarySegment, values func(SummarySegment) []string) []string {
	combined := make([]string, 0, len(segments))
	for _, segment := range segments {
		combined = append(combined, values(segment)...)
	}
	return dedupeStrings(combined)
}

func limitStrings(values []string, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	if limit <= 0 || len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func sortSegmentsForRollup(segments []SummarySegment) {
	sort.SliceStable(segments, func(i, j int) bool {
		if segments[i].SeqStart != segments[j].SeqStart {
			return segments[i].SeqStart < segments[j].SeqStart
		}
		if segments[i].Level != segments[j].Level {
			return segments[i].Level < segments[j].Level
		}
		return segments[i].CreatedAt.Before(segments[j].CreatedAt)
	})
}

func rollupStartTime(segments []SummarySegment) time.Time {
	start := segments[0].TSStart
	for _, segment := range segments[1:] {
		if start.IsZero() || (!segment.TSStart.IsZero() && segment.TSStart.Before(start)) {
			start = segment.TSStart
		}
	}
	if start.IsZero() {
		return time.Now().UTC()
	}
	return start
}

func rollupEndTime(segments []SummarySegment) time.Time {
	end := segments[0].TSEnd
	for _, segment := range segments[1:] {
		if end.IsZero() || segment.TSEnd.After(end) {
			end = segment.TSEnd
		}
	}
	if end.IsZero() {
		return rollupStartTime(segments)
	}
	return end
}

func newGeneratedSegmentID(prefix string) string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(raw))
}
