package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBTimeseries(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	port := newPort(t, db, dev, 1, "Gi1/0/1", "uplink")
	now := time.Now().UTC()
	writePortBps(t, d, dev, port, 1, now.Add(-30*time.Minute), 1000, 500)
	writePortBps(t, d, dev, port, 1, now.Add(-10*time.Minute), 3000, 700)
	visible := ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: now}

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps"],"devices":["%s"],"instances":["1"],"range":"1h"}`, dev), visible)
	testdb.Must(t, err)
	ts := data.(TimeseriesData)
	if len(ts.Lines) != 1 || ts.Lines[0].Label != "Gi1/0/1 · Traffic in" || ts.Lines[0].Unit != "bps" || len(ts.Lines[0].Points) != 2 {
		t.Errorf("one device, one port: %+v", ts)
	}

	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps","if_out_bps"],"devices":["%s"],"range":"1h"}`, dev), visible)
	testdb.Must(t, err)
	if ts = data.(TimeseriesData); len(ts.Lines) != 2 || ts.Lines[0].Label != "Traffic in" {
		t.Errorf("no instances (device total per metric): %+v", ts.Lines)
	}

	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps"],"site_id":"%s","range":"1h"}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	if ts = data.(TimeseriesData); len(ts.Lines) != 1 || ts.Lines[0].Label != "Site total · Traffic in" {
		t.Errorf("site total: %+v", ts.Lines)
	}

	_, err = resolveWidget(t, w, fmt.Sprintf(`{"source":"site_traffic","site_id":"%s","view":"internet"}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("internet traffic with no WAN port: err = %v, want ErrNoData", err)
	}
}

func TestDBTimeseriesUnknownMetricIsNoData(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	// Saved while fan_rpm existed; the custom metric has since been deleted,
	// so the saved config is resolved without validating it again.
	cfg := json.RawMessage(fmt.Sprintf(`{"source":"metrics","metrics":["fan_rpm"],"devices":["%s"],"range":"1h"}`, dev))
	_, err := w.Resolve(context.Background(), cfg, ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: time.Now()})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData (not a 400 or a 500)", err)
	}
}

func TestDBTimeseriesSumsPortsPerDevice(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	p1, p2 := newPort(t, db, dev, 1, "Gi1/0/1", ""), newPort(t, db, dev, 2, "Gi1/0/2", "")
	now := time.Now().UTC()
	t1, t2 := now.Add(-30*time.Minute), now.Add(-10*time.Minute)
	writePortBps(t, d, dev, p1, 1, t1, 100, 0)
	writePortBps(t, d, dev, p2, 2, t1, 250, 0)
	writePortBps(t, d, dev, p1, 1, t2, 400, 0)
	writePortBps(t, d, dev, p2, 2, t2, 50, 0)
	data, err := resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps"],"devices":["%s"],"range":"1h"}`, dev),
		ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: now})
	testdb.Must(t, err)
	lines := data.(TimeseriesData).Lines
	if len(lines) != 1 || len(lines[0].Points) != 2 || lines[0].Points[0].Avg != 350 || lines[0].Points[1].Avg != 450 {
		t.Errorf("two ports summed per timestamp: %+v, want points 350 and 450", lines)
	}
}

func TestDBTimeseriesMultiDeviceLabels(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	core, edge := newDevice(t, db, site, "core", "10.0.0.1"), newDevice(t, db, site, "edge", "10.0.0.2")
	now := time.Now().UTC()
	writePortBps(t, d, core, newPort(t, db, core, 1, "Gi1", ""), 1, now.Add(-10*time.Minute), 1, 1)
	writePortBps(t, d, edge, newPort(t, db, edge, 1, "Gi1", ""), 1, now.Add(-10*time.Minute), 2, 2)
	data, err := resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps"],"devices":["%s","%s"],"range":"1h"}`, core, edge),
		ResolveInput{Visible: Subjects{Devices: []uuid.UUID{core, edge}}, Now: now})
	testdb.Must(t, err)
	got := map[string]bool{}
	for _, l := range data.(TimeseriesData).Lines {
		got[l.Label] = true
	}
	if len(got) != 2 || !got["core · Traffic in"] || !got["edge · Traffic in"] {
		t.Errorf("labels = %v, want core · Traffic in and edge · Traffic in", got)
	}
}

func TestDBTimeseriesSiteTotalValues(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	a, b := newDevice(t, db, site, "a", "10.0.0.1"), newDevice(t, db, site, "b", "10.0.0.2")
	now := time.Now().UTC()
	at := now.Add(-10 * time.Minute)
	writePortBps(t, d, a, newPort(t, db, a, 1, "Gi1", ""), 1, at, 100, 0)
	writePortBps(t, d, b, newPort(t, db, b, 1, "Gi1", ""), 1, at, 20, 0)
	cfg := fmt.Sprintf(`{"metrics":["if_in_bps"],"site_id":"%s","range":"1h"}`, site)
	data, err := resolveWidget(t, w, cfg, ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	lines := data.(TimeseriesData).Lines
	if len(lines) != 1 || len(lines[0].Points) != 1 || lines[0].Points[0].Avg != 120 {
		t.Errorf("site total = %+v, want one point of 120", lines)
	}
	_, err = resolveWidget(t, w, cfg, ResolveInput{Now: now})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("site not visible: err = %v, want ErrNoData", err)
	}
}

func TestDBTimeseriesSiteTraffic(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	router := newDevice(t, db, site, "router", "10.0.0.1")
	wan := newPort(t, db, router, 1, "Gi0/0", "")
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'wan', collect = true WHERE id = ?`, wan)
	now := time.Now().UTC()
	writePortBps(t, d, router, wan, 1, now.Add(-20*time.Minute), 800, 80)
	in := ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now}

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"source":"site_traffic","site_id":"%s","range":"1h"}`, site), in)
	testdb.Must(t, err)
	lines := data.(TimeseriesData).Lines
	if len(lines) != 2 || lines[0].Label != "Download" || lines[1].Label != "Upload" ||
		len(lines[0].Points) != 1 || lines[0].Points[0].Avg != 800 || lines[1].Points[0].Avg != 80 {
		t.Errorf("internet = %+v, want Download 800 and Upload 80", lines)
	}

	// No access ports carry traffic, so there is no east-west estimate.
	_, err = resolveWidget(t, w, fmt.Sprintf(`{"source":"site_traffic","site_id":"%s","view":"east_west","range":"1h"}`, site), in)
	if !errors.Is(err, ErrNoData) {
		t.Errorf("east_west with no access traffic: err = %v, want ErrNoData", err)
	}

	_, err = resolveWidget(t, w, fmt.Sprintf(`{"source":"site_traffic","site_id":"%s","range":"1h"}`, site), ResolveInput{Now: now})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("site not visible: err = %v, want ErrNoData", err)
	}
}

func TestDBTimeseriesSiteTrafficEastWest(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	router, sw := newDevice(t, db, site, "router", "10.0.0.1"), newDevice(t, db, site, "switch", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'switch' WHERE id = ?`, sw)
	wan, access := newPort(t, db, router, 1, "Gi0/0", ""), newPort(t, db, sw, 1, "Gi1/0/1", "")
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'wan', collect = true WHERE id = ?`, wan)
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'access', collect = true WHERE id = ?`, access)
	now := time.Now().UTC()
	at := now.Add(-20 * time.Minute)
	writePortBps(t, d, router, wan, 1, at, 200, 80)
	writePortBps(t, d, sw, access, 1, at, 500, 300)

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"source":"site_traffic","site_id":"%s","view":"east_west","range":"1h"}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	lines := data.(TimeseriesData).Lines
	// ((access in - wan out) + (access out - wan in)) / 2 = ((500-80) + (300-200)) / 2
	if len(lines) != 1 || lines[0].Label != "Inside the site" || lines[0].Unit != "bps" ||
		len(lines[0].Points) != 1 || lines[0].Points[0].Avg != 260 || lines[0].Points[0].Min != 260 || lines[0].Points[0].Max != 260 {
		t.Errorf("east_west = %+v, want one Inside the site line with a point of 260 bps", lines)
	}
}

func TestTimeseriesValidate(t *testing.T) {
	w := timeseriesWidget{}
	dev, site := uuid.New(), uuid.New()
	for raw, field := range map[string]string{
		`{"metrics":[],"devices":["` + dev.String() + `"]}`:                                              "metrics",
		`{"metrics":["nope"],"devices":["` + dev.String() + `"]}`:                                        "metrics",
		`{"metrics":["if_in_bps"]}`:                                                                      "devices",
		`{"metrics":["if_in_bps"],"devices":["` + dev.String() + `"],"site_id":"` + site.String() + `"}`: "devices",
		`{"metrics":["if_in_bps"],"devices":["` + dev.String() + `"],"range":"2h"}`:                      "range",
		`{"source":"site_traffic"}`:                                                                      "site_id",
		`{"source":"site_traffic","site_id":"` + site.String() + `","view":"sideways"}`:                  "view",
		`{"source":"weather"}`:                                                                           "source",
	} {
		_, err := w.Validate(context.Background(), json.RawMessage(raw))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: err = %v, want a FieldError on %q", raw, err, field)
		}
	}
	got, err := w.Validate(context.Background(), json.RawMessage(`{"metrics":["if_in_bps","if_in_bps"],"devices":["`+dev.String()+`","`+dev.String()+`"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var c timeseriesConfig
	_ = json.Unmarshal(got, &c)
	if c.Source != "metrics" || c.Range != "24h" || len(c.Metrics) != 1 || len(c.Devices) != 1 {
		t.Errorf("normalised = %s, want defaults filled and duplicates dropped", got)
	}
}
