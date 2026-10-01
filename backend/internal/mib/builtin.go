package mib

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// The IETF and IANA modules Sentinel ships (IETF Trust BSD licence). Vendor
// MIBs are never embedded: they are uploaded by an admin.
//
//go:embed ietf/*.txt
var ietf embed.FS

// Builtins returns the embedded modules, named from their contents, sorted.
func Builtins() []File {
	entries, _ := fs.ReadDir(ietf, "ietf")
	var out []File
	for _, e := range entries {
		b, err := ietf.ReadFile("ietf/" + e.Name())
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".txt")
		if h, err := Inspect(b); err == nil {
			name = h.Name
		}
		out = append(out, File{Name: name, Content: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
