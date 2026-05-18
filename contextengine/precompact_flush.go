package contextengine

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// NewMemoryFlushHook creates a PreCompactHook that extracts important facts
// from messages about to be discarded and writes them to a MemoryWriter.
//
// Strategy (inspired by OpenClaw's "silent memory flush before compaction"):
//  1. Extract pinned facts from message metadata
//  2. Extract structured signal annotations from message metadata
//  3. Extract opaque identifiers (UUIDs, URLs, paths) from tool outputs
//  4. Write each as a memory entry via the MemoryWriter
func NewMemoryFlushHook(writer MemoryWriter) PreCompactHook {
	if writer == nil {
		return nil
	}
	return func(ctx context.Context, discarding []Message, session *Session) error {
		if len(discarding) == 0 {
			return nil
		}
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		ts := time.Now().UTC().Format("20060102T150405")
		prefix := fmt.Sprintf("compact_flush:%s:%s", sessionID, ts)

		var idx int
		var firstErr error
		setEntry := func(suffix, value string) {
			if value == "" {
				return
			}
			key := fmt.Sprintf("%s:%d:%s", prefix, idx, suffix)
			idx++
			if err := writer.Set(ctx, key, value); err != nil && firstErr == nil {
				firstErr = err
			}
		}

		// 1. Extract pinned facts from metadata.
		for _, f := range collectPinnedFacts(discarding) {
			setEntry("pinned", f.Content)
		}

		// 2. Extract structured signal annotations from message metadata.
		for _, msg := range discarding {
			for _, signal := range extractAnnotatedSignalTexts(msg) {
				setEntry("signal", signal)
			}
		}

		// 3. Extract opaque identifiers from tool outputs.
		var toolText strings.Builder
		for _, msg := range discarding {
			if msg.Role == RoleTool {
				toolText.WriteString(msg.TextContent())
				toolText.WriteByte('\n')
			}
		}
		if toolText.Len() > 0 {
			ids := extractOpaqueIdentifiers(toolText.String(), 12)
			if len(ids) > 0 {
				setEntry("identifiers", strings.Join(ids, "\n"))
			}
		}

		return firstErr
	}
}

func extractAnnotatedSignalTexts(msg Message) []string {
	decisions, todos, constraints := extractAnnotatedSignals(msg)
	out := make([]string, 0, len(decisions)+len(todos)+len(constraints))
	out = append(out, decisions...)
	out = append(out, todos...)
	out = append(out, constraints...)
	return dedupeStrings(out)
}

func extractAnnotatedSignals(msg Message) ([]string, []string, []string) {
	if len(msg.Metadata) == 0 {
		return nil, nil, nil
	}
	return messageMetadataStrings(msg.Metadata, MetadataKeySignalDecisions),
		messageMetadataStrings(msg.Metadata, MetadataKeySignalTODOs),
		messageMetadataStrings(msg.Metadata, MetadataKeySignalConstraints)
}

func messageMetadataStrings(metadata map[string]any, key string) []string {
	if len(metadata) == 0 || strings.TrimSpace(key) == "" {
		return nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return nil
		}
		return []string{typed}
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			out = append(out, item)
		}
		return dedupeStrings(out)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" || text == "<nil>" {
				continue
			}
			out = append(out, text)
		}
		return dedupeStrings(out)
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "" || text == "<nil>" {
			return nil
		}
		return []string{text}
	}
}

// countFlushableSignals counts the number of signal items that would be
// extracted from the given messages. Used for CompactEvent reporting.
func countFlushableSignals(messages []Message) int {
	count := len(collectPinnedFacts(messages))
	for _, msg := range messages {
		count += len(extractAnnotatedSignalTexts(msg))
	}
	// Count identifier extraction as 1 item if any tool output exists.
	for _, msg := range messages {
		if msg.Role == RoleTool && msg.TextContent() != "" {
			count++
			break
		}
	}
	return count
}
