package api

import (
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

func TestMeasurableWindow(t *testing.T) {
	end := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	start90 := end.AddDate(0, 0, -90)
	start30 := end.AddDate(0, 0, -30)

	t.Run("monitor older than the window is measured over the whole window", func(t *testing.T) {
		created := end.AddDate(0, 0, -200)
		from, ok := services.MeasurableWindow(created, start90, end)
		if !ok || !from.Equal(start90) {
			t.Fatalf("from=%v ok=%v, want the window start %v", from, ok, start90)
		}
	})

	// The case this exists for: a young monitor must not be judged over time it
	// did not exist for.
	t.Run("younger monitor is measured from its creation", func(t *testing.T) {
		created := end.AddDate(0, 0, -10)
		from, ok := services.MeasurableWindow(created, start90, end)
		if !ok {
			t.Fatal("a 10-day-old monitor is measurable in a 90-day window")
		}
		if !from.Equal(created) {
			t.Fatalf("from=%v, want the creation time %v", from, created)
		}
		if d := end.Sub(from); d != 10*24*time.Hour {
			t.Fatalf("measured span %v, want 240h — not the full 90 days", d)
		}
	})

	t.Run("created after the window closed is not measurable", func(t *testing.T) {
		created := end.Add(time.Hour)
		if _, ok := services.MeasurableWindow(created, start30, end); ok {
			t.Fatal("a monitor created after the window should be excluded, not scored 100%")
		}
	})

	// Exactly at the boundary there is no measurable time, so it is excluded
	// rather than dividing by zero.
	t.Run("created exactly at the window end is not measurable", func(t *testing.T) {
		if _, ok := services.MeasurableWindow(end, start30, end); ok {
			t.Fatal("zero measurable time should be excluded")
		}
	})

	t.Run("created exactly at the window start uses the whole window", func(t *testing.T) {
		from, ok := services.MeasurableWindow(start30, start30, end)
		if !ok || !from.Equal(start30) {
			t.Fatalf("from=%v ok=%v, want %v", from, ok, start30)
		}
	})
}

// The distortion the clamp removes, stated as the arithmetic the handler does:
// the same outage divided by the window versus by the monitor's own lifetime.
func TestMeasurableWindow_RemovesTheLongWindowFlattery(t *testing.T) {
	end := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	created := end.AddDate(0, 0, -10)
	downtime := 2 * time.Hour

	uptimeOver := func(start time.Time) float64 {
		from, ok := services.MeasurableWindow(created, start, end)
		if !ok {
			t.Fatal("expected a measurable window")
		}
		return 100 - downtime.Seconds()/end.Sub(from).Seconds()*100
	}

	over30 := uptimeOver(end.AddDate(0, 0, -30))
	over90 := uptimeOver(end.AddDate(0, 0, -90))

	// Both now describe the same ten days, so the window length no longer
	// changes the answer. Before the clamp these were 99.72% and 99.91%.
	if diff := over90 - over30; diff > 0.001 || diff < -0.001 {
		t.Errorf("30-day window gives %.4f%% but 90-day gives %.4f%%; a longer window must not flatter the monitor", over30, over90)
	}
	if over90 < 99.16 || over90 > 99.18 {
		t.Errorf("uptime over the monitor's actual life = %.4f%%, want about 99.17%%", over90)
	}
}
