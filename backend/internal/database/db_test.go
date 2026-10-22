package database

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// fakeClock is a clock that only moves when the code under test sleeps, so
// the retry window can be exercised without waiting for it.
type fakeClock struct {
	t      time.Time
	sleeps []time.Duration
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
}

// A database that is still starting (or whose container name does not
// resolve yet) must not stop the backend: it keeps trying, waiting a little
// longer each time, and carries on as soon as a connection works.
func TestConnectRetryKeepsTryingUntilTheDatabaseAnswers(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	want := &gorm.DB{}
	attempts := 0
	open := func() (*gorm.DB, error) {
		attempts++
		if attempts < 4 {
			return nil, errors.New("dial tcp: lookup postgres on 127.0.0.11:53: no such host")
		}
		return want, nil
	}

	got, err := connectRetry(open, time.Minute, clock.now, clock.sleep)
	if err != nil {
		t.Fatalf("got error %v, want a connection", err)
	}
	if got != want {
		t.Fatal("did not return the connection the successful attempt opened")
	}
	if attempts != 4 {
		t.Errorf("attempts = %d, want 4", attempts)
	}
	wantSleeps := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(clock.sleeps) != len(wantSleeps) {
		t.Fatalf("sleeps = %v, want %v", clock.sleeps, wantSleeps)
	}
	for i := range wantSleeps {
		if clock.sleeps[i] != wantSleeps[i] {
			t.Errorf("sleeps = %v, want %v", clock.sleeps, wantSleeps)
			break
		}
	}
}

// sqlStateError stands in for *pgconn.PgError, which reports its code the
// same way.
type sqlStateError struct{ code string }

func (e *sqlStateError) Error() string    { return "FATAL: server said no (SQLSTATE " + e.code + ")" }
func (e *sqlStateError) SQLState() string { return e.code }

// A server that answers and refuses the login will not change its mind, so
// waiting a minute only hides the real error from whoever is installing.
func TestConnectRetryStopsAtOnceWhenTheServerRefusesTheLogin(t *testing.T) {
	for _, code := range []string{"28P01", "28000", "3D000"} {
		clock := &fakeClock{t: time.Unix(0, 0)}
		attempts := 0
		open := func() (*gorm.DB, error) {
			attempts++
			return nil, fmt.Errorf("failed to connect to `user=sentinel database=sentinel`: %w", &sqlStateError{code})
		}

		_, err := connectRetry(open, time.Minute, clock.now, clock.sleep)
		if err == nil {
			t.Fatalf("%s: got nil, want the server's error", code)
		}
		if !strings.Contains(err.Error(), code) {
			t.Errorf("%s: error %q does not carry the server's error", code, err)
		}
		if attempts != 1 || len(clock.sleeps) != 0 {
			t.Errorf("%s: attempts = %d, sleeps = %v; want 1 attempt and no waiting", code, attempts, clock.sleeps)
		}
	}
}

// A database that never answers is still an error, reported once the window
// has run out, and the error says how long it tried and why the last
// attempt failed.
func TestConnectRetryGivesUpAfterTheWindow(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	attempts := 0
	open := func() (*gorm.DB, error) {
		attempts++
		return nil, errors.New("connection refused")
	}

	_, err := connectRetry(open, time.Minute, clock.now, clock.sleep)
	if err == nil {
		t.Fatal("got nil, want an error once the window ran out")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error %q does not carry the last failure", err)
	}
	if !strings.Contains(err.Error(), "1m0s") {
		t.Errorf("error %q does not say how long it tried", err)
	}
	if attempts < 5 {
		t.Errorf("attempts = %d, want several within a minute", attempts)
	}
	var slept time.Duration
	for _, d := range clock.sleeps {
		if d > connectMaxDelay {
			t.Errorf("slept %s in one go, want at most %s", d, connectMaxDelay)
		}
		slept += d
	}
	if slept > time.Minute {
		t.Errorf("slept %s in total, want no more than the 1m window", slept)
	}
}
