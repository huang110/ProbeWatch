package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHasScope(t *testing.T) {
	tests := []struct {
		scopes   string
		required string
		expected bool
	}{
		{"*", "read:nodes", true},
		{"", "read:nodes", true},
		{"read:nodes,write:alerts", "read:nodes", true},
		{"read:nodes,write:alerts", "write:alerts", true},
		{"read:nodes,write:alerts", "write:nodes", false},
		{"read:*", "read:nodes", true},
		{"read:*", "read:metrics", true},
		{"read:*", "write:alerts", false},
	}

	for _, tc := range tests {
		got := HasScope(tc.scopes, tc.required)
		if got != tc.expected {
			t.Errorf("HasScope(%q, %q) = %v; want %v", tc.scopes, tc.required, got, tc.expected)
		}
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Create token
	lifetime := 24 * time.Hour
	token, raw, err := store.CreateAPIToken(ctx, CreateAPITokenInput{
		Name:         "CI Deployment Token",
		UserID:       "user-123",
		Role:         RoleOperator,
		Scopes:       "read:nodes,write:nodes",
		AllowedNodes: "node-1,node-2",
		ExpiresIn:    &lifetime,
	}, now)
	if err != nil {
		t.Fatalf("create api token: %v", err)
	}

	if !strings.HasPrefix(raw, APITokenPrefix) {
		t.Fatalf("expected raw token to start with %s, got %s", APITokenPrefix, raw)
	}
	if token.TokenHash != HashAPIToken(raw) {
		t.Fatalf("token hash mismatch")
	}
	if token.Role != RoleOperator {
		t.Fatalf("expected role operator, got %s", token.Role)
	}
	if token.ExpiresAt == nil || token.ExpiresAt.Before(now) {
		t.Fatalf("expected valid future expiration")
	}

	// 2. Lookup by hash
	found, err := store.GetAPITokenByHash(ctx, HashAPIToken(raw))
	if err != nil {
		t.Fatalf("get api token by hash: %v", err)
	}
	if found.ID != token.ID || found.Name != "CI Deployment Token" {
		t.Fatalf("found token mismatch: %+v", found)
	}

	// 3. Lookup with invalid hash
	_, err = store.GetAPITokenByHash(ctx, "nonexistent-hash")
	if !errors.Is(err, ErrAPITokenNotFound) {
		t.Fatalf("expected ErrAPITokenNotFound, got %v", err)
	}

	// 4. Update last used
	usedTime := now.Add(10 * time.Minute)
	if err := store.UpdateAPITokenLastUsed(ctx, token.ID, usedTime); err != nil {
		t.Fatalf("update last used: %v", err)
	}
	found, err = store.GetAPITokenByHash(ctx, HashAPIToken(raw))
	if err != nil {
		t.Fatalf("get updated token: %v", err)
	}
	if found.LastUsedAt == nil || found.LastUsedAt.Unix() != usedTime.Unix() {
		t.Fatalf("expected last used at %v, got %v", usedTime, found.LastUsedAt)
	}

	// 5. List tokens
	userTokens, err := store.ListAPITokens(ctx, "user-123", false)
	if err != nil {
		t.Fatalf("list user tokens: %v", err)
	}
	if len(userTokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(userTokens))
	}

	otherTokens, err := store.ListAPITokens(ctx, "user-999", false)
	if err != nil {
		t.Fatalf("list other tokens: %v", err)
	}
	if len(otherTokens) != 0 {
		t.Fatalf("expected 0 tokens, got %d", len(otherTokens))
	}

	adminAllTokens, err := store.ListAPITokens(ctx, "", true)
	if err != nil {
		t.Fatalf("admin list all tokens: %v", err)
	}
	if len(adminAllTokens) < 1 {
		t.Fatalf("expected at least 1 token in admin list, got %d", len(adminAllTokens))
	}

	// 6. Disable token
	if err := store.ToggleAPITokenDisabled(ctx, token.ID, true, "user-123", false); err != nil {
		t.Fatalf("disable token: %v", err)
	}
	found, err = store.GetAPITokenByHash(ctx, HashAPIToken(raw))
	if err != nil {
		t.Fatalf("get disabled token: %v", err)
	}
	if !found.Disabled {
		t.Fatalf("expected token to be disabled")
	}

	// 7. Delete token
	if err := store.DeleteAPIToken(ctx, token.ID, "user-123", false); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	_, err = store.GetAPITokenByHash(ctx, HashAPIToken(raw))
	if !errors.Is(err, ErrAPITokenNotFound) {
		t.Fatalf("expected ErrAPITokenNotFound after deletion, got %v", err)
	}
}
