package snmp

import (
	"context"
	"errors"
	"testing"
)

func TestStatsOIDs(t *testing.T) {
	hc := StatsOIDs(7, true)
	if len(hc) != 10 || hc[0] != "1.3.6.1.2.1.31.1.1.1.6.7" || hc[1] != "1.3.6.1.2.1.31.1.1.1.10.7" || hc[9] != "1.3.6.1.2.1.31.1.1.1.15.7" {
		t.Errorf("hc oids %v", hc)
	}
	lo := StatsOIDs(7, false)
	if lo[0] != "1.3.6.1.2.1.2.2.1.10.7" || lo[1] != "1.3.6.1.2.1.2.2.1.16.7" || lo[9] != "1.3.6.1.2.1.2.2.1.5.7" {
		t.Errorf("32-bit oids %v", lo)
	}
}

func TestParseStats(t *testing.T) {
	x, e := "1.3.6.1.2.1.31.1.1.1", "1.3.6.1.2.1.2.2.1"
	pdus := []PDU{
		{OID: x + ".6.1", Value: uint64(1_000_000)}, {OID: x + ".10.1", Value: uint64(2_000_000)},
		{OID: e + ".14.1", Value: uint64(3)}, {OID: e + ".20.1", Value: uint64(4)},
		{OID: e + ".13.1", Value: uint64(5)}, {OID: e + ".19.1", Value: uint64(6)},
		{OID: e + ".7.1", Value: int64(1)}, {OID: e + ".8.1", Value: int64(1)},
		{OID: e + ".9.1", Value: uint64(12345)}, {OID: x + ".15.1", Value: uint64(1000)},
		// Port 2: no 64-bit counters (NoSuchInstance), link down.
		{OID: x + ".6.2", Value: nil}, {OID: x + ".10.2", Value: nil},
		{OID: e + ".7.2", Value: int64(1)}, {OID: e + ".8.2", Value: int64(2)},
	}
	got := ParseStats(pdus, true)
	p1 := got[1]
	if !p1.HaveOctets || !p1.HC || p1.InOctets != 1_000_000 || p1.OutOctets != 2_000_000 ||
		p1.InErrors != 3 || p1.OutErrors != 4 || p1.InDiscards != 5 || p1.OutDiscards != 6 ||
		!p1.HaveStatus || !p1.AdminUp || !p1.OperUp || p1.LastChangeSeconds != 123 || p1.SpeedBps != 1_000_000_000 {
		t.Errorf("port 1: %+v", p1)
	}
	p2 := got[2]
	if p2.HaveOctets || !p2.HaveStatus || p2.OperUp || !p2.AdminUp || p2.LastChangeSeconds != -1 {
		t.Errorf("port 2: %+v", p2)
	}

	lo := ParseStats([]PDU{
		{OID: e + ".10.3", Value: uint64(10)}, {OID: e + ".16.3", Value: uint64(20)},
		{OID: e + ".5.3", Value: uint64(100_000_000)}, {OID: e + ".8.3", Value: int64(1)},
	}, false)
	if p3 := lo[3]; !p3.HaveOctets || p3.HC || p3.SpeedBps != 100_000_000 || !p3.OperUp {
		t.Errorf("32-bit port 3: %+v", p3)
	}
}

type countingClient struct {
	gets   int
	perGet []int
	failOn int // 1-based GET number that fails; 0 = none
}

func (c *countingClient) Get(_ context.Context, _ Target, oids []string) ([]PDU, error) {
	c.gets++
	c.perGet = append(c.perGet, len(oids))
	if c.gets == c.failOn {
		return nil, errors.New("timeout")
	}
	var out []PDU
	for _, o := range oids {
		out = append(out, PDU{OID: o, Value: uint64(1)})
	}
	return out, nil
}

func (c *countingClient) Walk(context.Context, Target, string) ([]PDU, error) { return nil, nil }

func TestReadStatsBatchesAndKeepsPartialResults(t *testing.T) {
	idx := make([]int, 12)
	for i := range idx {
		idx[i] = i + 1
	}
	c := &countingClient{}
	got, err := ReadStats(context.Background(), c, Target{Credential: Credential{Version: "2c"}}, idx, true)
	if err != nil || len(got) != 12 || c.gets != 3 || c.perGet[0] != 50 {
		t.Errorf("v2c: gets %d per %v len %d err %v", c.gets, c.perGet, len(got), err)
	}

	c = &countingClient{}
	if _, err := ReadStats(context.Background(), c, Target{Credential: Credential{Version: "1"}}, idx, false); err != nil || c.gets != 6 {
		t.Errorf("v1 should use smaller requests: %d gets, err %v", c.gets, err)
	}

	c = &countingClient{failOn: 2}
	got, err = ReadStats(context.Background(), c, Target{Credential: Credential{Version: "2c"}}, idx, true)
	if err == nil || len(got) != 7 {
		t.Errorf("partial: len %d err %v (want 7 ports and the error)", len(got), err)
	}
	for i := 6; i <= 10; i++ {
		if _, ok := got[i]; ok {
			t.Errorf("port %d came from the failed request", i)
		}
	}
}
