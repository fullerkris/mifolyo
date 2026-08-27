package securefetch

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

type fetchTarget struct {
	decision  crawlpolicy.Decision
	url       *url.URL
	canonical string
	host      string
	port      uint16
	authority string
}

type hopResponse struct {
	body              []byte
	statusCode        int
	contentType       string
	contentTypeValues []string
	locations         []string
}

// Fetch performs one policy-gated GET and any policy-permitted manual
// redirects. maxBodyBytes must be positive and applies to the final response.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string, matcher Matcher, gate RequestGate, maxBodyBytes int64) (Result, error) {
	return f.FetchAuthorized(ctx, rawURL, matcher, gate, maxBodyBytes, nil)
}

// FetchAuthorized behaves like Fetch and additionally invokes authorizer for
// the initial page and every redirect target before DNS or page network I/O.
func (f *Fetcher) FetchAuthorized(
	ctx context.Context,
	rawURL string,
	matcher Matcher,
	gate RequestGate,
	maxBodyBytes int64,
	authorizer HopAuthorizer,
) (Result, error) {
	return f.fetchAuthorized(ctx, rawURL, matcher, gate, maxBodyBytes, authorizer, false)
}

// FetchAuthorizedDirect behaves like FetchAuthorized but never follows a
// redirect. A redirect response fails with ReasonRedirectHopLimit before its
// target is resolved, matched, authorized, gated, or fetched.
func (f *Fetcher) FetchAuthorizedDirect(
	ctx context.Context,
	rawURL string,
	matcher Matcher,
	gate RequestGate,
	maxBodyBytes int64,
	authorizer HopAuthorizer,
) (Result, error) {
	return f.fetchAuthorized(ctx, rawURL, matcher, gate, maxBodyBytes, authorizer, true)
}

func (f *Fetcher) fetchAuthorized(
	ctx context.Context,
	rawURL string,
	matcher Matcher,
	gate RequestGate,
	maxBodyBytes int64,
	authorizer HopAuthorizer,
	direct bool,
) (Result, error) {
	if f == nil || ctx == nil || matcher == nil || gate == nil || maxBodyBytes <= 0 || maxBodyBytes == int64(^uint64(0)>>1) {
		return Result{}, newError(ReasonInvalidArgument, "fetch", nil)
	}

	fetchContext, cancel := context.WithTimeout(ctx, f.totalTimeout)
	defer cancel()

	initial, err := prepareTarget(rawURL, matcher)
	if err != nil {
		return Result{}, err
	}
	redirectPolicy := initial.decision.Group.Redirects
	if redirectPolicy.MaxHops < 0 || redirectPolicy.MaxHops > maximumRedirectHops || !validRedirectMode(redirectPolicy.Mode) {
		return Result{}, newError(ReasonInvalidDecision, "validate_redirect_policy", nil)
	}

	current := initial
	seen := map[string]struct{}{initial.canonical: {}}
	redirectChain := make([]string, 0, redirectPolicy.MaxHops)

	for {
		if authorizer != nil {
			if authorizeErr := authorizer(fetchContext, current.decision, gate); authorizeErr != nil {
				return Result{}, newError(ReasonHopDenied, "authorize_hop", authorizeErr)
			}
		}
		response, hopErr := f.fetchHop(fetchContext, current, gate, maxBodyBytes)
		if hopErr != nil {
			return Result{}, hopErr
		}
		if !isRedirectStatus(response.statusCode) {
			return Result{
				Body:              response.body,
				StatusCode:        response.statusCode,
				ContentType:       response.contentType,
				ContentTypeValues: append([]string(nil), response.contentTypeValues...),
				EffectiveURL:      current.canonical,
				Decision:          current.decision,
				RedirectChain:     append([]string(nil), redirectChain...),
			}, nil
		}
		if direct {
			return Result{}, newError(ReasonRedirectHopLimit, "redirect", nil)
		}

		nextRaw, locationErr := resolveRedirect(current.url, response.locations)
		if locationErr != nil {
			return Result{}, locationErr
		}
		next, matchErr := prepareTarget(nextRaw, matcher)
		if matchErr != nil {
			return Result{}, matchErr
		}
		if current.url.Scheme == "https" && next.url.Scheme == "http" {
			return Result{}, newError(ReasonHTTPSDowngrade, "redirect", nil)
		}
		switch redirectPolicy.Mode {
		case crawlpolicy.RedirectNone:
			return Result{}, newError(ReasonRedirectMode, "redirect", nil)
		case crawlpolicy.RedirectSameHost:
			if next.host != initial.host {
				return Result{}, newError(ReasonRedirectHost, "redirect", nil)
			}
		case crawlpolicy.RedirectSameGroup:
			if next.decision.Group.ID != initial.decision.Group.ID {
				return Result{}, newError(ReasonRedirectGroup, "redirect", nil)
			}
		default:
			return Result{}, newError(ReasonInvalidDecision, "redirect", nil)
		}
		if _, cycle := seen[next.canonical]; cycle {
			return Result{}, newError(ReasonRedirectCycle, "redirect", nil)
		}
		if len(redirectChain) >= redirectPolicy.MaxHops {
			return Result{}, newError(ReasonRedirectHopLimit, "redirect", nil)
		}

		seen[next.canonical] = struct{}{}
		redirectChain = append(redirectChain, next.canonical)
		current = next
	}
}

func prepareTarget(rawURL string, matcher Matcher) (fetchTarget, error) {
	decision, matchErr := matcher(rawURL)
	if matchErr != nil {
		return fetchTarget{}, newError(ReasonMatcherDenied, "match_url", matchErr)
	}

	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return fetchTarget{}, newError(ReasonInvalidURL, "canonicalize_url", err)
	}
	if !identity.CrawlEligible {
		reason := ReasonStaticURLDenied
		if identity.CrawlRejection == utils.CrawlRejectionNonDefaultPort {
			reason = ReasonNonDefaultPort
		}
		return fetchTarget{}, newError(reason, "validate_url", &utils.CrawlAdmissionError{Rejection: identity.CrawlRejection})
	}

	canonical, err := url.Parse(identity.CanonicalURL)
	if err != nil {
		return fetchTarget{}, newError(ReasonInvalidURL, "parse_canonical_url", err)
	}
	if canonical.Scheme != "http" && canonical.Scheme != "https" || canonical.User != nil || canonical.Host == "" || canonical.Port() != "" {
		return fetchTarget{}, newError(ReasonNonDefaultPort, "validate_authority", nil)
	}
	if !utils.IsUnambiguousFetchPath(canonical) {
		return fetchTarget{}, newError(ReasonAmbiguousPath, "validate_path", nil)
	}
	if err := validateRawPort(rawURL, canonical.Scheme); err != nil {
		return fetchTarget{}, err
	}

	if decision.Identity != identity || decision.URL == nil || decision.URL.String() != identity.CanonicalURL ||
		decision.Scheme != canonical.Scheme || decision.Host != canonical.Hostname() || decision.Path != canonical.EscapedPath() || decision.Group.ID == "" {
		return fetchTarget{}, newError(ReasonInvalidDecision, "validate_matcher_decision", nil)
	}

	port := uint16(80)
	if canonical.Scheme == "https" {
		port = 443
	}
	host := canonical.Hostname()
	return fetchTarget{
		decision:  decision,
		url:       canonical,
		canonical: identity.CanonicalURL,
		host:      host,
		port:      port,
		authority: net.JoinHostPort(host, strconv.Itoa(int(port))),
	}, nil
}

func validateRawPort(rawURL, scheme string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return newError(ReasonInvalidURL, "parse_url_authority", err)
	}
	port := parsed.Port()
	if port == "" {
		return nil
	}
	want := uint64(80)
	if scheme == "https" {
		want = 443
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort != want {
		return newError(ReasonNonDefaultPort, "validate_authority", nil)
	}
	return nil
}

func (f *Fetcher) fetchHop(ctx context.Context, target fetchTarget, gate RequestGate, maxBodyBytes int64) (hopResponse, error) {
	release, err := gate.Acquire(ctx, target.decision)
	if err != nil {
		return hopResponse{}, newError(ReasonGateDenied, "acquire_request_gate", err)
	}
	if release == nil {
		return hopResponse{}, newError(ReasonGateDenied, "acquire_request_gate", nil)
	}
	defer release()

	addresses, err := f.resolve(ctx, target.host)
	if err != nil {
		return hopResponse{}, err
	}

	transport := f.transportFor(target, addresses)
	defer transport.CloseIdleConnections()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.canonical, nil)
	if err != nil {
		return hopResponse{}, newError(ReasonInvalidURL, "build_request", err)
	}
	request.Host = target.url.Host
	request.Close = true
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", target.decision.Group.UserAgent)

	response, err := transport.RoundTrip(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return hopResponse{}, normalizeRoundTripError(err)
	}
	if response == nil || response.Body == nil {
		return hopResponse{}, newError(ReasonInvalidResponse, "read_response", nil)
	}
	defer response.Body.Close()

	hop := hopResponse{
		statusCode:        response.StatusCode,
		contentType:       response.Header.Get("Content-Type"),
		contentTypeValues: append([]string(nil), response.Header.Values("Content-Type")...),
		locations:         append([]string(nil), response.Header.Values("Location")...),
	}
	if response.StatusCode == http.StatusSwitchingProtocols {
		return hopResponse{}, newError(ReasonInvalidResponse, "reject_protocol_switch", nil)
	}
	if isRedirectStatus(response.StatusCode) {
		return hop, nil
	}
	if !validFinalContentEncoding(response.Header.Values("Content-Encoding")) {
		return hopResponse{}, newError(ReasonContentEncoding, "validate_content_encoding", nil)
	}
	if response.ContentLength > maxBodyBytes {
		return hopResponse{}, newError(ReasonBodyTooLarge, "read_body", nil)
	}

	limited := io.LimitReader(response.Body, maxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return hopResponse{}, newError(ReasonRequestFailed, "read_body", err)
	}
	if int64(len(body)) > maxBodyBytes {
		return hopResponse{}, newError(ReasonBodyTooLarge, "read_body", nil)
	}
	hop.body = body
	return hop, nil
}

func validFinalContentEncoding(values []string) bool {
	return len(values) == 0 || len(values) == 1 && strings.EqualFold(strings.TrimSpace(values[0]), "identity")
}

func (f *Fetcher) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	dnsContext, cancel := context.WithTimeout(ctx, f.dnsLookupTimeout)
	defer cancel()

	answers, err := f.resolver.LookupNetIP(dnsContext, "ip", host)
	if err != nil {
		return nil, newError(ReasonDNSLookup, "resolve_host", err)
	}
	if len(answers) == 0 {
		return nil, newError(ReasonDNSNoAnswer, "resolve_host", nil)
	}
	if len(answers) > maximumDNSAnswers {
		return nil, newError(ReasonDNSTooManyAnswers, "resolve_host", nil)
	}

	approved := make([]netip.Addr, 0, len(answers))
	seen := make(map[netip.Addr]struct{}, len(answers))
	for _, answer := range answers {
		if !answer.IsValid() || answer.Zone() != "" {
			return nil, newError(ReasonDNSInvalidAnswer, "resolve_host", nil)
		}
		answer = answer.Unmap()
		if _, duplicate := seen[answer]; duplicate {
			return nil, newError(ReasonDNSDuplicateAnswer, "resolve_host", nil)
		}
		seen[answer] = struct{}{}
		if f.addresses.prohibited(answer) {
			return nil, newError(ReasonDNSProhibitedAddress, "resolve_host", nil)
		}
		approved = append(approved, answer)
	}
	return approved, nil
}

func (f *Fetcher) transportFor(target fetchTarget, addresses []netip.Addr) *http.Transport {
	pinned := pinnedDialer{
		dialer:            f.dialer,
		expectedAuthority: target.authority,
		port:              target.port,
		addresses:         append([]netip.Addr(nil), addresses...),
		timeout:           f.dialTimeout,
	}
	return &http.Transport{
		Proxy:              nil,
		DialContext:        pinned.dialContext,
		ForceAttemptHTTP2:  false,
		DisableKeepAlives:  true,
		DisableCompression: true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    f.rootCAs,
			ServerName: target.host,
			NextProtos: []string{"http/1.1"},
		},
		TLSHandshakeTimeout:    f.tlsHandshakeTimeout,
		ResponseHeaderTimeout:  f.responseHeaderTimeout,
		MaxResponseHeaderBytes: f.maxResponseHeaderBytes,
		TLSNextProto:           make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}
}

func normalizeRoundTripError(err error) error {
	var fetchError *Error
	if errors.As(err, &fetchError) {
		return fetchError
	}
	return newError(ReasonRequestFailed, "round_trip", err)
}

func resolveRedirect(current *url.URL, locations []string) (string, error) {
	if len(locations) != 1 || locations[0] == "" || len(locations[0]) > utils.MaxURLBytesV1 {
		return "", newError(ReasonRedirectLocation, "resolve_redirect", nil)
	}
	reference, err := url.Parse(locations[0])
	if err != nil {
		return "", newError(ReasonRedirectLocation, "resolve_redirect", err)
	}
	return current.ResolveReference(reference).String(), nil
}

func isRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func validRedirectMode(mode crawlpolicy.RedirectMode) bool {
	return mode == crawlpolicy.RedirectNone || mode == crawlpolicy.RedirectSameHost || mode == crawlpolicy.RedirectSameGroup
}

// Compile-time assertions protect the production defaults from interface drift.
var (
	_ Resolver = (*net.Resolver)(nil)
	_ Dialer   = (*net.Dialer)(nil)
)
