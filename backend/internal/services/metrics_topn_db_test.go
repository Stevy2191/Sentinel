package services

import (
	"context"
	"slices"
	"strconv"
	"strings"
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

// Ports that tie on value, device name and ifIndex keep one order from one
// refresh to the next: the tie-break ends with the device and interface ids.
func TestDBTopNTieBreakIsTotal(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	first := seedDevice(t, db, "HQ", "10.0.0.2")
	devices := []uuid.UUID{first.DeviceID}
	for i := 3; i <= 8; i++ {
		devices = append(devices, seedDeviceInSite(t, db, first.SiteID, "10.0.0."+strconv.Itoa(i)))
	}
	testdb.Exec(t, db, `UPDATE devices SET name = 'switch' WHERE id IN ?`, devices)
	now := time.Now().UTC()
	for _, dev := range devices {
		port := seedPort(t, db, dev, 1, "Gi1", "")
		testdb.Must(t, m.Write(ctx, dev, now.Add(-10*time.Minute), []SamplePoint{
			{Metric: MetricIfInBps, Instance: "1", InterfaceID: &port, Value: 500},
			{Metric: MetricIfOutBps, Instance: "1", InterfaceID: &port, Value: 500},
		}))
	}
	want := slices.Clone(devices)
	slices.SortFunc(want, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for run := 0; run < 3; run++ {
		rows, err := m.TopN(ctx, TopNQuery{DeviceIDs: devices, Measure: TopNTraffic, From: now.Add(-time.Hour), To: now, N: len(devices)})
		testdb.Must(t, err)
		got := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			got[i] = r.DeviceID
		}
		if !slices.Equal(got, want) {
			t.Fatalf("run %d: devices in order %v, want them by id %v", run, got, want)
		}
	}
}
