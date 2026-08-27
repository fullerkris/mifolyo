package controllers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawler"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

const (
	imageContractVersion  = "1"
	maxImagesPerPage      = 1000
	maxImageAltBytes      = 4096
	maxImageManifestBytes = 4 * 1024 * 1024
)

// persistImagesScript validates every immutable destination before writing any
// of them. Empty manifests are ordinary hashes with image_count=0 and [] keys.
var persistImagesScript = redis.NewScript(`
local argument = 1
for index = 1, #KEYS do
  local key_type = redis.call('TYPE', KEYS[index])['ok']
  if key_type ~= 'none' and key_type ~= 'hash' then
    return redis.error_reply('image publication keys must be hashes')
  end
  local field_count = tonumber(ARGV[argument])
  if not field_count or field_count < 1 or field_count > 10 then
    return redis.error_reply('invalid image publication field count')
  end
  argument = argument + 1
  if key_type == 'hash' and redis.call('HLEN', KEYS[index]) ~= field_count then
    return redis.error_reply('existing image publication field count mismatch')
  end
  for field = 1, field_count do
    if key_type == 'hash' and redis.call('HGET', KEYS[index], ARGV[argument]) ~= ARGV[argument + 1] then
      return redis.error_reply('existing image publication is immutable')
    end
    argument = argument + 2
  end
end
if argument ~= #ARGV + 1 then
  return redis.error_reply('invalid image publication arguments')
end

argument = 1
for index = 1, #KEYS do
  local field_count = tonumber(ARGV[argument])
  argument = argument + 1
  local values = {}
  for field = 1, field_count * 2 do
    values[field] = ARGV[argument]
    argument = argument + 1
  end
  if redis.call('TYPE', KEYS[index])['ok'] == 'none' then
    redis.call('HSET', KEYS[index], unpack(values))
  end
end
return #KEYS
`)

type ImageController struct {
	db *database.Database
}

func NewImageController(db *database.Database) *ImageController {
	return &ImageController{db: db}
}

type imageHash struct {
	key    string
	fields map[string]string
}

// SaveImages writes one immutable manifest for every page, including pages
// with zero images, plus publication-scoped metadata hashes. It intentionally
// performs no SADD and sets no TTL: only an image-indexer ACK may remove them.
func (controller *ImageController) SaveImages(crawcfg *crawler.CrawlerConfig) error {
	publicationID, err := batchPublicationID(crawcfg)
	if err != nil {
		return err
	}
	pageURLs := make([]string, 0, len(crawcfg.Pages))
	for pageURL := range crawcfg.Pages {
		pageURLs = append(pageURLs, pageURL)
	}
	sort.Strings(pageURLs)
	for imagePageURL := range crawcfg.Images {
		if _, exists := crawcfg.Pages[imagePageURL]; !exists {
			return fmt.Errorf("%w: image record has no page publication", ErrInvalidPage)
		}
	}

	hashes := make([]imageHash, 0, len(pageURLs))
	for _, pageURL := range pageURLs {
		images, validationErr := publicationImagesForPage(crawcfg, pageURL)
		if validationErr != nil {
			return validationErr
		}
		payloadKeys := make([]string, 0, len(images))
		for _, image := range images {
			payloadKey := imagePayloadPublicationKey(
				publicationID, pageURL, image.NormalizedSourceURL,
			)
			payloadKeys = append(payloadKeys, payloadKey)
			hashes = append(hashes, imageHash{key: payloadKey, fields: map[string]string{
				"contract_version":      imageContractVersion,
				"publication_id":        publicationID,
				"normalized_page_url":   pageURL,
				"normalized_source_url": image.NormalizedSourceURL,
				"alt":                   image.Alt,
			}})
		}
		encodedKeys, marshalErr := json.Marshal(payloadKeys)
		if marshalErr != nil {
			return fmt.Errorf("encode image manifest: %w", marshalErr)
		}
		if validationErr := validateImageManifestSize(encodedKeys); validationErr != nil {
			return validationErr
		}
		hashes = append(hashes, imageHash{
			key: imageManifestPublicationKey(publicationID, pageURL),
			fields: map[string]string{
				"contract_version": imageContractVersion,
				"publication_id":   publicationID,
				"normalized_url":   pageURL,
				"image_count":      strconv.Itoa(len(payloadKeys)),
				"image_keys":       string(encodedKeys),
			},
		})
	}
	if len(hashes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(hashes))
	arguments := make([]interface{}, 0, len(hashes)*12)
	for _, hash := range hashes {
		keys = append(keys, hash.key)
		fieldNames := make([]string, 0, len(hash.fields))
		for field := range hash.fields {
			fieldNames = append(fieldNames, field)
		}
		sort.Strings(fieldNames)
		arguments = append(arguments, strconv.Itoa(len(fieldNames)))
		for _, field := range fieldNames {
			arguments = append(arguments, field, hash.fields[field])
		}
	}
	if _, err := persistImagesScript.Run(
		controller.db.Context, controller.db.Client, keys, arguments...,
	).Result(); err != nil {
		return controllerError("persist immutable image publication", err)
	}
	return nil
}

func validateImageManifestSize(encodedKeys []byte) error {
	// Python accepts exactly 4 MiB and rejects only values above the bound.
	if len(encodedKeys) > maxImageManifestBytes {
		return fmt.Errorf("%w: serialized image manifest exceeds limit", ErrInvalidPage)
	}
	return nil
}

func publicationImagesForPage(crawcfg *crawler.CrawlerConfig, pageURL string) ([]*pages.Image, error) {
	images := append([]*pages.Image(nil), crawcfg.Images[pageURL]...)
	if len(images) > maxImagesPerPage {
		return nil, fmt.Errorf("%w: image count exceeds limit", ErrInvalidPage)
	}
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		if image == nil || image.NormalizedPageURL != pageURL || image.NormalizedSourceURL == "" {
			return nil, fmt.Errorf("%w: invalid image record", ErrInvalidPage)
		}
		if len(image.Alt) > maxImageAltBytes {
			return nil, fmt.Errorf("%w: image alt exceeds limit", ErrInvalidPage)
		}
		identity, err := utils.CanonicalizeURLV1(image.NormalizedSourceURL)
		if err != nil || identity.CanonicalURL != image.NormalizedSourceURL {
			return nil, fmt.Errorf("%w: image source is not canonical", ErrInvalidPage)
		}
		if _, duplicate := seen[image.NormalizedSourceURL]; duplicate {
			return nil, fmt.Errorf("%w: duplicate image source", ErrInvalidPage)
		}
		seen[image.NormalizedSourceURL] = struct{}{}
	}
	sort.Slice(images, func(i, j int) bool {
		if images[i].NormalizedSourceURL == images[j].NormalizedSourceURL {
			return images[i].Alt < images[j].Alt
		}
		return images[i].NormalizedSourceURL < images[j].NormalizedSourceURL
	})
	return images, nil
}
