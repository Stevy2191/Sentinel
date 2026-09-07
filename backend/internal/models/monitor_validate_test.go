package models

import "testing"

// Every supported type must be creatable. A regression here rejected DNS, ping
// and TCP monitors outright while the error message still listed them as valid.
func TestMonitorValidateAcceptsEverySupportedType(t *testing.T) {
	cases := []struct {
		typ string
		url string
	}{
		{MonitorTypeHTTP, "https://example.com"},
		{MonitorTypeWebhook, "https://example.com/hook"},
		{MonitorTypeTCP, "example.com:5432"},
		{MonitorTypePing, "1.1.1.1"},
		{MonitorTypeDNS, "example.com"},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			m := &Monitor{
				Name: "probe", Type: c.typ, URL: c.url,
				IntervalSeconds: 60, TimeoutSeconds: 10,
			}
			if err := m.Validate(); err != nil {
				t.Errorf("type %q should be valid, got: %v", c.typ, err)
			}
		})
	}
}

func TestMonitorValidateRejectsUnknownType(t *testing.T) {
	m := &Monitor{Name: "x", Type: "smtp", URL: "example.com", IntervalSeconds: 60, TimeoutSeconds: 10}
	if err := m.Validate(); err == nil {
		t.Error("an unsupported type should be rejected")
	}
}

// Only the URL-shaped types get URL validation; host[:port] targets must not.
func TestMonitorValidateURLCheckAppliesOnlyToHTTPTypes(t *testing.T) {
	http := &Monitor{Name: "x", Type: MonitorTypeHTTP, URL: "example.com:5432", IntervalSeconds: 60, TimeoutSeconds: 10}
	if err := http.Validate(); err == nil {
		t.Error("an http monitor with a non-URL target should be rejected")
	}
	tcp := &Monitor{Name: "x", Type: MonitorTypeTCP, URL: "example.com:5432", IntervalSeconds: 60, TimeoutSeconds: 10}
	if err := tcp.Validate(); err != nil {
		t.Errorf("a tcp monitor with a host:port target should be accepted, got: %v", err)
	}
}
