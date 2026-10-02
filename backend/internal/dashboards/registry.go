package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Widget is one widget type. Resolve and Subjects only ever see a config
// Validate has normalised (the saved form), so both may assume its shape; a
// config that no longer decodes resolves to zero values, never a panic.
type Widget interface {
	// Type is the name stored in dashboard_widgets.type.
	Type() string
	// Validate normalises a config and rejects a bad one with a *FieldError
	// (the API answers 400 naming the field). Any other error is a 500.
	Validate(ctx context.Context, cfg json.RawMessage) (json.RawMessage, error)
	// Subjects lists what the config refers to, for access checks.
	Subjects(cfg json.RawMessage) Subjects
	// Resolve loads the data for the visible subjects only. ErrNoData
	// becomes the no_data state.
	Resolve(ctx context.Context, cfg json.RawMessage, in ResolveInput) (any, error)
	// Refresh is how soon the browser should ask again; 0 means never.
	Refresh(cfg json.RawMessage, override string) time.Duration
}

// ResolveInput is everything a widget needs beyond its config.
type ResolveInput struct {
	// Visible is the config's subjects that the viewer may see.
	Visible Subjects
	Viewer  Viewer
	// Override is the dashboard's range picker ("" for none); time-based
	// widgets use it instead of their saved range.
	Override string
	Now      time.Time
}

// ErrNoData is returned by Resolve when there is nothing to show yet.
var ErrNoData = errors.New("no data")

// FieldError is a config problem the editor can show next to a field.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func fieldErr(field, msg string) error { return &FieldError{Field: field, Msg: msg} }

// decodeConfig unmarshals a config. A value of the wrong type is a
// FieldError naming the field; anything else that fails is a FieldError on
// "config", so a hostile or buggy client gets a 400, never a 500.
func decodeConfig(raw json.RawMessage, dst any) error {
	err := json.Unmarshal(raw, dst)
	if err == nil {
		return nil
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) && te.Field != "" {
		return fieldErr(te.Field, "has the wrong type")
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fieldErr("config", "is not valid JSON")
	}
	return fieldErr("config", "has an invalid value")
}

// Registry maps widget type names to their implementations.
type Registry struct {
	byType map[string]Widget
	order  []string
}

// NewRegistry registers ws in order. Registering a type twice is a
// programming error and panics at startup.
func NewRegistry(ws ...Widget) *Registry {
	r := &Registry{byType: make(map[string]Widget, len(ws))}
	for _, w := range ws {
		if _, dup := r.byType[w.Type()]; dup {
			panic(fmt.Sprintf("dashboards: widget type %q registered twice", w.Type()))
		}
		r.byType[w.Type()] = w
		r.order = append(r.order, w.Type())
	}
	return r
}

// Get returns the widget registered as t.
func (r *Registry) Get(t string) (Widget, bool) {
	w, ok := r.byType[t]
	return w, ok
}

// Types lists the registered type names in registration order.
func (r *Registry) Types() []string { return append([]string(nil), r.order...) }

// dedupe drops nil and repeated ids, keeping first-seen order.
func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// checkCount is a FieldError unless lo <= n <= hi.
func checkCount(field string, n, lo, hi int) error {
	if n < lo || n > hi {
		if lo == 0 {
			return fieldErr(field, fmt.Sprintf("allows at most %d", hi))
		}
		return fieldErr(field, fmt.Sprintf("needs %d to %d", lo, hi))
	}
	return nil
}
