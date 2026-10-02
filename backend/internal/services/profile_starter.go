package services

import "github.com/Stevy2191/Sentinel/backend/internal/models"

// Cisco OIDs (from the public Cisco MIBs, verified with gosmi while
// planning): CISCO-PROCESS-MIB, CISCO-ENHANCED-MEMPOOL-MIB,
// CISCO-MEMORY-POOL-MIB, CISCO-ENVMON-MIB, CISCO-ENTITY-SENSOR-MIB,
// CISCO-ENTITY-FRU-CONTROL-MIB; entPhysicalName from ENTITY-MIB.
const (
	oidEntPhysicalName      = "1.3.6.1.2.1.47.1.1.1.1.7"
	oidCpmCPUTotalPhysIndex = "1.3.6.1.4.1.9.9.109.1.1.1.1.2"
	oidCpmCPUTotal5minRev   = "1.3.6.1.4.1.9.9.109.1.1.1.1.8"
	oidCempMemPoolName      = "1.3.6.1.4.1.9.9.221.1.1.1.1.3"
	oidCempMemPoolHCUsed    = "1.3.6.1.4.1.9.9.221.1.1.1.1.18"
	oidCempMemPoolHCFree    = "1.3.6.1.4.1.9.9.221.1.1.1.1.20"
	oidCiscoMemPoolName     = "1.3.6.1.4.1.9.9.48.1.1.1.2"
	oidCiscoMemPoolUsed     = "1.3.6.1.4.1.9.9.48.1.1.1.5"
	oidCiscoMemPoolFree     = "1.3.6.1.4.1.9.9.48.1.1.1.6"
	oidEnvTempDescr         = "1.3.6.1.4.1.9.9.13.1.3.1.2"
	oidEnvTempValue         = "1.3.6.1.4.1.9.9.13.1.3.1.3"
	oidEnvFanDescr          = "1.3.6.1.4.1.9.9.13.1.4.1.2"
	oidEnvFanState          = "1.3.6.1.4.1.9.9.13.1.4.1.3"
	oidEnvSupplyDescr       = "1.3.6.1.4.1.9.9.13.1.5.1.2"
	oidEnvSupplyState       = "1.3.6.1.4.1.9.9.13.1.5.1.3"
	oidEntSensorType        = "1.3.6.1.4.1.9.9.91.1.1.1.1.1"
	oidEntSensorPrecision   = "1.3.6.1.4.1.9.9.91.1.1.1.1.3"
	oidEntSensorValue       = "1.3.6.1.4.1.9.9.91.1.1.1.1.4"
	oidCefcFanTrayOper      = "1.3.6.1.4.1.9.9.117.1.4.1.1.1"
	oidCefcFRUPowerOper     = "1.3.6.1.4.1.9.9.117.1.1.2.1.2"
)

var envMonStates = models.EnumMap{1: "normal", 2: "warning", 3: "critical", 4: "shutdown", 5: "notPresent", 6: "notFunctioning"}

// ruleValue is a convenience for the *float64 a RuleValue needs.
func ruleValue(v float64) *float64 { return &v }

// starterProfile is the built-in "Cisco switch health" profile. ENVMON's
// notPresent counts as OK: an empty redundant power-supply or fan slot
// reports it, and alerting on it would page for nothing.
func starterProfile() (models.MetricProfile, []models.ProfileMetric) {
	p := models.MetricProfile{Name: "Cisco switch health", Builtin: true, PollIntervalMinutes: 1,
		Description:   "CPU, memory, temperature, fans and power supplies on Cisco switches. Tables a model does not have are skipped.",
		MatchPrefixes: models.StringArray{"1.3.6.1.4.1.9.1"}}
	m := []models.ProfileMetric{
		{Name: "CPU busy (5 min)", Key: "cisco_cpu_5min", Source: "column", Kind: "gauge", Units: "%", Scale: 1,
			OID: oidCpmCPUTotal5minRev, LabelMode: "pointer", LabelPointerOID: oidCpmCPUTotalPhysIndex, LabelTargetOID: oidEntPhysicalName,
			RuleKind: "above", RuleValue: ruleValue(90), RuleHoldMinutes: 10, RuleEnabled: true},
		{Name: "Memory used", Key: "cisco_mem_used_pct", Source: "used_free_pct", Kind: "gauge", Units: "%", Scale: 1,
			OID: oidCempMemPoolHCUsed, OID2: oidCempMemPoolHCFree, LabelMode: "column", LabelOID: oidCempMemPoolName,
			RuleKind: "above", RuleValue: ruleValue(90), RuleHoldMinutes: 15, RuleEnabled: true},
		{Name: "Memory used (classic)", Key: "cisco_mem_pool_used_pct", Source: "used_free_pct", Kind: "gauge", Units: "%", Scale: 1,
			OID: oidCiscoMemPoolUsed, OID2: oidCiscoMemPoolFree, LabelMode: "column", LabelOID: oidCiscoMemPoolName,
			RuleKind: "above", RuleValue: ruleValue(90), RuleHoldMinutes: 15, RuleEnabled: true},
		{Name: "Temperature", Key: "cisco_temp_envmon", Source: "column", Kind: "gauge", Units: "°C", Scale: 1,
			OID: oidEnvTempValue, LabelMode: "column", LabelOID: oidEnvTempDescr,
			RuleKind: "above", RuleValue: ruleValue(70), RuleHoldMinutes: 5, RuleEnabled: true},
		{Name: "Temperature (sensors)", Key: "cisco_temp_sensor", Source: "column", Kind: "gauge", Units: "°C", Scale: 1,
			OID: oidEntSensorValue, PrecisionOID: oidEntSensorPrecision, FilterOID: oidEntSensorType, FilterValues: models.StringArray{"8"},
			LabelMode: "same_index", LabelOID: oidEntPhysicalName,
			RuleKind: "above", RuleValue: ruleValue(70), RuleHoldMinutes: 5, RuleEnabled: true},
		{Name: "Fan", Key: "cisco_fan_envmon", Source: "column", Kind: "status", Scale: 1,
			OID: oidEnvFanState, LabelMode: "column", LabelOID: oidEnvFanDescr,
			OKStates: models.Int64Array{1, 5}, StateNames: envMonStates, RuleKind: "not_ok", RuleEnabled: true},
		{Name: "Fan tray", Key: "cisco_fan_fru", Source: "column", Kind: "status", Scale: 1,
			OID: oidCefcFanTrayOper, LabelMode: "same_index", LabelOID: oidEntPhysicalName,
			OKStates: models.Int64Array{2}, StateNames: models.EnumMap{1: "unknown", 2: "up", 3: "down", 4: "warning"},
			RuleKind: "not_ok", RuleEnabled: true},
		{Name: "Power supply", Key: "cisco_psu_envmon", Source: "column", Kind: "status", Scale: 1,
			OID: oidEnvSupplyState, LabelMode: "column", LabelOID: oidEnvSupplyDescr,
			OKStates: models.Int64Array{1, 5}, StateNames: envMonStates, RuleKind: "not_ok", RuleEnabled: true},
		{Name: "Power supply (FRU)", Key: "cisco_psu_fru", Source: "column", Kind: "status", Scale: 1,
			OID: oidCefcFRUPowerOper, LabelMode: "same_index", LabelOID: oidEntPhysicalName,
			OKStates: models.Int64Array{2}, StateNames: models.EnumMap{1: "offEnvOther", 2: "on", 3: "offAdmin", 4: "offDenied",
				5: "offEnvPower", 6: "offEnvTemp", 7: "offEnvFan", 8: "failed", 9: "onButFanFail", 10: "offCooling",
				11: "offConnectorRating", 12: "onButInlinePowerFail"},
			RuleKind: "not_ok", RuleEnabled: true},
	}
	for i := range m {
		m[i].Position = i
	}
	return p, m
}
