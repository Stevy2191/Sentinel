package services

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBTopN(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	now := time.Now().UTC()
	ports := map[int]uuid.UUID{}
	for ifIndex, bps := range map[int]float64{1: 100, 2: 5000, 3: 900} {
		ports[ifIndex] = seedPort(t, db, s.DeviceID, ifIndex, "port"+strconv.Itoa(ifIndex), "")
		id := ports[ifIndex]
		for _, ago := range []time.Duration{40 * time.Minute, 20 * time.Minute} {
			testdb.Must(t, m.Write(ctx, s.DeviceID, now.Add(-ago), []SamplePoint{
				{Metric: MetricIfInBps, Instance: strconv.Itoa(ifIndex), InterfaceID: &id, Value: bps},
				{Metric: MetricIfOutBps, Instance: strconv.Itoa(ifIndex), InterfaceID: &id, Value: bps / 2},
				{Metric: MetricIfInErrorsPM, Instance: strconv.Itoa(ifIndex), InterfaceID: &id, Value: map[int]float64{1: 0, 2: 0, 3: 4}[ifIndex]},
			}))
		}
	}
	refreshRollups(t, db)

	for _, span := range []time.Duration{time.Hour, 24 * time.Hour} { // raw, then the 5-minute rollup
		rows, err := m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Measure: TopNTraffic, From: now.Add(-span), To: now, N: 2})
		testdb.Must(t, err)
		if len(rows) != 2 || rows[0].IfIndex != 2 || rows[1].IfIndex != 3 || rows[0].Value != 7500 {
			t.Errorf("span %v: traffic top 2 = %+v, want port 2 (7500 bps) then port 3", span, rows)
			continue
		}
		if rows[0].DeviceName != "dev-10.0.0.2" || rows[0].PortName != "port2" {
			t.Errorf("span %v: row = %+v, want device and port names joined in", span, rows[0])
		}
	}
	rows, err := m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Measure: TopNErrors, From: now.Add(-time.Hour), To: now, N: 10})
	testdb.Must(t, err)
	if len(rows) != 1 || rows[0].IfIndex != 3 {
		t.Errorf("errors = %+v, want only port 3 (ports with no errors are left out)", rows)
	}
	rows, err = m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, InterfaceIDs: []uuid.UUID{ports[1]}, Measure: TopNTraffic, From: now.Add(-time.Hour), To: now, N: 10})
	testdb.Must(t, err)
	if len(rows) != 1 || rows[0].IfIndex != 1 {
		t.Errorf("restricted to port 1 = %+v", rows)
	}
	if _, err := m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Measure: "nope", From: now.Add(-time.Hour), To: now, N: 5}); err == nil {
		t.Error("an unknown measure was accepted")
	}
}
