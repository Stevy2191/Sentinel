package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
)

// post issues a request with no body, mirroring the get helper in
// incident_access_test.go.
func post(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
	return w
}

// -- history --------------------------------------------------------------

// fakeNotificationLister records what the history handler asked for.
type fakeNotificationLister struct {
	listOpts *notifications.ListNotificationsOptions
}

func (f *fakeNotificationLister) ListNotifications(_ context.Context, opts notifications.ListNotificationsOptions) ([]models.Notification, int64, error) {
	f.listOpts = &opts
	return nil, 0, nil
}

type fakeMonitorNamer struct{}

func (fakeMonitorNamer) ListAccessibleMonitors(context.Context, uuid.UUID, bool, map[string]interface{}) ([]models.Monitor, error) {
	return nil, nil
}

type fakeAgentNamer struct{ called bool }

func (f *fakeAgentNamer) List(context.Context) ([]models.Agent, error) {
	f.called = true
	return nil, nil
}

func historyRouter(lister notificationLister, monitors notificationMonitorNamer, agents notificationAgentNamer, userID uuid.UUID, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	r.GET("/notifications/history", GetNotificationHistoryHandler(lister, monitors, agents))
	return r
}

// The history handler must tell the manager who is asking, so it can filter
// to the monitors that user owns or has been shared. Both admins and members
// must be passed through as a viewer - a nil viewer is what let every user see
// every record.
func TestGetNotificationHistoryPassesViewer(t *testing.T) {
	for _, isAdmin := range []bool{false, true} {
		user := uuid.New()
		lister := &fakeNotificationLister{}
		w := get(historyRouter(lister, fakeMonitorNamer{}, &fakeAgentNamer{}, user, isAdmin), "/notifications/history")
		if w.Code != http.StatusOK {
			t.Fatalf("admin=%v: status %d, want 200", isAdmin, w.Code)
		}
		if lister.listOpts == nil || lister.listOpts.Viewer == nil {
			t.Fatalf("admin=%v: list called without a viewer", isAdmin)
		}
		if lister.listOpts.Viewer.UserID != user || lister.listOpts.Viewer.IsAdmin != isAdmin {
			t.Errorf("admin=%v: viewer = %+v, want user %s admin %v", isAdmin, *lister.listOpts.Viewer, user, isAdmin)
		}
	}
}

// Agent names are admin-only, because agents are admin-only: a member's
// request must never resolve them, even though the endpoint still works for
// their own rows.
func TestGetNotificationHistoryResolvesAgentNamesOnlyForAdmins(t *testing.T) {
	agents := &fakeAgentNamer{}
	w := get(historyRouter(&fakeNotificationLister{}, fakeMonitorNamer{}, agents, uuid.New(), false), "/notifications/history")
	if w.Code != http.StatusOK {
		t.Fatalf("member: status %d, want 200", w.Code)
	}
	if agents.called {
		t.Error("member: agent names were resolved")
	}

	agents = &fakeAgentNamer{}
	w = get(historyRouter(&fakeNotificationLister{}, fakeMonitorNamer{}, agents, uuid.New(), true), "/notifications/history")
	if w.Code != http.StatusOK {
		t.Fatalf("admin: status %d, want 200", w.Code)
	}
	if !agents.called {
		t.Error("admin: agent names were not resolved")
	}
}

// -- retry ------------------------------------------------------------------

// fakeNotificationRetryStore stands in for the notification manager: it loads
// a fixed record and records whether a retry was actually attempted.
type fakeNotificationRetryStore struct {
	record      *models.Notification
	loadErr     error
	retryCalled bool
	retryErr    error
}

func (f *fakeNotificationRetryStore) GetNotificationByID(context.Context, uuid.UUID) (*models.Notification, error) {
	return f.record, f.loadErr
}

func (f *fakeNotificationRetryStore) RetryNotification(context.Context, uuid.UUID) error {
	f.retryCalled = true
	return f.retryErr
}

type fakeMonitorAccess struct {
	canView bool
	canEdit bool
}

func (f fakeMonitorAccess) CanUserViewMonitor(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.canView, nil
}

func (f fakeMonitorAccess) CanUserEditMonitor(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.canEdit, nil
}

func retryRouter(store notificationRetryStore, monitors monitorAccessChecker, userID uuid.UUID, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	r.POST("/notifications/retry/:notification_id", RetryFailedNotificationHandler(store, monitors))
	return r
}

// A notification on a monitor the caller cannot see answers exactly like one
// that does not exist, and no retry is attempted. One the caller can see but
// not edit answers 403, also without retrying. A server-agent record (no
// monitor at all) is admin-only, same as agents themselves. Only a caller who
// can edit the monitor - or an admin - actually triggers a retry.
func TestRetryFailedNotificationAccess(t *testing.T) {
	monitorID := uuid.New()
	monitorRecord := &models.Notification{ID: uuid.New(), MonitorID: &monitorID, Status: "failed"}
	agentID := uuid.New()
	agentRecord := &models.Notification{ID: uuid.New(), AgentID: &agentID, Status: "failed"}

	path := func(rec *models.Notification) string { return "/notifications/retry/" + rec.ID.String() }

	cases := []struct {
		name        string
		record      *models.Notification
		isAdmin     bool
		access      fakeMonitorAccess
		wantCode    int
		wantRetried bool
	}{
		{
			name:        "member cannot view the monitor",
			record:      monitorRecord,
			access:      fakeMonitorAccess{canView: false, canEdit: false},
			wantCode:    http.StatusNotFound,
			wantRetried: false,
		},
		{
			name:        "member can view but not edit",
			record:      monitorRecord,
			access:      fakeMonitorAccess{canView: true, canEdit: false},
			wantCode:    http.StatusForbidden,
			wantRetried: false,
		},
		{
			name:        "member can edit",
			record:      monitorRecord,
			access:      fakeMonitorAccess{canView: true, canEdit: true},
			wantCode:    http.StatusOK,
			wantRetried: true,
		},
		{
			name:        "agent record for a member",
			record:      agentRecord,
			access:      fakeMonitorAccess{canView: true, canEdit: true}, // must not even be consulted
			wantCode:    http.StatusNotFound,
			wantRetried: false,
		},
		{
			name:        "admin",
			record:      agentRecord,
			isAdmin:     true,
			access:      fakeMonitorAccess{canView: false, canEdit: false}, // must not be consulted
			wantCode:    http.StatusOK,
			wantRetried: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeNotificationRetryStore{record: tc.record}
			w := post(retryRouter(store, tc.access, uuid.New(), tc.isAdmin), path(tc.record))
			if w.Code != tc.wantCode {
				t.Errorf("status %d, want %d (body %s)", w.Code, tc.wantCode, w.Body.String())
			}
			if store.retryCalled != tc.wantRetried {
				t.Errorf("retryCalled = %v, want %v", store.retryCalled, tc.wantRetried)
			}
		})
	}
}

// -- test send ----------------------------------------------------------

// Sending a test notification is admin-only, mounted behind RequireAdmin.
// A rejected request must never reach the handler.
func TestSendTestNotificationRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("non-admin", func(t *testing.T) {
		handlerRan := false
		users := stubUsers{user: &models.User{IsAdmin: false}}
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", uuid.New())
			c.Set("username", "someone")
			c.Set("is_admin", false)
			c.Next()
		})
		r.POST("/notifications/test/:channel", RequireAdmin(users), func(c *gin.Context) {
			handlerRan = true
			SendTestNotificationHandler(notifications.NewNotificationManager(nil))(c)
		})
		w := post(r, "/notifications/test/slack")
		if w.Code != http.StatusForbidden {
			t.Errorf("status %d, want 403", w.Code)
		}
		if handlerRan {
			t.Error("non-admin request reached SendTestNotificationHandler")
		}
	})

	t.Run("admin", func(t *testing.T) {
		handlerRan := false
		users := stubUsers{user: &models.User{IsAdmin: true}}
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", uuid.New())
			c.Set("username", "someone")
			c.Set("is_admin", true)
			c.Next()
		})
		r.POST("/notifications/test/:channel", RequireAdmin(users), func(c *gin.Context) {
			handlerRan = true
			SendTestNotificationHandler(notifications.NewNotificationManager(nil))(c)
		})
		w := post(r, "/notifications/test/slack")
		if !handlerRan {
			t.Error("admin request did not reach SendTestNotificationHandler")
		}
		if w.Code == http.StatusForbidden {
			t.Errorf("status %d, admin should not be forbidden", w.Code)
		}
	})
}
