package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

func TestOverviewCacheBucketHitAndCrossBucket(t *testing.T) {
	// Base time aligned to a 5-second bucket boundary
	baseSec := int64(1700000000)
	t0 := time.Unix(baseSec, 0).UTC()

	req1 := httptest.NewRequest(http.MethodGet, "/api/overview?range=1h", nil)
	key1, win1, isRel1 := buildOverviewCacheKey(req1, t0)

	if !isRel1 {
		t.Fatalf("expected isRelative to be true for range=1h")
	}

	// 2 seconds later (within the same 5-second bucket)
	t2s := t0.Add(2 * time.Second)
	req2 := httptest.NewRequest(http.MethodGet, "/api/overview?range=1h", nil)
	key2, win2, _ := buildOverviewCacheKey(req2, t2s)

	if key1 != key2 {
		t.Fatalf("expected same cache key within same 5-second bucket, got %q vs %q", key1, key2)
	}
	if !win1.Equal(win2) {
		t.Fatalf("expected identical windowNow within same bucket, got %v vs %v", win1, win2)
	}

	// 6 seconds later (crosses into next 5-second bucket)
	t6s := t0.Add(6 * time.Second)
	req3 := httptest.NewRequest(http.MethodGet, "/api/overview?range=1h", nil)
	key3, win3, _ := buildOverviewCacheKey(req3, t6s)

	if key1 == key3 {
		t.Fatalf("expected different cache key across 5-second buckets, both got %q", key1)
	}
	if win1.Equal(win3) {
		t.Fatalf("expected different windowNow across buckets, both got %v", win1)
	}
}

func TestOverviewCacheExplicitAbsoluteNotMerged(t *testing.T) {
	now := time.Now().UTC()

	reqA := httptest.NewRequest(http.MethodGet, "/api/overview?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&limit=50", nil)
	keyA, _, isRelA := buildOverviewCacheKey(reqA, now)

	reqB := httptest.NewRequest(http.MethodGet, "/api/overview?from=2026-01-01T00:00:01Z&to=2026-01-02T00:00:00Z&limit=50", nil)
	keyB, _, isRelB := buildOverviewCacheKey(reqB, now)

	if isRelA || isRelB {
		t.Fatalf("expected explicit from/to requests to not be relative")
	}
	if keyA == keyB {
		t.Fatalf("expected distinct cache keys for different explicit timestamps, both got %q", keyA)
	}
	if !strings.Contains(keyA, ":abs:") || !strings.Contains(keyB, ":abs:") {
		t.Fatalf("expected absolute cache key to contain ':abs:', got %q and %q", keyA, keyB)
	}
}

func TestOverviewCacheExpirationAndPruning(t *testing.T) {
	srv := &Server{
		overviewCache: make(map[string]overviewCacheEntry),
	}

	t0 := time.Now().UTC()
	key := "test:item"
	payload := []byte(`{"test":true}`)

	// Store with 2-second TTL
	srv.setOverviewCache(key, payload, 2*time.Second, t0)

	// Read immediately at t0 + 1s -> should hit
	data, hit := srv.getOverviewCache(key, t0.Add(1*time.Second))
	if !hit || string(data) != string(payload) {
		t.Fatalf("expected cache hit within TTL, got hit=%v, data=%s", hit, string(data))
	}

	// Read after expiration at t0 + 3s -> should miss and be pruned
	data, hit = srv.getOverviewCache(key, t0.Add(3*time.Second))
	if hit {
		t.Fatalf("expected cache miss after TTL expiration, got data=%s", string(data))
	}

	// Verify entry was purged
	srv.overviewCacheMu.RLock()
	_, exists := srv.overviewCache[key]
	srv.overviewCacheMu.RUnlock()
	if exists {
		t.Fatalf("expected expired entry to be purged from map")
	}
}

func TestOverviewCacheCapacityEviction(t *testing.T) {
	srv := &Server{
		overviewCache: make(map[string]overviewCacheEntry),
	}

	baseTime := time.Now().UTC()

	// Insert more than maxOverviewCacheEntries (128)
	for i := 0; i < 150; i++ {
		key := fmt.Sprintf("key-%d", i)
		ts := baseTime.Add(time.Duration(i) * time.Millisecond)
		srv.setOverviewCache(key, []byte(key), 1*time.Hour, ts)
	}

	srv.overviewCacheMu.RLock()
	total := len(srv.overviewCache)
	srv.overviewCacheMu.RUnlock()

	if total > maxOverviewCacheEntries {
		t.Fatalf("expected cache size bounded to %d, got %d", maxOverviewCacheEntries, total)
	}

	// Earliest entries (e.g. key-0, key-1) should have been evicted
	data, hit := srv.getOverviewCache("key-0", baseTime.Add(time.Hour))
	if hit {
		t.Fatalf("expected oldest entry key-0 to be evicted, but got data=%s", string(data))
	}

	// Latest entries (e.g. key-149) should still exist
	_, hit = srv.getOverviewCache("key-149", baseTime.Add(time.Hour))
	if !hit {
		t.Fatalf("expected newest entry key-149 to still exist in cache")
	}
}

func TestOverviewCacheScopeIsolation(t *testing.T) {
	now := time.Now().UTC()

	// 1. Anonymous / Public
	reqAnon := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	keyAnon, _, _ := buildOverviewCacheKey(reqAnon, now)

	// 2. Admin User
	adminUser := db.AdminUser{
		ID:           "admin-1",
		Role:         "admin",
		AllowedNodes: "*",
	}
	ctxAdmin := context.WithValue(context.Background(), currentAdminUserKey, adminUser)
	reqAdmin := httptest.NewRequest(http.MethodGet, "/api/overview", nil).WithContext(ctxAdmin)
	keyAdmin, _, _ := buildOverviewCacheKey(reqAdmin, now)

	// 3. Restricted User
	subUser := db.AdminUser{
		ID:           "user-2",
		Role:         "user",
		AllowedNodes: "node-uuid-a,node-uuid-b",
	}
	ctxSub := context.WithValue(context.Background(), currentAdminUserKey, subUser)
	reqSub := httptest.NewRequest(http.MethodGet, "/api/overview", nil).WithContext(ctxSub)
	keySub, _, _ := buildOverviewCacheKey(reqSub, now)

	// Ensure all 3 scopes have completely different keys
	if keyAnon == keyAdmin {
		t.Fatalf("public and admin scopes collided on key: %q", keyAnon)
	}
	if keyAdmin == keySub {
		t.Fatalf("admin and restricted user scopes collided on key: %q", keyAdmin)
	}
	if keyAnon == keySub {
		t.Fatalf("public and restricted user scopes collided on key: %q", keyAnon)
	}

	if !strings.HasPrefix(keyAnon, "public:anon:") {
		t.Fatalf("expected public key to start with 'public:anon:', got %q", keyAnon)
	}
	if !strings.HasPrefix(keyAdmin, "admin:admin-1:") {
		t.Fatalf("expected admin key to start with 'admin:admin-1:', got %q", keyAdmin)
	}
	if !strings.HasPrefix(keySub, "user:user-2:node-uuid-a,node-uuid-b:") {
		t.Fatalf("expected sub-user key to include node scope, got %q", keySub)
	}
}
