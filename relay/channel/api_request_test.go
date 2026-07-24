package channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_ChannelMonitorHeadersHaveFinalPrecedence(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		IsChannelMonitor: true,
		MonitorHeadersOverride: map[string]string{
			"User-Agent":            "codex-tui/0.145.0",
			"Originator":            "codex-tui",
			"X-Client-Request-Id":   "request-123",
			"X-Codex-Beta-Features": "remote_compaction_v2",
			"Authorization":         "Bearer forged",
			"ChatGPT-Account-Id":    "forged-account",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"User-Agent": "channel-client",
				"Originator": "channel-originator",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	assert.Equal(t, "codex-tui/0.145.0", headers["user-agent"])
	assert.Equal(t, "codex-tui", headers["originator"])
	assert.Equal(t, "request-123", headers["x-client-request-id"])
	assert.Equal(t, "remote_compaction_v2", headers["x-codex-beta-features"])
	assert.NotContains(t, headers, "authorization")
	assert.NotContains(t, headers, "chatgpt-account-id")

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/backend-api/codex/responses", nil)
	upstreamReq.Header.Set("Authorization", "Bearer channel-token")
	upstreamReq.Header.Set("ChatGPT-Account-Id", "channel-account")
	applyHeaderOverrideToRequest(upstreamReq, headers)
	assert.Equal(t, "codex-tui/0.145.0", upstreamReq.Header.Get("User-Agent"))
	assert.Equal(t, "codex-tui", upstreamReq.Header.Get("Originator"))
	assert.Equal(t, "Bearer channel-token", upstreamReq.Header.Get("Authorization"))
	assert.Equal(t, "channel-account", upstreamReq.Header.Get("ChatGPT-Account-Id"))
}

func TestProcessHeaderOverride_NormalRequestIgnoresMonitorHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		IsChannelMonitor: false,
		MonitorHeadersOverride: map[string]string{
			"User-Agent": "monitor-client",
			"Originator": "monitor-originator",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"User-Agent": "normal-client",
				"Originator": "normal-originator",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	assert.Equal(t, "normal-client", headers["user-agent"])
	assert.Equal(t, "normal-originator", headers["originator"])
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}

func TestDoRequest_ManualMonitorTraceCapturesActualExchange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(body)
		assert.Equal(t, "codex-tui/0.145.0", r.Header.Get("User-Agent"))
		assert.Equal(t, "request-123", r.Header.Get("X-Client-Request-Id"))
		w.Header().Set("X-Upstream-Trace", "response-456")
		w.WriteHeader(http.StatusCreated)
		_, err = w.Write([]byte(`{"ok":true}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"hello"}`))
	trace := &relaycommon.MonitorProbeTrace{}
	info := &relaycommon.RelayInfo{
		IsChannelMonitor: true,
		MonitorTrace:     trace,
		ChannelMeta:      &relaycommon.ChannelMeta{},
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"input":"hello"}`))
	require.NoError(t, err)
	request.Header.Set("User-Agent", "codex-tui/0.145.0")
	request.Header.Set("X-Client-Request-Id", "request-123")

	response, err := DoRequest(ctx, request, info)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	snapshot := trace.Snapshot()
	assert.Equal(t, `{"input":"hello"}`, receivedBody)
	assert.Equal(t, http.MethodPost, snapshot.RequestMethod)
	assert.Equal(t, server.URL+"/v1/responses", snapshot.RequestURL)
	assert.Equal(t, []string{"codex-tui/0.145.0"}, snapshot.RequestHeaders["User-Agent"])
	assert.Equal(t, []string{"request-123"}, snapshot.RequestHeaders["X-Client-Request-Id"])
	assert.Equal(t, `{"input":"hello"}`, snapshot.RequestBody)
	assert.False(t, snapshot.RequestBodyTruncated)
	assert.Equal(t, http.StatusCreated, snapshot.ResponseStatusCode)
	assert.Equal(t, "201 Created", snapshot.ResponseStatus)
	assert.Equal(t, []string{"response-456"}, snapshot.ResponseHeaders["X-Upstream-Trace"])
	assert.Equal(t, string(responseBody), snapshot.ResponseBody)
	assert.False(t, snapshot.ResponseBodyTruncated)
}

func TestDoRequest_NormalForwardingDoesNotPopulateMonitorTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"ok":true}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	trace := &relaycommon.MonitorProbeTrace{}
	info := &relaycommon.RelayInfo{
		IsChannelMonitor: false,
		MonitorTrace:     trace,
		ChannelMeta:      &relaycommon.ChannelMeta{},
	}
	request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"model":"m"}`))
	require.NoError(t, err)

	response, err := DoRequest(ctx, request, info)
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	snapshot := trace.Snapshot()
	assert.Empty(t, snapshot.RequestMethod)
	assert.Empty(t, snapshot.RequestBody)
	assert.Empty(t, snapshot.ResponseBody)
}
