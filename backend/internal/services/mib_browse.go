package services

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
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
func (l *MIBLibrary) Search(ctx context.Context, q string, limit int) ([]MIBObjectView, error) {
	var out []MIBObjectView
	err := l.db.WithContext(ctx).Raw(
		`SELECT * FROM (`+objectSelect+`(
			lower(o.name) LIKE '%' || lower(?) || '%'
			OR o.oid LIKE ? || '%'
			OR to_tsvector('simple', o.name || ' ' || o.description) @@ plainto_tsquery('simple', ?)
		) ORDER BY o.oid, m.updated_at DESC) t
		ORDER BY (lower(name) = lower(?)) DESC, length(name) ASC
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
