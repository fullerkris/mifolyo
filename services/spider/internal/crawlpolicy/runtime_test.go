package crawlpolicy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeConcurrencyAndIdempotentRelease(t *testing.T) {
	group := strings.Replace(testGroupJSON("limited", "example.com", MatchExact), `"max_concurrency":2`, `"max_concurrency":1`, 1)
	runtime := decodeTestPolicy(t, policyJSON(group), "Bot").NewRuntime()

	release, err := runtime.Acquire(context.Background(), "limited")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	availability, err := runtime.Ready("limited", time.Now())
	if err != nil {
		t.Fatalf("Ready failed: %v", err)
	}
	if availability.Ready || availability.Reason != AvailabilityConcurrency || availability.ActiveRequests != 1 {
		t.Fatalf("unexpected active snapshot: %#v", availability)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := runtime.Acquire(ctx, "limited"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked Acquire error = %v, want context deadline", err)
	}

	release()
	release()
	availability, _ = runtime.Ready("limited", time.Now())
	if !availability.Ready || availability.ActiveRequests != 0 {
		t.Fatalf("release was not idempotent: %#v", availability)
	}

	secondRelease, err := runtime.Acquire(context.Background(), "limited")
	if err != nil {
		t.Fatalf("Acquire after release failed: %v", err)
	}
	secondRelease()
}

func TestRuntimeMinimumIntervalAndContextCancellation(t *testing.T) {
	group := strings.Replace(testGroupJSON("rate", "example.com", MatchExact), `"min_request_interval":"0s"`, `"min_request_interval":"10s"`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))

	release, err := runtime.Acquire(context.Background(), "rate")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	release()

	now := time.Now()
	availability, err := runtime.Ready("rate", now)
	if err != nil {
		t.Fatalf("Ready failed: %v", err)
	}
	if availability.Ready || availability.Reason != AvailabilityRate || availability.RetryAfter <= 0 {
		t.Fatalf("expected rate limit, got %#v", availability)
	}

	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := runtime.Acquire(shortContext, "rate"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rate wait error = %v, want context deadline", err)
	}

	readyAtDeadline, err := runtime.Ready("rate", availability.NextStart)
	if err != nil || !readyAtDeadline.Ready {
		t.Fatalf("deadline snapshot = %#v, %v", readyAtDeadline, err)
	}
	stillLimited, err := runtime.Ready("rate", time.Now())
	if err != nil || stillLimited.Reason != AvailabilityRate {
		t.Fatalf("future snapshot mutated live rate state: %#v, %v", stillLimited, err)
	}
}

func TestRuntimeBatchCapAndResetPreserveOtherState(t *testing.T) {
	group := testGroupJSON("batch", "example.com", MatchExact)
	group = strings.Replace(group, `"max_concurrency":2`, `"max_concurrency":1`, 1)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":1`, 1)
	group = strings.Replace(group, `"min_request_interval":"0s"`, `"min_request_interval":"1s"`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))

	release, err := runtime.Acquire(context.Background(), "batch")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	release()

	before, _ := runtime.Ready("batch", time.Now())
	if before.Reason != AvailabilityBatchLimit || before.RequestsInBatch != 1 {
		t.Fatalf("expected exhausted batch: %#v", before)
	}
	if _, err := runtime.Acquire(context.Background(), "batch"); !errors.Is(err, ErrBatchLimitReached) {
		t.Fatalf("Acquire at batch cap = %v", err)
	}

	runtime.ResetBatch()
	after, _ := runtime.Ready("batch", time.Now())
	if after.RequestsInBatch != 0 || after.Reason != AvailabilityRate {
		t.Fatalf("unexpected reset snapshot: %#v", after)
	}
	if !after.NextStart.Equal(before.NextStart) {
		t.Fatalf("ResetBatch changed next start: before=%s after=%s", before.NextStart, after.NextStart)
	}
	readyAtDeadline, _ := runtime.Ready("batch", after.NextStart)
	if !readyAtDeadline.Ready {
		t.Fatalf("group should be ready at preserved deadline: %#v", readyAtDeadline)
	}
}

func TestRuntimeResetPreservesActiveRequestCount(t *testing.T) {
	group := strings.Replace(testGroupJSON("active", "example.com", MatchExact), `"max_concurrency":2`, `"max_concurrency":1`, 1)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":1`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))

	release, err := runtime.Acquire(context.Background(), "active")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	runtime.ResetBatch()
	availability, _ := runtime.Ready("active", time.Now())
	if availability.ActiveRequests != 1 || availability.RequestsInBatch != 0 || availability.Reason != AvailabilityConcurrency {
		t.Fatalf("ResetBatch altered active capacity: %#v", availability)
	}
	release()
}

func TestRuntimeBatchCapCountsStartsAndErrors(t *testing.T) {
	group := strings.Replace(testGroupJSON("cap", "example.com", MatchExact), `"max_requests_per_batch":5`, `"max_requests_per_batch":2`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))
	for index := 0; index < 2; index++ {
		release, err := runtime.Acquire(context.Background(), "cap")
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", index, err)
		}
		release()
	}
	if _, err := runtime.Acquire(context.Background(), "cap"); !errors.Is(err, ErrBatchLimitReached) {
		t.Fatalf("third Acquire error = %v", err)
	}
	if _, err := runtime.Ready("missing", time.Now()); !errors.Is(err, ErrUnknownGroup) {
		t.Fatalf("unknown Ready error = %v", err)
	}
	if _, err := runtime.Acquire(context.Background(), "missing"); !errors.Is(err, ErrUnknownGroup) {
		t.Fatalf("unknown Acquire error = %v", err)
	}
	if _, err := runtime.Acquire(nil, "cap"); !errors.Is(err, ErrNilContext) {
		t.Fatalf("nil-context Acquire error = %v", err)
	}
}

func TestRuntimeRejectsDisabledGroup(t *testing.T) {
	group := strings.Replace(testGroupJSON("off", "example.com", MatchExact), `"enabled":true`, `"enabled":false`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))
	availability, err := runtime.Ready("off", time.Now())
	if err != nil || availability.Reason != AvailabilityDisabled || availability.Ready {
		t.Fatalf("disabled availability = %#v, %v", availability, err)
	}
	if _, err := runtime.Acquire(context.Background(), "off"); !errors.Is(err, ErrRuntimeGroupDisabled) {
		t.Fatalf("disabled Acquire error = %v", err)
	}
}

func TestRuntimeConcurrentAcquireNeverExceedsLimit(t *testing.T) {
	group := strings.Replace(testGroupJSON("parallel", "example.com", MatchExact), `"max_concurrency":2`, `"max_concurrency":3`, 1)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":20`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))

	var active atomic.Int32
	var maximum atomic.Int32
	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, 12)
	for index := 0; index < 12; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			release, err := runtime.Acquire(ctx, "parallel")
			if err != nil {
				errorsChannel <- err
				return
			}
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
			release()
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("parallel Acquire failed: %v", err)
	}
	if got := maximum.Load(); got > 3 {
		t.Fatalf("observed %d active requests with limit 3", got)
	}
}

func TestRuntimeTryAcquireDoesNotWaitOrOverReserve(t *testing.T) {
	group := testGroupJSON("runtime", "example.com", MatchExact)
	group = strings.Replace(group, `"max_concurrency":2`, `"max_concurrency":1`, 1)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":2`, 1)
	group = strings.Replace(group, `"min_request_interval":"0s"`, `"min_request_interval":"1h"`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))
	now := time.Now()

	release, availability, err := runtime.TryAcquire("runtime", now)
	if err != nil || release == nil || !availability.Ready {
		t.Fatalf("first TryAcquire ready=%v availability=%#v error=%v", release != nil, availability, err)
	}
	defer release()

	second, availability, err := runtime.TryAcquire("runtime", now)
	if err != nil || second != nil || availability.Reason != AvailabilityConcurrency {
		t.Fatalf("second TryAcquire ready=%v availability=%#v error=%v", second != nil, availability, err)
	}
}

func TestRuntimeReservationRefundRestoresConcurrencyBatchAndRate(t *testing.T) {
	group := testGroupJSON("refund", "example.com", MatchExact)
	group = strings.Replace(group, `"max_concurrency":2`, `"max_concurrency":1`, 1)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":1`, 1)
	group = strings.Replace(group, `"min_request_interval":"0s"`, `"min_request_interval":"1h"`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))
	now := time.Unix(1_700_000_000, 0)

	reservation, availability, err := runtime.TryReserve("refund", now)
	if err != nil || reservation == nil || !availability.Ready {
		t.Fatalf("TryReserve reservation=%v availability=%#v error=%v", reservation != nil, availability, err)
	}
	reserved, err := runtime.Ready("refund", now)
	if err != nil {
		t.Fatal(err)
	}
	if reserved.Reason != AvailabilityPending || reserved.ActiveRequests != 1 || reserved.RequestsInBatch != 1 || reserved.PendingReservations != 1 || !reserved.NextStart.IsZero() {
		t.Fatalf("reserved capacity = %#v", reserved)
	}

	reservation.Refund()
	reservation.Refund()
	after, err := runtime.Ready("refund", now)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Ready || after.ActiveRequests != 0 || after.RequestsInBatch != 0 || !after.NextStart.IsZero() {
		t.Fatalf("capacity after refund = %#v, want fully ready", after)
	}
	if _, err := reservation.Commit(); !errors.Is(err, ErrReservationFinalized) {
		t.Fatalf("Commit after Refund error = %v", err)
	}
}

func TestRuntimePendingReservationBlocksRateUntilFinalized(t *testing.T) {
	group := testGroupJSON("pending-rate", "example.com", MatchExact)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":3`, 1)
	group = strings.Replace(group, `"min_request_interval":"0s"`, `"min_request_interval":"100ms"`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))
	reservedAt := time.Now()

	first, _, err := runtime.TryReserve("pending-rate", reservedAt)
	if err != nil || first == nil {
		t.Fatalf("first reservation=%v error=%v", first != nil, err)
	}
	second, availability, err := runtime.TryReserve("pending-rate", reservedAt.Add(time.Hour))
	if err != nil || second != nil || availability.Reason != AvailabilityRate {
		t.Fatalf("second reservation=%v availability=%#v error=%v", second != nil, availability, err)
	}

	time.Sleep(20 * time.Millisecond)
	beforeCommit := time.Now()
	release, err := first.Commit()
	if err != nil {
		t.Fatalf("commit delayed reservation: %v", err)
	}
	defer release()
	afterCommit, err := runtime.Ready("pending-rate", beforeCommit)
	if err != nil {
		t.Fatal(err)
	}
	if afterCommit.Reason != AvailabilityRate || afterCommit.NextStart.Before(beforeCommit.Add(100*time.Millisecond)) {
		t.Fatalf("commit did not start a fresh rate window: %#v", afterCommit)
	}
}

func TestRuntimePendingBatchCapacityWaitsForRefund(t *testing.T) {
	group := testGroupJSON("pending-batch", "example.com", MatchExact)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":1`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))

	reservation, _, err := runtime.TryReserve("pending-batch", time.Now())
	if err != nil || reservation == nil {
		t.Fatalf("TryReserve reservation=%v error=%v", reservation != nil, err)
	}

	blockedContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := runtime.Acquire(blockedContext, "pending-batch"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire with provisional exhaustion error = %v, want context deadline", err)
	}

	reservation.Refund()
	release, err := runtime.Acquire(context.Background(), "pending-batch")
	if err != nil {
		t.Fatalf("Acquire after refund failed: %v", err)
	}
	release()
}

func TestRuntimeResetCarriesPendingReservationIntoNewBatch(t *testing.T) {
	group := testGroupJSON("pending-reset", "example.com", MatchExact)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":1`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))

	reservation, _, err := runtime.TryReserve("pending-reset", time.Now())
	if err != nil || reservation == nil {
		t.Fatalf("TryReserve reservation=%v error=%v", reservation != nil, err)
	}
	runtime.ResetBatch()

	release, err := reservation.Commit()
	if err != nil {
		t.Fatalf("commit after reset: %v", err)
	}
	defer release()
	availability, err := runtime.Ready("pending-reset", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if availability.Reason != AvailabilityBatchLimit || availability.RequestsInBatch != 1 || availability.PendingReservations != 0 {
		t.Fatalf("post-reset committed capacity = %#v", availability)
	}
}

func TestRuntimeResetThenRefundRestoresNewBatchCapacity(t *testing.T) {
	group := testGroupJSON("pending-reset-refund", "example.com", MatchExact)
	group = strings.Replace(group, `"max_requests_per_batch":5`, `"max_requests_per_batch":1`, 1)
	runtime := NewRuntime(decodeTestPolicy(t, policyJSON(group), "Bot"))

	reservation, _, err := runtime.TryReserve("pending-reset-refund", time.Now())
	if err != nil || reservation == nil {
		t.Fatalf("TryReserve reservation=%v error=%v", reservation != nil, err)
	}
	runtime.ResetBatch()
	reservation.Refund()

	availability, err := runtime.Ready("pending-reset-refund", time.Now())
	if err != nil || !availability.Ready || availability.RequestsInBatch != 0 || availability.PendingReservations != 0 {
		t.Fatalf("post-reset refunded capacity = %#v, %v", availability, err)
	}
}
