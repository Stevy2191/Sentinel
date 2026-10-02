package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBPublicLinkLifecycle(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	admin := w.viewer(w.admin)

	link, err := w.svc.CreatePublicLink(ctx, admin, w.siteDash)
	testdb.Must(t, err)
	if len(link.Token) != 43 {
		t.Errorf("token %q has %d chars, want 43 (32 bytes base64url)", link.Token, len(link.Token))
	}
	d, err := w.svc.ResolveToken(ctx, link.Token)
	testdb.Must(t, err)
	if d.ID != w.siteDash {
		t.Errorf("token resolves to %s, want %s", d.ID, w.siteDash)
	}

	again, err := w.svc.CreatePublicLink(ctx, admin, w.siteDash)
	testdb.Must(t, err)
	if again.Token == link.Token {
		t.Error("regenerating kept the old token")
	}
	if _, err := w.svc.ResolveToken(ctx, link.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token after regenerating err = %v, want ErrNotFound", err)
	}

	testdb.Must(t, w.svc.RevokePublicLink(ctx, admin, w.siteDash))
	if _, err := w.svc.ResolveToken(ctx, again.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token err = %v, want ErrNotFound", err)
	}
	if err := w.svc.RevokePublicLink(ctx, admin, w.siteDash); !errors.Is(err, ErrNoLink) {
		t.Errorf("revoking twice err = %v, want ErrNoLink", err)
	}
}

func TestDBResolveTokenCreatorDemoted(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	link, err := w.svc.CreatePublicLink(ctx, w.viewer(w.admin), w.siteDash)
	testdb.Must(t, err)
	testdb.Exec(t, w.db, `UPDATE users SET is_admin = false, role = 'user' WHERE id = ?`, w.admin)
	if _, err := w.svc.ResolveToken(ctx, link.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("token of a demoted admin err = %v, want ErrNotFound on the very next request", err)
	}
}

func TestDBPublicLinkRefusesNonAdmins(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	if _, err := w.svc.CreatePublicLink(ctx, w.viewer(w.siteRW), w.siteDash); !errors.Is(err, ErrForbidden) {
		t.Errorf("editor creating a link err = %v, want ErrForbidden", err)
	}
	for _, token := range []string{"", "short", "x' OR '1'='1"} {
		if _, err := w.svc.ResolveToken(ctx, token); !errors.Is(err, ErrNotFound) {
			t.Errorf("ResolveToken(%q) err = %v, want ErrNotFound", token, err)
		}
	}
}

func TestDBPublicLayoutHasNoConfigs(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	d, err := w.svc.Get(ctx, w.viewer(w.admin), w.siteDash)
	testdb.Must(t, err)
	layout, err := w.svc.PublicLayout(ctx, &d.Dashboard)
	testdb.Must(t, err)
	if layout.Name != "HQ overview" || len(layout.Widgets) != 1 || layout.Widgets[0].Type != "label" {
		t.Errorf("layout = %+v", layout)
	}
	if _, err := w.svc.PublicWidget(ctx, &d.Dashboard, uuid.New()); !errors.Is(err, ErrWidgetNotFound) {
		t.Errorf("unknown widget err = %v, want ErrWidgetNotFound", err)
	}
}
