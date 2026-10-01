// Package mib reads SNMP MIB modules: Inspect validates one file and returns
// its name and imports; Build loads a set of modules with gosmi from an
// in-memory filesystem and returns every object with a resolved OID.
package mib

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing/fstest"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/parser"
	"github.com/sleepinggenius2/gosmi/smi"
	"github.com/sleepinggenius2/gosmi/types"
)

type File struct {
	Name    string
	Content string
}

type Header struct {
	Name    string
	Imports []string
}

// ParseError is a syntax error at a line of the file.
type ParseError struct {
	Line, Column int
	Message      string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

var (
	ErrNotAMIB        = errors.New("not a MIB module (no DEFINITIONS ::= BEGIN)")
	ErrSeveralModules = errors.New("the file declares more than one module; upload one module per file")

	definitions = regexp.MustCompile(`(?m)^\s*[A-Za-z][A-Za-z0-9-]*\s+DEFINITIONS\s*::=\s*BEGIN`)
	// participle errors read "L:C: message" (optionally prefixed by a name).
	position = regexp.MustCompile(`(\d+):(\d+):\s*(.*)$`)
)

// Inspect checks one file's syntax and returns its module name and the
// modules it imports (deduplicated, in order of first appearance).
func Inspect(content []byte) (Header, error) {
	n := len(definitions.FindAllIndex(content, -1))
	if n == 0 {
		return Header{}, ErrNotAMIB
	}
	if n > 1 {
		return Header{}, ErrSeveralModules
	}
	m, err := parser.Parse(bytes.NewReader(content))
	if err != nil {
		pe := &ParseError{Message: err.Error()}
		if g := position.FindStringSubmatch(err.Error()); g != nil {
			fmt.Sscan(g[1], &pe.Line)
			fmt.Sscan(g[2], &pe.Column)
			pe.Message = g[3]
		}
		return Header{}, pe
	}
	h := Header{Name: string(m.Name)}
	seen := map[string]bool{}
	for _, im := range m.Body.Imports {
		mod := string(im.Module)
		if !seen[mod] {
			seen[mod] = true
			h.Imports = append(h.Imports, mod)
		}
	}
	return h, nil
}

type Object struct {
	Module, Name, OID, ParentOID, Kind string
	BaseType, TypeName, Units, Access  string
	Description                        string
	Enum                               map[int64]string
	IndexColumns                       []string
}

type Result struct {
	Objects map[string][]Object
	Missing map[string][]string
}

// gosmi keeps global state; every Build runs alone.
var buildMu sync.Mutex

var kinds = map[types.NodeKind]string{
	types.NodeNode: "node", types.NodeScalar: "scalar", types.NodeTable: "table",
	types.NodeRow: "row", types.NodeColumn: "column", types.NodeNotification: "notification",
}

// Build loads every file and returns, per module, its objects (ready modules
// only) and its transitively missing imports. Files must have distinct Names.
func Build(files []File) Result {
	res := Result{Objects: map[string][]Object{}, Missing: map[string][]string{}}
	byName := map[string]File{}
	imports := map[string][]string{}
	for _, f := range files {
		byName[f.Name] = f
		if h, err := Inspect([]byte(f.Content)); err == nil {
			imports[f.Name] = h.Imports
		}
	}
	for name := range byName {
		res.Missing[name] = missing(name, imports, byName, map[string]bool{})
	}

	buildMu.Lock()
	defer buildMu.Unlock()
	gosmi.Init()
	defer gosmi.Exit()
	smi.SetPath("") // never read the host's MIB directories
	fsys := fstest.MapFS{}
	for name, f := range byName {
		fsys[name] = &fstest.MapFile{Data: []byte(f.Content)}
	}
	gosmi.SetFS(gosmi.NamedFS("sentinel", fsys))
	smi.SetErrorHandler(func(string, int, int, string, string) {}) // errors are reported by Inspect

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(res.Missing[name]) > 0 {
			continue
		}
		loaded, err := gosmi.LoadModule(name)
		if err != nil || loaded == "" {
			continue
		}
		mod, err := gosmi.GetModule(loaded)
		if err != nil {
			continue
		}
		for _, n := range mod.GetNodes() {
			o := toObject(name, n)
			if o.OID != "" {
				res.Objects[name] = append(res.Objects[name], o)
			}
		}
	}
	return res
}

func missing(name string, imports map[string][]string, have map[string]File, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	set := map[string]bool{}
	for _, im := range imports[name] {
		if _, ok := have[im]; !ok {
			set[im] = true
			continue
		}
		for _, m := range missing(im, imports, have, seen) {
			set[m] = true
		}
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func toObject(module string, n gosmi.SmiNode) Object {
	o := Object{Module: module, Name: n.Name, OID: n.RenderNumeric(), Kind: kinds[n.Kind],
		Access: n.Access.String(), Description: strings.TrimSpace(n.Description)}
	if o.Kind == "" {
		o.Kind = "node"
	}
	if raw := n.GetRaw(); raw != nil {
		o.Units = raw.Units
	}
	if i := strings.LastIndex(o.OID, "."); i > 0 {
		o.ParentOID = o.OID[:i]
	}
	if n.Type != nil {
		o.TypeName = n.Type.Name
		o.BaseType = n.Type.BaseType.String()
		if n.Type.Enum != nil {
			o.Enum = map[int64]string{}
			for _, v := range n.Type.Enum.Values {
				o.Enum[v.Value] = v.Name
			}
		}
	}
	if n.Kind == types.NodeRow {
		for _, idx := range n.GetIndex() {
			o.IndexColumns = append(o.IndexColumns, idx.Name)
		}
	}
	return o
}
