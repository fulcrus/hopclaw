package repl

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type LayoutMode string

const (
	LayoutAuto    LayoutMode = "auto"
	LayoutFull    LayoutMode = "full"
	LayoutCompact LayoutMode = "compact"
	LayoutPlain   LayoutMode = "plain"
	LayoutMinimal LayoutMode = "minimal"
)

type REPLViewState struct {
	Target             string
	TargetKind         string
	Profile            string
	Model              string
	Think              string
	ExecutionState     string
	ApprovalMode       string
	ApprovalCount      int
	ApprovalID         string
	ApprovalRisk       string
	Health             string
	UpdateVersion      string
	CWD                string
	GitBranch          string
	GitAdded           int
	GitModified        int
	Project            string
	Badge              string
	BadgeSize          int
	SessionKey         string
	EpisodeID          string
	Channel            string
	Runtime            string
	QueueDepth         int
	Sandbox            string
	Phase              string
	LastTool           string
	ScopeSummary       string
	ContextPercent     int
	PromptTokens       int
	CompletionTokens   int
	Quality            string
	LastFailure        string
	RunID              string
	Elapsed            string
	Duration           string
	Resumable          bool
	ForegroundRunCount int
	BackgroundRunCount int
	PausedRunCount     int
	AttentionCount     int
	AttentionPrimary   string
	DeliveryState      string
	DeliverySummary    string
	DeliveryNext       string
	MemoryStrip        string
	ActivePanel        string
	LayoutMode         LayoutMode
	TerminalWidth      int
}

func (s REPLViewState) Summary() string {
	target := targetConnectionLabel(s.Target, s.TargetKind)
	session := strings.TrimSpace(s.SessionKey)
	if session == "" {
		session = "default"
	}
	model := strings.TrimSpace(s.Model)
	if model == "" {
		model = "(default)"
	}
	phase := strings.TrimSpace(s.Phase)
	if phase == "" {
		phase = "idle"
	}
	execution := strings.TrimSpace(s.ExecutionState)
	if execution == "" {
		execution = "ready"
	}
	return strings.Join([]string{target, "conversation " + session, "model " + model, "status " + execution, "phase " + phase}, " · ")
}

func parseLayoutMode(value string) (LayoutMode, bool) {
	switch LayoutMode(strings.ToLower(strings.TrimSpace(value))) {
	case LayoutFull:
		return LayoutFull, true
	case LayoutCompact:
		return LayoutCompact, true
	case LayoutPlain:
		return LayoutPlain, true
	case LayoutAuto:
		return LayoutAuto, true
	case LayoutMinimal:
		return LayoutMinimal, true
	default:
		return "", false
	}
}

const (
	ProfileCoding     = "coding"
	ProfileOps        = "ops"
	ProfileChannel    = "channel"
	ProfileAutomation = "automation"
)

func normalizeProfile(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProfileCoding:
		return ProfileCoding
	case ProfileOps:
		return ProfileOps
	case ProfileChannel:
		return ProfileChannel
	case ProfileAutomation:
		return ProfileAutomation
	default:
		return ""
	}
}

func inferProfile(state REPLViewState) string {
	if profile := normalizeProfile(state.Profile); profile != "" {
		return profile
	}
	switch {
	case strings.EqualFold(strings.TrimSpace(state.ActivePanel), "automation"),
		strings.EqualFold(strings.TrimSpace(state.AttentionPrimary), "automation"):
		return ProfileAutomation
	case strings.TrimSpace(state.Channel) != "" && !strings.EqualFold(strings.TrimSpace(state.Channel), "cli"):
		return ProfileChannel
	case strings.EqualFold(strings.TrimSpace(state.Runtime), "gateway"), strings.EqualFold(normalizeTargetKind(state.TargetKind, state.Target), "remote"):
		return ProfileOps
	default:
		return ProfileCoding
	}
}

func normalizeTargetName(name string) string {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "", "standalone":
		return "local"
	default:
		return name
	}
}

func normalizeTargetKind(kind, target string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "remote":
		return "remote"
	case "local", "standalone":
		return "local"
	}
	if strings.EqualFold(normalizeTargetName(target), "local") {
		return "local"
	}
	return "remote"
}

func targetConnectionLabel(target, kind string) string {
	target = displayTargetName(target)
	switch normalizeTargetKind(kind, target) {
	case "local":
		if strings.EqualFold(target, "local") {
			return "local"
		}
		return "local:" + target
	default:
		return "remote:" + target
	}
}

func targetChipLabel(target, kind string) string {
	target = displayTargetName(target)
	switch normalizeTargetKind(kind, target) {
	case "local":
		if strings.EqualFold(target, "local") {
			return "LOCAL"
		}
		return "LOCAL " + target
	default:
		return "REMOTE " + target
	}
}

func targetDescriptor(target, kind string) string {
	target = displayTargetName(target)
	switch normalizeTargetKind(kind, target) {
	case "local":
		if strings.EqualFold(target, "local") {
			return "local"
		}
		return "local runtime " + target
	default:
		return "remote " + target
	}
}

func displayTargetName(target string) string {
	target = normalizeTargetName(target)
	lower := strings.ToLower(target)
	switch {
	case strings.HasPrefix(lower, "remote:"):
		return strings.TrimSpace(target[len("remote:"):])
	case strings.HasPrefix(lower, "remote "):
		return strings.TrimSpace(target[len("remote "):])
	case strings.HasPrefix(lower, "local:"):
		return normalizeTargetName(target[len("local:"):])
	case strings.HasPrefix(lower, "local "):
		return normalizeTargetName(target[len("local "):])
	default:
		return target
	}
}

// GitSnapshot holds git status info for the working directory.
type GitSnapshot = gitSnapshot

type gitSnapshot struct {
	Branch   string
	Added    int
	Modified int
}

// DetectGitSnapshot returns git branch and file status for the given directory.
func DetectGitSnapshot(cwd string) GitSnapshot {
	return detectGitSnapshot(cwd)
}

func detectGitSnapshot(cwd string) gitSnapshot {
	if strings.TrimSpace(cwd) == "" {
		return gitSnapshot{}
	}
	cmd := exec.Command("git", "-C", cwd, "status", "--branch", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return gitSnapshot{}
	}
	var snapshot gitSnapshot
	lines := bytes.Split(output, []byte{'\n'})
	for i, raw := range lines {
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(line, "## ") {
			branch := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if idx := strings.Index(branch, "..."); idx >= 0 {
				branch = branch[:idx]
			}
			snapshot.Branch = branch
			continue
		}
		if len(line) < 2 {
			continue
		}
		x, y := line[0], line[1]
		if x == '?' || y == '?' || x == 'A' || y == 'A' {
			snapshot.Added++
		}
		if x == 'M' || y == 'M' || x == 'R' || y == 'R' || x == 'D' || y == 'D' {
			snapshot.Modified++
		}
	}
	return snapshot
}

func abbreviatePath(path string, maxLen int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			path = filepath.Join("~", rel)
		} else if path == home {
			path = "~"
		}
	}
	path = filepath.Clean(path)
	if maxLen <= 0 || displayWidth(path) <= maxLen {
		return path
	}
	if maxLen <= 3 {
		return displayPrefix(path, maxLen)
	}
	base := filepath.Base(path)
	if displayWidth(base)+2 <= maxLen {
		return "…/" + base
	}
	return displayEllipsis + displaySuffix(path, maxLen-displayWidth(displayEllipsis))
}
