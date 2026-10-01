package services

// Built-in metric keys. Phase 3 adds user-defined metrics beside these; the
// tables never need to change for it.
const (
	MetricIfInBps         = "if_in_bps"
	MetricIfOutBps        = "if_out_bps"
	MetricIfInUtilPct     = "if_in_util_pct"
	MetricIfOutUtilPct    = "if_out_util_pct"
	MetricIfInErrorsPM    = "if_in_errors_pm"
	MetricIfOutErrorsPM   = "if_out_errors_pm"
	MetricIfInDiscardsPM  = "if_in_discards_pm"
	MetricIfOutDiscardsPM = "if_out_discards_pm"
	MetricIfSpeedBps      = "if_speed_bps"

	MetricUPSChargePct     = "ups_charge_pct"
	MetricUPSRuntimeMin    = "ups_runtime_min"
	MetricUPSLoadPct       = "ups_load_pct"
	MetricUPSInputV        = "ups_input_v"
	MetricUPSOutputV       = "ups_output_v"
	MetricUPSBatteryTempC  = "ups_battery_temp_c"
	MetricUPSOnBattery     = "ups_on_battery"
	MetricUPSBatteryStatus = "ups_battery_status"
)

// MetricDef describes one metric for the API and the UI.
type MetricDef struct {
	Key   string `json:"key"`
	Unit  string `json:"unit"`
	Label string `json:"label"`
}

var MetricCatalogue = []MetricDef{
	{MetricIfInBps, "bps", "Traffic in"},
	{MetricIfOutBps, "bps", "Traffic out"},
	{MetricIfInUtilPct, "%", "Busy in"},
	{MetricIfOutUtilPct, "%", "Busy out"},
	{MetricIfInErrorsPM, "per_min", "Errors in"},
	{MetricIfOutErrorsPM, "per_min", "Errors out"},
	{MetricIfInDiscardsPM, "per_min", "Discards in"},
	{MetricIfOutDiscardsPM, "per_min", "Discards out"},
	{MetricIfSpeedBps, "bps", "Link speed"},
	{MetricUPSChargePct, "%", "Battery charge"},
	{MetricUPSRuntimeMin, "min", "Runtime left"},
	{MetricUPSLoadPct, "%", "Load"},
	{MetricUPSInputV, "V", "Input voltage"},
	{MetricUPSOutputV, "V", "Output voltage"},
	{MetricUPSBatteryTempC, "°C", "Battery temperature"},
	{MetricUPSOnBattery, "bool", "On battery"},
	{MetricUPSBatteryStatus, "enum", "Battery status"},
}

// UPSMetrics are the keys a UPS poll writes (instance "", no interface).
var UPSMetrics = []string{MetricUPSChargePct, MetricUPSRuntimeMin, MetricUPSLoadPct, MetricUPSInputV,
	MetricUPSOutputV, MetricUPSBatteryTempC, MetricUPSOnBattery, MetricUPSBatteryStatus}

var knownMetrics = func() map[string]bool {
	m := make(map[string]bool, len(MetricCatalogue))
	for _, d := range MetricCatalogue {
		m[d.Key] = true
	}
	return m
}()

// KnownMetric reports whether key is in the catalogue.
func KnownMetric(key string) bool { return knownMetrics[key] }
