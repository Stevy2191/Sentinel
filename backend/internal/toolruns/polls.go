package toolruns

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// pollTracker remembers when each agent last asked for a job. In memory on
// purpose: after a restart every agent polls again within one long-poll
// cycle.
type pollTracker struct {
	mu   sync.Mutex
	last map[uuid.UUID]time.Time
}

func newPollTracker() *pollTracker {
	return &pollTracker{last: make(map[uuid.UUID]time.Time)}
}

// seen records a poll by agentID at now.
func (p *pollTracker) seen(agentID uuid.UUID, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last[agentID] = now
}

// lastSeen returns agentID's last poll, and false when it has not polled
// since Sentinel started.
func (p *pollTracker) lastSeen(agentID uuid.UUID) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.last[agentID]
	return t, ok
}
