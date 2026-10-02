package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// openChecker: every subject exists and is visible.
type openChecker struct{}

func (openChecker) DeviceSites(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	for _, id := range ids {
		out[id] = uuid.Nil
	}
	return out, nil
}
func (openChecker) SiteLevel(context.Context, Viewer, uuid.UUID) (services.SiteAccessLevel, error) {
	return services.SiteAccessAdmin, nil
}
func (openChecker) ExistingMonitors(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
func (openChecker) MonitorVisible(context.Context, Viewer, uuid.UUID) (bool, error) { return true, nil }
func (openChecker) ExistingAgents(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return openChecker{}.ExistingMonitors(ctx, ids)
}

// countingWidget counts Resolve calls and returns what it was told to.
type countingWidget struct {
	calls *atomic.Int32
	err   error
	devs  []uuid.UUID
}

func (countingWidget) Type() string { return "counting" }
func (countingWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if string(raw) == `{"bad":true}` {
		return nil, fieldErr("bad", "is not allowed")
	}
	return raw, nil
}
func (w countingWidget) Subjects(json.RawMessage) Subjects { return Subjects{Devices: w.devs} }
func (w countingWidget) Resolve(_ context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	w.calls.Add(1)
	if w.err != nil {
		return nil, w.err
	}
	return map[string]any{"override": in.Override, "public": in.Viewer.Public}, nil
}
func (countingWidget) Refresh(_ json.RawMessage, override string) time.Duration {
	if override == "7d" {
		return 5 * time.Minute
	}
	return time.Minute
}

func newCountingResolver(err error) (*Resolver, *atomic.Int32) {
	calls := &atomic.Int32{}
	return NewResolver(NewRegistry(countingWidget{calls: calls, err: err}), openChecker{}), calls
}

func TestResolveStatesAndRefresh(t *testing.T) {
	ctx := context.Background()
	r, _ := newCountingResolver(nil)
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	resp, err := r.Resolve(ctx, Viewer{UserID: uuid.New()}, w, "7d")
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != StateOK || resp.RefreshSeconds != 300 || resp.Data.(map[string]any)["override"] != "7d" {
		t.Errorf("resp = %+v", resp)
	}

	r, _ = newCountingResolver(ErrNoData)
	resp, err = r.Resolve(ctx, Viewer{}, w, "")
	if err != nil || resp.State != StateNoData || resp.Data != nil {
		t.Errorf("ErrNoData: resp = %+v err = %v, want state no_data", resp, err)
	}

	boom := errors.New("boom")
	r, _ = newCountingResolver(boom)
	if _, err := r.Resolve(ctx, Viewer{}, w, ""); !errors.Is(err, boom) {
		t.Errorf("a real failure err = %v, want it returned (500)", err)
	}

	unknown := &models.DashboardWidget{ID: uuid.New(), Type: "gone", Config: models.RawJSON(`{}`)}
	resp, err = r.Resolve(ctx, Viewer{}, unknown, "")
	if err != nil || resp.State != StateNoData {
		t.Errorf("a widget whose type was removed: resp = %+v err = %v, want no_data", resp, err)
	}
}

func TestPublicCacheKeyedByVersion(t *testing.T) {
	ctx := context.Background()
	r, calls := newCountingResolver(nil)
	d := &models.Dashboard{ID: uuid.New(), Version: 4}
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	for i := 0; i < 3; i++ {
		resp, err := r.ResolvePublic(ctx, d, w)
		if err != nil || resp.Data.(map[string]any)["public"] != true {
			t.Fatalf("public resolve = %+v, %v", resp, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("three public requests resolved %d times, want 1 (cached)", calls.Load())
	}
	d.Version = 5 // the dashboard was edited
	if _, err := r.ResolvePublic(ctx, d, w); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("after an edit the cache answered with old data (%d resolves, want 2)", calls.Load())
	}
}

func TestPublicCacheExpires(t *testing.T) {
	ctx := context.Background()
	r, calls := newCountingResolver(nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	d := &models.Dashboard{ID: uuid.New(), Version: 1}
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	_, _ = r.ResolvePublic(ctx, d, w)
	now = now.Add(publicCacheTTL + time.Second)
	_, _ = r.ResolvePublic(ctx, d, w)
	if calls.Load() != 2 {
		t.Errorf("after the TTL: %d resolves, want 2", calls.Load())
	}
}

func TestPreviewValidatesFirst(t *testing.T) {
	r, calls := newCountingResolver(nil)
	_, err := r.Preview(context.Background(), Viewer{}, "counting", json.RawMessage(`{"bad":true}`), "")
	var fe *FieldError
	if !errors.As(err, &fe) || calls.Load() != 0 {
		t.Errorf("bad preview err = %v, calls = %d; want a FieldError and no resolve", err, calls.Load())
	}
	if _, err := r.Preview(context.Background(), Viewer{}, "nope", json.RawMessage(`{}`), ""); err == nil {
		t.Error("previewing an unknown type succeeded")
	}
}

// blockingWidget holds Resolve until release is closed, so tests can control
// when a public flight finishes.
type blockingWidget struct {
	calls   *atomic.Int32
	started chan struct{} // receives one value per Resolve call
	release chan struct{}
	failOne *atomic.Bool // when set, the next Resolve fails once
}

func (blockingWidget) Type() string { return "blocking" }
func (blockingWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (blockingWidget) Subjects(json.RawMessage) Subjects { return Subjects{} }
func (w blockingWidget) Resolve(ctx context.Context, _ json.RawMessage, _ ResolveInput) (any, error) {
	w.calls.Add(1)
	w.started <- struct{}{}
	<-w.release
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.failOne.CompareAndSwap(true, false) {
		return nil, errors.New("boom")
	}
	return map[string]any{"ok": true}, nil
}
func (blockingWidget) Refresh(json.RawMessage, string) time.Duration { return time.Minute }

func newBlockingResolver() (*Resolver, blockingWidget) {
	bw := blockingWidget{
		calls:   &atomic.Int32{},
		started: make(chan struct{}, 16),
		release: make(chan struct{}),
		failOne: &atomic.Bool{},
	}
	return NewResolver(NewRegistry(bw), openChecker{}), bw
}

func blockingFixtures() (*models.Dashboard, *models.DashboardWidget) {
	return &models.Dashboard{ID: uuid.New(), Version: 1},
		&models.DashboardWidget{ID: uuid.New(), Type: "blocking", Config: models.RawJSON(`{}`)}
}

type publicResult struct {
	resp *Response
	err  error
}

func TestPublicCacheConcurrentRequestsShareOneResolve(t *testing.T) {
	r, bw := newBlockingResolver()
	d, w := blockingFixtures()
	const n = 8
	results := make(chan publicResult, n)
	for i := 0; i < n; i++ {
		go func() {
			resp, err := r.ResolvePublic(context.Background(), d, w)
			results <- publicResult{resp, err}
		}()
	}
	<-bw.started // the flight is running; give the other callers time to join it
	time.Sleep(50 * time.Millisecond)
	close(bw.release)
	for i := 0; i < n; i++ {
		res := <-results
		if res.err != nil || res.resp.State != StateOK {
			t.Errorf("caller %d: resp = %+v err = %v, want ok", i, res.resp, res.err)
		}
	}
	if bw.calls.Load() != 1 {
		t.Errorf("%d concurrent requests resolved %d times, want 1", n, bw.calls.Load())
	}
}

func TestPublicCacheDoesNotCacheFailures(t *testing.T) {
	r, bw := newBlockingResolver()
	close(bw.release)
	d, w := blockingFixtures()
	bw.failOne.Store(true)
	if _, err := r.ResolvePublic(context.Background(), d, w); err == nil {
		t.Fatal("first resolve succeeded, want the failure")
	}
	resp, err := r.ResolvePublic(context.Background(), d, w)
	if err != nil || resp.State != StateOK {
		t.Fatalf("second resolve = %+v, %v; want ok", resp, err)
	}
	if bw.calls.Load() != 2 {
		t.Errorf("resolves = %d, want 2 (the failure was not cached)", bw.calls.Load())
	}
}

func TestPublicCacheLeaderCancelDoesNotFailWaiter(t *testing.T) {
	r, bw := newBlockingResolver()
	d, w := blockingFixtures()
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := r.ResolvePublic(leaderCtx, d, w)
		leaderErr <- err
	}()
	<-bw.started // the leader's flight is blocked in Resolve

	waiter := make(chan publicResult, 1)
	go func() {
		resp, err := r.ResolvePublic(context.Background(), d, w)
		waiter <- publicResult{resp, err}
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter join the flight
	cancelLeader()
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Errorf("leader err = %v, want context.Canceled", err)
	}
	close(bw.release)
	res := <-waiter
	if res.err != nil || res.resp.State != StateOK {
		t.Errorf("waiter = %+v, %v; want ok despite the leader's cancellation", res.resp, res.err)
	}
	if bw.calls.Load() != 1 {
		t.Errorf("resolves = %d, want 1", bw.calls.Load())
	}
}

func TestPublicCacheSweepDropsExpiredEntries(t *testing.T) {
	ctx := context.Background()
	r, _ := newCountingResolver(nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	d := &models.Dashboard{ID: uuid.New(), Version: 1}
	old := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	fresh := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	if _, err := r.ResolvePublic(ctx, d, old); err != nil {
		t.Fatal(err)
	}
	now = now.Add(publicCacheTTL + time.Second)
	if _, err := r.ResolvePublic(ctx, d, fresh); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cache[cacheKey{d.ID, d.Version, old.ID}]; ok || len(r.cache) != 1 {
		t.Errorf("cache has %d entries, want only the fresh one", len(r.cache))
	}
}
