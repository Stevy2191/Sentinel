package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func agentSiteRouter(t *testing.T, db *gorm.DB, caller uuid.UUID) *gin.Engine {
	t.Helper()
	r, v1, auth := siteTestGroup(t, db, caller)
	RegisterAgentRoutes(v1, services.NewAgentService(db), services.NewSettingsService(db), auth, services.NewSiteService(db))
	return r
}

type agentSiteView struct {
	AgentID  string  `json:"agent_id"`
	SiteID   *string `json:"site_id"`
	SiteName *string `json:"site_name"`
}

// Set on create, kept by a PATCH that leaves site_id out, cleared by null;
// the list carries the site name.
func TestDBAgentSiteSetKeptAndCleared(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := agentSiteRouter(t, db, testdb.NewUser(t, db, true))

	w := toolRequest(r, http.MethodPost, "/api/v1/agents", `{"name":"fs-01","os_type":"linux","site_id":"`+site.String()+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := dataOf[struct {
		Agent agentSiteView `json:"agent"`
	}](t, w).Agent
	if created.SiteID == nil || *created.SiteID != site.String() || created.SiteName == nil || *created.SiteName != "Annex" {
		t.Fatalf("created = %+v", created)
	}

	w = toolRequest(r, http.MethodPatch, "/api/v1/agents/"+created.AgentID, `{"name":"fs-02"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	list := dataOf[[]agentSiteView](t, toolRequest(r, http.MethodGet, "/api/v1/agents", ""))
	if len(list) != 1 || list[0].SiteID == nil || *list[0].SiteID != site.String() || list[0].SiteName == nil || *list[0].SiteName != "Annex" {
		t.Errorf("after a PATCH without site_id: %+v, want the site kept and named", list)
	}

	w = toolRequest(r, http.MethodPatch, "/api/v1/agents/"+created.AgentID, `{"site_id":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	if got := dataOf[agentSiteView](t, w); got.SiteID != nil || got.SiteName != nil {
		t.Errorf("after site_id null: %+v, want no site", got)
	}
}

// A site that does not exist is refused on create and on edit.
func TestDBAgentUnknownSiteRefused(t *testing.T) {
	db := testdb.Open(t)
	r := agentSiteRouter(t, db, testdb.NewUser(t, db, true))
	missing := uuid.New().String()

	w := toolRequest(r, http.MethodPost, "/api/v1/agents", `{"name":"fs-01","os_type":"linux","site_id":"`+missing+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
		t.Fatalf("create: %d %s, want 400 site not found", w.Code, w.Body.String())
	}

	w = toolRequest(r, http.MethodPost, "/api/v1/agents", `{"name":"fs-01","os_type":"linux"}`)
	created := dataOf[struct {
		Agent agentSiteView `json:"agent"`
	}](t, w).Agent
	w = toolRequest(r, http.MethodPatch, "/api/v1/agents/"+created.AgentID, `{"site_id":"`+missing+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
		t.Errorf("edit: %d %s, want 400 site not found", w.Code, w.Body.String())
	}
}
