package securefetch

import (
	"context"
	"crypto/x509"
	"net"
	"net/netip"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
)

const (
	defaultDNSLookupTimeout      = 3 * time.Second
	defaultDialTimeout           = 5 * time.Second
	defaultTLSHandshakeTimeout   = 5 * time.Second
	defaultResponseHeaderTimeout = 10 * time.Second
	defaultTotalTimeout          = 30 * time.Second
	defaultMaxResponseHeader     = 1 << 20

	maximumDNSAnswers      = 16
	maximumRedirectHops    = 10
	maximumPhaseTimeout    = 1 * time.Minute
	maximumTotalTimeout    = 5 * time.Minute
	maximumResponseHeaders = 8 << 20
)

// Matcher performs the caller's crawl-policy match for an initial URL or a
// redirect target. A Fetch invokes it again for every target before acquiring
// a request budget or doing DNS.
type Matcher func(rawURL string) (crawlpolicy.Decision, error)

// HopAuthorizer applies request-specific controls, such as robots.txt, after
// URL policy matching and before every page hop. Robots fetches themselves use
// Fetch without an authorizer to avoid recursion.
type HopAuthorizer func(context.Context, crawlpolicy.Decision, RequestGate) error

// RequestGate reserves global and policy-group request capacity. Acquire is
// called once immediately before each hop's DNS lookup. A successful Acquire
// must return a non-nil, idempotent release function.
type RequestGate interface {
	Acquire(context.Context, crawlpolicy.Decision) (release func(), err error)
}

// Resolver is the subset of net.Resolver used by Fetcher.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer is the subset of net.Dialer used by Fetcher.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Config contains bounded network controls and deterministic test injection
// points. Zero timeout/header values select hardened defaults. A nil Resolver,
// Dialer, or RootCAs selects the Go production default.
//
// AdditionalDeniedCIDRs must contain canonical CIDRs (no host bits, zones, or
// IPv4-mapped IPv6 prefixes). LocalAddresses nil discovers host interface
// addresses and denies them exactly; a non-nil slice is an explicit snapshot,
// primarily useful in hermetic tests.
type Config struct {
	Resolver Resolver
	Dialer   Dialer
	RootCAs  *x509.CertPool

	DNSLookupTimeout       time.Duration
	DialTimeout            time.Duration
	TLSHandshakeTimeout    time.Duration
	ResponseHeaderTimeout  time.Duration
	TotalTimeout           time.Duration
	MaxResponseHeaderBytes int64

	AdditionalDeniedCIDRs []string
	LocalAddresses        []netip.Addr
}

// Result is a completed final response. RedirectChain contains canonical
// redirect targets in follow order; it excludes the initial URL and is empty
// when no redirect was followed.
type Result struct {
	Body              []byte
	StatusCode        int
	ContentType       string
	ContentTypeValues []string
	EffectiveURL      string
	Decision          crawlpolicy.Decision
	RedirectChain     []string
}

// Fetcher is immutable after construction and safe for concurrent use when its
// injected Resolver and Dialer are safe for concurrent use.
type Fetcher struct {
	resolver Resolver
	dialer   Dialer
	rootCAs  *x509.CertPool

	dnsLookupTimeout       time.Duration
	dialTimeout            time.Duration
	tlsHandshakeTimeout    time.Duration
	responseHeaderTimeout  time.Duration
	totalTimeout           time.Duration
	maxResponseHeaderBytes int64

	addresses addressPolicy
}
