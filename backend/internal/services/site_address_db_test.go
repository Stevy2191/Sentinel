package services

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A site's address is stored as street, city, state and ZIP.
func TestDBSiteAddressParts(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'HQ')`, id)
	sites := NewSiteService(db)
	str := func(s string) *string { return &s }
	_, _, err := sites.Update(ctx, id, models.SiteInput{Name: "HQ", Street: str("12 Main St"), City: str("Springfield"),
		State: str("IL"), Zip: str("62701")})
	testdb.Must(t, err)
	got, err := sites.Get(ctx, id)
	testdb.Must(t, err)
	if got.Street == nil || *got.Street != "12 Main St" || got.City == nil || *got.City != "Springfield" ||
		got.State == nil || *got.State != "IL" || got.Zip == nil || *got.Zip != "62701" {
		t.Errorf("site %+v", got)
	}
}
