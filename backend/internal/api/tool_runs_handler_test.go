package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// toolErrorBody decodes the coded error envelope of the tool endpoints.
func toolErrorBody(t *testing.T, body []byte) (code, message string) {
	t.Helper()
	var env struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if env.Success {
		t.Errorf("body %s: success should be false", body)
	}
	return env.Error.Code, env.Error.Message
}

// guardedRouter mounts RequireNetTools in front of a handler that records
// the requester it was given. The token claims claimAdmin; users is what the
// database says.
func guardedRouter(users adminChecker, claimAdmin bool, ran *bool, who *toolruns.Requester) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", claimAdmin)
		c.Next()
	})
	r.GET("/thing", RequireNetTools(users), func(c *gin.Context) {
		*ran = true
		*who = requesterFrom(c)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

// A refused request must not reach the handler it guards, and the refusal is
// the coded 403 the frontend reads.
func TestRequireNetToolsDoesNotRunHandlerWhenRefused(t *testing.T) {
	cases := []struct {
		name       string
		claimAdmin bool
		users      stubUsers
		want       int
		wantAdmin  bool
	}{
		{"neither admin nor granted", false, stubUsers{user: &models.User{Username: "u"}}, http.StatusForbidden, false},
		{"admin claim the account no longer has", true, stubUsers{user: &models.User{Username: "u"}}, http.StatusForbidden, false},
		{"account cannot be read", true, stubUsers{err: context.DeadlineExceeded}, http.StatusForbidden, false},
		{"granted", false, stubUsers{user: &models.User{Username: "u", NetTools: true}}, http.StatusOK, false},
		{"admin without the grant", false, stubUsers{user: &models.User{Username: "u", IsAdmin: true}}, http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			var who toolruns.Requester
			w := httptest.NewRecorder()
			guardedRouter(tc.users, tc.claimAdmin, &ran, &who).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/thing", nil))
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
			if ran != (tc.want == http.StatusOK) {
				t.Errorf("handler ran = %v: a refused request must not reach it", ran)
			}
			if tc.want == http.StatusForbidden {
				if code, msg := toolErrorBody(t, w.Body.Bytes()); code != "forbidden" || msg != "network tools access required" {
					t.Errorf("refusal = %q %q, want forbidden / network tools access required", code, msg)
				}
				return
			}
			// The admin flag handlers see comes from the database, not the
			// token's claim.
			if who.IsAdmin != tc.wantAdmin || who.Username != "someone" {
				t.Errorf("requester = %+v, want IsAdmin %v", who, tc.wantAdmin)
			}
		})
	}
}

// A grant an admin removes stops working on the very next request, however
// fresh the user's token.
func TestRequireNetToolsRereadsTheGrantEveryRequest(t *testing.T) {
	users := &stubUsers{user: &models.User{Username: "u", NetTools: true}}
	ran := false
	var who toolruns.Requester
	r := guardedRouter(users, false, &ran, &who)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/thing", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("with the grant: status %d", w.Code)
	}
	users.user = &models.User{Username: "u", NetTools: false}
	ran = false
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/thing", nil))
	if w.Code != http.StatusForbidden || ran {
		t.Errorf("after the grant was removed: status %d, handler ran %v; want 403 and not run", w.Code, ran)
	}
}

// toolRouter mounts the tool routes for one signed-in user, with no service
// behind them: these tests only reach paths that answer before it is needed.
func toolRouter(users adminChecker, audit auditRecorder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	user := uuid.New()
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	registerToolRoutes(v1, &toolRunsHandler{}, users, audit)
	return r
}

// toolRequest sends a raw JSON body.
func toolRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Every tool route is guarded. A refused run request is audited as
// tool_run_refused; refused reads are not, so they cannot flood the log.
func TestToolRoutesRefuseUsersWithoutTheGrant(t *testing.T) {
	audit := &fakeAudit{}
	r := toolRouter(stubUsers{user: &models.User{Username: "u"}}, audit)
	id := uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tools/vantages"},
		{http.MethodGet, "/api/v1/tools/runs"},
		{http.MethodGet, "/api/v1/tools/runs/" + id},
		{http.MethodGet, "/api/v1/tools/runs/" + id + "/events"},
		{http.MethodPost, "/api/v1/tools/runs/" + id + "/cancel"},
	} {
		if w := toolRequest(r, rt.method, rt.path, ""); w.Code != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", rt.method, rt.path, w.Code)
		}
	}
	if len(audit.entries) != 0 {
		t.Errorf("refused reads were audited: %v", audit.entries)
	}
	w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{"tool":"ping","vantage":{"kind":"sentinel"},"target":"10.0.0.5"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST /tools/runs: status %d, want 403", w.Code)
	}
	if len(audit.entries) != 1 || audit.entries[0] != models.ActionToolRunRefused {
		t.Errorf("audit = %v, want one %s", audit.entries, models.ActionToolRunRefused)
	}
}

func TestCreateToolRunRejectsABodyThatIsNotJSON(t *testing.T) {
	r := toolRouter(stubUsers{user: &models.User{Username: "u", NetTools: true}}, &fakeAudit{})
	w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{"tool":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if code, _ := toolErrorBody(t, w.Body.Bytes()); code != "invalid_params" {
		t.Errorf("code %q, want invalid_params", code)
	}
}

// 30 runs a minute per user with a burst of 10: the eleventh request in a row
// is throttled before it reaches the handler.
func TestCreateToolRunIsRateLimited(t *testing.T) {
	r := toolRouter(stubUsers{user: &models.User{Username: "u", NetTools: true}}, &fakeAudit{})
	for i := 1; i <= 10; i++ {
		// Malformed on purpose: the handler answers 400 before it needs the
		// service, but each request still spends a token.
		if w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{`); w.Code != http.StatusBadRequest {
			t.Fatalf("request %d: status %d, want 400", i, w.Code)
		}
	}
	if w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{`); w.Code != http.StatusTooManyRequests {
		t.Errorf("request 11: status %d, want 429", w.Code)
	}
}

func TestWriteRefusalUsesTheRefusalsStatusAndCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	refusal := &toolruns.Refusal{Status: http.StatusUnprocessableEntity, Code: toolruns.CodeTargetNotAllowed,
		Message: "10.9.9.9 (10.9.9.9) is not on the network tools allowlist"}
	if !writeRefusal(c, refusal) {
		t.Fatal("a *Refusal was not written")
	}
	if w.Code != http.StatusUnprocessableEntity || !c.IsAborted() {
		t.Errorf("status %d, aborted %v; want 422 and aborted", w.Code, c.IsAborted())
	}
	if code, msg := toolErrorBody(t, w.Body.Bytes()); code != "target_not_allowed" || msg != refusal.Message {
		t.Errorf("body = %q %q", code, msg)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	if writeRefusal(c, errors.New("database is down")) {
		t.Error("a plain error was answered as a refusal")
	}
}

func TestLastEventID(t *testing.T) {
	for raw, want := range map[string]int{"": 0, "7": 7, " 12 ": 12, "-3": 0, "abc": 0} {
		if got := lastEventID(raw); got != want {
			t.Errorf("lastEventID(%q) = %d, want %d", raw, got, want)
		}
	}
}
