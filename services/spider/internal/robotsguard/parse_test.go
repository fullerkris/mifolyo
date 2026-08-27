package robotsguard

import (
	"errors"
	"strings"
	"testing"

	"github.com/temoto/robotstxt"
)

func TestParseRobotsResponseRejectsHTML(t *testing.T) {
	validRobots := []byte("User-agent: *\nDisallow: /private\n")
	tests := []struct {
		name        string
		contentType string
		body        []byte
		wantHTML    bool
	}{
		{
			name:        "text plain valid robots",
			contentType: "text/plain; charset=utf-8",
			body:        validRobots,
		},
		{
			name:        "explicit text html",
			contentType: "text/html; charset=utf-8",
			body:        validRobots,
			wantHTML:    true,
		},
		{
			name:        "explicit XHTML",
			contentType: "application/xhtml+xml; charset=utf-8",
			body:        validRobots,
			wantHTML:    true,
		},
		{
			name:        "doctype mislabeled as text plain",
			contentType: "text/plain",
			body:        []byte("\xef\xbb\xbf \n<!DOCTYPE html><title>Client challenge</title>"),
			wantHTML:    true,
		},
		{
			name:        "html element mislabeled as text plain",
			contentType: "text/plain",
			body:        []byte("\t<HTML lang=\"en\"><body>Client challenge</body></HTML>"),
			wantHTML:    true,
		},
		{
			name:     "doctype with missing content type",
			body:     []byte("<!doctype html><html><body>Client challenge</body></html>"),
			wantHTML: true,
		},
		{
			name:     "html element with missing content type",
			body:     []byte("\r\n<html><body>Client challenge</body></html>"),
			wantHTML: true,
		},
		{
			name:        "padded body element",
			contentType: "text/plain",
			body:        []byte(strings.Repeat(" ", maxRobotsHTMLSniffBytes+1) + "<body>Client challenge</body>"),
			wantHTML:    true,
		},
		{
			name:        "comments before head element",
			contentType: "text/plain",
			body:        []byte("<!-- first -->\n<!-- second -->\n<head><title>Client challenge</title></head>"),
			wantHTML:    true,
		},
		{
			name:        "XML declaration before body element",
			contentType: "text/plain",
			body:        []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<body>Client challenge</body>"),
			wantHTML:    true,
		},
		{
			name:        "unterminated HTML comment",
			contentType: "text/plain",
			body:        []byte("<!-- Client challenge<html>"),
			wantHTML:    true,
		},
		{
			name:        "unterminated XML declaration",
			contentType: "text/plain",
			body:        []byte("<?xml version=\"1.0\"\n<html>Client challenge</html>"),
			wantHTML:    true,
		},
		{
			name:        "ordinary robots comment is not an HTML prolog",
			contentType: "text/plain",
			body:        []byte("# ordinary robots comment\n<html>not sniffed through a robots comment</html>"),
		},
		{
			name:        "HTML-like name without delimiter",
			contentType: "text/plain",
			body:        []byte("<htmlish>not an HTML signature</htmlish>"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parserCalled := false
			_, err := parseRobotsResponse(test.body, test.contentType, func([]byte) (*robotstxt.RobotsData, error) {
				parserCalled = true
				return &robotstxt.RobotsData{}, nil
			})
			if test.wantHTML {
				if !errors.Is(err, errRobotsResponseIsHTML) {
					t.Fatalf("parseRobotsResponse error = %v, want HTML rejection", err)
				}
				if parserCalled {
					t.Fatal("robots parser was called for an HTML response")
				}
				return
			}
			if err != nil {
				t.Fatalf("valid robots response was rejected: %v", err)
			}
			if !parserCalled {
				t.Fatal("robots parser was not called for valid text/plain robots")
			}
		})
	}
}

func TestRobotsPreflightRejectsAmplificationBeforeParser(t *testing.T) {
	body := []byte(
		strings.Repeat("User-agent: *\n", 128) +
			strings.Repeat("Disallow: /x\n", 4096),
	)
	called := false
	_, err := parseRobotsBody(body, func([]byte) (*robotstxt.RobotsData, error) {
		called = true
		return nil, errors.New("parser must not be called")
	})
	if err == nil {
		t.Fatal("amplification payload was accepted")
	}
	if !strings.Contains(err.Error(), "expanded rule association") {
		t.Fatalf("amplification error = %v, want expanded-association rejection", err)
	}
	if called {
		t.Fatal("vulnerable parser was called for amplification payload")
	}
}

func TestRobotsExpandedAssociationLimitIsInclusive(t *testing.T) {
	body := []byte(
		strings.Repeat("User-agent: *\n", 128) +
			strings.Repeat("Disallow: /x\n", 64),
	)
	called := false
	_, err := parseRobotsBody(body, func([]byte) (*robotstxt.RobotsData, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("exact expanded-association limit was rejected: %v", err)
	}
	if !called {
		t.Fatal("parser was not called at the exact expanded-association limit")
	}
}

func TestRobotsExpandedAssociationAccountingResetsAfterCrawlDelayGroup(t *testing.T) {
	body := []byte(
		strings.Repeat("User-agent: *\n", maxRobotsUserAgents-1) +
			"Crawl-delay: 1\n" +
			"User-agent: MiFolyoBot\n" +
			strings.Repeat("Disallow: /x\n", maxRobotsAllowDisallows),
	)
	called := false
	_, err := parseRobotsBody(body, func([]byte) (*robotstxt.RobotsData, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("valid reset groups were rejected: %v", err)
	}
	if !called {
		t.Fatal("parser was not called for bounded reset groups")
	}
}

func TestRobotsExpandedAssociationAccountingDoesNotResetAfterInvalidCrawlDelay(t *testing.T) {
	body := []byte(
		strings.Repeat("User-agent: *\n", 2) +
			"Crawl-delay: invalid\n" +
			"User-agent: MiFolyoBot\n" +
			strings.Repeat("Disallow: /x\n", maxExpandedAssociations/3+1),
	)
	called := false
	_, err := parseRobotsBody(body, func([]byte) (*robotstxt.RobotsData, error) {
		called = true
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "expanded rule association") {
		t.Fatalf("invalid crawl-delay group error = %v, want expanded-association rejection", err)
	}
	if called {
		t.Fatal("parser was called after invalid crawl-delay incorrectly reset the group")
	}
}

func TestRobotsPreflightLimitsAndUTF8(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "invalid UTF-8", body: []byte{0xff}},
		{name: "too many lines", body: []byte(strings.Repeat("#\n", maxRobotsLines+1))},
		{name: "line too long", body: []byte(strings.Repeat("x", maxRobotsLineBytes+1))},
		{name: "too many agents", body: []byte(strings.Repeat("User-agent: x\n", maxRobotsUserAgents+1))},
		{name: "too many rules", body: []byte("User-agent: x\n" + strings.Repeat("Disallow: /x\n", maxRobotsAllowDisallows+1))},
		{name: "malformed rule escape", body: []byte("User-agent: x\nDisallow: /bad%G0\n")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			_, err := parseRobotsBody(test.body, func([]byte) (*robotstxt.RobotsData, error) {
				called = true
				return nil, nil
			})
			if err == nil {
				t.Fatal("invalid robots body was accepted")
			}
			if called {
				t.Fatal("parser was called before preflight rejection")
			}
		})
	}
}

func TestPrepareRobotsBodyNormalizesRFC9309Paths(t *testing.T) {
	prepared, err := prepareRobotsBody([]byte("User-agent: *\nDisallow: /秘密\nDisallow: /%70rivate\n"))
	if err != nil {
		t.Fatalf("prepareRobotsBody failed: %v", err)
	}
	want := "User-agent: *\nDisallow: /%E7%A7%98%E5%AF%86\nDisallow: /private\n"
	if string(prepared) != want {
		t.Fatalf("prepared robots body = %q, want %q", prepared, want)
	}
}

func TestNormalizeREPValueNormalizesTargetAndReservedEscapes(t *testing.T) {
	got, err := normalizeREPValue("/p%72ivate/%E7%A7%98%E5%AF%86?x=%61%2F")
	if err != nil {
		t.Fatalf("normalizeREPValue failed: %v", err)
	}
	if want := "/private/%E7%A7%98%E5%AF%86?x=a%2F"; got != want {
		t.Fatalf("normalizeREPValue = %q, want %q", got, want)
	}
}
