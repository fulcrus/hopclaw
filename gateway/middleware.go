package gateway

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

const (
	defaultCORSMaxAge        = 86400 // 24 hours
	defaultRatePerSecond     = 20.0
	defaultRateBurst         = 50
	rateLimitCleanupInterval = 5 * time.Minute
	rateLimitBucketExpiry    = 10 * time.Minute
	requestIDBytes           = 8
	hstsMaxAge               = "max-age=63072000; includeSubDomains" // 2 years

	headerContentType   = "Content-Type"
	headerRequestID     = "X-Request-ID"
	headerXForwardedFor = "X-Forwarded-For"
	headerXRealIP       = "X-Real-IP"

	readinessBooting   = "booting"
	readinessReady     = "ready"
	readinessUnhealthy = "unhealthy"
)

// ---------------------------------------------------------------------------
// Security headers
// ---------------------------------------------------------------------------

// SecurityHeaders adds standard security response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", hstsMaxAge)
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

// CORSConfig configures cross-origin resource sharing.
// Zero values use sensible defaults: all standard methods, common headers, 24h max-age.
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int // seconds; 0 = defaultCORSMaxAge (86400)
}

// CORS adds CORS headers based on configuration.
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	if len(cfg.AllowedHeaders) == 0 {
		cfg.AllowedHeaders = []string{headerContentType, "Authorization", headerRequestID, "X-HopClaw-Token", "X-OpenClaw-Token"}
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = defaultCORSMaxAge
	}

	allowedSet, allowAll := buildAllowedOriginSet(cfg.AllowedOrigins)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if allowAll || allowedSet[strings.ToLower(origin)] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
			}

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(cfg.MaxAge))
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func buildAllowedOriginSet(origins []string) (map[string]bool, bool) {
	allowedSet := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowedSet[strings.ToLower(strings.TrimSpace(o))] = true
	}
	return allowedSet, allowedSet["*"]
}

func websocketOriginAllowed(r *http.Request, allowedOrigins []string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	allowedSet, allowAll := buildAllowedOriginSet(allowedOrigins)
	if allowAll || allowedSet[strings.ToLower(origin)] {
		return true
	}
	return requestOriginMatchesHost(r, origin)
}

func requestOriginMatchesHost(r *http.Request, origin string) bool {
	if r == nil {
		return false
	}
	parsed, err := neturl.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	originHost, originPort := canonicalHostPort(parsed.Host, parsed.Scheme)
	requestHost, requestPort := canonicalHostPort(r.Host, requestScheme(r))
	return strings.EqualFold(originHost, requestHost) && originPort == requestPort
}

func canonicalHostPort(hostport, scheme string) (string, string) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", ""
	}
	host := hostport
	port := ""
	if parsedHost, parsedPort, err := net.SplitHostPort(hostport); err == nil {
		host, port = parsedHost, parsedPort
	} else if strings.Count(hostport, ":") > 1 && strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		host = strings.Trim(hostport, "[]")
	}
	host = strings.Trim(host, "[]")
	if port == "" {
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "https", "wss":
			port = "443"
		default:
			port = "80"
		}
	}
	return strings.ToLower(host), port
}

func requestScheme(r *http.Request) string {
	if r == nil {
		return "http"
	}
	if r.TLS != nil {
		return "https"
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return "https"
	}
	return "http"
}

// ---------------------------------------------------------------------------
// Request ID
// ---------------------------------------------------------------------------

// RequestID adds a unique X-Request-ID header to each request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerRequestID)
		if id == "" {
			id = generateRequestID()
		}
		w.Header().Set(headerRequestID, id)
		ctx := logging.WithRequestID(r.Context(), id)
		ctx = logging.WithTraceID(ctx, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateRequestID() string {
	b := make([]byte, requestIDBytes)
	_, _ = rand.Read(b)
	return "req-" + hex.EncodeToString(b)
}

// MetricsMiddleware records HTTP request duration and in-flight count.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.HTTPRequestsInFlight.Inc()
		defer metrics.HTTPRequestsInFlight.Dec()

		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		route := r.Method + " " + r.URL.Path
		if parts := strings.SplitN(r.URL.Path, "/", 5); len(parts) >= 4 {
			route = r.Method + " /" + parts[1] + "/" + parts[2]
			if parts[3] != "" {
				route += "/:id"
			}
		}

		metrics.HTTPRequestDuration.WithLabelValues(
			r.Method,
			route,
			strconv.Itoa(sw.status),
		).Observe(time.Since(start).Seconds())
	})
}

// statusWriter captures the HTTP status code for metrics.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	flusher.Flush()
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *statusWriter) ReadFrom(reader io.Reader) (int64, error) {
	rf, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(w.ResponseWriter, reader)
	}
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return rf.ReadFrom(reader)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// ---------------------------------------------------------------------------
// Rate limiter (token bucket per IP)
// ---------------------------------------------------------------------------

// RateLimitConfig configures per-IP rate limiting.
// Zero values use sensible defaults: 10 req/s sustained, burst of 20.
type RateLimitConfig struct {
	RequestsPerSecond float64 // sustained rate; 0 = defaultRatePerSecond (10)
	BurstSize         int     // maximum burst capacity; 0 = defaultRateBurst (20)
}

type rateLimiter struct {
	mu      sync.Mutex // guards buckets
	buckets map[string]*tokenBucket
	rate    float64
	burst   int
}

type tokenBucket struct {
	tokens   float64
	lastFill time.Time
}

// RateLimit enforces per-IP rate limiting via token bucket.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	if cfg.RequestsPerSecond <= 0 {
		cfg.RequestsPerSecond = defaultRatePerSecond
	}
	if cfg.BurstSize <= 0 {
		cfg.BurstSize = defaultRateBurst
	}
	rl := &rateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    cfg.RequestsPerSecond,
		burst:   cfg.BurstSize,
	}

	// Periodic cleanup of stale buckets.
	go func() {
		ticker := time.NewTicker(rateLimitCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for console UI assets.
			if strings.HasPrefix(r.URL.Path, "/dashboard") || strings.HasPrefix(r.URL.Path, "/webchat") {
				next.ServeHTTP(w, r)
				return
			}
			ip := extractIP(r)
			if !rl.allow(ip) {
				log.Warn("rate limit exceeded", "ip", ip, "path", r.URL.Path)
				gwError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, ok := rl.buckets[key]
	if !ok {
		bucket = &tokenBucket{tokens: float64(rl.burst), lastFill: now}
		rl.buckets[key] = bucket
	}

	elapsed := now.Sub(bucket.lastFill).Seconds()
	bucket.tokens += elapsed * rl.rate
	if bucket.tokens > float64(rl.burst) {
		bucket.tokens = float64(rl.burst)
	}
	bucket.lastFill = now

	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rateLimitBucketExpiry)
	for key, bucket := range rl.buckets {
		if bucket.lastFill.Before(cutoff) {
			delete(rl.buckets, key)
		}
	}
}

func extractIP(r *http.Request) string {
	if xff := r.Header.Get(headerXForwardedFor); xff != "" {
		if parts := strings.SplitN(xff, ",", 2); len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if xri := r.Header.Get(headerXRealIP); xri != "" {
		return strings.TrimSpace(xri)
	}
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

// ReadinessState tracks service readiness.
type ReadinessState struct {
	mu    sync.RWMutex
	state string // readinessBooting, readinessReady, "unhealthy"
	since time.Time
}

// NewReadinessState creates a new readiness tracker starting in readinessBooting state.
func NewReadinessState() *ReadinessState {
	return &ReadinessState{state: readinessBooting, since: time.Now().UTC()}
}

// SetReady transitions to readinessReady state.
func (rs *ReadinessState) SetReady() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.state = readinessReady
	rs.since = time.Now().UTC()
}

// SetUnhealthy transitions to "unhealthy" state.
func (rs *ReadinessState) SetUnhealthy() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.state = "unhealthy"
	rs.since = time.Now().UTC()
}

// Get returns current state and the time it was entered.
func (rs *ReadinessState) Get() (string, time.Time) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.state, rs.since
}

type readinessResponse struct {
	State string `json:"state"`
	Since string `json:"since"`
}

// ReadinessHandler returns an HTTP handler for liveness/readiness probes.
func ReadinessHandler(rs *ReadinessState) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, since := rs.Get()
		w.Header().Set(headerContentType, "application/json")
		if state != readinessReady {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		logging.LogIfErr(r.Context(), json.NewEncoder(w).Encode(readinessResponse{
			State: state,
			Since: since.Format(time.RFC3339),
		}), "write readiness response failed")
	})
}
