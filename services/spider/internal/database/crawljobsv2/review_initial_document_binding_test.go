package crawljobsv2

import (
	"errors"
	"testing"
)

func TestTryClaimInitialDocumentBinding(t *testing.T) {
	tests := []struct {
		name                       string
		mutate                     func(*testing.T, *wireOracleFixture, *TryClaimTransitionInput)
		wantErr                    error
		standaloneReservationValid bool
	}{
		{
			name: "distinct same-origin URL",
			mutate: func(t *testing.T, fixture *wireOracleFixture, input *TryClaimTransitionInput) {
				target := wireOracleTarget(t, "https://example.com/another-document")
				input.InitialIntent.Target = target
				input.InitialIntent.Decision = reviewInitialIntentDecision(t, fixture, RequestDocument, target, fixture.job.Depth)
			},
			wantErr:                    ErrDigestInputMismatch,
			standaloneReservationValid: true,
		},
		{
			name: "wrong job ID",
			mutate: func(t *testing.T, _ *wireOracleFixture, input *TryClaimTransitionInput) {
				input.InitialIntent.Target.URLID = wireOracleTarget(t, "https://example.com/wrong-job-id").URLID
			},
			wantErr: ErrURLIdentityMismatch,
		},
		{
			name: "mismatched decision",
			mutate: func(_ *testing.T, _ *wireOracleFixture, input *TryClaimTransitionInput) {
				input.InitialIntent.Decision.Depth++
			},
			wantErr:                    ErrDigestInputMismatch,
			standaloneReservationValid: true,
		},
		{
			name:                       "valid document identity",
			standaloneReservationValid: true,
		},
		{
			name: "valid robots behavior",
			mutate: func(t *testing.T, fixture *wireOracleFixture, input *TryClaimTransitionInput) {
				target := wireOracleTarget(t, "https://example.com/robots.txt")
				input.InitialIntent.Target = target
				input.InitialIntent.Decision = reviewInitialIntentDecision(t, fixture, RequestRobots, target, fixture.job.Depth)
			},
			standaloneReservationValid: true,
		},
		{
			name: "robots reservation depth mismatch",
			mutate: func(t *testing.T, fixture *wireOracleFixture, input *TryClaimTransitionInput) {
				target := wireOracleTarget(t, "https://example.com/robots.txt")
				input.InitialIntent.Target = target
				input.InitialIntent.Decision = reviewInitialIntentDecision(t, fixture, RequestRobots, target, fixture.job.Depth+1)
			},
			wantErr:                    ErrDigestInputMismatch,
			standaloneReservationValid: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWireOracleFixture(t)
			input := TryClaimTransitionInput{
				Job:                fixture.job,
				Lease:              fixture.lease,
				ExpectedPriorFence: 1,
				InitialIntent:      fixture.intent,
			}
			if testCase.mutate != nil {
				testCase.mutate(t, fixture, &input)
			}
			if testCase.standaloneReservationValid {
				if _, err := DeriveReservationID(fixture.runPolicy, input.InitialIntent); err != nil {
					t.Fatalf("standalone reservation validation failed: %v", err)
				}
			}

			calls := []struct {
				name string
				call func() error
			}{
				{
					name: "transition derivation",
					call: func() error {
						_, err := DeriveTryClaimTransitionID(fixture.runPolicy, input)
						return err
					},
				},
				{
					name: "wire construction",
					call: func() error {
						gate := wireOracleGate(t, fixture, OperationTryClaim, wireOracleActive)
						_, err := NewTryClaimWireRequest(gate, fixture.runPolicy, input)
						return err
					},
				},
			}
			for _, call := range calls {
				t.Run(call.name, func(t *testing.T) {
					err := call.call()
					if testCase.wantErr == nil {
						if err != nil {
							t.Fatalf("valid initial intent rejected: %v", err)
						}
						return
					}
					if !errors.Is(err, testCase.wantErr) {
						t.Fatalf("error = %v, want %v", err, testCase.wantErr)
					}
				})
			}
		})
	}
}

func reviewInitialIntentDecision(
	t *testing.T,
	fixture *wireOracleFixture,
	kind RequestKind,
	target RequestTarget,
	depth uint64,
) PolicyDecision {
	t.Helper()
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind:       kind,
		Target:            target,
		Depth:             depth,
		GroupID:           fixture.job.GroupID,
		RateScopeID:       fixture.job.RateScopeID,
		GroupConcurrency:  fixture.job.Decision.GroupConcurrency,
		GroupIntervalMS:   fixture.job.Decision.GroupIntervalMS,
		OriginConcurrency: fixture.job.Decision.OriginConcurrency,
		OriginIntervalMS:  fixture.job.Decision.OriginIntervalMS,
	})
	if err != nil {
		t.Fatalf("construct %s initial-intent decision: %v", kind, err)
	}
	return decision
}
