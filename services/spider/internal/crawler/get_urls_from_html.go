package crawler

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func getURLsFromHTML(htmlBody string, rawURL string) ([]string, map[string]map[string]string, error) {
	baseIdentity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return nil, nil, err
	}
	baseURL, err := url.Parse(baseIdentity.CanonicalURL)
	if err != nil {
		return nil, nil, err
	}

	node, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		return nil, nil, err
	}

	linksSet := make(map[string]struct{})
	imagesMap := make(map[string]map[string]string)
	traverse(node, baseURL, linksSet, imagesMap)

	links := make([]string, 0, len(linksSet))
	for link := range linksSet {
		links = append(links, link)
	}
	return links, imagesMap, nil
}

func traverse(node *html.Node, baseURL *url.URL, linksSet map[string]struct{}, imagesMap map[string]map[string]string) {
	if node == nil {
		return
	}

	if node.Type == html.ElementNode && node.Data == "a" {
		for _, attr := range node.Attr {
			if attr.Key != "href" || malformedHTMLURLAttribute(attr.Val) {
				continue
			}
			if canonicalURL, ok := canonicalizeReference(baseURL, attr.Val); ok {
				linksSet[canonicalURL] = struct{}{}
			}
		}
	} else if node.Type == html.ElementNode && node.Data == "img" {
		imageDetails := make(map[string]string)
		for _, attr := range node.Attr {
			switch attr.Key {
			case "src":
				if malformedHTMLURLAttribute(attr.Val) {
					continue
				}
				if canonicalURL, ok := canonicalizeReference(baseURL, attr.Val); ok {
					imageDetails["src"] = canonicalURL
				}
			case "alt":
				imageDetails["alt"] = attr.Val
			}
		}

		if imageURL := imageDetails["src"]; imageURL != "" {
			imagesMap[imageURL] = imageDetails
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		traverse(child, baseURL, linksSet, imagesMap)
	}
}

func malformedHTMLURLAttribute(value string) bool {
	return strings.ContainsAny(value, " <>\"")
}

func canonicalizeReference(baseURL *url.URL, rawReference string) (string, bool) {
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
