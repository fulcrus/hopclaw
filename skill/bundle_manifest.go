package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/bundle"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"gopkg.in/yaml.v3"
)

type BundleRuntimeSpec struct {
	Type       bundle.RuntimeType `json:"type" yaml:"type"`
	Executable *ToolRuntimeSpec   `json:"executable,omitempty" yaml:"executable,omitempty"`
	Sidecar    *ToolRuntimeSpec   `json:"sidecar,omitempty" yaml:"sidecar,omitempty"`
}

type BundleInstallSpec struct {
	Steps []InstallSpec `json:"steps,omitempty" yaml:"steps,omitempty"`
}

type BundlePromptSpec struct {
	Name          string `json:"name,omitempty" yaml:"name,omitempty"`
	Description   string `json:"description,omitempty" yaml:"description,omitempty"`
	Instructions  string `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	UserInvocable *bool  `json:"user_invocable,omitempty" yaml:"user_invocable,omitempty"`
}

type BundleManifest struct {
	ID          string             `json:"id" yaml:"id"`
	Version     string             `json:"version" yaml:"version"`
	Name        string             `json:"name" yaml:"name"`
	Description string             `json:"description" yaml:"description"`
	Homepage    string             `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Author      string             `json:"author,omitempty" yaml:"author,omitempty"`
	License     string             `json:"license,omitempty" yaml:"license,omitempty"`
	Tags        []string           `json:"tags,omitempty" yaml:"tags,omitempty"`
	Requires    RequiresSpec       `json:"requires,omitempty" yaml:"requires,omitempty"`
	Install     BundleInstallSpec  `json:"install,omitempty" yaml:"install,omitempty"`
	Runtime     BundleRuntimeSpec  `json:"runtime" yaml:"runtime"`
	Prompt      *BundlePromptSpec  `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Tools       []ToolManifestSpec `json:"tools,omitempty" yaml:"tools,omitempty"`
	OpenClaw    OpenClawMetadata   `json:"openclaw,omitempty" yaml:"openclaw,omitempty"`
}

func hasBundleManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "BUNDLE.yaml"))
	return err == nil
}

func loadBundleManifest(dir string) (*BundleManifest, error) {
	path := filepath.Join(dir, "BUNDLE.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest BundleManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	manifest.ID = strings.TrimSpace(manifest.ID)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Homepage = strings.TrimSpace(manifest.Homepage)
	if manifest.Name == "" {
		manifest.Name = manifest.ID
	}
	if manifest.ID == "" {
		return nil, fmt.Errorf("%s: bundle id is required", path)
	}
	if manifest.Name == "" {
		return nil, fmt.Errorf("%s: bundle name is required", path)
	}
	return &manifest, nil
}

func buildSpecFromBundleManifest(manifest *BundleManifest) *ExternalSkillSpec {
	if manifest == nil {
		return nil
	}
	openClaw := manifest.OpenClaw
	if openClaw.Homepage == "" {
		openClaw.Homepage = manifest.Homepage
	}
	if len(openClaw.Requires.Bins) == 0 {
		openClaw.Requires.Bins = append([]string(nil), manifest.Requires.Bins...)
	}
	if len(openClaw.Requires.AnyBins) == 0 {
		openClaw.Requires.AnyBins = append([]string(nil), manifest.Requires.AnyBins...)
	}
	if len(openClaw.Requires.Env) == 0 {
		openClaw.Requires.Env = append([]string(nil), manifest.Requires.Env...)
	}
	if len(openClaw.Requires.Config) == 0 {
		openClaw.Requires.Config = append([]string(nil), manifest.Requires.Config...)
	}
	if len(openClaw.Install) == 0 {
		openClaw.Install = append([]InstallSpec(nil), manifest.Install.Steps...)
	}

	name := manifest.Name
	description := manifest.Description
	body := defaultBundleInstructions(manifest)
	userInvocable := false
	if manifest.Prompt != nil {
		if v := strings.TrimSpace(manifest.Prompt.Name); v != "" {
			name = v
		}
		if v := strings.TrimSpace(manifest.Prompt.Description); v != "" {
			description = v
		}
		if v := strings.TrimSpace(manifest.Prompt.Instructions); v != "" {
			body = v
		}
		if manifest.Prompt.UserInvocable != nil {
			userInvocable = *manifest.Prompt.UserInvocable
		}
	}

	return &ExternalSkillSpec{
		Name:          name,
		Description:   description,
		Body:          body,
		Frontmatter:   map[string]any{"bundle_id": manifest.ID, "bundle_version": manifest.Version},
		Homepage:      normalize.FirstNonEmpty(openClaw.Homepage, manifest.Homepage),
		UserInvocable: userInvocable,
		OpenClaw:      openClaw,
		Bundle:        manifest,
	}
}

func defaultBundleInstructions(manifest *BundleManifest) string {
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		name = strings.TrimSpace(manifest.ID)
	}
	var toolNames []string
	for _, tool := range manifest.Tools {
		if trimmed := strings.TrimSpace(tool.Name); trimmed != "" {
			toolNames = append(toolNames, trimmed)
		}
		if len(toolNames) >= 8 {
			break
		}
	}
	if len(toolNames) == 0 {
		return fmt.Sprintf("Use the %s bundle when its tools are available.", name)
	}
	return fmt.Sprintf("Use the %s bundle tools when needed. Available tools include: %s.", name, strings.Join(toolNames, ", "))
}
