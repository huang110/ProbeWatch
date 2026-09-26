package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckTaskValidateAcceptsEveryAllowedKind(t *testing.T) {
	for _, kind := range []string{"tcp", "http", "https", "dns", "mtr", "media_http", "speedtest"} {
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

func TestCheckTaskValidateAcceptsBoundedRegionRules(t *testing.T) {
	task := validCheckTask()
	task.Kind = "media_http"
	task.Path = "/manifest"
	task.RegionRules = []RegionRule{
		{Region: "SG", Contains: "geo-SG"},
		{Region: "US-West_2", Contains: "edge=us-west"},
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	atLimit := validCheckTask()
	atLimit.Kind = "media_http"
	atLimit.Path = "/manifest"
	for index := 0; index < maxRegionRules; index++ {
		atLimit.RegionRules = append(atLimit.RegionRules, RegionRule{Region: "R" + strings.Repeat("x", 15), Contains: "marker"})
	}
	if len(atLimit.RegionRules) != maxRegionRules {
		t.Fatalf("rule count = %d, want %d", len(atLimit.RegionRules), maxRegionRules)
	}
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("Validate() rejected %d rules: %v", maxRegionRules, err)
	}

	maxRegion := validCheckTask()
	maxRegion.Kind = "media_http"
	maxRegion.Path = "/manifest"
	maxRegion.RegionRules = []RegionRule{{Region: strings.Repeat("a", maxRegionLength), Contains: strings.Repeat("b", maxRegionContainsLength)}}
	if err := maxRegion.Validate(); err != nil {
		t.Fatalf("Validate() rejected maximum-size rule: %v", err)
	}
}

func TestCheckTaskValidateRejectsInvalidRegionRules(t *testing.T) {
	base := func() CheckTask {
		task := validCheckTask()
		task.Kind = "media_http"
		task.Path = "/manifest"
		return task
	}
	for _, test := range []struct {
		name  string
		rules []RegionRule
	}{
		{name: "region too long", rules: []RegionRule{{Region: strings.Repeat("a", maxRegionLength+1), Contains: "geo"}}},
		{name: "region empty", rules: []RegionRule{{Region: "", Contains: "geo"}}},
		{name: "region blank", rules: []RegionRule{{Region: "   ", Contains: "geo"}}},
		{name: "region illegal characters", rules: []RegionRule{{Region: "SG US", Contains: "geo"}}},
		{name: "region punctuation", rules: []RegionRule{{Region: "SG!", Contains: "geo"}}},
		{name: "contains too long", rules: []RegionRule{{Region: "SG", Contains: strings.Repeat("a", maxRegionContainsLength+1)}}},
		{name: "contains empty", rules: []RegionRule{{Region: "SG", Contains: ""}}},
		{name: "contains control character", rules: []RegionRule{{Region: "SG", Contains: "geo\n-SG"}}},
		{name: "contains NUL", rules: []RegionRule{{Region: "SG", Contains: "geo\x00"}}},
		{name: "too many rules", rules: func() []RegionRule {
			rules := make([]RegionRule, maxRegionRules+1)
			for index := range rules {
				rules[index] = RegionRule{Region: "SG", Contains: "geo"}
			}
			return rules
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := base()
			task.RegionRules = test.rules
			if err := task.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid region rules")
			}
		})
	}

	nonMedia := validCheckTask()
	nonMedia.Kind = "tcp"
	nonMedia.RegionRules = []RegionRule{{Region: "SG", Contains: "geo"}}
	if err := nonMedia.Validate(); err == nil {
		t.Fatal("Validate() accepted region rules on a non-media task")
	}
}

func TestCheckTaskWithoutRegionRulesStaysCompatible(t *testing.T) {
	task := validCheckTask()
	task.Kind = "media_http"
	task.Path = "/manifest"
	if err := task.Validate(); err != nil {
		t.Fatalf("legacy task without rules rejected: %v", err)
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "region_rules") {
		t.Fatalf("legacy task marshaled a region_rules field: %s", encoded)
	}
}

func TestAgentConfigValidateAcceptsOptionalVersionAndMaxAge(t *testing.T) {
	legacy := AgentConfigResponse{}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy config without version fields rejected: %v", err)
	}
	current := AgentConfigResponse{ConfigVersion: 1700000000, ConfigMaxAgeSeconds: 1800}
	if err := current.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	atLimit := AgentConfigResponse{ConfigVersion: 1, ConfigMaxAgeSeconds: maxConfigMaxAgeSeconds}
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("Validate() rejected maximum max age: %v", err)
	}
	versionOnly := AgentConfigResponse{ConfigVersion: 1700000000}
	if err := versionOnly.Validate(); err != nil {
		t.Fatalf("Validate() rejected version without max age: %v", err)
	}
	maxAgeOnly := AgentConfigResponse{ConfigMaxAgeSeconds: 1}
	if err := maxAgeOnly.Validate(); err != nil {
		t.Fatalf("Validate() rejected minimum max age: %v", err)
	}
}

func TestAgentConfigValidateRejectsInvalidVersionAndMaxAge(t *testing.T) {
	tests := []struct {
		name string
		edit func(*AgentConfigResponse)
	}{
		{name: "negative version", edit: func(response *AgentConfigResponse) { response.ConfigVersion = -1 }},
		{name: "negative max age", edit: func(response *AgentConfigResponse) { response.ConfigMaxAgeSeconds = -1 }},
		{name: "max age above limit", edit: func(response *AgentConfigResponse) { response.ConfigMaxAgeSeconds = maxConfigMaxAgeSeconds + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := AgentConfigResponse{ConfigVersion: 1700000000, ConfigMaxAgeSeconds: 1800}
			test.edit(&response)
			if err := response.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid config version fields")
			}
		})
	}
}

func TestAgentConfigDecodesVersionFieldsStrictly(t *testing.T) {
	valid := []byte(`{"tasks":[],"config_version":1700000000,"config_max_age_seconds":1800}`)
	var response AgentConfigResponse
	if err := DecodeJSON(bytes.NewReader(valid), 1024, &response); err != nil {
		t.Fatalf("DecodeJSON(valid) error = %v", err)
	}
	if response.ConfigVersion != 1700000000 || response.ConfigMaxAgeSeconds != 1800 {
		t.Fatalf("decoded version fields = %d/%d", response.ConfigVersion, response.ConfigMaxAgeSeconds)
	}
	if err := response.Validate(); err != nil {
		t.Fatalf("decoded config rejected: %v", err)
	}

	unknown := []byte(`{"tasks":[],"config_version":1700000000,"config_max_age_seconds":1800,"command":"rm"}`)
	var rejected AgentConfigResponse
	if err := DecodeJSON(bytes.NewReader(unknown), 1024, &rejected); err == nil {
		t.Fatal("DecodeJSON accepted an unknown field next to the version fields")
	}
}

func TestLegacyAgentConfigOmitsVersionFields(t *testing.T) {
	encoded, err := json.Marshal(AgentConfigResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "config_version") || strings.Contains(string(encoded), "config_max_age_seconds") {
		t.Fatalf("legacy config marshaled version fields: %s", encoded)
	}
}

func TestRegionRulesDecodeStrictly(t *testing.T) {
	valid := []byte(`{"id":"media-1","kind":"media_http","host":"media.example.com","port":443,"path":"/manifest","timeout_ms":1000,"max_hops":20,"interval_seconds":10,"enabled":true,"region_rules":[{"region":"SG","contains":"geo-SG"}]}`)
	var task CheckTask
	if err := DecodeJSON(bytes.NewReader(valid), 1024, &task); err != nil {
		t.Fatalf("DecodeJSON(valid) error = %v", err)
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("decoded task rejected: %v", err)
	}
	if len(task.RegionRules) != 1 || task.RegionRules[0].Region != "SG" || task.RegionRules[0].Contains != "geo-SG" {
		t.Fatalf("decoded rules = %#v", task.RegionRules)
	}

	unknown := []byte(`{"id":"media-1","kind":"media_http","host":"media.example.com","port":443,"path":"/manifest","timeout_ms":1000,"max_hops":20,"interval_seconds":10,"enabled":true,"region_rules":[{"region":"SG","contains":"geo-SG","headers":{"X":"y"}}]}`)
	var rejected CheckTask
	if err := DecodeJSON(bytes.NewReader(unknown), 1024, &rejected); err == nil {
		t.Fatal("DecodeJSON accepted an unknown field inside a region rule")
	}
}

func TestSpeedtestResultValidation(t *testing.T) {
	valid := SpeedtestResult{
		ServerName:        "Cloudflare Edge",
		ServerURL:         "https://speed.cloudflare.com/__down",
		DownloadSpeedMbps: 125.5,
		UploadSpeedMbps:   48.2,
		LatencyMS:         18,
		JitterMS:          2,
		BytesReceived:     10485760,
		BytesSent:         5242880,
		DurationMS:        2500,
		Status:            "ok",
		TestedAt:          1700000000,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid speedtest result rejected: %v", err)
	}

	invalidNegative := valid
	invalidNegative.DownloadSpeedMbps = -1
	if err := invalidNegative.Validate(); err == nil {
		t.Fatal("expected error on negative download speed")
	}

	invalidTestedAt := valid
	invalidTestedAt.TestedAt = 0
	if err := invalidTestedAt.Validate(); err == nil {
		t.Fatal("expected error on zero tested_at")
	}

	envelope := SpeedtestResultEnvelope{
		TaskID: "speed-1",
		Result: valid,
	}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}

	checkRes := CheckResult{
		ID:        "speed-1",
		Kind:      "speedtest",
		Speedtest: &valid,
	}
	if err := checkRes.Validate(); err != nil {
		t.Fatalf("valid check result with speedtest rejected: %v", err)
	}

	missingRes := CheckResult{
		ID:   "speed-1",
		Kind: "speedtest",
	}
	if err := missingRes.Validate(); err == nil {
		t.Fatal("expected error when speedtest result is nil")
	}
}
