package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

type NodeBillingSettings struct {
	NodeID             string    `json:"node_id"`
	ResetDay           int       `json:"reset_day"`
	ResetTime          string    `json:"reset_time"`
	TrafficQuotaBytes  uint64    `json:"traffic_quota_bytes"`
	AccountingMethod   string    `json:"accounting_method"` // 'total', 'tx', 'rx', 'max', 'min'
	IncludedInterfaces string    `json:"included_interfaces"`
	LastResetAt        time.Time `json:"last_reset_at"`
	LastResetRxBytes   uint64    `json:"last_reset_rx_bytes"`
	LastResetTxBytes   uint64    `json:"last_reset_tx_bytes"`
	BonusQuotaBytes    uint64    `json:"bonus_quota_bytes"`
	Merchant           string    `json:"merchant"`
	Price              float64   `json:"price"`
	Currency           string    `json:"currency"`
	Cycle              string    `json:"cycle"`
	StartDate          string    `json:"start_date"`
	DueDate            string    `json:"due_date"`
	AutoRenew          bool      `json:"auto_renew"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CycleTrafficInfo struct {
	NodeID             string    `json:"node_id"`
	ResetDay           int       `json:"reset_day"`
	ResetTime          string    `json:"reset_time"`
	AccountingMethod   string    `json:"accounting_method"`
	IncludedInterfaces string    `json:"included_interfaces"`
	LastResetAt        time.Time `json:"last_reset_at"`
	NextResetAt        time.Time `json:"next_reset_at"`
	DaysUntilReset     int       `json:"days_until_reset"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	RawRxBytes         uint64    `json:"raw_rx_bytes"`
	RawTxBytes         uint64    `json:"raw_tx_bytes"`
	CycleRxBytes       uint64    `json:"cycle_rx_bytes"`
	CycleTxBytes       uint64    `json:"cycle_tx_bytes"`
	CycleUsedBytes     uint64    `json:"cycle_used_bytes"`
	TrafficQuotaBytes  uint64    `json:"traffic_quota_bytes"`
	BonusQuotaBytes    uint64    `json:"bonus_quota_bytes"`
	TotalQuotaBytes    uint64    `json:"total_quota_bytes"`
	UsedPercent        float64   `json:"used_percent"`
	Merchant           string    `json:"merchant"`
	Price              float64   `json:"price"`
	Currency           string    `json:"currency"`
	Cycle              string    `json:"cycle"`
	StartDate          string    `json:"start_date"`
	DueDate            string    `json:"due_date"`
	AutoRenew          bool      `json:"auto_renew"`
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func clampDay(year int, month time.Month, day int) int {
	maxDay := daysInMonth(year, month)
	if day > maxDay {
		return maxDay
	}
	if day < 1 {
		return 1
	}
	return day
}

func ComputeBillingCycle(now time.Time, resetDay int) (start, end, next time.Time, daysUntil int) {
	if resetDay < 1 {
		resetDay = 1
	}
	if resetDay > 31 {
		resetDay = 31
	}

	year, month, day := now.Year(), now.Month(), now.Day()
	if day >= resetDay {
		startDay := clampDay(year, month, resetDay)
		start = time.Date(year, month, startDay, 0, 0, 0, 0, time.UTC)

		nextMonth := month + 1
		nextYear := year
		if nextMonth > 12 {
			nextMonth = 1
			nextYear++
		}
		endDay := clampDay(nextYear, nextMonth, resetDay)
		end = time.Date(nextYear, nextMonth, endDay, 0, 0, 0, 0, time.UTC)
		next = end
	} else {
		prevMonth := month - 1
		prevYear := year
		if prevMonth < 1 {
			prevMonth = 12
			prevYear--
		}
		startDay := clampDay(prevYear, prevMonth, resetDay)
		start = time.Date(prevYear, prevMonth, startDay, 0, 0, 0, 0, time.UTC)

		endDay := clampDay(year, month, resetDay)
		end = time.Date(year, month, endDay, 0, 0, 0, 0, time.UTC)
		next = end
	}

	diff := next.Sub(now)
	if diff <= 0 {
		daysUntil = 0
	} else {
		daysUntil = int(math.Ceil(diff.Hours() / 24))
	}
	return start, end, next, daysUntil
}

func (s *Store) GetNodeBillingSettings(ctx context.Context, nodeID string) (NodeBillingSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		b                   NodeBillingSettings
		lastResetAt         int64
		lastResetRx, lastTx int64
		quotaBytes, bonus   int64
		autoRenewInt        int
		updatedAt           int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT node_id, reset_day, reset_time, traffic_quota_bytes, accounting_method, included_interfaces,
		       last_reset_at, last_reset_rx_bytes, last_reset_tx_bytes, bonus_quota_bytes,
		       merchant, price, currency, cycle, start_date, due_date, auto_renew, updated_at
		FROM node_billing_settings
		WHERE node_id = ?`, nodeID).Scan(
		&b.NodeID, &b.ResetDay, &b.ResetTime, &quotaBytes, &b.AccountingMethod, &b.IncludedInterfaces,
		&lastResetAt, &lastResetRx, &lastTx, &bonus,
		&b.Merchant, &b.Price, &b.Currency, &b.Cycle, &b.StartDate, &b.DueDate, &autoRenewInt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// Default settings for unconfigured node
		return NodeBillingSettings{
			NodeID:             nodeID,
			ResetDay:           1,
			ResetTime:          "00:00:00",
			TrafficQuotaBytes:  1000 * 1024 * 1024 * 1024, // 1TB default
			AccountingMethod:   "total",
			IncludedInterfaces: "*",
			Currency:           "CNY",
			Cycle:              "month",
			AutoRenew:          true,
		}, nil
	}
	if err != nil {
		return NodeBillingSettings{}, fmt.Errorf("get node billing settings: %w", err)
	}

	b.TrafficQuotaBytes = uint64(quotaBytes)
	b.BonusQuotaBytes = uint64(bonus)
	b.LastResetRxBytes = uint64(lastResetRx)
	b.LastResetTxBytes = uint64(lastTx)
	if lastResetAt > 0 {
		b.LastResetAt = time.Unix(0, lastResetAt).UTC()
	}
	b.AutoRenew = autoRenewInt == 1
	if updatedAt > 0 {
		b.UpdatedAt = time.Unix(0, updatedAt).UTC()
	}
	return b, nil
}

func (s *Store) UpsertNodeBillingSettings(ctx context.Context, b NodeBillingSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if b.ResetDay < 1 || b.ResetDay > 31 {
		b.ResetDay = 1
	}
	if b.ResetTime == "" {
		b.ResetTime = "00:00:00"
	}
	method := strings.ToLower(strings.TrimSpace(b.AccountingMethod))
	if method != "tx" && method != "rx" && method != "max" && method != "min" {
		method = "total"
	}
	b.AccountingMethod = method
	if b.IncludedInterfaces == "" {
		b.IncludedInterfaces = "*"
	}
	if b.Currency == "" {
		b.Currency = "CNY"
	}
	if b.Cycle == "" {
		b.Cycle = "month"
	}

	autoRenewInt := 0
	if b.AutoRenew {
		autoRenewInt = 1
	}
	now := time.Now().UTC()
	lastResetAt := int64(0)
	if !b.LastResetAt.IsZero() {
		lastResetAt = unixNano(b.LastResetAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO node_billing_settings (
			node_id, reset_day, reset_time, traffic_quota_bytes, accounting_method, included_interfaces,
			last_reset_at, last_reset_rx_bytes, last_reset_tx_bytes, bonus_quota_bytes,
			merchant, price, currency, cycle, start_date, due_date, auto_renew, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			reset_day = excluded.reset_day,
			reset_time = excluded.reset_time,
			traffic_quota_bytes = excluded.traffic_quota_bytes,
			accounting_method = excluded.accounting_method,
			included_interfaces = excluded.included_interfaces,
			bonus_quota_bytes = excluded.bonus_quota_bytes,
			merchant = excluded.merchant,
			price = excluded.price,
			currency = excluded.currency,
			cycle = excluded.cycle,
			start_date = excluded.start_date,
			due_date = excluded.due_date,
			auto_renew = excluded.auto_renew,
			updated_at = excluded.updated_at
	`, b.NodeID, b.ResetDay, b.ResetTime, int64(b.TrafficQuotaBytes), b.AccountingMethod, b.IncludedInterfaces,
		lastResetAt, int64(b.LastResetRxBytes), int64(b.LastResetTxBytes), int64(b.BonusQuotaBytes),
		b.Merchant, b.Price, b.Currency, b.Cycle, b.StartDate, b.DueDate, autoRenewInt, unixNano(now))
	if err != nil {
		return fmt.Errorf("upsert node billing settings: %w", err)
	}
	return nil
}

func extractRawTraffic(payload []byte, includedInterfaces string) (rx, tx uint64) {
	if len(payload) == 0 {
		return 0, 0
	}
	var snapshot protocol.ResourceSnapshot
	if json.Unmarshal(payload, &snapshot) != nil {
		return 0, 0
	}
	if includedInterfaces == "" || includedInterfaces == "*" {
		return snapshot.NetworkRxBytes, snapshot.NetworkTxBytes
	}
	allowed := make(map[string]bool)
	for _, part := range strings.Split(includedInterfaces, ",") {
		clean := strings.TrimSpace(part)
		if clean != "" {
			allowed[clean] = true
		}
	}
	for _, iface := range snapshot.Interfaces {
		if allowed[iface.Name] {
			rx += iface.RxBytes
			tx += iface.TxBytes
		}
	}
	if rx == 0 && tx == 0 {
		return snapshot.NetworkRxBytes, snapshot.NetworkTxBytes
	}
	return rx, tx
}

func (s *Store) ResetNodeBillingCycle(ctx context.Context, nodeID string, now time.Time) error {
	settings, err := s.GetNodeBillingSettings(ctx, nodeID)
	if err != nil {
		return err
	}

	_, payload, err := s.GetResourceLatest(ctx, nodeID)
	var rawRx, rawTx uint64
	if err == nil {
		rawRx, rawTx = extractRawTraffic(payload, settings.IncludedInterfaces)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.ExecContext(ctx, `
		UPDATE node_billing_settings
		SET last_reset_at = ?, last_reset_rx_bytes = ?, last_reset_tx_bytes = ?, updated_at = ?
		WHERE node_id = ?
	`, unixNano(now), int64(rawRx), int64(rawTx), unixNano(now), nodeID)
	if err != nil {
		return fmt.Errorf("reset node billing cycle: %w", err)
	}
	return nil
}

func (s *Store) CheckAndAutoResetBillingCycles(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, reset_day, last_reset_at
		FROM node_billing_settings
	`)
	if err != nil {
		return nil, fmt.Errorf("query billing auto-reset: %w", err)
	}
	defer rows.Close()

	type toReset struct {
		nodeID   string
		resetDay int
		lastAt   time.Time
	}
	var targets []toReset
	for rows.Next() {
		var (
			id       string
			resetDay int
			lastAt   int64
		)
		if err := rows.Scan(&id, &resetDay, &lastAt); err != nil {
			continue
		}
		var t time.Time
		if lastAt > 0 {
			t = time.Unix(0, lastAt).UTC()
		}
		targets = append(targets, toReset{nodeID: id, resetDay: resetDay, lastAt: t})
	}
	_ = rows.Close()

	var resetIDs []string
	for _, item := range targets {
		start, _, _, _ := ComputeBillingCycle(now, item.resetDay)
		// If last reset was before current cycle's start, it needs reset!
		if item.lastAt.Before(start) {
			if err := s.ResetNodeBillingCycle(ctx, item.nodeID, start); err == nil {
				resetIDs = append(resetIDs, item.nodeID)
			}
		}
	}
	return resetIDs, nil
}

func (s *Store) GetNodeCycleTraffic(ctx context.Context, nodeID string, now time.Time) (CycleTrafficInfo, error) {
	settings, err := s.GetNodeBillingSettings(ctx, nodeID)
	if err != nil {
		return CycleTrafficInfo{}, err
	}

	periodStart, periodEnd, nextReset, daysUntil := ComputeBillingCycle(now, settings.ResetDay)

	// Auto-reset check
	if settings.LastResetAt.Before(periodStart) {
		_ = s.ResetNodeBillingCycle(ctx, nodeID, periodStart)
		settings, _ = s.GetNodeBillingSettings(ctx, nodeID)
	}

	// Read latest raw traffic counters
	var rawRx, rawTx uint64
	_, payload, err := s.GetResourceLatest(ctx, nodeID)
	if err == nil {
		rawRx, rawTx = extractRawTraffic(payload, settings.IncludedInterfaces)
	}

	// Compute cycle increments with rollover detection
	var cycleRx, cycleTx uint64
	if rawRx >= settings.LastResetRxBytes {
		cycleRx = rawRx - settings.LastResetRxBytes
	} else {
		// Counter reset or reboot
		cycleRx = rawRx
	}

	if rawTx >= settings.LastResetTxBytes {
		cycleTx = rawTx - settings.LastResetTxBytes
	} else {
		cycleTx = rawTx
	}

	var cycleUsed uint64
	switch settings.AccountingMethod {
	case "tx":
		cycleUsed = cycleTx
	case "rx":
		cycleUsed = cycleRx
	case "max":
		if cycleTx > cycleRx {
			cycleUsed = cycleTx
		} else {
			cycleUsed = cycleRx
		}
	case "min":
		if cycleTx < cycleRx {
			cycleUsed = cycleTx
		} else {
			cycleUsed = cycleRx
		}
	default:
		cycleUsed = cycleRx + cycleTx
	}

	totalQuota := settings.TrafficQuotaBytes + settings.BonusQuotaBytes
	usedPercent := 0.0
	if totalQuota > 0 {
		usedPercent = float64(cycleUsed) / float64(totalQuota) * 100.0
		if usedPercent > 100.0 {
			usedPercent = 100.0
		}
	}

	return CycleTrafficInfo{
		NodeID:             nodeID,
		ResetDay:           settings.ResetDay,
		ResetTime:          settings.ResetTime,
		AccountingMethod:   settings.AccountingMethod,
		IncludedInterfaces: settings.IncludedInterfaces,
		LastResetAt:        settings.LastResetAt,
		NextResetAt:        nextReset,
		DaysUntilReset:     daysUntil,
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		RawRxBytes:         rawRx,
		RawTxBytes:         rawTx,
		CycleRxBytes:       cycleRx,
		CycleTxBytes:       cycleTx,
		CycleUsedBytes:     cycleUsed,
		TrafficQuotaBytes:  settings.TrafficQuotaBytes,
		BonusQuotaBytes:    settings.BonusQuotaBytes,
		TotalQuotaBytes:    totalQuota,
		UsedPercent:        usedPercent,
		Merchant:           settings.Merchant,
		Price:              settings.Price,
		Currency:           settings.Currency,
		Cycle:              settings.Cycle,
		StartDate:          settings.StartDate,
		DueDate:            settings.DueDate,
		AutoRenew:          settings.AutoRenew,
	}, nil
}
