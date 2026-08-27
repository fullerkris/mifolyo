package crawler

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

const (
	maxHTMLDiscoveryTokens   = 100_000
	maxUniqueOutgoingLinks   = 2_000
	maxUniqueImages          = 1_000
	maxRetainedImageAltBytes = 1_024
)

var ErrHTMLDiscoveryComplexity = errors.New("HTML discovery complexity limit exceeded")

type htmlDiscoveryLimit string

const (
	htmlDiscoveryTokenLimit        htmlDiscoveryLimit = "tokens"
	htmlDiscoveryLinkLimit         htmlDiscoveryLimit = "unique_links"
	htmlDiscoveryImageLimit        htmlDiscoveryLimit = "unique_images"
	htmlDiscoveryURLAttributeLimit htmlDiscoveryLimit = "url_attribute_bytes"
	htmlDiscoveryAltLimit          htmlDiscoveryLimit = "alt_bytes"
)

// HTMLDiscoveryComplexityError identifies which bounded streaming-discovery
// resource exceeded its limit. It unwraps to ErrHTMLDiscoveryComplexity.
type HTMLDiscoveryComplexityError struct {
	Resource htmlDiscoveryLimit
	Limit    int
}

func (err *HTMLDiscoveryComplexityError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s exceeds %d", ErrHTMLDiscoveryComplexity, err.Resource, err.Limit)
}

func (err *HTMLDiscoveryComplexityError) Unwrap() error {
	return ErrHTMLDiscoveryComplexity
}

func getURLsFromHTML(htmlBody string, rawURL string) ([]string, map[string]map[string]string, error) {
	baseIdentity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return nil, nil, err
	}
	baseURL, err := url.Parse(baseIdentity.CanonicalURL)
	if err != nil {
		return nil, nil, err
	}

	linksSet := make(map[string]struct{})
	imagesMap := make(map[string]map[string]string)
	tokenizer := html.NewTokenizer(strings.NewReader(htmlBody))
	tokenCount := 0
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			return nil, nil, tokenizer.Err()
		}

		tokenCount++
		if tokenCount > maxHTMLDiscoveryTokens {
			return nil, nil, newHTMLDiscoveryComplexityError(htmlDiscoveryTokenLimit, maxHTMLDiscoveryTokens)
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}

		token := tokenizer.Token()
		switch token.Data {
		case "a":
			if err := collectAnchorLinks(baseURL, token.Attr, linksSet); err != nil {
				return nil, nil, err
			}
		case "img":
			if err := collectImage(baseURL, token.Attr, imagesMap); err != nil {
				return nil, nil, err
			}
		}
	}

	links := make([]string, 0, len(linksSet))
	for link := range linksSet {
		links = append(links, link)
	}
	return links, imagesMap, nil
}

func collectAnchorLinks(baseURL *url.URL, attributes []html.Attribute, linksSet map[string]struct{}) error {
	for _, attribute := range attributes {
		if attribute.Key != "href" {
			continue
		}
		if err := validateURLAttributeLength(attribute.Val); err != nil {
			return err
		}
		if malformedHTMLURLAttribute(attribute.Val) {
			continue
		}
		canonicalURL, ok := canonicalizeReference(baseURL, attribute.Val)
		if !ok {
			continue
		}
		if _, exists := linksSet[canonicalURL]; exists {
			continue
		}
		if len(linksSet) >= maxUniqueOutgoingLinks {
			return newHTMLDiscoveryComplexityError(htmlDiscoveryLinkLimit, maxUniqueOutgoingLinks)
		}
		linksSet[canonicalURL] = struct{}{}
	}
	return nil
}

func collectImage(baseURL *url.URL, attributes []html.Attribute, imagesMap map[string]map[string]string) error {
	imageDetails := make(map[string]string)
	for _, attribute := range attributes {
		switch attribute.Key {
		case "src":
			if err := validateURLAttributeLength(attribute.Val); err != nil {
				return err
			}
			if malformedHTMLURLAttribute(attribute.Val) {
				continue
			}
			if canonicalURL, ok := canonicalizeReference(baseURL, attribute.Val); ok {
				imageDetails["src"] = canonicalURL
			}
		case "alt":
			if len(attribute.Val) > maxRetainedImageAltBytes {
				return newHTMLDiscoveryComplexityError(htmlDiscoveryAltLimit, maxRetainedImageAltBytes)
			}
			imageDetails["alt"] = attribute.Val
		}
	}

	imageURL := imageDetails["src"]
	if imageURL == "" {
		return nil
	}
	if _, exists := imagesMap[imageURL]; !exists && len(imagesMap) >= maxUniqueImages {
		return newHTMLDiscoveryComplexityError(htmlDiscoveryImageLimit, maxUniqueImages)
	}
	imagesMap[imageURL] = imageDetails
	return nil
}

func validateURLAttributeLength(value string) error {
	if len(value) > utils.MaxURLBytesV1 {
		return newHTMLDiscoveryComplexityError(htmlDiscoveryURLAttributeLimit, utils.MaxURLBytesV1)
	}
	return nil
}

func validateOutgoingLinkCount(links []string) error {
	if len(links) > maxUniqueOutgoingLinks {
		return newHTMLDiscoveryComplexityError(htmlDiscoveryLinkLimit, maxUniqueOutgoingLinks)
	}
	return nil
}

func newHTMLDiscoveryComplexityError(resource htmlDiscoveryLimit, limit int) error {
	return &HTMLDiscoveryComplexityError{Resource: resource, Limit: limit}
}

func malformedHTMLURLAttribute(value string) bool {
	return strings.ContainsAny(value, " <>\"")
}

func canonicalizeReference(baseURL *url.URL, rawReference string) (string, bool) {
	if len(rawReference) > utils.MaxURLBytesV1 {
		return "", false
	}
	reference, err := url.Parse(rawReference)
	if err != nil {
		return "", false
	}

	resolved := reference
	if !reference.IsAbs() {
		resolved = baseURL.ResolveReference(reference)
	}
	identity, err := utils.CanonicalizeURLV1(resolved.String())
	if err != nil {
		return "", false
	}
	return identity.CanonicalURL, true
}
