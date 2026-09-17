//revive:disable:package-comments
package client

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v7"
)

var errUnreachable = errors.New("connection refused")

// roundTripperStub stands in for the transport under the ready one. It fails
// requests to the hosts named in down with a transport error and answers the
// rest, counting calls per host.
type roundTripperStub struct {
	down  map[string]bool
	calls map[string]int
}

func newRoundTripperStub(down ...string) *roundTripperStub {
	s := &roundTripperStub{down: map[string]bool{}, calls: map[string]int{}}
	for _, host := range down {
		s.down[host] = true
	}

	return s
}

func (s *roundTripperStub) RoundTrip(request *http.Request) (*http.Response, error) {
	host := request.URL.Host
	s.calls[host]++

	if s.down[host] {
		return nil, errUnreachable
	}

	return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Request: request}, nil
}

// clockStub is a Clock the test drives: Now is fixed, and After records the
// delay asked for, signals asked, and answers with a channel the test fires.
type clockStub struct {
	now    time.Time
	fire   chan time.Time
	asked  chan struct{}
	waited []time.Duration
}

func newClockStub() *clockStub {
	return &clockStub{
		now:   time.Unix(1_000_000, 0),
		fire:  make(chan time.Time),
		asked: make(chan struct{}, 16),
	}
}

func (c *clockStub) Now() time.Time { return c.now }

func (c *clockStub) After(d time.Duration) <-chan time.Time {
	c.waited = append(c.waited, d)
	c.asked <- struct{}{}

	return c.fire
}

// scheduleStub is a BackOff answering a fixed sequence of delays, so a test
// knows what the transport should wait. It records how many were made and
// whether it was reset.
type scheduleStub struct {
	delays []time.Duration
	calls  int
	resets int
}

func (s *scheduleStub) NextBackOff() time.Duration {
	delay := s.delays[min(s.calls, len(s.delays)-1)]
	s.calls++

	return delay
}

func (s *scheduleStub) Reset() { s.resets++ }

// fixedSchedule makes every host follow the given delays.
func fixedSchedule(delays ...time.Duration) NewBackOff {
	return func() backoff.BackOff { return &scheduleStub{delays: delays} }
}

// newRequest builds a request to host under ctx for the stubs to see.
func newRequest(ctx context.Context, t *testing.T, host string) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+host+"/", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return request
}

func TestNewReadyTransport(t *testing.T) {
	t.Run("substitutes the standard transport, system clock, and exponential schedule", func(t *testing.T) {
		transport, ok := NewReadyTransport(nil, nil, nil).(*readyTransport)
		if !ok {
			t.Fatal("expected a readyTransport")
		}

		base, ok := transport.base.(*http.Transport)
		if !ok {
			t.Fatalf("base = %T, want the standard transport", transport.base)
		}
		if !base.Protocols.UnencryptedHTTP2() {
			t.Errorf("protocols = %v, want cleartext HTTP/2", base.Protocols)
		}
		if _, ok := transport.clock.(systemClock); !ok {
			t.Errorf("clock = %T, want the system clock", transport.clock)
		}
		if _, ok := transport.newBackOff().(*backoff.ExponentialBackOff); !ok {
			t.Error("expected the library's exponential schedule")
		}
	})

	t.Run("keeps what it is given", func(t *testing.T) {
		base := newRoundTripperStub()
		clock := newClockStub()

		transport := NewReadyTransport(base, clock, fixedSchedule(time.Second)).(*readyTransport)

		if transport.base != base {
			t.Error("expected the base transport it was given")
		}
		if transport.clock != clock {
			t.Error("expected the clock it was given")
		}
		if _, ok := transport.newBackOff().(*scheduleStub); !ok {
			t.Error("expected the schedule it was given")
		}
	})
}

func TestSystemClock(t *testing.T) {
	clock := systemClock{}

	if clock.Now().IsZero() {
		t.Error("expected the current time")
	}

	select {
	case <-clock.After(0):
	case <-t.Context().Done():
		t.Fatal("After never fired")
	}
}

// attempt sends a request to host on its own goroutine and answers with the
// channel its error arrives on.
func attempt(ctx context.Context, t *testing.T, transport http.RoundTripper, host string) <-chan error {
	t.Helper()

	results := make(chan error, 1)

	go func() {
		_, err := transport.RoundTrip(newRequest(ctx, t, host))
		results <- err
	}()

	return results
}

func TestReadyTransport(t *testing.T) {
	const h1, h2 = "h1:50051", "h2:50051"

	t.Run("sends at once while the host is reachable", func(t *testing.T) {
		base := newRoundTripperStub()
		clock := newClockStub()
		transport := NewReadyTransport(base, clock, fixedSchedule(time.Second))

		response, err := transport.RoundTrip(newRequest(t.Context(), t, h1))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// A response with any status is the host answering.
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want the base transport's answer passed through", response.StatusCode)
		}
		if len(clock.waited) != 0 {
			t.Errorf("waited %v, want no wait", clock.waited)
		}
	})

	t.Run("waits out the schedule after a failure and forgets the host on success", func(t *testing.T) {
		base := newRoundTripperStub(h1)
		clock := newClockStub()
		transport := NewReadyTransport(base, clock, fixedSchedule(time.Second, 2*time.Second))

		// First attempt fails; nothing was waited for.
		if _, err := transport.RoundTrip(newRequest(t.Context(), t, h1)); !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}

		// Second attempt waits the first delay, then fails again.
		results := attempt(t.Context(), t, transport, h1)
		clock.fire <- clock.now

		if err := <-results; !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}

		// The host comes back. The third attempt waits the second delay and
		// gets through.
		delete(base.down, h1)

		results = attempt(t.Context(), t, transport, h1)
		clock.fire <- clock.now

		if err := <-results; err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []time.Duration{time.Second, 2 * time.Second}
		if len(clock.waited) != len(want) || clock.waited[0] != want[0] || clock.waited[1] != want[1] {
			t.Errorf("waited %v, want %v", clock.waited, want)
		}

		// The success forgot the host: the next attempt does not wait.
		if _, err := transport.RoundTrip(newRequest(t.Context(), t, h1)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(clock.waited) != len(want) {
			t.Errorf("waited %v, want no further wait", clock.waited)
		}
		if base.calls[h1] != 4 {
			t.Errorf("base transport calls = %d, want 4", base.calls[h1])
		}
	})

	t.Run("keeps hosts apart", func(t *testing.T) {
		base := newRoundTripperStub(h1)
		clock := newClockStub()
		transport := NewReadyTransport(base, clock, fixedSchedule(time.Second))

		if _, err := transport.RoundTrip(newRequest(t.Context(), t, h1)); !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}

		// h1 is in backoff; h2 is not held by it.
		if _, err := transport.RoundTrip(newRequest(t.Context(), t, h2)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(clock.waited) != 0 {
			t.Errorf("waited %v, want no wait for another host", clock.waited)
		}
	})

	t.Run("attempts at once when the schedule stops", func(t *testing.T) {
		base := newRoundTripperStub(h1)
		clock := newClockStub()
		transport := NewReadyTransport(base, clock, fixedSchedule(backoff.Stop))

		if _, err := transport.RoundTrip(newRequest(t.Context(), t, h1)); !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}
		if _, err := transport.RoundTrip(newRequest(t.Context(), t, h1)); !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}
		if len(clock.waited) != 0 {
			t.Errorf("waited %v, want no wait after Stop", clock.waited)
		}
	})

	t.Run("gives up the wait when the request's context ends", func(t *testing.T) {
		base := newRoundTripperStub(h1)
		clock := newClockStub()
		transport := NewReadyTransport(base, clock, fixedSchedule(time.Second))

		if _, err := transport.RoundTrip(newRequest(t.Context(), t, h1)); !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}

		ctx, cancel := context.WithCancel(t.Context())

		results := attempt(ctx, t, transport, h1)

		// The wait is in progress once the clock has been asked; cancelling
		// then is what ends it.
		select {
		case <-clock.asked:
		case <-t.Context().Done():
			t.Fatal("the transport never waited")
		}

		cancel()

		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want %v", err, context.Canceled)
		}
		if base.calls[h1] != 1 {
			t.Errorf("base transport calls = %d, want the cancelled request not sent", base.calls[h1])
		}
	})
}
