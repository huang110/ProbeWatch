package monitor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/probewatch/probewatch/internal/protocol"
)

func mediaTask() protocol.CheckTask {
	return protocol.CheckTask{ID: "media-1", Kind: "media_http", Host: "media.example.com", Port: 443, Path: "/manifest", ExpectedStatus: 200, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
}

type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestMediaDetectorUsesGETAndDefaultsDetector(t *testing.T) {
	var got *http.Request
	body := strings.Repeat("x", 8)
	d := &MediaDetector{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			got = req
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "available" || result.Detector != "media-1" {
		t.Fatalf("result = %#v, want available with task detector", result)
	}
	if got == nil || got.Method != http.MethodGet || got.Body != nil {
		t.Fatalf("request = %#v, want bodyless GET", got)
	}
}

func TestMediaDetectorRejectsBodyOverLimitBeforeStatus(t *testing.T) {
	d := &MediaDetector{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 9))), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "error" || result.Reason != "body exceeds limit" {
		t.Fatalf("result = %#v, want fixed body limit error", result)
	}
}

func TestMediaDetectorRejectsBodyOverLimitEvenOnErrorStatus(t *testing.T) {
	d := &MediaDetector{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 9))), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "error" || result.Reason != "body exceeds limit" {
		t.Fatalf("result = %#v, want fixed body limit error", result)
	}
}

func TestMediaDetectorReturnsReadErrorWithoutStatus判定(t *testing.T) {
	readErr := errors.New("read failed")
	d := &MediaDetector{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&errorReader{err: readErr}), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "error" || result.Reason != readErr.Error() {
		t.Fatalf("result = %#v, want body read error", result)
	}
}

func TestMediaDetectorAcceptsExactBodyLimit(t *testing.T) {
	d := &MediaDetector{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 8))), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "available" || result.Reason != "" {
		t.Fatalf("result = %#v, want available at exact limit", result)
	}
}
func TestMediaDetectorMapsStatusMismatchToUnavailable(t *testing.T) {
	d := &MediaDetector{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "unavailable" {
		t.Fatalf("status = %q, want unavailable", result.Status)
	}
}

func TestMediaDetectorBlocksRedirectHost(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{
		{publicAddress(t, "93.184.216.34")},
		{publicAddress(t, "10.0.0.1")},
	}}
	d := &MediaDetector{Resolver: resolver, roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://private.example/manifest"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: req}, nil
	})}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %s", result.Status, result.Reason)
	}
	if len(resolver.hosts) != 2 {
		t.Fatalf("resolver hosts = %#v, want initial and redirect host", resolver.hosts)
	}
}

func TestMediaDetectorInvalidTaskDoesNotResolve(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}}
	task := mediaTask()
	task.Path = "relative"
	result := (&MediaDetector{Resolver: resolver}).Run(context.Background(), task)
	if result.Status != "invalid" {
		t.Fatalf("status = %q, want invalid", result.Status)
	}
	if len(resolver.hosts) != 0 {
		t.Fatal("invalid task reached DNS")
	}
}

func TestMediaDetectorMatchesRegionFromBodyPrefix(t *testing.T) {
	task := mediaTask()
	task.RegionRules = []protocol.RegionRule{
		{Region: "US", Contains: "geo-US"},
		{Region: "SG", Contains: "geo-SG"},
	}
	d := &MediaDetector{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("xgeo-SGx")), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), task)
	if result.Status != "available" || result.Region != "SG" {
		t.Fatalf("result = %#v, want available with region SG", result)
	}
}

func TestMediaDetectorRegionFollowsRuleOrder(t *testing.T) {
	task := mediaTask()
	task.RegionRules = []protocol.RegionRule{
		{Region: "US", Contains: "geo-US"},
		{Region: "SG", Contains: "geo-SG"},
	}
	d := &MediaDetector{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("geo-US and geo-SG")), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), task)
	if result.Region != "US" {
		t.Fatalf("region = %q, want first matching rule US", result.Region)
	}
}

func TestMediaDetectorLeavesRegionEmptyOnMiss(t *testing.T) {
	task := mediaTask()
	task.RegionRules = []protocol.RegionRule{{Region: "SG", Contains: "geo-SG"}}
	d := &MediaDetector{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("no marker here")), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), task)
	if result.Status != "available" || result.Region != "" {
		t.Fatalf("result = %#v, want available with empty region", result)
	}
}

func TestMediaDetectorWithoutRulesKeepsRegionEmpty(t *testing.T) {
	d := &MediaDetector{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("geo-SG")), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), mediaTask())
	if result.Status != "available" || result.Region != "" {
		t.Fatalf("result = %#v, want available with empty region when no rules configured", result)
	}
}

func TestMediaDetectorRegionOnlyMatchesWithinBodyPrefix(t *testing.T) {
	task := mediaTask()
	task.RegionRules = []protocol.RegionRule{{Region: "SG", Contains: "geo-SG"}}
	marker := "geo-SG"
	// Body whose marker starts exactly at the prefix boundary: only the first
	// mediaRegionProbeBytes bytes may ever be inspected.
	pastBody := strings.Repeat("x", mediaRegionProbeBytes) + marker
	// Body whose marker ends exactly at the prefix boundary.
	edgeBody := strings.Repeat("x", mediaRegionProbeBytes-len(marker)) + marker + strings.Repeat("y", 8)
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "marker beyond prefix is ignored", body: pastBody, want: ""},
		{name: "marker inside prefix matches", body: edgeBody, want: "SG"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &MediaDetector{
				Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
				MaxBodyBytes: int64(len(test.body)),
				roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header), Request: req}, nil
				}),
			}
			result := d.Run(context.Background(), task)
			if result.Status != "available" || result.Region != test.want {
				t.Fatalf("result = %#v, want available with region %q", result, test.want)
			}
		})
	}
}

func TestMediaDetectorRegionFillsOnUnavailableStatus(t *testing.T) {
	task := mediaTask()
	task.RegionRules = []protocol.RegionRule{{Region: "SG", Contains: "geo-blocked"}}
	d := &MediaDetector{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("geo-blocked")), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), task)
	if result.Status != "unavailable" || result.Region != "SG" {
		t.Fatalf("result = %#v, want unavailable with region SG", result)
	}
}

func TestMediaDetectorOverLimitBodyLeavesRegionEmpty(t *testing.T) {
	task := mediaTask()
	task.RegionRules = []protocol.RegionRule{{Region: "SG", Contains: "geo-SG"}}
	d := &MediaDetector{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("geo-SG-xx")), Header: make(http.Header), Request: req}, nil
		}),
	}
	result := d.Run(context.Background(), task)
	if result.Status != "error" || result.Region != "" {
		t.Fatalf("result = %#v, want error with empty region for over-limit body", result)
	}
}

func TestMediaDetectorInvalidRegionRulesAreRejectedAsInvalid(t *testing.T) {
	task := mediaTask()
	task.RegionRules = []protocol.RegionRule{{Region: "SG!", Contains: "geo-SG"}}
	d := &MediaDetector{Resolver: &fakeResolver{}}
	result := d.Run(context.Background(), task)
	if result.Status != "invalid" {
		t.Fatalf("status = %q, want invalid for malformed rules", result.Status)
	}
	if len(d.Resolver.(*fakeResolver).hosts) != 0 {
		t.Fatal("invalid rules reached DNS")
	}
}
