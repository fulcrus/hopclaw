package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
)

type openAICompatModelClient struct {
	lastRequest agent.ChatRequest
}

func (m *openAICompatModelClient) Chat(_ context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	m.lastRequest = req
	return &agent.ModelResponse{
		Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: "compat ok",
		},
	}, nil
}

func newGatewayWithOneShotRuntime(t *testing.T) (*Gateway, *openAICompatModelClient) {
	t.Helper()

	model := &openAICompatModelClient{}
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	bus := eventbus.NewInMemoryBus()
	engine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "test",
		IncludeSkillCatalog:  false,
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, nil)
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, engine, model, nil, nil)
	runtimeSvc := runtimepkg.NewService(component, sessions, runs, nil, bus, nil)
	srv := server.New(runtimeSvc, server.Config{AuthToken: "test-token"})
	return gatewayFromServer(srv, Config{
		AuthToken: "test-token",
		Runtime:   runtimeSvc,
	}), model
}

func TestHandleChatCompletionsExecutesOneShotRuntime(t *testing.T) {
	gw, model := newGatewayWithOneShotRuntime(t)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/v1/chat/completions", `{
		"model":"gpt-5",
		"messages":[
			{"role":"system","content":"be terse"},
			{"role":"user","content":"say hello"},
			{"role":"assistant","content":"ignored"},
			{"role":"user","content":"latest user wins"}
		]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/chat/completions status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp oaiChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Fatalf("object = %q, want chat.completion", resp.Object)
	}
	if resp.Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", resp.Model)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "compat ok" {
		t.Fatalf("choices = %#v, want compat ok", resp.Choices)
	}
	if model.lastRequest.Model != "gpt-5" {
		t.Fatalf("lastRequest.Model = %q, want gpt-5", model.lastRequest.Model)
	}
	if got := model.lastRequest.Messages[len(model.lastRequest.Messages)-1].Content; got != "latest user wins" {
		t.Fatalf("last user message = %q, want latest user wins", got)
	}
}
