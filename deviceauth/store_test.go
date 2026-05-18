package deviceauth

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Load / Save
// ---------------------------------------------------------------------------

func TestStoreLoadSave(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// First load with no file on disk should succeed.
	if err := s.Load(); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	tok := &DeviceToken{
		Token:    "hc_aaaa",
		DeviceID: "d-1",
		Role:     RoleOperator,
	}
	if err := s.SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}

	// Reload into a fresh store.
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := s2.GetToken("d-1", RoleOperator)
	if !ok {
		t.Fatal("expected token after reload")
	}
	if got.DeviceID != "d-1" {
		t.Fatalf("device_id = %q, want %q", got.DeviceID, "d-1")
	}
}

func TestStoreLoadVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, identitySubdir)
	if err := os.MkdirAll(subDir, storeDirPerms); err != nil {
		t.Fatal(err)
	}
	// Write a store file with wrong version.
	data := []byte(`{"version":99}`)
	if err := os.WriteFile(filepath.Join(subDir, storeFileName), data, storeFilePerms); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	err := s.Load()
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("expected ErrVersionMismatch, got: %v", err)
	}
}

func TestStoreLoadMigratesLegacyRoleKeyedTokens(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, identitySubdir)
	if err := os.MkdirAll(subDir, storeDirPerms); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{
  "version": 1,
  "tokens": {
    "operator": {
      "token": "hc_legacy",
      "device_id": "legacy-device",
      "role": "operator"
    }
  },
  "devices": {}
}`)
	if err := os.WriteFile(filepath.Join(subDir, storeFileName), data, storeFilePerms); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("load legacy store: %v", err)
	}
	token, ok := s.GetToken("legacy-device", RoleOperator)
	if !ok || token.Token != "hc_legacy" {
		t.Fatalf("legacy token = %#v ok=%v", token, ok)
	}
}

// ---------------------------------------------------------------------------
// Token CRUD
// ---------------------------------------------------------------------------

func TestStoreTokenCRUD(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	// Get non-existent token.
	_, ok := s.GetToken("d-2", RoleNode)
	if ok {
		t.Fatal("expected no token for RoleNode")
	}

	// Create.
	tok := &DeviceToken{Token: "hc_bb", DeviceID: "d-2", Role: RoleNode}
	if err := s.SetToken(tok); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Read.
	got, ok := s.GetToken("d-2", RoleNode)
	if !ok {
		t.Fatal("expected token")
	}
	if got.Token != "hc_bb" {
		t.Fatalf("token = %q, want %q", got.Token, "hc_bb")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}

	// Update (overwrite).
	tok2 := &DeviceToken{Token: "hc_cc", DeviceID: "d-2", Role: RoleNode}
	if err := s.SetToken(tok2); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := s.GetToken("d-2", RoleNode)
	if got2.Token != "hc_cc" {
		t.Fatalf("after update token = %q, want %q", got2.Token, "hc_cc")
	}

	// Delete.
	if err := s.DeleteToken("d-2", RoleNode); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, ok = s.GetToken("d-2", RoleNode)
	if ok {
		t.Fatal("expected token deleted")
	}

	// Delete non-existent.
	err := s.DeleteToken("d-2", RoleViewer)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Device registration
// ---------------------------------------------------------------------------

func TestStoreDeviceRegistration(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	dev := &DeviceIdentity{
		DeviceID: "dev-001",
		Name:     "test-phone",
		Platform: "ios",
	}
	if err := s.RegisterDevice(dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	if dev.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be populated")
	}

	got, ok := s.GetDevice("dev-001")
	if !ok {
		t.Fatal("expected device after register")
	}
	if got.Name != "test-phone" {
		t.Fatalf("name = %q, want %q", got.Name, "test-phone")
	}

	// List.
	list := s.ListDevices()
	if len(list) != 1 {
		t.Fatalf("len(devices) = %d, want 1", len(list))
	}

	// Not found.
	_, ok = s.GetDevice("no-such-device")
	if ok {
		t.Fatal("expected device not found")
	}
}

// ---------------------------------------------------------------------------
// Trust / Revoke
// ---------------------------------------------------------------------------

func TestStoreTrustRevoke(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	dev := &DeviceIdentity{DeviceID: "dev-002"}
	if err := s.RegisterDevice(dev); err != nil {
		t.Fatal(err)
	}

	// Trust.
	if err := s.TrustDevice("dev-002"); err != nil {
		t.Fatalf("trust: %v", err)
	}
	got, _ := s.GetDevice("dev-002")
	if !got.Trusted {
		t.Fatal("expected trusted")
	}

	// Revoke.
	if err := s.RevokeDevice("dev-002"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ = s.GetDevice("dev-002")
	if got.Trusted {
		t.Fatal("expected untrusted after revoke")
	}

	// Trust non-existent.
	err := s.TrustDevice("no-such")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	// Revoke non-existent.
	err = s.RevokeDevice("no-such")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// File permissions
// ---------------------------------------------------------------------------

func TestStoreFilePermissions(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	// Force a save by writing a token.
	tok := &DeviceToken{Token: "hc_dd", DeviceID: "d-3", Role: RoleViewer}
	if err := s.SetToken(tok); err != nil {
		t.Fatal(err)
	}

	fp := filepath.Join(dir, identitySubdir, storeFileName)
	info, err := os.Stat(fp)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != storeFilePerms {
		t.Fatalf("file perms = %o, want %o", perm, storeFilePerms)
	}
}

// ---------------------------------------------------------------------------
// UpdateLastSeen
// ---------------------------------------------------------------------------

func TestStoreUpdateLastSeen(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	dev := &DeviceIdentity{DeviceID: "dev-003"}
	if err := s.RegisterDevice(dev); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetDevice("dev-003")
	origSeen := before.LastSeenAt

	if err := s.UpdateLastSeen("dev-003"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetDevice("dev-003")
	if !after.LastSeenAt.After(origSeen) && after.LastSeenAt != origSeen {
		// Allow equal (fast test), but not earlier.
		if after.LastSeenAt.Before(origSeen) {
			t.Fatal("last_seen_at went backwards")
		}
	}

	// Non-existent device.
	err := s.UpdateLastSeen("no-such")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestStoreConcurrency(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			id := GenerateDeviceID()
			dev := &DeviceIdentity{DeviceID: id, Name: "concurrent-device"}
			_ = s.RegisterDevice(dev)
			_ = s.TrustDevice(id)
			_, _ = s.GetDevice(id)
			_ = s.UpdateLastSeen(id)
			_ = s.RevokeDevice(id)
			_ = s.ListDevices()

			tok := &DeviceToken{Token: "hc_tok", DeviceID: id, Role: RoleViewer}
			_ = s.SetToken(tok)
			_, _ = s.GetToken(id, RoleViewer)
			_ = idx
		}(i)
	}

	wg.Wait()
}

func TestStoreAllowsMultipleDevicesPerRole(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	if err := s.SetToken(&DeviceToken{Token: "hc_a", DeviceID: "device-a", Role: RoleViewer}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetToken(&DeviceToken{Token: "hc_b", DeviceID: "device-b", Role: RoleViewer}); err != nil {
		t.Fatal(err)
	}

	a, ok := s.GetToken("device-a", RoleViewer)
	if !ok || a.Token != "hc_a" {
		t.Fatalf("device-a token = %#v ok=%v", a, ok)
	}
	b, ok := s.GetToken("device-b", RoleViewer)
	if !ok || b.Token != "hc_b" {
		t.Fatalf("device-b token = %#v ok=%v", b, ok)
	}

	all := s.ListTokens("")
	if len(all) != 2 {
		t.Fatalf("len(ListTokens) = %d, want 2", len(all))
	}
}
