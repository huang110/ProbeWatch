package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/monitor"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
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

// sanitizePublicCertificates removes target domains, certificate identities,
// node IDs and other inventory details from the unauthenticated dashboard.
// Public pages retain only fleet health counts and expiry status.
func sanitizePublicCertificates(response CertificatesResponse) CertificatesResponse {
	for i := range response.Certificates {
		item := &response.Certificates[i]
		item.TargetID = fmt.Sprintf("certificate-%d", i+1)
		item.TargetName = fmt.Sprintf("HTTPS target %d", i+1)
		item.Host = ""
		item.Port = 0
		item.Subject = ""
		item.Issuer = ""
		item.DNSNames = nil
		item.NodeID = ""
		item.NodeName = fmt.Sprintf("Node %d", i+1)
		item.CipherSuite = ""
	}
	return response
}

func sanitizePublicDNSMatrix(response DNSMatrixResponse) DNSMatrixResponse {
	for i := range response.Matrix {
		item := &response.Matrix[i]
		item.TargetID = fmt.Sprintf("dns-target-%d", i+1)
		item.TargetName = fmt.Sprintf("DNS target %d", i+1)
		item.Host = ""
		nodeLabels := make(map[string]string, len(item.Nodes))
		for j := range item.Nodes {
			node := &item.Nodes[j]
			// Capture the original node name before it is overwritten so
			// DivergentNodes entries (which reference original names) can
			// be mapped to the sanitized per-position label.
			originalName := node.NodeName
			node.NodeID = fmt.Sprintf("node-%d", j+1)
			node.NodeName = fmt.Sprintf("Node %d", j+1)
			nodeLabels[originalName] = node.NodeName
			node.Records = nil
			node.Nameserver = ""
		}
		for j, name := range item.DivergentNodes {
			if label, ok := nodeLabels[name]; ok {
				item.DivergentNodes[j] = label
				continue
			}
			item.DivergentNodes[j] = fmt.Sprintf("Node %d", len(nodeLabels)+1)
		}
	}
	return response
}

func (s *Server) certificatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/public/") && s.publicLimiter != nil && !s.publicLimiter.Allow(s.publicLimiterKey(r), time.Now().UTC()) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	ctx := r.Context()
	isPublic := strings.HasPrefix(r.URL.Path, "/api/public/")
	fullAccess := true
	var allowedIDs map[string]struct{}
	if !isPublic {
		user, ok := s.authenticateUser(w, r)
		if !ok {
			return
		}
		var err error
		fullAccess, _, allowedIDs, err = s.getUserNodeScope(ctx, user)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "certificates unavailable")
			return
		}
	}

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
		if !isPublic && !fullAccess {
			if _, allowed := allowedIDs[item.NodeID]; !allowed {
				continue
			}
		}
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

	response := CertificatesResponse{
		Total:        len(items),
		ValidCount:   validCount,
		ExpiringSoon: expiringSoonCount,
		ExpiredCount: expiredCount,
		Certificates: items,
	}
	if strings.HasPrefix(r.URL.Path, "/api/public/") {
		response = sanitizePublicCertificates(response)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) dnsMatrixHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/public/") && s.publicLimiter != nil && !s.publicLimiter.Allow(s.publicLimiterKey(r), time.Now().UTC()) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	ctx := r.Context()
	isPublic := strings.HasPrefix(r.URL.Path, "/api/public/")
	fullAccess := true
	var allowedIDs map[string]struct{}
	if !isPublic {
		user, ok := s.authenticateUser(w, r)
		if !ok {
			return
		}
		var err error
		fullAccess, _, allowedIDs, err = s.getUserNodeScope(ctx, user)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "dns matrix unavailable")
			return
		}
	}

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
		if !isPublic && !fullAccess {
			if _, allowed := allowedIDs[item.NodeID]; !allowed {
				continue
			}
		}
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

	response := DNSMatrixResponse{
		TotalTargets:    len(matrix),
		ConsistentCount: consistentCount,
		DivergentCount:  divergentCount,
		Matrix:          matrix,
	}
	if strings.HasPrefix(r.URL.Path, "/api/public/") {
		response = sanitizePublicDNSMatrix(response)
	}
	writeJSON(w, http.StatusOK, response)
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

// RestrictedSyntheticTarget is returned to authenticated users without
// fleet-wide administrative access. A scoped administrator must not receive
// target URLs, headers, assertions or raw payloads outside the node scope.
// Synthetic target definitions may contain credentials in headers/body and
// internal URLs in the target configuration, so those fields must not be
// exposed merely because a user can view node metrics.
type RestrictedSyntheticTarget struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Protocol        string    `json:"protocol"`
	Method          string    `json:"method"`
	TimeoutMS       int       `json:"timeout_ms"`
	IntervalSeconds int       `json:"interval_seconds"`
	ConsensusNodes  int       `json:"consensus_nodes"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type RestrictedSyntheticResultRecord struct {
	NodeName   string    `json:"node_name"`
	Protocol   string    `json:"protocol"`
	StatusCode int       `json:"status_code"`
	Passed     bool      `json:"passed"`
	DNSMS      int64     `json:"dns_ms"`
	ConnectMS  int64     `json:"connect_ms"`
	TLSMS      int64     `json:"tls_ms"`
	TTFBMS     int64     `json:"ttfb_ms"`
	TotalMS    int64     `json:"total_ms"`
	CheckedAt  time.Time `json:"checked_at"`
}

type RestrictedSyntheticTargetStatus struct {
	Target          RestrictedSyntheticTarget         `json:"target"`
	LatestResults   []RestrictedSyntheticResultRecord `json:"latest_results"`
	TotalNodes      int                               `json:"total_nodes"`
	PassingNodes    int                               `json:"passing_nodes"`
	FailingNodes    int                               `json:"failing_nodes"`
	ConsensusStatus string                            `json:"consensus_status"`
	AvgLatencyMS    float64                           `json:"avg_latency_ms"`
	AvgTTFBMS       float64                           `json:"avg_ttfb_ms"`
}

type RestrictedSyntheticResultsOverviewResponse struct {
	TotalTargets    int                               `json:"total_targets"`
	PassingTargets  int                               `json:"passing_targets"`
	DegradedTargets int                               `json:"degraded_targets"`
	FailingTargets  int                               `json:"failing_targets"`
	Targets         []RestrictedSyntheticTargetStatus `json:"targets"`
	GeneratedAt     int64                             `json:"generated_at"`
	ReadOnly        bool                              `json:"read_only"`
}

type RestrictedSyntheticHistoryRecord struct {
	NodeName   string    `json:"node_name"`
	Protocol   string    `json:"protocol"`
	StatusCode int       `json:"status_code"`
	Passed     bool      `json:"passed"`
	DNSMS      int64     `json:"dns_ms"`
	ConnectMS  int64     `json:"connect_ms"`
	TLSMS      int64     `json:"tls_ms"`
	TTFBMS     int64     `json:"ttfb_ms"`
	TotalMS    int64     `json:"total_ms"`
	CheckedAt  time.Time `json:"checked_at"`
	RecordedAt time.Time `json:"recorded_at"`
}

// PublicSyntheticTarget is the allow-listed target shape exposed to guests.
// The private target record contains URLs, headers, assertions, node IDs and
// scheduling details; none of those belong in an unauthenticated response.
type PublicSyntheticTarget struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
}

// PublicSyntheticResultRecord contains only anonymous outcome and timing
// information. In particular it deliberately omits target/node IDs, raw
// payloads, assertion details and operator-selected names.
type PublicSyntheticResultRecord struct {
	NodeName   string    `json:"node_name"`
	Protocol   string    `json:"protocol"`
	StatusCode int       `json:"status_code"`
	Passed     bool      `json:"passed"`
	DNSMS      int64     `json:"dns_ms"`
	ConnectMS  int64     `json:"connect_ms"`
	TLSMS      int64     `json:"tls_ms"`
	TTFBMS     int64     `json:"ttfb_ms"`
	TotalMS    int64     `json:"total_ms"`
	CheckedAt  time.Time `json:"checked_at"`
}

type PublicSyntheticTargetStatus struct {
	Target          PublicSyntheticTarget         `json:"target"`
	LatestResults   []PublicSyntheticResultRecord `json:"latest_results"`
	TotalNodes      int                           `json:"total_nodes"`
	PassingNodes    int                           `json:"passing_nodes"`
	FailingNodes    int                           `json:"failing_nodes"`
	ConsensusStatus string                        `json:"consensus_status"`
	AvgLatencyMS    float64                       `json:"avg_latency_ms"`
	AvgTTFBMS       float64                       `json:"avg_ttfb_ms"`
}

type PublicSyntheticResultsOverviewResponse struct {
	TotalTargets    int                           `json:"total_targets"`
	PassingTargets  int                           `json:"passing_targets"`
	DegradedTargets int                           `json:"degraded_targets"`
	FailingTargets  int                           `json:"failing_targets"`
	Targets         []PublicSyntheticTargetStatus `json:"targets"`
	GeneratedAt     int64                         `json:"generated_at"`
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

const (
	// Keep persisted synthetic targets within the same bounds enforced by the
	// agent wire validator. Without server-side checks an administrator could
	// save a 1ms interval or an unbounded consensus threshold, causing a low-
	// memory agent to schedule excessive work or produce permanently degraded
	// status calculations.
	minSyntheticTimeoutMS      = 100
	maxSyntheticTimeoutMS      = 30_000
	minSyntheticIntervalSec    = 10
	maxSyntheticIntervalSec    = 86_400
	maxSyntheticConsensusNodes = 1_000
)

func validateSyntheticSchedule(timeoutMS, intervalSeconds, consensusNodes int, requireAll bool) error {
	if requireAll || timeoutMS != 0 {
		if timeoutMS < minSyntheticTimeoutMS || timeoutMS > maxSyntheticTimeoutMS {
			return fmt.Errorf("timeout_ms must be between %d and %d", minSyntheticTimeoutMS, maxSyntheticTimeoutMS)
		}
	}
	if requireAll || intervalSeconds != 0 {
		if intervalSeconds < minSyntheticIntervalSec || intervalSeconds > maxSyntheticIntervalSec {
			return fmt.Errorf("interval_seconds must be between %d and %d", minSyntheticIntervalSec, maxSyntheticIntervalSec)
		}
	}
	if requireAll || consensusNodes != 0 {
		if consensusNodes < 1 || consensusNodes > maxSyntheticConsensusNodes {
			return fmt.Errorf("consensus_nodes must be between 1 and %d", maxSyntheticConsensusNodes)
		}
	}
	return nil
}

func syntheticURLSchemes(proto string) []string {
	switch proto {
	case "http":
		return []string{"http"}
	case "https":
		return []string{"https"}
	case "websocket":
		return []string{"ws", "wss", "http", "https"}
	case "grpc":
		return []string{"grpc", "grpcs", "http", "https"}
	case "doh":
		return []string{"http", "https"}
	default:
		return nil
	}
}

func (s *Server) validateSyntheticURL(raw, proto string) error {
	if _, err := security.ValidateURLWithOptions(raw, syntheticURLSchemes(proto), true, s.cfg.Environment == "development"); err != nil {
		return err
	}
	return nil
}

func (s *Server) syntheticTargetsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireFleetWideNodeScope(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/synthetic/targets")
	path = strings.TrimPrefix(path, "/")

	mw := NewMiddleware(s.service, s.cfg)
	adminAction := mw.RequireRole(db.RoleAdmin)

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
		adminAction(mw.RequireCSRF(http.HandlerFunc(s.createSyntheticTarget))).ServeHTTP(w, r)
	case http.MethodPatch:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		adminAction(mw.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.updateSyntheticTarget(w, r, path)
		}))).ServeHTTP(w, r)
	case http.MethodDelete:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		adminAction(mw.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.deleteSyntheticTarget(w, r, path)
		}))).ServeHTTP(w, r)
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
	if err := s.validateSyntheticURL(targetURL, proto); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid target_url: "+err.Error())
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
	if timeoutMS < 0 {
		writeJSONError(w, http.StatusBadRequest, "timeout_ms must not be negative")
		return
	}
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	intervalSeconds := req.IntervalSeconds
	if intervalSeconds < 0 {
		writeJSONError(w, http.StatusBadRequest, "interval_seconds must not be negative")
		return
	}
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}
	consensusNodes := req.ConsensusNodes
	if consensusNodes < 0 {
		writeJSONError(w, http.StatusBadRequest, "consensus_nodes must not be negative")
		return
	}
	if consensusNodes <= 0 {
		consensusNodes = 1
	}
	if err := validateSyntheticSchedule(timeoutMS, intervalSeconds, consensusNodes, true); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
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
	if _, err := syntheticTargetToCheckTask(record, s.cfg.Environment == "development"); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid synthetic target: "+err.Error())
		return
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
		targetURL := strings.TrimSpace(req.TargetURL)
		if err := s.validateSyntheticURL(targetURL, existing.Protocol); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid target_url: "+err.Error())
			return
		}
		existing.TargetURL = targetURL
	}
	if req.Protocol != "" && req.TargetURL == "" {
		if err := s.validateSyntheticURL(existing.TargetURL, existing.Protocol); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid target_url: "+err.Error())
			return
		}
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
	if req.TimeoutMS != 0 {
		if err := validateSyntheticSchedule(req.TimeoutMS, 0, 0, false); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		existing.TimeoutMS = req.TimeoutMS
	}
	if req.IntervalSeconds != 0 {
		if err := validateSyntheticSchedule(0, req.IntervalSeconds, 0, false); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		existing.IntervalSeconds = req.IntervalSeconds
	}
	if req.ConsensusNodes != 0 {
		if err := validateSyntheticSchedule(0, 0, req.ConsensusNodes, false); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
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
	if _, err := syntheticTargetToCheckTask(existing, s.cfg.Environment == "development"); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid synthetic target: "+err.Error())
		return
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
	s.serveSyntheticResults(w, r, false)
}

func (s *Server) publicSyntheticResultsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.publicLimiter != nil && !s.publicLimiter.Allow(s.publicLimiterKey(r), time.Now().UTC()) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	s.serveSyntheticResults(w, r, true)
}

func (s *Server) serveSyntheticResults(w http.ResponseWriter, r *http.Request, isPublic bool) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	fullAccess := true
	adminAccess := false
	var allowedIDs map[string]struct{}
	var allowedNodes []db.Node
	if !isPublic {
		user, ok := s.authenticateUser(w, r)
		if !ok {
			return
		}
		var scopeErr error
		fullAccess, _, allowedIDs, scopeErr = s.getUserNodeScope(ctx, user)
		if scopeErr != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "synthetic results unavailable")
			return
		}
		// An administrator with an explicit node allow-list is still scoped.
		// Treat only fleet-wide administrators as entitled to private target
		// definitions and raw result payloads.
		adminAccess = user.IsAdmin() && fullAccess
		if !fullAccess {
			nodes, listErr := s.service.Store().ListNodes(ctx)
			if listErr != nil {
				writeJSONError(w, http.StatusServiceUnavailable, "synthetic results unavailable")
				return
			}
			for _, node := range nodes {
				if _, allowed := allowedIDs[node.ID]; allowed {
					allowedNodes = append(allowedNodes, node)
				}
			}
		}
	}
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
		if !isPublic && !fullAccess {
			if _, allowed := allowedIDs[res.NodeID]; !allowed {
				continue
			}
		}
		resultsByTarget[res.TargetID] = append(resultsByTarget[res.TargetID], res)
	}

	var overviewTargets []SyntheticTargetStatus
	passingTargets := 0
	degradedTargets := 0
	failingTargets := 0

	for _, target := range targets {
		if !isPublic && !fullAccess {
			matches := false
			for _, node := range allowedNodes {
				if nodeMatchesSyntheticTarget(node, target) {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}
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
			// A target cannot be healthy until the configured number of
			// distinct nodes has reported. Treat missing samples as degraded
			// instead of silently declaring a single passing node healthy.
			if total < reqConsensus {
				consensus = "degraded"
				degradedTargets++
			} else if failing >= reqConsensus {
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

	response := SyntheticResultsOverviewResponse{
		// For node-scoped users, targets without any allowed node are omitted
		// above. Count the response slice rather than the unfiltered store list,
		// otherwise the UI leaks the fleet-wide target count and shows an
		// impossible pass-rate denominator.
		TotalTargets:    len(overviewTargets),
		PassingTargets:  passingTargets,
		DegradedTargets: degradedTargets,
		FailingTargets:  failingTargets,
		Targets:         overviewTargets,
		GeneratedAt:     time.Now().UTC().Unix(),
	}
	if isPublic {
		writeJSON(w, http.StatusOK, sanitizePublicSyntheticResults(response))
		return
	}
	if !adminAccess {
		writeJSON(w, http.StatusOK, sanitizeRestrictedSyntheticResults(response))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// sanitizePublicSyntheticResults converts the private overview into a strict
// allow-list for the unauthenticated dashboard. Anonymous labels are assigned
// per response and are never derived from database IDs or operator names.
func sanitizePublicSyntheticResults(response SyntheticResultsOverviewResponse) PublicSyntheticResultsOverviewResponse {
	publicTargets := make([]PublicSyntheticTargetStatus, 0, len(response.Targets))
	nodeLabels := make(map[string]string)
	nextNode := 1
	for targetIndex, item := range response.Targets {
		targetLabel := fmt.Sprintf("Synthetic target %d", targetIndex+1)
		publicResults := make([]PublicSyntheticResultRecord, 0, len(item.LatestResults))
		for _, result := range item.LatestResults {
			labelKey := result.NodeID
			if labelKey == "" {
				// A missing node ID is unusual, but do not let several such
				// records collapse into one public label.
				labelKey = fmt.Sprintf("missing-node-%d", nextNode)
			}
			label, ok := nodeLabels[labelKey]
			if !ok {
				label = fmt.Sprintf("Node %d", nextNode)
				nodeLabels[labelKey] = label
				nextNode++
			}
			publicResults = append(publicResults, PublicSyntheticResultRecord{
				NodeName: label,
				// The result is agent-supplied telemetry. Use the persisted target
				// protocol so an untrusted agent cannot inject an arbitrary label
				// into the public response or contradict the configured check.
				Protocol:   item.Target.Protocol,
				StatusCode: result.StatusCode,
				Passed:     result.Passed,
				DNSMS:      result.DNSMS,
				ConnectMS:  result.ConnectMS,
				TLSMS:      result.TLSMS,
				TTFBMS:     result.TTFBMS,
				TotalMS:    result.TotalMS,
				CheckedAt:  result.CheckedAt,
			})
		}
		if publicResults == nil {
			publicResults = []PublicSyntheticResultRecord{}
		}
		publicTargets = append(publicTargets, PublicSyntheticTargetStatus{
			Target: PublicSyntheticTarget{
				ID:       fmt.Sprintf("synthetic-%d", targetIndex+1),
				Name:     targetLabel,
				Protocol: item.Target.Protocol,
			},
			LatestResults:   publicResults,
			TotalNodes:      item.TotalNodes,
			PassingNodes:    item.PassingNodes,
			FailingNodes:    item.FailingNodes,
			ConsensusStatus: item.ConsensusStatus,
			AvgLatencyMS:    item.AvgLatencyMS,
			AvgTTFBMS:       item.AvgTTFBMS,
		})
	}
	if publicTargets == nil {
		publicTargets = []PublicSyntheticTargetStatus{}
	}
	return PublicSyntheticResultsOverviewResponse{
		TotalTargets:    response.TotalTargets,
		PassingTargets:  response.PassingTargets,
		DegradedTargets: response.DegradedTargets,
		FailingTargets:  response.FailingTargets,
		Targets:         publicTargets,
		GeneratedAt:     response.GeneratedAt,
	}
}

// sanitizeSyntheticHistory removes identifiers, target names and raw agent
// payloads from history returned to non-admin users. Synthetic checks commonly
// contain internal URLs and credentials; timing/outcome data is sufficient for
// read-only dashboards.
func sanitizeSyntheticHistory(history []db.SyntheticHistoryRecord) []RestrictedSyntheticHistoryRecord {
	result := make([]RestrictedSyntheticHistoryRecord, 0, len(history))
	nodeLabels := make(map[string]string)
	nextNode := 1
	for _, item := range history {
		key := item.NodeID
		if key == "" {
			key = fmt.Sprintf("missing-node-%d", nextNode)
		}
		label, ok := nodeLabels[key]
		if !ok {
			label = fmt.Sprintf("Node %d", nextNode)
			nodeLabels[key] = label
			nextNode++
		}
		result = append(result, RestrictedSyntheticHistoryRecord{
			NodeName:   label,
			Protocol:   item.Protocol,
			StatusCode: item.StatusCode,
			Passed:     item.Passed,
			DNSMS:      item.DNSMS,
			ConnectMS:  item.ConnectMS,
			TLSMS:      item.TLSMS,
			TTFBMS:     item.TTFBMS,
			TotalMS:    item.TotalMS,
			CheckedAt:  item.CheckedAt,
			RecordedAt: item.RecordedAt,
		})
	}
	return result
}

func sanitizeRestrictedSyntheticResults(response SyntheticResultsOverviewResponse) RestrictedSyntheticResultsOverviewResponse {
	targets := make([]RestrictedSyntheticTargetStatus, 0, len(response.Targets))
	nodeLabels := make(map[string]string)
	nextNode := 1
	for targetIndex, item := range response.Targets {
		results := make([]RestrictedSyntheticResultRecord, 0, len(item.LatestResults))
		for _, result := range item.LatestResults {
			labelKey := result.NodeID
			if labelKey == "" {
				labelKey = fmt.Sprintf("missing-node-%d", nextNode)
			}
			label, ok := nodeLabels[labelKey]
			if !ok {
				label = fmt.Sprintf("Node %d", nextNode)
				nodeLabels[labelKey] = label
				nextNode++
			}
			results = append(results, RestrictedSyntheticResultRecord{
				NodeName:   label,
				Protocol:   item.Target.Protocol,
				StatusCode: result.StatusCode,
				Passed:     result.Passed,
				DNSMS:      result.DNSMS,
				ConnectMS:  result.ConnectMS,
				TLSMS:      result.TLSMS,
				TTFBMS:     result.TTFBMS,
				TotalMS:    result.TotalMS,
				CheckedAt:  result.CheckedAt,
			})
		}
		targets = append(targets, RestrictedSyntheticTargetStatus{
			Target: RestrictedSyntheticTarget{
				ID:              fmt.Sprintf("synthetic-%d", targetIndex+1),
				Name:            item.Target.Name,
				Protocol:        item.Target.Protocol,
				Method:          item.Target.Method,
				TimeoutMS:       item.Target.TimeoutMS,
				IntervalSeconds: item.Target.IntervalSeconds,
				ConsensusNodes:  item.Target.ConsensusNodes,
				Enabled:         item.Target.Enabled,
				CreatedAt:       item.Target.CreatedAt,
				UpdatedAt:       item.Target.UpdatedAt,
			},
			LatestResults:   results,
			TotalNodes:      item.TotalNodes,
			PassingNodes:    item.PassingNodes,
			FailingNodes:    item.FailingNodes,
			ConsensusStatus: item.ConsensusStatus,
			AvgLatencyMS:    item.AvgLatencyMS,
			AvgTTFBMS:       item.AvgTTFBMS,
		})
	}
	return RestrictedSyntheticResultsOverviewResponse{
		TotalTargets:    response.TotalTargets,
		PassingTargets:  response.PassingTargets,
		DegradedTargets: response.DegradedTargets,
		FailingTargets:  response.FailingTargets,
		Targets:         targets,
		GeneratedAt:     response.GeneratedAt,
		ReadOnly:        true,
	}
}

func (s *Server) syntheticHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	targetID := strings.TrimSpace(r.URL.Query().Get("target_id"))
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	user, ok := s.authenticateUser(w, r)
	if !ok {
		return
	}
	fullAccess, _, allowedIDs, scopeErr := s.getUserNodeScope(r.Context(), user)
	if scopeErr != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic history unavailable")
		return
	}
	if !fullAccess && nodeID != "" {
		if _, allowed := allowedIDs[nodeID]; !allowed {
			writeJSON(w, http.StatusOK, []db.SyntheticHistoryRecord{})
			return
		}
	}
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

	var history []db.SyntheticHistoryRecord
	var err error
	if fullAccess {
		history, err = s.service.Store().ListSyntheticHistory(r.Context(), nodeID, targetID, limit)
	} else if nodeID != "" {
		// The node was authorized above. Keep the scope predicate in SQL so the
		// requested limit is applied to authorized rows, not to fleet-wide rows
		// that are filtered out afterwards.
		history, err = s.service.Store().ListSyntheticHistoryForNodes(r.Context(), []string{nodeID}, targetID, limit)
	} else {
		// Apply the node allow-list in SQL before LIMIT. Filtering a fleet-wide
		// result after the query can hide older authorized samples whenever newer
		// samples belong to nodes outside the caller's scope.
		nodeIDs := make([]string, 0, len(allowedIDs))
		for id := range allowedIDs {
			nodeIDs = append(nodeIDs, id)
		}
		history, err = s.service.Store().ListSyntheticHistoryForNodes(r.Context(), nodeIDs, targetID, limit)
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "synthetic history unavailable")
		return
	}
	if history == nil {
		history = []db.SyntheticHistoryRecord{}
	}
	if !fullAccess || !user.IsAdmin() {
		writeJSON(w, http.StatusOK, sanitizeSyntheticHistory(history))
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) syntheticTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireFleetWideNodeScope(w, r) {
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
	if err := s.validateSyntheticURL(targetURL, proto); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid target_url: "+err.Error())
		return
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	timeoutMS := req.TimeoutMS
	if timeoutMS < 0 {
		writeJSONError(w, http.StatusBadRequest, "timeout_ms must not be negative")
		return
	}
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	if err := validateSyntheticSchedule(timeoutMS, 0, 0, false); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
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

	// Development mode may intentionally exercise a local httptest/lab
	// endpoint. Production keeps the monitor SSRF-safe by default.
	synMonitor := monitor.NewSyntheticMonitorWithOptions(s.cfg.Environment == "development")
	result := synMonitor.Run(r.Context(), task)
	writeJSON(w, http.StatusOK, result)
}
