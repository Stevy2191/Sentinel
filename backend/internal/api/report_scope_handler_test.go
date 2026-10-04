package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netreport"
)

// fakeScopes is a NetworkScopes that records its calls and answers with err
// (both methods) or preview.
type fakeScopes struct {
	err           error
	preview       *netreport.Preview
	validateCalls int
	previewCalls  int
	requester     uuid.UUID
	scopeType     string
	scope         models.ReportScope
}

func (f *fakeScopes) ValidateScope(_ context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error {
	f.validateCalls++
	f.requester, f.scopeType, f.scope = requester, scopeType, scope
	return f.err
}

func (f *fakeScopes) Preview(_ context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*netreport.Preview, error) {
	f.previewCalls++
	f.requester, f.scopeType, f.scope = requester, scopeType, scope
	if f.err != nil {
		return nil, f.err
	}
	return f.preview, nil
}

// reportBuilderRouter mounts the report-builder routes on h for one
// signed-in, non-admin user.
func reportBuilderRouter(h *ReportBuilder, user uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	RegisterReportBuilderRoutes(v1, h)
	return r
}

// reportScopeRouter is reportBuilderRouter with a fake scope checker and no
// database: these tests only reach paths that answer before one is needed.
func reportScopeRouter(scopes *fakeScopes, user uuid.UUID) *gin.Engine {
	h := &ReportBuilder{}
	h.SetNetworkScopes(scopes)
	return reportBuilderRouter(h, user)
}

// errorText decodes the error envelope's message.
func errorText(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if env.Success {
		t.Errorf("body %q: success should be false", body)
	}
	return env.Error
}

func TestScopePreviewSizesTheScopeAsTheCaller(t *testing.T) {
	user, site := uuid.New(), uuid.New()
	scopes := &fakeScopes{preview: &netreport.Preview{Ports: 640, Devices: 12, Capped: true,
		Metrics:  []netreport.MetricChoice{{Key: "if_in_bps", Label: "Traffic in", Unit: "bps", Source: "builtin"}},
		Defaults: []string{"if_in_bps", "if_out_bps"}}}
	w := do(reportScopeRouter(scopes, user), http.MethodPost, "/api/v1/reports/scope-preview", map[string]any{
		"scope_type": "port_roles",
		"scope_data": map[string]any{"site_ids": []string{site.String()}, "roles": []string{"wan", "uplink"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data netreport.Preview `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Ports != 640 || env.Data.Devices != 12 || !env.Data.Capped || len(env.Data.Metrics) != 1 || len(env.Data.Defaults) != 2 {
		t.Errorf("preview = %+v", env.Data)
	}
	if scopes.requester != user || scopes.scopeType != models.ScopeTypePortRoles ||
		len(scopes.scope.SiteIDs) != 1 || scopes.scope.SiteIDs[0] != site || len(scopes.scope.Roles) != 2 {
		t.Errorf("previewed %v %q %+v, want the caller's port_roles scope", scopes.requester, scopes.scopeType, scopes.scope)
	}
}

func TestScopePreviewAnswersFieldErrorsWithTheirMessage(t *testing.T) {
	scopes := &fakeScopes{err: &netreport.FieldError{Field: "port_ids", Message: netreport.MsgNotAvailable}}
	w := do(reportScopeRouter(scopes, uuid.New()), http.MethodPost, "/api/v1/reports/scope-preview", map[string]any{
		"scope_type": "ports", "scope_data": map[string]any{"port_ids": []string{uuid.NewString()}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if got := errorText(t, w.Body.String()); got != netreport.MsgNotAvailable {
		t.Errorf("error = %q, want %q", got, netreport.MsgNotAvailable)
	}
}

func TestScopePreviewHidesInternalErrors(t *testing.T) {
	scopes := &fakeScopes{err: errors.New(`pq: relation "secret_table" does not exist`)}
	w := do(reportScopeRouter(scopes, uuid.New()), http.MethodPost, "/api/v1/reports/scope-preview", map[string]any{
		"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{uuid.NewString()}},
	})
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret_table") {
		t.Errorf("status %d body %s: want a 500 without the database's text", w.Code, w.Body.String())
	}
}

func TestScopePreviewTakesOnlyNetworkScopes(t *testing.T) {
	scopes := &fakeScopes{preview: &netreport.Preview{}}
	r := reportScopeRouter(scopes, uuid.New())
	for _, body := range []map[string]any{
		{"scope_type": "monitors", "scope_data": map[string]any{"monitor_ids": []string{uuid.NewString()}}},
		{"scope_data": map[string]any{}},
	} {
		if w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", body); w.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d, want 400", body, w.Code)
		}
	}
	if scopes.previewCalls != 0 {
		t.Errorf("an invalid body reached the scope checker %d times", scopes.previewCalls)
	}
}

// The editor previews about 400 ms after each change, so the preview has
// its own bucket: twenty at once, then it throttles without reaching the
// checker.
func TestScopePreviewIsRateLimited(t *testing.T) {
	scopes := &fakeScopes{preview: &netreport.Preview{}}
	r := reportScopeRouter(scopes, uuid.New())
	body := map[string]any{"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{uuid.NewString()}}}
	for i := 0; i < 20; i++ {
		if w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", body); w.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i+1, w.Code)
		}
	}
	if w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", body); w.Code != http.StatusTooManyRequests {
		t.Errorf("21st request: status %d, want 429", w.Code)
	}
	if scopes.previewCalls != 20 {
		t.Errorf("checker ran %d times, want 20 (the throttled request must not reach it)", scopes.previewCalls)
	}
}

// metricsReportBody is a valid Metrics report request on one port.
func metricsReportBody(portID uuid.UUID) map[string]any {
	return map[string]any{
		"name": "Uplink", "report_type": "metrics", "scope_type": "ports",
		"scope_data":      map[string]any{"port_ids": []string{portID.String()}, "metrics": []string{"if_in_bps", "if_out_bps"}},
		"time_range_days": 30,
	}
}

func TestGenerateMetricsReportChecksTheScopeAsTheCreator(t *testing.T) {
	user, port := uuid.New(), uuid.New()
	scopes := &fakeScopes{err: &netreport.FieldError{Field: "port_ids", Message: netreport.MsgNotAvailable}}
	w := do(reportScopeRouter(scopes, user), http.MethodPost, "/api/v1/reports/generate", metricsReportBody(port))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if got := errorText(t, w.Body.String()); got != netreport.MsgNotAvailable {
		t.Errorf("error = %q, want %q", got, netreport.MsgNotAvailable)
	}
	if scopes.validateCalls != 1 || scopes.requester != user || scopes.scopeType != models.ScopeTypePorts ||
		len(scopes.scope.PortIDs) != 1 || scopes.scope.PortIDs[0] != port || len(scopes.scope.Metrics) != 2 {
		t.Errorf("checked %d times as %v: %q %+v", scopes.validateCalls, scopes.requester, scopes.scopeType, scopes.scope)
	}
}

func TestGenerateRefusesAScopeTypeThatDoesNotMatchTheReportType(t *testing.T) {
	scopes := &fakeScopes{}
	r := reportScopeRouter(scopes, uuid.New())
	for _, body := range []map[string]any{
		{"name": "x", "report_type": "metrics", "scope_type": "monitors",
			"scope_data": map[string]any{"monitor_ids": []string{uuid.NewString()}, "metrics": []string{"if_in_bps"}}, "time_range_days": 30},
		{"name": "x", "report_type": "uptime", "scope_type": "ports",
			"scope_data": map[string]any{"port_ids": []string{uuid.NewString()}}, "time_range_days": 30},
	} {
		if w := do(r, http.MethodPost, "/api/v1/reports/generate", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s with %s: status %d, want 400", body["report_type"], body["scope_type"], w.Code)
		}
	}
	if scopes.validateCalls != 0 {
		t.Errorf("a mismatched report reached the scope checker %d times", scopes.validateCalls)
	}
}
