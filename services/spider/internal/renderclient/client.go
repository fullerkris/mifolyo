// Package renderclient implements the bounded V2 Unix-socket protocol used by
// the spider to communicate with the networkless Chromium worker.
package renderclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/strictjson"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

const (
	ProtocolVersion = 2
	maxFrameBytes   = 32 * 1024 * 1024
	maxSourceBytes  = 5 * 1024 * 1024
	protocolGrace   = 2 * time.Second

	renderStartKind    = "render_start"
	resourceIntentKind = "resource_intent"
	resourceReplyKind  = "resource_reply"
	renderResultKind   = "render_result"

	maxEffectiveURLBytes = 2048
	maxContentTypeBytes  = 1024
	maxRenderTime        = 30 * time.Second
	maxSettleTime        = 5 * time.Second
	maxResourceRequests  = 64
	maxResourceBytes     = int64(32 * 1024 * 1024)
	maxResourceBodyBytes = int64(5 * 1024 * 1024)
	maxRenderedDOMBytes  = int64(5 * 1024 * 1024)
	maxDOMNodes          = 100000
	maxConsoleBytes      = int64(64 * 1024)
)

var (
	errorCodePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	resourceHostPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Renderer interface {
	Render(context.Context, Job) (Result, error)
}

// ResourceIntent is a single, policy-bound browser resource request. V2
// permits only canonical GET requests for scripts and stylesheets.
type ResourceIntent struct {
	JobID    string
	IntentID int
	URL      string
	Method   string
	Type     renderpolicy.ResourceType
}

// Resource is a brokered response body suitable for direct fulfillment by the
// networkless worker.
type Resource struct {
	Body        []byte
	ContentType string
}

// ResourceBroker fetches one already validated and policy-authorized resource.
// Implementations must honor cancellation on ctx.
type ResourceBroker interface {
	Fetch(context.Context, ResourceIntent) (Resource, error)
}

type Job struct {
	EffectiveURL string
	HTML         string
	Rule         renderpolicy.Rule
	Broker       ResourceBroker
}

type Result struct {
	HTML             string
	DOMNodes         int
	ConsoleBytes     int64
	ResourceRequests int
	ResourceBytes    int64
}

type Error struct {
	Code      string
	Temporary bool
	Err       error
}

func (err *Error) Error() string {
	if err.Err != nil {
		return fmt.Sprintf("%s: %v", err.Code, err.Err)
	}
	return err.Code
}

func (err *Error) Unwrap() error {
	return err.Err
}

func IsTemporary(err error) bool {
	var renderErr *Error
	return errors.As(err, &renderErr) && renderErr.Temporary
}

func temporaryError(code string, err error) error {
	return &Error{Code: code, Temporary: true, Err: err}
}

func protocolError(err error) error {
	return &Error{Code: "worker_protocol_failed", Err: err}
}

type Client struct {
	socketPath string
}

type renderStartDocument struct {
	Version       int                   `json:"version"`
	Kind          string                `json:"kind"`
	JobID         string                `json:"job_id"`
	Mode          string                `json:"mode"`
	EffectiveURL  string                `json:"effective_url"`
	HTML          string                `json:"html"`
	ResourceHosts resourceHostsDocument `json:"resource_hosts"`
	Limits        limitsDocument        `json:"limits"`
}

type resourceHostsDocument struct {
	Script     []string `json:"script"`
	Stylesheet []string `json:"stylesheet"`
}

type limitsDocument struct {
	MaxRenderTimeMS           int   `json:"max_render_time_ms"`
	SettleTimeMS              int   `json:"settle_time_ms"`
	MaxResourceRequests       int   `json:"max_resource_requests"`
	MaxAggregateResourceBytes int64 `json:"max_aggregate_resource_bytes"`
	MaxResourceBodyBytes      int64 `json:"max_resource_body_bytes"`
	MaxRenderedDOMBytes       int64 `json:"max_rendered_dom_bytes"`
	MaxDOMNodes               int   `json:"max_dom_nodes"`
	MaxRedirectHops           int   `json:"max_redirect_hops"`
	MaxConsoleBytes           int64 `json:"max_console_bytes"`
}

type resourceIntentDocument struct {
	Version      *int                       `json:"version"`
	Kind         *string                    `json:"kind"`
	JobID        *string                    `json:"job_id"`
	IntentID     *int                       `json:"intent_id"`
	URL          *string                    `json:"url"`
	Method       *string                    `json:"method"`
	ResourceType *renderpolicy.ResourceType `json:"resource_type"`
}

type renderResultDocument struct {
	Version          *int    `json:"version"`
	Kind             *string `json:"kind"`
	JobID            *string `json:"job_id"`
	Status           *string `json:"status"`
	HTML             *string `json:"html"`
	DOMNodes         *int    `json:"dom_nodes"`
	ConsoleBytes     *int64  `json:"console_bytes"`
	ResourceRequests *int    `json:"resource_requests"`
	ResourceBytes    *int64  `json:"resource_bytes"`
	ErrorCode        *string `json:"error_code"`
}

type resourceReplyDocument struct {
	Version     int    `json:"version"`
	Kind        string `json:"kind"`
	JobID       string `json:"job_id"`
	IntentID    int    `json:"intent_id"`
	Status      string `json:"status"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type"`
	BodyBase64  string `json:"body_base64"`
	BodyBytes   int64  `json:"body_bytes"`
	ErrorCode   string `json:"error_code"`
}

func New(socketPath string) (*Client, error) {
	if socketPath == "" || !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath || strings.IndexByte(socketPath, 0) >= 0 {
		return nil, fmt.Errorf("render socket path must be a clean absolute path")
	}
	return &Client{socketPath: socketPath}, nil
}

func (client *Client) Render(ctx context.Context, job Job) (Result, error) {
	if client == nil {
		return Result{}, fmt.Errorf("render client is not configured")
	}
	if ctx == nil {
		return Result{}, fmt.Errorf("render context is required")
	}
	resourceHosts, err := validateJob(job)
	if err != nil {
		return Result{}, err
	}

	jobID, err := newJobID()
	if err != nil {
		return Result{}, fmt.Errorf("create render job ID: %w", err)
	}
	start := renderStartDocument{
		Version:       ProtocolVersion,
		Kind:          renderStartKind,
		JobID:         jobID,
		Mode:          string(job.Rule.Mode),
		EffectiveURL:  job.EffectiveURL,
		HTML:          job.HTML,
		ResourceHosts: resourceHosts,
		Limits: limitsDocument{
			MaxRenderTimeMS:           int(job.Rule.Limits.MaxRenderTime / time.Millisecond),
			SettleTimeMS:              int(job.Rule.Limits.SettleTime / time.Millisecond),
			MaxResourceRequests:       job.Rule.Limits.MaxResourceRequests,
			MaxAggregateResourceBytes: job.Rule.Limits.MaxAggregateResourceBytes,
			MaxResourceBodyBytes:      job.Rule.Limits.MaxResourceBodyBytes,
			MaxRenderedDOMBytes:       job.Rule.Limits.MaxRenderedDOMBytes,
			MaxDOMNodes:               job.Rule.Limits.MaxDOMNodes,
			MaxRedirectHops:           job.Rule.Limits.MaxRedirectHops,
			MaxConsoleBytes:           job.Rule.Limits.MaxConsoleBytes,
		},
	}
	startPayload, err := json.Marshal(start)
	if err != nil {
		return Result{}, fmt.Errorf("encode render start: %w", err)
	}
	if len(startPayload) > maxFrameBytes {
		return Result{}, fmt.Errorf("render start frame exceeds %d bytes", maxFrameBytes)
	}

	requestContext, cancel := context.WithTimeout(ctx, job.Rule.Limits.MaxRenderTime+protocolGrace)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(requestContext, "unix", client.socketPath)
	if err != nil {
		return Result{}, temporaryError("worker_unavailable", err)
	}
	defer connection.Close()
	if deadline, ok := requestContext.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return Result{}, temporaryError("worker_deadline_failed", err)
		}
	}
	stopCancellationDeadline := context.AfterFunc(requestContext, func() {
		// The fixed render deadline does not move forward when a parent context is
		// canceled. Move both socket deadlines to now so an in-flight frame read or
		// write observes cancellation immediately.
		_ = connection.SetDeadline(time.Now())
	})
	defer stopCancellationDeadline()
	if err := writeFrame(connection, startPayload); err != nil {
		return Result{}, temporaryError("worker_write_failed", err)
	}
	framePump := newFrameReadPump(connection)
	defer framePump.stop()

	nextIntentID := 1
	resourceRequests := 0
	var resourceBytes int64
	for {
		payload, err := framePump.readFrame()
		if err != nil {
			return Result{}, temporaryError("worker_read_failed", err)
		}
		kind, err := inspectFrameKind(payload)
		if err != nil {
			return Result{}, protocolError(err)
		}

		switch kind {
		case resourceIntentKind:
			var document resourceIntentDocument
			if err := strictjson.Decode(payload, &document); err != nil {
				return Result{}, protocolError(fmt.Errorf("decode resource intent: %w", err))
			}
			intentID, err := validateResourceIntentIdentity(document, jobID, nextIntentID)
			if err != nil {
				return Result{}, protocolError(err)
			}
			if job.Rule.Mode == renderpolicy.ModeInlineOnly {
				return Result{}, protocolError(fmt.Errorf("inline render sent a resource intent"))
			}
			nextIntentID++
			resourceRequests++
			if resourceRequests > job.Rule.Limits.MaxResourceRequests {
				return Result{}, protocolError(fmt.Errorf("resource intent count exceeds the configured limit"))
			}
			intent, err := validateResourceIntent(document)
			if err != nil {
				return Result{}, protocolError(err)
			}
			if err := framePump.beginResourceReply(); err != nil {
				return Result{}, err
			}
			canonicalURL, authorizationErr := job.Rule.MatchResource(intent.URL, intent.Type)
			if authorizationErr != nil || canonicalURL != intent.URL {
				if authorizationErr == nil {
					authorizationErr = fmt.Errorf("resource URL changed during authorization")
				}
				cause := fmt.Errorf("resource intent %d is not authorized: %w", intentID, authorizationErr)
				return Result{}, denyResourceAndRequireTerminal(
					framePump,
					connection,
					jobID,
					intentID,
					job.Rule,
					resourceRequests,
					resourceBytes,
					cause,
				)
			}
			intent.URL = canonicalURL

			brokerOutcome, ipcErr := fetchResourceWhileReplyOutstanding(
				requestContext,
				framePump,
				job.Broker,
				intent,
			)
			if ipcErr != nil {
				return Result{}, ipcErr
			}
			resource, brokerErr := brokerOutcome.resource, brokerOutcome.err
			if brokerErr != nil {
				cause := fmt.Errorf("fetch resource intent %d: %w", intentID, brokerErr)
				return Result{}, denyResourceAndRequireTerminal(
					framePump,
					connection,
					jobID,
					intentID,
					job.Rule,
					resourceRequests,
					resourceBytes,
					cause,
				)
			}

			bodyBytes, err := validateResource(resource, intent.Type, job.Rule.Limits, resourceBytes)
			if err != nil {
				cause := fmt.Errorf("resource broker returned an invalid resource for intent %d: %w", intentID, err)
				return Result{}, denyResourceAndRequireTerminal(
					framePump,
					connection,
					jobID,
					intentID,
					job.Rule,
					resourceRequests,
					resourceBytes,
					cause,
				)
			}
			reply := resourceReplyDocument{
				Version:     ProtocolVersion,
				Kind:        resourceReplyKind,
				JobID:       jobID,
				IntentID:    intentID,
				Status:      "ok",
				StatusCode:  200,
				ContentType: resource.ContentType,
				BodyBase64:  base64.StdEncoding.EncodeToString(resource.Body),
				BodyBytes:   bodyBytes,
				ErrorCode:   "",
			}
			if err := writeResourceReply(framePump, connection, reply); err != nil {
				return Result{}, err
			}
			resourceBytes += bodyBytes

		case renderResultKind:
			var document renderResultDocument
			if err := strictjson.Decode(payload, &document); err != nil {
				return Result{}, protocolError(fmt.Errorf("decode render result: %w", err))
			}
			result, err := validateRenderResult(document, jobID, job.Rule, resourceRequests, resourceBytes)
			if err == nil {
				if err := framePump.requireCleanEOF(); err != nil {
					return Result{}, err
				}
				return result, nil
			}
			var workerErr *Error
			if errors.As(err, &workerErr) {
				if eofErr := framePump.requireCleanEOF(); eofErr != nil {
					return Result{}, eofErr
				}
				return Result{}, err
			}
			return Result{}, protocolError(err)

		default:
			return Result{}, protocolError(fmt.Errorf("worker frame kind %q is unsupported", kind))
		}
	}
}

func validateJob(job Job) (resourceHostsDocument, error) {
	emptyHosts := resourceHostsDocument{Script: []string{}, Stylesheet: []string{}}
	brokerIsNil := isNilResourceBroker(job.Broker)
	if !job.Rule.Enabled {
		return emptyHosts, fmt.Errorf("render rule is not enabled")
	}
	if err := validateEffectiveURL(job.EffectiveURL); err != nil {
		return emptyHosts, err
	}
	if job.HTML == "" {
		return emptyHosts, fmt.Errorf("render source HTML must be non-empty valid UTF-8")
	}
	if len(job.HTML) > maxSourceBytes {
		return emptyHosts, fmt.Errorf("source HTML exceeds %d bytes", maxSourceBytes)
	}
	if !utf8.ValidString(job.HTML) {
		return emptyHosts, fmt.Errorf("render source HTML must be non-empty valid UTF-8")
	}
	if err := validateLimits(job.Rule.Limits); err != nil {
		return emptyHosts, err
	}

	switch job.Rule.Mode {
	case renderpolicy.ModeInlineOnly:
		if !brokerIsNil {
			return emptyHosts, fmt.Errorf("inline render must not configure a resource broker")
		}
		if len(job.Rule.ResourceRules) != 0 {
			return emptyHosts, fmt.Errorf("inline render must not retain resource rules")
		}
		if job.Rule.Limits.MaxResourceRequests != 0 ||
			job.Rule.Limits.MaxAggregateResourceBytes != 0 ||
			job.Rule.Limits.MaxResourceBodyBytes != 0 ||
			job.Rule.Limits.MaxRedirectHops != 0 {
			return emptyHosts, fmt.Errorf("inline render resource limits must all be zero")
		}
		return emptyHosts, nil

	case renderpolicy.ModeBrokered:
		if brokerIsNil {
			return emptyHosts, fmt.Errorf("brokered render requires a resource broker")
		}
		if job.Rule.Limits.MaxResourceRequests <= 0 ||
			job.Rule.Limits.MaxAggregateResourceBytes <= 0 ||
			job.Rule.Limits.MaxResourceBodyBytes <= 0 {
			return emptyHosts, fmt.Errorf("brokered render resource limits must be positive")
		}
		if job.Rule.Limits.MaxRedirectHops != 0 {
			return emptyHosts, fmt.Errorf("brokered render redirects are not supported")
		}
		if len(job.Rule.ResourceRules) == 0 {
			return emptyHosts, fmt.Errorf("brokered render requires retained resource rules")
		}
		return deriveResourceHosts(job.Rule.ResourceRules)

	default:
		return emptyHosts, fmt.Errorf("render mode %q is not implemented", job.Rule.Mode)
	}
}

func isNilResourceBroker(broker ResourceBroker) bool {
	if broker == nil {
		return true
	}
	value := reflect.ValueOf(broker)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func validateEffectiveURL(rawURL string) error {
	if rawURL == "" || len(rawURL) > maxEffectiveURLBytes || !utf8.ValidString(rawURL) || strings.IndexByte(rawURL, 0) >= 0 {
		return fmt.Errorf("render effective URL is invalid")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("render effective URL is outside the renderer transport envelope")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("render effective URL is outside the renderer transport envelope")
	}
	return nil
}

func validateLimits(limits renderpolicy.Limits) error {
	if limits.MaxRenderTime <= 0 || limits.MaxRenderTime > maxRenderTime || limits.MaxRenderTime%time.Millisecond != 0 {
		return fmt.Errorf("max render time is invalid")
	}
	if limits.SettleTime < 0 || limits.SettleTime > maxSettleTime || limits.SettleTime%time.Millisecond != 0 {
		return fmt.Errorf("settle time is invalid")
	}
	if limits.MaxResourceRequests < 0 || limits.MaxResourceRequests > maxResourceRequests {
		return fmt.Errorf("max resource requests is invalid")
	}
	if limits.MaxAggregateResourceBytes < 0 || limits.MaxAggregateResourceBytes > maxResourceBytes {
		return fmt.Errorf("max aggregate resource bytes is invalid")
	}
	if limits.MaxResourceBodyBytes < 0 || limits.MaxResourceBodyBytes > maxResourceBodyBytes {
		return fmt.Errorf("max resource body bytes is invalid")
	}
	if limits.MaxRenderedDOMBytes <= 0 || limits.MaxRenderedDOMBytes > maxRenderedDOMBytes {
		return fmt.Errorf("max rendered DOM bytes is invalid")
	}
	if limits.MaxDOMNodes <= 0 || limits.MaxDOMNodes > maxDOMNodes {
		return fmt.Errorf("max DOM nodes is invalid")
	}
	if limits.MaxRedirectHops < 0 || limits.MaxRedirectHops > 3 {
		return fmt.Errorf("max redirect hops is invalid")
	}
	if limits.MaxConsoleBytes < 0 || limits.MaxConsoleBytes > maxConsoleBytes {
		return fmt.Errorf("max console bytes is invalid")
	}
	return nil
}

func deriveResourceHosts(rules []renderpolicy.ResourceRule) (resourceHostsDocument, error) {
	scriptHosts := make(map[string]struct{})
	stylesheetHosts := make(map[string]struct{})
	for index, rule := range rules {
		if len(rule.Host) > 253 || !resourceHostPattern.MatchString(rule.Host) || net.ParseIP(rule.Host) != nil {
			return resourceHostsDocument{}, fmt.Errorf("resource rule %d has an invalid host", index)
		}
		if len(rule.AllowPaths) == 0 && len(rule.AllowPathPrefixes) == 0 {
			return resourceHostsDocument{}, fmt.Errorf("resource rule %d has no retained allow path", index)
		}
		if len(rule.AllowedTypes) == 0 {
			return resourceHostsDocument{}, fmt.Errorf("resource rule %d has no retained resource type", index)
		}
		for _, resourceType := range rule.AllowedTypes {
			switch resourceType {
			case renderpolicy.ResourceTypeScript:
				scriptHosts[rule.Host] = struct{}{}
			case renderpolicy.ResourceTypeStylesheet:
				stylesheetHosts[rule.Host] = struct{}{}
			default:
				return resourceHostsDocument{}, fmt.Errorf("resource rule %d retains unsupported type %q", index, resourceType)
			}
		}
	}
	hosts := resourceHostsDocument{
		Script:     setKeys(scriptHosts),
		Stylesheet: setKeys(stylesheetHosts),
	}
	return hosts, nil
}

func setKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func inspectFrameKind(payload []byte) (string, error) {
	var object map[string]json.RawMessage
	if err := strictjson.Decode(payload, &object); err != nil {
		return "", fmt.Errorf("inspect worker frame: %w", err)
	}
	if object == nil {
		return "", fmt.Errorf("worker frame must be an object")
	}
	rawKind, exists := object["kind"]
	if !exists {
		return "", fmt.Errorf("worker frame is missing kind")
	}
	var kind string
	if err := strictjson.Decode(rawKind, &kind); err != nil || kind == "" {
		return "", fmt.Errorf("worker frame kind is invalid")
	}
	return kind, nil
}

func validateResourceIntentIdentity(
	document resourceIntentDocument,
	jobID string,
	expectedIntentID int,
) (int, error) {
	if document.Version == nil || document.Kind == nil || document.JobID == nil || document.IntentID == nil ||
		document.URL == nil || document.Method == nil || document.ResourceType == nil {
		return 0, fmt.Errorf("resource intent is missing required fields")
	}
	if *document.Version != ProtocolVersion {
		return 0, fmt.Errorf("resource intent version %d is unsupported", *document.Version)
	}
	if *document.Kind != resourceIntentKind {
		return 0, fmt.Errorf("resource intent kind is invalid")
	}
	if *document.JobID != jobID {
		return 0, fmt.Errorf("resource intent job ID does not match the active request")
	}
	if *document.IntentID != expectedIntentID {
		return 0, fmt.Errorf("resource intent ID %d does not match expected ID %d", *document.IntentID, expectedIntentID)
	}
	return *document.IntentID, nil
}

func validateResourceIntent(document resourceIntentDocument) (ResourceIntent, error) {
	if *document.Method != "GET" {
		return ResourceIntent{}, fmt.Errorf("resource intent method is invalid")
	}
	if *document.ResourceType != renderpolicy.ResourceTypeScript && *document.ResourceType != renderpolicy.ResourceTypeStylesheet {
		return ResourceIntent{}, fmt.Errorf("resource intent type is invalid")
	}
	rawURL := *document.URL
	if rawURL == "" || len(rawURL) > maxEffectiveURLBytes || !utf8.ValidString(rawURL) || strings.IndexByte(rawURL, 0) >= 0 {
		return ResourceIntent{}, fmt.Errorf("resource intent URL is invalid")
	}
	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil || identity.CanonicalURL != rawURL {
		return ResourceIntent{}, fmt.Errorf("resource intent URL is not canonical")
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return ResourceIntent{}, fmt.Errorf("resource intent URL is outside the direct HTTPS envelope: %w", err)
	}
	parsed, err := url.Parse(identity.CanonicalURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Port() != "" || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawFragment != "" ||
		!utils.IsUnambiguousFetchPath(parsed) {
		return ResourceIntent{}, fmt.Errorf("resource intent URL is outside the direct HTTPS envelope")
	}
	return ResourceIntent{
		JobID:    *document.JobID,
		IntentID: *document.IntentID,
		URL:      rawURL,
		Method:   *document.Method,
		Type:     *document.ResourceType,
	}, nil
}

func validateResource(
	resource Resource,
	resourceType renderpolicy.ResourceType,
	limits renderpolicy.Limits,
	aggregateBytes int64,
) (int64, error) {
	if len(resource.Body) == 0 {
		return 0, fmt.Errorf("resource body is empty")
	}
	bodyBytes := int64(len(resource.Body))
	if bodyBytes > limits.MaxResourceBodyBytes {
		return 0, fmt.Errorf("resource body exceeds the per-resource byte limit")
	}
	if aggregateBytes < 0 || aggregateBytes > limits.MaxAggregateResourceBytes ||
		bodyBytes > limits.MaxAggregateResourceBytes-aggregateBytes {
		return 0, fmt.Errorf("resource body exceeds the aggregate byte limit")
	}
	if !utf8.Valid(resource.Body) {
		return 0, fmt.Errorf("resource body is not valid UTF-8")
	}
	if err := validateContentType(resource.ContentType, resourceType); err != nil {
		return 0, err
	}
	return bodyBytes, nil
}

func validateContentType(contentType string, resourceType renderpolicy.ResourceType) error {
	if contentType == "" || len(contentType) > maxContentTypeBytes || !utf8.ValidString(contentType) || strings.TrimSpace(contentType) != contentType {
		return fmt.Errorf("resource content type is invalid")
	}
	for _, character := range contentType {
		if unicode.IsControl(character) {
			return fmt.Errorf("resource content type is invalid")
		}
	}
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return fmt.Errorf("resource content type is invalid")
	}
	if charset, present := parameters["charset"]; present && !strings.EqualFold(charset, "utf-8") {
		return fmt.Errorf("resource content type charset must be UTF-8")
	}
	allowed := false
	switch resourceType {
	case renderpolicy.ResourceTypeScript:
		allowed = mediaType == "application/javascript" || mediaType == "text/javascript"
	case renderpolicy.ResourceTypeStylesheet:
		allowed = mediaType == "text/css"
	}
	if !allowed {
		return fmt.Errorf("resource content type %q is invalid for %s", mediaType, resourceType)
	}
	return nil
}

func deniedResourceReply(jobID string, intentID int) resourceReplyDocument {
	return resourceReplyDocument{
		Version:     ProtocolVersion,
		Kind:        resourceReplyKind,
		JobID:       jobID,
		IntentID:    intentID,
		Status:      "error",
		StatusCode:  0,
		ContentType: "",
		BodyBase64:  "",
		BodyBytes:   0,
		ErrorCode:   "resource_denied",
	}
}

type brokerFetchOutcome struct {
	resource Resource
	err      error
}

func fetchResourceWhileReplyOutstanding(
	ctx context.Context,
	framePump *frameReadPump,
	broker ResourceBroker,
	intent ResourceIntent,
) (brokerFetchOutcome, error) {
	brokerContext, cancelBroker := context.WithCancel(ctx)
	defer cancelBroker()

	outcomes := make(chan brokerFetchOutcome, 1)
	go func() {
		resource, err := broker.Fetch(brokerContext, intent)
		outcomes <- brokerFetchOutcome{resource: resource, err: err}
	}()

	select {
	case outcome := <-outcomes:
		if err := framePump.rejectFrameBeforeReply(); err != nil {
			return brokerFetchOutcome{}, err
		}
		if err := ctx.Err(); err != nil {
			return brokerFetchOutcome{}, temporaryError("worker_read_failed", err)
		}
		return outcome, nil

	case event, open := <-framePump.events:
		cancelBroker()
		return brokerFetchOutcome{}, frameBeforeReplyError(event, open)

	case <-ctx.Done():
		if err := framePump.rejectFrameBeforeReply(); err != nil {
			return brokerFetchOutcome{}, err
		}
		return brokerFetchOutcome{}, temporaryError("worker_read_failed", ctx.Err())
	}
}

func denyResourceAndRequireTerminal(
	framePump *frameReadPump,
	connection net.Conn,
	jobID string,
	intentID int,
	rule renderpolicy.Rule,
	resourceRequests int,
	resourceBytes int64,
	cause error,
) error {
	if err := writeResourceReply(framePump, connection, deniedResourceReply(jobID, intentID)); err != nil {
		return errors.Join(cause, err)
	}
	if err := requireDeniedResourceTerminal(
		framePump,
		jobID,
		rule,
		resourceRequests,
		resourceBytes,
	); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func writeResourceReply(framePump *frameReadPump, connection net.Conn, reply resourceReplyDocument) error {
	if err := framePump.rejectFrameBeforeReply(); err != nil {
		return err
	}
	payload, err := json.Marshal(reply)
	if err != nil {
		return framePump.failResourceReply(fmt.Errorf("encode worker reply: %w", err))
	}
	if len(payload) == 0 || len(payload) > maxFrameBytes {
		return framePump.failResourceReply(fmt.Errorf("invalid frame length %d", len(payload)))
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	if err := writeAll(connection, header); err != nil {
		return framePump.failResourceReply(err)
	}
	if err := writeAll(connection, payload[:len(payload)-1]); err != nil {
		return framePump.failResourceReply(err)
	}
	if err := framePump.beginResourceReplyFinalization(); err != nil {
		return err
	}
	return framePump.completeResourceReply(writeAll(connection, payload[len(payload)-1:]))
}

func requireDeniedResourceTerminal(
	framePump *frameReadPump,
	jobID string,
	rule renderpolicy.Rule,
	resourceRequests int,
	resourceBytes int64,
) error {
	payload, err := framePump.readFrame()
	if err != nil {
		return temporaryError("worker_read_failed", err)
	}
	kind, err := inspectFrameKind(payload)
	if err != nil {
		return protocolError(err)
	}
	if kind != renderResultKind {
		return protocolError(fmt.Errorf("worker must terminate after a denied resource, got %q", kind))
	}
	var document renderResultDocument
	if err := strictjson.Decode(payload, &document); err != nil {
		return protocolError(fmt.Errorf("decode render result after denied resource: %w", err))
	}
	if err := validateBrokerFailureResult(document, jobID, rule, resourceRequests, resourceBytes); err != nil {
		return protocolError(err)
	}
	return framePump.requireCleanEOF()
}

func validateBrokerFailureResult(
	document renderResultDocument,
	jobID string,
	rule renderpolicy.Rule,
	resourceRequests int,
	resourceBytes int64,
) error {
	if err := validateRenderResultFields(document, jobID); err != nil {
		return err
	}
	if *document.Status != "error" || *document.ErrorCode != "resource_denied" {
		return fmt.Errorf("worker did not return a matching resource_denied terminal error")
	}
	if *document.HTML != "" || *document.DOMNodes != 0 {
		return fmt.Errorf("denied resource terminal result included page content")
	}
	if *document.ResourceRequests != resourceRequests || *document.ResourceBytes != resourceBytes {
		return fmt.Errorf("denied resource terminal counters do not match authoritative counters")
	}
	if *document.ConsoleBytes > rule.Limits.MaxConsoleBytes {
		return fmt.Errorf("denied resource terminal result exceeds the console-output limit")
	}
	return nil
}

func validateRenderResult(
	document renderResultDocument,
	jobID string,
	rule renderpolicy.Rule,
	authoritativeRequests int,
	authoritativeBytes int64,
) (Result, error) {
	if err := validateRenderResultFields(document, jobID); err != nil {
		return Result{}, err
	}
	if *document.ConsoleBytes > rule.Limits.MaxConsoleBytes {
		return Result{}, fmt.Errorf("render result exceeds the console-output limit")
	}
	if rule.Mode == renderpolicy.ModeInlineOnly && (*document.ResourceRequests != 0 || *document.ResourceBytes != 0) {
		return Result{}, fmt.Errorf("inline render reported brokered resources")
	}

	if *document.Status == "error" {
		if *document.HTML != "" || *document.DOMNodes != 0 {
			return Result{}, fmt.Errorf("failed render result included page content")
		}
		if !errorCodePattern.MatchString(*document.ErrorCode) {
			return Result{}, fmt.Errorf("render worker returned an invalid error code")
		}
		if *document.ResourceRequests != authoritativeRequests {
			return Result{}, fmt.Errorf("failed render result resource-request count does not match the authoritative count")
		}
		if *document.ResourceBytes != authoritativeBytes {
			return Result{}, fmt.Errorf("failed render result resource-byte count does not match the authoritative count")
		}
		workerError := &Error{Code: *document.ErrorCode}
		workerError.Temporary = *document.ErrorCode == "worker_busy" || *document.ErrorCode == "render_failed"
		return Result{}, workerError
	}
	if *document.Status != "ok" || *document.ErrorCode != "" {
		return Result{}, fmt.Errorf("render result has an invalid terminal state")
	}
	if *document.ResourceRequests != authoritativeRequests {
		return Result{}, fmt.Errorf("render result resource-request count does not match the authoritative count")
	}
	if *document.ResourceBytes != authoritativeBytes {
		return Result{}, fmt.Errorf("render result resource-byte count does not match the authoritative count")
	}
	if *document.DOMNodes < 1 || *document.DOMNodes > rule.Limits.MaxDOMNodes {
		return Result{}, fmt.Errorf("render result exceeds the DOM-node limit")
	}
	if len(*document.HTML) == 0 || !utf8.ValidString(*document.HTML) || int64(len(*document.HTML)) > rule.Limits.MaxRenderedDOMBytes {
		return Result{}, fmt.Errorf("render result exceeds the DOM-byte limit")
	}
	return Result{
		HTML:             *document.HTML,
		DOMNodes:         *document.DOMNodes,
		ConsoleBytes:     *document.ConsoleBytes,
		ResourceRequests: *document.ResourceRequests,
		ResourceBytes:    *document.ResourceBytes,
	}, nil
}

func validateRenderResultFields(document renderResultDocument, jobID string) error {
	if document.Version == nil || document.Kind == nil || document.JobID == nil || document.Status == nil ||
		document.HTML == nil || document.DOMNodes == nil || document.ConsoleBytes == nil ||
		document.ResourceRequests == nil || document.ResourceBytes == nil || document.ErrorCode == nil {
		return fmt.Errorf("render result is missing required fields")
	}
	if *document.Version != ProtocolVersion {
		return fmt.Errorf("render result version %d is unsupported", *document.Version)
	}
	if *document.Kind != renderResultKind {
		return fmt.Errorf("render result kind is invalid")
	}
	if *document.JobID != jobID {
		return fmt.Errorf("render result job ID does not match the active request")
	}
	if *document.DOMNodes < 0 || *document.ConsoleBytes < 0 || *document.ResourceRequests < 0 || *document.ResourceBytes < 0 {
		return fmt.Errorf("render result contains a negative counter")
	}
	return nil
}

type frameReadEventKind uint8

const (
	frameReadStarted frameReadEventKind = iota + 1
	frameReadComplete
	frameReadFailed
)

const (
	resourceReplyIdle uint32 = iota
	resourceReplyOutstanding
	resourceReplyViolated
	resourceReplyFinalizing
)

type frameReadEvent struct {
	kind       frameReadEventKind
	generation uint64
	payload    []byte
	err        error
}

// frameReadPump is the sole reader for a worker connection. A frame-start
// event is emitted after the first byte, before the remaining header or body is
// consumed. That lets the render state machine reject complete or partial
// worker input while a resource reply is outstanding without peeking at the
// socket or racing a second reader.
type frameReadPump struct {
	connection         net.Conn
	events             chan frameReadEvent
	stopSignal         chan struct{}
	done               chan struct{}
	replyState         atomic.Uint32
	inputGeneration    atomic.Uint64
	consumedGeneration uint64
	replyGeneration    uint64
}

func newFrameReadPump(connection net.Conn) *frameReadPump {
	pump := &frameReadPump{
		connection: connection,
		events:     make(chan frameReadEvent),
		stopSignal: make(chan struct{}),
		done:       make(chan struct{}),
	}
	go pump.run()
	return pump
}

func (pump *frameReadPump) run() {
	defer close(pump.done)
	defer close(pump.events)

	for {
		var firstHeaderByte [1]byte
		if _, err := io.ReadFull(pump.connection, firstHeaderByte[:]); err != nil {
			pump.send(frameReadEvent{kind: frameReadFailed, err: err})
			return
		}
		// This increment is the observation linearization point. Reply phase
		// transitions check the generation both before and after their CAS.
		generation := pump.inputGeneration.Add(1)
		if pump.replyState.CompareAndSwap(resourceReplyOutstanding, resourceReplyViolated) {
			// Definite input before final-byte publication interrupts a blocked
			// prefix write. The protocol error takes precedence over its timeout.
			if err := pump.connection.SetWriteDeadline(time.Now()); err != nil {
				_ = pump.connection.Close()
			}
		}
		if !pump.send(frameReadEvent{kind: frameReadStarted, generation: generation}) {
			return
		}

		var remainingHeader [3]byte
		if _, err := io.ReadFull(pump.connection, remainingHeader[:]); err != nil {
			pump.send(frameReadEvent{kind: frameReadFailed, err: err})
			return
		}
		header := [4]byte{
			firstHeaderByte[0],
			remainingHeader[0],
			remainingHeader[1],
			remainingHeader[2],
		}
		length := int64(binary.BigEndian.Uint32(header[:]))
		if length <= 0 || length > maxFrameBytes {
			pump.send(frameReadEvent{
				kind: frameReadFailed,
				err:  fmt.Errorf("invalid frame length %d", length),
			})
			return
		}

		payload := make([]byte, int(length))
		if _, err := io.ReadFull(pump.connection, payload); err != nil {
			pump.send(frameReadEvent{kind: frameReadFailed, err: err})
			return
		}
		if !pump.send(frameReadEvent{kind: frameReadComplete, payload: payload}) {
			return
		}
	}
}

func (pump *frameReadPump) send(event frameReadEvent) bool {
	select {
	case pump.events <- event:
		return true
	case <-pump.stopSignal:
		return false
	}
}

func (pump *frameReadPump) readFrame() ([]byte, error) {
	started, open := <-pump.events
	if !open {
		return nil, io.EOF
	}
	if started.kind == frameReadFailed {
		return nil, started.err
	}
	if started.kind != frameReadStarted {
		return nil, fmt.Errorf("frame reader entered an invalid state")
	}

	completed, open := <-pump.events
	if !open {
		return nil, io.EOF
	}
	if completed.kind == frameReadFailed {
		return nil, completed.err
	}
	if completed.kind != frameReadComplete {
		return nil, fmt.Errorf("frame reader entered an invalid state")
	}
	pump.consumedGeneration = started.generation
	return completed.payload, nil
}

func (pump *frameReadPump) beginResourceReply() error {
	pump.replyGeneration = pump.consumedGeneration
	if !pump.replyState.CompareAndSwap(resourceReplyIdle, resourceReplyOutstanding) {
		return protocolError(fmt.Errorf("resource reply state is invalid"))
	}
	if pump.inputGeneration.Load() != pump.replyGeneration {
		pump.replyState.Store(resourceReplyViolated)
		return protocolError(fmt.Errorf("worker sent bytes before the outstanding resource reply completed"))
	}
	return nil
}

func (pump *frameReadPump) failResourceReply(cause error) error {
	state := pump.replyState.Swap(resourceReplyIdle)
	if state == resourceReplyViolated || pump.inputGeneration.Load() != pump.replyGeneration {
		return protocolError(fmt.Errorf("worker sent bytes before the outstanding resource reply completed"))
	}
	if state != resourceReplyOutstanding {
		return protocolError(fmt.Errorf("resource reply state is invalid"))
	}
	return temporaryError("worker_write_failed", cause)
}

func (pump *frameReadPump) beginResourceReplyFinalization() error {
	if pump.inputGeneration.Load() != pump.replyGeneration {
		return protocolError(fmt.Errorf("worker sent bytes before the outstanding resource reply completed"))
	}
	if pump.replyState.CompareAndSwap(resourceReplyOutstanding, resourceReplyFinalizing) {
		if pump.inputGeneration.Load() == pump.replyGeneration {
			return nil
		}
		pump.replyState.Store(resourceReplyViolated)
		return protocolError(fmt.Errorf("worker sent bytes before the outstanding resource reply completed"))
	}
	if pump.replyState.Load() == resourceReplyViolated {
		return protocolError(fmt.Errorf("worker sent bytes before the outstanding resource reply completed"))
	}
	return protocolError(fmt.Errorf("resource reply state is invalid"))
}

func (pump *frameReadPump) completeResourceReply(writeErr error) error {
	state := pump.replyState.Swap(resourceReplyIdle)
	observed := pump.inputGeneration.Load() != pump.replyGeneration
	if observed {
		if writeErr != nil {
			return protocolError(fmt.Errorf("worker sent bytes before resource reply publication completed: %w", writeErr))
		}
	}
	if state != resourceReplyFinalizing {
		return protocolError(fmt.Errorf("resource reply state is invalid"))
	}
	if writeErr != nil {
		return temporaryError("worker_write_failed", writeErr)
	}
	return nil
}

func (pump *frameReadPump) rejectFrameBeforeReply() error {
	if pump.replyState.Load() == resourceReplyViolated || pump.inputGeneration.Load() != pump.replyGeneration {
		return protocolError(fmt.Errorf("worker sent bytes before the outstanding resource reply completed"))
	}
	select {
	case event, open := <-pump.events:
		return frameBeforeReplyError(event, open)
	default:
		return nil
	}
}

func frameBeforeReplyError(event frameReadEvent, open bool) error {
	if !open {
		return temporaryError("worker_read_failed", io.EOF)
	}
	if event.kind == frameReadFailed {
		return temporaryError("worker_read_failed", event.err)
	}
	return protocolError(fmt.Errorf("worker sent bytes before the outstanding resource reply"))
}

func (pump *frameReadPump) requireCleanEOF() error {
	event, open := <-pump.events
	if !open {
		return nil
	}
	if event.kind == frameReadFailed {
		if errors.Is(event.err, io.EOF) {
			return nil
		}
		return temporaryError("worker_read_failed", event.err)
	}
	return protocolError(fmt.Errorf("worker sent trailing bytes after the terminal render result"))
}

func (pump *frameReadPump) stop() {
	close(pump.stopSignal)
	_ = pump.connection.SetReadDeadline(time.Now())
	_ = pump.connection.Close()
	<-pump.done
}

func newJobID() (string, error) {
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return "", err
	}
	return hex.EncodeToString(identifier), nil
}

func writeFrame(writer io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > maxFrameBytes {
		return fmt.Errorf("invalid frame length %d", len(payload))
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	if err := writeAll(writer, header); err != nil {
		return err
	}
	return writeAll(writer, payload)
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func readFrame(reader io.Reader) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	length := int64(binary.BigEndian.Uint32(header))
	if length <= 0 || length > maxFrameBytes {
		return nil, fmt.Errorf("invalid frame length %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

var _ Renderer = (*Client)(nil)
