package dashboards

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBDashboardIsPersonalOrSiteNeverBoth(t *testing.T) {
	db := testdb.Open(t)
	user := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	for name, args := range map[string][]any{
		"neither": {uuid.New(), "x", nil, nil},
		"both":    {uuid.New(), "x", site, user},
	} {
		err := db.Exec(`INSERT INTO dashboards (id, name, site_id, owner_id) VALUES (?, ?, ?, ?)`, args...).Error
		if err == nil {
			t.Errorf("%s: insert succeeded, want the owner/site CHECK to refuse it", name)
		}
	}
	newDashboard(t, db, "mine", nil, &user)
	newDashboard(t, db, "site's", &site, nil)
}

func TestDBDashboardCascades(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	admin := testdb.NewUser(t, db, true)
	other := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	personal := newDashboard(t, db, "mine", nil, &owner)
	siteDash := newDashboard(t, db, "site's", &site, nil)
	newWidget(t, db, personal, "label", `{"text":"a"}`)
	newWidget(t, db, siteDash, "label", `{"text":"b"}`)
	testdb.Exec(t, db, `INSERT INTO dashboard_sharing (dashboard_id, user_id, permission) VALUES (?, ?, 'readonly')`, personal, other)
	publish(t, db, siteDash, admin)

	testdb.Exec(t, db, `DELETE FROM users WHERE id = ?`, owner)
	testdb.Exec(t, db, `DELETE FROM sites WHERE id = ?`, site)

	for _, table := range []string{"dashboards", "dashboard_widgets", "dashboard_sharing", "dashboard_public_links"} {
		var n int64
		testdb.Must(t, db.Table(table).Count(&n).Error)
		if n != 0 {
			t.Errorf("%s has %d rows after deleting the owner and the site, want 0", table, n)
		}
	}
}

func TestDBWidgetMustFitTheGrid(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	d := newDashboard(t, db, "mine", nil, &owner)
	err := db.Exec(`INSERT INTO dashboard_widgets (dashboard_id, type, x, y, w, h) VALUES (?, 'label', 10, 0, 4, 2)`, d).Error
	if err == nil {
		t.Fatal("x+w = 14 accepted, want the grid CHECK to refuse it")
	}
}

func TestDBWidgetConfigRoundTrips(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	d := newDashboard(t, db, "mine", nil, &owner)
	w := models.DashboardWidget{DashboardID: d, Type: "label", Config: models.RawJSON(`{"text":"Main Campus","size":"l"}`), W: 4, H: 1}
	testdb.Must(t, db.Create(&w).Error)
	var got models.DashboardWidget
	testdb.Must(t, db.First(&got, "id = ?", w.ID).Error)
	if string(got.Config) != `{"size": "l", "text": "Main Campus"}` {
		t.Fatalf("config = %s, want the jsonb normal form of what was saved", got.Config)
	}
}
