package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// fakeIdentifier answers for (host, community) pairs; everything else times out.
type fakeIdentifier struct {
	mu      sync.Mutex
	answers map[string]string // host -> community that works
	blocked map[string]bool
	tries   map[string]int
}

func (f *fakeIdentifier) IdentifyWith(_ context.Context, host string, _ int, cred snmp.Credential, _ time.Duration, _ int) (snmp.System, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tries == nil {
		f.tries = map[string]int{}
	}
	f.tries[host]++
	if f.blocked[host] {
		return snmp.System{}, ErrTargetBlocked
	}
	if f.answers[host] == cred.Community {
		return snmp.System{Name: "sw-" + host, Descr: "EdgeSwitch", ObjectID: "1.3.6.1.4.1.4413"}, nil
	}
	return snmp.System{}, errors.New("request timeout")
}

func waitDone(t *testing.T, m *ScanManager, site, job uuid.UUID) ScanJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := m.Get(site, job)
		if err != nil {
			t.Fatal(err)
		}
		if !j.Running {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan did not finish")
	return ScanJob{}
}

func TestScanFindsDevicesWithFirstWorkingProfile(t *testing.T) {
	id := &fakeIdentifier{answers: map[string]string{"10.0.0.2": "public", "10.0.0.3": "private"}}
	m := NewScanManager(id)
	site := uuid.New()
	pub := ScanCredential{ID: uuid.New(), Name: "Public", Cred: snmp.Credential{Version: "2c", Community: "public"}}
	priv := ScanCredential{ID: uuid.New(), Name: "Private", Cred: snmp.Credential{Version: "2c", Community: "private"}}

	job, err := m.Start(site, "10.0.0.0/29", []ScanCredential{pub, priv}, map[string]bool{"10.0.0.2:161": true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, m, site, job.ID)
	if done.Total != 6 || done.Done != 6 || len(done.Results) != 2 || done.FinishedAt == nil {
		t.Fatalf("job %+v", done)
	}
	byHost := map[string]ScanResult{}
	for _, r := range done.Results {
		byHost[r.Host] = r
	}
	if r := byHost["10.0.0.2"]; r.CredentialID != pub.ID || !r.AlreadyAdded || r.Vendor != "Ubiquiti (EdgeSwitch)" || r.Name != "sw-10.0.0.2" {
		t.Errorf("10.0.0.2: %+v", r)
	}
	if r := byHost["10.0.0.3"]; r.CredentialID != priv.ID || r.AlreadyAdded {
		t.Errorf("10.0.0.3: %+v", r)
	}
	// .2 answered the first profile, so the second was never tried.
	if id.tries["10.0.0.2"] != 1 || id.tries["10.0.0.4"] != 2 {
		t.Errorf("tries %v", id.tries)
	}
}

func TestScanStopsTryingBlockedAddresses(t *testing.T) {
	id := &fakeIdentifier{blocked: map[string]bool{"10.0.0.1": true}}
	m := NewScanManager(id)
	site := uuid.New()
	creds := []ScanCredential{{ID: uuid.New(), Cred: snmp.Credential{Community: "a"}}, {ID: uuid.New(), Cred: snmp.Credential{Community: "b"}}}
	job, _ := m.Start(site, "10.0.0.1/32", creds, nil)
	waitDone(t, m, site, job.ID)
	if id.tries["10.0.0.1"] != 1 {
		t.Errorf("blocked address tried %d times, want 1", id.tries["10.0.0.1"])
	}
}

func TestScanLimits(t *testing.T) {
	m := NewScanManager(&fakeIdentifier{})
	site := uuid.New()
	creds := []ScanCredential{{ID: uuid.New()}}
	if _, err := m.Start(site, "10.0.0.0/21", creds, nil); !errors.Is(err, ErrScanTooLarge) {
		t.Errorf("/21: %v, want ErrScanTooLarge", err)
	}
	if _, err := m.Start(site, "not-a-cidr", creds, nil); err == nil {
		t.Error("bad CIDR accepted")
	}
	if _, err := m.Start(site, "10.0.0.0/30", nil, nil); !errors.Is(err, ErrScanNoCredentials) {
		t.Errorf("no credentials: %v", err)
	}

	// One scan per site at a time.
	slow := &fakeIdentifier{}
	m = NewScanManager(slow)
	m.perHost = 0
	m.hold = make(chan struct{})
	if _, err := m.Start(site, "10.0.0.0/30", creds, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(site, "10.0.0.0/30", creds, nil); !errors.Is(err, ErrScanRunning) {
		t.Errorf("second scan: %v, want ErrScanRunning", err)
	}
	if _, err := m.Start(uuid.New(), "10.0.0.0/30", creds, nil); err != nil {
		t.Errorf("another site's scan was blocked: %v", err)
	}
	close(m.hold)
}

func TestScanJobsExpireAndStaySiteScoped(t *testing.T) {
	m := NewScanManager(&fakeIdentifier{})
	site := uuid.New()
	job, _ := m.Start(site, "10.0.0.1/32", []ScanCredential{{ID: uuid.New()}}, nil)
	waitDone(t, m, site, job.ID)

	if _, err := m.Get(uuid.New(), job.ID); !errors.Is(err, ErrScanNotFound) {
		t.Errorf("another site read the job: %v", err)
	}
	later := time.Now().Add(61 * time.Minute)
	m.now = func() time.Time { return later }
	if _, err := m.Get(site, job.ID); !errors.Is(err, ErrScanNotFound) {
		t.Errorf("job still there after an hour: %v", err)
	}
}
