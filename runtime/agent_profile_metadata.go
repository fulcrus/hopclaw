package runtime

import "github.com/fulcrus/hopclaw/agent"

func injectAgentProfileMetadata(metadata map[string]any, profile *AgentProfile, agentName string) map[string]any {
	if profile == nil {
		return metadata
	}
	if metadata == nil {
		metadata = make(map[string]any, 7)
	}
	metadata[metadataKeyAgent] = agentName
	metadata[agent.MetadataKeyAgentProfileName] = profile.Name
	metadata[agent.MetadataKeyAgentProfileModel] = profile.Model
	metadata[agent.MetadataKeyAgentProfileSystemPrompt] = profile.SystemPrompt
	metadata[agent.MetadataKeyAgentProfileTools] = append([]string(nil), profile.Tools...)
	metadata[agent.MetadataKeyAgentProfileSkills] = append([]string(nil), profile.Skills...)
	metadata[agent.MetadataKeyAgentProfileMaxTokens] = profile.MaxTokens
	metadata[agent.MetadataKeyAgentProfileSource] = "router"
	return metadata
}
