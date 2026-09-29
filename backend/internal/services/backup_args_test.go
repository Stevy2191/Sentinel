package services

import (
	"slices"
	"testing"
)

// Backups are configuration only. Dumping just the public schema leaves out the
// metrics schema, TimescaleDB's own schemas, and every extension. The last one
// matters most: with --clean, a dump that included the timescaledb extension
// would begin its restore with DROP EXTENSION, which cascades to every
// hypertable.
func TestDumpArgsArePublicSchemaOnly(t *testing.T) {
	s := NewBackupService("/tmp/x", DBConfig{Host: "h", Port: "5432", User: "u", Name: "n"})
	args := s.dumpArgs()

	for _, want := range []string{"--schema=public", "--clean", "--if-exists", "--no-owner", "--no-privileges"} {
		if !slices.Contains(args, want) {
			t.Errorf("dump args %v missing %q", args, want)
		}
	}
	for _, pair := range [][2]string{{"--host", "h"}, {"--port", "5432"}, {"--username", "u"}, {"--dbname", "n"}} {
		i := slices.Index(args, pair[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != pair[1] {
			t.Errorf("dump args %v: want %s %s", args, pair[0], pair[1])
		}
	}
}
