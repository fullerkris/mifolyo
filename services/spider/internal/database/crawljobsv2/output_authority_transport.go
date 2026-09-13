package crawljobsv2

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// transportAuthoritySeal prevents ordinary package consumers from manufacturing
// a transport authority. The concrete boundary and every method that consumes
// raw Redis values are deliberately unexported from crawljobsv2.
// Keep the seal non-zero-sized. Go permits pointers to distinct zero-sized
// variables to compare equal, which would weaken the identity check below.
type transportAuthoritySeal struct {
	marker byte
}

var redisTransportAuthoritySeal = transportAuthoritySeal{marker: 1}

type transportAuthority struct {
	seal             *transportAuthoritySeal
	requestIOSession *requestIOAuthoritySession
}

func newTransportAuthority() transportAuthority {
	// A seal alone cannot prove that a lease's local I/O history is known.
	// There is deliberately no production session constructor while this
	// transport is dormant. Test-only helpers may establish fresh sessions.
	return transportAuthority{seal: &redisTransportAuthoritySeal}
}

func (authority transportAuthority) valid() bool {
	return authority.seal == &redisTransportAuthoritySeal
}

// requestIOAuthoritySession belongs to one authenticated transport lease
// session, not to one reply, retry, or connection. All transport copies retain
// this pointer. It binds once to the full lease identity and immutable run-policy
// projection; it is never reset, rebound, evicted, or reconstructed from a START
// reply. A future transport must retain it across reconnects for that lease.
//
// knownUnused asserts complete, uninterrupted local I/O history for this lease:
// absent slots have never issued a permit, including before a reconnect. Neither
// STARTED nor ALREADY_STARTED nor an empty registry proves that fact. A lost
// registry or adopted lease MUST NOT set it. Only tests initialize it today;
// missing/unknown history must remain reconciliation-only.
// Reservation ordinals are bounded by the protocol's 100-creations-per-run cap,
// so a fixed per-fence array also rejects changed intents reusing an ordinal.
// No package-global reservation history or unbounded lease map is retained.
type requestIOAuthoritySession struct {
	mu           sync.Mutex
	knownUnused  bool
	bound        bool
	lease        LeaseIdentity
	runPolicy    runPolicyBinding
	reservations [MaxReservationCreationsPerRun]*requestIOAuthorityState
}

// reconcile is called only after complete intent, policy, and wire validation.
// The invocation time, status, and current io_permission bit may change on an
// exact replay; the intent and every stored start snapshot must not.
func (session *requestIOAuthoritySession) reconcile(binding startRequestBinding) (*requestIOAuthorityState, error) {
	permission := binding.started.ioPermission
	if session == nil {
		if !permission {
			return nil, nil // Tombstones need no local I/O authority.
		}
		return nil, ErrInvalidResponseAuthority
	}
	ordinal := binding.intent.RequestOrdinal
	if ordinal == 0 || ordinal > MaxReservationCreationsPerRun {
		return nil, ErrInvalidResponseAuthority
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.bound {
		if session.lease != binding.intent.Lease || !sameRunPolicyBinding(session.runPolicy, binding.runPolicy.binding) {
			return nil, ErrInvalidResponseAuthority
		}
	} else {
		if permission && !session.knownUnused {
			return nil, ErrInvalidResponseAuthority
		}
		session.lease = binding.intent.Lease
		session.runPolicy = binding.runPolicy.binding
		session.bound = true
	}

	state := session.reservations[ordinal-1]
	if state != nil {
		if !sameStartRequestSnapshot(state.binding, binding) {
			return nil, ErrInvalidResponseAuthority
		}
		if !permission {
			state.revoke()
			return nil, nil
		}
		switch requestIOAuthorityPhase(atomic.LoadUint32(&state.phase)) {
		case requestIOAuthorityAvailable, requestIOAuthorityIssued, requestIOAuthorityConsumed:
			return state, nil // Never reset a previously issued/consumed permit.
		default:
			return nil, ErrInvalidResponseAuthority
		}
	}
	if permission && !session.knownUnused {
		return nil, ErrInvalidResponseAuthority
	}
	state = newRequestIOAuthorityState(binding)
	if !permission {
		state.revoke()
	}
	session.reservations[ordinal-1] = state
	if !permission {
		return nil, nil
	}
	return state, nil
}

func sameStartRequestSnapshot(left, right startRequestBinding) bool {
	left.started.ioPermission = false
	right.started.ioPermission = false
	return left.intent == right.intent && left.reservationID == right.reservationID && left.started == right.started &&
		sameRunPolicyBinding(left.runPolicy.binding, right.runPolicy.binding)
}

// Sessions are pointer-only (they contain a mutex). Keep diagnostic surfaces
// lossy just like the transport and request authority values they retain.
func (*requestIOAuthoritySession) String() string { return redactedString("requestIOAuthoritySession") }
func (*requestIOAuthoritySession) GoString() string {
	return redactedString("requestIOAuthoritySession")
}
func (*requestIOAuthoritySession) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("requestIOAuthoritySession"))
}
func (*requestIOAuthoritySession) MarshalJSON() ([]byte, error) {
	return redactedJSON("requestIOAuthoritySession")
}
func (*requestIOAuthoritySession) MarshalText() ([]byte, error) {
	return redactedCompositeText("requestIOAuthoritySession")
}
func (*requestIOAuthoritySession) LogValue() slog.Value {
	return redactedLogValue("requestIOAuthoritySession")
}

type requestIOAuthorityPhase uint32

const (
	requestIOAuthorityAvailable requestIOAuthorityPhase = iota + 1
	requestIOAuthorityIssued
	requestIOAuthorityConsumed
	requestIOAuthorityRevoked
)

// startRequestBinding is the immutable semantic value authenticated by one
// successful START_REQUEST wire response.
type startRequestBinding struct {
	runPolicy     RunPolicyAuthority
	intent        ReservationIntent
	reservationID ReservationID
	started       StartRequestStarted
}

// requestIOAuthorityState is intentionally shared by separately parsed replies,
// response/permit copies, and consumed evidence in one lease session. Atomic phase
// transitions make permit issue and successful-I/O evidence consumption
// independently one-use operations. The binding never changes after allocation.
type requestIOAuthorityState struct {
	phase   uint32
	binding startRequestBinding
}

func newRequestIOAuthorityState(binding startRequestBinding) *requestIOAuthorityState {
	return &requestIOAuthorityState{
		phase:   uint32(requestIOAuthorityAvailable),
		binding: binding,
	}
}

func (state *requestIOAuthorityState) issue(binding startRequestBinding) bool {
	if state == nil || !state.binding.started.ioPermission || state.binding != binding {
		return false
	}
	return atomic.CompareAndSwapUint32(
		&state.phase,
		uint32(requestIOAuthorityAvailable),
		uint32(requestIOAuthorityIssued),
	)
}

// A reconciliation-only tombstone must not leave an older unused response or
// unconsumed permit usable. Already consumed evidence remains immutable and
// valid for transcript/output checks; reconciliation must never erase it.
func (state *requestIOAuthorityState) revoke() {
	if state == nil {
		return
	}
	for {
		phase := atomic.LoadUint32(&state.phase)
		if phase == uint32(requestIOAuthorityConsumed) || phase == uint32(requestIOAuthorityRevoked) {
			return
		}
		if atomic.CompareAndSwapUint32(&state.phase, phase, uint32(requestIOAuthorityRevoked)) {
			return
		}
	}
}

func (state *requestIOAuthorityState) consume() (startRequestBinding, bool) {
	if state == nil || !atomic.CompareAndSwapUint32(
		&state.phase,
		uint32(requestIOAuthorityIssued),
		uint32(requestIOAuthorityConsumed),
	) {
		return startRequestBinding{}, false
	}
	return state.binding, true
}

func (state *requestIOAuthorityState) consumedBinding() (startRequestBinding, bool) {
	if state == nil || atomic.LoadUint32(&state.phase) != uint32(requestIOAuthorityConsumed) {
		return startRequestBinding{}, false
	}
	return state.binding, true
}

// RenewLeaseResponseContext supplies the authoritative stage state that is not
// present in a RENEWED response. Its private fields force callers to choose an
// explicit staged or unstaged constructor.
type RenewLeaseResponseContext struct {
	stageExpiresAtMS RedisMilliseconds
	hasActiveStage   bool
	initialized      bool
}

func NewUnstagedRenewLeaseResponseContext() RenewLeaseResponseContext {
	return RenewLeaseResponseContext{initialized: true}
}

func NewStagedRenewLeaseResponseContext(stageExpiresAtMS RedisMilliseconds) (RenewLeaseResponseContext, error) {
	if stageExpiresAtMS == 0 || uint64(stageExpiresAtMS) > MaxExactInteger {
		return RenewLeaseResponseContext{}, ErrInvalidRenewLeaseContext
	}
	return RenewLeaseResponseContext{
		stageExpiresAtMS: stageExpiresAtMS,
		hasActiveStage:   true,
		initialized:      true,
	}, nil
}
