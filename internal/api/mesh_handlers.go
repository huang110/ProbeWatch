package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

// MeshNodeInfo represents one node in the mesh matrix.
type MeshNodeInfo struct {
	ID             string     `json:"id"`
	UUID           string     `json:"uuid"`
	Name           string     `json:"name"`
	Tags           []string   `json:"tags"`
	Status         string     `json:"status"`
	IP             string     `json:"ip,omitempty"`
	Hostname       string     `json:"hostname,omitempty"`
	LastReportedAt *time.Time `json:"last_reported_at,omitempty"`
}

// MeshCell represents the pairwise latency and reachability from FromNode to ToNode.
type MeshCell struct {
	FromNodeID   string  `json:"from_node_id"`
	ToNodeID     string  `json:"to_node_id"`
	LatencyMS    float64 `json:"latency_ms"`
	Status       string  `json:"status"` // self, excellent, good, fair, high, loss, unmeasured
	LossRate     float64 `json:"loss_rate"`
	Direct       bool    `json:"direct"`
	CheckedAt    int64   `json:"checked_at,omitempty"`
	TargetID     string  `json:"target_id,omitempty"`
}

// RelayRoute recommends an alternate 2-hop route when direct routing is suboptimal.
type RelayRoute struct {
	FromNodeID     string  `json:"from_node_id"`
	FromNodeName   string  `json:"from_node_name"`
	ToNodeID       string  `json:"to_node_id"`
	ToNodeName     string  `json:"to_node_name"`
	DirectMS       float64 `json:"direct_ms"`
	RelayNodeID    string  `json:"relay_node_id"`
	RelayNodeName  string  `json:"relay_node_name"`
	RelayMS        float64 `json:"relay_ms"`
	SavingsMS      float64 `json:"savings_ms"`
	SavingsPercent float64 `json:"savings_percent"`
}

// MeshStats summarizes the mesh network latency characteristics.
type MeshStats struct {
	TotalNodes              int     `json:"total_nodes"`
	OnlineNodes             int     `json:"online_nodes"`
	MeasuredPairs           int     `json:"measured_pairs"`
	AvgLatencyMS            float64 `json:"avg_latency_ms"`
	MinLatencyMS            float64 `json:"min_latency_ms"`
	MaxLatencyMS            float64 `json:"max_latency_ms"`
	RelayOptimizationsCount int     `json:"relay_optimizations_count"`
}

// MeshMatrixResponse is the complete response for the full mesh latency matrix.
type MeshMatrixResponse struct {
	Nodes       []MeshNodeInfo `json:"nodes"`
	Matrix      [][]MeshCell   `json:"matrix"`
	Relays      []RelayRoute   `json:"relays"`
	Stats       MeshStats      `json:"stats"`
	GeneratedAt int64          `json:"generated_at"`
}

// meshMatrixHandler builds the N x N inter-node latency matrix with relay optimization recommendations.
func (s *Server) meshMatrixHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()
	nodes, err := s.service.Store().ListNodes(ctx)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "nodes unavailable")
		return
	}

	user, hasUser := UserFromContext(ctx)
	meshNodes := make([]MeshNodeInfo, 0, len(nodes))
	nodeMap := make(map[string]MeshNodeInfo)

	for _, n := range nodes {
		if hasUser && !user.CanAccessNode(n.UUID) {
			continue
		}
		info := MeshNodeInfo{
			ID:     n.ID,
			UUID:   n.UUID,
			Name:   n.Name,
			Tags:   parseNodeTags(n.Tags),
			Status: "offline",
		}
		reportedAt, payload, err := s.service.Store().GetResourceLatest(ctx, n.ID)
		if err == nil {
			info.LastReportedAt = &reportedAt
			if time.Since(reportedAt) <= 2*time.Minute {
				info.Status = "online"
			} else if time.Since(reportedAt) <= 10*time.Minute {
				info.Status = "attention"
			}
			var snapshot protocol.ResourceSnapshot
			if json.Unmarshal(payload, &snapshot) == nil {
				if snapshot.IPv4 != "" {
					info.IP = snapshot.IPv4
				} else if snapshot.IPv6 != "" {
					info.IP = snapshot.IPv6
				}
				info.Hostname = snapshot.Hostname
			}
		}
		meshNodes = append(meshNodes, info)
		nodeMap[n.ID] = info
	}

	// Sort nodes deterministically by Name
	sort.Slice(meshNodes, func(i, j int) bool {
		return meshNodes[i].Name < meshNodes[j].Name
	})

	// Load all targets
	targetMap := make(map[string]db.TargetRecord)
	for _, kind := range targetKinds() {
		targets, err := s.service.Store().ListTargets(ctx, kind)
		if err == nil {
			for _, t := range targets {
				targetMap[t.ID] = t
			}
		}
	}

	// Load all latest network results
	allResults, _ := s.service.Store().ListAllNetworkLatest(ctx)
	nodeTargetResults := make(map[string]map[string]protocol.NetworkResult)
	for _, r := range allResults {
		if nodeTargetResults[r.NodeID] == nil {
			nodeTargetResults[r.NodeID] = make(map[string]protocol.NetworkResult)
		}
		var nr protocol.NetworkResult
		if err := json.Unmarshal(r.Payload, &nr); err == nil {
			nodeTargetResults[r.NodeID][r.TargetID] = nr
		}
	}

	n := len(meshNodes)
	matrix := make([][]MeshCell, n)
	for i := range matrix {
		matrix[i] = make([]MeshCell, n)
	}

	var sumLatency float64
	var minLatency, maxLatency float64
	measuredCount := 0
	onlineCount := 0

	for _, node := range meshNodes {
		if node.Status == "online" {
			onlineCount++
		}
	}

	// Build N x N cells
	for i := 0; i < n; i++ {
		src := meshNodes[i]
		for j := 0; j < n; j++ {
			dst := meshNodes[j]
			cell := MeshCell{
				FromNodeID: src.ID,
				ToNodeID:   dst.ID,
				LatencyMS:  -1,
				Status:     "unmeasured",
				LossRate:   0,
				Direct:     false,
			}

			if i == j {
				cell.LatencyMS = 0
				cell.Status = "self"
				cell.Direct = true
				matrix[i][j] = cell
				continue
			}

			// Find target that corresponds to dst node
			foundResult, foundCheckedAt, foundTargetID, ok := findProbeResult(src.ID, dst, targetMap, nodeTargetResults)
			if !ok {
				// Try reverse probe (dst -> src) as symmetric fallback
				revResult, revCheckedAt, revTargetID, revOk := findProbeResult(dst.ID, src, targetMap, nodeTargetResults)
				if revOk && (revResult.LatencyMS > 0 || revResult.Status == "up") {
					foundResult = revResult
					foundCheckedAt = revCheckedAt
					foundTargetID = revTargetID
					ok = true
				}
			}

			if ok {
				cell.Direct = true
				cell.TargetID = foundTargetID
				cell.CheckedAt = foundCheckedAt
				if foundResult.Error != "" || foundResult.Status == "down" {
					cell.Status = "loss"
					cell.LossRate = 100.0
					cell.LatencyMS = -1
				} else if foundResult.LatencyMS > 0 {
					cell.LatencyMS = float64(foundResult.LatencyMS)
					cell.Status = latencyStatus(foundResult.LatencyMS)
					cell.LossRate = 0
				} else {
					cell.LatencyMS = 1
					cell.Status = "excellent"
				}
			} else {
				// Estimate latency if both probed common target(s)
				if est, estOk := estimateCommonTargetLatency(src.ID, dst.ID, nodeTargetResults); estOk {
					cell.LatencyMS = est
					cell.Status = latencyStatus(int64(est))
					cell.Direct = false
				}
			}

			if cell.LatencyMS > 0 {
				measuredCount++
				sumLatency += cell.LatencyMS
				if minLatency == 0 || cell.LatencyMS < minLatency {
					minLatency = cell.LatencyMS
				}
				if cell.LatencyMS > maxLatency {
					maxLatency = cell.LatencyMS
				}
			}

			matrix[i][j] = cell
		}
	}

	// Compute intelligent 2-hop relay routing optimizations
	relays := make([]RelayRoute, 0)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			directMS := matrix[i][j].LatencyMS
			if directMS <= 0 {
				continue
			}

			bestRelayIdx := -1
			bestRelayMS := directMS

			for k := 0; k < n; k++ {
				if k == i || k == j {
					continue
				}
				leg1 := matrix[i][k].LatencyMS
				leg2 := matrix[k][j].LatencyMS
				if leg1 > 0 && leg2 > 0 {
					relayMS := leg1 + leg2
					if relayMS < bestRelayMS {
						bestRelayMS = relayMS
						bestRelayIdx = k
					}
				}
			}

			if bestRelayIdx >= 0 {
				savings := directMS - bestRelayMS
				savingsPct := (savings / directMS) * 100.0
				// Recommend relay if savings >= 10ms and >= 15% reduction
				if savings >= 10.0 && savingsPct >= 15.0 {
					relayNode := meshNodes[bestRelayIdx]
					relays = append(relays, RelayRoute{
						FromNodeID:     meshNodes[i].ID,
						FromNodeName:   meshNodes[i].Name,
						ToNodeID:       meshNodes[j].ID,
						ToNodeName:     meshNodes[j].Name,
						DirectMS:       roundTo1Dec(directMS),
						RelayNodeID:    relayNode.ID,
						RelayNodeName:  relayNode.Name,
						RelayMS:        roundTo1Dec(bestRelayMS),
						SavingsMS:      roundTo1Dec(savings),
						SavingsPercent: roundTo1Dec(savingsPct),
					})
				}
			}
		}
	}

	// Sort relays by savings in descending order
	sort.Slice(relays, func(i, j int) bool {
		return relays[i].SavingsMS > relays[j].SavingsMS
	})

	var avgLatency float64
	if measuredCount > 0 {
		avgLatency = roundTo1Dec(sumLatency / float64(measuredCount))
	}

	stats := MeshStats{
		TotalNodes:              n,
		OnlineNodes:             onlineCount,
		MeasuredPairs:           measuredCount,
		AvgLatencyMS:            avgLatency,
		MinLatencyMS:            roundTo1Dec(minLatency),
		MaxLatencyMS:            roundTo1Dec(maxLatency),
		RelayOptimizationsCount: len(relays),
	}

	writeJSON(w, http.StatusOK, MeshMatrixResponse{
		Nodes:       meshNodes,
		Matrix:      matrix,
		Relays:      relays,
		Stats:       stats,
		GeneratedAt: time.Now().UTC().Unix(),
	})
}

// findProbeResult checks if srcNode has probed a target representing dstNode.
func findProbeResult(srcNodeID string, dstNode MeshNodeInfo, targets map[string]db.TargetRecord, results map[string]map[string]protocol.NetworkResult) (protocol.NetworkResult, int64, string, bool) {
	nodeProbes := results[srcNodeID]
	if len(nodeProbes) == 0 {
		return protocol.NetworkResult{}, 0, "", false
	}

	for targetID, res := range nodeProbes {
		t, exists := targets[targetID]
		if !exists {
			continue
		}
		// Match by IP or hostname
		if dstNode.IP != "" && strings.EqualFold(t.Host, dstNode.IP) {
			return res, res.CheckedAt, targetID, true
		}
		if dstNode.Hostname != "" && strings.EqualFold(t.Host, dstNode.Hostname) {
			return res, res.CheckedAt, targetID, true
		}
		// Match by target identity
		if targetID == dstNode.ID || targetID == dstNode.UUID || targetID == "node-"+dstNode.ID || targetID == "node-"+dstNode.UUID {
			return res, res.CheckedAt, targetID, true
		}
		// Match by target name containing destination node name
		if strings.EqualFold(t.Name, dstNode.Name) || (len(dstNode.Name) >= 3 && strings.Contains(strings.ToLower(t.Name), strings.ToLower(dstNode.Name))) {
			return res, res.CheckedAt, targetID, true
		}
	}
	return protocol.NetworkResult{}, 0, "", false
}

// estimateCommonTargetLatency estimates inter-node latency using common targets probed by both nodes.
func estimateCommonTargetLatency(node1ID, node2ID string, results map[string]map[string]protocol.NetworkResult) (float64, bool) {
	probes1 := results[node1ID]
	probes2 := results[node2ID]
	if len(probes1) == 0 || len(probes2) == 0 {
		return 0, false
	}

	var totalEst float64
	count := 0
	for tID, r1 := range probes1 {
		if r2, ok := probes2[tID]; ok {
			if r1.LatencyMS > 0 && r2.LatencyMS > 0 {
				diff := math.Abs(float64(r1.LatencyMS - r2.LatencyMS))
				// Triangle lower-bound heuristic + base transit
				est := diff + math.Min(float64(r1.LatencyMS), float64(r2.LatencyMS))*0.3
				if est < 5 {
					est = 5
				}
				totalEst += est
				count++
			}
		}
	}
	if count > 0 {
		return roundTo1Dec(totalEst / float64(count)), true
	}
	return 0, false
}

func latencyStatus(ms int64) string {
	switch {
	case ms < 50:
		return "excellent"
	case ms < 100:
		return "good"
	case ms < 200:
		return "fair"
	default:
		return "high"
	}
}

func roundTo1Dec(val float64) float64 {
	return math.Round(val*10) / 10
}

type patchNodeRequest struct {
	Name *string         `json:"name"`
	Tags json.RawMessage `json:"tags"`
}

// patchNode handles PATCH /api/nodes/{id} for updating node name and tags.
func (s *Server) patchNode(w http.ResponseWriter, r *http.Request, idOrUUID string) {
	if r.Method != http.MethodPatch {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()
	var node db.Node
	var err error
	if security.IsRFC4122UUID(idOrUUID) {
		node, err = s.service.Store().GetNodeByUUID(ctx, idOrUUID)
	} else {
		node, err = s.service.Store().GetNodeByID(ctx, idOrUUID)
	}
	if errors.Is(err, db.ErrNodeNotFound) || errors.Is(err, db.ErrNodeDeleted) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node unavailable")
		return
	}

	user, hasUser := UserFromContext(ctx)
	if hasUser {
		if !user.CanAccessNode(node.UUID) {
			writeJSONError(w, http.StatusForbidden, "access denied")
			return
		}
		if user.Role != "admin" && user.Role != "operator" && user.Role != "" {
			writeJSONError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	var req patchNodeRequest
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	newName := node.Name
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || len([]byte(trimmed)) > 128 {
			writeJSONError(w, http.StatusBadRequest, "invalid node name")
			return
		}
		newName = trimmed
	}

	newTags := node.Tags
	if len(req.Tags) > 0 && !bytes.Equal(bytes.TrimSpace(req.Tags), []byte("null")) {
		var tagList []string
		if err := json.Unmarshal(req.Tags, &tagList); err == nil {
			cleaned := make([]string, 0, len(tagList))
			for _, t := range tagList {
				t = strings.TrimSpace(t)
				if t != "" {
					cleaned = append(cleaned, t)
				}
			}
			newTags = strings.Join(cleaned, ",")
		} else {
			var tagStr string
			if err := json.Unmarshal(req.Tags, &tagStr); err == nil {
				tags := parseNodeTags(tagStr)
				newTags = strings.Join(tags, ",")
			} else {
				writeJSONError(w, http.StatusBadRequest, "invalid tags format")
				return
			}
		}
	}

	if err := s.service.Store().UpdateNode(ctx, node.ID, newName, newTags); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to update node")
		return
	}

	node.Name = newName
	node.Tags = newTags
	writeJSON(w, http.StatusOK, s.nodeSummary(ctx, node))
}
