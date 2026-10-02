package services

import (
	"strings"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestMatchesPrefix(t *testing.T) {
	p := []string{"1.3.6.1.4.1.9.1"}
	for oid, want := range map[string]bool{
		"1.3.6.1.4.1.9.1.2066": true, ".1.3.6.1.4.1.9.1.1208": true, "1.3.6.1.4.1.9.1": true,
		"1.3.6.1.4.1.9.12.3": false, "1.3.6.1.4.1.850.1.1.1": false, "": false,
	} {
		if MatchesPrefix(oid, p) != want {
			t.Errorf("%q: want %v", oid, want)
		}
	}
}

func goodMetric() models.ProfileMetric {
	return models.ProfileMetric{Name: "Fan", Key: "acme_fan_state", Source: "column", Kind: "status", Scale: 1,
		OID: "1.3.6.1.4.1.9.9.13.1.4.1.3", LabelMode: "column", LabelOID: "1.3.6.1.4.1.9.9.13.1.4.1.2",
		OKStates: models.Int64Array{1}, RuleKind: "not_ok", RuleEnabled: true}
}

func TestValidateMetric(t *testing.T) {
	if err := ValidateMetric(goodMetric()); err != nil {
		t.Fatalf("good metric: %v", err)
	}
	cases := map[string]func(m *models.ProfileMetric){
		"reserved prefix":        func(m *models.ProfileMetric) { m.Key = "ups_fan" },
		"built-in key":           func(m *models.ProfileMetric) { m.Key = "if_in_bps" },
		"bad key":                func(m *models.ProfileMetric) { m.Key = "Fan State" },
		"bad oid":                func(m *models.ProfileMetric) { m.OID = "iso.3.6" },
		"label oid missing":      func(m *models.ProfileMetric) { m.LabelOID = "" },
		"not_ok on gauge":        func(m *models.ProfileMetric) { m.Kind = "gauge" },
		"status without ok":      func(m *models.ProfileMetric) { m.OKStates = nil },
		"above without value":    func(m *models.ProfileMetric) { m.Kind = "gauge"; m.RuleKind = "above"; m.RuleValue = nil },
		"used_free without oid2": func(m *models.ProfileMetric) { m.Kind = "gauge"; m.RuleKind = ""; m.Source = "used_free_pct" },
		"zero scale":             func(m *models.ProfileMetric) { m.Scale = 0 },
	}
	for name, mutate := range cases {
		m := goodMetric()
		mutate(&m)
		if err := ValidateMetric(m); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// Review focus 5: copies stay unique and within 63 characters.
func TestCopyKey(t *testing.T) {
	taken := map[string]bool{"cisco_cpu_5min": true, "cisco_cpu_5min_copy": true}
	isTaken := func(k string) bool { return taken[k] }
	if k := copyKey("cisco_cpu_5min", isTaken); k != "cisco_cpu_5min_copy2" {
		t.Errorf("second copy %q", k)
	}
	long := "a" + strings.Repeat("b", 62)
	k := copyKey(long, isTaken)
	if len(k) > 63 || !strings.HasSuffix(k, "_copy") || k == long {
		t.Errorf("long copy %q (%d)", k, len(k))
	}
}

func TestStarterProfileIsValid(t *testing.T) {
	p, metrics := starterProfile()
	if p.Name != "Cisco switch health" || !p.Builtin || len(p.MatchPrefixes) != 1 || p.MatchPrefixes[0] != "1.3.6.1.4.1.9.1" {
		t.Fatalf("profile %+v", p)
	}
	if len(metrics) != 9 {
		t.Fatalf("%d metrics, want 9", len(metrics))
	}
	for _, m := range metrics {
		if err := ValidateMetric(m); err != nil {
			t.Errorf("%s: %v", m.Key, err)
		}
	}
}
