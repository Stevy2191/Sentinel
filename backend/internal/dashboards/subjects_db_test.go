package dashboards

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type subjectWorld struct {
	checker                             *DBChecker
	admin, member                       uuid.UUID
	sharedSite, otherSite               uuid.UUID
	sharedDev, otherDev                 uuid.UUID
	ownMonitor, sharedMonitor, otherMon uuid.UUID
	agent                               uuid.UUID
}

func newSubjectWorld(t *testing.T) subjectWorld {
	t.Helper()
	db := testdb.Open(t)
	w := subjectWorld{admin: testdb.NewUser(t, db, true), member: testdb.NewUser(t, db, false)}
	w.sharedSite, w.otherSite = newSite(t, db, "Shared"), newSite(t, db, "Other")
	shareSite(t, db, w.sharedSite, w.member, models.PermissionReadonly)
	w.sharedDev = newDevice(t, db, w.sharedSite, "core", "10.0.0.1")
	w.otherDev = newDevice(t, db, w.otherSite, "edge", "10.9.0.1")
	w.ownMonitor = newMonitor(t, db, w.member, "mine", "https://mine.test")
	w.sharedMonitor = newMonitor(t, db, w.admin, "shared", "https://shared.test")
	shareMonitor(t, db, w.sharedMonitor, w.member, w.admin)
	w.otherMon = newMonitor(t, db, w.admin, "other", "https://other.test")
	w.agent = newAgent(t, db, "fileserver")
	w.checker = NewDBChecker(db, services.NewSiteService(db), services.NewMonitorService(db))
	return w
}

func (w subjectWorld) all() Subjects {
	return Subjects{
		Sites:    []uuid.UUID{w.sharedSite, w.otherSite},
		Devices:  []uuid.UUID{w.sharedDev, w.otherDev, uuid.New()},
		Monitors: []uuid.UUID{w.ownMonitor, w.sharedMonitor, w.otherMon, uuid.New()},
		Agents:   []uuid.UUID{w.agent},
	}
}

func TestDBFilterMember(t *testing.T) {
	w := newSubjectWorld(t)
	f, err := Filter(context.Background(), w.checker, Viewer{UserID: w.member}, w.all())
	testdb.Must(t, err)
	if len(f.Visible.Sites) != 1 || f.Visible.Sites[0] != w.sharedSite {
		t.Errorf("visible sites = %v, want only the shared site", f.Visible.Sites)
	}
	if len(f.Visible.Devices) != 1 || f.Visible.Devices[0] != w.sharedDev {
		t.Errorf("visible devices = %v, want only the device in the shared site", f.Visible.Devices)
	}
	if len(f.Visible.Monitors) != 2 || f.Visible.Monitors[0] != w.ownMonitor || f.Visible.Monitors[1] != w.sharedMonitor {
		t.Errorf("visible monitors = %v, want own and shared", f.Visible.Monitors)
	}
	if len(f.Visible.Agents) != 0 {
		t.Errorf("a member sees agents %v; agents are admin-only", f.Visible.Agents)
	}
	// Hidden: other site, other device, other monitor, the agent. Removed: one device, one monitor.
	if f.Hidden != 4 || f.Removed != 2 {
		t.Errorf("hidden=%d removed=%d, want 4 and 2", f.Hidden, f.Removed)
	}
	if f.State() != "" {
		t.Errorf("State() = %q with visible subjects, want \"\" (resolve)", f.State())
	}
}

func TestDBFilterAdminAndPublicSeeEverythingThatExists(t *testing.T) {
	w := newSubjectWorld(t)
	for name, v := range map[string]Viewer{"admin": {UserID: w.admin, IsAdmin: true}, "public": PublicViewer} {
		f, err := Filter(context.Background(), w.checker, v, w.all())
		testdb.Must(t, err)
		if f.Hidden != 0 || f.Removed != 2 {
			t.Errorf("%s: hidden=%d removed=%d, want 0 and 2", name, f.Hidden, f.Removed)
		}
		if len(f.Visible.Devices) != 2 || len(f.Visible.Monitors) != 3 || len(f.Visible.Agents) != 1 || len(f.Visible.Sites) != 2 {
			t.Errorf("%s: visible = %+v, want every existing subject", name, f.Visible)
		}
	}
}

func TestDBFilterStates(t *testing.T) {
	w := newSubjectWorld(t)
	ctx := context.Background()
	member := Viewer{UserID: w.member}

	f, err := Filter(ctx, w.checker, member, Subjects{Devices: []uuid.UUID{w.otherDev}})
	testdb.Must(t, err)
	if f.State() != StateNoAccess {
		t.Errorf("only a hidden device: State() = %q, want no_access", f.State())
	}
	f, err = Filter(ctx, w.checker, member, Subjects{Devices: []uuid.UUID{uuid.New()}})
	testdb.Must(t, err)
	if f.State() != StateRemoved {
		t.Errorf("only a missing device: State() = %q, want removed", f.State())
	}
	f, err = Filter(ctx, w.checker, member, Subjects{})
	testdb.Must(t, err)
	if f.State() != "" {
		t.Errorf("no subjects (a label): State() = %q, want \"\"", f.State())
	}
	f, err = Filter(ctx, w.checker, member, Subjects{Broad: true, Devices: []uuid.UUID{w.otherDev}})
	testdb.Must(t, err)
	if f.State() != "" || !f.Visible.Broad || f.Hidden != 1 {
		t.Errorf("broad with a hidden device: State() = %q, Broad = %v, Hidden = %d; want \"\", true, 1", f.State(), f.Visible.Broad, f.Hidden)
	}
	f, err = Filter(ctx, w.checker, member, Subjects{Broad: true})
	testdb.Must(t, err)
	if f.State() != "" || !f.Visible.Broad {
		t.Errorf("broad scope: State() = %q, Broad = %v; want \"\" and true", f.State(), f.Visible.Broad)
	}
}
