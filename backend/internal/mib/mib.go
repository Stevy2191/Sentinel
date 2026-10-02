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
	// parents maps each object the module defines to the name its OID is
	// assigned under (the first element of "::= { parent N }"); from maps
	// each imported name to the module it comes from. Build uses both to
	// refuse OID cycles that span modules.
	parents map[string]string
	from    map[string]string
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
	ErrOIDCycle       = errors.New("OID cycle")

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
	h := Header{Name: string(m.Name), parents: map[string]string{}, from: map[string]string{}}
	seen := map[string]bool{}
	for _, im := range m.Body.Imports {
		mod := string(im.Module)
		if !seen[mod] {
			seen[mod] = true
			h.Imports = append(h.Imports, mod)
		}
		for _, n := range im.Names {
			h.from[string(n)] = mod
		}
	}
	var order []string // declaration order, so the loop reported is stable
	if id := m.Body.Identity; id != nil {
		if p := parentOf(&id.Oid); p != "" {
			h.parents[string(id.Name)] = p
			order = append(order, string(id.Name))
		}
	}
	for _, n := range m.Body.Nodes {
		if p := parentOf(n.Oid); p != "" {
			h.parents[string(n.Name)] = p
			order = append(order, string(n.Name))
		}
	}
	// gosmi never returns from a module whose OID assignments loop.
	for _, name := range order {
		if loop := cycleFrom(name, func(n string) (string, bool) { p, ok := h.parents[n]; return p, ok }); loop != nil {
			return Header{}, fmt.Errorf("%w: %s", ErrOIDCycle, strings.Join(loop, " → "))
		}
	}
	return h, nil
}

// parentOf is the name an OID value is assigned under, or "" when it starts
// with a number.
func parentOf(oid *parser.Oid) string {
	if oid == nil || len(oid.SubIdentifiers) == 0 || oid.SubIdentifiers[0].Name == nil {
		return ""
	}
	return string(*oid.SubIdentifiers[0].Name)
}

// cycleFrom follows parent links from start and returns the loop it falls
// into ("a", "b", "a"), or nil when the chain ends.
func cycleFrom(start string, parent func(string) (string, bool)) []string {
	index := map[string]int{}
	var path []string
	for n := start; ; {
		if i, seen := index[n]; seen {
			return append(path[i:], n)
		}
		index[n] = len(path)
		path = append(path, n)
		p, ok := parent(n)
		if !ok {
			return nil
		}
		n = p
	}
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
	headers := map[string]Header{}
	for _, f := range files {
		h, err := Inspect([]byte(f.Content))
		if err != nil {
			continue
		}
		byName[h.Name] = f
		imports[h.Name] = h.Imports
		headers[h.Name] = h
	}
	for name := range byName {
		res.Missing[name] = missing(name, imports, byName, map[string]bool{})
	}
	looping := loopingModules(headers)

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
		if len(res.Missing[name]) > 0 || dependsOn(name, looping, imports, map[string]bool{}) {
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

// loopingModules finds the modules taking part in an OID cycle that spans
// modules (each module on its own passed Inspect). Objects are keyed
// "MODULE.name"; a parent is resolved in the defining module, else through
// that module's imports; anything unresolved is a root.
func loopingModules(headers map[string]Header) map[string]bool {
	parent := func(key string) (string, bool) {
		mod, name, _ := strings.Cut(key, ".")
		h, ok := headers[mod]
		if !ok {
			return "", false
		}
		p, ok := h.parents[name]
		if !ok {
			return "", false
		}
		if _, local := h.parents[p]; local {
			return mod + "." + p, true
		}
		if src, imported := h.from[p]; imported {
			return src + "." + p, true
		}
		return "", false
	}
	out := map[string]bool{}
	for mod, h := range headers {
		for name := range h.parents {
			for _, key := range cycleFrom(mod+"."+name, parent) {
				m, _, _ := strings.Cut(key, ".")
				out[m] = true
			}
		}
	}
	return out
}

// dependsOn reports whether name is, or transitively imports, a module in bad.
func dependsOn(name string, bad map[string]bool, imports map[string][]string, seen map[string]bool) bool {
	if bad[name] {
		return true
	}
	if seen[name] {
		return false
	}
	seen[name] = true
	for _, im := range imports[name] {
		if dependsOn(im, bad, imports, seen) {
			return true
		}
	}
	return false
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
