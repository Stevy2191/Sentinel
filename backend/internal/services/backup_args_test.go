package services

import (
	"slices"
	"strings"
	"testing"
)

// Backups are configuration only: every table in public, and nothing else.
//
// --table, not --schema. With --schema=public, pg_dump --clean emits
// DROP SCHEMA IF EXISTS public, which fails because the pgcrypto and
// timescaledb extensions live in public. The failure comes after every table
// has been dropped, so ON_ERROR_STOP left an empty database (found on the
// sandbox, 2026-09-29). --table dumps no schema object and no extensions.
func TestDumpArgsArePublicTablesOnly(t *testing.T) {
	s := NewBackupService("/tmp/x", DBConfig{Host: "h", Port: "5432", User: "u", Name: "n"})
	args := s.dumpArgs()

	for _, want := range []string{"--table=public.*", "--clean", "--if-exists", "--no-owner", "--no-privileges"} {
		if !slices.Contains(args, want) {
			t.Errorf("dump args %v missing %q", args, want)
		}
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--schema") || a == "-n" {
			t.Errorf("dump args %v select a schema; restoring would DROP SCHEMA public", args)
		}
	}
	for _, pair := range [][2]string{{"--host", "h"}, {"--port", "5432"}, {"--username", "u"}, {"--dbname", "n"}} {
		i := slices.Index(args, pair[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != pair[1] {
			t.Errorf("dump args %v: want %s %s", args, pair[0], pair[1])
		}
	}
}
