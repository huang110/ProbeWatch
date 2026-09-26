package notify

import (
	"testing"
	"time"
)

func TestFlappingTracker(t *testing.T) {
	tracker := NewFlappingTracker()
	// Set test-friendly thresholds: window 5m, cooling 3m, threshold 4
	window := 5 * time.Minute
	cooling := 3 * time.Minute
	threshold := 4
	tracker.SetThresholds(window, cooling, threshold)

	nodeID := "node-flapping-1"
	fingerprint := "fp-cpu-fluctuation"
	now := time.Now().UTC()

	// 1. Initial report (open) -> no transition, not flapping
	isFlapping, justEntered := tracker.RecordAlert(nodeID, fingerprint, "open", now)
	if isFlapping || justEntered {
		t.Fatalf("expected 1st report not flapping, got isFlapping=%v, justEntered=%v", isFlapping, justEntered)
	}

	// 2. Transition 1: resolved -> not flapping
	now = now.Add(10 * time.Second)
	isFlapping, justEntered = tracker.RecordAlert(nodeID, fingerprint, "resolved", now)
	if isFlapping || justEntered {
		t.Fatalf("expected 1st transition not flapping")
	}

	// 3. Transition 2: open -> not flapping
	now = now.Add(10 * time.Second)
	isFlapping, justEntered = tracker.RecordAlert(nodeID, fingerprint, "open", now)
	if isFlapping || justEntered {
		t.Fatalf("expected 2nd transition not flapping")
	}

	// 4. Transition 3: resolved -> not flapping
	now = now.Add(10 * time.Second)
	isFlapping, justEntered = tracker.RecordAlert(nodeID, fingerprint, "resolved", now)
	if isFlapping || justEntered {
		t.Fatalf("expected 3rd transition not flapping")
	}

	// 5. Transition 4: open -> crosses threshold 4 within 5m! Should ENTER flapping!
	now = now.Add(10 * time.Second)
	isFlapping, justEntered = tracker.RecordAlert(nodeID, fingerprint, "open", now)
	if !isFlapping || !justEntered {
		t.Fatalf("expected 4th transition to trigger flapping, got isFlapping=%v, justEntered=%v", isFlapping, justEntered)
	}

	// Check IsFlapping
	if !tracker.IsFlapping(nodeID, fingerprint, now) {
		t.Fatalf("expected IsFlapping to return true")
	}

	// Active flapping targets
	targets := tracker.GetActiveFlapping()
	if len(targets) != 1 || targets[0].NodeID != nodeID || targets[0].Fingerprint != fingerprint {
		t.Fatalf("unexpected flapping targets: %+v", targets)
	}

	// 6. Transition 5: resolved -> still flapping, but NOT justEntered
	now = now.Add(10 * time.Second)
	isFlapping, justEntered = tracker.RecordAlert(nodeID, fingerprint, "resolved", now)
	if !isFlapping || justEntered {
		t.Fatalf("expected transition while flapping to maintain flapping without justEntered, got isFlapping=%v, justEntered=%v", isFlapping, justEntered)
	}

	// 7. Stable period > coolingWindow (3 minutes) -> automatic recovery!
	now = now.Add(cooling + 10*time.Second)
	if tracker.IsFlapping(nodeID, fingerprint, now) {
		t.Fatalf("expected alert to recover from flapping after cooling window")
	}
	if len(tracker.GetActiveFlapping()) != 0 {
		t.Fatalf("expected 0 active flapping targets after recovery")
	}
}
