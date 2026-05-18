// Package contextengine provides a production-oriented context preparation layer
// for agent runs, including skill-aware system prompts, pruning, inspection, and
// deterministic fallback compaction.
package contextengine

import "github.com/fulcrus/hopclaw/logging"

var log = logging.WithSubsystem("contextengine")
