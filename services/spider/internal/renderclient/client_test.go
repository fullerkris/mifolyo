package renderclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/strictjson"
)

func inlineTestRule() renderpolicy.Rule {
	return renderpolicy.Rule{
		ID:      "inline-fixture",
		Enabled: true,
		Host:    "render.example.org",
		Mode:    renderpolicy.ModeInlineOnly,
		Limits: renderpolicy.Limits{
			MaxRenderTime:       time.Second,
			SettleTime:          10 * time.Millisecond,
			MaxRenderedDOMBytes: 2048,
			MaxDOMNodes:         100,
			MaxConsoleBytes:     100,
		},
	}
}

func brokeredTestRule() renderpolicy.Rule {
	return renderpolicy.Rule{
		ID:      "brokered-fixture",
		Enabled: true,
		Host:    "render.example.org",
		Mode:    renderpolicy.ModeBrokered,
		ResourceRules: []renderpolicy.ResourceRule{
			{
				Host:         "z.cdn.example.org",
				AllowPaths:   []string{"/app.js"},
				AllowedTypes: []renderpolicy.ResourceType{renderpolicy.ResourceTypeScript},
			},
			{
				Host:              "a.cdn.example.org",
				AllowPaths:        []string{"/shared.js"},
				AllowPathPrefixes: []string{"/css/"},
				AllowedTypes: []renderpolicy.ResourceType{
					renderpolicy.ResourceTypeStylesheet,
					renderpolicy.ResourceTypeScript,
				},
			},
			{
				Host:              "z.cdn.example.org",
				AllowPathPrefixes: []string{"/chunks/"},
				AllowedTypes:      []renderpolicy.ResourceType{renderpolicy.ResourceTypeScript},
			},
		},
		Limits: renderpolicy.Limits{
			MaxRenderTime:             time.Second,
			SettleTime:                10 * time.Millisecond,
			MaxResourceRequests:       4,
			MaxAggregateResourceBytes: 1024,
			MaxResourceBodyBytes:      512,
			MaxRenderedDOMBytes:       2048,
			MaxDOMNodes:               100,
			MaxConsoleBytes:           100,
		},
	}
}

func TestClientV2InlineSuccess(t *testing.T) {
	client := newScriptedClient(t, func(connection net.Conn) error {
		start, payload, err := readRenderStart(connection)
		if err != nil {
			return err
		}
		if err := requireObjectKeys(payload, "effective_url", "html", "job_id", "kind", "limits", "mode", "resource_hosts", "version"); err != nil {
			return err
		}
		if err := requireNestedObjectKeys(
			payload,
			"limits",
			"max_aggregate_resource_bytes",
			"max_console_bytes",
			"max_dom_nodes",
			"max_redirect_hops",
			"max_render_time_ms",
			"max_rendered_dom_bytes",
			"max_resource_body_bytes",
			"max_resource_requests",
			"settle_time_ms",
		); err != nil {
			return err
		}
		if start.Version != ProtocolVersion || start.Kind != renderStartKind || start.Mode != string(renderpolicy.ModeInlineOnly) {
			return fmt.Errorf("unexpected render start: %#v", start)
		}
		if len(start.JobID) != 32 {
			return fmt.Errorf("job ID %q is not a random 128-bit hex ID", start.JobID)
		}
		if !reflect.DeepEqual(start.ResourceHosts.Script, []string{}) || !reflect.DeepEqual(start.ResourceHosts.Stylesheet, []string{}) {
			return fmt.Errorf("inline resource hosts = %#v", start.ResourceHosts)
		}
		if start.Limits.MaxResourceRequests != 0 || start.Limits.MaxAggregateResourceBytes != 0 ||
			start.Limits.MaxResourceBodyBytes != 0 || start.Limits.MaxRedirectHops != 0 {
			return fmt.Errorf("inline resource limits = %#v", start.Limits)
		}
		return writeWorkerFrame(connection, terminalFrame(start.JobID, "ok", "<html><body>rendered</body></html>", 3, 0, 0, 0, ""))
	})

	result, err := client.Render(context.Background(), Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         "<html><script>document.body.textContent='rendered'</script></html>",
		Rule:         inlineTestRule(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DOMNodes != 3 || result.HTML == "" || result.ResourceRequests != 0 || result.ResourceBytes != 0 {
		t.Fatalf("render result = %#v", result)
	}
}

func TestClientRejectsStaleAndInvalidTerminalFrames(t *testing.T) {
	tests := []struct {
		name  string
		frame func(string) []byte
	}{
		{
			name: "stale job",
			frame: func(string) []byte {
				return mustJSON(terminalFrame("00000000000000000000000000000000", "ok", "<html></html>", 1, 0, 0, 0, ""))
			},
		},
		{
			name: "unknown member",
			frame: func(jobID string) []byte {
				frame := terminalFrame(jobID, "ok", "<html></html>", 1, 0, 0, 0, "")
				frame["unexpected"] = true
				return mustJSON(frame)
			},
		},
		{
			name: "duplicate member",
			frame: func(jobID string) []byte {
				return []byte(fmt.Sprintf(
					`{"version":2,"kind":"render_result","kind":"render_result","job_id":%q,"status":"ok","html":"<html></html>","dom_nodes":1,"console_bytes":0,"resource_requests":0,"resource_bytes":0,"error_code":""}`,
					jobID,
				))
			},
		},
		{
			name: "inline byte count",
			frame: func(jobID string) []byte {
				return mustJSON(terminalFrame(jobID, "ok", "<html></html>", 1, 0, 0, 1, ""))
			},
		},
		{
			name: "missing kind",
			frame: func(jobID string) []byte {
				frame := terminalFrame(jobID, "ok", "<html></html>", 1, 0, 0, 0, "")
				delete(frame, "kind")
				return mustJSON(frame)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				return writeFrame(connection, test.frame(start.JobID))
			})
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         inlineTestRule(),
			})
			if err == nil {
				t.Fatal("invalid terminal frame was accepted")
			}
			if IsTemporary(err) {
				t.Fatalf("protocol error was classified as temporary: %v", err)
			}
		})
	}
}

func TestClientRequiresCleanEOFAfterEveryTerminalResult(t *testing.T) {
	t.Run("successful terminal with trailing byte", func(t *testing.T) {
		client := newScriptedClient(t, func(connection net.Conn) error {
			start, _, err := readRenderStart(connection)
			if err != nil {
				return err
			}
			terminal := mustFrame(terminalFrame(start.JobID, "ok", "<html></html>", 1, 0, 0, 0, ""))
			return writeAll(connection, append(terminal, 0xff))
		})
		result, err := client.Render(context.Background(), Job{
			EffectiveURL: "https://render.example.org/app",
			HTML:         "<html></html>",
			Rule:         inlineTestRule(),
		})
		if !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("trailing-byte result = %#v", result)
		}
		requireProtocolFailure(t, err)
	})

	t.Run("worker error terminal with trailing partial frame", func(t *testing.T) {
		client := newScriptedClient(t, func(connection net.Conn) error {
			start, _, err := readRenderStart(connection)
			if err != nil {
				return err
			}
			terminal := mustFrame(terminalFrame(start.JobID, "error", "", 0, 0, 0, 0, "render_failed"))
			partialFrame := []byte{0, 0, 0, 10, '{'}
			return writeAll(connection, append(terminal, partialFrame...))
		})
		_, err := client.Render(context.Background(), Job{
			EffectiveURL: "https://render.example.org/app",
			HTML:         "<html></html>",
			Rule:         inlineTestRule(),
		})
		requireProtocolFailure(t, err)
	})

	t.Run("denied terminal with trailing complete frame", func(t *testing.T) {
		brokerCause := errors.New("test broker denial")
		client := newScriptedClient(t, func(connection net.Conn) error {
			start, _, err := readRenderStart(connection)
			if err != nil {
				return err
			}
			if err := writeWorkerFrame(connection, intentFrame(
				start.JobID,
				1,
				"https://z.cdn.example.org/app.js",
				renderpolicy.ResourceTypeScript,
			)); err != nil {
				return err
			}
			reply, _, err := readResourceReply(connection)
			if err != nil {
				return err
			}
			if err := requireNeutralDeniedReply(reply, start.JobID, 1); err != nil {
				return err
			}
			terminal := mustFrame(terminalFrame(start.JobID, "error", "", 0, 0, 1, 0, "resource_denied"))
			extra := mustFrame(terminalFrame(start.JobID, "error", "", 0, 0, 1, 0, "resource_denied"))
			return writeAll(connection, append(terminal, extra...))
		})
		_, err := client.Render(context.Background(), Job{
			EffectiveURL: "https://render.example.org/app",
			HTML:         "<html></html>",
			Rule:         brokeredTestRule(),
			Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
				return Resource{}, brokerCause
			}),
		})
		if !errors.Is(err, brokerCause) {
			t.Fatalf("trailing-frame denial lost broker cause: %v", err)
		}
		requireProtocolFailure(t, err)
	})
}

func TestClientClassifiesWorkerBusyAndRenderFailedAsTemporary(t *testing.T) {
	for _, code := range []string{"worker_busy", "render_failed"} {
		t.Run(code, func(t *testing.T) {
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				return writeWorkerFrame(connection, terminalFrame(start.JobID, "error", "", 0, 0, 0, 0, code))
			})
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         inlineTestRule(),
			})
			if !IsTemporary(err) {
				t.Fatalf("%s error = %v", code, err)
			}
			var renderErr *Error
			if !errors.As(err, &renderErr) || renderErr.Code != code {
				t.Fatalf("worker error = %#v", renderErr)
			}
		})
	}
}

func TestClientRequiresZeroAuthoritativeCountersForLocalWorkerFailure(t *testing.T) {
	for _, test := range []struct {
		name             string
		resourceRequests int
		resourceBytes    int64
		wantProtocol     bool
	}{
		{name: "exact zero counters"},
		{name: "invented request", resourceRequests: 1, wantProtocol: true},
		{name: "invented bytes", resourceBytes: 1, wantProtocol: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				return writeWorkerFrame(connection, terminalFrame(
					start.JobID,
					"error",
					"",
					0,
					0,
					test.resourceRequests,
					test.resourceBytes,
					"console_limit",
				))
			})
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         inlineTestRule(),
			})
			var renderErr *Error
			if !errors.As(err, &renderErr) {
				t.Fatalf("render error = %v", err)
			}
			if test.wantProtocol {
				if renderErr.Code != "worker_protocol_failed" {
					t.Fatalf("error code = %q, want worker_protocol_failed", renderErr.Code)
				}
				return
			}
			if renderErr.Code != "console_limit" {
				t.Fatalf("error code = %q, want console_limit", renderErr.Code)
			}
		})
	}
}

func TestClientRequiresExactAuthoritativeCountersForWorkerErrorAfterResource(t *testing.T) {
	for _, test := range []struct {
		name             string
		resourceRequests int
		resourceBytes    int64
		wantProtocol     bool
	}{
		{name: "exact counters", resourceRequests: 1, resourceBytes: 3},
		{name: "request undercount", resourceRequests: 0, resourceBytes: 3, wantProtocol: true},
		{name: "request overcount", resourceRequests: 2, resourceBytes: 3, wantProtocol: true},
		{name: "byte undercount", resourceRequests: 1, resourceBytes: 2, wantProtocol: true},
		{name: "byte overcount", resourceRequests: 1, resourceBytes: 4, wantProtocol: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				if err := writeWorkerFrame(connection, intentFrame(
					start.JobID,
					1,
					"https://z.cdn.example.org/app.js",
					renderpolicy.ResourceTypeScript,
				)); err != nil {
					return err
				}
				if _, _, err := readResourceReply(connection); err != nil {
					return err
				}
				return writeWorkerFrame(connection, terminalFrame(
					start.JobID,
					"error",
					"",
					0,
					0,
					test.resourceRequests,
					test.resourceBytes,
					"render_timeout",
				))
			})
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         brokeredTestRule(),
				Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
					return Resource{Body: []byte("abc"), ContentType: "application/javascript"}, nil
				}),
			})
			var renderErr *Error
			if !errors.As(err, &renderErr) {
				t.Fatalf("render error = %v", err)
			}
			if test.wantProtocol {
				if renderErr.Code != "worker_protocol_failed" {
					t.Fatalf("error code = %q, want worker_protocol_failed", renderErr.Code)
				}
				return
			}
			if renderErr.Code != "render_timeout" {
				t.Fatalf("error code = %q, want render_timeout", renderErr.Code)
			}
		})
	}
}

func TestClientBrokeredResourceExchange(t *testing.T) {
	var lock sync.Mutex
	var brokerIntents []ResourceIntent
	broker := brokerFunc(func(_ context.Context, intent ResourceIntent) (Resource, error) {
		lock.Lock()
		defer lock.Unlock()
		brokerIntents = append(brokerIntents, intent)
		switch intent.Type {
		case renderpolicy.ResourceTypeScript:
			return Resource{Body: []byte("abcde"), ContentType: "text/javascript; charset=utf-8"}, nil
		case renderpolicy.ResourceTypeStylesheet:
			return Resource{Body: []byte("body{}"), ContentType: "text/css; charset=utf-8"}, nil
		default:
			return Resource{}, fmt.Errorf("unexpected type %q", intent.Type)
		}
	})

	client := newScriptedClient(t, func(connection net.Conn) error {
		start, payload, err := readRenderStart(connection)
		if err != nil {
			return err
		}
		if err := requireNestedObjectKeys(payload, "resource_hosts", "script", "stylesheet"); err != nil {
			return err
		}
		if !reflect.DeepEqual(start.ResourceHosts.Script, []string{"a.cdn.example.org", "z.cdn.example.org"}) ||
			!reflect.DeepEqual(start.ResourceHosts.Stylesheet, []string{"a.cdn.example.org"}) {
			return fmt.Errorf("resource hosts = %#v", start.ResourceHosts)
		}

		if err := writeWorkerFrame(connection, intentFrame(
			start.JobID,
			1,
			"https://z.cdn.example.org/app.js",
			renderpolicy.ResourceTypeScript,
		)); err != nil {
			return err
		}
		first, firstPayload, err := readResourceReply(connection)
		if err != nil {
			return err
		}
		if err := requireObjectKeys(firstPayload, "body_base64", "body_bytes", "content_type", "error_code", "intent_id", "job_id", "kind", "status", "status_code", "version"); err != nil {
			return err
		}
		if first.Version != ProtocolVersion || first.Kind != resourceReplyKind || first.JobID != start.JobID ||
			first.IntentID != 1 || first.Status != "ok" || first.StatusCode != 200 || first.BodyBytes != 5 ||
			first.BodyBase64 != "YWJjZGU=" || first.ContentType != "text/javascript; charset=utf-8" || first.ErrorCode != "" {
			return fmt.Errorf("first resource reply = %#v", first)
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(first.BodyBase64)
		if err != nil || !bytes.Equal(decoded, []byte("abcde")) || base64.StdEncoding.EncodeToString(decoded) != first.BodyBase64 {
			return fmt.Errorf("resource body is not canonical padded base64")
		}

		if err := writeWorkerFrame(connection, intentFrame(
			start.JobID,
			2,
			"https://a.cdn.example.org/css/site.css",
			renderpolicy.ResourceTypeStylesheet,
		)); err != nil {
			return err
		}
		second, _, err := readResourceReply(connection)
		if err != nil {
			return err
		}
		if second.IntentID != 2 || second.BodyBytes != 6 || second.BodyBase64 != "Ym9keXt9" || second.ContentType != "text/css; charset=utf-8" {
			return fmt.Errorf("second resource reply = %#v", second)
		}
		return writeWorkerFrame(connection, terminalFrame(start.JobID, "ok", "<html>brokered</html>", 1, 0, 2, 11, ""))
	})

	result, err := client.Render(context.Background(), Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         "<html></html>",
		Rule:         brokeredTestRule(),
		Broker:       broker,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceRequests != 2 || result.ResourceBytes != 11 || result.HTML != "<html>brokered</html>" {
		t.Fatalf("render result = %#v", result)
	}
	lock.Lock()
	defer lock.Unlock()
	if len(brokerIntents) != 2 || brokerIntents[0].JobID == "" || brokerIntents[1].JobID != brokerIntents[0].JobID {
		t.Fatalf("broker intent job identities = %#v", brokerIntents)
	}
	jobID := brokerIntents[0].JobID
	wantIntents := []ResourceIntent{
		{JobID: jobID, IntentID: 1, URL: "https://z.cdn.example.org/app.js", Method: "GET", Type: renderpolicy.ResourceTypeScript},
		{JobID: jobID, IntentID: 2, URL: "https://a.cdn.example.org/css/site.css", Method: "GET", Type: renderpolicy.ResourceTypeStylesheet},
	}
	if !reflect.DeepEqual(brokerIntents, wantIntents) {
		t.Fatalf("broker intents = %#v, want %#v", brokerIntents, wantIntents)
	}
}

func TestClientRejectsStaleAndDuplicateResourceIntents(t *testing.T) {
	for _, test := range []struct {
		name      string
		firstID   int
		secondID  int
		staleJob  bool
		wantCalls int32
	}{
		{name: "ID does not start at one", firstID: 2, wantCalls: 0},
		{name: "duplicate ID", firstID: 1, secondID: 1, wantCalls: 1},
		{name: "stale job", firstID: 1, staleJob: true, wantCalls: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			broker := brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
				calls.Add(1)
				return Resource{Body: []byte("ok"), ContentType: "application/javascript"}, nil
			})
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				intentJobID := start.JobID
				if test.staleJob {
					intentJobID = "00000000000000000000000000000000"
				}
				if err := writeWorkerFrame(connection, intentFrame(intentJobID, test.firstID, "https://z.cdn.example.org/app.js", renderpolicy.ResourceTypeScript)); err != nil {
					return err
				}
				if test.secondID == 0 || test.staleJob {
					return nil
				}
				if _, _, err := readResourceReply(connection); err != nil {
					return err
				}
				return writeWorkerFrame(connection, intentFrame(start.JobID, test.secondID, "https://z.cdn.example.org/app.js", renderpolicy.ResourceTypeScript))
			})
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         brokeredTestRule(),
				Broker:       broker,
			})
			if err == nil {
				t.Fatal("stale resource intent was accepted")
			}
			if calls.Load() != test.wantCalls {
				t.Fatalf("broker calls = %d, want %d", calls.Load(), test.wantCalls)
			}
		})
	}
}

func TestClientRejectsWorkerDataWhileResourceReplyIsOutstanding(t *testing.T) {
	for _, test := range []struct {
		name       string
		writeEarly func(net.Conn, string) error
	}{
		{
			name: "second intent",
			writeEarly: func(connection net.Conn, jobID string) error {
				return writeWorkerFrame(connection, intentFrame(
					jobID,
					2,
					"https://z.cdn.example.org/chunks/early.js",
					renderpolicy.ResourceTypeScript,
				))
			},
		},
		{
			name: "early terminal",
			writeEarly: func(connection net.Conn, jobID string) error {
				return writeWorkerFrame(connection, terminalFrame(jobID, "error", "", 0, 0, 1, 0, "render_failed"))
			},
		},
		{
			name: "partial frame",
			writeEarly: func(connection net.Conn, _ string) error {
				return writeAll(connection, []byte{0})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			brokerStarted := make(chan struct{})
			var brokerStartedOnce sync.Once
			var brokerCalls atomic.Int32
			broker := brokerFunc(func(ctx context.Context, _ ResourceIntent) (Resource, error) {
				brokerCalls.Add(1)
				brokerStartedOnce.Do(func() { close(brokerStarted) })
				<-ctx.Done()
				return Resource{}, ctx.Err()
			})

			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				if err := writeWorkerFrame(connection, intentFrame(
					start.JobID,
					1,
					"https://z.cdn.example.org/app.js",
					renderpolicy.ResourceTypeScript,
				)); err != nil {
					return err
				}
				<-brokerStarted
				if err := test.writeEarly(connection, start.JobID); err != nil && !isExpectedPeerClose(err) {
					return err
				}
				var firstReplyByte [1]byte
				if _, err := connection.Read(firstReplyByte[:]); err == nil {
					return fmt.Errorf("client wrote a resource reply after early worker data")
				}
				return nil
			})

			startedAt := time.Now()
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         brokeredTestRule(),
				Broker:       broker,
			})
			var renderErr *Error
			if !errors.As(err, &renderErr) || renderErr.Code != "worker_protocol_failed" {
				t.Fatalf("early worker data error = %v", err)
			}
			if elapsed := time.Since(startedAt); elapsed >= time.Second {
				t.Fatalf("early worker data was not rejected promptly: %s", elapsed)
			}
			if brokerCalls.Load() != 1 {
				t.Fatalf("broker calls = %d, want 1", brokerCalls.Load())
			}
		})
	}
}

func TestClientRejectsWorkerDataBeforeLargeResourceReplyWriteCompletes(t *testing.T) {
	for _, test := range []struct {
		name           string
		replyPartBytes int
		writeEarly     func(net.Conn, string) error
	}{
		{
			name:           "second intent after first reply byte",
			replyPartBytes: 1,
			writeEarly: func(connection net.Conn, jobID string) error {
				return writeWorkerFrame(connection, intentFrame(
					jobID,
					2,
					"https://z.cdn.example.org/chunks/early.js",
					renderpolicy.ResourceTypeScript,
				))
			},
		},
		{
			name:           "terminal after partial reply",
			replyPartBytes: 1024,
			writeEarly: func(connection net.Conn, jobID string) error {
				return writeWorkerFrame(connection, terminalFrame(jobID, "error", "", 0, 0, 1, 0, "render_failed"))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resourceBody := bytes.Repeat([]byte("x"), 4*1024*1024)
			rule := brokeredTestRule()
			rule.Limits.MaxResourceBodyBytes = int64(len(resourceBody))
			rule.Limits.MaxAggregateResourceBytes = int64(len(resourceBody))

			renderReturned := make(chan struct{})
			earlyFrameWritten := make(chan time.Time, 1)
			var brokerCalls atomic.Int32
			client := newScriptedClient(t, func(connection net.Conn) error {
				unixConnection, ok := connection.(*net.UnixConn)
				if !ok {
					return fmt.Errorf("worker connection type = %T, want *net.UnixConn", connection)
				}
				if err := unixConnection.SetReadBuffer(1024); err != nil {
					return fmt.Errorf("set worker receive buffer: %w", err)
				}
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				if err := writeWorkerFrame(connection, intentFrame(
					start.JobID,
					1,
					"https://z.cdn.example.org/app.js",
					renderpolicy.ResourceTypeScript,
				)); err != nil {
					return err
				}

				var header [4]byte
				if _, err := io.ReadFull(connection, header[:]); err != nil {
					return fmt.Errorf("read resource reply header: %w", err)
				}
				replyLength := int(binary.BigEndian.Uint32(header[:]))
				if replyLength <= test.replyPartBytes {
					return fmt.Errorf("large resource reply length = %d", replyLength)
				}
				partialReply := make([]byte, test.replyPartBytes)
				if _, err := io.ReadFull(connection, partialReply); err != nil {
					return fmt.Errorf("read partial resource reply: %w", err)
				}
				earlyFrameWritten <- time.Now()
				if err := test.writeEarly(connection, start.JobID); err != nil && !isExpectedPeerClose(err) {
					return err
				}
				<-renderReturned
				return nil
			})

			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         rule,
				Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
					brokerCalls.Add(1)
					return Resource{Body: resourceBody, ContentType: "application/javascript; charset=utf-8"}, nil
				}),
			})
			close(renderReturned)
			earlyWriteTime := <-earlyFrameWritten
			var renderErr *Error
			if !errors.As(err, &renderErr) || renderErr.Code != "worker_protocol_failed" || renderErr.Temporary {
				t.Fatalf("early frame during reply write error = %v", err)
			}
			if elapsed := time.Since(earlyWriteTime); elapsed >= time.Second {
				t.Fatalf("early frame during reply write took %s to reject", elapsed)
			}
			if brokerCalls.Load() != 1 {
				t.Fatalf("broker calls = %d, want 1", brokerCalls.Load())
			}
		})
	}
}

func TestClientValidatesResourceIntentBeforeBrokerCallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "method",
			mutate: func(frame map[string]any) {
				frame["method"] = "POST"
			},
		},
		{
			name: "type",
			mutate: func(frame map[string]any) {
				frame["resource_type"] = "image"
			},
		},
		{
			name: "URL query",
			mutate: func(frame map[string]any) {
				frame["url"] = "https://z.cdn.example.org/app.js?uncanonical=true"
			},
		},
		{
			name: "URL scheme",
			mutate: func(frame map[string]any) {
				frame["url"] = "http://z.cdn.example.org/app.js"
			},
		},
		{
			name: "URL canonical form",
			mutate: func(frame map[string]any) {
				frame["url"] = "https://z.cdn.example.org:443/app.js"
			},
		},
		{
			name: "URL ambiguous path",
			mutate: func(frame map[string]any) {
				frame["url"] = "https://z.cdn.example.org/chunks%2Fapp.js"
			},
		},
		{
			name: "unknown field",
			mutate: func(frame map[string]any) {
				frame["crawl_group"] = "spoofed"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				frame := intentFrame(start.JobID, 1, "https://z.cdn.example.org/app.js", renderpolicy.ResourceTypeScript)
				test.mutate(frame)
				return writeWorkerFrame(connection, frame)
			})
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         brokeredTestRule(),
				Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
					calls.Add(1)
					return Resource{Body: []byte("unexpected"), ContentType: "application/javascript"}, nil
				}),
			})
			if err == nil {
				t.Fatal("invalid resource intent was accepted")
			}
			var renderErr *Error
			if !errors.As(err, &renderErr) || renderErr.Code != "worker_protocol_failed" || renderErr.Temporary {
				t.Fatalf("invalid resource intent error = %v", err)
			}
			if calls.Load() != 0 {
				t.Fatalf("broker calls = %d, want 0", calls.Load())
			}
		})
	}
}

func TestClientReturnsNeutralDenialForStructurallyValidUnauthorizedIntent(t *testing.T) {
	tests := []struct {
		name         string
		rawURL       string
		resourceType renderpolicy.ResourceType
	}{
		{
			name:         "path",
			rawURL:       "https://z.cdn.example.org/private.js",
			resourceType: renderpolicy.ResourceTypeScript,
		},
		{
			name:         "type",
			rawURL:       "https://z.cdn.example.org/app.js",
			resourceType: renderpolicy.ResourceTypeStylesheet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var brokerCalls atomic.Int32
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				if err := writeWorkerFrame(connection, intentFrame(start.JobID, 1, test.rawURL, test.resourceType)); err != nil {
					return err
				}
				reply, _, err := readResourceReply(connection)
				if err != nil {
					return err
				}
				if err := requireNeutralDeniedReply(reply, start.JobID, 1); err != nil {
					return err
				}
				return writeWorkerFrame(connection, terminalFrame(start.JobID, "error", "", 0, 0, 1, 0, "resource_denied"))
			})

			result, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         brokeredTestRule(),
				Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
					brokerCalls.Add(1)
					return Resource{Body: []byte("unexpected"), ContentType: "application/javascript"}, nil
				}),
			})
			if !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("denied result = %#v", result)
			}
			if !errors.Is(err, renderpolicy.ErrNoMatchingResourceRule) {
				t.Fatalf("authorization cause was not preserved: %v", err)
			}
			if brokerCalls.Load() != 0 {
				t.Fatalf("broker calls = %d, want 0", brokerCalls.Load())
			}
		})
	}
}

func TestClientPreservesBrokerErrorAfterNeutralReplyAndTerminal(t *testing.T) {
	brokerCause := errors.New("outbound request gate exhausted")
	broker := brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
		return Resource{}, brokerCause
	})
	client := newScriptedClient(t, func(connection net.Conn) error {
		start, _, err := readRenderStart(connection)
		if err != nil {
			return err
		}
		if err := writeWorkerFrame(connection, intentFrame(start.JobID, 1, "https://z.cdn.example.org/app.js", renderpolicy.ResourceTypeScript)); err != nil {
			return err
		}
		reply, payload, err := readResourceReply(connection)
		if err != nil {
			return err
		}
		if err := requireObjectKeys(payload, "body_base64", "body_bytes", "content_type", "error_code", "intent_id", "job_id", "kind", "status", "status_code", "version"); err != nil {
			return err
		}
		want := resourceReplyDocument{
			Version: ProtocolVersion, Kind: resourceReplyKind, JobID: start.JobID, IntentID: 1,
			Status: "error", StatusCode: 0, ContentType: "", BodyBase64: "", BodyBytes: 0,
			ErrorCode: "resource_denied",
		}
		if !reflect.DeepEqual(reply, want) {
			return fmt.Errorf("denied resource reply = %#v, want %#v", reply, want)
		}
		return writeWorkerFrame(connection, terminalFrame(start.JobID, "error", "", 0, 0, 1, 0, "resource_denied"))
	})

	_, err := client.Render(context.Background(), Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         "<html></html>",
		Rule:         brokeredTestRule(),
		Broker:       broker,
	})
	if !errors.Is(err, brokerCause) {
		t.Fatalf("render error %v does not retain broker cause", err)
	}
	if IsTemporary(err) {
		t.Fatalf("broker error was unexpectedly classified as temporary: %v", err)
	}
}

func TestClientPreservesBrokerErrorWhenTerminalDoesNotMatch(t *testing.T) {
	brokerCause := errors.New("robots denied resource")
	client := newScriptedClient(t, func(connection net.Conn) error {
		start, _, err := readRenderStart(connection)
		if err != nil {
			return err
		}
		if err := writeWorkerFrame(connection, intentFrame(start.JobID, 1, "https://z.cdn.example.org/app.js", renderpolicy.ResourceTypeScript)); err != nil {
			return err
		}
		if _, _, err := readResourceReply(connection); err != nil {
			return err
		}
		return writeWorkerFrame(connection, terminalFrame(start.JobID, "error", "", 0, 0, 0, 0, "resource_denied"))
	})
	_, err := client.Render(context.Background(), Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         "<html></html>",
		Rule:         brokeredTestRule(),
		Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
			return Resource{}, brokerCause
		}),
	})
	if !errors.Is(err, brokerCause) {
		t.Fatalf("mismatched terminal lost broker cause: %v", err)
	}
	if err == nil {
		t.Fatal("mismatched broker terminal was accepted")
	}
}

func TestClientEnforcesResourceLimitsBeforeFurtherBrokerCalls(t *testing.T) {
	rule := brokeredTestRule()
	rule.Limits.MaxResourceRequests = 1
	var calls atomic.Int32
	client := newScriptedClient(t, func(connection net.Conn) error {
		start, _, err := readRenderStart(connection)
		if err != nil {
			return err
		}
		if err := writeWorkerFrame(connection, intentFrame(start.JobID, 1, "https://z.cdn.example.org/app.js", renderpolicy.ResourceTypeScript)); err != nil {
			return err
		}
		if _, _, err := readResourceReply(connection); err != nil {
			return err
		}
		return writeWorkerFrame(connection, intentFrame(start.JobID, 2, "https://z.cdn.example.org/chunks/next.js", renderpolicy.ResourceTypeScript))
	})
	_, err := client.Render(context.Background(), Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         "<html></html>",
		Rule:         rule,
		Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
			calls.Add(1)
			return Resource{Body: []byte("one"), ContentType: "application/javascript"}, nil
		}),
	})
	if err == nil {
		t.Fatal("resource request limit was not enforced")
	}
	if calls.Load() != 1 {
		t.Fatalf("broker calls = %d, want 1", calls.Load())
	}
}

func TestClientRejectsInvalidOrOversizedBrokerResources(t *testing.T) {
	tests := []struct {
		name         string
		resource     Resource
		rawURL       string
		resourceType renderpolicy.ResourceType
		mutate       func(*renderpolicy.Rule)
	}{
		{name: "empty body", resource: Resource{ContentType: "application/javascript"}},
		{name: "invalid UTF-8 body", resource: Resource{Body: []byte{0xff}, ContentType: "application/javascript"}},
		{name: "empty content type", resource: Resource{Body: []byte("ok")}},
		{name: "invalid content type", resource: Resource{Body: []byte("ok"), ContentType: "text/plain\r\nx: y"}},
		{name: "non UTF-8 charset", resource: Resource{Body: []byte("ok"), ContentType: "application/javascript; charset=iso-8859-1"}},
		{name: "script with stylesheet MIME", resource: Resource{Body: []byte("ok"), ContentType: "text/css; charset=utf-8"}},
		{
			name:         "stylesheet with script MIME",
			resource:     Resource{Body: []byte("ok"), ContentType: "text/javascript; charset=utf-8"},
			rawURL:       "https://a.cdn.example.org/css/site.css",
			resourceType: renderpolicy.ResourceTypeStylesheet,
		},
		{
			name:     "body limit",
			resource: Resource{Body: []byte("12345"), ContentType: "application/javascript"},
			mutate: func(rule *renderpolicy.Rule) {
				rule.Limits.MaxResourceBodyBytes = 4
			},
		},
		{
			name:     "aggregate limit",
			resource: Resource{Body: []byte("12345"), ContentType: "application/javascript"},
			mutate: func(rule *renderpolicy.Rule) {
				rule.Limits.MaxAggregateResourceBytes = 4
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := brokeredTestRule()
			rawURL := test.rawURL
			if rawURL == "" {
				rawURL = "https://z.cdn.example.org/app.js"
			}
			resourceType := test.resourceType
			if resourceType == "" {
				resourceType = renderpolicy.ResourceTypeScript
			}
			if test.mutate != nil {
				test.mutate(&rule)
			}
			client := newScriptedClient(t, func(connection net.Conn) error {
				start, _, err := readRenderStart(connection)
				if err != nil {
					return err
				}
				if err := writeWorkerFrame(connection, intentFrame(start.JobID, 1, rawURL, resourceType)); err != nil {
					return err
				}
				reply, _, err := readResourceReply(connection)
				if err != nil {
					return err
				}
				if err := requireNeutralDeniedReply(reply, start.JobID, 1); err != nil {
					return fmt.Errorf("invalid broker resource reply: %w", err)
				}
				return writeWorkerFrame(connection, terminalFrame(start.JobID, "error", "", 0, 0, 1, 0, "resource_denied"))
			})
			_, err := client.Render(context.Background(), Job{
				EffectiveURL: "https://render.example.org/app",
				HTML:         "<html></html>",
				Rule:         rule,
				Broker: brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
					return test.resource, nil
				}),
			})
			if err == nil {
				t.Fatal("invalid broker resource was accepted")
			}
			if !strings.Contains(err.Error(), "resource broker returned an invalid resource") {
				t.Fatalf("validation cause was not preserved: %v", err)
			}
		})
	}
}

func TestClientValidatesInlineAndBrokeredJobInvariants(t *testing.T) {
	noSocketClient := &Client{socketPath: "/tmp/renderclient-does-not-exist.sock"}
	dummyBroker := brokerFunc(func(context.Context, ResourceIntent) (Resource, error) {
		return Resource{}, nil
	})
	tests := []struct {
		name string
		job  Job
	}{
		{
			name: "inline broker",
			job:  Job{EffectiveURL: "https://render.example.org/app", HTML: "<html></html>", Rule: inlineTestRule(), Broker: dummyBroker},
		},
		{
			name: "inline resource limit",
			job: func() Job {
				rule := inlineTestRule()
				rule.Limits.MaxResourceRequests = 1
				return Job{EffectiveURL: "https://render.example.org/app", HTML: "<html></html>", Rule: rule}
			}(),
		},
		{
			name: "broker missing",
			job:  Job{EffectiveURL: "https://render.example.org/app", HTML: "<html></html>", Rule: brokeredTestRule()},
		},
		{
			name: "broker redirect",
			job: func() Job {
				rule := brokeredTestRule()
				rule.Limits.MaxRedirectHops = 1
				return Job{EffectiveURL: "https://render.example.org/app", HTML: "<html></html>", Rule: rule, Broker: dummyBroker}
			}(),
		},
		{
			name: "broker rules stripped",
			job: func() Job {
				rule := brokeredTestRule()
				rule.ResourceRules = nil
				return Job{EffectiveURL: "https://render.example.org/app", HTML: "<html></html>", Rule: rule, Broker: dummyBroker}
			}(),
		},
		{
			name: "sub-millisecond render time",
			job: func() Job {
				rule := inlineTestRule()
				rule.Limits.MaxRenderTime = 1500 * time.Microsecond
				return Job{EffectiveURL: "https://render.example.org/app", HTML: "<html></html>", Rule: rule}
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := noSocketClient.Render(context.Background(), test.job)
			if err == nil {
				t.Fatal("invalid job reached socket dialing")
			}
			var renderErr *Error
			if errors.As(err, &renderErr) && renderErr.Code == "worker_unavailable" {
				t.Fatalf("invalid job reached socket dialing: %v", err)
			}
		})
	}
}

func TestInlineResourceIntentIsAProtocolFailure(t *testing.T) {
	client := newScriptedClient(t, func(connection net.Conn) error {
		start, _, err := readRenderStart(connection)
		if err != nil {
			return err
		}
		return writeWorkerFrame(connection, intentFrame(start.JobID, 1, "https://z.cdn.example.org/app.js", renderpolicy.ResourceTypeScript))
	})
	_, err := client.Render(context.Background(), Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         "<html></html>",
		Rule:         inlineTestRule(),
	})
	if err == nil || IsTemporary(err) {
		t.Fatalf("inline resource intent error = %v", err)
	}
}

func TestClientParentCancellationImmediatelyUnblocksWorkerRead(t *testing.T) {
	startReceived := make(chan struct{})
	client := newScriptedClient(t, func(connection net.Conn) error {
		if _, _, err := readRenderStart(connection); err != nil {
			return err
		}
		close(startReceived)
		var data [1]byte
		if _, err := connection.Read(data[:]); err == nil {
			return fmt.Errorf("client unexpectedly wrote another frame")
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	canceledAt := make(chan time.Time, 1)
	go func() {
		<-startReceived
		// Let the successful render_start write return so this specifically
		// exercises cancellation of the blocked response read.
		time.Sleep(25 * time.Millisecond)
		canceledAt <- time.Now()
		cancel()
	}()

	_, err := client.Render(ctx, Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         "<html></html>",
		Rule:         inlineTestRule(),
	})
	cancelTime := <-canceledAt
	if elapsed := time.Since(cancelTime); elapsed >= 500*time.Millisecond {
		t.Fatalf("canceled worker read took %s to unblock", elapsed)
	}
	var renderErr *Error
	if !errors.As(err, &renderErr) || renderErr.Code != "worker_read_failed" || !renderErr.Temporary {
		t.Fatalf("canceled read error = %#v (%v)", renderErr, err)
	}
}

func TestClientParentCancellationImmediatelyUnblocksWorkerWrite(t *testing.T) {
	connectionAccepted := make(chan struct{})
	releaseWorker := make(chan struct{})
	client := newScriptedClient(t, func(connection net.Conn) error {
		unixConnection, ok := connection.(*net.UnixConn)
		if !ok {
			return fmt.Errorf("worker connection type = %T, want *net.UnixConn", connection)
		}
		if err := unixConnection.SetReadBuffer(1024); err != nil {
			return fmt.Errorf("set worker receive buffer: %w", err)
		}
		close(connectionAccepted)
		<-releaseWorker
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	canceledAt := make(chan time.Time, 1)
	go func() {
		<-connectionAccepted
		// The worker deliberately never reads. Give the multi-megabyte frame
		// time to fill the bounded Unix-socket buffer and block the write.
		time.Sleep(25 * time.Millisecond)
		canceledAt <- time.Now()
		cancel()
	}()

	_, err := client.Render(ctx, Job{
		EffectiveURL: "https://render.example.org/app",
		HTML:         strings.Repeat("x", maxSourceBytes),
		Rule:         inlineTestRule(),
	})
	close(releaseWorker)
	cancelTime := <-canceledAt
	if elapsed := time.Since(cancelTime); elapsed >= 500*time.Millisecond {
		t.Fatalf("canceled worker write took %s to unblock", elapsed)
	}
	var renderErr *Error
	if !errors.As(err, &renderErr) || renderErr.Code != "worker_write_failed" || !renderErr.Temporary {
		t.Fatalf("canceled write error = %#v (%v)", renderErr, err)
	}
}

func TestFrameBoundsRemainFourByteBigEndian(t *testing.T) {
	var framed bytes.Buffer
	payload := []byte(`{"kind":"render_result"}`)
	if err := writeFrame(&framed, payload); err != nil {
		t.Fatal(err)
	}
	if got := framed.Bytes()[:4]; !bytes.Equal(got, []byte{0, 0, 0, byte(len(payload))}) {
		t.Fatalf("frame header = %v", got)
	}
	decoded, err := readFrame(&framed)
	if err != nil || !bytes.Equal(decoded, payload) {
		t.Fatalf("readFrame() = %q, %v", decoded, err)
	}
	for _, length := range []uint32{0, maxFrameBytes + 1} {
		reader := bytes.NewReader([]byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)})
		if _, err := readFrame(reader); err == nil {
			t.Fatalf("frame length %d was accepted", length)
		}
	}
}

type brokerFunc func(context.Context, ResourceIntent) (Resource, error)

func isExpectedPeerClose(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}

func (function brokerFunc) Fetch(ctx context.Context, intent ResourceIntent) (Resource, error) {
	return function(ctx, intent)
}

func newScriptedClient(t *testing.T, script func(net.Conn) error) *Client {
	t.Helper()
	socketDirectory, err := os.MkdirTemp("/tmp", "mifolyo-render-v2-")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(socketDirectory, "renderer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(socketDirectory)
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()
		done <- script(connection)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case scriptErr := <-done:
			if scriptErr != nil && !errors.Is(scriptErr, net.ErrClosed) {
				t.Errorf("worker script: %v", scriptErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("worker script did not finish")
		}
		_ = os.RemoveAll(socketDirectory)
	})
	client, err := New(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func readRenderStart(connection net.Conn) (renderStartDocument, []byte, error) {
	payload, err := readFrame(connection)
	if err != nil {
		return renderStartDocument{}, nil, err
	}
	var start renderStartDocument
	if err := strictjson.Decode(payload, &start); err != nil {
		return renderStartDocument{}, payload, fmt.Errorf("decode render start: %w", err)
	}
	return start, payload, nil
}

func readResourceReply(connection net.Conn) (resourceReplyDocument, []byte, error) {
	payload, err := readFrame(connection)
	if err != nil {
		return resourceReplyDocument{}, nil, err
	}
	var result resourceReplyDocument
	if err := strictjson.Decode(payload, &result); err != nil {
		return resourceReplyDocument{}, payload, fmt.Errorf("decode resource reply: %w", err)
	}
	return result, payload, nil
}

func requireNeutralDeniedReply(reply resourceReplyDocument, jobID string, intentID int) error {
	want := resourceReplyDocument{
		Version: ProtocolVersion, Kind: resourceReplyKind, JobID: jobID, IntentID: intentID,
		Status: "error", StatusCode: 0, ContentType: "", BodyBase64: "", BodyBytes: 0,
		ErrorCode: "resource_denied",
	}
	if !reflect.DeepEqual(reply, want) {
		return fmt.Errorf("resource reply = %#v, want %#v", reply, want)
	}
	return nil
}

func writeWorkerFrame(connection net.Conn, value any) error {
	return writeFrame(connection, mustJSON(value))
}

func mustFrame(value any) []byte {
	var framed bytes.Buffer
	if err := writeFrame(&framed, mustJSON(value)); err != nil {
		panic(err)
	}
	return framed.Bytes()
}

func mustJSON(value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}

func requireProtocolFailure(t *testing.T, err error) {
	t.Helper()
	var renderErr *Error
	if !errors.As(err, &renderErr) || renderErr.Code != "worker_protocol_failed" || renderErr.Temporary {
		t.Fatalf("render error = %v, want non-temporary worker_protocol_failed", err)
	}
}

func intentFrame(jobID string, intentID int, rawURL string, resourceType renderpolicy.ResourceType) map[string]any {
	return map[string]any{
		"version":       ProtocolVersion,
		"kind":          resourceIntentKind,
		"job_id":        jobID,
		"intent_id":     intentID,
		"url":           rawURL,
		"method":        "GET",
		"resource_type": resourceType,
	}
}

func terminalFrame(
	jobID string,
	status string,
	html string,
	domNodes int,
	consoleBytes int64,
	resourceRequests int,
	resourceBytes int64,
	errorCode string,
) map[string]any {
	return map[string]any{
		"version":           ProtocolVersion,
		"kind":              renderResultKind,
		"job_id":            jobID,
		"status":            status,
		"html":              html,
		"dom_nodes":         domNodes,
		"console_bytes":     consoleBytes,
		"resource_requests": resourceRequests,
		"resource_bytes":    resourceBytes,
		"error_code":        errorCode,
	}
}

func requireObjectKeys(payload []byte, expected ...string) error {
	var object map[string]json.RawMessage
	if err := strictjson.Decode(payload, &object); err != nil {
		return err
	}
	actual := make([]string, 0, len(object))
	for key := range object {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("object keys = %v, want %v", actual, expected)
	}
	return nil
}

func requireNestedObjectKeys(payload []byte, member string, expected ...string) error {
	var object map[string]json.RawMessage
	if err := strictjson.Decode(payload, &object); err != nil {
		return err
	}
	nested, exists := object[member]
	if !exists {
		return fmt.Errorf("object is missing %q", member)
	}
	return requireObjectKeys(nested, expected...)
}

var _ ResourceBroker = brokerFunc(nil)
