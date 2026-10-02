package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBCreateWithStandardWidgets(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	site := newSite(t, db, "HQ")
	sites := services.NewSiteService(db)
	checker := NewDBChecker(db, sites, services.NewMonitorService(db))
	svc := NewService(db, sites, NewDefaultRegistry(realDeps(t, db)), checker)

	d, err := svc.Create(ctx, Viewer{UserID: admin, IsAdmin: true}, CreateInput{Name: "HQ", SiteID: &site, Starter: true})
	testdb.Must(t, err)
	types := map[string]bool{}
	for _, w := range d.Widgets {
		types[w.Type] = true
	}
	for _, want := range []string{"timeseries", "site_power", "open_incidents", "top_n", "device_table"} {
		if !types[want] {
			t.Errorf("standard widgets lack %s: %+v", want, d.Widgets)
		}
	}
	if _, err := svc.Create(ctx, Viewer{UserID: admin, IsAdmin: true}, CreateInput{Name: "Mine", Starter: true}); !errors.Is(err, ErrInvalid) {
		t.Errorf("standard widgets on a personal dashboard: err = %v, want ErrInvalid", err)
	}
}
