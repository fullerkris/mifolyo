package crawljobsv2

import (
	"errors"
	"strings"
	"testing"
)

func TestReviewRetryScheduledResponseTimeSupportsExactReplay(t *testing.T) {
	const firstNow = uint64(1_000_000)
	firstDeadline := firstNow + RetryDelayAttempt1Milliseconds

	valid := []struct {
		name      string
		now       uint64
		notBefore uint64
		attempts  uint64
	}{
		{
			name: "first response",
			now:  firstNow, notBefore: firstDeadline, attempts: 1,
		},
		{
			name: "later replay before deadline",
			now:  firstNow + 10_000, notBefore: firstDeadline, attempts: 1,
		},
		{
			name: "replay after deadline",
			now:  firstDeadline + 1, notBefore: firstDeadline, attempts: 1,
		},
		{
			name: "second-delivery first response",
			now:  firstNow, notBefore: firstNow + RetryDelayAttempt2Milliseconds, attempts: 2,
		},
		{
			name:      "largest exact first response",
			now:       MaxExactInteger - RetryDelayAttempt1Milliseconds,
			notBefore: MaxExactInteger,
			attempts:  1,
		},
		{
			name: "largest exact deadline replay after now-plus-delay would overflow",
			now:  MaxExactInteger, notBefore: MaxExactInteger, attempts: 1,
		},
	}
	for _, testCase := range valid {
		t.Run(testCase.name, func(t *testing.T) {
			raw := []string{
				string(StatusRetryScheduled),
				canonicalDecimal(testCase.now),
				canonicalDecimal(testCase.notBefore),
				canonicalDecimal(testCase.attempts),
				string(ReasonRequestTimeout),
			}
			if err := ValidateOperationResponse(OperationRetry, raw); err != nil {
				t.Fatalf("valid retry response rejected: %v", err)
			}
		})
	}

	invalid := []struct {
		name      string
		now       string
		notBefore string
	}{
		{
			name:      "future transition disguised as deadline",
			now:       canonicalDecimal(firstNow),
			notBefore: canonicalDecimal(firstDeadline + 1),
		},
		{
			name:      "deadline subtraction underflow",
			now:       canonicalDecimal(firstNow),
			notBefore: canonicalDecimal(RetryDelayAttempt1Milliseconds - 1),
		},
		{
			name:      "impossible zero transition time",
			now:       canonicalDecimal(firstNow),
			notBefore: canonicalDecimal(RetryDelayAttempt1Milliseconds),
		},
		{
			name:      "maximum deadline still derived in the future",
			now:       canonicalDecimal(MaxExactInteger - RetryDelayAttempt1Milliseconds - 1),
			notBefore: canonicalDecimal(MaxExactInteger),
		},
		{
			name:      "deadline exceeds exact integer range",
			now:       canonicalDecimal(MaxExactInteger),
			notBefore: "9007199254740992",
		},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			raw := []string{
				string(StatusRetryScheduled), testCase.now, testCase.notBefore, "1", string(ReasonRequestTimeout),
			}
			if err := ValidateOperationResponse(OperationRetry, raw); !errors.Is(err, ErrResponseScalar) {
				t.Fatalf("invalid retry response error = %v", err)
			}
		})
	}
}

func TestReviewJobRecordAcceptsOnlyExactPostAbortBackpressureShape(t *testing.T) {
	record, _, _ := reviewPostAbortBackpressureJob(t)
	if err := ValidateRecord(SchemaJob, record); err != nil {
		t.Fatalf("exact immediate post-abort record rejected: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(Record)
	}{
		{
			name: "active reservation remains",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobActiveReservationIDIndex, strings.Repeat("5", 64))
			},
		},
		{
			name: "stage is still active despite abort status",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobActiveStageCommitIDIndex, string(record[jobLastStageCommitIDIndex].Value))
			},
		},
		{
			name: "last stage belongs to prior fence",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastStageFenceIndex, "0")
			},
		},
		{
			name: "current-fence document evidence is absent",
			mutate: func(record Record) {
				for _, index := range []int{
					jobLastDocumentRequestStartedAtMSIndex,
					jobLastDocumentRequestFenceIndex,
				} {
					recordAuthoritySet(record, index, "0")
				}
				for _, index := range []int{
					jobLastDocumentTargetURLIDIndex,
					jobLastDocumentTargetURLIndex,
					jobLastDocumentTargetDigestIndex,
				} {
					recordAuthoritySet(record, index, "")
				}
			},
		},
		{
			name: "abort transition reason is not none",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastReasonIndex, string(ReasonRequestTimeout))
			},
		},
		{
			name: "replay response status replaces stored abort status",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastTransitionStatusIndex, string(StatusExistsIdentical))
			},
		},
		{
			name: "abort transition identity is unrelated",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastTransitionIDIndex, strings.Repeat("f", 64))
			},
		},
		{
			name: "abort commit differs from transition identity",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastStageCommitIDIndex, strings.Repeat("7", 64))
			},
		},
		{
			name: "backpressure belongs to prior fence",
			mutate: func(record Record) {
				recordAuthoritySet(record, jobCommitBackpressureFenceIndex, "0")
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneRecord(record)
			mutation.mutate(candidate)
			if err := ValidateRecord(SchemaJob, candidate); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("invalid post-abort record error = %v", err)
			}
		})
	}
}

func TestReviewPostAbortRecordCanTakeLeaseEndingTransitions(t *testing.T) {
	postAbort, lease, _ := reviewPostAbortBackpressureJob(t)

	t.Run("retry", func(t *testing.T) {
		record := cloneRecord(postAbort)
		transitionID, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: lease, Reason: ReasonRequestTimeout})
		if err != nil {
			t.Fatal(err)
		}
		reviewClearLeaseAndBackpressure(record)
		recordAuthoritySet(record, jobStateIndex, "delayed")
		recordAuthoritySet(record, jobRetryCountIndex, "1")
		recordAuthoritySet(record, jobNotBeforeMSIndex, "30000")
		recordAuthoritySet(record, jobLastReasonIndex, string(ReasonRequestTimeout))
		recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonRequestTimeout))
		reviewSetJobTransition(record, transitionID, StatusRetryScheduled)
		if err := ValidateRecord(SchemaJob, record); err != nil {
			t.Fatalf("post-abort retry record rejected: %v", err)
		}
	})

	t.Run("completed without output", func(t *testing.T) {
		record := cloneRecord(postAbort)
		transitionID, err := DeriveCompleteNoOutputTransitionID(CompleteNoOutputTransitionInput{
			Lease: lease, Reason: ReasonAlreadyVisited,
		})
		if err != nil {
			t.Fatal(err)
		}
		reviewClearLeaseAndBackpressure(record)
		recordAuthoritySet(record, jobStateIndex, "completed")
		recordAuthoritySet(record, jobLastReasonIndex, string(ReasonAlreadyVisited))
		recordAuthoritySet(record, jobCompletedAtMSIndex, "200")
		reviewSetJobTransition(record, transitionID, StatusCompleted)
		if err := ValidateRecord(SchemaJob, record); err != nil {
			t.Fatalf("post-abort completion record rejected: %v", err)
		}
	})

	t.Run("dead", func(t *testing.T) {
		record := cloneRecord(postAbort)
		transitionID, err := DeriveDeadTransitionID(DeadTransitionInput{Lease: lease, Reason: ReasonHTTP4xx})
		if err != nil {
			t.Fatal(err)
		}
		reviewClearLeaseAndBackpressure(record)
		recordAuthoritySet(record, jobStateIndex, "dead")
		recordAuthoritySet(record, jobLastReasonIndex, string(ReasonHTTP4xx))
		recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonHTTP4xx))
		recordAuthoritySet(record, jobDeadAtMSIndex, "200")
		reviewSetJobTransition(record, transitionID, StatusDead)
		if err := ValidateRecord(SchemaJob, record); err != nil {
			t.Fatalf("post-abort dead record rejected: %v", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		record := cloneRecord(postAbort)
		transitionID, err := DeriveCancelJobTransitionID(CancelJobTransitionInput{Lease: lease, Reason: ReasonSourceCancelled})
		if err != nil {
			t.Fatal(err)
		}
		reviewClearLeaseAndBackpressure(record)
		recordAuthoritySet(record, jobStateIndex, "cancelled")
		recordAuthoritySet(record, jobLastReasonIndex, string(ReasonSourceCancelled))
		recordAuthoritySet(record, jobCancelledAtMSIndex, "200")
		reviewSetJobTransition(record, transitionID, StatusCancelled)
		if err := ValidateRecord(SchemaJob, record); err != nil {
			t.Fatalf("post-abort cancellation record rejected: %v", err)
		}
	})
}

func TestReviewTerminalJobReasonAndTransitionMatrix(t *testing.T) {
	directDead := recordAuthorityJobRecord(t, "dead")
	reviewSetJobTransition(directDead, Digest(strings.Repeat("b", 64)), StatusDead)
	retryExhausted := reviewRetryExhaustedJob(t)
	preIOExhausted := reviewPreIOExhaustedJob(t)
	completedNoOutput := recordAuthorityJobRecord(t, "completed")
	reviewSetJobTransition(completedNoOutput, Digest(strings.Repeat("c", 64)), StatusCompleted)
	visitedCompleted := recordAuthorityJobRecord(t, "completed")
	reviewSetJobTransition(visitedCompleted, Digest(strings.Repeat("d", 64)), StatusVisitedCompleted)
	published := recordAuthorityPublishedJobRecord(t)
	reviewSetJobTransition(published, Digest(string(published[jobCommitIDIndex].Value)), StatusCommitted)

	for name, record := range map[string]Record{
		"direct dead":                directDead,
		"retry exhausted":            retryExhausted,
		"pre-I/O recovery exhausted": preIOExhausted,
		"completed without output":   completedNoOutput,
		"visited completion":         visitedCompleted,
		"published completion":       published,
	} {
		t.Run("valid "+name, func(t *testing.T) {
			if err := ValidateRecord(SchemaJob, record); err != nil {
				t.Fatalf("valid terminal record rejected: %v", err)
			}
		})
	}

	for _, reason := range []Reason{ReasonAuthorizationExpired, ReasonOperatorCancelled, ReasonSourceCancelled} {
		t.Run("valid cancellation reason "+string(reason), func(t *testing.T) {
			record := recordAuthorityJobRecord(t, "cancelled")
			recordAuthoritySet(record, jobLastReasonIndex, string(reason))
			reviewSetJobTransition(record, Digest(strings.Repeat("e", 64)), StatusCancelled)
			if err := ValidateRecord(SchemaJob, record); err != nil {
				t.Fatalf("valid cancellation rejected: %v", err)
			}
		})
	}

	invalid := []struct {
		name   string
		base   Record
		mutate func(Record)
	}{
		{
			name: "dead job with cancellation reason", base: directDead,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastReasonIndex, string(ReasonOperatorCancelled))
				recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonOperatorCancelled))
			},
		},
		{
			name: "dead job with retryable disposition", base: directDead,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastReasonIndex, string(ReasonRequestTimeout))
				recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonRequestTimeout))
			},
		},
		{
			name: "direct dead reason and failure disagree", base: directDead,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonHTTP4xx))
			},
		},
		{
			name: "retry exhaustion has non-retryable failure", base: retryExhausted,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonHTTP4xx))
			},
		},
		{
			name: "retry exhaustion before third delivery", base: retryExhausted,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobDeliveryAttemptsIndex, "2")
				recordAuthoritySet(record, jobRequestStartsIndex, "2")
			},
		},
		{
			name: "cancelled job with dead-letter reason", base: recordAuthorityJobRecord(t, "cancelled"),
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastReasonIndex, string(ReasonPolicyDenied))
			},
		},
		{
			name: "cancelled job with no reason", base: recordAuthorityJobRecord(t, "cancelled"),
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastReasonIndex, string(ReasonNone))
			},
		},
		{
			name: "dead state with cancellation transition", base: directDead,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastTransitionStatusIndex, string(StatusCancelled))
			},
		},
		{
			name: "cancelled state with dead transition", base: recordAuthorityJobRecord(t, "cancelled"),
			mutate: func(record Record) {
				reviewSetJobTransition(record, Digest(strings.Repeat("f", 64)), StatusDead)
			},
		},
		{
			name: "no-output reason with commit transition", base: completedNoOutput,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastTransitionStatusIndex, string(StatusCommitted))
			},
		},
		{
			name: "published reason with no-output transition", base: published,
			mutate: func(record Record) {
				recordAuthoritySet(record, jobLastTransitionStatusIndex, string(StatusCompleted))
			},
		},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			record := cloneRecord(testCase.base)
			testCase.mutate(record)
			if err := ValidateRecord(SchemaJob, record); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("invalid terminal record error = %v", err)
			}
		})
	}
}

func TestReviewRunAndJobCountersHaveAbsoluteLifecycleBounds(t *testing.T) {
	t.Run("run counters", func(t *testing.T) {
		boundary := recordAuthorityRunRecord(t, "active")
		recordAuthoritySet(boundary, runRequestStartsIndex, "3")
		recordAuthoritySet(boundary, runReservationCreationsTotalIndex, "3")
		recordAuthoritySet(boundary, runClaimsTotalIndex, "3")
		recordAuthoritySet(boundary, runRetriesTotalIndex, "2")
		recordAuthoritySet(boundary, runRecoveredLeasesTotalIndex, "2")
		if err := ValidateRecord(SchemaRun, boundary); err != nil {
			t.Fatalf("claim/start boundary run rejected: %v", err)
		}

		mutations := []struct {
			name   string
			mutate func(Record)
		}{
			{
				name: "retries exceed claims",
				mutate: func(record Record) {
					recordAuthoritySet(record, runRequestStartsIndex, "4")
					recordAuthoritySet(record, runReservationCreationsTotalIndex, "4")
					recordAuthoritySet(record, runRetriesTotalIndex, "4")
				},
			},
			{
				name: "retries exceed request starts",
				mutate: func(record Record) {
					recordAuthoritySet(record, runRequestStartsIndex, "2")
					recordAuthoritySet(record, runReservationCreationsTotalIndex, "4")
					recordAuthoritySet(record, runClaimsTotalIndex, "4")
					recordAuthoritySet(record, runRetriesTotalIndex, "3")
					recordAuthoritySet(record, runRecoveredLeasesTotalIndex, "0")
				},
			},
			{
				name: "recovered leases exceed claims",
				mutate: func(record Record) {
					recordAuthoritySet(record, runRetriesTotalIndex, "0")
					recordAuthoritySet(record, runRecoveredLeasesTotalIndex, "4")
				},
			},
		}
		for _, mutation := range mutations {
			t.Run(mutation.name, func(t *testing.T) {
				record := cloneRecord(boundary)
				mutation.mutate(record)
				if err := ValidateRecord(SchemaRun, record); !errors.Is(err, ErrRecordRelation) {
					t.Fatalf("invalid run counters error = %v", err)
				}
			})
		}
	})

	t.Run("job retry count stops before terminal delivery", func(t *testing.T) {
		boundary := reviewRetryExhaustedJob(t)
		if err := ValidateRecord(SchemaJob, boundary); err != nil {
			t.Fatalf("two delayed retries before exhaustion rejected: %v", err)
		}
		recordAuthoritySet(boundary, jobRetryCountIndex, canonicalDecimal(MaxDeliveryAttempts))
		if err := ValidateRecord(SchemaJob, boundary); !errors.Is(err, ErrRecordRelation) {
			t.Fatalf("third delayed retry error = %v", err)
		}
	})

	t.Run("pre-I/O recovery exhaustion", func(t *testing.T) {
		boundary := reviewPreIOExhaustedJob(t)
		if err := ValidateRecord(SchemaJob, boundary); err != nil {
			t.Fatalf("exact pre-I/O exhaustion rejected: %v", err)
		}
		retainedFailure := cloneRecord(boundary)
		recordAuthoritySet(retainedFailure, jobLastFailureReasonIndex, string(ReasonRequestTimeout))
		if err := ValidateRecord(SchemaJob, retainedFailure); err != nil {
			t.Fatalf("pre-I/O exhaustion with retained failure evidence rejected: %v", err)
		}

		mutations := []struct {
			name   string
			mutate func(Record)
		}{
			{
				name: "above maximum",
				mutate: func(record Record) {
					recordAuthoritySet(record, jobClaimCountIndex, "4")
					recordAuthoritySet(record, jobLeaseFenceIndex, "4")
					recordAuthoritySet(record, jobPreIORecoveriesIndex, "4")
					recordAuthoritySet(record, jobNextRequestOrdinalIndex, "5")
				},
			},
			{
				name: "maximum without exhaustion reason",
				mutate: func(record Record) {
					recordAuthoritySet(record, jobLastReasonIndex, string(ReasonPolicyDenied))
					recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonPolicyDenied))
				},
			},
			{
				name: "exhaustion reason below maximum",
				mutate: func(record Record) {
					recordAuthoritySet(record, jobPreIORecoveriesIndex, "2")
				},
			},
			{
				name: "maximum in non-dead state",
				mutate: func(record Record) {
					recordAuthoritySet(record, jobStateIndex, "ready")
					recordAuthoritySet(record, jobDeadAtMSIndex, "0")
				},
			},
			{
				name: "pre-I/O exhaustion after delivery exhaustion",
				mutate: func(record Record) {
					recordAuthoritySet(record, jobClaimCountIndex, "6")
					recordAuthoritySet(record, jobDeliveryAttemptsIndex, "3")
					recordAuthoritySet(record, jobRequestStartsIndex, "3")
					recordAuthoritySet(record, jobNextRequestOrdinalIndex, "7")
					recordAuthoritySet(record, jobLastRequestStartedAtMSIndex, "150")
					recordAuthoritySet(record, jobLeaseFenceIndex, "6")
				},
			},
			{
				name: "pre-I/O exhaustion with non-failure evidence",
				mutate: func(record Record) {
					recordAuthoritySet(record, jobLastFailureReasonIndex, string(ReasonOperatorCancelled))
				},
			},
		}
		for _, mutation := range mutations {
			t.Run(mutation.name, func(t *testing.T) {
				record := cloneRecord(boundary)
				mutation.mutate(record)
				if err := ValidateRecord(SchemaJob, record); !errors.Is(err, ErrRecordRelation) {
					t.Fatalf("invalid pre-I/O recovery record error = %v", err)
				}
			})
		}
	})
}

func reviewPostAbortBackpressureJob(t *testing.T) (Record, LeaseIdentity, Digest) {
	t.Helper()
	record := recordAuthorityJobRecord(t, "leased")
	target := RequestTarget{
		URLID:        JobID(string(record[jobJobIDIndex].Value)),
		CanonicalURL: string(record[jobCanonicalURLIndex].Value),
	}
	targetDigest, err := DeriveTargetDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	lease := LeaseIdentity{
		RunID:   RunID(string(record[jobRunIDIndex].Value)),
		JobID:   JobID(string(record[jobJobIDIndex].Value)),
		OwnerID: OwnerID(string(record[jobLeaseOwnerIndex].Value)),
		Fence:   Fence(1),
		Token:   LeaseToken(string(record[jobLeaseTokenIndex].Value)),
	}
	commitID := Digest(strings.Repeat("6", 64))
	transitionID, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: lease, CommitID: commitID})
	if err != nil {
		t.Fatal(err)
	}
	recordAuthoritySet(record, jobActiveReservationIDIndex, "")
	recordAuthoritySet(record, jobLastDocumentRequestStartedAtMSIndex, "150")
	recordAuthoritySet(record, jobLastDocumentRequestFenceIndex, "1")
	recordAuthoritySet(record, jobLastDocumentTargetURLIDIndex, string(target.URLID))
	recordAuthoritySet(record, jobLastDocumentTargetURLIndex, target.CanonicalURL)
	recordAuthoritySet(record, jobLastDocumentTargetDigestIndex, string(targetDigest))
	recordAuthoritySet(record, jobLastStageCommitIDIndex, string(commitID))
	recordAuthoritySet(record, jobLastStageFenceIndex, "1")
	recordAuthoritySet(record, jobCommitBackpressureFenceIndex, "1")
	recordAuthoritySet(record, jobCommitBackpressureReasonIndex, "pages_queue_full")
	recordAuthoritySet(record, jobCommitBackpressureStartedAtMSIndex, "150")
	recordAuthoritySet(record, jobCommitBackpressureDeadlineMSIndex, "200")
	recordAuthoritySet(record, jobUpdatedAtMSIndex, "200")
	reviewSetJobTransition(record, transitionID, StatusStageAborted)
	return record, lease, commitID
}

func reviewClearLeaseAndBackpressure(record Record) {
	for _, change := range []struct {
		index int
		value string
	}{
		{jobLeaseOwnerIndex, ""},
		{jobLeaseTokenIndex, ""},
		{jobLeaseStartedAtMSIndex, "0"},
		{jobLeaseExpiresAtMSIndex, "0"},
		{jobLeaseDeliveryStartedIndex, "0"},
		{jobActiveReservationIDIndex, ""},
		{jobActiveStageCommitIDIndex, ""},
		{jobCommitBackpressureFenceIndex, "0"},
		{jobCommitBackpressureReasonIndex, "none"},
		{jobCommitBackpressureStartedAtMSIndex, "0"},
		{jobCommitBackpressureDeadlineMSIndex, "0"},
		{jobUpdatedAtMSIndex, "200"},
	} {
		recordAuthoritySet(record, change.index, change.value)
	}
}

func reviewSetJobTransition(record Record, transitionID Digest, status Status) {
	recordAuthoritySet(record, jobLastTransitionIDIndex, string(transitionID))
	recordAuthoritySet(record, jobLastTransitionStatusIndex, string(status))
}

func reviewRetryExhaustedJob(t *testing.T) Record {
	t.Helper()
	record := recordAuthorityJobRecord(t, "dead")
	for _, change := range []struct {
		index int
		value string
	}{
		{jobClaimCountIndex, "3"},
		{jobDeliveryAttemptsIndex, "3"},
		{jobRequestStartsIndex, "3"},
		{jobLeaseRequestStartsBaselineIndex, "2"},
		{jobRetryCountIndex, "2"},
		{jobNextRequestOrdinalIndex, "4"},
		{jobLastRequestStartedAtMSIndex, "150"},
		{jobLastReasonIndex, string(ReasonRetryExhausted)},
		{jobLastFailureReasonIndex, string(ReasonRequestTimeout)},
		{jobLeaseFenceIndex, "3"},
		{jobUpdatedAtMSIndex, "200"},
		{jobDeadAtMSIndex, "200"},
	} {
		recordAuthoritySet(record, change.index, change.value)
	}
	return record
}

func reviewPreIOExhaustedJob(t *testing.T) Record {
	t.Helper()
	record := recordAuthorityJobRecord(t, "dead")
	for _, change := range []struct {
		index int
		value string
	}{
		{jobClaimCountIndex, "3"},
		{jobPreIORecoveriesIndex, "3"},
		{jobNextRequestOrdinalIndex, "4"},
		{jobLastReasonIndex, string(ReasonPreIORecoveryExhausted)},
		{jobLastFailureReasonIndex, string(ReasonPreIORecoveryExhausted)},
		{jobLeaseFenceIndex, "3"},
		{jobUpdatedAtMSIndex, "200"},
		{jobDeadAtMSIndex, "200"},
	} {
		recordAuthoritySet(record, change.index, change.value)
	}
	return record
}
