package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWebAuthnStore(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "probewatch.db"), []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Create admin user
	admin, err := store.UpsertAdminUser(ctx, "local", "admin", "admin", now)
	if err != nil {
		t.Fatalf("upsert admin user: %v", err)
	}

	// 2. Save & consume challenge
	challengeID := "test-challenge-1"
	rawChallenge := []byte("random-32-byte-challenge-data-test")
	expiresAt := now.Add(5 * time.Minute)

	if err := store.SaveWebAuthnChallenge(ctx, challengeID, rawChallenge, "register", admin.ID, expiresAt); err != nil {
		t.Fatalf("save challenge: %v", err)
	}

	consumed, adminID, err := store.GetAndConsumeWebAuthnChallenge(ctx, challengeID, "register", now)
	if err != nil {
		t.Fatalf("get and consume challenge: %v", err)
	}
	if string(consumed) != string(rawChallenge) || adminID != admin.ID {
		t.Fatalf("challenge mismatch: got %s / %s", string(consumed), adminID)
	}

	// Consuming second time should fail
	_, _, err = store.GetAndConsumeWebAuthnChallenge(ctx, challengeID, "register", now)
	if err != ErrWebAuthnChallengeNotFound {
		t.Fatalf("expected ErrWebAuthnChallengeNotFound on replay, got: %v", err)
	}

	// 3. Create credential
	credID := []byte("raw-credential-id-12345")
	pubKey := []byte("der-pkix-public-key-bytes")
	cred := WebAuthnCredential{
		ID:           "cred-uuid-1",
		AdminID:      admin.ID,
		Name:         "Windows Hello (笔记本)",
		CredentialID: credID,
		PublicKey:    pubKey,
		Algorithm:    -7,
		SignCount:    1,
		AAGUID:       []byte("test-aaguid-16b"),
		CreatedAt:    now,
	}

	if err := store.CreateWebAuthnCredential(ctx, cred); err != nil {
		t.Fatalf("create credential: %v", err)
	}

	// 4. List credentials
	list, err := store.ListWebAuthnCredentials(ctx, admin.ID)
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Windows Hello (笔记本)" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// 5. Get by Credential ID
	byCred, err := store.GetWebAuthnCredentialByCredID(ctx, credID)
	if err != nil {
		t.Fatalf("get by cred ID: %v", err)
	}
	if byCred.ID != "cred-uuid-1" || string(byCred.PublicKey) != string(pubKey) {
		t.Fatalf("credential mismatch: %+v", byCred)
	}

	// 6. Update usage
	usageTime := now.Add(time.Hour)
	if err := store.UpdateWebAuthnCredentialUsage(ctx, cred.ID, 2, usageTime); err != nil {
		t.Fatalf("update usage: %v", err)
	}

	byCred, _ = store.GetWebAuthnCredentialByCredID(ctx, credID)
	if byCred.SignCount != 2 || byCred.LastUsedAt == nil {
		t.Fatalf("expected updated usage, got %+v", byCred)
	}

	// 7. Rename
	if err := store.RenameWebAuthnCredential(ctx, cred.ID, admin.ID, "主电脑 Passkey"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	byCred, _ = store.GetWebAuthnCredentialByCredID(ctx, credID)
	if byCred.Name != "主电脑 Passkey" {
		t.Fatalf("expected renamed, got %s", byCred.Name)
	}

	// 8. Delete
	if err := store.DeleteWebAuthnCredential(ctx, cred.ID, admin.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	listAfter, err := store.ListWebAuthnCredentials(ctx, admin.ID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(listAfter) != 0 {
		t.Fatalf("expected empty list after delete, got %d", len(listAfter))
	}
}
