package services

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// ChecksInRange returns what GetChecksInRange does without logging the call,
// so a dashboard refreshing 50 monitors does not fill the log.
func TestDBChecksInRangeDoesNotLog(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	mon := newMonitor(t, db, testdb.NewUser(t, db, true), "web")
	now := time.Now().UTC()
	testdb.Exec(t, db, `INSERT INTO checks (monitor_id, status, timestamp) VALUES (?, 'success', ?)`, mon, now.Add(-time.Minute))
	var buf bytes.Buffer
	s := NewCheckService(db)
	s.logger = log.New(&buf, "", 0)

	checks, err := s.ChecksInRange(ctx, mon, now.Add(-time.Hour), now, 0, 0)
	testdb.Must(t, err)
	if len(checks) != 1 || buf.Len() != 0 {
		t.Errorf("ChecksInRange: %d checks, log %q; want 1 check and no log line", len(checks), buf.String())
	}
	checks, err = s.GetChecksInRange(ctx, mon, now.Add(-time.Hour), now, 0, 0)
	testdb.Must(t, err)
	if len(checks) != 1 || !strings.Contains(buf.String(), "retrieved 1 checks") {
		t.Errorf("GetChecksInRange: %d checks, log %q; want 1 check, logged as before", len(checks), buf.String())
	}
}
