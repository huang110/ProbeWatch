package ai

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func TestMCPServerAndTools(t *testing.T) {
	root := t.TempDir()
	store, err := db.OpenStore(filepath.Join(root, "probewatch.db"), []byte("test-pepper-123456789012345678901234"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// Register a dummy node
	regToken, err := store.CreateRegistrationToken(ctx, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	regNode, err := store.RegisterNode(ctx, regToken.Token, db.NodeInput{
		UUID: "550e8400-e29b-41d4-a716-446655440001",
		Name: "hk-node-01",
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	// Save dummy resource snapshot
	snap := protocol.ResourceSnapshot{
		Hostname:             "hk-node-01",
		OS:                   "linux",
		Kernel:               "6.6.0",
		Arch:                 "amd64",
		AgentVersion:         "0.5.6",
		CPUPercent:           1.2,
		MemoryTotalBytes:     8 * 1024 * 1024 * 1024,
		MemoryUsedBytes:      2 * 1024 * 1024 * 1024,
		FilesystemTotalBytes: 50 * 1024 * 1024 * 1024,
		FilesystemUsedBytes:  10 * 1024 * 1024 * 1024,
	}
	snapBytes, _ := json.Marshal(snap)
	if err := store.UpsertResourceLatest(ctx, regNode.Node.ID, now, snapBytes); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Environment: "test",
	}
	service := NewAIService(store, cfg)

	// 1. Test MCP Initialize
	initReq := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	initResp, err := service.HandleMCPJSONRPC(ctx, initReq)
	if err != nil || initResp.Error != nil {
		t.Fatalf("mcp initialize failed: %v, resp: %+v", err, initResp)
	}

	// 2. Test MCP Tools List
	listReq := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	listResp, err := service.HandleMCPJSONRPC(ctx, listReq)
	if err != nil || listResp.Error != nil {
		t.Fatalf("mcp tools/list failed: %v", err)
	}
	listMap, ok := listResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map in tools/list")
	}
	tools, ok := listMap["tools"].([]MCPToolDefinition)
	if !ok || len(tools) != 5 {
		t.Fatalf("expected 5 MCP tools, got: %+v", listMap["tools"])
	}

	// 3. Test MCP Tool Call: probewatch_get_overview
	callOverviewReq := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"probewatch_get_overview","arguments":{}}}`)
	callResp, err := service.HandleMCPJSONRPC(ctx, callOverviewReq)
	if err != nil || callResp.Error != nil {
		t.Fatalf("mcp call probewatch_get_overview failed: %v", err)
	}
	overviewResult, ok := callResp.Result.(*MCPCallResult)
	if !ok || len(overviewResult.Content) == 0 {
		t.Fatalf("expected non-empty overview content")
	}

	// 4. Test MCP Tool Call: probewatch_get_node_metrics
	callNodeReq := []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"probewatch_get_node_metrics","arguments":{"name":"hk-node-01"}}}`)
	callNodeResp, err := service.HandleMCPJSONRPC(ctx, callNodeReq)
	if err != nil || callNodeResp.Error != nil {
		t.Fatalf("mcp call probewatch_get_node_metrics failed: %v", err)
	}

	// 5. Test MCP Token Generation and Validation
	token, err := service.GetOrGenerateMCPToken(ctx)
	if err != nil || token == "" {
		t.Fatalf("failed to generate token: %v", err)
	}
	if !service.ValidateMCPToken(ctx, token) {
		t.Fatal("expected token to validate successfully")
	}
	if service.ValidateMCPToken(ctx, "invalid-token-xyz") {
		t.Fatal("expected invalid token to be rejected")
	}
}
