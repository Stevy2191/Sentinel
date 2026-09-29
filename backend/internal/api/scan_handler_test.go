package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type fakeScans struct {
	startErr error
	started  int
}

func (f *fakeScans) Start(site uuid.UUID, cidr string, creds []services.ScanCredential, _ map[string]bool) (services.ScanJob, error) {
	if f.startErr != nil {
		return services.ScanJob{}, f.startErr
	}
	f.started++
	return services.ScanJob{ID: uuid.New(), SiteID: site, CIDR: cidr, Running: true}, nil
}
func (f *fakeScans) Get(uuid.UUID, uuid.UUID) (services.ScanJob, error) {
	return services.ScanJob{}, services.ErrScanNotFound
}

type fakeScanCreds struct{ ids []uuid.UUID }

func (f fakeScanCreds) ForSite(context.Context, uuid.UUID) ([]services.CredentialOption, error) {
	out := []services.CredentialOption{}
	for _, id := range f.ids {
		out = append(out, services.CredentialOption{ID: id, Name: "p"})
	}
	return out, nil
}
func (f fakeScanCreds) Decrypted(context.Context, uuid.UUID) (snmp.Credential, error) {
	return snmp.Credential{Version: "2c", Community: "public"}, nil
}

type fakeScanDevices struct{ added int }

func (f *fakeScanDevices) HostsInSite(context.Context, uuid.UUID) (map[string]bool, error) {
	return nil, nil
}
func (f *fakeScanDevices) CreateMany(_ context.Context, _ uuid.UUID, in []models.DeviceInput, _ uuid.UUID) ([]models.Device, []services.BulkAddError) {
	f.added += len(in)
	return make([]models.Device, len(in)), nil
}

func scanRouter(scans *fakeScans, devs *fakeScanDevices, creds fakeScanCreds, level services.SiteAccessLevel) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	RegisterScanRoutes(r.Group("/api/v1"), scans, creds, devs, fakeSiteAccess{level}, &fakeAudit{})
	return r
}

func TestScanRoutes(t *testing.T) {
	site := uuid.New().String()
	cred := uuid.New()
	start := "/api/v1/sites/" + site + "/scans"
	add := "/api/v1/sites/" + site + "/devices"

	// Readonly users can neither scan nor bulk-add.
	scans, devs := &fakeScans{}, &fakeScanDevices{}
	r := scanRouter(scans, devs, fakeScanCreds{ids: []uuid.UUID{cred}}, services.SiteAccessReadonly)
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24"}); w.Code != http.StatusForbidden {
		t.Errorf("readonly scan: %d", w.Code)
	}
	if w := do(r, http.MethodPost, add, map[string]any{"devices": []map[string]any{{"host": "10.0.0.2", "credential_id": cred}}}); w.Code != http.StatusForbidden || devs.added != 0 {
		t.Errorf("readonly add: %d, added %d", w.Code, devs.added)
	}

	// Editable: default is every profile available to the site; a profile
	// from elsewhere is refused.
	r = scanRouter(scans, devs, fakeScanCreds{ids: []uuid.UUID{cred}}, services.SiteAccessEditable)
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24"}); w.Code != http.StatusAccepted || scans.started != 1 {
		t.Errorf("editable scan: %d, started %d", w.Code, scans.started)
	}
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24", "credential_ids": []uuid.UUID{uuid.New()}}); w.Code != http.StatusBadRequest {
		t.Errorf("foreign profile: %d, want 400", w.Code)
	}
	if w := do(r, http.MethodGet, start+"/"+uuid.New().String(), nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown job: %d, want 404", w.Code)
	}
	if w := do(r, http.MethodPost, add, map[string]any{"devices": []map[string]any{{"host": "10.0.0.2", "credential_id": cred}}}); w.Code != http.StatusOK || devs.added != 1 {
		t.Errorf("bulk add: %d, added %d", w.Code, devs.added)
	}

	busy := &fakeScans{startErr: services.ErrScanRunning}
	r = scanRouter(busy, devs, fakeScanCreds{ids: []uuid.UUID{cred}}, services.SiteAccessEditable)
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24"}); w.Code != http.StatusConflict {
		t.Errorf("scan running: %d, want 409", w.Code)
	}
}
