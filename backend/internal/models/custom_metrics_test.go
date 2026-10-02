package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// The raw MIB text can be up to 2 MB; it must never be serialized into a
// module's JSON representation (e.g. a MIB library list response).
func TestMIBModuleJSONOmitsContent(t *testing.T) {
	m := MIBModule{Name: "IF-MIB", Content: "x"}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"content"`) {
		t.Errorf("JSON included content: %s", b)
	}
}
