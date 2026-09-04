package services

import (
	"strings"
	"testing"

	"github.com/google/uuid"

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
