package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootRegistersCompletionCommand(t *testing.T) {

	root := newRootCmd()
	if _, _, err := root.Find([]string{"completion"}); err != nil {
		t.Fatalf("root.Find(completion) error = %v", err)
	}
}

func TestCompletionCommandGeneratesBashScript(t *testing.T) {

	root := newRootCmd()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"completion", "bash"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(completion bash) error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "__start_hopclaw") {
		t.Fatalf("completion output missing bash entrypoint: %q", output)
	}
}
