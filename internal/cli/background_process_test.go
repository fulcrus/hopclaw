package cli

import (
	"os/exec"
	"testing"
)

func TestConfigureDetachedProcess(t *testing.T) {
	cmd := exec.Command("echo")
	configureDetachedProcess(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected detached process attributes to be configured")
	}
}
