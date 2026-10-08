package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func monitorGroupRouter(t *testing.T, db *gorm.DB, caller uuid.UUID) *gin.Engine {
	t.Helper()
	r, v1, _ := siteTestGroup(t, db, caller)
	RegisterMonitorGroupRoutes(v1, services.NewMonitorService(db), services.NewIncidentService(db))
	return r
}

// groupedMonitor creates a monitor owned by owner, in group, whose request
// headers carry a secret only its owner (or an admin) may see.
func groupedMonitor(t *testing.T, db *gorm.DB, owner uuid.UUID, group uuid.UUID, name, secret string) uuid.UUID {
	t.Helper()
	m, err := services.NewMonitorService(db).CreateMonitor(context.Background(), &models.Monitor{
		Name: name, Type: models.MonitorTypeHTTP, URL: "https://example.com/" + name,
		IntervalSeconds: 60, TimeoutSeconds: 5, OwnerID: &owner,
		Headers: models.StringMap{"Authorization": "Bearer " + secret},
	})
	testdb.Must(t, err)
	testdb.Exec(t, db, `UPDATE monitors SET group_id = ? WHERE id = ?`, group, m.ID)
	return m.ID
}

type groupView struct {
	ID           string `json:"id"`
	MonitorCount int    `json:"monitor_count"`
	Monitors     []struct {
		ID string `json:"id"`
	} `json:"monitors"`
}

// The group list shows each caller only the monitors they can see: another
// user's monitor - and its request headers, which can hold credentials - must
// not come back just because it shares a group. An admin still sees both.
func TestDBMonitorGroupsShowOnlyVisibleMonitors(t *testing.T) {
	db := testdb.Open(t)
	alice, bob := testdb.NewUser(t, db, false), testdb.NewUser(t, db, false)
	group, err := services.NewMonitorService(db).CreateMonitorGroup(context.Background(), "Web", nil, nil)
	testdb.Must(t, err)
	mine := groupedMonitor(t, db, alice, group.ID, "alice-web", "alice-secret")
	groupedMonitor(t, db, bob, group.ID, "bob-web", "bob-secret")

	w := toolRequest(monitorGroupRouter(t, db, alice), http.MethodGet, "/api/v1/monitor-groups", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "bob-secret") || strings.Contains(w.Body.String(), "bob-web") {
		t.Errorf("alice's group list carries bob's monitor: %s", w.Body.String())
	}
	groups := dataOf[[]groupView](t, w)
	if len(groups) != 1 || groups[0].MonitorCount != 1 || len(groups[0].Monitors) != 1 || groups[0].Monitors[0].ID != mine.String() {
		t.Errorf("alice sees %+v, want the group with only her monitor", groups)
	}

	w = toolRequest(monitorGroupRouter(t, db, testdb.NewUser(t, db, true)), http.MethodGet, "/api/v1/monitor-groups", "")
	if admin := dataOf[[]groupView](t, w); len(admin) != 1 || admin[0].MonitorCount != 2 {
		t.Errorf("admin sees %+v, want both monitors", admin)
	}
}

// Moving a monitor into or out of a group changes it, so it needs edit access
// to that monitor, like any other change.
func TestDBMoveMonitorToGroupNeedsEditAccess(t *testing.T) {
	db := testdb.Open(t)
	alice, bob := testdb.NewUser(t, db, false), testdb.NewUser(t, db, false)
	ms := services.NewMonitorService(db)
	group, err := ms.CreateMonitorGroup(context.Background(), "Web", nil, nil)
	testdb.Must(t, err)
	target, err := ms.CreateMonitorGroup(context.Background(), "Other", nil, nil)
	testdb.Must(t, err)
	monitor := groupedMonitor(t, db, alice, group.ID, "alice-web", "alice-secret")
	body := `{"group_id":"` + target.ID.String() + `"}`

	w := toolRequest(monitorGroupRouter(t, db, bob), http.MethodPost, "/api/v1/monitors/"+monitor.String()+"/group", body)
	if w.Code != http.StatusForbidden {
		t.Errorf("bob moving alice's monitor: %d %s, want 403", w.Code, w.Body.String())
	}
	var stillIn int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM monitors WHERE id = ? AND group_id = ?`, monitor, group.ID).Scan(&stillIn).Error)
	if stillIn != 1 {
		t.Fatalf("a refused move changed the monitor's group")
	}

	w = toolRequest(monitorGroupRouter(t, db, alice), http.MethodPost, "/api/v1/monitors/"+monitor.String()+"/group", body)
	if w.Code != http.StatusOK {
		t.Errorf("alice moving her own monitor: %d %s, want 200", w.Code, w.Body.String())
	}
}
