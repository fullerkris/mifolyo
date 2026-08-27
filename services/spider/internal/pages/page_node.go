package pages

import (
	"fmt"
	"strings"
)

type PageNode struct {
	NormalizedURL      string              // Key
	NormalizedLinkURLs map[string]struct{} // Set of strings as values
}

func CreatePageNode(normalizedURL string) *PageNode {
	return &PageNode{
		NormalizedURL:      normalizedURL,
		NormalizedLinkURLs: make(map[string]struct{}),
	}
}

func (b *PageNode) AppendLink(newNormalizedLink string) {
	// Check if NormalizedLinkURLs has been initialized before
	if b.NormalizedLinkURLs == nil {
		b.NormalizedLinkURLs = make(map[string]struct{})
	}

	b.NormalizedLinkURLs[newNormalizedLink] = struct{}{}
}

func (b *PageNode) GetLinks() []string {
	var links []string
	for link := range b.NormalizedLinkURLs {
		links = append(links, link)
	}

	return links
}

func (b *PageNode) String() string {
	var links []string

	for link := range b.NormalizedLinkURLs {
		links = append(links, link)
	}

	return fmt.Sprintf(
		"\n-------------------------------------------------\n"+
			"%s has %d backlinks:\n"+
			"%v\n"+
			"-------------------------------------------------\n",
		b.NormalizedURL, len(b.NormalizedLinkURLs), strings.Join(links, "\n"),
	)
}
