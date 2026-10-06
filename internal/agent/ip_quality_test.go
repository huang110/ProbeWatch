package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestParseIPQuality_IPWhoIs(t *testing.T) {
	payload := []byte(`{
		"ip": "103.159.207.11",
		"success": true,
		"type": "IPv4",
		"continent": "Asia",
		"continent_code": "AS",
		"country": "Taiwan",
		"country_code": "TW",
		"region": "Taipei",
		"region_code": "TPE",
		"city": "Taipei",
		"connection": {
			"asn": 31972,
			"org": "Taiwan Internet Technology Co., Ltd.",
			"isp": "Emagine Concept, Inc.",
			"domain": "ttns.com.tw"
		},
		"security": {
			"vpn": false,
			"proxy": false,
			"tor": false,
			"hosting": true
		}
	}`)

	info, err := ParseIPQualityResponse(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Country != "TW" {
		t.Errorf("expected Country TW, got %s", info.Country)
	}
	if info.Region != "Taipei" {
		t.Errorf("expected Region Taipei, got %s", info.Region)
	}
	if info.ASN != "AS31972" {
		t.Errorf("expected ASN AS31972, got %s", info.ASN)
	}
	if info.Organization != "Taiwan Internet Technology Co., Ltd." {
		t.Errorf("expected Org Taiwan Internet Technology Co., Ltd., got %s", info.Organization)
	}
	if info.IPType != "hosting" {
		t.Errorf("expected IPType hosting, got %s", info.IPType)
	}
	if info.Proxy == nil || *info.Proxy != false {
		t.Errorf("expected Proxy false, got %v", info.Proxy)
	}
	if info.Risk != "low" {
		t.Errorf("expected Risk low, got %s", info.Risk)
	}
}

func TestParseIPQuality_IPInfo(t *testing.T) {
	payload := []byte(`{
		"ip": "103.159.207.11",
		"city": "Taipei",
		"region": "Taiwan",
		"country": "TW",
		"loc": "25.0531,121.5264",
		"org": "AS31972 Emagine Concept, Inc.",
		"timezone": "Asia/Taipei"
	}`)

	info, err := ParseIPQualityResponse(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Country != "TW" {
		t.Errorf("expected Country TW, got %s", info.Country)
	}
	if info.ASN != "AS31972" {
		t.Errorf("expected ASN AS31972, got %s", info.ASN)
	}
	if info.Organization != "Emagine Concept, Inc." {
		t.Errorf("expected Org Emagine Concept, Inc., got %s", info.Organization)
	}
}

func TestParseIPQuality_IPAPI_Is(t *testing.T) {
	payload := []byte(`{
		"ip": "8.8.8.8",
		"company": "Google LLC",
		"asn": "AS15169 Google LLC",
		"city": "Mountain View",
		"region": "California",
		"country": "United States",
		"is_datacenter": true,
		"is_vpn": false,
		"is_proxy": false,
		"is_tor": false,
		"is_abuser": false
	}`)

	info, err := ParseIPQualityResponse(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Country != "United States" {
		t.Errorf("expected Country United States, got %s", info.Country)
	}
	if info.Region != "California" {
		t.Errorf("expected Region California, got %s", info.Region)
	}
	if info.ASN != "AS15169" {
		t.Errorf("expected ASN AS15169, got %s", info.ASN)
	}
	if info.Organization != "Google LLC" {
		t.Errorf("expected Org Google LLC, got %s", info.Organization)
	}
	if info.IPType != "hosting" {
		t.Errorf("expected IPType hosting, got %s", info.IPType)
	}
	if info.Risk != "low" {
		t.Errorf("expected Risk low, got %s", info.Risk)
	}
}

func TestParseIPQuality_IPQueryWithScores(t *testing.T) {
	payload := []byte(`{
		"ip": "1.2.3.4",
		"location": { "country_code": "US", "state": "California" },
		"isp": { "asn": "AS1234", "org": "Example ISP", "isp": "Example ISP" },
		"risk": { "is_proxy": true, "is_vpn": true, "is_tor": false, "is_datacenter": false, "risk_score": 85 }
	}`)

	info, err := ParseIPQualityResponse(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Country != "US" {
		t.Errorf("expected Country US, got %s", info.Country)
	}
	if info.Region != "California" {
		t.Errorf("expected Region California, got %s", info.Region)
	}
	if info.ASN != "AS1234" {
		t.Errorf("expected ASN AS1234, got %s", info.ASN)
	}
	if info.Proxy == nil || *info.Proxy != true {
		t.Errorf("expected Proxy true, got %v", info.Proxy)
	}
	if info.Risk != "high" {
		t.Errorf("expected Risk high (score 85), got %s", info.Risk)
	}
	if info.Sources == nil || info.Sources["risk_score"] != 85 {
		t.Errorf("expected Sources risk_score 85, got %v", info.Sources)
	}
}

func TestParseIPQuality_MissingFieldsNoPanic(t *testing.T) {
	testCases := [][]byte{
		[]byte(`{}`),
		[]byte(`{"success": true}`),
		[]byte(`{"ip": null, "country": "", "connection": null, "security": null}`),
		[]byte(`{"type": "unknown", "asn": 0, "org": "", "risk": "unknown"}`),
	}

	for i, tc := range testCases {
		info, err := ParseIPQualityResponse(tc)
		if err != nil {
			t.Errorf("case %d: unexpected error: %v", i, err)
		}
		if info == nil {
			t.Errorf("case %d: expected non-nil info", i)
		}
	}
}

func TestCollector_NonBlockingTimeout(t *testing.T) {
	c := &DefaultIPQualityCollector{
		cacheTTL: 1 * time.Hour,
		customFetch: func(ctx context.Context, endpoint string) (*protocol.IPQualityInfo, error) {
			select {
			case <-time.After(2 * time.Second):
				return nil, errors.New("timeout")
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	start := time.Now()
	val := c.Get()
	elapsed := time.Since(start)

	if val != nil {
		t.Errorf("expected nil initial value, got %v", val)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("Get() blocked for %v, must be non-blocking (<100ms)", elapsed)
	}
}

func TestCollector_CacheTTLEffective(t *testing.T) {
	var fetchCount int32
	c := &DefaultIPQualityCollector{
		cacheTTL: 50 * time.Millisecond,
		customFetch: func(ctx context.Context, endpoint string) (*protocol.IPQualityInfo, error) {
			atomic.AddInt32(&fetchCount, 1)
			return &protocol.IPQualityInfo{
				IPType: "hosting",
				ASN:    "AS1234",
				Risk:   "low",
			}, nil
		},
	}

	// First call triggers async fetch
	_ = c.Get()
	time.Sleep(20 * time.Millisecond)

	// Second call should return cached value
	firstVal := c.Get()
	if firstVal == nil || firstVal.ASN != "AS1234" {
		t.Fatalf("expected cached ASN AS1234, got %v", firstVal)
	}
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount)
	}

	// Third call within TTL should reuse cache without refetching
	_ = c.Get()
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected 1 fetch within TTL, got %d", fetchCount)
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Call after expiration returns old cache while triggering background refresh
	oldVal := c.Get()
	if oldVal == nil || oldVal.ASN != "AS1234" {
		t.Fatalf("expected old cached value returned, got %v", oldVal)
	}

	time.Sleep(20 * time.Millisecond)
	if atomic.LoadInt32(&fetchCount) < 2 {
		t.Fatalf("expected refresh fetch triggered after TTL expiration, got %d", fetchCount)
	}
}

func TestCollector_PreservesPreviousOnFailure(t *testing.T) {
	var shouldFail atomic.Bool
	c := &DefaultIPQualityCollector{
		cacheTTL: 10 * time.Millisecond,
		customFetch: func(ctx context.Context, endpoint string) (*protocol.IPQualityInfo, error) {
			if shouldFail.Load() {
				return nil, errors.New("upstream provider error 500")
			}
			return &protocol.IPQualityInfo{
				IPType: "hosting",
				ASN:    "AS31972",
				Risk:   "low",
			}, nil
		},
	}

	// First fetch succeeds
	_ = c.Get()
	time.Sleep(20 * time.Millisecond)

	val := c.Get()
	if val == nil || val.ASN != "AS31972" {
		t.Fatalf("expected ASN AS31972, got %v", val)
	}

	// Now fail subsequent fetch
	shouldFail.Store(true)
	time.Sleep(20 * time.Millisecond)

	// Trigger fetch which will fail
	_ = c.Get()
	time.Sleep(20 * time.Millisecond)

	// Value should still be preserved
	valAfterFailure := c.Get()
	if valAfterFailure == nil || valAfterFailure.ASN != "AS31972" {
		t.Fatalf("expected preserved ASN AS31972 on failure, got %v", valAfterFailure)
	}
}

func TestCollector_NilWhenNeverSucceeded(t *testing.T) {
	c := &DefaultIPQualityCollector{
		cacheTTL: 1 * time.Hour,
		customFetch: func(ctx context.Context, endpoint string) (*protocol.IPQualityInfo, error) {
			return nil, errors.New("initial connection failed")
		},
	}

	val := c.Get()
	if val != nil {
		t.Fatalf("expected nil when never succeeded, got %v", val)
	}

	time.Sleep(20 * time.Millisecond)
	val = c.Get()
	if val != nil {
		t.Fatalf("expected nil after failure when never succeeded, got %v", val)
	}
}
