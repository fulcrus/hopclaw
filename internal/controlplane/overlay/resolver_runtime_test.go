package overlay

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestResolverRuntimeCurrentResolvesSecrets(t *testing.T) {
	t.Setenv("HOPCLAW_OVERLAY_TEST_TOKEN", "resolved-token")

	base := testOverlayBaseConfig()
	base.Models.Providers["openai"] = config.ProviderConfig{
		API:          "openai-completions",
		APIKey:       "env:HOPCLAW_OVERLAY_TEST_TOKEN",
		DefaultModel: "gpt-4o",
	}

	resolver, err := NewResolver(context.Background(), base, nil, Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	if got := resolver.Current().Models.Providers["openai"].APIKey; got != "env:HOPCLAW_OVERLAY_TEST_TOKEN" {
		t.Fatalf("Current().APIKey = %q", got)
	}
	if got := resolver.RuntimeCurrent().Models.Providers["openai"].APIKey; got != "resolved-token" {
		t.Fatalf("RuntimeCurrent().APIKey = %q, want resolved-token", got)
	}
}
