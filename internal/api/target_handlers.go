package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

type targetRequest struct {
	ID              *string                `json:"id"`
	Name            *string                `json:"name"`
	Kind            *string                `json:"kind"`
	Host            *string                `json:"host"`
	Port            *int                   `json:"port"`
	Path            *string                `json:"path"`
	ExpectedStatus  *int                   `json:"expected_status"`
	DNSType         *string                `json:"dns_type"`
	TimeoutMS       *int                   `json:"timeout_ms"`
	MaxHops         *int                   `json:"max_hops"`
	IntervalSeconds *int                   `json:"interval_seconds"`
	Enabled         *bool                  `json:"enabled"`
	RegionRules     *[]protocol.RegionRule `json:"region_rules"`
}

type targetResponse struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Kind            string                `json:"kind"`
	Host            string                `json:"host"`
	Port            int                   `json:"port,omitempty"`
	Path            string                `json:"path,omitempty"`
	ExpectedStatus  int                   `json:"expected_status,omitempty"`
	DNSType         string                `json:"dns_type,omitempty"`
	TimeoutMS       int                   `json:"timeout_ms,omitempty"`
	MaxHops         int                   `json:"max_hops,omitempty"`
	IntervalSeconds int                   `json:"interval_seconds,omitempty"`
	Enabled         bool                  `json:"enabled"`
	RegionRules     []protocol.RegionRule `json:"region_rules,omitempty"`
}

type targetPayload struct {
	Host            string                `json:"host,omitempty"`
	Port            int                   `json:"port,omitempty"`
	Path            string                `json:"path,omitempty"`
	ExpectedStatus  int                   `json:"expected_status,omitempty"`
	DNSType         string                `json:"dns_type,omitempty"`
	TimeoutMS       int                   `json:"timeout_ms,omitempty"`
	MaxHops         int                   `json:"max_hops,omitempty"`
	IntervalSeconds int                   `json:"interval_seconds,omitempty"`
	RegionRules     []protocol.RegionRule `json:"region_rules,omitempty"`
}

func (s *Server) targetCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listTargets(w, r)
	case http.MethodPost:
		s.createTarget(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) targetAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "api" || parts[1] != "targets" || parts[2] == "" {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		s.updateTarget(w, r, parts[2])
	case http.MethodDelete:
		s.deleteTarget(w, r, parts[2])
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listTargets(w http.ResponseWriter, r *http.Request) {
	all := make([]targetResponse, 0)
	for _, kind := range targetKinds() {
		targets, err := s.service.Store().ListTargets(r.Context(), kind)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "targets unavailable")
			return
		}
		for _, target := range targets {
			response, err := targetToResponse(target)
			if err != nil {
				writeJSONError(w, http.StatusServiceUnavailable, "targets unavailable")
				return
			}
			all = append(all, response)
		}
	}
	writeJSON(w, http.StatusOK, all)
}

func (s *Server) createTarget(w http.ResponseWriter, r *http.Request) {
	var request targetRequest
	if err := decodeTargetRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	definition, err := request.definition(nil)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid target")
		return
	}
	record, err := s.service.Store().CreateTarget(r.Context(), definition, time.Now().UTC())
	if err != nil {
		if errors.Is(err, db.ErrTargetKindMismatch) {
			writeJSONError(w, http.StatusBadRequest, "invalid target")
			return
		}
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "constraint failed") {
			writeJSONError(w, http.StatusConflict, "target already exists")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "target unavailable")
		return
	}
	response, err := targetToResponse(record)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "target unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) updateTarget(w http.ResponseWriter, r *http.Request, id string) {
	existing, err := findTarget(r.Context(), s.service.Store(), id)
	if err != nil {
		writeTargetStoreError(w, err)
		return
	}
	var request targetRequest
	if err := decodeTargetRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	definition, err := request.definition(&existing)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid target")
		return
	}
	if request.ID != nil && *request.ID != id {
		writeJSONError(w, http.StatusBadRequest, "invalid target")
		return
	}
	record, err := s.service.Store().UpdateTarget(r.Context(), existing.Kind, id, definition, time.Now().UTC())
	if err != nil {
		writeTargetStoreError(w, err)
		return
	}
	response, err := targetToResponse(record)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "target unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) deleteTarget(w http.ResponseWriter, r *http.Request, id string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.requestBodyLimit()))
	if err != nil {
		writeRequestError(w, errRequestBodyTooLarge)
		return
	}
	if len(bytes.TrimSpace(body)) != 0 {
		writeRequestError(w, errInvalidRequest)
		return
	}
	target, err := findTarget(r.Context(), s.service.Store(), id)
	if err != nil {
		writeTargetStoreError(w, err)
		return
	}
	if err := s.service.Store().DeleteTarget(r.Context(), target.Kind, id); err != nil {
		writeTargetStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeTargetRequest(w http.ResponseWriter, r *http.Request, limit int64, destination *targetRequest) error {
	var raw json.RawMessage
	if err := decodeJSONObjectRequest(w, r, limit, &raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return errInvalidRequest
	}
	allowed := map[string]struct{}{
		"id": {}, "name": {}, "kind": {}, "host": {}, "port": {}, "path": {},
		"expected_status": {}, "dns_type": {}, "timeout_ms": {}, "max_hops": {},
		"interval_seconds": {}, "enabled": {}, "region_rules": {},
	}
	for name, value := range fields {
		if _, ok := allowed[name]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errInvalidRequest
		}
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return errInvalidRequest
	}
	return nil
}

func (r targetRequest) definition(existing *db.TargetRecord) (db.TargetDefinition, error) {
	config := targetPayload{}
	if existing != nil {
		if err := json.Unmarshal(existing.Payload, &config); err != nil {
			return db.TargetDefinition{}, err
		}
		if config.Host == "" {
			config.Host = existing.Host
		}
	}
	if r.ID != nil {
		if existing != nil && *r.ID != existing.ID {
			return db.TargetDefinition{}, errors.New("target ID cannot change")
		}
	}
	id := stringValue(r.ID)
	name := stringValue(r.Name)
	kindValue := stringValue(r.Kind)
	host := stringValue(r.Host)
	if existing != nil {
		id, name, kindValue, host = existing.ID, existing.Name, string(existing.Kind), config.Host
	}
	if r.Name != nil {
		name = *r.Name
	}
	if r.Kind != nil {
		kindValue = *r.Kind
	}
	if r.Host != nil {
		host = *r.Host
	}
	if r.Port != nil {
		config.Port = *r.Port
	}
	if r.Path != nil {
		config.Path = *r.Path
	}
	if r.ExpectedStatus != nil {
		config.ExpectedStatus = *r.ExpectedStatus
	}
	if r.DNSType != nil {
		config.DNSType = *r.DNSType
	}
	if r.TimeoutMS != nil {
		config.TimeoutMS = *r.TimeoutMS
	}
	if r.MaxHops != nil {
		config.MaxHops = *r.MaxHops
	}
	if r.IntervalSeconds != nil {
		config.IntervalSeconds = *r.IntervalSeconds
	}
	if r.RegionRules != nil {
		config.RegionRules = *r.RegionRules
	}
	enabled := true
	if existing != nil {
		enabled = existing.Enabled
	}
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	if config.TimeoutMS == 0 {
		config.TimeoutMS = 3000
	}
	if config.IntervalSeconds == 0 {
		config.IntervalSeconds = 60
	}
	if config.MaxHops == 0 {
		config.MaxHops = 20
	}
	if config.Port == 0 {
		switch kindValue {
		case "https":
			config.Port = 443
		case "dns":
			config.Port = 53
		default:
			config.Port = 80
		}
	}
	if kindValue == string(db.TargetKindMediaHTTP) {
		config.Host = host
	}
	if err := validateTargetFields(id, name, kindValue, host, config); err != nil {
		return db.TargetDefinition{}, err
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return db.TargetDefinition{}, err
	}
	return db.TargetDefinition{ID: id, Name: name, Kind: db.TargetKind(kindValue), Host: host, Enabled: enabled, Payload: payload}, nil
}

func validateTargetFields(id, name, kindValue, host string, config targetPayload) error {
	if strings.TrimSpace(id) != id || id == "" || len([]byte(id)) > 128 || strings.TrimSpace(name) != name || name == "" || len([]byte(name)) > 128 || kindValue == "" {
		return errors.New("invalid target identity")
	}
	if !validTargetKind(kindValue) || !validTargetHost(host) {
		return errors.New("invalid target host or kind")
	}
	if config.Port < 1 || config.Port > 65535 {
		return errors.New("invalid target port")
	}
	if config.TimeoutMS < 100 || config.TimeoutMS > 30000 || config.IntervalSeconds < 10 || config.IntervalSeconds > 86400 || config.MaxHops < 1 || config.MaxHops > 30 {
		return errors.New("invalid target limits")
	}
	if len([]byte(config.Path)) > 2048 || (config.Path != "" && (!strings.HasPrefix(config.Path, "/") || strings.ContainsAny(config.Path, "\\\r\n"))) {
		return errors.New("invalid target path")
	}
	if config.ExpectedStatus != 0 && (config.ExpectedStatus < 100 || config.ExpectedStatus > 599) {
		return errors.New("invalid expected status")
	}
	if len([]byte(config.DNSType)) > 16 || (config.DNSType != "" && config.DNSType != "A" && config.DNSType != "AAAA" && config.DNSType != "CNAME") {
		return errors.New("invalid DNS type")
	}
	if (kindValue == "http" || kindValue == "https" || kindValue == "media_http") && config.Path == "" {
		return errors.New("path is required")
	}
	if err := protocol.ValidateRegionRules(kindValue, config.RegionRules); err != nil {
		return err
	}
	if kindValue == "dns" && config.DNSType == "" {
		return errors.New("DNS type is required")
	}
	return nil
}

func validTargetKind(kind string) bool {
	for _, allowed := range targetKinds() {
		if string(allowed) == kind {
			return true
		}
	}
	return false
}

func validTargetHost(host string) bool {
	if host == "" || len([]byte(host)) > 253 || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/?#@\\") || strings.Contains(host, "://") {
		return false
	}
	for _, character := range host {
		if character <= ' ' || character == 0x7f {
			return false
		}
	}
	if net.ParseIP(host) != nil {
		return true
	}
	return !strings.Contains(host, ":")
}

func targetKinds() []db.TargetKind {
	return []db.TargetKind{db.TargetKindTCP, db.TargetKindHTTP, db.TargetKindHTTPS, db.TargetKindDNS, db.TargetKindMTR, db.TargetKindMediaHTTP}
}

func targetToResponse(target db.TargetRecord) (targetResponse, error) {
	var config targetPayload
	if err := json.Unmarshal(target.Payload, &config); err != nil {
		return targetResponse{}, err
	}
	host := target.Host
	if host == "" {
		host = config.Host
	}
	return targetResponse{ID: target.ID, Name: target.Name, Kind: string(target.Kind), Host: host, Port: config.Port, Path: config.Path, ExpectedStatus: config.ExpectedStatus, DNSType: config.DNSType, TimeoutMS: config.TimeoutMS, MaxHops: config.MaxHops, IntervalSeconds: config.IntervalSeconds, Enabled: target.Enabled, RegionRules: config.RegionRules}, nil
}

func findTarget(ctx context.Context, store *db.Store, id string) (db.TargetRecord, error) {
	for _, kind := range targetKinds() {
		target, err := store.GetTarget(ctx, kind, id)
		if err == nil {
			return target, nil
		}
		if !errors.Is(err, db.ErrTargetNotFound) && !errors.Is(err, db.ErrTargetKindMismatch) {
			return db.TargetRecord{}, err
		}
	}
	return db.TargetRecord{}, db.ErrTargetNotFound
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func writeTargetStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "target not found")
	case errors.Is(err, db.ErrTargetKindMismatch):
		writeJSONError(w, http.StatusBadRequest, "target kind mismatch")
	default:
		writeJSONError(w, http.StatusServiceUnavailable, "target unavailable")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) seedMediaTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	type preset struct {
		id          string
		name        string
		host        string
		path        string
		regionRules []protocol.RegionRule
	}
	presets := []preset{
		{
			id:   "media-chatgpt",
			name: "OpenAI / ChatGPT",
			host: "chatgpt.com",
			path: "/cdn-cgi/trace",
		},
		{
			id:   "media-claude",
			name: "Claude AI",
			host: "claude.ai",
			path: "/cdn-cgi/trace",
		},
		{
			id:          "media-youtube",
			name:        "YouTube Premium",
			host:        "www.youtube.com",
			path:        "/premium",
			regionRules: []protocol.RegionRule{{Region: "US", Contains: "Premium"}},
		},
		{
			id:          "media-netflix",
			name:        "Netflix",
			host:        "www.netflix.com",
			path:        "/title/80018499",
			regionRules: []protocol.RegionRule{{Region: "US", Contains: "United States"}},
		},
		{
			id:   "media-disney",
			name: "Disney+",
			host: "www.disneyplus.com",
			path: "/",
		},
		{
			id:   "media-tiktok",
			name: "TikTok",
			host: "www.tiktok.com",
			path: "/",
		},
		{
			id:   "media-spotify",
			name: "Spotify",
			host: "www.spotify.com",
			path: "/",
		},
		{
			id:          "media-bilibili",
			name:        "Bilibili 港澳台",
			host:        "api.bilibili.com",
			path:        "/pgc/player/web/v2/playurl?cid=144541892&ep_id=234405",
			regionRules: []protocol.RegionRule{
				{Region: "TW", Contains: "\"code\":0"},
				{Region: "HK", Contains: "\"code\":-10403"},
			},
		},
	}

	createdCount := 0
	enabledCount := 0
	now := time.Now().UTC()

	for _, p := range presets {
		existing, err := s.service.Store().GetTarget(r.Context(), db.TargetKindMediaHTTP, p.id)
		if err == nil {
			if !existing.Enabled {
				existingDef := db.TargetDefinition{
					ID:      existing.ID,
					Name:    existing.Name,
					Kind:    existing.Kind,
					Host:    existing.Host,
					Enabled: true,
					Payload: existing.Payload,
				}
				_, _ = s.service.Store().UpdateTarget(r.Context(), db.TargetKindMediaHTTP, p.id, existingDef, now)
				enabledCount++
			}
			continue
		}

		payloadObj := targetPayload{
			Host:            p.host,
			Port:            443,
			Path:            p.path,
			IntervalSeconds: 60,
			TimeoutMS:       5000,
			RegionRules:     p.regionRules,
		}
		rawPayload, err := json.Marshal(payloadObj)
		if err != nil {
			continue
		}

		def := db.TargetDefinition{
			ID:      p.id,
			Name:    p.name,
			Kind:    db.TargetKindMediaHTTP,
			Host:    p.host,
			Enabled: true,
			Payload: rawPayload,
		}
		if _, err := s.service.Store().CreateTarget(r.Context(), def, now); err == nil {
			createdCount++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"created": createdCount,
		"enabled": enabledCount,
		"total":   len(presets),
	})
}
