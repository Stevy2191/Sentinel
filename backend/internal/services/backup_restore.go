package services

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// ErrBackupVersionMismatch is returned when a backup was taken on a different
// schema version than the database it would be restored into.
var ErrBackupVersionMismatch = errors.New("backup is from a different Sentinel version")

// restoreArgs are the psql arguments for replaying a backup.
//
// --single-transaction: the dump drops constraints, indexes and tables before
// recreating them, so a failure part-way through used to leave the database
// with its constraints gone. In one transaction a failed restore changes
// nothing. ON_ERROR_STOP makes psql stop, and so roll back, at the first
// error instead of carrying on and reporting success.
func (s *BackupService) restoreArgs() []string {
	return []string{
		"--host", s.db.Host,
		"--port", s.db.Port,
		"--username", s.db.User,
		"--dbname", s.db.Name,
		"--quiet",
		"--single-transaction",
		"--set", "ON_ERROR_STOP=1",
	}
}

// latestMigrationInDump returns the highest migration filename recorded in the
// schema_migrations data of a plain-SQL dump.
func latestMigrationInDump(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	// Data rows can be long (report HTML, JSON); the default 64 KB line limit
	// would stop the scan before it reaches schema_migrations.
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)

	in := false
	latest := ""
	for sc.Scan() {
		line := sc.Text()
		if !in {
			in = strings.HasPrefix(line, "COPY public.schema_migrations ")
			continue
		}
		if line == `\.` {
			break
		}
		name, _, _ := strings.Cut(line, "\t")
		if name > latest {
			latest = name
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading backup: %w", err)
	}
	if latest == "" {
		return "", errors.New("backup has no schema_migrations data, so its version cannot be checked")
	}
	return latest, nil
}

// checkBackupVersion refuses a backup from a different schema version.
//
// The restore replays the dump over the live database. A dump only drops and
// recreates what it contains, so tables added since the backup was taken stay
// behind, and their foreign keys stop the dump from dropping the tables they
// point at (a pre-TimescaleDB backup fails on site_sharing -> users). Refusing
// up front gives a message a person can act on instead of a Postgres error.
func checkBackupVersion(backupLatest, dbLatest string) error {
	switch {
	case backupLatest == dbLatest:
		return nil
	case backupLatest < dbLatest:
		return fmt.Errorf("%w: it was taken on an older version (schema up to %s; this install is at %s). "+
			"Restore it on an install at that version, or restore a newer backup",
			ErrBackupVersionMismatch, backupLatest, dbLatest)
	default:
		return fmt.Errorf("%w: it was taken on a newer version (schema up to %s; this install is at %s). "+
			"Update Sentinel before restoring it",
			ErrBackupVersionMismatch, backupLatest, dbLatest)
	}
}

// backupMigration reads a backup file's latest migration.
func backupMigration(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening backup: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("reading backup (is it a valid gzip file?): %w", err)
	}
	defer gz.Close()
	return latestMigrationInDump(gz)
}

// currentMigration asks the database for its latest applied migration.
func (s *BackupService) currentMigration(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "psql",
		"--host", s.db.Host,
		"--port", s.db.Port,
		"--username", s.db.User,
		"--dbname", s.db.Name,
		"--no-psqlrc", "--tuples-only", "--no-align",
		"--command", "SELECT max(filename) FROM schema_migrations",
	)
	cmd.Env = s.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("reading the database version: %s", firstLine(stderr.String(), err))
	}
	return strings.TrimSpace(stdout.String()), nil
}
