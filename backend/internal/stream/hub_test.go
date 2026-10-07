package stream

import (
	"strconv"
	"sync"
	"testing"
)

func msg(id int) Message { return Message{ID: strconv.Itoa(id), Event: "event"} }

// recv takes what is buffered now, without waiting.
func recv(s *Subscription) []string {
	var ids []string
	for {
		select {
		case m, ok := <-s.C():
			if !ok {
				return append(ids, "closed")
			}
			ids = append(ids, m.ID)
		default:
			return ids
		}
	}
}

func TestHubFanOutInOrder(t *testing.T) {
	h := NewHub(8)
	a, b := h.Subscribe("run-1"), h.Subscribe("run-1")
	other := h.Subscribe("run-2")
	for i := 1; i <= 3; i++ {
		h.Publish("run-1", msg(i))
	}
	for name, s := range map[string]*Subscription{"a": a, "b": b} {
		if got := recv(s); len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "3" {
			t.Errorf("%s got %v, want [1 2 3]", name, got)
		}
	}
	if got := recv(other); len(got) != 0 {
		t.Errorf("a subscriber of another key got %v", got)
	}
}

// A subscriber that falls behind is dropped; the others keep receiving and
// the publisher never blocks.
func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub(2)
	slow, fast := h.Subscribe("k"), h.Subscribe("k")
	for i := 1; i <= 3; i++ {
		h.Publish("k", msg(i))
		recv(fast) // fast keeps up
	}
	if !slow.Dropped() || fast.Dropped() {
		t.Fatalf("slow dropped %v, fast dropped %v: want true, false", slow.Dropped(), fast.Dropped())
	}
	if got := recv(slow); len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "closed" {
		t.Errorf("slow got %v, want its buffer [1 2] then closed", got)
	}
	h.Publish("k", msg(4))
	if got := recv(fast); len(got) != 1 || got[0] != "4" {
		t.Errorf("fast got %v after the drop, want [4]", got)
	}
}

func TestHubCloseIsIdempotentAndForgets(t *testing.T) {
	h := NewHub(1)
	s := h.Subscribe("k")
	s.Close()
	s.Close()
	if _, ok := <-s.C(); ok {
		t.Error("channel still open after Close")
	}
	if s.Dropped() {
		t.Error("a closed subscription reports dropped")
	}
	h.mu.Lock()
	n := len(h.subs)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("hub still holds %d keys after the last Close", n)
	}
	h.Publish("k", msg(1))      // no subscribers: a no-op
	h.Publish("nobody", msg(1)) // never subscribed: a no-op
}

// Run with -race: publishers, subscribers and closers at once. Afterwards
// the hub must still work and must not have leaked subscriptions.
func TestHubConcurrentUse(t *testing.T) {
	h := NewHub(4)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				h.Publish("k", msg(i))
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s := h.Subscribe("k")
				recv(s)
				s.Close()
			}
		}()
	}
	wg.Wait()

	h.mu.Lock()
	n := len(h.subs)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("hub holds %d keys after every subscriber closed, want 0", n)
	}

	fresh := h.Subscribe("k")
	defer fresh.Close()
	h.Publish("k", msg(999))
	if got := recv(fresh); len(got) != 1 || got[0] != "999" {
		t.Errorf("fresh subscriber got %v after the storm, want [999]", got)
	}
}
