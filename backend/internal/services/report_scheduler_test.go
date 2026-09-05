package services

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func mustUUID() uuid.UUID { return uuid.New() }

func TestCronExpressionFor(t *testing.T) {
	custom := "*/15 * * * *"
	cases := []struct {
		name         string
		scheduleType string
		custom       *string
		want         string
		wantErr      bool
	}{
		{"daily", models.ScheduleTypeDaily, nil, "0 8 * * *", false},
		{"weekly", models.ScheduleTypeWeekly, nil, "0 8 * * MON", false},
		{"monthly", models.ScheduleTypeMonthly, nil, "0 8 1 * *", false},
		{"custom with expression", models.ScheduleTypeCustom, &custom, custom, false},
		// The original design fell back to daily here, delivering mail on a
		// cadence nobody chose.
		{"custom without expression is an error", models.ScheduleTypeCustom, nil, "", true},
		{"unknown type is an error", "hourly", nil, "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := CronExpressionFor(c.scheduleType, c.custom)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Every built-in cadence must parse, or a schedule would register and never run.
func TestBuiltInCadencesParse(t *testing.T) {
	for _, st := range []string{
		models.ScheduleTypeDaily, models.ScheduleTypeWeekly, models.ScheduleTypeMonthly,
	} {
		expr, err := CronExpressionFor(st, nil)
		if err != nil {
			t.Fatalf("%s: %v", st, err)
		}
		if err := ValidateCronExpression(expr); err != nil {
			t.Errorf("%s expression %q does not parse: %v", st, expr, err)
		}
	}
}

func TestValidateCronExpression(t *testing.T) {
	valid := []string{"0 8 * * *", "*/15 * * * *", "0 0 1 * *", "@daily", "@every 1h"}
	for _, e := range valid {
		if err := ValidateCronExpression(e); err != nil {
			t.Errorf("%q should be valid: %v", e, err)
		}
	}

	invalid := []string{"", "not a cron", "0 8 * *", "99 99 * * *"}
	for _, e := range invalid {
		if err := ValidateCronExpression(e); err == nil {
			t.Errorf("%q should have been rejected", e)
		}
	}
}

func TestReportScheduleValidate(t *testing.T) {
	valid := func() *models.ReportSchedule {
		return &models.ReportSchedule{
			ReportID:        mustUUID(),
			ScheduleType:    models.ScheduleTypeDaily,
			EmailRecipients: models.StringSlice{"ops@example.com"},
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}

	t.Run("recipients must be real addresses", func(t *testing.T) {
		s := valid()
		s.EmailRecipients = models.StringSlice{"not-an-email"}
		if err := s.Validate(); err == nil {
			t.Error("expected an error for a malformed recipient")
		}
	})

	t.Run("at least one recipient", func(t *testing.T) {
		s := valid()
		s.EmailRecipients = models.StringSlice{}
		if err := s.Validate(); err == nil {
			t.Error("expected an error for an empty recipient list")
		}
	})

	t.Run("recipient count is capped", func(t *testing.T) {
		s := valid()
		many := make(models.StringSlice, 0, 60)
		for i := 0; i < 60; i++ {
			many = append(many, "ops@example.com")
		}
		s.EmailRecipients = many
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), "at most") {
			t.Errorf("expected a cap error, got %v", err)
		}
	})

	t.Run("custom requires an expression", func(t *testing.T) {
		s := valid()
		s.ScheduleType = models.ScheduleTypeCustom
		if err := s.Validate(); err == nil {
			t.Error("expected an error for custom with no cron expression")
		}
	})
}

// ---- CronJobManager -------------------------------------------------------

func newTestManager() *CronJobManager {
	return NewCronJobManager(cron.New(cron.WithParser(cronParser)), cronParser)
}

// Adding a job for a schedule that already has one must replace it, or an
// edited schedule runs on both its old and its new cadence.
func TestCronJobManagerReplacesExistingJob(t *testing.T) {
	m := newTestManager()
	id := "schedule-1"

	first, err := m.AddJob(id, "0 8 * * *", func() {})
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	second, err := m.AddJob(id, "0 9 * * *", func() {})
	if err != nil {
		t.Fatalf("AddJob (replace): %v", err)
	}

	if first == second {
		t.Error("replacing a job should produce a new entry id")
	}
	if m.Count() != 1 {
		t.Errorf("registry holds %d entries, want 1 - the old job was not replaced", m.Count())
	}
	if got, ok := m.EntryID(id); !ok || got != second {
		t.Errorf("EntryID = (%d, %v), want (%d, true)", got, ok, second)
	}
}

func TestCronJobManagerRemoveJob(t *testing.T) {
	m := newTestManager()
	if _, err := m.AddJob("s1", "@daily", func() {}); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	if !m.RemoveJob("s1") {
		t.Error("RemoveJob should report that it removed a job")
	}
	if m.Count() != 0 {
		t.Errorf("registry still holds %d entries", m.Count())
	}
	// Removing an unregistered schedule is a no-op, not a failure: an inactive
	// schedule has no job and must still be deletable.
	if m.RemoveJob("s1") {
		t.Error("removing an already-removed job should report false, not panic or error")
	}
	if m.RemoveJob("never-registered") {
		t.Error("removing an unknown schedule should report false")
	}
}

func TestCronJobManagerRejectsBadExpression(t *testing.T) {
	m := newTestManager()
	if _, err := m.AddJob("s1", "not a cron", func() {}); err == nil {
		t.Error("expected an error for an invalid expression")
	}
	if m.Count() != 0 {
		t.Error("a failed AddJob must not leave a registry entry")
	}
}

// The manager's parser must match the one the cron runner uses, or an
// expression that registers could fail when its next run is computed.
func TestCronJobManagerNextRunUsesTheSameParser(t *testing.T) {
	m := newTestManager()
	for _, expr := range []string{"0 8 * * *", "@daily", "@every 1h", "*/15 * * * *"} {
		if _, err := m.AddJob("s", expr, func() {}); err != nil {
			t.Fatalf("AddJob(%q): %v", expr, err)
		}
		next, err := m.GetNextRunTime(expr)
		if err != nil {
			t.Errorf("GetNextRunTime(%q) failed though AddJob accepted it: %v", expr, err)
			continue
		}
		if !next.After(time.Now().Add(-time.Second)) {
			t.Errorf("GetNextRunTime(%q) returned a past time %v", expr, next)
		}
	}
}

// The registry is written from concurrent HTTP handlers; a bare map would race.
func TestCronJobManagerIsConcurrencySafe(t *testing.T) {
	m := newTestManager()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("schedule-%d", i%5)
			if _, err := m.AddJob(id, "@daily", func() {}); err != nil {
				t.Errorf("AddJob: %v", err)
			}
			m.EntryID(id)
			m.RemoveJob(id)
		}(i)
	}
	wg.Wait()
}
