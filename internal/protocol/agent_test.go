package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckTaskValidateAcceptsEveryAllowedKind(t *testing.T) {
	for _, kind := range []string{"tcp", "http", "https", "dns", "mtr", "media_http"} {
		t.Run(kind, func(t *testing.T) {
			task := validCheckTask()
			task.Kind = kind
			if err := task.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestCheckTaskValidateRejectsInvalidKindAndBounds(t *testing.T) {
	tests := []struct {
		name string
		edit func(*CheckTask)
	}{
		{name: "kind", edit: func(task *CheckTask) { task.Kind = "shell" }},
		{name: "port below minimum", edit: func(task *CheckTask) { task.Port = 0 }},
		{name: "port above maximum", edit: func(task *CheckTask) { task.Port = 65536 }},
		{name: "timeout below minimum", edit: func(task *CheckTask) { task.TimeoutMS = 99 }},
		{name: "timeout above maximum", edit: func(task *CheckTask) { task.TimeoutMS = 30001 }},
		{name: "max hops below minimum", edit: func(task *CheckTask) { task.MaxHops = 0 }},
		{name: "max hops above maximum", edit: func(task *CheckTask) { task.MaxHops = 31 }},
		{name: "interval below minimum", edit: func(task *CheckTask) { task.IntervalSeconds = 9 }},
		{name: "interval above maximum", edit: func(task *CheckTask) { task.IntervalSeconds = 86401 }},
		{name: "id too long", edit: func(task *CheckTask) { task.ID = strings.Repeat("a", maxIDLength+1) }},
		{name: "host too long", edit: func(task *CheckTask) { task.Host = strings.Repeat("a", maxHostLength+1) }},
		{name: "path too long", edit: func(task *CheckTask) { task.Path = strings.Repeat("a", maxPathLength+1) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := validCheckTask()
			test.edit(&task)
			if err := task.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid task")
			}
		})
	}
}

func TestCheckTaskValidateRequiresFieldsForTaskKind(t *testing.T) {
	tests := []struct {
		name string
		kind string
		edit func(*CheckTask)
	}{
		{name: "missing id", kind: "tcp", edit: func(task *CheckTask) { task.ID = "" }},
		{name: "missing host", kind: "tcp", edit: func(task *CheckTask) { task.Host = "" }},
		{name: "http path must start with slash", kind: "http", edit: func(task *CheckTask) { task.Path = "health" }},
		{name: "dns type", kind: "dns", edit: func(task *CheckTask) { task.DNSType = "TXT" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := validCheckTask()
			task.Kind = test.kind
			test.edit(&task)
			if err := task.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid task")
			}
		})
	}
}

func TestReportRequestValidateChecksNestedTasks(t *testing.T) {
	report := ReportRequest{
		NodeUUID:   "550e8400-e29b-41d4-a716-446655440000",
		ReportedAt: 1,
		Resource:   ResourceSnapshot{Hostname: "agent-1"},
		Results: []CheckResult{{
			ID:   "check-1",
			Kind: "tcp",
			Network: &NetworkResult{
				Host:      "example.com",
				Port:      443,
				CheckedAt: 1,
			},
		}},
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	report.Results[0].Kind = "exec"
	if err := report.Validate(); err == nil {
		t.Fatal("Validate() accepted an unallowlisted result kind")
	}
}

func TestResultValidateRequiresCheckedAt(t *testing.T) {
	tests := []struct {
		name   string
		result CheckResult
	}{
		{
			name: "network",
			result: CheckResult{ID: "check-1", Kind: "tcp", Network: &NetworkResult{
				Host: "example.com", Port: 443,
			}},
		},
		{
			name: "mtr",
			result: CheckResult{ID: "check-1", Kind: "mtr", MTR: &MTRResult{
				Host: "example.com",
			}},
		},
		{
			name: "media",
			result: CheckResult{ID: "check-1", Kind: "media_http", Media: &MediaResult{
				Detector: "custom", Status: "available",
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.result.Validate(); err == nil {
				t.Fatal("Validate() accepted a result without checked_at")
			}
		})
	}
}

func TestCheckResultValidateRequiresMatchingExactlyOnePayload(t *testing.T) {
	tests := []struct {
		name   string
		result CheckResult
	}{
		{
			name:   "empty payload",
			result: CheckResult{ID: "check-1", Kind: "tcp"},
		},
		{
			name: "network kind with mtr payload",
			result: CheckResult{ID: "check-1", Kind: "tcp", MTR: &MTRResult{
				Host: "example.com",
			}},
		},
		{
			name: "mtr kind with network payload",
			result: CheckResult{ID: "check-1", Kind: "mtr", Network: &NetworkResult{
				Host: "example.com", Port: 443,
			}},
		},
		{
			name: "media kind with network payload",
			result: CheckResult{ID: "check-1", Kind: "media_http", Network: &NetworkResult{
				Host: "example.com", Port: 443,
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.result.Validate(); err == nil {
				t.Fatal("Validate() accepted an invalid typed payload shape")
			}
		})
	}
}

func TestRegisterRequestRejectsResourceFieldsInSystem(t *testing.T) {
	body := []byte(`{"registration_token":"token","node_uuid":"550e8400-e29b-41d4-a716-446655440000","name":"node","system":{"os":"linux","cpu_percent":1}}`)
	var request RegisterRequest
	if err := DecodeJSON(bytes.NewReader(body), 1024, &request); err == nil {
		t.Fatal("DecodeJSON accepted a resource field in registration system")
	}
}

func TestRegisterRequestRejectsNonUUIDNodeID(t *testing.T) {
	request := RegisterRequest{RegistrationToken: "token", NodeUUID: "node-1", Name: "node"}
	if err := request.Validate(); err == nil {
		t.Fatal("Validate() accepted a non-UUID node ID")
	}
}

func TestRegisterRequestRequiresNonNullBoundedSystem(t *testing.T) {
	base := RegisterRequest{
		RegistrationToken: "token",
		NodeUUID:          "550e8400-e29b-41d4-a716-446655440000",
		Name:              "node",
	}
	if err := base.Validate(); err == nil {
		t.Fatal("Validate accepted an omitted system")
	}
	if err := (RegisterRequest{RegistrationToken: base.RegistrationToken, NodeUUID: base.NodeUUID, Name: base.Name, System: &RegistrationSystem{OS: strings.Repeat("x", maxVersionLength+1)}}).Validate(); err == nil {
		t.Fatal("Validate accepted an oversized system field")
	}
	var request RegisterRequest
	if err := DecodeJSON(bytes.NewReader([]byte(`{"registration_token":"token","node_uuid":"550e8400-e29b-41d4-a716-446655440000","name":"node","system":null}`)), 1024, &request); err != nil {
		t.Fatal(err)
	}
	if err := request.Validate(); err == nil {
		t.Fatal("Validate accepted a null system")
	}
}

func TestDecodeJSONRejectsUnknownTrailingAndOversizedJSON(t *testing.T) {
	valid := []byte(`{"node_uuid":"550e8400-e29b-41d4-a716-446655440000","reported_at":1,"resource":{"hostname":"agent-1"},"results":[]}`)

	var report ReportRequest
	if err := DecodeJSON(bytes.NewReader(valid), 256, &report); err != nil {
		t.Fatalf("DecodeJSON(valid) error = %v", err)
	}

	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "unknown field", body: []byte(`{"node_uuid":"550e8400-e29b-41d4-a716-446655440000","unexpected":true}`)},
		{name: "trailing json", body: append(append([]byte{}, valid...), []byte(`{"extra":true}`)...)},
		{name: "oversized", body: append(valid, bytes.Repeat([]byte("x"), 300)...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := DecodeJSON(bytes.NewReader(test.body), 256, &report); err == nil {
				t.Fatal("DecodeJSON accepted invalid input")
			}
		})
	}
}

func TestProtocolWireShapeUsesRequiredJSONNames(t *testing.T) {
	payload, err := json.Marshal(CheckTask{
		ID: "check-1", Kind: "media_http", Host: "example.com", Port: 443,
		Path: "/health", ExpectedStatus: 200, DNSType: "A", TimeoutMS: 1000,
		MaxHops: 10, IntervalSeconds: 60, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, field := range []string{"id", "kind", "host", "port", "path", "expected_status", "dns_type", "timeout_ms", "max_hops", "interval_seconds", "enabled"} {
		if !strings.Contains(text, `"`+field+`"`) {
			t.Fatalf("marshaled task does not contain %q: %s", field, text)
		}
	}
	for _, forbidden := range []string{"command", "shell", "script", "executable", "arbitrary_args", "headers", "cookies", "file"} {
		if strings.Contains(text, `"`+forbidden+`"`) {
			t.Fatalf("marshaled task contains forbidden field %q: %s", forbidden, text)
		}
	}
}

func validCheckTask() CheckTask {
	return CheckTask{
		ID: "check-1", Kind: "tcp", Host: "example.com", Port: 443,
		Path: "/", ExpectedStatus: 200, DNSType: "A", TimeoutMS: 1000,
		MaxHops: 10, IntervalSeconds: 60, Enabled: true,
	}
}
