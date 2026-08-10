package crawler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestGetPageData(t *testing.T) {
	tests := []struct {
		name  string
		start func(http.Handler) *httptest.Server
	}{
		{
			name:  "absolute https url",
			start: httptest.NewTLSServer,
		},
		{
			name:  "absolute http url",
			start: httptest.NewServer,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := tc.start(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html><body>ok</body></html>"))
			}))
			defer server.Close()

			originalClient := pageHTTPClient
			pageHTTPClient = server.Client()
			defer func() { pageHTTPClient = originalClient }()

			_, _, _, err := getPageData(server.URL, utils.DefaultUserAgent)

			if err != nil {
				t.Errorf("Test %v - '%s' FAIL: unexpected error: %v", i, tc.name, err)
				return
			}

		})
	}
}

func TestGetPageDataRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxPageBodyBytes)+1)))
	}))
	defer server.Close()

	_, _, _, err := getPageData(server.URL, utils.DefaultUserAgent)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("expected response size error, got %v", err)
	}
}

func TestGetPageDataSendsUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != utils.DefaultUserAgent {
			t.Fatalf("expected user agent %q, got %q", utils.DefaultUserAgent, got)
		}

		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	_, _, _, err := getPageData(server.URL, utils.DefaultUserAgent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
