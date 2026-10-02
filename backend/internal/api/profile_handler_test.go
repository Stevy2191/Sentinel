package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// fakeProfiles is an in-memory profileStore. setCalls/setMode record the
// last SetDeviceProfile call so a test can check what reached the service.
type fakeProfiles struct {
	setCalls int
	setMode  string
}

func (f *fakeProfiles) List(context.Context) ([]services.ProfileView, error) {
	return []services.ProfileView{}, nil
}
func (f *fakeProfiles) Get(context.Context, uuid.UUID) (*services.ProfileDetail, error) {
	return &services.ProfileDetail{}, nil
}
func (f *fakeProfiles) Create(context.Context, services.ProfileInput) (*models.MetricProfile, error) {
	return &models.MetricProfile{ID: uuid.New()}, nil
}
func (f *fakeProfiles) Update(_ context.Context, id uuid.UUID, _ services.ProfileInput) (*models.MetricProfile, *models.MetricProfile, error) {
	return &models.MetricProfile{ID: id}, &models.MetricProfile{ID: id}, nil
}
func (f *fakeProfiles) Delete(_ context.Context, id uuid.UUID) (*models.MetricProfile, error) {
	return &models.MetricProfile{ID: id}, nil
}
func (f *fakeProfiles) Copy(context.Context, uuid.UUID) (*services.ProfileDetail, error) {
	return &services.ProfileDetail{MetricProfile: models.MetricProfile{ID: uuid.New()}}, nil
}
func (f *fakeProfiles) GetMetric(context.Context, uuid.UUID) (*models.ProfileMetric, error) {
	return &models.ProfileMetric{Key: "acme_fan_state"}, nil
}

// CreateMetric runs the real validation, so a handler test can prove an
// invalid body never reaches the service as a "valid" metric.
func (f *fakeProfiles) CreateMetric(_ context.Context, profileID uuid.UUID, m models.ProfileMetric) (*models.ProfileMetric, error) {
	if err := services.ValidateMetric(m); err != nil {
		return nil, err
	}
	m.ProfileID = profileID
	return &m, nil
}
func (f *fakeProfiles) UpdateMetric(_ context.Context, id uuid.UUID, _ models.ProfileMetric) (*models.ProfileMetric, *models.ProfileMetric, error) {
	return &models.ProfileMetric{ID: id}, &models.ProfileMetric{ID: id}, nil
}
func (f *fakeProfiles) DeleteMetric(_ context.Context, id uuid.UUID) (*models.ProfileMetric, error) {
	return &models.ProfileMetric{ID: id}, nil
}
func (f *fakeProfiles) MetricDataDevices(context.Context, string) (int, error) { return 0, nil }
func (f *fakeProfiles) ProfilesForDevice(context.Context, models.Device) ([]services.ProfileWithMetrics, error) {
	return []services.ProfileWithMetrics{}, nil
}
func (f *fakeProfiles) DeviceProfiles(context.Context, models.Device) ([]services.DeviceProfileView, error) {
	return []services.DeviceProfileView{}, nil
}
func (f *fakeProfiles) SetDeviceProfile(_ context.Context, _, _ uuid.UUID, mode string) error {
	f.setCalls++
	f.setMode = mode
	return nil
}

// profileRouter mounts the profile routes as a signed-in user with the given
// admin claim. stubUsers (auth_middleware_test.go) confirms the claim.
func profileRouter(profiles *fakeProfiles, devs *fakeDevices, levels fakeSiteLevels, audit *fakeAudit, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterProfileRoutes(r.Group("/api/v1"), profiles, devs, levels, audit, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

func TestProfileRoutesAdminOnly(t *testing.T) {
	r := profileRouter(&fakeProfiles{}, &fakeDevices{}, fakeSiteLevels{}, &fakeAudit{}, false)
	if w := do(r, http.MethodGet, "/api/v1/network/profiles", nil); w.Code != http.StatusForbidden {
		t.Errorf("non-admin list: %d, want 403", w.Code)
	}
}

func TestProfileCreateMetricValidation(t *testing.T) {
	r := profileRouter(&fakeProfiles{}, &fakeDevices{}, fakeSiteLevels{}, &fakeAudit{}, true)
	body := map[string]any{"name": "Bad", "key": "Not Valid", "source": "column", "kind": "gauge", "oid": "1.3.6.1.2.1.1.3", "scale": 1}
	path := "/api/v1/network/profiles/" + uuid.New().String() + "/metrics"
	w := do(r, http.MethodPost, path, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid metric: %d, want 400, body %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Error("expected a validation message in the response body")
	}
}

func TestDeviceProfileAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	devs := &fakeDevices{device: dev}
	profiles := &fakeProfiles{}
	base := "/api/v1/devices/" + dev.ID.String() + "/profiles"
	putPath := base + "/" + uuid.New().String()

	r := profileRouter(profiles, devs, fakeSiteLevels{}, &fakeAudit{}, false)
	if w := do(r, http.MethodGet, base, nil); w.Code != http.StatusNotFound {
		t.Errorf("no access GET: %d, want 404", w.Code)
	}
	if w := do(r, http.MethodPut, putPath, map[string]any{"mode": "attach"}); w.Code != http.StatusNotFound {
		t.Errorf("no access PUT: %d, want 404", w.Code)
	}

	r = profileRouter(profiles, devs, fakeSiteLevels{site: services.SiteAccessReadonly}, &fakeAudit{}, false)
	if w := do(r, http.MethodGet, base, nil); w.Code != http.StatusOK {
		t.Errorf("readonly GET: %d, want 200", w.Code)
	}
	if w := do(r, http.MethodPut, putPath, map[string]any{"mode": "attach"}); w.Code != http.StatusForbidden || profiles.setCalls != 0 {
		t.Errorf("readonly PUT: %d, calls %d", w.Code, profiles.setCalls)
	}

	r = profileRouter(profiles, devs, fakeSiteLevels{site: services.SiteAccessEditable}, &fakeAudit{}, false)
	if w := do(r, http.MethodPut, putPath, map[string]any{"mode": "attach"}); w.Code != http.StatusOK || profiles.setCalls != 1 || profiles.setMode != "attach" {
		t.Errorf("editable PUT: %d, calls %d, mode %q, body %s", w.Code, profiles.setCalls, profiles.setMode, w.Body.String())
	}
}
