package pages

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

type Page struct {
	NormalizedURL      string
	HTML               string
	OriginalHTML       string
	ContentType        string
	StatusCode         int
	LastCrawled        time.Time
	Rendered           bool
	RenderPolicyRule   string
	RenderPolicySHA256 string
}

// Custom String() method
func (p Page) String() string {
	htmlPreview := p.HTML

	// Truncate the HTML output
	if len(htmlPreview) > 15 {
		htmlPreview = htmlPreview[:15] + "..."
	}

	return fmt.Sprintf(
		"-------------------------------------------------\n"+
			"Normalized URL:    %-10s\n"+
			"HTML:              %-40s\n"+
			"Last Crawled:      %-30s\n"+
			"Status Code:       %-10d\n"+
			"Content Type:      %-20s\n"+
			"Rendered:          %-5t\n"+
			"-------------------------------------------------\n",
		p.NormalizedURL, htmlPreview, p.LastCrawled.Format(time.RFC1123),
		p.StatusCode, p.ContentType, p.Rendered,
	)
}

func CreatePage(normalizedUrl, html, contentType string, statusCode int) *Page {
	return &Page{
		NormalizedURL: normalizedUrl,
		HTML:          html,
		ContentType:   contentType,
		StatusCode:    statusCode,
		LastCrawled:   time.Now(),
	}
}

func CreateRenderedPage(
	normalizedURL, originalHTML, renderedHTML, contentType string,
	statusCode int,
	ruleID, policySHA256 string,
) *Page {
	return &Page{
		NormalizedURL:      normalizedURL,
		HTML:               renderedHTML,
		OriginalHTML:       originalHTML,
		ContentType:        contentType,
		StatusCode:         statusCode,
		LastCrawled:        time.Now(),
		Rendered:           true,
		RenderPolicyRule:   ruleID,
		RenderPolicySHA256: policySHA256,
	}
}

func HashPage(page *Page) (map[string]interface{}, error) {
	if page == nil {
		return nil, fmt.Errorf("page is required")
	}
	if page.Rendered && (page.OriginalHTML == "" || page.RenderPolicyRule == "" || !validSHA256(page.RenderPolicySHA256)) {
		return nil, fmt.Errorf("rendered page requires original HTML, render policy rule, and policy digest")
	}
	if !page.Rendered && (page.OriginalHTML != "" || page.RenderPolicyRule != "" || page.RenderPolicySHA256 != "") {
		return nil, fmt.Errorf("static page cannot contain rendered-page provenance")
	}
	return map[string]interface{}{
		"normalized_url":       page.NormalizedURL,
		"html":                 page.HTML,
		"original_html":        page.OriginalHTML,
		"content_type":         page.ContentType,
		"status_code":          page.StatusCode,
		"last_crawled":         page.LastCrawled.Format(time.RFC1123),
		"rendered":             strconv.FormatBool(page.Rendered),
		"render_policy_rule":   page.RenderPolicyRule,
		"render_policy_sha256": page.RenderPolicySHA256,
	}, nil
}

func DehashPage(data map[string]string) (*Page, error) {
	lastCrawled, err := utils.ParseTime(data["last_crawled"])
	if err != nil {
		return nil, fmt.Errorf("error parsing LastCrawled in hash: %w", err)
	}

	statusCode, err := utils.ParseInt(data["status_code"])
	if err != nil {
		return nil, fmt.Errorf("error parsing StatusCode in hash: %w", err)
	}
	rendered := false
	if rawRendered, exists := data["rendered"]; exists {
		rendered, err = strconv.ParseBool(rawRendered)
		if err != nil {
			return nil, fmt.Errorf("error parsing Rendered in hash: %w", err)
		}
	}
	page := &Page{
		NormalizedURL:      data["normalized_url"],
		HTML:               data["html"],
		OriginalHTML:       data["original_html"],
		ContentType:        data["content_type"],
		StatusCode:         statusCode,
		LastCrawled:        lastCrawled,
		Rendered:           rendered,
		RenderPolicyRule:   data["render_policy_rule"],
		RenderPolicySHA256: data["render_policy_sha256"],
	}
	if rendered && (page.OriginalHTML == "" || page.RenderPolicyRule == "" || !validSHA256(page.RenderPolicySHA256)) {
		return nil, fmt.Errorf("rendered page hash is missing provenance")
	}
	if !rendered && (page.OriginalHTML != "" || page.RenderPolicyRule != "" || page.RenderPolicySHA256 != "") {
		return nil, fmt.Errorf("static page hash contains rendered-page provenance")
	}
	return page, nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
