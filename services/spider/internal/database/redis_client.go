package database

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var (
	ErrRedisNotConfigured = errors.New("Redis client is not configured")
	ErrInvalidQueueScore  = errors.New("crawl queue score must be finite")
	ErrInvalidCrawlDepth  = errors.New("crawl depth must be a non-negative integer")
	ErrInvalidURLID       = errors.New("invalid V1 URL ID")
	ErrURLIDCollision     = errors.New("V1 URL ID is already mapped to a different URL")
	ErrInvalidQueueMember = errors.New("crawl queue member is not a string URL ID")
	ErrOrphanURLLookup    = errors.New("crawl queue URL lookup is missing")
	ErrInvalidQueueEntry  = errors.New("crawl queue URL lookup does not match its V1 identity")
	ErrQueueSnapshotLimit = errors.New("crawl queue exceeds the bounded scheduler snapshot")
)

const defaultCrawlSnapshotLimit int64 = 10_000

func queueMemberReference(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:8])
}

// Database owns the Redis connection and V1 crawl-queue key configuration.
// Empty key fields deliberately resolve to the versioned V1 defaults.
type Database struct {
	Client         *redis.Client
	Context        context.Context
	CrawlQueueKey  string
	CrawlURLsKey   string
	CrawlDepthsKey string
	// CrawlSnapshotLimit bounds scheduler memory and Redis response size. A
	// non-positive value selects defaultCrawlSnapshotLimit.
	CrawlSnapshotLimit int64
	// CrawlPopTimeout defaults to utils.Timeout. A negative value selects a
	// non-blocking ZPOPMIN, which is useful for bounded workers and tests.
	CrawlPopTimeout time.Duration
}

func (db *Database) ConnectToRedis(redisHost, redisPort, redisPassword, redisDB string) error {
	log.Println("Connecting to Redis...")
	redisUsername := os.Getenv("REDIS_USERNAME")
	allowInsecure := os.Getenv("ALLOW_INSECURE_DATASTORES") == "true"
	if redisUsername != "" && redisPassword == "" {
		return errors.New("REDIS_USERNAME requires REDIS_PASSWORD")
	}
	if redisPassword == "" && !allowInsecure {
		return errors.New("Redis authentication is required unless ALLOW_INSECURE_DATASTORES=true")
	}

	dbIndex, err := strconv.Atoi(redisDB)
	if err != nil {
		return fmt.Errorf("parse Redis DB value: %w", err)
	}

	db.Client = redis.NewClient(&redis.Options{
		Addr:         redisHost + ":" + redisPort,
		Username:     redisUsername,
		Password:     redisPassword,
		DB:           dbIndex,
		DialTimeout:  utils.Timeout,
		ReadTimeout:  utils.Timeout,
		WriteTimeout: utils.Timeout,
	})
	db.Context = context.Background()

	if _, err = db.Client.Ping(db.context()).Result(); err != nil {
		return errors.New("connect to Redis failed")
	}

	log.Println("Successfully connected to Redis!")
	return nil
}

func (db *Database) context() context.Context {
	if db.Context != nil {
		return db.Context
	}
	return context.Background()
}

func (db *Database) queueKey() string {
	if db.CrawlQueueKey != "" {
		return db.CrawlQueueKey
	}
	return utils.CrawlQueueKeyV1
}

func (db *Database) urlsKey() string {
	if db.CrawlURLsKey != "" {
		return db.CrawlURLsKey
	}
	return utils.CrawlURLsKeyV1
}

func (db *Database) depthsKey() string {
	if db.CrawlDepthsKey != "" {
		return db.CrawlDepthsKey
	}
	return utils.CrawlDepthsKeyV1
}

func (db *Database) popTimeout() time.Duration {
	if db.CrawlPopTimeout != 0 {
		return db.CrawlPopTimeout
	}
	return utils.Timeout
}

func (db *Database) snapshotLimit() int64 {
	if db.CrawlSnapshotLimit > 0 {
		return db.CrawlSnapshotLimit
	}
	return defaultCrawlSnapshotLimit
}

func (db *Database) requireClient() error {
	if db.Client == nil {
		return ErrRedisNotConfigured
	}
	return nil
}

var pushURLV1Script = redis.NewScript(`
local function is_finite_number(value)
  return value and value == value and value ~= math.huge and value ~= -math.huge
end

local function parse_canonical_depth(value)
  if value == '0' then
    return 0
  end
  if not string.match(value, '^[1-9][0-9]*$') then
    return nil
  end
  local parsed = tonumber(value)
  if not parsed or parsed ~= math.floor(parsed) or parsed > 9007199254740991 then
    return nil
  end
  return parsed
end

local existing_url = redis.call('HGET', KEYS[2], ARGV[1])
if existing_url and existing_url ~= ARGV[2] then
  return redis.error_reply('URL_ID_COLLISION')
end

local existing_score = redis.call('ZSCORE', KEYS[1], ARGV[1])
local existing_depth = redis.call('HGET', KEYS[3], ARGV[1])
local requested_score = tonumber(ARGV[3])
local requested_depth = parse_canonical_depth(ARGV[4])
if not is_finite_number(requested_score) then
  return redis.error_reply('INVALID_QUEUE_SCORE')
end
if not requested_depth then
  return redis.error_reply('INVALID_CRAWL_DEPTH')
end
if existing_score and not is_finite_number(tonumber(existing_score)) then
  return redis.error_reply('INVALID_EXISTING_SCORE')
end
if existing_depth and not parse_canonical_depth(existing_depth) then
  return redis.error_reply('INVALID_EXISTING_DEPTH')
end

redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])
if (not existing_score) or requested_score < tonumber(existing_score) then
  redis.call('ZADD', KEYS[1], ARGV[3], ARGV[1])
end
if (not existing_score) or (not existing_depth) or requested_depth < tonumber(existing_depth) then
  redis.call('HSET', KEYS[3], ARGV[1], ARGV[4])
end
return 1
`)

// PushURL applies V1 canonicalization and static admission before atomically
// storing URL ID => canonical URL and queueing the ID at its best (lowest)
// observed score.
func (db *Database) PushURL(rawURL string, score float64) error {
	return db.PushURLWithDepth(rawURL, score, 0)
}

// PushURLWithDepth stores explicit crawl depth independently from queue score.
// Replays retain both the lowest score and the shallowest observed depth.
func (db *Database) PushURLWithDepth(rawURL string, score float64, depth int) error {
	if err := db.requireClient(); err != nil {
		return err
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return ErrInvalidQueueScore
	}
	if depth < 0 || uint64(depth) > utils.MaxCrawlDepthV1 {
		return ErrInvalidCrawlDepth
	}

	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return fmt.Errorf("canonicalize URL for crawl queue: %w", err)
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return fmt.Errorf("URL ID %s is not statically crawl eligible: %w", identity.URLID, err)
	}

	keys := []string{db.queueKey(), db.urlsKey(), db.depthsKey()}
	err = pushURLV1Script.Run(
		db.context(),
		db.Client,
		keys,
		identity.URLID,
		identity.CanonicalURL,
		strconv.FormatFloat(score, 'g', -1, 64),
		strconv.Itoa(depth),
	).Err()
	if err != nil {
		if strings.Contains(err.Error(), "URL_ID_COLLISION") {
			return fmt.Errorf("%w: %s", ErrURLIDCollision, identity.URLID)
		}
		if strings.Contains(err.Error(), "INVALID_CRAWL_DEPTH") || strings.Contains(err.Error(), "INVALID_EXISTING_DEPTH") {
			return fmt.Errorf("%w: %s", ErrInvalidCrawlDepth, identity.URLID)
		}
		if strings.Contains(err.Error(), "INVALID_QUEUE_SCORE") || strings.Contains(err.Error(), "INVALID_EXISTING_SCORE") {
			return fmt.Errorf("%w: %s", ErrInvalidQueueScore, identity.URLID)
		}
		return fmt.Errorf("store URL in V1 crawl queue: %w", err)
	}

	return nil
}

// CrawlCandidate is one pending V1 queue member with policy scheduling data.
// ValidationError is non-nil when the queue member cannot safely be claimed.
type CrawlCandidate struct {
	URLID           string
	CanonicalURL    string
	Score           float64
	Depth           int
	ValidationError error
}

// ListPendingURLs returns a bounded, stable score/ID ordered snapshot without
// removing queue members. Missing depth metadata is invalid and must be
// repaired by a reviewed feeder replay before any request is attempted.
func (db *Database) ListPendingURLs() ([]CrawlCandidate, error) {
	if err := db.requireClient(); err != nil {
		return nil, err
	}

	limit := db.snapshotLimit()
	queued, err := db.Client.ZRangeWithScores(db.context(), db.queueKey(), 0, limit).Result()
	if err != nil {
		return nil, fmt.Errorf("list V1 crawl queue: %w", err)
	}
	if int64(len(queued)) > limit {
		return nil, fmt.Errorf("%w: limit=%d", ErrQueueSnapshotLimit, limit)
	}

	candidates := make([]CrawlCandidate, 0, len(queued))
	candidateIndexes := make(map[string]int, len(queued))
	urlIDs := make([]string, 0, len(queued))
	for _, item := range queued {
		candidate := CrawlCandidate{Score: item.Score}
		member, ok := item.Member.(string)
		if !ok {
			candidate.ValidationError = ErrInvalidQueueMember
			candidates = append(candidates, candidate)
			continue
		}
		if !utils.IsURLIDV1(member) {
			candidate.ValidationError = fmt.Errorf("%w: ref=%s", ErrInvalidURLID, queueMemberReference(member))
			candidates = append(candidates, candidate)
			continue
		}
		candidate.URLID = member
		if math.IsNaN(item.Score) || math.IsInf(item.Score, 0) {
			candidate.ValidationError = fmt.Errorf("%w for %s", ErrInvalidQueueScore, member)
			candidates = append(candidates, candidate)
			continue
		}
		candidateIndexes[member] = len(candidates)
		urlIDs = append(urlIDs, member)
		candidates = append(candidates, candidate)
	}
	if len(urlIDs) == 0 {
		return candidates, nil
	}

	pipeline := db.Client.Pipeline()
	urlValuesCommand := pipeline.HMGet(db.context(), db.urlsKey(), urlIDs...)
	depthValuesCommand := pipeline.HMGet(db.context(), db.depthsKey(), urlIDs...)
	if _, err := pipeline.Exec(db.context()); err != nil {
		return nil, fmt.Errorf("resolve V1 crawl queue metadata: %w", err)
	}
	urlValues, err := urlValuesCommand.Result()
	if err != nil {
		return nil, fmt.Errorf("resolve V1 crawl queue URLs: %w", err)
	}
	depthValues, err := depthValuesCommand.Result()
	if err != nil {
		return nil, fmt.Errorf("resolve V1 crawl queue depths: %w", err)
	}

	for index, urlID := range urlIDs {
		candidate := &candidates[candidateIndexes[urlID]]
		canonicalURL, ok := urlValues[index].(string)
		if !ok {
			candidate.ValidationError = fmt.Errorf("%w: %s", ErrOrphanURLLookup, urlID)
			continue
		}
		candidate.CanonicalURL = canonicalURL

		depthValue, ok := depthValues[index].(string)
		if !ok {
			candidate.ValidationError = fmt.Errorf("%w for %s: missing crawl depth", ErrInvalidQueueEntry, urlID)
			continue
		}
		depth, parseErr := parseCanonicalCrawlDepth(depthValue)
		if parseErr != nil {
			candidate.ValidationError = fmt.Errorf("%w for %s", ErrInvalidCrawlDepth, urlID)
			continue
		}
		candidate.Depth = depth

		identity, identityErr := utils.CanonicalizeURLV1(canonicalURL)
		if identityErr != nil || identity.CanonicalURL != canonicalURL || identity.URLID != urlID {
			candidate.ValidationError = fmt.Errorf("%w: %s", ErrInvalidQueueEntry, urlID)
			continue
		}
		if admissionErr := utils.RequireStaticCrawlEligibility(identity); admissionErr != nil {
			candidate.ValidationError = fmt.Errorf("%w for %s: %w", ErrInvalidQueueEntry, urlID, admissionErr)
		}
	}

	return candidates, nil
}

func parseCanonicalCrawlDepth(value string) (int, error) {
	if value == "" || (value != "0" && (value[0] < '1' || value[0] > '9')) {
		return 0, ErrInvalidCrawlDepth
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, ErrInvalidCrawlDepth
		}
	}
	depth, err := strconv.Atoi(value)
	if err != nil || depth < 0 || uint64(depth) > utils.MaxCrawlDepthV1 || strconv.Itoa(depth) != value {
		return 0, ErrInvalidCrawlDepth
	}
	return depth, nil
}

var claimURLV1Script = redis.NewScript(`
local score = redis.call('ZSCORE', KEYS[1], ARGV[1])
local canonical_url = redis.call('HGET', KEYS[2], ARGV[1])
local depth = redis.call('HGET', KEYS[3], ARGV[1])
if not score or not canonical_url or not depth then
  return 0
end
local parsed_score = tonumber(score)
local expected_score = tonumber(ARGV[3])
if not parsed_score or parsed_score ~= parsed_score or parsed_score == math.huge or parsed_score == -math.huge then
  return redis.error_reply('INVALID_QUEUE_SCORE')
end
if not expected_score or expected_score ~= expected_score or expected_score == math.huge or expected_score == -math.huge then
  return redis.error_reply('INVALID_QUEUE_SCORE')
end
if parsed_score ~= expected_score or canonical_url ~= ARGV[2] or depth ~= ARGV[4] then
  return 0
end
return redis.call('ZREM', KEYS[1], ARGV[1])
`)

// ClaimURL atomically removes the candidate only when its score, canonical URL,
// and depth still match the inspected snapshot. False requires a fresh scan.
func (db *Database) ClaimURL(candidate CrawlCandidate) (bool, error) {
	if err := db.requireClient(); err != nil {
		return false, err
	}
	if !utils.IsURLIDV1(candidate.URLID) || candidate.CanonicalURL == "" || candidate.Depth < 0 || uint64(candidate.Depth) > utils.MaxCrawlDepthV1 {
		return false, ErrInvalidQueueEntry
	}
	if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
		return false, ErrInvalidQueueScore
	}
	result, err := claimURLV1Script.Run(
		db.context(),
		db.Client,
		[]string{db.queueKey(), db.urlsKey(), db.depthsKey()},
		candidate.URLID,
		candidate.CanonicalURL,
		strconv.FormatFloat(candidate.Score, 'g', -1, 64),
		strconv.Itoa(candidate.Depth),
	).Int()
	if err != nil {
		if strings.Contains(err.Error(), "INVALID_QUEUE_SCORE") {
			return false, fmt.Errorf("%w: %s", ErrInvalidQueueScore, candidate.URLID)
		}
		return false, fmt.Errorf("claim URL ID %s: %w", candidate.URLID, err)
	}
	return result == 1, nil
}

// ExistsInQueue looks up a queue member by V1 URL ID. A missing member is not
// an error; Redis failures remain distinguishable from absence.
func (db *Database) ExistsInQueue(urlID string) (score float64, exists bool, err error) {
	if err := db.requireClient(); err != nil {
		return 0, false, err
	}
	if !utils.IsURLIDV1(urlID) {
		return 0, false, fmt.Errorf("%w: ref=%s", ErrInvalidURLID, queueMemberReference(urlID))
	}

	result, err := db.Client.ZScore(db.context(), db.queueKey(), urlID).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("look up URL ID in crawl queue: %w", err)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, false, fmt.Errorf("%w: %s", ErrInvalidQueueScore, urlID)
	}
	return result, true, nil
}

// HasURLBeenVisited is intentionally process-local for now. Durable leases,
// attempts, and retry state require a separate crawl-job contract.
func (db *Database) HasURLBeenVisited(_ string) (bool, error) {
	return false, nil
}

// VisitPage intentionally does not persist crawl-job state. See
// HasURLBeenVisited for the lifecycle boundary.
func (db *Database) VisitPage(_ string) error {
	return nil
}

// PopURL returns the queue's URL ID, its exact canonical URL lookup, and score.
// It never reconstructs a URL or forces a scheme. A popped orphan or corrupt
// lookup is returned as an error so it cannot reach the HTTP client.
//
// V1 queue membership is only pending-work admission; it has no enabled or
// cancelled state. A producer must ZREM a disabled/cancelled URL ID before it
// is popped. Once popped, cancellation, leases, attempts, retries, and ACKs
// require the explicitly separate durable crawl-job contract.
func (db *Database) PopURL() (urlID string, canonicalURL string, score float64, err error) {
	if err := db.requireClient(); err != nil {
		return "", "", 0, err
	}

	var memberValue interface{}
	results, popErr := db.Client.ZPopMin(db.context(), db.queueKey(), 1).Result()
	if popErr != nil {
		return "", "", 0, fmt.Errorf("pop URL from V1 crawl queue: %w", popErr)
	}
	if len(results) > 0 {
		memberValue = results[0].Member
		score = results[0].Score
	} else if db.popTimeout() < 0 {
		return "", "", 0, fmt.Errorf("pop URL from V1 crawl queue: %w", redis.Nil)
	} else {
		result, popErr := db.Client.BZPopMin(db.context(), db.popTimeout(), db.queueKey()).Result()
		if popErr != nil {
			return "", "", 0, fmt.Errorf("pop URL from V1 crawl queue: %w", popErr)
		}
		memberValue = result.Z.Member
		score = result.Z.Score
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return "", "", score, ErrInvalidQueueScore
	}

	member, ok := memberValue.(string)
	if !ok {
		return "", "", score, ErrInvalidQueueMember
	}
	if !utils.IsURLIDV1(member) {
		return "", "", score, fmt.Errorf("%w: ref=%s", ErrInvalidURLID, queueMemberReference(member))
	}
	urlID = member

	canonicalURL, err = db.Client.HGet(db.context(), db.urlsKey(), urlID).Result()
	if errors.Is(err, redis.Nil) {
		return urlID, "", score, fmt.Errorf("%w: %s", ErrOrphanURLLookup, urlID)
	}
	if err != nil {
		return urlID, "", score, fmt.Errorf("resolve canonical URL for ID %s: %w", urlID, err)
	}

	identity, canonicalizationErr := utils.CanonicalizeURLV1(canonicalURL)
	if canonicalizationErr != nil {
		return urlID, "", score, fmt.Errorf("%w for %s: %w", ErrInvalidQueueEntry, urlID, canonicalizationErr)
	}
	if identity.CanonicalURL != canonicalURL || identity.URLID != urlID {
		return urlID, "", score, fmt.Errorf("%w: %s", ErrInvalidQueueEntry, urlID)
	}
	if admissionErr := utils.RequireStaticCrawlEligibility(identity); admissionErr != nil {
		return urlID, "", score, fmt.Errorf("%w for %s: %w", ErrInvalidQueueEntry, urlID, admissionErr)
	}
	depthValue, depthErr := db.Client.HGet(db.context(), db.depthsKey(), urlID).Result()
	if errors.Is(depthErr, redis.Nil) {
		return urlID, "", score, fmt.Errorf("%w for %s: missing crawl depth", ErrInvalidQueueEntry, urlID)
	}
	if depthErr != nil {
		return urlID, "", score, fmt.Errorf("resolve crawl depth for ID %s: %w", urlID, depthErr)
	}
	if _, depthErr := parseCanonicalCrawlDepth(depthValue); depthErr != nil {
		return urlID, "", score, fmt.Errorf("%w for %s", ErrInvalidCrawlDepth, urlID)
	}

	return urlID, canonicalURL, score, nil
}

func (db *Database) PopSignalQueue() (string, error) {
	if err := db.requireClient(); err != nil {
		return "", err
	}
	result, err := db.Client.BRPop(db.context(), 0, utils.SignalQueueKey).Result()
	if err != nil {
		return "", fmt.Errorf("pop from signal queue: %w", err)
	}
	if len(result) != 2 {
		return "", fmt.Errorf("signal queue returned %d values", len(result))
	}
	return result[1], nil
}

func (db *Database) GetIndexerQueueSize() (int64, error) {
	if err := db.requireClient(); err != nil {
		return -1, err
	}
	size, err := db.Client.LLen(db.context(), utils.IndexerQueueKey).Result()
	if err != nil {
		return -1, fmt.Errorf("get %s size: %w", utils.IndexerQueueKey, err)
	}
	return size, nil
}
