package gateway

// ---------------------------------------------------------------------------
// OpenAI Chat Completions compatible types
// ---------------------------------------------------------------------------

// oaiChatMessage represents a single message in the OpenAI chat format.
type oaiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// oaiChatRequest is the request body for POST /v1/chat/completions.
type oaiChatRequest struct {
	Model     string           `json:"model"`
	Messages  []oaiChatMessage `json:"messages"`
	Stream    bool             `json:"stream"`
	MaxTokens int              `json:"max_tokens,omitempty"`
}

// oaiChatResponse is the OpenAI-compatible chat completion response.
type oaiChatResponse struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []oaiChatChoice `json:"choices"`
	Usage   oaiUsage        `json:"usage"`
}

// oaiChatChoice represents a single completion choice.
type oaiChatChoice struct {
	Index        int            `json:"index"`
	Message      oaiChatMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

// oaiUsage reports token consumption for the completion.
type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
