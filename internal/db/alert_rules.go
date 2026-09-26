package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrAlertRuleNotFound = errors.New("alert rule not found")
	ErrInvalidAlertRule  = errors.New("invalid alert rule")
)

// AlertCondition defines an atomic threshold condition within a composite rule.
type AlertCondition struct {
	Metric    string  `json:"metric"`
	Operator  string  `json:"operator"` // >, >=, <, <=, ==
	Threshold float64 `json:"threshold"`
}

// AlertRule represents a custom metric threshold or composite condition for alerting.
type AlertRule struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Metric           string           `json:"metric"` // For simple rules, primary metric
	Operator         string           `json:"operator"` // >, >=, <, <=, ==
	Threshold        float64          `json:"threshold"`
	DurationSeconds  int              `json:"duration_seconds"`
	Severity         string           `json:"severity"` // critical, warning, info
	NodeFilter       string           `json:"node_filter"` // * or comma-separated node IDs
	Enabled          bool             `json:"enabled"`
	ExpressionType   string           `json:"expression_type"` // "simple" | "composite"
	Conditions       []AlertCondition `json:"conditions"`
	Logic            string           `json:"logic"` // "AND" | "OR"
	ConsecutiveCount int              `json:"consecutive_count"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

var consecutiveTracker = struct {
	sync.Mutex
	counts map[string]int
}{
	counts: make(map[string]int),
}

// ResetConsecutiveTracker clears the consecutive evaluation counts.
func ResetConsecutiveTracker() {
	consecutiveTracker.Lock()
	defer consecutiveTracker.Unlock()
	consecutiveTracker.counts = make(map[string]int)
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, metric, operator, threshold, duration_seconds, severity, node_filter, enabled,
		       expression_type, conditions, logic, consecutive_count, created_at, updated_at
		FROM alert_rules
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()

	var rules []AlertRule
	for rows.Next() {
		var (
			r             AlertRule
			enabled       int
			created       int64
			updated       int64
			conditionsRaw string
		)
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Metric, &r.Operator, &r.Threshold, &r.DurationSeconds,
			&r.Severity, &r.NodeFilter, &enabled,
			&r.ExpressionType, &conditionsRaw, &r.Logic, &r.ConsecutiveCount,
			&created, &updated,
		); err != nil {
			return nil, fmt.Errorf("scan alert rule: %w", err)
		}
		r.Enabled = enabled == 1
		r.CreatedAt = time.Unix(0, created).UTC()
		r.UpdatedAt = time.Unix(0, updated).UTC()
		if r.ExpressionType == "" {
			r.ExpressionType = "simple"
		}
		if r.Logic == "" {
			r.Logic = "AND"
		}
		if r.ConsecutiveCount <= 0 {
			r.ConsecutiveCount = 1
		}
		if conditionsRaw != "" && conditionsRaw != "[]" {
			_ = json.Unmarshal([]byte(conditionsRaw), &r.Conditions)
		}
		if r.Conditions == nil {
			r.Conditions = []AlertCondition{}
		}
		rules = append(rules, r)
	}
	if rules == nil {
		rules = []AlertRule{}
	}
	return rules, rows.Err()
}

func (s *Store) GetAlertRule(ctx context.Context, id string) (AlertRule, error) {
	var (
		r             AlertRule
		enabled       int
		created       int64
		updated       int64
		conditionsRaw string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, metric, operator, threshold, duration_seconds, severity, node_filter, enabled,
		       expression_type, conditions, logic, consecutive_count, created_at, updated_at
		FROM alert_rules
		WHERE id = ?
	`, id).Scan(
		&r.ID, &r.Name, &r.Metric, &r.Operator, &r.Threshold, &r.DurationSeconds,
		&r.Severity, &r.NodeFilter, &enabled,
		&r.ExpressionType, &conditionsRaw, &r.Logic, &r.ConsecutiveCount,
		&created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AlertRule{}, ErrAlertRuleNotFound
	}
	if err != nil {
		return AlertRule{}, fmt.Errorf("get alert rule: %w", err)
	}
	r.Enabled = enabled == 1
	r.CreatedAt = time.Unix(0, created).UTC()
	r.UpdatedAt = time.Unix(0, updated).UTC()
	if r.ExpressionType == "" {
		r.ExpressionType = "simple"
	}
	if r.Logic == "" {
		r.Logic = "AND"
	}
	if r.ConsecutiveCount <= 0 {
		r.ConsecutiveCount = 1
	}
	if conditionsRaw != "" && conditionsRaw != "[]" {
		_ = json.Unmarshal([]byte(conditionsRaw), &r.Conditions)
	}
	if r.Conditions == nil {
		r.Conditions = []AlertCondition{}
	}
	return r, nil
}

func (s *Store) CreateAlertRule(ctx context.Context, rule AlertRule) error {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Metric = strings.ToLower(strings.TrimSpace(rule.Metric))
	rule.Operator = strings.TrimSpace(rule.Operator)
	if rule.Operator == "" {
		rule.Operator = ">"
	}
	rule.Severity = strings.ToLower(strings.TrimSpace(rule.Severity))
	if rule.Severity == "" {
		rule.Severity = "warning"
	}
	rule.NodeFilter = strings.TrimSpace(rule.NodeFilter)
	if rule.NodeFilter == "" {
		rule.NodeFilter = "*"
	}
	if rule.ExpressionType == "" {
		if len(rule.Conditions) > 0 {
			rule.ExpressionType = "composite"
		} else {
			rule.ExpressionType = "simple"
		}
	}
	if rule.Logic == "" {
		rule.Logic = "AND"
	} else {
		rule.Logic = strings.ToUpper(strings.TrimSpace(rule.Logic))
	}
	if rule.ConsecutiveCount <= 0 {
		rule.ConsecutiveCount = 1
	}

	if rule.ID == "" || rule.Name == "" {
		return ErrInvalidAlertRule
	}

	if rule.ExpressionType == "composite" {
		if len(rule.Conditions) == 0 {
			return ErrInvalidAlertRule
		}
		if rule.Metric == "" && len(rule.Conditions) > 0 {
			rule.Metric = rule.Conditions[0].Metric
		}
	} else {
		if rule.Metric == "" {
			return ErrInvalidAlertRule
		}
	}

	condBytes, _ := json.Marshal(rule.Conditions)
	now := time.Now().UTC().UnixNano()
	enabledInt := 0
	if rule.Enabled {
		enabledInt = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO alert_rules (
			id, name, metric, operator, threshold, duration_seconds, severity, node_filter, enabled,
			expression_type, conditions, logic, consecutive_count, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rule.ID, rule.Name, rule.Metric, rule.Operator, rule.Threshold, rule.DurationSeconds,
		rule.Severity, rule.NodeFilter, enabledInt,
		rule.ExpressionType, string(condBytes), rule.Logic, rule.ConsecutiveCount, now, now)
	if err != nil {
		return fmt.Errorf("create alert rule: %w", err)
	}
	return nil
}

func (s *Store) UpdateAlertRule(ctx context.Context, rule AlertRule) error {
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Metric = strings.ToLower(strings.TrimSpace(rule.Metric))
	rule.Operator = strings.TrimSpace(rule.Operator)
	if rule.Operator == "" {
		rule.Operator = ">"
	}
	rule.Severity = strings.ToLower(strings.TrimSpace(rule.Severity))
	if rule.Severity == "" {
		rule.Severity = "warning"
	}
	rule.NodeFilter = strings.TrimSpace(rule.NodeFilter)
	if rule.NodeFilter == "" {
		rule.NodeFilter = "*"
	}
	if rule.ExpressionType == "" {
		if len(rule.Conditions) > 0 {
			rule.ExpressionType = "composite"
		} else {
			rule.ExpressionType = "simple"
		}
	}
	if rule.Logic == "" {
		rule.Logic = "AND"
	} else {
		rule.Logic = strings.ToUpper(strings.TrimSpace(rule.Logic))
	}
	if rule.ConsecutiveCount <= 0 {
		rule.ConsecutiveCount = 1
	}

	if rule.ID == "" || rule.Name == "" {
		return ErrInvalidAlertRule
	}

	if rule.ExpressionType == "composite" {
		if len(rule.Conditions) == 0 {
			return ErrInvalidAlertRule
		}
		if rule.Metric == "" && len(rule.Conditions) > 0 {
			rule.Metric = rule.Conditions[0].Metric
		}
	} else {
		if rule.Metric == "" {
			return ErrInvalidAlertRule
		}
	}

	condBytes, _ := json.Marshal(rule.Conditions)
	now := time.Now().UTC().UnixNano()
	enabledInt := 0
	if rule.Enabled {
		enabledInt = 1
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE alert_rules
		SET name = ?, metric = ?, operator = ?, threshold = ?, duration_seconds = ?, severity = ?, node_filter = ?, enabled = ?,
		    expression_type = ?, conditions = ?, logic = ?, consecutive_count = ?, updated_at = ?
		WHERE id = ?
	`, rule.Name, rule.Metric, rule.Operator, rule.Threshold, rule.DurationSeconds, rule.Severity, rule.NodeFilter, enabledInt,
		rule.ExpressionType, string(condBytes), rule.Logic, rule.ConsecutiveCount, now, rule.ID)
	if err != nil {
		return fmt.Errorf("update alert rule: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}

func (s *Store) DeleteAlertRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}

func (s *Store) ToggleAlertRule(ctx context.Context, id string) (bool, error) {
	rule, err := s.GetAlertRule(ctx, id)
	if err != nil {
		return false, err
	}
	newState := !rule.Enabled
	rule.Enabled = newState
	if err := s.UpdateAlertRule(ctx, rule); err != nil {
		return false, err
	}
	return newState, nil
}

func isRuleApplicableToNode(nodeFilter, nodeID string) bool {
	if nodeFilter == "" || nodeFilter == "*" {
		return true
	}
	for _, p := range strings.Split(nodeFilter, ",") {
		clean := strings.TrimSpace(p)
		if clean == nodeID || clean == "*" {
			return true
		}
	}
	return false
}

func checkThreshold(val float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return val > threshold
	case ">=":
		return val >= threshold
	case "<":
		return val < threshold
	case "<=":
		return val <= threshold
	case "==":
		return val == threshold
	default:
		return val > threshold
	}
}

// EvaluateResourceMetricsWithRules checks the node's live metrics against configured alert rules,
// evaluating simple and composite conditions along with consecutive evaluation breach counts.
func (s *Store) EvaluateResourceMetricsWithRules(ctx context.Context, tx *sql.Tx, nodeID string, metrics map[string]float64, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, metric, operator, threshold, severity, node_filter, expression_type, conditions, logic, consecutive_count
		FROM alert_rules
		WHERE enabled = 1
	`)
	if err != nil {
		return fmt.Errorf("query active alert rules: %w", err)
	}
	defer rows.Close()

	type activeRule struct {
		id, name, metric, op, severity, filter string
		threshold                             float64
		expressionType                        string
		conditionsRaw                         string
		logic                                 string
		consecutiveCount                      int
	}
	var rules []activeRule
	for rows.Next() {
		var r activeRule
		if err := rows.Scan(
			&r.id, &r.name, &r.metric, &r.op, &r.threshold, &r.severity, &r.filter,
			&r.expressionType, &r.conditionsRaw, &r.logic, &r.consecutiveCount,
		); err != nil {
			continue
		}
		rules = append(rules, r)
	}

	matchedMetrics := make(map[string]bool)

	for _, rule := range rules {
		if !isRuleApplicableToNode(rule.filter, nodeID) {
			continue
		}

		var breached bool

		if strings.EqualFold(rule.expressionType, "composite") {
			var conditions []AlertCondition
			if rule.conditionsRaw != "" && rule.conditionsRaw != "[]" {
				_ = json.Unmarshal([]byte(rule.conditionsRaw), &conditions)
			}
			if len(conditions) == 0 {
				continue
			}

			allMet := true
			anyMet := false
			atLeastOnePresent := false

			for _, cond := range conditions {
				matchedMetrics[cond.Metric] = true
				val, exists := metrics[cond.Metric]
				if !exists {
					allMet = false
					continue
				}
				atLeastOnePresent = true
				met := checkThreshold(val, cond.Operator, cond.Threshold)
				if met {
					anyMet = true
				} else {
					allMet = false
				}
			}

			if !atLeastOnePresent {
				continue
			}

			if strings.EqualFold(rule.logic, "OR") {
				breached = anyMet
			} else {
				breached = allMet
			}
		} else {
			val, exists := metrics[rule.metric]
			if !exists {
				continue
			}
			matchedMetrics[rule.metric] = true
			breached = checkThreshold(val, rule.op, rule.threshold)
		}

		// Consecutive breach check
		trackerKey := fmt.Sprintf("%s:%s", nodeID, rule.id)
		consecutiveTracker.Lock()
		var failing bool
		if breached {
			consecutiveTracker.counts[trackerKey]++
			req := rule.consecutiveCount
			if req <= 0 {
				req = 1
			}
			failing = consecutiveTracker.counts[trackerKey] >= req
		} else {
			consecutiveTracker.counts[trackerKey] = 0
			failing = false
		}
		consecutiveTracker.Unlock()

		targetID := rule.metric
		if targetID == "" {
			targetID = "composite"
		}
		reason := fmt.Sprintf("%s_%s_%.1f", rule.metric, rule.op, rule.threshold)
		if strings.EqualFold(rule.expressionType, "composite") {
			reason = fmt.Sprintf("composite_%s_%s", rule.id, strings.ToLower(rule.logic))
		}

		eval := AlertEvaluation{
			Category:             "resource",
			TargetID:             targetID,
			Reason:               reason,
			Severity:             rule.severity,
			Failing:              failing,
			FingerprintDimension: rule.id,
		}
		if err := evaluateAlertTx(ctx, tx, nodeID, eval, now); err != nil {
			return err
		}
	}

	// For metrics that had no matching custom rules, retain safe built-in fallback checks
	if !matchedMetrics["cpu"] {
		if cpu, ok := metrics["cpu"]; ok {
			_ = evaluateAlertTx(ctx, tx, nodeID, AlertEvaluation{
				Category: "resource",
				TargetID: "cpu",
				Reason:   "cpu_high",
				Severity: AlertSeverityCrit,
				Failing:  cpu > 90.0,
			}, now)
		}
	}
	if !matchedMetrics["memory"] {
		if mem, ok := metrics["memory"]; ok {
			_ = evaluateAlertTx(ctx, tx, nodeID, AlertEvaluation{
				Category: "resource",
				TargetID: "memory",
				Reason:   "memory_high",
				Severity: AlertSeverityCrit,
				Failing:  mem > 90.0,
			}, now)
		}
	}
	if !matchedMetrics["disk"] {
		if disk, ok := metrics["disk"]; ok {
			_ = evaluateAlertTx(ctx, tx, nodeID, AlertEvaluation{
				Category: "resource",
				TargetID: "filesystem",
				Reason:   "filesystem_high",
				Severity: AlertSeverityCrit,
				Failing:  disk > 90.0,
			}, now)
		}
	}

	return nil
}
