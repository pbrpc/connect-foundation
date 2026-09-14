package client

import (
	"math"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// Clock is what the ready transport needs of time: the current instant, and a
// channel that fires after a duration. A test supplies one it controls.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// systemClock is the Clock production uses.
type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Backoff is how long a peer that could not be reached is left alone before
// the next attempt, growing with each failure in a row.
type Backoff struct {
	// Base is the delay after the first failure.
	Base time.Duration
	// Multiplier grows the delay on each further failure.
	Multiplier float64
	// Jitter is the fraction of the delay that is randomized either way, so
	// clients that failed together do not retry together.
	Jitter float64
	// Max caps the delay before jitter.
	Max time.Duration
}

// DefaultBackoff is grpc-go's connection backoff, so a client that moved from
// a gRPC connection waits out an outage the way it did before.
var DefaultBackoff = Backoff{
	Base:       time.Second,
	Multiplier: 1.6,
	Jitter:     0.2,
	Max:        2 * time.Minute,
}

// Delay answers with how long to wait after the attempt-th failure in a row,
// counting from zero. random is a draw in [0, 1) that places the jitter.
func (b Backoff) Delay(attempt int, random float64) time.Duration {
	delay := float64(b.Base) * math.Pow(b.Multiplier, float64(attempt))
	delay = math.Min(delay, float64(b.Max))
	delay *= 1 + b.Jitter*(2*random-1)

	return time.Duration(delay)
}

// readyTransport paces requests to a peer that cannot be reached. A gRPC
// connection did this on its own: an RPC opened with WaitForReady blocked
// until the connection's reconnect backoff got through. An HTTP transport
// tries a dial on every request, so the loops that reopen a stream after it
// drops would spin against a peer that is down. This puts the same backoff
// under them: after a failure to reach the peer, every request waits until
// the backoff has elapsed before trying again, and a request that got through
// resets it.
type readyTransport struct {
	base    http.RoundTripper
	clock   Clock
	backoff Backoff
	random  func() float64

	mu      sync.Mutex
	next    time.Time
	attempt int
}

// NewReadyTransport wraps base with the pacing described on readyTransport.
// clock nil means the system clock; a zero backoff means DefaultBackoff.
func NewReadyTransport(base http.RoundTripper, clock Clock, backoff Backoff) http.RoundTripper {
	if clock == nil {
		clock = systemClock{}
	}

	if backoff == (Backoff{}) {
		backoff = DefaultBackoff
	}

	return &readyTransport{
		base:    base,
		clock:   clock,
		backoff: backoff,
		random:  rand.Float64,
	}
}

// RoundTrip waits out the backoff, if one is running, then sends the request.
// A request the peer answered, with whatever status, proves it reachable and
// clears the backoff; one that produced no response starts or extends it.
func (t *readyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.wait(req); err != nil {
		return nil, err
	}

	response, err := t.base.RoundTrip(req)

	t.record(err == nil)

	return response, err
}

// wait blocks until the backoff has elapsed or the request's context ends.
func (t *readyTransport) wait(req *http.Request) error {
	t.mu.Lock()
	next := t.next
	t.mu.Unlock()

	delay := next.Sub(t.clock.Now())
	if delay <= 0 {
		return nil
	}

	select {
	case <-t.clock.After(delay):
		return nil
	case <-req.Context().Done():
		return req.Context().Err()
	}
}

// record notes whether the peer was reached, and schedules the next attempt
// when it was not.
func (t *readyTransport) record(reached bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if reached {
		t.next = time.Time{}
		t.attempt = 0

		return
	}

	t.next = t.clock.Now().Add(t.backoff.Delay(t.attempt, t.random()))
	t.attempt++
}
