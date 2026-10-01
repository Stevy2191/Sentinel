package snmp

import (
	"context"
	"errors"
	"testing"
)

// brokenUptime answers like the Tripp Lite card at .201: any GET that
// includes sysUpTime is refused with badValue; everything else answers.
type brokenUptime struct{ gets int }

func (b *brokenUptime) Get(_ context.Context, _ Target, oids []string) ([]PDU, error) {
	b.gets++
	vals := map[string]any{
		OIDSysDescr: []byte("PowerAlert 20.2.1"), OIDSysObjectID: "1.3.6.1.4.1.850.1.1.1",
		OIDSysContact: []byte("it@example.test"), OIDSysName: []byte("EXP-ARA-SMART1500"), OIDSysLocation: []byte("Arena"),
	}
	out := make([]PDU, 0, len(oids))
	for i, o := range oids {
		if o == OIDSysUpTime {
			return nil, &AgentError{Status: "BadValue", Index: i + 1}
		}
		out = append(out, PDU{OID: o, Value: vals[o]})
	}
	return out, nil
}
func (b *brokenUptime) Walk(context.Context, Target, string) ([]PDU, error) { return nil, nil }

// A refused value no longer fails identification: the rest are asked for
// one at a time and kept.
func TestIdentifySkipsARefusedValue(t *testing.T) {
	sys, err := Identify(context.Background(), &brokenUptime{}, Target{Credential: Credential{Version: "2c"}})
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if sys.Name != "EXP-ARA-SMART1500" || sys.ObjectID != "1.3.6.1.4.1.850.1.1.1" || sys.Location != "Arena" || sys.UptimeSeconds != 0 {
		t.Errorf("system %+v", sys)
	}
}

type refuseAll struct{}

func (refuseAll) Get(context.Context, Target, []string) ([]PDU, error) {
	return nil, &AgentError{Status: "GenErr"}
}
func (refuseAll) Walk(context.Context, Target, string) ([]PDU, error) { return nil, nil }

// If the agent refuses every value, identification still fails with its error.
func TestIdentifyAllRefusedIsAnError(t *testing.T) {
	_, err := Identify(context.Background(), refuseAll{}, Target{})
	var ae *AgentError
	if !errors.As(err, &ae) || err.Error() != "agent returned GenErr" {
		t.Fatalf("err %v", err)
	}
}
