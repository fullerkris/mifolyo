package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var (
	ErrRedisNotConfigured = errors.New("Redis client is not configured")
	ErrInvalidQueueScore  = errors.New("crawl queue score must be finite")
	ErrInvalidURLID       = errors.New("invalid V1 URL ID")
	ErrURLIDCollision     = errors.New("V1 URL ID is already mapped to a different URL")
	ErrInvalidQueueMember = errors.New("crawl queue member is not a string URL ID")
	ErrOrphanURLLookup    = errors.New("crawl queue URL lookup is missing")
	ErrInvalidQueueEntry  = errors.New("crawl queue URL lookup does not match its V1 identity")
)

// Database owns the Redis connection and V1 crawl-queue key configuration.
// Empty key fields deliberately resolve to the versioned V1 defaults.
type Database struct {
	Client        *redis.Client
	Context       context.Context
	CrawlQueueKey string
	CrawlURLsKey  string
	// CrawlPopTimeout defaults to utils.Timeout. A negative value selects a
	// non-blocking ZPOPMIN, which is useful for bounded workers and tests.
	CrawlPopTimeout time.Duration
}

func (db *Database) ConnectToRedis(redisHost, redisPort, redisPassword, redisDB string) error {
	log.Println("Connecting to Redis...")

	dbIndex, err := strconv.Atoi(redisDB)
	if err != nil {
		return fmt.Errorf("parse Redis DB value: %w", err)
	}

	db.Client = redis.NewClient(&redis.Options{
		Addr:     redisHost + ":" + redisPort,
		Password: redisPassword,
		DB:       dbIndex,
	})
	db.Context = context.Background()

	if _, err = db.Client.Ping(db.context()).Result(); err != nil {
		return fmt.Errorf("connect to Redis at %s:%s: %w", redisHost, redisPort, err)
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

func (db *Database) popTimeout() time.Duration {
	if db.CrawlPopTimeout != 0 {
		return db.CrawlPopTimeout
	}
	return utils.Timeout
}

func (db *Database) requireClient() error {
	if db.Client == nil {
		return ErrRedisNotConfigured
	}
	return nil
}

var pushURLV1Script = redis.NewScript(`
local existing_url = redis.call('HGET', KEYS[2], ARGV[1])
if existing_url and existing_url ~= ARGV[2] then
  return redis.error_reply('URL_ID_COLLISION')
end

local existing_score = redis.call('ZSCORE', KEYS[1], ARGV[1])
redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])
if (not existing_score) or tonumber(ARGV[3]) < tonumber(existing_score) then
  redis.call('ZADD', KEYS[1], ARGV[3], ARGV[1])
end
return 1
`)

// PushURL applies V1 canonicalization and static admission before atomically
// storing URL ID => canonical URL and queueing the ID at its best (lowest)
// observed score.
func (db *Database) PushURL(rawURL string, score float64) error {
	if err := db.requireClient(); err != nil {
		return err
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return ErrInvalidQueueScore
	}

	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return fmt.Errorf("canonicalize URL for crawl queue: %w", err)
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return fmt.Errorf("URL %q is not statically crawl eligible: %w", identity.CanonicalURL, err)
	}

	keys := []string{db.queueKey(), db.urlsKey()}
	err = pushURLV1Script.Run(
		db.context(),
		db.Client,
		keys,
		identity.URLID,
		identity.CanonicalURL,
		strconv.FormatFloat(score, 'g', -1, 64),
	).Err()
	if err != nil {
		if strings.Contains(err.Error(), "URL_ID_COLLISION") {
			return fmt.Errorf("%w: %s", ErrURLIDCollision, identity.URLID)
		}
		return fmt.Errorf("store URL in V1 crawl queue: %w", err)
	}

	return nil
}

// ExistsInQueue looks up a queue member by V1 URL ID. A missing member is not
// an error; Redis failures remain distinguishable from absence.
func (db *Database) ExistsInQueue(urlID string) (score float64, exists bool, err error) {
	if err := db.requireClient(); err != nil {
		return 0, false, err
	}
	if !utils.IsURLIDV1(urlID) {
		return 0, false, fmt.Errorf("%w: %q", ErrInvalidURLID, urlID)
	}

	result, err := db.Client.ZScore(db.context(), db.queueKey(), urlID).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("look up URL ID in crawl queue: %w", err)
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

	member, ok := memberValue.(string)
	if !ok {
		return "", "", score, ErrInvalidQueueMember
	}
	urlID = member
	if !utils.IsURLIDV1(urlID) {
		return urlID, "", score, fmt.Errorf("%w: %q", ErrInvalidURLID, urlID)
	}

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
