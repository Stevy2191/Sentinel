package database_test

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/database"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// insertReport adds a report row of the given type and scope.
func insertReport(db *gorm.DB, user uuid.UUID, reportType, scopeType, scopeData string) error {
	return db.Exec(`INSERT INTO reports (user_id, name, report_type, scope_type, scope_data, time_range_days, created_by)
		VALUES (?, ?, ?, ?, ?::jsonb, 7, ?)`, user, reportType+" report", reportType, scopeType, scopeData, user).Error
}

// 058 applies to a database that already holds uptime and incident reports,
// leaves them as they are, and from then on accepts metrics reports.
func TestDBMigration058KeepsReportsAndAcceptsMetrics(t *testing.T) {
	db := testdb.Open(t)
	// Put the reports table back as it was before 058, and forget that 058 ran.
	testdb.Exec(t, db, `ALTER TABLE reports DROP CONSTRAINT reports_report_type_check`)
	testdb.Exec(t, db, `ALTER TABLE reports ADD CONSTRAINT reports_report_type_check CHECK (report_type IN ('uptime', 'incident'))`)
	testdb.Exec(t, db, `ALTER TABLE reports DROP CONSTRAINT reports_scope_type_check`)
	testdb.Exec(t, db, `ALTER TABLE reports ADD CONSTRAINT reports_scope_type_check CHECK (scope_type IN ('monitors', 'tags', 'groups', 'types'))`)
	testdb.Exec(t, db, `DELETE FROM schema_migrations WHERE filename = '058_metric_reports.sql'`)

	user := testdb.NewUser(t, db, false)
	testdb.Must(t, insertReport(db, user, "uptime", "monitors", `{"monitor_ids":["`+uuid.NewString()+`"]}`))
	testdb.Must(t, insertReport(db, user, "incident", "types", `{"types":["dns"]}`))
	if err := insertReport(db, user, "metrics", "ports", `{"port_ids":["`+uuid.NewString()+`"],"metrics":["if_in_bps"]}`); err == nil {
		t.Fatal("a metrics report was accepted before 058: the rollback above did not take")
	}

	testdb.Must(t, database.RunMigrations(db, testdb.MigrationsDir()))

	var kept []struct {
		ReportType string
		ScopeType  string
	}
	testdb.Must(t, db.Raw(`SELECT report_type, scope_type FROM reports ORDER BY report_type`).Scan(&kept).Error)
	if len(kept) != 2 || kept[0].ReportType != "incident" || kept[0].ScopeType != "types" ||
		kept[1].ReportType != "uptime" || kept[1].ScopeType != "monitors" {
		t.Fatalf("reports after 058 = %+v, want the incident and uptime reports unchanged", kept)
	}
	for _, s := range []struct{ scopeType, data string }{
		{"ports", `{"port_ids":["` + uuid.NewString() + `"],"metrics":["if_in_bps"]}`},
		{"port_roles", `{"site_ids":["` + uuid.NewString() + `"],"roles":["wan"],"metrics":["if_in_bps"]}`},
		{"devices", `{"device_ids":["` + uuid.NewString() + `"],"metrics":["if_in_bps"]}`},
		{"sites", `{"site_ids":["` + uuid.NewString() + `"],"metrics":["if_in_bps"]}`},
	} {
		if err := insertReport(db, user, "metrics", s.scopeType, s.data); err != nil {
			t.Errorf("a metrics report scoped to %s was refused: %v", s.scopeType, err)
		}
	}
	if err := insertReport(db, user, "weekly", "monitors", `{}`); err == nil {
		t.Error("report_type 'weekly' was accepted")
	}
	if err := insertReport(db, user, "metrics", "everything", `{}`); err == nil {
		t.Error("scope_type 'everything' was accepted")
	}
}
