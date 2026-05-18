package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/resultmodel"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

const (
	verificationRepairEventPrefix = "__verification_repair__:"
	verificationHTTPTimeout       = 2 * time.Second
)

var (
	terminalURLPattern      = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `)]+`)
	terminalArtifactPattern = regexp.MustCompile(`artifact://[^\s<>"'` + "`" + `)]+`)
)

type terminalVerificationReport struct {
	Failures []string
}

type terminalDeliverableRef struct {
	Kind        string
	ToolName    string
	Path        string
	Workspace   string
	ArtifactURI string
}

func (r terminalVerificationReport) OK() bool {
	return len(r.Failures) == 0
}

func (r terminalVerificationReport) Summary() string {
	if len(r.Failures) == 0 {
		return ""
	}
	if len(r.Failures) == 1 {
		return r.Failures[0]
	}
	return strings.Join(r.Failures, "; ")
}

func DeliverTerminalWithVerification(ctx context.Context, runtime BridgeRuntime, notifier *RunStatusNotifier, send func(context.Context, OutboundMessage) error, session *agent.Session, run *agent.Run, outbound OutboundMessage) error {
	if send == nil {
		return fmt.Errorf("send function is required")
	}
	statusKind := meta.NormalizeStatusKind(fmt.Sprint(outbound.Metadata[meta.KeyStatusKind]))
	if (statusKind != meta.StatusKindCompleted && statusKind != meta.StatusKindPartial) || runtime == nil || session == nil || run == nil {
		return send(ctx, outbound)
	}

	deliverables := extractRunDeliverables(session, run.ID)
	report := verifyTerminalOutbound(ctx, runtime, outbound.Content, deliverables)
	if report.OK() {
		normalized := outbound
		normalized.Metadata = cloneAnyMap(outbound.Metadata)
		normalized.Content = normalizeTerminalOutboundContent(BridgeInputContent(session, run.InputEventID), outbound.Content, deliverables)
		return send(ctx, normalized)
	}

	if isVerificationRepairRun(run) {
		fallback := outbound
		fallback.Metadata = cloneAnyMap(outbound.Metadata)
		if fallback.Metadata == nil {
			fallback.Metadata = make(map[string]any, 2)
		}
		fallback.Metadata[meta.KeyStatusKind] = meta.StatusKindVerificationFailed.String()
		fallback.Content = BridgeVerificationFailureMessage(BridgeInputContent(session, run.InputEventID), report.Summary())
		return send(ctx, fallback)
	}

	repairReq := runtimesvc.SubmitRequest{
		SessionKey:      session.Key,
		ParentRunID:     run.ID,
		ExternalEventID: verificationRepairEventID(run.ID),
		Content:         buildVerificationRepairContent(session, run, outbound.Content, report),
		Model:           run.Model,
		AutomationID:    strings.TrimSpace(run.Scope.AutomationID),
		Metadata: map[string]any{
			"verification_repair_for_run_id": run.ID,
			"verification_failures":          append([]string(nil), report.Failures...),
		},
	}
	repairRun, err := runtime.Submit(ctx, repairReq)
	if err != nil || repairRun == nil || stringsTrim(repairRun.ID) == "" {
		fallback := outbound
		fallback.Metadata = cloneAnyMap(outbound.Metadata)
		if fallback.Metadata == nil {
			fallback.Metadata = make(map[string]any, 2)
		}
		fallback.Metadata[meta.KeyStatusKind] = meta.StatusKindVerificationFailed.String()
		fallback.Content = BridgeVerificationFailureMessage(BridgeInputContent(session, run.InputEventID), report.Summary())
		return send(ctx, fallback)
	}

	ack := outbound
	ack.Metadata = cloneAnyMap(outbound.Metadata)
	if ack.Metadata == nil {
		ack.Metadata = make(map[string]any, 4)
	}
	ack.Metadata[meta.KeyStatusKind] = meta.StatusKindVerificationRepairStarted.String()
	ack.Metadata[meta.KeyRunID] = repairRun.ID
	ack.Metadata["replaces_run_id"] = run.ID
	ack.Metadata["verification_summary"] = report.Summary()
	ack.Content = BridgeVerificationRepairStartedMessage(BridgeInputContent(session, run.InputEventID), report.Summary())
	if err := send(ctx, ack); err != nil {
		return err
	}

	if notifier != nil {
		target := RunNotificationTarget{
			RunID:        repairRun.ID,
			SessionKey:   session.Key,
			ChannelID:    outbound.ChannelID,
			TargetID:     outbound.TargetID,
			ReplyToID:    outbound.ReplyToID,
			InputContent: BridgeInputContent(session, run.InputEventID),
			Format:       defaultOutboundFormat(outbound.Format),
			Metadata:     cloneAnyMap(outbound.Metadata),
		}
		if target.Metadata != nil {
			delete(target.Metadata, meta.KeyRunID)
			delete(target.Metadata, meta.KeyStatusKind)
		}
		notifier.Track(ctx, target)
	}
	return nil
}

func verifyTerminalOutbound(ctx context.Context, runtime BridgeRuntime, content string, deliverables []terminalDeliverableRef) terminalVerificationReport {
	failures := make([]string, 0, 4)
	for _, rawURL := range extractLocallyVerifiableURLs(content) {
		if err := verifyServiceURL(ctx, rawURL); err != nil {
			failures = append(failures, fmt.Sprintf("%s (%v)", rawURL, err))
		}
	}
	for _, uri := range extractArtifactURIs(content) {
		if _, err := runtime.GetArtifact(ctx, uri); err != nil {
			failures = append(failures, fmt.Sprintf("%s (%v)", uri, err))
		}
	}
	seenFiles := make(map[string]struct{})
	for _, ref := range deliverables {
		if ref.Path == "" || ref.Workspace == "" {
			continue
		}
		key := ref.Workspace + "|" + ref.Path
		if _, ok := seenFiles[key]; ok {
			continue
		}
		seenFiles[key] = struct{}{}
		if err := verifyDeliverablePath(ref.Workspace, ref.Path); err != nil {
			failures = append(failures, fmt.Sprintf("%s (%v)", filepath.ToSlash(filepath.Join(ref.Workspace, ref.Path)), err))
		}
	}
	return terminalVerificationReport{Failures: failures}
}

func extractLocallyVerifiableURLs(content string) []string {
	matches := terminalURLPattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := trimTerminalReference(match)
		parsed, err := url.Parse(candidate)
		if err != nil || !shouldVerifyTerminalURL(parsed) {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

func extractArtifactURIs(content string) []string {
	matches := terminalArtifactPattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := trimTerminalReference(match)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

func trimTerminalReference(value string) string {
	return stringsTrim(strings.TrimRight(value, ".,;:!?)]}"))
}

func shouldVerifyTerminalURL(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	switch strings.ToLower(stringsTrim(parsed.Scheme)) {
	case "http", "https":
	default:
		return false
	}
	host := strings.ToLower(stringsTrim(parsed.Hostname()))
	if host == "" {
		return false
	}
	if host == "localhost" || host == "0.0.0.0" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func verifyServiceURL(ctx context.Context, rawURL string) error {
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), verificationHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "HopClaw/verification")
	client := &http.Client{Timeout: verificationHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func verificationRepairEventID(runID string) string {
	return verificationRepairEventPrefix + stringsTrim(runID)
}

func isVerificationRepairRun(run *agent.Run) bool {
	if run == nil {
		return false
	}
	return strings.HasPrefix(stringsTrim(run.InputEventID), verificationRepairEventPrefix)
}

func buildVerificationRepairContent(session *agent.Session, run *agent.Run, previousReply string, report terminalVerificationReport) string {
	parts := []string{
		"The previous completion failed terminal verification. Re-check the concrete deliverables before replying.",
		"Do not repeat the earlier success claim until the externally observable result is verified.",
		"Verification failures: " + report.Summary(),
	}
	if input := stringsTrim(BridgeInputContent(session, run.InputEventID)); input != "" {
		parts = append(parts, "Original user request: "+input)
	}
	if reply := stringsTrim(previousReply); reply != "" {
		parts = append(parts, "Previous assistant reply: "+reply)
	}
	parts = append(parts, "If the issue cannot be fixed, explain what failed and what you verified.")
	return strings.Join(parts, "\n")
}

func defaultOutboundFormat(format string) string {
	if stringsTrim(format) == "" {
		return "text"
	}
	return format
}

func extractRunDeliverables(session *agent.Session, runID string) []terminalDeliverableRef {
	if session == nil || stringsTrim(runID) == "" {
		return nil
	}
	toolCallIDs := make(map[string]struct{})
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleAssistant || bridgeMessageRunID(msg.Metadata) != runID {
			continue
		}
		for _, ref := range msg.ToolCalls {
			if stringsTrim(ref.ID) != "" {
				toolCallIDs[ref.ID] = struct{}{}
			}
		}
	}
	if len(toolCallIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]terminalDeliverableRef, 0, 4)
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleTool || stringsTrim(msg.ToolCallID) == "" {
			continue
		}
		if _, ok := toolCallIDs[msg.ToolCallID]; !ok {
			continue
		}
		for _, ref := range parseToolDeliverables(msg) {
			key := deliverableKey(ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}

func parseToolDeliverables(msg contextengine.Message) []terminalDeliverableRef {
	result, hasStructuredResult := resultmodel.DecodeToolResultMetadata(msg.Metadata)
	payload := parseToolResultPayload(msg.Content)
	if hasStructuredResult && len(payload) == 0 {
		payload = parseToolResultPayload(result.Content)
	}
	if len(payload) == 0 && !strings.Contains(msg.Content, "artifact://") {
		if !hasStructuredResult || len(result.Artifacts) == 0 {
			return nil
		}
	}
	out := make([]terminalDeliverableRef, 0, 2)
	workspace := stringsTrim(normalize.String(payload["workspace"]))
	switch stringsTrim(msg.Name) {
	case "fs.write", "fs.append", "net.download":
		if path := stringsTrim(normalize.String(payload["path"])); path != "" {
			out = append(out, terminalDeliverableRef{Kind: "file", ToolName: msg.Name, Path: path, Workspace: workspace})
		}
	case "fs.move", "fs.copy":
		if path := stringsTrim(normalize.String(payload["destination"])); path != "" {
			out = append(out, terminalDeliverableRef{Kind: "file", ToolName: msg.Name, Path: path, Workspace: workspace})
		}
	}
	for _, key := range []string{"artifact_uri", "artifact_ref"} {
		if uri := stringsTrim(normalize.String(payload[key])); strings.HasPrefix(uri, "artifact://") {
			out = append(out, terminalDeliverableRef{Kind: "artifact", ToolName: msg.Name, ArtifactURI: uri})
		}
	}
	if hasStructuredResult {
		for _, artifact := range result.Artifacts {
			if uri := stringsTrim(artifact.URI); strings.HasPrefix(uri, "artifact://") {
				out = append(out, terminalDeliverableRef{Kind: "artifact", ToolName: msg.Name, ArtifactURI: uri})
			}
		}
	}
	for _, uri := range extractArtifactURIs(msg.Content) {
		out = append(out, terminalDeliverableRef{Kind: "artifact", ToolName: msg.Name, ArtifactURI: uri})
	}
	return out
}

func parseToolResultPayload(content string) map[string]any {
	trimmed := stringsTrim(content)
	if trimmed == "" {
		return nil
	}
	if idx := strings.Index(trimmed, "\n[artifact] "); idx >= 0 {
		trimmed = stringsTrim(trimmed[:idx])
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	return payload
}

func normalizeTerminalOutboundContent(inputContent, content string, deliverables []terminalDeliverableRef) string {
	refs := filterDeliverablesMissingFromContent(content, deliverables)
	if len(refs) == 0 {
		return content
	}
	var lines []string
	if bridgeLooksChinese(inputContent) {
		lines = append(lines, "交付物：")
		for _, ref := range refs {
			switch ref.Kind {
			case "artifact":
				lines = append(lines, "- 产物: "+ref.ArtifactURI)
			case "file":
				lines = append(lines, "- 文件: "+ref.Path)
			}
		}
	} else {
		lines = append(lines, "Deliverables:")
		for _, ref := range refs {
			switch ref.Kind {
			case "artifact":
				lines = append(lines, "- Artifact: "+ref.ArtifactURI)
			case "file":
				lines = append(lines, "- File: "+ref.Path)
			}
		}
	}
	return strings.TrimSpace(content) + "\n\n" + strings.Join(lines, "\n")
}

func filterDeliverablesMissingFromContent(content string, refs []terminalDeliverableRef) []terminalDeliverableRef {
	if len(refs) == 0 {
		return nil
	}
	trimmed := stringsTrim(content)
	out := make([]terminalDeliverableRef, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		key := deliverableKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		switch ref.Kind {
		case "artifact":
			if ref.ArtifactURI == "" || strings.Contains(trimmed, ref.ArtifactURI) {
				continue
			}
		case "file":
			if ref.Path == "" || strings.Contains(trimmed, ref.Path) || strings.Contains(trimmed, filepath.Base(ref.Path)) {
				continue
			}
		}
		out = append(out, ref)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func deliverableKey(ref terminalDeliverableRef) string {
	if ref.ArtifactURI != "" {
		return "artifact|" + ref.ArtifactURI
	}
	return "file|" + ref.Workspace + "|" + ref.Path
}

func verifyDeliverablePath(workspace, path string) error {
	workspace = stringsTrim(workspace)
	path = stringsTrim(path)
	if workspace == "" || path == "" {
		return nil
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspace, filepath.FromSlash(path))
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory")
	}
	return nil
}
