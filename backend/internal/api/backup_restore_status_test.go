package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// A backup from another version is refused before anything happens, and the
// refusal is the caller's to act on, so it is a 409 with its message, not a
// 500 that reads as a server fault.
func TestRestoreErrorStatus(t *testing.T) {
	mismatch := fmt.Errorf("%w: it was taken on an older version", services.ErrBackupVersionMismatch)
	if got := restoreErrorStatus(mismatch); got != http.StatusConflict {
		t.Errorf("version mismatch: got %d, want 409", got)
	}
	if got := restoreErrorStatus(errors.New("restore failed: boom")); got != http.StatusInternalServerError {
		t.Errorf("other failure: got %d, want 500", got)
	}
}

type fakeRestorer struct{ fail bool }

func (f fakeRestorer) Get(id string) (*services.BackupInfo, error) {
	return &services.BackupInfo{BackupID: id, Filename: id + ".sql.gz"}, nil
}
func (f fakeRestorer) Restore(_ context.Context, id string) (*services.RestoreResult, error) {
	if f.fail {
		return nil, errors.New("restore failed: boom")
	}
	return &services.RestoreResult{Restored: id + ".sql.gz"}, nil
}

type countingLoader struct{ loads int }

func (c *countingLoader) Load(context.Context) error { c.loads++; return nil }

// A successful restore replaces the profiles, so the custom metric key
// registry is reloaded; a failed one changed nothing and is not.
func TestRestoreReloadsMetricRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		fail  bool
		loads int
	}{{false, 1}, {true, 0}} {
		loader := &countingLoader{}
		r := gin.New()
		r.POST("/backups/:backup_id/restore", RestoreBackupHandler(fakeRestorer{fail: tc.fail}, nil, loader))
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/backups/2026-10-01-12-00-00/restore", strings.NewReader(`{"confirm": true}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if loader.loads != tc.loads {
			t.Errorf("fail=%v: status %d, %d registry loads, want %d", tc.fail, w.Code, loader.loads, tc.loads)
		}
	}
}
