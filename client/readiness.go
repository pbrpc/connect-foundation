//revive:disable:package-comments
package client

import (
	"net/http"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v7"
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

// NewBackOff makes the schedule one host's attempts follow while it cannot be
// reached. Each host gets its own, made when its first attempt fails.
type NewBackOff func() backoff.BackOff

// hostState is a host that could not be reached: the schedule its attempts
// follow, and when the next one may go.
type hostState struct {
	backoff backoff.BackOff
	next    time.Time
}

// readyTransport paces requests to hosts that cannot be reached. A gRPC
// connection did this on its own: an RPC opened with WaitForReady blocked
// until the connection's reconnect backoff got through. An HTTP transport
// tries a dial on every request, so the loops that reopen a stream after it
// drops would spin against a peer that is down. This gates them: after a
// failure to reach a host, every request to that host waits until its
// schedule allows the next attempt, and a request that gets through clears it.
//
// A request is held before it is sent and never sent twice, so a streaming
// request body is as safe here as a unary one. Hosts are kept apart: one being
// down holds nothing bound for another.
type readyTransport struct {
	base       http.RoundTripper
	clock      Clock
	newBackOff NewBackOff

	// mu guards hosts. Requests arrive on their own goroutines, and a BackOff
	// is not safe for concurrent use.
	mu    sync.Mutex
	hosts map[string]*hostState
}

// NewReadyTransport wraps base with the pacing described on readyTransport.
// base nil means the standard transport, the one NewHTTPClient uses on its
// own; clock nil means the system clock; newBackOff nil means the backoff
// library's exponential schedule with its defaults.
func NewReadyTransport(base http.RoundTripper, clock Clock, newBackOff NewBackOff) http.RoundTripper {
	if base == nil {
		base = NewTransport()
	}

	if clock == nil {
		clock = systemClock{}
	}

	if newBackOff == nil {
		newBackOff = func() backoff.BackOff { return backoff.NewExponentialBackOff() }
	}

	return &readyTransport{
		base:       base,
		clock:      clock,
		newBackOff: newBackOff,
		hosts:      map[string]*hostState{},
	}
}

// RoundTrip waits until the host may be attempted, then sends the request. A
// response, with whatever status, proves the host reachable and clears its
// state; no response starts or extends its schedule.
func (t *readyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host

	if err := t.wait(req, host); err != nil {
		return nil, err
	}

	response, err := t.base.RoundTrip(req)

	t.record(host, err == nil)

	return response, err
}

// wait blocks until host's next attempt may go or the request's context ends.
// A host with no state may be attempted at once.
func (t *readyTransport) wait(req *http.Request, host string) error {
	t.mu.Lock()
	var next time.Time
	if state, found := t.hosts[host]; found {
		next = state.next
	}
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

// record notes whether host was reached. Reached forgets the host; not reached
// schedules its next attempt, starting its schedule if this was the first
// failure. A schedule that answers Stop has nothing more to wait for, so the
// next attempt is immediate.
func (t *readyTransport) record(host string, reached bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if reached {
		delete(t.hosts, host)

		return
	}

	state, found := t.hosts[host]
	if !found {
		state = &hostState{backoff: t.newBackOff()}
		t.hosts[host] = state
	}

	delay := state.backoff.NextBackOff()
	if delay == backoff.Stop {
		state.next = time.Time{}

		return
	}

	state.next = t.clock.Now().Add(delay)
}
