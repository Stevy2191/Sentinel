package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// Ending a span event must not overwrite its start figures: the tracker uses
// the same keys ("per_minute", "util_pct", "speed_bps") at start and end, so
// a naive merge makes a closed event's start and end figures look identical.
// Review finding: Important 1.
func TestDBRecordPortEventsKeepsStartFiguresOnEnd(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 3, "0/3", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC()

	testdb.Must(t, svc.RecordPortEvents(ctx, []models.PortEvent{
		{DeviceID: s.DeviceID, InterfaceID: port, IfIndex: 3, Kind: models.PortEventErrors, StartedAt: start,
			Detail: models.JSONMap{"per_minute": 50}},
	}, nil))
	testdb.Must(t, svc.RecordPortEvents(ctx, nil, []PortEventEnd{
		{InterfaceID: port, Kind: models.PortEventErrors, At: end, Detail: map[string]any{"per_minute": 2}},
	}))

	var row models.PortEvent
	testdb.Must(t, db.First(&row, "interface_id = ? AND kind = ?", port, models.PortEventErrors).Error)
	if row.EndedAt == nil {
		t.Fatal("ended_at not set")
	}
	if v, ok := row.Detail["per_minute"].(float64); !ok || v != 50 {
		t.Errorf("top-level per_minute changed: %+v", row.Detail)
	}
	endDetail, ok := row.Detail["end"].(map[string]any)
	if !ok {
		t.Fatalf("no nested end detail: %+v", row.Detail)
	}
	if v, ok := endDetail["per_minute"].(float64); !ok || v != 2 {
		t.Errorf("end.per_minute: %+v", endDetail)
	}
}

// Ending a span event with no end detail (e.g. a link going down while
// flapping) sets ended_at but writes no "end" key at all.
func TestDBRecordPortEventsNoEndDetailWritesNoEndKey(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 3, "0/3", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC()

	testdb.Must(t, svc.RecordPortEvents(ctx, []models.PortEvent{
		{DeviceID: s.DeviceID, InterfaceID: port, IfIndex: 3, Kind: models.PortEventFlapping, StartedAt: start,
			Detail: models.JSONMap{"transitions": 4}},
	}, nil))
	testdb.Must(t, svc.RecordPortEvents(ctx, nil, []PortEventEnd{
		{InterfaceID: port, Kind: models.PortEventFlapping, At: end, Detail: nil},
	}))

	var row models.PortEvent
	testdb.Must(t, db.First(&row, "interface_id = ? AND kind = ?", port, models.PortEventFlapping).Error)
	if row.EndedAt == nil {
		t.Fatal("ended_at not set")
	}
	if _, ok := row.Detail["end"]; ok {
		t.Errorf("end key written despite no end detail: %+v", row.Detail)
	}
	if v, ok := row.Detail["transitions"].(float64); !ok || v != 4 {
		t.Errorf("top-level transitions changed: %+v", row.Detail)
	}
}
