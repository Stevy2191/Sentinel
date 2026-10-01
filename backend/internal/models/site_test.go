package models

import (
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

func TestNormalizeSiteInput(t *testing.T) {
	t.Run("trims name and optional fields", func(t *testing.T) {
		got, err := NormalizeSiteInput(SiteInput{Name: "  HQ  ", Description: strp("  main office "), Street: strp("  ")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "HQ" {
			t.Errorf("name = %q, want %q", got.Name, "HQ")
		}
		if got.Description == nil || *got.Description != "main office" {
			t.Errorf("description = %v, want %q", got.Description, "main office")
		}
		// A blank optional field is stored as NULL, not as an empty string, so
		// "no address" has one representation.
		if got.Street != nil {
			t.Errorf("street = %q, want nil", *got.Street)
		}
	})

	t.Run("address parts are trimmed, blanks become nil", func(t *testing.T) {
		got, err := NormalizeSiteInput(SiteInput{Name: "HQ", Street: strp(" 12 Main St "), City: strp(" Springfield"),
			State: strp("IL "), Zip: strp("  ")})
		if err != nil {
			t.Fatal(err)
		}
		if *got.Street != "12 Main St" || *got.City != "Springfield" || *got.State != "IL" || got.Zip != nil {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("whitespace-only name is rejected", func(t *testing.T) {
		if _, err := NormalizeSiteInput(SiteInput{Name: "   "}); err == nil {
			t.Fatal("want error for blank name")
		}
	})

	t.Run("name longer than 255 characters is rejected", func(t *testing.T) {
		if _, err := NormalizeSiteInput(SiteInput{Name: strings.Repeat("a", 256)}); err == nil {
			t.Fatal("want error for 256-character name")
		}
	})

	t.Run("255 characters is allowed", func(t *testing.T) {
		if _, err := NormalizeSiteInput(SiteInput{Name: strings.Repeat("é", 255)}); err != nil {
			t.Fatalf("255 characters (multi-byte) should pass: %v", err)
		}
	})
}
