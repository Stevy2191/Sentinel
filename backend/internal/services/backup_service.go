package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrBackupNotFound is returned when no backup file has the given id.
var ErrBackupNotFound = errors.New("backup not found")

// backupPrefix and backupSuffix bracket every file this service manages.
//
// Nothing outside that pattern is listed, restored or deleted, so an unrelated
// file that finds its way into the directory cannot be fed to psql or removed
// by this code.
const (
	backupPrefix = "sentinel-backup-"
	backupSuffix = ".sql.gz"
)

// backupIDPattern is the timestamp that identifies a backup, and the only
// thing accepted from a request. It cannot express a path separator or a
// parent directory, so a crafted id cannot escape the backup directory.
var backupIDPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2}$`)

// Bounds on the external commands. A dump of a large database is slow but not
// unbounded, and a hung pg_dump must not hold a request open forever.
const (
	backupTimeout  = 30 * time.Minute
	restoreTimeout = 30 * time.Minute
)

// BackupInfo describes one backup file.
type BackupInfo struct {
	BackupID  string    `json:"backup_id"`
	Filename  string    `json:"filename"`
	SizeBytes int64     `json:"size_bytes"`
	SizeMB    float64   `json:"size_mb"`
	CreatedAt time.Time `json:"created_at"`
	// SchemaVersion is the highest migration applied when the backup was
	// taken, read back from the file. Empty when it could not be determined.
	SchemaVersion string `json:"schema_version,omitempty"`
}

// DBConfig is what the external postgres tools need to reach the database.
type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

// BackupService dumps and restores the database with pg_dump and psql.
//
// The external tools are used rather than reading the tables through GORM
// because a backup has to capture the schema as well as the rows, including
// sequences, constraints and extensions. Reproducing that faithfully is what
// pg_dump exists for, and getting it subtly wrong is only discovered when a
// restore is attempted in earnest.
type BackupService struct {
	dir    string
	db     DBConfig
	logger *log.Logger
}

func NewBackupService(dir string, db DBConfig) *BackupService {
	if strings.TrimSpace(dir) == "" {
		dir = "/var/lib/sentinel/backups"
	}
	return &BackupService{dir: dir, db: db, logger: log.Default()}
}

// Dir is where backups are written.
func (s *BackupService) Dir() string { return s.dir }

// Available reports whether the postgres client tools are installed. Without
// them the feature cannot work, and saying so is better than failing at the
// moment someone tries to use it.
func (s *BackupService) Available() error {
	for _, tool := range []string{"pg_dump", "psql"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is not installed on the server", tool)
		}
	}
	return nil
}

// ensureDir creates the backup directory, readable only by the owner.
//
// 0700 because these files contain every credential in the database: password
// hashes, notification channel secrets, agent tokens and TOTP seeds.
func (s *BackupService) ensureDir() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}
	// Set explicitly as well as at creation. Under Docker the volume mount
	// creates this directory before the process runs, and MkdirAll leaves an
	// existing directory's mode alone — so the volume's default 0755 would
	// stand and the backup filenames would be listable by any other user in
	// the container.
	if err := os.Chmod(s.dir, 0o700); err != nil {
		s.logger.Printf("[backup] WARNING: could not restrict %s to owner-only: %v", s.dir, err)
	}
	return nil
}

// env passes the password out of band. Putting it in the command line would
// expose it to anything that can read the process list.
func (s *BackupService) env() []string {
	return append(os.Environ(), "PGPASSWORD="+s.db.Password)
}

func (s *BackupService) filename(id string) string {
	return backupPrefix + id + backupSuffix
}

// pathFor resolves a backup id to a file path, refusing anything that is not
// a plain timestamp.
func (s *BackupService) pathFor(id string) (string, error) {
	if !backupIDPattern.MatchString(id) {
		return "", ErrBackupNotFound
	}
	return filepath.Join(s.dir, s.filename(id)), nil
}

// Create dumps the database to a compressed file and returns what it wrote.
func (s *BackupService) Create(ctx context.Context) (*BackupInfo, error) {
	if err := s.Available(); err != nil {
		return nil, err
	}
	if err := s.ensureDir(); err != nil {
		return nil, err
	}

	id := time.Now().Format("2006-01-02-15-04-05")
	path := filepath.Join(s.dir, s.filename(id))

	runCtx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()

	// --clean --if-exists makes the dump able to restore over a populated
	// database: it drops each object before recreating it. That is what lets a
	// restore run without dropping the database itself, which postgres refuses
	// while anything is connected — and the application is always connected.
	//
	// --no-owner and --no-privileges keep the dump portable, so it can be
	// restored into an install whose database role has a different name.
	cmd := exec.CommandContext(runCtx, "pg_dump",
		"--host", s.db.Host,
		"--port", s.db.Port,
		"--username", s.db.User,
		"--dbname", s.db.Name,
		"--clean", "--if-exists",
		"--no-owner", "--no-privileges",
	)
	cmd.Env = s.env()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("preparing pg_dump: %w", err)
	}

	// Written to a temporary name and renamed on success, so a failed or
	// interrupted dump never leaves a half-written file that looks restorable.
	tmp := path + ".partial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating backup file: %w", err)
	}
	cleanup := func() {
		out.Close()
		os.Remove(tmp)
	}

	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, fmt.Errorf("starting pg_dump: %w", err)
	}

	// Compressed in-process rather than by piping to gzip: one less external
	// command whose failure would have to be noticed separately, and the
	// error surfaces here rather than in a second process's exit status.
	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, stdout); err != nil {
		cleanup()
		_ = cmd.Wait()
		return nil, fmt.Errorf("writing backup: %w", err)
	}
	if err := gz.Close(); err != nil {
		cleanup()
		_ = cmd.Wait()
		return nil, fmt.Errorf("finishing backup: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		_ = cmd.Wait()
		return nil, fmt.Errorf("closing backup: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("pg_dump failed: %s", firstLine(stderr.String(), err))
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("saving backup: %w", err)
	}

	info, err := s.describe(id)
	if err != nil {
		return nil, err
	}
	s.logger.Printf("[backup] created %s (%.1f MB)", info.Filename, info.SizeMB)
	return info, nil
}

// List returns every backup, newest first.
func (s *BackupService) List() ([]BackupInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		// Nothing has been backed up yet, which is not a fault.
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading backup directory: %w", err)
	}

	out := make([]BackupInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, ok := idFromFilename(e.Name())
		if !ok {
			continue
		}
		info, err := s.describe(id)
		if err != nil {
			s.logger.Printf("[backup] skipping %s: %v", e.Name(), err)
			continue
		}
		out = append(out, *info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Get returns one backup's details.
func (s *BackupService) Get(id string) (*BackupInfo, error) {
	return s.describe(id)
}

// Path returns the file path for a backup, for serving a download.
func (s *BackupService) Path(id string) (string, error) {
	path, err := s.pathFor(id)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", ErrBackupNotFound
	}
	return path, nil
}

// Delete removes a backup file.
func (s *BackupService) Delete(id string) (string, error) {
	path, err := s.pathFor(id)
	if err != nil {
		return "", err
	}
	name := filepath.Base(path)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrBackupNotFound
		}
		return "", fmt.Errorf("deleting backup: %w", err)
	}
	s.logger.Printf("[backup] deleted %s", name)
	return name, nil
}

// RestoreResult says what a restore did, including the safety copy it took.
type RestoreResult struct {
	Restored string `json:"restored"`
	// SafetyBackupID is the backup taken of the current data immediately
	// before it was replaced, so a restore run by mistake is recoverable.
	SafetyBackupID string `json:"safety_backup_id,omitempty"`
	SafetyError    string `json:"safety_backup_error,omitempty"`
}

// Restore replaces the current database contents with a backup.
//
// A backup of the current data is taken first. Restoring is the one action in
// the application that destroys data outright, and the difference between a
// mistake and a disaster is whether the previous state still exists. It is not
// enough to warn in the interface and hope.
func (s *BackupService) Restore(ctx context.Context, id string) (*RestoreResult, error) {
	if err := s.Available(); err != nil {
		return nil, err
	}
	path, err := s.pathFor(id)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, ErrBackupNotFound
	}

	result := &RestoreResult{Restored: filepath.Base(path)}
	if safety, err := s.Create(ctx); err != nil {
		// Recorded rather than fatal: refusing to restore because the safety
		// copy failed would block recovery in exactly the situation where a
		// restore matters most, such as a full disk.
		result.SafetyError = err.Error()
		s.logger.Printf("[backup] WARNING: safety backup before restore failed: %v", err)
	} else {
		result.SafetyBackupID = safety.BackupID
		s.logger.Printf("[backup] safety backup %s taken before restoring %s", safety.BackupID, id)
	}

	runCtx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening backup: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("reading backup (is it a valid gzip file?): %w", err)
	}
	defer gz.Close()

	// ON_ERROR_STOP makes psql exit on the first failure instead of carrying
	// on and reporting success over a half-applied restore.
	cmd := exec.CommandContext(runCtx, "psql",
		"--host", s.db.Host,
		"--port", s.db.Port,
		"--username", s.db.User,
		"--dbname", s.db.Name,
		"--quiet",
		"--set", "ON_ERROR_STOP=1",
	)
	cmd.Env = s.env()
	cmd.Stdin = gz

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("restore failed: %s", firstLine(stderr.String(), err))
	}

	s.logger.Printf("[backup] restored %s", result.Restored)
	return result, nil
}

// describe reads a backup file's metadata.
func (s *BackupService) describe(id string) (*BackupInfo, error) {
	path, err := s.pathFor(id)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, ErrBackupNotFound
	}

	created, err := time.ParseInLocation("2006-01-02-15-04-05", id, time.Local)
	if err != nil {
		// The name is the authority for when a backup was taken, but a file
		// whose name cannot be parsed still has a modification time.
		created = stat.ModTime()
	}

	return &BackupInfo{
		BackupID:      id,
		Filename:      filepath.Base(path),
		SizeBytes:     stat.Size(),
		SizeMB:        float64(stat.Size()) / (1024 * 1024),
		CreatedAt:     created,
		SchemaVersion: schemaVersionOf(path),
	}, nil
}

// schemaVersionOf reads the highest migration recorded in a backup.
//
// Worth knowing before restoring: a backup taken under an older schema leaves
// the database behind the running binary, and the interface can say so rather
// than letting someone find out from a broken page.
func schemaVersionOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return ""
	}
	defer gz.Close()

	// The migration rows appear in a COPY block near the end of a dump, so the
	// whole file is scanned — but only up to a bound, since a large database
	// should not be decompressed in full just to label a row.
	const maxScan = 64 << 20
	limited := io.LimitReader(gz, maxScan)
	data, err := io.ReadAll(limited)
	if err != nil {
		return ""
	}

	var highest string
	for _, m := range regexp.MustCompile(`(?m)^(\d{3}_[a-z0-9_]+\.sql)`).FindAllStringSubmatch(string(data), -1) {
		if m[1] > highest {
			highest = m[1]
		}
	}
	return highest
}

// idFromFilename extracts the backup id from a filename this service owns.
func idFromFilename(name string) (string, bool) {
	if !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, backupSuffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, backupPrefix), backupSuffix)
	if !backupIDPattern.MatchString(id) {
		return "", false
	}
	return id, true
}

// firstLine returns the most useful line of a tool's stderr, falling back to
// the exit error. pg_dump and psql print the actual cause first and then a
// wall of context, and the first line is what an operator needs.
func firstLine(stderr string, fallback error) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return fallback.Error()
}
