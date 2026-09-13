package crawljobsv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReviewStartReplaySeparatelyParsedResponsesShareOnePermit(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	for _, firstStatus := range []Status{StatusStarted, StatusAlreadyStarted} {
		for _, replayStatus := range []Status{StatusStarted, StatusAlreadyStarted} {
			t.Run(string(firstStatus)+"/"+string(replayStatus), func(t *testing.T) {
				transport := newReviewStartReplayTransportAuthority()
				first := fixture.raw(t, fixture.intent, firstStatus, true)
				response, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent, first)
				if err != nil {
					t.Fatal(err)
				}
				permit, err := response.IOPermit()
				if err != nil {
					t.Fatalf("first permit: %v", err)
				}
				transportCopy := transport
				replay := fixture.raw(t, fixture.intent, replayStatus, true)
				if replayStatus == StatusAlreadyStarted {
					replay[1] = "2000" // Invocation time is not an immutable start snapshot.
				}
				parsed, err := transportCopy.parseStartRequestResponse(fixture.runPolicy, fixture.intent, replay)
				if err != nil {
					t.Fatalf("exact replay: %v", err)
				}
				if _, err := parsed.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
					t.Fatalf("separately parsed replay issued a duplicate permit: %v", err)
				}
				evidence, err := NewSuccessfulRequest(permit)
				if err != nil {
					t.Fatalf("original permit consumption: %v", err)
				}
				parsed, err = transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent, replay)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := parsed.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
					t.Fatalf("consumed replay issued a duplicate permit: %v", err)
				}
				if err := validateSuccessfulDocumentRequest(evidence); err != nil {
					t.Fatalf("replay invalidated consumed evidence: %v", err)
				}
			})
		}
	}
}

func TestReviewStartReplayConcurrentParsesShareOnePermit(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	transport := newReviewStartReplayTransportAuthority()
	const workers = 32
	start := make(chan struct{})
	results := make(chan error, workers)
	permits := make(chan RequestIOPermit, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		status := StatusStarted
		if index%2 == 0 {
			status = StatusAlreadyStarted
		}
		raw := fixture.raw(t, fixture.intent, status, true)
		group.Add(1)
		go func(copy transportAuthority) {
			defer group.Done()
			<-start
			response, err := copy.parseStartRequestResponse(fixture.runPolicy, fixture.intent, raw)
			if err != nil {
				results <- err
				return
			}
			permit, err := response.IOPermit()
			if err == nil {
				permits <- permit
			} else if !errors.Is(err, ErrInvalidResponseAuthority) {
				results <- err
				return
			}
			results <- nil
		}(transport)
	}
	close(start)
	group.Wait()
	close(results)
	close(permits)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent exact parse: %v", err)
		}
	}
	if len(permits) != 1 {
		t.Fatalf("concurrent separately parsed replies issued %d permits, want 1", len(permits))
	}
	if _, err := NewSuccessfulRequest(<-permits); err != nil {
		t.Fatalf("winning permit consumption: %v", err)
	}
}

func TestReviewStartReplayKnownUnusedLostResponse(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	for _, parsedBeforeLoss := range []bool{false, true} {
		t.Run(fmt.Sprintf("parsed_before_loss_%t", parsedBeforeLoss), func(t *testing.T) {
			transport := newReviewStartReplayTransportAuthority()
			if parsedBeforeLoss {
				raw := fixture.raw(t, fixture.intent, StatusStarted, true)
				if _, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent, raw); err != nil {
					t.Fatal(err)
				}
				// Discard the response without issuing a permit. Mutation of the
				// caller's original array must not change the stored snapshot.
				raw[3] = "999"
			}
			// A newly authenticated run-record value with the same immutable
			// policy projection is not a new I/O session or a mismatch.
			reparsedPolicy := newAuthenticatedTestRunPolicyAuthority(t, fixture.intent.Lease.RunID,
				fixture.intent.CrawlPolicyDigest, plainSHA256(testDenyAllRenderPolicyArtifact()), fixture.groups)
			raw := fixture.raw(t, fixture.intent, StatusAlreadyStarted, true)
			raw[1] = "2000"
			response, err := transport.parseStartRequestResponse(reparsedPolicy, fixture.intent, raw)
			if err != nil {
				t.Fatalf("known-unused lost-response replay: %v", err)
			}
			permit, err := response.IOPermit()
			if err != nil {
				t.Fatalf("lost-response replay permit: %v", err)
			}
			if _, err := NewSuccessfulRequest(permit); err != nil {
				t.Fatal(err)
			}
			response, err = transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent, raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := response.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
				t.Fatalf("lost-response reconciliation reminted permission: %v", err)
			}
		})
	}
}

func TestReviewStartReplayRejectsImmutableMismatch(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	for _, permission := range []bool{false, true} {
		for _, mutation := range []struct {
			name  string
			index int
			value string
		}{
			{name: "started_at", index: 3, value: "1001"},
			{name: "delivery_attempts", index: 4, value: "2"},
			{name: "job_starts", index: 5, value: "2"},
			{name: "run_starts", index: 6, value: "4"},
			{name: "group_starts", index: 7, value: "2"},
		} {
			t.Run(fmt.Sprintf("%s/permission_%t", mutation.name, permission), func(t *testing.T) {
				transport := newReviewStartReplayTransportAuthority()
				firstRaw := fixture.raw(t, fixture.intent, StatusStarted, true)
				if mutation.name == "delivery_attempts" {
					firstRaw[5] = "2" // Both delivery snapshots must remain <= job starts.
				}
				original, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent, firstRaw)
				if err != nil {
					t.Fatal(err)
				}
				raw := fixture.raw(t, fixture.intent, StatusAlreadyStarted, permission)
				raw[5] = firstRaw[5]
				raw[1] = "2000"
				raw[mutation.index] = mutation.value
				// This is not merely a schema/scalar rejection: each mutated
				// response is independently valid without the first snapshot.
				if _, err := newReviewStartReplayTransportAuthority().parseStartRequestResponse(fixture.runPolicy, fixture.intent, raw); err != nil {
					t.Fatalf("mismatch fixture is not independently valid: %v", err)
				}
				response, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent, raw)
				if err != ErrInvalidResponseAuthority {
					t.Fatalf("mismatched immutable snapshot error = %v", err)
				}
				if _, err := response.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
					t.Fatal("rejected snapshot retained authority")
				}
				if _, err := original.IOPermit(); err != nil {
					t.Fatalf("rejected mismatch modified the original binding: %v", err)
				}
			})
		}
	}
	for _, mutation := range []string{"intent", "render_policy"} {
		t.Run(mutation, func(t *testing.T) {
			transport := newReviewStartReplayTransportAuthority()
			if _, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
				fixture.raw(t, fixture.intent, StatusStarted, true)); err != nil {
				t.Fatal(err)
			}
			changed := fixture
			if mutation == "intent" {
				changed.intent.Decision.Depth++
			} else {
				changed.runPolicy = newAuthenticatedTestRunPolicyAuthority(t, fixture.intent.Lease.RunID,
					fixture.intent.CrawlPolicyDigest, plainSHA256([]byte("different render artifact")), fixture.groups)
			}
			raw := changed.raw(t, changed.intent, StatusAlreadyStarted, true)
			if _, err := newReviewStartReplayTransportAuthority().parseStartRequestResponse(changed.runPolicy, changed.intent, raw); err != nil {
				t.Fatalf("changed intent/policy is not independently valid: %v", err)
			}
			if _, err := transport.parseStartRequestResponse(changed.runPolicy, changed.intent, raw); err != ErrInvalidResponseAuthority {
				t.Fatalf("same-ordinal intent/policy substitution error = %v", err)
			}
		})
	}
}

func TestReviewStartReplayUnknownHistoryFailsClosed(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	for _, status := range []Status{StatusStarted, StatusAlreadyStarted} {
		for _, history := range []string{"zero_transport", "seal_only", "unknown_session", "unknown_phase"} {
			t.Run(string(status)+"/"+history, func(t *testing.T) {
				transport := newTransportAuthority()
				switch history {
				case "zero_transport":
					transport = transportAuthority{}
				case "unknown_session":
					transport.requestIOSession = &requestIOAuthoritySession{}
				case "unknown_phase":
					transport = newReviewStartReplayTransportAuthority()
					response, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
						fixture.raw(t, fixture.intent, StatusStarted, true))
					if err != nil {
						t.Fatal(err)
					}
					atomic.StoreUint32(&response.authority.phase, 0)
					if _, err := response.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
						t.Fatal("unknown local phase issued a permit")
					}
				}
				response, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
					fixture.raw(t, fixture.intent, status, true))
				if err != ErrInvalidResponseAuthority {
					t.Fatalf("unknown local history error = %v", err)
				}
				if _, err := response.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
					t.Fatal("unknown local history issued a permit")
				}
				if history != "zero_transport" {
					reconciled, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
						fixture.raw(t, fixture.intent, StatusAlreadyStarted, false))
					if err != nil {
						t.Fatalf("unknown-history tombstone reconciliation: %v", err)
					}
					if _, err := reconciled.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
						t.Fatal("unknown-history tombstone issued authority")
					}
					if _, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
						fixture.raw(t, fixture.intent, status, true)); err != ErrInvalidResponseAuthority {
						t.Fatalf("tombstone initialized unknown local authority: %v", err)
					}
				}
			})
		}
	}
}

func TestReviewStartReplayTombstoneDoesNotReviveAuthorityOrInvalidateConsumedContext(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	for _, phase := range []string{"unseen", "available", "issued", "consumed"} {
		t.Run(phase, func(t *testing.T) {
			transport := newReviewStartReplayTransportAuthority()
			var original StartRequestResponse
			var permit RequestIOPermit
			var evidence SuccessfulDocumentRequest
			var consumedContext OutputContext
			var err error
			if phase != "unseen" {
				original, err = transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
					fixture.raw(t, fixture.intent, StatusStarted, true))
				if err != nil {
					t.Fatal(err)
				}
			}
			if phase == "issued" || phase == "consumed" {
				permit, err = original.IOPermit()
				if err != nil {
					t.Fatal(err)
				}
			}
			if phase == "consumed" {
				evidence, err = NewSuccessfulRequest(permit)
				if err != nil {
					t.Fatal(err)
				}
				consumedContext = fixture.assertConsumedOutputContext(t, transport, evidence)
			}
			tombstone := fixture.raw(t, fixture.intent, StatusAlreadyStarted, false)
			tombstone[1] = "2000"
			for retry := 0; retry < 2; retry++ {
				response, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent, tombstone)
				if err != nil {
					t.Fatalf("tombstone reconciliation: %v", err)
				}
				details, ok := response.StartedDetails()
				if !ok || details.IOPermission() || response.authority != nil {
					t.Fatal("tombstone gained authority")
				}
				if _, err := response.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
					t.Fatal("tombstone issued a permit")
				}
			}
			if _, err := original.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
				t.Fatal("older response remained usable after tombstone")
			}
			if _, err := NewSuccessfulRequest(permit); !errors.Is(err, ErrInvalidResponseAuthority) {
				t.Fatal("unconsumed/reused permit remained usable after tombstone")
			}
			for _, status := range []Status{StatusStarted, StatusAlreadyStarted} {
				response, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
					fixture.raw(t, fixture.intent, status, true))
				if phase == "consumed" {
					if err != nil {
						t.Fatalf("consumed exact replay: %v", err)
					}
				} else if err != ErrInvalidResponseAuthority {
					t.Fatalf("delayed pre-tombstone reply revived authority: %v", err)
				}
				if _, err := response.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
					t.Fatal("delayed reply issued I/O")
				}
			}
			if phase == "consumed" {
				fixture.assertConsumedOutputContext(t, transport, evidence)
				if err := validateOutputContextAuthority(consumedContext); err != nil {
					t.Fatalf("replay invalidated the previously consumed output context: %v", err)
				}
			}
		})
	}
}

func TestReviewStartReplayNewReservationsAndBoundedRetention(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	transport := newReviewStartReplayTransportAuthority()
	for _, ordinal := range []uint64{fixture.intent.RequestOrdinal, MaxReservationCreationsPerRun} {
		intent := fixture.intent
		intent.RequestOrdinal = ordinal
		response, err := transport.parseStartRequestResponse(fixture.runPolicy, intent,
			fixture.raw(t, intent, StatusStarted, true))
		if err != nil {
			t.Fatal(err)
		}
		permit, err := response.IOPermit()
		if err != nil {
			t.Fatalf("new reservation blocked by previous consumption: %v", err)
		}
		if _, err := NewSuccessfulRequest(permit); err != nil {
			t.Fatal(err)
		}
	}
	// Stress the retention bound with scalar-valid snapshots, not a claim
	// that a real run may exceed its separate ten-request-start budget.
	for ordinal := uint64(1); ordinal <= MaxReservationCreationsPerRun; ordinal++ {
		intent := fixture.intent
		intent.RequestOrdinal = ordinal
		if _, err := transport.parseStartRequestResponse(fixture.runPolicy, intent,
			fixture.raw(t, intent, StatusAlreadyStarted, false)); err != nil {
			t.Fatalf("bounded tombstone retention: %v", err)
		}
	}
	for _, slot := range transport.requestIOSession.reservations {
		if slot == nil {
			t.Fatal("registry evicted a reservation before session end")
		}
	}
	for _, ordinal := range []uint64{0, MaxReservationCreationsPerRun + 1} {
		intent := fixture.intent
		intent.RequestOrdinal = ordinal
		if _, err := transport.parseStartRequestResponse(fixture.runPolicy, intent,
			fixture.raw(t, fixture.intent, StatusStarted, true)); err == nil {
			t.Fatal("out-of-bounds reservation ordinal accepted")
		}
	}
	response, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
		fixture.raw(t, fixture.intent, StatusAlreadyStarted, true))
	if err != nil {
		t.Fatalf("retained consumed reservation: %v", err)
	}
	if _, err := response.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatal("full registry reminted authority by eviction")
	}
}

func TestReviewStartReplaySessionCannotRebindAndIndependentLeasesRemainUsable(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	for _, field := range []string{"run", "job", "owner", "fence", "token"} {
		t.Run(field, func(t *testing.T) {
			transport := newReviewStartReplayTransportAuthority()
			if _, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
				fixture.raw(t, fixture.intent, StatusStarted, true)); err != nil {
				t.Fatal(err)
			}
			other := fixture
			switch field {
			case "run":
				other.intent.Lease.RunID = RunID(strings.Repeat("5", 32))
				other.runPolicy = newAuthenticatedTestRunPolicyAuthority(t, other.intent.Lease.RunID,
					other.intent.CrawlPolicyDigest, plainSHA256(testDenyAllRenderPolicyArtifact()), other.groups)
			case "job":
				other.intent.Lease.JobID = mustFixtureTargetForURL(t, "https://start-replay.example.org/other-job").URLID
			case "owner":
				other.intent.Lease.OwnerID = OwnerID(strings.Repeat("5", 32))
			case "fence":
				other.intent.Lease.Fence++
			case "token":
				other.intent.Lease.Token = LeaseToken(strings.Repeat("5", 64))
			}
			raw := other.raw(t, other.intent, StatusAlreadyStarted, true)
			if _, err := transport.parseStartRequestResponse(other.runPolicy, other.intent, raw); err != ErrInvalidResponseAuthority {
				t.Fatalf("lease session rebound to another identity: %v", err)
			}
			independent := newReviewStartReplayTransportAuthority()
			response, err := independent.parseStartRequestResponse(other.runPolicy, other.intent, raw)
			if err != nil {
				t.Fatalf("independent actual test lease rejected: %v", err)
			}
			if _, err := response.IOPermit(); err != nil {
				t.Fatalf("independent test lease inherited another session's history: %v", err)
			}
		})
	}
}

func TestReviewStartReplaySessionDiagnosticsAreRedacted(t *testing.T) {
	fixture := newReviewStartReplayFixture(t)
	transport := newReviewStartReplayTransportAuthority()
	if _, err := transport.parseStartRequestResponse(fixture.runPolicy, fixture.intent,
		fixture.raw(t, fixture.intent, StatusStarted, true)); err != nil {
		t.Fatal(err)
	}
	session := transport.requestIOSession
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	text, err := session.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("test session", "authority", session)
	for _, surface := range []string{fmt.Sprintf("%+v %#v %q", session, session, session), string(encoded), string(text), logs.String()} {
		for _, secret := range []string{string(fixture.intent.Lease.Token), fixture.intent.Target.CanonicalURL,
			string(fixture.intent.Lease.RunID), string(fixture.intent.CrawlPolicyDigest)} {
			if strings.Contains(surface, secret) {
				t.Fatal("session diagnostics leaked sensitive authority data")
			}
		}
	}
}

type reviewStartReplayFixture struct {
	runPolicy RunPolicyAuthority
	intent    ReservationIntent
	source    SourceJob
	groups    []PolicyGroup
}

// Test-only assertion of a genuinely fresh local lease session. Replays and
// reconnects must copy/reuse this authority, never call this helper again.
func newReviewStartReplayTransportAuthority() transportAuthority {
	authority := newTransportAuthority()
	authority.requestIOSession = &requestIOAuthoritySession{knownUnused: true}
	return authority
}

func newReviewStartReplayFixture(t *testing.T) reviewStartReplayFixture {
	t.Helper()
	target := mustFixtureTargetForURL(t, "https://start-replay.example.org/source")
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: RequestDocument, Target: target, Depth: 1,
		GroupID: GroupID("start-replay"), RateScopeID: RateScopeID(strings.Repeat("2", 32)),
		GroupConcurrency: 1, GroupIntervalMS: 0, OriginConcurrency: 1, OriginIntervalMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent := ReservationIntent{
		Lease: LeaseIdentity{
			RunID: RunID(strings.Repeat("1", 32)), JobID: target.URLID,
			OwnerID: OwnerID(strings.Repeat("3", 32)), Fence: Fence(2), Token: LeaseToken(strings.Repeat("4", 64)),
		},
		RequestOrdinal: 2, Target: target, CrawlPolicyDigest: plainSHA256([]byte("start replay crawl policy")), Decision: decision,
	}
	groups := []PolicyGroup{{
		GroupID: decision.GroupID, RateScopeID: decision.RateScopeID, GroupScopeID: decision.GroupScopeID,
		RequestStartLimit: MaxRequestStartsPerGroup, Concurrency: decision.GroupConcurrency, IntervalMS: decision.GroupIntervalMS,
	}}
	runPolicy := newAuthenticatedTestRunPolicyAuthority(t, intent.Lease.RunID, intent.CrawlPolicyDigest,
		plainSHA256(testDenyAllRenderPolicyArtifact()), groups)
	return reviewStartReplayFixture{
		runPolicy: runPolicy, intent: intent, groups: groups,
		source: SourceJob{
			JobID: target.URLID, CanonicalURL: target.CanonicalURL, ScoreText: ScoreText("1"), Depth: decision.Depth,
			GroupID: decision.GroupID, RateScopeID: decision.RateScopeID, Decision: decision,
		},
	}
}

func (fixture reviewStartReplayFixture) raw(t *testing.T, intent ReservationIntent, status Status, permission bool) []string {
	t.Helper()
	reservationID, err := DeriveReservationID(fixture.runPolicy, intent)
	if err != nil {
		t.Fatal(err)
	}
	bit := "0"
	if permission {
		bit = "1"
	}
	return []string{string(status), "1000", string(reservationID), "1000", "1", "1", "3", "3", bit}
}

func (fixture reviewStartReplayFixture) assertConsumedOutputContext(t *testing.T, transport transportAuthority, evidence SuccessfulDocumentRequest) OutputContext {
	t.Helper()
	transcript, err := NewDocumentTranscript(fixture.runPolicy, fixture.source, evidence)
	if err != nil {
		t.Fatalf("consumed transcript after replay: %v", err)
	}
	targetDigest, err := DeriveTargetDigest(fixture.intent.Target)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := transport.parseFinalDocumentWitness(fixture.intent.Lease, []string{
		"1000", canonicalDecimal(uint64(fixture.intent.Lease.Fence)), string(fixture.intent.Target.URLID),
		fixture.intent.Target.CanonicalURL, string(targetDigest), "1", "1000",
		"0", "leased", string(fixture.intent.Lease.OwnerID), string(fixture.intent.Lease.Token),
		canonicalDecimal(uint64(fixture.intent.Lease.Fence)), "",
	})
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(fixture.runPolicy, testDenyAllRenderPolicyArtifact())
	if err != nil {
		t.Fatal(err)
	}
	context, err := NewOutputContext(fixture.runPolicy, fixture.source, transcript, witness, renderPolicy)
	if err != nil {
		t.Fatalf("consumed output context after replay: %v", err)
	}
	if err := validateOutputContextAuthority(context); err != nil {
		t.Fatalf("replay invalidated consumed output authority: %v", err)
	}
	return context
}
