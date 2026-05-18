package eventbus

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("eventbus")

const (
	scannerInitBuf = 256 * 1024  // initial scanner buffer size
	scannerMaxBuf  = 1024 * 1024 // max scanner token size (1 MB)
)

// FileEventLog persists events as newline-delimited JSON (JSONL).
// It implements Sink for writing and supports replay for reading.
type FileEventLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	enc  *json.Encoder
	max  int64 // max file size in bytes before rotation; 0 = no limit
}

// FileEventLogConfig configures the file event log.
type FileEventLogConfig struct {
	Path        string // Path to the JSONL file.
	MaxFileSize int64  // Max file size in bytes before rotation (0 = unlimited).
}

// NewFileEventLog creates a new append-only event log.
func NewFileEventLog(cfg FileEventLogConfig) (*FileEventLog, error) {
	f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &FileEventLog{
		path: cfg.Path,
		file: f,
		enc:  json.NewEncoder(f),
		max:  cfg.MaxFileSize,
	}, nil
}

// Handle implements Sink — called for every published event.
func (l *FileEventLog) Handle(_ context.Context, event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return nil
	}

	// Check rotation before writing.
	if l.max > 0 {
		if info, err := l.file.Stat(); err == nil && info.Size() >= l.max {
			l.rotate()
		}
	}

	return l.enc.Encode(event)
}

// Replay reads all persisted events from the log file.
// New callers should prefer ReplayContext so replay can be cancelled.
func (l *FileEventLog) Replay() ([]Event, error) {
	return l.ReplayContext(context.Background())
}

// ReplayContext reads all persisted events from the log file with caller
// cancellation support.
func (l *FileEventLog) ReplayContext(ctx context.Context) ([]Event, error) {
	l.mu.Lock()
	path := l.path
	l.mu.Unlock()

	return readEventsFromFile(ctx, path)
}

// ReplaySince returns events after the given cursor ID.
// If sinceID is empty, all events are returned. New callers should prefer
// ReplaySinceContext so replay can be cancelled.
func (l *FileEventLog) ReplaySince(sinceID string, limit int) ([]Event, error) {
	return l.ReplaySinceContext(context.Background(), sinceID, limit)
}

// ReplaySinceContext returns events after the given cursor ID with caller
// cancellation support. If sinceID is empty, all events are returned.
func (l *FileEventLog) ReplaySinceContext(ctx context.Context, sinceID string, limit int) ([]Event, error) {
	all, err := l.ReplayContext(ctx)
	if err != nil {
		return nil, err
	}

	if sinceID == "" {
		if limit > 0 && len(all) > limit {
			return all[len(all)-limit:], nil
		}
		return all, nil
	}

	start := 0
	for i, e := range all {
		if e.ID == sinceID {
			start = i + 1
			break
		}
	}

	result := all[start:]
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// Close flushes and closes the log file.
func (l *FileEventLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		l.enc = nil
		return err
	}
	return nil
}

// rotate renames the current file to .old and opens a fresh one.
// Must be called with l.mu held.
func (l *FileEventLog) rotate() {
	logging.DebugIfErr(l.file.Close(), "close event log file failed")
	logging.DebugIfErr(os.Rename(l.path, l.path+".old"), "rename event log file failed")
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		l.file = nil
		l.enc = nil
		return
	}
	l.file = f
	l.enc = json.NewEncoder(f)
}

func readEventsFromFile(ctx context.Context, path string) ([]Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			log.Warn("event log: skipping corrupted line", "error", err, "path", path)
			continue
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return events, err
	}
	return events, nil
}
