package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUserPasswordHashingAndVerification(t *testing.T) {
	pepper := []byte("test-pepper-12345678901234567890")
	password := "SecretPass#2026"

	hash, err := HashUserPassword(password, pepper)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if !VerifyUserPassword(hash, password, pepper) {
		t.Fatal("expected password to verify successfully")
	}

	if VerifyUserPassword(hash, "WrongPassword", pepper) {
		t.Fatal("wrong password should not verify")
	}

	if VerifyUserPassword("invalid-hash-format", password, pepper) {
		t.Fatal("invalid hash format should not verify")
	}
}

func TestUserRBACCRUDAndScoping(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Initial admin creation
	admin, err := store.CreateLocalUser(ctx, CreateUserInput{
		Login:        "admin",
		Password:     "AdminPass#1",
		DisplayName:  "系统管理员",
		Role:         RoleAdmin,
		AllowedNodes: "*",
	}, now)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if !admin.IsAdmin() || !admin.CanWrite() {
		t.Fatal("expected admin to have full admin & write permissions")
	}

	// 2. Duplicate login rejected
	_, err = store.CreateLocalUser(ctx, CreateUserInput{
		Login:    "admin",
		Password: "Password#2",
	}, now)
	if !errors.Is(err, ErrUserAlreadyExists) {
		t.Fatalf("expected ErrUserAlreadyExists, got %v", err)
	}

	// 3. Create operator with node scoping
	operator, err := store.CreateLocalUser(ctx, CreateUserInput{
		Login:        "ops_bob",
		Password:     "BobPass#2026",
		DisplayName:  "运维工程师 Bob",
		Role:         RoleOperator,
		AllowedNodes: "node-uuid-1, node-uuid-2",
	}, now)
	if err != nil {
		t.Fatalf("create operator user: %v", err)
	}
	if operator.IsAdmin() {
		t.Fatal("operator should not be admin")
	}
	if !operator.IsOperator() || !operator.CanWrite() {
		t.Fatal("operator should be operator and have write permissions")
	}
	if !operator.CanAccessNode("node-uuid-1") || !operator.CanAccessNode("node-uuid-2") {
		t.Fatal("operator should have access to scoped nodes 1 and 2")
	}
	if operator.CanAccessNode("node-uuid-3") {
		t.Fatal("operator should NOT have access to node 3")
	}

	// 4. Create viewer
	viewer, err := store.CreateLocalUser(ctx, CreateUserInput{
		Login:       "viewer_claire",
		Password:    "ClairePass#1",
		DisplayName: "监控观察员 Claire",
		Role:        RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("create viewer user: %v", err)
	}
	if viewer.CanWrite() {
		t.Fatal("viewer should NOT have write permission")
	}
	if !viewer.IsViewer() {
		t.Fatal("viewer role expected")
	}

	// 5. List users
	users, err := store.ListAdminUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) < 3 {
		t.Fatalf("expected at least 3 users, got %d", len(users))
	}

	// 6. Authenticate local user
	authUser, err := store.AuthenticateLocalUser(ctx, "ops_bob", "BobPass#2026")
	if err != nil {
		t.Fatalf("authenticate local user: %v", err)
	}
	if authUser.ID != operator.ID {
		t.Fatalf("authenticated user id = %s, want %s", authUser.ID, operator.ID)
	}

	// Bad password
	_, err = store.AuthenticateLocalUser(ctx, "ops_bob", "BadPassword")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}

	// 7. Update user
	newRole := RoleViewer
	disabledTrue := true
	updated, err := store.UpdateAdminUser(ctx, operator.ID, UpdateUserInput{
		Role:     &newRole,
		Disabled: &disabledTrue,
	}, now)
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if updated.Role != RoleViewer || !updated.Disabled {
		t.Fatalf("expected updated role viewer and disabled true, got %+v", updated)
	}

	// Disabled user cannot authenticate
	_, err = store.AuthenticateLocalUser(ctx, "ops_bob", "BobPass#2026")
	if !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("expected ErrAccountDisabled, got %v", err)
	}

	// 8. Last admin protection: cannot delete or demote sole active admin
	demoteRole := RoleViewer
	_, err = store.UpdateAdminUser(ctx, admin.ID, UpdateUserInput{Role: &demoteRole}, now)
	if !errors.Is(err, ErrLastAdminProtection) {
		t.Fatalf("expected ErrLastAdminProtection on demoting sole admin, got %v", err)
	}

	err = store.DeleteAdminUser(ctx, admin.ID)
	if !errors.Is(err, ErrLastAdminProtection) {
		t.Fatalf("expected ErrLastAdminProtection on deleting sole admin, got %v", err)
	}
}
