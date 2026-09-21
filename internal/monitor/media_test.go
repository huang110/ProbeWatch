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
