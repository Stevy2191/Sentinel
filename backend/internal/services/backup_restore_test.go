package services

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// A restore replays DROP statements before recreating anything. Run outside a
// transaction, a failure part-way leaves the database with its constraints
// and indexes dropped (seen on the sandbox, 2026-09-29: 35 foreign keys down to
// 4). One transaction means a failed restore changes nothing.
func TestRestoreArgsAreOneTransaction(t *testing.T) {
	s := NewBackupService("/tmp/x", DBConfig{Host: "h", Port: "5432", User: "u", Name: "n"})
	args := s.restoreArgs()
	for _, want := range []string{"--single-transaction", "ON_ERROR_STOP=1"} {
		if !slices.Contains(args, want) {
			t.Errorf("restore args %v missing %q", args, want)
		}
	}
}

func TestLatestMigrationInDump(t *testing.T) {
	dump := strings.Join([]string{
		"SET statement_timeout = 0;",
		"COPY public.monitors (id, name) FROM stdin;",
		"999_not_a_migration.sql\tsomething",
		`\.`,
		"COPY public.schema_migrations (filename, applied_at) FROM stdin;",
		"001_initial.sql\t2026-01-01 00:00:00+00",
		"043_report_types_and_sla.sql\t2026-09-20 00:00:00+00",
		"010_monitor_ownership_and_sharing.sql\t2026-02-01 00:00:00+00",
		`\.`,
		"ALTER TABLE ONLY public.users ADD CONSTRAINT users_pkey PRIMARY KEY (id);",
	}, "\n")

	got, err := latestMigrationInDump(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only rows of the schema_migrations block count, and the latest is the
	// highest by name, not the last row.
	if got != "043_report_types_and_sla.sql" {
		t.Errorf("got %q, want 043_report_types_and_sla.sql", got)
	}

	if _, err := latestMigrationInDump(strings.NewReader("SET x = 1;\n")); err == nil {
		t.Error("dump without schema_migrations: want an error, got nil")
	}
}

func TestCheckBackupVersion(t *testing.T) {
	if err := checkBackupVersion("045_sites.sql", "045_sites.sql"); err != nil {
		t.Errorf("same version: got %v, want nil", err)
	}

	err := checkBackupVersion("043_report_types_and_sla.sql", "045_sites.sql")
	if !errors.Is(err, ErrBackupVersionMismatch) {
		t.Fatalf("older backup: got %v, want ErrBackupVersionMismatch", err)
	}
	// The message is shown in the UI, so it must say what happened and what
	// to do, in terms of the migrations involved.
	for _, want := range []string{"older", "043_report_types_and_sla.sql", "045_sites.sql"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}

	err = checkBackupVersion("046_future.sql", "045_sites.sql")
	if !errors.Is(err, ErrBackupVersionMismatch) || !strings.Contains(err.Error(), "newer") {
		t.Errorf("newer backup: got %v, want ErrBackupVersionMismatch mentioning newer", err)
	}
}
