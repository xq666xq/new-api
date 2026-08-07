package common

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
)

// MonitorProbeTraceBodyLimit bounds the one-time request/response payload kept
// in memory for an administrator-triggered probe. Scheduled probes leave
// MonitorTrace nil and pay no capture cost.
const MonitorProbeTraceBodyLimit = 256 << 10

const monitorTraceRedacted = "[REDACTED]"

// MonitorProbeTraceSnapshot is the JSON-safe, immutable view returned to the
// administrator after a manual probe. It is never persisted by the model layer.
type MonitorProbeTraceSnapshot struct {
	RequestMethod         string              `json:"request_method"`
	RequestURL            string              `json:"request_url"`
	RequestHeaders        map[string][]string `json:"request_headers"`
	RequestBody           string              `json:"request_body"`
	RequestBodyTruncated  bool                `json:"request_body_truncated"`
	RequestWriteError     string              `json:"request_write_error"`
	ResponseURL           string              `json:"response_url"`
	ResponseStatusCode    int                 `json:"response_status_code"`
	ResponseStatus        string              `json:"response_status"`
	ResponseHeaders       map[string][]string `json:"response_headers"`
	ResponseBody          string              `json:"response_body"`
	ResponseBodyTruncated bool                `json:"response_body_truncated"`
	BodyLimitBytes        int                 `json:"body_limit_bytes"`
}

// MonitorProbeTrace captures the bytes and headers actually handled by the Go
// HTTP transport for one manual monitor probe. Its fields are intentionally
// private so request/response details cannot accidentally be serialized into a
// database model or log structure.
type MonitorProbeTrace struct {
	mu sync.Mutex

	requestMethod      string
	requestURL         string
	fallbackReqHeaders http.Header
	sentReqHeaders     http.Header
	requestBody        bytes.Buffer
	requestBodyCut     bool
	requestWriteError  string

	responseURL     string
	responseStatus  string
	responseCode    int
	responseHeaders http.Header
	responseBody    bytes.Buffer
	responseBodyCut bool
}

type monitorProbeTraceWriter struct {
	trace    *MonitorProbeTrace
	response bool
}

func (w monitorProbeTraceWriter) Write(p []byte) (int, error) {
	w.trace.mu.Lock()
	defer w.trace.mu.Unlock()

	buffer := &w.trace.requestBody
	truncated := &w.trace.requestBodyCut
	if w.response {
		buffer = &w.trace.responseBody
		truncated = &w.trace.responseBodyCut
	}
	remaining := MonitorProbeTraceBodyLimit - buffer.Len()
	if remaining > 0 {
		writeLen := len(p)
		if writeLen > remaining {
			writeLen = remaining
		}
		_, _ = buffer.Write(p[:writeLen])
	}
	if len(p) > remaining {
		*truncated = true
	}
	// The capture is observational. Always report the original byte count so a
	// full in-memory buffer never changes the request or response read path.
	return len(p), nil
}

type monitorProbeTraceReadCloser struct {
	io.Reader
	io.Closer
}

// AttachRequest instruments a fully assembled upstream request immediately
// before client.Do. httptrace records the header fields after net/http adds its
// transport defaults, while TeeReader records the exact request-body bytes read
// by the transport.
func (t *MonitorProbeTrace) AttachRequest(req *http.Request) *http.Request {
	if t == nil || req == nil {
		return req
	}

	fallbackHeaders := req.Header.Clone()
	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}
	if host != "" {
		fallbackHeaders.Set("Host", host)
	}

	t.mu.Lock()
	t.requestMethod = req.Method
	if req.URL != nil {
		t.requestURL = req.URL.String()
	}
	t.fallbackReqHeaders = fallbackHeaders
	t.sentReqHeaders = make(http.Header)
	t.requestBody.Reset()
	t.requestBodyCut = false
	t.requestWriteError = ""
	t.responseURL = ""
	t.responseStatus = ""
	t.responseCode = 0
	t.responseHeaders = nil
	t.responseBody.Reset()
	t.responseBodyCut = false
	t.mu.Unlock()

	clientTrace := &httptrace.ClientTrace{
		WroteHeaderField: func(key string, values []string) {
			t.mu.Lock()
			for _, value := range values {
				t.sentReqHeaders.Add(key, value)
			}
			t.mu.Unlock()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				return
			}
			t.mu.Lock()
			t.requestWriteError = info.Err.Error()
			t.mu.Unlock()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), clientTrace))
	if req.Body != nil {
		req.Body = &monitorProbeTraceReadCloser{
			Reader: io.TeeReader(req.Body, monitorProbeTraceWriter{trace: t}),
			Closer: req.Body,
		}
	}
	return req
}

// AttachResponse snapshots upstream response metadata and wraps the body so the
// adaptor continues consuming it normally while a bounded copy is retained for
// the one-time manual-probe response.
func (t *MonitorProbeTrace) AttachResponse(resp *http.Response) {
	if t == nil || resp == nil {
		return
	}

	t.mu.Lock()
	if resp.Request != nil && resp.Request.URL != nil {
		t.responseURL = resp.Request.URL.String()
	}
	t.responseStatus = resp.Status
	t.responseCode = resp.StatusCode
	t.responseHeaders = resp.Header.Clone()
	t.mu.Unlock()

	if resp.Body != nil {
		resp.Body = &monitorProbeTraceReadCloser{
			Reader: io.TeeReader(resp.Body, monitorProbeTraceWriter{trace: t, response: true}),
			Closer: resp.Body,
		}
	}
}

// Snapshot returns a detached copy safe to marshal after the relay handler has
// completed. Request/response bodies stay only in this in-memory trace object.
func (t *MonitorProbeTrace) Snapshot() MonitorProbeTraceSnapshot {
	if t == nil {
		return MonitorProbeTraceSnapshot{BodyLimitBytes: MonitorProbeTraceBodyLimit}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	requestHeaders := t.sentReqHeaders
	if len(requestHeaders) == 0 {
		requestHeaders = t.fallbackReqHeaders
	}
	return MonitorProbeTraceSnapshot{
		RequestMethod:         t.requestMethod,
		RequestURL:            redactMonitorTraceURL(t.requestURL),
		RequestHeaders:        cloneMonitorTraceHeaders(requestHeaders),
		RequestBody:           t.requestBody.String(),
		RequestBodyTruncated:  t.requestBodyCut,
		RequestWriteError:     t.requestWriteError,
		ResponseURL:           redactMonitorTraceURL(t.responseURL),
		ResponseStatusCode:    t.responseCode,
		ResponseStatus:        t.responseStatus,
		ResponseHeaders:       cloneMonitorTraceHeaders(t.responseHeaders),
		ResponseBody:          t.responseBody.String(),
		ResponseBodyTruncated: t.responseBodyCut,
		BodyLimitBytes:        MonitorProbeTraceBodyLimit,
	}
}

func cloneMonitorTraceHeaders(headers http.Header) map[string][]string {
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		if isSensitiveMonitorHeader(key) {
			cloned[key] = []string{monitorTraceRedacted}
			continue
		}
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func isSensitiveMonitorHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "cookie" || name == "set-cookie" || name == "api-key" {
		return true
	}
	if strings.Contains(name, "authorization") || strings.HasSuffix(name, "-api-key") {
		return true
	}
	return strings.HasSuffix(name, "-access-token") || strings.HasSuffix(name, "-auth-token")
}

func redactMonitorTraceURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	for key := range query {
		if isSensitiveMonitorQueryKey(key) {
			query.Set(key, monitorTraceRedacted)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func isSensitiveMonitorQueryKey(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer("-", "", "_", "", ".", "").Replace(normalized)
	switch normalized {
	case "key", "apikey", "token", "accesstoken", "authtoken", "password", "passwd":
		return true
	}
	return strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "signature") ||
		strings.Contains(normalized, "credential")
}
