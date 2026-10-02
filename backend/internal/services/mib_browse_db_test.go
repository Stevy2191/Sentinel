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
