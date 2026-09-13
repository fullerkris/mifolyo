package crawljobsv2

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

type protocolConstantExpectation struct {
	name          string
	got           any
	want          any
	documentation string
}

func TestProtocolConstantsMatchNormativeLiteralManifest(t *testing.T) {
	const expectedManifestEntries = 63

	manifest := []protocolConstantExpectation{
		{name: "ProtocolVersion", got: ProtocolVersion, want: 2, documentation: "sections 5 and 7: protocol_version=2"},
		{name: "MaxExactInteger", got: MaxExactInteger, want: uint64(9_007_199_254_740_991), documentation: "section 3: Lua exact-integer range"},

		{name: "LeaseTTLMilliseconds", got: LeaseTTLMilliseconds, want: uint64(60_000), documentation: "section 3: Lease TTL"},
		{name: "LeaseRenewalIntervalMilliseconds", got: LeaseRenewalIntervalMilliseconds, want: uint64(10_000), documentation: "section 3: Lease renewal interval"},
		{name: "MaxDeliveryAttempts", got: MaxDeliveryAttempts, want: uint64(3), documentation: "section 3: Maximum delivery attempts"},
		{name: "RetryDelayAttempt1Milliseconds", got: RetryDelayAttempt1Milliseconds, want: uint64(30_000), documentation: "section 3: Delay after failed delivery attempt 1"},
		{name: "RetryDelayAttempt2Milliseconds", got: RetryDelayAttempt2Milliseconds, want: uint64(120_000), documentation: "section 3: Delay after failed delivery attempt 2"},
		{name: "MaxPreIOExpiredLeaseRecoveries", got: MaxPreIOExpiredLeaseRecoveries, want: uint64(3), documentation: "section 3: Maximum pre-I/O expired-lease recoveries"},

		{name: "MaxRequestStartsPerRun", got: MaxRequestStartsPerRun, want: uint64(10), documentation: "section 3: Maximum request starts per run"},
		{name: "MaxReservationCreationsPerRun", got: MaxReservationCreationsPerRun, want: uint64(100), documentation: "sections 1 and 3: Maximum reservation creations per run"},
		{name: "MinRequestStartsPerGroup", got: MinRequestStartsPerGroup, want: uint64(1), documentation: "section 3: Minimum policy request starts per group"},
		{name: "MaxRequestStartsPerGroup", got: MaxRequestStartsPerGroup, want: uint64(10), documentation: "section 3: Maximum policy request starts per group"},
		{name: "GlobalActiveRequestLimit", got: GlobalActiveRequestLimit, want: uint64(2), documentation: "section 3: Current global active-request limit"},
		{name: "MaxScopeConcurrency", got: MaxScopeConcurrency, want: uint64(32), documentation: "section 3: Maximum supported scope concurrency"},
		{name: "MaxScopeIntervalMilliseconds", got: MaxScopeIntervalMilliseconds, want: uint64(3_600_000), documentation: "section 3: Maximum supported scope interval"},
		{name: "GlobalScopeIntervalMilliseconds", got: GlobalScopeIntervalMilliseconds, want: uint64(0), documentation: "section 3: Global scope interval"},
		{name: "MaxDurableRateScopes", got: MaxDurableRateScopes, want: uint64(100_000), documentation: "section 3: Maximum durable rate scopes"},

		{name: "MaxPolicyGroupsPerRun", got: MaxPolicyGroupsPerRun, want: 64, documentation: "section 3: Maximum policy groups per run"},
		{name: "MaxPolicyGroupIDBytes", got: MaxPolicyGroupIDBytes, want: 128, documentation: "section 3: Maximum policy group ID bytes"},
		{name: "MaxRenderPolicyRuleIDBytes", got: MaxRenderPolicyRuleIDBytes, want: 128, documentation: "sections 3 and 6.4: Maximum render-policy rule ID bytes"},
		{name: "MaxActiveLeases", got: MaxActiveLeases, want: 64, documentation: "section 3: Maximum active leases across all runs"},
		{name: "MaxJobsPerRun", got: MaxJobsPerRun, want: 10_000, documentation: "section 3: Maximum jobs per run"},
		{name: "MaxActiveRuns", got: MaxActiveRuns, want: 16, documentation: "section 3: Maximum active runs"},
		{name: "MaxUnarchivedRuns", got: MaxUnarchivedRuns, want: 100, documentation: "section 3: Maximum retained unarchived runs"},
		{name: "MaxRunRecordsPendingPurge", got: MaxRunRecordsPendingPurge, want: 128, documentation: "section 3: Maximum total run records pending purge"},

		{name: "MaxPagesQueueDepthBeforeCommit", got: MaxPagesQueueDepthBeforeCommit, want: 5_000, documentation: "sections 3 and 10.5: Maximum pages_queue depth before first commit"},
		{name: "FeederEnqueueBatchSize", got: FeederEnqueueBatchSize, want: 500, documentation: "section 3: Feeder enqueue batch"},
		{name: "RunAuditBatchSize", got: RunAuditBatchSize, want: 100, documentation: "section 3: Run audit batch"},
		{name: "ReadyMetadataPageSize", got: ReadyMetadataPageSize, want: 128, documentation: "sections 3 and 11: Ready metadata page"},
		{name: "MaintenanceBatchSize", got: MaintenanceBatchSize, want: 100, documentation: "sections 3 and 10.6: Maintenance batch"},

		{name: "MaxOutlinksPerJob", got: MaxOutlinksPerJob, want: 256, documentation: "sections 3 and 6.4: Maximum outlinks per job"},
		{name: "MaxDiscoveriesPerJob", got: MaxDiscoveriesPerJob, want: 128, documentation: "section 3: Maximum discoveries per job"},
		{name: "MaxImagesPerPage", got: MaxImagesPerPage, want: 64, documentation: "sections 3 and 6.4: Maximum images per page"},
		{name: "MaxAliasesPerJob", got: MaxAliasesPerJob, want: 5, documentation: "sections 3 and 4: Maximum document/effective aliases per job"},
		{name: "MaxCanonicalURLBytes", got: MaxCanonicalURLBytes, want: 2_048, documentation: "sections 3 and 6.4: Maximum canonical URL bytes"},
		{name: "MaxPageBlobBytes", got: MaxPageBlobBytes, want: 5_242_880, documentation: "sections 3 and 6.4: Maximum page or rendered DOM bytes"},
		{name: "MaxCombinedHTMLBytes", got: MaxCombinedHTMLBytes, want: 10_485_760, documentation: "sections 3 and 6.4: Maximum combined html and original_html bytes"},
		{name: "MaxImageAltBytes", got: MaxImageAltBytes, want: 1_024, documentation: "sections 3 and 6.4: Maximum image alt bytes"},
		{name: "MaxImageManifestBytes", got: MaxImageManifestBytes, want: 393_216, documentation: "sections 3 and 6.4: Maximum serialized image manifest bytes"},

		{name: "StageTTLMilliseconds", got: StageTTLMilliseconds, want: uint64(900_000), documentation: "sections 3, 7.5, and 10.5: Stage TTL"},
		{name: "MaxCommittedStageCleanupTTLMilliseconds", got: MaxCommittedStageCleanupTTLMilliseconds, want: uint64(60_000), documentation: "sections 3 and 10.5: Committed residual-stage cleanup TTL"},
		{name: "MaxCommitBackpressureMilliseconds", got: MaxCommitBackpressureMilliseconds, want: uint64(120_000), documentation: "sections 3, 7.2, and 10.5: Commit backpressure deadline"},
		{name: "CommitBackpressureBeforeStageExpiryMilliseconds", got: CommitBackpressureBeforeStageExpiryMilliseconds, want: uint64(10_000), documentation: "sections 3, 7.2, and 10.5: Backpressure deadline margin before stage expiry"},
		{name: "MaxStageSlots", got: MaxStageSlots, want: 4, documentation: "sections 1 through 3: Maximum simultaneous stage slots"},
		{name: "StageMemoryReservationBytes", got: StageMemoryReservationBytes, want: uint64(50_331_648), documentation: "sections 2.2 and 3: Stage memory reservation bytes"},
		{name: "StageControlReservationBytes", got: StageControlReservationBytes, want: uint64(65_536), documentation: "sections 2.2 and 3: Reserved stage-control bytes"},
		{name: "TerminalStageControlFloorBytes", got: TerminalStageControlFloorBytes, want: uint64(32_768), documentation: "sections 2.2 and 3: Terminal stage-control floor bytes"},
		{name: "CommitMemoryReservationBytes", got: CommitMemoryReservationBytes, want: uint64(67_108_864), documentation: "sections 2.2 and 3: Commit memory reservation bytes"},
		{name: "LeaseSafetyReservationBytes", got: LeaseSafetyReservationBytes, want: uint64(16_777_216), documentation: "sections 2.2 and 3: Lease safety reservation bytes"},
		{name: "MaxStageAggregateLogicalDataBytes", got: MaxStageAggregateLogicalDataBytes, want: uint64(14_680_064), documentation: "sections 3 and 10.5: Maximum stage aggregate logical data bytes"},
		{name: "MaxStageKeys", got: MaxStageKeys, want: 73, documentation: "sections 3, 6.3, and 10.5: Maximum stage keys"},
		{name: "ReservationTombstoneTTLSeconds", got: ReservationTombstoneTTLSeconds, want: uint64(86_400), documentation: "sections 3 and 7.3: Reservation tombstone TTL"},

		{name: "MaxAuthorizationWindowMilliseconds", got: MaxAuthorizationWindowMilliseconds, want: uint64(86_400_000), documentation: "sections 3 and 7.1: Maximum authorization window"},
		{name: "MinAuthorizationRemainingMilliseconds", got: MinAuthorizationRemainingMilliseconds, want: uint64(60_000), documentation: "section 3: Minimum authorization time remaining at activation"},
		{name: "MaxOrdinaryEvalSHARequestBytes", got: MaxOrdinaryEvalSHARequestBytes, want: 2_097_152, documentation: "section 3: Maximum ordinary serialized EVALSHA request bytes"},
		{name: "MaxPageBlobEvalSHARequestBytes", got: MaxPageBlobEvalSHARequestBytes, want: 5_373_952, documentation: "sections 3 and 10.5: Maximum page-blob EVALSHA request bytes"},
		{name: "MaxCommitEvalSHARequestBytes", got: MaxCommitEvalSHARequestBytes, want: 65_536, documentation: "sections 3 and 10.5: Maximum commit EVALSHA request bytes"},
		{name: "MaxNonBlobStageBatchRecords", got: MaxNonBlobStageBatchRecords, want: 64, documentation: "sections 3, 7.5, and 10.5: Maximum non-blob stage batch records"},
		{name: "MaxNonBlobStageBatchRequestBytes", got: MaxNonBlobStageBatchRequestBytes, want: 524_288, documentation: "sections 3 and 10.5: Maximum non-blob serialized stage request bytes"},
		{name: "MaxOutlinkChunks", got: MaxOutlinkChunks, want: 4, documentation: "sections 7.5 and 10.5: Maximum outlink chunks"},
		{name: "MaxDiscoveryChunks", got: MaxDiscoveryChunks, want: 2, documentation: "sections 7.5 and 10.5: Maximum discovery chunks"},
		{name: "FinalPageFieldCount", got: FinalPageFieldCount, want: 10, documentation: "sections 6.4 and 7.5: Exact final page field count"},
		{name: "MaxResponseEnvelopeScalars", got: MaxResponseEnvelopeScalars, want: 9, documentation: "section 9.1: Longest exact response array"},
	}

	if len(manifest) != expectedManifestEntries {
		t.Fatalf("protocol constant manifest has %d entries, want %d", len(manifest), expectedManifestEntries)
	}

	declared := protocolConstantNamesFromSource(t)
	manifestNames := make(map[string]struct{}, len(manifest))
	for _, expectation := range manifest {
		t.Run(expectation.name, func(t *testing.T) {
			if expectation.name == "" || expectation.documentation == "" {
				t.Fatal("manifest entries require a name and normative documentation location")
			}
			if _, duplicate := manifestNames[expectation.name]; duplicate {
				t.Fatalf("duplicate protocol constant manifest entry %q", expectation.name)
			}
			manifestNames[expectation.name] = struct{}{}
			if _, exists := declared[expectation.name]; !exists {
				t.Fatalf("manifest constant %q is not declared in constants.go", expectation.name)
			}
			if !reflect.DeepEqual(expectation.got, expectation.want) {
				t.Fatalf(
					"%s = %T(%v), want literal %T(%v) from %s",
					expectation.name,
					expectation.got,
					expectation.got,
					expectation.want,
					expectation.want,
					expectation.documentation,
				)
			}
		})
	}

	if len(declared) != len(manifestNames) {
		t.Errorf("constants.go declares %d constants, literal manifest covers %d", len(declared), len(manifestNames))
	}
	var unmanifested []string
	for name := range declared {
		if _, exists := manifestNames[name]; !exists {
			unmanifested = append(unmanifested, name)
		}
	}
	if len(unmanifested) != 0 {
		sort.Strings(unmanifested)
		t.Errorf("constants.go has constants absent from the literal manifest (including any string constants): %v", unmanifested)
	}

	t.Run("documented derived relations", testDocumentedProtocolConstantRelations)
	t.Logf("pinned %d normative protocol constants", len(manifest))
}

func protocolConstantNamesFromSource(t *testing.T) map[string]struct{} {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate protocol constant manifest test source")
	}
	constantsFile := filepath.Join(filepath.Dir(testFile), "constants.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), constantsFile, nil, 0)
	if err != nil {
		t.Fatalf("parse constants.go for manifest completeness: %v", err)
	}
	if parsed.Name.Name != "crawljobsv2" {
		t.Fatalf("constants.go package = %q, want crawljobsv2", parsed.Name.Name)
	}

	names := make(map[string]struct{})
	for _, declaration := range parsed.Decls {
		constantDeclaration, ok := declaration.(*ast.GenDecl)
		if !ok || constantDeclaration.Tok != token.CONST {
			continue
		}
		for _, specification := range constantDeclaration.Specs {
			valueSpecification, ok := specification.(*ast.ValueSpec)
			if !ok {
				t.Fatal("constants.go contains an unexpected const specification")
			}
			for _, identifier := range valueSpecification.Names {
				if _, duplicate := names[identifier.Name]; duplicate {
					t.Fatalf("constants.go declares duplicate constant %q", identifier.Name)
				}
				names[identifier.Name] = struct{}{}
			}
		}
	}
	return names
}

func testDocumentedProtocolConstantRelations(t *testing.T) {
	t.Helper()
	documentedLongestResponse := [...]string{
		"status",
		"now_ms",
		"commit_id",
		"chunk_kind",
		"chunk_ordinal",
		"accepted_records",
		"data_bytes",
		"key_count",
		"memory_reservation_remaining_bytes",
	}
	relations := []struct {
		name          string
		documentation string
		holds         bool
	}{
		{name: "maximum exact integer is 2^53-1", documentation: "section 3", holds: MaxExactInteger == (uint64(1)<<53)-1},
		{name: "lease TTL is 60 seconds", documentation: "sections 3 and 10.4", holds: LeaseTTLMilliseconds == 60*1_000},
		{name: "lease renewal interval is 10 seconds", documentation: "sections 3 and 11", holds: LeaseRenewalIntervalMilliseconds == 10*1_000},
		{name: "retry delay 1 is 30 seconds", documentation: "sections 3 and 10.6", holds: RetryDelayAttempt1Milliseconds == 30*1_000},
		{name: "retry delay 2 is 120 seconds", documentation: "sections 3 and 10.6", holds: RetryDelayAttempt2Milliseconds == 120*1_000},
		{name: "group request-start range is the documented 1 through run maximum", documentation: "sections 3 and 7.1", holds: MinRequestStartsPerGroup == 1 && MaxRequestStartsPerGroup == MaxRequestStartsPerRun},
		{name: "page blob limit is 5 MiB", documentation: "sections 3 and 6.4", holds: MaxPageBlobBytes == 5*1_024*1_024},
		{name: "combined HTML limit is 10 MiB", documentation: "sections 3 and 6.4", holds: MaxCombinedHTMLBytes == 10*1_024*1_024},
		{name: "combined HTML limit is two maximum page blobs", documentation: "sections 3 and 6.4", holds: MaxCombinedHTMLBytes == 2*MaxPageBlobBytes},
		{name: "image manifest limit is 384 KiB", documentation: "section 3", holds: MaxImageManifestBytes == 384*1_024},
		{name: "stage TTL is 15 minutes", documentation: "sections 3, 7.5, and 10.5", holds: StageTTLMilliseconds == 15*60*1_000},
		{name: "committed residual cleanup TTL is 60 seconds", documentation: "sections 3 and 10.5", holds: MaxCommittedStageCleanupTTLMilliseconds == 60*1_000},
		{name: "commit backpressure maximum is 120 seconds", documentation: "sections 3, 7.2, and 10.5", holds: MaxCommitBackpressureMilliseconds == 120*1_000},
		{name: "commit backpressure leaves 10 seconds before stage expiry", documentation: "sections 3, 7.2, and 10.5", holds: CommitBackpressureBeforeStageExpiryMilliseconds == 10*1_000},
		{name: "stage memory reservation is 48 MiB", documentation: "sections 2.2 and 3", holds: StageMemoryReservationBytes == 48*1_024*1_024},
		{name: "stage control reservation is 64 KiB", documentation: "sections 2.2 and 3", holds: StageControlReservationBytes == 64*1_024},
		{name: "terminal stage-control floor is 32 KiB", documentation: "sections 2.2 and 3", holds: TerminalStageControlFloorBytes == 32*1_024},
		{name: "commit memory reservation is 64 MiB", documentation: "sections 2.2 and 3", holds: CommitMemoryReservationBytes == 64*1_024*1_024},
		{name: "lease safety reservation is 16 MiB", documentation: "sections 2.2 and 3", holds: LeaseSafetyReservationBytes == 16*1_024*1_024},
		{name: "stage aggregate logical data limit is 14 MiB", documentation: "sections 3 and 10.5", holds: MaxStageAggregateLogicalDataBytes == 14*1_024*1_024},
		{name: "maximum stage key inventory matches the maximum documented shape", documentation: "sections 6.3 and 10.5", holds: MaxStageKeys == 2+1+1+3+1+MaxImagesPerPage+1},
		{name: "reservation tombstone TTL is 24 hours", documentation: "sections 3 and 7.3", holds: ReservationTombstoneTTLSeconds == 24*60*60},
		{name: "authorization window is 24 hours", documentation: "sections 3 and 7.1", holds: MaxAuthorizationWindowMilliseconds == 24*60*60*1_000},
		{name: "authorization and tombstone windows represent the same 24 hours", documentation: "sections 3, 7.1, and 7.3", holds: MaxAuthorizationWindowMilliseconds == ReservationTombstoneTTLSeconds*1_000},
		{name: "minimum activation authorization remainder is 60 seconds", documentation: "section 3", holds: MinAuthorizationRemainingMilliseconds == 60*1_000},
		{name: "ordinary EVALSHA request limit is 2 MiB", documentation: "section 3", holds: MaxOrdinaryEvalSHARequestBytes == 2*1_024*1_024},
		{name: "page-blob EVALSHA request limit is 5.125 MiB", documentation: "sections 3 and 10.5", holds: MaxPageBlobEvalSHARequestBytes == 5*1_024*1_024+128*1_024},
		{name: "commit EVALSHA request limit is 64 KiB", documentation: "sections 3 and 10.5", holds: MaxCommitEvalSHARequestBytes == 64*1_024},
		{name: "non-blob stage request limit is 512 KiB", documentation: "sections 3 and 10.5", holds: MaxNonBlobStageBatchRequestBytes == 512*1_024},
		{name: "outlink chunk count is the ceiling of 256 records in batches of 64", documentation: "sections 7.5 and 10.5", holds: MaxOutlinkChunks == (MaxOutlinksPerJob+MaxNonBlobStageBatchRecords-1)/MaxNonBlobStageBatchRecords},
		{name: "discovery chunk count is the ceiling of 128 records in batches of 64", documentation: "sections 7.5 and 10.5", holds: MaxDiscoveryChunks == (MaxDiscoveriesPerJob+MaxNonBlobStageBatchRecords-1)/MaxNonBlobStageBatchRecords},
		{name: "final page has eight non-blob and two blob fields", documentation: "sections 6.4, 7.5, and 10.5", holds: FinalPageFieldCount == 8+2},
		{name: "longest exact response has two envelope and seven operation scalars", documentation: "section 9.1", holds: MaxResponseEnvelopeScalars == len(documentedLongestResponse)},
	}

	for _, relation := range relations {
		t.Run(relation.name, func(t *testing.T) {
			if relation.documentation == "" {
				t.Fatal("derived relation requires a normative documentation location")
			}
			if !relation.holds {
				t.Fatalf("documented protocol constant relation from %s does not hold", relation.documentation)
			}
		})
	}
}
