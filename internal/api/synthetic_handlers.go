package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/probewatch/probewatch/internal/db"
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
