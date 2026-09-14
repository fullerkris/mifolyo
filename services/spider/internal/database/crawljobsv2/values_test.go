package crawljobsv2

import (
	"errors"
	"math"
	"strings"
	"testing"
)

const (
	testRunID       = RunID("00112233445566778899aabbccddeeff")
	testOwnerID     = OwnerID("ffeeddccbbaa99887766554433221100")
	testPageURL     = "https://example.com/page"
	testPageJobID   = JobID("e73f29cb21ebd90c5b8d31b650169d9154d3ab942bc3dd67a456fe9feddc5f9a")
	testTargetURL   = "https://example.com/target"
	testTargetJobID = JobID("3219b3cc346ff852a87a672323789b7132dc0a7587103d8300d5b17c6ec4f841")
	testToken       = LeaseToken("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	testRateScopeID = RateScopeID("11111111111111111111111111111111")
)

func TestProtocolConstants(t *testing.T) {
	if LeaseTTLMilliseconds != 60_000 || LeaseRenewalIntervalMilliseconds != 10_000 {
		t.Fatal("lease constants changed")
	}
	if MaxDeliveryAttempts != 3 || RetryDelayAttempt1Milliseconds != 30_000 || RetryDelayAttempt2Milliseconds != 120_000 || MaxPreIOExpiredLeaseRecoveries != 3 {
		t.Fatal("retry constants changed")
	}
	if MaxRequestStartsPerRun != 10 || MaxReservationCreationsPerRun != 100 || GlobalActiveRequestLimit != 2 || MaxScopeConcurrency != 32 || MaxScopeIntervalMilliseconds != 3_600_000 {
		t.Fatal("request constants changed")
	}
	if MaxJobsPerRun != 10_000 || MaxActiveRuns != 16 || MaxUnarchivedRuns != 100 || MaxRunRecordsPendingPurge != 128 {
		t.Fatal("run constants changed")
	}
	if MaxStageSlots != 4 || StageTTLMilliseconds != 900_000 || StageMemoryReservationBytes != 50_331_648 || MaxStageAggregateLogicalDataBytes != 14_680_064 || MaxStageKeys != 73 {
		t.Fatal("stage constants changed")
	}
	if MaxOutlinksPerJob != 256 || MaxDiscoveriesPerJob != 128 || MaxImagesPerPage != 64 || MaxAliasesPerJob != 5 {
		t.Fatal("fanout constants changed")
	}
}

func TestIdentifierValidation(t *testing.T) {
	if _, err := ParseRunID(string(testRunID)); err != nil {
		t.Fatalf("valid run ID: %v", err)
	}
	if _, err := ParseJobID(string(testPageJobID)); err != nil {
		t.Fatalf("valid job ID: %v", err)
	}
	if _, err := ParseOwnerID(string(testOwnerID)); err != nil {
		t.Fatalf("valid owner ID: %v", err)
	}
	if _, err := ParseLeaseToken(string(testToken)); err != nil {
		t.Fatalf("valid token: %v", err)
	}
	if _, err := ParseDigest(strings.Repeat("a", 64)); err != nil {
		t.Fatalf("valid digest: %v", err)
	}
	if _, err := ParseRateScopeID(string(testRateScopeID)); err != nil {
		t.Fatalf("valid rate lineage: %v", err)
	}

	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "run length", err: parseRunError("abc"), want: ErrInvalidRunID},
		{name: "run uppercase", err: parseRunError("00112233445566778899AABBCCDDEEFF"), want: ErrInvalidRunID},
		{name: "job separator", err: parseJobError(strings.Repeat("a", 63) + ":"), want: ErrInvalidJobID},
		{name: "owner uppercase", err: parseOwnerError(strings.Repeat("F", 32)), want: ErrInvalidOwnerID},
		{name: "token short", err: parseTokenError(strings.Repeat("a", 63)), want: ErrInvalidLeaseToken},
		{name: "digest uppercase", err: parseDigestError(strings.Repeat("A", 64)), want: ErrInvalidDigest},
		{name: "rate long", err: parseRateError(strings.Repeat("a", 33)), want: ErrInvalidRateScopeID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, test.want) {
				t.Fatalf("error = %v, want %v", test.err, test.want)
			}
		})
	}
}

func TestLexicalDigestParsersDoNotBecomeProductionAuthority(t *testing.T) {
	parsed, err := ParseDigest(ZeroSHA256)
	if err != nil || parsed != Digest(ZeroSHA256) {
		t.Fatalf("lexical ZERO_SHA256 parse = %q, %v", parsed, err)
	}
	reservationID, err := ParseReservationID(ZeroSHA256)
	if err != nil || reservationID != ReservationID(ZeroSHA256) {
		t.Fatalf("lexical zero reservation parse = %q, %v", reservationID, err)
	}
	image, err := ParseImageDigest("sha256:" + ZeroSHA256)
	if err != nil || image != ImageDigest("sha256:"+ZeroSHA256) {
		t.Fatalf("lexical zero image parse = %q, %v", image, err)
	}

	if _, err := parseNonzeroDigest(ZeroSHA256); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("production digest helper error = %v", err)
	}
	if _, err := parseNonzeroReservationID(ZeroSHA256); !errors.Is(err, ErrInvalidReservationID) {
		t.Fatalf("production reservation helper error = %v", err)
	}
	if err := validateNonzeroImageDigest(image); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("production image helper error = %v", err)
	}
}

func TestGroupIDAndRequestKindValidation(t *testing.T) {
	for _, value := range []string{"group-a", "grüppe", strings.Repeat("a", MaxPolicyGroupIDBytes)} {
		if _, err := ParseGroupID(value); err != nil {
			t.Fatalf("valid group ID rejected: %v", err)
		}
	}
	for _, value := range []string{"", "bad\nvalue", strings.Repeat("é", 65)} {
		if _, err := ParseGroupID(value); !errors.Is(err, ErrInvalidGroupID) {
			t.Fatalf("invalid group ID error = %v", err)
		}
	}
	for _, kind := range []RequestKind{RequestRobots, RequestDocument, RequestRedirect, RequestRenderResource} {
		parsed, err := ParseRequestKind(string(kind))
		if err != nil || parsed != kind {
			t.Fatalf("request kind %q: parsed=%q err=%v", kind, parsed, err)
		}
	}
	if _, err := ParseRequestKind("DOCUMENT"); !errors.Is(err, ErrInvalidRequestKind) {
		t.Fatalf("invalid request kind error = %v", err)
	}
}

func TestCanonicalUnsignedDecimalsAndFences(t *testing.T) {
	valid := map[string]uint64{"0": 0, "1": 1, "9007199254740991": MaxExactInteger}
	for input, expected := range valid {
		value, err := ParseUnsignedDecimal(input)
		if err != nil {
			t.Fatalf("decimal %q: err=%v", input, err)
		}
		parsed, err := value.Uint64()
		if err != nil || parsed != expected {
			t.Fatalf("decimal %q: value=%d err=%v", input, parsed, err)
		}
		formatted, err := CanonicalUnsignedDecimal(expected)
		if err != nil || formatted != value {
			t.Fatalf("format %d: value=%q err=%v", expected, formatted, err)
		}
	}
	for _, input := range []string{"", "00", "01", "+1", "-1", "1.0", " 1", "9007199254740992"} {
		if _, err := ParseUnsignedDecimal(input); !errors.Is(err, ErrInvalidUnsignedDecimal) {
			t.Fatalf("decimal %q error = %v", input, err)
		}
	}
	if _, err := CanonicalUnsignedDecimal(MaxExactInteger + 1); !errors.Is(err, ErrInvalidUnsignedDecimal) {
		t.Fatalf("oversized decimal error = %v", err)
	}
	if fence, err := ParseFence("7"); err != nil || fence != 7 {
		t.Fatalf("fence: value=%d err=%v", fence, err)
	}
	for _, input := range []string{"0", "01", "9007199254740992"} {
		if _, err := ParseFence(input); !errors.Is(err, ErrInvalidFence) {
			t.Fatalf("fence %q error = %v", input, err)
		}
	}
}

func TestCanonicalScoreTextGrammarAndRange(t *testing.T) {
	valid := []string{"0", "1", "-1", "10000", "-1000", "0.5", "-0.5", "1.000001", "9999.999999", "-999.999999"}
	for _, input := range valid {
		score, err := ParseScoreText(input)
		if err != nil {
			t.Fatalf("valid score %q: score=%q err=%v", input, score, err)
		}
		parsed, err := score.Float64()
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			t.Fatalf("valid score %q: score=%q err=%v", input, score, err)
		}
	}
	invalid := []string{"", "00", "01", "+1", "-0", "0.0", "1.0", "1.", ".5", "1e2", "NaN", "Inf", "10000.000001", "-1000.000001", "0.0000001", "1.2345678"}
	for _, input := range invalid {
		if _, err := ParseScoreText(input); !errors.Is(err, ErrInvalidScoreText) {
			t.Fatalf("invalid score %q error = %v", input, err)
		}
	}
	if err := ValidateRedisScore(ScoreText("0.5"), "5e-1"); err != nil {
		t.Fatalf("equivalent Redis score rejected: %v", err)
	}
	for _, input := range []string{"0.6", "nan", "+inf", "REDIS_SCORE_CANARY"} {
		err := ValidateRedisScore(ScoreText("0.5"), input)
		if !errors.Is(err, ErrInvalidRedisScore) || strings.Contains(err.Error(), "CANARY") {
			t.Fatalf("invalid Redis score was not safely rejected: %v", err)
		}
	}
	if err := ValidateRedisScore(ScoreText("0"), "-0"); !errors.Is(err, ErrInvalidRedisScore) {
		t.Fatalf("negative Redis zero error = %v", err)
	}
}

func TestStableCodeSets(t *testing.T) {
	expectedStatuses := strings.Fields(`
		OK CREATED EXISTS_IDENTICAL CANDIDATE_INSTALLED LEGACY_RETIRED CONTRACTS_PROMOTED
		SEALED ACTIVATED AUDIT_STARTED CLAIMED ALREADY_CLAIMED NO_CANDIDATE VISITED_COMPLETED
		RESERVED ALREADY_RESERVED STARTED ALREADY_STARTED FINISHED ALREADY_FINISHED
		RESERVATION_CANCELLED RELEASED_READY RENEWED RETRY_SCHEDULED STAGE_BEGUN STAGED
		STAGE_ABORTED COMPLETED DEAD CANCELLED COMMITTED ALREADY_COMMITTED CAPACITY_BLOCKED
		RATE_BLOCKED LEASE_CAPACITY_BLOCKED STAGE_CAPACITY_BLOCKED RUN_BUDGET_EXHAUSTED
		RUN_RESERVATION_LIMIT_EXHAUSTED GROUP_BUDGET_EXHAUSTED DOWNSTREAM_BACKPRESSURE
		AUTHORIZATION_EXPIRED RUN_CANCELLED LEASE_LOST NOT_DUE BATCH_MORE BATCH_DONE ARCHIVED PURGED
	`)
	if len(statuses) != len(expectedStatuses) {
		t.Fatalf("status count = %d, want %d", len(statuses), len(expectedStatuses))
	}
	for _, expected := range expectedStatuses {
		if _, ok := statuses[Status(expected)]; !ok {
			t.Errorf("missing exact status %q", expected)
		}
	}
	for status := range statuses {
		if parsed, err := ParseStatus(string(status)); err != nil || parsed != status {
			t.Fatalf("status %q: parsed=%q err=%v", status, parsed, err)
		}
	}

	expectedErrors := strings.Fields(`
		BOOT_UNAPPROVED COMPATIBILITY_MISMATCH CONTRACT_MISMATCH WRONG_TYPE INVALID_ARGUMENT
		INVALID_IDENTIFIER INVALID_NUMBER INVALID_STATE IMMUTABLE_MISMATCH URL_ID_COLLISION
		LIMIT_EXCEEDED COUNTER_CORRUPT STATE_INDEX_CORRUPT RESERVATION_CORRUPT RATE_STATE_CORRUPT
		STAGE_INVALID STAGE_UNSEALED DESTINATION_EXISTS OUTPUT_CONTRACT_MISMATCH
		COMMAND_BOUNDS_EXCEEDED MEMORY_HEADROOM_LOW RATE_SCOPE_CAPACITY_EXCEEDED
		ADMIN_FREEZE_REQUIRED COMMIT_GUARD_UNAPPROVED
	`)
	if len(errorCodes) != len(expectedErrors) {
		t.Fatalf("error-code count = %d, want %d", len(errorCodes), len(expectedErrors))
	}
	for _, expected := range expectedErrors {
		if _, ok := errorCodes[ErrorCode(expected)]; !ok {
			t.Errorf("missing exact error code %q", expected)
		}
	}
	for code := range errorCodes {
		if parsed, err := ParseErrorCode(string(code)); err != nil || parsed != code {
			t.Fatalf("error code %q: parsed=%q err=%v", code, parsed, err)
		}
		reply, err := code.RedisReply()
		if err != nil || reply != "ERR CRAWL_V2_"+string(code) {
			t.Fatalf("error reply %q: reply=%q err=%v", code, reply, err)
		}
	}

	expectedReasons := strings.Fields(`
		none published already_visited request_timeout dns_temporary dial_temporary request_temporary
		http_429 http_5xx robots_temporary renderer_temporary downstream_backpressure
		capacity_blocked_after_io run_budget_exhausted_after_io group_budget_exhausted_after_io
		rate_blocked_after_io lease_expired_after_io worker_shutdown_after_io policy_denied
		policy_scope_changed robots_denied robots_invalid job_malformed url_identity_mismatch
		static_url_denied dns_prohibited http_4xx response_invalid body_too_large html_invalid
		discovery_limit renderer_permanent output_invalid run_job_limit reservation_limit_exhausted
		retry_exhausted pre_io_recovery_exhausted protocol_corrupt authorization_expired
		operator_cancelled source_cancelled all_jobs_terminal request_budget_exhausted
		group_budgets_exhausted
	`)
	if len(reasons) != len(expectedReasons) {
		t.Fatalf("reason count = %d, want %d", len(reasons), len(expectedReasons))
	}
	for _, expected := range expectedReasons {
		if _, ok := reasons[Reason(expected)]; !ok {
			t.Errorf("missing exact reason %q", expected)
		}
	}
	for reason := range reasons {
		if parsed, err := ParseReason(string(reason)); err != nil || parsed != reason {
			t.Fatalf("reason %q: parsed=%q err=%v", reason, parsed, err)
		}
	}
	for _, blocked := range []BlockedReason{BlockedPagesQueueFull, BlockedMemoryHeadroomLow, BlockedStageSlotsFull} {
		if parsed, err := ParseBlockedReason(string(blocked)); err != nil || parsed != blocked {
			t.Fatalf("blocked reason %q: parsed=%q err=%v", blocked, parsed, err)
		}
	}
	if _, err := ParseBlockedReason("unknown_block_canary"); !errors.Is(err, ErrUnknownBlockedReason) || strings.Contains(err.Error(), "canary") {
		t.Fatalf("unknown blocked reason was not safely redacted: %v", err)
	}
	if _, err := ParseStatus("UNKNOWN_VALUE_CANARY"); !errors.Is(err, ErrUnknownStatus) || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("unknown status was not safely redacted: %v", err)
	}
}

func parseRunError(value string) error    { _, err := ParseRunID(value); return err }
func parseJobError(value string) error    { _, err := ParseJobID(value); return err }
func parseOwnerError(value string) error  { _, err := ParseOwnerID(value); return err }
func parseTokenError(value string) error  { _, err := ParseLeaseToken(value); return err }
func parseDigestError(value string) error { _, err := ParseDigest(value); return err }
func parseRateError(value string) error   { _, err := ParseRateScopeID(value); return err }
