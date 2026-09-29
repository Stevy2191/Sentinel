package models

import (
	"strings"
	"testing"
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
