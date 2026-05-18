package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsercap "github.com/fulcrus/hopclaw/capabilities/browser"
	desktopcap "github.com/fulcrus/hopclaw/capabilities/desktop"
	runtimecap "github.com/fulcrus/hopclaw/capabilities/runtime"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/config"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/skill"
)

func initCapabilities(browserClient *browserclient.Client, desktopClient *desktopclient.Client) *capregistry.Registry {
	reg := capregistry.New()

	if browserClient != nil {
		logging.LogIfErr(context.Background(), reg.Register(browsercap.New(browsercap.Config{
			Client: browserClient,
		})), "register browser capability failed")
	}
	if desktopClient != nil {
		logging.LogIfErr(context.Background(), reg.Register(desktopcap.New(desktopcap.Config{
			Client: desktopClient,
		})), "register desktop capability failed")
	}
	logging.LogIfErr(context.Background(), reg.Register(runtimecap.New(reg)), "register runtime capability failed")

	return reg
}

func browserHostConfigured(cfg config.BrowserHostConfig) bool {
	if cfg.Enabled != nil {
		return *cfg.Enabled
	}
	return strings.TrimSpace(cfg.BaseURL) != "" || strings.TrimSpace(cfg.AuthToken) != ""
}

func desktopHostConfigured(cfg config.DesktopHostConfig) bool {
	if cfg.Enabled != nil {
		return *cfg.Enabled
	}
	return strings.TrimSpace(cfg.BaseURL) != "" || strings.TrimSpace(cfg.AuthToken) != ""
}

func appendPromptSection(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "\n\n" + extra
	}
}

// DefaultDiscoveryRoots returns the standard skill discovery paths, ordered by priority.
func DefaultDiscoveryRoots(workspaceRoot string) []skill.DiscoveryRoot {
	home, _ := os.UserHomeDir()
	roots := []skill.DiscoveryRoot{
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, ".hopclaw", "skills"), Priority: 500},
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, ".hopclaw", "bundles"), Priority: 490},
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, "skills"), Priority: 475},
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, "bundles"), Priority: 470},
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, ".openclaw", "workspace", "skills"), Priority: 455},
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, ".openclaw", "workspace", "bundles"), Priority: 452},
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, ".openclaw", "skills"), Priority: 450},
		{Kind: skill.SourceWorkspace, Path: filepath.Join(workspaceRoot, ".openclaw", "bundles"), Priority: 445},
	}
	if home != "" {
		roots = append(roots,
			skill.DiscoveryRoot{Kind: skill.SourceUser, Path: filepath.Join(home, ".hopclaw", "skills"), Priority: 400},
			skill.DiscoveryRoot{Kind: skill.SourceUser, Path: filepath.Join(home, ".hopclaw", "bundles"), Priority: 390},
			skill.DiscoveryRoot{Kind: skill.SourceUser, Path: filepath.Join(home, ".openclaw", "workspace", "skills"), Priority: 360},
			skill.DiscoveryRoot{Kind: skill.SourceUser, Path: filepath.Join(home, ".openclaw", "workspace", "bundles"), Priority: 355},
			skill.DiscoveryRoot{Kind: skill.SourceUser, Path: filepath.Join(home, ".openclaw", "skills"), Priority: 350},
			skill.DiscoveryRoot{Kind: skill.SourceUser, Path: filepath.Join(home, ".openclaw", "bundles"), Priority: 345},
			skill.DiscoveryRoot{Kind: skill.SourceClawHub, Path: filepath.Join(home, ".hopclaw", "clawhub", "installs"), Priority: 300},
			skill.DiscoveryRoot{Kind: skill.SourceClawHub, Path: filepath.Join(home, ".openclaw", "clawhub", "installs"), Priority: 275},
			skill.DiscoveryRoot{Kind: skill.SourcePlugin, Path: filepath.Join(home, ".hopclaw", "plugins"), Priority: 100},
			skill.DiscoveryRoot{Kind: skill.SourcePlugin, Path: filepath.Join(home, ".hopclaw", "extensions"), Priority: 95},
			skill.DiscoveryRoot{Kind: skill.SourcePlugin, Path: filepath.Join(home, ".openclaw", "plugins"), Priority: 90},
			skill.DiscoveryRoot{Kind: skill.SourcePlugin, Path: filepath.Join(home, ".openclaw", "extensions"), Priority: 85},
		)
	}
	return roots
}

func parseQueueMode(v string) agent.QueueMode {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case string(agent.QueueReject):
		return agent.QueueReject
	case string(agent.QueueCoalesce):
		return agent.QueueCoalesce
	default:
		return agent.QueueEnqueue
	}
}
