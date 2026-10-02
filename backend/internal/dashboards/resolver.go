package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// publicCacheTTL is how long a public widget response is reused, so several
// TVs on one link cost one query per widget per 15 s (spec §4).
const publicCacheTTL = 15 * time.Second

// publicResolveTimeout bounds a shared public resolve, which outlives any one caller.
const publicResolveTimeout = 30 * time.Second

// Response is one widget's data, as the data routes return it. Expected
// states (no_access, removed, no_data) are 200s.
type Response struct {
	State          string    `json:"state"`
	Data           any       `json:"data,omitempty"`
	Hidden         int       `json:"hidden"`
	Removed        int       `json:"removed"`
	RefreshSeconds int       `json:"refresh_seconds"`
	GeneratedAt    time.Time `json:"generated_at"`
}

type cacheKey struct {
	dashboard uuid.UUID
	version   int
	widget    uuid.UUID
}

type cacheEntry struct {
	resp    *Response
	expires time.Time
}

// Resolver turns a saved widget into its data for one viewer.
type Resolver struct {
	registry *Registry
	checker  Checker
	now      func() time.Time

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
	group singleflight.Group
}

// NewResolver builds a resolver over registry, checking subjects with checker.
func NewResolver(registry *Registry, checker Checker) *Resolver {
	return &Resolver{registry: registry, checker: checker, now: time.Now, cache: map[cacheKey]cacheEntry{}}
}

// Resolve loads w's data as v sees it. override is a range key from the
// dashboard's range picker ("" for none); the caller validates it.
func (r *Resolver) Resolve(ctx context.Context, v Viewer, w *models.DashboardWidget, override string) (*Response, error) {
	return r.resolve(ctx, v, w.Type, json.RawMessage(w.Config), override)
}

// Preview resolves an unsaved widget for its editor, after validating it.
// A bad config is a *FieldError; an unknown type is an error too. A subject
// the editor cannot see and one that does not exist get the same *FieldError
// as Save gives, so Preview cannot be used to probe which ids exist.
func (r *Resolver) Preview(ctx context.Context, v Viewer, typ string, cfg json.RawMessage, override string) (*Response, error) {
	wd, ok := r.registry.Get(typ)
	if !ok {
		return nil, fieldErr("type", fmt.Sprintf("%q is not a widget type", typ))
	}
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	clean, err := wd.Validate(ctx, cfg)
	if err != nil {
		return nil, err
	}
	f, err := Filter(ctx, r.checker, v, wd.Subjects(clean))
	if err != nil {
		return nil, fmt.Errorf("checking widget subjects: %w", err)
	}
	if f.Hidden+f.Removed > 0 {
		return nil, fieldErr("config", msgSubjectUnavailable)
	}
	return r.resolveFiltered(ctx, v, wd, clean, override, f)
}

// ResolvePublic loads w's data for d's public link, with the public trim,
// reusing a response for publicCacheTTL. The key includes d's version, so an
// edit is never answered from the cache; concurrent identical requests share
// one resolve. ResolveToken runs before this on every request, so a revoked
// link or demoted creator is refused even while an entry is cached.
func (r *Resolver) ResolvePublic(ctx context.Context, d *models.Dashboard, w *models.DashboardWidget) (*Response, error) {
	key := cacheKey{dashboard: d.ID, version: d.Version, widget: w.ID}
	now := r.now()
	r.mu.Lock()
	if e, ok := r.cache[key]; ok && now.Before(e.expires) {
		r.mu.Unlock()
		return e.resp, nil
	}
	r.mu.Unlock()

	// The flight is shared, so it must not die with the first caller's request:
	// it runs on a context detached from the leader (with its own deadline),
	// and each caller stops waiting when its own ctx ends.
	ch := r.group.DoChan(fmt.Sprintf("%s/%d/%s", key.dashboard, key.version, key.widget), func() (any, error) {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publicResolveTimeout)
		defer cancel()
		resp, err := r.resolve(sctx, PublicViewer, w.Type, json.RawMessage(w.Config), "")
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.sweepLocked(now)
		r.cache[key] = cacheEntry{resp: resp, expires: now.Add(publicCacheTTL)}
		r.mu.Unlock()
		return resp, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*Response), nil
	}
}

// sweepLocked drops expired entries; called with mu held.
func (r *Resolver) sweepLocked(now time.Time) {
	for k, e := range r.cache {
		if !now.Before(e.expires) {
			delete(r.cache, k)
		}
	}
}

func (r *Resolver) resolve(ctx context.Context, v Viewer, typ string, cfg json.RawMessage, override string) (*Response, error) {
	now := r.now()
	resp := &Response{GeneratedAt: now.UTC()}
	wd, ok := r.registry.Get(typ)
	if !ok {
		// A type removed in a later release: show "No data", not an error.
		resp.State = StateNoData
		return resp, nil
	}
	f, err := Filter(ctx, r.checker, v, wd.Subjects(cfg))
	if err != nil {
		return nil, err
	}
	return r.resolveFiltered(ctx, v, wd, cfg, override, f)
}

// resolveFiltered resolves wd for v given its already-filtered subjects f.
func (r *Resolver) resolveFiltered(ctx context.Context, v Viewer, wd Widget, cfg json.RawMessage, override string, f Filtered) (*Response, error) {
	now := r.now()
	resp := &Response{GeneratedAt: now.UTC(), RefreshSeconds: int(wd.Refresh(cfg, override).Seconds())}
	resp.Hidden, resp.Removed = f.Hidden, f.Removed
	if st := f.State(); st != "" {
		resp.State = st
		return resp, nil
	}
	data, err := wd.Resolve(ctx, cfg, ResolveInput{Visible: f.Visible, Viewer: v, Override: override, Now: now})
	if errors.Is(err, ErrNoData) {
		resp.State = StateNoData
		return resp, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolving %s widget: %w", wd.Type(), err)
	}
	resp.State = StateOK
	resp.Data = data
	return resp, nil
}
