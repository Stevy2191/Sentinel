package services

import (
	"context"
	"errors"
	"testing"
)

// Listing incidents without saying who is asking must fail rather than
// return everything. The list had no access filter at all, so every signed-in
// user saw every monitor's incidents; making the viewer mandatory means a
// future caller cannot reintroduce that by forgetting it.
func TestListIncidentsRequiresViewer(t *testing.T) {
	s := NewIncidentService(nil) // never reached: the check comes first
	_, _, err := s.ListIncidents(context.Background(), IncidentListOptions{})
	if !errors.Is(err, ErrIncidentViewerRequired) {
		t.Fatalf("got %v, want ErrIncidentViewerRequired", err)
	}
}
