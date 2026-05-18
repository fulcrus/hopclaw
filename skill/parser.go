package skill

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"gopkg.in/yaml.v3"
)

var ErrMissingSkillFile = errors.New("missing SKILL.md or BUNDLE.yaml")

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	Homepage               string `yaml:"homepage"`
	UserInvocable          *bool  `yaml:"user-invocable"`
	DisableModelInvocation *bool  `yaml:"disable-model-invocation"`
	CommandDispatch        string `yaml:"command-dispatch"`
	CommandTool            string `yaml:"command-tool"`
	CommandArgMode         string `yaml:"command-arg-mode"`
	Metadata               any    `yaml:"metadata"`
}

func ParseDir(dir string) (*ExternalSkillSpec, error) {
	hasSkill := hasSkillMarkdown(dir)
	hasBundle := hasBundleManifest(dir)
	if hasSkill && hasBundle {
		return nil, fmt.Errorf("directory %s contains both SKILL.md and BUNDLE.yaml", dir)
	}
	if hasBundle {
		manifest, err := loadBundleManifest(dir)
		if err != nil {
			return nil, err
		}
		spec := buildSpecFromBundleManifest(manifest)
		files, err := supportingFiles(dir)
		if err != nil {
			return nil, err
		}
		spec.SupportingFiles = files
		return spec, nil
	}

	skillPath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrMissingSkillFile
		}
		return nil, fmt.Errorf("read skill file: %w", err)
	}

	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", skillPath, err)
	}

	companion, err := loadCompanionManifest(dir)
	if err != nil {
		return nil, err
	}
	spec.Companion = companion

	files, err := supportingFiles(dir)
	if err != nil {
		return nil, err
	}
	spec.SupportingFiles = files
	return spec, nil
}

func ParseSkillMarkdown(data []byte) (*ExternalSkillSpec, error) {
	fmBytes, body := splitFrontmatter(data)

	spec := &ExternalSkillSpec{
		Frontmatter: make(map[string]any),
	}
	if len(fmBytes) > 0 {
		if err := yaml.Unmarshal(fmBytes, &spec.Frontmatter); err != nil {
			return nil, fmt.Errorf("decode frontmatter: %w", err)
		}

		var fm skillFrontmatter
		if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
			return nil, fmt.Errorf("decode frontmatter struct: %w", err)
		}
		spec.Name = strings.TrimSpace(fm.Name)
		spec.Description = strings.TrimSpace(fm.Description)
		spec.Homepage = strings.TrimSpace(fm.Homepage)
		spec.UserInvocable = fm.UserInvocable == nil || *fm.UserInvocable
		spec.DisableModelInvocation = fm.DisableModelInvocation != nil && *fm.DisableModelInvocation
		spec.CommandDispatch = strings.TrimSpace(fm.CommandDispatch)
		spec.CommandTool = strings.TrimSpace(fm.CommandTool)
		spec.CommandArgMode = strings.TrimSpace(fm.CommandArgMode)

		rawMeta, ocMeta, err := normalizeMetadata(fm.Metadata)
		if err != nil {
			return nil, err
		}
		spec.RawMetadata = rawMeta
		spec.OpenClaw = ocMeta
		if spec.Homepage == "" {
			spec.Homepage = ocMeta.Homepage
		}
	}

	body = strings.TrimSpace(body)
	spec.Body = body
	if spec.Name == "" {
		return nil, errors.New("skill name is required")
	}
	if spec.Description == "" {
		spec.Description = inferDescription(body)
	}
	return spec, nil
}

func hasSkillMarkdown(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}

func splitFrontmatter(data []byte) ([]byte, string) {
	const marker = "---"
	trimmed := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := strings.Split(string(trimmed), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != marker {
		return nil, string(trimmed)
	}

	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == marker {
			return []byte(strings.Join(lines[1:i], "\n")), strings.Join(lines[i+1:], "\n")
		}
	}
	return nil, string(trimmed)
}

func normalizeMetadata(raw any) (map[string]any, OpenClawMetadata, error) {
	if raw == nil {
		return map[string]any{}, OpenClawMetadata{}, nil
	}

	meta := map[string]any{}
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return map[string]any{}, OpenClawMetadata{}, nil
		}
		if err := json.Unmarshal([]byte(v), &meta); err != nil {
			return nil, OpenClawMetadata{}, fmt.Errorf("decode metadata JSON: %w", err)
		}
	case map[string]any:
		meta = supportmaps.Clone(v)
		if meta == nil {
			meta = map[string]any{}
		}
	default:
		buf, err := yaml.Marshal(v)
		if err != nil {
			return nil, OpenClawMetadata{}, fmt.Errorf("encode metadata: %w", err)
		}
		if err := yaml.Unmarshal(buf, &meta); err != nil {
			return nil, OpenClawMetadata{}, fmt.Errorf("decode metadata map: %w", err)
		}
	}

	node := map[string]any{}
	if oc, ok := meta["openclaw"]; ok {
		switch typed := oc.(type) {
		case map[string]any:
			node = supportmaps.Clone(typed)
			if node == nil {
				node = map[string]any{}
			}
		default:
			buf, err := yaml.Marshal(typed)
			if err != nil {
				return nil, OpenClawMetadata{}, fmt.Errorf("encode metadata.openclaw: %w", err)
			}
			if err := yaml.Unmarshal(buf, &node); err != nil {
				return nil, OpenClawMetadata{}, fmt.Errorf("decode metadata.openclaw: %w", err)
			}
		}
	} else {
		node = meta
	}

	var normalized OpenClawMetadata
	buf, err := yaml.Marshal(node)
	if err != nil {
		return nil, OpenClawMetadata{}, fmt.Errorf("encode openclaw metadata: %w", err)
	}
	if err := yaml.Unmarshal(buf, &normalized); err != nil {
		return nil, OpenClawMetadata{}, fmt.Errorf("decode openclaw metadata: %w", err)
	}
	return meta, normalized, nil
}

func inferDescription(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line != "" {
			return line
		}
	}
	return ""
}

func supportingFiles(dir string) ([]SkillFile, error) {
	var files []SkillFile
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		switch filepath.Base(path) {
		case "SKILL.md", "BUNDLE.yaml":
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, SkillFile{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk supporting files: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func loadCompanionManifest(dir string) (*CompanionManifest, error) {
	manifestPath := filepath.Join(dir, "skill.manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read companion manifest: %w", err)
	}

	var manifest CompanionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode %s: %w", manifestPath, err)
	}
	return &manifest, nil
}
