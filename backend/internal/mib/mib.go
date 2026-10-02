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
	found := definitions.FindAll(content, -1)
	if len(found) == 0 {
		return Header{}, ErrNotAMIB
	}
	if len(found) > 1 {
		return Header{}, ErrSeveralModules
	}
	var m *parser.Module
	var err error
	if perr := guard(strings.Fields(string(found[0]))[0], func() error {
		m, err = parser.Parse(bytes.NewReader(content))
		return nil
	}); perr != nil {
		return Header{}, perr
	}
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
// only) and its transitively missing imports. Everything is keyed by the
// module name declared in each file's content (what Inspect returns), never
// by File.Name: a file whose Inspect fails is ignored entirely.
func Build(files []File) Result {
	res := Result{Objects: map[string][]Object{}, Missing: map[string][]string{}}
	byName := map[string]File{}
	imports := map[string][]string{}
	for _, f := range files {
		h, err := Inspect([]byte(f.Content))
		if err != nil {
			continue
		}
		byName[h.Name] = f
		imports[h.Name] = h.Imports
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
		var objects []Object
		err := guard(name, func() error {
			if loadModuleHook != nil {
				loadModuleHook(name)
			}
			loaded, err := gosmi.LoadModule(name)
			if err != nil || loaded == "" {
				return nil
			}
			mod, err := gosmi.GetModule(loaded)
			if err != nil {
				return nil
			}
			for _, n := range mod.GetNodes() {
				if o := toObject(name, n); o.OID != "" {
					objects = append(objects, o)
				}
			}
			return nil
		})
		if err == nil && len(objects) > 0 {
			res.Objects[name] = objects
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

// loadModuleHook, when set (tests only), runs inside Build's guarded load of
// each module, so a test can make a load panic.
var loadModuleHook func(name string)

// guard runs f, turning a panic (gosmi and its parser are not hardened
// against hostile input, and stored modules are loaded at every start) into
// "module X could not be loaded" so one bad module cannot crash-loop the
// process.
func guard(module string, f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("module %s could not be loaded: %v", module, r)
		}
	}()
	return f()
}
