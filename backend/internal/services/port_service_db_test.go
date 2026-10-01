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
	testdb.Exec(t, db, `UPDATE device_interfaces SET collect = true WHERE id = ?`, port)
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
	testdb.Exec(t, db, `UPDATE device_interfaces SET collect = true WHERE id = ?`, port)
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

// I1: SavePortState must not touch a port that is no longer collected — a
// poll that loaded its row just before UpdatePort turned collection off for
// it must not write stale live state or conditions back.
func TestDBSavePortStateSkipsUncollectedPort(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 3, "0/3", "") // collect NULL, collect_default false: uncollected
	testdb.Exec(t, db, `UPDATE device_interfaces SET oper_status = 'up', admin_status = 'up', speed_bps = 1000000000 WHERE id = ?`, port)
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	ctx := context.Background()

	testdb.Must(t, svc.SavePortState(ctx, port, PortStateUpdate{
		OperStatus: "down", AdminStatus: "down", SpeedBps: 0, LastChangeSeconds: 10,
		Conditions: []string{"link_down"}, ConditionsSince: map[string]time.Time{"link_down": time.Now().UTC()},
	}))

	var row models.DeviceInterface
	testdb.Must(t, db.First(&row, "id = ?", port).Error)
	if row.OperStatus != "up" || row.AdminStatus != "up" || row.SpeedBps != 1000000000 || len(row.Conditions) != 0 {
		t.Errorf("SavePortState changed an uncollected port: %+v", row)
	}
}

// I1: RecordPortEvents must not record a new span-start event for a port
// that is no longer collected (the same race as SavePortState above).
func TestDBRecordPortEventsSkipsUncollectedPort(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 3, "0/3", "") // collect NULL, collect_default false: uncollected
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	ctx := context.Background()

	testdb.Must(t, svc.RecordPortEvents(ctx, []models.PortEvent{
		{DeviceID: s.DeviceID, InterfaceID: port, IfIndex: 3, Kind: models.PortEventErrors, StartedAt: time.Now().UTC(),
			Detail: models.JSONMap{"per_minute": 50}},
	}, nil))

	var n int64
	testdb.Must(t, db.Model(&models.PortEvent{}).Where("interface_id = ?", port).Count(&n).Error)
	if n != 0 {
		t.Errorf("event recorded for an uncollected port: %d rows", n)
	}
}
