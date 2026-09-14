package client

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// roundTripperStub stands in for the transport under the ready one. It fails
// the first failures calls with a transport error and answers the rest.
type roundTripperStub struct {
	failures int
	calls    int
}

var errUnreachable = errors.New("connection refused")

func (s *roundTripperStub) RoundTrip(request *http.Request) (*http.Response, error) {
	s.calls++

	if s.calls <= s.failures {
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

// fixedRandom makes the jitter deterministic: 0.5 places it at zero.
func fixedRandom() float64 { return 0.5 }

// newRequest builds a request under ctx for the stubs to see.
func newRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://service/", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return request
}

func TestBackoffDelay(t *testing.T) {
	backoff := Backoff{Base: time.Second, Multiplier: 2, Jitter: 0.5, Max: 10 * time.Second}

	cases := []struct {
		name    string
		attempt int
		random  float64
		want    time.Duration
	}{
		{"first failure", 0, 0.5, time.Second},
		{"grows by the multiplier", 2, 0.5, 4 * time.Second},
		{"capped at max", 10, 0.5, 10 * time.Second},
		{"jitter below", 0, 0, 500 * time.Millisecond},
		{"jitter above", 0, 1, 1500 * time.Millisecond},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := backoff.Delay(c.attempt, c.random); got != c.want {
				t.Errorf("delay = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNewReadyTransport(t *testing.T) {
	t.Run("substitutes the system clock and default backoff", func(t *testing.T) {
		transport, ok := NewReadyTransport(&roundTripperStub{}, nil, Backoff{}).(*readyTransport)
		if !ok {
			t.Fatal("expected a readyTransport")
		}
		if _, ok := transport.clock.(systemClock); !ok {
			t.Errorf("clock = %T, want the system clock", transport.clock)
		}
		if transport.backoff != DefaultBackoff {
			t.Errorf("backoff = %v, want the default", transport.backoff)
		}
	})

	t.Run("keeps what it is given", func(t *testing.T) {
		clock := newClockStub()
		backoff := Backoff{Base: time.Millisecond, Multiplier: 1, Jitter: 0, Max: time.Millisecond}

		transport := NewReadyTransport(&roundTripperStub{}, clock, backoff).(*readyTransport)

		if transport.clock != clock {
			t.Error("expected the clock it was given")
		}
		if transport.backoff != backoff {
			t.Errorf("backoff = %v, want %v", transport.backoff, backoff)
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

// newReadyTransport builds a ready transport over base with the stub clock and
// a deterministic jitter.
func newReadyTransport(base http.RoundTripper, clock *clockStub) *readyTransport {
	transport := NewReadyTransport(base, clock, DefaultBackoff).(*readyTransport)
	transport.random = fixedRandom

	return transport
}

func TestReadyTransport(t *testing.T) {
	t.Run("sends at once while the peer is reachable", func(t *testing.T) {
		base := &roundTripperStub{}
		clock := newClockStub()
		transport := newReadyTransport(base, clock)

		response, err := transport.RoundTrip(newRequest(t, t.Context()))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// A response with any status is the peer answering.
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want the base transport's answer passed through", response.StatusCode)
		}
		if len(clock.waited) != 0 {
			t.Errorf("waited %v, want no wait", clock.waited)
		}
	})

	t.Run("waits out the backoff after a failure and resets on success", func(t *testing.T) {
		base := &roundTripperStub{failures: 2}
		clock := newClockStub()
		transport := newReadyTransport(base, clock)

		// First attempt fails; nothing was waited for.
		if _, err := transport.RoundTrip(newRequest(t, t.Context())); !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}

		// Second attempt waits the first delay, then fails again.
		results := make(chan error, 1)
		go func() {
			_, err := transport.RoundTrip(newRequest(t, t.Context()))
			results <- err
		}()

		clock.fire <- clock.now

		if err := <-results; !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}

		// Third attempt waits the second, longer delay, and gets through.
		go func() {
			_, err := transport.RoundTrip(newRequest(t, t.Context()))
			results <- err
		}()

		clock.fire <- clock.now

		if err := <-results; err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []time.Duration{DefaultBackoff.Delay(0, 0.5), DefaultBackoff.Delay(1, 0.5)}
		if len(clock.waited) != len(want) || clock.waited[0] != want[0] || clock.waited[1] != want[1] {
			t.Errorf("waited %v, want %v", clock.waited, want)
		}

		// The success cleared the backoff: the next attempt does not wait.
		if _, err := transport.RoundTrip(newRequest(t, t.Context())); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(clock.waited) != len(want) {
			t.Errorf("waited %v, want no further wait", clock.waited)
		}
		if base.calls != 4 {
			t.Errorf("base transport calls = %d, want 4", base.calls)
		}
	})

	t.Run("gives up the wait when the request's context ends", func(t *testing.T) {
		base := &roundTripperStub{failures: 1}
		clock := newClockStub()
		transport := newReadyTransport(base, clock)

		if _, err := transport.RoundTrip(newRequest(t, t.Context())); !errors.Is(err, errUnreachable) {
			t.Fatalf("error = %v, want %v", err, errUnreachable)
		}

		ctx, cancel := context.WithCancel(t.Context())

		results := make(chan error, 1)
		go func() {
			_, err := transport.RoundTrip(newRequest(t, ctx))
			results <- err
		}()

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
		if base.calls != 1 {
			t.Errorf("base transport calls = %d, want the cancelled request not sent", base.calls)
		}
	})
}
