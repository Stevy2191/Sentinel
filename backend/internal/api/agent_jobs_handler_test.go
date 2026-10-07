package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// The dispatcher's errors reach the agent as the codes its job loop acts on;
// anything else is a generic 500.
func TestRespondJobErrorMapsTheDispatchErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{toolruns.ErrRunNotActive, http.StatusConflict, "conflict"},
		{fmt.Errorf("finishing: %w", toolruns.ErrRunNotActive), http.StatusConflict, "conflict"},
		{toolruns.ErrTooMuchOutput, http.StatusRequestEntityTooLarge, "limit"},
		{toolruns.ErrBadFinishStatus, http.StatusBadRequest, "invalid_params"},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/job", nil)
		respondJobError(c, "testing", tc.err)
		if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != tc.status || code != tc.code {
			t.Errorf("%v: status %d code %q, want %d %q", tc.err, w.Code, code, tc.status, tc.code)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/job", nil)
	respondJobError(c, "testing", errors.New("database is down"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("a plain error: status %d, want 500", w.Code)
	}
}
