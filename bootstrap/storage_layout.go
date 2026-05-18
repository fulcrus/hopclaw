package bootstrap

import (
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

const defaultStorageRoot = ".hopclaw/state"

type storageLayout struct {
	root           string
	runtimeDBPath  string
	controlDBPath  string
	knowledgeDBPath string
	auditDBPath    string
	memoryNotebook string
	knowledgeRoot  string
}

func resolveStorageLayout(cfg config.Config) storageLayout {
	root := strings.TrimSpace(cfg.Store.Path)
	if root == "" {
		root = defaultStorageRoot
	}
	layout := storageLayout{root: root}
	layout.runtimeDBPath = filepath.Join(root, "runtime.db")
	layout.controlDBPath = filepath.Join(root, "control.db")
	layout.knowledgeDBPath = filepath.Join(root, "knowledge.db")
	layout.auditDBPath = filepath.Join(root, "audit.db")
	layout.memoryNotebook = filepath.Join(root, "memory", "MEMORY.md")
	layout.knowledgeRoot = filepath.Join(root, "knowledge")
	return layout
}
