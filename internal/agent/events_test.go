package agent

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		Unit:     "; rm -rf /",
	}
	_, err := QuerySystemLogs(ctx, invalidReq)
	if err == nil || !strings.Contains(err.Error(), "invalid unit") {
		t.Fatalf("expected invalid unit error, got: %v", err)
	}

	// Test overlong unit (> 64 chars)
	overlongUnit := protocol.LogQueryRequest{
		NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
		Unit:     strings.Repeat("a", 65),
	}
	_, err = QuerySystemLogs(ctx, overlongUnit)
	if err == nil || !strings.Contains(err.Error(), "invalid unit") {
		t.Fatalf("expected overlong unit error, got: %v", err)
	}

	// Test malicious since injection
	invalidSince := protocol.LogQueryRequest{
		NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
		Since:    "; reboot",
	}
	_, err = QuerySystemLogs(ctx, invalidSince)
	if err == nil || !strings.Contains(err.Error(), "invalid since") {
		t.Fatalf("expected invalid since error on '; reboot', got: %v", err)
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

	// Test malicious grep containing null byte
	nullGrep := protocol.LogQueryRequest{
		NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
		Grep:     "valid\x00malicious",
	}
	_, err = QuerySystemLogs(ctx, nullGrep)
	if err == nil || !strings.Contains(err.Error(), "disallowed") {
		t.Fatalf("expected error on grep containing null byte, got: %v", err)
	}

	// Test command substitution attempt in grep: BuildJournalctlArgs must pass it as structured literal argument
	cmdSubstReq := protocol.LogQueryRequest{
		NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
		Grep:     "$(whoami)",
		Lines:    5000, // tests lines truncation
	}
	args, err := BuildJournalctlArgs(cmdSubstReq)
	if err != nil {
		t.Fatalf("unexpected error on BuildJournalctlArgs: %v", err)
	}
	var foundGrep, foundLines bool
	for i, a := range args {
		if a == "--grep=$(whoami)" {
			foundGrep = true
		}
		if a == "-n" && i+1 < len(args) && args[i+1] == "1000" {
			foundLines = true
		}
	}
	if !foundGrep {
		t.Fatalf("expected literal structured arg '--grep=$(whoami)', got: %v", args)
	}
	if !foundLines {
		t.Fatalf("expected lines 5000 to be clamped to 1000, got: %v", args)
	}

	// Test unit parameter injection attacks
	for _, badUnit := range []string{"nginx; id", "nginx && whoami", "$(id)", "`whoami`", "nginx|cat", "nginx\nid"} {
		_, err := BuildJournalctlArgs(protocol.LogQueryRequest{
			NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
			Unit:     badUnit,
		})
		if err == nil || !strings.Contains(err.Error(), "invalid unit name") {
			t.Fatalf("expected invalid unit name error for %q, got: %v", badUnit, err)
		}
	}

	// Test since parameter injection attacks
	for _, badSince := range []string{"1h; id", "1h && whoami", "$(id)", "`whoami`", "1h|cat"} {
		_, err := BuildJournalctlArgs(protocol.LogQueryRequest{
			NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
			Since:    badSince,
		})
		if err == nil || !strings.Contains(err.Error(), "invalid since parameter") {
			t.Fatalf("expected invalid since error for %q, got: %v", badSince, err)
		}
	}

	// Test grep parameter newline and carriage return rejections
	for _, badGrep := range []string{"err\nmalicious", "err\rmalicious", strings.Repeat("A", 129)} {
		_, err := BuildJournalctlArgs(protocol.LogQueryRequest{
			NodeUUID: "123e4567-e89b-12d3-a456-426614174000",
			Grep:     badGrep,
		})
		if err == nil {
			t.Fatalf("expected error for bad grep %q, got nil", badGrep)
		}
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

func TestReadLogTailSmallFile(t *testing.T) {
	content := "line 1: boot sequence\nline 2: services started\nline 3: ready\n"
	r := bytes.NewReader([]byte(content))

	// Small file: size <= chunkSize, must retain full first line
	lines := readLogTailChunk(r, int64(len(content)), 1024, 10)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line 1: boot sequence" {
		t.Fatalf("expected first line to be preserved for small file, got %q", lines[0])
	}
	if lines[2] != "line 3: ready" {
		t.Fatalf("expected last line %q, got %q", "line 3: ready", lines[2])
	}
}

func TestReadLogTailLargeFileTruncationDiscardsPartialFirstLine(t *testing.T) {
	// Full simulated log with a partial cut at chunkSize bytes from end
	prefix := "0123456789prefix_cut_in_middle_of_line_"
	chunkContent := "PARTIAL_BROKEN_SUFFIX\nline 2: fully intact\nline 3: fully intact\n"
	fullContent := prefix + chunkContent

	r := bytes.NewReader([]byte(fullContent))
	chunkSize := int64(len(chunkContent))

	// Size > chunkSize triggers seeking into the middle of the first line
	lines := readLogTailChunk(r, int64(len(fullContent)), chunkSize, 10)

	if len(lines) != 2 {
		t.Fatalf("expected 2 complete lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line 2: fully intact" {
		t.Fatalf("expected partial first line to be discarded and line 2 returned, got %q", lines[0])
	}
	if lines[1] != "line 3: fully intact" {
		t.Fatalf("expected line 3 to be complete, got %q", lines[1])
	}
}

func TestReadLogTailNoNewlineInChunk(t *testing.T) {
	content := "PREFIX_TRUNCATED_BLOCK_WITHOUT_ANY_NEWLINE_IN_THE_TAIL_CHUNK"
	r := bytes.NewReader([]byte(content))

	// Chunk has no newlines at all and is truncated
	lines := readLogTailChunk(r, int64(len(content)), 25, 10)
	if len(lines) != 0 {
		t.Fatalf("expected empty lines on truncated block with no newline, got: %v", lines)
	}
}

func TestReadLogTailEmptyFile(t *testing.T) {
	r := bytes.NewReader([]byte(""))
	lines := readLogTailChunk(r, 0, 1024, 10)
	if lines != nil {
		t.Fatalf("expected nil for empty reader, got %v", lines)
	}

	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.log")
	_ = os.WriteFile(emptyPath, []byte(""), 0600)

	fileLines := readLogTail(emptyPath, 10)
	if fileLines != nil {
		t.Fatalf("expected nil for empty file on disk, got %v", fileLines)
	}
}

func TestReadLogTailSingleLineTruncated(t *testing.T) {
	content := "LONG_PREFIX_BEFORE_CUT_SINGLE_LINE_NO_NEWLINE"
	r := bytes.NewReader([]byte(content))

	// Truncated and only 1 partial line
	lines := readLogTailChunk(r, int64(len(content)), 15, 10)
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines when only a partial line exists after cut, got: %v", lines)
	}
}

func TestReadLogTailDiskFileIntegration(t *testing.T) {
	dir := t.TempDir()
	testPath := filepath.Join(dir, "sys.log")
	content := "first log line\nsecond log line\nthird log line\n"
	_ = os.WriteFile(testPath, []byte(content), 0600)

	lines := readLogTail(testPath, 2)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines with maxLines=2, got %d: %v", len(lines), lines)
	}
	if lines[0] != "second log line" || lines[1] != "third log line" {
		t.Fatalf("expected second and third lines, got: %v", lines)
	}
}

func TestScanSystemdFailuresNoDuplicateReplay(t *testing.T) {
	ctx := context.Background()

	var outputToReturn string
	collector := &EventCollector{
		seenSignatures: make(map[string]int64),
		systemdRunner: func(ctx context.Context) (io.Reader, error) {
			return strings.NewReader(outputToReturn), nil
		},
	}

	// 1. Initial scan with failure: must return event
	outputToReturn = "nginx.service loaded failed failed Nginx Web Server\n"
	t0 := int64(1000)
	eventsT0 := collector.scanSystemdFailures(ctx, t0)
	if len(eventsT0) != 1 {
		t.Fatalf("expected 1 event at t0, got %d", len(eventsT0))
	}
	if !strings.Contains(eventsT0[0].Title, "nginx.service") {
		t.Fatalf("unexpected event title: %s", eventsT0[0].Title)
	}

	// 2. Cache hits at 10s, 20s, 30s, 40s, 50s: must return nil/empty
	for _, offset := range []int64{10, 20, 30, 40, 50} {
		hits := collector.scanSystemdFailures(ctx, t0+offset)
		if len(hits) != 0 {
			t.Fatalf("expected empty events during cache window at offset +%ds, got %d", offset, len(hits))
		}
	}

	// 3. Modifying returned events does not mutate internal cachedSystemdEvents
	eventsT0[0].Title = "MUTATED"
	if len(collector.cachedSystemdEvents) > 0 && collector.cachedSystemdEvents[0].Title == "MUTATED" {
		t.Fatal("mutating returned slice corrupted internal cachedSystemdEvents")
	}

	// 4. At 60s: re-scan occurs. Same failure is still present, but 24h deduplication suppresses it
	t60 := t0 + 60
	eventsT60 := collector.scanSystemdFailures(ctx, t60)
	if len(eventsT60) != 0 {
		t.Fatalf("expected 0 events at t60 due to 24h deduplication, got %d", len(eventsT60))
	}

	// 5. Failure disappears at t120
	t120 := t0 + 120
	outputToReturn = "" // no failed units
	eventsT120 := collector.scanSystemdFailures(ctx, t120)
	if len(eventsT120) != 0 {
		t.Fatalf("expected 0 events when no units failed, got %d", len(eventsT120))
	}

	// 6. 24h later: failure reappears. Signature should be pruned and new event generated
	t24h := t0 + 86400 + 10
	collector.pruneSignatures(t24h)
	outputToReturn = "nginx.service loaded failed failed Nginx Web Server\n"
	eventsReappear := collector.scanSystemdFailures(ctx, t24h)
	if len(eventsReappear) != 1 {
		t.Fatalf("expected 1 new event after 24h expiry, got %d", len(eventsReappear))
	}
}

func TestScanSystemdFailuresConcurrencyRace(t *testing.T) {
	ctx := context.Background()
	collector := &EventCollector{
		seenSignatures: make(map[string]int64),
		systemdRunner: func(ctx context.Context) (io.Reader, error) {
			return strings.NewReader("redis.service loaded failed failed Redis In-Memory Store\n"), nil
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(iter int) {
			defer wg.Done()
			collector.mu.Lock()
			_ = collector.scanSystemdFailures(ctx, int64(1000+iter*5))
			collector.mu.Unlock()
		}(i)
	}
	wg.Wait()
}
