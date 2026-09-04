package models

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestReportScopeValidate(t *testing.T) {
	id := uuid.New()

	cases := []struct {
		name      string
		scopeType string
		scope     ReportScope
		wantErr   bool
	}{
		{"monitors with ids", ScopeTypeMonitors, ReportScope{MonitorIDs: []uuid.UUID{id}}, false},
		{"monitors without ids", ScopeTypeMonitors, ReportScope{}, true},
		{"tags with tags", ScopeTypeTags, ReportScope{Tags: []string{"prod"}}, false},
		{"tags without tags", ScopeTypeTags, ReportScope{}, true},
		{"groups with ids", ScopeTypeGroups, ReportScope{GroupIDs: []uuid.UUID{id}}, false},
		{"groups without ids", ScopeTypeGroups, ReportScope{}, true},
		{"unknown scope type", "everything", ReportScope{}, true},
		// A scope carrying the wrong field for its type is empty as far as that
		// type is concerned, and must not silently produce an empty report.
		{"wrong field for type", ScopeTypeTags, ReportScope{MonitorIDs: []uuid.UUID{id}}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.scope.Validate(c.scopeType)
			if c.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// The scope round-trips through JSONB, so Value and Scan must agree.
func TestReportScopeRoundTrip(t *testing.T) {
	original := ReportScope{Tags: []string{"prod", "eu-west"}}

	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	var restored ReportScope
	if err := restored.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(restored.Tags) != 2 || restored.Tags[0] != "prod" {
		t.Errorf("round trip lost data: %+v", restored)
	}

	t.Run("nil scans to an empty scope", func(t *testing.T) {
		var s ReportScope
		if err := s.Scan(nil); err != nil {
			t.Fatalf("Scan(nil): %v", err)
		}
		if len(s.Tags) != 0 || len(s.MonitorIDs) != 0 || len(s.GroupIDs) != 0 {
			t.Errorf("expected an empty scope, got %+v", s)
		}
	})

	t.Run("marshals to the documented shape", func(t *testing.T) {
		b, err := json.Marshal(ReportScope{Tags: []string{"prod"}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != `{"tags":["prod"]}` {
			t.Errorf("got %s, want {\"tags\":[\"prod\"]}", b)
		}
	})
}

func TestReportValidate(t *testing.T) {
	valid := func() *Report {
		return &Report{
			Name:          "Monthly SLA",
			TemplateID:    uuid.New(),
			ScopeType:     ScopeTypeTags,
			ScopeData:     ReportScope{Tags: []string{"prod"}},
			TimeRangeDays: 30,
		}
	}

	if err := valid().Validate(); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}

	cases := map[string]func(*Report){
		"missing name":          func(r *Report) { r.Name = "" },
		"missing template":      func(r *Report) { r.TemplateID = uuid.Nil },
		"bad scope type":        func(r *Report) { r.ScopeType = "everything" },
		"zero time range":       func(r *Report) { r.TimeRangeDays = 0 },
		"negative time range":   func(r *Report) { r.TimeRangeDays = -7 },
		"scope data mismatched": func(r *Report) { r.ScopeData = ReportScope{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := valid()
			mutate(r)
			if err := r.Validate(); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestReportTemplateValidate(t *testing.T) {
	ok := &ReportTemplate{Name: "Standard", Sections: StringSlice{SectionSLACompliance, SectionCharts}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}

	if err := (&ReportTemplate{Sections: StringSlice{SectionCharts}}).Validate(); err == nil {
		t.Error("expected an error for a template with no name")
	}
	if err := (&ReportTemplate{Name: "Empty"}).Validate(); err == nil {
		t.Error("expected an error for a template with no sections")
	}
	if err := (&ReportTemplate{Name: "Bad", Sections: StringSlice{"pie_charts"}}).Validate(); err == nil {
		t.Error("expected an error for an unknown section")
	}
}
