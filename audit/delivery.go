package audit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/fulcrus/hopclaw/logging"
)

var deliveryLog = logging.WithSubsystem("audit.delivery")

type DeliveryStatus string

const (
	DeliveryStatusPending    DeliveryStatus = "pending"
	DeliveryStatusDelivered  DeliveryStatus = "delivered"
	DeliveryStatusDeadLetter DeliveryStatus = "dead_letter"
)

var ErrDeliveryNotFound = errors.New("audit delivery not found")

type DeliveryEntry struct {
	ID            string             `json:"id"`
	SinkName      string             `json:"sink_name"`
	EventID       string             `json:"event_id,omitempty"`
	EventType     eventbus.EventType `json:"event_type,omitempty"`
	RunID         string             `json:"run_id,omitempty"`
	SessionID     string             `json:"session_id,omitempty"`
	Event         eventbus.Event     `json:"event"`
	Status        DeliveryStatus     `json:"status"`
	Attempts      int                `json:"attempts,omitempty"`
	MaxAttempts   int                `json:"max_attempts,omitempty"`
	LastError     string             `json:"last_error,omitempty"`
	NextAttemptAt time.Time          `json:"next_attempt_at,omitempty"`
	LastAttemptAt time.Time          `json:"last_attempt_at,omitempty"`
	CreatedAt     time.Time          `json:"created_at,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at,omitempty"`
	DeliveredAt   time.Time          `json:"delivered_at,omitempty"`
}

type DeliveryListFilter struct {
	Status    DeliveryStatus     `json:"status,omitempty"`
	SinkName  string             `json:"sink_name,omitempty"`
	RunID     string             `json:"run_id,omitempty"`
	SessionID string             `json:"session_id,omitempty"`
	EventType eventbus.EventType `json:"event_type,omitempty"`
	Query     string             `json:"q,omitempty"`
	Limit     int                `json:"limit,omitempty"`
}

type DeliveryRedriveOptions struct {
	ResetAttempts bool `json:"reset_attempts,omitempty"`
	ClearError    bool `json:"clear_error,omitempty"`
}

type DeliveryStats struct {
	Total              int                    `json:"total"`
	ByStatus           map[DeliveryStatus]int `json:"by_status,omitempty"`
	Delivered          int                    `json:"delivered"`
	Pending            int                    `json:"pending"`
	DeadLetter         int                    `json:"dead_letter"`
	OldestPendingAt    time.Time              `json:"oldest_pending_at,omitempty"`
	OldestDeadLetterAt time.Time              `json:"oldest_dead_letter_at,omitempty"`
}

type DeliverySink interface {
	Name() string
	Deliver(ctx context.Context, event eventbus.Event) error
}

type DeliveryController interface {
	GetDelivery(ctx context.Context, id string) (*DeliveryEntry, error)
	ListDeliveries(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error)
	GetDeliveryStats(ctx context.Context, filter DeliveryListFilter) (DeliveryStats, error)
	Redrive(ctx context.Context, ids []string, opts DeliveryRedriveOptions) ([]*DeliveryEntry, error)
}

type DeliveryStore interface {
	Enqueue(ctx context.Context, entry DeliveryEntry) (*DeliveryEntry, error)
	Due(ctx context.Context, now time.Time, limit int) ([]*DeliveryEntry, error)
	Save(ctx context.Context, entry *DeliveryEntry) error
	Get(ctx context.Context, id string) (*DeliveryEntry, error)
	List(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error)
	Stats(ctx context.Context, filter DeliveryListFilter) (DeliveryStats, error)
	Redrive(ctx context.Context, ids []string, opts DeliveryRedriveOptions) ([]*DeliveryEntry, error)
}

type DeliveryConfig struct {
	MaxAttempts  int
	BaseBackoff  time.Duration
	MaxBackoff   time.Duration
	PollInterval time.Duration
	BatchSize    int
}

func (c DeliveryConfig) normalized() DeliveryConfig {
	out := c
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = 8
	}
	if out.BaseBackoff <= 0 {
		out.BaseBackoff = 5 * time.Second
	}
	if out.MaxBackoff <= 0 {
		out.MaxBackoff = 5 * time.Minute
	}
	if out.MaxBackoff < out.BaseBackoff {
		out.MaxBackoff = out.BaseBackoff
	}
	if out.PollInterval <= 0 {
		out.PollInterval = 2 * time.Second
	}
	if out.BatchSize <= 0 {
		out.BatchSize = 32
	}
	return out
}

type ReliableDispatcher struct {
	config       DeliveryConfig
	store        DeliveryStore
	targets      []DeliverySink
	targetByName map[string]DeliverySink

	mu     sync.Mutex
	cancel context.CancelFunc
	wakeCh chan struct{}
}

func NewReliableDispatcher(cfg DeliveryConfig, store DeliveryStore, sinks ...DeliverySink) *ReliableDispatcher {
	cfg = cfg.normalized()
	if store == nil {
		store = NewInMemoryDeliveryStore()
	}
	targets := make([]DeliverySink, 0, len(sinks))
	targetByName := make(map[string]DeliverySink, len(sinks))
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		name := strings.TrimSpace(sink.Name())
		if name == "" {
			continue
		}
		targets = append(targets, sink)
		targetByName[strings.ToLower(name)] = sink
	}
	return &ReliableDispatcher{
		config:       cfg,
		store:        store,
		targets:      targets,
		targetByName: targetByName,
		wakeCh:       make(chan struct{}, 1),
	}
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
	if ctx == nil {
		ctx = context.Background()
	}
	loopCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.mu.Unlock()
	go d.loop(loopCtx)
}

func (d *ReliableDispatcher) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	cancel := d.cancel
	d.cancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *ReliableDispatcher) Handle(ctx context.Context, event eventbus.Event) error {
	if d == nil || len(d.targets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	if event.Time.IsZero() {
		event.Time = now
	}
	for _, sink := range d.targets {
		sinkName := strings.TrimSpace(sink.Name())
		entry := DeliveryEntry{
			SinkName:      sinkName,
			EventID:       strings.TrimSpace(event.ID),
			EventType:     event.Type,
			RunID:         strings.TrimSpace(event.RunID),
			SessionID:     strings.TrimSpace(event.SessionID),
			Event:         cloneEvent(event),
			Status:        DeliveryStatusPending,
			MaxAttempts:   d.config.MaxAttempts,
			NextAttemptAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if _, err := d.store.Enqueue(ctx, entry); err != nil {
			return err
		}
		metrics.AuditDeliveryQueuedTotal.WithLabelValues(defaultAuditMetricSinkLabel(sinkName)).Inc()
	}
	d.wake()
	return nil
}

func (d *ReliableDispatcher) GetDelivery(ctx context.Context, id string) (*DeliveryEntry, error) {
	if d == nil || d.store == nil {
		return nil, ErrDeliveryNotFound
	}
	return d.store.Get(ctx, id)
}

func (d *ReliableDispatcher) ListDeliveries(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error) {
	if d == nil || d.store == nil {
		return nil, nil
	}
	return d.store.List(ctx, filter)
}

func (d *ReliableDispatcher) GetDeliveryStats(ctx context.Context, filter DeliveryListFilter) (DeliveryStats, error) {
	if d == nil || d.store == nil {
		return DeliveryStats{}, nil
	}
	return d.store.Stats(ctx, filter)
}

func (d *ReliableDispatcher) Redrive(ctx context.Context, ids []string, opts DeliveryRedriveOptions) ([]*DeliveryEntry, error) {
	if d == nil || d.store == nil {
		return nil, nil
	}
	items, err := d.store.Redrive(ctx, ids, opts)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		d.wake()
	}
	return items, nil
}

func (d *ReliableDispatcher) loop(ctx context.Context) {
	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()
	for {
		d.processDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.wakeCh:
		}
	}
}

func (d *ReliableDispatcher) processDue(ctx context.Context) {
	if d == nil || d.store == nil {
		return
	}
	for {
		items, err := d.store.Due(ctx, time.Now().UTC(), d.config.BatchSize)
		if err != nil {
			deliveryLog.WarnContext(ctx, "audit delivery due query failed", "error", err)
			return
		}
		if len(items) == 0 {
			return
		}
		for _, item := range items {
			if item == nil {
				continue
			}
			d.deliverOne(ctx, item)
		}
		if len(items) < d.config.BatchSize {
			return
		}
	}
}

func (d *ReliableDispatcher) deliverOne(ctx context.Context, entry *DeliveryEntry) {
	if d == nil || entry == nil {
		return
	}
	sink := d.targetByName[strings.ToLower(strings.TrimSpace(entry.SinkName))]
	if sink == nil {
		entry.Status = DeliveryStatusDeadLetter
		entry.LastError = "audit sink not registered"
		entry.LastAttemptAt = time.Now().UTC()
		entry.UpdatedAt = entry.LastAttemptAt
		if entry.Attempts < entry.MaxAttempts {
			entry.Attempts = entry.MaxAttempts
		}
		_ = d.store.Save(ctx, entry)
		metrics.AuditDeliveryAttemptsTotal.WithLabelValues(defaultAuditMetricSinkLabel(entry.SinkName), "dead_letter").Inc()
		return
	}
	now := time.Now().UTC()
	if err := sink.Deliver(ctx, cloneEvent(entry.Event)); err != nil {
		entry.Attempts++
		entry.LastAttemptAt = now
		entry.UpdatedAt = now
		entry.LastError = strings.TrimSpace(err.Error())
		if entry.Attempts >= entry.MaxAttempts {
			entry.Status = DeliveryStatusDeadLetter
			entry.DeliveredAt = time.Time{}
			metrics.AuditDeliveryAttemptsTotal.WithLabelValues(defaultAuditMetricSinkLabel(entry.SinkName), "dead_letter").Inc()
		} else {
			entry.Status = DeliveryStatusPending
			entry.NextAttemptAt = now.Add(computeBackoff(d.config.BaseBackoff, d.config.MaxBackoff, entry.Attempts))
			metrics.AuditDeliveryAttemptsTotal.WithLabelValues(defaultAuditMetricSinkLabel(entry.SinkName), "retry_scheduled").Inc()
		}
		if saveErr := d.store.Save(ctx, entry); saveErr != nil {
			deliveryLog.WarnContext(ctx, "save failed audit delivery state", "id", entry.ID, "error", saveErr)
		}
		return
	}
	entry.Attempts++
	entry.Status = DeliveryStatusDelivered
	entry.LastError = ""
	entry.LastAttemptAt = now
	entry.DeliveredAt = now
	entry.UpdatedAt = now
	metrics.AuditDeliveryAttemptsTotal.WithLabelValues(defaultAuditMetricSinkLabel(entry.SinkName), "delivered").Inc()
	if saveErr := d.store.Save(ctx, entry); saveErr != nil {
		deliveryLog.WarnContext(ctx, "save delivered audit record failed", "id", entry.ID, "error", saveErr)
	}
}

func (d *ReliableDispatcher) wake() {
	if d == nil {
		return
	}
	select {
	case d.wakeCh <- struct{}{}:
	default:
	}
}

type InMemoryDeliveryStore struct {
	mu          sync.RWMutex
	nextID      atomic.Uint64
	entries     map[string]*DeliveryEntry
	order       []string
	bySinkEvent map[string]string
}

func NewInMemoryDeliveryStore() *InMemoryDeliveryStore {
	return &InMemoryDeliveryStore{
		entries:     map[string]*DeliveryEntry{},
		bySinkEvent: map[string]string{},
	}
}

func (s *InMemoryDeliveryStore) Enqueue(_ context.Context, entry DeliveryEntry) (*DeliveryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sinkEventKey(entry.SinkName, entry.EventID)
	if key != "" {
		if existingID, ok := s.bySinkEvent[key]; ok {
			existing := s.entries[existingID]
			return cloneDeliveryEntry(existing), nil
		}
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.CreatedAt
	}
	if entry.NextAttemptAt.IsZero() {
		entry.NextAttemptAt = entry.CreatedAt
	}
	if entry.Status == "" {
		entry.Status = DeliveryStatusPending
	}
	entry.ID = fmt.Sprintf("adel-%06d", s.nextID.Add(1))
	cloned := cloneDeliveryEntryValue(entry)
	s.entries[entry.ID] = &cloned
	s.order = append(s.order, entry.ID)
	if key != "" {
		s.bySinkEvent[key] = entry.ID
	}
	return cloneDeliveryEntry(&cloned), nil
}

func (s *InMemoryDeliveryStore) Due(_ context.Context, now time.Time, limit int) ([]*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*DeliveryEntry, 0, len(s.entries))
	for _, id := range s.order {
		entry := s.entries[id]
		if entry == nil || entry.Status != DeliveryStatusPending {
			continue
		}
		if entry.NextAttemptAt.IsZero() || entry.NextAttemptAt.After(now) {
			continue
		}
		items = append(items, cloneDeliveryEntry(entry))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].NextAttemptAt.Equal(items[j].NextAttemptAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].NextAttemptAt.Before(items[j].NextAttemptAt)
	})
	return applyLimit(items, limit), nil
}

func (s *InMemoryDeliveryStore) Save(_ context.Context, entry *DeliveryEntry) error {
	if entry == nil {
		return ErrDeliveryNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[entry.ID]; !ok {
		return ErrDeliveryNotFound
	}
	cloned := cloneDeliveryEntryValue(*entry)
	s.entries[entry.ID] = &cloned
	return nil
}

func (s *InMemoryDeliveryStore) Get(_ context.Context, id string) (*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[strings.TrimSpace(id)]
	if !ok {
		return nil, ErrDeliveryNotFound
	}
	return cloneDeliveryEntry(entry), nil
}

func (s *InMemoryDeliveryStore) List(_ context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*DeliveryEntry, 0, len(s.entries))
	for _, id := range s.order {
		entry := s.entries[id]
		if entry == nil || !matchesDeliveryFilter(entry, filter) {
			continue
		}
		items = append(items, cloneDeliveryEntry(entry))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return applyLimit(items, filter.Limit), nil
}

func (s *InMemoryDeliveryStore) Stats(ctx context.Context, filter DeliveryListFilter) (DeliveryStats, error) {
	items, err := s.List(ctx, DeliveryListFilter{
		Status:    filter.Status,
		SinkName:  filter.SinkName,
		RunID:     filter.RunID,
		SessionID: filter.SessionID,
		EventType: filter.EventType,
		Query:     filter.Query,
	})
	if err != nil {
		return DeliveryStats{}, err
	}
	return summarizeDeliveries(items), nil
}

func (s *InMemoryDeliveryStore) Redrive(_ context.Context, ids []string, opts DeliveryRedriveOptions) ([]*DeliveryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var updated []*DeliveryEntry
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		entry, ok := s.entries[id]
		if !ok || entry == nil || entry.Status == DeliveryStatusDelivered {
			continue
		}
		entry.Status = DeliveryStatusPending
		entry.NextAttemptAt = time.Now().UTC()
		entry.UpdatedAt = entry.NextAttemptAt
		entry.DeliveredAt = time.Time{}
		if opts.ResetAttempts {
			entry.Attempts = 0
		}
		if opts.ClearError {
			entry.LastError = ""
		}
		updated = append(updated, cloneDeliveryEntry(entry))
	}
	return updated, nil
}

func computeBackoff(base, max time.Duration, attempts int) time.Duration {
	if attempts <= 0 {
		return base
	}
	delay := base
	for i := 1; i < attempts && delay < max; i++ {
		delay *= 2
		if delay > max {
			delay = max
		}
	}
	return delay
}

func summarizeDeliveries(items []*DeliveryEntry) DeliveryStats {
	stats := DeliveryStats{
		ByStatus: map[DeliveryStatus]int{},
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		stats.Total++
		stats.ByStatus[item.Status]++
		switch item.Status {
		case DeliveryStatusDelivered:
			stats.Delivered++
		case DeliveryStatusPending:
			stats.Pending++
			if stats.OldestPendingAt.IsZero() || (!item.CreatedAt.IsZero() && item.CreatedAt.Before(stats.OldestPendingAt)) {
				stats.OldestPendingAt = item.CreatedAt
			}
		case DeliveryStatusDeadLetter:
			stats.DeadLetter++
			if stats.OldestDeadLetterAt.IsZero() || (!item.CreatedAt.IsZero() && item.CreatedAt.Before(stats.OldestDeadLetterAt)) {
				stats.OldestDeadLetterAt = item.CreatedAt
			}
		}
	}
	if len(stats.ByStatus) == 0 {
		stats.ByStatus = nil
	}
	return stats
}

func matchesDeliveryFilter(entry *DeliveryEntry, filter DeliveryListFilter) bool {
	if entry == nil {
		return false
	}
	if filter.Status != "" && entry.Status != filter.Status {
		return false
	}
	if name := strings.TrimSpace(filter.SinkName); name != "" && !strings.EqualFold(entry.SinkName, name) {
		return false
	}
	if runID := strings.TrimSpace(filter.RunID); runID != "" && entry.RunID != runID {
		return false
	}
	if sessionID := strings.TrimSpace(filter.SessionID); sessionID != "" && entry.SessionID != sessionID {
		return false
	}
	if filter.EventType != "" && entry.EventType != filter.EventType {
		return false
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if query == "" {
		return true
	}
	for _, candidate := range []string{
		entry.ID,
		entry.SinkName,
		entry.EventID,
		string(entry.EventType),
		entry.RunID,
		entry.SessionID,
		entry.LastError,
	} {
		if strings.Contains(strings.ToLower(candidate), query) {
			return true
		}
	}
	return false
}

func applyLimit[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func sinkEventKey(sinkName, eventID string) string {
	sinkName = strings.ToLower(strings.TrimSpace(sinkName))
	eventID = strings.TrimSpace(eventID)
	if sinkName == "" || eventID == "" {
		return ""
	}
	return sinkName + "|" + eventID
}

func cloneDeliveryEntry(entry *DeliveryEntry) *DeliveryEntry {
	if entry == nil {
		return nil
	}
	cloned := cloneDeliveryEntryValue(*entry)
	return &cloned
}

func cloneDeliveryEntryValue(entry DeliveryEntry) DeliveryEntry {
	entry.Event = cloneEvent(entry.Event)
	return entry
}

func defaultAuditMetricSinkLabel(name string) string {
	if strings.TrimSpace(name) == "" {
		return "unnamed"
	}
	return strings.TrimSpace(name)
}
