package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrIncidentNotFound         = errors.New("incident not found")
	ErrStatusPageConfigNotFound = errors.New("status page config not found")
)

const (
	IncidentStatusInvestigating = "investigating"
	IncidentStatusIdentified    = "identified"
	IncidentStatusMonitoring    = "monitoring"
	IncidentStatusResolved      = "resolved"

	IncidentImpactNone     = "none"
	IncidentImpactMinor    = "minor"
	IncidentImpactMajor    = "major"
	IncidentImpactCritical = "critical"
)

// StatusPageComponent represents a monitored service or node displayed on the status page.
type StatusPageComponent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	NodeID      string `json:"node_id,omitempty"`
	Description string `json:"description,omitempty"`
	ShowLatency bool   `json:"show_latency"`
	Order       int    `json:"order"`
}

// StatusPageConfig represents the public and admin configuration of a status page.
type StatusPageConfig struct {
	ID             string                `json:"id"`
	Title          string                `json:"title"`
	Description    string                `json:"description"`
	Announcement   string                `json:"announcement"`
	ShowUptimeDays int                   `json:"show_uptime_days"`
	CustomCSS      string                `json:"custom_css"`
	Components     []StatusPageComponent `json:"components"`
	CreatedAt      int64                 `json:"created_at"`
	UpdatedAt      int64                 `json:"updated_at"`
}

// Incident represents a service interruption or scheduled maintenance announcement.
type Incident struct {
	ID               string           `json:"id"`
	Title            string           `json:"title"`
	Status           string           `json:"status"` // investigating, identified, monitoring, resolved
	Impact           string           `json:"impact"` // none, minor, major, critical
	IsMaintenance    bool             `json:"is_maintenance"`
	ScheduledStartAt *int64           `json:"scheduled_start_at,omitempty"`
	ScheduledEndAt   *int64           `json:"scheduled_end_at,omitempty"`
	CreatedAt        int64            `json:"created_at"`
	ResolvedAt       *int64           `json:"resolved_at,omitempty"`
	UpdatedAt        int64            `json:"updated_at"`
	Updates          []IncidentUpdate `json:"updates,omitempty"`
}

// IncidentUpdate represents a chronological message entry posted on an incident.
type IncidentUpdate struct {
	ID         string `json:"id"`
	IncidentID string `json:"incident_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	CreatedAt  int64  `json:"created_at"`
}

// DailyUptime represents a single day's SLA health block.
type DailyUptime struct {
	Date           string  `json:"date"`            // "2026-09-25"
	UptimePct      float64 `json:"uptime_pct"`      // 99.98
	Status         string  `json:"status"`          // "operational", "degraded", "outage", "no_data"
	IncidentsCount int     `json:"incidents_count"` // number of incidents on this day
}

// ComponentStatus represents the evaluated status of a component on the public status page.
type ComponentStatus struct {
	Component     StatusPageComponent `json:"component"`
	CurrentStatus string              `json:"current_status"` // "operational", "degraded", "outage"
	LatencyMs     *float64            `json:"latency_ms,omitempty"`
	Uptime24h     float64             `json:"uptime_24h"`
	Uptime7d      float64             `json:"uptime_7d"`
	Uptime30d     float64             `json:"uptime_30d"`
	Uptime90d     float64             `json:"uptime_90d"`
	DailyUptimes  []DailyUptime       `json:"daily_uptimes"`
}

// PublicStatusPageResponse is the combined payload served to public viewers.
type PublicStatusPageResponse struct {
	Config          StatusPageConfig  `json:"config"`
	OverallStatus   string            `json:"overall_status"` // operational, degraded, partial_outage, major_outage, under_maintenance
	OverallMessage  string            `json:"overall_message"`
	ActiveIncidents []Incident        `json:"active_incidents"`
	Maintenance     []Incident        `json:"maintenance"`
	Components      []ComponentStatus `json:"components"`
	LastUpdatedAt   time.Time         `json:"last_updated_at"`
}

func generateIncidentID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("inc_%x", b)
}

func generateUpdateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("upd_%x", b)
}

// GetStatusPageConfig loads the status page configuration. If none exists, a sensible default is created.
func (s *Store) GetStatusPageConfig(ctx context.Context, id string) (*StatusPageConfig, error) {
	if strings.TrimSpace(id) == "" {
		id = "default"
	}

	var (
		cfgID          string
		title          string
		desc           string
		announcement   string
		showUptimeDays int
		customCSS      string
		componentsRaw  string
		createdAt      int64
		updatedAt      int64
	)

	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, announcement, show_uptime_days, custom_css, components, created_at, updated_at
		FROM status_page_configs WHERE id = ?
	`, id)

	err := row.Scan(&cfgID, &title, &desc, &announcement, &showUptimeDays, &customCSS, &componentsRaw, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Auto-initialize default status page
		now := time.Now().Unix()
		defaultCfg := &StatusPageConfig{
			ID:             id,
			Title:          "ProbeWatch 服务运行状态",
			Description:    "实时监控各节点与分布式服务的运行健康状况与 90 天可用率",
			Announcement:   "",
			ShowUptimeDays: 90,
			CustomCSS:      "",
			Components:     []StatusPageComponent{},
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		// Auto-populate components from existing nodes if any
		nodes, errList := s.ListNodes(ctx)
		if errList == nil && len(nodes) > 0 {
			comps := make([]StatusPageComponent, 0, len(nodes))
			for idx, n := range nodes {
				comps = append(comps, StatusPageComponent{
					ID:          fmt.Sprintf("comp_node_%s", n.ID),
					Name:        n.Name,
					Group:       "基础设施节点",
					NodeID:      n.ID,
					Description: "服务器节点实时监控",
					ShowLatency: true,
					Order:       idx + 1,
				})
			}
			defaultCfg.Components = comps
		}

		data, _ := json.Marshal(defaultCfg.Components)
		_, insertErr := s.db.ExecContext(ctx, `
			INSERT INTO status_page_configs (id, title, description, announcement, show_uptime_days, custom_css, components, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, defaultCfg.ID, defaultCfg.Title, defaultCfg.Description, defaultCfg.Announcement, defaultCfg.ShowUptimeDays, defaultCfg.CustomCSS, string(data), defaultCfg.CreatedAt, defaultCfg.UpdatedAt)
		if insertErr != nil {
			return nil, fmt.Errorf("insert default status page config: %w", insertErr)
		}
		return defaultCfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query status page config: %w", err)
	}

	var comps []StatusPageComponent
	if err := json.Unmarshal([]byte(componentsRaw), &comps); err != nil {
		comps = []StatusPageComponent{}
	}

	return &StatusPageConfig{
		ID:             cfgID,
		Title:          title,
		Description:    desc,
		Announcement:   announcement,
		ShowUptimeDays: showUptimeDays,
		CustomCSS:      customCSS,
		Components:     comps,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}

// UpdateStatusPageConfig persists changes to the status page configuration.
func (s *Store) UpdateStatusPageConfig(ctx context.Context, cfg *StatusPageConfig) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = "default"
	}
	if cfg.ShowUptimeDays <= 0 {
		cfg.ShowUptimeDays = 90
	}
	cfg.UpdatedAt = time.Now().Unix()

	compsData, err := json.Marshal(cfg.Components)
	if err != nil {
		return fmt.Errorf("marshal components: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO status_page_configs (id, title, description, announcement, show_uptime_days, custom_css, components, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			announcement = excluded.announcement,
			show_uptime_days = excluded.show_uptime_days,
			custom_css = excluded.custom_css,
			components = excluded.components,
			updated_at = excluded.updated_at
	`, cfg.ID, cfg.Title, cfg.Description, cfg.Announcement, cfg.ShowUptimeDays, cfg.CustomCSS, string(compsData), cfg.UpdatedAt, cfg.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save status page config: %w", err)
	}
	return nil
}

// CreateIncident creates a new incident or scheduled maintenance with an optional initial update message.
func (s *Store) CreateIncident(ctx context.Context, inc *Incident, initialMessage string) (*Incident, error) {
	if inc == nil {
		return nil, errors.New("nil incident")
	}
	if strings.TrimSpace(inc.Title) == "" {
		return nil, errors.New("incident title is required")
	}
	if strings.TrimSpace(inc.Status) == "" {
		inc.Status = IncidentStatusInvestigating
	}
	if strings.TrimSpace(inc.Impact) == "" {
		inc.Impact = IncidentImpactMinor
	}
	now := time.Now().Unix()
	if inc.ID == "" {
		inc.ID = generateIncidentID()
	}
	inc.CreatedAt = now
	inc.UpdatedAt = now

	isMaint := 0
	if inc.IsMaintenance {
		isMaint = 1
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO incidents (id, title, status, impact, is_maintenance, scheduled_start_at, scheduled_end_at, created_at, resolved_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, inc.ID, inc.Title, inc.Status, inc.Impact, isMaint, inc.ScheduledStartAt, inc.ScheduledEndAt, inc.CreatedAt, inc.ResolvedAt, inc.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert incident: %w", err)
	}

	if strings.TrimSpace(initialMessage) != "" {
		updID := generateUpdateID()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO incident_updates (id, incident_id, status, message, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, updID, inc.ID, inc.Status, initialMessage, now)
		if err != nil {
			return nil, fmt.Errorf("insert initial incident update: %w", err)
		}
		inc.Updates = []IncidentUpdate{
			{
				ID:         updID,
				IncidentID: inc.ID,
				Status:     inc.Status,
				Message:    initialMessage,
				CreatedAt:  now,
			},
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return inc, nil
}

// GetIncident retrieves a single incident by ID including all chronological updates.
func (s *Store) GetIncident(ctx context.Context, id string) (*Incident, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, status, impact, is_maintenance, scheduled_start_at, scheduled_end_at, created_at, resolved_at, updated_at
		FROM incidents WHERE id = ?
	`, id)

	var (
		inc              Incident
		isMaint          int
		schedStart, schedEnd sql.NullInt64
		resolvedAt       sql.NullInt64
	)

	err := row.Scan(&inc.ID, &inc.Title, &inc.Status, &inc.Impact, &isMaint, &schedStart, &schedEnd, &inc.CreatedAt, &resolvedAt, &inc.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIncidentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan incident: %w", err)
	}

	inc.IsMaintenance = isMaint == 1
	if schedStart.Valid {
		inc.ScheduledStartAt = &schedStart.Int64
	}
	if schedEnd.Valid {
		inc.ScheduledEndAt = &schedEnd.Int64
	}
	if resolvedAt.Valid {
		inc.ResolvedAt = &resolvedAt.Int64
	}

	updates, err := s.listIncidentUpdates(ctx, id)
	if err != nil {
		return nil, err
	}
	inc.Updates = updates

	return &inc, nil
}

func (s *Store) listIncidentUpdates(ctx context.Context, incidentID string) ([]IncidentUpdate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, incident_id, status, message, created_at
		FROM incident_updates
		WHERE incident_id = ?
		ORDER BY created_at ASC
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("query updates: %w", err)
	}
	defer rows.Close()

	var updates []IncidentUpdate
	for rows.Next() {
		var u IncidentUpdate
		if err := rows.Scan(&u.ID, &u.IncidentID, &u.Status, &u.Message, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan update: %w", err)
		}
		updates = append(updates, u)
	}
	if updates == nil {
		updates = []IncidentUpdate{}
	}
	return updates, nil
}

// AddIncidentUpdate appends a new message to an existing incident and updates its status.
func (s *Store) AddIncidentUpdate(ctx context.Context, incidentID string, status string, message string) (*IncidentUpdate, error) {
	if strings.TrimSpace(message) == "" {
		return nil, errors.New("update message is required")
	}
	now := time.Now().Unix()
	updID := generateUpdateID()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Verify incident exists
	var curStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM incidents WHERE id = ?`, incidentID).Scan(&curStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIncidentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query incident: %w", err)
	}

	if strings.TrimSpace(status) == "" {
		status = curStatus
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO incident_updates (id, incident_id, status, message, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, updID, incidentID, status, message, now)
	if err != nil {
		return nil, fmt.Errorf("insert update: %w", err)
	}

	var resolvedAt *int64
	if status == IncidentStatusResolved {
		resolvedAt = &now
	}

	if resolvedAt != nil {
		_, err = tx.ExecContext(ctx, `
			UPDATE incidents
			SET status = ?, resolved_at = ?, updated_at = ?
			WHERE id = ?
		`, status, *resolvedAt, now, incidentID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE incidents
			SET status = ?, updated_at = ?
			WHERE id = ?
		`, status, now, incidentID)
	}
	if err != nil {
		return nil, fmt.Errorf("update incident status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &IncidentUpdate{
		ID:         updID,
		IncidentID: incidentID,
		Status:     status,
		Message:    message,
		CreatedAt:  now,
	}, nil
}

// UpdateIncident updates fields of an incident.
func (s *Store) UpdateIncident(ctx context.Context, inc *Incident) error {
	if inc == nil || strings.TrimSpace(inc.ID) == "" {
		return errors.New("invalid incident")
	}
	now := time.Now().Unix()
	inc.UpdatedAt = now

	isMaint := 0
	if inc.IsMaintenance {
		isMaint = 1
	}

	if inc.Status == IncidentStatusResolved && inc.ResolvedAt == nil {
		inc.ResolvedAt = &now
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE incidents
		SET title = ?, status = ?, impact = ?, is_maintenance = ?, scheduled_start_at = ?, scheduled_end_at = ?, resolved_at = ?, updated_at = ?
		WHERE id = ?
	`, inc.Title, inc.Status, inc.Impact, isMaint, inc.ScheduledStartAt, inc.ScheduledEndAt, inc.ResolvedAt, inc.UpdatedAt, inc.ID)
	if err != nil {
		return fmt.Errorf("update incident: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrIncidentNotFound
	}
	return nil
}

// DeleteIncident deletes an incident and all its updates.
func (s *Store) DeleteIncident(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, _ = tx.ExecContext(ctx, `DELETE FROM incident_updates WHERE incident_id = ?`, id)
	res, err := tx.ExecContext(ctx, `DELETE FROM incidents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete incident: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrIncidentNotFound
	}
	return tx.Commit()
}

// ListIncidents lists incidents with pagination and status filtering.
func (s *Store) ListIncidents(ctx context.Context, includeResolved bool, limit int, offset int) ([]Incident, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	var countQuery string
	var listQuery string
	var args []interface{}

	if includeResolved {
		countQuery = `SELECT COUNT(*) FROM incidents`
		listQuery = `
			SELECT id, title, status, impact, is_maintenance, scheduled_start_at, scheduled_end_at, created_at, resolved_at, updated_at
			FROM incidents
			ORDER BY created_at DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{limit, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM incidents WHERE status != 'resolved'`
		listQuery = `
			SELECT id, title, status, impact, is_maintenance, scheduled_start_at, scheduled_end_at, created_at, resolved_at, updated_at
			FROM incidents
			WHERE status != 'resolved'
			ORDER BY created_at DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{limit, offset}
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count incidents: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		var (
			inc        Incident
			isMaint    int
			schedStart sql.NullInt64
			schedEnd   sql.NullInt64
			resolvedAt sql.NullInt64
		)
		if err := rows.Scan(&inc.ID, &inc.Title, &inc.Status, &inc.Impact, &isMaint, &schedStart, &schedEnd, &inc.CreatedAt, &resolvedAt, &inc.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan incident: %w", err)
		}
		inc.IsMaintenance = isMaint == 1
		if schedStart.Valid {
			inc.ScheduledStartAt = &schedStart.Int64
		}
		if schedEnd.Valid {
			inc.ScheduledEndAt = &schedEnd.Int64
		}
		if resolvedAt.Valid {
			inc.ResolvedAt = &resolvedAt.Int64
		}
		incidents = append(incidents, inc)
	}

	// Fetch updates for these incidents
	for i := range incidents {
		upds, err := s.listIncidentUpdates(ctx, incidents[i].ID)
		if err == nil {
			incidents[i].Updates = upds
		}
	}

	if incidents == nil {
		incidents = []Incident{}
	}
	return incidents, total, nil
}

// CalculateComponentSLA computes 90-day daily uptime history and aggregate SLA for a component.
func (s *Store) CalculateComponentSLA(ctx context.Context, nodeID string, days int) ([]DailyUptime, float64, float64, float64, float64, error) {
	if days <= 0 {
		days = 90
	}
	if days > 180 {
		days = 180
	}

	now := time.Now().UTC()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// Pre-populate day buckets from oldest to today
	buckets := make([]DailyUptime, days)
	dayTimes := make([]time.Time, days)
	for i := 0; i < days; i++ {
		t := startOfToday.AddDate(0, 0, -(days - 1 - i))
		dayTimes[i] = t
		buckets[i] = DailyUptime{
			Date:           t.Format("2006-01-02"),
			UptimePct:      100.0,
			Status:         "operational",
			IncidentsCount: 0,
		}
	}

	if strings.TrimSpace(nodeID) == "" {
		// Node-less component: check incidents affecting all or default to operational
		return buckets, 100.0, 100.0, 100.0, 100.0, nil
	}

	// Fetch node creation time and last report
	var nodeCreatedAt int64
	var lastReportAt sql.NullInt64
	row := s.db.QueryRowContext(ctx, `SELECT created_at, last_report_at FROM nodes WHERE id = ?`, nodeID)
	err := row.Scan(&nodeCreatedAt, &lastReportAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Node not found: return neutral no_data
		for i := range buckets {
			buckets[i].Status = "no_data"
			buckets[i].UptimePct = 0
		}
		return buckets, 0, 0, 0, 0, nil
	} else if err != nil {
		return buckets, 100.0, 100.0, 100.0, 100.0, fmt.Errorf("scan node: %w", err)
	}

	createdTime := time.Unix(nodeCreatedAt, 0).UTC()
	createdDay := time.Date(createdTime.Year(), createdTime.Month(), createdTime.Day(), 0, 0, 0, 0, time.UTC)

	// Fetch daily sample metrics if recorded
	minWindow := startOfToday.AddDate(0, 0, -days).Unix()
	dailySamples := make(map[string]int)
	mRows, mErr := s.db.QueryContext(ctx, `
		SELECT window_start, sample_count
		FROM resource_history_daily
		WHERE node_id = ? AND window_start >= ?
	`, nodeID, minWindow)
	if mErr == nil {
		defer mRows.Close()
		for mRows.Next() {
			var wStart int64
			var sCount int
			if err := mRows.Scan(&wStart, &sCount); err == nil {
				dStr := time.Unix(wStart, 0).UTC().Format("2006-01-02")
				dailySamples[dStr] = sCount
			}
		}
	}

	// Check alert events for downtime/offline incidents
	alertCounts := make(map[string]int)
	aRows, aErr := s.db.QueryContext(ctx, `
		SELECT first_seen_at FROM alert_events
		WHERE node_id = ? AND first_seen_at >= ?
	`, nodeID, minWindow)
	if aErr == nil {
		defer aRows.Close()
		for aRows.Next() {
			var seenAt int64
			if err := aRows.Scan(&seenAt); err == nil {
				dStr := time.Unix(seenAt, 0).UTC().Format("2006-01-02")
				alertCounts[dStr]++
			}
		}
	}

	// Evaluate each day
	sum24h, count24h := 0.0, 0
	sum7d, count7d := 0.0, 0
	sum30d, count30d := 0.0, 0
	sum90d, count90d := 0.0, 0

	for i, t := range dayTimes {
		dStr := buckets[i].Date
		alerts := alertCounts[dStr]
		buckets[i].IncidentsCount = alerts

		if t.Before(createdDay) {
			buckets[i].Status = "no_data"
			buckets[i].UptimePct = 100.0
			continue
		}

		sCount, hasSamples := dailySamples[dStr]
		uptime := 100.0
		if alerts > 0 {
			// Deduct roughly 5-15% per alert event
			deduction := float64(alerts) * 8.5
			if deduction > 40.0 {
				deduction = 40.0
			}
			uptime -= deduction
		}

		if hasSamples && sCount < 10 && t.Before(startOfToday) {
			uptime = float64(sCount) / 10.0 * 100.0
			if uptime < 50.0 {
				uptime = 50.0
			}
		}

		if uptime >= 99.5 {
			buckets[i].Status = "operational"
		} else if uptime >= 95.0 {
			buckets[i].Status = "degraded"
		} else {
			buckets[i].Status = "outage"
		}
		buckets[i].UptimePct = uptime

		// Aggregate calculation
		daysAgo := days - 1 - i
		if daysAgo < 1 {
			sum24h += uptime
			count24h++
		}
		if daysAgo < 7 {
			sum7d += uptime
			count7d++
		}
		if daysAgo < 30 {
			sum30d += uptime
			count30d++
		}
		sum90d += uptime
		count90d++
	}

	calcAvg := func(sum float64, count int) float64 {
		if count <= 0 {
			return 100.0
		}
		avg := sum / float64(count)
		if avg > 100.0 {
			return 100.0
		}
		return float64(int(avg*100)) / 100 // truncate to 2 decimal places
	}

	return buckets, calcAvg(sum24h, count24h), calcAvg(sum7d, count7d), calcAvg(sum30d, count30d), calcAvg(sum90d, count90d), nil
}
