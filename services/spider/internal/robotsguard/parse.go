package robotsguard

import (
	"bytes"
	"errors"
	"math"
	"mime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/temoto/robotstxt"
)

const (
	maxRobotsLines          = 8192
	maxRobotsLineBytes      = 4096
	maxRobotsUserAgents     = 128
	maxRobotsAllowDisallows = 4096
	maxExpandedAssociations = 8192
	maxRobotsHTMLSniffBytes = 512
)

type robotsParser func([]byte) (*robotstxt.RobotsData, error)

var errRobotsResponseIsHTML = errors.New("robots response is HTML/XHTML, not a robots policy")

func parseRobotsBody(body []byte, parser robotsParser) (*robotstxt.RobotsData, error) {
	return parseRobotsResponse(body, "", parser)
}

func parseRobotsResponse(body []byte, contentType string, parser robotsParser) (*robotstxt.RobotsData, error) {
	normalized, err := prepareRobotsBody(body)
	if err != nil {
		return nil, err
	}
	if robotsResponseIsHTML(contentType, body) {
		return nil, errRobotsResponseIsHTML
	}
	if parser == nil {
		return nil, errors.New("robots parser is nil")
	}
	return parser(normalized)
}

// robotsResponseIsHTML rejects explicit HTML/XHTML media types and sniffs only
// a small meaningful prefix for obvious HTML documents when the media type is
// absent or mislabeled. The caller bounds and validates the complete body first.
func robotsResponseIsHTML(contentType string, body []byte) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "text/html") || strings.EqualFold(mediaType, "application/xhtml+xml") {
		return true
	}

	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	for {
		body = bytes.TrimLeft(body, " \t\r\n\f\v")
		switch {
		case bytes.HasPrefix(body, []byte("<!--")):
			end := bytes.Index(body[len("<!--"):], []byte("-->"))
			if end < 0 {
				return true
			}
			body = body[len("<!--")+end+len("-->"):]
		case hasXMLDeclarationPrefix(body):
			end := bytes.Index(body[len("<?xml"):], []byte("?>"))
			if end < 0 {
				return true
			}
			body = body[len("<?xml")+end+len("?>"):]
		default:
			prefix := body
			if len(prefix) > maxRobotsHTMLSniffBytes {
				prefix = prefix[:maxRobotsHTMLSniffBytes]
			}
			return hasHTMLSignature(prefix, []byte("<!doctype html")) ||
				hasHTMLSignature(prefix, []byte("<html")) ||
				hasHTMLSignature(prefix, []byte("<head")) ||
				hasHTMLSignature(prefix, []byte("<body"))
		}
	}
}

func hasXMLDeclarationPrefix(prefix []byte) bool {
	signature := []byte("<?xml")
	if len(prefix) < len(signature) || !bytes.EqualFold(prefix[:len(signature)], signature) {
		return false
	}
	if len(prefix) == len(signature) {
		return true
	}
	switch prefix[len(signature)] {
	case ' ', '\t', '\r', '\n', '\f', '\v', '?':
		return true
	default:
		return false
	}
}

func hasHTMLSignature(prefix, signature []byte) bool {
	if len(prefix) < len(signature) || !bytes.EqualFold(prefix[:len(signature)], signature) {
		return false
	}
	if len(prefix) == len(signature) {
		return true
	}
	switch prefix[len(signature)] {
	case ' ', '\t', '\r', '\n', '\f', '\v', '>', '/':
		return true
	default:
		return false
	}
}

// prepareRobotsBody bounds parser cardinality before invoking the third-party
// parser and canonicalizes rule paths for RFC 9309-equivalent comparison.
func prepareRobotsBody(body []byte) ([]byte, error) {
	if len(body) > int(MaxRobotsBodyBytes) {
		return nil, errors.New("robots body exceeds limit")
	}
	if !utf8.Valid(body) {
		return nil, errors.New("robots body is not valid UTF-8")
	}

	var normalized bytes.Buffer
	normalized.Grow(len(body))
	lineCount := 0
	userAgentCount := 0
	ruleCount := 0
	agentsInGroup := 0
	expandedAssociations := 0
	groupHasMember := false

	for offset := 0; offset < len(body); {
		line, next := nextRobotsLine(body, offset)
		offset = next
		lineCount++
		if lineCount > maxRobotsLines {
			return nil, errors.New("robots line count exceeds limit")
		}
		if len(line) > maxRobotsLineBytes {
			return nil, errors.New("robots line exceeds byte limit")
		}
		if lineCount == 1 {
			line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
		}

		key, value, directive := splitRobotsDirective(line)
		switch key {
		case "user-agent", "useragent":
			if directive {
				userAgentCount++
				if userAgentCount > maxRobotsUserAgents {
					return nil, errors.New("robots user-agent directive count exceeds limit")
				}
				if value != "" {
					if groupHasMember {
						agentsInGroup = 0
						groupHasMember = false
					}
					agentsInGroup++
				}
			}
		case "allow", "disallow":
			if directive {
				ruleCount++
				if ruleCount > maxRobotsAllowDisallows {
					return nil, errors.New("robots rule count exceeds limit")
				}
				if value != "" && agentsInGroup > 0 {
					if agentsInGroup > maxExpandedAssociations-expandedAssociations {
						return nil, errors.New("robots expanded rule association count exceeds limit")
					}
					expandedAssociations += agentsInGroup
					groupHasMember = true
				}
				normalizedValue, err := normalizeREPValue(value)
				if err != nil {
					return nil, err
				}
				canonicalKey := "Allow"
				if key == "disallow" {
					canonicalKey = "Disallow"
				}
				normalized.WriteString(canonicalKey)
				normalized.WriteString(": ")
				normalized.WriteString(normalizedValue)
				normalized.WriteByte('\n')
				continue
			}
		case "crawl-delay", "crawldelay":
			if directive && agentsInGroup > 0 && validCrawlDelay(value) {
				// temoto treats a valid crawl-delay as a group member. A later
				// User-agent therefore starts a new group.
				groupHasMember = true
			}
		}

		normalized.Write(line)
		normalized.WriteByte('\n')
	}

	return normalized.Bytes(), nil
}

func validCrawlDelay(value string) bool {
	delay, err := strconv.ParseFloat(value, 64)
	return err == nil && delay >= 0 && !math.IsInf(delay, 0) && !math.IsNaN(delay)
}

func nextRobotsLine(body []byte, offset int) ([]byte, int) {
	end := offset
	for end < len(body) && body[end] != '\n' && body[end] != '\r' {
		end++
	}
	next := end
	if next < len(body) {
		if body[next] == '\r' && next+1 < len(body) && body[next+1] == '\n' {
			next += 2
		} else {
			next++
		}
	}
	return body[offset:end], next
}

func splitRobotsDirective(line []byte) (key string, value string, ok bool) {
	if comment := bytes.IndexByte(line, '#'); comment >= 0 {
		line = line[:comment]
	}
	colon := bytes.IndexByte(line, ':')
	if colon < 0 {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(string(line[:colon])))
	value = strings.TrimSpace(string(line[colon+1:]))
	if separator := strings.IndexAny(value, " \t\v"); separator >= 0 {
		value = value[:separator]
	}
	return key, value, true
}

func normalizeREPValue(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("robots rule is not valid UTF-8")
	}

	var normalized strings.Builder
	normalized.Grow(len(value))
	for index := 0; index < len(value); index++ {
		current := value[index]
		switch {
		case current == '%':
			if index+2 >= len(value) || !isHexByte(value[index+1]) || !isHexByte(value[index+2]) {
				return "", errors.New("robots rule contains a malformed percent escape")
			}
			octet := fromHex(value[index+1])<<4 | fromHex(value[index+2])
			if isUnreserved(octet) {
				normalized.WriteByte(octet)
			} else {
				normalized.WriteByte('%')
				normalized.WriteByte(upperHexDigit(octet >> 4))
				normalized.WriteByte(upperHexDigit(octet & 0x0f))
			}
			index += 2
		case current >= utf8.RuneSelf:
			normalized.WriteByte('%')
			normalized.WriteByte(upperHexDigit(current >> 4))
			normalized.WriteByte(upperHexDigit(current & 0x0f))
		default:
			normalized.WriteByte(current)
		}
	}
	return normalized.String(), nil
}

func isUnreserved(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '-' || value == '.' || value == '_' || value == '~'
}

func isHexByte(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func fromHex(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10
	default:
		return value - 'A' + 10
	}
}

func upperHexDigit(value byte) byte {
	if value < 10 {
		return '0' + value
	}
	return 'A' + value - 10
}
