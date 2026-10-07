package toolruns

import (
	"testing"
	"unicode/utf8"
)

// clip never splits a UTF-8 character: the stored error text stays valid.
func TestClip(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"short", 500, "short"},
		{"exactly", 7, "exactly"},
		{"abcdef", 3, "abc"},
		{"aé", 2, "a"},   // é is 2 bytes: cutting at 2 would split it
		{"aéb", 3, "aé"}, // the cut falls after é
		{"€€", 4, "€"},   // € is 3 bytes
		{"€", 2, ""},     // nothing fits
		{"", 0, ""},
	} {
		got := clip(tc.in, tc.n)
		if got != tc.want || len(got) > tc.n || !utf8.ValidString(got) {
			t.Errorf("clip(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
