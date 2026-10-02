package services

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// ErrMIBObjectNotFound is returned by Object when no ready module defines an
// object at the given OID.
var ErrMIBObjectNotFound = errors.New("no MIB object at that OID")

// MIBObjectView is one browsable tree node: the MIB object itself, which
// module currently defines it, and whether it has children in the tree.
type MIBObjectView struct {
	models.MIBObject
	Module      string `json:"module" gorm:"column:module"`
	HasChildren bool   `json:"has_children" gorm:"column:has_children"`
}

// MIBObjectDetail is one object's full detail. Columns holds a table's row's
// columns, or a row's own columns; it is empty for every other kind.
type MIBObjectDetail struct {
	MIBObjectView
	Columns []MIBObjectView `json:"columns"`
}

// mibRoots are the two synthetic nodes Children("") returns when no loaded
// module defines them under that exact OID: mib-2 (the standard MIB tree)
// and enterprises (the vendor tree). SNMPv2-SMI, a built-in module, in fact
// declares both as real nodes, so in practice these are replaced by the real
// objects the moment it is ready.
var mibRoots = []struct{ oid, name string }{
	{oid: "1.3.6.1.2.1", name: "mib-2"},
	{oid: "1.3.6.1.4.1", name: "enterprises"},
}

// objectSelect picks, per OID, the object from the most recently updated
// ready module. Callers append a WHERE condition and an ORDER BY/LIMIT.
const objectSelect = `SELECT DISTINCT ON (o.oid) o.*, m.name AS module,
		EXISTS (SELECT 1 FROM mib_objects c WHERE c.parent_oid = o.oid) AS has_children
	FROM mib_objects o
	JOIN mib_modules m ON m.id = o.module_id
	WHERE m.status = 'ready' AND `

// Children lists the objects directly under parentOID, in numeric OID order
// (so .2 sorts before .10). "" lists the two tree roots.
func (l *MIBLibrary) Children(ctx context.Context, parentOID string) ([]MIBObjectView, error) {
	if parentOID == "" {
		return l.roots(ctx)
	}
	var out []MIBObjectView
	err := l.db.WithContext(ctx).Raw(
		`SELECT * FROM (`+objectSelect+`o.parent_oid = ? ORDER BY o.oid, m.updated_at DESC) t
		 ORDER BY string_to_array(oid, '.')::int[]`, parentOID).Scan(&out).Error
	return out, err
}

// roots returns mib-2 and enterprises: the real object when a ready module
// defines it at that exact OID, otherwise a synthetic placeholder.
func (l *MIBLibrary) roots(ctx context.Context) ([]MIBObjectView, error) {
	out := make([]MIBObjectView, 0, len(mibRoots))
	for _, r := range mibRoots {
		v, err := l.objectByOID(ctx, r.oid)
		switch {
		case errors.Is(err, ErrMIBObjectNotFound):
			out = append(out, MIBObjectView{
				MIBObject:   models.MIBObject{Name: r.name, OID: r.oid, Kind: "node"},
				HasChildren: true,
			})
		case err != nil:
			return nil, err
		default:
			out = append(out, *v)
		}
	}
	return out, nil
}

// objectByOID looks up a single object by its exact OID.
func (l *MIBLibrary) objectByOID(ctx context.Context, oid string) (*MIBObjectView, error) {
	var out []MIBObjectView
	err := l.db.WithContext(ctx).Raw(
		objectSelect+`o.oid = ? ORDER BY o.oid, m.updated_at DESC`, oid).Scan(&out).Error
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrMIBObjectNotFound
	}
	return &out[0], nil
}

// Search finds objects by name substring, OID prefix, or description words,
// at most limit results. An exact name match (case-insensitive) sorts first,
// then shorter names.
//
// The per-OID dedup must run before the text predicate: when two ready
// modules define the same OID under different names, filtering text first
// could leave only the superseded module's row as the sole match for that
// OID, which DISTINCT ON would then wave through unopposed. Picking the
// newest module's row for every OID first, and only then matching the query
// against that already-deduped set, keeps Search agreeing with Object/
// Children about which module currently owns an OID.
func (l *MIBLibrary) Search(ctx context.Context, q string, limit int) ([]MIBObjectView, error) {
	var out []MIBObjectView
	err := l.db.WithContext(ctx).Raw(
		`SELECT * FROM (`+objectSelect+`TRUE ORDER BY o.oid, m.updated_at DESC) picked
		 WHERE lower(picked.name) LIKE '%' || lower(?) || '%'
			OR picked.oid LIKE ? || '%'
			OR to_tsvector('simple', picked.name || ' ' || picked.description) @@ plainto_tsquery('simple', ?)
		 ORDER BY (lower(picked.name) = lower(?)) DESC, length(picked.name) ASC
		 LIMIT ?`, q, q, q, q, limit).Scan(&out).Error
	return out, err
}

// Object returns one object's full detail by OID.
func (l *MIBLibrary) Object(ctx context.Context, oid string) (*MIBObjectDetail, error) {
	v, err := l.objectByOID(ctx, oid)
	if err != nil {
		return nil, err
	}
	d := &MIBObjectDetail{MIBObjectView: *v}
	switch v.Kind {
	case "table":
		rows, err := l.Children(ctx, v.OID)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			cols, err := l.Children(ctx, rows[0].OID)
			if err != nil {
				return nil, err
			}
			d.Columns = cols
		}
	case "row":
		cols, err := l.Children(ctx, v.OID)
		if err != nil {
			return nil, err
		}
		d.Columns = cols
	}
	return d, nil
}

// testWalkMaxRows caps how many rows a test walk keeps: a UI preview of a
// table, not a bulk export, and some tables (the ARP cache, the bridge
// forwarding table) run into the tens of thousands of rows.
const testWalkMaxRows = 500

// TestWalkColumn is one column a test walk found: its OID, its name when the
// MIB library knows it (empty for a raw/unknown OID), and its enum when it is
// a named-number INTEGER.
type TestWalkColumn struct {
	OID  string           `json:"oid"`
	Name string           `json:"name"`
	Enum map[int64]string `json:"enum"`
}

// TestWalkCell is one row's value in one column: Raw is the number (decimal,
// no exponent) or the text: Meaning is the enum name for a numeric value
// whose column has one, otherwise empty.
type TestWalkCell struct {
	Raw     string `json:"raw"`
	Meaning string `json:"meaning"`
}

// TestWalkRow is one row of a test walk, keyed by column OID. Index is a
// scalar's "" or a table/row's index (the walked OID's suffix past the
// column, which may itself have several dotted parts for a composite index).
type TestWalkRow struct {
	Index  string                  `json:"index"`
	Values map[string]TestWalkCell `json:"values"`
}

// TestWalkResult is what the MIB browser's "test walk" shows for one OID.
type TestWalkResult struct {
	OID       string           `json:"oid"`
	Columns   []TestWalkColumn `json:"columns"`
	Rows      []TestWalkRow    `json:"rows"`
	Truncated bool             `json:"truncated"`
}

// TestWalk walks oid against a real device and shapes the answer for the MIB
// browser: a table or row walk splits into columns and rows; a lone column
// (or any OID the library does not recognise) walks as a single column
// keyed by its own index; anything else — a scalar, or an OID whose walk came
// back empty — is read with one GET of oid+".0".
func (l *MIBLibrary) TestWalk(ctx context.Context, c snmp.Client, t snmp.Target, oid string) (*TestWalkResult, error) {
	obj, err := l.Object(ctx, oid)
	if err != nil && !errors.Is(err, ErrMIBObjectNotFound) {
		return nil, err
	}

	switch {
	case obj != nil && obj.Kind == "table":
		pdus, err := c.Walk(ctx, t, oid)
		if err != nil {
			return nil, err
		}
		return buildTestWalkTable(oid, obj, pdus, 1), nil
	case obj != nil && obj.Kind == "row":
		pdus, err := c.Walk(ctx, t, oid)
		if err != nil {
			return nil, err
		}
		return buildTestWalkTable(oid, obj, pdus, 0), nil
	case obj != nil && obj.Kind == "scalar":
		return testWalkScalar(ctx, c, t, oid, obj)
	default:
		pdus, err := c.Walk(ctx, t, oid)
		if err != nil {
			return nil, err
		}
		if len(pdus) == 0 {
			return testWalkScalar(ctx, c, t, oid, obj)
		}
		return buildTestWalkColumn(oid, obj, pdus), nil
	}
}

// testWalkCell formats one answer for its column: a number (or its enum name
// when the column has one for that value), else the text.
func testWalkCell(p snmp.PDU, col TestWalkColumn) TestWalkCell {
	v := ValueOf(p)
	if !v.NumOK {
		return TestWalkCell{Raw: v.Text}
	}
	cell := TestWalkCell{Raw: strconv.FormatFloat(v.Num, 'f', -1, 64)}
	if name, ok := col.Enum[int64(v.Num)]; ok {
		cell.Meaning = name
	}
	return cell
}

// testWalkScalar reads oid+".0" with one GET (retried per OID if the agent
// refuses it), for a scalar object or any OID whose walk answered nothing.
func testWalkScalar(ctx context.Context, c snmp.Client, t snmp.Target, oid string, obj *MIBObjectDetail) (*TestWalkResult, error) {
	scalarOID := oid + ".0"
	pdus, err := snmp.GetEach(ctx, c, t, []string{scalarOID})
	if err != nil {
		return nil, err
	}
	col := TestWalkColumn{OID: scalarOID}
	if obj != nil {
		col.Name, col.Enum = obj.Name, obj.Enum
	}
	rows := make([]TestWalkRow, 0, len(pdus))
	for _, p := range pdus {
		rows = append(rows, TestWalkRow{Values: map[string]TestWalkCell{col.OID: testWalkCell(p, col)}})
	}
	return &TestWalkResult{OID: oid, Columns: []TestWalkColumn{col}, Rows: rows}, nil
}

// buildTestWalkColumn shapes a walk of a single column (or an OID the
// library does not recognise) into one column, one row per index: the
// walked OID's suffix past oid.
func buildTestWalkColumn(oid string, obj *MIBObjectDetail, pdus []snmp.PDU) *TestWalkResult {
	col := TestWalkColumn{OID: oid}
	if obj != nil {
		col.Name, col.Enum = obj.Name, obj.Enum
	}
	rows := make([]TestWalkRow, 0, len(pdus))
	truncated := false
	for _, p := range pdus {
		index, ok := strings.CutPrefix(p.OID, oid+".")
		if !ok {
			continue
		}
		if len(rows) >= testWalkMaxRows {
			truncated = true
			continue
		}
		rows = append(rows, TestWalkRow{Index: index, Values: map[string]TestWalkCell{col.OID: testWalkCell(p, col)}})
	}
	return &TestWalkResult{OID: oid, Columns: []TestWalkColumn{col}, Rows: rows, Truncated: truncated}
}

// buildTestWalkTable shapes a table or row walk into columns and rows. skip
// is 1 for a table walk, where each PDU's suffix past oid is
// "1.<col>.<index>" (the leading 1 is the table's one row-entry sub-id), and
// 0 for a row walk, where the suffix is already "<col>.<index>". obj's
// Columns (already loaded by Object, for a table from its row, for a row
// directly) name and enum the columns the library knows.
func buildTestWalkTable(oid string, obj *MIBObjectDetail, pdus []snmp.PDU, skip int) *TestWalkResult {
	known := map[string]MIBObjectView{}
	if obj != nil {
		for _, c := range obj.Columns {
			known[c.OID] = c
		}
	}

	cols := map[string]TestWalkColumn{}
	rows := map[string]*TestWalkRow{}
	truncated := false

	for _, p := range pdus {
		suffix, ok := strings.CutPrefix(p.OID, oid+".")
		if !ok {
			continue
		}
		parts := strings.Split(suffix, ".")
		if len(parts) < skip+2 {
			continue
		}
		colOID := oid + "." + strings.Join(parts[:skip+1], ".")
		index := strings.Join(parts[skip+1:], ".")

		col, ok := cols[colOID]
		if !ok {
			col = TestWalkColumn{OID: colOID}
			if kc, ok := known[colOID]; ok {
				col.Name, col.Enum = kc.Name, kc.Enum
			}
			cols[colOID] = col
		}

		row, ok := rows[index]
		if !ok {
			if len(rows) >= testWalkMaxRows {
				truncated = true
				continue
			}
			row = &TestWalkRow{Index: index, Values: map[string]TestWalkCell{}}
			rows[index] = row
		}
		row.Values[colOID] = testWalkCell(p, col)
	}

	colOIDs := make([]string, 0, len(cols))
	for k := range cols {
		colOIDs = append(colOIDs, k)
	}
	sort.Slice(colOIDs, func(i, j int) bool { return oidLess(colOIDs[i], colOIDs[j]) })
	outCols := make([]TestWalkColumn, len(colOIDs))
	for i, k := range colOIDs {
		outCols[i] = cols[k]
	}

	indices := make([]string, 0, len(rows))
	for k := range rows {
		indices = append(indices, k)
	}
	sort.Slice(indices, func(i, j int) bool { return oidLess(indices[i], indices[j]) })
	outRows := make([]TestWalkRow, len(indices))
	for i, k := range indices {
		outRows[i] = *rows[k]
	}

	return &TestWalkResult{OID: oid, Columns: outCols, Rows: outRows, Truncated: truncated}
}

// oidLess reports whether OID a sorts before OID b by numeric arc comparison
// (so .2 sorts before .10, unlike a plain string compare).
func oidLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, _ := strconv.Atoi(as[i])
		bn, _ := strconv.Atoi(bs[i])
		if an != bn {
			return an < bn
		}
	}
	return len(as) < len(bs)
}
