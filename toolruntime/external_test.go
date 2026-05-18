package toolruntime

import "testing"

func TestExternalToolDefinitionsAreSortedByName(t *testing.T) {
	t.Parallel()

	executor := NewExternalToolExecutor([]ExternalToolConfig{
		{Name: "zeta.echo", Description: "zeta", Endpoint: "https://example.com/zeta"},
		{Name: "alpha.echo", Description: "alpha", Endpoint: "https://example.com/alpha"},
	})
	if executor == nil {
		t.Fatal("expected external tool executor")
	}

	defs := executor.ToolDefinitions(nil)
	if len(defs) != 2 {
		t.Fatalf("tool definitions = %#v", defs)
	}
	if defs[0].Name != "alpha.echo" || defs[1].Name != "zeta.echo" {
		t.Fatalf("tool definition order = %#v", defs)
	}
}
