package snmp

import (
	"context"
	"fmt"
)

// IF-MIB columns the stats poll reads.
const (
	oidIfSpeed       = "1.3.6.1.2.1.2.2.1.5"
	oidIfAdminStatus = "1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = "1.3.6.1.2.1.2.2.1.8"
	oidIfLastChange  = "1.3.6.1.2.1.2.2.1.9"
	oidIfInOctets    = "1.3.6.1.2.1.2.2.1.10"
	oidIfInDiscards  = "1.3.6.1.2.1.2.2.1.13"
	oidIfInErrors    = "1.3.6.1.2.1.2.2.1.14"
	oidIfOutOctets   = "1.3.6.1.2.1.2.2.1.16"
	oidIfOutDiscards = "1.3.6.1.2.1.2.2.1.19"
	oidIfOutErrors   = "1.3.6.1.2.1.2.2.1.20"
	oidIfHCInOctets  = "1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCOutOctets = "1.3.6.1.2.1.31.1.1.1.10"
	oidIfHighSpeed   = "1.3.6.1.2.1.31.1.1.1.15"
)

// IfStats is one interface's counters and state from one stats poll. A value
// the agent did not return leaves its Have* flag false (or LastChangeSeconds
// -1, SpeedBps 0).
type IfStats struct {
	Index                   int
	HC                      bool // octets are the 64-bit ifHC* counters
	HaveOctets              bool // both octet counters were returned
	InOctets, OutOctets     uint64
	InErrors, OutErrors     uint64
	InDiscards, OutDiscards uint64
	HaveStatus              bool // ifOperStatus was returned
	AdminUp, OperUp         bool
	LastChangeSeconds       int64
	SpeedBps                int64
}

// StatsOIDs are the ten OIDs read for one interface: octets (64-bit when hc),
// errors, discards, admin and oper status, last change, speed (ifHighSpeed
// when hc, else ifSpeed).
func StatsOIDs(index int, hc bool) []string {
	in, out, speed := oidIfInOctets, oidIfOutOctets, oidIfSpeed
	if hc {
		in, out, speed = oidIfHCInOctets, oidIfHCOutOctets, oidIfHighSpeed
	}
	cols := []string{in, out, oidIfInErrors, oidIfOutErrors, oidIfInDiscards, oidIfOutDiscards,
		oidIfAdminStatus, oidIfOperStatus, oidIfLastChange, speed}
	oids := make([]string, len(cols))
	for i, c := range cols {
		oids[i] = fmt.Sprintf("%s.%d", c, index)
	}
	return oids
}

// ParseStats reads the answers to StatsOIDs requests, by ifIndex.
func ParseStats(pdus []PDU, hc bool) map[int]IfStats {
	type acc struct {
		st      IfStats
		in, out bool
	}
	byIndex := map[int]*acc{}
	get := func(i int) *acc {
		if byIndex[i] == nil {
			byIndex[i] = &acc{st: IfStats{Index: i, HC: hc, LastChangeSeconds: -1}}
		}
		return byIndex[i]
	}
	for _, p := range pdus {
		n, ok := p.Number()
		if col, idx, found := splitColumn(p.OID, oidIfEntry); found {
			a := get(idx)
			if !ok {
				continue
			}
			switch col {
			case 5:
				if !hc {
					a.st.SpeedBps = int64(n)
				}
			case 7:
				a.st.AdminUp = n == 1
			case 8:
				a.st.OperUp, a.st.HaveStatus = n == 1, true
			case 9:
				a.st.LastChangeSeconds = int64(n / 100)
			case 10:
				if !hc {
					a.st.InOctets, a.in = n, true
				}
			case 13:
				a.st.InDiscards = n
			case 14:
				a.st.InErrors = n
			case 16:
				if !hc {
					a.st.OutOctets, a.out = n, true
				}
			case 19:
				a.st.OutDiscards = n
			case 20:
				a.st.OutErrors = n
			}
			continue
		}
		if col, idx, found := splitColumn(p.OID, oidIfXEntry); found {
			a := get(idx)
			if !ok {
				continue
			}
			switch col {
			case 6:
				if hc {
					a.st.InOctets, a.in = n, true
				}
			case 10:
				if hc {
					a.st.OutOctets, a.out = n, true
				}
			case 15:
				if hc {
					a.st.SpeedBps = int64(n) * 1_000_000
				}
			}
		}
	}
	out := make(map[int]IfStats, len(byIndex))
	for idx, a := range byIndex {
		a.st.HaveOctets = a.in && a.out
		out[idx] = a.st
	}
	return out
}

// interfacesPerGet keeps each request at 50 varbinds (gosnmp caps a request
// at 60), and at 20 for SNMPv1 agents, which are often small radios that
// answer a large request with tooBig.
func interfacesPerGet(version string) int {
	if version == "1" {
		return 2
	}
	return 5
}

// ReadStats reads the given interfaces in batched GETs. A failed batch does
// not stop the rest: the result holds every interface that answered, and the
// first error is returned alongside so the caller can log it.
func ReadStats(ctx context.Context, c Client, t Target, indexes []int, hc bool) (map[int]IfStats, error) {
	per := interfacesPerGet(t.Credential.Version)
	out := make(map[int]IfStats, len(indexes))
	var firstErr error
	for i := 0; i < len(indexes); i += per {
		batch := indexes[i:min(i+per, len(indexes))]
		oids := make([]string, 0, len(batch)*10)
		for _, idx := range batch {
			oids = append(oids, StatsOIDs(idx, hc)...)
		}
		pdus, err := c.Get(ctx, t, oids)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("reading interface counters: %w", err)
			}
			if ctx.Err() != nil {
				break
			}
			continue
		}
		for idx, st := range ParseStats(pdus, hc) {
			out[idx] = st
		}
	}
	return out, firstErr
}
