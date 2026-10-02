package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBResolveRemovedSubject(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	member := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	shareSite(t, db, site, member, models.PermissionReadonly)
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	sites := services.NewSiteService(db)
	calls := &atomic.Int32{}
	r := NewResolver(NewRegistry(countingWidget{calls: calls, devs: []uuid.UUID{dev}}),
		NewDBChecker(db, sites, services.NewMonitorService(db)))
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}

	resp, err := r.Resolve(ctx, Viewer{UserID: member}, w, "")
	testdb.Must(t, err)
	if resp.State != StateOK {
		t.Fatalf("before the delete: state %s, want ok", resp.State)
	}
	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, dev)
	resp, err = r.Resolve(ctx, Viewer{UserID: member}, w, "")
	testdb.Must(t, err)
	if resp.State != StateRemoved || resp.Removed != 1 || calls.Load() != 1 {
		t.Errorf("after the delete: %+v (resolves %d); want removed, 1 removed, and no second resolve", resp, calls.Load())
	}
}

func TestDBResolveNoAccess(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	member := testdb.NewUser(t, db, false)
	site := newSite(t, db, "Elsewhere")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	sites := services.NewSiteService(db)
	r := NewResolver(NewRegistry(countingWidget{calls: &atomic.Int32{}, devs: []uuid.UUID{dev}}),
		NewDBChecker(db, sites, services.NewMonitorService(db)))
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	resp, err := r.Resolve(ctx, Viewer{UserID: member}, w, "")
	testdb.Must(t, err)
	if resp.State != StateNoAccess || resp.Hidden != 1 || resp.Data != nil {
		t.Errorf("resp = %+v, want no_access with hidden 1 and no data", resp)
	}
}

// Preview must not tell a hidden id from a nonexistent one (R8).
func TestDBPreviewSubjectUnavailableIsOneAnswer(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	member := testdb.NewUser(t, db, false)
	visibleSite := newSite(t, db, "HQ")
	shareSite(t, db, visibleSite, member, models.PermissionReadonly)
	visible := newDevice(t, db, visibleSite, "core", "10.0.0.1")
	hiddenSite := newSite(t, db, "Elsewhere")
	hidden := newDevice(t, db, hiddenSite, "secret", "10.0.1.1")
	sites := services.NewSiteService(db)
	checker := NewDBChecker(db, sites, services.NewMonitorService(db))
	v := Viewer{UserID: member}

	preview := func(dev uuid.UUID) (*Response, int32, error) {
		calls := &atomic.Int32{}
		r := NewResolver(NewRegistry(countingWidget{calls: calls, devs: []uuid.UUID{dev}}), checker)
		resp, err := r.Preview(ctx, v, "counting", json.RawMessage(`{}`), "")
		return resp, calls.Load(), err
	}

	var msgs []string
	for name, dev := range map[string]uuid.UUID{"hidden": hidden, "nonexistent": uuid.New()} {
		resp, calls, err := preview(dev)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "config" || resp != nil || calls != 0 {
			t.Fatalf("%s: resp = %+v err = %v calls = %d; want a config FieldError and no resolve", name, resp, err, calls)
		}
		msgs = append(msgs, fe.Msg)
	}
	if msgs[0] != msgs[1] || msgs[0] != msgSubjectUnavailable {
		t.Errorf("messages differ (%q vs %q): preview is an id-existence oracle", msgs[0], msgs[1])
	}

	resp, _, err := preview(visible)
	testdb.Must(t, err)
	if resp.State != StateOK {
		t.Errorf("visible device: state %s, want ok", resp.State)
	}
}
