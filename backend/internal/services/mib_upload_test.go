package services

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

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
		"vendor/v2/ACME-MIB.my":  "ACME-MIB DEFINITIONS ::= BEGIN END",
		"vendor/README.txt":      "Read me",
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

// The 20 MB decompressed budget is for the whole call, not reset per zip:
// several small, highly compressible zips must not be able to add up past it.
func TestExpandUploadBudgetAcrossZips(t *testing.T) {
	entry := strings.Repeat("A", 1900000) // just under the 2 MB per-file limit
	zipOfEntries := func(prefix string) []byte {
		files := map[string]string{}
		for i := 0; i < 6; i++ {
			files[fmt.Sprintf("%s%d.mib", prefix, i)] = entry
		}
		return zipOf(t, files) // ~11.4 MB unpacked, compresses to almost nothing
	}
	z1 := zipOfEntries("a")
	z2 := zipOfEntries("b")

	// One zip alone (~11.4 MB unpacked) is well under the 20 MB budget.
	if _, err := ExpandUpload([]UploadFile{{FileName: "one.zip", Content: z1}}); err != nil {
		t.Fatalf("one zip: %v", err)
	}
	// Two zips together (~22.8 MB unpacked) must not slip past the budget
	// just because each zip's own unpacked count starts back at zero.
	if _, err := ExpandUpload([]UploadFile{{FileName: "one.zip", Content: z1}, {FileName: "two.zip", Content: z2}}); !errors.Is(err, ErrMIBUploadTooLarge) {
		t.Errorf("two zips: %v", err)
	}
}
