package crawljobsv2

import (
	"errors"
	"net"
	"net/url"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var ErrInvalidCanonicalOrigin = errors.New("crawljobsv2: invalid canonical origin")

type CanonicalOrigin string

// DeriveCanonicalOrigin accepts only an already-canonical V1 HTTP(S) URL. It
// performs no DNS and always includes the effective port.
func DeriveCanonicalOrigin(canonicalURL string) (CanonicalOrigin, error) {
	identity, err := utils.CanonicalizeURLV1(canonicalURL)
	if err != nil || identity.CanonicalURL != canonicalURL {
		return "", ErrInvalidCanonicalURL
	}

	parsed, err := url.Parse(canonicalURL)
	if err != nil || parsed.User != nil || parsed.Opaque != "" || parsed.Host == "" {
		return "", ErrInvalidCanonicalOrigin
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidCanonicalOrigin
	}
	host := parsed.Hostname()
	if host == "" || net.ParseIP(host) != nil || identity.CrawlRejection == utils.CrawlRejectionIPLiteral {
		return "", ErrInvalidCanonicalOrigin
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return CanonicalOrigin(parsed.Scheme + "://" + host + ":" + port), nil
}

func validateCanonicalOrigin(origin CanonicalOrigin) error {
	parsed, err := url.Parse(string(origin))
	if err != nil || parsed.User != nil || parsed.Opaque != "" || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ErrInvalidCanonicalOrigin
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Port() == "" || net.ParseIP(parsed.Hostname()) != nil {
		return ErrInvalidCanonicalOrigin
	}
	host := parsed.Hostname()
	port := parsed.Port()
	probeAuthority := host + ":" + port
	if parsed.Scheme == "http" && port == "80" || parsed.Scheme == "https" && port == "443" {
		probeAuthority = host
	}
	probe := parsed.Scheme + "://" + probeAuthority + "/"
	derived, err := DeriveCanonicalOrigin(probe)
	if err != nil || derived != origin {
		return ErrInvalidCanonicalOrigin
	}
	return nil
}
