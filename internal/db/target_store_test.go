package db

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTargetCRUDPreservesCreatedAtAndUpdatesPayload(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	createdAt := time.Unix(1_700_000_000, 0).UTC()
	updatedAt := createdAt.Add(time.Hour)
	definition := TargetDefinition{
		ID:      "network-1",
		Name:    "Primary HTTPS",
		Kind:    TargetKindHTTPS,
		Host:    "example.com",
		Enabled: true,
		Payload: []byte(`{"path":"/health","timeout_ms":3000}`),
	}

	created, err := store.CreateTarget(ctx, definition, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != definition.ID || created.Kind != definition.Kind || !created.Enabled || !created.CreatedAt.Equal(createdAt) || !created.UpdatedAt.Equal(createdAt) {
		t.Fatalf("created target = %#v", created)
	}
	if !bytes.Equal(created.Payload, definition.Payload) {
		t.Fatalf("created payload = %s, want %s", created.Payload, definition.Payload)
	}

	got, err := store.GetTarget(ctx, TargetKindHTTPS, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %s, want %s", got.CreatedAt, createdAt)
	}

	definition.Name = "Updated HTTPS"
	definition.Enabled = false
	definition.Payload = []byte(`{"path":"/ready","timeout_ms":5000}`)
	updated, err := store.UpdateTarget(ctx, TargetKindHTTPS, definition.ID, definition, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.CreatedAt.Equal(createdAt) || !updated.UpdatedAt.Equal(updatedAt) || updated.Name != definition.Name || updated.Enabled != definition.Enabled {
		t.Fatalf("updated target = %#v", updated)
	}

	list, err := store.ListTargets(ctx, TargetKindHTTPS)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != definition.ID {
		t.Fatalf("listed targets = %#v", list)
	}

	if err := store.DeleteTarget(ctx, TargetKindHTTPS, definition.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTarget(ctx, TargetKindHTTPS, definition.ID); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("deleted target error = %v, want ErrTargetNotFound", err)
	}
}

func TestTargetCRUDSupportsMTRAndMediaKinds(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()

	for _, definition := range []TargetDefinition{
		{ID: "mtr-1", Name: "Route", Kind: TargetKindMTR, Host: "example.com", Payload: []byte(`{"max_hops":20}`)},
		{ID: "media-1", Name: "Media", Kind: TargetKindMediaHTTP, Host: "media.example.com", Enabled: true, Payload: []byte(`{"host":"media.example.com","path":"/manifest"}`)},
	} {
		if _, err := store.CreateTarget(context.Background(), definition, now); err != nil {
			t.Fatalf("CreateTarget(%s): %v", definition.Kind, err)
		}
		got, err := store.GetTarget(context.Background(), definition.Kind, definition.ID)
		if err != nil {
			t.Fatalf("GetTarget(%s): %v", definition.Kind, err)
		}
		if got.Kind != definition.Kind {
			t.Fatalf("target = %#v, want kind %q", got, definition.Kind)
		}
	}
}

func TestLookupEnabledTargetRejectsUnknownDisabledAndTypeMismatch(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	definition := TargetDefinition{
		ID:      "lookup-1",
		Name:    "Lookup target",
		Kind:    TargetKindTCP,
		Host:    "example.com",
		Enabled: false,
		Payload: []byte(`{"port":443}`),
	}
	if _, err := store.CreateTarget(context.Background(), definition, now); err != nil {
		t.Fatal(err)
	}

	if _, err := store.LookupEnabledTarget(context.Background(), TargetKindTCP, "missing"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("unknown target error = %v, want ErrTargetNotFound", err)
	}
	if _, err := store.LookupEnabledTarget(context.Background(), TargetKindTCP, definition.ID); !errors.Is(err, ErrTargetDisabled) {
		t.Fatalf("disabled target error = %v, want ErrTargetDisabled", err)
	}
	if _, err := store.LookupEnabledTarget(context.Background(), TargetKindMTR, definition.ID); !errors.Is(err, ErrTargetKindMismatch) {
		t.Fatalf("type mismatch error = %v, want ErrTargetKindMismatch", err)
	}
	if _, err := store.LookupEnabledTarget(context.Background(), "shell", definition.ID); !errors.Is(err, ErrTargetKindMismatch) {
		t.Fatalf("unknown kind error = %v, want ErrTargetKindMismatch", err)
	}
}

func TestTargetPersistenceRejectsInvalidOversizedJSONAndIDs(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	base := TargetDefinition{ID: "invalid-1", Name: "Invalid", Kind: TargetKindTCP, Host: "example.com"}

	for name, payload := range map[string][]byte{
		"invalid":   []byte(`{"broken"}`),
		"oversized": []byte(`{"data":"` + strings.Repeat("a", maxResourcePayloadBytes) + `"}`),
	} {
		t.Run(name, func(t *testing.T) {
			definition := base
			definition.ID = name
			definition.Payload = payload
			if _, err := store.CreateTarget(context.Background(), definition, now); err == nil {
				t.Fatal("CreateTarget accepted invalid payload")
			}
		})
	}

	tooLong := base
	tooLong.ID = strings.Repeat("x", maxTargetIDLength+1)
	tooLong.Payload = []byte(`{}`)
	if _, err := store.CreateTarget(context.Background(), tooLong, now); err == nil {
		t.Fatal("CreateTarget accepted an oversized ID")
	}
	if _, err := store.CreateTarget(context.Background(), TargetDefinition{ID: "missing-kind", Name: "Missing kind", Host: "example.com", Payload: []byte(`{}`)}, now); !errors.Is(err, ErrTargetKindMismatch) {
		t.Fatalf("missing kind error = %v, want ErrTargetKindMismatch", err)
	}
}
