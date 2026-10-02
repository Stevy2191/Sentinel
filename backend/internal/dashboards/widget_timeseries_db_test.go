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
