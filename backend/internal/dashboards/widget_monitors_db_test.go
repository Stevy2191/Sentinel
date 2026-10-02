package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBMonitorsWidget(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := monitorsWidget{monitors: d.Monitors, checks: d.Checks, incidents: d.Incidents, agents: d.Agents}
	owner := testdb.NewUser(t, db, true)
	m1 := newMonitor(t, db, owner, "Website", "https://intranet.secret.test")
	m2 := newMonitor(t, db, owner, "Mail", "https://mail.secret.test")
	testdb.Exec(t, db, `UPDATE monitors SET enabled = false WHERE id = ?`, m2)
	agent := newAgent(t, db, "fileserver")
	now := time.Now().UTC()
	testdb.Exec(t, db, `INSERT INTO checks (monitor_id, status, response_time_ms, timestamp) VALUES (?, 'success', 40, ?), (?, 'failed', 0, ?)`,
		m1, now.Add(-2*time.Hour), m1, now.Add(-90*time.Minute))
	in := ResolveInput{Visible: Subjects{Monitors: []uuid.UUID{m2, m1}, Agents: []uuid.UUID{agent}}, Now: now}

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"monitors":["%s","%s"],"agents":["%s"],"style":"bars","window":"24h"}`, m2, m1, agent), in)
	testdb.Must(t, err)
	md := data.(MonitorsData)
	if len(md.Monitors) != 2 || md.Monitors[0].Name != "Mail" || md.Monitors[0].Status != "paused" || len(md.Monitors[1].Buckets) != 24 {
		t.Errorf("monitors = %+v, want config order, paused for the disabled one, 24 hourly buckets", md.Monitors)
	}
	if len(md.Agents) != 1 || md.Agents[0].Name != "fileserver" {
		t.Errorf("agents = %+v", md.Agents)
	}
	in.Viewer = PublicViewer
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"monitors":["%s"],"style":"bars","window":"90d"}`, m1), in)
	testdb.Must(t, err)
	raw, _ := json.Marshal(data)
	if containsAny(string(raw), "secret.test", m1.String()) {
		t.Errorf("public monitors data leaks the URL or id: %s", raw)
	}
	if b := data.(MonitorsData).Monitors[0].Buckets; len(b) != 90 {
		t.Errorf("90-day window has %d buckets, want 90", len(b))
	}
	clean, _ := w.Validate(context.Background(), json.RawMessage(fmt.Sprintf(`{"monitors":["%s"],"style":"bars","window":"90d"}`, m1)))
	if w.Refresh(clean, "") != 15*time.Minute {
		t.Error("90-day bars should refresh every 15 minutes")
	}
}
