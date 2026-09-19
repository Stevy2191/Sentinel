package services

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// The reported case: four short ping outages over a 30-day window rendered as
// "0m" each, and the summary claimed 100.00% uptime beside "Total incidents: 4".
func TestShortIncidentsAreNotZeroDowntime(t *testing.T) {
	end := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -30)

	// Four ~40-second outages, the length a monitor on a short interval makes.
	var incidents []models.Incident
	for i := 0; i < 4; i++ {
		s := end.Add(-time.Duration(12+i*2) * time.Hour)
		e := s.Add(40 * time.Second)
		incidents = append(incidents, models.Incident{
			ID: uuid.New(), StartTime: s, EndTime: &e,
		})
	}

	summaries, downtime := summarizeIncidents(incidents, start, end)

	if downtime <= 0 {
		t.Fatalf("total downtime = %v minutes; four real outages must not sum to zero", downtime)
	}
	for i, sum := range summaries {
		if sum.Duration <= 0 {
			t.Errorf("incident %d has duration %v; a 40-second outage is not zero", i, sum.Duration)
		}
		if got := formatMinutes(sum.Duration); got == "0m" {
			t.Errorf("incident %d renders as %q", i, got)
		}
	}

	// The headline figure must not claim a perfect record.
	pct := uptimePercent(start, end, downtime)
	if pct >= 100 {
		t.Fatalf("uptime = %v%%, want below 100 when downtime was recorded", pct)
	}
	if got := formatUptimePercent(pct); got == "100.00%" {
		t.Errorf("uptime renders as %q despite %v minutes of downtime", got, downtime)
	}
}

func TestFormatUptimePercent(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{100, "100.00%"},    // a genuinely perfect record
		{99.9954, "99.99%"}, // the screenshot's case: must not round up to 100
		{99.999999, "99.99%"},
		{99.5, "99.50%"},
		{87.126, "87.12%"}, // rounded down, never flattering
		{0, "0.00%"},
	}
	for _, c := range cases {
		if got := formatUptimePercent(c.in); got != c.want {
			t.Errorf("formatUptimePercent(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// End to end through the renderer, since that is where the contradiction was
// actually seen.
func TestPDF_ShortOutageDoesNotRenderAsPerfect(t *testing.T) {
	end := time.Now()
	start := end.AddDate(0, 0, -30)
	target := 99.0
	data := &ReportData{
		ReportName:     "Short outages",
		TimeRangeStart: start,
		TimeRangeEnd:   end,
		Metrics: []ReportMetrics{{
			MonitorName:     "Cloudflare",
			Uptime:          uptimePercent(start, end, 4*40.0/60.0),
			DowntimeMinutes: 4 * 40.0 / 60.0,
			IncidentCount:   4,
			SLATarget:       &target,
		}},
	}
	drawn := renderProbe(t, data)
	if strings.Contains(drawn, "100.00%") {
		t.Errorf("report shows 100.00%% despite four outages:\n%s", drawn)
	}
}
