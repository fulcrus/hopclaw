package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/skill"
)

func initSkillHub(ctx context.Context, cfg config.SkillsConfig) skill.ClawHubClient {
	root := defaultClawHubRoot()
	if root == "" {
		return nil
	}

	client := skill.NewFileClawHubClient(root)
	client.Sources = defaultHubSources()
	client.Sources = append(client.Sources, cfg.Hub.Sources...)
	client.Sources = normalize.DedupeStrings(client.Sources)
	if url := strings.TrimSpace(cfg.Hub.URL); url != "" {
		client.BaseURL = url
		client.AuthToken = strings.TrimSpace(cfg.Hub.Token)
	}
	if cfg.Hub.SyncOnStart {
		if err := client.Sync(ctx); err != nil {
			log.Warn("skill hub sync failed", "error", err, "url", cfg.Hub.URL)
		}
	}
	return client
}

func defaultClawHubRoot() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		if root := filepath.Join(home, ".openclaw", "clawhub"); pathExists(root) {
			return root
		}
		return skill.DefaultClawHubRoot(home)
	}
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		if root := filepath.Join(cwd, ".openclaw", "clawhub"); pathExists(root) {
			return root
		}
		return filepath.Join(cwd, ".hopclaw", "clawhub")
	}
	return ""
}

func defaultHubSources() []string {
	var sources []string
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		sources = append(sources,
			filepath.Join(cwd, ".hopclaw", "skills"),
			filepath.Join(cwd, ".hopclaw", "bundles"),
			filepath.Join(cwd, "skills"),
			filepath.Join(cwd, "bundles"),
			filepath.Join(cwd, ".openclaw", "workspace", "skills"),
			filepath.Join(cwd, ".openclaw", "workspace", "bundles"),
			filepath.Join(cwd, ".openclaw", "skills"),
			filepath.Join(cwd, ".openclaw", "bundles"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		sources = append(sources,
			filepath.Join(home, ".hopclaw", "skills"),
			filepath.Join(home, ".hopclaw", "bundles"),
			filepath.Join(home, ".openclaw", "workspace", "skills"),
			filepath.Join(home, ".openclaw", "workspace", "bundles"),
			filepath.Join(home, ".openclaw", "skills"),
			filepath.Join(home, ".openclaw", "bundles"),
		)
	}
	return normalize.DedupeStrings(sources)
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
