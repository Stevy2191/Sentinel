package nettools

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParsePorts(t *testing.T) {
	cases := []struct {
		in      string
		want    []int
		wantErr string // substring; "" = no error
	}{
		{"22", []int{22}, ""},
		{"80,80,22", []int{22, 80}, ""},
		{" 443 , 22 , 8000-8003 ", []int{22, 443, 8000, 8001, 8002, 8003}, ""},
		{"22,,80,", []int{22, 80}, ""},
		{"5-5", []int{5}, ""},
		{"1-1024", nil, ""}, // checked by length below
		{"1-1025", nil, "at most 1024 ports"},
		{"1-1000,2000-2025", nil, "at most 1024 ports"},
		{"1-65535", nil, "at most 1024 ports"},
		{"0", nil, `"0" is not a port number (1-65535)`},
		{"65536", nil, `"65536" is not a port number (1-65535)`},
		{"10-5", nil, `"10-5": the range starts above its end`},
		{"ssh", nil, "is not a port number"},
		{"22-", nil, "is not a port number"},
		{"", nil, "no ports given"},
		{" , ", nil, "no ports given"},
	}
	for _, c := range cases {
		got, err := ParsePorts(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("ParsePorts(%q) error %v, want %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePorts(%q): %v", c.in, err)
			continue
		}
		if c.want != nil && !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParsePorts(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if got, _ := ParsePorts("1-1024"); len(got) != 1024 || got[0] != 1 || got[1023] != 1024 {
		t.Errorf("1-1024: %d ports", len(got))
	}
}

func TestCommonPorts(t *testing.T) {
	if len(CommonPorts) != 100 {
		t.Errorf("%d common ports, want 100", len(CommonPorts))
	}
	if !sort.IntsAreSorted(CommonPorts) {
		t.Error("CommonPorts is not ascending")
	}
	for i := 1; i < len(CommonPorts); i++ {
		if CommonPorts[i] == CommonPorts[i-1] {
			t.Errorf("port %d listed twice", CommonPorts[i])
		}
	}
	got, err := ParsePorts(" Common ")
	if err != nil || !reflect.DeepEqual(got, CommonPorts) {
		t.Errorf("ParsePorts(common) = %d ports, %v", len(got), err)
	}
	got[0] = 9999 // the preset is a copy
	if CommonPorts[0] != 7 {
		t.Error("ParsePorts returned the preset itself")
	}
}

func TestServiceName(t *testing.T) {
	for port, want := range map[int]string{22: "ssh", 3389: "rdp", 443: "https", 5985: "winrm", 27017: "mongodb", 12345: ""} {
		if got := ServiceName(port); got != want {
			t.Errorf("ServiceName(%d) = %q, want %q", port, got, want)
		}
	}
}
