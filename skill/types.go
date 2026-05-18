package skill

import "time"

type SourceKind string

const (
	SourceWorkspace SourceKind = "workspace"
	SourceUser      SourceKind = "user"
	SourceBundled   SourceKind = "bundled"
	SourceClawHub   SourceKind = "clawhub"
	SourcePlugin    SourceKind = "plugin"
)

type DiscoveryRoot struct {
	Kind     SourceKind
	Path     string
	Priority int
}

func (r DiscoveryRoot) effectivePriority() int {
	if r.Priority != 0 {
		return r.Priority
	}
	switch r.Kind {
	case SourceWorkspace:
		return 500
	case SourceUser:
		return 400
	case SourceClawHub:
		return 300
	case SourceBundled:
		return 200
	case SourcePlugin:
		return 100
	default:
		return 10
	}
}

type SkillSource struct {
	Kind     SourceKind
	Root     string
	Dir      string
	NameHint string
	Priority int
}

type JSONSchema map[string]any

type SkillFile struct {
	Path string
	Size int64
}

type RequiresSpec struct {
	Bins    []string `json:"bins" yaml:"bins"`
	AnyBins []string `json:"any_bins" yaml:"anyBins"`
	Env     []string `json:"env" yaml:"env"`
	Config  []string `json:"config" yaml:"config"`
}

type InstallSpec struct {
	ID              string            `json:"id" yaml:"id"`
	Kind            string            `json:"kind" yaml:"kind"`
	Label           string            `json:"label" yaml:"label"`
	OS              []string          `json:"os" yaml:"os"`
	Bins            []string          `json:"bins" yaml:"bins"`
	Formula         string            `json:"formula" yaml:"formula"`
	Package         string            `json:"package" yaml:"package"`
	Module          string            `json:"module" yaml:"module"`
	URL             string            `json:"url" yaml:"url"`
	Archive         string            `json:"archive" yaml:"archive"`
	Extract         *bool             `json:"extract" yaml:"extract"`
	StripComponents int               `json:"strip_components" yaml:"stripComponents"`
	TargetDir       string            `json:"target_dir" yaml:"targetDir"`
	Args            []string          `json:"args" yaml:"args"`
	Env             map[string]string `json:"env" yaml:"env"`
	Shell           string            `json:"shell" yaml:"shell"`
	Script          string            `json:"script" yaml:"script"`
}

type OpenClawMetadata struct {
	Always     bool          `json:"always" yaml:"always"`
	Emoji      string        `json:"emoji" yaml:"emoji"`
	Homepage   string        `json:"homepage" yaml:"homepage"`
	OS         []string      `json:"os" yaml:"os"`
	PrimaryEnv string        `json:"primary_env" yaml:"primaryEnv"`
	SkillKey   string        `json:"skill_key" yaml:"skillKey"`
	Requires   RequiresSpec  `json:"requires" yaml:"requires"`
	Install    []InstallSpec `json:"install" yaml:"install"`
}

type ToolRuntimeSpec struct {
	Entry string `json:"entry" yaml:"entry"`
	Shell string `json:"shell" yaml:"shell"`
}

type ToolSecuritySpec struct {
	Trust            string `json:"trust" yaml:"trust"`
	RequiresApproval *bool  `json:"requires_approval" yaml:"requires_approval"`
}

type ToolManifestSpec struct {
	Name             string     `json:"name" yaml:"name"`
	Description      string     `json:"description" yaml:"description"`
	Aliases          []string   `json:"aliases" yaml:"aliases"`
	InputSchema      JSONSchema `json:"input_schema" yaml:"input_schema"`
	OutputSchema     JSONSchema `json:"output_schema" yaml:"output_schema"`
	SideEffectClass  string     `json:"side_effect_class" yaml:"side_effect_class"`
	Idempotent       *bool      `json:"idempotent" yaml:"idempotent"`
	ExecutionKey     string     `json:"execution_key" yaml:"execution_key"`
	Timeout          string     `json:"timeout" yaml:"timeout"`
	RequiresApproval *bool      `json:"requires_approval" yaml:"requires_approval"`
}

type CompanionManifest struct {
	Version  string           `json:"version" yaml:"version"`
	Tool     ToolManifestSpec `json:"tool" yaml:"tool"`
	Runtime  ToolRuntimeSpec  `json:"runtime" yaml:"runtime"`
	Security ToolSecuritySpec `json:"security" yaml:"security"`
}

type ExternalSkillSpec struct {
	Name                   string
	Description            string
	Body                   string
	Frontmatter            map[string]any
	RawMetadata            map[string]any
	Homepage               string
	UserInvocable          bool
	DisableModelInvocation bool
	CommandDispatch        string
	CommandTool            string
	CommandArgMode         string
	OpenClaw               OpenClawMetadata
	Companion              *CompanionManifest
	Bundle                 *BundleManifest
	SupportingFiles        []SkillFile
}

type CommandDescriptor struct {
	Dispatch string
	Tool     string
	ArgMode  string
}

type PromptSkill struct {
	Name                   string
	Description            string
	Instructions           string
	Location               string
	Homepage               string
	UserInvocable          bool
	DisableModelInvocation bool
	Command                CommandDescriptor
}

type ToolManifest struct {
	Name             string
	Aliases          []string
	Description      string
	InputSchema      JSONSchema
	OutputSchema     JSONSchema
	SideEffectClass  string
	Idempotent       bool
	RequiresApproval bool
	ExecutionKey     string
	Timeout          time.Duration
	Runtime          ToolRuntimeSpec
}

type SkillKind string

const (
	SkillKindPrompt     SkillKind = "prompt"
	SkillKindExecutable SkillKind = "executable"
)

type PackageStatus string

const (
	StatusReady    PackageStatus = "ready"
	StatusDegraded PackageStatus = "degraded"
	StatusBlocked  PackageStatus = "blocked"
)

type IssueSeverity string

const (
	SeverityWarning IssueSeverity = "warning"
	SeverityError   IssueSeverity = "error"
)

type SkillIssue struct {
	Severity IssueSeverity
	Code     string
	Message  string
}

type TrustClass string

const (
	TrustUnknown   TrustClass = "unknown"
	TrustCommunity TrustClass = "community"
	TrustVerified  TrustClass = "verified"
	TrustInternal  TrustClass = "internal"
	TrustBundled   TrustClass = "bundled"
)

type SkillPackage struct {
	ID            string
	Source        SkillSource
	Kind          SkillKind
	Status        PackageStatus
	Prompt        PromptSkill
	ToolManifests []ToolManifest
	OpenClaw      OpenClawMetadata
	Trust         TrustClass
	Issues        []SkillIssue
	Raw           ExternalSkillSpec
	LoadedAt      time.Time
	Normalized    bool
}

func (p *SkillPackage) Name() string {
	return p.Prompt.Name
}

func (p *SkillPackage) ConfigKey() string {
	if p.OpenClaw.SkillKey != "" {
		return p.OpenClaw.SkillKey
	}
	return p.Prompt.Name
}

type PromptCatalogEntry struct {
	Name        string
	Description string
	Location    string
	ToolDomains []string
}

type RegistrySnapshot struct {
	GeneratedAt time.Time
	Fingerprint string
	Skills      map[string]*SkillPackage
	Ordered     []*SkillPackage
	Blocked     []BlockedSkill
}

type BlockedSkill struct {
	Source   SkillSource
	NameHint string
	Issues   []SkillIssue
}

type SecretStatus struct {
	Resolved bool   `json:"resolved"`
	Source   string `json:"source,omitempty"`
}

type ConfigStatus struct {
	Present bool   `json:"present"`
	Truthy  bool   `json:"truthy"`
	Source  string `json:"source,omitempty"`
}

type ManagedEntry struct {
	Enabled     *bool                   `json:"enabled,omitempty"`
	InjectedEnv map[string]SecretStatus `json:"injected_env,omitempty"`
	ConfigTruth map[string]ConfigStatus `json:"config_truth,omitempty"`
}

type RuntimeContext struct {
	GOOS               string                  `json:"goos"`
	GOARCH             string                  `json:"goarch,omitempty"`
	Shell              ShellContext            `json:"shell,omitempty"`
	Git                GitContext              `json:"git,omitempty"`
	IDE                IDEContext              `json:"ide,omitempty"`
	Workspace          WorkspaceContext        `json:"workspace,omitempty"`
	SecretPresence     map[string]SecretStatus `json:"secret_presence,omitempty"`
	ConfigTruth        map[string]ConfigStatus `json:"config_truth,omitempty"`
	Managed            map[string]ManagedEntry `json:"managed,omitempty"`
	ModuleCapabilities []string                `json:"module_capabilities,omitempty"`
}

type ShellContext struct {
	Name    string `json:"name,omitempty"` // bash, zsh, fish, etc.
	Path    string `json:"path,omitempty"` // /bin/zsh
	Version string `json:"version,omitempty"`
}

type GitContext struct {
	InRepo    bool     `json:"in_repo"`
	Root      string   `json:"root,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Remotes   []string `json:"remotes,omitempty"`
	RemoteURL string   `json:"remote_url,omitempty"`
	Dirty     bool     `json:"dirty,omitempty"`
}

type IDEContext struct {
	Name    string `json:"name,omitempty"` // vscode, cursor, jetbrains, etc.
	Version string `json:"version,omitempty"`
}

type WorkspaceContext struct {
	Root        string   `json:"root,omitempty"`
	ProjectType string   `json:"project_type,omitempty"` // go, node, python, rust, etc.
	Markers     []string `json:"markers,omitempty"`      // go.mod, package.json, etc.
}

type EligibilityResult struct {
	Eligible    bool
	Always      bool
	InjectedEnv []string
	Reasons     []string
	Checks      []DependencyCheck
}

type BoundSkill struct {
	Package     *SkillPackage
	Eligibility EligibilityResult
}

type BoundTool struct {
	Package     *SkillPackage
	Manifest    ToolManifest
	Eligibility EligibilityResult
}

type SessionSkillSnapshot struct {
	GeneratedAt        time.Time
	Fingerprint        string
	ContextFingerprint string
	Skills             map[string]BoundSkill
	Ordered            []BoundSkill
	PromptCatalog      []PromptCatalogEntry
	PromptBlock        string
	Blocked            []BlockedSkill
}

type RegistrySkill struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Summary   string `json:"summary"`
	BundleURL string `json:"bundle_url,omitempty"`
	BundleDir string `json:"bundle_dir,omitempty"`
}

type InstallRequest struct {
	SkillID string
	Version string
	Root    string
}

type InstallResult struct {
	SkillID        string
	Version        string
	InstallDir     string
	LockFilePath   string
	InstallerSteps []InstallStepResult
}

type InstallStepStatus string

const (
	InstallStepSkipped InstallStepStatus = "skipped"
	InstallStepRan     InstallStepStatus = "ran"
)

type InstallStepResult struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Label   string            `json:"label,omitempty"`
	Status  InstallStepStatus `json:"status"`
	Reason  string            `json:"reason,omitempty"`
	Command []string          `json:"command,omitempty"`
	Path    string            `json:"path,omitempty"`
}

type InstalledSkillLock struct {
	SkillID     string    `json:"skill_id"`
	Version     string    `json:"version"`
	InstallDir  string    `json:"install_dir"`
	BundleDir   string    `json:"bundle_dir,omitempty"`
	Pinned      bool      `json:"pinned"`
	InstalledAt time.Time `json:"installed_at"`
}

type SkillsLockFile struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Skills      []InstalledSkillLock `json:"skills"`
}

// PublishRequest describes a skill to publish to a remote hub.
type PublishRequest struct {
	SkillDir  string // path to bundle directory containing SKILL.md or BUNDLE.yaml
	Slug      string // registry slug (e.g. "my-skill")
	Version   string // semver version to publish
	Changelog string // what changed in this version
}

// PublishResult is returned after a successful publish.
type PublishResult struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
	URL     string `json:"url,omitempty"` // URL to view the published skill
}

// Limits controls size and count caps for skill loading.
type Limits struct {
	MaxFileSize     int64 // max SKILL.md file size in bytes (default 256KB)
	MaxSkillsPerDir int   // max skills per discovery root (default 200)
	MaxTotalSkills  int   // max total skills across all roots (default 500)
	MaxPromptChars  int   // max total chars in prompt catalog (default 30000)
}

// DefaultLimits returns the default skill loading limits.
func DefaultLimits() Limits {
	return Limits{
		MaxFileSize:     256 * 1024,
		MaxSkillsPerDir: 200,
		MaxTotalSkills:  500,
		MaxPromptChars:  30000,
	}
}
