package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	manifestFile         = "hopclaw.plugin.yaml"
	openClawManifestFile = "openclaw.plugin.json"
	packageManifestFile  = "package.json"
)

// Discover scans dirs for plugin manifests and returns loaded plugins.
// Each dir is searched non-recursively for hopclaw.plugin.yaml, then
// each immediate subdirectory is also checked (one level deep).
func Discover(dirs []string) []LoadedPlugin {
	var plugins []LoadedPlugin
	seen := make(map[string]bool)

	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dir = expandHome(dir)

		// Check dir itself.
		if p, err := loadManifest(dir); err == nil && !seen[p.Dir] {
			seen[p.Dir] = true
			plugins = append(plugins, p)
		}

		// Check immediate subdirectories.
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			subDir := filepath.Join(dir, entry.Name())
			if !entry.IsDir() {
				info, err := os.Stat(subDir)
				if err != nil || !info.IsDir() {
					continue
				}
			}
			p, err := loadManifest(subDir)
			if err != nil {
				continue
			}
			if seen[p.Dir] {
				continue
			}
			seen[p.Dir] = true
			plugins = append(plugins, p)
		}
	}

	return plugins
}

// DefaultPluginDirs returns the standard plugin discovery paths.
func DefaultPluginDirs(workspaceRoot string) []string {
	dirs := []string{
		filepath.Join(workspaceRoot, ".hopclaw", "plugins"),
		filepath.Join(workspaceRoot, ".hopclaw", "extensions"),
		filepath.Join(workspaceRoot, ".openclaw", "plugins"),
		filepath.Join(workspaceRoot, ".openclaw", "extensions"),
		filepath.Join(workspaceRoot, "extensions"),
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".hopclaw", "plugins"),
			filepath.Join(home, ".hopclaw", "extensions"),
			filepath.Join(home, ".openclaw", "plugins"),
			filepath.Join(home, ".openclaw", "extensions"),
		)
	}
	return dirs
}

func loadManifest(dir string) (LoadedPlugin, error) {
	path, format, err := resolveManifestPath(dir)
	if err != nil {
		return LoadedPlugin{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LoadedPlugin{}, err
	}
	expanded := os.ExpandEnv(string(data))

	var m Manifest
	switch format {
	case ManifestFormatOpenClawJSON:
		m, err = translateOpenClawManifest([]byte(expanded))
		if err != nil {
			return LoadedPlugin{}, fmt.Errorf("parse %s: %w", path, err)
		}
	default:
		if err := yaml.Unmarshal([]byte(expanded), &m); err != nil {
			return LoadedPlugin{}, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	absDir, _ := filepath.Abs(dir)
	if format == ManifestFormatOpenClawJSON {
		if err := mergeOpenClawPackageMetadata(absDir, &m); err != nil {
			return LoadedPlugin{}, fmt.Errorf("parse %s: %w", filepath.Join(absDir, packageManifestFile), err)
		}
	}
	m.Format = format
	if strings.TrimSpace(m.Name) == "" {
		return LoadedPlugin{}, fmt.Errorf("%s: plugin name is required", path)
	}
	if err := validateManifest(absDir, m); err != nil {
		return LoadedPlugin{}, fmt.Errorf("%s: %w", path, err)
	}
	log.Info("plugin discovered", "name", m.Name, "dir", absDir, "format", m.Format)
	return LoadedPlugin{Manifest: m, Dir: absDir}, nil
}

// Load loads a plugin manifest from a directory and returns the parsed plugin.
func Load(dir string) (LoadedPlugin, error) {
	return loadManifest(dir)
}

// LoadForTest loads a plugin manifest from a directory. It is intended for
// package-external tests that need a parsed plugin without directory discovery.
func LoadForTest(dir string) (LoadedPlugin, error) {
	return Load(dir)
}

func validateManifest(pluginDir string, manifest Manifest) error {
	if err := validatePluginLocalDir(pluginDir, "skills_dir", manifest.SkillsDir); err != nil {
		return err
	}
	for _, raw := range manifest.SkillsDirs {
		if err := validatePluginLocalDir(pluginDir, "skills_dirs", raw); err != nil {
			return err
		}
	}
	if err := validatePluginLocalDir(pluginDir, "hooks_dir", manifest.HooksDir); err != nil {
		return err
	}
	if err := validatePluginLocalFile(pluginDir, "provider_discovery_entry", manifest.OpenClawProviderDiscoveryEntry); err != nil {
		return err
	}
	for _, raw := range manifest.OpenClawExtensions {
		if err := validatePluginLocalFile(pluginDir, "openclaw.extensions", raw); err != nil {
			return err
		}
	}
	if err := validatePluginLocalFile(pluginDir, "openclaw.setup_entry", manifest.OpenClawSetupEntry); err != nil {
		return err
	}
	for _, key := range []string{"configuredState", "persistedAuthState"} {
		if specifier := openClawChannelStateSpecifier(manifest.OpenClawChannelMetadata, key); specifier != "" {
			if err := validatePluginLocalFile(pluginDir, "openclaw.channel."+key+".specifier", specifier); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePluginLocalDir(pluginDir, label, raw string) error {
	return validatePluginLocalPath(pluginDir, label, raw)
}

func validatePluginLocalFile(pluginDir, label, raw string) error {
	return validatePluginLocalPath(pluginDir, label, raw)
}

func validatePluginLocalPath(pluginDir, label, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if filepath.IsAbs(raw) {
		return fmt.Errorf("%s must stay within plugin root and cannot be absolute", label)
	}
	cleaned := filepath.Clean(raw)
	target := filepath.Join(pluginDir, cleaned)
	rel, err := filepath.Rel(pluginDir, target)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", label, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s escapes plugin root", label)
	}
	return nil
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func resolveManifestPath(dir string) (string, ManifestFormat, error) {
	if path := filepath.Join(dir, manifestFile); fileExists(path) {
		return path, ManifestFormatHopClawYAML, nil
	}
	if path := filepath.Join(dir, openClawManifestFile); fileExists(path) {
		return path, ManifestFormatOpenClawJSON, nil
	}
	return "", "", os.ErrNotExist
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

type openClawCompatPackageFile struct {
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	OpenClaw    map[string]any `json:"openclaw"`
}

type openClawCompatPackage struct {
	PackageName        string
	PackageVersion     string
	PackageDescription string
	PackageMetadata    map[string]any
	Extensions         []string
	SetupEntry         string
	ChannelMetadata    map[string]any
}

type openClawCompatManifest struct {
	ID                                string                       `json:"id"`
	Name                              string                       `json:"name"`
	Version                           string                       `json:"version"`
	Description                       string                       `json:"description"`
	EnabledByDefault                  *bool                        `json:"enabledByDefault"`
	LegacyPluginIDs                   []string                     `json:"legacyPluginIds"`
	AutoEnableWhenConfiguredProviders []string                     `json:"autoEnableWhenConfiguredProviders"`
	Kind                              any                          `json:"kind"`
	Providers                         []string                     `json:"providers"`
	Skills                            []string                     `json:"skills"`
	Channels                          []string                     `json:"channels"`
	ProviderDiscoveryEntry            string                       `json:"providerDiscoveryEntry"`
	ModelSupport                      OpenClawModelSupport         `json:"modelSupport"`
	CLIBackends                       []string                     `json:"cliBackends"`
	ConfigSchema                      map[string]any               `json:"configSchema"`
	UIHints                           map[string]ConfigUIHint      `json:"uiHints"`
	ProviderAuthEnvVars               map[string][]string          `json:"providerAuthEnvVars"`
	ProviderAuthAliases               map[string]string            `json:"providerAuthAliases"`
	ChannelEnvVars                    map[string][]string          `json:"channelEnvVars"`
	ProviderAuthChoices               []OpenClawProviderAuthChoice `json:"providerAuthChoices"`
	Contracts                         map[string]any               `json:"contracts"`
	ConfigContracts                   map[string]any               `json:"configContracts"`
}

func translateOpenClawManifest(data []byte) (Manifest, error) {
	var src openClawCompatManifest
	if err := json.Unmarshal(data, &src); err != nil {
		return Manifest{}, err
	}
	id := strings.TrimSpace(src.ID)
	name := strings.TrimSpace(src.Name)
	if name == "" {
		name = id
	}
	if id == "" && name == "" {
		return Manifest{}, fmt.Errorf("plugin id is required")
	}
	if len(src.ConfigSchema) == 0 {
		return Manifest{}, fmt.Errorf("plugin configSchema is required")
	}

	manifest := Manifest{
		Name:                              name,
		Version:                           strings.TrimSpace(src.Version),
		Description:                       strings.TrimSpace(src.Description),
		ConfigSchema:                      cloneMapAny(src.ConfigSchema),
		UIHints:                           cloneConfigUIHints(src.UIHints),
		ProviderAuthEnvVars:               normalizeStringSliceMap(src.ProviderAuthEnvVars),
		Format:                            ManifestFormatOpenClawJSON,
		OpenClawID:                        id,
		EnabledByDefault:                  cloneBoolPtr(src.EnabledByDefault),
		LegacyPluginIDs:                   normalizeStringList(src.LegacyPluginIDs),
		AutoEnableWhenConfiguredProviders: normalizeStringList(src.AutoEnableWhenConfiguredProviders),
		OpenClawKinds:                     normalizeStringListValue(src.Kind),
		OpenClawChannels:                  normalizeStringList(src.Channels),
		OpenClawProviderDiscoveryEntry:    strings.TrimSpace(src.ProviderDiscoveryEntry),
		OpenClawModelSupport:              normalizeOpenClawModelSupport(src.ModelSupport),
		OpenClawCLIBackends:               normalizeStringList(src.CLIBackends),
		OpenClawProviderAuthAliases:       normalizeStringMap(src.ProviderAuthAliases),
		OpenClawChannelEnvVars:            normalizeStringSliceMap(src.ChannelEnvVars),
		OpenClawProviderAuthChoices:       normalizeOpenClawProviderAuthChoices(src.ProviderAuthChoices),
		OpenClawContracts:                 normalizeStringListMap(src.Contracts),
		OpenClawConfigContracts:           cloneMapAny(src.ConfigContracts),
	}
	for _, providerName := range src.Providers {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			continue
		}
		decl, ok := openClawCatalogProviderDecl(providerName)
		if !ok {
			log.Warn("openclaw plugin provider skipped: unsupported provider catalog entry", "provider", providerName, "plugin", name)
			manifest.UnsupportedProviders = append(manifest.UnsupportedProviders, providerName)
			continue
		}
		if envVars := normalizedProviderAuthEnvVars(manifest.ProviderAuthEnvVars, providerName); len(envVars) > 0 {
			decl.EnvVars = envVars
			decl.APIKeyHint = openClawProviderAPIKeyHint(providerName, envVars)
		}
		if manifest.Providers == nil {
			manifest.Providers = make(map[string]ProviderDecl)
		}
		manifest.Providers[providerName] = decl
	}
	for _, skillDir := range src.Skills {
		if trimmed := strings.TrimSpace(skillDir); trimmed != "" {
			manifest.SkillsDirs = append(manifest.SkillsDirs, trimmed)
		}
	}
	if len(manifest.SkillsDirs) == 1 {
		manifest.SkillsDir = manifest.SkillsDirs[0]
	}
	return manifest, nil
}

func mergeOpenClawPackageMetadata(pluginDir string, manifest *Manifest) error {
	if manifest == nil {
		return nil
	}
	path := filepath.Join(pluginDir, packageManifestFile)
	if !fileExists(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pkg, err := translateOpenClawPackageMetadata([]byte(os.ExpandEnv(string(data))))
	if err != nil {
		return err
	}
	if manifest.Version == "" {
		manifest.Version = pkg.PackageVersion
	}
	if manifest.Description == "" {
		manifest.Description = pkg.PackageDescription
	}
	manifest.OpenClawPackageName = pkg.PackageName
	manifest.OpenClawPackageVersion = pkg.PackageVersion
	manifest.OpenClawPackageMetadata = pkg.PackageMetadata
	manifest.OpenClawExtensions = pkg.Extensions
	manifest.OpenClawSetupEntry = pkg.SetupEntry
	manifest.OpenClawChannelMetadata = pkg.ChannelMetadata
	return nil
}

func translateOpenClawPackageMetadata(data []byte) (openClawCompatPackage, error) {
	var src openClawCompatPackageFile
	if err := json.Unmarshal(data, &src); err != nil {
		return openClawCompatPackage{}, err
	}
	pkg := openClawCompatPackage{
		PackageName:        strings.TrimSpace(src.Name),
		PackageVersion:     strings.TrimSpace(src.Version),
		PackageDescription: strings.TrimSpace(src.Description),
		PackageMetadata:    cloneMapAny(src.OpenClaw),
	}
	if len(src.OpenClaw) == 0 {
		return pkg, nil
	}
	pkg.Extensions = normalizeStringListValue(src.OpenClaw["extensions"])
	pkg.SetupEntry = stringValue(src.OpenClaw["setupEntry"])
	pkg.ChannelMetadata = mapValue(src.OpenClaw["channel"])
	return pkg, nil
}

func openClawCatalogProviderDecl(name string) (ProviderDecl, bool) {
	entry, ok := openClawProviderCatalog[strings.TrimSpace(strings.ToLower(name))]
	if !ok {
		return ProviderDecl{}, false
	}
	entry.PreferUnscopedID = true
	return entry, true
}

func cloneMapAny(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = cloneJSONValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMapAny(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneJSONValue(item)
		}
		return out
	default:
		return value
	}
}

func mapValue(value any) map[string]any {
	typed, _ := value.(map[string]any)
	return cloneMapAny(typed)
}

func cloneConfigUIHints(src map[string]ConfigUIHint) map[string]ConfigUIHint {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]ConfigUIHint, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = ConfigUIHint{
			Label:       strings.TrimSpace(value.Label),
			Help:        strings.TrimSpace(value.Help),
			Advanced:    value.Advanced,
			Sensitive:   value.Sensitive,
			Placeholder: strings.TrimSpace(value.Placeholder),
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeStringSliceMap(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for provider, values := range src {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			continue
		}
		if normalized := normalizeStringList(values); len(normalized) > 0 {
			out[provider] = normalized
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizedProviderAuthEnvVars(src map[string][]string, provider string) []string {
	if len(src) == 0 {
		return nil
	}
	return append([]string(nil), src[strings.TrimSpace(provider)]...)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func normalizeStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeStringListMap(src map[string]any) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if normalized := normalizeStringListValue(value); len(normalized) > 0 {
			out[key] = normalized
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeStringListValue(value any) []string {
	switch typed := value.(type) {
	case string:
		return normalizeStringList([]string{typed})
	case []string:
		return normalizeStringList(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
		return normalizeStringList(out)
	default:
		return nil
	}
}

func normalizeOpenClawModelSupport(src OpenClawModelSupport) OpenClawModelSupport {
	return OpenClawModelSupport{
		ModelPrefixes: normalizeStringList(src.ModelPrefixes),
		ModelPatterns: normalizeStringList(src.ModelPatterns),
	}
}

func normalizeOpenClawProviderAuthChoices(src []OpenClawProviderAuthChoice) []OpenClawProviderAuthChoice {
	if len(src) == 0 {
		return nil
	}
	out := make([]OpenClawProviderAuthChoice, 0, len(src))
	seen := make(map[string]struct{}, len(src))
	for _, choice := range src {
		choice.Provider = strings.TrimSpace(choice.Provider)
		choice.Method = strings.TrimSpace(choice.Method)
		choice.ChoiceID = strings.TrimSpace(choice.ChoiceID)
		if choice.Provider == "" || choice.Method == "" || choice.ChoiceID == "" {
			continue
		}
		if _, ok := seen[choice.ChoiceID]; ok {
			continue
		}
		seen[choice.ChoiceID] = struct{}{}
		choice.ChoiceLabel = strings.TrimSpace(choice.ChoiceLabel)
		choice.ChoiceHint = strings.TrimSpace(choice.ChoiceHint)
		choice.AssistantVisibility = strings.TrimSpace(choice.AssistantVisibility)
		choice.DeprecatedChoiceIDs = normalizeStringList(choice.DeprecatedChoiceIDs)
		choice.GroupID = strings.TrimSpace(choice.GroupID)
		choice.GroupLabel = strings.TrimSpace(choice.GroupLabel)
		choice.GroupHint = strings.TrimSpace(choice.GroupHint)
		choice.OptionKey = strings.TrimSpace(choice.OptionKey)
		choice.CLIFlag = strings.TrimSpace(choice.CLIFlag)
		choice.CLIOption = strings.TrimSpace(choice.CLIOption)
		choice.CLIDescription = strings.TrimSpace(choice.CLIDescription)
		choice.OnboardingScopes = normalizeStringList(choice.OnboardingScopes)
		if choice.AssistantPriority != nil {
			priority := *choice.AssistantPriority
			choice.AssistantPriority = &priority
		}
		out = append(out, choice)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func openClawChannelStateSpecifier(channel map[string]any, key string) string {
	if len(channel) == 0 {
		return ""
	}
	state := mapValue(channel[key])
	if len(state) == 0 {
		return ""
	}
	return stringValue(state["specifier"])
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func openClawProviderAPIKeyHint(provider string, envVars []string) string {
	envVars = normalizeStringList(envVars)
	if len(envVars) == 0 {
		return ""
	}
	label := strings.TrimSpace(strings.ReplaceAll(provider, "-", " "))
	if label == "" {
		label = "provider"
	}
	if len(envVars) == 1 {
		return fmt.Sprintf("Enter the %s API key. Common env var: %s", label, envVars[0])
	}
	return fmt.Sprintf("Enter the %s API key. Common env vars: %s", label, strings.Join(envVars, ", "))
}

var openClawProviderCatalog = map[string]ProviderDecl{
	"amazon-bedrock":        {API: "bedrock-converse"},
	"anthropic":             {API: "anthropic-messages", BaseURL: "https://api.anthropic.com", DefaultModel: "claude-sonnet-4-5-20241022"},
	"byteplus":              {API: "openai-completions", BaseURL: "https://ark.ap-southeast.bytepluses.com/api/v3", DefaultModel: "seed-1-8-251228"},
	"byteplus-plan":         {API: "openai-completions", BaseURL: "https://ark.ap-southeast.bytepluses.com/api/coding/v3", DefaultModel: "ark-code-latest"},
	"chutes":                {API: "anthropic-messages", BaseURL: "https://chutes.ai/anthropic"},
	"cloudflare-ai-gateway": {API: "anthropic-messages", DefaultModel: "claude-sonnet-4-5"},
	"deepgram":              {API: "openai-completions", BaseURL: "https://api.deepgram.com/v1"},
	"deepseek":              {API: "openai-completions", BaseURL: "https://api.deepseek.com/v1", DefaultModel: "deepseek-chat"},
	"github-copilot":        {API: "github-copilot"},
	"google":                {API: "google-generative-ai", BaseURL: "https://generativelanguage.googleapis.com", DefaultModel: "gemini-2.0-flash"},
	"groq":                  {API: "openai-completions", BaseURL: "https://api.groq.com/openai/v1", DefaultModel: "llama-3.3-70b-versatile"},
	"huggingface":           {API: "openai-completions", BaseURL: "https://router.huggingface.co/v1"},
	"kilocode":              {API: "openai-completions", BaseURL: "https://api.kilo.ai/api/gateway/", DefaultModel: "kilo/auto"},
	"kimi":                  {API: "openai-completions", BaseURL: "https://api.moonshot.ai/v1", DefaultModel: "kimi-k2.5"},
	"kimi-coding":           {API: "anthropic-messages", BaseURL: "https://api.kimi.com/coding/", DefaultModel: "k2p5"},
	"litellm":               {API: "openai-completions"},
	"minimax":               {API: "anthropic-messages", BaseURL: "https://api.minimax.io/anthropic", DefaultModel: "MiniMax-M2.5"},
	"minimax-portal":        {API: "anthropic-messages", BaseURL: "https://api.minimax.io/anthropic", DefaultModel: "MiniMax-M2.5"},
	"mistral":               {API: "openai-completions", BaseURL: "https://api.mistral.ai/v1"},
	"modelstudio":           {API: "openai-completions", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", DefaultModel: "qwen-plus-latest"},
	"moonshot":              {API: "openai-completions", BaseURL: "https://api.moonshot.ai/v1", DefaultModel: "kimi-k2.5"},
	"nvidia":                {API: "openai-completions", BaseURL: "https://integrate.api.nvidia.com/v1", DefaultModel: "nvidia/llama-3.1-nemotron-70b-instruct"},
	"ollama":                {API: "ollama", BaseURL: "http://127.0.0.1:11434/v1"},
	"openai":                {API: "openai-completions", BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4o"},
	"openrouter":            {API: "openai-completions", BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "auto"},
	"opencode":              {API: "openai-completions"},
	"qianfan":               {API: "openai-completions", BaseURL: "https://qianfan.baidubce.com/v2", DefaultModel: "deepseek-v3.2"},
	"qwen-portal":           {API: "openai-completions", BaseURL: "https://portal.qwen.ai/v1", DefaultModel: "qwen3.5-plus"},
	"sglang":                {API: "openai-completions", BaseURL: "http://127.0.0.1:30000/v1"},
	"synthetic":             {API: "anthropic-messages", BaseURL: "https://api.synthetic.new/anthropic", DefaultModel: "hf:MiniMaxAI/MiniMax-M2.5"},
	"together":              {API: "openai-completions", BaseURL: "https://api.together.xyz/v1", DefaultModel: "meta-llama/Llama-3.3-70B-Instruct-Turbo"},
	"venice":                {API: "openai-completions", BaseURL: "https://api.venice.ai/api/v1", DefaultModel: "kimi-k2-5"},
	"vercel-ai-gateway":     {API: "anthropic-messages", BaseURL: "https://ai-gateway.vercel.sh", DefaultModel: "anthropic/claude-opus-4.6"},
	"vllm":                  {API: "openai-completions", BaseURL: "http://127.0.0.1:8000/v1"},
	"volcengine":            {API: "openai-completions", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", DefaultModel: "doubao-seed-1-8-251228"},
	"volcengine-plan":       {API: "openai-completions", BaseURL: "https://ark.cn-beijing.volces.com/api/coding/v3", DefaultModel: "ark-code-latest"},
	"xai":                   {API: "openai-completions", BaseURL: "https://api.x.ai/v1", DefaultModel: "grok-2"},
	"xiaomi":                {API: "anthropic-messages", BaseURL: "https://api.xiaomimimo.com/anthropic", DefaultModel: "mimo-v2-flash"},
	"zai":                   {API: "openai-completions", BaseURL: "https://api.z.ai/v1"},
}
