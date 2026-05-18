package toolruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	automationsetup "github.com/fulcrus/hopclaw/automation/setup"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func destinationToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "destination.inventory",
				Description:     "List configured delivery destinations and setup-ready options for automation workflows.",
				InputSchema:     destinationInventoryInputSchema(),
				OutputSchema:    destinationInventoryOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "destination:inventory",
			},
			Handler: handleDestinationInventory,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "destination.probe",
				Description:     "Validate whether a delivery destination is configured and ready for use.",
				InputSchema:     destinationProbeInputSchema(),
				OutputSchema:    destinationProbeOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "destination:probe:{provider}:{target}",
			},
			Handler: handleDestinationProbe,
		},
	}
}

func destinationInventoryInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"kind":            stringSchema("Optional destination kind filter: channel or email."),
		"query":           stringSchema("Optional search query for labels, providers, and targets."),
		"current_channel": stringSchema("Optional current inbound channel name."),
		"current_target":  stringSchema("Optional current inbound target ID."),
		"current_account": stringSchema("Optional current inbound account ID for multi-account channels."),
		"include_probe":   booleanSchema("Whether to include lightweight readiness probes for each item."),
	})
}

func destinationProbeInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"kind":        stringSchema("Destination kind: channel or email."),
		"provider":    stringSchema("Destination provider name such as feishu, slack, telegram, or smtp."),
		"channel":     stringSchema("Channel adapter name when probing channel delivery."),
		"account_id":  stringSchema("Optional account ID for multi-account channel providers."),
		"target_type": stringSchema("Optional target type, for example chat_id or email."),
		"target":      stringSchema("Destination target identifier such as a chat/channel ID or email address."),
		"label":       stringSchema("Optional display label."),
	}, "kind")
}

func destinationSpecSchema() map[string]any {
	return objectSchema(map[string]any{
		"kind":        stringSchema("Destination kind."),
		"provider":    stringSchema("Destination provider."),
		"channel":     stringSchema("Channel adapter name."),
		"account_id":  stringSchema("Optional provider account ID."),
		"target_type": stringSchema("Target type."),
		"target":      stringSchema("Destination target."),
		"label":       stringSchema("Display label."),
		"metadata":    objectSchema(map[string]any{}),
	}, "kind", "provider")
}

func destinationProbeSchema() map[string]any {
	return objectSchema(map[string]any{
		"slot_id":    stringSchema("Associated setup slot ID."),
		"kind":       stringSchema("Destination kind."),
		"provider":   stringSchema("Destination provider."),
		"status":     stringSchema("Probe status: ready, needs_target, not_connected, not_configured, unsupported, or invalid."),
		"reachable":  booleanSchema("Whether the destination is currently usable."),
		"code":       stringSchema("Short machine-readable probe code."),
		"message":    stringSchema("Human-readable probe summary."),
		"checked_at": stringSchema("Probe time in RFC3339 format."),
		"metadata":   objectSchema(map[string]any{}),
	}, "kind", "provider", "status", "reachable")
}

func destinationInventoryOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":           stringSchema("Inventory item ID."),
		"kind":         stringSchema("Destination kind."),
		"provider":     stringSchema("Destination provider."),
		"label":        stringSchema("Display label."),
		"summary":      stringSchema("Short operational summary."),
		"status":       stringSchema("Current item readiness status."),
		"capabilities": stringArraySchema("Destination capabilities."),
		"spec":         destinationSpecSchema(),
		"probe":        destinationProbeSchema(),
		"metadata":     objectSchema(map[string]any{}),
	}, "id", "kind", "provider", "label", "status", "spec")
	return objectSchema(map[string]any{
		"items": arraySchema(entry, "Available destination choices."),
		"count": integerSchema("Number of returned destination items."),
	}, "items", "count")
}

func destinationProbeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"probe": destinationProbeSchema(),
		"spec":  destinationSpecSchema(),
	}, "probe", "spec")
}

func handleDestinationInventory(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	items, err := destinationInventoryItems(b, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("destination.inventory: %w", err)
	}
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, destinationInventoryJSON(item))
	}
	return b.jsonResult(call, map[string]any{
		"items": payload,
		"count": len(payload),
	})
}

func handleDestinationProbe(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	spec := destinationSpecFromInput(call.Input)
	probe := probeDestination(b, spec)
	return b.jsonResult(call, map[string]any{
		"probe": destinationProbeJSON(probe),
		"spec":  destinationSpecJSON(spec),
	})
}

func destinationInventoryItems(b *Builtins, input map[string]any) ([]automationsetup.InventoryItem, error) {
	kind := strings.TrimSpace(strings.ToLower(optionalString(input, "kind")))
	query := strings.TrimSpace(strings.ToLower(optionalString(input, "query")))
	currentChannel := strings.TrimSpace(strings.ToLower(optionalString(input, "current_channel")))
	currentTarget := optionalString(input, "current_target")
	currentAccount := optionalString(input, "current_account")
	includeProbe, err := boolFromDefault(input["include_probe"], true)
	if err != nil {
		return nil, err
	}

	items := make([]automationsetup.InventoryItem, 0, 8)
	if kind == "" || kind == "channel" {
		items = append(items, channelDestinationItems(b, currentChannel, currentTarget, currentAccount, includeProbe)...)
	}
	if kind == "" || kind == "email" {
		items = append(items, emailDestinationItems(b, includeProbe)...)
	}
	if query != "" {
		filtered := make([]automationsetup.InventoryItem, 0, len(items))
		for _, item := range items {
			if destinationInventoryMatches(item, query) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	sort.SliceStable(items, func(i, j int) bool {
		scoreI := destinationInventoryRank(items[i])
		scoreJ := destinationInventoryRank(items[j])
		switch {
		case scoreI != scoreJ:
			return scoreI < scoreJ
		case items[i].Provider != items[j].Provider:
			return items[i].Provider < items[j].Provider
		default:
			return items[i].Label < items[j].Label
		}
	})
	return items, nil
}

func channelDestinationItems(b *Builtins, currentChannel, currentTarget, currentAccount string, includeProbe bool) []automationsetup.InventoryItem {
	if b == nil || b.channelManager == nil {
		return nil
	}
	names := b.channelManager.Names()
	sort.Strings(names)

	items := make([]automationsetup.InventoryItem, 0, len(names)+1)
	for _, name := range names {
		adapter, ok := b.channelManager.Get(name)
		if !ok {
			continue
		}
		if strings.EqualFold(name, currentChannel) && strings.TrimSpace(currentTarget) != "" {
			spec := automationsetup.DestinationSpec{
				Kind:       "channel",
				Provider:   name,
				Channel:    name,
				AccountID:  currentAccount,
				TargetType: destinationTargetType(name),
				Target:     currentTarget,
				Label:      fmt.Sprintf("Current %s conversation", name),
			}.Normalized()
			item := automationsetup.InventoryItem{
				ID:           "channel:" + name + ":current",
				Kind:         "channel",
				Provider:     name,
				Label:        spec.Label,
				Summary:      fmt.Sprintf("Reply back to the current %s destination %s.", name, currentTarget),
				Capabilities: destinationCapabilities(adapter.Capabilities()),
				Spec:         spec,
				Metadata: map[string]any{
					"current": true,
				},
			}.Normalized()
			if includeProbe {
				probe := probeDestination(b, spec)
				item.Probe = &probe
				item.Status = string(probe.Status)
			}
			items = append(items, item)
		}

		accounts := b.destinations.ChannelAccounts[name]
		if len(accounts) == 0 {
			spec := automationsetup.DestinationSpec{
				Kind:       "channel",
				Provider:   name,
				Channel:    name,
				TargetType: destinationTargetType(name),
				Label:      destinationProviderLabel(name),
			}.Normalized()
			item := automationsetup.InventoryItem{
				ID:           "channel:" + name,
				Kind:         "channel",
				Provider:     name,
				Label:        spec.Label,
				Summary:      fmt.Sprintf("%s is registered. Provide %s to deliver notifications.", name, spec.TargetType),
				Capabilities: destinationCapabilities(adapter.Capabilities()),
				Spec:         spec,
			}.Normalized()
			if includeProbe {
				probe := probeDestination(b, spec)
				item.Probe = &probe
				item.Status = string(probe.Status)
			}
			items = append(items, item)
			continue
		}

		for _, account := range accounts {
			spec := automationsetup.DestinationSpec{
				Kind:       "channel",
				Provider:   name,
				Channel:    name,
				AccountID:  account.ID,
				TargetType: destinationTargetType(name),
				Label:      account.Label,
			}.Normalized()
			item := automationsetup.InventoryItem{
				ID:           fmt.Sprintf("channel:%s:%s", name, account.ID),
				Kind:         "channel",
				Provider:     name,
				Label:        destinationAccountLabel(name, account),
				Summary:      destinationAccountSummary(name, account, spec.TargetType),
				Capabilities: destinationCapabilities(adapter.Capabilities()),
				Spec:         spec,
				Metadata: map[string]any{
					"default": account.Default,
				},
			}.Normalized()
			if includeProbe {
				probe := probeDestination(b, spec)
				item.Probe = &probe
				item.Status = string(probe.Status)
			}
			items = append(items, item)
		}
	}
	return items
}

func emailDestinationItems(b *Builtins, includeProbe bool) []automationsetup.InventoryItem {
	if b == nil {
		return nil
	}
	svc := b.config.Services.Email
	sender := firstNonEmpty(strings.TrimSpace(svc.From), strings.TrimSpace(svc.Username))
	spec := automationsetup.DestinationSpec{
		Kind:       "email",
		Provider:   "smtp",
		TargetType: "email",
		Label:      firstNonEmpty(sender, "SMTP delivery"),
	}.Normalized()

	status := automationsetup.ProbeStatusNotConfigured
	summary := "SMTP is not configured. Fill tools.services.email to enable email delivery."
	if svc.HasSMTP() {
		status = automationsetup.ProbeStatusNeedsTarget
		summary = fmt.Sprintf("SMTP is configured via %s:%d. Provide the recipient email address to deliver notifications.", svc.SMTPHost, emailSMTPPort(svc))
	}

	item := automationsetup.InventoryItem{
		ID:       "email:smtp",
		Kind:     "email",
		Provider: "smtp",
		Label:    spec.Label,
		Summary:  summary,
		Status:   string(status),
		Spec:     spec,
	}.Normalized()
	if includeProbe {
		probe := probeDestination(b, spec)
		item.Probe = &probe
		item.Status = string(probe.Status)
	}
	return []automationsetup.InventoryItem{item}
}

func destinationInventoryMatches(item automationsetup.InventoryItem, query string) bool {
	item = item.Normalized()
	haystack := strings.ToLower(strings.Join([]string{
		item.ID,
		item.Kind,
		item.Provider,
		item.Label,
		item.Summary,
		item.Spec.AccountID,
		item.Spec.Target,
		item.Spec.TargetType,
	}, " "))
	if strings.Contains(haystack, query) {
		return true
	}
	for _, token := range strings.Fields(query) {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func destinationInventoryRank(item automationsetup.InventoryItem) int {
	switch strings.TrimSpace(item.Status) {
	case string(automationsetup.ProbeStatusReady):
		return 0
	case string(automationsetup.ProbeStatusNeedsTarget):
		return 1
	case string(automationsetup.ProbeStatusNotConnected):
		return 2
	case string(automationsetup.ProbeStatusNotConfigured):
		return 3
	default:
		return 4
	}
}

func destinationCapabilities(caps channels.Capabilities) []string {
	out := make([]string, 0, 3)
	if caps.SendText {
		out = append(out, "send_text")
	}
	if caps.SendRichText {
		out = append(out, "send_rich_text")
	}
	if caps.SendFile {
		out = append(out, "send_file")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func destinationTargetType(provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "feishu", "telegram":
		return "chat_id"
	case "slack", "discord":
		return "channel_id"
	default:
		return "target_id"
	}
}

func destinationProviderLabel(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return ""
	}
	return strings.ToUpper(provider[:1]) + provider[1:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func destinationSpecFromInput(input map[string]any) automationsetup.DestinationSpec {
	return automationsetup.DestinationSpec{
		Kind:       optionalString(input, "kind"),
		Provider:   firstNonEmpty(optionalString(input, "provider"), optionalString(input, "channel")),
		Channel:    firstNonEmpty(optionalString(input, "channel"), optionalString(input, "provider")),
		AccountID:  optionalString(input, "account_id"),
		TargetType: optionalString(input, "target_type"),
		Target:     optionalString(input, "target"),
		Label:      optionalString(input, "label"),
	}.Normalized()
}

func probeDestination(b *Builtins, spec automationsetup.DestinationSpec) automationsetup.ProbeResult {
	spec = spec.Normalized()
	switch spec.Kind {
	case "channel":
		return probeChannelDestination(b, spec)
	case "email":
		return probeEmailDestination(b, spec)
	default:
		return automationsetup.ProbeResult{
			Kind:      spec.Kind,
			Provider:  spec.Provider,
			Status:    automationsetup.ProbeStatusUnsupported,
			Reachable: false,
			Code:      "unsupported_kind",
			Message:   "Unsupported destination kind.",
			CheckedAt: time.Now().UTC(),
		}
	}
}

func probeChannelDestination(b *Builtins, spec automationsetup.DestinationSpec) automationsetup.ProbeResult {
	probe := automationsetup.ProbeResult{
		Kind:      "channel",
		Provider:  firstNonEmpty(spec.Provider, spec.Channel),
		CheckedAt: time.Now().UTC(),
	}
	if b == nil || b.channelManager == nil {
		probe.Status = automationsetup.ProbeStatusNotConfigured
		probe.Code = "channel_manager_unavailable"
		probe.Message = "Channel manager is not available."
		return probe
	}
	channelName := firstNonEmpty(spec.Channel, spec.Provider)
	adapter, ok := b.channelManager.Get(channelName)
	if !ok {
		probe.Status = automationsetup.ProbeStatusNotConfigured
		probe.Code = "channel_not_registered"
		probe.Message = fmt.Sprintf("Channel %s is not registered.", channelName)
		return probe
	}
	caps := adapter.Capabilities()
	if !caps.SendText && !caps.SendRichText {
		probe.Status = automationsetup.ProbeStatusUnsupported
		probe.Code = "channel_not_sendable"
		probe.Message = fmt.Sprintf("Channel %s does not support outbound messaging.", channelName)
		return probe
	}
	if strings.TrimSpace(spec.Target) == "" {
		probe.Status = automationsetup.ProbeStatusNeedsTarget
		probe.Code = "missing_target"
		probe.Message = fmt.Sprintf("Channel %s is available. Provide %s to finish setup.", channelName, destinationTargetType(channelName))
		return probe
	}
	if adapter.Status() != channels.StatusConnected {
		probe.Status = automationsetup.ProbeStatusNotConnected
		probe.Code = "channel_not_connected"
		probe.Message = fmt.Sprintf("Channel %s is registered but currently %s.", channelName, adapter.Status())
		return probe
	}
	probe.Status = automationsetup.ProbeStatusReady
	probe.Reachable = true
	probe.Code = "channel_connected"
	if strings.TrimSpace(spec.AccountID) != "" {
		probe.Message = fmt.Sprintf("Channel %s account %s is connected. Target %s is set; no live send-test was performed.", channelName, spec.AccountID, spec.Target)
	} else {
		probe.Message = fmt.Sprintf("Channel %s is connected. Target %s is set; no live send-test was performed.", channelName, spec.Target)
	}
	return probe
}

func probeEmailDestination(b *Builtins, spec automationsetup.DestinationSpec) automationsetup.ProbeResult {
	probe := automationsetup.ProbeResult{
		Kind:      "email",
		Provider:  "smtp",
		CheckedAt: time.Now().UTC(),
	}
	if b == nil || !b.config.Services.Email.HasSMTP() {
		probe.Status = automationsetup.ProbeStatusNotConfigured
		probe.Code = "smtp_not_configured"
		probe.Message = "SMTP is not configured."
		return probe
	}
	if strings.TrimSpace(spec.Target) == "" {
		probe.Status = automationsetup.ProbeStatusNeedsTarget
		probe.Code = "missing_target"
		probe.Message = "SMTP is configured. Provide the recipient email address to finish setup."
		return probe
	}
	if !strings.Contains(spec.Target, "@") {
		probe.Status = automationsetup.ProbeStatusInvalid
		probe.Code = "invalid_email"
		probe.Message = "The email recipient format looks invalid."
		return probe
	}
	sender := firstNonEmpty(strings.TrimSpace(b.config.Services.Email.From), strings.TrimSpace(b.config.Services.Email.Username))
	probe.Status = automationsetup.ProbeStatusReady
	probe.Reachable = true
	probe.Code = "smtp_configured"
	probe.Message = fmt.Sprintf("SMTP is configured as %s. Recipient %s is set; no live send-test was performed.", firstNonEmpty(sender, "configured sender"), spec.Target)
	return probe
}

func emailSMTPPort(cfg EmailServiceConfig) int {
	if cfg.SMTPPort > 0 {
		return cfg.SMTPPort
	}
	return 587
}

func destinationAccountLabel(provider string, account DestinationAccount) string {
	label := strings.TrimSpace(account.Label)
	if label == "" {
		label = account.ID
	}
	if account.Default {
		return fmt.Sprintf("%s (%s default)", label, provider)
	}
	return fmt.Sprintf("%s (%s)", label, provider)
}

func destinationAccountSummary(provider string, account DestinationAccount, targetType string) string {
	if strings.TrimSpace(account.Description) != "" {
		return fmt.Sprintf("%s account via %s. Provide %s to deliver notifications.", provider, account.Description, targetType)
	}
	return fmt.Sprintf("%s account is configured. Provide %s to deliver notifications.", provider, targetType)
}

func destinationInventoryJSON(item automationsetup.InventoryItem) map[string]any {
	item = item.Normalized()
	out := map[string]any{
		"id":           item.ID,
		"kind":         item.Kind,
		"provider":     item.Provider,
		"label":        item.Label,
		"summary":      item.Summary,
		"status":       item.Status,
		"capabilities": nonNilStringSlice(item.Capabilities),
		"spec":         destinationSpecJSON(item.Spec),
	}
	if item.Probe != nil {
		out["probe"] = destinationProbeJSON(*item.Probe)
	}
	if len(item.Metadata) > 0 {
		out["metadata"] = item.Metadata
	}
	return out
}

func destinationSpecJSON(spec automationsetup.DestinationSpec) map[string]any {
	spec = spec.Normalized()
	out := map[string]any{
		"kind":        spec.Kind,
		"provider":    spec.Provider,
		"channel":     spec.Channel,
		"account_id":  spec.AccountID,
		"target_type": spec.TargetType,
		"target":      spec.Target,
		"label":       spec.Label,
	}
	if len(spec.Metadata) > 0 {
		out["metadata"] = spec.Metadata
	}
	return out
}

func destinationProbeJSON(probe automationsetup.ProbeResult) map[string]any {
	probe = probe.Normalized()
	out := map[string]any{
		"slot_id":    probe.SlotID,
		"kind":       probe.Kind,
		"provider":   probe.Provider,
		"status":     string(probe.Status),
		"reachable":  probe.Reachable,
		"code":       probe.Code,
		"message":    probe.Message,
		"checked_at": probe.CheckedAt.Format(time.RFC3339),
	}
	if len(probe.Metadata) > 0 {
		out["metadata"] = probe.Metadata
	}
	return out
}

func nonNilStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
