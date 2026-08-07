package channel

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type socks5ProxyObservation struct {
	targetAddress string
}

func startSOCKS5ForwardProxy(t *testing.T, upstreamAddress string) (string, <-chan socks5ProxyObservation, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	observations := make(chan socks5ProxyObservation, 1)
	errors := make(chan error, 1)
	go func() {
		clientConn, err := listener.Accept()
		if err != nil {
			errors <- err
			return
		}
		defer clientConn.Close()
		reader := bufio.NewReader(clientConn)

		greeting := make([]byte, 2)
		if _, err := io.ReadFull(reader, greeting); err != nil {
			errors <- err
			return
		}
		if greeting[0] != 5 || greeting[1] == 0 {
			errors <- fmt.Errorf("invalid SOCKS5 greeting")
			return
		}
		methods := make([]byte, int(greeting[1]))
		if _, err := io.ReadFull(reader, methods); err != nil {
			errors <- err
			return
		}
		if _, err := clientConn.Write([]byte{5, 0}); err != nil {
			errors <- err
			return
		}

		requestHeader := make([]byte, 4)
		if _, err := io.ReadFull(reader, requestHeader); err != nil {
			errors <- err
			return
		}
		if requestHeader[0] != 5 || requestHeader[1] != 1 {
			errors <- fmt.Errorf("unsupported SOCKS5 command")
			return
		}

		var host string
		switch requestHeader[3] {
		case 1:
			address := make([]byte, net.IPv4len)
			if _, err := io.ReadFull(reader, address); err != nil {
				errors <- err
				return
			}
			host = net.IP(address).String()
		case 3:
			length, err := reader.ReadByte()
			if err != nil {
				errors <- err
				return
			}
			address := make([]byte, int(length))
			if _, err := io.ReadFull(reader, address); err != nil {
				errors <- err
				return
			}
			host = string(address)
		case 4:
			address := make([]byte, net.IPv6len)
			if _, err := io.ReadFull(reader, address); err != nil {
				errors <- err
				return
			}
			host = net.IP(address).String()
		default:
			errors <- fmt.Errorf("unsupported SOCKS5 address type")
			return
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(reader, portBytes); err != nil {
			errors <- err
			return
		}
		targetAddress := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes))))

		upstreamConn, err := net.DialTimeout("tcp", upstreamAddress, 5*time.Second)
		if err != nil {
			errors <- err
			return
		}
		defer upstreamConn.Close()
		if _, err := clientConn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
			errors <- err
			return
		}
		observations <- socks5ProxyObservation{targetAddress: targetAddress}

		upstreamDone := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstreamConn, reader)
			if tcpConn, ok := upstreamConn.(*net.TCPConn); ok {
				_ = tcpConn.CloseWrite()
			}
			close(upstreamDone)
		}()
		_, _ = io.Copy(clientConn, upstreamConn)
		<-upstreamDone
	}()

	return listener.Addr().String(), observations, errors
}

func TestProcessHeaderOverride_MonitorHeadersHaveFinalSafePrecedence(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		IsChannelTest:    true,
		IsChannelMonitor: true,
		MonitorHeadersOverride: map[string]string{
			"Authorization": "Bearer forged",
			"X-Probe-Mode":  "monitor",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "channel-secret",
			HeadersOverride: map[string]any{
				"Authorization": "Bearer {api_key}",
				"X-Probe-Mode":  "channel",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Bearer channel-secret", headers["authorization"])
	require.Equal(t, "monitor", headers["x-probe-mode"])
}

func TestDoRequest_MonitorUsesChannelHTTPProxy(t *testing.T) {
	t.Parallel()

	proxiedURLs := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedURLs <- r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, err := io.WriteString(w, `{"ok":true}`)
		require.NoError(t, err)
	}))
	t.Cleanup(proxy.Close)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(`{}`))
	request, err := http.NewRequest(
		http.MethodPost,
		"http://upstream.invalid/v1/probe",
		strings.NewReader(`{"model":"test"}`),
	)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		IsChannelMonitor: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{Proxy: proxy.URL},
		},
	}

	response, err := doRequest(ctx, request, info)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "http://upstream.invalid/v1/probe", <-proxiedURLs)
}

func TestDoRequest_MonitorUsesChannelSOCKSProxy(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			upstreamRequests := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamRequests <- r.Host + r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Connection", "close")
				_, _ = io.WriteString(w, `{"ok":true}`)
			}))
			t.Cleanup(upstream.Close)
			upstreamURL, err := url.Parse(upstream.URL)
			require.NoError(t, err)
			proxyAddress, observations, proxyErrors := startSOCKS5ForwardProxy(t, upstreamURL.Host)

			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(`{}`))
			request, err := http.NewRequest(
				http.MethodPost,
				"http://upstream.invalid:18080/v1/probe",
				strings.NewReader(`{"model":"test"}`),
			)
			require.NoError(t, err)
			request.Close = true
			info := &relaycommon.RelayInfo{
				IsChannelMonitor: true,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelSetting: dto.ChannelSettings{Proxy: scheme + "://" + proxyAddress},
				},
			}

			response, err := doRequest(ctx, request, info)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.JSONEq(t, `{"ok":true}`, string(body))
			require.Equal(t, "upstream.invalid:18080", (<-observations).targetAddress)
			require.Equal(t, "upstream.invalid:18080/v1/probe", <-upstreamRequests)
			select {
			case proxyErr := <-proxyErrors:
				require.NoError(t, proxyErr)
			default:
			}
		})
	}
}

func TestWebSocketDialer_UsesChannelSOCKSProxy(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			upstreamErrors := make(chan error, 1)
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					upstreamErrors <- err
					return
				}
				defer conn.Close()
				if err := conn.WriteMessage(websocket.TextMessage, []byte("ok")); err != nil {
					upstreamErrors <- err
				}
			}))
			t.Cleanup(upstream.Close)
			upstreamURL, err := url.Parse(upstream.URL)
			require.NoError(t, err)
			proxyAddress, observations, proxyErrors := startSOCKS5ForwardProxy(t, upstreamURL.Host)

			dialer, err := service.NewWebSocketDialerWithProxy(scheme+"://"+proxyAddress, 5*time.Second)
			require.NoError(t, err)
			conn, _, err := dialer.DialContext(
				context.Background(),
				"ws://upstream.invalid:18080/probe",
				nil,
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			messageType, message, err := conn.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, websocket.TextMessage, messageType)
			require.Equal(t, "ok", string(message))
			require.Equal(t, "upstream.invalid:18080", (<-observations).targetAddress)
			select {
			case proxyErr := <-proxyErrors:
				require.NoError(t, proxyErr)
			default:
			}
			select {
			case upstreamErr := <-upstreamErrors:
				require.NoError(t, upstreamErr)
			default:
			}
		})
	}
}

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
