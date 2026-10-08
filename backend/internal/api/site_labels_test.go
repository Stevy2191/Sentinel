package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// site_id in a request body has three meanings: left out (keep), null
// (clear) and an id (set). The test goes through a real decode, because what
// json.RawMessage receives for each of them is the point.
func TestParseSiteField(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name    string
		body    string
		wantSet bool
		wantID  *uuid.UUID
		wantErr bool
	}{
		{"left out", `{"name":"x"}`, false, nil, false},
		{"null", `{"site_id":null}`, true, nil, false},
		{"an id", `{"site_id":"` + id.String() + `"}`, true, &id, false},
		{"not an id", `{"site_id":"annex"}`, false, nil, true},
		{"a number", `{"site_id":7}`, false, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body struct {
				SiteID json.RawMessage `json:"site_id"`
			}
			if err := json.Unmarshal([]byte(c.body), &body); err != nil {
				t.Fatal(err)
			}
			got, err := parseSiteField(body.SiteID)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error %v", err, c.wantErr)
			}
			if got.Set != c.wantSet {
				t.Errorf("Set = %v, want %v", got.Set, c.wantSet)
			}
			if (got.ID == nil) != (c.wantID == nil) || (got.ID != nil && *got.ID != *c.wantID) {
				t.Errorf("ID = %v, want %v", got.ID, c.wantID)
			}
		})
	}
}
