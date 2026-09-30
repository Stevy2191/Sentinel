package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A device's notification history row must carry its own device_id and the
// device's name in monitor_name, not a null monitor_id/agent_id and an empty
// name - the frontend table renders `n.monitor_name || n.monitor_id.slice(...)`
// and a null monitor_id there throws.
func TestDBNotificationHistoryIncludesDeviceRow(t *testing.T) {
	db := testdb.Open(t)
	site := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'HQ')`, site)
	cred := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'cred', '2c', 'x')`, cred)
	deviceID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'core-sw-1', '10.0.0.2')`,
		deviceID, site, cred)

	testdb.Must(t, db.Create(&models.Notification{
		ID: uuid.New(), DeviceID: &deviceID, Channel: "slack", Status: "failed", ErrorMessage: "boom",
	}).Error)

	manager := notifications.NewNotificationManager(db)
	monitorService := services.NewMonitorService(db)
	agentService := services.NewAgentService(db)
	credService := services.NewSNMPCredentialService(db)
	incidentService := services.NewIncidentService(db)
	deviceService := services.NewDeviceService(db, credService, incidentService)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "admin")
		c.Set("is_admin", true)
		c.Next()
	})
	r.GET("/history", GetNotificationHistoryHandler(manager, monitorService, agentService, deviceService))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Data struct {
			Notifications []map[string]any `json:"notifications"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (body: %s)", err, w.Body.String())
	}
	if len(body.Data.Notifications) != 1 {
		t.Fatalf("got %d notifications, want 1: %+v", len(body.Data.Notifications), body.Data.Notifications)
	}
	row := body.Data.Notifications[0]
	if row["monitor_id"] != nil {
		t.Errorf("monitor_id = %v, want nil", row["monitor_id"])
	}
	if row["device_id"] != deviceID.String() {
		t.Errorf("device_id = %v, want %s", row["device_id"], deviceID)
	}
	if row["monitor_name"] != "core-sw-1" {
		t.Errorf("monitor_name = %v, want device name %q", row["monitor_name"], "core-sw-1")
	}
}
