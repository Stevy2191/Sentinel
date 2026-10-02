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

// A module whose OID assignments loop is refused (gosmi would hang loading
// it), naming the file and the loop; nothing from the upload is saved.
func TestDBMIBUploadRefusesOIDCycle(t *testing.T) {
	l, ctx := mibLib(t)
	loop := "LOOP-MIB DEFINITIONS ::= BEGIN\na OBJECT IDENTIFIER ::= { b 1 }\nb OBJECT IDENTIFIER ::= { a 1 }\nEND\n"
	_, err := l.Upload(ctx, []UploadFile{{FileName: "smi.my", Content: []byte(acmeSMI)}, {FileName: "loop.my", Content: []byte(loop)}}, uuid.Nil)
	var ue *MIBUploadError
	if !errors.As(err, &ue) || ue.File != "loop.my" || !errors.Is(err, mib.ErrOIDCycle) || !strings.Contains(err.Error(), "a → b → a") {
		t.Fatalf("err %v", err)
	}
	list, _ := l.List(ctx)
	for _, m := range list {
		if m.Name == "ACME-SMI" || m.Name == "LOOP-MIB" {
			t.Errorf("%s saved from a rejected upload", m.Name)
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

	// A profile metric reading one of ACME-MIB's OIDs blocks the delete too.
	db := l.db
	profileID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO metric_profiles (id, name, match_prefixes) VALUES (?, ?, '{1.3.6.1.4.1.99999}')`,
		profileID, "ACME health")
	testdb.Exec(t, db, `INSERT INTO profile_metrics (id, profile_id, name, key, source, kind, oid) VALUES (?, ?, ?, ?, 'scalar', 'gauge', ?)`,
		uuid.New(), profileID, "ACME temperature", "acme_temp", "1.3.6.1.4.1.99999.1")
	var inUseMetric *MIBInUseError
	if _, err := l.Delete(ctx, module(t, l, "ACME-MIB").ID); !errors.As(err, &inUseMetric) || strings.Join(inUseMetric.Metrics, ",") != "acme_temp" {
		t.Errorf("delete module referenced by a metric: %v", err)
	}
	testdb.Exec(t, db, `DELETE FROM metric_profiles WHERE id = ?`, profileID) // cascades the metric

	if _, err := l.Delete(ctx, module(t, l, "ACME-MIB").ID); err != nil {
		t.Errorf("delete leaf: %v", err)
	}
}
