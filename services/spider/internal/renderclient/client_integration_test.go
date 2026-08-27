package renderclient

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
)

func TestNetworklessChromiumWorkerIntegration(t *testing.T) {
	socketPath := os.Getenv("RENDER_WORKER_INTEGRATION_SOCKET")
	if socketPath == "" {
		t.Skip("RENDER_WORKER_INTEGRATION_SOCKET is not configured")
	}
	client, err := New(socketPath)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("inline", func(t *testing.T) {
		result, err := client.Render(context.Background(), Job{
			EffectiveURL: "https://render.example.org/app",
			HTML: `<!doctype html><html><body><main id="root"></main><script>
				document.querySelector('#root').textContent = 'Go IPC rendered fixture';
			</script></body></html>`,
			Rule: renderpolicy.Rule{
				ID:      "inline-integration-fixture",
				Enabled: true,
				Host:    "render.example.org",
				Mode:    renderpolicy.ModeInlineOnly,
				Limits: renderpolicy.Limits{
					MaxRenderTime:       10 * time.Second,
					SettleTime:          50 * time.Millisecond,
					MaxRenderedDOMBytes: 1024 * 1024,
					MaxDOMNodes:         1000,
					MaxConsoleBytes:     1024,
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.HTML, "Go IPC rendered fixture") ||
			result.ResourceRequests != 0 || result.ResourceBytes != 0 {
			t.Fatalf("render result = %#v", result)
		}
	})

	t.Run("brokered script and stylesheet", func(t *testing.T) {
		scriptBody := []byte("document.body.dataset.externalScript = 'true';")
		stylesheetBody := []byte("#styled { color: rgb(1, 2, 3); }")
		var mutex sync.Mutex
		var intents []ResourceIntent
		broker := integrationBrokerFunc(func(_ context.Context, intent ResourceIntent) (Resource, error) {
			mutex.Lock()
			intents = append(intents, intent)
			mutex.Unlock()
			switch {
			case intent.URL == "https://styles.cdn.example.org/site.css" && intent.Type == renderpolicy.ResourceTypeStylesheet:
				return Resource{Body: stylesheetBody, ContentType: "text/css; charset=utf-8"}, nil
			case intent.URL == "https://scripts.cdn.example.org/app.js" && intent.Type == renderpolicy.ResourceTypeScript:
				return Resource{Body: scriptBody, ContentType: "application/javascript; charset=utf-8"}, nil
			default:
				return Resource{}, errors.New("unexpected integration resource intent")
			}
		})

		result, err := client.Render(context.Background(), Job{
			EffectiveURL: "https://render.example.org/app",
			HTML: `<!doctype html><html><head>
				<link rel="stylesheet" href="https://styles.cdn.example.org/site.css">
			</head><body><main id="styled">brokered fixture</main>
				<script src="https://scripts.cdn.example.org/app.js"></script>
				<script>
					document.body.dataset.styleApplied =
						getComputedStyle(document.querySelector('#styled')).color === 'rgb(1, 2, 3)' ? 'true' : 'false';
				</script>
			</body></html>`,
			Rule:   brokeredIntegrationRule(),
			Broker: broker,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.HTML, `data-external-script="true"`) ||
			!strings.Contains(result.HTML, `data-style-applied="true"`) {
			t.Fatalf("brokered effects are absent from rendered HTML: %s", result.HTML)
		}
		wantBytes := int64(len(scriptBody) + len(stylesheetBody))
		if result.ResourceRequests != 2 || result.ResourceBytes != wantBytes {
			t.Fatalf("brokered counters = %d/%d, want 2/%d", result.ResourceRequests, result.ResourceBytes, wantBytes)
		}

		mutex.Lock()
		gotIntents := append([]ResourceIntent(nil), intents...)
		mutex.Unlock()
		if len(gotIntents) != 2 || gotIntents[0].JobID == "" || gotIntents[1].JobID != gotIntents[0].JobID {
			t.Fatalf("brokered intent identities = %#v", gotIntents)
		}
		jobID := gotIntents[0].JobID
		wantIntents := []ResourceIntent{
			{JobID: jobID, IntentID: 1, URL: "https://styles.cdn.example.org/site.css", Method: "GET", Type: renderpolicy.ResourceTypeStylesheet},
			{JobID: jobID, IntentID: 2, URL: "https://scripts.cdn.example.org/app.js", Method: "GET", Type: renderpolicy.ResourceTypeScript},
		}
		if !reflect.DeepEqual(gotIntents, wantIntents) {
			t.Fatalf("brokered intents = %#v, want %#v", gotIntents, wantIntents)
		}
	})

	t.Run("broker denial", func(t *testing.T) {
		denialCause := errors.New("integration broker denied resource")
		var mutex sync.Mutex
		var intents []ResourceIntent
		broker := integrationBrokerFunc(func(_ context.Context, intent ResourceIntent) (Resource, error) {
			mutex.Lock()
			intents = append(intents, intent)
			mutex.Unlock()
			return Resource{}, denialCause
		})

		result, err := client.Render(context.Background(), Job{
			EffectiveURL: "https://render.example.org/app",
			HTML: `<!doctype html><html><body>
				<script src="https://scripts.cdn.example.org/denied.js"></script>
			</body></html>`,
			Rule:   brokeredIntegrationRule(),
			Broker: broker,
		})
		if !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("denied render returned a result: %#v", result)
		}
		if !errors.Is(err, denialCause) {
			t.Fatalf("denied render did not preserve broker cause: %v", err)
		}
		var protocolOrWorkerError *Error
		if errors.As(err, &protocolOrWorkerError) {
			t.Fatalf("denied render terminal did not match: %v", err)
		}

		mutex.Lock()
		gotIntents := append([]ResourceIntent(nil), intents...)
		mutex.Unlock()
		if len(gotIntents) != 1 || gotIntents[0].JobID == "" {
			t.Fatalf("denied broker intents = %#v", gotIntents)
		}
		want := ResourceIntent{
			JobID:    gotIntents[0].JobID,
			IntentID: 1,
			URL:      "https://scripts.cdn.example.org/denied.js",
			Method:   "GET",
			Type:     renderpolicy.ResourceTypeScript,
		}
		if !reflect.DeepEqual(gotIntents[0], want) {
			t.Fatalf("denied broker intent = %#v, want %#v", gotIntents[0], want)
		}
	})
}

func brokeredIntegrationRule() renderpolicy.Rule {
	return renderpolicy.Rule{
		ID:      "brokered-integration-fixture",
		Enabled: true,
		Host:    "render.example.org",
		Mode:    renderpolicy.ModeBrokered,
		ResourceRules: []renderpolicy.ResourceRule{
			{
				Host:         "scripts.cdn.example.org",
				AllowPaths:   []string{"/app.js", "/denied.js"},
				AllowedTypes: []renderpolicy.ResourceType{renderpolicy.ResourceTypeScript},
			},
			{
				Host:         "styles.cdn.example.org",
				AllowPaths:   []string{"/site.css"},
				AllowedTypes: []renderpolicy.ResourceType{renderpolicy.ResourceTypeStylesheet},
			},
		},
		Limits: renderpolicy.Limits{
			MaxRenderTime:             10 * time.Second,
			SettleTime:                50 * time.Millisecond,
			MaxResourceRequests:       4,
			MaxAggregateResourceBytes: 1024 * 1024,
			MaxResourceBodyBytes:      512 * 1024,
			MaxRenderedDOMBytes:       1024 * 1024,
			MaxDOMNodes:               1000,
			MaxConsoleBytes:           1024,
		},
	}
}

type integrationBrokerFunc func(context.Context, ResourceIntent) (Resource, error)

func (function integrationBrokerFunc) Fetch(ctx context.Context, intent ResourceIntent) (Resource, error) {
	return function(ctx, intent)
}

var _ ResourceBroker = integrationBrokerFunc(nil)
