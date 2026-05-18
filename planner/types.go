package planner

type Strategy string

const (
	StrategySerial   Strategy = "serial"
	StrategyParallel Strategy = "parallel"
	StrategyMixed    Strategy = "mixed"
)

type FailurePolicy string

const (
	FailFast        FailurePolicy = "fail_fast"
	ContinueOnError FailurePolicy = "continue_on_error"
)

type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
	TaskSkipped   TaskStatus = "skipped"
)

type TaskKind string

const (
	TaskResearch  TaskKind = "research"
	TaskTranslate TaskKind = "translate"
	TaskTransform TaskKind = "transform"
	TaskWrite     TaskKind = "write"
	TaskExecute   TaskKind = "execute"
	TaskReview    TaskKind = "review"
	TaskDeliver   TaskKind = "deliver"
)

type Plan struct {
	Goal             string        `json:"goal"`
	Summary          string        `json:"summary,omitempty"`
	Strategy         Strategy      `json:"strategy,omitempty"`
	FailurePolicy    FailurePolicy `json:"failure_policy,omitempty"`
	Tasks            []Task        `json:"tasks"`
	FinalTask        string        `json:"final_task,omitempty"`
	CoverageWarnings []string      `json:"plan_coverage_warnings,omitempty"`

	// Legacy single-active-task field, kept for backward compatibility.
	// Parallel execution uses RunningTasks instead.
	ActiveTask string `json:"active_task,omitempty"`

	// RunningTasks tracks all currently executing task IDs.
	RunningTasks []string `json:"running_tasks,omitempty"`
}

type Task struct {
	ID                   string     `json:"id"`
	Title                string     `json:"title,omitempty"`
	Kind                 TaskKind   `json:"kind"`
	Goal                 string     `json:"goal"`
	DependsOn            []string   `json:"depends_on,omitempty"`
	Outputs              []string   `json:"outputs,omitempty"`
	RequiredCapabilities []string   `json:"required_capabilities,omitempty"`
	VerificationHints    []string   `json:"verification_hints,omitempty"`
	Status               TaskStatus `json:"status,omitempty"`
	ResultSummary        string     `json:"result_summary,omitempty"`
	Error                string     `json:"error,omitempty"`
}

type Context struct {
	LatestMessage   string      `json:"latest_message"`
	SessionSummary  string      `json:"session_summary,omitempty"`
	RecentMessages  []Message   `json:"recent_messages,omitempty"`
	AvailableTools  []string    `json:"available_tools,omitempty"`
	TaskContract    *Contract   `json:"task_contract,omitempty"`
	Delegation      *Delegation `json:"delegation_contract,omitempty"`
	PinnedFacts     []string    `json:"pinned_facts,omitempty"`
	SessionState    string      `json:"session_state,omitempty"`
	RecalledContext string      `json:"recalled_context,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Contract struct {
	Goal                   string   `json:"goal,omitempty"`
	JobType                string   `json:"job_type,omitempty"`
	TargetSummary          string   `json:"target_summary,omitempty"`
	SuggestedDomains       []string `json:"suggested_domains,omitempty"`
	CapabilityHints        []string `json:"capability_hints,omitempty"`
	ExpectedDeliverables   []string `json:"expected_deliverables,omitempty"`
	EvidenceRequirements   []string `json:"evidence_requirements,omitempty"`
	AcceptanceCriteria     []string `json:"acceptance_criteria,omitempty"`
	MissingInfo            []string `json:"missing_info,omitempty"`
	RequiresExternalEffect bool     `json:"requires_external_effect,omitempty"`
	RequiresApproval       bool     `json:"requires_approval,omitempty"`
}

type Delegation struct {
	Goal                string   `json:"goal,omitempty"`
	AllowedDomains      []string `json:"allowed_domains,omitempty"`
	SideEffectClass     string   `json:"side_effect_class,omitempty"`
	MaxTurns            int      `json:"max_turns,omitempty"`
	MaxBudgetTokens     int      `json:"max_budget_tokens,omitempty"`
	RequiresApproval    bool     `json:"requires_approval,omitempty"`
	VerificationPlanRef string   `json:"verification_plan_ref,omitempty"`
}
