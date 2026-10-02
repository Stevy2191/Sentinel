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
	// The unclosed "{" means the parser only notices EOF one line past "END"
	// (line 4 of this input), not at line 3 where the brace was left open.
	if !errors.As(err, &pe) || pe.Line != 4 {
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

// Build must key everything by the module name declared in a file's content
// (what Inspect returns), never by the caller-supplied File.Name: a file
// could be uploaded, or embedded, under any filename.
func TestBuildKeysByDeclaredName(t *testing.T) {
	smi := "ACME-SMI DEFINITIONS ::= BEGIN\nIMPORTS enterprises FROM SNMPv2-SMI;\nacme OBJECT IDENTIFIER ::= { enterprises 99999 }\nEND\n"
	files := append(Builtins(), File{Name: "WRONG-NAME", Content: smi})
	res := Build(files)

	if len(res.Missing["ACME-SMI"]) != 0 {
		t.Fatalf("ACME-SMI missing: %v", res.Missing["ACME-SMI"])
	}
	var acme *Object
	for i, o := range res.Objects["ACME-SMI"] {
		if o.Name == "acme" {
			acme = &res.Objects["ACME-SMI"][i]
		}
	}
	if acme == nil || acme.OID != "1.3.6.1.4.1.99999" {
		t.Fatalf("acme %+v", acme)
	}
	if _, ok := res.Objects["WRONG-NAME"]; ok {
		t.Error("objects stored under the caller-supplied name WRONG-NAME")
	}
	if _, ok := res.Missing["WRONG-NAME"]; ok {
		t.Error("missing stored under the caller-supplied name WRONG-NAME")
	}
}

// A pathological module that panics gosmi must not take the process down:
// the panic becomes "module X could not be loaded".
func TestGuardTurnsPanicIntoError(t *testing.T) {
	err := guard("ACME-MIB", func() error { panic("index out of range") })
	if err == nil || err.Error() != "module ACME-MIB could not be loaded: index out of range" {
		t.Errorf("guard error %v", err)
	}
	if err := guard("ACME-MIB", func() error { return nil }); err != nil {
		t.Errorf("no panic: %v", err)
	}
	want := errors.New("plain failure")
	if err := guard("ACME-MIB", func() error { return want }); err != want {
		t.Errorf("plain error %v", err)
	}
}

// A module whose load panics yields no objects; the others still build.
func TestBuildSurvivesAPanickingModule(t *testing.T) {
	loadModuleHook = func(name string) {
		if name == "IF-MIB" {
			panic("boom")
		}
	}
	defer func() { loadModuleHook = nil }()
	res := Build(Builtins())
	if len(res.Objects["IF-MIB"]) != 0 || len(res.Objects["SNMPv2-MIB"]) == 0 {
		t.Errorf("IF-MIB %d objects, SNMPv2-MIB %d", len(res.Objects["IF-MIB"]), len(res.Objects["SNMPv2-MIB"]))
	}
}
