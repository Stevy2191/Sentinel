package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// auditCall is one recorded audit entry, resource included.
type auditCall struct {
	action, resourceType string
	resourceID           *uuid.UUID
	changes              models.AuditChanges
}

// recordingAudit keeps every audit call.
type recordingAudit struct{ calls []auditCall }

func (a *recordingAudit) Record(_ context.Context, _ services.Actor, action, resourceType string, id *uuid.UUID, changes models.AuditChanges) {
	a.calls = append(a.calls, auditCall{action, resourceType, id, changes})
}

// netToolsAdminRouter mounts the admin routes behind RequireAdmin, as main.go
// does, for the user caller (read from the database).
func netToolsAdminRouter(t *testing.T, db *gorm.DB, audit *recordingAudit, caller uuid.UUID) *gin.Engine {
	t.Helper()
	auth := services.NewAuthService(db, testJWTSecret)
	user, err := auth.GetUserByID(context.Background(), caller)
	testdb.Must(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID)
		c.Set("username", user.Username)
		c.Set("is_admin", user.IsAdmin)
		c.Next()
	})
	admin := v1.Group("")
	admin.Use(RequireAdmin(auth))
	RegisterNetToolsAdminRoutes(admin, services.NewSettingsService(db), auth, services.NewAgentService(db), audit)
	return r
}

func storedSettings(t *testing.T, db *gorm.DB) toolruns.Settings {
	t.Helper()
	return toolruns.LoadSettings(context.Background(), services.NewSettingsService(db))
}

// Every bad line is reported against its entry, and nothing is saved.
func TestDBNetToolsSettingsRejectBadEntriesAndSaveNothing(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	w := toolRequest(r, http.MethodPut, "/api/v1/settings/net-tools",
		`{"allowlist":["10.0.0.0/8","10.0.0.0/7","not a host!"],"server_enabled":true,"retention_days":30}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Entries []struct {
			Entry   string `json:"entry"`
			Message string `json:"message"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "invalid_allowlist" || len(body.Entries) != 2 ||
		body.Entries[0].Entry != "10.0.0.0/7" || body.Entries[0].Message != "too broad: use /8 or narrower" ||
		body.Entries[1].Entry != "not a host!" || body.Entries[1].Message != "not a valid IPv4 address, CIDR or host name" {
		t.Errorf("body = %+v", body)
	}
	if got := storedSettings(t, db); len(got.Allowlist) != 0 {
		t.Errorf("allowlist %v saved from a refused PUT", got.Allowlist)
	}
	if len(audit.calls) != 0 {
		t.Errorf("a refused save was audited: %+v", audit.calls)
	}

	w = toolRequest(r, http.MethodPut, "/api/v1/settings/net-tools", `{"allowlist":[],"server_enabled":true,"retention_days":0}`)
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusUnprocessableEntity || code != "invalid_params" {
		t.Errorf("retention 0: status %d code %q, want 422 invalid_params", w.Code, code)
	}
}

func TestDBNetToolsSettingsSaveTidyAndAudit(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	w := toolRequest(r, http.MethodPut, "/api/v1/settings/net-tools",
		`{"allowlist":["10.0.0.0/24"," Files.Example.ORG ","","10.0.0.0/24"],"server_enabled":false,"retention_days":7}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var saved toolruns.Settings
	toolData(t, w.Body.Bytes(), &saved)
	want := map[string]bool{"10.0.0.0/24": true, "files.example.org": true}
	if len(saved.Allowlist) != 2 || !want[saved.Allowlist[0]] || !want[saved.Allowlist[1]] || saved.ServerEnabled || saved.RetentionDays != 7 {
		t.Errorf("saved = %+v, want the two tidied entries, server off, 7 days", saved)
	}
	w = toolRequest(r, http.MethodGet, "/api/v1/settings/net-tools", "")
	var got toolruns.Settings
	toolData(t, w.Body.Bytes(), &got)
	if len(got.Allowlist) != 2 || got.ServerEnabled || got.RetentionDays != 7 {
		t.Errorf("GET = %+v, want what was saved", got)
	}
	if len(audit.calls) != 1 || audit.calls[0].action != models.ActionNetToolsSettingsUpdated ||
		audit.calls[0].resourceType != models.ResourceSettings || audit.calls[0].resourceID != nil ||
		audit.calls[0].changes.Before["retention_days"] != 30 || audit.calls[0].changes.After["retention_days"] != 7 {
		t.Errorf("audit = %+v", audit.calls)
	}
}

func TestDBUserNetToolsGrant(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	target := testdb.NewUser(t, db, false)

	w := toolRequest(r, http.MethodPatch, "/api/v1/users/"+target.String()+"/net-tools", `{"enabled":true}`)
	var reply struct {
		ID       uuid.UUID `json:"id"`
		NetTools bool      `json:"net_tools"`
	}
	toolData(t, w.Body.Bytes(), &reply)
	if reply.ID != target || !reply.NetTools {
		t.Errorf("reply = %+v", reply)
	}
	var stored bool
	testdb.Must(t, db.Raw(`SELECT net_tools FROM users WHERE id = ?`, target).Scan(&stored).Error)
	if !stored {
		t.Error("the grant was not stored")
	}
	if len(audit.calls) != 1 || audit.calls[0].action != models.ActionUserNetToolsChanged ||
		audit.calls[0].resourceType != models.ResourceUser || audit.calls[0].resourceID == nil || *audit.calls[0].resourceID != target ||
		audit.calls[0].changes.Before["net_tools"] != false || audit.calls[0].changes.After["net_tools"] != true {
		t.Errorf("audit = %+v", audit.calls)
	}

	if w := toolRequest(r, http.MethodPatch, "/api/v1/users/"+target.String()+"/net-tools", `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("no enabled: status %d, want 400", w.Code)
	}
	for _, id := range []string{uuid.NewString(), "not-a-user"} {
		if w := toolRequest(r, http.MethodPatch, "/api/v1/users/"+id+"/net-tools", `{"enabled":true}`); w.Code != http.StatusNotFound {
			t.Errorf("user %s: status %d, want 404", id, w.Code)
		}
	}
}

func TestDBAgentToolsSwitch(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	agent := registerToolAgent(t, db, "file-server", false)

	w := toolRequest(r, http.MethodPut, "/api/v1/agents/"+agent.AgentID+"/tools", `{"enabled":true}`)
	var reply map[string]any
	toolData(t, w.Body.Bytes(), &reply)
	if reply["tools_enabled"] != true || reply["agent_id"] != agent.AgentID {
		t.Errorf("reply = %v", reply)
	}
	if _, leaked := reply["server_token"]; leaked {
		t.Error("the reply carries the agent's token")
	}
	var stored bool
	testdb.Must(t, db.Raw(`SELECT tools_enabled FROM agents WHERE id = ?`, agent.ID).Scan(&stored).Error)
	if !stored {
		t.Error("the switch was not stored")
	}
	if len(audit.calls) != 1 || audit.calls[0].action != models.ActionAgentToolsChanged ||
		audit.calls[0].resourceType != models.ResourceAgent || audit.calls[0].resourceID == nil || *audit.calls[0].resourceID != agent.ID ||
		audit.calls[0].changes.Before["tools_enabled"] != false || audit.calls[0].changes.After["tools_enabled"] != true {
		t.Errorf("audit = %+v", audit.calls)
	}
	if w := toolRequest(r, http.MethodPut, "/api/v1/agents/agent_ffffffffff/tools", `{"enabled":true}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown agent: status %d, want 404", w.Code)
	}
}

// Grant holders may run tools but change nothing: settings, grants and agent
// switches are refused by RequireAdmin before any handler runs.
func TestDBNetToolsAdminRoutesRefuseGrantHolders(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, grantedUser(t, db))
	target := testdb.NewUser(t, db, false)
	agent := registerToolAgent(t, db, "file-server", false)

	for _, rt := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/settings/net-tools", ""},
		{http.MethodPut, "/api/v1/settings/net-tools", `{"allowlist":["10.0.0.0/8"],"server_enabled":true,"retention_days":30}`},
		{http.MethodPatch, "/api/v1/users/" + target.String() + "/net-tools", `{"enabled":true}`},
		{http.MethodPut, "/api/v1/agents/" + agent.AgentID + "/tools", `{"enabled":true}`},
	} {
		if w := toolRequest(r, rt.method, rt.path, rt.body); w.Code != http.StatusForbidden {
			t.Errorf("%s %s as a grant holder: status %d, want 403", rt.method, rt.path, w.Code)
		}
	}
	var granted, switched bool
	testdb.Must(t, db.Raw(`SELECT net_tools FROM users WHERE id = ?`, target).Scan(&granted).Error)
	testdb.Must(t, db.Raw(`SELECT tools_enabled FROM agents WHERE id = ?`, agent.ID).Scan(&switched).Error)
	if got := storedSettings(t, db); len(got.Allowlist) != 0 || granted || switched || len(audit.calls) != 0 {
		t.Errorf("a refused request changed something: allowlist %v, grant %v, switch %v, audit %d",
			got.Allowlist, granted, switched, len(audit.calls))
	}
}
