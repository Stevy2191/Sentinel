package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
)

// fakeMaps decides access from a level per user, as the service does.
type fakeMaps struct {
	mu      sync.Mutex
	site    uuid.UUID
	levels  map[uuid.UUID]services.SiteAccessLevel
	token   string
	onMap   map[uuid.UUID]bool
	edits   int
	revoked bool
}

func (f *fakeMaps) level(v services.Viewer) services.SiteAccessLevel {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v.IsAdmin {
		return services.SiteAccessAdmin
	}
	return f.levels[v.UserID]
}

func (f *fakeMaps) snap() *services.MapSnapshot {
	return &services.MapSnapshot{SiteID: f.site, SiteName: "Work", Devices: []services.MapDevice{{ID: uuid.New(), Name: "Core-SW", Host: "10.20.0.2"}}}
}

func (f *fakeMaps) Snapshot(_ context.Context, v services.Viewer, site uuid.UUID) (*services.MapSnapshot, error) {
	if site != f.site || f.level(v) == services.SiteAccessNone {
		return nil, services.ErrSiteMapNotFound
	}
	return f.snap(), nil
}
func (f *fakeMaps) PublicSnapshot(context.Context, uuid.UUID) (*services.MapSnapshot, error) {
	s := f.snap()
	s.Devices[0].Host = ""
	return s, nil
}
func (f *fakeMaps) PublicDevice(_ context.Context, _, alias uuid.UUID) (uuid.UUID, bool, error) {
	return alias, f.onMap[alias], nil
}
func (f *fakeMaps) edit(v services.Viewer, site uuid.UUID) error {
	switch l := f.level(v); {
	case site != f.site || l == services.SiteAccessNone:
		return services.ErrSiteMapNotFound
	case l < services.SiteAccessEditable:
		return services.ErrSiteMapForbidden
	}
	f.mu.Lock()
	f.edits++
	f.mu.Unlock()
	return nil
}
func (f *fakeMaps) SetPositions(_ context.Context, v services.Viewer, s uuid.UUID, _ []services.PositionInput) error {
	return f.edit(v, s)
}
func (f *fakeMaps) SetDevice(_ context.Context, v services.Viewer, s, _ uuid.UUID, _, _ *bool) error {
	return f.edit(v, s)
}
func (f *fakeMaps) AddLink(_ context.Context, v services.Viewer, s uuid.UUID, _, _ sitemap.End) (*models.SiteMapLink, error) {
	return &models.SiteMapLink{ID: uuid.New()}, f.edit(v, s)
}
func (f *fakeMaps) UpdateLink(_ context.Context, v services.Viewer, s, _ uuid.UUID, _, _ sitemap.End) error {
	return f.edit(v, s)
}
func (f *fakeMaps) DeleteLink(_ context.Context, v services.Viewer, s, _ uuid.UUID) error {
	return f.edit(v, s)
}
func (f *fakeMaps) SetLinkHidden(_ context.Context, v services.Viewer, s uuid.UUID, _ string, _ bool) error {
	return f.edit(v, s)
}
func (f *fakeMaps) KeepGuess(_ context.Context, v services.Viewer, s uuid.UUID, _ string) (*models.SiteMapLink, error) {
	return &models.SiteMapLink{ID: uuid.New()}, f.edit(v, s)
}
func (f *fakeMaps) ResetLayout(_ context.Context, v services.Viewer, s uuid.UUID) error {
	return f.edit(v, s)
}
func (f *fakeMaps) PublicLink(context.Context, services.Viewer, uuid.UUID) (*models.SiteMapPublicLink, error) {
	return nil, nil
}
func (f *fakeMaps) CreatePublicLink(_ context.Context, v services.Viewer, _ uuid.UUID) (*models.SiteMapPublicLink, bool, error) {
	if !v.IsAdmin {
		return nil, false, services.ErrSiteMapForbidden
	}
	return &models.SiteMapPublicLink{Token: f.token}, false, nil
}
func (f *fakeMaps) RevokePublicLink(context.Context, services.Viewer, uuid.UUID) error { return nil }
func (f *fakeMaps) ResolveToken(_ context.Context, tok string) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revoked || tok != f.token {
		return uuid.Nil, services.ErrSiteMapNotFound
	}
	return f.site, nil
}

type fakeTraffic struct {
	mu      sync.Mutex
	touched int
	watched int
}

func (f *fakeTraffic) Watch(uuid.UUID) func() {
	f.mu.Lock()
	f.watched++
	f.mu.Unlock()
	return func() {}
}
func (f *fakeTraffic) Touch(uuid.UUID) { f.mu.Lock(); f.touched++; f.mu.Unlock() }
func (f *fakeTraffic) Latest(_ context.Context, site uuid.UUID) services.MapTraffic {
	return services.MapTraffic{SiteID: site, Links: map[string]services.LinkRate{}}
}

type fakeMapPorts struct{}

func (fakeMapPorts) DevicePorts(context.Context, *services.DeviceView) (*services.DevicePortsView, error) {
	return &services.DevicePortsView{Ports: []services.PortView{{DeviceInterface: models.DeviceInterface{ID: uuid.New(), MAC: "aa:bb:cc:dd:ee:ff"}}}}, nil
}

type mapRig struct {
	maps    *fakeMaps
	traffic *fakeTraffic
	audit   *fakeAudit
	hub     *stream.Hub
	sites   *flipSites
	users   *flipUsers
	h       *SiteMapHandler
}

// flipUsers is the users table: a test can demote or delete someone.
type flipUsers struct {
	mu    sync.Mutex
	admin map[uuid.UUID]bool // absent: the user is gone
}

func (f *flipUsers) GetUserByID(_ context.Context, id uuid.UUID) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.admin[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return &models.User{ID: id, IsAdmin: a}, nil
}

// flipSites answers site access from fakeMaps' levels, so a test can take
// a share away mid-stream.
type flipSites struct{ maps *fakeMaps }

func (f *flipSites) SiteAccess(_ context.Context, user uuid.UUID, admin bool, _ uuid.UUID) (services.SiteAccessLevel, error) {
	return f.maps.level(services.Viewer{UserID: user, IsAdmin: admin}), nil
}

func newMapRig() *mapRig {
	r := &mapRig{maps: &fakeMaps{site: uuid.New(), levels: map[uuid.UUID]services.SiteAccessLevel{}, token: strings.Repeat("t", 43), onMap: map[uuid.UUID]bool{}},
		traffic: &fakeTraffic{}, audit: &fakeAudit{}, hub: stream.NewHub(16)}
	r.sites = &flipSites{r.maps}
	r.users = &flipUsers{admin: map[uuid.UUID]bool{}}
	r.h = NewSiteMapHandler(r.maps, r.traffic, r.sites, r.users, &fakeDevices{}, fakeMapPorts{}, r.hub, r.audit)
	r.h.recheck, r.h.keepAlive = 30*time.Millisecond, time.Hour
	return r
}

func (r *mapRig) router(user uuid.UUID, admin bool) *gin.Engine {
	return r.routerExp(user, admin, time.Now().Add(time.Hour))
}

func (r *mapRig) routerExp(user uuid.UUID, admin bool, exp time.Time) *gin.Engine {
	r.users.mu.Lock()
	if _, ok := r.users.admin[user]; !ok {
		r.users.admin[user] = admin
	}
	r.users.mu.Unlock()
	gin.SetMode(gin.TestMode)
	g := gin.New()
	RegisterPublicSiteMapRoutes(g, r.h)
	authed := g.Group("/api/v1")
	authed.Use(func(c *gin.Context) {
		c.Set("user_id", user)
		c.Set("username", "someone")
		c.Set("is_admin", admin)
		c.Set("token_exp", exp)
		c.Next()
	})
	RegisterSiteMapRoutes(authed, r.h)
	return g
}

func TestMapHandlerAccess(t *testing.T) {
	r := newMapRig()
	nobody, reader, editor := uuid.New(), uuid.New(), uuid.New()
	r.maps.levels[reader] = services.SiteAccessReadonly
	r.maps.levels[editor] = services.SiteAccessEditable
	base := "/api/v1/network/sites/" + r.maps.site.String() + "/map"
	body := `{"positions":[{"device_id":"` + uuid.New().String() + `","x":8,"y":8}]}`
	for _, c := range []struct {
		user         uuid.UUID
		method, path string
		body         string
		want         int
	}{
		{nobody, http.MethodGet, base, "", http.StatusNotFound},
		{reader, http.MethodGet, base, "", http.StatusOK},
		{reader, http.MethodPut, base + "/positions", body, http.StatusForbidden},
		{editor, http.MethodPut, base + "/positions", body, http.StatusNoContent},
		{editor, http.MethodPut, base + "/positions", `not json`, http.StatusBadRequest},
		{editor, http.MethodGet, "/api/v1/network/sites/not-a-uuid/map", "", http.StatusBadRequest},
		{editor, http.MethodPost, base + "/public-link", "", http.StatusForbidden},
	} {
		if w := toolRequest(r.router(c.user, false), c.method, c.path, c.body); w.Code != c.want {
			t.Errorf("%s %s: %d, want %d (%s)", c.method, c.path, w.Code, c.want, w.Body.String())
		}
	}
}

func TestMapHandlerAudit(t *testing.T) {
	r := newMapRig()
	w := toolRequest(r.router(uuid.New(), true), http.MethodPost, "/api/v1/network/sites/"+r.maps.site.String()+"/map/reset", "")
	if w.Code != http.StatusNoContent || len(r.audit.entries) != 1 || r.audit.entries[0] != models.ActionSiteMapEdited {
		t.Fatalf("reset: %d, audit %v", w.Code, r.audit.entries)
	}
	w = toolRequest(r.router(uuid.New(), true), http.MethodPost, "/api/v1/network/sites/"+r.maps.site.String()+"/map/public-link", "")
	if w.Code != http.StatusCreated || r.audit.entries[len(r.audit.entries)-1] != models.ActionSiteMapLinkCreated {
		t.Fatalf("public link: %d, audit %v", w.Code, r.audit.entries)
	}
}

func TestPublicMapTrimmedAndTouched(t *testing.T) {
	r := newMapRig()
	w := toolRequest(r.router(uuid.New(), false), http.MethodGet, "/api/v1/public/maps/"+r.maps.token, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"host"`) || r.traffic.touched != 1 {
		t.Fatalf("public map: %d touched %d body %s", w.Code, r.traffic.touched, w.Body.String())
	}
	if w := toolRequest(r.router(uuid.New(), false), http.MethodGet, "/api/v1/public/maps/"+strings.Repeat("x", 43), ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown token: %d", w.Code)
	}
}

func TestPublicPortsOnlyMapDevices(t *testing.T) {
	r := newMapRig()
	on, off := uuid.New(), uuid.New()
	r.maps.onMap[on] = true
	g := r.router(uuid.New(), false)
	if w := toolRequest(g, http.MethodGet, "/api/v1/public/maps/"+r.maps.token+"/devices/"+off.String()+"/ports", ""); w.Code != http.StatusNotFound {
		t.Errorf("device not on the map: %d", w.Code)
	}
	w := toolRequest(g, http.MethodGet, "/api/v1/public/maps/"+r.maps.token+"/devices/"+on.String()+"/ports", "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "aa:bb:cc") {
		t.Errorf("map device ports: %d %s", w.Code, w.Body.String())
	}
}

func openMapStream(t *testing.T, srv *httptest.Server, path string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	s := &sseStream{resp: resp, frames: make(chan sseFrame, 64)}
	go s.read()
	return s
}

// waitClosed is whether the server ends the stream within d.
func waitClosed(s *sseStream, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case _, ok := <-s.frames:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestMapStreamFirstFrameIsMap(t *testing.T) {
	r := newMapRig()
	srv := httptest.NewServer(r.router(uuid.New(), true))
	t.Cleanup(srv.Close) // after the streams' own cleanups cancel them
	s := openMapStream(t, srv, "/api/v1/network/sites/"+r.maps.site.String()+"/map/stream")
	if f := s.next(t); f.event != "map" || !strings.Contains(f.data, "Core-SW") {
		t.Fatalf("first frame = %+v, want the map", f)
	}
	if f := s.next(t); f.event != "traffic" {
		t.Fatalf("second frame = %+v, want the latest traffic", f)
	}
	data, _ := json.Marshal(services.MapTraffic{SiteID: r.maps.site, Links: map[string]services.LinkRate{"k": {}}})
	r.hub.Publish(mapHubKey(r.maps.site), stream.Message{Event: "traffic", Data: data})
	if f := s.next(t); f.event != "traffic" || !strings.Contains(f.data, `"k"`) {
		t.Fatalf("published frame = %+v", f)
	}
	r.hub.Publish(mapHubKey(r.maps.site), stream.Message{Event: "changed"})
	if f := s.next(t); f.event != "map" {
		t.Fatalf("after a change = %+v, want the map again", f)
	}
	if r.traffic.watched != 1 {
		t.Errorf("Watch called %d times, want 1", r.traffic.watched)
	}
}

// Review Focus 4.
func TestMapStreamEndsOnRevoke(t *testing.T) {
	r := newMapRig()
	user := uuid.New()
	r.maps.levels[user] = services.SiteAccessReadonly
	srv := httptest.NewServer(r.router(user, false))
	t.Cleanup(srv.Close) // after the streams' own cleanups cancel them

	pub := openMapStream(t, srv, "/api/v1/public/maps/"+r.maps.token+"/stream")
	if f := pub.next(t); f.event != "map" {
		t.Fatalf("public first frame = %+v", f)
	}
	mine := openMapStream(t, srv, "/api/v1/network/sites/"+r.maps.site.String()+"/map/stream")
	if f := mine.next(t); f.event != "map" {
		t.Fatalf("user first frame = %+v", f)
	}
	r.maps.mu.Lock()
	r.maps.revoked = true
	delete(r.maps.levels, user)
	r.maps.mu.Unlock()
	if !waitClosed(pub, time.Second) {
		t.Error("the public stream stayed open after the link was revoked")
	}
	if !waitClosed(mine, time.Second) {
		t.Error("the user's stream stayed open after their share was removed")
	}
}

// Review: a stream must not outlive its viewer's rights — a demoted admin,
// a deleted user, or an expired sign-in.
func TestMapStreamEndsWhenViewerChanges(t *testing.T) {
	r := newMapRig()
	admin, gone := uuid.New(), uuid.New()
	srvA := httptest.NewServer(r.router(admin, true))
	t.Cleanup(srvA.Close)
	srvB := httptest.NewServer(r.router(gone, true))
	t.Cleanup(srvB.Close)
	srvC := httptest.NewServer(r.routerExp(uuid.New(), true, time.Now().Add(80*time.Millisecond)))
	t.Cleanup(srvC.Close)
	path := "/api/v1/network/sites/" + r.maps.site.String() + "/map/stream"
	a, b, c := openMapStream(t, srvA, path), openMapStream(t, srvB, path), openMapStream(t, srvC, path)
	for _, s := range []*sseStream{a, b, c} {
		if f := s.next(t); f.event != "map" {
			t.Fatalf("first frame = %+v", f)
		}
	}
	r.users.mu.Lock()
	r.users.admin[admin] = false // demoted: no share, so no access
	delete(r.users.admin, gone)
	r.users.mu.Unlock()
	for name, s := range map[string]*sseStream{"demoted": a, "deleted": b, "expired": c} {
		if !waitClosed(s, time.Second) {
			t.Errorf("%s viewer's stream stayed open", name)
		}
	}
}
