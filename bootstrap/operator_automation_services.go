package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/automation"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/heartbeat"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/toolruntime"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/fulcrus/hopclaw/wire"
)

type automationGatewayTarget interface {
	ApplyAutomationServices(gateway.AutomationServices)
}

type preparedOperatorAutomationPack struct {
	cronService      *cronsvc.Service
	watchService     *watch.Service
	heartbeatService *heartbeat.Service
	wireLogger       *wire.Logger
	wakeupService    *wakeup.Service
	deliverer        *channelCronDeliverer
}

func (a *App) automationPackForState() *preparedOperatorAutomationPack {
	if a == nil {
		return nil
	}
	services := a.automationServices()
	return &preparedOperatorAutomationPack{
		cronService:      services.cron,
		watchService:     services.watch,
		heartbeatService: services.heartbeat,
		wireLogger:       services.wire,
		wakeupService:    services.wakeup,
		deliverer:        a.automationDeliverer,
	}
}

func (p *preparedOperatorAutomationPack) packID() string {
	if p == nil {
		return ""
	}
	return builtinBindingPackAutomation
}

func (p *preparedOperatorAutomationPack) moduleExposed() bool {
	return p != nil && (p.cronService != nil || p.watchService != nil || p.heartbeatService != nil || p.wireLogger != nil || p.wakeupService != nil)
}

func (p *preparedOperatorAutomationPack) module() modules.StaticModule {
	details := map[string]any{
		"cron_service":      p != nil && p.cronService != nil,
		"watch_service":     p != nil && p.watchService != nil,
		"heartbeat_service": p != nil && p.heartbeatService != nil,
		"wire_logger":       p != nil && p.wireLogger != nil,
		"wakeup_service":    p != nil && p.wakeupService != nil,
	}

	active := 0
	for _, enabled := range []bool{
		p != nil && p.cronService != nil,
		p != nil && p.watchService != nil,
		p != nil && p.heartbeatService != nil,
		p != nil && p.wireLogger != nil,
		p != nil && p.wakeupService != nil,
	} {
		if enabled {
			active++
		}
	}

	health := modules.HealthReport{
		Status:  modules.HealthReady,
		Summary: fmt.Sprintf("%d automation services wired.", active),
		Details: details,
	}
	if active == 0 {
		health.Status = modules.HealthDegraded
		health.Summary = "No automation services are wired."
	}

	return staticFirstPartyPackModule(
		builtinBindingPackAutomation,
		"automation-pack",
		"First-party automation services pack for cron, watch, wakeup, heartbeat, and wire diagnostics.",
		health,
	)
}

func (p *preparedOperatorAutomationPack) applySurface(surface *preparedBootstrapSurface) {
	if p == nil || surface == nil {
		return
	}
	surface.cronService = p.cronService
	surface.watchService = p.watchService
	surface.heartbeatService = p.heartbeatService
	surface.wireLogger = p.wireLogger
	surface.wakeupService = p.wakeupService
	surface.automationDeliverer = p.deliverer
}

func (p *preparedOperatorAutomationPack) applyGateway(gw *gateway.Gateway) {
	if gw == nil {
		return
	}
	gw.ApplyAutomationServices(gateway.AutomationServices{
		Cron:      p.cronService,
		Watch:     p.watchService,
		Heartbeat: p.heartbeatService,
		Wire:      p.wireLogger,
		Wakeup:    p.wakeupService,
	})
}

func (p *preparedOperatorAutomationPack) applyAutomationGateway(target automationGatewayTarget) {
	if p == nil || target == nil {
		return
	}
	target.ApplyAutomationServices(gateway.AutomationServices{
		Cron:      p.cronService,
		Watch:     p.watchService,
		Heartbeat: p.heartbeatService,
		Wire:      p.wireLogger,
		Wakeup:    p.wakeupService,
	})
}

func (p *preparedOperatorAutomationPack) applyBuiltins(bindings *toolruntime.BuiltinsBindings) {
	if p == nil || bindings == nil {
		return
	}
	bindings.CronService = p.cronService
	bindings.WatchService = p.watchService
	bindings.WakeupService = p.wakeupService
}

func prepareOperatorAutomationPack(
	ctx context.Context,
	cfg config.Config,
	foundation *preparedBootstrapFoundation,
	runtimeCore *preparedBootstrapRuntimeCore,
	channelManager *channelmgr.Manager,
) *preparedOperatorAutomationPack {
	submitter := &runtimeAutomationSubmitter{runtime: runtimeCore.runtime}
	deliverer := channelDelivererFor(channelManager)

	var cronService *cronsvc.Service
	if cronEnabled(cfg.Cron) {
		cronStore, cronErr := initCronStore(cfg.Cron)
		if cronErr != nil {
			foundation.startupWarnings.Add("cron_store", cronErr)
			log.Warn("cron store init failed", "error", cronErr)
		} else {
			cronService = cronsvc.NewService(
				cronStore,
				submitter,
				deliverer,
				cronsvc.WithExecutionTimeout(cfg.Cron.ExecutionTimeout),
			)
			if err := cronService.Start(ctx); err != nil {
				foundation.startupWarnings.Add("cron_service", err)
				log.Warn("cron service start failed", "error", err)
			}
		}
	}

	var watchService *watch.Service
	if watchEnabled(cfg.Watch) {
		watchStore, watchErr := initWatchStore(cfg.Watch)
		if watchErr != nil {
			foundation.startupWarnings.Add("watch_store", watchErr)
			log.Warn("watch store init failed", "error", watchErr)
		} else {
			watchService = watch.NewService(watchStore, submitter,
				watch.WithExecutionTimeout(cfg.Watch.ExecutionTimeout),
				watch.WithEmailConfig(watch.EmailConfig{
					IMAPHost: strings.TrimSpace(cfg.Tools.Services.Email.IMAPHost),
					IMAPPort: cfg.Tools.Services.Email.IMAPPort,
					Username: strings.TrimSpace(cfg.Tools.Services.Email.Username),
					Password: cfg.Tools.Services.Email.Password,
				}),
				watch.WithCalendarConfig(watch.CalendarConfig{
					CalDAVURL: strings.TrimSpace(cfg.Tools.Services.Calendar.CalDAVURL),
					Username:  strings.TrimSpace(cfg.Tools.Services.Calendar.Username),
					Password:  cfg.Tools.Services.Calendar.Password,
				}),
				watch.WithSessionInboxReader(sessionInboxReader(foundation.sessions)),
				watch.WithBrowserClient(runtimeCore.browserClient),
				watch.WithChannelDeliverer(deliverer),
			)
			runtimeCore.component.WithWatchWorkflow(newBootstrapWatchWorkflow(watchService))
			if err := watchService.Start(ctx); err != nil {
				foundation.startupWarnings.Add("watch_service", err)
				log.Warn("watch service start failed", "error", err)
			}
		}
	}

	var heartbeatService *heartbeat.Service
	if enabledOrDefault(cfg.Heartbeat.Enabled, true) {
		heartbeatService = heartbeat.NewService(heartbeat.Config{
			Interval: cfg.Heartbeat.Interval,
			Timeout:  cfg.Heartbeat.Timeout,
		})
		if err := heartbeatService.Start(ctx); err != nil {
			foundation.startupWarnings.Add("heartbeat_service", err)
			log.Warn("heartbeat service start failed", "error", err)
		}
	}

	var wireLogger *wire.Logger
	if enabledOrDefault(cfg.Wire.Enabled, false) {
		wireLogger = wire.NewLogger(wire.Config{
			Enabled:       true,
			MaxEntries:    cfg.Wire.MaxEntries,
			MaxBodyBytes:  cfg.Wire.MaxBodyBytes,
			RetentionTime: cfg.Wire.RetentionTime,
			RedactHeaders: cfg.Wire.RedactHeaders,
			Providers:     cfg.Wire.Providers,
		})
	}

	var wakeupService *wakeup.Service
	if wakeupEnabled(cfg.Wakeup) {
		wakeupStore, wakeupErr := initWakeupStore(cfg.Wakeup)
		if wakeupErr != nil {
			foundation.startupWarnings.Add("wakeup_store", wakeupErr)
			log.Warn("wakeup store init failed", "error", wakeupErr)
		} else if wakeupStore != nil {
			wakeupService = wakeup.NewService(wakeupStore, newWakeupSubmitFunc(submitter))
			if err := wakeupService.Start(ctx); err != nil {
				foundation.startupWarnings.Add("wakeup_service", err)
				log.Warn("wakeup service start failed", "error", err)
			}
		}
	}

	return &preparedOperatorAutomationPack{
		cronService:      cronService,
		watchService:     watchService,
		heartbeatService: heartbeatService,
		wireLogger:       wireLogger,
		wakeupService:    wakeupService,
		deliverer:        deliverer,
	}
}

func channelDelivererFor(channelManager *channelmgr.Manager) *channelCronDeliverer {
	if channelManager == nil {
		return nil
	}
	return newChannelCronDeliverer(channelManager)
}

func newWakeupSubmitFunc(submitter *runtimeAutomationSubmitter) func(context.Context, wakeup.Trigger) (*wakeup.ExecutionResult, error) {
	return func(ctx context.Context, trigger wakeup.Trigger) (*wakeup.ExecutionResult, error) {
		sessionKey := strings.TrimSpace(trigger.SessionKey)
		if sessionKey == "" {
			sessionKey = "wakeup:" + strings.TrimSpace(trigger.ID)
		}
		automationID := strings.TrimSpace(trigger.AutomationID)
		if automationID == "" {
			automationID = strings.TrimSpace(trigger.ID)
		}
		metadata := make(map[string]any, len(trigger.Metadata)+4)
		for key, value := range trigger.Metadata {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			metadata[key] = value
		}
		metadata["automation_kind"] = "wakeup"
		metadata["automation_id"] = strings.TrimSpace(trigger.ID)
		metadata["automation_name"] = strings.TrimSpace(trigger.Name)
		if channel := strings.TrimSpace(trigger.Channel); channel != "" {
			metadata["channel"] = channel
		}
		result, err := submitter.Submit(ctx, automation.SubmitRequest{
			SessionKey:   sessionKey,
			Content:      trigger.Message,
			Model:        strings.TrimSpace(trigger.Model),
			AutomationID: automationID,
			Metadata:     metadata,
		})
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
		}
		return &wakeup.ExecutionResult{
			RunID:               strings.TrimSpace(result.RunID),
			Summary:             normalize.FirstNonEmpty(strings.TrimSpace(result.Summary), strings.TrimSpace(result.Output)),
			VerificationStatus:  strings.TrimSpace(result.VerificationStatus),
			VerificationSummary: strings.TrimSpace(result.VerificationSummary),
		}, nil
	}
}
