package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The report pages describe a Metrics scope in words from scope_data, so a
// signed-in response carries it; a share-link response is read by anyone with
// the link and must not.
func TestDBReportResponseCarriesScopeOnlyWhenSignedIn(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	monitor := uuid.New()
	report := models.Report{
		ID:            uuid.New(),
		UserID:        owner,
		CreatedBy:     owner,
		Name:          "Web",
		ReportType:    models.ReportTypeUptime,
		ScopeType:     models.ScopeTypeMonitors,
		ScopeData:     models.ReportScope{MonitorIDs: []uuid.UUID{monitor}},
		TimeRangeDays: 30,
		PeriodKind:    models.PeriodRolling,
	}
	testdb.Must(t, db.Create(&report).Error)
	h := NewReportBuilder(db, nil, nil, nil, nil)

	signedIn, err := h.buildReportResponse(context.Background(), &report, "")
	testdb.Must(t, err)
	if signedIn.ScopeData == nil || len(signedIn.ScopeData.MonitorIDs) != 1 || signedIn.ScopeData.MonitorIDs[0] != monitor {
		t.Errorf("signed-in scope_data = %+v, want the one monitor", signedIn.ScopeData)
	}

	shared, err := h.buildReportResponse(context.Background(), &report, "a-share-token")
	testdb.Must(t, err)
	body, err := json.Marshal(shared)
	testdb.Must(t, err)
	if shared.ScopeData != nil || strings.Contains(string(body), "scope_data") {
		t.Errorf("share-link response carries scope_data: %s", body)
	}
}

// The same through the routes: the signed-in list carries the scope, and the
// public share-link view of the same report names neither scope_data nor the
// monitor it covers.
func TestDBReportRoutesCarryScopeOnlyWhenSignedIn(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	monitor := uuid.New()
	report := models.Report{
		ID:            uuid.New(),
		UserID:        owner,
		CreatedBy:     owner,
		Name:          "Web",
		ReportType:    models.ReportTypeUptime,
		ScopeType:     models.ScopeTypeMonitors,
		ScopeData:     models.ReportScope{MonitorIDs: []uuid.UUID{monitor}},
		TimeRangeDays: 30,
		PeriodKind:    models.PeriodRolling,
	}
	testdb.Must(t, db.Create(&report).Error)
	token := "share-" + uuid.NewString()
	testdb.Must(t, db.Create(&models.ReportAccess{
		ID:         uuid.New(),
		ReportID:   report.ID,
		AccessType: models.AccessTypeViewer,
		ShareToken: &token,
	}).Error)

	h := NewReportBuilder(db, nil, nil, nil, nil)
	r := reportBuilderRouter(h, owner)
	RegisterPublicReportRoutes(r, h)

	list := do(r, http.MethodGet, "/api/v1/reports", nil)
	want := `"scope_data":{"monitor_ids":["` + monitor.String() + `"]}`
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), want) {
		t.Errorf("GET /reports: status %d, body %s; want %s", list.Code, list.Body.String(), want)
	}

	shared := do(r, http.MethodGet, "/api/v1/public/reports/share/"+token, nil)
	body := shared.Body.String()
	if shared.Code != http.StatusOK || strings.Contains(body, "scope_data") || strings.Contains(body, monitor.String()) {
		t.Errorf("GET share link: status %d, body %s; want 200 without the scope", shared.Code, body)
	}
}
