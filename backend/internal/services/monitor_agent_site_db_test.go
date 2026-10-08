package services

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// Deleting a site leaves its monitors and agents in place, with no site.
func TestDBMonitorAndAgentSitesClearWhenTheSiteGoes(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	site := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Annex')`, site)

	mon, err := NewMonitorService(db).CreateMonitor(ctx, &models.Monitor{
		Name: "core", Type: models.MonitorTypePing, URL: "10.0.0.1",
		IntervalSeconds: 60, TimeoutSeconds: 5, FailureThreshold: 2, SiteID: &site,
	})
	testdb.Must(t, err)
	agent := &models.Agent{Name: "fs-01", OSType: "linux", CheckInterval: 60, RetryAttempts: 3, SiteID: &site}
	testdb.Must(t, NewAgentService(db).Register(ctx, agent))

	var withSite int64
	testdb.Must(t, db.Raw(`SELECT
		(SELECT count(*) FROM monitors WHERE id = ? AND site_id = ?) +
		(SELECT count(*) FROM agents WHERE id = ? AND site_id = ?)`, mon.ID, site, agent.ID, site).Scan(&withSite).Error)
	if withSite != 2 {
		t.Fatalf("stored with a site: %d of 2", withSite)
	}

	if _, err := NewSiteService(db).Delete(ctx, site); err != nil {
		t.Fatalf("deleting the site: %v", err)
	}

	var cleared int64
	testdb.Must(t, db.Raw(`SELECT
		(SELECT count(*) FROM monitors WHERE id = ? AND site_id IS NULL) +
		(SELECT count(*) FROM agents WHERE id = ? AND site_id IS NULL)`, mon.ID, agent.ID).Scan(&cleared).Error)
	if cleared != 2 {
		t.Errorf("after the site was deleted, %d of 2 rows remain with no site", cleared)
	}
}

func TestDBSiteNamesByID(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Annex'), (?, 'Courthouse')`, a, b)

	names, err := NewSiteService(db).NamesByID(ctx, []uuid.UUID{a, b, uuid.New()})
	testdb.Must(t, err)
	if len(names) != 2 || names[a] != "Annex" || names[b] != "Courthouse" {
		t.Errorf("names = %v", names)
	}

	none, err := NewSiteService(db).NamesByID(ctx, nil)
	testdb.Must(t, err)
	if len(none) != 0 {
		t.Errorf("no ids gave %v", none)
	}
}
