package netreport

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// allowedOn is the one rule create, the preview and the run share: a port
// metric on any scope, a device metric only where there are devices, and
// never a text metric or link speed.
func TestAllowedOn(t *testing.T) {
	builtin := func(key string) metricInfo {
		t.Helper()
		m, ok := builtinInfo(key)
		if !ok {
			t.Fatalf("%s is not in the catalogue", key)
		}
		return m
	}
	traffic := builtin(services.MetricIfInBps)
	onBattery := builtin(services.MetricUPSOnBattery)
	batteryStatus := builtin(services.MetricUPSBatteryStatus)
	speed := builtin(services.MetricIfSpeedBps)
	cpu := metricInfo{Key: "core_cpu", Label: "CPU", Unit: "%", Source: sourceProfile, Kind: models.MetricKindGauge}
	fan := metricInfo{Key: "core_fan", Label: "Fan", Source: sourceProfile, Kind: models.MetricKindStatus}

	scopes := []string{models.ScopeTypePorts, models.ScopeTypePortRoles, models.ScopeTypeDevices, models.ScopeTypeSites}
	cases := []struct {
		name string
		m    metricInfo
		want []bool // per scope, in the order of scopes
	}{
		{"a port metric", traffic, []bool{true, true, true, true}},
		{"a built-in device metric", onBattery, []bool{false, false, true, true}},
		{"a profile device metric", cpu, []bool{false, false, true, true}},
		{"an enum", batteryStatus, []bool{false, false, false, false}},
		{"a status metric", fan, []bool{false, false, false, false}},
		{"link speed", speed, []bool{false, false, false, false}},
	}
	for _, c := range cases {
		for i, s := range scopes {
			if got := c.m.allowedOn(s); got != c.want[i] {
				t.Errorf("%s on %s: allowedOn = %v, want %v", c.name, s, got, c.want[i])
			}
		}
	}
}
