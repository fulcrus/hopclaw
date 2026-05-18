package toolruntime

import (
	"strings"

	"github.com/fulcrus/hopclaw/agent"
)

const envOverlayMetadataKey = "toolruntime.run_env_overlay"

func sessionEnvOverlay(session *agent.Session, run *agent.Run) map[string]string {
	if session == nil || session.Metadata == nil {
		return nil
	}
	raw, ok := session.Metadata[envOverlayMetadataKey]
	if !ok {
		return nil
	}
	all, ok := raw.(map[string]map[string]string)
	if ok {
		if run != nil {
			if overlay := cloneEnvMap(all[strings.TrimSpace(run.ID)]); len(overlay) > 0 {
				return overlay
			}
		}
		if overlay := cloneEnvMap(all[""]); len(overlay) > 0 {
			return overlay
		}
		return nil
	}
	legacy, ok := raw.(map[string]string)
	if !ok {
		return nil
	}
	return cloneEnvMap(legacy)
}

func setSessionEnvOverlay(session *agent.Session, run *agent.Run, key, value string) {
	if session == nil || strings.TrimSpace(key) == "" {
		return
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}
	raw, _ := session.Metadata[envOverlayMetadataKey].(map[string]map[string]string)
	if raw == nil {
		raw = make(map[string]map[string]string)
	}
	runID := ""
	if run != nil {
		runID = strings.TrimSpace(run.ID)
	}
	overlay := raw[runID]
	if overlay == nil {
		overlay = make(map[string]string)
	}
	overlay[key] = value
	raw[runID] = overlay
	session.Metadata[envOverlayMetadataKey] = raw
}

func cloneEnvMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
