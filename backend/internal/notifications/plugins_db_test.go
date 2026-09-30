package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// Retrying a device alert has no monitor to rebuild a message from (the
// stored record's monitor_id is null), and there is no "retry" concept for a
// device or agent event - it must error clearly rather than fail obscurely
// trying to load a monitor that was never there.
func TestDBRetryNotificationRejectsDeviceRecord(t *testing.T) {
	db := testdb.Open(t)
	site := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'HQ')`, site)
	cred := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'cred', '2c', 'x')`, cred)
	deviceID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'core-sw-1', '10.0.0.2')`,
		deviceID, site, cred)

	notifID := uuid.New()
	testdb.Must(t, db.Create(&models.Notification{
		ID: notifID, DeviceID: &deviceID, Channel: "slack", Status: statusFailed, ErrorMessage: "boom",
	}).Error)

	m := NewNotificationManager(db)
	err := m.RetryNotification(context.Background(), notifID)
	if !errors.Is(err, ErrNotificationNotRetryable) {
		t.Fatalf("got %v, want ErrNotificationNotRetryable", err)
	}
}

// A member must see their own monitor's notification rows and a shared
// site's device rows, but never another site's device rows and never a
// server-agent row (agents are admin-only), the same rule device incidents
// already follow.
func TestDBListNotificationsScopesDeviceRowsBySiteSharing(t *testing.T) {
	db := testdb.Open(t)
	member := testdb.NewUser(t, db, false)

	// A monitor the member owns directly.
	ownMonitor := uuid.New()
	testdb.Exec(t, db, `INSERT INTO monitors (id, name, type, url, owner_id) VALUES (?, 'own', 'http', 'http://own.example', ?)`,
		ownMonitor, member)
	ownNotif := uuid.New()
	testdb.Must(t, db.Create(&models.Notification{
		ID: ownNotif, MonitorID: &ownMonitor, Channel: "slack", Status: statusFailed,
	}).Error)

	cred := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'cred', '2c', 'x')`, cred)

	// A site shared with the member, and a device notification in it.
	sharedSite := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Shared HQ')`, sharedSite)
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id) VALUES (?, ?)`, sharedSite, member)
	sharedDevice := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'shared-sw', '10.0.0.2')`,
		sharedDevice, sharedSite, cred)
	sharedNotif := uuid.New()
	testdb.Must(t, db.Create(&models.Notification{
		ID: sharedNotif, DeviceID: &sharedDevice, Channel: "slack", Status: statusFailed,
	}).Error)

	// A different site NOT shared with the member, and its device notification.
	otherSite := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Other Branch')`, otherSite)
	otherDevice := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'other-sw', '10.0.0.3')`,
		otherDevice, otherSite, cred)
	testdb.Must(t, db.Create(&models.Notification{
		ID: uuid.New(), DeviceID: &otherDevice, Channel: "slack", Status: statusFailed,
	}).Error)

	// A server-agent alert - admin-only, must never appear for a member.
	agentID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO agents (id, name, agent_id, server_token) VALUES (?, 'agent1', 'agent-scope-test', 'tok-scope-test')`, agentID)
	testdb.Must(t, db.Create(&models.Notification{
		ID: uuid.New(), AgentID: &agentID, Channel: "slack", Status: statusFailed,
	}).Error)

	m := NewNotificationManager(db)
	records, total, err := m.ListNotifications(context.Background(), ListNotificationsOptions{
		Viewer: &NotificationViewer{UserID: member, IsAdmin: false},
		Limit:  50,
	})
	testdb.Must(t, err)

	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	got := map[uuid.UUID]bool{}
	for _, r := range records {
		got[r.ID] = true
	}
	if !got[ownNotif] {
		t.Error("missing the member's own monitor notification row")
	}
	if !got[sharedNotif] {
		t.Error("missing the shared site's device notification row")
	}
	if len(records) != 2 {
		t.Errorf("got %d records, want exactly 2: %+v", len(records), records)
	}
}
