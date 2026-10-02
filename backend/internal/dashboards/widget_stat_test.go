package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestStatLevel(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		v          float64
		warn, crit *float64
		dir        string
		want       string
	}{
		{50, f(80), f(90), "above", "ok"},
		{85, f(80), f(90), "above", "warn"},
		{95, f(80), f(90), "above", "crit"},
		{90, f(80), f(90), "above", "crit"},
		{30, f(25), f(10), "below", "ok"},
		{20, f(25), f(10), "below", "warn"},
		{5, f(25), f(10), "below", "crit"},
		{99, nil, nil, "above", "ok"},
		{95, nil, f(90), "above", "crit"},
	}
	for _, c := range cases {
		if got := statLevel(c.v, c.warn, c.crit, c.dir); got != c.want {
			t.Errorf("statLevel(%v, %v, %v, %s) = %s, want %s", c.v, c.warn, c.crit, c.dir, got, c.want)
		}
	}
}

func TestStatValidate(t *testing.T) {
	w := statWidget{}
	dev, site := uuid.New(), uuid.New()
	d, s := `"device_id":"`+dev.String()+`"`, `"site_id":"`+site.String()+`"`
	for raw, field := range map[string]string{
		`{` + d + `}`:                                                              "metric",
		`{"metric":"nope",` + d + `}`:                                              "metric",
		`{"metric":"if_in_bps",` + d + `,` + s + `}`:                               "device_id",
		`{"metric":"if_in_bps"}`:                                                   "device_id",
		`{"metric":"if_in_bps",` + d + `,"mode":"x"}`:                              "mode",
		`{"metric":"if_in_bps",` + d + `,"direction":"x"}`:                         "direction",
		`{"metric":"if_in_bps",` + d + `,"warn":90,"crit":80}`:                     "warn",
		`{"metric":"if_in_bps",` + d + `,"direction":"below","warn":10,"crit":20}`: "warn",
		`{"metric":"if_in_bps",` + d + `,"range":"2h"}`:                            "range",
	} {
		_, err := w.Validate(context.Background(), json.RawMessage(raw))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: err = %v, want a FieldError on %q", raw, err, field)
		}
	}
	got, err := w.Validate(context.Background(), json.RawMessage(`{"metric":"if_in_bps",`+s+`,"instance":"3"}`))
	if err != nil {
		t.Fatal(err)
	}
	var c statConfig
	_ = json.Unmarshal(got, &c)
	if c.Mode != "latest" || c.Direction != "above" || c.Range != "24h" || c.Instance != "" {
		t.Errorf("normalised = %s, want defaults filled and the instance cleared for a site total", got)
	}
}
