package repl

type ToolSummary struct {
	Name             string
	Description      string
	InputSchema      map[string]any
	OutputSchema     map[string]any
	SideEffectClass  string
	RequiresApproval bool
	Source           string
	Eligible         bool
}

type SkillSummary struct {
	ID              string
	Name            string
	Kind            string
	Status          string
	Trust           string
	Version         string
	InstallDir      string
	BundleDir       string
	SourceKind      string
	Summary         string
	Description     string
	Pinned          bool
	Installed       bool
	InstalledAt     string
	Ready           bool
	Eligible        bool
	DetailAvailable bool
}

type SkillCatalogSummary struct {
	ID              string
	Name            string
	Version         string
	Summary         string
	Description     string
	Installed       bool
	Ready           bool
	Eligible        bool
	SourceKind      string
	DetailAvailable bool
}

type SkillDetail struct {
	Installed *SkillSummary
	Catalog   *SkillCatalogSummary
}

type SkillInstallResult struct {
	SkillID            string
	Version            string
	InstallDir         string
	LockFile           string
	InstallerStepCount int
}
