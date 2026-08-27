package main

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

const maxURLBytesV1 = 2048

func validateURLIdentity(value string) error {
	canonical, err := canonicalizeURLV1(value)
	if err != nil {
		return err
	}
	if canonical != value {
		return fmt.Errorf("URL is not canonicalization V1 identity")
	}
	return nil
}

func canonicalizeURLV1(rawURL string) (string, error) {
	if len(rawURL) > maxURLBytesV1 {
		return "", fmt.Errorf("URL exceeds V1 byte limit")
	}
	if !utf8.ValidString(rawURL) {
		return "", fmt.Errorf("URL is not valid UTF-8")
	}
	if hasForbiddenWhitespaceOrControl(rawURL) {
		return "", fmt.Errorf("URL contains whitespace or control characters")
	}
	if strings.ContainsRune(rawURL, '\\') {
		return "", fmt.Errorf("URL contains a backslash")
	}
	if hasMalformedPercentEscape(rawURL) {
		return "", fmt.Errorf("URL contains a malformed percent escape")
	}
	if hasEncodedControl(rawURL) {
		return "", fmt.Errorf("URL contains an encoded control character")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("URL cannot be parsed")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("URL must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("URL userinfo is forbidden")
	}
	if !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" {
		return "", fmt.Errorf("URL must be absolute")
	}

	host, port, ipv6, err := canonicalHostPortV1(parsed.Host)
	if err != nil {
		return "", err
	}
	if port != "" {
		portNumber, _ := strconv.Atoi(port)
		if scheme == "http" && portNumber == 80 || scheme == "https" && portNumber == 443 {
			port = ""
		}
	}

	authority := host
	if ipv6 {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority += ":" + port
	}

	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	path = encodeURLComponentV1(path, pathSafeCharactersV1)
	canonical := scheme + "://" + authority + path
	if parsed.ForceQuery || parsed.RawQuery != "" {
		canonical += "?" + encodeURLComponentV1(parsed.RawQuery, querySafeCharactersV1)
	}
	if len(canonical) > maxURLBytesV1 {
		return "", fmt.Errorf("canonical URL exceeds V1 byte limit")
	}
	return canonical, nil
}

func hasForbiddenWhitespaceOrControl(value string) bool {
	if strings.TrimFunc(value, unicode.IsSpace) != value {
		return true
	}
	for _, character := range value {
		if unicode.IsControl(character) {
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

func canonicalHostPortV1(hostPort string) (host string, port string, ipv6 bool, err error) {
	if strings.HasPrefix(hostPort, "[") {
		closingBracket := strings.IndexByte(hostPort, ']')
		if closingBracket < 0 {
			return "", "", false, fmt.Errorf("URL host is invalid")
		}
		host = hostPort[1:closingBracket]
		suffix := hostPort[closingBracket+1:]
		if suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return "", "", false, fmt.Errorf("URL host is invalid")
			}
			port = suffix[1:]
			if port == "" {
				return "", "", false, fmt.Errorf("URL port is invalid")
			}
		}
		if strings.Contains(host, "%") || net.ParseIP(host) == nil || !strings.Contains(host, ":") {
			return "", "", false, fmt.Errorf("URL host is invalid")
		}
		ipv6 = true
	} else {
		if strings.ContainsAny(hostPort, "[]") || strings.Count(hostPort, ":") > 1 {
			return "", "", false, fmt.Errorf("URL host is invalid")
		}
		if separator := strings.LastIndexByte(hostPort, ':'); separator >= 0 {
			host = hostPort[:separator]
			port = hostPort[separator+1:]
			if port == "" {
				return "", "", false, fmt.Errorf("URL port is invalid")
			}
		} else {
			host = hostPort
		}
	}

	if host == "" {
		return "", "", false, fmt.Errorf("URL host is invalid")
	}
	for _, character := range host {
		if character > unicode.MaxASCII {
			return "", "", false, fmt.Errorf("URL host must be ASCII")
		}
	}
	host = strings.ToLower(host)
	if !ipv6 && net.ParseIP(host) == nil && (isDottedNumericHostname(host) || !validDNSHostname(host)) {
		return "", "", false, fmt.Errorf("URL host is invalid")
	}
	if port != "" {
		portNumber, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || portNumber == 0 {
			return "", "", false, fmt.Errorf("URL port is invalid")
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
	upperHex              = "0123456789ABCDEF"
)

func encodeURLComponentV1(value, safeCharacters string) string {
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
