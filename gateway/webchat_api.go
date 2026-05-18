package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// uploadMaxFileSize is the maximum allowed upload size (10 MB).
	uploadMaxFileSize = 10 << 20

	// uploadTempPrefix is the directory prefix for temporary upload storage.
	uploadTempPrefix = "hopclaw-webchat-upload-"

	// uploadCleanupInterval is how often stale uploads are cleaned.
	uploadCleanupInterval = 15 * time.Minute

	// uploadMaxAge is how long uploaded files are kept before cleanup.
	uploadMaxAge = 1 * time.Hour

	// attachmentRefIDLen is the byte length of random hex IDs for attachments.
	attachmentRefIDLen = 16

	// sseSessionFilterParam is the query parameter for session-scoped SSE.
	sseSessionFilterParam = "session_id"

	// sseEventBufferSize is the subscription buffer for session-scoped SSE.
	sseEventBufferSize = 128
)

// ---------------------------------------------------------------------------
// Upload response types
// ---------------------------------------------------------------------------

type uploadResponse struct {
	OK            bool   `json:"ok"`
	AttachmentRef string `json:"attachment_ref"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	ContentType   string `json:"content_type"`
}

// ---------------------------------------------------------------------------
// Upload store (temporary file storage with cleanup)
// ---------------------------------------------------------------------------

type uploadEntry struct {
	Path        string
	Filename    string
	ContentType string
	Size        int64
	CreatedAt   time.Time
}

type uploadStore struct {
	mu      sync.Mutex // guards entries
	entries map[string]*uploadEntry
	dir     string
}

func newUploadStore() (*uploadStore, error) {
	dir, err := os.MkdirTemp("", uploadTempPrefix)
	if err != nil {
		return nil, fmt.Errorf("create upload temp dir: %w", err)
	}
	return &uploadStore{
		entries: make(map[string]*uploadEntry),
		dir:     dir,
	}, nil
}

func (u *uploadStore) store(filename, contentType string, data []byte) (string, error) {
	id, err := generateAttachmentID()
	if err != nil {
		return "", fmt.Errorf("generate attachment id: %w", err)
	}

	ext := filepath.Ext(filename)
	safeName := id + ext
	path := filepath.Join(u.dir, safeName)

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write upload file: %w", err)
	}

	u.mu.Lock()
	u.entries[id] = &uploadEntry{
		Path:        path,
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(data)),
		CreatedAt:   time.Now().UTC(),
	}
	u.mu.Unlock()

	return id, nil
}

func (u *uploadStore) cleanup() {
	u.mu.Lock()
	defer u.mu.Unlock()

	cutoff := time.Now().UTC().Add(-uploadMaxAge)
	for id, entry := range u.entries {
		if entry.CreatedAt.Before(cutoff) {
			_ = os.Remove(entry.Path)
			delete(u.entries, id)
		}
	}
}

func generateAttachmentID() (string, error) {
	b := make([]byte, attachmentRefIDLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// WebChat page is served as static files via webChatHandler() in webchat.go.
// Config is served via handleWebChatConfig().
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// handleWebChatSSE serves SSE events filtered to a specific session.
// ---------------------------------------------------------------------------

func (g *Gateway) handleWebChatSSE(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "runtime not available"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		gwJSON(w, http.StatusInternalServerError, errorResponse{Error: "streaming not supported"})
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get(sseSessionFilterParam))

	sub := g.runtime.SubscribeEvents(sseEventBufferSize)
	if sub == nil {
		gwJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "event streaming not available"})
		return
	}
	defer sub.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events():
			if !ok {
				return
			}
			// If session filter is specified, only forward matching events.
			if sessionID != "" && event.SessionID != "" && event.SessionID != sessionID {
				continue
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ---------------------------------------------------------------------------
// handleWebChatUpload handles file uploads and returns attachment references.
// ---------------------------------------------------------------------------

func (g *Gateway) handleWebChatUpload(w http.ResponseWriter, r *http.Request) {
	if g.uploads == nil {
		gwJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "file upload not available"})
		return
	}

	if err := r.ParseMultipartForm(uploadMaxFileSize); err != nil {
		gwJSON(w, http.StatusBadRequest, errorResponse{Error: "file too large or invalid multipart form"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		gwJSON(w, http.StatusBadRequest, errorResponse{Error: "missing file field"})
		return
	}
	defer file.Close()

	if header.Size > uploadMaxFileSize {
		gwJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "file exceeds maximum size"})
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, uploadMaxFileSize+1))
	if err != nil {
		gwJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to read file"})
		return
	}
	if int64(len(data)) > uploadMaxFileSize {
		gwJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "file exceeds maximum size"})
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	attachID, err := g.uploads.store(header.Filename, contentType, data)
	if err != nil {
		logging.FromContext(r.Context()).Warn("webchat upload failed", "error", err)
		gwJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to store file"})
		return
	}

	gwJSON(w, http.StatusOK, uploadResponse{
		OK:            true,
		AttachmentRef: attachID,
		Filename:      header.Filename,
		Size:          int64(len(data)),
		ContentType:   contentType,
	})
}

// ---------------------------------------------------------------------------
// Upload lifecycle management
// ---------------------------------------------------------------------------

// initWebChatUploads initializes the upload store and starts cleanup goroutine.
func (g *Gateway) initWebChatUploads() {
	store, err := newUploadStore()
	if err != nil {
		log.Warn("webchat upload store initialization failed, file uploads disabled", "error", err)
		return
	}
	g.uploads = store

	go func() {
		ticker := time.NewTicker(uploadCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			if g.uploads != nil {
				g.uploads.cleanup()
			}
		}
	}()
}
