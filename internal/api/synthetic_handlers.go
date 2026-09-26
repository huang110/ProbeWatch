package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/monitor"
	"github.com/probewatch/probewatch/internal/protocol"
)

// CertificateItem represents a monitored SSL/TLS certificate's details.
type CertificateItem struct {
	TargetID     string   `json:"target_id"`
	TargetName   string   `json:"target_name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Subject      string   `json:"subject"`
	Issuer       string   `json:"issuer"`
	DNSNames     []string `json:"dns_names"`
	NotBefore    int64    `json:"not_before"`
	NotAfter     int64    `json:"not_after"`
	DaysLeft     int      `json:"days_left"`
	Protocol     string   `json:"protocol"`
	CipherSuite  string   `json:"cipher_suite"`
	IsExpired    bool     `json:"is_expired"`
	ExpiringSoon bool     `json:"expiring_soon"`
	NodeID       string   `json:"node_id"`
	NodeName     string   `json:"node_name"`
	CheckedAt    int64    `json:"checked_at"`
}

// CertificatesResponse wraps the certificate inventory and health summary.
type CertificatesResponse struct {
	Total        int               `json:"total"`
	ValidCount   int               `json:"valid_count"`
	ExpiringSoon int               `json:"expiring_soon"`
	ExpiredCount int               `json:"expired_count"`
	Certificates []CertificateItem `json:"certificates"`
}

// DNSNodeRecord represents one node's resolved DNS record set.
type DNSNodeRecord struct {
	NodeID      string   `json:"node_id"`
	NodeName    string   `json:"node_name"`
	Records     []string `json:"records"`
	Nameserver  string   `json:"nameserver,omitempty"`
	QueryTimeMS int64    `json:"query_time_ms"`
	CheckedAt   int64    `json:"checked_at"`
	Status      string   `json:"status"`
}

// DNSTargetMatrix represents the cross-node DNS resolution consistency for one target.
type DNSTargetMatrix struct {
	TargetID        string          `json:"target_id"`
	TargetName      string          `json:"target_name"`
	Host            string          `json:"host"`
	DNSType         string          `json:"dns_type"`
	IsConsistent    bool            `json:"is_consistent"`
	UniqueSetsCount int             `json:"unique_sets_count"`
	DivergentNodes  []string        `json:"divergent_nodes"`
	AvgQueryTimeMS  int64           `json:"avg_query_time_ms"`
	Nodes           []DNSNodeRecord `json:"nodes"`
}

// DNSMatrixResponse wraps the DNS resolution consistency matrix across nodes.
type DNSMatrixResponse struct {
	TotalTargets    int               `json:"total_targets"`
	ConsistentCount int               `json:"consistent_count"`
	DivergentCount  int               `json:"divergent_count"`
	Matrix          []DNSTargetMatrix `json:"matrix"`
}

func (s *Server) certificatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	// Load target names and hosts
	targetMap := make(map[string]db.TargetRecord)
	for _, kind := range []db.TargetKind{db.TargetKindTCP, db.TargetKindHTTP, db.TargetKindHTTPS} {
		targets, err := s.service.Store().ListTargets(ctx, kind)
		if err == nil {
			for _, t := range targets {
				targetMap[t.ID] = t
			}
		}
	}

	// Load node names
	nodeMap := make(map[string]string)
	if nodes, err := s.service.Store().ListNodes(ctx); err == nil {
		for _, node := range nodes {
			nodeMap[node.ID] = node.Name
		}
	}

	// Load all latest network results
	allLatest, err := s.service.Store().ListAllNetworkLatest(ctx)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "certificates unavailable")
		return
	}

	// Group certificates by TargetID, keeping the worst/lowest DaysLeft (or freshest)
	certByTarget := make(map[string]CertificateItem)
	for _, item := range allLatest {
		var res protocol.NetworkResult
		if err := json.Unmarshal(item.Payload, &res); err != nil || res.TLSCert == nil {
			continue
		}
		target, ok := targetMap[item.TargetID]
		targetName := item.TargetID
		host := res.Host
		port := res.Port
		if ok {
			if target.Name != "" {
				targetName = target.Name
			}
			if target.Host != "" {
				host = target.Host
			}
		}

		nodeName := nodeMap[item.NodeID]
		if nodeName == "" {
			nodeName = item.NodeID
		}

		certItem := CertificateItem{
			TargetID:     item.TargetID,
			TargetName:   targetName,
			Host:         host,
			Port:         port,
			Subject:      res.TLSCert.Subject,
			Issuer:       res.TLSCert.Issuer,
			DNSNames:     res.TLSCert.DNSNames,
			NotBefore:    res.TLSCert.NotBefore,
			NotAfter:     res.TLSCert.NotAfter,
			DaysLeft:     res.TLSCert.DaysLeft,
			Protocol:     res.TLSCert.Protocol,
			CipherSuite:  res.TLSCert.CipherSuite,
			IsExpired:    res.TLSCert.IsExpired,
			ExpiringSoon: res.TLSCert.ExpiringSoon,
			NodeID:       item.NodeID,
			NodeName:     nodeName,
			CheckedAt:    res.CheckedAt,
		}

		existing, exists := certByTarget[item.TargetID]
		if !exists || certItem.DaysLeft < existing.DaysLeft {
			certByTarget[item.TargetID] = certItem
		}
	}

	items := make([]CertificateItem, 0, len(certByTarget))
	validCount := 0
	expiringSoonCount := 0
	expiredCount := 0

	for _, cert := range certByTarget {
		if cert.IsExpired || cert.DaysLeft <= 0 {
			expiredCount++
		} else if cert.ExpiringSoon || cert.DaysLeft <= 14 {
			expiringSoonCount++
		} else {
			validCount++
		}
		items = append(items, cert)
	}

	// Sort certificates by DaysLeft ascending (most urgent first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].DaysLeft < items[j].DaysLeft
	})

	writeJSON(w, http.StatusOK, CertificatesResponse{
		Total:        len(items),
		ValidCount:   validCount,
		ExpiringSoon: expiringSoonCount,
		ExpiredCount: expiredCount,
		Certificates: items,
	})
}

func (s *Server) dnsMatrixHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	// Load DNS targets
	targetMap := make(map[string]db.TargetRecord)
	targets, err := s.service.Store().ListTargets(ctx, db.TargetKindDNS)
	if err == nil {
		for _, t := range targets {
			targetMap[t.ID] = t
		}
	}

	// Load nodes
	nodeMap := make(map[string]string)
	if nodes, err := s.service.Store().ListNodes(ctx); err == nil {
		for _, node := range nodes {
			nodeMap[node.ID] = node.Name
		}
	}

	// Load all latest network results
	allLatest, err := s.service.Store().ListAllNetworkLatest(ctx)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "dns matrix unavailable")
		return
	}

	// Group DNS results by TargetID
	type targetData struct {
		target db.TargetRecord
		nodes  []DNSNodeRecord
	}
	matrixData := make(map[string]*targetData)

	for _, item := range allLatest {
		var res protocol.NetworkResult
		if err := json.Unmarshal(item.Payload, &res); err != nil || res.DNS == nil {
			continue
		}
		target, ok := targetMap[item.TargetID]
		if !ok {
			// Also support non-explicit DNS targets if they recorded DNS results
			target = db.TargetRecord{
				TargetDefinition: db.TargetDefinition{
					ID:   item.TargetID,
					Name: item.TargetID,
					Host: res.Host,
					Kind: db.TargetKindDNS,
				},
			}
		}

		td, exists := matrixData[item.TargetID]
		if !exists {
			td = &targetData{target: target}
			matrixData[item.TargetID] = td
		}

		nodeName := nodeMap[item.NodeID]
		if nodeName == "" {
			nodeName = item.NodeID
		}

		td.nodes = append(td.nodes, DNSNodeRecord{
			NodeID:      item.NodeID,
			NodeName:    nodeName,
			Records:     res.DNS.Records,
			Nameserver:  res.DNS.Nameserver,
			QueryTimeMS: res.DNS.QueryTimeMS,
			CheckedAt:   res.CheckedAt,
			Status:      res.Status,
		})
	}

	matrix := make([]DNSTargetMatrix, 0, len(matrixData))
	consistentCount := 0
	divergentCount := 0

	for targetID, td := range matrixData {
		var dnsType string
		var config targetPayload
		if len(td.target.Payload) > 0 {
			_ = json.Unmarshal(td.target.Payload, &config)
			dnsType = config.DNSType
		}
		if dnsType == "" {
			dnsType = "A"
		}

		targetName := td.target.Name
		if targetName == "" {
			targetName = targetID
		}
		host := td.target.Host
		if host == "" {
			host = config.Host
		}

		// Analyze consistency across nodes
		// Fingerprint each node's sorted record set
		recordSetCounts := make(map[string]int)
		nodeFingerprints := make([]string, len(td.nodes))
		var totalQueryTime int64

		for i, n := range td.nodes {
			totalQueryTime += n.QueryTimeMS
			sorted := make([]string, len(n.Records))
			copy(sorted, n.Records)
			sort.Strings(sorted)
			fp := strings.Join(sorted, ",")
			nodeFingerprints[i] = fp
			recordSetCounts[fp]++
		}

		uniqueSetsCount := len(recordSetCounts)
		isConsistent := uniqueSetsCount <= 1
		var divergentNodes []string

		if !isConsistent {
			// Find majority fingerprint
			majorityFP := ""
			maxCount := -1
			for fp, count := range recordSetCounts {
				if count > maxCount {
					maxCount = count
					majorityFP = fp
				}
			}
			for i, fp := range nodeFingerprints {
				if fp != majorityFP {
					divergentNodes = append(divergentNodes, td.nodes[i].NodeName)
				}
			}
			divergentCount++
		} else {
			consistentCount++
		}

		var avgQueryTime int64
		if len(td.nodes) > 0 {
			avgQueryTime = totalQueryTime / int64(len(td.nodes))
		}

		matrix = append(matrix, DNSTargetMatrix{
			TargetID:        targetID,
			TargetName:      targetName,
			Host:            host,
			DNSType:         dnsType,
			IsConsistent:    isConsistent,
			UniqueSetsCount: uniqueSetsCount,
			DivergentNodes:  divergentNodes,
			AvgQueryTimeMS:  avgQueryTime,
			Nodes:           td.nodes,
		})
	}

	sort.Slice(matrix, func(i, j int) bool {
		// Put divergent targets first, then sort by name
		if matrix[i].IsConsistent != matrix[j].IsConsistent {
			return !matrix[i].IsConsistent
		}
		return matrix[i].TargetName < matrix[j].TargetName
	})

	writeJSON(w, http.StatusOK, DNSMatrixResponse{
		TotalTargets:    len(matrix),
		ConsistentCount: consistentCount,
		DivergentCount:  divergentCount,
		Matrix:          matrix,
	})
}

// SyntheticTargetRequest represents the payload for creating or updating a synthetic target.
type SyntheticTargetRequest struct {
	ID              string                            `json:"id,omitempty"`
	Name            string                            `json:"name"`
	Protocol        string                            `json:"protocol"` // http, https, grpc, websocket, doh
	TargetURL       string                            `json:"target_url"`
	Method          string                            `json:"method,omitempty"`
	Headers         map[string]string                 `json:"headers,omitempty"`
	BodyPayload     string                            `json:"body_payload,omitempty"`
	Assertions      []protocol.SyntheticAssertionRule `json:"assertions,omitempty"`
	GRPCService     string                            `json:"grpc_service,omitempty"`
	TimeoutMS       int                               `json:"timeout_ms,omitempty"`
	IntervalSeconds int                               `json:"interval_seconds,omitempty"`
	NodeTags        []string                          `json:"node_tags,omitempty"`
	NodeIDs         []string                          `json:"node_ids,omitempty"`
	ConsensusNodes  int                               `json:"consensus_nodes,omitempty"`
	Enabled         *bool                             `json:"enabled,omitempty"`
}

// SyntheticTargetStatus wraps target definition, latest results from nodes, and consensus status.
type SyntheticTargetStatus struct {
	Target          db.SyntheticTargetRecord   `json:"target"`
	LatestResults   []db.SyntheticResultRecord `json:"latest_results"`
	TotalNodes      int                        `json:"total_nodes"`
	PassingNodes    int                        `json:"passing_nodes"`
	FailingNodes    int                        `json:"failing_nodes"`
	ConsensusStatus string                     `json:"consensus_status"` // "healthy", "degraded", "failing", "unknown"
	AvgLatencyMS    float64                    `json:"avg_latency_ms"`
	AvgTTFBMS       float64                    `json:"avg_ttfb_ms"`
}

// SyntheticResultsOverviewResponse is the full response for synthetic monitoring dashboard.
type SyntheticResultsOverviewResponse struct {
	TotalTargets    int                     `json:"total_targets"`
	PassingTargets  int                     `json:"passing_targets"`
	DegradedTargets int                     `json:"degraded_targets"`
	FailingTargets  int                     `json:"failing_targets"`
	Targets         []SyntheticTargetStatus `json:"targets"`
	GeneratedAt     int64                   `json:"generated_at"`
}

// SyntheticTestRequest represents the payload to test a synthetic target in real-time.
type SyntheticTestRequest struct {
	Protocol    string                            `json:"protocol"`
	TargetURL   string                            `json:"target_url"`
	Method      string                            `json:"method,omitempty"`
	Headers     map[string]string                 `json:"headers,omitempty"`
	BodyPayload string                            `json:"body_payload,omitempty"`
	Assertions  []protocol.SyntheticAssertionRule `json:"assertions,omitempty"`
	GRPCService string                            `json:"grpc_service,omitempty"`
	TimeoutMS   int                               `json:"timeout_ms,omitempty"`
}

func (s *Server) syntheticTargetsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, "/api/synthetic/targets")
	path = strings.TrimPrefix(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			s.listSyntheticTargets(w, r)
		} else {
			s.getSyntheticTarget(w, r, path)
		}
	case http.MethodPost:
		if path != "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		s.createSyntheticTarget(w, r)
	case http.MethodPatch:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		s.updateSyntheticTarget(w, r, path)
	case http.MethodDelete:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		s.deleteSyntheticTarget(w, r, path)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listSyntheticTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := s.service.Store().ListSyntheticTargets(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic targets unavailable")
		return
	}
	if targets == nil {
		targets = []db.SyntheticTargetRecord{}
	}
	writeJSON(w, http.StatusOK, targets)
}

func (s *Server) getSyntheticTarget(w http.ResponseWriter, r *http.Request, id string) {
	target, err := s.service.Store().GetSyntheticTarget(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrTargetNotFound) {
			writeJSONError(w, http.StatusNotFound, "synthetic target not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic target unavailable")
		return
	}
	writeJSON(w, http.StatusOK, target)
}

func (s *Server) createSyntheticTarget(w http.ResponseWriter, r *http.Request) {
	var req SyntheticTargetRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "synthetic target name is required")
		return
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "https"
	}
	switch proto {
	case "http", "https", "grpc", "websocket", "doh":
	default:
		writeJSONError(w, http.StatusBadRequest, "invalid protocol: must be http, https, grpc, websocket, or doh")
		return
	}
	targetURL := strings.TrimSpace(req.TargetURL)
	if targetURL == "" {
		writeJSONError(w, http.StatusBadRequest, "target_url is required")
		return
	}

	for _, a := range req.Assertions {
		if err := a.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid assertion: "+err.Error())
			return
		}
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}

	headersJSON := ""
	if len(req.Headers) > 0 {
		hBytes, _ := json.Marshal(req.Headers)
		headersJSON = string(hBytes)
	}

	assertionsJSON := ""
	if len(req.Assertions) > 0 {
		aBytes, _ := json.Marshal(req.Assertions)
		assertionsJSON = string(aBytes)
	}

	timeoutMS := req.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	intervalSeconds := req.IntervalSeconds
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}
	consensusNodes := req.ConsensusNodes
	if consensusNodes <= 0 {
		consensusNodes = 1
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = "syn-" + randomHex(8)
	}

	nodeTags := strings.Join(req.NodeTags, ",")
	nodeIDs := strings.Join(req.NodeIDs, ",")

	record := db.SyntheticTargetRecord{
		ID:              id,
		Name:            name,
		Protocol:        proto,
		TargetURL:       targetURL,
		Method:          method,
		Headers:         headersJSON,
		BodyPayload:     req.BodyPayload,
		Assertions:      assertionsJSON,
		GRPCService:     req.GRPCService,
		TimeoutMS:       timeoutMS,
		IntervalSeconds: intervalSeconds,
		NodeTags:        nodeTags,
		NodeIDs:         nodeIDs,
		ConsensusNodes:  consensusNodes,
		Enabled:         enabled,
	}

	if err := s.service.Store().CreateSyntheticTarget(r.Context(), record); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to create synthetic target")
		return
	}

	created, err := s.service.Store().GetSyntheticTarget(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusCreated, record)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateSyntheticTarget(w http.ResponseWriter, r *http.Request, id string) {
	existing, err := s.service.Store().GetSyntheticTarget(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrTargetNotFound) {
			writeJSONError(w, http.StatusNotFound, "synthetic target not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic target unavailable")
		return
	}

	var req SyntheticTargetRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	if req.Name != "" {
		existing.Name = strings.TrimSpace(req.Name)
	}
	if req.Protocol != "" {
		proto := strings.ToLower(strings.TrimSpace(req.Protocol))
		switch proto {
		case "http", "https", "grpc", "websocket", "doh":
			existing.Protocol = proto
		default:
			writeJSONError(w, http.StatusBadRequest, "invalid protocol")
			return
		}
	}
	if req.TargetURL != "" {
		existing.TargetURL = strings.TrimSpace(req.TargetURL)
	}
	if req.Method != "" {
		existing.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	}
	if req.Headers != nil {
		hBytes, _ := json.Marshal(req.Headers)
		existing.Headers = string(hBytes)
	}
	if req.BodyPayload != "" {
		existing.BodyPayload = req.BodyPayload
	}
	if req.Assertions != nil {
		for _, a := range req.Assertions {
			if err := a.Validate(); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid assertion: "+err.Error())
				return
			}
		}
		aBytes, _ := json.Marshal(req.Assertions)
		existing.Assertions = string(aBytes)
	}
	if req.GRPCService != "" {
		existing.GRPCService = req.GRPCService
	}
	if req.TimeoutMS > 0 {
		existing.TimeoutMS = req.TimeoutMS
	}
	if req.IntervalSeconds > 0 {
		existing.IntervalSeconds = req.IntervalSeconds
	}
	if req.ConsensusNodes > 0 {
		existing.ConsensusNodes = req.ConsensusNodes
	}
	if req.NodeTags != nil {
		existing.NodeTags = strings.Join(req.NodeTags, ",")
	}
	if req.NodeIDs != nil {
		existing.NodeIDs = strings.Join(req.NodeIDs, ",")
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := s.service.Store().UpdateSyntheticTarget(r.Context(), existing); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to update synthetic target")
		return
	}

	updated, err := s.service.Store().GetSyntheticTarget(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteSyntheticTarget(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.service.Store().DeleteSyntheticTarget(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrTargetNotFound) {
			writeJSONError(w, http.StatusNotFound, "synthetic target not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "failed to delete synthetic target")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) syntheticResultsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.serveSyntheticResults(w, r)
}

func (s *Server) publicSyntheticResultsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.serveSyntheticResults(w, r)
}

func (s *Server) serveSyntheticResults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	targets, err := s.service.Store().ListSyntheticTargets(ctx)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic results unavailable")
		return
	}
	latestResults, err := s.service.Store().ListLatestSyntheticResults(ctx)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic results unavailable")
		return
	}

	resultsByTarget := make(map[string][]db.SyntheticResultRecord)
	for _, res := range latestResults {
		resultsByTarget[res.TargetID] = append(resultsByTarget[res.TargetID], res)
	}

	var overviewTargets []SyntheticTargetStatus
	passingTargets := 0
	degradedTargets := 0
	failingTargets := 0

	for _, target := range targets {
		resList := resultsByTarget[target.ID]
		total := len(resList)
		passing := 0
		failing := 0
		var totalLat, totalTTFB float64

		for _, r := range resList {
			if r.Passed {
				passing++
			} else {
				failing++
			}
			totalLat += float64(r.TotalMS)
			totalTTFB += float64(r.TTFBMS)
		}

		consensus := "unknown"
		reqConsensus := target.ConsensusNodes
		if reqConsensus <= 0 {
			reqConsensus = 1
		}

		if total > 0 {
			if failing >= reqConsensus {
				consensus = "failing"
				failingTargets++
			} else if failing > 0 {
				consensus = "degraded"
				degradedTargets++
			} else {
				consensus = "healthy"
				passingTargets++
			}
		}

		var avgLat, avgTTFB float64
		if total > 0 {
			avgLat = totalLat / float64(total)
			avgTTFB = totalTTFB / float64(total)
		}

		if resList == nil {
			resList = []db.SyntheticResultRecord{}
		}

		overviewTargets = append(overviewTargets, SyntheticTargetStatus{
			Target:          target,
			LatestResults:   resList,
			TotalNodes:      total,
			PassingNodes:    passing,
			FailingNodes:    failing,
			ConsensusStatus: consensus,
			AvgLatencyMS:    avgLat,
			AvgTTFBMS:       avgTTFB,
		})
	}

	if overviewTargets == nil {
		overviewTargets = []SyntheticTargetStatus{}
	}

	writeJSON(w, http.StatusOK, SyntheticResultsOverviewResponse{
		TotalTargets:    len(targets),
		PassingTargets:  passingTargets,
		DegradedTargets: degradedTargets,
		FailingTargets:  failingTargets,
		Targets:         overviewTargets,
		GeneratedAt:     time.Now().UTC().Unix(),
	})
}

func (s *Server) syntheticHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	targetID := r.URL.Query().Get("target_id")
	nodeID := r.URL.Query().Get("node_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
			if limit > 500 {
				limit = 500
			}
		}
	}

	history, err := s.service.Store().ListSyntheticHistory(r.Context(), nodeID, targetID, limit)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic history unavailable")
		return
	}
	if history == nil {
		history = []db.SyntheticHistoryRecord{}
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) syntheticTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req SyntheticTestRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "https"
	}
	switch proto {
	case "http", "https", "grpc", "websocket", "doh":
	default:
		writeJSONError(w, http.StatusBadRequest, "invalid protocol")
		return
	}
	targetURL := strings.TrimSpace(req.TargetURL)
	if targetURL == "" {
		writeJSONError(w, http.StatusBadRequest, "target_url is required")
		return
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	timeoutMS := req.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}

	task := protocol.CheckTask{
		ID:          "simulation-test",
		Kind:        proto,
		Host:        targetURL,
		Method:      method,
		ServerURL:   targetURL,
		Headers:     req.Headers,
		BodyPayload: req.BodyPayload,
		Assertions:  req.Assertions,
		GRPCService: req.GRPCService,
		TimeoutMS:   timeoutMS,
		Enabled:     true,
	}

	synMonitor := monitor.NewSyntheticMonitor()
	result := synMonitor.Run(r.Context(), task)
	writeJSON(w, http.StatusOK, result)
}

