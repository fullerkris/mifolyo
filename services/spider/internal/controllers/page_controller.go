package controllers

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawler"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var ErrInvalidPage = errors.New("invalid page data")

type redactedControllerError struct {
	category string
	causes   []error
}

func (err *redactedControllerError) Error() string {
	return err.category
}

func (err *redactedControllerError) Unwrap() []error {
	return err.causes
}

func controllerError(category string, causes ...error) error {
	filtered := make([]error, 0, len(causes))
	for _, cause := range causes {
		if cause != nil {
			filtered = append(filtered, cause)
		}
	}
	return &redactedControllerError{category: category, causes: filtered}
}

func valueReference(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:8])
}

var publishPagesScript = redis.NewScript(`
local expected_publication = ARGV[1]
local page_count = tonumber(ARGV[2])
local queue_type = redis.call("TYPE", KEYS[1])["ok"]
local marker_type = redis.call("TYPE", KEYS[2])["ok"]
if queue_type ~= "none" and queue_type ~= "list" then
  return redis.error_reply("indexer queue must be a list")
end
if marker_type ~= "none" and marker_type ~= "string" then
  return redis.error_reply("publication marker must be a string")
end
if redis.call("EXISTS", KEYS[2]) == 1 then
  return 0
end
if not page_count or page_count < 1 or #ARGV ~= 3 + (page_count * 2) then
  return redis.error_reply("invalid page publication arguments")
end
for index = 1, page_count do
  local page_key = ARGV[2 + index]
  local manifest_key = ARGV[2 + page_count + index]
  if redis.call("TYPE", page_key)["ok"] ~= "hash" then
    return redis.error_reply("page publication key must be a hash")
  end
  if redis.call("HGET", page_key, "publication_id") ~= expected_publication then
    return redis.error_reply("page publication ID mismatch")
  end
  if redis.call("TYPE", manifest_key)["ok"] ~= "hash" then
    return redis.error_reply("image manifest key must be a hash")
  end
  if redis.call("HGET", manifest_key, "publication_id") ~= expected_publication then
    return redis.error_reply("image manifest publication ID mismatch")
  end
end

local published = 0
for index = 1, page_count do
  local page_key = ARGV[2 + index]
  redis.call("LPUSH", KEYS[1], page_key)
  published = published + 1
end
redis.call("SET", KEYS[2], "1", "EX", ARGV[#ARGV])
return published
`)

type PageController struct {
	db *database.Database
}

func NewPageController(db *database.Database) *PageController {
	return &PageController{db: db}
}

func (pgc *PageController) GetAllPages() map[string]*pages.Page {
	log.Printf("Fetching data from Redis...\n")
	redisPages := make(map[string]*pages.Page)

	keys, err := pgc.db.Client.Keys(pgc.db.Context, utils.PagePrefix+":*").Result()
	if err != nil {
		log.Printf("Error fetching page data from Redis\n")
		return nil
	}

	pipeline := pgc.db.Client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(keys))
	for index, key := range keys {
		cmds[index] = pipeline.HGetAll(pgc.db.Context, key)
	}

	if _, err = pipeline.Exec(pgc.db.Context); err != nil {
		log.Printf("Error executing page data Redis pipeline")
		return nil
	}

	for _, command := range cmds {
		data, resultErr := command.Result()
		if resultErr != nil {
			log.Printf("Error reading page data Redis pipeline result")
			return nil
		}

		page, decodeErr := pages.DehashPage(data)
		if decodeErr != nil {
			log.Printf("Error decoding persisted page data")
			return nil
		}
		redisPages[page.NormalizedURL] = page
	}

	return redisPages
}

func (pgc *PageController) SavePages(crawcfg *crawler.CrawlerConfig) error {
	data := crawcfg.Pages
	log.Printf("Writing %d entries to the db...\n", len(data))
	if len(data) == 0 {
		return nil
	}
	publicationID, err := batchPublicationID(crawcfg)
	if err != nil {
		return err
	}
	published, err := pgc.BatchPublished(crawcfg)
	if err != nil {
		return fmt.Errorf("check page publication state: %w", err)
	}
	if published {
		return nil
	}

	// Page data is durable before any indexer notification is published. The
	// caller invokes PublishPages only after links and images also succeed.
	pipeline := pgc.db.Client.Pipeline()
	for _, page := range data {
		pageHash, err := pages.HashPage(page)
		if err != nil {
			return controllerError(
				fmt.Sprintf("invalid page data: hash page_ref=%s", valueReference(page.NormalizedURL)),
				ErrInvalidPage,
				err,
			)
		}
		pageHash["publication_id"] = publicationID

		pageKey := pagePublicationKey(publicationID, page.NormalizedURL)
		pipeline.HSet(pgc.db.Context, pageKey, pageHash)
	}

	if _, err := pipeline.Exec(pgc.db.Context); err != nil {
		return controllerError("persist page data", err)
	}
	log.Printf("Successfully written %d entries to the db!", len(data))
	return nil
}

func (pgc *PageController) BatchPublished(crawcfg *crawler.CrawlerConfig) (bool, error) {
	if len(crawcfg.Pages) == 0 {
		return false, nil
	}
	publicationID, err := batchPublicationID(crawcfg)
	if err != nil {
		return false, err
	}
	published, err := pgc.db.Client.Exists(
		pgc.db.Context,
		publicationMarkerKey(publicationID),
	).Result()
	if err != nil {
		return false, controllerError("check page publication state", err)
	}
	return published == 1, nil
}

// PublishPages exposes a completed batch to the indexer with one atomic Redis
// list operation. It must run only after page, link, and image persistence has
// succeeded for the batch.
func (pgc *PageController) PublishPages(crawcfg *crawler.CrawlerConfig) error {
	if len(crawcfg.Pages) == 0 {
		return nil
	}
	publicationID, err := batchPublicationID(crawcfg)
	if err != nil {
		return err
	}

	pageKeys := make([]string, 0, len(crawcfg.Pages))
	for _, page := range crawcfg.Pages {
		pageKeys = append(pageKeys, pagePublicationKey(publicationID, page.NormalizedURL))
	}
	sort.Strings(pageKeys)
	values := make([]interface{}, 0, len(pageKeys)*2+3)
	values = append(values, publicationID, strconv.Itoa(len(pageKeys)))
	for _, pageKey := range pageKeys {
		values = append(values, pageKey)
	}
	for _, pageKey := range pageKeys {
		_, normalizedURL, parseErr := parsePagePublicationKey(pageKey)
		if parseErr != nil {
			return parseErr
		}
		values = append(values, imageManifestPublicationKey(publicationID, normalizedURL))
	}
	values = append(values, strconv.FormatInt(int64(utils.PagePublicationTTL/time.Second), 10))

	if _, err := publishPagesScript.Run(
		pgc.db.Context,
		pgc.db.Client,
		[]string{utils.IndexerQueueKey, publicationMarkerKey(publicationID)},
		values...,
	).Result(); err != nil {
		return controllerError("publish pages to indexer queue", err)
	}
	log.Printf("Published %d pages to the indexer queue!", len(pageKeys))
	return nil
}

func batchPublicationID(crawcfg *crawler.CrawlerConfig) (string, error) {
	data := crawcfg.Pages
	for imagePageURL := range crawcfg.Images {
		if _, exists := data[imagePageURL]; !exists {
			return "", fmt.Errorf("%w: image record has no page publication", ErrInvalidPage)
		}
	}
	pageURLs := make([]string, 0, len(data))
	pagesByURL := make(map[string]*pages.Page, len(data))
	for _, page := range data {
		if page == nil {
			return "", fmt.Errorf("%w: nil page", ErrInvalidPage)
		}
		if _, exists := pagesByURL[page.NormalizedURL]; exists {
			return "", controllerError(
				fmt.Sprintf("invalid page data: duplicate normalized URL ref=%s", valueReference(page.NormalizedURL)),
				ErrInvalidPage,
			)
		}
		pageURLs = append(pageURLs, page.NormalizedURL)
		pagesByURL[page.NormalizedURL] = page
	}
	sort.Strings(pageURLs)

	digest := sha256.New()
	for _, pageURL := range pageURLs {
		page := pagesByURL[pageURL]
		for _, value := range []string{
			page.NormalizedURL,
			page.HTML,
			page.OriginalHTML,
			page.ContentType,
			strconv.Itoa(page.StatusCode),
			strconv.FormatInt(page.LastCrawled.UnixNano(), 10),
			strconv.FormatBool(page.Rendered),
			page.RenderPolicyRule,
			page.RenderPolicySHA256,
		} {
			writePublicationField(digest, value)
		}
		links := make([]string, 0)
		if outlinks := crawcfg.Outlinks[pageURL]; outlinks != nil {
			links = append(links, outlinks.GetLinks()...)
		}
		sort.Strings(links)
		writePublicationField(digest, strconv.Itoa(len(links)))
		for _, link := range links {
			writePublicationField(digest, link)
		}
		images, err := publicationImagesForPage(crawcfg, pageURL)
		if err != nil {
			return "", err
		}
		writePublicationField(digest, strconv.Itoa(len(images)))
		for _, image := range images {
			writePublicationField(digest, image.NormalizedSourceURL)
			writePublicationField(digest, image.Alt)
		}
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func parsePagePublicationKey(pageKey string) (string, string, error) {
	parts := strings.SplitN(pageKey, ":", 3)
	if len(parts) != 3 || parts[0] != utils.PagePrefix {
		return "", "", fmt.Errorf("%w: invalid page publication key", ErrInvalidPage)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", "", fmt.Errorf("%w: invalid page URL encoding", ErrInvalidPage)
	}
	return parts[1], string(decoded), nil
}

func publicationMarkerKey(publicationID string) string {
	return utils.PagePublicationPrefix + ":" + publicationID
}

func pagePublicationKey(publicationID, normalizedURL string) string {
	encodedURL := base64.RawURLEncoding.EncodeToString([]byte(normalizedURL))
	return strings.Join([]string{utils.PagePrefix, publicationID, encodedURL}, ":")
}

func outlinksPublicationKey(publicationID, normalizedURL string) string {
	encodedURL := base64.RawURLEncoding.EncodeToString([]byte(normalizedURL))
	return strings.Join([]string{utils.OutlinksPrefix, publicationID, encodedURL}, ":")
}

func imageManifestPublicationKey(publicationID, normalizedURL string) string {
	encodedURL := base64.RawURLEncoding.EncodeToString([]byte(normalizedURL))
	return strings.Join([]string{utils.PageImagesPrefix, publicationID, encodedURL}, ":")
}

func imagePayloadPublicationKey(publicationID, pageURL, imageURL string) string {
	encodedPageURL := base64.RawURLEncoding.EncodeToString([]byte(pageURL))
	encodedImageURL := base64.RawURLEncoding.EncodeToString([]byte(imageURL))
	return strings.Join([]string{utils.ImagePrefix, publicationID, encodedPageURL, encodedImageURL}, ":")
}

func writePublicationField(digest hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write([]byte(value))
}
