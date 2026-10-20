package services

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPublicTrafficAliasesWANKeys(t *testing.T) {
	site, real := uuid.New(), uuid.New()
	up := true
	pt := PublicTraffic(site, MapTraffic{Links: map[string]LinkRate{wanKey(real, 13): {OperUp: &up}, "wan:junk": {OperUp: &up}}})
	want := "wan:" + publicID(site, real).String() + ":13"
	if _, ok := pt.Links[want]; !ok || len(pt.Links) != 1 {
		t.Errorf("public traffic %v, want only %s", pt.Links, want)
	}
	for k := range pt.Links {
		if strings.Contains(k, real.String()) {
			t.Errorf("public key %s carries the real id", k)
		}
	}
}
