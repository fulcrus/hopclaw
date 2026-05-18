package repl

type ApprovalSummary struct {
	ID            string
	RunID         string
	SessionID     string
	Status        string
	ToolName      string
	PolicySummary string
	CreatedAt     string
}

type QualitySnapshot struct {
	RunCount            int
	TerminalRunCount    int
	Status              string
	TaskSuccess         string
	FalseSuccess        string
	VerificationFailure string
	TraceCount          int
	Ready               bool
	CheckCount          int
	BlockerCount        int
	Blockers            []string
	Warnings            []string
	LastCheck           string
}

type EvalSuiteSummary struct {
	ID        string
	Name      string
	Surface   string
	CaseCount int
}

type EvalRunSummary struct {
	SuiteID   string
	Status    string
	CaseCount int
	Passed    int
	Failed    int
	Errored   int
}

type SupervisorSnapshot struct {
	ForegroundRunID    string
	ActiveRunCount     int
	BackgroundRunCount int
	PausedRunCount     int
	AttentionCount     int
	Items              []RunSummary
}

type ReadinessCategory struct {
	ID      string
	Label   string
	Status  string
	Summary string
	Kind    string
}

type ReadinessSnapshot struct {
	OverallStatus      string
	StartupDiagnostics string
	Categories         []ReadinessCategory
	RecoveryCandidates []RecoveryCandidate
}

type RecoveryCandidate struct {
	Type    string
	ID      string
	Summary string
	Action  string
}

type RunDelivery struct {
	Status       string
	Summary      string
	Attempt      string
	NextAttempt  string
	ReceiptCount int
}

type RunDeliveryDetail struct {
	Status   string
	Summary  string
	Targets  []DeliveryTarget
	Receipts []DeliveryReceipt
}

type DeliveryTarget struct {
	Kind     string
	Label    string
	Status   string
	Attempts int
	NextAt   string
}

type DeliveryReceipt struct {
	TargetLabel    string
	Adapter        string
	Status         string
	At             string
	Error          string
	Attempt        int
	IdempotencyKey string
}

type DeliveryListItem struct {
	ID          string
	RunID       string
	AdapterName string
	Status      string
	Attempts    int
	MaxAttempts int
	LastError   string
	NextAt      string
	CanRedrive  bool
	Summary     string
}

type RedriveResult struct {
	Redriven int
	Failed   int
	Errors   []string
}

type AutomationProjection struct {
	ID      string
	Name    string
	Kind    string
	NextRun string
	Health  string
}

type AutomationSetupSlot struct {
	Field    string
	Question string
	Example  string
	Required bool
	Value    string
}

type AutomationSetupInfo struct {
	Status  string
	Summary string
	Slots   []AutomationSetupSlot
}

type AutomationItem struct {
	ID            string
	Name          string
	Kind          string
	Status        string
	Schedule      string
	Delivery      string
	NextRun       string
	Health        string
	SetupContract *AutomationSetupInfo
}

type AutomationCreateRequest struct {
	Name         string
	Kind         string
	Prompt       string
	Model        string
	SessionKey   string
	Delivery     string
	ScheduleKind string
	Expression   string
	Every        string
	At           string
	Enabled      bool
}

type MemoryUsageItem struct {
	Key       string
	Namespace string
	Scope     string
	Source    string
	Reason    string
}

type ContextPressureInfo struct {
	WindowSize     int
	UsedTokens     int
	UsedPercent    int
	KeptItems      int
	TrimmedItems   int
	Recommendation string
}

type RunSummary struct {
	ID                  string
	SessionID           string
	SessionKey          string
	Status              string
	Phase               string
	Model               string
	Error               string
	CreatedAt           string
	ToolName            string
	Target              string
	ScopeSummary        string
	Attention           string
	Outcome             string
	VerificationStatus  string
	VerificationSummary string
	Resumable           bool
	Automation          *AutomationProjection
	Delivery            *RunDelivery
}

type RunDetail struct {
	Run            RunSummary
	Output         string
	Scope          string
	Target         string
	Tool           string
	Timeline       []ToolTimelineEntry
	Delivery       *RunDelivery
	Automation     *AutomationProjection
	ScopeDetails   *RunScopeDetails
	Semantic       *RunSemanticDetails
	Workflow       *RunWorkflowDetails
	Delegation     *RunDelegationDetails
	ExecutionGraph *RunExecutionGraphDetails
}

type DoctorCheck struct {
	Category string
	Name     string
	Status   string
	Detail   string
	Fix      string
}

type RunScopeDetails struct {
	SideEffectScope string
	Destructive     bool
	Resources       []string
	Summary         string
}

type RunSemanticDetails struct {
	Language            string
	RequiresCurrentInfo bool
	NeedsReference      bool
	NeedsConfirmation   bool
	SuggestedDomains    []string
	JobType             string
	TargetSummary       string
	CapabilityHints     []string
	DeliverableKinds    []string
	MissingInfoIDs      []string
	TriageReady         bool
	TaskContractReady   bool
	Reason              string
}

type RunWorkflowDetails struct {
	Mode              string
	ContinuationIndex int
	TotalRoundsUsed   int
	Yielded           bool
	YieldReason       string
}

type RunDelegationDetails struct {
	Enabled         bool
	ParallelTasks   int
	SerialFallback  int
	SideEffectClass string
}

type RunExecutionGraphDetails struct {
	SingleSession  bool
	SessionLocking bool
	Tasks          []RunExecutionTask
}

type RunExecutionTask struct {
	ID              string
	Title           string
	Status          string
	AttemptCount    int
	MergeStrategy   string
	SideEffectScope string
	ResourceKeys    []string
	Summary         string
}
