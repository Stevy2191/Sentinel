package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The heartbeat's tools_local reaches agents.tools_local: true, false, and
// NULL when an older agent leaves it out.
func TestDBHeartbeatHandlerToolsLocal(t *testing.T) {
	db := testdb.Open(t)
	agents := services.NewAgentService(db)
	agent := &models.Agent{Name: "file-server", OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, agents.Register(context.Background(), agent))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterAgentIngestRoutes(r, agents)
	beat := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/heartbeat", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+agent.ServerToken)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("heartbeat %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	stored := func() *bool {
		t.Helper()
		var v *bool
		testdb.Must(t, db.Raw(`SELECT tools_local FROM agents WHERE id = ?`, agent.ID).Scan(&v).Error)
		return v
	}

	beat(`{"agent_id":"` + agent.AgentID + `","tools_local":true}`)
	if v := stored(); v == nil || !*v {
		t.Errorf("tools_local true stored as %v", v)
	}
	beat(`{"agent_id":"` + agent.AgentID + `","tools_local":false}`)
	if v := stored(); v == nil || *v {
		t.Errorf("tools_local false stored as %v", v)
	}
	beat(`{"agent_id":"` + agent.AgentID + `","agent_version":"1.0.0"}`)
	if v := stored(); v != nil {
		t.Errorf("a heartbeat without tools_local stored %v, want NULL", *v)
	}
}

// GET /auth/me carries net_tools, so the frontend knows whether to show the
// tools.
func TestDBMeIncludesNetTools(t *testing.T) {
	db := testdb.Open(t)
	auth := services.NewAuthService(db, "0123456789abcdef0123456789abcdef")
	id := testdb.NewUser(t, db, false)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", id)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	r.GET("/me", GetCurrentUserHandler(auth))
	me := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
		var body struct {
			Data map[string]any `json:"data"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatalf("/me: %d %s", w.Code, w.Body.String())
		}
		return body.Data
	}

	if got := me()["net_tools"]; got != false {
		t.Errorf("net_tools = %v, want false", got)
	}
	_, err := auth.SetNetTools(context.Background(), id, true)
	testdb.Must(t, err)
	if got := me()["net_tools"]; got != true {
		t.Errorf("net_tools after the grant = %v, want true", got)
	}
}

// GET /users tells an admin who holds the network tools grant (for the
// AdminUsers checkbox) and tells a non-admin nothing about it.
func TestDBUsersListNetTools(t *testing.T) {
	db := testdb.Open(t)
	auth := services.NewAuthService(db, "0123456789abcdef0123456789abcdef")
	granted := testdb.NewUser(t, db, false)
	_, err := auth.SetNetTools(context.Background(), granted, true)
	testdb.Must(t, err)

	list := func(admin bool) []map[string]any {
		t.Helper()
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", granted)
			c.Set("username", "someone")
			c.Set("is_admin", admin)
			c.Next()
		})
		r.GET("/users", ListUsersHandler(auth))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
		var body struct {
			Data []map[string]any `json:"data"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Data) != 1 {
			t.Fatalf("/users: %d %s", w.Code, w.Body.String())
		}
		return body.Data
	}
	if got := list(true)[0]["net_tools"]; got != true {
		t.Errorf("admin view: net_tools = %v, want true", got)
	}
	if _, ok := list(false)[0]["net_tools"]; ok {
		t.Error("a non-admin sees net_tools")
	}
}
