package services

import (
	"strings"
	"sync"

	"github.com/go-pdf/fpdf"
)

// The PDF's core fonts are single-byte encoded (Windows-1252). Go strings are
// UTF-8, so any character above ASCII reaches the page as its raw UTF-8 bytes
// and is drawn one glyph per byte: a middle dot (U+00B7, bytes C2 B7) renders
// as "Â·", an accented name as mojibake.
//
// fpdf ships the cp1252 mapping table embedded, so the translator needs no
// files on disk. It is built once and reused: the returned function is a pure
// string transform with no tie to the document it came from, and parsing the
// table per call would be wasteful.
var (
	pdfTranslatorOnce sync.Once
	pdfTranslator     func(string) string
)

func pdfTranslate() func(string) string {
	pdfTranslatorOnce.Do(func() {
		doc := fpdf.New("P", "mm", "A4", "")
		if tr := doc.UnicodeTranslatorFromDescriptor(""); !doc.Err() {
			pdfTranslator = tr
		}
	})
	return pdfTranslator
}

// pdfText prepares a string for drawing into the PDF.
//
// Characters cp1252 cannot represent — CJK, emoji, anything outside the table —
// have no glyph in the core fonts, so they are replaced with a close ASCII
// equivalent where one obviously exists and dropped otherwise. Dropping is
// deliberate: a placeholder box in the middle of a monitor's name is no more
// readable than its absence, and this is a report, not a text editor.
func pdfText(s string) string {
	if isASCII(s) {
		return s
	}
	s = foldToLatin(s)
	if tr := pdfTranslate(); tr != nil {
		return tr(s)
	}
	// No translator: strip what cannot be drawn rather than emit raw UTF-8,
	// which is what produced the mojibake in the first place.
	return strings.Map(func(r rune) rune {
		if r > 0xFF {
			return -1
		}
		return r
	}, s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			return false
		}
	}
	return true
}

// latinFolds maps characters that are common in real text but absent from
// cp1252 onto something the core fonts can draw.
var latinFolds = strings.NewReplacer(
	"\u2011", "-", // non-breaking hyphen
	"\u2012", "-", // figure dash
	"\u2015", "-", // horizontal bar
	"\u2212", "-", // minus sign
	" ", " ", // non-breaking space
	" ", " ", // narrow no-break space
	" ", " ", // thin space
	"\u200b", "", // zero-width space
	"\ufeff", "", // byte order mark
	"\u2044", "/", // fraction slash
	"\u2027", "-", // hyphenation point
)

func foldToLatin(s string) string {
	return latinFolds.Replace(s)
}
