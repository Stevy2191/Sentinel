package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSharing(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)

	testdb.Must(t, w.svc.UpsertShare(ctx, owner, w.personal, w.stranger, models.PermissionReadonly))
	if _, err := w.svc.Get(ctx, w.viewer(w.stranger), w.personal); err != nil {
		t.Fatalf("after sharing, Get by the new viewer: %v", err)
	}
	testdb.Must(t, w.svc.UpsertShare(ctx, owner, w.personal, w.stranger, models.PermissionEditable))
	shares, err := w.svc.ListShares(ctx, owner, w.personal)
	testdb.Must(t, err)
	found := false
	for _, s := range shares {
		if s.UserID == w.stranger {
			found = s.Permission == models.PermissionEditable && s.Username != ""
		}
	}
	if !found {
		t.Errorf("shares = %+v, want the stranger with editable and a username", shares)
	}
	testdb.Must(t, w.svc.RemoveShare(ctx, owner, w.personal, w.stranger))
	if _, err := w.svc.Get(ctx, w.viewer(w.stranger), w.personal); !errors.Is(err, ErrNotFound) {
		t.Errorf("after unsharing, Get err = %v, want ErrNotFound", err)
	}
}

func TestDBSharingRules(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"editable sharer is not the owner", w.svc.UpsertShare(ctx, w.viewer(w.editable), w.personal, w.stranger, "readonly"), ErrForbidden},
		{"stranger", w.svc.UpsertShare(ctx, w.viewer(w.stranger), w.personal, w.stranger, "readonly"), ErrNotFound},
		{"site dashboard", w.svc.UpsertShare(ctx, w.viewer(w.admin), w.siteDash, w.stranger, "readonly"), ErrShareSiteDashboard},
		{"owner", w.svc.UpsertShare(ctx, owner, w.personal, w.owner, "readonly"), ErrShareOwner},
		{"unknown user", w.svc.UpsertShare(ctx, owner, w.personal, uuid.New(), "readonly"), ErrUnknownUser},
		{"bad permission", w.svc.UpsertShare(ctx, owner, w.personal, w.stranger, "admin"), ErrInvalid},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, c.err, c.want)
		}
	}
	if err := w.svc.UpsertShare(ctx, w.viewer(w.admin), w.personal, w.stranger, "readonly"); err != nil {
		t.Errorf("admin sharing a personal dashboard: %v", err)
	}
}
