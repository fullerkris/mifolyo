package utils

// IsValidURL reports whether a URL is both valid V1 identity and statically
// eligible for crawling. It performs no DNS or network authorization.
func IsValidURL(link string) bool {
	result, err := CanonicalizeURLV1(link)
	return err == nil && result.CrawlEligible
}
