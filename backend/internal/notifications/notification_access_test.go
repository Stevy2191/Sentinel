package notifications

import (
	"context"
	"errors"
	"testing"
)

// Listing notification history without saying who is asking must fail rather
// than return everything. The list had no access filter at all, so every
// signed-in user saw every monitor's alerts (and every server agent's, which
// are admin-only); making the viewer mandatory means a future caller cannot
// reintroduce that by forgetting it.
func TestListNotificationsRequiresViewer(t *testing.T) {
	m := NewNotificationManager(nil) // never reached: the check comes first
	_, _, err := m.ListNotifications(context.Background(), ListNotificationsOptions{})
	if !errors.Is(err, ErrNotificationViewerRequired) {
		t.Fatalf("got %v, want ErrNotificationViewerRequired", err)
	}
}
