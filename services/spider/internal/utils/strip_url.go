package utils

// StripURL is retained for compatibility with older spider code. V1 removes
// fragments, but query strings and trailing slashes are part of URL identity
// and are therefore preserved. New code should use CanonicalizeURLV1.
func StripURL(rawURL string) (string, error) {
	return NormalizeURL(rawURL)
}
