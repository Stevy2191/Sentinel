package snmp

import (
	"context"

	"github.com/Stevy2191/Sentinel/backend/internal/upsmon"
)

const oidUPSMIB = "1.3.6.1.2.1.33.1"

// UPSReadingOIDs are the UPS-MIB (RFC 1628) values a UPS poll reads, line
// tables at index 1 only (single-phase).
var UPSReadingOIDs = []string{
	oidUPSMIB + ".2.1.0",     // upsBatteryStatus
	oidUPSMIB + ".2.2.0",     // upsSecondsOnBattery
	oidUPSMIB + ".2.3.0",     // upsEstimatedMinutesRemaining
	oidUPSMIB + ".2.4.0",     // upsEstimatedChargeRemaining (%)
	oidUPSMIB + ".2.7.0",     // upsBatteryTemperature (°C, may be negative)
	oidUPSMIB + ".3.3.1.3.1", // upsInputVoltage, line 1 (V RMS)
	oidUPSMIB + ".4.1.0",     // upsOutputSource
	oidUPSMIB + ".4.4.1.2.1", // upsOutputVoltage, line 1 (V RMS)
	oidUPSMIB + ".4.4.1.5.1", // upsOutputPercentLoad, line 1 (%)
}

// signed returns a numeric value of either sign; ok is false for a missing
// (noSuchObject) or non-numeric value.
func signed(p PDU) (float64, bool) {
	switch v := p.Value.(type) {
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	}
	return 0, false
}

// ParseUPSReadings turns a GET of UPSReadingOIDs into Readings; anything the
// agent did not answer stays absent.
func ParseUPSReadings(pdus []PDU) upsmon.Readings {
	var r upsmon.Readings
	ptr := func(v float64) *float64 { return &v }
	for _, p := range pdus {
		v, ok := signed(p)
		if !ok {
			continue
		}
		switch p.OID {
		case oidUPSMIB + ".2.1.0":
			r.BatteryStatus = int(v)
		case oidUPSMIB + ".2.2.0":
			s := int64(v)
			r.SecondsOnBattery = &s
		case oidUPSMIB + ".2.3.0":
			r.RuntimeMin = ptr(v)
		case oidUPSMIB + ".2.4.0":
			r.ChargePct = ptr(v)
		case oidUPSMIB + ".2.7.0":
			r.BatteryTempC = ptr(v)
		case oidUPSMIB + ".3.3.1.3.1":
			r.InputV = ptr(v)
		case oidUPSMIB + ".4.1.0":
			r.OutputSource = int(v)
		case oidUPSMIB + ".4.4.1.2.1":
			r.OutputV = ptr(v)
		case oidUPSMIB + ".4.4.1.5.1":
			r.LoadPct = ptr(v)
		}
	}
	return r
}

// ReadUPS reads a UPS's readings in one GET. SNMPv1 fails a whole GET when
// any one OID is missing, so on v1 a failed GET is retried one OID at a time
// and whatever answers is kept.
func ReadUPS(ctx context.Context, c Client, t Target) (upsmon.Readings, error) {
	pdus, err := c.Get(ctx, t, UPSReadingOIDs)
	if err == nil {
		return ParseUPSReadings(pdus), nil
	}
	if t.Credential.Version != "1" { // SNMPv1, as stats.go spells it
		return upsmon.Readings{}, err
	}
	var all []PDU
	for _, o := range UPSReadingOIDs {
		if p, err := c.Get(ctx, t, []string{o}); err == nil {
			all = append(all, p...)
		}
	}
	return ParseUPSReadings(all), nil
}
