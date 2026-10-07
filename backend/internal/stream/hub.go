package stream

import (
	"sync"
	"sync/atomic"
)

// Hub fans messages out to subscribers by key (a tool run's id, say). It
// never blocks a publisher: a subscriber whose buffer is full is dropped,
// and its reader is expected to reconnect and catch up from the database.
type Hub struct {
	buffer int

	mu   sync.Mutex
	subs map[string]map[*Subscription]struct{}
}

// NewHub returns a hub whose subscriptions buffer up to buffer messages
// (at least 1).
func NewHub(buffer int) *Hub {
	if buffer < 1 {
		buffer = 1
	}
	return &Hub{buffer: buffer, subs: make(map[string]map[*Subscription]struct{})}
}

// Subscription is one reader of one key.
type Subscription struct {
	hub     *Hub
	key     string
	ch      chan Message
	closed  bool // guarded by hub.mu
	dropped atomic.Bool
}

// Subscribe starts receiving the messages published to key from now on.
func (h *Hub) Subscribe(key string) *Subscription {
	s := &Subscription{hub: h, key: key, ch: make(chan Message, h.buffer)}
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[key]
	if set == nil {
		set = make(map[*Subscription]struct{})
		h.subs[key] = set
	}
	set[s] = struct{}{}
	return s
}

// Publish delivers m to every subscriber of key without blocking. A
// subscriber whose buffer is full is dropped: its channel is closed and
// Dropped reports true.
func (h *Hub) Publish(key string, m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[key] {
		select {
		case s.ch <- m:
		default:
			s.dropped.Store(true)
			h.removeLocked(s)
		}
	}
}

// removeLocked closes s and forgets it. Caller holds h.mu.
func (h *Hub) removeLocked(s *Subscription) {
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
	set := h.subs[s.key]
	delete(set, s)
	if len(set) == 0 {
		delete(h.subs, s.key)
	}
}

// C delivers the messages; it is closed when the subscription is dropped
// or closed.
func (s *Subscription) C() <-chan Message { return s.ch }

// Dropped reports whether the hub dropped this subscriber for falling
// behind.
func (s *Subscription) Dropped() bool { return s.dropped.Load() }

// Close stops the subscription. Safe to call more than once, and after a
// drop.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.removeLocked(s)
}
