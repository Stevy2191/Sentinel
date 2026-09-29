package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

var (
	ErrScanRunning       = errors.New("a scan is already running for this site")
	ErrScanTooLarge      = fmt.Errorf("subnet too large to scan (at most %d addresses, a /22)", maxDiscoveryHosts)
	ErrScanNotFound      = errors.New("scan not found (scans are kept for an hour after they finish)")
	ErrScanNoCredentials = errors.New("choose at least one credential profile to scan with")
)

const (
	scanConcurrency = 64
	scanPerHost     = time.Second
	scanJobTTL      = time.Hour
	scanMaxDuration = 15 * time.Minute
)

// ScanCredential is a decrypted profile to try, with its identity.
type ScanCredential struct {
	ID   uuid.UUID
	Name string
	Cred snmp.Credential
}

// ScanResult is one address that answered.
type ScanResult struct {
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Name           string    `json:"name"`
	Descr          string    `json:"descr"`
	ObjectID       string    `json:"object_id"`
	Vendor         string    `json:"vendor"`
	CredentialID   uuid.UUID `json:"credential_id"`
	CredentialName string    `json:"credential_name"`
	AlreadyAdded   bool      `json:"already_added"`
}

// ScanJob is a scan's progress and results so far.
type ScanJob struct {
	ID         uuid.UUID    `json:"id"`
	SiteID     uuid.UUID    `json:"site_id"`
	CIDR       string       `json:"cidr"`
	Total      int          `json:"total"`
	Done       int          `json:"done"`
	Running    bool         `json:"running"`
	Results    []ScanResult `json:"results"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at"`
}

type scanIdentifier interface {
	IdentifyWith(ctx context.Context, host string, port int, cred snmp.Credential, timeout time.Duration, retries int) (snmp.System, error)
}

// ScanManager runs subnet scans as in-memory background jobs: one per site at
// a time, kept for an hour after finishing. A restart loses them.
type ScanManager struct {
	mu      sync.Mutex
	jobs    map[uuid.UUID]*ScanJob
	running map[uuid.UUID]uuid.UUID // site -> job
	id      scanIdentifier
	now     func() time.Time
	perHost time.Duration
	hold    chan struct{} // tests: blocks workers until closed
}

func NewScanManager(id scanIdentifier) *ScanManager {
	return &ScanManager{jobs: map[uuid.UUID]*ScanJob{}, running: map[uuid.UUID]uuid.UUID{},
		id: id, now: time.Now, perHost: scanPerHost}
}

// Start begins a scan and returns its initial state.
func (m *ScanManager) Start(siteID uuid.UUID, cidr string, creds []ScanCredential, existing map[string]bool) (ScanJob, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ScanJob{}, fmt.Errorf("%q is not a subnet in CIDR form, e.g. 10.20.0.0/24", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return ScanJob{}, errors.New("only IPv4 subnets can be scanned")
	}
	if 1<<uint(bits-ones) > maxDiscoveryHosts {
		return ScanJob{}, ErrScanTooLarge
	}
	if len(creds) == 0 {
		return ScanJob{}, ErrScanNoCredentials
	}
	hosts, err := enumerateHosts(ipnet)
	if err != nil {
		return ScanJob{}, err
	}

	m.mu.Lock()
	m.purgeLocked()
	if _, busy := m.running[siteID]; busy {
		m.mu.Unlock()
		return ScanJob{}, ErrScanRunning
	}
	job := &ScanJob{ID: uuid.New(), SiteID: siteID, CIDR: ipnet.String(), Total: len(hosts), Running: true,
		Results: []ScanResult{}, StartedAt: m.now()}
	m.jobs[job.ID] = job
	m.running[siteID] = job.ID
	snapshot := *job
	m.mu.Unlock()

	// Not the request's context: the scan outlives the request that started it.
	go m.run(job, hosts, creds, existing)
	return snapshot, nil
}

func (m *ScanManager) run(job *ScanJob, hosts []string, creds []ScanCredential, existing map[string]bool) {
	ctx, cancel := context.WithTimeout(context.Background(), scanMaxDuration)
	defer cancel()
	work := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < scanConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range work {
				if m.hold != nil {
					<-m.hold
				}
				result, found := m.probe(ctx, host, creds, existing)
				m.mu.Lock()
				job.Done++
				if found {
					job.Results = append(job.Results, result)
				}
				m.mu.Unlock()
			}
		}()
	}
	for _, h := range hosts {
		work <- h
	}
	close(work)
	wg.Wait()

	m.mu.Lock()
	now := m.now()
	job.Running, job.FinishedAt = false, &now
	delete(m.running, job.SiteID)
	m.mu.Unlock()
}

// probe tries each profile in order and stops at the first that answers. A
// blocked address is not tried again with other profiles.
func (m *ScanManager) probe(ctx context.Context, host string, creds []ScanCredential, existing map[string]bool) (ScanResult, bool) {
	for _, c := range creds {
		sys, err := m.id.IdentifyWith(ctx, host, 161, c.Cred, m.perHost, 0)
		if errors.Is(err, ErrTargetBlocked) {
			return ScanResult{}, false
		}
		if err != nil {
			continue
		}
		return ScanResult{
			Host: host, Port: 161, Name: sys.Name, Descr: sys.Descr, ObjectID: sys.ObjectID,
			Vendor: snmp.VendorFor(sys.ObjectID), CredentialID: c.ID, CredentialName: c.Name,
			AlreadyAdded: existing[fmt.Sprintf("%s:%d", host, 161)],
		}, true
	}
	return ScanResult{}, false
}

// Get returns a copy of a site's scan.
func (m *ScanManager) Get(siteID, jobID uuid.UUID) (ScanJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()
	job, ok := m.jobs[jobID]
	if !ok || job.SiteID != siteID {
		return ScanJob{}, ErrScanNotFound
	}
	out := *job
	out.Results = append([]ScanResult(nil), job.Results...)
	return out, nil
}

func (m *ScanManager) purgeLocked() {
	now := m.now()
	for id, j := range m.jobs {
		if j.FinishedAt != nil && now.Sub(*j.FinishedAt) > scanJobTTL {
			delete(m.jobs, id)
		}
	}
}
