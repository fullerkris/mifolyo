package crawler

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
)

func TestUpdateLinksKeepsCanonicalAbsoluteGraphIdentity(t *testing.T) {
	config := &CrawlerConfig{
		Mu:        &sync.Mutex{},
		Pages:     make(map[string]*pages.Page),
		Outlinks:  make(map[string]*pages.PageNode),
		Backlinks: make(map[string]*pages.PageNode),
		Images:    make(map[string][]*pages.Image),
	}

	config.UpdateLinks(
		"HTTPS://Example.COM:443/current/",
		[]string{
			"https://example.com/target/?b=2&a=1#fragment",
			"not-an-absolute-url",
			"c38d6638dfd6f0bf2ab2e8432d5303f377a1540aa1ee8207fc2742f7ff2104cd",
		},
	)

	currentURL := "https://example.com/current/"
	targetURL := "https://example.com/target/?b=2&a=1"
	outlinks, exists := config.Outlinks[currentURL]
	if !exists {
		t.Fatalf("canonical current URL %q was not retained as graph identity: %v", currentURL, config.Outlinks)
	}
	links := outlinks.GetLinks()
	if len(links) != 1 || links[0] != targetURL {
		t.Fatalf("outlinks = %v, want only canonical absolute URL %q", links, targetURL)
	}
	backlinks, exists := config.Backlinks[targetURL]
	if !exists {
		t.Fatalf("canonical target URL %q was not retained as graph identity", targetURL)
	}
	links = backlinks.GetLinks()
	if len(links) != 1 || links[0] != currentURL {
		t.Fatalf("backlinks = %v, want only canonical absolute URL %q", links, currentURL)
	}
}

func TestAddImagesRetainsOnlyStaticallyEligiblePolicyURLs(t *testing.T) {
	config := &CrawlerConfig{
		Mu:     &sync.Mutex{},
		Images: make(map[string][]*pages.Image),
		Policy: schedulerPolicy(t, 1),
	}
	config.AddImages("https://a.example.com/page", map[string]map[string]string{
		"https://a.example.com/image.png":          {"alt": "allowed"},
		"https://other.example.com/image.png":      {"alt": "not in policy"},
		"http://169.254.169.254/latest/meta-data/": {"alt": "literal address"},
	}, 1)
	config.AddImages("https://a.example.com/page", map[string]map[string]string{
		"https://a.example.com/too-deep.png": {"alt": "depth denied"},
	}, 2)

	images := config.Images["https://a.example.com/page"]
	if len(images) != 1 || images[0].NormalizedSourceURL != "https://a.example.com/image.png" {
		t.Fatalf("retained images = %#v", images)
	}
}

func TestReservePageAttemptIsAConcurrentHardLimit(t *testing.T) {
	config := &CrawlerConfig{
		Mu:       &sync.Mutex{},
		MaxPages: 10,
	}

	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 100 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if reservation, reserved := config.reservePageAttempt(); reserved {
				reservation.commit()
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()

	if got := accepted.Load(); got != 10 {
		t.Fatalf("accepted attempts = %d, want 10", got)
	}
	if config.PageAttempts != 10 {
		t.Fatalf("recorded attempts = %d, want 10", config.PageAttempts)
	}
}

func TestUnusedRequestReservationIsRefundedExactlyOnce(t *testing.T) {
	config := &CrawlerConfig{Mu: &sync.Mutex{}, MaxPages: 1}
	reservation, reserved := config.reservePageAttempt()
	if !reserved || config.PageAttempts != 1 {
		t.Fatalf("reservation = %v attempts=%d", reserved, config.PageAttempts)
	}
	reservation.refund()
	reservation.refund()
	if config.PageAttempts != 0 {
		t.Fatalf("refunded attempts = %d, want 0", config.PageAttempts)
	}
}
