package crawlpolicy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrUnknownGroup indicates that a runtime group ID is not in the policy.
	ErrUnknownGroup = errors.New("crawl policy runtime: unknown group")
	// ErrRuntimeGroupDisabled indicates that Acquire was called for a disabled
	// group.
	ErrRuntimeGroupDisabled = errors.New("crawl policy runtime: group disabled")
	// ErrBatchLimitReached indicates that no more starts may be reserved until
	// ResetBatch is called.
	ErrBatchLimitReached = errors.New("crawl policy runtime: batch request limit reached")
	// ErrNilContext indicates a nil context passed to Acquire.
	ErrNilContext = errors.New("crawl policy runtime: nil context")
	// ErrReservationFinalized indicates that a provisional request reservation
	// has already been committed or refunded.
	ErrReservationFinalized = errors.New("crawl policy runtime: request reservation already finalized")
)

// AvailabilityReason explains why a group is or is not currently ready.
type AvailabilityReason string

const (
	AvailabilityReady       AvailabilityReason = "ready"
	AvailabilityDisabled    AvailabilityReason = "disabled"
	AvailabilityBatchLimit  AvailabilityReason = "batch_limit"
	AvailabilityPending     AvailabilityReason = "pending_reservation"
	AvailabilityConcurrency AvailabilityReason = "concurrency_limit"
	AvailabilityRate        AvailabilityReason = "rate_limit"
)

// Availability is a race-safe point-in-time scheduling snapshot. RetryAfter is
// populated for rate-limited groups; concurrency and batch availability depend
// on Release and ResetBatch respectively.
type Availability struct {
	Ready               bool
	Reason              AvailabilityReason
	ActiveRequests      int
	MaxConcurrency      int
	RequestsInBatch     int
	PendingReservations int
	MaxRequestsPerBatch int
	NextStart           time.Time
	RetryAfter          time.Duration
}

// Runtime enforces mutable per-group concurrency, request-start interval, and
// batch-budget state. A Runtime is safe for concurrent use.
type Runtime struct {
	mu      sync.Mutex
	groups  map[string]*runtimeGroup
	changed chan struct{}
}

type runtimeGroup struct {
	enabled             bool
	minimumInterval     time.Duration
	maxConcurrency      int
	maxRequestsPerBatch int
	active              int
	requestsInBatch     int
	batchGeneration     uint64
	nextStart           time.Time
	rateContributions   map[*RequestReservation]time.Time
	pendingReservations map[*RequestReservation]struct{}
	changed             chan struct{}
}

type reservationState uint8

const (
	reservationPending reservationState = iota
	reservationCommitted
	reservationRefunded
)

// RequestReservation provisionally owns one group's concurrency, batch, and
// rate capacity. Commit converts it into an actual request start and returns
// the release function for the active request. Refund restores every
// provisional contribution. A reservation may be finalized exactly once.
type RequestReservation struct {
	runtime         *Runtime
	group           *runtimeGroup
	batchGeneration uint64
	state           reservationState
	releaseOnce     sync.Once
}

// NewRuntime creates independent mutable limit state from a validated policy.
// A nil policy creates an empty runtime whose group operations return
// ErrUnknownGroup.
func NewRuntime(policy *Policy) *Runtime {
	runtime := &Runtime{
		groups:  make(map[string]*runtimeGroup),
		changed: make(chan struct{}),
	}
	if policy == nil {
		return runtime
	}
	for id, group := range policy.groupsByID {
		runtime.groups[id] = &runtimeGroup{
			enabled:             group.Enabled,
			minimumInterval:     group.MinRequestInterval,
			maxConcurrency:      group.MaxConcurrency,
			maxRequestsPerBatch: group.MaxRequestsPerBatch,
			rateContributions:   make(map[*RequestReservation]time.Time),
			pendingReservations: make(map[*RequestReservation]struct{}),
			changed:             make(chan struct{}),
		}
	}
	return runtime
}

// NewRuntime creates independent mutable limit state for p.
func (p *Policy) NewRuntime() *Runtime {
	return NewRuntime(p)
}

// Acquire waits for the group's concurrency and minimum-start-interval limits,
// then atomically reserves one active request and one request from the current
// batch. A fully committed batch cap returns ErrBatchLimitReached immediately;
// provisional exhaustion waits for a pending reservation to commit or refund.
// Context cancellation interrupts all waits.
//
// The returned release function must be called when the request finishes. It is
// idempotent and may safely be called from a deferred cleanup path.
func (r *Runtime) Acquire(ctx context.Context, groupID string) (release func(), err error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if r == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		r.mu.Lock()
		state, ok := r.groups[groupID]
		if !ok {
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
		}
		if !state.enabled {
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: %q", ErrRuntimeGroupDisabled, groupID)
		}
		if err := ctx.Err(); err != nil {
			r.mu.Unlock()
			return nil, err
		}

		now := time.Now()
		availability := availabilityLocked(state, now)
		if availability.Reason == AvailabilityBatchLimit {
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: %q", ErrBatchLimitReached, groupID)
		}
		if availability.Ready {
			reservation := reserveRuntimeGroupLocked(r, state)
			release := commitReservationLocked(reservation, now)
			r.mu.Unlock()
			return release, nil
		}

		changed := state.changed
		nextStart := state.nextStart
		r.mu.Unlock()

		if now.Before(nextStart) {
			if err := waitForRuntimeChange(ctx, changed, nextStart); err != nil {
				return nil, err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

// TryAcquire atomically reserves a request only when the group is ready at
// now. A non-ready group returns its availability and a nil release without
// waiting.
func (r *Runtime) TryAcquire(groupID string, now time.Time) (release func(), availability Availability, err error) {
	if r == nil {
		return nil, Availability{}, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.groups[groupID]
	if !ok {
		return nil, Availability{}, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
	}
	availability = availabilityLocked(state, now)
	if !availability.Ready {
		return nil, availability, nil
	}
	reservation := reserveRuntimeGroupLocked(r, state)
	return commitReservationLocked(reservation, now), availability, nil
}

// TryReserve provisionally reserves a request only when the group is ready at
// now. Unlike TryAcquire, the caller must later Commit immediately before the
// first actual request attempt or Refund if no request will be attempted.
func (r *Runtime) TryReserve(groupID string, now time.Time) (reservation *RequestReservation, availability Availability, err error) {
	if r == nil {
		return nil, Availability{}, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.groups[groupID]
	if !ok {
		return nil, Availability{}, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
	}
	availability = availabilityLocked(state, now)
	if !availability.Ready {
		return nil, availability, nil
	}
	return reserveRuntimeGroupLocked(r, state), availability, nil
}

// Ready returns a point-in-time availability snapshot for groupID using the
// caller-supplied time. It never reserves capacity. Unknown IDs return
// ErrUnknownGroup; disabled groups return a non-ready snapshot.
func (r *Runtime) Ready(groupID string, now time.Time) (Availability, error) {
	if r == nil {
		return Availability{}, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.groups[groupID]
	if !ok {
		return Availability{}, fmt.Errorf("%w: %q", ErrUnknownGroup, groupID)
	}
	return availabilityLocked(state, now), nil
}

// Changes returns a broadcast channel closed by the next runtime state change.
// Callers obtain the channel before taking availability snapshots so a change
// between inspection and waiting cannot be missed.
func (r *Runtime) Changes() <-chan struct{} {
	if r == nil {
		changed := make(chan struct{})
		close(changed)
		return changed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

// ResetBatch resets per-batch request-start counts for every group. Active
// request counts and next-start timestamps are deliberately preserved.
func (r *Runtime) ResetBatch() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, state := range r.groups {
		state.batchGeneration++
		state.requestsInBatch = len(state.pendingReservations)
		for reservation := range state.pendingReservations {
			reservation.batchGeneration = state.batchGeneration
		}
		signalRuntimeGroupLocked(r, state)
	}
}

func availabilityLocked(state *runtimeGroup, now time.Time) Availability {
	nextStart := state.nextStart
	if !now.Before(nextStart) {
		nextStart = time.Time{}
	}
	availability := Availability{
		ActiveRequests:      state.active,
		MaxConcurrency:      state.maxConcurrency,
		RequestsInBatch:     state.requestsInBatch,
		PendingReservations: len(state.pendingReservations),
		MaxRequestsPerBatch: state.maxRequestsPerBatch,
		NextStart:           nextStart,
	}
	switch {
	case !state.enabled:
		availability.Reason = AvailabilityDisabled
	case state.requestsInBatch >= state.maxRequestsPerBatch && len(state.pendingReservations) > 0:
		availability.Reason = AvailabilityPending
	case state.requestsInBatch >= state.maxRequestsPerBatch:
		availability.Reason = AvailabilityBatchLimit
	case state.active >= state.maxConcurrency:
		availability.Reason = AvailabilityConcurrency
	case hasPendingRateReservationLocked(state):
		availability.Reason = AvailabilityRate
	case !nextStart.IsZero():
		availability.Reason = AvailabilityRate
		availability.RetryAfter = nextStart.Sub(now)
	default:
		availability.Ready = true
		availability.Reason = AvailabilityReady
	}
	return availability
}

func signalRuntimeGroupLocked(runtime *Runtime, state *runtimeGroup) {
	close(state.changed)
	state.changed = make(chan struct{})
	close(runtime.changed)
	runtime.changed = make(chan struct{})
}

func reserveRuntimeGroupLocked(runtime *Runtime, state *runtimeGroup) *RequestReservation {
	reservation := &RequestReservation{
		runtime:         runtime,
		group:           state,
		batchGeneration: state.batchGeneration,
		state:           reservationPending,
	}
	state.active++
	state.requestsInBatch++
	state.pendingReservations[reservation] = struct{}{}
	if state.minimumInterval > 0 {
		// A pending reservation blocks another rate reservation even if claim
		// processing takes longer than the configured interval. Commit replaces
		// this sentinel with the actual request-start deadline.
		state.rateContributions[reservation] = time.Time{}
	}
	signalRuntimeGroupLocked(runtime, state)
	return reservation
}

// Commit finalizes a provisional reservation and returns the idempotent active
// request release function. Calling Commit after Commit or Refund is an error.
func (reservation *RequestReservation) Commit() (func(), error) {
	if reservation == nil || reservation.runtime == nil || reservation.group == nil {
		return nil, ErrReservationFinalized
	}
	reservation.runtime.mu.Lock()
	defer reservation.runtime.mu.Unlock()
	if reservation.state != reservationPending {
		return nil, ErrReservationFinalized
	}
	return commitReservationLocked(reservation, time.Now()), nil
}

func commitReservationLocked(reservation *RequestReservation, now time.Time) func() {
	reservation.state = reservationCommitted
	state := reservation.group
	delete(state.pendingReservations, reservation)
	if state.minimumInterval > 0 {
		pruneRateContributionsLocked(state, now)
		state.rateContributions[reservation] = now.Add(state.minimumInterval)
		recalculateNextStartLocked(state)
	}
	signalRuntimeGroupLocked(reservation.runtime, state)
	return reservation.release
}

// Refund restores all capacity provisionally owned by an uncommitted
// reservation. It is idempotent; committed reservations must instead use the
// release function returned by Commit.
func (reservation *RequestReservation) Refund() {
	if reservation == nil || reservation.runtime == nil || reservation.group == nil {
		return
	}
	runtime := reservation.runtime
	runtime.mu.Lock()
	if reservation.state != reservationPending {
		runtime.mu.Unlock()
		return
	}
	reservation.state = reservationRefunded
	state := reservation.group
	if state.active > 0 {
		state.active--
	}
	if reservation.batchGeneration == state.batchGeneration && state.requestsInBatch > 0 {
		state.requestsInBatch--
	}
	delete(state.pendingReservations, reservation)
	delete(state.rateContributions, reservation)
	recalculateNextStartLocked(state)
	signalRuntimeGroupLocked(runtime, state)
	runtime.mu.Unlock()
}

func (reservation *RequestReservation) release() {
	reservation.releaseOnce.Do(func() {
		runtime := reservation.runtime
		runtime.mu.Lock()
		if reservation.group.active > 0 {
			reservation.group.active--
		}
		pruneRateContributionsLocked(reservation.group, time.Now())
		signalRuntimeGroupLocked(runtime, reservation.group)
		runtime.mu.Unlock()
	})
}

func pruneRateContributionsLocked(state *runtimeGroup, now time.Time) {
	for reservation, deadline := range state.rateContributions {
		if reservation.state != reservationPending && !deadline.After(now) {
			delete(state.rateContributions, reservation)
		}
	}
	recalculateNextStartLocked(state)
}

func hasPendingRateReservationLocked(state *runtimeGroup) bool {
	return state.minimumInterval > 0 && len(state.pendingReservations) > 0
}

func recalculateNextStartLocked(state *runtimeGroup) {
	state.nextStart = time.Time{}
	for _, deadline := range state.rateContributions {
		if deadline.After(state.nextStart) {
			state.nextStart = deadline
		}
	}
}

func waitForRuntimeChange(ctx context.Context, changed <-chan struct{}, nextStart time.Time) error {
	delay := time.Until(nextStart)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-changed:
		return nil
	case <-timer.C:
		return nil
	}
}
