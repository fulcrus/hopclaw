package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controlpolicy "github.com/fulcrus/hopclaw/internal/controlplane/policypack"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/internal/edition"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/policy"
)

func resolveDefaultPolicy(profile string, skillsCfg config.SkillsConfig) controlpolicy.Resolved {
	return controlpolicy.Resolve(controlpolicy.ResolveInput{
		RuntimeProfile:     profile,
		SkillInstallPolicy: strings.TrimSpace(skillsCfg.InstallPolicy),
	})
}

func defaultPolicyConfig(profile string, skillsCfg config.SkillsConfig) policy.Config {
	return resolveDefaultPolicy(profile, skillsCfg).Config
}

func effectiveConfigLayers(resolved controlpolicy.Resolved, hasOverlay bool) []controlsnapshot.Layer {
	layers := []controlsnapshot.Layer{{
		Name:   "runtime-config",
		Kind:   "base",
		Source: "bootstrap",
	}, {
		Name:   "runtime-profile",
		Kind:   "profile",
		Source: strings.TrimSpace(resolved.RuntimeProfile),
	}, {
		Name:   "edition-defaults",
		Kind:   "edition",
		Source: strings.TrimSpace(edition.Edition),
	}}
	for _, pack := range resolved.Packs {
		layers = append(layers, controlsnapshot.Layer{
			Name:   strings.TrimSpace(pack.ID),
			Kind:   strings.TrimSpace(pack.Kind),
			Source: strings.TrimSpace(pack.Source),
		})
	}
	if hasOverlay {
		layers = append(layers, controlsnapshot.Layer{
			Name:   "policy-overlay",
			Kind:   "overlay",
			Source: "dependency",
		})
	}
	return layers
}

func publishApprovalLifecycleEvent(ctx context.Context, bus eventbus.Bus, eventType eventbus.EventType, ticket *approval.Ticket, remaining time.Duration) {
	if bus == nil || ticket == nil {
		return
	}
	attrs := eventbus.ApprovalEventAttrs{
		ApprovalID:   ticket.ID,
		ApprovalKind: string(ticket.Kind),
		Status:       string(ticket.Status),
		ResolvedBy:   ticket.ResolvedBy,
		Reasons:      append([]string(nil), ticket.Reasons...),
		ExternalRefs: eventbus.ApprovalExternalRefAttrsFromTicketRefs(ticket.External),
	}
	if remaining > 0 {
		attrs.RemainingMs = remaining.Milliseconds()
	}
	extraAttrs := mergeBootstrapEventAttrs(nil, approvalTicketGovernanceAttrs(ticket))
	var event eventbus.Event
	switch eventType {
	case eventbus.EventApprovalRequested:
		event = eventbus.NewApprovalRequestedEvent(strings.TrimSpace(ticket.RunID), strings.TrimSpace(ticket.SessionID), attrs, extraAttrs)
	case eventbus.EventApprovalResolved:
		event = eventbus.NewApprovalResolvedEvent(strings.TrimSpace(ticket.RunID), strings.TrimSpace(ticket.SessionID), attrs, extraAttrs)
	case eventbus.EventApprovalTimedOut:
		event = eventbus.NewApprovalTimedOutEvent(strings.TrimSpace(ticket.RunID), strings.TrimSpace(ticket.SessionID), attrs, extraAttrs)
	case eventbus.EventApprovalGraceWarning:
		event = eventbus.NewApprovalGraceWarningEvent(strings.TrimSpace(ticket.RunID), strings.TrimSpace(ticket.SessionID), attrs, extraAttrs)
	default:
		event = eventbus.Event{
			Type:      eventType,
			RunID:     strings.TrimSpace(ticket.RunID),
			SessionID: strings.TrimSpace(ticket.SessionID),
			Attrs:     mergeBootstrapEventAttrs(attrs.ToMap(), approvalTicketGovernanceAttrs(ticket)),
		}
	}
	logging.LogIfErr(ctx, bus.Publish(ctx, event), "emit approval lifecycle event failed")
}

func approvalTicketGovernanceAttrs(ticket *approval.Ticket) map[string]any {
	if ticket == nil {
		return nil
	}
	return domaingov.MetadataAttrs(ticket.Metadata)
}

func mergeBootstrapEventAttrs(base map[string]any, extras ...map[string]any) map[string]any {
	return domaingov.MergeEventAttrs(base, extras...)
}

func skillRecoveryPrompt(cfg config.SkillsConfig) string {
	policyMode := strings.TrimSpace(cfg.InstallPolicy)
	if policyMode == "" {
		policyMode = config.SkillInstallPolicyAsk
	}
	return strings.TrimSpace(fmt.Sprintf(`## Skill Recovery

IMPORTANT: Always use the tools already available to you FIRST. Do NOT call %q if the tools you need are already in your tool list. For example, if you need to search the web and "search.web" or "search.news" is already available, use it directly.

If the user asks for today's news, hot topics, or a news table/summary, prefer "news.digest" over manually chaining search + fetch + regex + file tools.

Only if the current tools are truly insufficient to complete the user's task:
- Call %q with:
  - %q: the user goal or missing capability
  - %q: the tool or capability names you need
- After %q succeeds, continue the task using the newly loaded tools.
- Runtime install policy is currently %q:
  - %q: the runtime will pause for user approval before installing
  - %q: the runtime may install automatically
  - %q: installation is blocked and you should explain the missing capability clearly`, "skill.ensure", "skill.ensure", "goal", "required_tools", "skill.ensure", policyMode, config.SkillInstallPolicyAsk, config.SkillInstallPolicyAuto, config.SkillInstallPolicyDeny))
}

func enabledOrDefault(v *bool, fallback bool) bool {
	return normalize.BoolOrDefault(v, fallback)
}

func approvalProviderDescriptors(items []config.ApprovalProviderConfig) []controlapproval.ProviderDescriptor {
	if len(items) == 0 {
		return nil
	}
	out := make([]controlapproval.ProviderDescriptor, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		out = append(out, controlapproval.ProviderDescriptor{
			Name:          name,
			Type:          strings.TrimSpace(item.Type),
			Enabled:       enabledOrDefault(item.Enabled, true),
			SubmitEnabled: strings.TrimSpace(item.Webhook.SubmitURL) != "",
			UpdateEnabled: strings.TrimSpace(item.Webhook.UpdateURL) != "",
			SyncEnabled:   strings.TrimSpace(item.Webhook.SyncURL) != "",
			CallbackAuth: controlapproval.CallbackAuthPolicy{
				Mode:            strings.ToLower(strings.TrimSpace(item.CallbackAuth.Mode)),
				HeaderName:      strings.TrimSpace(item.CallbackAuth.HeaderName),
				Token:           strings.TrimSpace(item.CallbackAuth.Token),
				Secret:          strings.TrimSpace(item.CallbackAuth.Secret),
				SignatureHeader: strings.TrimSpace(item.CallbackAuth.SignatureHeader),
				TimestampHeader: strings.TrimSpace(item.CallbackAuth.TimestampHeader),
				MaxAge:          item.CallbackAuth.MaxAge,
			},
			Metadata: cloneAnyMap(item.Metadata),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildConfiguredApprovalProviders(items []config.ApprovalProviderConfig) ([]controlapproval.Provider, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]controlapproval.Provider, 0, len(items))
	for _, item := range items {
		if !enabledOrDefault(item.Enabled, true) {
			continue
		}
		if !approvalProviderUsesWebhook(item) {
			continue
		}
		provider, err := controlapproval.NewWebhookProvider(controlapproval.WebhookProviderConfig{
			Name:      strings.TrimSpace(item.Name),
			SubmitURL: strings.TrimSpace(item.Webhook.SubmitURL),
			UpdateURL: strings.TrimSpace(item.Webhook.UpdateURL),
			SyncURL:   strings.TrimSpace(item.Webhook.SyncURL),
			Headers:   cloneStringMap(item.Webhook.Headers),
			Secret:    strings.TrimSpace(item.Webhook.Secret),
			Timeout:   item.Webhook.Timeout,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, provider)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func buildConfiguredGovernanceAdapters(items []config.GovernanceAdapterConfig) ([]controlgov.Adapter, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]controlgov.Adapter, 0, len(items))
	for _, item := range items {
		if !enabledOrDefault(item.Enabled, true) {
			continue
		}
		if !governanceAdapterUsesWebhook(item) {
			continue
		}
		adapter, err := controlgov.NewWebhookAdapter(controlgov.WebhookAdapterConfig{
			Name:            strings.TrimSpace(item.Name),
			URL:             strings.TrimSpace(item.Webhook.URL),
			Headers:         cloneStringMap(item.Webhook.Headers),
			Secret:          strings.TrimSpace(item.Webhook.Secret),
			Timeout:         item.Webhook.Timeout,
			IncludeSnapshot: enabledOrDefault(item.Webhook.IncludeSnapshot, true),
			Kinds:           governanceAdapterKinds(item.Webhook.Kinds),
			Metadata:        cloneAnyMap(item.Metadata),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, adapter)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func buildConfiguredAuditSinks(items []config.AuditSinkConfig) ([]audit.DeliverySink, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]audit.DeliverySink, 0, len(items))
	for _, item := range items {
		if !enabledOrDefault(item.Enabled, true) {
			continue
		}
		switch auditSinkType(item) {
		case "webhook":
			sink, err := audit.NewWebhookRecorder(audit.WebhookRecorderConfig{
				Name:    strings.TrimSpace(item.Name),
				URL:     strings.TrimSpace(item.Webhook.URL),
				Timeout: item.Webhook.Timeout,
				Headers: cloneStringMap(item.Webhook.Headers),
				Secret:  strings.TrimSpace(item.Webhook.Secret),
			})
			if err != nil {
				return nil, err
			}
			out = append(out, sink)
		case "elasticsearch":
			sink, err := audit.NewElasticsearchRecorder(audit.ElasticsearchRecorderConfig{
				Name:    strings.TrimSpace(item.Name),
				URL:     strings.TrimSpace(item.Elasticsearch.URL),
				Index:   strings.TrimSpace(item.Elasticsearch.Index),
				Timeout: item.Elasticsearch.Timeout,
				Headers: cloneStringMap(item.Elasticsearch.Headers),
				APIKey:  strings.TrimSpace(item.Elasticsearch.APIKey),
			})
			if err != nil {
				return nil, err
			}
			out = append(out, sink)
		case "splunk_hec":
			sink, err := audit.NewSplunkHECRecorder(audit.SplunkHECRecorderConfig{
				Name:       strings.TrimSpace(item.Name),
				URL:        strings.TrimSpace(item.SplunkHEC.URL),
				Token:      strings.TrimSpace(item.SplunkHEC.Token),
				Timeout:    item.SplunkHEC.Timeout,
				Headers:    cloneStringMap(item.SplunkHEC.Headers),
				Source:     strings.TrimSpace(item.SplunkHEC.Source),
				SourceType: strings.TrimSpace(item.SplunkHEC.SourceType),
				Index:      strings.TrimSpace(item.SplunkHEC.Index),
				Host:       strings.TrimSpace(item.SplunkHEC.Host),
			})
			if err != nil {
				return nil, err
			}
			out = append(out, sink)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func approvalProviderUsesWebhook(item config.ApprovalProviderConfig) bool {
	if strings.EqualFold(strings.TrimSpace(item.Type), "webhook") {
		return true
	}
	return strings.TrimSpace(item.Webhook.SubmitURL) != "" ||
		strings.TrimSpace(item.Webhook.UpdateURL) != "" ||
		strings.TrimSpace(item.Webhook.SyncURL) != ""
}

func governanceAdapterUsesWebhook(item config.GovernanceAdapterConfig) bool {
	if strings.EqualFold(strings.TrimSpace(item.Type), "webhook") {
		return true
	}
	return strings.TrimSpace(item.Webhook.URL) != ""
}

func auditSinkType(item config.AuditSinkConfig) string {
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "webhook", "elasticsearch", "splunk_hec":
		return strings.ToLower(strings.TrimSpace(item.Type))
	}
	switch {
	case strings.TrimSpace(item.Webhook.URL) != "":
		return "webhook"
	case strings.TrimSpace(item.Elasticsearch.URL) != "" || strings.TrimSpace(item.Elasticsearch.Index) != "":
		return "elasticsearch"
	case strings.TrimSpace(item.SplunkHEC.URL) != "" || strings.TrimSpace(item.SplunkHEC.Token) != "":
		return "splunk_hec"
	default:
		return ""
	}
}

func governanceAdapterKinds(items []string) []controlgov.Kind {
	if len(items) == 0 {
		return nil
	}
	out := make([]controlgov.Kind, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, controlgov.Kind(strings.ToLower(trimmed)))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func governanceAdapterDescriptors(items []config.GovernanceAdapterConfig) []controlgov.AdapterDescriptor {
	if len(items) == 0 {
		return nil
	}
	out := make([]controlgov.AdapterDescriptor, 0, len(items))
	for _, item := range items {
		out = append(out, controlgov.AdapterDescriptor{
			Name:            strings.TrimSpace(item.Name),
			Type:            normalize.FirstNonEmpty(strings.TrimSpace(item.Type), governanceAdapterType(item)),
			Enabled:         enabledOrDefault(item.Enabled, true),
			IncludeSnapshot: enabledOrDefault(item.Webhook.IncludeSnapshot, true),
			Kinds:           governanceAdapterKinds(item.Webhook.Kinds),
			Metadata:        cloneAnyMap(item.Metadata),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func governanceAdapterType(item config.GovernanceAdapterConfig) string {
	if governanceAdapterUsesWebhook(item) {
		return "webhook"
	}
	return "custom"
}

func auditSinkDescriptors(cfg config.Config) []controlaudit.SinkDescriptor {
	if !cfg.Runtime.Audit.Enabled {
		return nil
	}
	out := make([]controlaudit.SinkDescriptor, 0, len(cfg.Runtime.Audit.Sinks)+1)
	if path := strings.TrimSpace(cfg.Runtime.Audit.Output); path != "" {
		out = append(out, controlaudit.SinkDescriptor{
			Name:    "jsonl",
			Type:    "jsonl",
			Enabled: true,
			Target:  path,
		})
	}
	for _, item := range cfg.Runtime.Audit.Sinks {
		sinkType := auditSinkType(item)
		if sinkType == "" {
			continue
		}
		metadata := cloneAnyMap(item.Metadata)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["delivery_backend"] = resolvedAuditDeliveryBackend(cfg)
		if sinkType == "elasticsearch" {
			metadata["index"] = strings.TrimSpace(item.Elasticsearch.Index)
		}
		if sinkType == "splunk_hec" {
			if source := strings.TrimSpace(item.SplunkHEC.Source); source != "" {
				metadata["source"] = source
			}
			if index := strings.TrimSpace(item.SplunkHEC.Index); index != "" {
				metadata["index"] = index
			}
		}
		out = append(out, controlaudit.SinkDescriptor{
			Name:     strings.TrimSpace(item.Name),
			Type:     sinkType,
			Enabled:  enabledOrDefault(item.Enabled, true),
			Target:   auditSinkTarget(item, sinkType),
			Metadata: metadata,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func enabledAuditSinkNames(cfg config.Config) []string {
	if !cfg.Runtime.Audit.Enabled {
		return nil
	}
	out := make([]string, 0, len(cfg.Runtime.Audit.Sinks)+1)
	if strings.TrimSpace(cfg.Runtime.Audit.Output) != "" {
		out = append(out, "jsonl")
	}
	for _, item := range cfg.Runtime.Audit.Sinks {
		if !enabledOrDefault(item.Enabled, true) || auditSinkType(item) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(item.Name))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolvedAuditDeliveryBackend(cfg config.Config) string {
	backend := normalizeBootstrapAuditDeliveryBackend(cfg.Runtime.Audit.Delivery.Backend)
	if backend != "" {
		return backend
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "sqlite") {
		return "sqlite"
	}
	return "memory"
}

func normalizeBootstrapAuditDeliveryBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return ""
	case "memory":
		return "memory"
	case "sqlite":
		return "sqlite"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func auditSinkTarget(item config.AuditSinkConfig, sinkType string) string {
	switch sinkType {
	case "webhook":
		return strings.TrimSpace(item.Webhook.URL)
	case "elasticsearch":
		base := strings.TrimRight(strings.TrimSpace(item.Elasticsearch.URL), "/")
		index := strings.TrimSpace(item.Elasticsearch.Index)
		if base == "" || index == "" {
			return base
		}
		return base + "/" + index
	case "splunk_hec":
		return strings.TrimSpace(item.SplunkHEC.URL)
	default:
		return ""
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
