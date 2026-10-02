package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLabelValidate(t *testing.T) {
	ctx := context.Background()
	w := labelWidget{}
	got, err := w.Validate(ctx, json.RawMessage(`{"text":"  Main Campus  "}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"text":"Main Campus","size":"m"}` {
		t.Errorf("normalised = %s, want trimmed text and default size m", got)
	}
	for raw, field := range map[string]string{
		`{"text":""}`:                                      "text",
		`{"text":"` + strings.Repeat("x", 201) + `"}`:      "text",
		`{"text":"a\u0007b"}`:                              "text",
		`{"text":"ok","size":"xl"}`:                        "size",
		`{"text":5}`:                                       "text",
	} {
		_, err := w.Validate(ctx, json.RawMessage(raw))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: err = %v, want a FieldError on %q", raw, err, field)
		}
	}
}

func TestLabelResolveAndRefresh(t *testing.T) {
	w := labelWidget{}
	data, err := w.Resolve(context.Background(), json.RawMessage(`{"text":"HQ","size":"l"}`), ResolveInput{})
	if err != nil {
		t.Fatal(err)
	}
	if d := data.(LabelData); d.Text != "HQ" || d.Size != "l" {
		t.Errorf("data = %+v", d)
	}
	if w.Refresh(nil, "") != 0 {
		t.Error("a label refreshes; it should never need to")
	}
	if s := w.Subjects(nil); s.count() != 0 || s.Broad {
		t.Errorf("a label has subjects %+v", s)
	}
}
