package crawler

import (
	"context"
	"fmt"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
)

const maxPageBodyBytes int64 = 5 * 1024 * 1024

type pageData struct {
	HTML          string
	StatusCode    int
	ContentType   string
	EffectiveURL  string
	Decision      crawlpolicy.Decision
	RedirectChain []string
}

func getPageData(
	ctx context.Context,
	fetcher *securefetch.Fetcher,
	rawURL string,
	matcher securefetch.Matcher,
	gate securefetch.RequestGate,
	authorizer securefetch.HopAuthorizer,
) (pageData, error) {
	if fetcher == nil {
		return pageData{}, fmt.Errorf("secure fetcher is not configured")
	}
	result, err := fetcher.FetchAuthorized(ctx, rawURL, matcher, gate, maxPageBodyBytes, authorizer)
	if err != nil {
		return pageData{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	if result.StatusCode > 399 {
		return pageData{}, fmt.Errorf("HTTP error status: %d", result.StatusCode)
	}

	if err := validatePageHTMLResponse(result.ContentType, result.ContentTypeValues, result.Body); err != nil {
		return pageData{}, err
	}

	return pageData{
		HTML:          string(result.Body),
		StatusCode:    result.StatusCode,
		ContentType:   result.ContentType,
		EffectiveURL:  result.EffectiveURL,
		Decision:      result.Decision,
		RedirectChain: append([]string(nil), result.RedirectChain...),
	}, nil
}

func validatePageHTMLResponse(contentType string, contentTypeValues []string, body []byte) error {
	if len(contentTypeValues) != 1 || contentTypeValues[0] == "" || contentTypeValues[0] != contentType ||
		!utf8.ValidString(contentTypeValues[0]) || strings.TrimSpace(contentTypeValues[0]) != contentTypeValues[0] {
		return fmt.Errorf("HTML Content-Type must contain exactly one unambiguous value")
	}
	mediaType, parameters, err := mime.ParseMediaType(contentTypeValues[0])
	if err != nil || !strings.EqualFold(mediaType, "text/html") {
		return fmt.Errorf("invalid HTML content type")
	}
	if len(parameters) > 0 {
		charset, present := parameters["charset"]
		if len(parameters) != 1 || !present || !strings.EqualFold(charset, "utf-8") {
			return fmt.Errorf("HTML Content-Type permits only UTF-8 charset")
		}
	}
	if !utf8.Valid(body) {
		return fmt.Errorf("HTML body must be valid UTF-8")
	}
	return nil
}
