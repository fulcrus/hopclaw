package verify

import "strings"

type Status string

const (
	StatusPassed  Status = "passed"
	StatusWarning Status = "warning"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

type Requirement string

const (
	RequirementRequired Requirement = "required"
	RequirementAdvisory Requirement = "advisory"
)

type IssueSeverity string

const (
	SeverityInfo     IssueSeverity = "info"
	SeverityWarning  IssueSeverity = "warning"
	SeverityError    IssueSeverity = "error"
	SeverityBlocking IssueSeverity = "blocking"
)

type Issue struct {
	Code     string        `json:"code,omitempty"`
	Severity IssueSeverity `json:"severity"`
	Message  string        `json:"message"`
}

type Check struct {
	Name        string        `json:"name"`
	Domain      string        `json:"domain,omitempty"`
	Requirement Requirement   `json:"requirement,omitempty"`
	Status      Status        `json:"status"`
	Severity    IssueSeverity `json:"severity,omitempty"`
	Summary     string        `json:"summary,omitempty"`
	Issues      []Issue       `json:"issues,omitempty"`
	Tools       []string      `json:"tools,omitempty"`
	Evidence    []string      `json:"evidence,omitempty"`
}

type Deliverable struct {
	Kind        string `json:"kind"`
	URI         string `json:"uri,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type Contract struct {
	Goal                   string                `json:"goal,omitempty"`
	JobType                string                `json:"job_type,omitempty"`
	TargetSummary          string                `json:"target_summary,omitempty"`
	ExpectedDeliverables   []ContractDeliverable `json:"expected_deliverables,omitempty"`
	AcceptanceCriteria     []ContractAcceptance  `json:"acceptance_criteria,omitempty"`
	MissingInfo            []ContractMissingInfo `json:"missing_info,omitempty"`
	RequiresExternalEffect bool                  `json:"requires_external_effect,omitempty"`
	RequiresApproval       bool                  `json:"requires_approval,omitempty"`
}

type ContractDeliverable struct {
	Kind     string `json:"kind"`
	Required bool   `json:"required,omitempty"`
}

type ContractAcceptance struct {
	ID               string   `json:"id,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Required         bool     `json:"required,omitempty"`
	DeliverableKinds []string `json:"deliverable_kinds,omitempty"`
}

type ContractMissingInfo struct {
	ID       string `json:"id,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type Input struct {
	RunID                string
	SessionID            string
	SessionKey           string
	Status               string
	Error                string
	Summary              string
	Output               string
	Deliverables         []Deliverable
	ToolNames            []string
	ToolOutputs          []string
	Contract             *Contract
	PlanCoverageWarnings []string
}

type RunVerification struct {
	RunID            string  `json:"run_id"`
	Status           Status  `json:"status"`
	Summary          string  `json:"summary,omitempty"`
	Checks           []Check `json:"checks,omitempty"`
	Infos            int     `json:"infos,omitempty"`
	Warnings         int     `json:"warnings,omitempty"`
	Failures         int     `json:"failures,omitempty"`
	BlockingFailures int     `json:"blocking_failures,omitempty"`
	RequiredWarnings int     `json:"required_warnings,omitempty"`
	RequiredFailures int     `json:"required_failures,omitempty"`
	AdvisoryWarnings int     `json:"advisory_warnings,omitempty"`
	AdvisoryFailures int     `json:"advisory_failures,omitempty"`
}

type Policy struct {
	VerifierSeverities map[string]IssueSeverity
}

type Verifier interface {
	Name() string
	Applies(input Input) bool
	Verify(input Input) Check
}

func (in Input) HasToolPrefix(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	for _, name := range in.ToolNames {
		if strings.HasPrefix(strings.TrimSpace(name), prefix) {
			return true
		}
	}
	return false
}

func (rv RunVerification) HasRequiredIssues() bool {
	return rv.RequiredWarnings > 0 || rv.RequiredFailures > 0
}

func (rv RunVerification) HasAdvisoryIssues() bool {
	return rv.AdvisoryWarnings > 0 || rv.AdvisoryFailures > 0
}

func (rv RunVerification) ShouldBlockDelivery() bool {
	return rv.BlockingFailures > 0
}

func DefaultPolicy() Policy {
	return Policy{}
}

func (p Policy) Normalized() Policy {
	if len(p.VerifierSeverities) == 0 {
		return Policy{}
	}
	out := make(map[string]IssueSeverity, len(p.VerifierSeverities))
	for name, severity := range p.VerifierSeverities {
		name = strings.TrimSpace(name)
		severity = normalizeIssueSeverity(severity)
		if name == "" || severity == "" {
			continue
		}
		out[name] = severity
	}
	if len(out) == 0 {
		return Policy{}
	}
	return Policy{VerifierSeverities: out}
}

func (p Policy) SeverityFor(name string, status Status) IssueSeverity {
	if status == StatusPassed || status == StatusSkipped {
		return SeverityInfo
	}
	if severity, ok := p.Normalized().VerifierSeverities[strings.TrimSpace(name)]; ok && severity != "" {
		return severity
	}
	return SeverityWarning
}

func PolicyFromStrings(values map[string]string) Policy {
	if len(values) == 0 {
		return Policy{}
	}
	out := make(map[string]IssueSeverity, len(values))
	for name, severity := range values {
		name = strings.TrimSpace(name)
		parsed := normalizeIssueSeverity(IssueSeverity(strings.TrimSpace(severity)))
		if name == "" || parsed == "" {
			continue
		}
		out[name] = parsed
	}
	return Policy{VerifierSeverities: out}.Normalized()
}

func normalizeIssueSeverity(severity IssueSeverity) IssueSeverity {
	switch strings.ToLower(strings.TrimSpace(string(severity))) {
	case string(SeverityInfo):
		return SeverityInfo
	case string(SeverityWarning):
		return SeverityWarning
	case string(SeverityError):
		return SeverityError
	case string(SeverityBlocking):
		return SeverityBlocking
	default:
		return ""
	}
}
