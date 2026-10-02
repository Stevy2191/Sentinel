package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func labelInput(id *uuid.UUID, text string, x, y int) WidgetInput {
	return WidgetInput{ID: id, Type: "label", Config: json.RawMessage(`{"text":"` + text + `"}`), X: x, Y: y, W: 4, H: 1}
}

func TestDBSaveKeepsIdsAndBumpsVersion(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	d, err := w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "Mine", Widgets: []WidgetInput{
		labelInput(nil, "first", 0, 0), labelInput(nil, "second", 4, 0),
	}})
	testdb.Must(t, err)
	if d.Version != 2 || len(d.Widgets) != 2 {
		t.Fatalf("after save: version %d, %d widgets; want 2 and 2", d.Version, len(d.Widgets))
	}
	keep := d.Widgets[0].ID
	d, err = w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 2, Name: "Renamed", Widgets: []WidgetInput{
		labelInput(&keep, "first, moved", 0, 3),
	}})
	testdb.Must(t, err)
	if d.Name != "Renamed" || d.Version != 3 || len(d.Widgets) != 1 || d.Widgets[0].ID != keep || d.Widgets[0].Y != 3 {
		t.Fatalf("second save = %+v, want the kept widget moved and the other deleted", d)
	}
	if string(d.Widgets[0].Config) != `{"size": "m", "text": "first, moved"}` {
		t.Errorf("config = %s, want the normalised config", d.Widgets[0].Config)
	}
}

func TestDBSaveVersionConflictWritesNothing(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	_, err := w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "A", Widgets: []WidgetInput{labelInput(nil, "a", 0, 0)}})
	testdb.Must(t, err)
	_, err = w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "B"})
	var vc *VersionConflictError
	if !errors.As(err, &vc) || vc.Current != 2 {
		t.Fatalf("stale save err = %v, want a VersionConflictError at 2", err)
	}
	d, err := w.svc.Get(ctx, owner, w.personal)
	testdb.Must(t, err)
	if d.Name != "A" || len(d.Widgets) != 1 {
		t.Errorf("a refused save changed the dashboard: %+v", d)
	}
}

func TestDBSaveRejectsWrongShapeConfig(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	cases := []struct {
		widget WidgetInput
		field  string
	}{
		{WidgetInput{Type: "nope", W: 1, H: 1}, "type"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text": 5}`), W: 1, H: 1}, "text"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text": ["a"]}`), W: 1, H: 1}, "text"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`[1,2]`), W: 1, H: 1}, "config"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text":"ok"}`), X: 10, W: 4, H: 1}, "position"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text":"ok"}`), W: 1, H: 25}, "position"},
	}
	for _, c := range cases {
		_, err := w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "Mine", Widgets: []WidgetInput{labelInput(nil, "ok", 0, 0), c.widget}})
		var we *WidgetError
		if !errors.As(err, &we) || we.Index != 1 || we.Field != c.field {
			t.Errorf("%+v: err = %v, want a WidgetError on widget index 1, field %q", c.widget, err, c.field)
		}
	}
}

func TestDBSaveRefusesForeignOrRepeatedWidgetIds(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	foreign := newWidget(t, w.db, w.siteDash, "label", `{"text":"x"}`)
	_, err := w.svc.Save(ctx, w.viewer(w.owner), w.personal, SaveInput{Version: 1, Name: "Mine", Widgets: []WidgetInput{labelInput(&foreign, "steal", 0, 0)}})
	var we *WidgetError
	if !errors.As(err, &we) || we.Field != "id" {
		t.Errorf("saving another dashboard's widget id err = %v, want a WidgetError on id", err)
	}
}

func TestDBSaveAccess(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	in := SaveInput{Version: 1, Name: "x"}
	if _, err := w.svc.Save(ctx, w.viewer(w.readonly), w.personal, in); !errors.Is(err, ErrForbidden) {
		t.Errorf("readonly save err = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.Save(ctx, w.viewer(w.stranger), w.personal, in); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger save err = %v, want ErrNotFound", err)
	}
	publish(t, w.db, w.siteDash, w.admin)
	if _, err := w.svc.Save(ctx, w.viewer(w.siteRW), w.siteDash, in); !errors.Is(err, ErrPublished) {
		t.Errorf("editor saving a published dashboard err = %v, want ErrPublished", err)
	}
	if _, err := w.svc.Save(ctx, w.viewer(w.admin), w.siteDash, in); err != nil {
		t.Errorf("admin saving a published dashboard: %v", err)
	}
	many := make([]WidgetInput, 51)
	for i := range many {
		many[i] = labelInput(nil, "x", 0, i)
	}
	if _, err := w.svc.Save(ctx, w.viewer(w.owner), w.personal, SaveInput{Version: 1, Name: "x", Widgets: many}); !errors.Is(err, ErrInvalid) {
		t.Errorf("51 widgets err = %v, want ErrInvalid", err)
	}
}
