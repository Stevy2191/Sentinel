package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func sp(s string) *string { return &s }

func TestNormalizeCredentialInput(t *testing.T) {
	cases := []struct {
		name     string
		in       CredentialInput
		existing *SNMPCredential
		wantErr  string // substring; "" = valid
	}{
		{"v2c with community", CredentialInput{Name: "Default", Version: "2c", Community: sp("public")}, nil, ""},
		{"v1 with community", CredentialInput{Name: "Radios", Version: "1", Community: sp("radios")}, nil, ""},
		{"v2c without community", CredentialInput{Name: "x", Version: "2c"}, nil, "community"},
		{"v2c blank community keeps existing", CredentialInput{Name: "x", Version: "2c", Community: sp("")},
			&SNMPCredential{Version: "2c", Community: "ciphertext"}, ""},
		{"v2c blank community with nothing stored", CredentialInput{Name: "x", Version: "2c", Community: sp("")},
			&SNMPCredential{Version: "3"}, "community"},
		{"unknown version", CredentialInput{Name: "x", Version: "4", Community: sp("c")}, nil, "version"},
		{"blank name", CredentialInput{Name: "  ", Version: "2c", Community: sp("c")}, nil, "name"},
		{"v3 noAuthNoPriv", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "none"}, nil, ""},
		{"v3 without username", CredentialInput{Name: "x", Version: "3", AuthProtocol: "none", PrivProtocol: "none"}, nil, "username"},
		{"v3 authNoPriv", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "none"}, nil, ""},
		{"v3 auth password too short", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("short"), PrivProtocol: "none"}, nil, "8 characters"},
		{"v3 auth without password", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA256", PrivProtocol: "none"}, nil, "auth password"},
		{"v3 authPriv", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "AES", PrivPassword: sp("alsolongenough")}, nil, ""},
		{"v3 priv without auth", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "AES", PrivPassword: sp("alsolongenough")}, nil, "without authentication"},
		{"v3 unknown auth protocol", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA1", AuthPassword: sp("longenough"), PrivProtocol: "none"}, nil, "auth protocol"},
		{"v3 unknown priv protocol", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "3DES", PrivPassword: sp("alsolongenough")}, nil, "privacy protocol"},
		{"v3 keeps stored auth password", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", PrivProtocol: "none"},
			&SNMPCredential{Version: "3", AuthPassword: "ciphertext"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeCredentialInput(tc.in, tc.existing)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// Fields that do not belong to the chosen version are cleared, so a profile
// switched from v3 to v2c does not keep a username it no longer uses.
func TestNormalizeCredentialInputClearsOtherVersionFields(t *testing.T) {
	out, err := NormalizeCredentialInput(CredentialInput{
		Name: " Default ", Version: "2c", Community: sp("public"),
		Username: "leftover", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "AES", PrivPassword: sp("x"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "Default" || out.Username != "" || out.AuthProtocol != "none" || out.PrivProtocol != "none" ||
		out.AuthPassword != nil || out.PrivPassword != nil {
		t.Errorf("v2c profile kept v3 fields: %+v", out)
	}

	out, err = NormalizeCredentialInput(CredentialInput{
		Name: "v3", Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "none", Community: sp("leftover"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Community != nil {
		t.Error("v3 profile kept a community")
	}
}

func TestNormalizeDeviceInput(t *testing.T) {
	ok := DeviceInput{Name: " core-sw-1 ", Host: " 10.20.0.2 "}
	out, err := NormalizeDeviceInput(ok)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "core-sw-1" || out.Host != "10.20.0.2" || out.Port != 161 || out.PollInterval != 60 ||
		out.TimeoutMs != 3000 || out.Retries != 1 || out.Enabled == nil || !*out.Enabled {
		t.Errorf("defaults not applied: %+v", out)
	}

	// Name may be empty: the device is named from sysName after its first
	// inventory. Host may not.
	if _, err := NormalizeDeviceInput(DeviceInput{Host: "10.0.0.1"}); err != nil {
		t.Errorf("empty name should be allowed: %v", err)
	}
	for name, in := range map[string]DeviceInput{
		"blank host":      {Host: "  "},
		"host with space": {Host: "10.0.0.1 extra"},
		"port 0 given":    {Host: "h", Port: -1},
		"port too high":   {Host: "h", Port: 70000},
		"interval low":    {Host: "h", PollInterval: 5},
		"interval high":   {Host: "h", PollInterval: 4000},
		"timeout low":     {Host: "h", TimeoutMs: 100},
		"retries high":    {Host: "h", Retries: 9},
	} {
		if _, err := NormalizeDeviceInput(in); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestPortPatch(t *testing.T) {
	var p PortPatch
	if err := json.Unmarshal([]byte(`{"important": true, "util_threshold_pct": null}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	u := p.Updates()
	if u["important"] != true || u["util_threshold_pct"] != nil || len(u) != 3 { // + updated_at
		t.Errorf("updates %v", u)
	}
	if _, ok := u["collect"]; ok {
		t.Error("absent field included")
	}
	for _, body := range []string{`{"util_threshold_pct": 5}`, `{"error_threshold_per_min": 0}`,
		`{"down_grace_seconds": 90000}`, `{"important": null}`, `{}`} {
		var bad PortPatch
		_ = json.Unmarshal([]byte(body), &bad)
		if bad.Validate() == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

func TestPortPatchRole(t *testing.T) {
	var p PortPatch
	if err := json.Unmarshal([]byte(`{"role":"wan"}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	u := p.Updates()
	if u["role"] != "wan" {
		t.Errorf("updates %v", u)
	}

	for _, body := range []string{`{"role":"toaster"}`, `{"role":null}`} {
		var bad PortPatch
		_ = json.Unmarshal([]byte(body), &bad)
		if bad.Validate() == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

func TestPortPatchNeighbor(t *testing.T) {
	id := uuid.New()
	body := `{"neighbor_device_id":"` + id.String() + `","neighbor_if_index":10}`
	var p PortPatch
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	u := p.Updates()
	if u["neighbor_device_id"] != id || u["neighbor_if_index"] != 10 {
		t.Errorf("updates %v", u)
	}

	// Clearing the neighbor (null) with no if_index is fine.
	var clear PortPatch
	if err := json.Unmarshal([]byte(`{"neighbor_device_id": null}`), &clear); err != nil {
		t.Fatal(err)
	}
	if err := clear.Validate(); err != nil {
		t.Errorf("clearing neighbor: %v", err)
	}
	if u := clear.Updates(); u["neighbor_device_id"] != nil {
		t.Errorf("clear updates %v", u)
	}

	for name, body := range map[string]string{
		"if_index 0":                `{"neighbor_device_id":"` + id.String() + `","neighbor_if_index":0}`,
		"if_index negative":         `{"neighbor_device_id":"` + id.String() + `","neighbor_if_index":-1}`,
		"if_index without a device": `{"neighbor_device_id":null,"neighbor_if_index":5}`,
		"access role with neighbor": `{"role":"access","neighbor_device_id":"` + id.String() + `"}`,
		"wan role with neighbor":    `{"role":"wan","neighbor_device_id":"` + id.String() + `"}`,
	} {
		var bad PortPatch
		if err := json.Unmarshal([]byte(body), &bad); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if bad.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDeviceDetailsPatch(t *testing.T) {
	var p DeviceDetailsPatch
	if err := json.Unmarshal([]byte(`{"model_override": "  UNVR (4-bay) ", "vendor_override": "", "device_type": "nvr",
		"faceplate_sfp_ports": [52, 49, 49], "faceplate_rows": null}`), &p); err != nil {
		t.Fatal(err)
	}
	u, err := p.Updates()
	if err != nil {
		t.Fatal(err)
	}
	if u["model_override"] != "UNVR (4-bay)" || u["vendor_override"] != nil || u["device_type"] != "nvr" || u["faceplate_rows"] != nil {
		t.Errorf("updates %v", u)
	}
	if sfp, ok := u["faceplate_sfp_ports"].(IntSlice); !ok || len(sfp) != 2 || sfp[0] != 49 || sfp[1] != 52 {
		t.Errorf("sfp %v", u["faceplate_sfp_ports"])
	}
	for _, body := range []string{`{"device_type": "toaster"}`, `{"faceplate_rows": 3}`, `{"faceplate_sfp_ports": [0]}`, `{}`} {
		var bad DeviceDetailsPatch
		_ = json.Unmarshal([]byte(body), &bad)
		if _, err := bad.Updates(); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

func TestDeviceDetailsPatchAcceptsUPS(t *testing.T) {
	ups := "ups"
	u, err := DeviceDetailsPatch{DeviceType: Opt[string]{Set: true, Value: &ups}}.Updates()
	if err != nil || u["device_type"] != "ups" {
		t.Fatalf("ups: %v %v", u, err)
	}
	toaster := "toaster"
	if _, err := (DeviceDetailsPatch{DeviceType: Opt[string]{Set: true, Value: &toaster}}).Updates(); err == nil {
		t.Error("unknown device type accepted")
	}
}

func TestDeviceDetailsPatchPortStyle(t *testing.T) {
	for body, want := range map[string]any{`{"faceplate_port_style": "sfp"}`: "sfp",
		`{"faceplate_port_style": "rj45"}`: "rj45", `{"faceplate_port_style": ""}`: nil, `{"faceplate_port_style": null}`: nil} {
		var p DeviceDetailsPatch
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		u, err := p.Updates()
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if v, ok := u["faceplate_port_style"]; !ok || v != want {
			t.Errorf("%s: got %v, want %v", body, v, want)
		}
	}
	var bad DeviceDetailsPatch
	_ = json.Unmarshal([]byte(`{"faceplate_port_style": "lc"}`), &bad)
	if _, err := bad.Updates(); err == nil {
		t.Error("unknown port style accepted")
	}
}

func TestDeviceDetailsPatchUPSThresholds(t *testing.T) {
	var p DeviceDetailsPatch
	if err := json.Unmarshal([]byte(`{"ups_low_battery_pct": 30, "ups_high_load_pct": null}`), &p); err != nil {
		t.Fatal(err)
	}
	u, err := p.Updates()
	if err != nil || u["ups_low_battery_pct"] != 30 || u["ups_high_load_pct"] != nil {
		t.Fatalf("updates %v %v", u, err)
	}
	if _, ok := u["ups_high_load_pct"]; !ok {
		t.Error("null did not clear the override")
	}
	for _, body := range []string{`{"ups_low_battery_pct": 4}`, `{"ups_low_battery_pct": 96}`, `{"ups_high_load_pct": 9}`, `{"ups_high_load_pct": 101}`} {
		var bad DeviceDetailsPatch
		_ = json.Unmarshal([]byte(body), &bad)
		if _, err := bad.Updates(); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}
