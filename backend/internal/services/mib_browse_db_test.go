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

// Two different modules defining an object at the same OID under different
// names: oldName (ACME-OLD-MIB) and newName (ACME-NEW-MIB, uploaded after),
// both at acme 1. Object/Children already pick the newest module's row per
// OID; Search must agree, which means deduping before matching the query
// rather than after — otherwise filtering by "oldname" first could leave
// oldName as the sole surviving row for that OID, and DISTINCT ON would wave
// it through unopposed even though newName is the OID's current owner.
const acmeOldMIB = "ACME-OLD-MIB DEFINITIONS ::= BEGIN\nIMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI acme FROM ACME-SMI;\n" +
	"oldName OBJECT-TYPE SYNTAX Integer32 MAX-ACCESS read-only STATUS current DESCRIPTION \"Old.\" ::= { acme 1 }\nEND\n"
const acmeNewMIB = "ACME-NEW-MIB DEFINITIONS ::= BEGIN\nIMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI acme FROM ACME-SMI;\n" +
	"newName OBJECT-TYPE SYNTAX Integer32 MAX-ACCESS read-only STATUS current DESCRIPTION \"New.\" ::= { acme 1 }\nEND\n"

func TestDBMIBSearchPicksNewestModule(t *testing.T) {
	l, ctx := mibLib(t)
	_, err := l.Upload(ctx, []UploadFile{{FileName: "smi.my", Content: []byte(acmeSMI)}, {FileName: "old.my", Content: []byte(acmeOldMIB)}}, uuid.Nil)
	testdb.Must(t, err)
	_, err = l.Upload(ctx, []UploadFile{{FileName: "new.my", Content: []byte(acmeNewMIB)}}, uuid.Nil)
	testdb.Must(t, err)

	if hits, err := l.Search(ctx, "oldname", 100); err != nil || len(hits) != 0 {
		t.Errorf("superseded name still found: %+v (err %v)", hits, err)
	}
	hits, err := l.Search(ctx, "newname", 100)
	testdb.Must(t, err)
	if len(hits) != 1 || hits[0].Name != "newName" || hits[0].Module != "ACME-NEW-MIB" {
		t.Errorf("current name %+v", hits)
	}
}
