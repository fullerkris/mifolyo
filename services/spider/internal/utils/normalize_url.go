package utils

// NormalizeURL is retained for compatibility with older spider code. Unlike
// the former lossy normalizer, it returns the V1 canonical absolute URL.
// New code should use CanonicalizeURLV1 when it also needs the URL ID or crawl
// admission decision.
func NormalizeURL(rawURL string) (string, error) {
	result, err := CanonicalizeURLV1(rawURL)
	if err != nil {
		return "", err
	}
	return result.CanonicalURL, nil
}
