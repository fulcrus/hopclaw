package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/config"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/fulcrus/hopclaw/keychain"
)

const (
	serveInstanceHeartbeatInterval = 5 * time.Second
	serveInstanceStaleAfter        = 20 * time.Second
	localTargetName                = "local"
)

type interactiveTargetKind string

const (
	interactiveTargetLocal  interactiveTargetKind = "local"
	interactiveTargetRemote interactiveTargetKind = "remote"
)

type interactiveTarget struct {
	Kind        interactiveTargetKind
	Name        string
	BaseURL     string
	AuthType    string
	AuthToken   string
	AuthRef     string
	Insecure    bool
	Description string
	ConfigPath  string
}

func localSessionInteractiveTarget() interactiveTarget {
	return interactiveTarget{
		Kind:        interactiveTargetLocal,
		Name:        localTargetName,
		Description: "Start a private local conversation in this terminal",
	}
}

func isPrivateLocalInteractiveTarget(t interactiveTarget) bool {
	return t.Kind == interactiveTargetLocal && strings.TrimSpace(t.BaseURL) == "" && isBuiltinLocalTargetName(t.Name)
}

func (t interactiveTarget) label() string {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return localTargetName
	}
	return name
}

func isBuiltinLocalTargetName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case localTargetName:
		return true
	default:
		return false
	}
}

func (t interactiveTarget) targetInfo() replpkg.TargetInfo {
	kind := "remote"
	if t.Kind != interactiveTargetRemote {
		kind = "local"
	}
	return replpkg.TargetInfo{
		Name:        t.label(),
		Kind:        kind,
		Description: strings.TrimSpace(t.Description),
	}
}

type localServeInstanceRecord struct {
	InstanceID    string    `json:"instance_id"`
	Name          string    `json:"name"`
	BaseURL       string    `json:"base_url"`
	PID           int       `json:"pid"`
	ConfigPath    string    `json:"config_path,omitempty"`
	Profile       string    `json:"profile,omitempty"`
	AuthMode      string    `json:"auth_mode,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

type localServeInstanceLease struct {
	mu     sync.Mutex
	closed bool
	path   string
	record localServeInstanceRecord
}

func serveInstanceDir() string {
	return filepath.Join(daemon.StateDir(), "instances")
}

func registerServeInstance(ctx context.Context, cfg config.Config, configPath, requestedName string) (*localServeInstanceLease, error) {
	if err := daemon.EnsureStateDir(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(serveInstanceDir(), 0o755); err != nil {
		return nil, err
	}

	instanceID, err := randomID("inst")
	if err != nil {
		return nil, err
	}
	name := deriveServeInstanceName(cfg.Server.Address, configPath, requestedName)
	record := localServeInstanceRecord{
		InstanceID: instanceID,
		Name:       name,
		BaseURL:    normalizeGatewayURL(cfg.Server.Address),
		PID:        os.Getpid(),
		ConfigPath: strings.TrimSpace(configPath),
		Profile:    strings.TrimSpace(cfg.Runtime.Profile),
		AuthMode:   deriveServeAuthMode(cfg),
		StartedAt:  time.Now().UTC(),
	}
	record.LastHeartbeat = record.StartedAt

	lease := &localServeInstanceLease{
		path:   filepath.Join(serveInstanceDir(), instanceID+".json"),
		record: record,
	}
	if err := lease.writeLocked(); err != nil {
		return nil, err
	}
	go lease.loop(ctx)
	return lease, nil
}

func (l *localServeInstanceLease) loop(ctx context.Context) {
	ticker := time.NewTicker(serveInstanceHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			l.Close()
			return
		case <-ticker.C:
			l.mu.Lock()
			if l.closed {
				l.mu.Unlock()
				return
			}
			l.record.LastHeartbeat = time.Now().UTC()
			_ = l.writeLocked()
			l.mu.Unlock()
		}
	}
}

func (l *localServeInstanceLease) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	_ = os.Remove(l.path)
}

func (l *localServeInstanceLease) writeLocked() error {
	data, err := json.MarshalIndent(l.record, "", "  ")
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

func deriveServeAuthMode(cfg config.Config) string {
	switch {
	case strings.TrimSpace(cfg.Server.AuthToken) != "":
		return "server_token"
	case strings.TrimSpace(cfg.Auth.BearerToken) != "":
		return "bearer"
	case cfg.Auth.JWT != nil:
		return "jwt"
	case len(cfg.Auth.APIKeys) > 0:
		return "api_key"
	case cfg.Auth.OAuth2 != nil:
		return "oauth2"
	default:
		return "none"
	}
}

func deriveServeInstanceName(address, configPath, requested string) string {
	if name := strings.TrimSpace(requested); name != "" {
		return name
	}
	if base := normalizeSuggestedTargetName(strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))); base != "" && !strings.EqualFold(base, "config") {
		return base
	}
	if port := portFromAddress(address); port != "" {
		return "local-" + port
	}
	return "local-serve"
}

func portFromAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.Contains(address, "://") {
		u, err := url.Parse(address)
		if err == nil {
			if port := strings.TrimSpace(u.Port()); port != "" {
				return port
			}
			address = strings.TrimSpace(u.Host)
		}
	}
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		_ = host
		return strings.TrimSpace(port)
	}
	return ""
}

func normalizeGatewayURL(value string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(value), "/")
	if baseURL == "" {
		baseURL = "http://" + defaultGatewayAddr
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return baseURL
}

func randomID(prefix string) (string, error) {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return strings.TrimSpace(prefix) + "-" + hex.EncodeToString(raw[:]), nil
}

func generateInteractiveSessionKey() string {
	var raw [3]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("cli-%s", time.Now().UTC().Format("20060102-150405"))
	}
	return fmt.Sprintf("cli-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(raw[:]))
}

func loadRegisteredLocalTargets(ctx context.Context) ([]interactiveTarget, error) {
	dir := serveInstanceDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	items := make([]interactiveTarget, 0, len(entries))
	byBaseURL := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		recordPath := filepath.Join(dir, entry.Name())
		record, ok := loadLocalServeInstanceRecord(recordPath)
		if !ok {
			continue
		}
		if time.Since(record.LastHeartbeat) > serveInstanceStaleAfter {
			continue
		}
		target, ok := targetFromInstanceRecord(ctx, record)
		if !ok {
			continue
		}
		if _, exists := byBaseURL[target.BaseURL]; exists {
			continue
		}
		byBaseURL[target.BaseURL] = struct{}{}
		items = append(items, target)
	}

	ensureUniqueTargetNames(items)
	targets := make([]interactiveTarget, 0, len(items))
	for _, item := range items {
		targets = append(targets, item)
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
	})
	return targets, nil
}

func loadLocalServeInstanceRecord(path string) (localServeInstanceRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return localServeInstanceRecord{}, false
	}
	var record localServeInstanceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return localServeInstanceRecord{}, false
	}
	if strings.TrimSpace(record.BaseURL) == "" {
		return localServeInstanceRecord{}, false
	}
	return record, true
}

func targetFromInstanceRecord(ctx context.Context, record localServeInstanceRecord) (interactiveTarget, bool) {
	authToken := loadTargetAuthToken(record.ConfigPath)
	client, _, err := newGatewayClientWithOptions(record.BaseURL, authToken, false)
	if err != nil {
		return interactiveTarget{}, false
	}
	if !gatewayHealthy(ctx, client) {
		return interactiveTarget{}, false
	}
	description := strings.TrimSpace(record.BaseURL)
	if profile := strings.TrimSpace(record.Profile); profile != "" {
		description += "   profile=" + profile
	}
	return interactiveTarget{
		Kind:        interactiveTargetLocal,
		Name:        strings.TrimSpace(record.Name),
		BaseURL:     strings.TrimSpace(record.BaseURL),
		AuthToken:   authToken,
		Description: description,
		ConfigPath:  strings.TrimSpace(record.ConfigPath),
	}, true
}

func loadTargetAuthToken(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return ""
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return ""
	}
	return resolveConfigOperatorToken(cfg)
}

func ensureUniqueTargetNames(items []interactiveTarget) {
	seen := map[string]int{}
	for i := range items {
		baseName := strings.TrimSpace(items[i].Name)
		if baseName == "" {
			baseName = "local-serve"
		}
		name := baseName
		if count := seen[baseName]; count > 0 {
			suffix := portFromAddress(items[i].BaseURL)
			if suffix == "" {
				suffix = strconv.Itoa(count + 1)
			}
			name = baseName + "-" + suffix
		}
		seen[baseName]++
		items[i].Name = name
	}
}

func configuredGatewayTarget(ctx context.Context) (interactiveTarget, bool, error) {
	client, err := newConfiguredGatewayClient()
	if err != nil {
		return interactiveTarget{}, false, err
	}
	if !gatewayHealthy(ctx, client) {
		return interactiveTarget{}, false, nil
	}
	baseURL := strings.TrimSpace(client.BaseURL)
	name := deriveConfiguredTargetName(baseURL)
	target := interactiveTarget{
		Kind:        targetKindForBaseURL(baseURL),
		Name:        name,
		BaseURL:     baseURL,
		AuthToken:   strings.TrimSpace(client.AuthToken),
		Description: baseURL,
		ConfigPath:  resolveConfigPath(),
	}
	return target, true, nil
}

func deriveConfiguredTargetName(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "configured"
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "configured"
	}
	if isLoopbackHost(host) {
		if port := strings.TrimSpace(u.Port()); port != "" {
			return "local-" + port
		}
		return "configured-local"
	}
	return host
}

func deriveExplicitRemoteTargetName(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return deriveConfiguredTargetName(baseURL)
	}
	host := strings.TrimSpace(u.Host)
	if host == "" {
		return deriveConfiguredTargetName(baseURL)
	}
	return host
}

func targetKindForBaseURL(baseURL string) interactiveTargetKind {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return interactiveTargetRemote
	}
	if isLoopbackHost(strings.TrimSpace(u.Hostname())) {
		return interactiveTargetLocal
	}
	return interactiveTargetRemote
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func selectableLocalTargets(ctx context.Context) ([]interactiveTarget, error) {
	targets, err := loadRegisteredLocalTargets(ctx)
	if err != nil {
		return nil, err
	}
	configured, ok, err := configuredGatewayTarget(ctx)
	if err != nil {
		return nil, err
	}
	if ok && configured.Kind == interactiveTargetLocal {
		exists := false
		for _, item := range targets {
			if item.BaseURL == configured.BaseURL {
				exists = true
				break
			}
		}
		if !exists {
			targets = append(targets, configured)
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
	})
	return targets, nil
}

func resolveInitialInteractiveTarget(ctx context.Context, in io.Reader, out io.Writer, allowPrompt bool) (interactiveTarget, error) {
	if flagLocal {
		return localSessionInteractiveTarget(), nil
	}
	if target := strings.TrimSpace(flagRemote); target != "" {
		return resolveNamedInteractiveTarget(ctx, target, interactiveTarget{})
	}
	if allowPrompt {
		targets, err := selectableLocalTargets(ctx)
		if err != nil {
			return interactiveTarget{}, err
		}
		if len(targets) > 0 {
			return promptForInteractiveTarget(in, out, targets)
		}
	}
	configured, ok, err := configuredGatewayTarget(ctx)
	if err != nil {
		return interactiveTarget{}, err
	}
	if ok {
		return configured, nil
	}
	return localSessionInteractiveTarget(), nil
}

func resolveNamedInteractiveTarget(ctx context.Context, name string, current interactiveTarget) (interactiveTarget, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return interactiveTarget{}, fmt.Errorf("remote name is required")
	}
	if isBuiltinLocalTargetName(name) {
		return localSessionInteractiveTarget(), nil
	}
	if strings.Contains(name, "://") {
		baseURL := normalizeGatewayURL(name)
		client, _, err := newGatewayClientWithOptions(baseURL, "", false)
		if err != nil {
			return interactiveTarget{}, err
		}
		if !gatewayHealthy(ctx, client) {
			return interactiveTarget{}, fmt.Errorf("remote %q is not reachable", name)
		}
		return interactiveTarget{
			Kind:        interactiveTargetRemote,
			Name:        deriveExplicitRemoteTargetName(baseURL),
			BaseURL:     baseURL,
			AuthToken:   strings.TrimSpace(client.AuthToken),
			Description: baseURL,
		}, nil
	}
	for _, item := range availableInteractiveTargets(ctx, current) {
		if strings.EqualFold(item.Name, name) {
			return item, nil
		}
	}
	return interactiveTarget{}, fmt.Errorf("remote %q not found", name)
}

func availableInteractiveTargets(ctx context.Context, current interactiveTarget) []interactiveTarget {
	targets, _ := selectableLocalTargets(ctx)
	saved := loadSavedRemoteInteractiveTargets()
	seen := map[string]struct{}{
		localTargetName: {},
	}
	all := make([]interactiveTarget, 0, len(targets)+len(saved)+2)
	for _, item := range targets {
		key := strings.ToLower(strings.TrimSpace(item.Name))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		all = append(all, item)
	}
	for _, item := range saved {
		key := strings.ToLower(strings.TrimSpace(item.Name))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		all = append(all, item)
	}
	if current.Kind == interactiveTargetRemote {
		key := strings.ToLower(strings.TrimSpace(current.Name))
		if _, exists := seen[key]; !exists && strings.TrimSpace(current.Name) != "" {
			all = append(all, current)
		}
	}
	all = append(all, localSessionInteractiveTarget())
	sort.Slice(all, func(i, j int) bool {
		if isPrivateLocalInteractiveTarget(all[i]) {
			return false
		}
		if isPrivateLocalInteractiveTarget(all[j]) {
			return true
		}
		return all[i].Name < all[j].Name
	})
	return all
}

func promptForInteractiveTarget(in io.Reader, out io.Writer, targets []interactiveTarget) (interactiveTarget, error) {
	reader := bufio.NewReader(in)
	choices := append([]interactiveTarget(nil), targets...)
	choices = append(choices, localSessionInteractiveTarget())

	header := "Detected local HopClaw runtimes."
	if len(targets) == 1 {
		header = "Detected a local HopClaw runtime."
	}
	fmt.Fprintln(out, header)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Choose where to start:")
	for i, item := range choices {
		line := fmt.Sprintf("%d. %s", i+1, item.label())
		if desc := strings.TrimSpace(item.Description); desc != "" {
			line += "   " + desc
		}
		fmt.Fprintln(out, line)
	}

	for {
		fmt.Fprint(out, "\nSelection: ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return interactiveTarget{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" && errors.Is(err, io.EOF) {
			return localSessionInteractiveTarget(), nil
		}
		if idx, convErr := strconv.Atoi(line); convErr == nil {
			if idx >= 1 && idx <= len(choices) {
				return choices[idx-1], nil
			}
		}
		for _, item := range choices {
			if strings.EqualFold(item.Name, line) {
				return item, nil
			}
		}
		fmt.Fprintln(out, "Please choose a listed number or remote name.")
	}
}

type interactiveTargetManager struct {
	current interactiveTarget
}

func newInteractiveTargetManager(current interactiveTarget) *interactiveTargetManager {
	return &interactiveTargetManager{current: current}
}

func (m *interactiveTargetManager) CurrentTarget() replpkg.TargetInfo {
	return m.current.targetInfo()
}

func (m *interactiveTargetManager) ListTargets(ctx context.Context) ([]replpkg.TargetInfo, error) {
	items := availableInteractiveTargets(ctx, m.current)
	out := make([]replpkg.TargetInfo, 0, len(items))
	for _, item := range items {
		out = append(out, item.targetInfo())
	}
	return out, nil
}

func (m *interactiveTargetManager) SwitchTarget(ctx context.Context, name string) (*replpkg.TargetBinding, error) {
	target, err := resolveNamedInteractiveTarget(ctx, name, m.current)
	if err != nil {
		return nil, err
	}
	if target.label() == m.current.label() && target.Kind == m.current.Kind && target.BaseURL == m.current.BaseURL {
		return nil, fmt.Errorf("already connected to runtime %q", target.label())
	}
	binding, err := newInteractiveTargetBinding(ctx, target, generateInteractiveSessionKey())
	if err != nil {
		return nil, err
	}
	m.current = target
	return binding, nil
}

func (m *interactiveTargetManager) LoginTarget(_ context.Context, name, token string) error {
	_, err := updateSavedTargetCredentials(name, targetAuthUpdate{Token: token})
	return err
}

func (m *interactiveTargetManager) LogoutTarget(_ context.Context, name string) error {
	_, err := clearSavedTargetCredentials(name)
	return err
}

func newInteractiveTargetBinding(ctx context.Context, target interactiveTarget, sessionKey string) (*replpkg.TargetBinding, error) {
	connection, err := openInteractiveTarget(ctx, target, sessionKey)
	if err != nil {
		return nil, err
	}
	if _, err := connection.client.Initialize(ctx, acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo: acp.Implementation{
			Name:    "hopclaw-repl",
			Version: version.Version,
		},
	}); err != nil {
		closeInteractiveConnection(connection)
		return nil, err
	}
	commands, err := connection.backend.Commands(ctx)
	if err != nil {
		closeInteractiveConnection(connection)
		return nil, err
	}
	state, err := openInteractiveSession(ctx, connection.client, connection.backend, sessionKey)
	if err != nil {
		closeInteractiveConnection(connection)
		return nil, err
	}
	models, err := connection.backend.Models(ctx)
	if err != nil {
		models = nil
	}
	return &replpkg.TargetBinding{
		Client:       connection.client,
		Service:      connection.backend,
		Target:       target.targetInfo(),
		SessionID:    state.ID,
		SessionKey:   state.Key,
		SessionModel: state.Model,
		Models:       models,
		Commands:     commands,
	}, nil
}

type interactiveConnection struct {
	client  *acp.InProcessClient
	backend *interactiveBackend
}

type interactiveSessionState struct {
	ID    string
	Key   string
	Model string
}

func openInteractiveTarget(ctx context.Context, target interactiveTarget, sessionKey string) (*interactiveConnection, error) {
	backend, err := newInteractiveBackendForTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	server := acp.NewServer(backend.gateway, acp.ServerConfig{
		DefaultSessionKey: sessionKey,
		CommandProvider:   backend.Commands,
	})
	client, err := acp.NewInProcessClient(ctx, server)
	if err != nil {
		_ = backend.Close(context.Background())
		return nil, err
	}
	return &interactiveConnection{client: client, backend: backend}, nil
}

func closeInteractiveConnection(connection *interactiveConnection) {
	if connection == nil {
		return
	}
	if connection.client != nil {
		connection.client.Close()
	}
	if connection.backend != nil {
		_ = connection.backend.Close(context.Background())
	}
}

func newInteractiveBackendForTarget(ctx context.Context, target interactiveTarget) (*interactiveBackend, error) {
	switch target.Kind {
	case interactiveTargetLocal:
		if !isPrivateLocalInteractiveTarget(target) {
			break
		}
		cfg, err := loadInteractiveConfig()
		if err != nil {
			return nil, err
		}
		applyInteractiveRuntimeProfileDefaults(&cfg, target)
		applyInteractiveLoggingOverrides(&cfg, target)
		app, err := bootstrap.New(ctx, cfg, bootstrap.Dependencies{
			ConfigPath: resolveConfigPath(),
		})
		if err != nil {
			return nil, fmt.Errorf("bootstrap interactive runtime: %w", err)
		}
		return newEmbeddedInteractiveBackend(app, target), nil
	}
	authToken, err := resolveInteractiveTargetAuthToken(target)
	if err != nil {
		return nil, err
	}
	client, _, err := newGatewayClientWithOptions(target.BaseURL, authToken, target.Insecure)
	if err != nil {
		return nil, err
	}
	if !gatewayHealthy(ctx, client) {
		return nil, fmt.Errorf("remote %q is not reachable", target.label())
	}
	return newExternalInteractiveBackend(client, target), nil
}

func loadSavedRemoteInteractiveTargets() []interactiveTarget {
	profiles, err := loadSavedTargetProfiles()
	if err != nil {
		return nil
	}
	targets := make([]interactiveTarget, 0, len(profiles))
	for _, profile := range profiles {
		targets = append(targets, interactiveTargetFromSavedProfile(profile))
	}
	return targets
}

func interactiveTargetFromSavedProfile(profile savedTargetProfile) interactiveTarget {
	description := strings.TrimSpace(profile.BaseURL)
	if strings.TrimSpace(profile.AuthType) != "" && profile.AuthType != targetAuthTypeNone {
		description += "   auth=" + strings.TrimSpace(profile.AuthType)
		if strings.EqualFold(strings.TrimSpace(profile.AuthType), targetAuthTypeBearer) && strings.TrimSpace(profile.AuthRef) == "" {
			description += "   login required"
		}
	}
	if profile.Insecure {
		description += "   insecure"
	}
	return interactiveTarget{
		Kind:        interactiveTargetRemote,
		Name:        strings.TrimSpace(profile.Name),
		BaseURL:     strings.TrimSpace(profile.BaseURL),
		AuthType:    strings.TrimSpace(profile.AuthType),
		AuthRef:     strings.TrimSpace(profile.AuthRef),
		Insecure:    profile.Insecure,
		Description: description,
	}
}

func resolveInteractiveTargetAuthToken(target interactiveTarget) (string, error) {
	if ref := strings.TrimSpace(target.AuthRef); ref != "" {
		value, err := keychain.ResolveSecret(ref)
		if err != nil {
			return "", fmt.Errorf("resolve auth for remote %q: %w", target.label(), err)
		}
		return strings.TrimSpace(value), nil
	}
	if strings.EqualFold(strings.TrimSpace(target.AuthType), targetAuthTypeBearer) {
		return "", missingTargetCredentialsError(target.label())
	}
	return strings.TrimSpace(target.AuthToken), nil
}

func openInteractiveSession(ctx context.Context, client *acp.InProcessClient, service replpkg.Service, key string) (interactiveSessionState, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return interactiveSessionState{}, fmt.Errorf("conversation key is required")
	}
	info, err := client.LoadSession(ctx, acp.LoadSessionParams{SessionKey: key})
	if err != nil {
		info, err = client.NewSession(ctx, acp.NewSessionParams{SessionKey: key})
	}
	if err != nil {
		return interactiveSessionState{}, err
	}
	state := interactiveSessionState{
		ID:  info.SessionID,
		Key: info.SessionKey,
	}
	if detail, err := service.GetSession(ctx, state.ID); err == nil && detail != nil {
		state.Model = detail.Summary.Model
	}
	return state, nil
}
