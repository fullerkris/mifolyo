package crawljobsv2

import (
	"errors"
	"strings"
	"testing"
)

func TestGlobalAndLiteralKeys(t *testing.T) {
	global := map[string]string{
		"contracts active":    ContractsActiveKey,
		"contracts candidate": ContractsCandidateKey,
		"contract candidate":  CrawlContractCandidateKey,
		"contract":            CrawlContractKey,
		"durability":          DurabilityKey,
		"admin freeze":        AdminFreezeKey,
		"legacy retirement":   LegacyRetirementKey,
		"commit guard":        CommitGuardKey,
		"runs":                RunsKey,
		"active runs":         ActiveRunsKey,
		"unarchived runs":     UnarchivedRunsKey,
		"first request":       FirstRequestStartKey,
		"active leases":       ActiveLeasesKey,
		"stage expiry":        StageExpiryKey,
		"stage slots":         StageSlotsKey,
		"rate scopes":         RateScopesKey,
		"pages":               PagesQueueKey,
		"pages processing":    PagesQueueProcessingKey,
		"pages dead":          PagesQueueDeadKey,
		"images":              ImageIndexerQueueKey,
		"images processing":   ImageIndexerQueueProcessingKey,
		"images dead":         ImageIndexerQueueDeadKey,
		"pages owner":         PagesQueueOwnerKey,
		"images owner":        ImageIndexerQueueOwnerKey,
	}
	expected := map[string]string{
		"contracts active":    "mifolyo:contracts:active",
		"contracts candidate": "mifolyo:contracts:candidate",
		"contract candidate":  "mifolyo:crawl:v2:contract:candidate",
		"contract":            "mifolyo:crawl:v2:contract",
		"durability":          "mifolyo:crawl:v2:durability",
		"admin freeze":        "mifolyo:crawl:v2:admin_freeze",
		"legacy retirement":   "mifolyo:crawl:v2:legacy_retirement",
		"commit guard":        "mifolyo:crawl:v2:commit_guard",
		"runs":                "mifolyo:crawl:v2:runs",
		"active runs":         "mifolyo:crawl:v2:active_runs",
		"unarchived runs":     "mifolyo:crawl:v2:unarchived_runs",
		"first request":       "mifolyo:crawl:v2:first_request_start",
		"active leases":       "mifolyo:crawl:v2:active_leases",
		"stage expiry":        "mifolyo:crawl:v2:stage_expiry",
		"stage slots":         "mifolyo:crawl:v2:stage_slots",
		"rate scopes":         "mifolyo:crawl:v2:rate_scopes",
		"pages":               "pages_queue",
		"pages processing":    "pages_queue:processing",
		"pages dead":          "pages_queue:dead",
		"images":              "image_indexer_queue",
		"images processing":   "image_indexer_queue:processing",
		"images dead":         "image_indexer_queue:dead",
		"pages owner":         "pages_queue:indexer_owner",
		"images owner":        "image_indexer_queue:owner",
	}
	for name, want := range expected {
		if got := global[name]; got != want {
			t.Errorf("%s key = %q, want %q", name, got, want)
		}
	}

	wantLegacy := [5]string{"mifolyo:crawl:v1:queue", "mifolyo:crawl:v1:urls", "mifolyo:crawl:v1:depths", "spider_queue", "signal_queue"}
	if got := LegacyKeysInBitmapOrder(); got != wantLegacy {
		t.Fatalf("legacy keys = %#v, want %#v", got, wantLegacy)
	}
	wantQueues := [6]string{"pages_queue", "pages_queue:processing", "pages_queue:dead", "image_indexer_queue", "image_indexer_queue:processing", "image_indexer_queue:dead"}
	if got := DownstreamQueueKeys(); got != wantQueues {
		t.Fatalf("downstream queues = %#v, want %#v", got, wantQueues)
	}
}

func TestRunAndIndexKeyBuilders(t *testing.T) {
	base := "mifolyo:crawl:v2:run:" + string(testRunID)
	tests := []struct {
		name   string
		build  func(RunID) (string, error)
		suffix string
	}{
		{name: "run", build: RunKey, suffix: ""},
		{name: "jobs", build: RunJobsKey, suffix: ":jobs"},
		{name: "order", build: RunJobOrderKey, suffix: ":job_order"},
		{name: "ready", build: RunReadyKey, suffix: ":ready"},
		{name: "ready at", build: RunReadyAtKey, suffix: ":ready_at"},
		{name: "leased", build: RunLeasedKey, suffix: ":leased"},
		{name: "leased at", build: RunLeasedAtKey, suffix: ":leased_at"},
		{name: "delayed", build: RunDelayedKey, suffix: ":delayed"},
		{name: "backpressure", build: RunCommitBackpressureKey, suffix: ":commit_backpressure"},
		{name: "completed", build: RunCompletedKey, suffix: ":completed"},
		{name: "dead", build: RunDeadKey, suffix: ":dead"},
		{name: "cancelled", build: RunCancelledKey, suffix: ":cancelled"},
		{name: "limits", build: RunGroupLimitsKey, suffix: ":group_limits"},
		{name: "rate IDs", build: RunGroupRateScopeIDsKey, suffix: ":group_rate_scope_ids"},
		{name: "scope IDs", build: RunGroupScopeIDsKey, suffix: ":group_scope_ids"},
		{name: "concurrency", build: RunGroupConcurrencyKey, suffix: ":group_concurrency"},
		{name: "interval", build: RunGroupIntervalMSKey, suffix: ":group_interval_ms"},
		{name: "started", build: RunGroupStartedKey, suffix: ":group_started"},
		{name: "pending", build: RunGroupPendingKey, suffix: ":group_pending"},
		{name: "active started", build: RunGroupActiveStartedKey, suffix: ":group_active_started"},
		{name: "open jobs", build: RunGroupOpenJobsKey, suffix: ":group_open_jobs"},
		{name: "audit", build: RunAuditGroupCountsKey, suffix: ":audit_group_counts"},
		{name: "retry", build: RunRetryReasonCountsKey, suffix: ":retry_reason_counts"},
		{name: "recovery", build: RunRecoveryOutcomeCountsKey, suffix: ":recovery_outcome_counts"},
		{name: "disposition", build: RunDispositionReasonCountsKey, suffix: ":disposition_reason_counts"},
		{name: "visited depth", build: RunVisitedDepthKey, suffix: ":visited_depth"},
		{name: "visited URLs", build: RunVisitedURLsKey, suffix: ":visited_urls"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.build(testRunID)
			if err != nil || got != base+test.suffix {
				t.Fatalf("key = %q, err=%v, want %q", got, err, base+test.suffix)
			}
			if _, err := test.build(RunID("bad:run")); !errors.Is(err, ErrInvalidRunID) {
				t.Fatalf("malformed run error = %v", err)
			}
		})
	}

	jobKey, err := RunJobKey(testRunID, testPageJobID)
	if err != nil || jobKey != base+":job:"+string(testPageJobID) {
		t.Fatalf("job key = %q, err=%v", jobKey, err)
	}
	member, err := ActiveLeaseMember(testRunID, testPageJobID)
	if err != nil || member != string(testRunID)+":"+string(testPageJobID) {
		t.Fatalf("lease member = %q, err=%v", member, err)
	}
	if _, err := RunJobKey(testRunID, JobID("bad")); !errors.Is(err, ErrInvalidJobID) {
		t.Fatalf("malformed job error = %v", err)
	}
}

func TestRateReservationAndStageKeyBuilders(t *testing.T) {
	digest := Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tests := []struct {
		name    string
		build   func(Digest) (string, error)
		want    string
		wantErr error
	}{
		{name: "rate", build: RateScopeKey, want: "mifolyo:crawl:v2:rate:" + string(digest), wantErr: ErrInvalidDigest},
		{name: "rate active", build: RateScopeActiveKey, want: "mifolyo:crawl:v2:rate:" + string(digest) + ":active", wantErr: ErrInvalidDigest},
		{name: "rate pending", build: RateScopePendingKey, want: "mifolyo:crawl:v2:rate:" + string(digest) + ":pending", wantErr: ErrInvalidDigest},
		{name: "rate started", build: RateScopeStartedKey, want: "mifolyo:crawl:v2:rate:" + string(digest) + ":started", wantErr: ErrInvalidDigest},
		{name: "reservation", build: func(value Digest) (string, error) {
			return ReservationKey(ReservationID(value))
		}, want: "mifolyo:crawl:v2:reservation:" + string(digest), wantErr: ErrInvalidReservationID},
		{name: "stage meta", build: StageMetaKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":meta", wantErr: ErrInvalidDigest},
		{name: "stage keys", build: StageKeysKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":keys", wantErr: ErrInvalidDigest},
		{name: "stage page", build: StagePageKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":page", wantErr: ErrInvalidDigest},
		{name: "stage outlinks", build: StageOutlinksKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":outlinks", wantErr: ErrInvalidDigest},
		{name: "stage discoveries", build: StageDiscoveriesKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":discoveries", wantErr: ErrInvalidDigest},
		{name: "stage records", build: StageDiscoveryRecordsKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":discovery_records", wantErr: ErrInvalidDigest},
		{name: "stage depths", build: StageDiscoveryDepthsKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":discovery_depths", wantErr: ErrInvalidDigest},
		{name: "stage aliases", build: StageAliasesKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":aliases", wantErr: ErrInvalidDigest},
		{name: "stage manifest", build: StageImageManifestKey, want: "mifolyo:crawl:v2:stage:" + string(digest) + ":image_manifest", wantErr: ErrInvalidDigest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.build(digest)
			if err != nil || got != test.want {
				t.Fatalf("key = %q, err=%v, want %q", got, err, test.want)
			}
			if _, err := test.build(Digest("not-a-digest")); !errors.Is(err, test.wantErr) {
				t.Fatalf("malformed digest error = %v", err)
			}
		})
	}
	imageKey, err := StageImageKey(digest, 63)
	if err != nil || imageKey != "mifolyo:crawl:v2:stage:"+string(digest)+":image:63" {
		t.Fatalf("stage image key = %q, err=%v", imageKey, err)
	}
	for _, index := range []int{-1, 64} {
		if _, err := StageImageKey(digest, index); !errors.Is(err, ErrInvalidImageIndex) {
			t.Fatalf("image index %d error = %v", index, err)
		}
	}
}

func TestPreservedOutputKeyBuilders(t *testing.T) {
	publicationID := Digest("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	page64 := "aHR0cHM6Ly9leGFtcGxlLmNvbS9wYWdl"
	imageURL := "https://example.com/a.jpg"
	image64 := "aHR0cHM6Ly9leGFtcGxlLmNvbS9hLmpwZw"
	tests := []struct {
		name  string
		build func(Digest, string) (string, error)
		want  string
	}{
		{name: "page", build: PageDataKey, want: "page_data:" + string(publicationID) + ":" + page64},
		{name: "outlinks", build: OutlinksKey, want: "outlinks:" + string(publicationID) + ":" + page64},
		{name: "manifest", build: PageImagesKey, want: "page_images:" + string(publicationID) + ":" + page64},
	}
	for _, test := range tests {
		got, err := test.build(publicationID, testPageURL)
		if err != nil || got != test.want {
			t.Fatalf("%s key = %q, err=%v, want %q", test.name, got, err, test.want)
		}
	}
	imageKey, err := ImageDataKey(publicationID, testPageURL, imageURL)
	wantImage := "image_data:" + string(publicationID) + ":" + page64 + ":" + image64
	if err != nil || imageKey != wantImage {
		t.Fatalf("image key = %q, err=%v, want %q", imageKey, err, wantImage)
	}
	backlink, err := BacklinksKey(testTargetURL)
	if err != nil || backlink != "backlinks:"+testTargetURL {
		t.Fatalf("backlink key = %q, err=%v", backlink, err)
	}

	secretURL := "https://EXAMPLE.com/private-canary"
	if _, err := PageDataKey(publicationID, secretURL); !errors.Is(err, ErrInvalidCanonicalURL) || strings.Contains(err.Error(), "private-canary") {
		t.Fatalf("canonical URL error was not redacted: %v", err)
	}
	secretDigest := ReservationID("reservation-secret-canary")
	if _, err := ReservationKey(secretDigest); !errors.Is(err, ErrInvalidReservationID) || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("reservation error was not redacted: %v", err)
	}
}
