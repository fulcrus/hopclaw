package model

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

type benchmarkModelClient struct {
	resp *agent.ModelResponse
}

func (c benchmarkModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	return c.resp, nil
}

var benchmarkRegistryResponseSink *agent.ModelResponse

func BenchmarkRegistryChatDispatch(b *testing.B) {
	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"stub": benchmarkModelClient{
				resp: &agent.ModelResponse{
					Message: contextengine.Message{
						Role:    contextengine.RoleAssistant,
						Content: "ok",
					},
				},
			},
		},
		defaultName: "stub",
	}
	req := agent.ChatRequest{Model: "stub/gpt-4o-mini"}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := reg.Chat(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkRegistryResponseSink = resp
	}
}
