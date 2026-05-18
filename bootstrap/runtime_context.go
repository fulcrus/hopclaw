package bootstrap

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/runtimeenv"
	"github.com/fulcrus/hopclaw/skill"
)

type bootstrapRuntimeContextProvider struct {
	workDir string
	getCfg  func() config.Config
}

func newBootstrapRuntimeContextProvider(getCfg func() config.Config, workDir string) agent.RuntimeContextProvider {
	root := strings.TrimSpace(workDir)
	if root == "" {
		root = "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return &bootstrapRuntimeContextProvider{
		workDir: root,
		getCfg:  getCfg,
	}
}

func (p *bootstrapRuntimeContextProvider) Current(context.Context, *agent.Session, *agent.Run) (skill.RuntimeContext, error) {
	cfg := config.Config{}
	if p.getCfg != nil {
		cfg = p.getCfg()
	}
	return runtimeenv.BuildRuntimeFacts(p.workDir, cfg), nil
}
