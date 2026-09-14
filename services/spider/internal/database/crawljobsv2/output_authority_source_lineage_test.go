package crawljobsv2

import (
	"errors"
	"strings"
	"testing"
)

type outputAuthoritySourceChain struct {
	transcript   DocumentTranscript
	witness      FinalDocumentWitness
	runPolicy    RunPolicyAuthority
	renderPolicy RenderPolicyAuthorization
	document     SuccessfulDocumentRequest
	redirect     SuccessfulDocumentRequest
	robots       SuccessfulDocumentRequest
}

func TestOutputAuthorityBindsSourceLineageAndAliasDepth(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	lease := vectorLease(t, fixture)
	chain := newOutputAuthoritySourceChain(t, source, source, lease, false)

	context, err := NewOutputContext(chain.runPolicy, source, chain.transcript, chain.witness, chain.renderPolicy)
	if err != nil {
		t.Fatalf("authenticated source context: %v", err)
	}
	if len(context.aliases) != 2 {
		t.Fatalf("alias count = %d, want 2", len(context.aliases))
	}
	for _, alias := range context.aliases {
		if alias.Depth != source.Depth {
			t.Fatalf("alias depth = %d, want authenticated depth %d", alias.Depth, source.Depth)
		}
	}

	otherGroup, err := ParseGroupID("stale-cross-run-group")
	if err != nil {
		t.Fatal(err)
	}
	otherRateScope, err := ParseRateScopeID(strings.Repeat("3", 32))
	if err != nil {
		t.Fatal(err)
	}
	otherTarget := mustFixtureTargetForURL(t, "https://stale-source.example.org/page")

	tests := []struct {
		name   string
		mutate func(SourceJob) SourceJob
		want   error
	}{
		{
			name: "altered depth with internally valid decision",
			mutate: func(candidate SourceJob) SourceJob {
				candidate.Depth++
				return rebindOutputAuthoritySourceDecision(t, candidate, candidate.Decision.GroupConcurrency, candidate.Decision.GroupIntervalMS)
			},
			want: ErrOutputContextMismatch,
		},
		{
			name: "altered group with internally valid decision",
			mutate: func(candidate SourceJob) SourceJob {
				candidate.GroupID = otherGroup
				return rebindOutputAuthoritySourceDecision(t, candidate, candidate.Decision.GroupConcurrency, candidate.Decision.GroupIntervalMS)
			},
			want: ErrPolicyGroupBindingMismatch,
		},
		{
			name: "altered rate scope with internally valid decision",
			mutate: func(candidate SourceJob) SourceJob {
				candidate.RateScopeID = otherRateScope
				return rebindOutputAuthoritySourceDecision(t, candidate, candidate.Decision.GroupConcurrency, candidate.Decision.GroupIntervalMS)
			},
			want: ErrPolicyGroupBindingMismatch,
		},
		{
			name: "altered full policy decision",
			mutate: func(candidate SourceJob) SourceJob {
				return rebindOutputAuthoritySourceDecision(t, candidate, 2, candidate.Decision.GroupIntervalMS+1)
			},
			want: ErrPolicyGroupBindingMismatch,
		},
		{
			name: "altered source target",
			mutate: func(candidate SourceJob) SourceJob {
				candidate.JobID = otherTarget.URLID
				candidate.CanonicalURL = otherTarget.CanonicalURL
				return rebindOutputAuthoritySourceDecision(t, candidate, candidate.Decision.GroupConcurrency, candidate.Decision.GroupIntervalMS)
			},
			want: ErrOutputContextMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := test.mutate(source)
			if _, err := sourceJobRecord(candidate); err != nil {
				t.Fatalf("mutated DTO is not independently valid: %v", err)
			}
			if _, err := NewOutputContext(chain.runPolicy, candidate, chain.transcript, chain.witness, chain.renderPolicy); !errors.Is(err, test.want) {
				t.Fatalf("stale source DTO error = %v", err)
			}
		})
	}
}

// TestOutputContextRejectsZeroSHA256AcrossAuthorityChain proves that the final
// output authority boundary revalidates every opaque layer instead of assuming
// that a once-valid RunPolicyAuthority, transcript, witness, or render-policy
// projection remained intact.
func TestOutputContextRejectsZeroSHA256AcrossAuthorityChain(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	chain := newOutputAuthoritySourceChain(t, source, source, vectorLease(t, fixture), false)
	if _, err := NewOutputContext(chain.runPolicy, source, chain.transcript, chain.witness, chain.renderPolicy); err != nil {
		t.Fatalf("valid output authority chain: %v", err)
	}
	zero := Digest(ZeroSHA256)

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "run authority crawl policy",
			call: func() error {
				runPolicy := chain.runPolicy
				runPolicy.binding.crawlPolicySHA256 = zero
				_, err := NewOutputContext(runPolicy, source, chain.transcript, chain.witness, chain.renderPolicy)
				return err
			},
		},
		{
			name: "run authority render policy",
			call: func() error {
				runPolicy := chain.runPolicy
				runPolicy.binding.renderPolicySHA256 = zero
				_, err := NewOutputContext(runPolicy, source, chain.transcript, chain.witness, chain.renderPolicy)
				return err
			},
		},
		{
			name: "run authority policy-group map",
			call: func() error {
				runPolicy := chain.runPolicy
				runPolicy.binding.policyGroupMapSHA256 = zero
				_, err := NewOutputContext(runPolicy, source, chain.transcript, chain.witness, chain.renderPolicy)
				return err
			},
		},
		{
			name: "source decision scope",
			call: func() error {
				candidate := source
				candidate.Decision.GroupScopeID = zero
				_, err := NewOutputContext(chain.runPolicy, candidate, chain.transcript, chain.witness, chain.renderPolicy)
				return err
			},
		},
		{
			name: "transcript run authority",
			call: func() error {
				transcript := chain.transcript
				transcript.runPolicy.binding.crawlPolicySHA256 = zero
				_, err := NewOutputContext(chain.runPolicy, source, transcript, chain.witness, chain.renderPolicy)
				return err
			},
		},
		{
			name: "authenticated request intent",
			call: func() error {
				transcript := chain.transcript
				transcript.requests = append([]SuccessfulDocumentRequest(nil), chain.transcript.requests...)
				request := transcript.requests[0]
				authority := *request.authority
				authority.binding.intent.CrawlPolicyDigest = zero
				request.authority = &authority
				transcript.requests[0] = request
				_, err := NewOutputContext(chain.runPolicy, source, transcript, chain.witness, chain.renderPolicy)
				return err
			},
		},
		{
			name: "final witness target digest",
			call: func() error {
				witness := chain.witness
				witness.targetDigest = zero
				_, err := NewOutputContext(chain.runPolicy, source, chain.transcript, witness, chain.renderPolicy)
				return err
			},
		},
		{
			name: "render-policy authorization digest",
			call: func() error {
				renderPolicy := chain.renderPolicy
				renderPolicy.digest = zero
				_, err := NewOutputContext(chain.runPolicy, source, chain.transcript, chain.witness, renderPolicy)
				return err
			},
		},
	}
	if len(tests) != 8 {
		t.Fatalf("output authority zero manifest = %d, want 8", len(tests))
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.call(); err == nil {
				t.Fatal("NewOutputContext accepted ZERO_SHA256 in its authority chain")
			}
		})
	}
}

func TestOutputAuthoritySupportsAuthenticatedRobotsThenDocumentAndRedirect(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	chain := newOutputAuthoritySourceChain(t, source, source, vectorLease(t, fixture), true)

	context, err := NewOutputContext(chain.runPolicy, source, chain.transcript, chain.witness, chain.renderPolicy)
	if err != nil {
		t.Fatalf("robots-first output context: %v", err)
	}
	if len(context.aliases) != 2 {
		t.Fatalf("robots request entered aliases: got %d aliases", len(context.aliases))
	}
	for _, alias := range context.aliases {
		if alias.URLID == chain.robots.target.URLID {
			t.Fatal("authenticated robots target was projected as an alias")
		}
		if alias.Depth != chain.document.authority.binding.intent.Decision.Depth {
			t.Fatal("redirect alias did not retain authenticated source depth")
		}
	}

	if _, err := chain.transcript.AppendSuccessfulRequest(chain.redirect); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("replayed redirect error = %v", err)
	}

	tampered := chain.transcript
	tampered.sourceDepth++
	if _, err := NewOutputContext(chain.runPolicy, source, tampered, chain.witness, chain.renderPolicy); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("tampered authenticated source snapshot error = %v", err)
	}
}

func TestOutputAuthorityRejectsCrossRunWitnessAndStaleDTOReplay(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	lease := vectorLease(t, fixture)
	chain := newOutputAuthoritySourceChain(t, source, source, lease, false)

	crossRunLease := lease
	var err error
	crossRunLease.RunID, err = ParseRunID(strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	crossRunChain := newOutputAuthoritySourceChain(t, source, source, crossRunLease, false)
	if _, err := NewOutputContext(chain.runPolicy, source, chain.transcript, crossRunChain.witness, chain.renderPolicy); !errors.Is(err, ErrDigestInputMismatch) && !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("cross-run witness error = %v", err)
	}

	staleSource := source
	staleSource.Depth++
	staleSource = rebindOutputAuthoritySourceDecision(t, staleSource, staleSource.Decision.GroupConcurrency, staleSource.Decision.GroupIntervalMS)
	staleCrossRunChain := newOutputAuthoritySourceChain(t, source, staleSource, crossRunLease, false)
	if _, err := NewOutputContext(staleCrossRunChain.runPolicy, source, staleCrossRunChain.transcript, staleCrossRunChain.witness, staleCrossRunChain.renderPolicy); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("source DTO replayed across run lineage error = %v", err)
	}
}

func TestOutputAuthorityRequiresOneRunIDAcrossEveryAuthority(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	chain := newOutputAuthoritySourceChain(t, source, source, vectorLease(t, fixture), false)
	context, err := NewOutputContext(chain.runPolicy, source, chain.transcript, chain.witness, chain.renderPolicy)
	if err != nil {
		t.Fatal(err)
	}

	otherRunID, err := ParseRunID(strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := chain.runPolicy.authenticatedBinding()
	if err != nil {
		t.Fatal(err)
	}
	artifact := testDenyAllRenderPolicyArtifact()
	otherRunPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, otherRunID, binding.crawlPolicySHA256, plainSHA256(artifact), vectorPolicyGroupsValue(t, fixture),
	)
	otherRenderPolicy, err := NewRenderPolicyAuthorization(otherRunPolicy, artifact)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewOutputContext(chain.runPolicy, source, chain.transcript, chain.witness, otherRenderPolicy); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("cross-run render authorization error = %v", err)
	}
	if _, err := NewOutputContext(otherRunPolicy, source, chain.transcript, chain.witness, otherRenderPolicy); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("cross-run run authority/lease error = %v", err)
	}
	differentCrawlPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, chain.transcript.lease.RunID, Digest(strings.Repeat("f", 64)), plainSHA256(artifact),
		vectorPolicyGroupsValue(t, fixture),
	)
	differentCrawlRenderPolicy, err := NewRenderPolicyAuthorization(differentCrawlPolicy, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputContext(
		differentCrawlPolicy, source, chain.transcript, chain.witness, differentCrawlRenderPolicy,
	); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("run-authenticated crawl-policy mismatch error = %v", err)
	}

	tamperedContext := context
	tamperedContext.runID = otherRunID
	if _, err := DeriveOutputDigest(tamperedContext, vectorOutputValue(t, fixture)); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("tampered output-context run ID error = %v", err)
	}

	tamperedWitness := chain.witness
	tamperedWitness.runID = otherRunID
	if _, err := NewOutputContext(chain.runPolicy, source, chain.transcript, tamperedWitness, chain.renderPolicy); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("tampered witness run ID error = %v", err)
	}
}

func TestOutputAuthorityRejectsRobotsWithDifferentSourceLineage(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	lease := vectorLease(t, fixture)
	policyDigest := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	runPolicy := vectorRunPolicyAuthority(t, fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
	robotsSource := source
	robotsSource.Depth++
	robotsSource = rebindOutputAuthoritySourceDecision(t, robotsSource, robotsSource.Decision.GroupConcurrency, robotsSource.Decision.GroupIntervalMS)
	robotsTarget := mustFixtureTargetForURL(t, "https://example.com/robots.txt")
	robots := newTestSuccessfulStartEvent(t, runPolicy, robotsSource, lease, policyDigest, RequestRobots, robotsTarget, 1, 1_788_266_090_000, 1, 1, 1)
	transcript, err := NewRequestTranscript(runPolicy, source, robots)
	if err != nil {
		t.Fatal(err)
	}
	documentTarget := RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL}
	document := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestDocument, documentTarget, 2, 1_788_266_091_000, 2, 2, 2)
	if _, err := transcript.AppendSuccessfulRequest(document); !errors.Is(err, ErrOutputContextMismatch) {
		t.Fatalf("cross-lineage robots error = %v", err)
	}
}

func TestFinalDocumentWitnessRejectsEqualMillisecondTranscriptPrefix(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	lease := vectorLease(t, fixture)
	crawlPolicySHA256 := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	artifact := testDenyAllRenderPolicyArtifact()
	runPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, lease.RunID, crawlPolicySHA256, plainSHA256(artifact), vectorPolicyGroupsValue(t, fixture),
	)
	sourceTarget := RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL}
	redirectTarget := mustFixtureTargetForURL(t, "https://equal-ms-redirect.example.org/hop")
	const sameRedisMillisecond = uint64(1_788_266_099_999)

	document := newTestSuccessfulStartEvent(
		t, runPolicy, source, lease, crawlPolicySHA256, RequestDocument, sourceTarget,
		1, sameRedisMillisecond, 1, 1, 1,
	)
	redirect := newTestSuccessfulStartEvent(
		t, runPolicy, source, lease, crawlPolicySHA256, RequestRedirect, redirectTarget,
		2, sameRedisMillisecond, 2, 2, 2,
	)
	returnToSource := newTestSuccessfulStartEvent(
		t, runPolicy, source, lease, crawlPolicySHA256, RequestRedirect, sourceTarget,
		3, sameRedisMillisecond, 3, 3, 3,
	)

	prefix, err := NewDocumentTranscript(runPolicy, source, document)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := prefix.AppendSuccessfulRequest(redirect)
	if err != nil {
		t.Fatal(err)
	}
	complete, err = complete.AppendSuccessfulRequest(returnToSource)
	if err != nil {
		t.Fatal(err)
	}
	sourceTargetSHA256, err := DeriveTargetDigest(sourceTarget)
	if err != nil {
		t.Fatal(err)
	}
	witnessValues := []string{
		canonicalDecimal(sameRedisMillisecond), canonicalDecimal(uint64(lease.Fence)),
		string(sourceTarget.URLID), sourceTarget.CanonicalURL, string(sourceTargetSHA256),
		"3", canonicalDecimal(sameRedisMillisecond),
		"0", "leased", string(lease.OwnerID), string(lease.Token), canonicalDecimal(uint64(lease.Fence)), "",
	}
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, witnessValues)
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(runPolicy, artifact)
	if err != nil {
		t.Fatal(err)
	}
	context, err := NewOutputContext(runPolicy, source, complete, witness, renderPolicy)
	if err != nil {
		t.Fatalf("complete source-to-redirect-to-source transcript: %v", err)
	}
	if context.finalTarget != sourceTarget || len(context.aliases) != 2 {
		t.Fatal("complete equal-millisecond transcript projected the wrong final target or aliases")
	}

	if _, err := NewOutputContext(runPolicy, source, prefix, witness, renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("stale transcript prefix error = %v", err)
	}
	if _, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, witnessValues[:5]); !errors.Is(err, ErrResponseArity) {
		t.Fatalf("legacy five-field witness error = %v", err)
	}
	staleTerminal := append([]string(nil), witnessValues...)
	staleTerminal[5] = "2"
	staleWitness, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, staleTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputContext(runPolicy, source, complete, staleWitness, renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("stale terminal request-start snapshot error = %v", err)
	}
}

func newOutputAuthoritySourceChain(
	t *testing.T,
	transcriptSource SourceJob,
	requestSource SourceJob,
	lease LeaseIdentity,
	robotsFirst bool,
) outputAuthoritySourceChain {
	t.Helper()
	fixture := loadDigestVectorFixture(t)
	policyDigest := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	artifact := testDenyAllRenderPolicyArtifact()
	chain := outputAuthoritySourceChain{}
	chain.runPolicy = newAuthenticatedTestRunPolicyAuthority(
		t, lease.RunID, policyDigest, plainSHA256(artifact), vectorPolicyGroupsValue(t, fixture),
	)
	var err error
	chain.renderPolicy, err = NewRenderPolicyAuthorization(chain.runPolicy, artifact)
	if err != nil {
		t.Fatalf("authorize source-chain render policy: %v", err)
	}

	nextOrdinal := uint64(1)
	nextCounter := uint64(1)
	if robotsFirst {
		robotsTarget := mustFixtureTargetForURL(t, "https://example.com/robots.txt")
		chain.robots = newTestSuccessfulStartEvent(
			t, chain.runPolicy, requestSource, lease, policyDigest, RequestRobots, robotsTarget,
			nextOrdinal, 1_788_266_090_000, nextCounter, nextCounter, nextCounter,
		)
		chain.transcript, err = NewRequestTranscript(chain.runPolicy, transcriptSource, chain.robots)
		if err != nil {
			t.Fatalf("start robots-first transcript: %v", err)
		}
		nextOrdinal++
		nextCounter++
	}

	documentTarget := RequestTarget{URLID: requestSource.JobID, CanonicalURL: requestSource.CanonicalURL}
	chain.document = newTestSuccessfulStartEvent(
		t, chain.runPolicy, requestSource, lease, policyDigest, RequestDocument, documentTarget,
		nextOrdinal, 1_788_266_091_000, nextCounter, nextCounter, nextCounter,
	)
	if robotsFirst {
		chain.transcript, err = chain.transcript.AppendSuccessfulRequest(chain.document)
	} else {
		chain.transcript, err = NewDocumentTranscript(chain.runPolicy, transcriptSource, chain.document)
	}
	if err != nil {
		t.Fatalf("bind authenticated source document: %v", err)
	}
	nextOrdinal++
	nextCounter++

	redirectTarget := vectorTargetValue(t, fixture, "target")
	chain.redirect = newTestSuccessfulStartEvent(
		t, chain.runPolicy, requestSource, lease, policyDigest, RequestRedirect, redirectTarget,
		nextOrdinal, 1_788_266_092_000, nextCounter, nextCounter, nextCounter,
	)
	chain.transcript, err = chain.transcript.AppendSuccessfulRequest(chain.redirect)
	if err != nil {
		t.Fatalf("append authenticated redirect: %v", err)
	}

	redirectDigest, err := DeriveTargetDigest(redirectTarget)
	if err != nil {
		t.Fatal(err)
	}
	chain.witness, err = newTestTransportAuthority().parseFinalDocumentWitness(lease, []string{
		canonicalDecimal(uint64(chain.redirect.redisStartedAtMS)), canonicalDecimal(uint64(lease.Fence)),
		string(redirectTarget.URLID), redirectTarget.CanonicalURL, string(redirectDigest),
		canonicalDecimal(chain.redirect.jobRequestStarts), canonicalDecimal(uint64(chain.redirect.redisStartedAtMS)),
		"0", "leased", string(lease.OwnerID), string(lease.Token), canonicalDecimal(uint64(lease.Fence)), "",
	})
	if err != nil {
		t.Fatalf("parse final redirect witness: %v", err)
	}
	return chain
}

func rebindOutputAuthoritySourceDecision(t testing.TB, source SourceJob, concurrency, intervalMS uint64) SourceJob {
	t.Helper()
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: RequestDocument,
		Target: RequestTarget{
			URLID:        source.JobID,
			CanonicalURL: source.CanonicalURL,
		},
		Depth:             source.Depth,
		GroupID:           source.GroupID,
		RateScopeID:       source.RateScopeID,
		GroupConcurrency:  concurrency,
		GroupIntervalMS:   intervalMS,
		OriginConcurrency: concurrency,
		OriginIntervalMS:  intervalMS,
	})
	if err != nil {
		t.Fatalf("rebind source policy decision: %v", err)
	}
	source.Decision = decision
	return source
}
