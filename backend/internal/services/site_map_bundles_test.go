package services

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
)

var (
	bundleA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	bundleB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	bundleC = uuid.MustParse("00000000-0000-0000-0000-00000000000c")
)

func bEnd(dev uuid.UUID, ix int) MapEnd {
	if ix == 0 {
		return MapEnd{DeviceID: dev}
	}
	return MapEnd{DeviceID: dev, IfIndex: &ix, Label: itoaTest(ix), Name: "0/" + itoaTest(ix)}
}

func bLink(a MapEnd, b MapEnd, rateEnd string, speed int64, sources ...string) MapLink {
	ka, kb := sitemap.End{DeviceID: a.DeviceID}, sitemap.End{DeviceID: b.DeviceID}
	if a.IfIndex != nil {
		ka.IfIndex = *a.IfIndex
	}
	if b.IfIndex != nil {
		kb.IfIndex = *b.IfIndex
	}
	return MapLink{Key: sitemap.KeyOf(ka, kb), A: a, B: b, RateEnd: rateEnd, SpeedBps: speed, Sources: sources, ThresholdPct: 80}
}

func noLAG(MapEnd) (string, int) { return "", 0 }

func TestBundleTwoMembers(t *testing.T) {
	l49 := bLink(bEnd(bundleA, 49), bEnd(bundleB, 49), "a", 10e9, "lldp")
	l50 := bLink(bEnd(bundleB, 50), bEnd(bundleA, 50), "a", 10e9, "cdp", "lldp")
	out := bundleLinks([]MapLink{l49, l50}, noLAG)
	if len(out) != 1 {
		t.Fatalf("links = %+v, want one bundle", out)
	}
	b := out[0]
	if b.Key != "bundle:"+l49.Key+","+l50.Key || !isBundleKey(b.Key) || !reflect.DeepEqual(bundleMembers(b.Key), []string{l49.Key, l50.Key}) {
		t.Errorf("key %q", b.Key)
	}
	if len(b.Members) != 2 || b.Members[0].Key != l49.Key || b.Members[1].Key != l50.Key {
		t.Fatalf("members = %+v", b.Members)
	}
	m := b.Members[1]
	if m.A.DeviceID != bundleA || *m.A.IfIndex != 50 || m.B.DeviceID != bundleB || m.RateEnd != "b" {
		t.Errorf("second member = %+v, want A:50–B:50 with rate end b", m)
	}
	if b.A.DeviceID != bundleA || *b.A.IfIndex != 49 || *b.B.IfIndex != 49 || b.SpeedBps != 20e9 || b.RateEnd != "b" ||
		b.ManualID != nil || b.Lag != nil || b.Guess || b.Stale {
		t.Errorf("bundle = %+v", b)
	}
	if !reflect.DeepEqual(b.Sources, []string{"lldp", "cdp"}) {
		t.Errorf("sources = %v, want the union, most trusted first", b.Sources)
	}

	// Stale only when every member is.
	l49.Stale = true
	if out := bundleLinks([]MapLink{l49, l50}, noLAG); out[0].Stale {
		t.Error("stale with one fresh member")
	}
	l50.Stale = true
	if out := bundleLinks([]MapLink{l49, l50}, noLAG); !out[0].Stale {
		t.Error("not stale with every member stale")
	}
}

func TestBundleLagNames(t *testing.T) {
	l49 := bLink(bEnd(bundleA, 49), bEnd(bundleB, 49), "a", 10e9, "lldp")
	l50 := bLink(bEnd(bundleA, 50), bEnd(bundleB, 50), "a", 10e9, "lldp")
	lagOf := func(e MapEnd) (string, int) {
		switch {
		case e.DeviceID == bundleA:
			return "lag 1", 66
		case *e.IfIndex == 49:
			return "lag 1", 70
		default:
			return "lag 2", 71
		}
	}
	out := bundleLinks([]MapLink{l49, l50}, lagOf)
	if len(out) != 1 || out[0].Lag == nil || *out[0].Lag != (MapLag{A: "lag 1", B: ""}) {
		t.Fatalf("lag = %+v, want lag 1 at A only", out[0].Lag)
	}
}

func TestBundleLeavesOthersAlone(t *testing.T) {
	single := bLink(bEnd(bundleA, 1), bEnd(bundleC, 1), "a", 1e9, "lldp")
	guess := bLink(bEnd(bundleA, 2), bEnd(bundleB, 0), "a", 1e9, "guess")
	guess.Guess = true
	portless := bLink(bEnd(bundleA, 3), bEnd(bundleB, 0), "a", 1e9, "lldp")
	firm := bLink(bEnd(bundleA, 4), bEnd(bundleB, 4), "a", 1e9, "lldp")
	in := []MapLink{single, guess, portless, firm}
	if out := bundleLinks(in, noLAG); !reflect.DeepEqual(out, in) {
		t.Errorf("links = %+v, want them unchanged", out)
	}
}

func TestBundleOrderFollowsAPorts(t *testing.T) {
	l50 := bLink(bEnd(bundleA, 50), bEnd(bundleB, 2), "a", 1e9, "lldp")
	l49 := bLink(bEnd(bundleA, 49), bEnd(bundleB, 1), "a", 1e9, "lldp")
	single := bLink(bEnd(bundleA, 1), bEnd(bundleC, 1), "a", 1e9, "lldp")
	out := bundleLinks([]MapLink{single, l50, l49}, noLAG)
	if len(out) != 2 || out[0].Key != single.Key {
		t.Fatalf("links = %+v, want the single link then the bundle", out)
	}
	if b := out[1]; b.Key != "bundle:"+l49.Key+","+l50.Key || *b.A.IfIndex != 49 {
		t.Errorf("bundle %q from %v, want 49 first", b.Key, *b.A.IfIndex)
	}
}
