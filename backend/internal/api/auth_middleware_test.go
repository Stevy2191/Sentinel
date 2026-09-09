package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// stubUsers stands in for the auth service.
type stubUsers struct {
	user *models.User
	err  error
}

func (s stubUsers) GetUserByID(context.Context, uuid.UUID) (*models.User, error) {
	return s.user, s.err
}

// A rejected request must not reach the handler it was guarding.
//
// Regression test. Gin runs the next handler whenever a middleware returns
// without aborting, so a middleware that only wrote a 403 still let the
// handler run: the caller was refused while the action went ahead. That is an
// authorization bypass, not a cosmetic bug, so it is pinned here.
func TestRequireAdminDoesNotRunHandlerWhenRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name    string
		isAdmin bool      // what the token claims
		users   stubUsers // what the database says
		want    int
	}{
		{
			name:    "claim says not admin",
			isAdmin: false,
			users:   stubUsers{user: &models.User{IsAdmin: true}},
			want:    http.StatusForbidden,
		},
		{
			name:    "claim says admin but the account no longer is",
			isAdmin: true,
			users:   stubUsers{user: &models.User{Username: "demoted", IsAdmin: false}},
			want:    http.StatusForbidden,
		},
		{
			name:    "account cannot be read",
			isAdmin: true,
			users:   stubUsers{err: context.DeadlineExceeded},
			want:    http.StatusForbidden,
		},
		{
			name:    "genuine admin",
			isAdmin: true,
			users:   stubUsers{user: &models.User{IsAdmin: true}},
			want:    http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handlerRan := false
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", uuid.New())
				c.Set("username", "someone")
				c.Set("is_admin", tc.isAdmin)
				c.Next()
			})
			router.DELETE("/thing", RequireAdmin(tc.users), func(c *gin.Context) {
				handlerRan = true
				c.JSON(http.StatusOK, gin.H{"deleted": true})
			})

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/thing", nil))

			if w.Code != tc.want {
				t.Errorf("status = %d, want %d", w.Code, tc.want)
			}
			shouldRun := tc.want == http.StatusOK
			if handlerRan != shouldRun {
				t.Errorf("handler ran = %v, want %v — a refused request must not perform the action",
					handlerRan, shouldRun)
			}
		})
	}
}
