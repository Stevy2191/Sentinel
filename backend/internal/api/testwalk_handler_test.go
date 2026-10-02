package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type fakeTestWalker struct {
	calls int
	oid   string
	err   error
}

func (f *fakeTestWalker) TestWalkDevice(_ context.Context, _ *services.DeviceView, oid string) (*services.TestWalkResult, error) {
	f.calls++
	f.oid = oid
	if f.err != nil {
		return nil, f.err
	}
	return &services.TestWalkResult{OID: oid}, nil
}

func newTestWalkRig(dev services.DeviceView, levels fakeSiteLevels) (*gin.Engine, *fakeTestWalker) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	walker := &fakeTestWalker{}
	RegisterTestWalkRoutes(r.Group("/api/v1"), &fakeDevices{device: dev}, levels, walker)
	return r, walker
}

func TestTestWalkRouteAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	base := "/api/v1/devices/" + dev.ID.String() + "/test-walk"
	body := map[string]any{"oid": "1.3.6.1.2.1.1.1"}

	r, walker := newTestWalkRig(dev, fakeSiteLevels{})
	if w := do(r, http.MethodPost, base, body); w.Code != http.StatusNotFound || walker.calls != 0 {
		t.Errorf("no access: %d calls %d, want 404 and no call", w.Code, walker.calls)
	}

	r, walker = newTestWalkRig(dev, fakeSiteLevels{site: services.SiteAccessReadonly})
	if w := do(r, http.MethodPost, base, body); w.Code != http.StatusForbidden || walker.calls != 0 {
		t.Errorf("readonly: %d calls %d, want 403 and no call", w.Code, walker.calls)
	}

	r, walker = newTestWalkRig(dev, fakeSiteLevels{site: services.SiteAccessEditable})
	if w := do(r, http.MethodPost, base, map[string]any{"oid": "not-an-oid"}); w.Code != http.StatusBadRequest {
		t.Errorf("bad oid: %d, want 400", w.Code)
	}
	if w := do(r, http.MethodPost, base, body); w.Code != http.StatusOK || walker.calls != 1 || walker.oid != "1.3.6.1.2.1.1.1" {
		t.Errorf("editor: %d calls %d oid %q, want 200, 1 call with that oid", w.Code, walker.calls, walker.oid)
	}
}

// An SNMP failure (bad credential, unreachable device, agent refusal) is a
// 200 carrying ok:false, the same shape Test connection uses: the server did
// its job; the device just did not answer.
func TestTestWalkSNMPFailureIsA200(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	base := "/api/v1/devices/" + dev.ID.String() + "/test-walk"

	r, walker := newTestWalkRig(dev, fakeSiteLevels{site: services.SiteAccessEditable})
	walker.err = errors.New("request timeout")
	w := do(r, http.MethodPost, base, map[string]any{"oid": "1.3.6.1.2.1.1.1"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	var resp struct {
		Data struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.OK || resp.Data.Error != "request timeout" {
		t.Errorf("body %+v", resp)
	}
}
