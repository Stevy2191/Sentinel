package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBIncidentListBySiteAndSubjects(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	a := seedDevice(t, db, "A", "10.0.0.1")
	b := seedDevice(t, db, "B", "10.0.0.2")
	mon := newMonitor(t, db, admin, "web")
	start := time.Now().UTC().Add(-time.Hour)
	inA := insertIncident(t, db, nil, &a.DeviceID, start, nil)
	inB := insertIncident(t, db, nil, &b.DeviceID, start, nil)
	inM := insertIncident(t, db, &mon, nil, start, nil)
	svc := NewIncidentService(db)
	viewer := &IncidentViewer{UserID: admin, IsAdmin: true}

	ids := func(opts IncidentListOptions) map[uuid.UUID]bool {
		opts.Viewer = viewer
		rows, _, err := svc.ListIncidents(ctx, opts)
		testdb.Must(t, err)
		out := map[uuid.UUID]bool{}
		for _, r := range rows {
			out[r.ID] = true
		}
		return out
	}
	if got := ids(IncidentListOptions{SiteID: &a.SiteID}); len(got) != 1 || !got[inA] {
		t.Errorf("site A = %v, want only its device's incident", got)
	}
	if got := ids(IncidentListOptions{DeviceIDs: []uuid.UUID{b.DeviceID}, MonitorIDs: []uuid.UUID{mon}}); len(got) != 2 || !got[inB] || !got[inM] {
		t.Errorf("device B or the monitor = %v", got)
	}
	if got := ids(IncidentListOptions{MonitorIDs: []uuid.UUID{mon}}); len(got) != 1 || !got[inM] {
		t.Errorf("monitor only = %v", got)
	}

	counts, err := svc.OpenCountsByDevice(ctx, []uuid.UUID{a.DeviceID, b.DeviceID, uuid.New()})
	testdb.Must(t, err)
	if counts[a.DeviceID] != 1 || counts[b.DeviceID] != 1 || len(counts) != 2 {
		t.Errorf("open counts = %v", counts)
	}
}

func TestDBMonitorsByIDs(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	m1, m2 := newMonitor(t, db, owner, "one"), newMonitor(t, db, owner, "two")
	got, err := NewMonitorService(db).MonitorsByIDs(context.Background(), []uuid.UUID{m1, m2, uuid.New()})
	testdb.Must(t, err)
	if len(got) != 2 {
		t.Errorf("got %d monitors, want the 2 that exist", len(got))
	}
	if none, err := NewMonitorService(db).MonitorsByIDs(context.Background(), nil); err != nil || len(none) != 0 {
		t.Errorf("no ids: %v, %v", none, err)
	}
}
