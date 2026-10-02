package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// accessWorld has one user per role against one personal and one site dashboard.
type accessWorld struct {
	db                                           *gorm.DB
	svc                                          *Service
	admin, owner, readonly, editable, siteRO     uuid.UUID
	siteRW, stranger                             uuid.UUID
	site, personal, siteDash                     uuid.UUID
}

func newAccessWorld(t *testing.T) accessWorld {
	t.Helper()
	db := testdb.Open(t)
	w := accessWorld{db: db}
	w.admin = testdb.NewUser(t, db, true)
	for _, id := range []*uuid.UUID{&w.owner, &w.readonly, &w.editable, &w.siteRO, &w.siteRW, &w.stranger} {
		*id = testdb.NewUser(t, db, false)
	}
	w.site = newSite(t, db, "HQ")
	shareSite(t, db, w.site, w.siteRO, models.PermissionReadonly)
	shareSite(t, db, w.site, w.siteRW, models.PermissionEditable)
	w.personal = newDashboard(t, db, "Mine", nil, &w.owner)
	testdb.Exec(t, db, `INSERT INTO dashboard_sharing (dashboard_id, user_id, permission) VALUES (?, ?, 'readonly'), (?, ?, 'editable')`,
		w.personal, w.readonly, w.personal, w.editable)
	w.siteDash = newDashboard(t, db, "HQ overview", &w.site, nil)
	newWidget(t, db, w.siteDash, "label", `{"text":"HQ","size":"m"}`)
	sites := services.NewSiteService(db)
	w.svc = NewService(db, sites, NewRegistry(labelWidget{}), NewDBChecker(db, sites, services.NewMonitorService(db)))
	return w
}

func (w accessWorld) viewer(id uuid.UUID) Viewer { return Viewer{UserID: id, IsAdmin: id == w.admin} }

func TestDBListShowsWhatEachRoleMaySee(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	want := map[uuid.UUID][]uuid.UUID{
		w.admin:    {w.siteDash, w.personal},
		w.owner:    {w.personal},
		w.readonly: {w.personal},
		w.editable: {w.personal},
		w.siteRO:   {w.siteDash},
		w.siteRW:   {w.siteDash},
		w.stranger: {},
	}
	for user, ids := range want {
		got, err := w.svc.List(ctx, w.viewer(user), nil)
		testdb.Must(t, err)
		if len(got) != len(ids) {
			t.Errorf("user %s sees %d dashboards, want %d", user, len(got), len(ids))
			continue
		}
		seen := map[uuid.UUID]bool{}
		for _, d := range got {
			seen[d.ID] = true
		}
		for _, id := range ids {
			if !seen[id] {
				t.Errorf("user %s does not see dashboard %s", user, id)
			}
		}
	}
	if _, err := w.svc.List(ctx, PublicViewer, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("public List err = %v, want ErrNotFound", err)
	}
}

func TestDBGetAccessAndConfigs(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	cases := []struct {
		user       uuid.UUID
		id         uuid.UUID
		access     string
		withConfig bool
	}{
		{w.admin, w.siteDash, "manage", true},
		{w.siteRW, w.siteDash, "edit", true},
		{w.siteRO, w.siteDash, "view", false},
		{w.owner, w.personal, "edit", true},
		{w.readonly, w.personal, "view", false},
	}
	for _, c := range cases {
		d, err := w.svc.Get(ctx, w.viewer(c.user), c.id)
		testdb.Must(t, err)
		if d.Access != c.access {
			t.Errorf("access = %s, want %s", d.Access, c.access)
		}
		if c.id == w.siteDash {
			if len(d.Widgets) != 1 {
				t.Fatalf("widgets = %d, want 1", len(d.Widgets))
			}
			if hasConfig := len(d.Widgets[0].Config) > 0; hasConfig != c.withConfig {
				t.Errorf("%s: config sent = %v, want %v (configs need edit)", c.access, hasConfig, c.withConfig)
			}
		}
	}
	for _, user := range []uuid.UUID{w.stranger, w.siteRO} {
		if _, err := w.svc.Get(ctx, w.viewer(user), w.personal); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get by a user without access err = %v, want ErrNotFound (404, not 403)", err)
		}
	}
}

func TestDBCreate(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	d, err := w.svc.Create(ctx, w.viewer(w.stranger), CreateInput{Name: "  My view  "})
	testdb.Must(t, err)
	if d.Name != "My view" || d.OwnerID == nil || *d.OwnerID != w.stranger || d.SiteID != nil || d.Version != 1 {
		t.Errorf("personal create = %+v", d.Dashboard)
	}
	if _, err := w.svc.Create(ctx, w.viewer(w.siteRO), CreateInput{Name: "x", SiteID: &w.site}); !errors.Is(err, ErrForbidden) {
		t.Errorf("readonly site create err = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.Create(ctx, w.viewer(w.stranger), CreateInput{Name: "x", SiteID: &w.site}); !errors.Is(err, ErrSiteNotFound) {
		t.Errorf("create on an unshared site err = %v, want ErrSiteNotFound", err)
	}
	sd, err := w.svc.Create(ctx, w.viewer(w.siteRW), CreateInput{Name: "Closet", SiteID: &w.site})
	testdb.Must(t, err)
	if sd.SiteID == nil || sd.OwnerID != nil || sd.CreatedBy == nil || *sd.CreatedBy != w.siteRW {
		t.Errorf("site create = %+v", sd.Dashboard)
	}
	for _, name := range []string{"", "   ", string(make([]rune, 101))} {
		if _, err := w.svc.Create(ctx, w.viewer(w.owner), CreateInput{Name: name}); !errors.Is(err, ErrInvalid) {
			t.Errorf("name %q err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestDBDelete(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	if _, err := w.svc.Delete(ctx, w.viewer(w.readonly), w.personal); !errors.Is(err, ErrForbidden) {
		t.Errorf("readonly delete err = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.Delete(ctx, w.viewer(w.stranger), w.personal); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger delete err = %v, want ErrNotFound", err)
	}
	publish(t, w.db, w.siteDash, w.admin)
	if _, err := w.svc.Delete(ctx, w.viewer(w.siteRW), w.siteDash); !errors.Is(err, ErrPublished) {
		t.Errorf("editor deleting a published dashboard err = %v, want ErrPublished", err)
	}
	d, err := w.svc.Get(ctx, w.viewer(w.siteRW), w.siteDash)
	testdb.Must(t, err)
	if !d.Published || d.CanEdit {
		t.Errorf("published=%v can_edit=%v for an editor of a published dashboard, want true/false", d.Published, d.CanEdit)
	}
	if _, err := w.svc.Delete(ctx, w.viewer(w.admin), w.siteDash); err != nil {
		t.Errorf("admin delete of a published dashboard: %v", err)
	}
	if _, err := w.svc.Delete(ctx, w.viewer(w.editable), w.personal); err != nil {
		t.Errorf("shared-editable delete: %v", err)
	}
}
