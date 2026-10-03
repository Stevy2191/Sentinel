package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Top-N measures.
const (
	TopNTraffic     = "traffic"
	TopNUtilisation = "utilisation"
	TopNErrors      = "errors"
)

// TopNQuery ranks ports of DeviceIDs over [From, To).
type TopNQuery struct {
	DeviceIDs []uuid.UUID
	// InterfaceIDs restricts to these ports (physical ones, say); empty = all.
	InterfaceIDs []uuid.UUID
	Measure      string
	From, To     time.Time
	N            int
}

// TopNRow is one ranked port.
type TopNRow struct {
	DeviceID   uuid.UUID `json:"device_id" gorm:"column:device_id"`
	DeviceName string    `json:"device_name" gorm:"column:device_name"`
	IfIndex    int       `json:"if_index" gorm:"column:if_index"`
	PortName   string    `json:"port_name" gorm:"column:port_name"`
	Alias      string    `json:"alias" gorm:"column:alias"`
	Value      float64   `json:"value" gorm:"column:value"`
}

// topNMeasure is a measure's metrics and how their per-series averages combine.
func topNMeasure(measure string) (metrics []string, combine string, having string, err error) {
	switch measure {
	case TopNTraffic:
		return []string{MetricIfInBps, MetricIfOutBps}, "sum", "", nil
	case TopNUtilisation:
		return []string{MetricIfInUtilPct, MetricIfOutUtilPct}, "max", "", nil
	case TopNErrors:
		return []string{MetricIfInErrorsPM, MetricIfOutErrorsPM}, "sum", " HAVING sum(x.avg) > 0", nil
	}
	return nil, "", "", fmt.Errorf("unknown top-N measure %q", measure)
}

// TopN ranks ports by measure over the window, reading the same source
// PickResolution would (raw up to 6 h, then the 5-minute and hourly rollups).
func (m *MetricsStore) TopN(ctx context.Context, q TopNQuery) ([]TopNRow, error) {
	metrics, combine, having, err := topNMeasure(q.Measure)
	if err != nil {
		return nil, err
	}
	if len(q.DeviceIDs) == 0 || q.N <= 0 {
		return []TopNRow{}, nil
	}
	rawSince := time.Now().UTC().Add(-time.Duration(m.rawRetentionDays(ctx)) * 24 * time.Hour)
	source, _ := PickResolution(q.From, q.To, rawSince)

	var inner string
	switch source {
	case "raw":
		inner = `SELECT s.device_id, s.interface_id, s.metric, avg(x.value) AS avg
			FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND x.time >= ? AND x.time < ?`
	default:
		table := "metrics.samples_5m"
		if source == "1h" {
			table = "metrics.samples_1h"
		}
		inner = `SELECT s.device_id, s.interface_id, s.metric, COALESCE(sum(r.vsum) / NULLIF(sum(r.n), 0), 0) AS avg
			FROM ` + table + ` r JOIN metrics.series s ON s.id = r.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND r.bucket >= ? AND r.bucket < ?`
	}
	args := []any{q.DeviceIDs, metrics, q.From, q.To}
	if len(q.InterfaceIDs) > 0 {
		inner += " AND s.interface_id IN ?"
		args = append(args, q.InterfaceIDs)
	}
	inner += " AND s.interface_id IS NOT NULL GROUP BY s.device_id, s.interface_id, s.metric"

	// The order ends with the ids, so ports that tie never swap places
	// between refreshes. di.id is one per (device, ifIndex): grouping by it
	// changes nothing.
	sql := `SELECT x.device_id, d.name AS device_name, di.if_index, di.name AS port_name, di.alias,
			` + combine + `(x.avg) AS value
		FROM (` + inner + `) x
		JOIN devices d ON d.id = x.device_id
		JOIN device_interfaces di ON di.id = x.interface_id
		GROUP BY x.device_id, di.id, d.name, di.if_index, di.name, di.alias` + having + `
		ORDER BY value DESC, d.name, di.if_index, x.device_id, di.id
		LIMIT ?`
	args = append(args, q.N)

	var rows []TopNRow
	if err := m.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("ranking ports: %w", err)
	}
	if rows == nil {
		rows = []TopNRow{}
	}
	return rows, nil
}
