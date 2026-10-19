package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type fakeSiteMaps struct {
	touched int
	empty   bool
}

func (f *fakeSiteMaps) snap(host string) *services.MapSnapshot {
	if f.empty {
		return &services.MapSnapshot{}
	}
	return &services.MapSnapshot{Devices: []services.MapDevice{{ID: uuid.New(), Name: "Core-SW", Host: host}}}
}
func (f *fakeSiteMaps) Snapshot(context.Context, services.Viewer, uuid.UUID) (*services.MapSnapshot, error) {
	return f.snap("10.20.0.2"), nil
}
func (f *fakeSiteMaps) PublicSnapshot(context.Context, uuid.UUID) (*services.MapSnapshot, error) {
	return f.snap(""), nil
}
func (f *fakeSiteMaps) Latest(_ context.Context, site uuid.UUID) services.MapTraffic {
	return services.MapTraffic{SiteID: site}
}
func (f *fakeSiteMaps) Touch(uuid.UUID) { f.touched++ }

func TestSiteMapWidget(t *testing.T) {
	maps := &fakeSiteMaps{}
	w := siteMapWidget{maps: maps}
	ctx := context.Background()
	site := uuid.New()
	cfg, err := w.Validate(ctx, json.RawMessage(`{"site_id":"`+site.String()+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var fe *FieldError
	if _, err := w.Validate(ctx, json.RawMessage(`{}`)); !errors.As(err, &fe) || fe.Field != "site_id" {
		t.Errorf("missing site_id = %v, want a field error on site_id", err)
	}
	if w.Refresh(cfg, "") != 10*time.Second || w.Type() != "site_map" {
		t.Errorf("type %q refresh %v", w.Type(), w.Refresh(cfg, ""))
	}
	in := ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Viewer: Viewer{UserID: uuid.New()}}
	got, err := w.Resolve(ctx, cfg, in)
	if err != nil || got.(SiteMapData).Map.Devices[0].Host != "10.20.0.2" {
		t.Fatalf("signed-in resolve = %+v %v", got, err)
	}
	got, err = w.Resolve(ctx, cfg, ResolveInput{Visible: in.Visible, Viewer: PublicViewer})
	if err != nil || got.(SiteMapData).Map.Devices[0].Host != "" || maps.touched != 2 {
		t.Fatalf("public resolve = %+v %v, touched %d", got, err, maps.touched)
	}
	maps.empty = true
	if _, err := w.Resolve(ctx, cfg, in); !errors.Is(err, ErrNoData) {
		t.Errorf("empty map = %v, want ErrNoData", err)
	}
	if _, err := w.Resolve(ctx, cfg, ResolveInput{Viewer: in.Viewer}); !errors.Is(err, ErrNoData) {
		t.Errorf("site not visible = %v, want ErrNoData", err)
	}
}
