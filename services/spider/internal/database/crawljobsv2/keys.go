package crawljobsv2

import (
	"encoding/base64"
	"errors"
	"strconv"
)

const (
	ContractsActiveKey        = "mifolyo:contracts:active"
	ContractsCandidateKey     = "mifolyo:contracts:candidate"
	CrawlContractCandidateKey = "mifolyo:crawl:v2:contract:candidate"
	CrawlContractKey          = "mifolyo:crawl:v2:contract"
	DurabilityKey             = "mifolyo:crawl:v2:durability"
	AdminFreezeKey            = "mifolyo:crawl:v2:admin_freeze"
	LegacyRetirementKey       = "mifolyo:crawl:v2:legacy_retirement"
	CommitGuardKey            = "mifolyo:crawl:v2:commit_guard"
	RunsKey                   = "mifolyo:crawl:v2:runs"
	ActiveRunsKey             = "mifolyo:crawl:v2:active_runs"
	UnarchivedRunsKey         = "mifolyo:crawl:v2:unarchived_runs"
	FirstRequestStartKey      = "mifolyo:crawl:v2:first_request_start"
	ActiveLeasesKey           = "mifolyo:crawl:v2:active_leases"
	StageExpiryKey            = "mifolyo:crawl:v2:stage_expiry"
	StageSlotsKey             = "mifolyo:crawl:v2:stage_slots"
	RateScopesKey             = "mifolyo:crawl:v2:rate_scopes"

	PagesQueueKey                  = "pages_queue"
	PagesQueueProcessingKey        = "pages_queue:processing"
	PagesQueueDeadKey              = "pages_queue:dead"
	ImageIndexerQueueKey           = "image_indexer_queue"
	ImageIndexerQueueProcessingKey = "image_indexer_queue:processing"
	ImageIndexerQueueDeadKey       = "image_indexer_queue:dead"
	PagesQueueOwnerKey             = "pages_queue:indexer_owner"
	ImageIndexerQueueOwnerKey      = "image_indexer_queue:owner"

	LegacyCrawlQueueKey  = "mifolyo:crawl:v1:queue"
	LegacyCrawlURLsKey   = "mifolyo:crawl:v1:urls"
	LegacyCrawlDepthsKey = "mifolyo:crawl:v1:depths"
	LegacySpiderQueueKey = "spider_queue"
	LegacySignalQueueKey = "signal_queue"
)

var ErrInvalidImageIndex = errors.New("crawljobsv2: invalid image index")

func LegacyKeysInBitmapOrder() [5]string {
	return [5]string{LegacyCrawlQueueKey, LegacyCrawlURLsKey, LegacyCrawlDepthsKey, LegacySpiderQueueKey, LegacySignalQueueKey}
}

func DownstreamQueueKeys() [6]string {
	return [6]string{PagesQueueKey, PagesQueueProcessingKey, PagesQueueDeadKey, ImageIndexerQueueKey, ImageIndexerQueueProcessingKey, ImageIndexerQueueDeadKey}
}

func RateScopeKey(scopeID Digest) (string, error) {
	return digestKey("mifolyo:crawl:v2:rate:", scopeID, "")
}

func RateScopeActiveKey(scopeID Digest) (string, error) {
	return digestKey("mifolyo:crawl:v2:rate:", scopeID, ":active")
}

func RateScopePendingKey(scopeID Digest) (string, error) {
	return digestKey("mifolyo:crawl:v2:rate:", scopeID, ":pending")
}

func RateScopeStartedKey(scopeID Digest) (string, error) {
	return digestKey("mifolyo:crawl:v2:rate:", scopeID, ":started")
}

func ReservationKey(reservationID ReservationID) (string, error) {
	if err := validateReservationID(reservationID); err != nil {
		return "", err
	}
	return "mifolyo:crawl:v2:reservation:" + string(reservationID), nil
}

func RunKey(runID RunID) (string, error)         { return runSuffixKey(runID, "") }
func RunJobsKey(runID RunID) (string, error)     { return runSuffixKey(runID, ":jobs") }
func RunJobOrderKey(runID RunID) (string, error) { return runSuffixKey(runID, ":job_order") }
func RunReadyKey(runID RunID) (string, error)    { return runSuffixKey(runID, ":ready") }
func RunReadyAtKey(runID RunID) (string, error)  { return runSuffixKey(runID, ":ready_at") }
func RunLeasedKey(runID RunID) (string, error)   { return runSuffixKey(runID, ":leased") }
func RunLeasedAtKey(runID RunID) (string, error) { return runSuffixKey(runID, ":leased_at") }
func RunDelayedKey(runID RunID) (string, error)  { return runSuffixKey(runID, ":delayed") }
func RunCommitBackpressureKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":commit_backpressure")
}
func RunCompletedKey(runID RunID) (string, error)   { return runSuffixKey(runID, ":completed") }
func RunDeadKey(runID RunID) (string, error)        { return runSuffixKey(runID, ":dead") }
func RunCancelledKey(runID RunID) (string, error)   { return runSuffixKey(runID, ":cancelled") }
func RunGroupLimitsKey(runID RunID) (string, error) { return runSuffixKey(runID, ":group_limits") }
func RunGroupRateScopeIDsKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":group_rate_scope_ids")
}
func RunGroupScopeIDsKey(runID RunID) (string, error) { return runSuffixKey(runID, ":group_scope_ids") }
func RunGroupConcurrencyKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":group_concurrency")
}
func RunGroupIntervalMSKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":group_interval_ms")
}
func RunGroupStartedKey(runID RunID) (string, error) { return runSuffixKey(runID, ":group_started") }
func RunGroupPendingKey(runID RunID) (string, error) { return runSuffixKey(runID, ":group_pending") }
func RunGroupActiveStartedKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":group_active_started")
}
func RunGroupOpenJobsKey(runID RunID) (string, error) { return runSuffixKey(runID, ":group_open_jobs") }
func RunAuditGroupCountsKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":audit_group_counts")
}
func RunRetryReasonCountsKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":retry_reason_counts")
}
func RunRecoveryOutcomeCountsKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":recovery_outcome_counts")
}
func RunDispositionReasonCountsKey(runID RunID) (string, error) {
	return runSuffixKey(runID, ":disposition_reason_counts")
}
func RunVisitedDepthKey(runID RunID) (string, error) { return runSuffixKey(runID, ":visited_depth") }
func RunVisitedURLsKey(runID RunID) (string, error)  { return runSuffixKey(runID, ":visited_urls") }

func RunJobKey(runID RunID, jobID JobID) (string, error) {
	base, err := runSuffixKey(runID, "")
	if err != nil {
		return "", err
	}
	if err := validateJobID(jobID); err != nil {
		return "", err
	}
	return base + ":job:" + string(jobID), nil
}

func ActiveLeaseMember(runID RunID, jobID JobID) (string, error) {
	if err := validateRunID(runID); err != nil {
		return "", err
	}
	if err := validateJobID(jobID); err != nil {
		return "", err
	}
	return string(runID) + ":" + string(jobID), nil
}

func StageMetaKey(commitID Digest) (string, error)     { return stageSuffixKey(commitID, ":meta") }
func StageKeysKey(commitID Digest) (string, error)     { return stageSuffixKey(commitID, ":keys") }
func StagePageKey(commitID Digest) (string, error)     { return stageSuffixKey(commitID, ":page") }
func StageOutlinksKey(commitID Digest) (string, error) { return stageSuffixKey(commitID, ":outlinks") }
func StageDiscoveriesKey(commitID Digest) (string, error) {
	return stageSuffixKey(commitID, ":discoveries")
}
func StageDiscoveryRecordsKey(commitID Digest) (string, error) {
	return stageSuffixKey(commitID, ":discovery_records")
}
func StageDiscoveryDepthsKey(commitID Digest) (string, error) {
	return stageSuffixKey(commitID, ":discovery_depths")
}
func StageAliasesKey(commitID Digest) (string, error) { return stageSuffixKey(commitID, ":aliases") }
func StageImageManifestKey(commitID Digest) (string, error) {
	return stageSuffixKey(commitID, ":image_manifest")
}

func StageImageKey(commitID Digest, index int) (string, error) {
	if index < 0 || index >= MaxImagesPerPage {
		return "", ErrInvalidImageIndex
	}
	return stageSuffixKey(commitID, ":image:"+strconv.Itoa(index))
}

func PageDataKey(publicationID Digest, canonicalPageURL string) (string, error) {
	return publicationURLKey("page_data", publicationID, canonicalPageURL)
}

func OutlinksKey(publicationID Digest, canonicalPageURL string) (string, error) {
	return publicationURLKey("outlinks", publicationID, canonicalPageURL)
}

func PageImagesKey(publicationID Digest, canonicalPageURL string) (string, error) {
	return publicationURLKey("page_images", publicationID, canonicalPageURL)
}

func ImageDataKey(publicationID Digest, canonicalPageURL, canonicalImageURL string) (string, error) {
	prefix, err := publicationURLKey("image_data", publicationID, canonicalPageURL)
	if err != nil {
		return "", err
	}
	if _, err := requireCanonicalURL(canonicalImageURL); err != nil {
		return "", err
	}
	return prefix + ":" + base64.RawURLEncoding.EncodeToString([]byte(canonicalImageURL)), nil
}

func BacklinksKey(canonicalTargetURL string) (string, error) {
	if _, err := requireCanonicalURL(canonicalTargetURL); err != nil {
		return "", err
	}
	return "backlinks:" + canonicalTargetURL, nil
}

func runSuffixKey(runID RunID, suffix string) (string, error) {
	if err := validateRunID(runID); err != nil {
		return "", err
	}
	return "mifolyo:crawl:v2:run:" + string(runID) + suffix, nil
}

func digestKey(prefix string, digest Digest, suffix string) (string, error) {
	if err := validateDigest(digest); err != nil {
		return "", err
	}
	return prefix + string(digest) + suffix, nil
}

func stageSuffixKey(commitID Digest, suffix string) (string, error) {
	return digestKey("mifolyo:crawl:v2:stage:", commitID, suffix)
}

func publicationURLKey(prefix string, publicationID Digest, canonicalURL string) (string, error) {
	if err := validateDigest(publicationID); err != nil {
		return "", err
	}
	if _, err := requireCanonicalURL(canonicalURL); err != nil {
		return "", err
	}
	return prefix + ":" + string(publicationID) + ":" + base64.RawURLEncoding.EncodeToString([]byte(canonicalURL)), nil
}
