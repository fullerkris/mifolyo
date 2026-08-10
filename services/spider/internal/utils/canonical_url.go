package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

const (
	URLCanonicalizationVersionV1 = 1
	MaxURLBytesV1                = 2048
	URLIDNamespaceV1             = "mifolyo-url:v1\x00"

	CrawlRejectionIPLiteral      = "ip_literal_forbidden"
	CrawlRejectionLocalName      = "local_name_forbidden"
	CrawlRejectionNonDefaultPort = "non_default_crawl_port"
)

// CanonicalizationError is a stable, machine-readable V1 validation error.
// Error deliberately returns only Code so callers in every language can
// compare the same value from the shared contract.
type CanonicalizationError struct {
	Code string
}

func (e *CanonicalizationError) Error() string {
	return e.Code
}

var (
	ErrURLTooLong          = &CanonicalizationError{Code: "url_too_long"}
	ErrInvalidUTF8         = &CanonicalizationError{Code: "invalid_utf8"}
	ErrWhitespaceOrControl = &CanonicalizationError{Code: "whitespace_or_control"}
	ErrBackslashForbidden  = &CanonicalizationError{Code: "backslash_forbidden"}
	ErrMalformedEscape     = &CanonicalizationError{Code: "malformed_escape"}
	ErrEncodedControl      = &CanonicalizationError{Code: "encoded_control"}
	ErrAbsoluteURLRequired = &CanonicalizationError{Code: "absolute_url_required"}
	ErrSchemeNotAllowed    = &CanonicalizationError{Code: "scheme_not_allowed"}
	ErrUserinfoForbidden   = &CanonicalizationError{Code: "userinfo_forbidden"}
	ErrInvalidPort         = &CanonicalizationError{Code: "invalid_port"}
	ErrNonASCIIHostV1      = &CanonicalizationError{Code: "non_ascii_host_v1"}
	ErrInvalidHost         = &CanonicalizationError{Code: "invalid_host"}
	ErrInvalidURL          = &CanonicalizationError{Code: "invalid_url"}
)

// CanonicalizedURL is the complete V1 URL identity and static crawl decision.
// CrawlRejection is empty when CrawlEligible is true.
type CanonicalizedURL struct {
	CanonicalURL            string
	URLID                   string
	CrawlEligible           bool
	CrawlRejection          string
	CanonicalizationVersion int
}

// URLIdentity is an alias emphasizing that canonical URLs and opaque IDs are
// two representations of the same versioned identity.
type URLIdentity = CanonicalizedURL

// CrawlAdmissionError reports a stable static crawl rejection. Canonical URLs
// that fail this policy still have valid identity; they must simply not be
// used for a network request.
type CrawlAdmissionError struct {
	Rejection string
}

func (e *CrawlAdmissionError) Error() string {
	return e.Rejection
}

// CanonicalizationErrorCode extracts a stable V1 error code through wrapping.
func CanonicalizationErrorCode(err error) string {
	var canonicalizationErr *CanonicalizationError
	if errors.As(err, &canonicalizationErr) {
		return canonicalizationErr.Code
	}
	return ""
}

// CrawlAdmissionErrorCode extracts a static crawl rejection through wrapping.
func CrawlAdmissionErrorCode(err error) string {
	var admissionErr *CrawlAdmissionError
	if errors.As(err, &admissionErr) {
		return admissionErr.Rejection
	}
	return ""
}

// CanonicalizeURL applies the current URL identity contract. V1 is explicit
// internally so a future version cannot silently reinterpret existing IDs.
func CanonicalizeURL(rawURL string) (CanonicalizedURL, error) {
	return CanonicalizeURLV1(rawURL)
}

// IdentifyURL is a semantic alias for callers that consume the URL ID and
// admission fields in addition to the canonical URL.
func IdentifyURL(rawURL string) (URLIdentity, error) {
	return CanonicalizeURLV1(rawURL)
}

// CanonicalizeURLV1 canonicalizes an absolute HTTP(S) URL without DNS or any
// other network access.
func CanonicalizeURLV1(rawURL string) (CanonicalizedURL, error) {
	if len(rawURL) > MaxURLBytesV1 {
		return CanonicalizedURL{}, ErrURLTooLong
	}
	if !utf8.ValidString(rawURL) {
		return CanonicalizedURL{}, ErrInvalidUTF8
	}
	if hasForbiddenWhitespaceOrControl(rawURL) {
		return CanonicalizedURL{}, ErrWhitespaceOrControl
	}
	if strings.ContainsRune(rawURL, '\\') {
		return CanonicalizedURL{}, ErrBackslashForbidden
	}
	if hasMalformedPercentEscape(rawURL) {
		return CanonicalizedURL{}, ErrMalformedEscape
	}
	if hasEncodedControl(rawURL) {
		return CanonicalizedURL{}, ErrEncodedControl
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return CanonicalizedURL{}, classifyURLParseError(err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		if scheme == "" {
			return CanonicalizedURL{}, ErrAbsoluteURLRequired
		}
		return CanonicalizedURL{}, ErrSchemeNotAllowed
	}
	if parsed.User != nil {
		return CanonicalizedURL{}, ErrUserinfoForbidden
	}
	if !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" {
		return CanonicalizedURL{}, ErrAbsoluteURLRequired
	}

	host, port, ipv6, err := canonicalHostPort(parsed.Host)
	if err != nil {
		return CanonicalizedURL{}, err
	}
	if port != "" {
		portNumber, _ := strconv.Atoi(port)
		if scheme == "http" && portNumber == 80 || scheme == "https" && portNumber == 443 {
			port = ""
		}
	}

	canonicalAuthority := host
	if ipv6 {
		canonicalAuthority = "[" + host + "]"
	}
	if port != "" {
		canonicalAuthority += ":" + port
	}

	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	path = encodeComponentV1(path, pathSafeCharactersV1)

	canonicalURL := scheme + "://" + canonicalAuthority + path
	if parsed.ForceQuery || parsed.RawQuery != "" {
		canonicalURL += "?" + encodeComponentV1(parsed.RawQuery, querySafeCharactersV1)
	}
	if len(canonicalURL) > MaxURLBytesV1 {
		return CanonicalizedURL{}, ErrURLTooLong
	}

	rejection := staticCrawlRejection(host, port)
	return CanonicalizedURL{
		CanonicalURL:            canonicalURL,
		URLID:                   URLIDV1(canonicalURL),
		CrawlEligible:           rejection == "",
		CrawlRejection:          rejection,
		CanonicalizationVersion: URLCanonicalizationVersionV1,
	}, nil
}

// URLIDV1 returns the lowercase hexadecimal V1 digest for a canonical URL.
func URLIDV1(canonicalURL string) string {
	digest := sha256.Sum256([]byte(URLIDNamespaceV1 + canonicalURL))
	return hex.EncodeToString(digest[:])
}

// IsURLIDV1 reports whether value has the external form of a V1 URL ID.
func IsURLIDV1(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// RequireStaticCrawlEligibility turns a canonicalization result's admission
// decision into an error suitable for queue and fetch boundaries.
func RequireStaticCrawlEligibility(result CanonicalizedURL) error {
	if result.CrawlEligible {
		return nil
	}
	return &CrawlAdmissionError{Rejection: result.CrawlRejection}
}

func hasForbiddenWhitespaceOrControl(value string) bool {
	if strings.TrimFunc(value, unicode.IsSpace) != value {
		return true
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return true
		}
	}
	return false
}

func hasMalformedPercentEscape(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
			return true
		}
		index += 2
	}
	return false
}

func hasEncodedControl(value string) bool {
	for index := 0; index+2 < len(value); index++ {
		if value[index] != '%' {
			continue
		}

		decoded := decodeHexByte(value[index+1], value[index+2])
		if decoded < 0x20 || decoded == 0x7f {
			return true
		}

		// U+0080 through U+009F (the Unicode C1 controls) encode as
		// 0xC2 followed by 0x80 through 0x9F in UTF-8. Reject their escaped
		// form for the same reason the raw scalar values are rejected.
		if decoded == 0xc2 && index+5 < len(value) && value[index+3] == '%' {
			next := decodeHexByte(value[index+4], value[index+5])
			if next >= 0x80 && next <= 0x9f {
				return true
			}
		}

		index += 2
	}
	return false
}

func decodeHexByte(high, low byte) byte {
	return decodeHexNibble(high)<<4 | decodeHexNibble(low)
}

func decodeHexNibble(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10
	default:
		return value - 'A' + 10
	}
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func classifyURLParseError(err error) error {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "invalid url escape"):
		// Malformed escapes are rejected before parsing. A remaining escape
		// error is therefore a valid escape in a component (normally a host)
		// where URL V1 does not permit percent encoding.
		return ErrInvalidHost
	case strings.Contains(message, "invalid port"):
		return ErrInvalidPort
	case strings.Contains(message, "missing ']' in host"),
		strings.Contains(message, "too many colons in address"):
		return ErrInvalidHost
	case strings.Contains(message, "invalid control character"):
		return ErrWhitespaceOrControl
	default:
		return ErrInvalidURL
	}
}

func canonicalHostPort(hostPort string) (host string, port string, ipv6 bool, err error) {
	if strings.HasPrefix(hostPort, "[") {
		closingBracket := strings.IndexByte(hostPort, ']')
		if closingBracket < 0 {
			return "", "", false, ErrInvalidHost
		}

		host = hostPort[1:closingBracket]
		suffix := hostPort[closingBracket+1:]
		if suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return "", "", false, ErrInvalidHost
			}
			port = suffix[1:]
			if port == "" {
				return "", "", false, ErrInvalidPort
			}
		}

		if strings.Contains(host, "%") || net.ParseIP(host) == nil || !strings.Contains(host, ":") {
			return "", "", false, ErrInvalidHost
		}
		ipv6 = true
	} else {
		if strings.ContainsAny(hostPort, "[]") {
			return "", "", false, ErrInvalidHost
		}
		if strings.Count(hostPort, ":") > 1 {
			return "", "", false, ErrInvalidHost
		}
		if separator := strings.LastIndexByte(hostPort, ':'); separator >= 0 {
			host = hostPort[:separator]
			port = hostPort[separator+1:]
			if port == "" {
				return "", "", false, ErrInvalidPort
			}
		} else {
			host = hostPort
		}
	}

	if host == "" {
		return "", "", false, ErrInvalidHost
	}
	for _, char := range host {
		if char > unicode.MaxASCII {
			return "", "", false, ErrNonASCIIHostV1
		}
	}

	host = strings.ToLower(host)
	if !ipv6 && net.ParseIP(host) == nil {
		if isDottedNumericHostname(host) || !validDNSHostname(host) {
			return "", "", false, ErrInvalidHost
		}
	}

	if port != "" {
		portNumber, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || portNumber == 0 {
			return "", "", false, ErrInvalidPort
		}
		port = strconv.FormatUint(portNumber, 10)
	}

	return host, port, ipv6, nil
}

func validDNSHostname(host string) bool {
	if len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || !isASCIIAlphaNumeric(label[0]) || !isASCIIAlphaNumeric(label[len(label)-1]) {
			return false
		}
		for index := 1; index < len(label)-1; index++ {
			if !isASCIIAlphaNumeric(label[index]) && label[index] != '-' {
				return false
			}
		}
		if strings.HasPrefix(label, "xn--") && !validIDNAALabel(label) {
			return false
		}
	}
	return true
}

// idnaV1RegistrationProfile locks V1 to UTS #46 non-transitional mapping with
// registration-grade STD3, label, ContextJ, Bidi, and DNS-length validation.
// ValidateForRegistration establishes the registration checks; MapForLookup
// then selects the UTS #46 mapping used by the Python and PHP implementations.
// The remaining options are intentionally explicit so a changing package
// default cannot silently change URL identity validation.
var idnaV1RegistrationProfile = idna.New(
	idna.ValidateForRegistration(),
	idna.MapForLookup(),
	idna.Transitional(false),
	idna.StrictDomainName(true),
	idna.ValidateLabels(true),
	idna.CheckHyphens(true),
	idna.CheckJoiners(true),
	idna.BidiRule(),
	idna.VerifyDNSLength(true),
)

func validIDNAALabel(label string) bool {
	unicodeLabel, err := idnaV1RegistrationProfile.ToUnicode(label)
	if err != nil || unicodeLabel == label {
		return false
	}
	roundTrip, err := idnaV1RegistrationProfile.ToASCII(unicodeLabel)
	return err == nil && strings.EqualFold(roundTrip, label)
}

func isASCIIAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

const (
	pathSafeCharactersV1  = "/:@!$&'()*+,;=-._~"
	querySafeCharactersV1 = "/?:@!$&'()*+,;=-._~"
)

func encodeComponentV1(value string, safeCharacters string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for index := 0; index < len(value); index++ {
		current := value[index]
		if current == '%' && index+2 < len(value) {
			builder.WriteByte('%')
			builder.WriteByte(toUpperHex(value[index+1]))
			builder.WriteByte(toUpperHex(value[index+2]))
			index += 2
			continue
		}
		if current >= utf8.RuneSelf || !isASCIIAlphaNumeric(current) && !strings.ContainsRune(safeCharacters, rune(current)) {
			builder.WriteByte('%')
			builder.WriteByte(upperHex[current>>4])
			builder.WriteByte(upperHex[current&0x0f])
			continue
		}
		builder.WriteByte(current)
	}
	return builder.String()
}

const upperHex = "0123456789ABCDEF"

func toUpperHex(value byte) byte {
	if value >= 'a' && value <= 'f' {
		return value - ('a' - 'A')
	}
	return value
}

func isDottedNumericHostname(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) != 4 {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
		for index := 0; index < len(label); index++ {
			if label[index] < '0' || label[index] > '9' {
				return false
			}
		}
	}
	return true
}

func staticCrawlRejection(host string, port string) string {
	if net.ParseIP(host) != nil {
		return CrawlRejectionIPLiteral
	}
	if isLocalHostname(host) {
		return CrawlRejectionLocalName
	}
	if port != "" {
		return CrawlRejectionNonDefaultPort
	}
	return ""
}

func isLocalHostname(host string) bool {
	if !strings.Contains(host, ".") {
		return true
	}
	localNames := []string{
		"localhost.localdomain",
		".localhost",
		".local",
		".localdomain",
		".lan",
		".home",
		".home.arpa",
		".internal",
		".intranet",
		".test",
		".invalid",
	}
	for _, localName := range localNames {
		if host == localName || strings.HasSuffix(host, localName) {
			return true
		}
	}
	return false
}
