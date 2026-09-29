package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

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
