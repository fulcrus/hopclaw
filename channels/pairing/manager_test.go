package pairing

import (
	"testing"
	"time"
)

func TestInitiateAndVerify(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	code, err := mgr.InitiatePairing("slack", "U123", "Alice")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if len(code) != codeLength {
		t.Fatalf("expected %d-digit code, got %q", codeLength, code)
	}

	// Before verification, IsVerified should be false.
	if mgr.IsVerified("slack", "U123") {
		t.Fatal("expected not verified before VerifyCode")
	}

	rec, err := mgr.VerifyCode(code)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rec.Status != StatusVerified {
		t.Fatalf("expected status %q, got %q", StatusVerified, rec.Status)
	}
	if rec.Code != "" {
		t.Fatal("expected code to be cleared after verification")
	}
	if rec.VerifiedAt.IsZero() {
		t.Fatal("expected VerifiedAt to be set")
	}

	// After verification, IsVerified should be true.
	if !mgr.IsVerified("slack", "U123") {
		t.Fatal("expected verified after VerifyCode")
	}
}

func TestExpiredCodeRejected(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	code, err := mgr.InitiatePairing("telegram", "T456", "Bob")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}

	// Manually expire the code.
	rec, _ := store.Get("telegram", "T456")
	rec.CodeExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	_ = store.Save(rec)

	_, err = mgr.VerifyCode(code)
	if err == nil {
		t.Fatal("expected error for expired code")
	}
}

func TestDuplicateInitiationReplacesCode(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	code1, err := mgr.InitiatePairing("slack", "U999", "Charlie")
	if err != nil {
		t.Fatalf("initiate 1: %v", err)
	}

	code2, err := mgr.InitiatePairing("slack", "U999", "Charlie")
	if err != nil {
		t.Fatalf("initiate 2: %v", err)
	}

	// Old code should no longer work.
	_, err = mgr.VerifyCode(code1)
	if err == nil {
		t.Fatal("expected old code to be invalid after re-initiation")
	}

	// New code should work.
	rec, err := mgr.VerifyCode(code2)
	if err != nil {
		t.Fatalf("verify new code: %v", err)
	}
	if rec.Status != StatusVerified {
		t.Fatalf("expected verified, got %q", rec.Status)
	}
}

func TestIsVerifiedStates(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	// Non-existent user.
	if mgr.IsVerified("slack", "UNOTEXIST") {
		t.Fatal("expected false for non-existent user")
	}

	// Pending user.
	_, _ = mgr.InitiatePairing("slack", "UPENDING", "Pending")
	if mgr.IsVerified("slack", "UPENDING") {
		t.Fatal("expected false for pending user")
	}

	// Verified user.
	code, _ := mgr.InitiatePairing("slack", "UVERIFIED", "Verified")
	_, _ = mgr.VerifyCode(code)
	if !mgr.IsVerified("slack", "UVERIFIED") {
		t.Fatal("expected true for verified user")
	}

	// Revoked user.
	_ = mgr.Revoke("slack", "UVERIFIED")
	if mgr.IsVerified("slack", "UVERIFIED") {
		t.Fatal("expected false for revoked user")
	}
}

func TestRevoke(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	// Revoke non-existent should fail.
	if err := mgr.Revoke("slack", "UNONE"); err == nil {
		t.Fatal("expected error revoking non-existent record")
	}

	// Set up and verify a user, then revoke.
	code, _ := mgr.InitiatePairing("slack", "UREVOKE", "Revokee")
	_, _ = mgr.VerifyCode(code)
	if !mgr.IsVerified("slack", "UREVOKE") {
		t.Fatal("expected verified before revoke")
	}

	if err := mgr.Revoke("slack", "UREVOKE"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if mgr.IsVerified("slack", "UREVOKE") {
		t.Fatal("expected not verified after revoke")
	}

	rec, _ := store.Get("slack", "UREVOKE")
	if rec.Status != StatusRevoked {
		t.Fatalf("expected status %q, got %q", StatusRevoked, rec.Status)
	}
	if rec.Code != "" {
		t.Fatal("expected code cleared after revoke")
	}
}

func TestList(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	_, _ = mgr.InitiatePairing("slack", "U1", "One")
	_, _ = mgr.InitiatePairing("telegram", "T1", "Two")

	records, err := mgr.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}

func TestVerifyInvalidCode(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	_, err := mgr.VerifyCode("000000")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
}

func TestVerifyEmptyCode(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	_, err := mgr.VerifyCode("")
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestInitiateMissingFields(t *testing.T) {
	store := NewInMemoryStore()
	mgr := NewManager(store)

	_, err := mgr.InitiatePairing("", "U1", "Name")
	if err == nil {
		t.Fatal("expected error for empty channel")
	}

	_, err = mgr.InitiatePairing("slack", "", "Name")
	if err == nil {
		t.Fatal("expected error for empty user id")
	}
}
