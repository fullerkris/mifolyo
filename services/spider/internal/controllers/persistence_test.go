package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawler"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func persistenceTestDatabase(t *testing.T) *database.Database {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &database.Database{Client: client, Context: context.Background()}
}

func TestPageControllerErrorsRedactURLAndContentWhilePreservingCauses(t *testing.T) {
	db := persistenceTestDatabase(t)
	pageURL := "https://example.com/private/path?token=do-not-log"
	page := pages.CreateRenderedPage(
		pageURL,
		"<html>private original content</html>",
		"<html>private rendered content</html>",
		"text/html",
		200,
		"",
		"",
	)
	crawcfg := &crawler.CrawlerConfig{Pages: map[string]*pages.Page{pageURL: page}}

	err := NewPageController(db).SavePages(crawcfg)
	if err == nil || !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("SavePages error = %v, want ErrInvalidPage", err)
	}
	for _, sensitive := range []string{pageURL, "do-not-log", "private rendered content"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("error leaked sensitive value %q: %v", sensitive, err)
		}
	}
	if !strings.Contains(err.Error(), valueReference(pageURL)) {
		t.Fatalf("error lacks stable page reference: %v", err)
	}

	cause := errors.New("redis failure for " + pageURL)
	wrapped := controllerError("persist page data", cause)
	if !errors.Is(wrapped, cause) {
		t.Fatal("redacted controller error does not preserve its cause")
	}
	if strings.Contains(wrapped.Error(), pageURL) {
		t.Fatalf("redacted controller error leaked cause: %v", wrapped)
	}
}

func TestSavePagesDefersIndexerPublicationUntilExplicitPublish(t *testing.T) {
	db := persistenceTestDatabase(t)
	page := pages.CreatePage("https://example.com/page", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{Pages: map[string]*pages.Page{page.NormalizedURL: page}}
	controller := NewPageController(db)

	if err := controller.SavePages(crawcfg); err != nil {
		t.Fatalf("SavePages: %v", err)
	}
	publicationID, err := batchPublicationID(crawcfg)
	if err != nil {
		t.Fatal(err)
	}
	pageKey := pagePublicationKey(publicationID, page.NormalizedURL)
	if exists, err := db.Client.Exists(db.Context, pageKey).Result(); err != nil || exists != 1 {
		t.Fatalf("page hash exists=%d error=%v", exists, err)
	}
	queued, err := db.Client.LRange(db.Context, utils.IndexerQueueKey, 0, -1).Result()
	if err != nil || len(queued) != 0 {
		t.Fatalf("page was published before the batch completed: %v, %v", queued, err)
	}
	if err := NewImageController(db).SaveImages(crawcfg); err != nil {
		t.Fatalf("SaveImages: %v", err)
	}

	if err := controller.PublishPages(crawcfg); err != nil {
		t.Fatalf("PublishPages: %v", err)
	}
	queued, err = db.Client.LRange(db.Context, utils.IndexerQueueKey, 0, -1).Result()
	if err != nil || len(queued) != 1 || queued[0] != pageKey {
		t.Fatalf("indexer queue = %v, %v", queued, err)
	}
	ttl, err := db.Client.TTL(db.Context, publicationMarkerKey(publicationID)).Result()
	if err != nil || ttl <= 0 || ttl > utils.PagePublicationTTL {
		t.Fatalf("publication marker TTL = %s, %v", ttl, err)
	}

	if err := controller.PublishPages(crawcfg); err != nil {
		t.Fatalf("retry PublishPages: %v", err)
	}
	queued, err = db.Client.LRange(db.Context, utils.IndexerQueueKey, 0, -1).Result()
	if err != nil || len(queued) != 1 || queued[0] != pageKey {
		t.Fatalf("publication retry duplicated queue entry: %v, %v", queued, err)
	}
}

func TestPublishPagesRequiresImmutableImageManifest(t *testing.T) {
	db := persistenceTestDatabase(t)
	page := pages.CreatePage("https://example.com/page", "<html></html>", "text/html", 200)
	batch := &crawler.CrawlerConfig{Pages: map[string]*pages.Page{page.NormalizedURL: page}}
	controller := NewPageController(db)
	if err := controller.SavePages(batch); err != nil {
		t.Fatal(err)
	}
	if err := controller.PublishPages(batch); err == nil {
		t.Fatal("publication without its image manifest was accepted")
	}
	if queued, err := db.Client.LLen(db.Context, utils.IndexerQueueKey).Result(); err != nil || queued != 0 {
		t.Fatalf("incomplete publication queued=%d error=%v", queued, err)
	}
}

func TestPublishPagesQueuesANewPageVersion(t *testing.T) {
	db := persistenceTestDatabase(t)
	controller := NewPageController(db)
	page := pages.CreatePage("https://example.com/page", "<html>first</html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{Pages: map[string]*pages.Page{page.NormalizedURL: page}}

	if err := controller.SavePages(crawcfg); err != nil {
		t.Fatal(err)
	}
	if err := NewImageController(db).SaveImages(crawcfg); err != nil {
		t.Fatal(err)
	}
	if err := controller.PublishPages(crawcfg); err != nil {
		t.Fatal(err)
	}

	page = pages.CreatePage("https://example.com/page", "<html>second</html>", "text/html", 200)
	crawcfg.Pages[page.NormalizedURL] = page
	if err := controller.SavePages(crawcfg); err != nil {
		t.Fatal(err)
	}
	if err := NewImageController(db).SaveImages(crawcfg); err != nil {
		t.Fatal(err)
	}
	if err := controller.PublishPages(crawcfg); err != nil {
		t.Fatal(err)
	}

	queued, err := db.Client.LRange(db.Context, utils.IndexerQueueKey, 0, -1).Result()
	if err != nil || len(queued) != 2 {
		t.Fatalf("indexer queue = %v, %v", queued, err)
	}
}

func TestPublicationRetryAfterConsumptionDoesNotRecreateOrRequeuePage(t *testing.T) {
	db := persistenceTestDatabase(t)
	controller := NewPageController(db)
	page := pages.CreatePage("https://example.com/page", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{Pages: map[string]*pages.Page{page.NormalizedURL: page}}
	publicationID, err := batchPublicationID(crawcfg)
	if err != nil {
		t.Fatal(err)
	}
	pageKey := pagePublicationKey(publicationID, page.NormalizedURL)

	if err := controller.SavePages(crawcfg); err != nil {
		t.Fatal(err)
	}
	if err := NewImageController(db).SaveImages(crawcfg); err != nil {
		t.Fatal(err)
	}
	if err := controller.PublishPages(crawcfg); err != nil {
		t.Fatal(err)
	}
	if queued, err := db.Client.RPop(db.Context, utils.IndexerQueueKey).Result(); err != nil || queued != pageKey {
		t.Fatalf("consume publication = %q, %v", queued, err)
	}
	if err := db.Client.Del(db.Context, pageKey).Err(); err != nil {
		t.Fatal(err)
	}

	if err := controller.SavePages(crawcfg); err != nil {
		t.Fatalf("retry SavePages: %v", err)
	}
	if err := controller.PublishPages(crawcfg); err != nil {
		t.Fatalf("retry PublishPages: %v", err)
	}
	if exists, err := db.Client.Exists(db.Context, pageKey).Result(); err != nil || exists != 0 {
		t.Fatalf("consumed page hash was recreated: exists=%d error=%v", exists, err)
	}
	if queued, err := db.Client.LLen(db.Context, utils.IndexerQueueKey).Result(); err != nil || queued != 0 {
		t.Fatalf("consumed publication was requeued: queued=%d error=%v", queued, err)
	}
}

func TestPersistenceControllersReturnRedisCommandErrors(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		db := persistenceTestDatabase(t)
		page := pages.CreatePage("https://example.com/page", "<html></html>", "text/html", 200)
		crawcfg := &crawler.CrawlerConfig{Pages: map[string]*pages.Page{page.NormalizedURL: page}}
		publicationID, publicationErr := batchPublicationID(crawcfg)
		if publicationErr != nil {
			t.Fatal(publicationErr)
		}
		pageKey := pagePublicationKey(publicationID, page.NormalizedURL)
		if err := db.Client.Set(db.Context, pageKey, "wrong-type", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if err := NewPageController(db).SavePages(crawcfg); err == nil {
			t.Fatal("SavePages returned nil for WRONGTYPE")
		}
		queued, err := db.Client.LLen(db.Context, utils.IndexerQueueKey).Result()
		if err != nil || queued != 0 {
			t.Fatalf("failed page persistence published %d notifications: %v", queued, err)
		}
	})

	t.Run("links", func(t *testing.T) {
		db := persistenceTestDatabase(t)
		key := "https://example.com/target"
		node := pages.CreatePageNode(key)
		node.AppendLink("https://example.com/source")
		page := pages.CreatePage(key, "<html></html>", "text/html", 200)
		crawcfg := &crawler.CrawlerConfig{
			Pages:     map[string]*pages.Page{key: page},
			Backlinks: make(map[string]*pages.PageNode),
			Outlinks:  map[string]*pages.PageNode{key: node},
		}
		publicationID, publicationErr := batchPublicationID(crawcfg)
		if publicationErr != nil {
			t.Fatal(publicationErr)
		}
		if err := db.Client.Set(db.Context, outlinksPublicationKey(publicationID, key), "wrong-type", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if err := NewLinksController(db).SaveLinks(crawcfg); err == nil {
			t.Fatal("SaveLinks returned nil for WRONGTYPE")
		}
	})

	t.Run("images", func(t *testing.T) {
		db := persistenceTestDatabase(t)
		pageURL := "https://example.com/page"
		imageURL := "https://example.com/image.jpg"
		page := pages.CreatePage(pageURL, "<html></html>", "text/html", 200)
		crawcfg := &crawler.CrawlerConfig{
			Pages: map[string]*pages.Page{pageURL: page},
			Images: map[string][]*pages.Image{pageURL: {{
				NormalizedPageURL:   pageURL,
				NormalizedSourceURL: imageURL,
			}}},
		}
		publicationID, err := batchPublicationID(crawcfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Client.Set(db.Context, imagePayloadPublicationKey(publicationID, pageURL, imageURL), "wrong-type", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if err := NewImageController(db).SaveImages(crawcfg); err == nil {
			t.Fatal("SaveImages returned nil for WRONGTYPE")
		}
	})
}

func TestImagePublicationIsImmutableSortedAndRepresentsEmpty(t *testing.T) {
	db := persistenceTestDatabase(t)
	pageURL := "https://example.com/page"
	page := pages.CreatePage(pageURL, "<html></html>", "text/html", 200)
	batch := &crawler.CrawlerConfig{
		Pages:  map[string]*pages.Page{pageURL: page},
		Images: map[string][]*pages.Image{},
	}
	emptyPublication, err := batchPublicationID(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewImageController(db).SaveImages(batch); err != nil {
		t.Fatal(err)
	}
	emptyManifest := imageManifestPublicationKey(emptyPublication, pageURL)
	manifest, err := db.Client.HGetAll(db.Context, emptyManifest).Result()
	if err != nil || manifest["image_count"] != "0" || manifest["image_keys"] != "[]" {
		t.Fatalf("empty manifest = %#v, %v", manifest, err)
	}

	batch.Images[pageURL] = []*pages.Image{
		{NormalizedPageURL: pageURL, NormalizedSourceURL: "https://example.com/z.jpg", Alt: "z"},
		{NormalizedPageURL: pageURL, NormalizedSourceURL: "https://example.com/a.jpg", Alt: "a"},
	}
	imagePublication, err := batchPublicationID(batch)
	if err != nil {
		t.Fatal(err)
	}
	if imagePublication == emptyPublication {
		t.Fatal("image-only change did not change publication ID")
	}
	if err := NewImageController(db).SaveImages(batch); err != nil {
		t.Fatal(err)
	}
	imageManifest := imageManifestPublicationKey(imagePublication, pageURL)
	manifest, err = db.Client.HGetAll(db.Context, imageManifest).Result()
	if err != nil || manifest["image_count"] != "2" {
		t.Fatalf("image manifest = %#v, %v", manifest, err)
	}
	var payloadKeys []string
	if err := json.Unmarshal([]byte(manifest["image_keys"]), &payloadKeys); err != nil {
		t.Fatal(err)
	}
	expected := []string{
		imagePayloadPublicationKey(imagePublication, pageURL, "https://example.com/a.jpg"),
		imagePayloadPublicationKey(imagePublication, pageURL, "https://example.com/z.jpg"),
	}
	if len(payloadKeys) != 2 || payloadKeys[0] != expected[0] || payloadKeys[1] != expected[1] {
		t.Fatalf("payload keys = %#v", payloadKeys)
	}
	if exists, _ := db.Client.Exists(db.Context, emptyManifest).Result(); exists != 1 {
		t.Fatal("new publication overwrote the empty A manifest")
	}
	if ttl, _ := db.Client.TTL(db.Context, imageManifest).Result(); ttl != -1 {
		t.Fatalf("immutable manifest unexpectedly expires: %s", ttl)
	}
}

func TestImageManifestSerializedSizeBoundaryMatchesConsumer(t *testing.T) {
	if err := validateImageManifestSize(make([]byte, maxImageManifestBytes)); err != nil {
		t.Fatalf("exact 4 MiB manifest rejected: %v", err)
	}
	if err := validateImageManifestSize(make([]byte, maxImageManifestBytes+1)); err == nil || !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("over-limit manifest error = %v, want ErrInvalidPage", err)
	}
}

func TestOlderClaimCompletionCannotDeleteNewerPublication(t *testing.T) {
	db := persistenceTestDatabase(t)
	controller := NewPageController(db)
	url := "https://example.com/page"

	pageA := pages.CreatePage(url, "<html>A</html>", "text/html", 200)
	linksA := pages.CreatePageNode(url)
	linksA.AppendLink("https://example.com/a")
	batchA := &crawler.CrawlerConfig{
		Pages: map[string]*pages.Page{url: pageA}, Outlinks: map[string]*pages.PageNode{url: linksA},
	}
	publicationA, _ := batchPublicationID(batchA)
	keyA := pagePublicationKey(publicationA, url)
	if err := controller.SavePages(batchA); err != nil {
		t.Fatal(err)
	}
	if err := NewLinksController(db).SaveLinks(batchA); err != nil {
		t.Fatal(err)
	}
	if err := NewImageController(db).SaveImages(batchA); err != nil {
		t.Fatal(err)
	}
	if err := controller.PublishPages(batchA); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.Client.BRPopLPush(db.Context, utils.IndexerQueueKey, "pages_queue:processing", 0).Result()
	if err != nil || claimed != keyA {
		t.Fatalf("claim A = %q, %v", claimed, err)
	}

	pageB := pages.CreatePage(url, "<html>B</html>", "text/html", 200)
	linksB := pages.CreatePageNode(url)
	linksB.AppendLink("https://example.com/b")
	batchB := &crawler.CrawlerConfig{
		Pages: map[string]*pages.Page{url: pageB}, Outlinks: map[string]*pages.PageNode{url: linksB},
	}
	publicationB, _ := batchPublicationID(batchB)
	keyB := pagePublicationKey(publicationB, url)
	if err := controller.SavePages(batchB); err != nil {
		t.Fatal(err)
	}
	if err := NewLinksController(db).SaveLinks(batchB); err != nil {
		t.Fatal(err)
	}
	if err := NewImageController(db).SaveImages(batchB); err != nil {
		t.Fatal(err)
	}
	if err := controller.PublishPages(batchB); err != nil {
		t.Fatal(err)
	}

	// Model the indexer's immutable A completion: only A's exact keys are removed.
	if err := db.Client.Del(db.Context, keyA, outlinksPublicationKey(publicationA, url)).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Client.LRem(db.Context, "pages_queue:processing", 1, keyA).Err(); err != nil {
		t.Fatal(err)
	}
	if exists, _ := db.Client.Exists(db.Context, keyB).Result(); exists != 1 {
		t.Fatal("completion of A deleted B's immutable page")
	}
	if exists, _ := db.Client.Exists(db.Context, outlinksPublicationKey(publicationB, url)).Result(); exists != 1 {
		t.Fatal("completion of A deleted B's immutable outlinks")
	}
	queued, err := db.Client.LRange(db.Context, utils.IndexerQueueKey, 0, -1).Result()
	if err != nil || len(queued) != 1 || queued[0] != keyB {
		t.Fatalf("B queue entry = %v, %v", queued, err)
	}
}

func TestPublishPagesReturnsQueueTypeErrorWithoutChangingExistingValue(t *testing.T) {
	db := persistenceTestDatabase(t)
	if err := db.Client.Set(db.Context, utils.IndexerQueueKey, "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	page := pages.CreatePage("https://example.com/page", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{Pages: map[string]*pages.Page{page.NormalizedURL: page}}

	if err := NewPageController(db).PublishPages(crawcfg); err == nil {
		t.Fatal("PublishPages returned nil for WRONGTYPE")
	}
	stored, err := db.Client.Get(db.Context, utils.IndexerQueueKey).Result()
	if err != nil || stored != "wrong-type" {
		t.Fatalf("queue value = %q, %v", stored, err)
	}
}
