package notify

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// FlappingTarget contains live information about an alert target that is currently flapping.
type FlappingTarget struct {
	Key             string     `json:"key"`
	NodeID          string     `json:"node_id"`
	Fingerprint     string     `json:"fingerprint"`
	Transitions     int        `json:"transitions"`
	FirstTransition time.Time  `json:"first_transition"`
	LastTransition  time.Time  `json:"last_transition"`
	Flapping        bool       `json:"flapping"`
	FlappingSince   *time.Time `json:"flapping_since,omitempty"`
}

// FlappingTracker monitors rapid state oscillations (flapping) between alert open and resolved states.
type FlappingTracker struct {
	mu            sync.Mutex
	window        time.Duration // Sliding time window to count transitions (default 5m)
	coolingWindow time.Duration // Cooldown duration with no transitions to declare recovery (default 3m)
	threshold     int           // State transitions required to trigger flapping (default 4)
	transitions   map[string][]time.Time
	flappingState map[string]time.Time
	flappingSent  map[string]bool
	lastStatus    map[string]string
}

// NewFlappingTracker initializes a FlappingTracker with standard thresholds.
func NewFlappingTracker() *FlappingTracker {
	return &FlappingTracker{
		window:        5 * time.Minute,
		coolingWindow: 3 * time.Minute,
		threshold:     4,
		transitions:   make(map[string][]time.Time),
		flappingState: make(map[string]time.Time),
		flappingSent:  make(map[string]bool),
		lastStatus:    make(map[string]string),
	}
}

// SetThresholds overrides default parameters (useful for testing).
func (ft *FlappingTracker) SetThresholds(window, cooling time.Duration, threshold int) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.window = window
	ft.coolingWindow = cooling
	ft.threshold = threshold
}

// RecordAlert updates the state history for an alert.
// Returns isFlapping=true if the alert is in flapping suppression,
// and justEnteredFlapping=true if this transition just crossed the flapping threshold.
func (ft *FlappingTracker) RecordAlert(nodeID, fingerprint, status string, now time.Time) (bool, bool) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	key := fmt.Sprintf("%s:%s", nodeID, fingerprint)
	prevStatus, hadStatus := ft.lastStatus[key]
	isTransition := hadStatus && prevStatus != status

	if isTransition {
		ft.transitions[key] = append(ft.transitions[key], now)
	}
	ft.lastStatus[key] = status

	// Prune transitions outside the sliding window
	var recent []time.Time
	for _, t := range ft.transitions[key] {
		if now.Sub(t) <= ft.window {
			recent = append(recent, t)
		}
	}
	if len(recent) > 0 {
		ft.transitions[key] = recent
	} else {
		delete(ft.transitions, key)
	}

	// Check if already in flapping state
	if since, wasFlapping := ft.flappingState[key]; wasFlapping {
		var lastTransition time.Time
		if len(ft.transitions[key]) > 0 {
			lastTransition = ft.transitions[key][len(ft.transitions[key])-1]
		} else {
			lastTransition = since
		}

		// If stable for cooling window, declare recovery
		if now.Sub(lastTransition) > ft.coolingWindow {
			delete(ft.flappingState, key)
			delete(ft.flappingSent, key)
			delete(ft.transitions, key)
			return false, false
		}

		// Still flapping
		if !ft.flappingSent[key] {
			ft.flappingSent[key] = true
			return true, true
		}
		return true, false
	}

	// Not flapping yet, check if recent transitions reached threshold
	if len(ft.transitions[key]) >= ft.threshold {
		ft.flappingState[key] = now
		ft.flappingSent[key] = true
		return true, true
	}

	return false, false
}

// IsFlapping checks if an alert is currently suppressed due to flapping.
func (ft *FlappingTracker) IsFlapping(nodeID, fingerprint string, now time.Time) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	key := fmt.Sprintf("%s:%s", nodeID, fingerprint)
	since, wasFlapping := ft.flappingState[key]
	if !wasFlapping {
		return false
	}

	var lastTransition time.Time
	if len(ft.transitions[key]) > 0 {
		lastTransition = ft.transitions[key][len(ft.transitions[key])-1]
	} else {
		lastTransition = since
	}

	if now.Sub(lastTransition) > ft.coolingWindow {
		delete(ft.flappingState, key)
		delete(ft.flappingSent, key)
		delete(ft.transitions, key)
		return false
	}

	return true
}

// GetActiveFlapping returns all alerts currently marked as flapping.
func (ft *FlappingTracker) GetActiveFlapping() []FlappingTarget {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	now := time.Now().UTC()
	var targets []FlappingTarget

	for key, since := range ft.flappingState {
		ts := ft.transitions[key]
		var last time.Time
		var first time.Time
		if len(ts) > 0 {
			first = ts[0]
			last = ts[len(ts)-1]
		} else {
			first = since
			last = since
		}

		if now.Sub(last) > ft.coolingWindow {
			delete(ft.flappingState, key)
			delete(ft.flappingSent, key)
			delete(ft.transitions, key)
			continue
		}

		parts := strings.SplitN(key, ":", 2)
		nodeID := parts[0]
		fp := ""
		if len(parts) > 1 {
			fp = parts[1]
		}
		sinceCopy := since
		targets = append(targets, FlappingTarget{
			Key:             key,
			NodeID:          nodeID,
			Fingerprint:     fp,
			Transitions:     len(ts),
			FirstTransition: first,
			LastTransition:  last,
			Flapping:        true,
			FlappingSince:   &sinceCopy,
		})
	}

	if targets == nil {
		targets = []FlappingTarget{}
	}
	return targets
}

// Reset clears all recorded history and flapping states.
func (ft *FlappingTracker) Reset() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.transitions = make(map[string][]time.Time)
	ft.flappingState = make(map[string]time.Time)
	ft.flappingSent = make(map[string]bool)
	ft.lastStatus = make(map[string]string)
}
