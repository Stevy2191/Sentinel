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
