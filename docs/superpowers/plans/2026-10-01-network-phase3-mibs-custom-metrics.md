# Network Phase 3 (MIB Library and Custom Metrics) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admins upload vendor MIBs, browse them and test-walk live devices, define custom metrics grouped in profiles matched by sysObjectID, and Sentinel polls, charts and alerts on them — starting with a built-in Cisco switch health profile (CPU, memory, temperature, fans, power supplies).

**Architecture:** A pure package `internal/mib` validates MIB files with `gosmi/parser` (name, imports, syntax errors with line) and builds the object tree with `gosmi` from an in-memory filesystem; `MIBLibrary` stores modules and objects in Postgres and rebuilds the tree after every change. A pure package `internal/custommetric` turns walked columns into rows (filter, precision, scale, labels, used/free %, counter rates, status) and evaluates alert rules. `ProfileService` manages profiles/metrics/overrides; `ProfileMonitor`, called by `DevicePoller` like `UPSMonitor`, reads due profiles, writes samples (with labels) to the existing metrics store, and opens/closes `metric` device-condition incidents with open incidents as the truth. React pages: MIB library + browser + test-walk, profiles + metric editor with live preview, and a device Health section.

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL 16 + TimescaleDB, gosnmp, `github.com/sleepinggenius2/gosmi v0.4.4` (MIT); React/TS/Vite/Tailwind/Recharts; snmpsim.

**Spec:** `docs/superpowers/specs/2026-10-01-network-phase3-mibs-custom-metrics-design.md`

## Global Constraints

- Work only in `/home/sysadmin/sentinel-phase3` (branch `feature/network-phase3`). The live checkout `/home/sysadmin/sentinel` stays on `main`.
- One migration for this phase: `backend/migrations/056_custom_metrics.sql` (055 is latest). New timestamp columns are TIMESTAMPTZ. A CHECK built as `col IN (...)` must also say `col IS NOT NULL` where NULL must be refused.
- **Licensing ruling (spec §1 check, done while planning):** Cisco's MIB repository (`github.com/cisco/cisco-mibs`) has no licence file and the MIBs say "All rights reserved". **Do not bundle any Cisco MIB file.** Bundle only the IETF modules below (IETF Trust BSD licence). The Cisco starter profile is defined by numeric OIDs and works without Cisco MIBs; the MIB library page lists the Cisco files to upload for names in the browser (URLs in Task 12).
- Built-in (embedded) modules, exactly: SNMPv2-SMI, SNMPv2-TC, SNMPv2-CONF, SNMPv2-MIB, SNMP-FRAMEWORK-MIB, IF-MIB, IANAifType-MIB, ENTITY-MIB, IANA-ENTITY-MIB, UUID-TC-MIB, ENTITY-SENSOR-MIB, UPS-MIB, HCNUM-TC, INET-ADDRESS-MIB.
- Limits: 2 MB per MIB file, 20 MB per upload, 500 files per zip; test-walk 500 rows and 20 s; profile poll 30 s per device; metric key `^[a-z][a-z0-9_]{2,62}$`, not a built-in key, not starting `if_` or `ups_`.
- Profile poll intervals: 1, 5 or 15 minutes. Profile match compares whole OID arcs.
- MIB/profile/metric/rule writes are admin-only and audited; test-walk and preview need editor access to the device's site (404 when the caller cannot see it, 403 when readonly); Health data follows site access.
- Go: `gofmt`, `go vet ./...` clean. Unit: `cd backend && go test ./...`. DB tests: `cd backend && ./scripts/test-db.sh -run <Name> -v` (the make target ignores args); full: `./scripts/test-db.sh`. Simulator: `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ ./internal/services/ -run Sim`.
- Frontend check: `docker run --rm --user 1000:1000 -e HOME=/tmp -v /home/sysadmin/sentinel-phase3/frontend:/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"` → exit 0. Component files export only components (react-refresh lint); put helpers in `src/utils/`.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. No push, no merge.
- **Ruling (planning): ENVMON `notPresent` (5) counts as OK** for the starter profile's fan and power-supply metrics, alongside `normal` (1). The spec lists only `normal`; but an empty redundant PSU/fan slot reports `notPresent`, and alerting on it would page for nothing. Editable per metric like every other OK state.
- **Ruling (planning): built-in modules differ from the spec's list.** No Cisco modules (licensing, above); added the IETF modules the standard ones import (SNMP-FRAMEWORK-MIB, IANA-ENTITY-MIB, UUID-TC-MIB, HCNUM-TC, INET-ADDRESS-MIB), found by loading them with gosmi while planning.
- **Ruling (planning): gosmi does not fail on missing imports** (it loads the module and leaves objects under unresolved parents without an OID), so "waiting" is computed by Sentinel from each module's IMPORTS against the stored module names (transitively), and syntax errors come from `gosmi/parser.Parse`, which returns the line; `gosmi.LoadModule` only prints them.

## Review Focus

1. **A vendor zip that contains a README, `.DS_Store` or nested folders**: non-MIB files (no `DEFINITIONS ::= BEGIN`) are skipped and listed as skipped, not parsed — otherwise all-or-nothing would reject every real vendor bundle. Pinned in Task 3.
2. **A Cisco CPU row whose `cpmCPUTotalPhysicalIndex` is 0** (no entity, common on single switches): the label falls back to `"Row <index>"` (e.g. "Row 2"), never empty. Pinned in Task 5.
3. **A switch answering only some of the profile's tables** (ENVMON absent on a 3850, FRU absent on a 2960): the run is recorded as successful, with no error. Pinned in Task 10.
4. **Re-uploading a module that already exists under a different file name**: the module (by name, from its content) is replaced, not duplicated, and objects are rebuilt. Pinned in Task 3.
5. **Copying a profile twice, or copying one with long keys**: copied keys stay unique and within the 63-character rule. Pinned in Task 7.

---

### Task 1: `internal/mib` — inspect and build MIB modules

**Files:**
- Create: `backend/internal/mib/mib.go`, `backend/internal/mib/builtin.go`, `backend/internal/mib/ietf/*.txt` (14 files)
- Test: `backend/internal/mib/mib_test.go`
- Modify: `backend/go.mod`, `backend/go.sum` (add `github.com/sleepinggenius2/gosmi v0.4.4`)

**Interfaces:**
- Produces:
  - `type File struct { Name, Content string }` (Name = the module name declared in Content)
  - `type Header struct { Name string; Imports []string }`
  - `type ParseError struct { Line, Column int; Message string }` with `Error() string` → `"line L: message"`
  - `func Inspect(content []byte) (Header, error)` — errors are `*ParseError` for syntax; `ErrNotAMIB` when there is no `DEFINITIONS ::= BEGIN`; `ErrSeveralModules` when more than one module is declared
  - `type Object struct { Module, Name, OID, ParentOID, Kind, BaseType, TypeName, Units, Access, Description string; Enum map[int64]string; IndexColumns []string }` — Kind one of `node|scalar|table|row|column|notification`
  - `type Result struct { Objects map[string][]Object; Missing map[string][]string }` keyed by module name; `Missing[m]` is the sorted transitive list of absent modules (empty = ready); waiting modules get no Objects
  - `func Build(files []File) Result`
  - `func Builtins() []File`

- [ ] **Step 1: Add the dependency and the embedded IETF files**

```bash
cd /home/sysadmin/sentinel-phase3/backend && go get github.com/sleepinggenius2/gosmi@v0.4.4
mkdir -p internal/mib/ietf && cd internal/mib/ietf
for m in SNMPv2-SMI SNMPv2-TC SNMPv2-CONF SNMPv2-MIB SNMP-FRAMEWORK-MIB IF-MIB IANAifType-MIB; do
  curl -sfL --max-time 30 -o $m.txt https://raw.githubusercontent.com/net-snmp/net-snmp/master/mibs/$m.txt; done
for m in ENTITY-MIB IANA-ENTITY-MIB UUID-TC-MIB ENTITY-SENSOR-MIB UPS-MIB HCNUM-TC INET-ADDRESS-MIB; do
  curl -sfL --max-time 30 -o $m.txt https://raw.githubusercontent.com/librenms/librenms/master/mibs/$m; done
ls | wc -l   # 14
grep -L "IETF Trust\|Internet Society\|RFC" *.txt   # expect no output: every file is an IETF/IANA module
```

If any `grep -L` line prints, open that file and confirm it is the IETF/IANA module (IANA files say "IANA"); do not add any vendor file here.

- [ ] **Step 2: Write the failing tests** `backend/internal/mib/mib_test.go`

```go
package mib

import (
	"errors"
	"strings"
	"testing"
)

func TestBuiltinsAllReady(t *testing.T) {
	files := Builtins()
	if len(files) != 14 {
		t.Fatalf("%d built-ins, want 14", len(files))
	}
	res := Build(files)
	for _, f := range files {
		if len(res.Missing[f.Name]) != 0 {
			t.Errorf("%s waiting for %v", f.Name, res.Missing[f.Name])
		}
	}
	var found *Object
	for i, o := range res.Objects["UPS-MIB"] {
		if o.Name == "upsBatteryStatus" {
			found = &res.Objects["UPS-MIB"][i]
		}
	}
	if found == nil || found.OID != "1.3.6.1.2.1.33.1.2.1" || found.Kind != "scalar" || found.Enum[3] != "batteryLow" {
		t.Fatalf("upsBatteryStatus %+v", found)
	}
	var row *Object
	for i, o := range res.Objects["ENTITY-MIB"] {
		if o.Name == "entPhysicalEntry" {
			row = &res.Objects["ENTITY-MIB"][i]
		}
	}
	if row == nil || row.Kind != "row" || len(row.IndexColumns) != 1 || row.IndexColumns[0] != "entPhysicalIndex" {
		t.Errorf("entPhysicalEntry %+v", row)
	}
}

func TestInspect(t *testing.T) {
	h, err := Inspect([]byte("X-MIB DEFINITIONS ::= BEGIN\nIMPORTS enterprises FROM SNMPv2-SMI DisplayString FROM SNMPv2-TC;\nx OBJECT IDENTIFIER ::= { enterprises 99999 }\nEND\n"))
	if err != nil || h.Name != "X-MIB" || strings.Join(h.Imports, ",") != "SNMPv2-SMI,SNMPv2-TC" {
		t.Fatalf("header %+v err %v", h, err)
	}
	_, err = Inspect([]byte("X-MIB DEFINITIONS ::= BEGIN\nx OBJECT IDENTIFIER ::= { enterprises 99999\nEND\n"))
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Line != 3 {
		t.Fatalf("syntax error %v", err)
	}
	if _, err := Inspect([]byte("This archive contains Cisco MIBs.\n")); !errors.Is(err, ErrNotAMIB) {
		t.Errorf("readme: %v", err)
	}
}

// A module whose import is absent is waiting (transitively: its dependants
// too), has no objects, and becomes ready once the import is supplied.
func TestBuildWaitingThenReady(t *testing.T) {
	smi := "ACME-SMI DEFINITIONS ::= BEGIN\nIMPORTS enterprises FROM SNMPv2-SMI;\nacme OBJECT IDENTIFIER ::= { enterprises 99999 }\nEND\n"
	mib := "ACME-MIB DEFINITIONS ::= BEGIN\nIMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI acme FROM ACME-SMI;\n" +
		"acmeTemp OBJECT-TYPE SYNTAX Integer32 UNITS \"celsius\" MAX-ACCESS read-only STATUS current DESCRIPTION \"Temp.\" ::= { acme 1 }\nEND\n"
	child := "ACME-EXT DEFINITIONS ::= BEGIN\nIMPORTS acmeTemp FROM ACME-MIB;\nEND\n"
	files := append(Builtins(), File{Name: "ACME-MIB", Content: mib}, File{Name: "ACME-EXT", Content: child})
	res := Build(files)
	if strings.Join(res.Missing["ACME-MIB"], ",") != "ACME-SMI" || strings.Join(res.Missing["ACME-EXT"], ",") != "ACME-SMI" {
		t.Fatalf("missing %v / %v", res.Missing["ACME-MIB"], res.Missing["ACME-EXT"])
	}
	if len(res.Objects["ACME-MIB"]) != 0 {
		t.Error("waiting module has objects")
	}
	res = Build(append(files, File{Name: "ACME-SMI", Content: smi}))
	if len(res.Missing["ACME-MIB"]) != 0 {
		t.Fatalf("still waiting: %v", res.Missing["ACME-MIB"])
	}
	var temp *Object
	for i, o := range res.Objects["ACME-MIB"] {
		if o.Name == "acmeTemp" {
			temp = &res.Objects["ACME-MIB"][i]
		}
	}
	if temp == nil || temp.OID != "1.3.6.1.4.1.99999.1" || temp.Units != "celsius" || temp.ParentOID != "1.3.6.1.4.1.99999" {
		t.Errorf("acmeTemp %+v", temp)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `cd backend && go test ./internal/mib/`
Expected: build failure (undefined `Builtins`, `Build`, `Inspect`).

- [ ] **Step 4: Implement** `backend/internal/mib/builtin.go`:

```go
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
```

`backend/internal/mib/mib.go`:

```go
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
```

Notes: if `types.NodeNotification` or `n.Access.String()` do not exist under those names in v0.4.4, use the names in `$(go env GOMODCACHE)/github.com/sleepinggenius2/gosmi@v0.4.4/types/` (enumer-generated `String()` methods exist for NodeKind, BaseType and Access). If a test shows the participle error text does not match `position`, adjust the regexp to the real format (print `err.Error()` once) — keep the test's expectations.

- [ ] **Step 5: Run tests**

Run: `cd backend && gofmt -l internal/mib; go vet ./internal/mib/ && go test ./internal/mib/ -v 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/mib
git commit -m "feat(mib): inspect and build MIB modules, with the IETF modules built in

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Migration 056 and models

**Files:**
- Create: `backend/migrations/056_custom_metrics.sql`, `backend/internal/models/custom_metrics.go`
- Modify: `backend/internal/models/audit*.go` (actions/resources), `backend/internal/models/monitor.go` (`Incident` gains two fields)
- Test: `backend/internal/services/custom_metrics_schema_db_test.go`

**Interfaces:**
- Produces (Go, package `models`):
  - `MIBModule{ID uuid.UUID; Name, Source, FileName string; SizeBytes int; SHA256, Content string; Imports, Missing StringArray; Status string; UploadedBy *uuid.UUID; CreatedAt, UpdatedAt time.Time}` table `mib_modules`; consts `MIBSourceBuiltin="builtin"`, `MIBSourceUpload="upload"`, `MIBStatusReady="ready"`, `MIBStatusWaiting="waiting"`
  - `MIBObject{ID int64; ModuleID uuid.UUID; Name, OID, ParentOID, Kind, BaseType, TypeName, Units, Access, Description string; Enum EnumMap; IndexColumns StringArray}` table `mib_objects`
  - `MetricProfile{ID uuid.UUID; Name, Description string; MatchPrefixes StringArray; PollIntervalMinutes int; Builtin bool; CreatedAt, UpdatedAt time.Time}` table `metric_profiles`
  - `ProfileMetric{ID uuid.UUID; ProfileID uuid.UUID; Name, Key, Source, Kind, Units string; Scale float64; OID, OID2, PrecisionOID string; FilterOID string; FilterValues StringArray; LabelMode, LabelOID, LabelPointerOID, LabelTargetOID string; OKStates Int64Array; StateNames EnumMap; RuleKind string; RuleValue *float64; RuleHoldMinutes int; RuleEnabled bool; Position int; CreatedAt, UpdatedAt time.Time}` table `profile_metrics`; consts for Source (`scalar|column|used_free_pct`), Kind (`gauge|counter|status`), LabelMode (`index|column|same_index|pointer`), RuleKind (`""|above|below|not_ok`)
  - `DeviceProfileOverride{DeviceID, ProfileID uuid.UUID; Mode string}` (`attach|detach`) table `device_profile_overrides`
  - `DeviceProfileRun{DeviceID, ProfileID uuid.UUID; RanAt time.Time; OK bool; Error string}` table `device_profile_runs`
  - `models.IncidentConditionMetric = "metric"`; `Incident.MetricKey *string`, `Incident.MetricInstance *string`
  - helper types `StringArray` (`[]string` ↔ Postgres `text[]` via `pq`-style Scan/Value — reuse an existing one if `grep -rn "text\[\]" backend/internal/models` shows a type), `Int64Array`, `EnumMap` (`map[int64]string` ↔ jsonb)
  - audit: `ActionMIBUploaded="mib_uploaded"`, `ActionMIBDeleted="mib_deleted"`, `ActionProfileCreated="metric_profile_created"`, `ActionProfileUpdated="metric_profile_updated"`, `ActionProfileDeleted="metric_profile_deleted"`, `ActionMetricCreated="custom_metric_created"`, `ActionMetricUpdated="custom_metric_updated"`, `ActionMetricDeleted="custom_metric_deleted"`; `ResourceMIBModule="mib_module"`, `ResourceMetricProfile="metric_profile"`, `ResourceCustomMetric="custom_metric"`

- [ ] **Step 1: Write the failing DB test** `backend/internal/services/custom_metrics_schema_db_test.go`:

```go
package services

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBCustomMetricsSchema(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	// A metric incident needs both key and instance.
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, start_time, incident_type) VALUES (?, 'metric', now(), 'error')`, s.DeviceID).Error; err == nil {
		t.Error("metric incident without key accepted")
	}
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, condition, metric_key, metric_instance, start_time, incident_type) VALUES (?, 'metric', 'cisco_fan_state', '1004', now(), 'error')`, s.DeviceID)
	// One open per (device, key, instance).
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, metric_key, metric_instance, start_time, incident_type) VALUES (?, 'metric', 'cisco_fan_state', '1004', now(), 'error')`, s.DeviceID).Error; err == nil {
		t.Error("second open metric incident accepted")
	}
	// Other conditions must leave the metric columns NULL.
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, metric_key, start_time, incident_type) VALUES (?, 'ups_on_battery', 'x', now(), 'error')`, s.DeviceID).Error; err == nil {
		t.Error("UPS incident with a metric key accepted")
	}
	// Profiles, metrics, overrides, runs, modules, objects exist and cascade.
	p := uuid.New()
	testdb.Exec(t, db, `INSERT INTO metric_profiles (id, name, match_prefixes) VALUES (?, 'P', '{1.3.6.1.4.1.9.1}')`, p)
	testdb.Exec(t, db, `INSERT INTO profile_metrics (profile_id, name, key, source, kind, oid, label_mode) VALUES (?, 'CPU', 'cisco_cpu_5min', 'column', 'gauge', '1.3.6.1.4.1.9.9.109.1.1.1.1.8', 'index')`, p)
	if err := db.Exec(`INSERT INTO profile_metrics (profile_id, name, key, source, kind, oid, label_mode) VALUES (?, 'Bad', 'ups_x', 'column', 'gauge', '1.3', 'index')`, p).Error; err == nil {
		t.Error("reserved key prefix accepted")
	}
	testdb.Exec(t, db, `INSERT INTO device_profile_overrides (device_id, profile_id, mode) VALUES (?, ?, 'detach')`, s.DeviceID, p)
	testdb.Exec(t, db, `INSERT INTO device_profile_runs (device_id, profile_id, ran_at, ok) VALUES (?, ?, now(), true)`, s.DeviceID, p)
	testdb.Exec(t, db, `DELETE FROM metric_profiles WHERE id = ?`, p)
	var n int64
	testdb.Must(t, db.Raw(`SELECT (SELECT count(*) FROM profile_metrics) + (SELECT count(*) FROM device_profile_overrides) + (SELECT count(*) FROM device_profile_runs)`).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d rows survived the profile delete", n)
	}
	testdb.Exec(t, db, `UPDATE metrics.series SET label = 'x' WHERE false`)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && ./scripts/test-db.sh -run CustomMetricsSchema -v 2>&1 | grep -E "^(--- |FAIL)|_test.go" | head`
Expected: FAIL (relation "metric_profiles" does not exist / column "metric_key" does not exist).

- [ ] **Step 3: Migration** `backend/migrations/056_custom_metrics.sql`:

```sql
-- 056_custom_metrics.sql
-- Network phase 3: the MIB library, metric profiles and custom metrics, the
-- label on a metrics series, and metric-rule incidents.

CREATE TABLE IF NOT EXISTS mib_modules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    source      TEXT NOT NULL CHECK (source IS NOT NULL AND source IN ('builtin', 'upload')),
    file_name   TEXT NOT NULL DEFAULT '',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    sha256      TEXT NOT NULL,
    content     TEXT NOT NULL,
    imports     TEXT[] NOT NULL DEFAULT '{}',
    missing     TEXT[] NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL CHECK (status IS NOT NULL AND status IN ('ready', 'waiting')),
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS mib_objects (
    id            BIGSERIAL PRIMARY KEY,
    module_id     UUID NOT NULL REFERENCES mib_modules(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    oid           TEXT NOT NULL,
    parent_oid    TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL,
    base_type     TEXT NOT NULL DEFAULT '',
    type_name     TEXT NOT NULL DEFAULT '',
    units         TEXT NOT NULL DEFAULT '',
    access        TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    enum          JSONB,
    index_columns TEXT[] NOT NULL DEFAULT '{}',
    UNIQUE (module_id, name)
);
CREATE INDEX IF NOT EXISTS idx_mib_objects_oid ON mib_objects (oid);
CREATE INDEX IF NOT EXISTS idx_mib_objects_parent ON mib_objects (parent_oid);
CREATE INDEX IF NOT EXISTS idx_mib_objects_lname ON mib_objects (lower(name));
CREATE INDEX IF NOT EXISTS idx_mib_objects_search ON mib_objects
    USING gin (to_tsvector('simple', name || ' ' || description));

CREATE TABLE IF NOT EXISTS metric_profiles (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT NOT NULL UNIQUE,
    description           TEXT NOT NULL DEFAULT '',
    match_prefixes        TEXT[] NOT NULL DEFAULT '{}',
    poll_interval_minutes INTEGER NOT NULL DEFAULT 1 CHECK (poll_interval_minutes IN (1, 5, 15)),
    builtin               BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS profile_metrics (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id        UUID NOT NULL REFERENCES metric_profiles(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    key               TEXT NOT NULL UNIQUE
        CHECK (key ~ '^[a-z][a-z0-9_]{2,62}$' AND key NOT LIKE 'if\_%' AND key NOT LIKE 'ups\_%'),
    source            TEXT NOT NULL CHECK (source IS NOT NULL AND source IN ('scalar', 'column', 'used_free_pct')),
    kind              TEXT NOT NULL CHECK (kind IS NOT NULL AND kind IN ('gauge', 'counter', 'status')),
    units             TEXT NOT NULL DEFAULT '',
    scale             DOUBLE PRECISION NOT NULL DEFAULT 1,
    oid               TEXT NOT NULL,
    oid2              TEXT NOT NULL DEFAULT '',
    precision_oid     TEXT NOT NULL DEFAULT '',
    filter_oid        TEXT NOT NULL DEFAULT '',
    filter_values     TEXT[] NOT NULL DEFAULT '{}',
    label_mode        TEXT NOT NULL DEFAULT 'index'
        CHECK (label_mode IS NOT NULL AND label_mode IN ('index', 'column', 'same_index', 'pointer')),
    label_oid         TEXT NOT NULL DEFAULT '',
    label_pointer_oid TEXT NOT NULL DEFAULT '',
    label_target_oid  TEXT NOT NULL DEFAULT '',
    ok_states         BIGINT[] NOT NULL DEFAULT '{}',
    state_names       JSONB,
    rule_kind         TEXT NOT NULL DEFAULT '' CHECK (rule_kind IN ('', 'above', 'below', 'not_ok')),
    rule_value        DOUBLE PRECISION,
    rule_hold_minutes INTEGER NOT NULL DEFAULT 0 CHECK (rule_hold_minutes BETWEEN 0 AND 1440),
    rule_enabled      BOOLEAN NOT NULL DEFAULT false,
    position          INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_profile_metrics_profile ON profile_metrics (profile_id, position);

CREATE TABLE IF NOT EXISTS device_profile_overrides (
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES metric_profiles(id) ON DELETE CASCADE,
    mode       TEXT NOT NULL CHECK (mode IS NOT NULL AND mode IN ('attach', 'detach')),
    PRIMARY KEY (device_id, profile_id)
);

CREATE TABLE IF NOT EXISTS device_profile_runs (
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES metric_profiles(id) ON DELETE CASCADE,
    ran_at     TIMESTAMPTZ NOT NULL,
    ok         BOOLEAN NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, profile_id)
);

ALTER TABLE metrics.series ADD COLUMN IF NOT EXISTS label TEXT NOT NULL DEFAULT '';

-- Metric-rule incidents: device-level condition incidents with the metric
-- key and row they are about.
ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS metric_key      TEXT,
    ADD COLUMN IF NOT EXISTS metric_instance TEXT;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_port_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_port_check CHECK (
    (interface_id IS NULL AND condition IS NULL AND metric_key IS NULL AND metric_instance IS NULL)
    OR (interface_id IS NOT NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('link_down', 'errors', 'flapping', 'slow_link', 'saturated')
        AND metric_key IS NULL AND metric_instance IS NULL)
    OR (interface_id IS NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('ups_on_battery', 'ups_low_battery', 'ups_high_load')
        AND metric_key IS NULL AND metric_instance IS NULL)
    OR (interface_id IS NULL AND device_id IS NOT NULL AND condition IS NOT NULL AND condition = 'metric'
        AND metric_key IS NOT NULL AND metric_instance IS NOT NULL)
);
-- uq_incidents_open_device_condition (055) is one open per (device, condition);
-- metric incidents are one open per (device, key, instance) instead.
DROP INDEX IF EXISTS uq_incidents_open_device_condition;
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_device_condition
    ON incidents (device_id, condition)
    WHERE end_time IS NULL AND interface_id IS NULL AND condition IS NOT NULL AND condition <> 'metric';
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_metric
    ON incidents (device_id, metric_key, metric_instance)
    WHERE end_time IS NULL AND condition = 'metric';
```

- [ ] **Step 4: Models.** Write `backend/internal/models/custom_metrics.go` with the structs and consts listed in Interfaces, `TableName()` methods, and gorm column tags matching the SQL names (`gorm:"column:match_prefixes;type:text[]"`, `gorm:"column:enum;type:jsonb"` etc.). For `StringArray`/`Int64Array`, first `grep -rn "pq.StringArray\|StringArray\b" backend/internal/models | head` — reuse what exists (e.g. `pq.StringArray`, `pq.Int64Array` from `github.com/lib/pq` if already a dependency); otherwise implement `Scan`/`Value` for the Postgres array literal format. `EnumMap` implements `Scan`/`Value` via `encoding/json` with string keys (`"1":"normal"`) converted to `int64`.

In `Incident` (`backend/internal/models/monitor.go`) after `Condition`:

```go
	// MetricKey and MetricInstance identify the row of a metric-rule incident
	// (Condition "metric"); nil otherwise.
	MetricKey      *string `json:"metric_key" gorm:"column:metric_key"`
	MetricInstance *string `json:"metric_instance" gorm:"column:metric_instance"`
```

and add `IncidentConditionMetric = "metric"` next to the UPS condition consts in `models/snmp.go`. Add the audit consts listed above to the audit file's two const blocks.

- [ ] **Step 5: Run tests**

Run: `cd backend && gofmt -l internal; go vet ./... && ./scripts/test-db.sh -run 'CustomMetricsSchema|UPSIncident|DeviceCondition|PortIncident' -v 2>&1 | grep -E "^(--- |FAIL|ok  .*services)"`
Expected: all PASS (the UPS and port incident tests prove the CHECK rewrite kept their branches).

- [ ] **Step 6: Commit**

```bash
git add backend/migrations/056_custom_metrics.sql backend/internal/models backend/internal/services/custom_metrics_schema_db_test.go
git commit -m "feat(metrics): schema for MIBs, metric profiles, custom metrics and metric incidents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 3: `MIBLibrary` service — built-ins, upload, rebuild, delete

**Files:**
- Create: `backend/internal/services/mib_library.go`, `backend/internal/services/mib_upload.go`
- Test: `backend/internal/services/mib_upload_test.go` (pure), `backend/internal/services/mib_library_db_test.go`

**Interfaces:**
- Consumes: Task 1 `mib.Inspect`, `mib.Build`, `mib.Builtins`, `mib.ErrNotAMIB`, `*mib.ParseError`; Task 2 `models.MIBModule`, `models.MIBObject`.
- Produces:
  - `type UploadFile struct { FileName string; Content []byte }`
  - consts `MaxMIBFileBytes = 2 << 20`, `MaxMIBUploadBytes = 20 << 20`, `MaxMIBZipFiles = 500`
  - `func ExpandUpload(files []UploadFile) ([]UploadFile, error)` — `.zip` entries read in memory (base names only; directories, dotfiles and `__MACOSX/` skipped); errors `ErrMIBUploadTooLarge`, `ErrMIBFileTooLarge`, `ErrMIBTooManyFiles`
  - `type MIBUploadError struct { File string; Err error }` (`Error()` → `"<file>: <err>"`)
  - `type MIBUploadResult struct { Saved []string; Skipped []string; Waiting map[string][]string }`
  - `func NewMIBLibrary(db *gorm.DB) *MIBLibrary`
  - `func (l *MIBLibrary) SyncBuiltins(ctx context.Context) error`
  - `func (l *MIBLibrary) Upload(ctx context.Context, files []UploadFile, by uuid.UUID) (*MIBUploadResult, error)`
  - `func (l *MIBLibrary) Delete(ctx context.Context, id uuid.UUID) (*models.MIBModule, error)` — errors `ErrMIBNotFound`, `ErrMIBBuiltin`, `*MIBInUseError{Modules, Metrics []string}`
  - `type MIBModuleView struct { models.MIBModule; Objects int }` (Content omitted from JSON: `json:"-"` on Content in the model)
  - `func (l *MIBLibrary) List(ctx context.Context) ([]MIBModuleView, error)`

- [ ] **Step 1: Write the failing tests.**

`backend/internal/services/mib_upload_test.go`:

```go
package services

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A vendor bundle: nested folders, a README, macOS junk. Folders and dot
// files are dropped here; the README is kept (Upload skips it as not a MIB).
func TestExpandUploadZip(t *testing.T) {
	z := zipOf(t, map[string]string{
		"vendor/v2/ACME-MIB.my": "ACME-MIB DEFINITIONS ::= BEGIN END",
		"vendor/README.txt":     "Read me",
		"__MACOSX/._ACME-MIB.my": "junk",
		"vendor/.DS_Store":       "junk",
	})
	out, err := ExpandUpload([]UploadFile{{FileName: "bundle.zip", Content: z}})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range out {
		names = append(names, f.FileName)
	}
	if strings.Join(sortedCopy(names), ",") != "ACME-MIB.my,README.txt" {
		t.Errorf("files %v", names)
	}
}

func TestExpandUploadLimits(t *testing.T) {
	big := make([]byte, MaxMIBFileBytes+1)
	if _, err := ExpandUpload([]UploadFile{{FileName: "BIG.mib", Content: big}}); !errors.Is(err, ErrMIBFileTooLarge) {
		t.Errorf("big file: %v", err)
	}
	many := map[string]string{}
	for i := 0; i <= MaxMIBZipFiles; i++ {
		many[strings.Repeat("A", 3)+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".mib"] = "x"
	}
	if _, err := ExpandUpload([]UploadFile{{FileName: "m.zip", Content: zipOf(t, many)}}); !errors.Is(err, ErrMIBTooManyFiles) {
		t.Errorf("many files: %v", err)
	}
}
```

(`sortedCopy` — add to the test file: copy, `sort.Strings`, return.)

`backend/internal/services/mib_library_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/mib"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

const acmeSMI = "ACME-SMI DEFINITIONS ::= BEGIN\nIMPORTS enterprises FROM SNMPv2-SMI;\nacme OBJECT IDENTIFIER ::= { enterprises 99999 }\nEND\n"
const acmeMIB = "ACME-MIB DEFINITIONS ::= BEGIN\nIMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI acme FROM ACME-SMI;\n" +
	"acmeTemp OBJECT-TYPE SYNTAX Integer32 MAX-ACCESS read-only STATUS current DESCRIPTION \"Temperature.\" ::= { acme 1 }\nEND\n"

func mibLib(t *testing.T) (*MIBLibrary, context.Context) {
	t.Helper()
	db := testdb.Open(t)
	l := NewMIBLibrary(db)
	ctx := context.Background()
	testdb.Must(t, l.SyncBuiltins(ctx))
	return l, ctx
}

func module(t *testing.T, l *MIBLibrary, name string) MIBModuleView {
	t.Helper()
	list, err := l.List(context.Background())
	testdb.Must(t, err)
	for _, m := range list {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("module %s not listed", name)
	return MIBModuleView{}
}

func TestDBMIBBuiltinsSynced(t *testing.T) {
	l, _ := mibLib(t)
	if m := module(t, l, "UPS-MIB"); m.Source != models.MIBSourceBuiltin || m.Status != models.MIBStatusReady || m.Objects < 100 {
		t.Errorf("UPS-MIB %+v", m.MIBModule)
	}
	testdb.Must(t, l.SyncBuiltins(context.Background())) // idempotent
}

func TestDBMIBUploadWaitingThenReady(t *testing.T) {
	l, ctx := mibLib(t)
	res, err := l.Upload(ctx, []UploadFile{{FileName: "acme.my", Content: []byte(acmeMIB)}}, uuid.Nil)
	testdb.Must(t, err)
	if strings.Join(res.Waiting["ACME-MIB"], ",") != "ACME-SMI" {
		t.Fatalf("waiting %v", res.Waiting)
	}
	if m := module(t, l, "ACME-MIB"); m.Status != models.MIBStatusWaiting || m.Objects != 0 {
		t.Errorf("before: %+v", m.MIBModule)
	}
	_, err = l.Upload(ctx, []UploadFile{{FileName: "smi.txt", Content: []byte(acmeSMI)}}, uuid.Nil)
	testdb.Must(t, err)
	if m := module(t, l, "ACME-MIB"); m.Status != models.MIBStatusReady || m.Objects == 0 {
		t.Errorf("after: %+v", m.MIBModule)
	}
}

// Review focus 1: a zip with a README is saved minus the README.
func TestDBMIBUploadSkipsNonMIBs(t *testing.T) {
	l, ctx := mibLib(t)
	z := zipOf(t, map[string]string{"v2/ACME-SMI.my": acmeSMI, "v2/ACME-MIB.my": acmeMIB, "README.txt": "Cisco-style readme"})
	res, err := l.Upload(ctx, []UploadFile{{FileName: "bundle.zip", Content: z}}, uuid.Nil)
	testdb.Must(t, err)
	if strings.Join(sortedCopy(res.Saved), ",") != "ACME-MIB,ACME-SMI" || strings.Join(res.Skipped, ",") != "README.txt" {
		t.Errorf("saved %v skipped %v", res.Saved, res.Skipped)
	}
}

// One bad file rejects the whole upload, naming the file and line.
func TestDBMIBUploadAllOrNothing(t *testing.T) {
	l, ctx := mibLib(t)
	bad := "BAD-MIB DEFINITIONS ::= BEGIN\nx OBJECT IDENTIFIER ::= { enterprises 1\nEND\n"
	_, err := l.Upload(ctx, []UploadFile{{FileName: "smi.my", Content: []byte(acmeSMI)}, {FileName: "bad.my", Content: []byte(bad)}}, uuid.Nil)
	var ue *MIBUploadError
	var pe *mib.ParseError
	if !errors.As(err, &ue) || ue.File != "bad.my" || !errors.As(err, &pe) || pe.Line == 0 {
		t.Fatalf("err %v", err)
	}
	list, _ := l.List(ctx)
	for _, m := range list {
		if m.Name == "ACME-SMI" {
			t.Error("good file saved from a rejected upload")
		}
	}
}

// Review focus 4: the same module under another file name replaces it.
func TestDBMIBReuploadReplaces(t *testing.T) {
	l, ctx := mibLib(t)
	_, err := l.Upload(ctx, []UploadFile{{FileName: "smi.my", Content: []byte(acmeSMI)}}, uuid.Nil)
	testdb.Must(t, err)
	v2 := strings.Replace(acmeSMI, "99999", "99998", 1)
	_, err = l.Upload(ctx, []UploadFile{{FileName: "ACME-SMI-v2.txt", Content: []byte(v2)}}, uuid.Nil)
	testdb.Must(t, err)
	n := 0
	list, _ := l.List(ctx)
	for _, m := range list {
		if m.Name == "ACME-SMI" {
			n++
			if m.FileName != "ACME-SMI-v2.txt" {
				t.Errorf("file name %s", m.FileName)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d ACME-SMI rows", n)
	}
}

func TestDBMIBDeleteRefusals(t *testing.T) {
	l, ctx := mibLib(t)
	_, err := l.Upload(ctx, []UploadFile{{FileName: "smi.my", Content: []byte(acmeSMI)}, {FileName: "mib.my", Content: []byte(acmeMIB)}}, uuid.Nil)
	testdb.Must(t, err)
	var inUse *MIBInUseError
	if _, err := l.Delete(ctx, module(t, l, "ACME-SMI").ID); !errors.As(err, &inUse) || strings.Join(inUse.Modules, ",") != "ACME-MIB" {
		t.Errorf("delete imported module: %v", err)
	}
	if _, err := l.Delete(ctx, module(t, l, "UPS-MIB").ID); !errors.Is(err, ErrMIBBuiltin) {
		t.Errorf("delete built-in: %v", err)
	}
	if _, err := l.Delete(ctx, module(t, l, "ACME-MIB").ID); err != nil {
		t.Errorf("delete leaf: %v", err)
	}
}
```

Add one more case to `TestDBMIBDeleteRefusals` after Task 7 exists (it adds the metric reference check's test there).

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go vet ./internal/services/ 2>&1 | head -3`
Expected: undefined `ExpandUpload`, `NewMIBLibrary`, ….

- [ ] **Step 3: Implement** `backend/internal/services/mib_upload.go`:

```go
package services

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	MaxMIBFileBytes   = 2 << 20
	MaxMIBUploadBytes = 20 << 20
	MaxMIBZipFiles    = 500
)

var (
	ErrMIBUploadTooLarge = errors.New("the upload is larger than 20 MB")
	ErrMIBFileTooLarge   = errors.New("a MIB file is larger than 2 MB")
	ErrMIBTooManyFiles   = errors.New("the zip holds more than 500 files")
)

// UploadFile is one uploaded file: a MIB, or a zip of them.
type UploadFile struct {
	FileName string
	Content  []byte
}

// ExpandUpload replaces each .zip with its files (read in memory; folders,
// dot files and __MACOSX entries dropped; base names only) and enforces the
// size and count limits.
func ExpandUpload(files []UploadFile) ([]UploadFile, error) {
	total := 0
	var out []UploadFile
	for _, f := range files {
		total += len(f.Content)
		if total > MaxMIBUploadBytes {
			return nil, ErrMIBUploadTooLarge
		}
		if !strings.EqualFold(path.Ext(f.FileName), ".zip") {
			if len(f.Content) > MaxMIBFileBytes {
				return nil, fmt.Errorf("%s: %w", f.FileName, ErrMIBFileTooLarge)
			}
			out = append(out, UploadFile{FileName: path.Base(f.FileName), Content: f.Content})
			continue
		}
		zr, err := zip.NewReader(bytes.NewReader(f.Content), int64(len(f.Content)))
		if err != nil {
			return nil, fmt.Errorf("%s: not a readable zip: %w", f.FileName, err)
		}
		if len(zr.File) > MaxMIBZipFiles {
			return nil, ErrMIBTooManyFiles
		}
		unpacked := 0
		for _, zf := range zr.File {
			base := path.Base(zf.Name)
			if zf.FileInfo().IsDir() || strings.HasPrefix(base, ".") || strings.HasPrefix(zf.Name, "__MACOSX/") {
				continue
			}
			if zf.UncompressedSize64 > MaxMIBFileBytes {
				return nil, fmt.Errorf("%s: %w", base, ErrMIBFileTooLarge)
			}
			rc, err := zf.Open()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", base, err)
			}
			b, err := io.ReadAll(io.LimitReader(rc, MaxMIBFileBytes+1))
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", base, err)
			}
			if len(b) > MaxMIBFileBytes {
				return nil, fmt.Errorf("%s: %w", base, ErrMIBFileTooLarge)
			}
			unpacked += len(b)
			if unpacked > MaxMIBUploadBytes {
				return nil, ErrMIBUploadTooLarge
			}
			out = append(out, UploadFile{FileName: base, Content: b})
		}
	}
	return out, nil
}
```

`backend/internal/services/mib_library.go`:

```go
package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/mib"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

var (
	ErrMIBNotFound = errors.New("MIB module not found")
	ErrMIBBuiltin  = errors.New("built-in MIB modules cannot be deleted")
)

// MIBUploadError names the uploaded file that failed.
type MIBUploadError struct {
	File string
	Err  error
}

func (e *MIBUploadError) Error() string { return e.File + ": " + e.Err.Error() }
func (e *MIBUploadError) Unwrap() error { return e.Err }

// MIBInUseError lists what still depends on a module.
type MIBInUseError struct {
	Modules []string
	Metrics []string
}

func (e *MIBInUseError) Error() string {
	return fmt.Sprintf("in use by modules %v and metrics %v", e.Modules, e.Metrics)
}

type MIBUploadResult struct {
	Saved   []string            `json:"saved"`
	Skipped []string            `json:"skipped"`
	Waiting map[string][]string `json:"waiting"`
}

type MIBModuleView struct {
	models.MIBModule
	Objects int `json:"objects" gorm:"column:objects"`
}

// MIBLibrary stores MIB modules and their objects. Every change rebuilds the
// whole object tree (modules number in the tens), so a module waiting for an
// import becomes ready the moment the import arrives.
type MIBLibrary struct {
	db *gorm.DB
	mu sync.Mutex // one rebuild at a time
}

func NewMIBLibrary(db *gorm.DB) *MIBLibrary { return &MIBLibrary{db: db} }

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SyncBuiltins inserts or refreshes the embedded modules — unless an upload
// of the same name replaced one — and rebuilds when anything changed.
func (l *MIBLibrary) SyncBuiltins(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := false
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, f := range mib.Builtins() {
			var existing models.MIBModule
			err := tx.Where("name = ?", f.Name).Limit(1).Find(&existing).Error
			if err != nil {
				return err
			}
			h := hashOf(f.Content)
			if existing.ID != uuid.Nil && (existing.Source == models.MIBSourceUpload || existing.SHA256 == h) {
				continue
			}
			if err := upsertModule(tx, f, models.MIBSourceBuiltin, f.Name+".txt", nil); err != nil {
				return err
			}
			changed = true
		}
		var n int64
		if err := tx.Model(&models.MIBObject{}).Count(&n).Error; err != nil {
			return err
		}
		if changed || n == 0 {
			return rebuild(tx)
		}
		return nil
	})
	return err
}

func upsertModule(tx *gorm.DB, f mib.File, source, fileName string, by *uuid.UUID) error {
	h, _ := mib.Inspect([]byte(f.Content))
	now := time.Now().UTC()
	return tx.Exec(`INSERT INTO mib_modules (name, source, file_name, size_bytes, sha256, content, imports, status, uploaded_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'waiting', ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET source = EXCLUDED.source, file_name = EXCLUDED.file_name,
			size_bytes = EXCLUDED.size_bytes, sha256 = EXCLUDED.sha256, content = EXCLUDED.content,
			imports = EXCLUDED.imports, uploaded_by = EXCLUDED.uploaded_by, updated_at = EXCLUDED.updated_at`,
		f.Name, source, fileName, len(f.Content), hashOf(f.Content), f.Content, models.StringArray(h.Imports), by, now, now).Error
}

// rebuild recomputes every module's status and replaces all objects.
func rebuild(tx *gorm.DB) error {
	var mods []models.MIBModule
	if err := tx.Find(&mods).Error; err != nil {
		return err
	}
	files := make([]mib.File, len(mods))
	ids := map[string]uuid.UUID{}
	for i, m := range mods {
		files[i] = mib.File{Name: m.Name, Content: m.Content}
		ids[m.Name] = m.ID
	}
	res := mib.Build(files)
	for _, m := range mods {
		status := models.MIBStatusReady
		if len(res.Missing[m.Name]) > 0 {
			status = models.MIBStatusWaiting
		}
		if err := tx.Model(&models.MIBModule{}).Where("id = ?", m.ID).
			Updates(map[string]any{"status": status, "missing": models.StringArray(res.Missing[m.Name])}).Error; err != nil {
			return err
		}
	}
	if err := tx.Exec(`DELETE FROM mib_objects`).Error; err != nil {
		return err
	}
	var rows []models.MIBObject
	for name, objs := range res.Objects {
		for _, o := range objs {
			rows = append(rows, models.MIBObject{ModuleID: ids[name], Name: o.Name, OID: o.OID, ParentOID: o.ParentOID,
				Kind: o.Kind, BaseType: o.BaseType, TypeName: o.TypeName, Units: o.Units, Access: o.Access,
				Description: o.Description, Enum: models.EnumMap(o.Enum), IndexColumns: models.StringArray(o.IndexColumns)})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.CreateInBatches(rows, 500).Error
}

// Upload saves every MIB in files, or none: a file that fails to parse
// rejects the whole upload. Files that are not MIBs at all are skipped.
func (l *MIBLibrary) Upload(ctx context.Context, files []UploadFile, by uuid.UUID) (*MIBUploadResult, error) {
	expanded, err := ExpandUpload(files)
	if err != nil {
		return nil, err
	}
	res := &MIBUploadResult{Waiting: map[string][]string{}}
	var mods []mib.File
	fileNames := map[string]string{}
	for _, f := range expanded {
		h, err := mib.Inspect(f.Content)
		if errors.Is(err, mib.ErrNotAMIB) {
			res.Skipped = append(res.Skipped, f.FileName)
			continue
		}
		if err != nil {
			return nil, &MIBUploadError{File: f.FileName, Err: err}
		}
		if prev, dup := fileNames[h.Name]; dup {
			return nil, &MIBUploadError{File: f.FileName, Err: fmt.Errorf("module %s is also in %s", h.Name, prev)}
		}
		fileNames[h.Name] = f.FileName
		mods = append(mods, mib.File{Name: h.Name, Content: string(f.Content)})
	}
	var byPtr *uuid.UUID
	if by != uuid.Nil {
		byPtr = &by
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err = l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range mods {
			if err := upsertModule(tx, m, models.MIBSourceUpload, fileNames[m.Name], byPtr); err != nil {
				return err
			}
		}
		if err := rebuild(tx); err != nil {
			return err
		}
		var waiting []models.MIBModule
		if err := tx.Where("name IN ? AND status = ?", keysOf(fileNames), models.MIBStatusWaiting).Find(&waiting).Error; err != nil {
			return err
		}
		for _, w := range waiting {
			res.Waiting[w.Name] = w.Missing
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, m := range mods {
		res.Saved = append(res.Saved, m.Name)
	}
	sort.Strings(res.Saved)
	return res, nil
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// Delete removes an uploaded module unless something depends on it. An
// upload that had replaced a built-in brings the built-in back.
func (l *MIBLibrary) Delete(ctx context.Context, id uuid.UUID) (*models.MIBModule, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var m models.MIBModule
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).Limit(1).Find(&m).Error; err != nil {
			return err
		}
		if m.ID == uuid.Nil {
			return ErrMIBNotFound
		}
		if m.Source == models.MIBSourceBuiltin {
			return ErrMIBBuiltin
		}
		inUse := &MIBInUseError{}
		if err := tx.Raw(`SELECT name FROM mib_modules WHERE ? = ANY(imports) ORDER BY name`, m.Name).Scan(&inUse.Modules).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT DISTINCT pm.key FROM profile_metrics pm JOIN mib_objects o ON o.module_id = ?
			AND o.oid IN (pm.oid, pm.oid2, pm.precision_oid, pm.filter_oid, pm.label_oid, pm.label_pointer_oid, pm.label_target_oid)
			ORDER BY pm.key`, m.ID).Scan(&inUse.Metrics).Error; err != nil {
			return err
		}
		if len(inUse.Modules) > 0 || len(inUse.Metrics) > 0 {
			return inUse
		}
		if err := tx.Delete(&models.MIBModule{}, "id = ?", m.ID).Error; err != nil {
			return err
		}
		for _, f := range mib.Builtins() {
			if f.Name == m.Name {
				if err := upsertModule(tx, f, models.MIBSourceBuiltin, f.Name+".txt", nil); err != nil {
					return err
				}
			}
		}
		return rebuild(tx)
	})
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// List returns every module with its object count, by name.
func (l *MIBLibrary) List(ctx context.Context) ([]MIBModuleView, error) {
	var out []MIBModuleView
	err := l.db.WithContext(ctx).Raw(`SELECT m.*, (SELECT count(*) FROM mib_objects o WHERE o.module_id = m.id) AS objects
		FROM mib_modules m ORDER BY m.name`).Scan(&out).Error
	return out, err
}
```

Note: the metric-reference query uses `profile_metrics`, which Task 2 created, so it works before Task 7. `MIBModule.Content` must carry `json:"-"`.

- [ ] **Step 4: Run tests**

Run: `cd backend && gofmt -l internal; go vet ./... && go test ./internal/services/ -run ExpandUpload -v 2>&1 | grep -E "^(--- |ok|FAIL)" && ./scripts/test-db.sh -run 'DBMIB' -v 2>&1 | grep -E "^(--- |FAIL|ok  .*services)"`
Expected: all PASS.

- [ ] **Step 5: Wire startup.** In `backend/cmd/sentinel/main.go`, after migrations and before the poller starts: `mibLibrary := services.NewMIBLibrary(db)` and `if err := mibLibrary.SyncBuiltins(ctx); err != nil { log.Printf("[mib] syncing built-in MIBs: %v", err) }` (log, do not exit). Run `go build ./...`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/mib_library.go backend/internal/services/mib_upload.go backend/internal/services/mib_upload_test.go backend/internal/services/mib_library_db_test.go backend/cmd/sentinel/main.go
git commit -m "feat(mib): the MIB library — built-ins, zip and file upload, delete with dependency checks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: MIB library and browser API

**Files:**
- Create: `backend/internal/services/mib_browse.go`, `backend/internal/api/mib_handler.go`
- Test: `backend/internal/services/mib_browse_db_test.go`, `backend/internal/api/mib_handler_test.go`
- Modify: `backend/cmd/sentinel/main.go` (register routes)

**Interfaces:**
- Consumes: Task 3 `MIBLibrary`.
- Produces:
  - `type MIBObjectView struct { models.MIBObject; Module string; HasChildren bool }` (json `module`, `has_children`)
  - `func (l *MIBLibrary) Children(ctx context.Context, parentOID string) ([]MIBObjectView, error)` — `""` returns the two roots `1.3.6.1.2.1` (mib-2) and `1.3.6.1.4.1` (enterprises) as synthetic nodes when no object has those OIDs
  - `func (l *MIBLibrary) Search(ctx context.Context, q string, limit int) ([]MIBObjectView, error)` — name (case-insensitive substring), OID prefix (when q starts with a digit), or description words; at most `limit` (100)
  - `func (l *MIBLibrary) Object(ctx context.Context, oid string) (*MIBObjectDetail, error)` — `MIBObjectDetail{MIBObjectView; Columns []MIBObjectView}` (Columns: for a table, its row's columns; for a row, its columns); `ErrMIBObjectNotFound`
  - Every query picks, per OID, the object from the most recently updated **ready** module (`DISTINCT ON (o.oid) … ORDER BY o.oid, m.updated_at DESC`), and orders children numerically (`string_to_array(o.oid, '.')::int[]`).
  - Routes (all under `/api/v1/network/mibs`):
    - any signed-in user: `GET /objects/children?oid=`, `GET /objects/search?q=`, `GET /objects/by-oid?oid=`
    - admin (RequireAdmin): `GET ""` (list), `POST ""` (multipart field `files`, repeatable; 20 MB request cap via `http.MaxBytesReader`), `DELETE "/:id"`
    - Upload responds 200 with `MIBUploadResult`; `MIBUploadError` → 400 `"<file>: line N: …"`; size errors → 413; `MIBInUseError` → 409 with message `"used by modules X and metrics Y"`; `ErrMIBBuiltin` → 400; audit `mib_uploaded` (after: saved/skipped) and `mib_deleted` (before: name)

- [ ] **Step 1: Write the failing tests.**

`backend/internal/services/mib_browse_db_test.go`:

```go
package services

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBMIBBrowse(t *testing.T) {
	l, ctx := mibLib(t)
	roots, err := l.Children(ctx, "")
	testdb.Must(t, err)
	if len(roots) != 2 || roots[0].OID != "1.3.6.1.2.1" || roots[1].OID != "1.3.6.1.4.1" {
		t.Fatalf("roots %+v", roots)
	}
	kids, err := l.Children(ctx, "1.3.6.1.2.1.33.1.2")
	testdb.Must(t, err)
	if len(kids) == 0 || kids[0].Name != "upsBatteryStatus" || kids[0].Module != "UPS-MIB" {
		t.Errorf("upsBattery children %+v", kids)
	}
	// Numeric order: .2 before .10.
	for i := 1; i < len(kids); i++ {
		if oidLess(kids[i].OID, kids[i-1].OID) {
			t.Errorf("out of order %s after %s", kids[i].OID, kids[i-1].OID)
		}
	}
	hits, err := l.Search(ctx, "batterystatus", 100)
	testdb.Must(t, err)
	if len(hits) == 0 || hits[0].Name != "upsBatteryStatus" {
		t.Errorf("search %+v", hits)
	}
	if hits, _ := l.Search(ctx, "1.3.6.1.2.1.33.1.2.1", 100); len(hits) == 0 {
		t.Error("OID search found nothing")
	}
	d, err := l.Object(ctx, "1.3.6.1.2.1.47.1.1.1") // entPhysicalTable
	testdb.Must(t, err)
	if d.Kind != "table" || len(d.Columns) < 10 || d.Columns[0].Name != "entPhysicalIndex" {
		t.Errorf("table detail %+v (%d columns)", d.MIBObject, len(d.Columns))
	}
	// A newer upload of a module wins for its OIDs.
	_, err = l.Upload(ctx, []UploadFile{{FileName: "smi.my", Content: []byte(acmeSMI)}, {FileName: "mib.my", Content: []byte(acmeMIB)}}, uuid.Nil)
	testdb.Must(t, err)
	if d, err := l.Object(ctx, "1.3.6.1.4.1.99999.1"); err != nil || d.Name != "acmeTemp" {
		t.Errorf("acmeTemp %+v %v", d, err)
	}
}
```

(`oidLess(a, b string) bool` — numeric arc comparison; implement in `mib_browse.go` and export nothing.)

`backend/internal/api/mib_handler_test.go`: build a router the way `port_handler_test.go`'s `newPortRig` does (read it), with a fake `mibStore` interface that records calls. Assert: a non-admin GET `/api/v1/network/mibs` → 403; non-admin GET `/api/v1/network/mibs/objects/search?q=x` → 200; admin multipart POST with one file → 200 and fake upload called once; fake returning `&services.MIBUploadError{File: "bad.my", Err: errors.New("line 3: unexpected")}` → 400 with body containing `bad.my: line 3`; fake `Delete` returning `&services.MIBInUseError{Modules: []string{"ACME-MIB"}}` → 409.

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go vet ./internal/services/ ./internal/api/ 2>&1 | head -3`
Expected: undefined `Children`, `Search`, `Object`, `RegisterMIBRoutes`.

- [ ] **Step 3: Implement** `mib_browse.go` (queries per the Interfaces block; synthetic roots `MIBObjectView{MIBObject: models.MIBObject{Name: "mib-2", OID: "1.3.6.1.2.1", Kind: "node"}, HasChildren: true}` and `enterprises`/`1.3.6.1.4.1`; `HasChildren` via `EXISTS (SELECT 1 FROM mib_objects c WHERE c.parent_oid = o.oid)`; Search uses `lower(o.name) LIKE '%' || lower(?) || '%'` OR `o.oid LIKE ? || '%'` (digits) OR `to_tsvector('simple', o.name || ' ' || o.description) @@ plainto_tsquery('simple', ?)`, ordered by exact-name match first, then name length, limited) and `mib_handler.go`:

```go
// mibStore is MIBLibrary as the handlers use it.
type mibStore interface {
	List(ctx context.Context) ([]services.MIBModuleView, error)
	Upload(ctx context.Context, files []services.UploadFile, by uuid.UUID) (*services.MIBUploadResult, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.MIBModule, error)
	Children(ctx context.Context, parentOID string) ([]services.MIBObjectView, error)
	Search(ctx context.Context, q string, limit int) ([]services.MIBObjectView, error)
	Object(ctx context.Context, oid string) (*services.MIBObjectDetail, error)
}

// RegisterMIBRoutes mounts /network/mibs: browsing for every signed-in
// user; the library itself (list, upload, delete) for admins.
func RegisterMIBRoutes(rg *gin.RouterGroup, mibs mibStore, audit auditRecorder, users adminChecker) {
	g := rg.Group("/network/mibs")
	g.GET("/objects/children", func(c *gin.Context) {
		v, err := mibs.Children(c.Request.Context(), c.Query("oid"))
		if err != nil {
			respondInternal(c, "mibChildren", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	g.GET("/objects/search", func(c *gin.Context) {
		q := strings.TrimSpace(c.Query("q"))
		if len(q) < 2 {
			respondError(c, http.StatusBadRequest, "search needs at least 2 characters")
			return
		}
		v, err := mibs.Search(c.Request.Context(), q, 100)
		if err != nil {
			respondInternal(c, "mibSearch", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	g.GET("/objects/by-oid", func(c *gin.Context) {
		v, err := mibs.Object(c.Request.Context(), c.Query("oid"))
		if errors.Is(err, services.ErrMIBObjectNotFound) {
			respondError(c, http.StatusNotFound, "no MIB object at that OID")
			return
		}
		if err != nil {
			respondInternal(c, "mibObject", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	admin := g.Group("", RequireAdmin(users))
	admin.GET("", func(c *gin.Context) {
		v, err := mibs.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "mibList", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	admin.POST("", uploadMIBHandler(mibs, audit))
	admin.DELETE("/:id", deleteMIBHandler(mibs, audit))
}
```

`uploadMIBHandler`: wrap `c.Request.Body` with `http.MaxBytesReader(c.Writer, c.Request.Body, services.MaxMIBUploadBytes+1<<20)`, `c.Request.ParseMultipartForm(32 << 20)`; read every `files` header into `services.UploadFile` (read via `io.ReadAll(io.LimitReader(f, services.MaxMIBUploadBytes+1))`); a `*http.MaxBytesError` or the three size errors → 413; `*services.MIBUploadError` → 400 `err.Error()`; none uploaded → 400 `"choose at least one file"`; success → audit and 200. `deleteMIBHandler`: parse id; map errors (not found 404, builtin 400, in use 409 `"used by modules … and metrics …"` with empty lists omitted); audit; 204 or 200 `{deleted: name}` matching how `deleteCredentialHandler` responds (read it and match).

Register in `main.go`: `api.RegisterMIBRoutes(v1, mibLibrary, auditService, authService)` next to `RegisterNetworkRoutes` (use the same `users adminChecker` value that line passes).

- [ ] **Step 4: Run tests**

Run: `cd backend && gofmt -l internal cmd; go vet ./... && go test ./internal/api/ -run MIB -v 2>&1 | grep -E "^(--- |ok|FAIL)" && ./scripts/test-db.sh -run DBMIBBrowse -v 2>&1 | grep -E "^(--- |FAIL)"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/mib_browse.go backend/internal/services/mib_browse_db_test.go backend/internal/api/mib_handler.go backend/internal/api/mib_handler_test.go backend/cmd/sentinel/main.go
git commit -m "feat(mib): API to list, upload, delete and browse MIB modules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `internal/custommetric` — rows, rates and rules

**Files:**
- Create: `backend/internal/custommetric/custommetric.go`, `backend/internal/custommetric/rules.go`
- Test: `backend/internal/custommetric/custommetric_test.go`, `backend/internal/custommetric/rules_test.go`

**Interfaces:**
- Produces:
  - `type Value struct { Num float64; NumOK bool; Text string }`
  - `type Columns map[string]map[string]Value` (column OID → row index → value)
  - `type Definition struct { Name, Key, Source, Kind string; Scale float64; OID, OID2, PrecisionOID, FilterOID string; FilterValues []string; LabelMode, LabelOID, LabelPointerOID, LabelTargetOID string; OKStates []int64; StateNames map[int64]string; Rule Rule }`
  - `type Rule struct { Kind string; Value float64; Hold time.Duration; Enabled bool }` (Kind `above|below|not_ok`; `Enabled=false` or `Kind==""` means no rule)
  - `type Row struct { Instance, Label string; Value float64; State string; OK bool }`
  - `func Needed(d Definition) (every, cached []string)` — OIDs to walk each run vs. those cached with inventory (filter, label, pointer, target, precision). For `scalar` sources `every` holds `OID+".0"`; `Evaluate` reads it from `cols[OID+".0"][""]`.
  - `func Evaluate(d Definition, cols Columns) []Row` — sorted by instance (numeric arcs)
  - `type Sample struct { Value float64; At time.Time }`
  - `func Rates(prev map[string]Sample, rows []Row, at time.Time) ([]Row, map[string]Sample)` — for counters: rows with a usable previous sample get a per-second rate; a decrease is a 32-bit wrap when prev < 2^32, else dropped (reboot); first sight produces no row
  - `type RuleState struct { Active map[string]time.Time; Since map[string]time.Time }` (per instance: active since; violating since)
  - `type Change struct { Instance, Label string; Started bool; At time.Time; Value float64; State string }`
  - `func Violates(r Rule, kind string, row Row) bool`
  - `func EvalRule(r Rule, kind string, prev RuleState, rows []Row, now time.Time) (RuleState, []Change)` — missing rows keep state; a clear is the first poll back inside the limit; `prev` is not mutated

- [ ] **Step 1: Write the failing tests** `backend/internal/custommetric/custommetric_test.go`:

```go
package custommetric

import (
	"reflect"
	"testing"
	"time"
)

func n(v float64) Value { return Value{Num: v, NumOK: true} }
func s(t string) Value  { return Value{Text: t} }

const (
	cpuTable   = "1.3.6.1.4.1.9.9.109.1.1.1.1"
	cpu5min    = cpuTable + ".8"
	cpuPhys    = cpuTable + ".2"
	entName    = "1.3.6.1.2.1.47.1.1.1.1.7"
	sensorType = "1.3.6.1.4.1.9.9.91.1.1.1.1.1"
	sensorPrec = "1.3.6.1.4.1.9.9.91.1.1.1.1.3"
	sensorVal  = "1.3.6.1.4.1.9.9.91.1.1.1.1.4"
	fanState   = "1.3.6.1.4.1.9.9.13.1.4.1.3"
	fanDescr   = "1.3.6.1.4.1.9.9.13.1.4.1.2"
)

// Review focus 2: a CPU with physical index 0 (no entity) still gets a label.
func TestPointerLabelWithFallback(t *testing.T) {
	d := Definition{Name: "CPU busy (5 min)", Source: "column", Kind: "gauge", Scale: 1, OID: cpu5min,
		LabelMode: "pointer", LabelPointerOID: cpuPhys, LabelTargetOID: entName}
	cols := Columns{
		cpu5min: {"1": n(12), "2": n(7)},
		cpuPhys: {"1": n(1000), "2": n(0)},
		entName: {"1000": s("Switch 1")},
	}
	rows := Evaluate(d, cols)
	want := []Row{{Instance: "1", Label: "Switch 1", Value: 12}, {Instance: "2", Label: "Row 2", Value: 7}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows %+v", rows)
	}
}

// ENTITY-SENSOR: only celsius rows, value / 10^precision, label by entPhysicalName.
func TestFilterPrecisionSameIndex(t *testing.T) {
	d := Definition{Source: "column", Kind: "gauge", Scale: 1, OID: sensorVal, PrecisionOID: sensorPrec,
		FilterOID: sensorType, FilterValues: []string{"8"}, LabelMode: "same_index", LabelOID: entName}
	cols := Columns{
		sensorVal:  {"1010": n(415), "1011": n(12000)},
		sensorPrec: {"1010": n(1), "1011": n(3)},
		sensorType: {"1010": n(8), "1011": n(4)},
		entName:    {"1010": s("Switch 1 - Inlet Temp Sensor"), "1011": s("Switch 1 - 12V")},
	}
	rows := Evaluate(d, cols)
	if len(rows) != 1 || rows[0].Instance != "1010" || rows[0].Value != 41.5 || rows[0].Label != "Switch 1 - Inlet Temp Sensor" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestStatusStates(t *testing.T) {
	d := Definition{Source: "column", Kind: "status", Scale: 1, OID: fanState, LabelMode: "column", LabelOID: fanDescr,
		OKStates: []int64{1}, StateNames: map[int64]string{1: "normal", 3: "critical"}}
	rows := Evaluate(d, Columns{fanState: {"1": n(1), "2": n(3), "3": n(9)}, fanDescr: {"1": s("Fan 1"), "2": s("Fan 2")}})
	if len(rows) != 3 || !rows[0].OK || rows[0].State != "normal" || rows[1].OK || rows[1].State != "critical" ||
		rows[2].State != "9" || rows[2].Label != "Row 3" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestUsedFreePct(t *testing.T) {
	used, free := "1.3.6.1.4.1.9.9.221.1.1.1.1.18", "1.3.6.1.4.1.9.9.221.1.1.1.1.20"
	d := Definition{Source: "used_free_pct", Kind: "gauge", Scale: 1, OID: used, OID2: free, LabelMode: "index"}
	rows := Evaluate(d, Columns{used: {"1.1": n(300), "1.2": n(0)}, free: {"1.1": n(100), "1.2": n(0)}})
	if len(rows) != 1 || rows[0].Instance != "1.1" || rows[0].Value != 75 || rows[0].Label != "1.1" {
		t.Fatalf("rows %+v", rows) // 1.2 has used+free = 0: no value
	}
}

func TestScalarAndScale(t *testing.T) {
	d := Definition{Source: "scalar", Kind: "gauge", Scale: 0.1, OID: "1.3.6.1.4.1.1.2", LabelMode: "index"}
	every, cached := Needed(d)
	if !reflect.DeepEqual(every, []string{"1.3.6.1.4.1.1.2.0"}) || len(cached) != 0 {
		t.Fatalf("needed %v %v", every, cached)
	}
	rows := Evaluate(d, Columns{"1.3.6.1.4.1.1.2.0": {"": n(235)}})
	if len(rows) != 1 || rows[0].Instance != "" || rows[0].Value != 23.5 {
		t.Fatalf("rows %+v", rows)
	}
}

func TestNeeded(t *testing.T) {
	d := Definition{Source: "column", OID: sensorVal, PrecisionOID: sensorPrec, FilterOID: sensorType, LabelMode: "pointer",
		LabelPointerOID: cpuPhys, LabelTargetOID: entName}
	every, cached := Needed(d)
	if !reflect.DeepEqual(every, []string{sensorVal}) || !reflect.DeepEqual(cached, []string{sensorPrec, sensorType, cpuPhys, entName}) {
		t.Errorf("every %v cached %v", every, cached)
	}
}

func TestRates(t *testing.T) {
	t0 := time.Unix(1000, 0)
	rows := []Row{{Instance: "1", Value: 100}, {Instance: "2", Value: 4294967000}}
	out, prev := Rates(nil, rows, t0)
	if len(out) != 0 {
		t.Fatalf("first sight produced %+v", out)
	}
	out, prev = Rates(prev, []Row{{Instance: "1", Value: 700}, {Instance: "2", Value: 704}}, t0.Add(60*time.Second))
	if len(out) != 2 || out[0].Value != 10 || out[1].Value != 1000.0/60 {
		t.Fatalf("rates %+v", out) // row 2 wrapped at 2^32: (704 + 2^32 - 4294967000) / 60
	}
	out, _ = Rates(prev, []Row{{Instance: "1", Value: 5e12}}, t0.Add(120*time.Second))
	if len(out) != 1 {
		t.Fatalf("64-bit growth %+v", out)
	}
	big := map[string]Sample{"1": {Value: 5e12, At: t0}}
	if out, _ := Rates(big, []Row{{Instance: "1", Value: 10}}, t0.Add(60*time.Second)); len(out) != 0 {
		t.Errorf("reboot produced %+v", out)
	}
}
```

`backend/internal/custommetric/rules_test.go`:

```go
package custommetric

import (
	"testing"
	"time"
)

var r0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestNotOKStartsAndClears(t *testing.T) {
	r := Rule{Kind: "not_ok", Enabled: true}
	st, ch := EvalRule(r, "status", RuleState{}, []Row{{Instance: "2", Label: "Fan 2", State: "critical"}}, r0)
	if len(ch) != 1 || !ch[0].Started || ch[0].State != "critical" || ch[0].Label != "Fan 2" {
		t.Fatalf("start %+v", ch)
	}
	st, ch = EvalRule(r, "status", st, []Row{}, r0.Add(time.Minute)) // row missing: keep
	if len(ch) != 0 || st.Active["2"].IsZero() {
		t.Fatalf("missing row changed state %+v", ch)
	}
	_, ch = EvalRule(r, "status", st, []Row{{Instance: "2", State: "normal", OK: true}}, r0.Add(2*time.Minute))
	if len(ch) != 1 || ch[0].Started {
		t.Fatalf("clear %+v", ch)
	}
}

func TestAboveWithHold(t *testing.T) {
	r := Rule{Kind: "above", Value: 90, Hold: 10 * time.Minute, Enabled: true}
	st := RuleState{}
	var ch []Change
	for m := 0; m < 10; m++ {
		st, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 95}}, r0.Add(time.Duration(m)*time.Minute))
		if len(ch) != 0 {
			t.Fatalf("minute %d started early", m)
		}
	}
	st, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 95}}, r0.Add(10*time.Minute))
	if len(ch) != 1 || !ch[0].Started || ch[0].Value != 95 {
		t.Fatalf("start %+v", ch)
	}
	_, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 90}}, r0.Add(11*time.Minute)) // 90 is not above 90
	if len(ch) != 1 || ch[0].Started {
		t.Fatalf("clear %+v", ch)
	}
}

func TestBelowAndDipResets(t *testing.T) {
	r := Rule{Kind: "below", Value: 10, Hold: 5 * time.Minute, Enabled: true}
	st, _ := EvalRule(r, "gauge", RuleState{}, []Row{{Instance: "1", Value: 5}}, r0)
	st, _ = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 50}}, r0.Add(4*time.Minute))
	st, ch := EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 5}}, r0.Add(6*time.Minute))
	if len(ch) != 0 {
		t.Fatalf("dip did not reset %+v", ch)
	}
	if _, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 5}}, r0.Add(11*time.Minute)); len(ch) != 1 {
		t.Fatalf("no start after hold %+v", ch)
	}
}

func TestDisabledRuleNeverFires(t *testing.T) {
	_, ch := EvalRule(Rule{Kind: "not_ok"}, "status", RuleState{}, []Row{{Instance: "1", State: "critical"}}, r0)
	if len(ch) != 0 {
		t.Fatalf("disabled rule fired %+v", ch)
	}
}

func TestEvalRuleDoesNotMutatePrev(t *testing.T) {
	prev := RuleState{Active: map[string]time.Time{}, Since: map[string]time.Time{}}
	EvalRule(Rule{Kind: "not_ok", Enabled: true}, "status", prev, []Row{{Instance: "1", State: "x"}}, r0)
	if len(prev.Active) != 0 || len(prev.Since) != 0 {
		t.Error("prev mutated")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/custommetric/ 2>&1 | tail -2`
Expected: build failure (no non-test files).

- [ ] **Step 3: Implement** `backend/internal/custommetric/custommetric.go`:

```go
// Package custommetric turns walked SNMP columns into metric rows (filter,
// precision, scale, labels, used/free percentages, states, counter rates)
// and evaluates a metric's alert rule per row. It is pure: no I/O.
package custommetric

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Value struct {
	Num   float64
	NumOK bool
	Text  string
}

// text is how a value compares with a filter value or reads as a label.
func (v Value) text() string {
	if v.NumOK {
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	}
	return v.Text
}

type Columns map[string]map[string]Value

type Rule struct {
	Kind    string
	Value   float64
	Hold    time.Duration
	Enabled bool
}

type Definition struct {
	Name, Key, Source, Kind                             string
	Scale                                               float64
	OID, OID2, PrecisionOID, FilterOID                  string
	FilterValues                                        []string
	LabelMode, LabelOID, LabelPointerOID, LabelTargetOID string
	OKStates                                            []int64
	StateNames                                          map[int64]string
	Rule                                                Rule
}

type Row struct {
	Instance, Label string
	Value           float64
	State           string
	OK              bool
}

// Needed lists the OIDs to read: every run (values) and cached with the
// inventory (filter, labels, precision), each without duplicates.
func Needed(d Definition) (every, cached []string) {
	add := func(list *[]string, oid string) {
		if oid != "" && !slices.Contains(*list, oid) {
			*list = append(*list, oid)
		}
	}
	if d.Source == "scalar" {
		add(&every, d.OID+".0")
		return every, nil
	}
	add(&every, d.OID)
	if d.Source == "used_free_pct" {
		add(&every, d.OID2)
	}
	add(&cached, d.PrecisionOID)
	add(&cached, d.FilterOID)
	switch d.LabelMode {
	case "column", "same_index":
		add(&cached, d.LabelOID)
	case "pointer":
		add(&cached, d.LabelPointerOID)
		add(&cached, d.LabelTargetOID)
	}
	return every, cached
}

// Evaluate returns the metric's rows from the walked columns.
func Evaluate(d Definition, cols Columns) []Row {
	scale := d.Scale
	if scale == 0 {
		scale = 1
	}
	if d.Source == "scalar" {
		v, ok := cols[d.OID+".0"][""]
		if !ok || !v.NumOK {
			return nil
		}
		return []Row{finish(d, Row{Instance: "", Label: d.Name}, v.Num*scale)}
	}
	var rows []Row
	for idx, v := range cols[d.OID] {
		if d.FilterOID != "" {
			fv, ok := cols[d.FilterOID][idx]
			if !ok || !slices.Contains(d.FilterValues, fv.text()) {
				continue
			}
		}
		if !v.NumOK {
			continue
		}
		num := v.Num
		if d.Source == "used_free_pct" {
			free, ok := cols[d.OID2][idx]
			if !ok || !free.NumOK || num+free.Num == 0 {
				continue
			}
			num = num / (num + free.Num) * 100
		}
		if d.PrecisionOID != "" {
			if p, ok := cols[d.PrecisionOID][idx]; ok && p.NumOK {
				num /= math.Pow(10, p.Num)
			}
		}
		rows = append(rows, finish(d, Row{Instance: idx, Label: label(d, cols, idx)}, num*scale))
	}
	sort.Slice(rows, func(i, j int) bool { return lessIndex(rows[i].Instance, rows[j].Instance) })
	return rows
}

func finish(d Definition, r Row, num float64) Row {
	r.Value = num
	if d.Kind == "status" {
		code := int64(num)
		r.State = d.StateNames[code]
		if r.State == "" {
			r.State = strconv.FormatInt(code, 10)
		}
		r.OK = slices.Contains(d.OKStates, code)
	}
	return r
}

func label(d Definition, cols Columns, idx string) string {
	var l string
	switch d.LabelMode {
	case "column", "same_index":
		l = cols[d.LabelOID][idx].text()
	case "pointer":
		if p, ok := cols[d.LabelPointerOID][idx]; ok && p.NumOK && p.Num != 0 {
			l = cols[d.LabelTargetOID][p.text()].text()
		}
	default:
		l = idx
	}
	if l = strings.TrimSpace(l); l == "" {
		l = "Row " + idx
	}
	return l
}

// lessIndex orders row indexes by numeric arcs ("2" before "10", "1.2" before "1.10").
func lessIndex(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, ex := strconv.ParseUint(as[i], 10, 64)
		y, ey := strconv.ParseUint(bs[i], 10, 64)
		if ex != nil || ey != nil {
			if as[i] != bs[i] {
				return as[i] < bs[i]
			}
			continue
		}
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}

type Sample struct {
	Value float64
	At    time.Time
}

// Rates turns counter rows into per-second rates against the previous
// samples, returning the rate rows and the samples to keep for next time.
func Rates(prev map[string]Sample, rows []Row, at time.Time) ([]Row, map[string]Sample) {
	next := make(map[string]Sample, len(rows))
	var out []Row
	for _, r := range rows {
		next[r.Instance] = Sample{Value: r.Value, At: at}
		p, ok := prev[r.Instance]
		if !ok {
			continue
		}
		dt := at.Sub(p.At).Seconds()
		if dt <= 0 {
			continue
		}
		delta := r.Value - p.Value
		if delta < 0 {
			if p.Value >= 1<<32 {
				continue // a 64-bit counter went backwards: a reboot
			}
			delta += 1 << 32
		}
		r.Value = delta / dt
		out = append(out, r)
	}
	return out, next
}
```

`backend/internal/custommetric/rules.go`:

```go
package custommetric

import "time"

type RuleState struct {
	Active map[string]time.Time
	Since  map[string]time.Time
}

type Change struct {
	Instance, Label string
	Started         bool
	At              time.Time
	Value           float64
	State           string
}

// Violates reports whether a row breaks the rule.
func Violates(r Rule, kind string, row Row) bool {
	switch r.Kind {
	case "not_ok":
		return kind == "status" && !row.OK
	case "above":
		return row.Value > r.Value
	case "below":
		return row.Value < r.Value
	}
	return false
}

// EvalRule applies one poll's rows to the rule. A row absent this poll keeps
// its state. prev is not modified.
func EvalRule(r Rule, kind string, prev RuleState, rows []Row, now time.Time) (RuleState, []Change) {
	next := RuleState{Active: map[string]time.Time{}, Since: map[string]time.Time{}}
	for k, v := range prev.Active {
		next.Active[k] = v
	}
	for k, v := range prev.Since {
		next.Since[k] = v
	}
	if !r.Enabled || r.Kind == "" {
		return next, nil
	}
	var changes []Change
	for _, row := range rows {
		ch := Change{Instance: row.Instance, Label: row.Label, At: now, Value: row.Value, State: row.State}
		_, active := next.Active[row.Instance]
		if Violates(r, kind, row) {
			since, held := next.Since[row.Instance]
			if !held {
				since = now
				next.Since[row.Instance] = since
			}
			if !active && now.Sub(since) >= r.Hold {
				next.Active[row.Instance] = now
				ch.Started = true
				changes = append(changes, ch)
			}
			continue
		}
		delete(next.Since, row.Instance)
		if active {
			delete(next.Active, row.Instance)
			changes = append(changes, ch)
		}
	}
	return next, changes
}
```

- [ ] **Step 4: Run tests**

Run: `cd backend && gofmt -l internal/custommetric; go vet ./internal/custommetric/ && go test ./internal/custommetric/ -v 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/custommetric
git commit -m "feat(metrics): evaluate custom metric rows, counter rates and alert rules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Reading columns and the test-walk API

**Files:**
- Create: `backend/internal/services/metric_reader.go`, `backend/internal/api/testwalk_handler.go`
- Test: `backend/internal/services/metric_reader_test.go`, `backend/internal/api/testwalk_handler_test.go`

**Interfaces:**
- Consumes: `snmp.Client`, `snmp.Walk`/`snmp.GetEach`, Task 5 `custommetric.Value`, `custommetric.Columns`, Task 4 `MIBLibrary.Object`.
- Produces:
  - `func ValueOf(p snmp.PDU) custommetric.Value` — `int64`/`uint64` → Num; `[]byte` → Text (printable) or hex `aa:bb:..` when not printable; `string` (OID/IP) → Text
  - `func ReadColumns(ctx context.Context, c snmp.Client, t snmp.Target, oids []string) (custommetric.Columns, map[string]error)` — OIDs ending in `.0` are fetched together with `snmp.GetEach` and stored under index `""`; the rest are walked one by one (index = PDU OID minus `oid+"."`); per-OID errors returned (a walk error does not stop the others); empty result = an empty map for that OID
  - `type TestWalkResult struct { OID string; Columns []TestWalkColumn; Rows []TestWalkRow; Truncated bool }`, `TestWalkColumn{OID, Name string; Enum map[int64]string}`, `TestWalkRow{Index string; Values map[string]TestWalkCell}`, `TestWalkCell{Raw string; Meaning string}`
  - `func (l *MIBLibrary) TestWalk(ctx context.Context, c snmp.Client, t snmp.Target, oid string) (*TestWalkResult, error)` — table: walk the table OID, split each PDU suffix `1.<col>.<index>` into column and row; column/raw subtree: walk and use the suffix as index; scalar (object kind scalar, or a raw OID whose walk is empty): `GetEach(oid+".0")`; at most 500 rows (`Truncated`); names/enums from the library when known
  - Route: `POST /api/v1/devices/:id/test-walk` body `{"oid":"1.3.6..."}` — `loadDevice(c, devices, sites, services.SiteAccessEditable)`; 20 s context; OID must match `^\d+(\.\d+)+$` (400 otherwise); SNMP failures → 200 `{"ok":false,"error":"…"}` like Test connection; success → 200 `{"ok":true,"result":…}`

- [ ] **Step 1: Write the failing tests.** `metric_reader_test.go` — a fake `snmp.Client` (Walk returns PDUs for registered roots, Get answers scalars):

```go
package services

import (
	"context"
	"errors"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type walkFake struct {
	walks map[string][]snmp.PDU
	gets  map[string]any
	fail  map[string]bool
}

func (f *walkFake) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	out := make([]snmp.PDU, len(oids))
	for i, o := range oids {
		out[i] = snmp.PDU{OID: o, Value: f.gets[o]}
	}
	return out, nil
}
func (f *walkFake) Walk(_ context.Context, _ snmp.Target, root string) ([]snmp.PDU, error) {
	if f.fail[root] {
		return nil, errors.New("request timeout")
	}
	return f.walks[root], nil
}

func TestReadColumns(t *testing.T) {
	cpu := "1.3.6.1.4.1.9.9.109.1.1.1.1.8"
	f := &walkFake{
		walks: map[string][]snmp.PDU{cpu: {{OID: cpu + ".1", Value: uint64(12)}, {OID: cpu + ".2", Value: uint64(7)}}},
		gets:  map[string]any{"1.3.6.1.2.1.1.5.0": []byte("core")},
		fail:  map[string]bool{"1.3.6.1.4.1.9.9.13.1.4.1.3": true},
	}
	cols, errs := ReadColumns(context.Background(), f, snmp.Target{}, []string{cpu, "1.3.6.1.2.1.1.5.0", "1.3.6.1.4.1.9.9.13.1.4.1.3", "1.3.6.1.4.1.9.9.48.1.1.1.5"})
	if cols[cpu]["1"].Num != 12 || cols[cpu]["2"].Num != 7 || cols["1.3.6.1.2.1.1.5.0"][""].Text != "core" {
		t.Errorf("cols %+v", cols)
	}
	if errs["1.3.6.1.4.1.9.9.13.1.4.1.3"] == nil || len(errs) != 1 {
		t.Errorf("errs %v", errs)
	}
	if v, ok := cols["1.3.6.1.4.1.9.9.48.1.1.1.5"]; !ok || len(v) != 0 {
		t.Errorf("unsupported table should be empty, got %v", v)
	}
}

func TestValueOf(t *testing.T) {
	if v := ValueOf(snmp.PDU{Value: int64(-5)}); !v.NumOK || v.Num != -5 {
		t.Errorf("int %+v", v)
	}
	if v := ValueOf(snmp.PDU{Value: []byte{0x00, 0x1a, 0xff}}); v.Text != "00:1a:ff" {
		t.Errorf("binary %+v", v)
	}
	if v := ValueOf(snmp.PDU{Value: nil}); v.NumOK || v.Text != "" {
		t.Errorf("nil %+v", v)
	}
}
```

Add a DB test `TestDBMIBTestWalkTable` in `mib_browse_db_test.go`: sync built-ins, then `TestWalk` with a fake client whose walk of `1.3.6.1.2.1.47.1.1.1` (entPhysicalTable) returns `{.1.2.1001 "desc"}`, `{.1.7.1001 "Switch 1"}`, `{.1.5.1001 int64(3)}` (OID prefix `1.3.6.1.2.1.47.1.1.1`); expect one row `1001` with cells keyed by column OID, the `entPhysicalName` column named, and the class cell's Meaning `"chassis"` (ENTITY-MIB's `PhysicalClass` enum 3 — confirm the name in the built-in object's enum and use it).

`testwalk_handler_test.go`: readonly caller → 403; no access → 404; bad OID → 400; editor → 200 and the fake called with the OID.

- [ ] **Step 2: Run to verify they fail** — `go vet` reports undefined `ReadColumns`, `ValueOf`, `TestWalk`.

- [ ] **Step 3: Implement** `metric_reader.go`:

```go
package services

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/Stevy2191/Sentinel/backend/internal/custommetric"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// ValueOf converts one SNMP value for metric evaluation.
func ValueOf(p snmp.PDU) custommetric.Value {
	switch v := p.Value.(type) {
	case int64:
		return custommetric.Value{Num: float64(v), NumOK: true}
	case uint64:
		return custommetric.Value{Num: float64(v), NumOK: true}
	case string:
		return custommetric.Value{Text: v}
	case []byte:
		for _, r := range string(v) {
			if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
				parts := make([]string, len(v))
				for i, b := range v {
					parts[i] = fmt.Sprintf("%02x", b)
				}
				return custommetric.Value{Text: strings.Join(parts, ":")}
			}
		}
		return custommetric.Value{Text: snmp.Clean(string(v))}
	}
	return custommetric.Value{}
}

// ReadColumns reads scalars (OIDs ending ".0", one GET) and walks every other
// OID, returning what answered and each OID's error.
func ReadColumns(ctx context.Context, c snmp.Client, t snmp.Target, oids []string) (custommetric.Columns, map[string]error) {
	cols := custommetric.Columns{}
	errs := map[string]error{}
	var scalars []string
	for _, o := range oids {
		if strings.HasSuffix(o, ".0") {
			scalars = append(scalars, o)
			continue
		}
		pdus, err := c.Walk(ctx, t, o)
		if err != nil {
			errs[o] = err
			continue
		}
		col := map[string]custommetric.Value{}
		for _, p := range pdus {
			if idx, ok := strings.CutPrefix(p.OID, o+"."); ok {
				col[idx] = ValueOf(p)
			}
		}
		cols[o] = col
	}
	if len(scalars) > 0 {
		pdus, err := snmp.GetEach(ctx, c, t, scalars)
		if err != nil {
			for _, o := range scalars {
				errs[o] = err
			}
		}
		for _, p := range pdus {
			if v := ValueOf(p); v.NumOK || v.Text != "" {
				cols[p.OID] = map[string]custommetric.Value{"": v}
			}
		}
	}
	return cols, errs
}
```

(Confirm `snmp.Clean` exists — it does in `parse.go`; PDU OIDs have no leading dot.)

Add `TestWalk` to `mib_browse.go` per the Interfaces block (look the OID up with `Object`; `kind == "table"` → walk the table and split `1.<col>.<index>`; `row` → walk the row; `column` or unknown with a non-empty walk → single column named from the object if known; else `GetEach(oid+".0")`). Cells: `Raw` = `ValueOf(p).Num` formatted or `Text`; `Meaning` from the column's `Enum` when numeric. Stop adding rows at 500 and set `Truncated`.

`testwalk_handler.go`:

```go
var oidPattern = regexp.MustCompile(`^\d+(\.\d+)+$`)

// testWalker is MIBLibrary's TestWalk with the device's target resolved.
type testWalker interface {
	TestWalkDevice(ctx context.Context, d *services.DeviceView, oid string) (*services.TestWalkResult, error)
}

func RegisterTestWalkRoutes(rg *gin.RouterGroup, devices deviceStore, sites siteAccessChecker, walker testWalker) {
	rg.POST("/devices/:id/test-walk", func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		var req struct {
			OID string `json:"oid"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || !oidPattern.MatchString(strings.Trim(req.OID, ".")) {
			respondError(c, http.StatusBadRequest, "give a numeric OID such as 1.3.6.1.2.1.1")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		res, err := walker.TestWalkDevice(ctx, d, strings.Trim(req.OID, "."))
		if err != nil {
			respondSuccess(c, http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"ok": true, "result": res})
	})
}
```

`TestWalkDevice` lives on a small `services.DeviceWalker` (create in `metric_reader.go`): `NewDeviceWalker(lib *MIBLibrary, prober *Prober, creds *SNMPCredentialService, client snmp.Client)`; it builds the target the way `Prober.IdentifyWith` does (decrypt the device's credential, resolve and netguard-check the host, `TargetFor`-equivalent with the device's port, timeout and retries) and calls `lib.TestWalk`. Read `prober.go` and reuse its resolve/blocked fields rather than duplicating them (add a method `Prober.TargetFor(ctx, d models.Device) (snmp.Target, error)` there and use it). Register in `main.go`.

- [ ] **Step 4: Run tests**

Run: `cd backend && gofmt -l internal cmd; go vet ./... && go test ./internal/services/ -run 'ReadColumns|ValueOf' -v 2>&1 | grep -E "^(--- |FAIL)" && go test ./internal/api/ -run TestWalk -v 2>&1 | grep -E "^(--- |FAIL)" && ./scripts/test-db.sh -run DBMIBTestWalk -v 2>&1 | grep -E "^(--- |FAIL)"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/metric_reader.go backend/internal/services/metric_reader_test.go backend/internal/services/mib_browse.go backend/internal/services/mib_browse_db_test.go backend/internal/services/prober.go backend/internal/api/testwalk_handler.go backend/internal/api/testwalk_handler_test.go backend/cmd/sentinel/main.go
git commit -m "feat(mib): test-walk a device from the MIB browser

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 7: `ProfileService` — profiles, metrics, matching, overrides, the Cisco starter profile

**Files:**
- Create: `backend/internal/services/profile_service.go`, `backend/internal/services/profile_starter.go`, `backend/internal/api/profile_handler.go`
- Modify: `backend/internal/services/metrics_catalog.go` (custom key registry), `backend/cmd/sentinel/main.go`
- Test: `backend/internal/services/profile_service_test.go` (pure), `backend/internal/services/profile_service_db_test.go`, `backend/internal/api/profile_handler_test.go`

**Interfaces:**
- Consumes: Task 2 models; Task 5 `custommetric.Definition`, `custommetric.Rule`.
- Produces:
  - metrics catalogue: `func SetCustomMetricKeys(keys []string)`, `func BuiltinMetric(key string) bool`; `KnownMetric` = built-in OR registered custom key (RWMutex-guarded)
  - `func MatchesPrefix(objectID string, prefixes []string) bool` — whole-arc prefix (`1.3.6.1.4.1.9.1` matches `…9.1.2066`, not `…9.12`); leading dots ignored
  - `func ValidateMetric(m models.ProfileMetric) error` — key rule, not `BuiltinMetric`; OIDs numeric; `column`/`used_free_pct` need OID (and OID2); label modes need their OIDs; `rule_kind` above/below need `rule_value`; `not_ok` only with kind `status`; `status` needs at least one OK state; scale ≠ 0
  - `func ToDefinition(m models.ProfileMetric) custommetric.Definition`
  - `type ProfileInput struct { Name, Description string; MatchPrefixes []string; PollIntervalMinutes int }`
  - `type ProfileView struct { models.MetricProfile; Metrics int; Devices int }`, `type ProfileDetail struct { models.MetricProfile; Metrics []models.ProfileMetric }`
  - `type ProfileWithMetrics struct { Profile models.MetricProfile; Metrics []models.ProfileMetric }`
  - `type DeviceProfileView struct { Profile models.MetricProfile; Applies bool; Matched bool; Mode string; LastRun *models.DeviceProfileRun }` (Mode `auto|attach|detach`)
  - `func NewProfileService(db *gorm.DB) *ProfileService`
  - methods: `Load(ctx) error` (registers custom keys), `SeedStarter(ctx) error` (creates the starter profile once, by name, never overwriting), `List`, `Get(id)`, `Create(in)`, `Update(id, in) (before, after)`, `Delete(id) (*models.MetricProfile, error)`, `Copy(id) (*ProfileDetail, error)`, `CreateMetric(profileID, m)`, `UpdateMetric(id, m) (before, after)` (key cannot change: `ErrMetricKeyImmutable`), `DeleteMetric(id) (*models.ProfileMetric, error)`, `MetricDataDevices(key) (int, error)`, `ProfilesForDevice(ctx, d models.Device) ([]ProfileWithMetrics, error)`, `DeviceProfiles(ctx, d models.Device) ([]DeviceProfileView, error)`, `SetDeviceProfile(ctx, deviceID, profileID uuid.UUID, mode string) error`, `SaveRun(ctx, deviceID, profileID uuid.UUID, at time.Time, ok bool, errText string) error`
  - errors: `ErrProfileNotFound`, `ErrMetricNotFound`, `ErrProfileBuiltin` (delete), `ErrProfileNameTaken`, `ErrMetricKeyTaken`, `ErrMetricKeyImmutable`
  - Deleting a metric (or a profile's metrics) queues its series for the nightly cleanup: `INSERT INTO metrics.deleted_series (series_id) SELECT id FROM metrics.series WHERE metric = ? ON CONFLICT DO NOTHING; DELETE FROM metrics.series WHERE metric = ?` in the same transaction, then `Load`.
  - Routes: admin — `GET/POST /network/profiles`, `GET/PUT/DELETE /network/profiles/:id`, `POST /network/profiles/:id/copy`, `POST /network/profiles/:id/metrics`, `PUT/DELETE /network/metrics/:id`, `GET /network/metrics/:id/data-devices`; site access — `GET /devices/:id/profiles` (readonly), `PUT /devices/:id/profiles/:profileId` body `{"mode":"auto|attach|detach"}` (editable). Audit every admin write (`metric_profile_*`, `custom_metric_*`) with before/after.

- [ ] **Step 1: Write the failing tests.** `profile_service_test.go` (pure):

```go
package services

import (
	"strings"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestMatchesPrefix(t *testing.T) {
	p := []string{"1.3.6.1.4.1.9.1"}
	for oid, want := range map[string]bool{
		"1.3.6.1.4.1.9.1.2066": true, ".1.3.6.1.4.1.9.1.1208": true, "1.3.6.1.4.1.9.1": true,
		"1.3.6.1.4.1.9.12.3": false, "1.3.6.1.4.1.850.1.1.1": false, "": false,
	} {
		if MatchesPrefix(oid, p) != want {
			t.Errorf("%q: want %v", oid, want)
		}
	}
}

func goodMetric() models.ProfileMetric {
	return models.ProfileMetric{Name: "Fan", Key: "acme_fan_state", Source: "column", Kind: "status", Scale: 1,
		OID: "1.3.6.1.4.1.9.9.13.1.4.1.3", LabelMode: "column", LabelOID: "1.3.6.1.4.1.9.9.13.1.4.1.2",
		OKStates: models.Int64Array{1}, RuleKind: "not_ok", RuleEnabled: true}
}

func TestValidateMetric(t *testing.T) {
	if err := ValidateMetric(goodMetric()); err != nil {
		t.Fatalf("good metric: %v", err)
	}
	cases := map[string]func(m *models.ProfileMetric){
		"reserved prefix":    func(m *models.ProfileMetric) { m.Key = "ups_fan" },
		"built-in key":       func(m *models.ProfileMetric) { m.Key = "if_in_bps" },
		"bad key":            func(m *models.ProfileMetric) { m.Key = "Fan State" },
		"bad oid":            func(m *models.ProfileMetric) { m.OID = "iso.3.6" },
		"label oid missing":  func(m *models.ProfileMetric) { m.LabelOID = "" },
		"not_ok on gauge":    func(m *models.ProfileMetric) { m.Kind = "gauge" },
		"status without ok":  func(m *models.ProfileMetric) { m.OKStates = nil },
		"above without value": func(m *models.ProfileMetric) { m.Kind = "gauge"; m.RuleKind = "above"; m.RuleValue = nil },
		"used_free without oid2": func(m *models.ProfileMetric) { m.Kind = "gauge"; m.RuleKind = ""; m.Source = "used_free_pct" },
		"zero scale":         func(m *models.ProfileMetric) { m.Scale = 0 },
	}
	for name, mutate := range cases {
		m := goodMetric()
		mutate(&m)
		if err := ValidateMetric(m); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// Review focus 5: copies stay unique and within 63 characters.
func TestCopyKey(t *testing.T) {
	taken := map[string]bool{"cisco_cpu_5min": true, "cisco_cpu_5min_copy": true}
	isTaken := func(k string) bool { return taken[k] }
	if k := copyKey("cisco_cpu_5min", isTaken); k != "cisco_cpu_5min_copy2" {
		t.Errorf("second copy %q", k)
	}
	long := "a" + strings.Repeat("b", 62)
	k := copyKey(long, isTaken)
	if len(k) > 63 || !strings.HasSuffix(k, "_copy") || k == long {
		t.Errorf("long copy %q (%d)", k, len(k))
	}
}

func TestStarterProfileIsValid(t *testing.T) {
	p, metrics := starterProfile()
	if p.Name != "Cisco switch health" || !p.Builtin || len(p.MatchPrefixes) != 1 || p.MatchPrefixes[0] != "1.3.6.1.4.1.9.1" {
		t.Fatalf("profile %+v", p)
	}
	if len(metrics) != 9 {
		t.Fatalf("%d metrics, want 9", len(metrics))
	}
	for _, m := range metrics {
		if err := ValidateMetric(m); err != nil {
			t.Errorf("%s: %v", m.Key, err)
		}
	}
}
```

`profile_service_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBProfilesForDevice(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	testdb.Must(t, svc.SeedStarter(ctx))
	testdb.Must(t, svc.SeedStarter(ctx)) // once only
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET sys_object_id = '1.3.6.1.4.1.9.1.1208' WHERE id = ?`, s.DeviceID)
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)

	got, err := svc.ProfilesForDevice(ctx, d)
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Profile.Name != "Cisco switch health" || len(got[0].Metrics) != 9 {
		t.Fatalf("profiles %+v", got)
	}
	testdb.Must(t, svc.SetDeviceProfile(ctx, d.ID, got[0].Profile.ID, "detach"))
	if got, _ := svc.ProfilesForDevice(ctx, d); len(got) != 0 {
		t.Fatalf("detached profile still applies")
	}
	views, err := svc.DeviceProfiles(ctx, d)
	testdb.Must(t, err)
	if len(views) != 1 || views[0].Applies || !views[0].Matched || views[0].Mode != "detach" {
		t.Errorf("views %+v", views)
	}
	testdb.Must(t, svc.SetDeviceProfile(ctx, d.ID, got0(t, svc, ctx).ID, "auto"))
	if got, _ := svc.ProfilesForDevice(ctx, d); len(got) != 1 {
		t.Errorf("auto did not restore the match")
	}
	testdb.Must(t, svc.SaveRun(ctx, d.ID, got0(t, svc, ctx).ID, time.Now().UTC(), false, "timeout"))
	views, _ = svc.DeviceProfiles(ctx, d)
	if views[0].LastRun == nil || views[0].LastRun.OK || views[0].LastRun.Error != "timeout" {
		t.Errorf("last run %+v", views[0].LastRun)
	}
}

func got0(t *testing.T, svc *ProfileService, ctx context.Context) models.MetricProfile {
	t.Helper()
	list, err := svc.List(ctx)
	testdb.Must(t, err)
	return list[0].MetricProfile
}

func TestDBProfileCopyDeleteAndRegistry(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	testdb.Must(t, svc.SeedStarter(ctx))
	testdb.Must(t, svc.Load(ctx))
	if !KnownMetric("cisco_cpu_5min") {
		t.Fatal("starter key not registered")
	}
	starter := got0(t, svc, ctx)
	if _, err := svc.Delete(ctx, starter.ID); !errors.Is(err, ErrProfileBuiltin) {
		t.Errorf("deleting built-in: %v", err)
	}
	cp, err := svc.Copy(ctx, starter.ID)
	testdb.Must(t, err)
	if cp.Builtin || cp.Name != "Cisco switch health (copy)" || len(cp.Metrics) != 9 || cp.Metrics[0].Key == "cisco_cpu_5min" {
		t.Fatalf("copy %+v", cp.MetricProfile)
	}
	if _, err := svc.Copy(ctx, starter.ID); err != nil { // second copy: unique name and keys
		t.Fatalf("second copy: %v", err)
	}
	// Deleting a metric queues its history.
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	m := NewMetricsStore(db)
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: cp.Metrics[0].Key, Instance: "1", Value: 5}}))
	if n, _ := svc.MetricDataDevices(ctx, cp.Metrics[0].Key); n != 1 {
		t.Errorf("data devices %d", n)
	}
	_, err = svc.DeleteMetric(ctx, cp.Metrics[0].ID)
	testdb.Must(t, err)
	var queued int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.deleted_series`).Scan(&queued).Error)
	if queued != 1 || KnownMetric(cp.Metrics[0].Key) {
		t.Errorf("queued %d, still known %v", queued, KnownMetric(cp.Metrics[0].Key))
	}
	if _, err := svc.UpdateMetric(ctx, cp.Metrics[1].ID, withKey(cp.Metrics[1], "renamed_key")); !errors.Is(err, ErrMetricKeyImmutable) {
		t.Errorf("key change: %v", err)
	}
	_ = uuid.Nil
}

func withKey(m models.ProfileMetric, k string) models.ProfileMetric { m.Key = k; return m }
```

(`UpdateMetric` returns `(before, after *models.ProfileMetric, err error)`; adjust the call to `_, _, err :=` accordingly.)

Also add to `TestDBMIBDeleteRefusals` (Task 3 file): create a profile with one metric whose `oid` is `1.3.6.1.4.1.99999.1` (acmeTemp), then deleting ACME-MIB returns `*MIBInUseError` with `Metrics == ["<that key>"]`.

`profile_handler_test.go`: non-admin `GET /api/v1/network/profiles` → 403; admin `POST /api/v1/network/profiles/:id/metrics` with an invalid metric → 400 with the validation message; `PUT /api/v1/devices/:id/profiles/:pid` readonly → 403, editable → 200.

- [ ] **Step 2: Run to verify they fail** — `go vet ./internal/services/ ./internal/api/` reports the undefined names.

- [ ] **Step 3: Implement the registry** in `metrics_catalog.go`:

```go
var (
	customMu      sync.RWMutex
	customMetrics = map[string]bool{}
)

// SetCustomMetricKeys replaces the registered custom metric keys
// (ProfileService.Load calls it whenever metrics change).
func SetCustomMetricKeys(keys []string) {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	customMu.Lock()
	customMetrics = m
	customMu.Unlock()
}

// BuiltinMetric reports a key Sentinel itself writes.
func BuiltinMetric(key string) bool { return knownMetrics[key] }

// KnownMetric reports a built-in or registered custom key.
func KnownMetric(key string) bool {
	if knownMetrics[key] {
		return true
	}
	customMu.RLock()
	defer customMu.RUnlock()
	return customMetrics[key]
}
```

(Replace the existing one-line `KnownMetric`.)

- [ ] **Step 4: The starter profile** `profile_starter.go`:

```go
package services

import "github.com/Stevy2191/Sentinel/backend/internal/models"

// Cisco OIDs (from the public Cisco MIBs, verified with gosmi while
// planning): CISCO-PROCESS-MIB, CISCO-ENHANCED-MEMPOOL-MIB,
// CISCO-MEMORY-POOL-MIB, CISCO-ENVMON-MIB, CISCO-ENTITY-SENSOR-MIB,
// CISCO-ENTITY-FRU-CONTROL-MIB; entPhysicalName from ENTITY-MIB.
const (
	oidEntPhysicalName      = "1.3.6.1.2.1.47.1.1.1.1.7"
	oidCpmCPUTotalPhysIndex = "1.3.6.1.4.1.9.9.109.1.1.1.1.2"
	oidCpmCPUTotal5minRev   = "1.3.6.1.4.1.9.9.109.1.1.1.1.8"
	oidCempMemPoolName      = "1.3.6.1.4.1.9.9.221.1.1.1.1.3"
	oidCempMemPoolHCUsed    = "1.3.6.1.4.1.9.9.221.1.1.1.1.18"
	oidCempMemPoolHCFree    = "1.3.6.1.4.1.9.9.221.1.1.1.1.20"
	oidCiscoMemPoolName     = "1.3.6.1.4.1.9.9.48.1.1.1.2"
	oidCiscoMemPoolUsed     = "1.3.6.1.4.1.9.9.48.1.1.1.5"
	oidCiscoMemPoolFree     = "1.3.6.1.4.1.9.9.48.1.1.1.6"
	oidEnvTempDescr         = "1.3.6.1.4.1.9.9.13.1.3.1.2"
	oidEnvTempValue         = "1.3.6.1.4.1.9.9.13.1.3.1.3"
	oidEnvFanDescr          = "1.3.6.1.4.1.9.9.13.1.4.1.2"
	oidEnvFanState          = "1.3.6.1.4.1.9.9.13.1.4.1.3"
	oidEnvSupplyDescr       = "1.3.6.1.4.1.9.9.13.1.5.1.2"
	oidEnvSupplyState       = "1.3.6.1.4.1.9.9.13.1.5.1.3"
	oidEntSensorType        = "1.3.6.1.4.1.9.9.91.1.1.1.1.1"
	oidEntSensorPrecision   = "1.3.6.1.4.1.9.9.91.1.1.1.1.3"
	oidEntSensorValue       = "1.3.6.1.4.1.9.9.91.1.1.1.1.4"
	oidCefcFanTrayOper      = "1.3.6.1.4.1.9.9.117.1.4.1.1.1"
	oidCefcFRUPowerOper     = "1.3.6.1.4.1.9.9.117.1.1.2.1.2"
)

var envMonStates = models.EnumMap{1: "normal", 2: "warning", 3: "critical", 4: "shutdown", 5: "notPresent", 6: "notFunctioning"}

func f64(v float64) *float64 { return &v }

// starterProfile is the built-in "Cisco switch health" profile. ENVMON's
// notPresent counts as OK: an empty redundant power-supply or fan slot
// reports it, and alerting on it would page for nothing.
func starterProfile() (models.MetricProfile, []models.ProfileMetric) {
	p := models.MetricProfile{Name: "Cisco switch health", Builtin: true, PollIntervalMinutes: 1,
		Description:   "CPU, memory, temperature, fans and power supplies on Cisco switches. Tables a model does not have are skipped.",
		MatchPrefixes: models.StringArray{"1.3.6.1.4.1.9.1"}}
	m := []models.ProfileMetric{
		{Name: "CPU busy (5 min)", Key: "cisco_cpu_5min", Source: "column", Kind: "gauge", Units: "%", Scale: 1,
			OID: oidCpmCPUTotal5minRev, LabelMode: "pointer", LabelPointerOID: oidCpmCPUTotalPhysIndex, LabelTargetOID: oidEntPhysicalName,
			RuleKind: "above", RuleValue: f64(90), RuleHoldMinutes: 10, RuleEnabled: true},
		{Name: "Memory used", Key: "cisco_mem_used_pct", Source: "used_free_pct", Kind: "gauge", Units: "%", Scale: 1,
			OID: oidCempMemPoolHCUsed, OID2: oidCempMemPoolHCFree, LabelMode: "column", LabelOID: oidCempMemPoolName,
			RuleKind: "above", RuleValue: f64(90), RuleHoldMinutes: 15, RuleEnabled: true},
		{Name: "Memory used (classic)", Key: "cisco_mem_pool_used_pct", Source: "used_free_pct", Kind: "gauge", Units: "%", Scale: 1,
			OID: oidCiscoMemPoolUsed, OID2: oidCiscoMemPoolFree, LabelMode: "column", LabelOID: oidCiscoMemPoolName,
			RuleKind: "above", RuleValue: f64(90), RuleHoldMinutes: 15, RuleEnabled: true},
		{Name: "Temperature", Key: "cisco_temp_envmon", Source: "column", Kind: "gauge", Units: "°C", Scale: 1,
			OID: oidEnvTempValue, LabelMode: "column", LabelOID: oidEnvTempDescr,
			RuleKind: "above", RuleValue: f64(70), RuleHoldMinutes: 5, RuleEnabled: true},
		{Name: "Temperature (sensors)", Key: "cisco_temp_sensor", Source: "column", Kind: "gauge", Units: "°C", Scale: 1,
			OID: oidEntSensorValue, PrecisionOID: oidEntSensorPrecision, FilterOID: oidEntSensorType, FilterValues: models.StringArray{"8"},
			LabelMode: "same_index", LabelOID: oidEntPhysicalName,
			RuleKind: "above", RuleValue: f64(70), RuleHoldMinutes: 5, RuleEnabled: true},
		{Name: "Fan", Key: "cisco_fan_envmon", Source: "column", Kind: "status", Scale: 1,
			OID: oidEnvFanState, LabelMode: "column", LabelOID: oidEnvFanDescr,
			OKStates: models.Int64Array{1, 5}, StateNames: envMonStates, RuleKind: "not_ok", RuleEnabled: true},
		{Name: "Fan tray", Key: "cisco_fan_fru", Source: "column", Kind: "status", Scale: 1,
			OID: oidCefcFanTrayOper, LabelMode: "same_index", LabelOID: oidEntPhysicalName,
			OKStates: models.Int64Array{2}, StateNames: models.EnumMap{1: "unknown", 2: "up", 3: "down", 4: "warning"},
			RuleKind: "not_ok", RuleEnabled: true},
		{Name: "Power supply", Key: "cisco_psu_envmon", Source: "column", Kind: "status", Scale: 1,
			OID: oidEnvSupplyState, LabelMode: "column", LabelOID: oidEnvSupplyDescr,
			OKStates: models.Int64Array{1, 5}, StateNames: envMonStates, RuleKind: "not_ok", RuleEnabled: true},
		{Name: "Power supply (FRU)", Key: "cisco_psu_fru", Source: "column", Kind: "status", Scale: 1,
			OID: oidCefcFRUPowerOper, LabelMode: "same_index", LabelOID: oidEntPhysicalName,
			OKStates: models.Int64Array{2}, StateNames: models.EnumMap{1: "offEnvOther", 2: "on", 3: "offAdmin", 4: "offDenied",
				5: "offEnvPower", 6: "offEnvTemp", 7: "offEnvFan", 8: "failed", 9: "onButFanFail", 10: "offCooling",
				11: "offConnectorRating", 12: "onButInlinePowerFail"},
			RuleKind: "not_ok", RuleEnabled: true},
	}
	for i := range m {
		m[i].Position = i
	}
	return p, m
}
```

- [ ] **Step 5: Implement `profile_service.go`** — the methods in the Interfaces block. The pieces with real logic:

```go
// MatchesPrefix reports whether objectID starts with any prefix on whole arcs.
func MatchesPrefix(objectID string, prefixes []string) bool {
	oid := strings.Trim(objectID, ".")
	if oid == "" {
		return false
	}
	for _, p := range prefixes {
		p = strings.Trim(p, ".")
		if p != "" && (oid == p || strings.HasPrefix(oid, p+".")) {
			return true
		}
	}
	return false
}

var (
	metricKey  = regexp.MustCompile(`^[a-z][a-z0-9_]{2,62}$`)
	numericOID = regexp.MustCompile(`^\d+(\.\d+)+$`)
)

func ValidateMetric(m models.ProfileMetric) error {
	switch {
	case strings.TrimSpace(m.Name) == "":
		return errors.New("give the metric a name")
	case !metricKey.MatchString(m.Key):
		return errors.New("the key must be 3–63 lowercase letters, digits or underscores, starting with a letter")
	case strings.HasPrefix(m.Key, "if_") || strings.HasPrefix(m.Key, "ups_") || BuiltinMetric(m.Key):
		return errors.New("that key is reserved for Sentinel's own metrics")
	case m.Scale == 0:
		return errors.New("the scale cannot be 0")
	}
	oids := map[string]string{"OID": m.OID, "second OID": m.OID2, "precision OID": m.PrecisionOID, "filter OID": m.FilterOID,
		"label OID": m.LabelOID, "pointer OID": m.LabelPointerOID, "target OID": m.LabelTargetOID}
	for what, o := range oids {
		if o != "" && !numericOID.MatchString(o) {
			return fmt.Errorf("the %s must be numeric, like 1.3.6.1.2.1.1.3", what)
		}
	}
	switch m.Source {
	case "scalar", "column":
		if m.OID == "" {
			return errors.New("choose the OID to read")
		}
	case "used_free_pct":
		if m.OID == "" || m.OID2 == "" {
			return errors.New("used/free needs both the used and the free column")
		}
	default:
		return errors.New("source must be scalar, column or used_free_pct")
	}
	switch m.Kind {
	case "gauge", "counter":
	case "status":
		if len(m.OKStates) == 0 {
			return errors.New("a status metric needs at least one OK state")
		}
	default:
		return errors.New("kind must be gauge, counter or status")
	}
	switch m.LabelMode {
	case "index":
	case "column", "same_index":
		if m.LabelOID == "" {
			return errors.New("choose the column the label comes from")
		}
	case "pointer":
		if m.LabelPointerOID == "" || m.LabelTargetOID == "" {
			return errors.New("a pointer label needs the pointer column and the target column")
		}
	default:
		return errors.New("label must be index, column, same_index or pointer")
	}
	switch m.RuleKind {
	case "":
	case "above", "below":
		if m.RuleValue == nil {
			return errors.New("the rule needs a value")
		}
	case "not_ok":
		if m.Kind != "status" {
			return errors.New(`"not OK" rules are for status metrics`)
		}
	default:
		return errors.New("rule must be above, below or not_ok")
	}
	if m.RuleHoldMinutes < 0 || m.RuleHoldMinutes > 1440 {
		return errors.New("the hold must be 0–1440 minutes")
	}
	return nil
}

func ToDefinition(m models.ProfileMetric) custommetric.Definition {
	d := custommetric.Definition{Name: m.Name, Key: m.Key, Source: m.Source, Kind: m.Kind, Scale: m.Scale,
		OID: m.OID, OID2: m.OID2, PrecisionOID: m.PrecisionOID, FilterOID: m.FilterOID, FilterValues: m.FilterValues,
		LabelMode: m.LabelMode, LabelOID: m.LabelOID, LabelPointerOID: m.LabelPointerOID, LabelTargetOID: m.LabelTargetOID,
		OKStates: m.OKStates, StateNames: map[int64]string(m.StateNames),
		Rule: custommetric.Rule{Kind: m.RuleKind, Hold: time.Duration(m.RuleHoldMinutes) * time.Minute, Enabled: m.RuleEnabled}}
	if m.RuleValue != nil {
		d.Rule.Value = *m.RuleValue
	}
	return d
}

// copyKey suffixes key with _copy (then _copy2, _copy3 …), trimming the base
// so the result fits in 63 characters.
func copyKey(key string, taken func(string) bool) string {
	for i := 1; ; i++ {
		suffix := "_copy"
		if i > 1 {
			suffix += strconv.Itoa(i)
		}
		base := key
		if len(base)+len(suffix) > 63 {
			base = base[:63-len(suffix)]
		}
		if k := base + suffix; !taken(k) {
			return k
		}
	}
}
```

`ProfilesForDevice`: load all profiles with their metrics (ordered by profile name, metric position) and the device's overrides; a profile applies when override `attach`, or (no `detach` override and `MatchesPrefix(d.SysObjectID, p.MatchPrefixes)`). `DeviceProfiles` returns every profile with `Matched`, `Mode` (`auto` when no override), `Applies` and its `device_profile_runs` row. `SetDeviceProfile` with `auto` deletes the override; `attach`/`detach` upserts it. `SaveRun` upserts `device_profile_runs`. `Copy` names `"<name> (copy)"`, then `"<name> (copy 2)"` … while taken, copies metrics with `copyKey`, `Builtin=false`, in one transaction, then `Load`. `Create`/`Update` validate: name non-empty and unique (`ErrProfileNameTaken`), prefixes numeric OIDs, interval in {1,5,15}. `CreateMetric`/`UpdateMetric` call `ValidateMetric`, map a unique-key violation to `ErrMetricKeyTaken`, and call `Load` after commit. `SeedStarter` inserts the profile and metrics only when no profile named "Cisco switch health" exists.

- [ ] **Step 6: Handlers** `profile_handler.go` — the routes in the Interfaces block, following `snmp_credential_handler.go`'s shape (read it): bind JSON into `services.ProfileInput` or `models.ProfileMetric`; map `ErrProfileNotFound`/`ErrMetricNotFound` → 404, `ErrProfileBuiltin` → 400 `"the built-in profile cannot be deleted; copy it instead"`, `ErrProfileNameTaken`/`ErrMetricKeyTaken` → 409, `ErrMetricKeyImmutable` → 400, validation errors → 400 with the message; audit each write. Device routes use `loadDevice(c, devices, sites, services.SiteAccessReadonly | SiteAccessEditable)`.

Wire in `main.go`: `profileService := services.NewProfileService(db)`; at startup `SeedStarter` then `Load` (log errors); `api.RegisterProfileRoutes(v1, profileService, deviceService, siteService, auditService, authService)`.

- [ ] **Step 7: Run tests**

Run: `cd backend && gofmt -l internal cmd; go vet ./... && go test ./internal/services/ -run 'MatchesPrefix|ValidateMetric|CopyKey|StarterProfile' -v 2>&1 | grep -E "^(--- |FAIL)" && go test ./internal/api/ -run Profile -v 2>&1 | grep -E "^(--- |FAIL)" && ./scripts/test-db.sh -run 'DBProfile|DBMIBDeleteRefusals' -v 2>&1 | grep -E "^(--- |FAIL)"`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/services backend/internal/api/profile_handler.go backend/internal/api/profile_handler_test.go backend/cmd/sentinel/main.go
git commit -m "feat(metrics): metric profiles with the Cisco switch health starter profile

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Metric preview

**Files:**
- Modify: `backend/internal/services/metric_reader.go` (add `PreviewMetric`), `backend/internal/api/profile_handler.go` (route)
- Test: `backend/internal/services/metric_reader_test.go`, `backend/internal/api/profile_handler_test.go`

**Interfaces:**
- Consumes: Task 5 `custommetric.Needed/Evaluate`, Task 6 `ReadColumns`, `DeviceWalker`, Task 7 `ValidateMetric`, `ToDefinition`.
- Produces:
  - `type MetricPreview struct { Rows []PreviewRow; Errors map[string]string; RawCounter bool }`, `PreviewRow{Instance, Label string; Value float64; State string; OK bool; Violates bool}`
  - `func PreviewMetric(ctx context.Context, c snmp.Client, t snmp.Target, m models.ProfileMetric) (*MetricPreview, error)` — validates with key checks skipped (preview before a key is chosen: validate a copy with `Key = "preview_metric"`), reads every+cached OIDs, evaluates; counters show raw values with `RawCounter: true` (rates need two polls); `Violates` per row via `custommetric.Violates`
  - `func (w *DeviceWalker) PreviewDevice(ctx context.Context, d *DeviceView, m models.ProfileMetric) (*MetricPreview, error)`
  - Route: `POST /api/v1/devices/:id/metric-preview` (admin, plus `loadDevice` readonly), body = a `models.ProfileMetric` JSON; 20 s context; validation error → 400; SNMP failure → 200 `{"ok":false,"error":…}`; success → 200 `{"ok":true,"preview":…}`

- [ ] **Step 1: Failing test** in `metric_reader_test.go`:

```go
func TestPreviewMetric(t *testing.T) {
	val, typ, name := "1.3.6.1.4.1.9.9.91.1.1.1.1.4", "1.3.6.1.4.1.9.9.91.1.1.1.1.1", "1.3.6.1.2.1.47.1.1.1.1.7"
	f := &walkFake{walks: map[string][]snmp.PDU{
		val:  {{OID: val + ".1010", Value: int64(75)}, {OID: val + ".1011", Value: int64(12)}},
		typ:  {{OID: typ + ".1010", Value: int64(8)}, {OID: typ + ".1011", Value: int64(4)}},
		name: {{OID: name + ".1010", Value: []byte("Inlet")}},
	}}
	m := models.ProfileMetric{Name: "Temp", Source: "column", Kind: "gauge", Scale: 1, OID: val, FilterOID: typ,
		FilterValues: models.StringArray{"8"}, LabelMode: "same_index", LabelOID: name, RuleKind: "above", RuleValue: f64(70), RuleEnabled: true}
	p, err := PreviewMetric(context.Background(), f, snmp.Target{}, m)
	if err != nil || len(p.Rows) != 1 || p.Rows[0].Label != "Inlet" || p.Rows[0].Value != 75 || !p.Rows[0].Violates {
		t.Fatalf("preview %+v err %v", p, err)
	}
	m.OID = "nope"
	if _, err := PreviewMetric(context.Background(), f, snmp.Target{}, m); err == nil {
		t.Error("invalid metric previewed")
	}
}
```

(imports `models`; `f64` from `profile_starter.go`.) API test: non-admin → 403; admin with readonly site → 200.

- [ ] **Step 2: Run** — fails on undefined `PreviewMetric`.

- [ ] **Step 3: Implement:**

```go
type PreviewRow struct {
	Instance string  `json:"instance"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	State    string  `json:"state,omitempty"`
	OK       bool    `json:"ok"`
	Violates bool    `json:"violates"`
}

type MetricPreview struct {
	Rows       []PreviewRow      `json:"rows"`
	Errors     map[string]string `json:"errors"`
	RawCounter bool              `json:"raw_counter"`
}

// PreviewMetric evaluates a metric definition against a device right now.
func PreviewMetric(ctx context.Context, c snmp.Client, t snmp.Target, m models.ProfileMetric) (*MetricPreview, error) {
	check := m
	check.Key = "preview_metric"
	if err := ValidateMetric(check); err != nil {
		return nil, err
	}
	d := ToDefinition(m)
	every, cached := custommetric.Needed(d)
	cols, errs := ReadColumns(ctx, c, t, append(every, cached...))
	out := &MetricPreview{Rows: []PreviewRow{}, Errors: map[string]string{}, RawCounter: m.Kind == "counter"}
	for oid, err := range errs {
		out.Errors[oid] = err.Error()
	}
	for _, r := range custommetric.Evaluate(d, cols) {
		out.Rows = append(out.Rows, PreviewRow{Instance: r.Instance, Label: r.Label, Value: r.Value, State: r.State, OK: r.OK,
			Violates: d.Rule.Kind != "" && custommetric.Violates(d.Rule, d.Kind, r)})
	}
	return out, nil
}
```

`PreviewDevice` builds the target like `TestWalkDevice`. Handler mirrors the test-walk handler with `RequireAdmin` in front.

- [ ] **Step 4: Run tests** — `go test ./internal/services/ -run PreviewMetric ./internal/api/ -run Preview` → PASS.

- [ ] **Step 5: Commit** `feat(metrics): preview a metric against a live device`.

---

### Task 9: Metric-rule incidents

**Files:**
- Modify: `backend/internal/services/incident_service.go`
- Test: `backend/internal/services/metric_incidents_db_test.go`, `backend/internal/services/ups_monitor_test.go` (unchanged behaviour check below)

**Interfaces:**
- Produces:
  - `func (s *IncidentService) OpenMetricIncident(ctx context.Context, deviceID uuid.UUID, key, instance string, start time.Time, reason string) (*models.Incident, bool, error)` — idempotent per (device, key, instance); duplicate-key race → existing, opened=false
  - `func (s *IncidentService) CloseMetricIncident(ctx context.Context, deviceID uuid.UUID, key, instance string, end time.Time, note string) (*models.Incident, error)` — nil, nil when none open
  - `func (s *IncidentService) OpenMetricIncidents(ctx context.Context, deviceID uuid.UUID) ([]models.Incident, error)`
  - **Fix:** `OpenDeviceConditionIncidents` adds `AND condition <> 'metric'` — otherwise `UPSMonitor.ReconcileUPS`, which runs for every non-UPS device each poll, would close every metric incident a minute after it opens.
  - Subject: `incidentSubjectSelect` gains a branch: `WHEN i.condition = 'metric' THEN d.name || ' · ' || COALESCE(pm.name, i.metric_key) || ': ' || COALESCE(NULLIF(ms.label, ''), i.metric_instance)` with `LEFT JOIN profile_metrics pm ON pm.key = i.metric_key LEFT JOIN metrics.series ms ON ms.device_id = i.device_id AND ms.metric = i.metric_key AND ms.instance = i.metric_instance` added to the FROM used with that select (read how the select's FROM is composed and add the joins there).

- [ ] **Step 1: Failing DB test** `metric_incidents_db_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBMetricIncidents(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET created_at = now() - interval '2 days' WHERE id = ?`, s.DeviceID)
	inc := NewIncidentService(db)
	start := time.Now().UTC().Add(-time.Hour)
	a, opened, err := inc.OpenMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", start, "Fan 2 is down")
	if err != nil || !opened || a.MetricKey == nil || *a.Condition != models.IncidentConditionMetric {
		t.Fatalf("open %+v %v %v", a, opened, err)
	}
	if b, opened, _ := inc.OpenMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", start, "again"); opened || b.ID != a.ID {
		t.Fatal("second open was not idempotent")
	}
	if _, opened, _ := inc.OpenMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1005", start, "other row"); !opened {
		t.Fatal("another row did not open its own incident")
	}
	// UPS reconciliation lists only UPS-style conditions.
	if ups, _ := inc.OpenDeviceConditionIncidents(ctx, s.DeviceID); len(ups) != 0 {
		t.Fatalf("metric incidents listed as UPS ones: %d", len(ups))
	}
	if open, _ := inc.OpenMetricIncidents(ctx, s.DeviceID); len(open) != 2 {
		t.Fatalf("open metric incidents %d", len(open))
	}
	// Not downtime.
	d, err := NewDeviceService(db, NewSNMPCredentialService(db), inc).Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Availability30d == nil || *d.Availability30d != 100 {
		t.Errorf("availability %v", d.Availability30d)
	}
	closed, err := inc.CloseMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", time.Now().UTC(), "")
	if err != nil || closed == nil || closed.EndTime == nil {
		t.Fatalf("close %+v %v", closed, err)
	}
	if again, err := inc.CloseMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", time.Now().UTC(), ""); err != nil || again != nil {
		t.Fatalf("second close %+v %v", again, err)
	}
	// Subject names the metric and the row's label.
	testdb.Exec(t, db, `INSERT INTO metrics.series (device_id, metric, instance, label) VALUES (?, 'cisco_fan_fru', '1005', 'Switch 1 - Fan 2')`, s.DeviceID)
	list, _, err := inc.ListIncidents(ctx, IncidentListOptions{DeviceID: &s.DeviceID, Limit: 10})
	testdb.Must(t, err)
	found := false
	for _, r := range list {
		if r.MetricInstance != nil && *r.MetricInstance == "1005" {
			found = true
			if r.SubjectName != "dev-10.0.0.9 · cisco_fan_fru: Switch 1 - Fan 2" {
				t.Errorf("subject %q", r.SubjectName)
			}
		}
	}
	if !found {
		t.Error("metric incident not listed")
	}
}
```

Check `seedDevice`'s device name (the test above assumes `dev-<host>`; read the helper and use its real name in the expected subject). `IncidentListOptions` field names: read the struct and adapt (`DeviceID`, `Limit`).

- [ ] **Step 2: Run** — `./scripts/test-db.sh -run DBMetricIncidents -v` fails (undefined methods).

- [ ] **Step 3: Implement** — mirror `OpenDeviceConditionIncident` / `CloseDeviceConditionIncident` / `activeDeviceConditionIncident`, keyed on `device_id = ? AND condition = 'metric' AND metric_key = ? AND metric_instance = ? AND end_time IS NULL`, setting `Condition = "metric"`, `MetricKey`, `MetricInstance`, `IncidentType = models.IncidentTypeError`; `OpenMetricIncidents` lists `device_id = ? AND condition = 'metric' AND end_time IS NULL`. Apply the `OpenDeviceConditionIncidents` fix and the subject branch. `CloseDeviceConditionIncidentsTx` (pause) already matches `condition IS NOT NULL`, so pausing closes metric incidents too — keep it.

- [ ] **Step 4: Run** — `./scripts/test-db.sh -run 'DBMetricIncidents|UPSIncident|DeviceCondition|PauseCloses' -v` → PASS; `go test ./internal/services/ -run UPSMonitor` → PASS.

- [ ] **Step 5: Commit** `feat(metrics): metric-rule incidents, kept apart from UPS reconciliation`.

---

### Task 10: `ProfileMonitor` — poll profiles, store labelled history, alert; device Health API

**Files:**
- Create: `backend/internal/services/profile_monitor.go`, `backend/internal/services/device_health.go`
- Modify: `backend/internal/services/metrics_store.go` (`SamplePoint.Label`, label upsert), `backend/internal/services/device_poller.go` (`ProfilePoller`, `SetProfiles`, call), `backend/cmd/sentinel/main.go`, `backend/internal/api/profile_handler.go` (`GET /devices/:id/health`)
- Test: `backend/internal/services/profile_monitor_test.go`, `backend/internal/services/metrics_store_label_db_test.go`, `backend/internal/services/device_health_db_test.go`, `backend/internal/services/device_poller_test.go`

**Interfaces:**
- Consumes: Tasks 5–9.
- Produces:
  - `SamplePoint.Label string` — written to `metrics.series.label` when non-empty and different (series identity unchanged)
  - `type ProfileSource interface { ProfilesForDevice(ctx context.Context, d models.Device) ([]ProfileWithMetrics, error); SaveRun(ctx context.Context, deviceID, profileID uuid.UUID, at time.Time, ok bool, errText string) error }`
  - `type MetricIncidents interface { OpenMetricIncident(...); CloseMetricIncident(...); OpenMetricIncidents(...) }` (Task 9 signatures)
  - `func NewProfileMonitor(profiles ProfileSource, metrics MetricsWriter, incidents MetricIncidents, notifier Notifier, client snmp.Client, sites SiteNamer) *ProfileMonitor`
  - `func (m *ProfileMonitor) PollProfiles(ctx context.Context, d models.Device, t snmp.Target)` — for every up device after the stats and UPS polls
  - `type ProfilePoller interface { PollProfiles(ctx context.Context, d models.Device, t snmp.Target) }`; `func (p *DevicePoller) SetProfiles(pp ProfilePoller)`
  - `type DeviceHealthView struct { Profiles []DeviceProfileView; Metrics []HealthMetric }`, `HealthMetric{Key, Name, Kind, Units string; Rule string; Rows []HealthRow}`, `HealthRow{Instance, Label string; Value float64; State string; OK bool; Problem bool}`
  - `func (s *ProfileService) DeviceHealth(ctx context.Context, d *DeviceView, metrics *MetricsStore, incidents *IncidentService) (*DeviceHealthView, error)` — latest values within the live window (`liveSince(now, d.PollInterval * profile interval)` — use 3 × the longest applicable interval, minimum 5 minutes), labels from `metrics.series`, states from the metric's `StateNames`, `Problem` when an open metric incident exists for that row
  - Route `GET /api/v1/devices/:id/health` (readonly)

**Behaviour (PollProfiles), each rule backed by a test below:**
1. Load applicable profiles. With none, still reconcile: close quietly every open metric incident on the device (note `"Closed: no profile applies to this device any more."`).
2. A profile is due when `now - lastRun[device][profile] >= interval` (first poll: due). All due profiles share one 30 s context.
3. For due profiles: gather `Needed` OIDs across their metrics; walk "every" OIDs each run; walk "cached" OIDs when the device's cache is missing or older than `inventoryInterval` (15 min); one `ReadColumns` call for the union.
4. A metric whose OIDs all read (empty is fine) is evaluated; counters go through `Rates` with per-(device,key) previous samples in memory. A metric needing an OID that errored is skipped this run. Run status per profile: OK unless any of its OIDs errored, then `Error = "<first OID>: <error>"`.
5. Write samples `{Metric: key, Instance: row.Instance, Label: row.Label, Value: row.Value}` (status metrics store the state code).
6. Rules (enabled, non-empty kind): previous active set = open metric incidents for that key (the truth); hold clocks per (device,key,instance) in memory; `EvalRule`; Started → `OpenMetricIncident` + notify when opened; ended → `CloseMetricIncident` + notify when closed. An open incident whose row now evaluates inside the rule but had no change (a failed close earlier) is covered by the truth rule: it is in the active set, so `EvalRule` produces the end change.
7. Reconcile quietly: open metric incidents whose key is not an applicable metric with an enabled rule → close with note `"Closed: the rule was turned off or removed."`.
8. Notifications follow `UPSMonitor.notify`'s shape (device channels; `nil` = all, `[]` = none; site name once per poll). Status: `warning`; `down` for `not_ok` rules. Messages:
   - status start: `"<dev> <label> is <state>"` + `" (was <previous state>)"` when the previous state is known (kept in memory per row)
   - status end: `"<dev> <label> is <state> again"` + `" after <duration>."`
   - numeric start: `"<dev> <metric name>, <label>: <value> <units> (above <thr> <units> for <hold> min)"` (`below` likewise; omit `for … min` when hold is 0; trim the trailing space when units are empty)
   - numeric end: `"<dev> <metric name>, <label>: back to <value> <units> after <duration>."`
   - `MonitorName`: `"<dev> · <metric name>: <label>"`

- [ ] **Step 1: Failing tests.**

`metrics_store_label_db_test.go`:

```go
func TestDBSeriesLabel(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	SetCustomMetricKeys([]string{"acme_temp"})
	defer SetCustomMetricKeys(nil)
	m := NewMetricsStore(db)
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: "acme_temp", Instance: "1", Label: "Inlet", Value: 30}}))
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: "acme_temp", Instance: "1", Label: "Inlet (rear)", Value: 31}}))
	var label string
	var n int64
	testdb.Must(t, db.Raw(`SELECT label FROM metrics.series WHERE metric = 'acme_temp'`).Scan(&label).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE metric = 'acme_temp'`).Scan(&n).Error)
	if label != "Inlet (rear)" || n != 1 {
		t.Errorf("label %q series %d", label, n)
	}
}
```

`profile_monitor_test.go` — fakes: `fakeProfileSource` (returns profiles; records runs), the Task 5 UPS-test `fakeUPSMetrics`-style writer (reuse `fakeUPSMetrics`), a `fakeMetricIncidents` (map keyed `key|instance`, `failClose` flag), `fakeNotifier` (from `device_poller_test.go`), `fakeSiteNamer` (from `ups_monitor_test.go`), and `walkFake` (Task 6) as the client. A rig with one profile holding a `not_ok` fan metric (`cisco_fan_envmon`, column, label column) and an `above 90 hold 10` CPU metric. Tests:

```go
func TestProfileMonitorFanAlertAndRecovery(t *testing.T)           // fan 2 critical → one incident, one "Fan 2 is critical" alert; back to normal → closed, "is normal again" recovery
func TestProfileMonitorCPUHold(t *testing.T)                        // 95% for 9 polls → nothing; 10th → alert "… CPU busy (5 min), Switch 1: 95 % (above 90 % for 10 min)"
func TestProfileMonitorUnsupportedTablesAreOK(t *testing.T)         // review focus 3: ENVMON walks return empty → run saved OK with no error, no samples for it
func TestProfileMonitorWalkErrorRecordsRun(t *testing.T)            // CPU walk times out → run saved not OK with "…: request timeout"; fan metric still evaluated
func TestProfileMonitorIntervalDue(t *testing.T)                    // interval 5: second poll 1 min later reads nothing; poll at +5 min reads
func TestProfileMonitorLabelsCachedUntilInventory(t *testing.T)     // label column walked on first run, not on the next; walked again after 15 min
func TestProfileMonitorRetriesFailedClose(t *testing.T)             // failClose on recovery → stays open; next poll closes it and sends the recovery once
func TestProfileMonitorReconcilesRemovedRule(t *testing.T)          // open incident for a key no longer in the profile → closed quietly, no notification
func TestProfileMonitorNoProfilesClosesLeftovers(t *testing.T)      // device with no profiles and one open metric incident → closed quietly
func TestProfileMonitorCounterRates(t *testing.T)                   // counter metric: first poll writes nothing for it, second writes the per-second rate
```

The rig and the first test, as the pattern for the rest:

```go
package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

const (
	tFanDescr = "1.3.6.1.4.1.9.9.13.1.4.1.2"
	tFanState = "1.3.6.1.4.1.9.9.13.1.4.1.3"
	tCPU      = "1.3.6.1.4.1.9.9.109.1.1.1.1.8"
)

type fakeProfileSource struct {
	profiles []ProfileWithMetrics
	runs     []models.DeviceProfileRun
}

func (f *fakeProfileSource) ProfilesForDevice(context.Context, models.Device) ([]ProfileWithMetrics, error) {
	return f.profiles, nil
}
func (f *fakeProfileSource) SaveRun(_ context.Context, d, p uuid.UUID, at time.Time, ok bool, e string) error {
	f.runs = append(f.runs, models.DeviceProfileRun{DeviceID: d, ProfileID: p, RanAt: at, OK: ok, Error: e})
	return nil
}

type fakeMetricIncidents struct {
	open      map[string]*models.Incident
	opened    []string
	closed    []string
	failClose bool
}

func (f *fakeMetricIncidents) OpenMetricIncident(_ context.Context, d uuid.UUID, key, inst string, start time.Time, _ string) (*models.Incident, bool, error) {
	k := key + "|" + inst
	if inc, ok := f.open[k]; ok {
		return inc, false, nil
	}
	cond := models.IncidentConditionMetric
	inc := &models.Incident{ID: uuid.New(), DeviceID: &d, Condition: &cond, MetricKey: &key, MetricInstance: &inst, StartTime: start}
	f.open[k] = inc
	f.opened = append(f.opened, k)
	return inc, true, nil
}
func (f *fakeMetricIncidents) CloseMetricIncident(_ context.Context, _ uuid.UUID, key, inst string, end time.Time, _ string) (*models.Incident, error) {
	if f.failClose {
		return nil, errors.New("database unavailable")
	}
	k := key + "|" + inst
	inc, ok := f.open[k]
	if !ok {
		return nil, nil
	}
	delete(f.open, k)
	f.closed = append(f.closed, k)
	inc.EndTime = &end
	inc.DurationSeconds = int(end.Sub(inc.StartTime).Seconds())
	return inc, nil
}
func (f *fakeMetricIncidents) OpenMetricIncidents(context.Context, uuid.UUID) ([]models.Incident, error) {
	var out []models.Incident
	for _, inc := range f.open {
		out = append(out, *inc)
	}
	return out, nil
}

type profileRig struct {
	src    *fakeProfileSource
	agent  *walkFake
	inc    *fakeMetricIncidents
	mets   *fakeUPSMetrics
	notif  *fakeNotifier
	mon    *ProfileMonitor
	dev    models.Device
	now    time.Time
}

func newProfileRig(interval int) *profileRig {
	p, metrics := starterProfile()
	p.ID, p.PollIntervalMinutes = uuid.New(), interval
	var keep []models.ProfileMetric
	for _, m := range metrics {
		if m.Key == "cisco_fan_envmon" || m.Key == "cisco_cpu_5min" {
			keep = append(keep, m)
		}
	}
	// A CPU labelled by index keeps the rig small.
	for i := range keep {
		if keep[i].Key == "cisco_cpu_5min" {
			keep[i].LabelMode, keep[i].LabelPointerOID, keep[i].LabelTargetOID = "index", "", ""
		}
	}
	r := &profileRig{
		src:   &fakeProfileSource{profiles: []ProfileWithMetrics{{Profile: p, Metrics: keep}}},
		agent: &walkFake{walks: map[string][]snmp.PDU{
			tFanState: {{OID: tFanState + ".1", Value: int64(1)}, {OID: tFanState + ".2", Value: int64(1)}},
			tFanDescr: {{OID: tFanDescr + ".1", Value: []byte("Fan 1")}, {OID: tFanDescr + ".2", Value: []byte("Fan 2")}},
			tCPU:      {{OID: tCPU + ".1", Value: uint64(20)}},
		}, fail: map[string]bool{}},
		inc:   &fakeMetricIncidents{open: map[string]*models.Incident{}},
		mets:  &fakeUPSMetrics{},
		notif: &fakeNotifier{},
		now:   time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	r.mon = NewProfileMonitor(r.src, r.mets, r.inc, r.notif, r.agent, fakeSiteNamer{})
	r.mon.now = func() time.Time { return r.now }
	r.dev = models.Device{ID: uuid.New(), Name: "core-3850", Host: "10.0.0.5", SysObjectID: "1.3.6.1.4.1.9.1.2066"}
	return r
}

func (r *profileRig) set(oid, idx string, v any) {
	pdus := r.agent.walks[oid]
	for i := range pdus {
		if pdus[i].OID == oid+"."+idx {
			pdus[i].Value = v
		}
	}
}

func (r *profileRig) poll() {
	r.mon.PollProfiles(context.Background(), r.dev, snmp.Target{})
	r.now = r.now.Add(time.Minute)
}

func TestProfileMonitorFanAlertAndRecovery(t *testing.T) {
	r := newProfileRig(1)
	r.poll()
	r.set(tFanState, "2", int64(3)) // critical
	r.poll()
	r.poll() // still critical: nothing new
	if len(r.inc.opened) != 1 || r.inc.opened[0] != "cisco_fan_envmon|2" || len(r.notif.sent) != 1 {
		t.Fatalf("opened %v sent %d", r.inc.opened, len(r.notif.sent))
	}
	start := r.notif.sent[0]
	if start.Message != "core-3850 Fan 2 is critical (was normal)" || start.Status != "down" || start.MonitorName != "core-3850 · Fan: Fan 2" {
		t.Errorf("start %+v", start)
	}
	r.set(tFanState, "2", int64(1))
	r.poll()
	if len(r.inc.closed) != 1 || len(r.notif.sent) != 2 || !strings.HasPrefix(r.notif.sent[1].Message, "core-3850 Fan 2 is normal again") {
		t.Errorf("recovery: closed %v sent %+v", r.inc.closed, r.notif.sent)
	}
	for _, p := range r.mets.points {
		if p.Metric == "cisco_fan_envmon" && p.Instance == "2" && p.Label != "Fan 2" {
			t.Errorf("sample label %q", p.Label)
		}
	}
}
```

Write each with the rig, advancing `m.now` by a minute per poll as `ups_monitor_test.go` does. In `device_poller_test.go` add `TestPollerRunsProfilePoll`: a recording `ProfilePoller` is called once for an up switch and not after a failed poll.

`device_health_db_test.go`: seed starter profile + device with Cisco sysObjectID; write samples for `cisco_fan_envmon` instance `1` (value 1, label "Fan 1") and `2` (value 3, label "Fan 2") and open a metric incident for instance 2; `DeviceHealth` returns the Fan metric with rows `Fan 1 normal OK` and `Fan 2 critical problem`, and the profile view with its last run.

- [ ] **Step 2: Run** — compile failures (undefined `Label`, `NewProfileMonitor`, `SetProfiles`, `DeviceHealth`).

- [ ] **Step 3: Implement the label write** in `metrics_store.go`: add `Label string` to `SamplePoint`; the series cache stores `{id int64; label string}`; when cached and `p.Label != "" && p.Label != cached.label`, run `UPDATE metrics.series SET label = ? WHERE id = ?` and update the cache; the insert sets `label` and its `ON CONFLICT … DO UPDATE` sets `label = CASE WHEN EXCLUDED.label <> '' THEN EXCLUDED.label ELSE metrics.series.label END`.

- [ ] **Step 4: Implement `ProfileMonitor`** per the Behaviour list. State per device (mutex-guarded): `lastRun map[profileID]time.Time`, `cache custommetric.Columns`, `cacheAt time.Time`, `prevSamples map[key]map[instance]custommetric.Sample`, `holds map[key]custommetric.RuleState` (only `Since` is used across polls; `Active` is rebuilt from open incidents each poll), `lastState map[key]map[instance]string`. Use the same `site := ""` lazy site-name lookup and `notify` structure as `UPSMonitor` (`ups_monitor.go`). Keep functions under ~60 lines: `due`, `read`, `evaluateProfile`, `applyRule`, `reconcile`, `notify`, `message`.

Wire the poller exactly like `SetUPS`:

```go
// ProfilePoller runs a device's profile poll (ProfileMonitor).
type ProfilePoller interface {
	PollProfiles(ctx context.Context, d models.Device, t snmp.Target)
}

// SetProfiles makes every successful poll of an up device run its profile
// poll (custom metrics) too.
func (p *DevicePoller) SetProfiles(pp ProfilePoller) { p.profiles = pp }
```

and after the UPS block: `if result == PollOK && tr.Status == models.DeviceStatusUp && p.profiles != nil { p.profiles.PollProfiles(ctx, d, target) }`. In `main.go`: `devicePoller.SetProfiles(services.NewProfileMonitor(profileService, metricsStore, incidentService, notificationManager, snmpClient, portService))`.

- [ ] **Step 5: Implement `DeviceHealth`** and the route (readonly `loadDevice`; 200 with the view).

- [ ] **Step 6: Run tests**

Run: `cd backend && gofmt -l internal cmd; go vet ./... && go test ./internal/services/ -run 'ProfileMonitor|Poller' -v 2>&1 | grep -E "^(--- |FAIL)" && ./scripts/test-db.sh -run 'DBSeriesLabel|DBDeviceHealth' -v 2>&1 | grep -E "^(--- |FAIL)" && ./scripts/test-db.sh 2>&1 | grep -E "^(FAIL|---|panic)"`
Expected: all PASS; full DB suite clean.

- [ ] **Step 7: Commit** `feat(metrics): poll profiles, store labelled history and alert on rules`.

---
### Task 11: Simulated Cisco health data and the end-to-end test

**Files:**
- Modify: `deploy/snmpsim/gen_data.py`, regenerate `deploy/snmpsim/data/cisco.snmprec`
- Create: `backend/internal/services/profile_sim_db_test.go`

**Interfaces:**
- Consumes: everything in Tasks 1–10.
- Produces: simulator rows for the `cisco` community: ENTITY names (1000 "Switch 1", 1004 "Switch 1 - Fan 1", 1005 "Switch 1 - Fan 2", 1010 "Switch 1 - Inlet Temp Sensor", 1011 "Switch 1 - 12V Rail", 1020 "Switch 1 - FAN-T1", 1030 "Switch 1 - Power Supply A"), CPU (index 1 → phys 1000, 5-min 23; index 2 → phys 0, 5-min 4), enhanced mempool (`1.1` "Processor" used 300 free 700), classic mempool (`1` "Processor" used 600 free 400), ENVMON temperature (1 "Switch 1 Inlet" 31), fans (1 "Switch 1 Fan 1" normal(1); 2 "Switch 1 Fan 2" critical(3)), supply (1 "Switch 1 PS A" normal(1); 2 "Switch 1 PS B" notPresent(5)), ENTITY-SENSOR (1010 celsius(8) precision 1 value 415; 1011 voltsDC(4) precision 3 value 12000), FRU fan tray 1020 up(2), FRU power 1030 on(2).

- [ ] **Step 1: Add the rows.** In `gen_data.py`, add:

```python
def cisco_health():
    ent = "1.3.6.1.2.1.47.1.1.1.1.7"
    names = {1000: "Switch 1", 1004: "Switch 1 - Fan 1", 1005: "Switch 1 - Fan 2", 1010: "Switch 1 - Inlet Temp Sensor",
             1011: "Switch 1 - 12V Rail", 1020: "Switch 1 - FAN-T1", 1030: "Switch 1 - Power Supply A"}
    rows = [f"{ent}.{i}|4|{n}" for i, n in names.items()]
    cpu = "1.3.6.1.4.1.9.9.109.1.1.1.1"
    rows += [f"{cpu}.2.1|2|1000", f"{cpu}.8.1|66|23", f"{cpu}.2.2|2|0", f"{cpu}.8.2|66|4"]
    emp = "1.3.6.1.4.1.9.9.221.1.1.1.1"
    rows += [f"{emp}.3.1.1|4|Processor", f"{emp}.18.1.1|70|300", f"{emp}.20.1.1|70|700"]
    cmp_ = "1.3.6.1.4.1.9.9.48.1.1.1"
    rows += [f"{cmp_}.2.1|4|Processor", f"{cmp_}.5.1|66|600", f"{cmp_}.6.1|66|400"]
    env = "1.3.6.1.4.1.9.9.13.1"
    rows += [f"{env}.3.1.2.1|4|Switch 1 Inlet", f"{env}.3.1.3.1|66|31",
             f"{env}.4.1.2.1|4|Switch 1 Fan 1", f"{env}.4.1.3.1|2|1",
             f"{env}.4.1.2.2|4|Switch 1 Fan 2", f"{env}.4.1.3.2|2|3",
             f"{env}.5.1.2.1|4|Switch 1 PS A", f"{env}.5.1.3.1|2|1",
             f"{env}.5.1.2.2|4|Switch 1 PS B", f"{env}.5.1.3.2|2|5"]
    sen = "1.3.6.1.4.1.9.9.91.1.1.1.1"
    rows += [f"{sen}.1.1010|2|8", f"{sen}.3.1010|2|1", f"{sen}.4.1010|2|415",
             f"{sen}.1.1011|2|4", f"{sen}.3.1011|2|3", f"{sen}.4.1011|2|12000"]
    fru = "1.3.6.1.4.1.9.9.117.1"
    rows += [f"{fru}.4.1.1.1.1020|2|2", f"{fru}.1.2.1.2.1030|2|2"]
    return rows
```

and append `+ cisco_health()` to the `cisco = (...)` expression. The existing `entity()` writes index 1's class/serial/model; the new names use other indexes, so nothing collides. Then:

```bash
cd deploy/snmpsim && python3 gen_data.py && git diff --stat data/
SNMPSIM_VERSION=1.2.2 ./run.sh
```

Check that `ups.snmprec` (hand-written in UPS monitoring) survived regeneration: `git status --short data/` must not show it deleted; if `gen_data.py` removes unknown files, add `write("ups", ...)` is NOT needed — just confirm the file is untouched.

- [ ] **Step 2: Write the end-to-end test** `backend/internal/services/profile_sim_db_test.go` (DB + simulator; skips without `SENTINEL_TEST_SNMPSIM`, like `snmp/simulator_test.go` — copy its skip helper):

```go
func TestDBSimCiscoHealthEndToEnd(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set")
	}
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	db := testdb.Open(t)
	ctx := context.Background()
	lib := NewMIBLibrary(db)
	testdb.Must(t, lib.SyncBuiltins(ctx))
	profiles := NewProfileService(db)
	testdb.Must(t, profiles.SeedStarter(ctx))
	testdb.Must(t, profiles.Load(ctx))
	s := seedDevice(t, db, "HQ", host)
	testdb.Exec(t, db, `UPDATE devices SET sys_object_id = '1.3.6.1.4.1.9.1.1208', name = 'sim-cisco' WHERE id = ?`, s.DeviceID)
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	target := snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: "cisco"}, Timeout: 2 * time.Second, Retries: 1}

	incidents := NewIncidentService(db)
	metrics := NewMetricsStore(db)
	notif := &fakeNotifier{}
	mon := NewProfileMonitor(profiles, metrics, incidents, notif, snmp.GoSNMPClient{}, NewPortService(db, metrics, incidents, NewSettingsService(db)))
	mon.PollProfiles(ctx, d, target)

	type row struct {
		Metric, Instance, Label string
		Value                   float64
	}
	var got []row
	testdb.Must(t, db.Raw(`SELECT s.metric, s.instance, s.label, x.value FROM metrics.series s
		JOIN LATERAL (SELECT value FROM metrics.samples WHERE series_id = s.id ORDER BY time DESC LIMIT 1) x ON true
		WHERE s.device_id = ? ORDER BY s.metric, s.instance`, d.ID).Scan(&got).Error)
	want := map[string]row{
		"cisco_cpu_5min|1":          {Label: "Switch 1", Value: 23},
		"cisco_cpu_5min|2":          {Label: "Row 2", Value: 4},
		"cisco_mem_used_pct|1.1":    {Label: "Processor", Value: 30},
		"cisco_mem_pool_used_pct|1": {Label: "Processor", Value: 60},
		"cisco_temp_envmon|1":       {Label: "Switch 1 Inlet", Value: 31},
		"cisco_temp_sensor|1010":    {Label: "Switch 1 - Inlet Temp Sensor", Value: 41.5},
		"cisco_fan_envmon|2":        {Label: "Switch 1 Fan 2", Value: 3},
		"cisco_psu_envmon|2":        {Label: "Switch 1 PS B", Value: 5},
		"cisco_fan_fru|1020":        {Label: "Switch 1 - FAN-T1", Value: 2},
		"cisco_psu_fru|1030":        {Label: "Switch 1 - Power Supply A", Value: 2},
	}
	have := map[string]row{}
	for _, r := range got {
		have[r.Metric+"|"+r.Instance] = r
	}
	for k, w := range want {
		if h, ok := have[k]; !ok || h.Label != w.Label || h.Value != w.Value {
			t.Errorf("%s: got %+v, want label %q value %v", k, h, w.Label, w.Value)
		}
	}
	if _, ok := have["cisco_temp_sensor|1011"]; ok {
		t.Error("the volts sensor was stored as a temperature")
	}
	// Exactly one alert: Fan 2 critical (PS B notPresent counts as OK).
	open, err := incidents.OpenMetricIncidents(ctx, d.ID)
	testdb.Must(t, err)
	if len(open) != 1 || *open[0].MetricKey != "cisco_fan_envmon" || *open[0].MetricInstance != "2" || len(notif.sent) != 1 {
		t.Fatalf("open %d, sent %d", len(open), len(notif.sent))
	}
	runs, err := profiles.DeviceProfiles(ctx, d)
	testdb.Must(t, err)
	if len(runs) != 1 || runs[0].LastRun == nil || !runs[0].LastRun.OK {
		t.Errorf("run %+v", runs)
	}
}
```

(imports: `context`, `os`, `strconv`, `strings`, `testing`, `time`, `models`, `snmp`, `testdb`.) Run it with `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 ./scripts/test-db.sh -run DBSimCiscoHealth -v` (check that `test-db.sh` passes the environment through; if it does not, export the variable inside the script's test invocation the way the script passes `SENTINEL_TEST_DATABASE_URL`).

- [ ] **Step 3: Run** — the test PASSES against the regenerated simulator; `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ -run Sim` still PASSES (the Cisco inventory test is unaffected by the new rows).

- [ ] **Step 4: Commit** `test(metrics): simulated Cisco health tables and an end-to-end profile test`.

---

### Task 12: Frontend — MIB library and browser

**Files:**
- Create: `frontend/src/hooks/useMibs.ts`, `frontend/src/pages/network/MibLibrary.tsx`, `frontend/src/pages/network/MibBrowser.tsx`, `frontend/src/components/network/TestWalkPanel.tsx`
- Modify: `frontend/src/App.tsx` (routes `/network/mibs` and `/network/mibs/browse`), `frontend/src/components/Layout.tsx` (nav: `{ to: '/network/mibs/browse', label: 'MIB browser' }`, `{ to: '/network/mibs', label: 'MIB library', end: true, adminOnly: true }`)

**Interfaces:**
- Consumes: Task 4 and Task 6 routes.
- Produces (`useMibs.ts`):

```ts
import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export interface MibModule {
  id: string
  name: string
  source: 'builtin' | 'upload'
  file_name: string
  size_bytes: number
  imports: string[]
  missing: string[]
  status: 'ready' | 'waiting'
  updated_at: string
  objects: number
}

export interface MibObject {
  id: number
  name: string
  oid: string
  parent_oid: string
  kind: 'node' | 'scalar' | 'table' | 'row' | 'column' | 'notification'
  base_type: string
  type_name: string
  units: string
  access: string
  description: string
  enum: Record<string, string> | null
  index_columns: string[]
  module: string
  has_children: boolean
}

export interface MibObjectDetail extends MibObject {
  columns: MibObject[] | null
}

export interface MibUploadResult {
  saved: string[]
  skipped: string[]
  waiting: Record<string, string[]>
}

export interface TestWalkResult {
  oid: string
  columns: { oid: string; name: string; enum: Record<string, string> | null }[]
  rows: { index: string; values: Record<string, { raw: string; meaning: string }> }[]
  truncated: boolean
}

/** Cisco files to upload for names in the browser (Sentinel cannot ship them). */
export const CISCO_MIB_FILES = [
  'CISCO-SMI', 'CISCO-TC', 'CISCO-PROCESS-MIB', 'CISCO-MEMORY-POOL-MIB', 'CISCO-ENHANCED-MEMPOOL-MIB',
  'CISCO-ENVMON-MIB', 'CISCO-ENTITY-SENSOR-MIB', 'CISCO-ENTITY-FRU-CONTROL-MIB',
].map((name) => ({ name, url: `https://raw.githubusercontent.com/cisco/cisco-mibs/main/v2/${name}.my` }))

export function useMibModules() {
  const [modules, setModules] = useState<MibModule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const refetch = useCallback(async () => {
    try {
      const { data } = await api.get<ApiResponse<MibModule[]>>('/network/mibs')
      setModules(data.data ?? [])
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load MIB modules')
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => void refetch(), [refetch])
  return { modules, loading, error, refetch }
}

export async function uploadMibs(files: File[]): Promise<MibUploadResult> {
  const form = new FormData()
  files.forEach((f) => form.append('files', f))
  const { data } = await api.post<ApiResponse<MibUploadResult>>('/network/mibs', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return data.data
}

export async function deleteMib(id: string): Promise<void> {
  await api.delete(`/network/mibs/${id}`)
}

export async function mibChildren(oid: string): Promise<MibObject[]> {
  const { data } = await api.get<ApiResponse<MibObject[]>>('/network/mibs/objects/children', { params: { oid } })
  return data.data ?? []
}

export async function mibSearch(q: string): Promise<MibObject[]> {
  const { data } = await api.get<ApiResponse<MibObject[]>>('/network/mibs/objects/search', { params: { q } })
  return data.data ?? []
}

export async function mibObject(oid: string): Promise<MibObjectDetail> {
  const { data } = await api.get<ApiResponse<MibObjectDetail>>('/network/mibs/objects/by-oid', { params: { oid } })
  return data.data
}

export async function testWalk(deviceId: string, oid: string): Promise<{ ok: boolean; error?: string; result?: TestWalkResult }> {
  const { data } = await api.post<ApiResponse<{ ok: boolean; error?: string; result?: TestWalkResult }>>(
    `/devices/${deviceId}/test-walk`, { oid })
  return data.data
}
```

(Check `src/services/api.ts` for how it handles `FormData`; if its axios instance forces JSON, pass the header as above. Check the response envelope type name in `@/types`.)

- [ ] **Step 1: MIB library page** `MibLibrary.tsx` (admin): a header ("MIB library", one-line explanation); an upload card with a file input accepting `.mib,.my,.txt,.zip` (multiple) and an Upload button; after upload, a result box listing Saved, Skipped ("not MIB files") and Waiting (`"ACME-MIB is waiting for ACME-SMI"`), errors shown in the red error box with the server message (it names file and line); a "Cisco MIBs" note — "Sentinel can't ship Cisco's MIB files. The Cisco switch health profile works without them; upload these for names and descriptions in the browser:" followed by the eight `CISCO_MIB_FILES` links (open in a new tab); a table of modules (Name, Source badge Built-in/Uploaded, Status chip Ready/"Waiting for: …", Objects, File, Updated, Delete button for uploads with an in-page confirm row — no `window.confirm`; a 409 shows "Used by …"). Follow `Credentials.tsx` for page layout, card and button classes.

- [ ] **Step 2: MIB browser page** `MibBrowser.tsx` (all signed-in users): two columns on wide screens, stacked on narrow:
  - left: a search box (debounced 300 ms, min 2 chars) showing results (name, module, OID); below it the tree starting from `mibChildren('')`, nodes expandable (fetch children on expand, cache by OID), each row showing name and kind badge;
  - right: the selected object's detail from `mibObject(oid)` — name, module, OID (copy button using `navigator.clipboard.writeText` with a fallback that selects the text), kind, type and base type, units, access, description (whitespace preserved, scrollable), enumeration as `1 normal · 2 warning …`, and for tables/rows the columns list (name, OID, type);
  - a "Raw OID" input that selects an arbitrary numeric OID (validated `^\d+(\.\d+)+$`) for test-walk;
  - `TestWalkPanel` under the detail.

- [ ] **Step 3: `TestWalkPanel.tsx`** — props `{ oid: string; kind?: string }`: a device picker (load devices with the existing `useDevices` hook; group by site name), a "Test on device" button, then: an error box for `ok:false`; a results table with one column per walked column (header = column name or OID), one row per index (first column = index), cells showing `raw` and, when present, `meaning` in muted text (`3 · critical`); "First 500 rows of more" when `truncated`; the table scrolls horizontally inside its own container. A "Make a metric from this" button (admin only, shown after a successful walk) navigates to `/network/profiles?new=1&oid=<oid>&kind=<kind>` (Task 13 reads these).

- [ ] **Step 4: Routes and nav** — add the two lazy routes in `App.tsx` next to `/network/settings`, and the two nav entries in `Layout.tsx`'s `networkNav` (browser before Credentials; library admin-only).

- [ ] **Step 5: Check** — the frontend check from Global Constraints → exit 0.

- [ ] **Step 6: Commit** `feat(mib): MIB library and browser pages with test-walk`.

---

### Task 13: Frontend — profiles and the metric editor

**Files:**
- Create: `frontend/src/hooks/useProfiles.ts`, `frontend/src/pages/network/Profiles.tsx`, `frontend/src/pages/network/ProfileDetail.tsx`, `frontend/src/components/network/MetricEditor.tsx`, `frontend/src/utils/metrics.ts`
- Modify: `frontend/src/App.tsx` (`/network/profiles`, `/network/profiles/:id`), `frontend/src/components/Layout.tsx` (`{ to: '/network/profiles', label: 'Profiles', adminOnly: true }`)

**Interfaces:**
- Consumes: Task 7 and Task 8 routes; Task 12 `mibSearch`, `mibObject`.
- Produces (`useProfiles.ts`): types mirroring the Go JSON — `MetricProfile` (`id, name, description, match_prefixes, poll_interval_minutes, builtin, created_at, updated_at`), `ProfileView` (+ `metrics`, `devices`), `ProfileMetric` (every column of `profile_metrics` in snake_case, `rule_value: number | null`, `ok_states: number[]`, `state_names: Record<string, string> | null`), `ProfileDetail` (+ `metrics: ProfileMetric[]`), `MetricPreview` (`rows: {instance,label,value,state?,ok,violates}[]`, `errors: Record<string,string>`, `raw_counter: boolean`); functions `useProfiles()`, `useProfile(id)`, `createProfile`, `updateProfile`, `deleteProfile`, `copyProfile`, `createMetric(profileId, m)`, `updateMetric(id, m)`, `deleteMetric(id)`, `metricDataDevices(id)`, `previewMetric(deviceId, m)`. `utils/metrics.ts`: `ruleText(m: ProfileMetric): string` (`"above 90 % for 10 min"`, `"not OK"`, `"—"`), `emptyMetric(): ProfileMetric`, `metricFromMibObject(o: MibObjectDetail): Partial<ProfileMetric>` (column → `source:'column'`; scalar → `'scalar'`; `Enum` present → `kind:'status'`, `state_names` from it; `Counter32/Counter64` base types → `'counter'`; units copied).

- [ ] **Step 1: Profiles list** `Profiles.tsx` — table: Name (Built-in badge), Matches (prefixes, monospace), Every (`1 min`), Metrics, Devices; buttons New profile (inline form: name, description, prefixes one per line, interval select) and per row Copy and Delete (disabled for built-in, with the tooltip "Copy it to make your own"); delete uses an in-page confirm. If the URL has `?new=1&oid=…`, show a picker "Add this as a metric to which profile?" listing non-built-in and built-in profiles; choosing one navigates to `/network/profiles/<id>?addOid=<oid>`.

- [ ] **Step 2: Profile detail** `ProfileDetail.tsx` — editable header (name, description, prefixes, interval; Save), then the metrics table (Name, Key, Source, Kind, Units, Rule via `ruleText`, Edit / Delete). Delete first calls `metricDataDevices` and the confirm reads "Delete <name>? Its history on N devices is deleted too." Add metric opens `MetricEditor` (pre-filled from `metricFromMibObject(await mibObject(addOid))` when `?addOid=` is present).

- [ ] **Step 3: `MetricEditor.tsx`** — a modal form over a `ProfileMetric` draft:
  - Name, Key (auto-suggested from the name as snake_case until edited; read-only when editing an existing metric, with the note "The key names the metric's history and cannot change").
  - Source: Scalar / Table column / Used ÷ (used + free) % — the OID inputs each have a "Find…" button opening a small search (Task 12 `mibSearch`) that fills the numeric OID; the second OID appears for used/free.
  - Kind: Gauge / Counter (per-second rate) / Status. Units, Scale.
  - Advanced (collapsible): Decimal places from column (OID), Keep only rows where column (OID) is one of (comma list).
  - Label: Row index / Column in this table / Column in another table with the same index / Through a pointer column (pointer OID + target OID); show only the OID inputs the mode needs.
  - Status (kind status): a list of states (from `state_names`, editable name; add value/name pairs) with an OK checkbox per state.
  - Alert rule: None / Above / Below (value + hold minutes) / Not OK (hold minutes) — Not OK only offered for status; an Enabled toggle.
  - Preview: device picker + "Preview" → `previewMetric`; shows errors per OID and a table (Label, Value with units or state chip, a red "would alert" mark when `violates`); for counters a note "Counters show raw values here; history stores per-second rates". **Save is disabled until a preview returned at least one row with the current values** (any edit after a preview disables it again — keep a hash of the draft at preview time).
  - Server validation errors (400) show in the error box.

- [ ] **Step 4: Routes and nav** — `/network/profiles`, `/network/profiles/:id` (lazy), nav entry admin-only between MIB library and Credentials.

- [ ] **Step 5: Check** — frontend check → exit 0.

- [ ] **Step 6: Commit** `feat(metrics): profile pages and the metric editor with live preview`.

---

### Task 14: Frontend — device Health section, profile attach/detach, incident labels

**Files:**
- Create: `frontend/src/components/network/HealthSection.tsx`, `frontend/src/hooks/useDeviceHealth.ts`
- Modify: `frontend/src/pages/network/DeviceDetail.tsx`, `frontend/src/components/network/EditDetailsModal.tsx`, `frontend/src/components/network/TrafficChart.tsx` (generic unit), `frontend/src/pages/IncidentDetail.tsx`, `frontend/src/pages/network/DeviceDetail.tsx` (incident list label), `frontend/src/utils/network.ts`

**Interfaces:**
- Consumes: Task 10 `GET /devices/:id/health`, Task 7 `GET/PUT /devices/:id/profiles…`.
- Produces: `useDeviceHealth(deviceId)` (60 s refresh, like `useDevicePorts`), types `DeviceHealth { profiles: DeviceProfileView[]; metrics: HealthMetric[] }`, `HealthMetric { key, name, kind, units, rule, rows: HealthRow[] }`, `HealthRow { instance, label, value, state, ok, problem }`, `DeviceProfileView { profile: MetricProfile; applies: boolean; matched: boolean; mode: 'auto'|'attach'|'detach'; last_run: { ran_at: string; ok: boolean; error: string } | null }`; `setDeviceProfile(deviceId, profileId, mode)`.

- [ ] **Step 1: `TrafficChart` units** — add a `unit` variant `'custom'` with an optional `unitLabel` prop: `formatValue` returns `` `${+v.toFixed(1)} ${unitLabel}` `` (trimmed). Existing callers unchanged.

- [ ] **Step 2: `HealthSection.tsx`** — renders nothing when `metrics` is empty. Otherwise a "Health" heading; one line per applicable profile with its last run (`"Cisco switch health · last poll 1 min ago"` or red `"last poll failed: <error>"`); then a card per metric: name, rule text muted; rows as a compact grid — label, then a value (`41.5 °C`) or a state chip (green OK / red problem / amber when not OK and no rule), a red ring when `problem`; clicking a numeric row opens an inline chart below the card (`TrafficChart` with `query={{ deviceIds:[id], instances:[row.instance] }}`, `lines=[{metric:key,label:row.label,colour}]`, `unit='custom'`, `unitLabel=units`). Grid wraps to one column on phones.

- [ ] **Step 3: Device page** — render `<HealthSection>` after the UPS panel / before Traffic for every device (it hides itself when empty). In the device page's incident list, label metric incidents with their `subject_name` after the device name: `inc.condition === 'metric' ? inc.subject_name.split(' · ').slice(1).join(' · ') : …` (keep the existing port/UPS branches).

- [ ] **Step 4: Edit details** — a "Metric profiles" fieldset listing `DeviceProfileView`s: name, "matches this device" / "doesn't match", and a select Automatic / Always use / Never use (`auto|attach|detach`) saving immediately via `setDeviceProfile` (editors only; the modal is already editor-only).

- [ ] **Step 5: Incident page** — "Detected by" for `condition === 'metric'`: `` `The profile poll: ${inc.subject_name.split(' · ').slice(1).join(' · ')}` ``. Add `metric: 'Metric rule'` to `CONDITION_LABEL`.

- [ ] **Step 6: Check** — frontend check → exit 0.

- [ ] **Step 7: Commit** `feat(metrics): device Health section, profile attach/detach and metric incident labels`.

---

### Task 15: Sandbox verification (controller, with the user)

**Files:** none (failures become fix tasks with their own failing test first).

- [ ] **Step 1: Full runs** — `go vet ./...`, `go test ./...`, `./scripts/test-db.sh`, the simulator tests (`SENTINEL_TEST_SNMPSIM=127.0.0.1:1161`), and the frontend check → all clean.
- [ ] **Step 2: Deploy the branch to the sandbox** — `git -C /srv/docker/sentinel-dev fetch /home/sysadmin/sentinel-phase3 feature/network-phase3 && git -C /srv/docker/sentinel-dev checkout -B feature/network-phase3 FETCH_HEAD && cd /srv/docker/sentinel-dev && docker compose up -d --build`; confirm `applying migration 056_custom_metrics.sql`, `[mib]` sync without errors, no `panic`.
- [ ] **Step 3: Measure** — on the sandbox database: `SELECT count(*) FROM mib_objects` (expect thousands), and after 3 minutes `SELECT metric, count(*) FROM metrics.series WHERE metric LIKE 'cisco_%' GROUP BY 1` for the simulator Cisco device (if it is a sandbox device). Record the profile poll duration from the backend log (add a debug log line only if needed; remove it before merge).
- [ ] **Step 4: Ask the user to check** (after merging to dev and rebuilding their work install, or on the sandbox if they reach it):
  1. MIB library lists the 14 built-ins as Ready; uploading the eight Cisco files from the links makes them Ready and the browser shows `cpmCPUTotal5minRev` with its description.
  2. MIB browser → `ciscoEnvMonFanStatusTable` (or the sensor table) → Test on device against a 3850 shows real rows with meanings.
  3. A 3850 and a 4500-X show the Health section within two minutes: CPU per switch, temperatures with sensible values (not 10× too big — precision), fans and power supplies with states.
  4. Profiles → copy the starter profile, add a metric from the browser with a preview, save, and see it on the device page.
  5. Pull a redundant fan tray or power supply on a lab switch if one is available (or lower the temperature rule to 20 °C temporarily): one incident and alert, then recovery.
- [ ] **Step 5: Hand off to the final review.**
