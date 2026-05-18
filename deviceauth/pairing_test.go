package deviceauth

import (
	"errors"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Full pairing flow
// ---------------------------------------------------------------------------

func TestPairingInitiateAndVerify(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)
	// Not starting the reap goroutine - not needed for this test.

	rec, err := pm.InitiatePairing("web", "dev-p1")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if rec.Status != PairingPending {
		t.Fatalf("status = %q, want %q", rec.Status, PairingPending)
	}
	if len(rec.Code) != pairingCodeLength {
		t.Fatalf("code length = %d, want %d", len(rec.Code), pairingCodeLength)
	}
	if rec.ExpiresAt.Before(time.Now()) {
		t.Fatal("expected expiry in the future")
	}

	// Verify the code.
	verified, err := pm.VerifyCode(rec.Code)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Status != PairingVerified {
		t.Fatalf("status = %q, want %q", verified.Status, PairingVerified)
	}
	if verified.VerifiedAt.IsZero() {
		t.Fatal("expected verified_at to be set")
	}

	// Device should now be registered and trusted.
	dev, ok := store.GetDevice("dev-p1")
	if !ok {
		t.Fatal("expected device to be registered after verification")
	}
	if !dev.Trusted {
		t.Fatal("expected device to be trusted")
	}

	// IsVerified should return true.
	if !pm.IsVerified("web", "dev-p1") {
		t.Fatal("expected IsVerified to return true")
	}
}

// ---------------------------------------------------------------------------
// Code expiry
// ---------------------------------------------------------------------------

func TestPairingCodeExpiry(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)

	rec, err := pm.InitiatePairing("ios", "dev-p2")
	if err != nil {
		t.Fatal(err)
	}

	// Manually expire the record.
	pm.mu.Lock()
	key := pairingKey("ios", "dev-p2")
	pm.records[key].ExpiresAt = time.Now().Add(-time.Second)
	pm.mu.Unlock()

	_, err = pm.VerifyCode(rec.Code)
	if !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("expected ErrPairingExpired, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Revocation
// ---------------------------------------------------------------------------

func TestPairingRevoke(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)

	rec, err := pm.InitiatePairing("android", "dev-p3")
	if err != nil {
		t.Fatal(err)
	}

	if err := pm.RevokePairing("android", "dev-p3"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Verify should fail - code was removed from byCode index.
	_, err = pm.VerifyCode(rec.Code)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after revoke, got: %v", err)
	}

	// Revoke non-existent should error.
	err = pm.RevokePairing("web", "no-such")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Max pending limit
// ---------------------------------------------------------------------------

func TestPairingMaxPending(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)

	// Fill up to the limit.
	for i := range pairingMaxPending {
		id := GenerateDeviceID()
		_, err := pm.InitiatePairing("web", id)
		if err != nil {
			t.Fatalf("initiate %d: %v", i, err)
		}
	}

	// One more should fail.
	_, err := pm.InitiatePairing("web", "overflow-device")
	if !errors.Is(err, ErrPairingMaxPending) {
		t.Fatalf("expected ErrPairingMaxPending, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Code uniqueness
// ---------------------------------------------------------------------------

func TestPairingCodeUniqueness(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)

	seen := make(map[string]bool)
	const count = 50
	for i := range count {
		id := GenerateDeviceID()
		rec, err := pm.InitiatePairing("web", id)
		if err != nil {
			t.Fatalf("initiate %d: %v", i, err)
		}
		if seen[rec.Code] {
			t.Fatalf("duplicate code %q at iteration %d", rec.Code, i)
		}
		seen[rec.Code] = true
	}
}

// ---------------------------------------------------------------------------
// Replace existing pairing
// ---------------------------------------------------------------------------

func TestPairingReplace(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)

	rec1, err := pm.InitiatePairing("web", "dev-replace")
	if err != nil {
		t.Fatal(err)
	}
	code1 := rec1.Code

	// Initiate again for the same channel+device - should replace.
	rec2, err := pm.InitiatePairing("web", "dev-replace")
	if err != nil {
		t.Fatal(err)
	}
	if rec2.Code == code1 {
		// Codes are random, extremely unlikely but technically possible.
		// Just verify the old code is no longer valid.
		t.Log("codes happen to match; skipping distinctness check")
	}

	// Old code should be gone.
	_, err = pm.VerifyCode(code1)
	if err == nil {
		t.Fatal("expected old code to be invalid after replacement")
	}

	// New code should work.
	_, err = pm.VerifyCode(rec2.Code)
	if err != nil {
		t.Fatalf("verify new code: %v", err)
	}
}

// ---------------------------------------------------------------------------
// List pairings
// ---------------------------------------------------------------------------

func TestPairingList(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)

	_, _ = pm.InitiatePairing("web", "d1")
	_, _ = pm.InitiatePairing("ios", "d2")

	list := pm.ListPairings()
	if len(list) != 2 {
		t.Fatalf("len(pairings) = %d, want 2", len(list))
	}
}

// ---------------------------------------------------------------------------
// Reap expired
// ---------------------------------------------------------------------------

func TestPairingReapExpired(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)

	rec, err := pm.InitiatePairing("web", "dev-reap")
	if err != nil {
		t.Fatal(err)
	}

	// Manually expire.
	pm.mu.Lock()
	key := pairingKey("web", "dev-reap")
	pm.records[key].ExpiresAt = time.Now().Add(-time.Second)
	pm.mu.Unlock()

	// Trigger reap.
	pm.reapExpired()

	// Record should be gone.
	_, err = pm.VerifyCode(rec.Code)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after reap, got: %v", err)
	}

	list := pm.ListPairings()
	if len(list) != 0 {
		t.Fatalf("expected 0 pairings after reap, got %d", len(list))
	}
}
