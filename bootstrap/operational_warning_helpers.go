package bootstrap

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/approval"
)

const (
	configStoreWarningComponent   = "config_store"
	channelHealthWarningComponent = "channel_health_monitor"
	approvalTimeoutSweepComponent = "approval_timeout/sweep"
)

func recordConfigStoreFallbackWarning(warnings *startupWarningCollector, err error) {
	if warnings == nil || err == nil {
		return
	}
	warnings.AddDetailed(
		configStoreWarningComponent,
		"Dynamic config store unavailable; using YAML-only mode",
		err.Error(),
		"restore control DB access and restart to re-enable persisted config changes",
	)
}

func channelConnectWarningComponent(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "channel"
	}
	return "channel/" + name
}

func recordChannelConnectWarning(warnings *startupWarningCollector, name string, err error) {
	if warnings == nil || err == nil {
		return
	}
	name = strings.TrimSpace(name)
	warnings.AddDetailed(
		channelConnectWarningComponent(name),
		fmt.Sprintf("Channel %q failed to connect", name),
		err.Error(),
		"inspect channel credentials/network and reconnect the integration",
	)
}

func clearChannelConnectWarning(warnings *startupWarningCollector, name string) {
	if warnings == nil {
		return
	}
	warnings.Clear(channelConnectWarningComponent(name))
}

func recordChannelHealthMonitorWarning(warnings *startupWarningCollector, err error) {
	if warnings == nil || err == nil {
		return
	}
	warnings.AddDetailed(
		channelHealthWarningComponent,
		"Channel health monitor unavailable",
		err.Error(),
		"restart the gateway after fixing channel monitoring dependencies",
	)
}

func approvalTimeoutResolveWarningComponent(ticketID string) string {
	ticketID = strings.TrimSpace(ticketID)
	if ticketID == "" {
		return "approval_timeout"
	}
	return "approval_timeout/" + ticketID
}

func approvalTimeoutRunWarningComponent(runID string) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return "approval_timeout/run"
	}
	return "approval_timeout/run/" + runID
}

func recordApprovalTimeoutSweepWarning(warnings *startupWarningCollector, err error) {
	if warnings == nil || err == nil {
		return
	}
	warnings.AddDetailed(
		approvalTimeoutSweepComponent,
		"Approval timeout monitor cannot scan pending tickets",
		err.Error(),
		"inspect approval store/database health so timed-out requests can be cancelled automatically",
	)
}

func clearApprovalTimeoutSweepWarning(warnings *startupWarningCollector) {
	if warnings == nil {
		return
	}
	warnings.Clear(approvalTimeoutSweepComponent)
}

func recordApprovalTimeoutResolveWarning(warnings *startupWarningCollector, ticket *approval.Ticket, attempts int, err error) {
	if warnings == nil || err == nil {
		return
	}
	ticketID := ""
	runID := ""
	if ticket != nil {
		ticketID = strings.TrimSpace(ticket.ID)
		runID = strings.TrimSpace(ticket.RunID)
	}
	detail := strings.TrimSpace(err.Error())
	if attempts > 0 {
		detail = fmt.Sprintf("attempt %d failed: %s", attempts, detail)
	}
	if runID != "" {
		detail = fmt.Sprintf("%s (run=%s)", detail, runID)
	}
	summary := "Timed-out approval could not be auto-cancelled"
	if ticketID != "" {
		summary = fmt.Sprintf("Timed-out approval %q could not be auto-cancelled", ticketID)
	}
	warnings.AddDetailed(
		approvalTimeoutResolveWarningComponent(ticketID),
		summary,
		detail,
		"inspect approval/runtime storage health and manually cancel the affected run if needed",
	)
}

func clearApprovalTimeoutResolveWarning(warnings *startupWarningCollector, ticketID string) {
	if warnings == nil {
		return
	}
	warnings.Clear(approvalTimeoutResolveWarningComponent(ticketID))
}

func recordApprovalTimeoutRunWarning(warnings *startupWarningCollector, runID string, err error) {
	if warnings == nil || err == nil {
		return
	}
	runID = strings.TrimSpace(runID)
	summary := "Approval timeout cleanup left run state uncertain"
	if runID != "" {
		summary = fmt.Sprintf("Run %q may still be stuck after approval timeout fallback", runID)
	}
	warnings.AddDetailed(
		approvalTimeoutRunWarningComponent(runID),
		summary,
		err.Error(),
		"inspect the affected run and cancel it manually if it remains waiting for approval",
	)
}

func clearApprovalTimeoutRunWarning(warnings *startupWarningCollector, runID string) {
	if warnings == nil {
		return
	}
	warnings.Clear(approvalTimeoutRunWarningComponent(runID))
}
