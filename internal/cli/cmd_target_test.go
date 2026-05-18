package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/keychain"
)

type targetTestKeychainStore struct {
	secrets map[string]string
}

func newTargetTestKeychainStore() *targetTestKeychainStore {
	return &targetTestKeychainStore{secrets: map[string]string{}}
}

func (s *targetTestKeychainStore) Get(service, key string) (string, error) {
	if service != keychain.DefaultService() {
		return "", errors.New("unexpected service")
	}
	value, ok := s.secrets[key]
	if !ok {
		return "", keychain.ErrNotFound
	}
	return value, nil
}

func (s *targetTestKeychainStore) Set(service, key, value string) error {
	if service != keychain.DefaultService() {
		return errors.New("unexpected service")
	}
	s.secrets[key] = value
	return nil
}

func (s *targetTestKeychainStore) Delete(service, key string) error {
	if service != keychain.DefaultService() {
		return errors.New("unexpected service")
	}
	if _, ok := s.secrets[key]; !ok {
		return keychain.ErrNotFound
	}
	delete(s.secrets, key)
	return nil
}

func setupTargetTestKeychain(t *testing.T) *targetTestKeychainStore {
	t.Helper()
	store := newTargetTestKeychainStore()
	previous := keychain.CurrentStore()
	keychain.SetStore(store)
	t.Cleanup(func() {
		keychain.SetStore(previous)
	})
	return store
}

func TestTargetCommandsAddGetListRemove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := setupTargetTestKeychain(t)

	root := newRootCmd()
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "add", "prod", "https://prod.example.com", "--token", "secret-token"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote add Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Added remote prod -> https://prod.example.com") {
		t.Fatalf("add output = %q", output.String())
	}

	profiles, err := loadSavedTargetProfiles()
	if err != nil {
		t.Fatalf("loadSavedTargetProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if profiles[0].Name != "prod" || profiles[0].AuthType != targetAuthTypeBearer {
		t.Fatalf("profile = %#v", profiles[0])
	}
	if got := store.secrets[managedTargetSecretKey("prod")]; got != "secret-token" {
		t.Fatalf("stored secret = %q, want %q", got, "secret-token")
	}

	root = newRootCmd()
	output.Reset()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"--json", "remote", "get", "prod"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote get Execute() error = %v", err)
	}
	var view targetView
	if err := json.Unmarshal([]byte(output.String()), &view); err != nil {
		t.Fatalf("decode target get JSON: %v", err)
	}
	if view.Name != "prod" || view.Kind != targetKindRemote || !view.AuthConfigured {
		t.Fatalf("target get view = %#v", view)
	}

	root = newRootCmd()
	output.Reset()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote list Execute() error = %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "prod") || !strings.Contains(text, "local") {
		t.Fatalf("target list output = %q", text)
	}

	root = newRootCmd()
	output.Reset()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "remove", "prod"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote remove Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Removed remote prod") {
		t.Fatalf("remote remove output = %q", output.String())
	}
	profiles, err = loadSavedTargetProfiles()
	if err != nil {
		t.Fatalf("loadSavedTargetProfiles() after remove error = %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("len(profiles) after remove = %d, want 0", len(profiles))
	}
	if _, ok := store.secrets[managedTargetSecretKey("prod")]; ok {
		t.Fatal("expected managed target secret to be deleted")
	}
}

func TestTargetTestUsesSavedBearerToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setupTargetTestKeychain(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-HopClaw-Token"); got != "secret-token" {
			t.Fatalf("X-HopClaw-Token = %q, want %q", got, "secret-token")
		}
		writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer server.Close()

	authRef, err := saveManagedTargetSecret("prod", "secret-token")
	if err != nil {
		t.Fatalf("saveManagedTargetSecret() error = %v", err)
	}
	if err := addSavedTargetProfile(savedTargetProfile{
		Name:     "prod",
		Kind:     targetKindRemote,
		BaseURL:  server.URL,
		AuthType: targetAuthTypeBearer,
		AuthRef:  authRef,
	}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	root := newRootCmd()
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "test", "prod"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote test Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Remote prod is healthy") {
		t.Fatalf("remote test output = %q", output.String())
	}
}

func TestTargetTestBuiltinLocalUsesRuntimeTerminology(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "test", "local"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote test local Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Local connection local is available.") {
		t.Fatalf("remote test local output = %q", output.String())
	}

	root = newRootCmd()
	output.Reset()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"--json", "remote", "test", "local"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote test local JSON Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), `"runtime": "local"`) || !strings.Contains(output.String(), `"kind": "local"`) {
		t.Fatalf("remote test local JSON output = %q", output.String())
	}
}

func TestTargetLoginLogoutLifecycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := setupTargetTestKeychain(t)

	if err := addSavedTargetProfile(savedTargetProfile{
		Name:    "prod",
		Kind:    targetKindRemote,
		BaseURL: "https://prod.example.com",
	}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	root := newRootCmd()
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "login", "prod", "--token", "login-secret"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote login Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Updated credentials for remote prod") {
		t.Fatalf("remote login output = %q", output.String())
	}

	profile, found, err := getSavedTargetProfile("prod")
	if err != nil || !found {
		t.Fatalf("getSavedTargetProfile() = (%#v, %t, %v)", profile, found, err)
	}
	if profile.AuthType != targetAuthTypeBearer {
		t.Fatalf("profile.AuthType = %q, want %q", profile.AuthType, targetAuthTypeBearer)
	}
	if got := store.secrets[managedTargetSecretKey("prod")]; got != "login-secret" {
		t.Fatalf("stored secret = %q, want %q", got, "login-secret")
	}

	root = newRootCmd()
	output.Reset()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "logout", "prod"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote logout Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Cleared credentials for remote prod") {
		t.Fatalf("remote logout output = %q", output.String())
	}

	profile, found, err = getSavedTargetProfile("prod")
	if err != nil || !found {
		t.Fatalf("getSavedTargetProfile() after logout = (%#v, %t, %v)", profile, found, err)
	}
	if profile.AuthType != targetAuthTypeNone || profile.AuthRef != "" {
		t.Fatalf("profile after logout = %#v, want auth cleared", profile)
	}
	if _, ok := store.secrets[managedTargetSecretKey("prod")]; ok {
		t.Fatal("expected managed target secret to be deleted on logout")
	}
}

func TestTargetAddAllowsBearerWithoutCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setupTargetTestKeychain(t)

	root := newRootCmd()
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "add", "prod", "https://prod.example.com", "--auth", "bearer"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote add Execute() error = %v", err)
	}
	profile, found, err := getSavedTargetProfile("prod")
	if err != nil || !found {
		t.Fatalf("getSavedTargetProfile() = (%#v, %t, %v)", profile, found, err)
	}
	if profile.AuthType != targetAuthTypeBearer || profile.AuthRef != "" {
		t.Fatalf("profile = %#v, want bearer auth without stored credentials", profile)
	}
	if !strings.Contains(output.String(), "Credentials not configured yet") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestTargetLoginPromptsForTokenWhenFlagsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := setupTargetTestKeychain(t)

	if err := addSavedTargetProfile(savedTargetProfile{
		Name:     "prod",
		Kind:     targetKindRemote,
		BaseURL:  "https://prod.example.com",
		AuthType: targetAuthTypeBearer,
	}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	root := newRootCmd()
	root.SetIn(strings.NewReader("prompt-secret\n"))
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"remote", "login", "prod"})

	if err := root.Execute(); err != nil {
		t.Fatalf("remote login Execute() error = %v", err)
	}
	if got := store.secrets[managedTargetSecretKey("prod")]; got != "prompt-secret" {
		t.Fatalf("stored secret = %q, want %q", got, "prompt-secret")
	}
}

func TestTargetAddErrorsWhenPromptWouldBlockInNonInteractiveMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setupTargetTestKeychain(t)

	in, out, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	_ = out.Close()
	defer in.Close()

	root := newRootCmd()
	root.SetIn(in)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"remote", "add"})

	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "remote name is required in non-interactive mode") {
		t.Fatalf("Execute() error = %v, want non-interactive guidance", err)
	}
}

func TestTargetLoginErrorsWhenPromptWouldBlockInNonInteractiveMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setupTargetTestKeychain(t)

	if err := addSavedTargetProfile(savedTargetProfile{
		Name:     "prod",
		Kind:     targetKindRemote,
		BaseURL:  "https://prod.example.com",
		AuthType: targetAuthTypeBearer,
	}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	in, out, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	_ = out.Close()
	defer in.Close()

	root := newRootCmd()
	root.SetIn(in)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"remote", "login", "prod"})

	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "credentials are required in non-interactive mode") {
		t.Fatalf("Execute() error = %v, want non-interactive guidance", err)
	}
}
