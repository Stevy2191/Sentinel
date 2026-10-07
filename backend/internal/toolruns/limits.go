package toolruns

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// targetIdleTTL is how long an unused per-target bucket is kept.
const targetIdleTTL = 15 * time.Minute

// targetLimiter is the per-target-address rate limit: TargetRatePerMinute
// runs a minute against one address, across all users, with a burst of the
// same size. Checked in the service after the target is resolved, since
// middleware cannot know the address.
type targetLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*targetBucket
	lastSweep time.Time
}

type targetBucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func newTargetLimiter() *targetLimiter {
	return &targetLimiter{buckets: make(map[string]*targetBucket)}
}

// allow takes one token from ip's bucket at now. Idle buckets are swept
// while holding the lock, at most once per targetIdleTTL, so the map stays
// bounded without a goroutine of its own.
func (l *targetLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > targetIdleTTL {
		for k, b := range l.buckets {
			if now.Sub(b.lastSeen) > targetIdleTTL {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b := l.buckets[ip]
	if b == nil {
		b = &targetBucket{lim: rate.NewLimiter(rate.Every(time.Minute/TargetRatePerMinute), TargetRatePerMinute)}
		l.buckets[ip] = b
	}
	b.lastSeen = now
	return b.lim.AllowN(now, 1)
}

// activeCounts are the runs in progress (queued or running) that the caps
// compare against.
type activeCounts struct {
	Overall  int64 `gorm:"column:overall"`
	ByUser   int64 `gorm:"column:by_user"`
	ByTarget int64 `gorm:"column:by_target"`
	ByAgent  int64 `gorm:"column:by_agent"`
}

// checkCaps counts the active runs inside the caller's transaction, which
// holds the caps advisory lock, and returns a 429 Refusal for the first cap
// the new run would pass (per user, per target, overall, per agent), or nil.
// A nil targetIP or agentID counts nothing for that cap.
func checkCaps(tx *gorm.DB, userID uuid.UUID, targetIP *string, agentID *uuid.UUID, agentName string) (*Refusal, error) {
	var c activeCounts
	if err := tx.Raw(`SELECT count(*) AS overall,
			count(*) FILTER (WHERE user_id = ?) AS by_user,
			count(*) FILTER (WHERE target_ip = ?) AS by_target,
			count(*) FILTER (WHERE agent_id = ?) AS by_agent
		FROM tool_runs WHERE status IN (?, ?)`,
		userID, targetIP, agentID, models.ToolRunQueued, models.ToolRunRunning).Scan(&c).Error; err != nil {
		return nil, fmt.Errorf("counting active tool runs: %w", err)
	}
	limit := func(msg string) *Refusal {
		return &Refusal{Status: http.StatusTooManyRequests, Code: CodeLimit, Message: msg}
	}
	switch {
	case c.ByUser >= MaxActivePerUser:
		return limit(fmt.Sprintf("You already have %d runs in progress", MaxActivePerUser)), nil
	case targetIP != nil && c.ByTarget >= MaxActivePerTarget:
		return limit(fmt.Sprintf("%d runs are already running against %s", MaxActivePerTarget, *targetIP)), nil
	case c.Overall >= MaxActiveOverall:
		return limit(fmt.Sprintf("Sentinel is already running %d tool runs", MaxActiveOverall)), nil
	case agentID != nil && c.ByAgent >= MaxActivePerAgent:
		return limit(fmt.Sprintf("%s is already running %d tool runs", agentName, MaxActivePerAgent)), nil
	}
	return nil, nil
}
