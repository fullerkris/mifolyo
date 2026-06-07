package crawler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestGetPageData(t *testing.T) {
	tests := []struct {
		name     string
		inputURL string
	}{
		{
			name:     "absolute https url",
			inputURL: "https://ionelpopjara.github.io/",
		},
		{
			name:     "absolute http url",
			inputURL: "http://ionelpopjara.github.io/",
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := getPageData(tc.inputURL, utils.DefaultUserAgent)

			if err != nil {
				t.Errorf("Test %v - '%s' FAIL: unexpected error: %v", i, tc.name, err)
				return
			}

		})
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
