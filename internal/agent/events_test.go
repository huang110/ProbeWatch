package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestEventSignature(t *testing.T) {
	sig1 := eventSig("oom", "Killed process 123", "kernel")
	sig2 := eventSig("oom", "Killed process 123", "kernel")
	sig3 := eventSig("oom", "Killed process 456", "kernel")

	if sig1 != sig2 {
		t.Fatalf("identical inputs generated different signatures: %s vs %s", sig1, sig2)
	}
	if sig1 == sig3 {
		t.Fatalf("different inputs generated identical signature: %s", sig1)
	}
}

func TestEventCollectorDeduplication(t *testing.T) {
	collector := &EventCollector{
		seenSignatures: make(map[string]int64),
	}

	sig := eventSig("ssh_auth", "SSH failed from 1.1.1.1", "sshd")
	nowSec := time.Now().Unix()

	if !collector.isNewEvent(sig, nowSec) {
		t.Fatal("first encounter must be new")
	}

	if collector.isNewEvent(sig, nowSec) {
		t.Fatal("second encounter must be deduplicated")
	}
}

func TestQuerySystemLogsParameterValidation(t *testing.T) {
	ctx := context.Background()

	// Test invalid unit with unsafe characters
	invalidReq := protocol.LogQueryRequest{
		NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
		Unit:     "nginx; rm -rf /",
	}
	_, err := QuerySystemLogs(ctx, invalidReq)
	if err == nil || !strings.Contains(err.Error(), "invalid unit") {
		t.Fatalf("expected invalid unit error, got: %v", err)
	}

	// Test invalid priority
	invalidPri := protocol.LogQueryRequest{
		NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
		Priority: "malicious_priority",
	}
	_, err = QuerySystemLogs(ctx, invalidPri)
	if err == nil || !strings.Contains(err.Error(), "invalid priority") {
		t.Fatalf("expected invalid priority error, got: %v", err)
	}

	// Valid query fallback
	validReq := protocol.LogQueryRequest{
		NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
		Lines:    10,
	}
	resp, err := QuerySystemLogs(ctx, validReq)
	if err != nil {
		t.Fatalf("unexpected error on valid query: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}
