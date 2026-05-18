package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/fulcrus/hopclaw/internal/version"
	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
	"github.com/spf13/cobra"
)

type pluginScaffoldKind string

const (
	pluginScaffoldTool       pluginScaffoldKind = "tool"
	pluginScaffoldChannel    pluginScaffoldKind = "channel"
	pluginScaffoldProvider   pluginScaffoldKind = "provider"
	pluginScaffoldSkill      pluginScaffoldKind = "skill"
	pluginScaffoldGoVersion                     = "1.26.1"
	pluginScaffoldModulePath                    = "github.com/fulcrus/hopclaw"
)

type pluginScaffoldSpec struct {
	Kind        pluginScaffoldKind
	Name        string
	Slug        string
	PackageName string
	Description string
	ToolName    string
	ModelID     string
}

type pluginScaffoldDependency struct {
	RequireVersion string
	ReplacePath    string
}

func newPluginsInitCmd() *cobra.Command {
	var kind string
	var baseDir string

	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a local plugin starter",
		Long:  "Create a local HopClaw plugin starter with a manifest, Go entrypoint, and starter tests.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsInit(cmd, args[0], kind, baseDir)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", string(pluginScaffoldTool), "starter kind: tool, channel, provider, or skill")
	cmd.Flags().StringVar(&baseDir, "dir", ".", "directory where the plugin folder will be created")
	return cmd
}

func runPluginsInit(cmd *cobra.Command, rawName, rawKind, baseDir string) error {
	kind, err := normalizePluginScaffoldKind(rawKind)
	if err != nil {
		return err
	}
	spec, err := newPluginScaffoldSpec(rawName, kind)
	if err != nil {
		return err
	}

	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "."
	}
	targetDir := filepath.Join(baseDir, spec.Slug)
	if err := ensurePluginScaffoldTarget(targetDir); err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}

	dependency := resolvePluginScaffoldDependency()
	files := pluginScaffoldFiles(spec, dependency)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		path := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create scaffold parent: %w", err)
		}
		if err := os.WriteFile(path, []byte(files[rel]), 0o644); err != nil {
			return fmt.Errorf("write scaffold file %s: %w", rel, err)
		}
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Initialized %s plugin at %s\n", kind, targetDir)
	fmt.Fprintf(out, "Next: cd %s && go test ./...\n", spec.Slug)
	return nil
}

func normalizePluginScaffoldKind(raw string) (pluginScaffoldKind, error) {
	switch pluginScaffoldKind(strings.TrimSpace(strings.ToLower(raw))) {
	case pluginScaffoldTool:
		return pluginScaffoldTool, nil
	case pluginScaffoldChannel:
		return pluginScaffoldChannel, nil
	case pluginScaffoldProvider:
		return pluginScaffoldProvider, nil
	case pluginScaffoldSkill:
		return pluginScaffoldSkill, nil
	default:
		return "", fmt.Errorf("unsupported plugin kind %q (want one of: tool, channel, provider, skill)", raw)
	}
}

func newPluginScaffoldSpec(rawName string, kind pluginScaffoldKind) (pluginScaffoldSpec, error) {
	slug := pluginScaffoldSlug(rawName)
	if slug == "" {
		return pluginScaffoldSpec{}, fmt.Errorf("plugin name must contain at least one letter or digit")
	}
	return pluginScaffoldSpec{
		Kind:        kind,
		Name:        slug,
		Slug:        slug,
		PackageName: pluginScaffoldPackageName(slug),
		Description: fmt.Sprintf("Example %s plugin scaffolded by hopclaw plugins init.", kind),
		ToolName:    slug + ".echo",
		ModelID:     slug + "-chat",
	}, nil
}

func ensurePluginScaffoldTarget(targetDir string) error {
	info, err := os.Stat(targetDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin target already exists and is not a directory: %s", targetDir)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("plugin target already exists and is not empty: %s", targetDir)
	}
	return nil
}

func pluginScaffoldFiles(spec pluginScaffoldSpec, dependency pluginScaffoldDependency) map[string]string {
	files := map[string]string{
		"go.mod":              pluginGoModTemplate(spec, dependency),
		"hopclaw.plugin.yaml": pluginManifestTemplate(spec),
	}
	switch spec.Kind {
	case pluginScaffoldTool:
		files["plugin.go"] = pluginToolTemplate(spec)
		files["plugin_test.go"] = pluginToolTestTemplate(spec)
	case pluginScaffoldChannel:
		files["plugin.go"] = pluginChannelTemplate(spec)
		files["plugin_test.go"] = pluginChannelTestTemplate(spec)
	case pluginScaffoldProvider:
		files["plugin.go"] = pluginProviderTemplate(spec)
		files["plugin_test.go"] = pluginProviderTestTemplate(spec)
	case pluginScaffoldSkill:
		files["plugin.go"] = pluginSkillTemplate(spec)
		files["plugin_test.go"] = pluginSkillTestTemplate(spec)
		skill := sdkplugin.Skill{
			Name:        spec.Name,
			Description: "Starter skill scaffolded by hopclaw plugins init.",
			TLDR:        "Replace the starter workflow with the shortest safe path for your users.",
			Body:        "## Usage\n\n```bash\n# Add copy-pasteable commands here.\n```\n",
		}
		files[filepath.Join("skills", skill.DirectoryName(), "SKILL.md")] = skill.Markdown()
	}
	return files
}

func pluginGoModTemplate(spec pluginScaffoldSpec, dependency pluginScaffoldDependency) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "module hopclaw.local/%s\n\n", spec.Slug)
	fmt.Fprintf(&builder, "go %s\n", pluginScaffoldGoVersion)

	requireVersion := dependency.RequireVersion
	if requireVersion == "" {
		requireVersion = "v0.0.0"
	}
	fmt.Fprintf(&builder, "\nrequire %s %s\n", pluginScaffoldModulePath, requireVersion)
	if dependency.ReplacePath != "" {
		fmt.Fprintf(&builder, "\nreplace %s => %s\n", pluginScaffoldModulePath, dependency.ReplacePath)
	}

	return builder.String()
}

func pluginManifestTemplate(spec pluginScaffoldSpec) string {
	switch spec.Kind {
	case pluginScaffoldTool:
		return fmt.Sprintf(`name: %s
version: "0.1.0"
description: %s

tools:
  - name: %s
    description: Echo a line of text back to the caller.
    endpoint: inline://%s
    input_schema:
      type: object
      properties:
        text:
          type: string
          description: Text to echo back.
`, spec.Name, spec.Description, spec.ToolName, spec.ToolName)
	case pluginScaffoldChannel:
		return fmt.Sprintf(`name: %s
version: "0.1.0"
description: %s

channels:
  %s:
    type: stdio
    command: ./%s
    capabilities:
      - connect
      - send
`, spec.Name, spec.Description, spec.Name, spec.Name)
	case pluginScaffoldProvider:
		return fmt.Sprintf(`name: %s
version: "0.1.0"
description: %s

providers:
  %s:
    api: openai-completions
    base_url: https://api.example.com/v1
    default_model: %s
`, spec.Name, spec.Description, spec.Name, spec.ModelID)
	default:
		return fmt.Sprintf(`name: %s
version: "0.1.0"
description: %s
skills_dir: skills
`, spec.Name, spec.Description)
	}
}

func pluginToolTemplate(spec pluginScaffoldSpec) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"fmt"
	"strings"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

const ToolName = %q

type Plugin struct{}

func Manifest() sdkplugin.Manifest {
	return sdkplugin.Manifest{
		Name:        %q,
		Version:     "0.1.0",
		Description: %q,
		Tools: []sdkplugin.ToolDecl{{
			Name:        ToolName,
			Description: "Echo a line of text back to the caller.",
			Endpoint:    "inline://%s",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Text to echo back.",
					},
				},
			},
		}},
	}
}

func (Plugin) Tool() sdkplugin.Tool {
	return sdkplugin.Tool{
		Decl: Manifest().Tools[0],
		ExecuteFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime, request sdkplugin.ToolRequest) (sdkplugin.ToolOutput, error) {
			text := strings.TrimSpace(fmt.Sprint(request.Input["text"]))
			if text == "" || text == "<nil>" {
				text = "hello from %s"
			}
			if err := runtime.Emit(ctx, sdkplugin.Event{
				Name: "plugin.tool.executed",
				Payload: map[string]any{
					"text": text,
					"tool": ToolName,
				},
			}); err != nil {
				return sdkplugin.ToolOutput{}, err
			}
			runtime.Logf("tool %%s handled %%q", ToolName, text)
			return sdkplugin.ToolOutput{
				Output: text,
				Structured: map[string]any{
					"text": text,
					"tool": ToolName,
				},
			}, nil
		},
	}
}
`, spec.PackageName, spec.ToolName, spec.Name, spec.Description, spec.ToolName, spec.Name)
}

func pluginToolTestTemplate(spec pluginScaffoldSpec) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"testing"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestToolManifestIsValid(t *testing.T) {
	t.Parallel()

	if errs := sdkplugin.ValidateManifest(Manifest()); len(errs) != 0 {
		t.Fatalf("ValidateManifest() errors = %%#v", errs)
	}
}

func TestToolExecution(t *testing.T) {
	t.Parallel()

	harness := sdkplugin.NewTestHarness(nil)
	output, err := harness.Execute(context.Background(), Plugin{}, sdkplugin.ToolRequest{
		Input: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %%v", err)
	}
	if output.Output != "hello" {
		t.Fatalf("Output = %%q, want hello", output.Output)
	}
}
`, spec.PackageName)
}

func pluginChannelTemplate(spec pluginScaffoldSpec) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"fmt"
	"strings"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

const ChannelName = %q

type Plugin struct{}

func Manifest() sdkplugin.Manifest {
	return sdkplugin.Manifest{
		Name:        %q,
		Version:     "0.1.0",
		Description: %q,
		Channels: map[string]sdkplugin.ChannelDecl{
			ChannelName: {
				Type:         "stdio",
				Command:      "./%s",
				Capabilities: []string{"connect", "send"},
			},
		},
	}
}

func (Plugin) Channel() sdkplugin.Channel {
	return sdkplugin.Channel{
		ConnectFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime) error {
			runtime.Logf("channel connected")
			return runtime.Emit(ctx, sdkplugin.Event{Name: "plugin.channel.connected"})
		},
		SendFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime, message sdkplugin.OutboundMessage) (sdkplugin.SendResult, error) {
			content := strings.TrimSpace(message.Content)
			if content == "" {
				content = "hello from %s"
			}
			if err := runtime.Emit(ctx, sdkplugin.Event{Name: "plugin.channel.sent"}); err != nil {
				return sdkplugin.SendResult{}, err
			}
			return sdkplugin.SendResult{
				MessageID: fmt.Sprintf("%%s:%%s", ChannelName, message.TargetID),
				Metadata: map[string]any{
					"echo": content,
				},
			}, nil
		},
	}
}
`, spec.PackageName, spec.Name, spec.Name, spec.Description, spec.Name, spec.Name)
}

func pluginChannelTestTemplate(spec pluginScaffoldSpec) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"testing"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestChannelManifestIsValid(t *testing.T) {
	t.Parallel()

	if errs := sdkplugin.ValidateManifest(Manifest()); len(errs) != 0 {
		t.Fatalf("ValidateManifest() errors = %%#v", errs)
	}
}

func TestChannelSend(t *testing.T) {
	t.Parallel()

	harness := sdkplugin.NewTestHarness(nil)
	if err := harness.Connect(context.Background(), Plugin{}); err != nil {
		t.Fatalf("Connect() error = %%v", err)
	}
	result, err := harness.Send(context.Background(), Plugin{}, sdkplugin.OutboundMessage{
		TargetID: "ops",
		Content:  "hello",
	})
	if err != nil {
		t.Fatalf("Send() error = %%v", err)
	}
	if result.MessageID == "" {
		t.Fatal("expected MessageID to be populated")
	}
}
`, spec.PackageName)
}

func pluginProviderTemplate(spec pluginScaffoldSpec) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"fmt"
	"strings"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

const (
	ProviderName = %q
	DefaultModel = %q
)

type Plugin struct{}

func Manifest() sdkplugin.Manifest {
	return sdkplugin.Manifest{
		Name:        %q,
		Version:     "0.1.0",
		Description: %q,
		Providers: map[string]sdkplugin.ProviderDecl{
			ProviderName: {
				API:          "openai-completions",
				BaseURL:      "https://api.example.com/v1",
				DefaultModel: DefaultModel,
			},
		},
	}
}

func (Plugin) Provider() sdkplugin.Provider {
	return sdkplugin.Provider{
		ModelsFunc: func(context.Context, sdkplugin.PluginRuntime) ([]sdkplugin.ModelInfo, error) {
			return []sdkplugin.ModelInfo{{
				ID:            DefaultModel,
				DisplayName:   "Starter Provider",
				ContextWindow: 32000,
				Capabilities:  []string{"chat"},
			}}, nil
		},
		ChatFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime, request sdkplugin.ChatRequest) (sdkplugin.ChatResponse, error) {
			modelID := strings.TrimSpace(request.Model)
			if modelID == "" {
				modelID = DefaultModel
			}
			reply := "hello from %s"
			if text := lastUserMessage(request.Messages); text != "" {
				reply = fmt.Sprintf("Echo: %%s", text)
			}
			if err := runtime.Emit(ctx, sdkplugin.Event{Name: "plugin.provider.chat"}); err != nil {
				return sdkplugin.ChatResponse{}, err
			}
			return sdkplugin.ChatResponse{
				Model: modelID,
				Message: sdkplugin.ChatMessage{
					Role:    sdkplugin.ChatRoleAssistant,
					Content: reply,
				},
			}, nil
		},
	}
}

func lastUserMessage(messages []sdkplugin.ChatMessage) string {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if messages[idx].Role != sdkplugin.ChatRoleUser {
			continue
		}
		if text := strings.TrimSpace(messages[idx].Content); text != "" {
			return text
		}
	}
	return ""
}
`, spec.PackageName, spec.Name, spec.ModelID, spec.Name, spec.Description, spec.Name)
}

func pluginProviderTestTemplate(spec pluginScaffoldSpec) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"testing"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestProviderManifestIsValid(t *testing.T) {
	t.Parallel()

	if errs := sdkplugin.ValidateManifest(Manifest()); len(errs) != 0 {
		t.Fatalf("ValidateManifest() errors = %%#v", errs)
	}
}

func TestProviderChat(t *testing.T) {
	t.Parallel()

	harness := sdkplugin.NewTestHarness(nil)
	models, err := harness.ListModels(context.Background(), Plugin{})
	if err != nil {
		t.Fatalf("ListModels() error = %%v", err)
	}
	if len(models) != 1 {
		t.Fatalf("ListModels() = %%#v", models)
	}
}
`, spec.PackageName)
}

func pluginSkillTemplate(spec pluginScaffoldSpec) string {
	body := "## Usage\n\n```bash\n# Add copy-pasteable commands here.\n```\n"
	return fmt.Sprintf(`package %s

import sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"

type Plugin struct{}

func Manifest() sdkplugin.Manifest {
	return sdkplugin.Manifest{
		Name:        %q,
		Version:     "0.1.0",
		Description: %q,
		SkillsDir:   "skills",
	}
}

func (Plugin) Skill() sdkplugin.Skill {
	return sdkplugin.Skill{
		Name:        %q,
		Description: "Starter skill scaffolded by hopclaw plugins init.",
		TLDR:        "Replace the starter workflow with the shortest safe path for your users.",
		Body:        %q,
	}
}
`, spec.PackageName, spec.Name, spec.Description, spec.Name, body)
}

func pluginSkillTestTemplate(spec pluginScaffoldSpec) string {
	return fmt.Sprintf(`package %s

import (
	"strings"
	"testing"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestSkillManifestIsValid(t *testing.T) {
	t.Parallel()

	if errs := sdkplugin.ValidateManifest(Manifest()); len(errs) != 0 {
		t.Fatalf("ValidateManifest() errors = %%#v", errs)
	}
}

func TestSkillMarkdown(t *testing.T) {
	t.Parallel()

	markdown := Plugin{}.Skill().Markdown()
	if !strings.Contains(markdown, "## TL;DR") {
		t.Fatalf("Markdown() = %%q", markdown)
	}
}
`, spec.PackageName)
}

func pluginScaffoldSlug(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	var builder strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func resolvePluginScaffoldDependency() pluginScaffoldDependency {
	dependency := pluginScaffoldDependency{
		RequireVersion: normalizePluginScaffoldModuleVersion(version.Version),
	}
	if repoRoot, ok := locatePluginScaffoldModuleRoot(); ok {
		dependency.ReplacePath = repoRoot
	}
	return dependency
}

func normalizePluginScaffoldModuleVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "", "dev", "(devel)":
		return ""
	}
	if strings.HasPrefix(raw, "v") {
		return raw
	}
	return "v" + raw
}

func locatePluginScaffoldModuleRoot() (string, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}

	dir := filepath.Clean(wd)
	for {
		goModPath := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(goModPath)
		if err == nil && strings.Contains(string(data), "module "+pluginScaffoldModulePath) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func pluginScaffoldPackageName(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
	if len(parts) == 0 {
		return "pluginstarter"
	}
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strings.ToLower(part))
	}
	name := builder.String()
	if name == "" {
		name = "pluginstarter"
	}
	if first := rune(name[0]); unicode.IsDigit(first) {
		return "plugin" + name
	}
	return name
}
