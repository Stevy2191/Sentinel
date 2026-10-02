package dashboards

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBTopNWidget(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := topNWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	p1, p2 := newPort(t, db, dev, 1, "Gi1/0/1", "printer"), newPort(t, db, dev, 2, "Gi1/0/2", "uplink")
	now := time.Now().UTC()
	writePortBps(t, d, dev, p1, 1, now.Add(-20*time.Minute), 100, 100)
	writePortBps(t, d, dev, p2, 2, now.Add(-20*time.Minute), 9000, 1000)
	in := ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now}
	data, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s","range":"1h","n":5}`, site), in)
	testdb.Must(t, err)
	tn := data.(TopNData)
	if tn.Unit != "bps" || len(tn.Items) != 2 || tn.Items[0].Alias != "uplink" || tn.Items[0].Value != 10000 || tn.Items[0].DeviceID == nil {
		t.Errorf("top-N = %+v", tn)
	}
	in.Viewer = PublicViewer
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s","range":"1h","n":5}`, site), in)
	testdb.Must(t, err)
	if data.(TopNData).Items[0].DeviceID != nil {
		t.Error("public top-N carries device ids")
	}
}

func TestDBEventLogMergesIncidentsAndPortEvents(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := eventLogWidget{ports: d.Ports, incidents: d.Incidents}
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	port := newPort(t, db, dev, 3, "Gi1/0/3", "")
	now := time.Now().UTC()
	testdb.Exec(t, db, `INSERT INTO port_events (device_id, interface_id, if_index, kind, started_at, detail) VALUES (?, ?, 3, 'link_down', ?, '{}')`,
		dev, port, now.Add(-30*time.Minute))
	end := now.Add(-5 * time.Minute)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, end_time, severity, root_cause) VALUES (?, ?, ?, 'high', 'cannot reach 10.0.0.1')`,
		dev, now.Add(-20*time.Minute), end)

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s","limit":10}`, site), ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	items := data.(EventLogData).Items
	kinds := []string{}
	for _, it := range items {
		kinds = append(kinds, it.Kind)
	}
	if fmt.Sprint(kinds) != "[incident_closed incident_opened link_down]" {
		t.Errorf("kinds newest first = %v", kinds)
	}
	raw, _ := json.Marshal(data)
	if containsAny(string(raw), "cannot reach", "10.0.0.1") {
		t.Errorf("event log leaks free text or the host: %s", raw)
	}
}

func TestDBDeviceTableAndOpenIncidents(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	member := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	shareSite(t, db, site, member, "readonly")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	newDevice(t, db, site, "ap-1", "10.0.0.20")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'access_point' WHERE name = 'ap-1'`)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, severity) VALUES (?, ?, 'high')`, dev, time.Now().UTC().Add(-time.Hour))
	other := newDevice(t, db, newSite(t, db, "Other"), "edge", "10.9.0.1")
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, severity) VALUES (?, ?, 'low')`, other, time.Now().UTC().Add(-time.Hour))

	table := deviceTableWidget{devices: d.Devices, incidents: d.Incidents}
	data, err := resolveWidget(t, table, fmt.Sprintf(`{"site_id":"%s","types":["switch","other"]}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Viewer: PublicViewer})
	testdb.Must(t, err)
	rows := data.(DeviceTableData).Devices
	if len(rows) != 1 || rows[0].Name != "core" || rows[0].OpenIncidents != 1 || rows[0].Host != "" || rows[0].DeviceID != nil {
		t.Errorf("public device table filtered to switch/other = %+v", rows)
	}

	open := openIncidentsWidget{incidents: d.Incidents}
	data, err = resolveWidget(t, open, `{"scope":"all"}`, ResolveInput{Visible: Subjects{Broad: true}, Viewer: Viewer{UserID: member}})
	testdb.Must(t, err)
	if inc := data.(OpenIncidentsData); len(inc.Incidents) != 1 || inc.Incidents[0].SubjectName != "core" {
		t.Errorf("member's all-scope incidents = %+v, want only the shared site's", inc)
	}
}

func TestDBOpenIncidentsNoVisibleSubjectIsNoData(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, severity) VALUES (?, ?, 'high')`, dev, time.Now().UTC().Add(-time.Hour))
	open := openIncidentsWidget{incidents: d.Incidents}
	_, err := resolveWidget(t, open, fmt.Sprintf(`{"scope":"devices","devices":["%s"]}`, uuid.New()),
		ResolveInput{Viewer: PublicViewer, Now: time.Now().UTC()})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData (an empty filter would list every incident)", err)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
