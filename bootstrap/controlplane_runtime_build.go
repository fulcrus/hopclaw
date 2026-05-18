package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type approvalTimeoutResolver func(context.Context, string) (*approval.Ticket, error)

func newApprovalTimeoutService(
	cfg config.ExecApprovalConfig,
	store approval.Store,
	resolve approvalTimeoutResolver,
	bus eventbus.Bus,
	runtime *runtimesvc.Service,
	warnings *startupWarningCollector,
) *approval.TimeoutService {
	if store == nil || cfg.ApprovalTimeout <= 0 || resolve == nil {
		return nil
	}
	service := approval.NewTimeoutService(approval.TimeoutConfig{
		ApprovalTimeout: cfg.ApprovalTimeout,
		GracePeriod:     cfg.GracePeriod,
	}, store,
		func(ctx context.Context, ticketID string) error {
			ticket, _ := store.Get(ctx, ticketID)
			resolved, err := resolve(ctx, ticketID)
			if err != nil {
				return err
			}
			if resolved != nil {
				ticket = resolved
			}
			publishApprovalLifecycleEvent(ctx, bus, eventbus.EventApprovalTimedOut, ticket, 0)
			return nil
		},
		func(ctx context.Context, ticket *approval.Ticket, remaining time.Duration) {
			publishApprovalLifecycleEvent(ctx, bus, eventbus.EventApprovalGraceWarning, ticket, remaining)
		},
	)
	service.WithFailureHooks(approval.TimeoutFailureHooks{
		OnListFailure: func(err error) {
			recordApprovalTimeoutSweepWarning(warnings, err)
		},
		OnListRecovered: func() {
			clearApprovalTimeoutSweepWarning(warnings)
		},
		OnResolveFailure: func(ticket *approval.Ticket, attempts int, err error) {
			recordApprovalTimeoutResolveWarning(warnings, ticket, attempts, err)
		},
		OnResolveRecovered: func(ticketID string) {
			clearApprovalTimeoutResolveWarning(warnings, ticketID)
		},
	})
	service.WithForceResolve(0, func(ctx context.Context, ticket *approval.Ticket) error {
		return forceCancelTimedOutApproval(ctx, runtime, store, bus, warnings, ticket)
	})
	return service
}

func newGovernanceDispatcher(
	cfg config.Config,
	sharedDB *sql.DB,
	bus eventbus.Bus,
	snapshotResolver controlgov.SnapshotResolver,
	adapters []controlgov.Adapter,
) (*controlgov.ReliableDispatcher, *sql.DB, error) {
	if len(adapters) == 0 {
		return nil, nil, nil
	}
	deliveryStore, deliveryDB, err := buildGovernanceDeliveryStore(cfg, sharedDB)
	if err != nil {
		return nil, nil, err
	}
	dispatcher := controlgov.NewReliableDispatcher(governanceDeliveryConfig(cfg.Runtime.Governance.Delivery), deliveryStore, adapters...).
		WithSnapshotResolver(snapshotResolver).
		WithEventBus(bus)
	return dispatcher, deliveryDB, nil
}

func newRuntimeAuditSink(cfg config.Config, sharedAuditDB *sql.DB) (eventbus.Sink, error) {
	if !cfg.Runtime.Audit.Enabled {
		return nil, nil
	}
	sinks := make([]eventbus.Sink, 0, len(cfg.Runtime.Audit.Sinks)+1)
	if path := strings.TrimSpace(cfg.Runtime.Audit.Output); path != "" {
		sinks = append(sinks, &audit.JSONLRecorder{Path: path})
	}
	configured, err := buildConfiguredAuditSinks(cfg.Runtime.Audit.Sinks)
	if err != nil {
		return nil, err
	}
	if len(configured) > 0 {
		dispatcher, err := newAuditDispatcher(cfg, sharedAuditDB, configured)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, dispatcher)
	}
	switch len(sinks) {
	case 0:
		return nil, nil
	case 1:
		return sinks[0], nil
	default:
		return audit.MultiSink{Sinks: sinks}, nil
	}
}

func newAuditDispatcher(cfg config.Config, sharedAuditDB *sql.DB, sinks []audit.DeliverySink) (*audit.ReliableDispatcher, error) {
	if len(sinks) == 0 {
		return nil, nil
	}
	var deliveryStore audit.DeliveryStore
	switch resolvedAuditDeliveryBackend(cfg) {
	case "sqlite":
		if sharedAuditDB == nil {
			return nil, fmt.Errorf("sqlite audit delivery requires store.backend=sqlite")
		}
		sqliteStore, err := audit.NewSQLiteDeliveryStore(sharedAuditDB)
		if err != nil {
			return nil, err
		}
		deliveryStore = sqliteStore
	default:
		deliveryStore = audit.NewInMemoryDeliveryStore()
	}
	return audit.NewReliableDispatcher(auditDeliveryConfig(cfg.Runtime.Audit.Delivery), deliveryStore, sinks...), nil
}

func auditDeliveryConfig(cfg config.AuditDeliveryConfig) audit.DeliveryConfig {
	return audit.DeliveryConfig{
		MaxAttempts:  cfg.MaxAttempts,
		BaseBackoff:  cfg.BaseBackoff,
		MaxBackoff:   cfg.MaxBackoff,
		PollInterval: cfg.PollInterval,
		BatchSize:    cfg.BatchSize,
	}
}
