package services

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// The report window is [windowStart, windowEnd): a 10-day range.
var (
	windowEnd   = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	windowStart = windowEnd.AddDate(0, 0, -10)
)

func at(daysBeforeEnd float64) time.Time {
	return windowEnd.Add(-time.Duration(daysBeforeEnd * float64(24*time.Hour)))
}

func incident(start time.Time, end *time.Time) models.Incident {
	return models.Incident{ID: uuid.New(), StartTime: start, EndTime: end}
}

func TestSummarizeIncidentsClampsToWindow(t *testing.T) {
	oneDayIn := at(9) // 1 day after the window opens
	cases := []struct {
		name         string
		incidents    []models.Incident
		wantDowntime int // minutes
	}{
		{
			name:         "no incidents",
			incidents:    nil,
			wantDowntime: 0,
		},
		{
			name:         "fully inside the window",
			incidents:    []models.Incident{incident(at(5), ptrTime(at(5).Add(30*time.Minute)))},
			wantDowntime: 30,
		},
		{
			// The bug the original query had: filtering on start_time alone drops
			// this incident entirely, reporting uptime that is too high.
			name:         "started before the window, ended inside it",
			incidents:    []models.Incident{incident(windowStart.Add(-48*time.Hour), &oneDayIn)},
			wantDowntime: 24 * 60, // only the day inside the window counts
		},
		{
			name:         "started before the window and still open",
			incidents:    []models.Incident{incident(windowStart.Add(-48*time.Hour), nil)},
			wantDowntime: 10 * 24 * 60, // the whole window
		},
		{
			name:         "open incident starting inside the window",
			incidents:    []models.Incident{incident(at(2), nil)},
			wantDowntime: 2 * 24 * 60, // runs to the end of the window
		},
		{
			name:         "ended after the window closes",
			incidents:    []models.Incident{incident(at(1), ptrTime(windowEnd.Add(48*time.Hour)))},
			wantDowntime: 24 * 60, // cut at the window end
		},
		{
			name: "several incidents sum",
			incidents: []models.Incident{
				incident(at(8), ptrTime(at(8).Add(15*time.Minute))),
				incident(at(3), ptrTime(at(3).Add(45*time.Minute))),
			},
			wantDowntime: 60,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			summaries, total := summarizeIncidents(c.incidents, windowStart, windowEnd)
			if total != c.wantDowntime {
				t.Errorf("downtime = %d minutes, want %d", total, c.wantDowntime)
			}
			if len(summaries) != len(c.incidents) {
				t.Errorf("got %d summaries, want %d", len(summaries), len(c.incidents))
			}
		})
	}
}

func TestSummarizeIncidentsDerivesStatus(t *testing.T) {
	end := at(4)
	summaries, _ := summarizeIncidents([]models.Incident{
		incident(at(5), &end),
		incident(at(2), nil),
	}, windowStart, windowEnd)

	if summaries[0].Status != "resolved" {
		t.Errorf("closed incident status = %q, want \"resolved\"", summaries[0].Status)
	}
	if summaries[1].Status != "ongoing" {
		t.Errorf("open incident status = %q, want \"ongoing\"", summaries[1].Status)
	}
}

func TestUptimePercent(t *testing.T) {
	windowMinutes := 10 * 24 * 60

	cases := []struct {
		name     string
		downtime int
		want     float64
	}{
		{"no downtime is 100%", 0, 100},
		{"whole window down is 0%", windowMinutes, 0},
		{"half the window", windowMinutes / 2, 50},
		// Overlapping incidents can sum past the window length; uptime must not
		// go negative.
		{"downtime exceeding the window floors at zero", windowMinutes * 2, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := uptimePercent(windowStart, windowEnd, c.downtime)
			if got != c.want {
				t.Errorf("uptimePercent(%d) = %v, want %v", c.downtime, got, c.want)
			}
		})
	}

	t.Run("zero-length window is not a division by zero", func(t *testing.T) {
		if got := uptimePercent(windowEnd, windowEnd, 0); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})
}

func ptrTime(t time.Time) *time.Time { return &t }
