package auth_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"vpsbot/internal/auth"
)

func TestAuthStore_Workflow(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "users.json")
	adminID := int64(111222333)

	store, err := auth.NewStore(dbPath, adminID)
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}

	// 1. Admin must be authorized and have admin privileges
	if !store.IsAuthorized(adminID) {
		t.Error("admin should be authorized")
	}
	if !store.IsAdmin(adminID) {
		t.Error("admin should have admin role")
	}

	// 2. Unknown user must not be authorized
	unknownUser := int64(999888777)
	if store.IsAuthorized(unknownUser) {
		t.Error("unknown user should not be authorized")
	}

	// 3. Add user directly
	if err := store.AddUser(unknownUser, "tester", auth.RoleUser); err != nil {
		t.Fatalf("failed to add user: %v", err)
	}
	if !store.IsAuthorized(unknownUser) {
		t.Error("newly added user should be authorized")
	}
	if store.IsAdmin(unknownUser) {
		t.Error("regular user should not have admin role")
	}

	// 4. Generate and redeem invite code
	newUser := int64(444555666)
	code, err := store.GenerateInvite(adminID, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate invite: %v", err)
	}
	if len(code) == 0 {
		t.Fatal("invite code should not be empty")
	}

	if err := store.RedeemInvite(code, newUser, "invited_person"); err != nil {
		t.Fatalf("failed to redeem invite: %v", err)
	}
	if !store.IsAuthorized(newUser) {
		t.Error("redeemed user should now be authorized")
	}

	// 5. Code cannot be reused
	if err := store.RedeemInvite(code, int64(777), "cheater"); err == nil {
		t.Error("expected error redeeming already used code, got nil")
	}

	// 6. Delete user
	if err := store.RemoveUser(newUser); err != nil {
		t.Fatalf("failed to remove user: %v", err)
	}
	if store.IsAuthorized(newUser) {
		t.Error("removed user should no longer be authorized")
	}

	// 7. Cannot delete master admin
	if err := store.RemoveUser(adminID); err == nil {
		t.Error("expected error removing main admin, got nil")
	}
}
