package governanceadapter

import (
	"bufio"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
)

const (
	defaultDeliveryMaxAttempts  = 8
	defaultDeliveryBaseBackoff  = 5 * time.Second
	defaultDeliveryMaxBackoff   = 5 * time.Minute
	defaultDeliveryPollInterval = 2 * time.Second
	defaultDeliveryBatchSize    = 32
	deliveryQueuePublishGrace   = time.Millisecond
	deliveryJSONLMaxRecordBytes = 16 * 1024 * 1024
	deliveryOutboxDirName       = "delivery_outbox"
	legacyDeliveryJSONLDirName  = "governance_deliveries"
)

type DeliveryStatus = controlplane.GovernanceDeliveryStatus

const (
	DeliveryStatusPending    DeliveryStatus = controlplane.GovernanceDeliveryStatusPending
	DeliveryStatusDelivered  DeliveryStatus = controlplane.GovernanceDeliveryStatusDelivered
	DeliveryStatusDeadLetter DeliveryStatus = controlplane.GovernanceDeliveryStatusDeadLetter
)

type DeliveryEntry struct {
	ID             string         `json:"id"`
	AdapterName    string         `json:"adapter_name"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Status         DeliveryStatus `json:"status"`
	Record         Record         `json:"record"`
	Attempts       int            `json:"attempts,omitempty"`
	MaxAttempts    int            `json:"max_attempts,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	NextAttemptAt  time.Time      `json:"next_attempt_at,omitempty"`
	LastAttemptAt  time.Time      `json:"last_attempt_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeliveredAt    time.Time      `json:"delivered_at,omitempty"`
}

func (e DeliveryEntry) Normalized() DeliveryEntry {
	out := e
	out.ID = strings.TrimSpace(out.ID)
	out.AdapterName = strings.TrimSpace(out.AdapterName)
	out.Status = DeliveryStatus(strings.TrimSpace(string(out.Status)))
	if out.Status == "" {
		out.Status = DeliveryStatusPending
	}
	out.Record = out.Record.Normalized()
	out.IdempotencyKey = normalize.FirstNonEmpty(
		strings.TrimSpace(out.IdempotencyKey),
		DefaultDeliveryIdempotencyKey(out.AdapterName, out.Record),
	)
	out.LastError = strings.TrimSpace(out.LastError)
	out.NextAttemptAt = out.NextAttemptAt.UTC()
	out.LastAttemptAt = out.LastAttemptAt.UTC()
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	out.DeliveredAt = out.DeliveredAt.UTC()
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = defaultDeliveryMaxAttempts
	}
	return out
}

func DefaultDeliveryIdempotencyKey(adapterName string, record Record) string {
	adapterName = strings.ToLower(strings.TrimSpace(adapterName))
	record = record.Normalized()
	if adapterName == "" {
		return ""
	}
	if eventID := strings.TrimSpace(record.EventID); eventID != "" {
		return eventID
	}
	parts := []string{
		string(record.Kind),
		string(record.EventType),
		strings.TrimSpace(record.RunID),
		strings.TrimSpace(record.SessionID),
		strings.TrimSpace(record.Summary),
	}
	meaningful := false
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			meaningful = true
			break
		}
	}
	if !meaningful {
		return ""
	}
	joined := strings.Join(parts, "\x00")
	sum := sha1.Sum([]byte(joined))
	return hex.EncodeToString(sum[:])
}

type DeliveryListFilter = controlplane.GovernanceDeliveryListFilter

type DeliveryRedriveOptions = controlplane.GovernanceDeliveryRedriveOptions

type DeliveryStore interface {
	Enqueue(ctx context.Context, entry DeliveryEntry) (*DeliveryEntry, bool, error)
	Get(ctx context.Context, id string) (*DeliveryEntry, error)
	ListDue(ctx context.Context, before time.Time, limit int) ([]*DeliveryEntry, error)
	Update(ctx context.Context, entry *DeliveryEntry) error
	List(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error)
}

type DeliveryConfig struct {
	MaxAttempts  int           `json:"max_attempts,omitempty"`
	BaseBackoff  time.Duration `json:"base_backoff,omitempty"`
	MaxBackoff   time.Duration `json:"max_backoff,omitempty"`
	PollInterval time.Duration `json:"poll_interval,omitempty"`
	BatchSize    int           `json:"batch_size,omitempty"`
}

func (c DeliveryConfig) normalized() DeliveryConfig {
	out := c
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = defaultDeliveryMaxAttempts
	}
	if out.BaseBackoff <= 0 {
		out.BaseBackoff = defaultDeliveryBaseBackoff
	}
	if out.MaxBackoff <= 0 {
		out.MaxBackoff = defaultDeliveryMaxBackoff
	}
	if out.MaxBackoff < out.BaseBackoff {
		out.MaxBackoff = out.BaseBackoff
	}
	if out.PollInterval <= 0 {
		out.PollInterval = defaultDeliveryPollInterval
	}
	if out.BatchSize <= 0 {
		out.BatchSize = defaultDeliveryBatchSize
	}
	return out
}

type ReliableDispatcher struct {
	config           DeliveryConfig
	store            DeliveryStore
	targets          []deliveryTarget
	targetByName     map[string]deliveryTarget
	snapshotResolver SnapshotResolver
	bus              eventbus.Bus

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	wakeCh chan struct{}
}

type deliveryTarget struct {
	name    string
	adapter Adapter
}

func NewReliableDispatcher(cfg DeliveryConfig, store DeliveryStore, adapters ...Adapter) *ReliableDispatcher {
	cfg = cfg.normalized()
	if store == nil {
		store = NewInMemoryDeliveryStore()
	}
	targets := make([]deliveryTarget, 0, len(adapters))
	targetByName := make(map[string]deliveryTarget, len(adapters))
	for i, adapter := range adapters {
		if isNilAdapter(adapter) {
			continue
		}
		name := adapterDeliveryName(adapter, i)
		target := deliveryTarget{name: name, adapter: adapter}
		targets = append(targets, target)
		targetByName[name] = target
	}
	return &ReliableDispatcher{
		config:       cfg,
		store:        store,
		targets:      targets,
		targetByName: targetByName,
		wakeCh:       make(chan struct{}, 1),
	}
}

func (d *ReliableDispatcher) WithSnapshotResolver(resolver SnapshotResolver) *ReliableDispatcher {
	if d != nil {
		d.snapshotResolver = resolver
	}
	return d
}

func (d *ReliableDispatcher) WithEventBus(bus eventbus.Bus) *ReliableDispatcher {
	if d != nil {
		d.bus = bus
	}
	return d
}

func (d *ReliableDispatcher) Start(ctx context.Context) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.cancel != nil {
		d.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	done := make(chan struct{})
	d.done = done
	d.mu.Unlock()
	go func() {
		defer close(done)
		d.loop(loopCtx)
	}()
}

func (d *ReliableDispatcher) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	cancel := d.cancel
	done := d.done
	d.cancel = nil
	d.done = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (d *ReliableDispatcher) Handle(ctx context.Context, event eventbus.Event) error {
	if d == nil || len(d.targets) == 0 {
		return nil
	}
	record, ok := Project(event)
	if !ok {
		return nil
	}
	if d.snapshotResolver != nil && strings.TrimSpace(record.Governance.EffectiveConfigSnapshotID) != "" {
		if snapshot := d.snapshotResolver.EffectiveConfigSnapshot(); snapshot != nil && strings.TrimSpace(snapshot.ID) == record.Governance.EffectiveConfigSnapshotID {
			record.Snapshot = snapshot.Clone()
		}
	}
	record = record.Normalized()
	for _, target := range d.targets {
		initialAttemptAt := time.Now().UTC().Add(deliveryQueuePublishGrace)
		idempotencyKey := DefaultDeliveryIdempotencyKey(target.name, record)
		entry, created, err := d.store.Enqueue(ctx, DeliveryEntry{
			AdapterName:    target.name,
			IdempotencyKey: idempotencyKey,
			Status:         DeliveryStatusPending,
			Record:         record,
			MaxAttempts:    d.config.MaxAttempts,
			NextAttemptAt:  initialAttemptAt,
		})
		if err != nil {
			log.Warn("governance delivery enqueue failed",
				"adapter", target.name,
				"event_id", record.EventID,
				"event_type", string(record.EventType),
				"error", err)
			continue
		}
		if created {
			d.publishDeliveryEvent(ctx, eventbus.EventGovernanceDeliveryQueued, entry, "")
			entry.NextAttemptAt = time.Now().UTC()
			entry.UpdatedAt = entry.NextAttemptAt
			if err := d.store.Update(ctx, entry); err != nil {
				log.Warn("governance delivery arm queued entry failed",
					"delivery_id", entry.ID,
					"adapter", entry.AdapterName,
					"error", err)
			}
		}
		d.signal()
	}
	return nil
}

func (d *ReliableDispatcher) GetDelivery(ctx context.Context, id string) (*DeliveryEntry, error) {
	if d == nil || d.store == nil {
		return nil, fmt.Errorf("governance delivery store is not configured")
	}
	return d.store.Get(ctx, strings.TrimSpace(id))
}

func (d *ReliableDispatcher) ListDeliveries(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error) {
	if d == nil || d.store == nil {
		return nil, fmt.Errorf("governance delivery store is not configured")
	}
	return d.store.List(ctx, filter)
}

func (d *ReliableDispatcher) Redrive(ctx context.Context, ids []string, opts DeliveryRedriveOptions) ([]*DeliveryEntry, error) {
	if d == nil || d.store == nil {
		return nil, fmt.Errorf("governance delivery store is not configured")
	}
	ids = dedupeDeliveryIDs(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("delivery ids are required")
	}
	now := time.Now().UTC()
	items := make([]*DeliveryEntry, 0, len(ids))
	for _, id := range ids {
		entry, err := d.store.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if entry == nil {
			continue
		}
		next := entry.Normalized()
		if next.Status == DeliveryStatusDelivered {
			items = append(items, cloneDeliveryEntry(&next))
			continue
		}
		next.Status = DeliveryStatusPending
		if opts.ResetAttempts {
			next.Attempts = 0
		}
		if opts.ClearError || opts.ResetAttempts {
			next.LastError = ""
		}
		next.NextAttemptAt = now
		next.UpdatedAt = now
		next.DeliveredAt = time.Time{}
		if err := d.store.Update(ctx, &next); err != nil {
			return nil, err
		}
		items = append(items, cloneDeliveryEntry(&next))
		d.publishDeliveryEvent(ctx, eventbus.EventGovernanceDeliveryRedriven, &next, "")
	}
	if len(items) > 0 {
		d.signal()
	}
	return items, nil
}

func (d *ReliableDispatcher) loop(ctx context.Context) {
	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()
	d.processDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.processDue(ctx)
		case <-d.wakeCh:
			d.processDue(ctx)
		}
	}
}

func (d *ReliableDispatcher) processDue(ctx context.Context) {
	entries, err := d.store.ListDue(ctx, time.Now().UTC(), d.config.BatchSize)
	if err != nil {
		log.Warn("governance delivery load due entries failed", "error", err)
		return
	}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		d.processOne(ctx, entry)
	}
}

func (d *ReliableDispatcher) processOne(ctx context.Context, entry *DeliveryEntry) {
	target, ok := d.targetByName[strings.TrimSpace(entry.AdapterName)]
	if !ok {
		d.failEntry(ctx, entry, fmt.Errorf("adapter %q not configured", entry.AdapterName))
		return
	}
	attempted := entry.Normalized()
	attempted.Attempts++
	attempted.LastAttemptAt = time.Now().UTC()
	attempted.UpdatedAt = attempted.LastAttemptAt
	if err := d.handleOne(ctx, target.adapter, attempted.Record); err != nil {
		d.failEntry(ctx, &attempted, err)
		return
	}
	attempted.Status = DeliveryStatusDelivered
	attempted.LastError = ""
	attempted.NextAttemptAt = time.Time{}
	attempted.DeliveredAt = attempted.LastAttemptAt
	if err := d.store.Update(ctx, &attempted); err != nil {
		log.Warn("governance delivery mark delivered failed",
			"delivery_id", attempted.ID,
			"adapter", attempted.AdapterName,
			"error", err)
		return
	}
	d.publishDeliveryEvent(ctx, eventbus.EventGovernanceDeliveryDelivered, &attempted, "")
}

func (d *ReliableDispatcher) failEntry(ctx context.Context, entry *DeliveryEntry, cause error) {
	if entry == nil {
		return
	}
	failed := entry.Normalized()
	if cause == nil {
		cause = fmt.Errorf("unknown delivery error")
	}
	if !failed.LastAttemptAt.IsZero() {
		failed.UpdatedAt = failed.LastAttemptAt
	} else {
		failed.LastAttemptAt = time.Now().UTC()
		failed.UpdatedAt = failed.LastAttemptAt
	}
	failed.LastError = strings.TrimSpace(cause.Error())
	if failed.Attempts >= failed.MaxAttempts {
		failed.Status = DeliveryStatusDeadLetter
		failed.NextAttemptAt = time.Time{}
		if err := d.store.Update(ctx, &failed); err != nil {
			log.Warn("governance delivery mark dead letter failed",
				"delivery_id", failed.ID,
				"adapter", failed.AdapterName,
				"error", err)
			return
		}
		d.publishDeliveryEvent(ctx, eventbus.EventGovernanceDeliveryDeadLettered, &failed, failed.LastError)
		return
	}
	failed.Status = DeliveryStatusPending
	failed.NextAttemptAt = failed.LastAttemptAt.Add(computeBackoff(d.config.BaseBackoff, d.config.MaxBackoff, failed.Attempts))
	if err := d.store.Update(ctx, &failed); err != nil {
		log.Warn("governance delivery reschedule failed",
			"delivery_id", failed.ID,
			"adapter", failed.AdapterName,
			"error", err)
		return
	}
	d.publishDeliveryEvent(ctx, eventbus.EventGovernanceDeliveryRetryScheduled, &failed, failed.LastError)
	d.signal()
}

func (d *ReliableDispatcher) handleOne(ctx context.Context, adapter Adapter, record Record) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("adapter panic: %v", recovered)
		}
	}()
	return adapter.HandleGovernanceRecord(ctx, record)
}

func (d *ReliableDispatcher) publishDeliveryEvent(ctx context.Context, eventType eventbus.EventType, entry *DeliveryEntry, deliveryErr string) {
	if d == nil || d.bus == nil || entry == nil {
		return
	}
	payload := eventbus.GovernanceDeliveryAttrs{
		DeliveryID:          entry.ID,
		AdapterName:         entry.AdapterName,
		IdempotencyKey:      entry.IdempotencyKey,
		DeliveryStatus:      string(entry.Status),
		DeliveryAttempts:    entry.Attempts,
		DeliveryMaxAttempts: entry.MaxAttempts,
		GovernanceKind:      string(entry.Record.Kind),
		SourceEventID:       entry.Record.EventID,
		SourceEventType:     string(entry.Record.EventType),
		NextAttemptAt:       entry.NextAttemptAt,
		DeliveredAt:         entry.DeliveredAt,
		Error:               strings.TrimSpace(deliveryErr),
	}
	var (
		event eventbus.Event
		ok    bool
	)
	switch eventType {
	case eventbus.EventGovernanceDeliveryQueued:
		event = eventbus.NewGovernanceDeliveryQueuedEvent(entry.Record.RunID, entry.Record.SessionID, payload, nil)
		ok = true
	case eventbus.EventGovernanceDeliveryRedriven:
		event = eventbus.NewGovernanceDeliveryRedrivenEvent(entry.Record.RunID, entry.Record.SessionID, payload, nil)
		ok = true
	case eventbus.EventGovernanceDeliveryRetryScheduled:
		event = eventbus.NewGovernanceDeliveryRetryScheduledEvent(entry.Record.RunID, entry.Record.SessionID, payload, nil)
		ok = true
	case eventbus.EventGovernanceDeliveryDelivered:
		event = eventbus.NewGovernanceDeliveryDeliveredEvent(entry.Record.RunID, entry.Record.SessionID, payload, nil)
		ok = true
	case eventbus.EventGovernanceDeliveryDeadLettered:
		event = eventbus.NewGovernanceDeliveryDeadLetteredEvent(entry.Record.RunID, entry.Record.SessionID, payload, nil)
		ok = true
	}
	if !ok {
		return
	}
	logging.LogIfErr(ctx, d.bus.Publish(ctx, event), "emit governance delivery event failed")
}

func (d *ReliableDispatcher) signal() {
	if d == nil {
		return
	}
	select {
	case d.wakeCh <- struct{}{}:
	default:
	}
}

func computeBackoff(base, max time.Duration, attempts int) time.Duration {
	if base <= 0 {
		base = defaultDeliveryBaseBackoff
	}
	if max <= 0 {
		max = defaultDeliveryMaxBackoff
	}
	delay := base
	for i := 1; i < attempts && delay < max; i++ {
		delay *= 2
	}
	if delay > max {
		return max
	}
	return delay
}

func adapterDeliveryName(adapter Adapter, index int) string {
	if named, ok := adapter.(NamedAdapter); ok {
		if name := strings.TrimSpace(named.Name()); name != "" {
			return name
		}
	}
	return fmt.Sprintf("adapter-%02d:%s", index, reflect.TypeOf(adapter).String())
}

func DeliveryOutboxKey(adapterName, idempotencyKey string) string {
	adapterName = strings.ToLower(strings.TrimSpace(adapterName))
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if adapterName == "" || idempotencyKey == "" {
		return ""
	}
	return adapterName + "\x00" + idempotencyKey
}

func DeliverySourceKey(adapterName, eventID string) string {
	return DeliveryOutboxKey(adapterName, eventID)
}

func cloneDeliveryEntry(entry *DeliveryEntry) *DeliveryEntry {
	if entry == nil {
		return nil
	}
	out := entry.Normalized()
	return &out
}

type InMemoryDeliveryStore struct {
	mu       sync.RWMutex
	nextID   atomic.Uint64
	byID     map[string]*DeliveryEntry
	byOutbox map[string]string
}

func NewInMemoryDeliveryStore() *InMemoryDeliveryStore {
	return &InMemoryDeliveryStore{
		byID:     make(map[string]*DeliveryEntry),
		byOutbox: make(map[string]string),
	}
}

func (s *InMemoryDeliveryStore) Enqueue(_ context.Context, entry DeliveryEntry) (*DeliveryEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry = entry.Normalized()
	if entry.AdapterName == "" {
		return nil, false, fmt.Errorf("adapter name is required")
	}
	if entry.ID == "" {
		if key := DeliveryOutboxKey(entry.AdapterName, entry.IdempotencyKey); key != "" {
			if existingID, ok := s.byOutbox[key]; ok {
				existing, exists := s.byID[existingID]
				if exists {
					return cloneDeliveryEntry(existing), false, nil
				}
			}
		}
		entry.ID = fmt.Sprintf("gdel-%06d", s.nextID.Add(1))
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = now
	}
	if entry.NextAttemptAt.IsZero() && entry.Status == DeliveryStatusPending {
		entry.NextAttemptAt = now
	}
	copied := cloneDeliveryEntry(&entry)
	s.byID[copied.ID] = copied
	if key := DeliveryOutboxKey(copied.AdapterName, copied.IdempotencyKey); key != "" {
		s.byOutbox[key] = copied.ID
	}
	return cloneDeliveryEntry(copied), true, nil
}

func (s *InMemoryDeliveryStore) Get(_ context.Context, id string) (*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("governance delivery %s not found", id)
	}
	return cloneDeliveryEntry(item), nil
}

func (s *InMemoryDeliveryStore) ListDue(_ context.Context, before time.Time, limit int) ([]*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*DeliveryEntry, 0, len(s.byID))
	for _, item := range s.byID {
		if item == nil || item.Status != DeliveryStatusPending {
			continue
		}
		if !item.NextAttemptAt.IsZero() && item.NextAttemptAt.After(before) {
			continue
		}
		items = append(items, cloneDeliveryEntry(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].NextAttemptAt.Equal(items[j].NextAttemptAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].NextAttemptAt.Before(items[j].NextAttemptAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *InMemoryDeliveryStore) Update(_ context.Context, entry *DeliveryEntry) error {
	if entry == nil {
		return fmt.Errorf("delivery entry is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.byID[strings.TrimSpace(entry.ID)]
	if !ok {
		return fmt.Errorf("governance delivery %s not found", entry.ID)
	}
	next := cloneDeliveryEntry(entry)
	s.byID[next.ID] = next
	if key := DeliveryOutboxKey(current.AdapterName, current.IdempotencyKey); key != "" && key != DeliveryOutboxKey(next.AdapterName, next.IdempotencyKey) {
		delete(s.byOutbox, key)
	}
	if key := DeliveryOutboxKey(next.AdapterName, next.IdempotencyKey); key != "" {
		s.byOutbox[key] = next.ID
	}
	return nil
}

func (s *InMemoryDeliveryStore) List(_ context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*DeliveryEntry, 0, len(s.byID))
	for _, item := range s.byID {
		if item == nil {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if strings.TrimSpace(filter.AdapterName) != "" && item.AdapterName != strings.TrimSpace(filter.AdapterName) {
			continue
		}
		items = append(items, cloneDeliveryEntry(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

type JSONLDeliveryStore struct {
	root       string
	legacyRoot string

	mu       sync.RWMutex
	nextID   atomic.Uint64
	byID     map[string]*DeliveryEntry
	byOutbox map[string]string
}

func NewJSONLDeliveryStore(root string) (*JSONLDeliveryStore, error) {
	store := &JSONLDeliveryStore{
		root:       filepath.Join(strings.TrimSpace(root), deliveryOutboxDirName),
		legacyRoot: filepath.Join(strings.TrimSpace(root), legacyDeliveryJSONLDirName),
		byID:       make(map[string]*DeliveryEntry),
		byOutbox:   make(map[string]string),
	}
	if err := os.MkdirAll(store.root, 0o755); err != nil {
		return nil, err
	}
	if store.legacyRoot != "" && store.legacyRoot != store.root {
		if err := os.MkdirAll(store.legacyRoot, 0o755); err != nil {
			return nil, err
		}
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *JSONLDeliveryStore) Enqueue(_ context.Context, entry DeliveryEntry) (*DeliveryEntry, bool, error) {
	s.mu.Lock()
	entry = entry.Normalized()
	if entry.AdapterName == "" {
		s.mu.Unlock()
		return nil, false, fmt.Errorf("adapter name is required")
	}
	if key := DeliveryOutboxKey(entry.AdapterName, entry.IdempotencyKey); key != "" {
		if existingID, ok := s.byOutbox[key]; ok {
			existing := cloneDeliveryEntry(s.byID[existingID])
			s.mu.Unlock()
			return existing, false, nil
		}
	}
	now := time.Now().UTC()
	entry.ID = fmt.Sprintf("gdel-%06d", s.nextID.Add(1))
	entry.CreatedAt = now
	entry.UpdatedAt = now
	if entry.NextAttemptAt.IsZero() && entry.Status == DeliveryStatusPending {
		entry.NextAttemptAt = now
	}
	copied := cloneDeliveryEntry(&entry)
	s.byID[copied.ID] = copied
	if key := DeliveryOutboxKey(copied.AdapterName, copied.IdempotencyKey); key != "" {
		s.byOutbox[key] = copied.ID
	}
	s.mu.Unlock()

	if err := s.appendSnapshot(copied); err != nil {
		s.mu.Lock()
		delete(s.byID, copied.ID)
		if key := DeliveryOutboxKey(copied.AdapterName, copied.IdempotencyKey); key != "" && s.byOutbox[key] == copied.ID {
			delete(s.byOutbox, key)
		}
		s.mu.Unlock()
		return nil, false, err
	}
	return cloneDeliveryEntry(copied), true, nil
}

func (s *JSONLDeliveryStore) Get(_ context.Context, id string) (*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("governance delivery %s not found", id)
	}
	return cloneDeliveryEntry(item), nil
}

func (s *JSONLDeliveryStore) ListDue(_ context.Context, before time.Time, limit int) ([]*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*DeliveryEntry, 0, len(s.byID))
	for _, item := range s.byID {
		if item == nil || item.Status != DeliveryStatusPending {
			continue
		}
		if !item.NextAttemptAt.IsZero() && item.NextAttemptAt.After(before) {
			continue
		}
		items = append(items, cloneDeliveryEntry(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].NextAttemptAt.Equal(items[j].NextAttemptAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].NextAttemptAt.Before(items[j].NextAttemptAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *JSONLDeliveryStore) Update(_ context.Context, entry *DeliveryEntry) error {
	if entry == nil {
		return fmt.Errorf("delivery entry is required")
	}
	s.mu.Lock()
	current, ok := s.byID[strings.TrimSpace(entry.ID)]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("governance delivery %s not found", entry.ID)
	}
	next := cloneDeliveryEntry(entry)
	s.byID[next.ID] = next
	if key := DeliveryOutboxKey(current.AdapterName, current.IdempotencyKey); key != "" && key != DeliveryOutboxKey(next.AdapterName, next.IdempotencyKey) {
		delete(s.byOutbox, key)
	}
	if key := DeliveryOutboxKey(next.AdapterName, next.IdempotencyKey); key != "" {
		s.byOutbox[key] = next.ID
	}
	s.mu.Unlock()
	if err := s.appendSnapshot(next); err != nil {
		s.mu.Lock()
		s.byID[current.ID] = cloneDeliveryEntry(current)
		if oldKey := DeliveryOutboxKey(current.AdapterName, current.IdempotencyKey); oldKey != "" {
			s.byOutbox[oldKey] = current.ID
		}
		if newKey := DeliveryOutboxKey(next.AdapterName, next.IdempotencyKey); newKey != "" && newKey != DeliveryOutboxKey(current.AdapterName, current.IdempotencyKey) {
			delete(s.byOutbox, newKey)
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *JSONLDeliveryStore) List(_ context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*DeliveryEntry, 0, len(s.byID))
	for _, item := range s.byID {
		if item == nil {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if strings.TrimSpace(filter.AdapterName) != "" && item.AdapterName != strings.TrimSpace(filter.AdapterName) {
			continue
		}
		items = append(items, cloneDeliveryEntry(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (s *JSONLDeliveryStore) load() error {
	loaded, err := s.loadRoot(s.root)
	if err != nil {
		return err
	}
	if loaded > 0 {
		return nil
	}
	if strings.TrimSpace(s.legacyRoot) != "" && s.legacyRoot != s.root {
		if _, err := os.Stat(s.legacyRoot); err == nil {
			_, err = s.loadRoot(s.legacyRoot)
			return err
		}
	}
	return nil
}

func (s *JSONLDeliveryStore) loadRoot(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	loaded := 0
	var maxID uint64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		item, err := loadLatestDeliveryJSONL(filepath.Join(root, entry.Name()))
		if err != nil {
			return loaded, err
		}
		s.byID[item.ID] = item
		if key := DeliveryOutboxKey(item.AdapterName, item.IdempotencyKey); key != "" {
			s.byOutbox[key] = item.ID
		}
		maxID = max(maxID, parseDeliverySequence(item.ID))
		loaded++
	}
	s.nextID.Store(maxID)
	return loaded, nil
}

func (s *JSONLDeliveryStore) path(id string) string {
	return filepath.Join(s.root, strings.TrimSpace(id)+".jsonl")
}

func (s *JSONLDeliveryStore) legacyPath(id string) string {
	return filepath.Join(s.legacyRoot, strings.TrimSpace(id)+".jsonl")
}

func (s *JSONLDeliveryStore) appendSnapshot(entry *DeliveryEntry) error {
	if entry == nil {
		return fmt.Errorf("delivery entry is required")
	}
	if err := appendDeliveryJSONL(s.path(entry.ID), entry); err != nil {
		return err
	}
	if s.legacyRoot != "" && s.legacyRoot != s.root {
		if err := appendDeliveryJSONL(s.legacyPath(entry.ID), entry); err != nil {
			return err
		}
	}
	return nil
}

func appendDeliveryJSONL(path string, entry *DeliveryEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry.Normalized())
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func loadLatestDeliveryJSONL(path string) (*DeliveryEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), deliveryJSONLMaxRecordBytes)
	var last DeliveryEntry
	found := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &last); err != nil {
			return nil, err
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("delivery snapshot %s is empty", path)
	}
	return cloneDeliveryEntry(&last), nil
}

func parseDeliverySequence(id string) uint64 {
	id = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "gdel-"))
	if id == "" {
		return 0
	}
	var value uint64
	fmt.Sscanf(id, "%d", &value)
	return value
}

type SQLiteDeliveryStore struct {
	db     *sql.DB
	nextID atomic.Uint64
}

func NewSQLiteDeliveryStore(db *sql.DB) (*SQLiteDeliveryStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite delivery db is required")
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS delivery_outbox (
    id                TEXT PRIMARY KEY,
    adapter_name      TEXT NOT NULL,
    idempotency_key   TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending',
    source_event_id   TEXT NOT NULL DEFAULT '',
    source_event_type TEXT NOT NULL DEFAULT '',
    record            TEXT NOT NULL DEFAULT '{}',
    attempts          INTEGER NOT NULL DEFAULT 0,
    max_attempts      INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT NOT NULL DEFAULT '',
    next_attempt_at   TEXT NOT NULL DEFAULT '',
    last_attempt_at   TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    delivered_at      TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_outbox_idempotency
    ON delivery_outbox (adapter_name, idempotency_key)
    WHERE adapter_name != '' AND idempotency_key != '';
CREATE INDEX IF NOT EXISTS idx_delivery_outbox_status_due
    ON delivery_outbox (status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_delivery_outbox_adapter
    ON delivery_outbox (adapter_name);
CREATE INDEX IF NOT EXISTS idx_delivery_outbox_source_event
    ON delivery_outbox (source_event_id)
    WHERE source_event_id != '';
`); err != nil {
		return nil, fmt.Errorf("init delivery outbox table: %w", err)
	}
	if err := migrateLegacyGovernanceDeliveries(db); err != nil {
		return nil, err
	}
	store := &SQLiteDeliveryStore{db: db}
	var maxID uint64
	if err := db.QueryRow(`SELECT COALESCE(MAX(CAST(SUBSTR(id, 6) AS INTEGER)), 0) FROM delivery_outbox WHERE id LIKE 'gdel-%'`).Scan(&maxID); err == nil {
		store.nextID.Store(maxID)
	}
	return store, nil
}

func migrateLegacyGovernanceDeliveries(db *sql.DB) error {
	if db == nil {
		return nil
	}
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'governance_deliveries'`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect legacy governance deliveries table: %w", err)
	}
	if exists == 0 {
		return nil
	}
	_, err := db.Exec(`
INSERT OR IGNORE INTO delivery_outbox
    (id, adapter_name, idempotency_key, status, source_event_id, source_event_type, record, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at)
SELECT
    id,
    adapter_name,
    CASE
        WHEN source_event_id != '' THEN source_event_id
        ELSE id
    END,
    status,
    source_event_id,
    source_event_type,
    record,
    attempts,
    max_attempts,
    last_error,
    next_attempt_at,
    last_attempt_at,
    created_at,
    updated_at,
    delivered_at
FROM governance_deliveries`)
	if err != nil {
		return fmt.Errorf("migrate legacy governance deliveries: %w", err)
	}
	return nil
}

func (s *SQLiteDeliveryStore) Enqueue(ctx context.Context, entry DeliveryEntry) (*DeliveryEntry, bool, error) {
	entry = entry.Normalized()
	if entry.AdapterName == "" {
		return nil, false, fmt.Errorf("adapter name is required")
	}
	if item, err := s.getByOutboxKey(ctx, entry.AdapterName, entry.IdempotencyKey); err == nil && item != nil {
		return item, false, nil
	} else if err != nil && err != sql.ErrNoRows {
		return nil, false, err
	}
	now := time.Now().UTC()
	entry.ID = fmt.Sprintf("gdel-%06d", s.nextID.Add(1))
	entry.CreatedAt = now
	entry.UpdatedAt = now
	if entry.NextAttemptAt.IsZero() && entry.Status == DeliveryStatusPending {
		entry.NextAttemptAt = now
	}
	recordJSON, err := json.Marshal(entry.Record.Normalized())
	if err != nil {
		return nil, false, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO delivery_outbox
		(id, adapter_name, idempotency_key, status, source_event_id, source_event_type, record, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.AdapterName, entry.IdempotencyKey, string(entry.Status), entry.Record.EventID, string(entry.Record.EventType), string(recordJSON),
		entry.Attempts, entry.MaxAttempts, entry.LastError, formatDeliveryTime(entry.NextAttemptAt), formatDeliveryTime(entry.LastAttemptAt), formatDeliveryTime(entry.CreatedAt), formatDeliveryTime(entry.UpdatedAt), formatDeliveryTime(entry.DeliveredAt),
	)
	if err != nil {
		return nil, false, err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr == nil && rows == 0 {
		item, getErr := s.getByOutboxKey(ctx, entry.AdapterName, entry.IdempotencyKey)
		return item, false, getErr
	}
	item, err := s.Get(ctx, entry.ID)
	return item, true, err
}

func (s *SQLiteDeliveryStore) Get(ctx context.Context, id string) (*DeliveryEntry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, adapter_name, idempotency_key, status, source_event_id, source_event_type, record, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at FROM delivery_outbox WHERE id = ?`, strings.TrimSpace(id))
	return scanSQLiteDelivery(row)
}

func (s *SQLiteDeliveryStore) getByOutboxKey(ctx context.Context, adapterName, idempotencyKey string) (*DeliveryEntry, error) {
	key := DeliveryOutboxKey(adapterName, idempotencyKey)
	if key == "" {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, adapter_name, idempotency_key, status, source_event_id, source_event_type, record, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at FROM delivery_outbox WHERE adapter_name = ? AND idempotency_key = ? LIMIT 1`, strings.TrimSpace(adapterName), strings.TrimSpace(idempotencyKey))
	return scanSQLiteDelivery(row)
}

func (s *SQLiteDeliveryStore) ListDue(ctx context.Context, before time.Time, limit int) ([]*DeliveryEntry, error) {
	query := `SELECT id, adapter_name, idempotency_key, status, source_event_id, source_event_type, record, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at
		FROM delivery_outbox
		WHERE status = ? AND (next_attempt_at = '' OR next_attempt_at <= ?)
		ORDER BY next_attempt_at ASC, created_at ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, query, string(DeliveryStatusPending), formatDeliveryTime(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DeliveryEntry
	for rows.Next() {
		item, err := scanSQLiteDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteDeliveryStore) Update(ctx context.Context, entry *DeliveryEntry) error {
	if entry == nil {
		return fmt.Errorf("delivery entry is required")
	}
	entry = cloneDeliveryEntry(entry)
	recordJSON, err := json.Marshal(entry.Record.Normalized())
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE delivery_outbox
		SET adapter_name = ?, idempotency_key = ?, status = ?, source_event_id = ?, source_event_type = ?, record = ?, attempts = ?, max_attempts = ?, last_error = ?, next_attempt_at = ?, last_attempt_at = ?, created_at = ?, updated_at = ?, delivered_at = ?
		WHERE id = ?`,
		entry.AdapterName, entry.IdempotencyKey, string(entry.Status), entry.Record.EventID, string(entry.Record.EventType), string(recordJSON),
		entry.Attempts, entry.MaxAttempts, entry.LastError, formatDeliveryTime(entry.NextAttemptAt), formatDeliveryTime(entry.LastAttemptAt), formatDeliveryTime(entry.CreatedAt), formatDeliveryTime(entry.UpdatedAt), formatDeliveryTime(entry.DeliveredAt),
		entry.ID,
	)
	return err
}

func (s *SQLiteDeliveryStore) List(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error) {
	query := `SELECT id, adapter_name, idempotency_key, status, source_event_id, source_event_type, record, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at FROM delivery_outbox WHERE 1=1`
	var args []any
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, string(filter.Status))
	}
	if strings.TrimSpace(filter.AdapterName) != "" {
		query += ` AND adapter_name = ?`
		args = append(args, strings.TrimSpace(filter.AdapterName))
	}
	query += ` ORDER BY created_at ASC`
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DeliveryEntry
	for rows.Next() {
		item, err := scanSQLiteDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanSQLiteDelivery(row interface{ Scan(...any) error }) (*DeliveryEntry, error) {
	var (
		id, adapterName, idempotencyKey, status, sourceEventID, sourceEventType string
		recordJSON                                                              string
		attempts, maxAttempts                                                   int
		lastError, nextAttemptAt, lastAttemptAt                                 string
		createdAt, updatedAt, deliveredAt                                       string
	)
	if err := row.Scan(&id, &adapterName, &idempotencyKey, &status, &sourceEventID, &sourceEventType, &recordJSON, &attempts, &maxAttempts, &lastError, &nextAttemptAt, &lastAttemptAt, &createdAt, &updatedAt, &deliveredAt); err != nil {
		return nil, err
	}
	var record Record
	if err := json.Unmarshal([]byte(recordJSON), &record); err != nil {
		return nil, fmt.Errorf("decode governance delivery record %s: %w", id, err)
	}
	record.EventID = normalize.FirstNonEmpty(record.EventID, sourceEventID)
	if record.EventType == "" && sourceEventType != "" {
		record.EventType = eventbus.EventType(sourceEventType)
	}
	return cloneDeliveryEntry(&DeliveryEntry{
		ID:             id,
		AdapterName:    adapterName,
		IdempotencyKey: idempotencyKey,
		Status:         DeliveryStatus(status),
		Record:         record,
		Attempts:       attempts,
		MaxAttempts:    maxAttempts,
		LastError:      lastError,
		NextAttemptAt:  parseDeliveryTime(nextAttemptAt),
		LastAttemptAt:  parseDeliveryTime(lastAttemptAt),
		CreatedAt:      parseDeliveryTime(createdAt),
		UpdatedAt:      parseDeliveryTime(updatedAt),
		DeliveredAt:    parseDeliveryTime(deliveredAt),
	}), nil
}

func formatDeliveryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDeliveryTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}

func dedupeDeliveryIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
